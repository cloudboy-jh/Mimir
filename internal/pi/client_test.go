package pi

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	// The client correctly puts --mode rpc first; remove it before the test
	// package parses flags when this binary is reused as the fake Pi process.
	if len(os.Args) >= 3 && os.Args[1] == "--mode" && os.Args[2] == "rpc" {
		os.Args = append(os.Args[:1], os.Args[3:]...)
	}
	os.Exit(m.Run())
}

func TestClientCommandsAndEvents(t *testing.T) {
	c := startHelper(t, "echo")
	defer c.Close()

	type sent struct {
		id      string
		command string
	}
	results := make(chan sent, 3)
	var callers sync.WaitGroup
	callers.Add(3)
	go func() {
		defer callers.Done()
		id, err := c.Prompt(context.Background(), "hello")
		if err != nil {
			t.Errorf("Prompt: %v", err)
		}
		results <- sent{id, "prompt"}
	}()
	go func() {
		defer callers.Done()
		id, err := c.Abort(context.Background())
		if err != nil {
			t.Errorf("Abort: %v", err)
		}
		results <- sent{id, "abort"}
	}()
	go func() {
		defer callers.Done()
		id, err := c.SetModel(context.Background(), "openrouter/vendor/model")
		if err != nil {
			t.Errorf("SetModel: %v", err)
		}
		results <- sent{id, "set_model"}
	}()
	callers.Wait()
	close(results)

	want := map[string]string{}
	for result := range results {
		want[result.id] = result.command
	}
	got := map[string]string{}
	for len(got) < 3 {
		select {
		case envelope := <-c.Events():
			if !envelope.IsResponse() || envelope.Success == nil || !*envelope.Success {
				t.Fatalf("unexpected envelope: %+v", envelope)
			}
			if len(envelope.Raw) == 0 {
				t.Fatal("raw envelope is empty")
			}
			got[envelope.ID] = envelope.Command
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for responses")
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("responses = %#v, want %#v", got, want)
	}
}

func TestSetModelValidation(t *testing.T) {
	c := startHelper(t, "idle")
	defer c.Close()
	for _, model := range []string{"", "provider", "/model", "provider/"} {
		if _, err := c.SetModel(context.Background(), model); err == nil {
			t.Errorf("SetModel(%q) succeeded", model)
		}
	}
}

func TestLargeEventAndWorkingDirectory(t *testing.T) {
	dir := t.TempDir()
	c := startHelperConfig(t, Config{
		Executable: os.Args[0],
		Args:       []string{"-test.run=TestRPCHelperProcess", "--", "large", dir},
		Dir:        dir,
	})
	defer c.Close()

	select {
	case envelope := <-c.Events():
		var event struct {
			Type  string `json:"type"`
			Value string `json:"value"`
			Dir   string `json:"dir"`
		}
		if err := json.Unmarshal(envelope.Raw, &event); err != nil {
			t.Fatal(err)
		}
		if len(event.Value) != 128<<10 {
			t.Fatalf("large value length = %d", len(event.Value))
		}
		if event.Dir != dir {
			t.Fatalf("working directory = %q, want %q", event.Dir, dir)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for large event")
	}
}

func TestContextCancellationCleansUpProcess(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	c := startHelperContext(t, ctx, "idle")
	cancel()
	if err := c.Wait(); err != context.Canceled {
		t.Fatalf("Wait error = %v, want context.Canceled", err)
	}
	if _, err := c.Abort(context.Background()); err != ErrClosed {
		t.Fatalf("Abort error = %v, want ErrClosed", err)
	}
}

func TestStderrDiagnosticsAreBounded(t *testing.T) {
	c := startHelperConfig(t, Config{
		Executable:  os.Args[0],
		Args:        []string{"-test.run=TestRPCHelperProcess", "--", "fail"},
		StderrLimit: 8,
	})
	err := c.Wait()
	if err == nil || !strings.Contains(err.Error(), "3456789") {
		t.Fatalf("Wait error = %v", err)
	}
	if got := c.Stderr(); got != "3456789\n" {
		t.Fatalf("Stderr = %q, want %q", got, "3456789\n")
	}
	if err := c.Close(); err == nil {
		t.Fatal("Close after failed process unexpectedly returned nil")
	}
}

func TestRPCHelperProcess(t *testing.T) {
	separator := -1
	for i, arg := range os.Args {
		if arg == "--" {
			separator = i
			break
		}
	}
	if separator < 0 || separator+1 >= len(os.Args) {
		return
	}
	args := os.Args[separator+1:]
	mode := args[0]
	switch mode {
	case "echo":
		scanner := bufio.NewScanner(os.Stdin)
		encoder := json.NewEncoder(os.Stdout)
		for scanner.Scan() {
			var command map[string]any
			if err := json.Unmarshal(scanner.Bytes(), &command); err != nil {
				os.Exit(2)
			}
			response := map[string]any{
				"type":    "response",
				"id":      command["id"],
				"command": command["type"],
				"success": true,
			}
			if err := encoder.Encode(response); err != nil {
				os.Exit(2)
			}
		}
	case "large":
		dir, _ := os.Getwd()
		fmt.Printf("{\"type\":\"message_update\",\"value\":%q,\"dir\":%q}\n", strings.Repeat("x", 128<<10), dir)
		select {}
	case "idle":
		_, _ = bufio.NewReader(os.Stdin).ReadByte()
		select {}
	case "fail":
		_, _ = fmt.Fprint(os.Stderr, "0123456789\n")
		os.Exit(3)
	default:
		os.Exit(2)
	}
}

func startHelper(t *testing.T, mode string) *Client {
	t.Helper()
	return startHelperContext(t, context.Background(), mode)
}

func startHelperContext(t *testing.T, ctx context.Context, mode string) *Client {
	t.Helper()
	return startHelperConfigContext(t, ctx, Config{
		Executable: os.Args[0],
		Args:       []string{"-test.run=TestRPCHelperProcess", "--", mode},
	})
}

func startHelperConfig(t *testing.T, config Config) *Client {
	t.Helper()
	return startHelperConfigContext(t, context.Background(), config)
}

func startHelperConfigContext(t *testing.T, ctx context.Context, config Config) *Client {
	t.Helper()
	c, err := Start(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestRequestIDsAreUnique(t *testing.T) {
	c := startHelper(t, "echo")
	defer c.Close()
	var ids []string
	for i := 0; i < 20; i++ {
		id, err := c.Abort(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for i := 1; i < len(ids); i++ {
		if ids[i] == ids[i-1] {
			t.Fatalf("duplicate request ID %q", ids[i])
		}
	}
}
