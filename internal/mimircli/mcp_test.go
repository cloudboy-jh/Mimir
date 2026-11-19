package mimircli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMCPBridgeFraming(t *testing.T) {
	var received []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/mcp" || r.Header.Get("authorization") != "Bearer token" || r.Header.Get("content-type") != "application/json" {
			t.Errorf("request %s %s %v", r.Method, r.URL.Path, r.Header)
		}
		body, _ := io.ReadAll(r.Body)
		received = append(received, string(body))
		var message struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		_ = json.Unmarshal(body, &message)
		switch {
		case strings.HasPrefix(message.Method, "notifications/"):
			w.WriteHeader(http.StatusAccepted)
		case message.Method == "boom":
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":"worker failed"}`))
		default:
			_, _ = w.Write([]byte("{\n  \"jsonrpc\": \"2.0\",\n  \"id\": " + string(message.ID) + ",\n  \"result\": {}\n}"))
		}
	}))
	defer server.Close()

	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		``,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{not json`,
		`{"jsonrpc":"2.0","id":"x","method":"boom"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
	}, "\n")
	var out, log bytes.Buffer
	bridge := mcpBridge{client: server.Client(), endpoint: server.URL + "/mcp", token: "token", out: &out, log: &log}
	if err := bridge.run(context.Background(), strings.NewReader(input)); err != nil {
		t.Fatal(err)
	}
	if len(received) != 4 {
		t.Fatalf("forwarded %d messages: %v", len(received), received)
	}
	lines := strings.Split(strings.TrimSuffix(out.String(), "\n"), "\n")
	want := []string{
		`{"jsonrpc":"2.0","id":1,"result":{}}`,
		`{"jsonrpc":"2.0","id":null,"error":{"code":-32700,"message":"Parse error"}}`,
		`{"error":{"code":-32603,"message":"Mimir MCP 500 Internal Server Error: worker failed"},"id":"x","jsonrpc":"2.0"}`,
		`{"jsonrpc":"2.0","id":2,"result":{}}`,
	}
	if len(lines) != len(want) {
		t.Fatalf("output lines %q", lines)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Fatalf("line %d = %s, want %s", i, lines[i], want[i])
		}
	}
	if !strings.Contains(log.String(), "worker failed") {
		t.Fatalf("log %q", log.String())
	}
}

func TestMCPBridgeAnswersRequestsWhenNotConnected(t *testing.T) {
	var out bytes.Buffer
	bridge := mcpBridge{unavailable: "Mimir is not connected; run mimir setup", out: &out}
	input := `[{"jsonrpc":"2.0","id":7,"method":"tools/list"},{"jsonrpc":"2.0","method":"notifications/initialized"}]` + "\n"
	if err := bridge.run(context.Background(), strings.NewReader(input)); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != `{"error":{"code":-32603,"message":"Mimir is not connected; run mimir setup"},"id":7,"jsonrpc":"2.0"}`+"\n" {
		t.Fatalf("output %q", got)
	}
}
