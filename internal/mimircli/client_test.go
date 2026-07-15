package mimircli

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

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
