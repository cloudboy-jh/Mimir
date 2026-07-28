package mimircli

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cloudboy-jh/mimir/internal/mimirapi"
)

func TestCmdListPassesFilters(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sessions" {
			t.Fatalf("path %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("repo"); got != "mimir" {
			t.Fatalf("repo %q", got)
		}
		if got := r.URL.Query().Get("outcome"); got != "landed" {
			t.Fatalf("outcome %q", got)
		}
		_, _ = w.Write([]byte(`{"sessions":[{"id":"s1","started_at":"2026-07-12T18:06:00Z","outcome":"landed","model_primary":"m","intent":null,"capture":{"status":"saved","saved_exchanges":3,"failed_exchanges":0,"pending_exchanges":0}}]}`))
	}))
	defer server.Close()
	t.Setenv(envMimirHome, t.TempDir())
	if err := savePointer(mimirapi.Pointer{URL: server.URL, Token: "test-token"}); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if err := cmdList(context.Background(), []string{"--repo", "mimir", "--outcome=landed"}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "[LANDED] s1") || !strings.Contains(out.String(), "m · 3 exchanges saved") {
		t.Fatalf("output %q", out.String())
	}
}

func TestCmdListRejectsBadInput(t *testing.T) {
	var out strings.Builder
	if err := cmdList(context.Background(), []string{"--outcome", "bogus"}, &out); err == nil {
		t.Fatal("expected invalid outcome error")
	}
	if err := cmdList(context.Background(), []string{"--limit", "nope"}, &out); err == nil {
		t.Fatal("expected invalid limit error")
	}
	if err := cmdList(context.Background(), []string{"--bogus"}, &out); err == nil {
		t.Fatal("expected usage error")
	}
}
