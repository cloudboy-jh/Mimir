package mimircli

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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

func TestRemoteRequestAuthenticates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("authorization"); got != "Bearer test-token" {
			t.Fatalf("authorization %q", got)
		}
		if r.Method != http.MethodPost || r.URL.Path != "/search" {
			t.Fatalf("request %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("content-type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	t.Setenv(envMimirHome, t.TempDir())
	if err := savePointer(Pointer{URL: server.URL, Token: "test-token"}); err != nil {
		t.Fatal(err)
	}
	data, err := remoteRequest(context.Background(), http.MethodPost, "/search", map[string]string{"query": "auth"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(data)) != `{"ok":true}` {
		t.Fatalf("response %s", data)
	}
}

func TestValidateDeploymentURL(t *testing.T) {
	for _, valid := range []string{"https://mimir.example.workers.dev", "http://127.0.0.1:8787"} {
		if err := validateDeploymentURL(valid); err != nil {
			t.Fatalf("%s: %v", valid, err)
		}
	}
	for _, invalid := range []string{"http://example.com", "https://user:pass@example.com", "not-a-url"} {
		if err := validateDeploymentURL(invalid); err == nil {
			t.Fatalf("expected %s to be rejected", invalid)
		}
	}
}

func TestOverlapsSessionFiles(t *testing.T) {
	if !overlaps([]string{"src/auth/login.go"}, []string{"src/auth/login.go"}) {
		t.Fatal("exact file did not overlap")
	}
	if overlaps([]string{"src/auth/login.go"}, []string{"src/store.go"}) {
		t.Fatal("unrelated files overlapped")
	}
}

func TestDurableBranch(t *testing.T) {
	if !durableBranch([]string{"origin/main"}) {
		t.Fatal("main should be durable")
	}
	if durableBranch([]string{"origin/feature"}) {
		t.Fatal("feature should not be durable")
	}
}
