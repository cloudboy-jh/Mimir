package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/cloudboy-jh/mimir/internal/sessions"
)

type requesterFunc func(context.Context, string, string, any) ([]byte, error)

func (f requesterFunc) Request(ctx context.Context, method, path string, body any) ([]byte, error) {
	return f(ctx, method, path, body)
}

func TestServeUsesNewlineDelimitedJSON(t *testing.T) {
	server := New("test", requesterFunc(func(context.Context, string, string, any) ([]byte, error) { return nil, nil }), sessions.Service{}, nil)
	input := `{"jsonrpc":"2.0","id":1,"method":"initialize"}` + "\n" + `{"jsonrpc":"2.0","id":2,"method":"ping"}` + "\n"
	var output bytes.Buffer
	if err := server.Serve(context.Background(), strings.NewReader(input), &output); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "Content-Length") || len(strings.Split(strings.TrimSpace(output.String()), "\n")) != 2 {
		t.Fatalf("output %q", output.String())
	}
}

func TestServeMatchesContentLengthFraming(t *testing.T) {
	server := New("test", requesterFunc(func(context.Context, string, string, any) ([]byte, error) { return nil, nil }), sessions.Service{}, nil)
	body := `{"jsonrpc":"2.0","id":1,"method":"ping"}`
	input := fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(body), body)
	var output bytes.Buffer
	if err := server.Serve(context.Background(), strings.NewReader(input), &output); err != nil {
		t.Fatal(err)
	}
	header, framedBody, ok := strings.Cut(output.String(), "\r\n\r\n")
	if !ok || !strings.HasPrefix(header, "Content-Length: ") {
		t.Fatalf("output %q", output.String())
	}
	if header != fmt.Sprintf("Content-Length: %d", len(framedBody)) || !json.Valid([]byte(framedBody)) {
		t.Fatalf("header %q body %q", header, framedBody)
	}
}

