package mimirapi

import (
	"context"
	"encoding/json"
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

func TestRequestWithHeadersPreservesOwnedHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("authorization"); got != "Bearer test-token" {
			t.Fatalf("authorization %q", got)
		}
		if got := r.Header.Get("content-type"); got != "application/json" {
			t.Fatalf("content-type %q", got)
		}
		if got := r.Header.Values("x-mimir-repo"); len(got) != 2 || got[0] != "one" || got[1] != "two" {
			t.Fatalf("metadata headers %#v", got)
		}
	}))
	defer server.Close()
	headers := http.Header{
		"Authorization":  {"Bearer attacker"},
		"Content-Type":   {"text/plain"},
		"Content-Length": {"1"},
		"X-Mimir-Repo":   {"one", "two"},
	}
	client := Client{HTTPClient: server.Client(), Pointer: Pointer{URL: server.URL, Token: "test-token"}}
	if _, err := client.RequestWithHeaders(context.Background(), http.MethodPost, "/sessions/s/exchanges", map[string]any{"ok": true}, headers); err != nil {
		t.Fatal(err)
	}
	if headers.Get("Authorization") != "Bearer attacker" {
		t.Fatal("caller headers were mutated")
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

func TestWhoAmIAndAssociateMachine(t *testing.T) {
	var associated MachineAssociation
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/whoami":
			_, _ = w.Write([]byte(`{"service":"mimir","api_version":1,"capabilities":["machine_identity_association"]}`))
		case "/machine/associate":
			if r.Method != http.MethodPost {
				t.Fatalf("method %s", r.Method)
			}
			if err := json.NewDecoder(r.Body).Decode(&associated); err != nil {
				t.Fatal(err)
			}
			_, _ = w.Write([]byte(`{"associated":true}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := Client{HTTPClient: server.Client(), Pointer: Pointer{URL: server.URL, Token: "token"}}
	whoami, err := client.WhoAmI(context.Background())
	if err != nil || !whoami.HasCapability("machine_identity_association") || whoami.HasCapability("missing") {
		t.Fatalf("whoami = %#v, %v", whoami, err)
	}
	want := MachineAssociation{Version: 1, InstallationID: strings.Repeat("a", 32), Name: "Machine", Platform: "linux", Arch: "amd64"}
	if err := client.AssociateMachine(context.Background(), want); err != nil {
		t.Fatal(err)
	}
	if associated != want {
		t.Fatalf("association = %#v", associated)
	}
}
