package mimircli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// mcpClient bounds each forwarded MCP message; tool calls may read archives.
var mcpClient = &http.Client{Timeout: 2 * time.Minute}

// cmdMCP is the stdio MCP bridge harness configs launch: each newline-delimited
// JSON-RPC message on stdin is POSTed to <url>/mcp and the JSON response is
// written as one stdout line (nothing for notifications/202).
func cmdMCP(ctx context.Context, args []string, ioctx IO) error {
	if len(args) != 0 {
		return fmt.Errorf("usage: mimir mcp")
	}
	in := ioctx.In
	if in == nil {
		in = strings.NewReader("")
	}
	bridge := mcpBridge{client: mcpClient, out: ioctx.Out, log: ioctx.Err}
	if pointer, err := loadPointer(); err != nil {
		bridge.unavailable = err.Error()
	} else {
		bridge.endpoint, bridge.token = strings.TrimRight(pointer.URL, "/")+"/mcp", pointer.Token
	}
	return bridge.run(ctx, in)
}

type mcpBridge struct {
	client      *http.Client
	endpoint    string
	token       string
	unavailable string
	out, log    io.Writer
}

func (b mcpBridge) run(ctx context.Context, in io.Reader) error {
	reader := bufio.NewReaderSize(in, 64*1024)
	for {
		line, readErr := reader.ReadBytes('\n')
		if message := bytes.TrimSpace(line); len(message) > 0 {
			if err := b.forward(ctx, message); err != nil {
				return err
			}
		}
		if readErr == io.EOF {
			return nil
		}
		if readErr != nil {
			return readErr
		}
	}
}

// forward relays one message. Transport failures become JSON-RPC errors for
// each request id so the harness never waits on a lost response.
func (b mcpBridge) forward(ctx context.Context, message []byte) error {
	if !json.Valid(message) {
		return b.write([]byte(`{"jsonrpc":"2.0","id":null,"error":{"code":-32700,"message":"Parse error"}}`))
	}
	if b.unavailable != "" {
		return b.fail(message, b.unavailable)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.endpoint, bytes.NewReader(message))
	if err != nil {
		return b.fail(message, err.Error())
	}
	req.Header.Set("authorization", "Bearer "+b.token)
	req.Header.Set("content-type", "application/json")
	req.Header.Set("accept", "application/json")
	res, err := b.client.Do(req)
	if err != nil {
		return b.fail(message, "Mimir request failed: "+err.Error())
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 32<<20))
	if err != nil {
		return b.fail(message, "reading Mimir response: "+err.Error())
	}
	body = bytes.TrimSpace(body)
	switch {
	case res.StatusCode == http.StatusAccepted || (res.StatusCode >= 200 && res.StatusCode < 300 && len(body) == 0):
		return nil
	case res.StatusCode >= 200 && res.StatusCode < 300 && json.Valid(body):
		var compact bytes.Buffer
		if err := json.Compact(&compact, body); err != nil {
			return b.fail(message, err.Error())
		}
		return b.write(compact.Bytes())
	case res.StatusCode == http.StatusNotFound:
		return b.fail(message, errQueryAPIMissing.Error())
	}
	detail := strings.TrimSpace(string(body))
	var payload struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(body, &payload) == nil && payload.Error != "" {
		detail = payload.Error
	}
	return b.fail(message, fmt.Sprintf("Mimir MCP %s: %s", res.Status, detail))
}

func (b mcpBridge) fail(message []byte, detail string) error {
	if b.log != nil {
		fmt.Fprintln(b.log, "mimir mcp: "+detail)
	}
	for _, id := range mcpRequestIDs(message) {
		response, err := json.Marshal(map[string]any{
			"jsonrpc": "2.0", "id": id,
			"error": map[string]any{"code": -32603, "message": detail},
		})
		if err != nil {
			return err
		}
		if err := b.write(response); err != nil {
			return err
		}
	}
	return nil
}

func (b mcpBridge) write(line []byte) error {
	_, err := b.out.Write(append(line, '\n'))
	return err
}

// mcpRequestIDs returns the ids of requests (not notifications) in a single
// message or batch.
func mcpRequestIDs(message []byte) []json.RawMessage {
	type envelope struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
	}
	var batch []envelope
	if json.Unmarshal(message, &batch) != nil {
		var single envelope
		if json.Unmarshal(message, &single) != nil {
			return nil
		}
		batch = []envelope{single}
	}
	var ids []json.RawMessage
	for _, item := range batch {
		if item.Method != "" && len(item.ID) > 0 && string(item.ID) != "null" {
			ids = append(ids, item.ID)
		}
	}
	return ids
}
