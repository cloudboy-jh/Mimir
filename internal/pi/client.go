// Package pi provides a standard-library-only client for Pi's JSONL RPC mode.
package pi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
)

const (
	defaultExecutable  = "pi"
	defaultStderrLimit = 32 << 10
	outputBuffer       = 256
)

var ErrClosed = errors.New("pi RPC client is closed")

// Config controls the Pi subprocess. Args are passed after --mode rpc.
type Config struct {
	Executable  string
	Args        []string
	Dir         string
	StderrLimit int
}

// Envelope contains the common RPC fields and preserves the complete message.
// Event-specific fields remain available through Raw without coupling the UI to
// Pi's evolving event schema.
type Envelope struct {
	Type    string          `json:"type"`
	ID      string          `json:"id,omitempty"`
	Command string          `json:"command,omitempty"`
	Success *bool           `json:"success,omitempty"`
	Error   string          `json:"error,omitempty"`
	Data    json.RawMessage `json:"data,omitempty"`
	Raw     json.RawMessage `json:"-"`
}

func (e Envelope) IsResponse() bool { return e.Type == "response" }

// Client owns one Pi RPC subprocess.
type Client struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	events chan Envelope
	done   chan struct{}
	stop   chan struct{}
	ctx    context.Context

	writeToken chan struct{}
	nextID     atomic.Uint64
	closed     atomic.Bool
	stopOnce   sync.Once
	waitMu     sync.Mutex
	waitErr    error
	stderr     *tailBuffer
}

// Start launches the configured executable in Pi RPC mode.
func Start(ctx context.Context, config Config) (*Client, error) {
	if ctx == nil {
		return nil, errors.New("starting pi RPC: nil context")
	}
	executable := config.Executable
	if executable == "" {
		executable = defaultExecutable
	}
	args := append([]string{"--mode", "rpc"}, config.Args...)

	limit := config.StderrLimit
	if limit <= 0 {
		limit = defaultStderrLimit
	}
	stderr := &tailBuffer{limit: limit}
	cmd := exec.CommandContext(ctx, executable, args...)
	cmd.Dir = config.Dir
	cmd.Stderr = stderr

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("opening pi RPC stdin: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("opening pi RPC stdout: %w", err)
	}
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		return nil, fmt.Errorf("starting pi RPC: %w", err)
	}

	c := &Client{
		cmd:        cmd,
		stdin:      stdin,
		events:     make(chan Envelope, outputBuffer),
		done:       make(chan struct{}),
		stop:       make(chan struct{}),
		ctx:        ctx,
		writeToken: make(chan struct{}, 1),
		stderr:     stderr,
	}
	c.writeToken <- struct{}{}
	go c.run(stdout)
	return c, nil
}

// Events returns responses and asynchronous Pi events in process order.
func (c *Client) Events() <-chan Envelope { return c.events }

// Prompt submits a user message and returns its correlation ID.
func (c *Client) Prompt(ctx context.Context, message string) (string, error) {
	return c.send(ctx, map[string]any{"type": "prompt", "message": message})
}

// Abort asks Pi to abort its current operation.
func (c *Client) Abort(ctx context.Context) (string, error) {
	return c.send(ctx, map[string]any{"type": "abort"})
}

// SetModel switches to a provider/model pair such as "anthropic/claude-sonnet".
func (c *Client) SetModel(ctx context.Context, model string) (string, error) {
	provider, modelID, ok := strings.Cut(strings.TrimSpace(model), "/")
	if !ok || provider == "" || modelID == "" {
		return "", fmt.Errorf("invalid model %q: expected provider/model", model)
	}
	return c.send(ctx, map[string]any{
		"type":     "set_model",
		"provider": provider,
		"modelId":  modelID,
	})
}

func (c *Client) send(ctx context.Context, command map[string]any) (string, error) {
	if ctx == nil {
		return "", errors.New("sending pi RPC command: nil context")
	}
	if c.closed.Load() {
		return "", ErrClosed
	}
	id := fmt.Sprintf("req-%d", c.nextID.Add(1))
	command["id"] = id
	data, err := json.Marshal(command)
	if err != nil {
		return "", fmt.Errorf("encoding pi RPC command: %w", err)
	}
	data = append(data, '\n')

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case <-c.done:
		return "", ErrClosed
	case <-c.writeToken:
	}
	defer func() { c.writeToken <- struct{}{} }()
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if c.closed.Load() {
		return "", ErrClosed
	}
	if err := writeAll(c.stdin, data); err != nil {
		if c.closed.Load() {
			return "", ErrClosed
		}
		return "", fmt.Errorf("writing pi RPC command: %w", err)
	}
	return id, nil
}

func writeAll(w io.Writer, data []byte) error {
	for len(data) > 0 {
		n, err := w.Write(data)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}

func (c *Client) run(stdout io.Reader) {
	readErr := readJSONL(stdout, func(line []byte) error {
		var envelope Envelope
		if err := json.Unmarshal(line, &envelope); err != nil {
			return fmt.Errorf("decoding pi RPC output: %w", err)
		}
		envelope.Raw = append(json.RawMessage(nil), line...)
		select {
		case c.events <- envelope:
			return nil
		case <-c.stop:
			return ErrClosed
		}
	})
	if readErr != nil && !errors.Is(readErr, ErrClosed) && c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
	processErr := c.cmd.Wait()

	c.closed.Store(true)
	_ = c.stdin.Close()
	if c.ctx.Err() != nil {
		c.setWaitErr(c.ctx.Err())
	} else if readErr != nil && !errors.Is(readErr, ErrClosed) {
		c.setWaitErr(c.withDiagnostics(readErr))
	} else if processErr != nil && !c.isStopping() {
		c.setWaitErr(c.withDiagnostics(fmt.Errorf("pi RPC process: %w", processErr)))
	}
	close(c.events)
	close(c.done)
}

func (c *Client) withDiagnostics(err error) error {
	diagnostics := strings.TrimSpace(c.stderr.String())
	if diagnostics == "" {
		return err
	}
	return fmt.Errorf("%w; stderr: %s", err, diagnostics)
}

func (c *Client) setWaitErr(err error) {
	c.waitMu.Lock()
	c.waitErr = err
	c.waitMu.Unlock()
}

func (c *Client) isStopping() bool {
	select {
	case <-c.stop:
		return true
	default:
		return false
	}
}

// Wait blocks until Pi exits and returns its terminal error, if any.
func (c *Client) Wait() error {
	<-c.done
	c.waitMu.Lock()
	defer c.waitMu.Unlock()
	return c.waitErr
}

// Close hard-stops Pi, waits for process cleanup, and is safe to call repeatedly.
func (c *Client) Close() error {
	c.stopOnce.Do(func() {
		c.closed.Store(true)
		close(c.stop)
		_ = c.stdin.Close()
		if c.cmd.Process != nil {
			_ = c.cmd.Process.Kill()
		}
	})
	return c.Wait()
}

// Stderr returns the bounded tail of diagnostics emitted by Pi.
func (c *Client) Stderr() string { return c.stderr.String() }

type tailBuffer struct {
	mu    sync.Mutex
	buf   []byte
	limit int
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(p) >= b.limit {
		b.buf = append(b.buf[:0], p[len(p)-b.limit:]...)
		return len(p), nil
	}
	overflow := len(b.buf) + len(p) - b.limit
	if overflow > 0 {
		copy(b.buf, b.buf[overflow:])
		b.buf = b.buf[:len(b.buf)-overflow]
	}
	b.buf = append(b.buf, p...)
	return len(p), nil
}

func (b *tailBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(bytes.Clone(b.buf))
}