func TestServeReturnsParseError(t *testing.T) {
	server := New("test", requesterFunc(func(context.Context, string, string, any) ([]byte, error) { return nil, nil }), sessions.Service{}, nil)
	var output bytes.Buffer
	if err := server.Serve(context.Background(), strings.NewReader("not-json\n"), &output); err != nil {
		t.Fatal(err)
	}
	var response struct {
		ID    any `json:"id"`
		Error struct {
			Code int `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(output.Bytes()), &response); err != nil {
		t.Fatal(err)
	}
	if response.ID != nil || response.Error.Code != -32700 {
		t.Fatalf("response = %#v", response)
	}
}

func TestServeRejectsInvalidRequestsAndContinues(t *testing.T) {
	server := New("test", requesterFunc(func(context.Context, string, string, any) ([]byte, error) { return nil, nil }), sessions.Service{}, nil)
	invalid := []string{
		`{"jsonrpc":"1.0","id":1,"method":"ping"}`,
		`{"jsonrpc":"2.0","id":{},"method":"ping"}`,
		`{"jsonrpc":"2.0","id":2}`,
		`{"jsonrpc":"2.0","id":3,"method":"ping","params":true}`,
		`[]`,
	}
	input := strings.Join(append(invalid, `{"jsonrpc":"2.0","id":4,"method":"ping"}`), "\n") + "\n"
	var output bytes.Buffer
	if err := server.Serve(context.Background(), strings.NewReader(input), &output); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != len(invalid)+1 {
		t.Fatalf("responses=%d output=%q", len(lines), output.String())
	}
	for i, line := range lines[:len(invalid)] {
		var response struct {
			Error struct {
				Code int `json:"code"`
			} `json:"error"`
		}
		if err := json.Unmarshal([]byte(line), &response); err != nil || response.Error.Code != -32600 {
			t.Fatalf("invalid request %d response=%q error=%v", i, line, err)
		}
	}
	var final map[string]any
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &final); err != nil || final["result"] == nil {
		t.Fatalf("valid request after invalid input was not handled: %q, %v", lines[len(lines)-1], err)
	}
}

func TestServerReturnsToolFailuresAsCallToolErrors(t *testing.T) {
	wantErr := errors.New("worker unavailable")
	server := Server{
		Tools: func() []map[string]any { return nil },
		Call: func(context.Context, string, map[string]any) (ToolCallOutput, error) {
			return ToolCallOutput{}, wantErr
		},
	}
	params, _ := json.Marshal(map[string]any{"name": "whoami", "arguments": map[string]any{}})
	response := server.Handle(context.Background(), Request{ID: json.RawMessage("1"), Method: "tools/call", Params: params})
	if _, exists := response["error"]; exists {
		t.Fatalf("tool failure was returned as a JSON-RPC error: %#v", response)
	}
	result := response["result"].(map[string]any)
	if result["isError"] != true || result["content"].([]map[string]string)[0]["text"] != wantErr.Error() {
		t.Fatalf("result = %#v", result)
	}
}

func TestServerReturnsInvalidToolParamsAsJSONRPCError(t *testing.T) {
	server := Server{Tools: func() []map[string]any { return nil }}
	response := server.Handle(context.Background(), Request{ID: json.RawMessage("1"), Method: "tools/call", Params: json.RawMessage(`{`)})
	errorObject := response["error"].(map[string]any)
	if errorObject["code"] != -32602 {
		t.Fatalf("response = %#v", response)
	}
}

func TestServerRejectsMissingNameAndNonObjectArgumentsAsInvalidParams(t *testing.T) {
	server := Server{Tools: func() []map[string]any { return nil }}
	for _, params := range []string{`{}`, `null`, `{"name":"whoami","arguments":[]}`} {
		response := server.Handle(context.Background(), Request{ID: json.RawMessage("1"), Method: "tools/call", Params: json.RawMessage(params)})
		errorObject := response["error"].(map[string]any)
		if errorObject["code"] != -32602 {
			t.Fatalf("params %s response = %#v", params, response)
		}
	}
}

func TestToolsPublishSessionLifecycle(t *testing.T) {
	data, err := json.Marshal(Tools())
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"name":"session_status"`, `"name":"session_end"`, `"name":"session_set_outcome"`} {
		if !strings.Contains(string(data), want) {
			t.Fatalf("tools missing %s: %s", want, data)
		}
	}
}

func TestDomainDispatchesOutcome(t *testing.T) {
	api := requesterFunc(func(_ context.Context, method, path string, body any) ([]byte, error) {
		if method != "POST" || path != "/sessions/session-1/outcome" || body.(map[string]any)["outcome"] != "landed" {
			t.Fatalf("request %s %s %#v", method, path, body)
		}
		return []byte(`{"ok":true}`), nil
	})
	domain := Domain{API: api, Sessions: sessions.New(api)}
	if _, err := domain.CallTool(context.Background(), "session_set_outcome", map[string]any{"id": "session-1", "outcome": "landed"}); err != nil {
		t.Fatal(err)
	}
}

func TestDomainReturnsCompactSessionStatusAndEndReceipts(t *testing.T) {
	api := requesterFunc(func(_ context.Context, method, path string, body any) ([]byte, error) {
		switch path {
		case "/sessions/session-1/end":
			if method != "POST" || body.(map[string]any)["outcome"] != "landed" {
				t.Fatalf("end request %s %s %#v", method, path, body)
			}
			return []byte(`{"session":{"id":"session-1","state":"inactive"}}`), nil
		case "/sessions/session-1/status":
			return []byte(`{"session_id":"session-1","capture":{"status":"partial","saved_exchanges":2,"failed_exchanges":1,"pending_exchanges":0},"receipt":{"label":"Partially saved","detail":"2 of 3 exchanges","action_label":"View details"},"dashboard_url":"https://mimir.example/dashboard/sessions/session-1","outcome":"landed"}`), nil
		default:
			t.Fatalf("unexpected request %s %s", method, path)
			return nil, nil
		}
	})
	domain := Domain{API: api, Sessions: serviceWithoutPolling(api)}
	status, err := domain.CallTool(context.Background(), "session_status", map[string]any{"id": "session-1"})
	if err != nil {
		t.Fatal(err)
	}
	wantStatus := "Partially saved · 2 of 3 exchanges · [View details](https://mimir.example/dashboard/sessions/session-1)"
	if status.Text != wantStatus {
		t.Fatalf("status receipt = %q", status.Text)
	}
	ended, err := domain.CallTool(context.Background(), "session_end", map[string]any{"id": "session-1", "outcome": "landed"})
	if err != nil {
		t.Fatal(err)
	}
	if ended.Text != "Session ended · "+wantStatus {
		t.Fatalf("end receipt = %q", ended.Text)
	}
}

func serviceWithoutPolling(api Requester) sessions.Service {
	service := sessions.New(api)
	service.PollSchedule = nil
	return service
}

func TestDomainLegacyMarkKeepsEndpointAndOutcome(t *testing.T) {
	api := requesterFunc(func(_ context.Context, method, path string, body any) ([]byte, error) {
		if method != "POST" || path != "/sessions/session-1/mark" || body.(map[string]any)["outcome"] != "unknown" {
			t.Fatalf("request %s %s %#v", method, path, body)
		}
		return []byte(`{"outcome":"unknown"}`), nil
	})
	domain := Domain{API: api}
	if _, err := domain.CallTool(context.Background(), "mark", map[string]any{"id": "session-1", "outcome": "unknown"}); err != nil {
		t.Fatal(err)
	}
}

func TestDomainPropagatesToolAndAPIErrors(t *testing.T) {
	wantErr := errors.New("remote failed")
	api := requesterFunc(func(context.Context, string, string, any) ([]byte, error) { return nil, wantErr })
	domain := Domain{API: api, Sessions: serviceWithoutPolling(api)}
	for _, test := range []struct {
		name string
		args map[string]any
	}{
		{name: "whoami", args: map[string]any{}},
		{name: "session_status", args: map[string]any{"id": "session-1"}},
	} {
		if _, err := domain.CallTool(context.Background(), test.name, test.args); !errors.Is(err, wantErr) {
			t.Errorf("%s error = %v", test.name, err)
		}
	}
	if _, err := domain.CallTool(context.Background(), "session_status", map[string]any{}); err == nil || err.Error() != "id is required" {
		t.Fatalf("validation error = %v", err)
	}
	if _, err := domain.CallTool(context.Background(), "unknown", map[string]any{}); err == nil || err.Error() != "unknown tool: unknown" {
		t.Fatalf("unknown tool error = %v", err)
	}
}
