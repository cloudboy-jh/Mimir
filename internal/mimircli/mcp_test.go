package mimircli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestServeMCPUsesNewlineDelimitedJSON(t *testing.T) {
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"ping"}`,
	}, "\n") + "\n"
	var output bytes.Buffer
	if err := serveMCP(context.Background(), mcpOptions{In: strings.NewReader(input), Out: &output}); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("got %d responses: %q", len(lines), output.String())
	}
	for _, line := range lines {
		var response map[string]any
		if err := json.Unmarshal([]byte(line), &response); err != nil {
			t.Fatalf("response is not newline-delimited JSON: %q: %v", line, err)
		}
	}
	if strings.Contains(output.String(), "Content-Length") {
		t.Fatalf("response used header framing: %q", output.String())
	}
}

func TestServeMCPReturnsParseError(t *testing.T) {
	var output bytes.Buffer
	if err := serveMCP(context.Background(), mcpOptions{In: strings.NewReader("not-json\n"), Out: &output}); err != nil {
		t.Fatal(err)
	}
	var response struct {
		Error struct {
			Code int `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &response); err != nil {
		t.Fatal(err)
	}
	if response.Error.Code != -32700 {
		t.Fatalf("error code %d", response.Error.Code)
	}
}

func TestMCPListsSessionStatusAndOutcomeTools(t *testing.T) {
	data, err := json.Marshal(tools())
	if err != nil {
		t.Fatal(err)
	}
	listed := string(data)
	for _, want := range []string{`"name":"session_status"`, `"name":"session_set_outcome"`, `"enum":["landed","discarded","abandoned","unresolved"]`, `"name":"mark"`, `"promoted"`, `"unknown"`} {
		if !strings.Contains(listed, want) {
			t.Fatalf("tools/list missing %s: %s", want, listed)
		}
	}
}

func TestMCPCallsSessionStatusAndOutcomeTools(t *testing.T) {
	oldSchedule := sessionStatusPollSchedule
	sessionStatusPollSchedule = []time.Duration{0}
	t.Cleanup(func() { sessionStatusPollSchedule = oldSchedule })
	requests := make(chan struct {
		method string
		path   string
		body   map[string]any
	}, 1)
	statusCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/sessions/session-1/status" {
			statusCalls++
			_, _ = w.Write([]byte(`{"session_id":"session-1","capture":{"status":"saved","saved_exchanges":2,"failed_exchanges":0,"pending_exchanges":0,"last_saved_at":"2026-07-16T00:00:00Z"},"receipt":{"label":"Saved to Mimir","detail":"2 exchanges in this session","action_label":"View session"},"dashboard_url":"https://mimir.example/dashboard/sessions/session-1","outcome":"unresolved"}`))
			return
		}
		request := struct {
			method string
			path   string
			body   map[string]any
		}{method: r.Method, path: r.URL.Path}
		if r.Body != nil && r.ContentLength != 0 {
			if err := json.NewDecoder(r.Body).Decode(&request.body); err != nil {
				t.Fatal(err)
			}
		}
		requests <- request
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	t.Setenv(envMimirHome, t.TempDir())
	if err := savePointer(Pointer{URL: server.URL, Token: "test-token"}); err != nil {
		t.Fatal(err)
	}

	statusOutput, err := callTool(context.Background(), "session_status", map[string]any{"id": "session-1"})
	if err != nil {
		t.Fatal(err)
	}
	if statusCalls != 2 || !strings.Contains(statusOutput.Text, "Saved to Mimir · 2 exchanges in this session · [View session]") {
		t.Fatalf("calls=%d output=%#v", statusCalls, statusOutput)
	}

	if _, err := callTool(context.Background(), "session_set_outcome", map[string]any{"id": "session-1", "outcome": "landed", "reason": "merged", "evidence": "commit abc123"}); err != nil {
		t.Fatal(err)
	}
	outcomeRequest := <-requests
	if outcomeRequest.method != http.MethodPost || outcomeRequest.path != "/sessions/session-1/outcome" {
		t.Fatalf("outcome request %s %s", outcomeRequest.method, outcomeRequest.path)
	}
	want := map[string]any{"outcome": "landed", "source": "agent", "reason": "merged", "evidence": "commit abc123"}
	if got := mustJSON(t, outcomeRequest.body); got != mustJSON(t, want) {
		t.Fatalf("outcome body %s, want %s", got, mustJSON(t, want))
	}
}

func TestMCPStatusResultIsCompact(t *testing.T) {
	oldSchedule := sessionStatusPollSchedule
	sessionStatusPollSchedule = []time.Duration{0}
	t.Cleanup(func() { sessionStatusPollSchedule = oldSchedule })
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"session_id":"session-1","capture":{"status":"partial","saved_exchanges":2,"failed_exchanges":1,"pending_exchanges":0,"last_saved_at":"2026-07-16T00:00:00Z"},"receipt":{"label":"Partially saved","detail":"2 of 3 exchanges","action_label":"View details"},"dashboard_url":"https://mimir.example/dashboard/sessions/session-1","outcome":"unresolved"}`))
	}))
	defer server.Close()
	t.Setenv(envMimirHome, t.TempDir())
	if err := savePointer(Pointer{URL: server.URL, Token: "test-token"}); err != nil {
		t.Fatal(err)
	}
	params, _ := json.Marshal(map[string]any{"name": "session_status", "arguments": map[string]any{"id": "session-1"}})
	response := handle(context.Background(), request{JSONRPC: "2.0", ID: json.RawMessage("1"), Method: "tools/call", Params: params})
	result := response["result"].(map[string]any)
	content := result["content"].([]map[string]string)
	if got := content[0]["text"]; got != "Partially saved · 2 of 3 exchanges · [View details](https://mimir.example/dashboard/sessions/session-1)" {
		t.Fatalf("text=%q", got)
	}
	if _, exists := result["structuredContent"]; exists || result["isError"] != false {
		t.Fatalf("result=%#v", result)
	}
}

func TestMCPMarkKeepsLegacyEndpointAndOutcome(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/sessions/session-1/mark" {
			t.Fatalf("request %s %s", r.Method, r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["outcome"] != "unknown" {
			t.Fatalf("outcome %#v", body["outcome"])
		}
		_, _ = w.Write([]byte(`{"outcome":"unknown"}`))
	}))
	defer server.Close()
	t.Setenv(envMimirHome, t.TempDir())
	if err := savePointer(Pointer{URL: server.URL, Token: "test-token"}); err != nil {
		t.Fatal(err)
	}
	if _, err := callTool(context.Background(), "mark", map[string]any{"id": "session-1", "outcome": "unknown"}); err != nil {
		t.Fatal(err)
	}
}
