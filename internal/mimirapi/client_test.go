package mimirapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequestAuthenticates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("authorization"); got != "Bearer test-token" {
			t.Fatalf("authorization %q", got)
		}
		if r.Method != http.MethodPost || r.URL.Path != "/search" {
			t.Fatalf("request %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()
	data, err := (Client{HTTPClient: server.Client(), Pointer: Pointer{URL: server.URL, Token: "test-token"}}).Request(context.Background(), http.MethodPost, "/search", map[string]string{"query": "auth"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(data)) != `{"ok":true}` {
		t.Fatalf("response %s", data)
	}
}

func TestValidateDeploymentURL(t *testing.T) {
	for _, valid := range []string{"https://mimir.example.workers.dev", "http://127.0.0.1:8787"} {
		if err := ValidateDeploymentURL(valid); err != nil {
			t.Fatalf("%s: %v", valid, err)
		}
	}
	for _, invalid := range []string{"http://example.com", "https://user:pass@example.com", "not-a-url"} {
		if err := ValidateDeploymentURL(invalid); err == nil {
			t.Fatalf("expected %s to be rejected", invalid)
		}
	}
}

func TestVerifyUsesWhoAmI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/whoami" {
			t.Fatalf("path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"service":"mimir"}`))
	}))
	defer server.Close()
	if err := (Client{HTTPClient: server.Client(), Pointer: Pointer{URL: server.URL, Token: "token"}}).Verify(context.Background()); err != nil {
		t.Fatal(err)
	}
}
