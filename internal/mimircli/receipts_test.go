package mimircli

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func strptr(value string) *string { return &value }

func TestFormatSessionReceipts(t *testing.T) {
	started := "2026-07-12T18:06:00Z"
	local, err := time.Parse(time.RFC3339, started)
	if err != nil {
		t.Fatal(err)
	}
	stamp := local.Local().Format("2006-01-02 15:04")
	sessions := []sessionReceipt{
		{ID: "01JZ3A2KPM", StartedAt: started, Outcome: "landed", Model: strptr("claude-sonnet-4"), Intent: strptr("Fix the login redirect loop"), Capture: struct {
			Status           string `json:"status"`
			SavedExchanges   int    `json:"saved_exchanges"`
			FailedExchanges  int    `json:"failed_exchanges"`
			PendingExchanges int    `json:"pending_exchanges"`
		}{Status: "saved", SavedExchanges: 12}},
		{ID: "01JZ3B9XYZ", StartedAt: started, State: "active", Outcome: "", Capture: struct {
			Status           string `json:"status"`
			SavedExchanges   int    `json:"saved_exchanges"`
			FailedExchanges  int    `json:"failed_exchanges"`
			PendingExchanges int    `json:"pending_exchanges"`
		}{Status: "pending", PendingExchanges: 1}},
	}
	text := formatSessionReceipts(sessions, 20)
	lines := strings.Split(text, "\n")
	if len(lines) != 3 {
		t.Fatalf("lines=%d: %q", len(lines), text)
	}
	wantFirst := stamp + " · 01JZ3A2KPM · landed · 12 exchanges saved · claude-sonnet-4"
	if lines[0] != wantFirst {
		t.Fatalf("first line %q, want %q", lines[0], wantFirst)
	}
	if lines[1] != "  Fix the login redirect loop" {
		t.Fatalf("intent line %q", lines[1])
	}
	if !strings.Contains(lines[2], "01JZ3B9XYZ · unresolved · saving… · unknown model · active") {
		t.Fatalf("second session line %q", lines[2])
	}
}

func TestFormatSessionReceiptsHonorsLimit(t *testing.T) {
	sessions := []sessionReceipt{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	text := formatSessionReceipts(sessions, 2)
	if strings.Contains(text, " · c · ") || !strings.Contains(text, " · b · ") {
		t.Fatalf("limit not applied: %q", text)
	}
	if got := formatSessionReceipts(nil, 20); got != "No sessions found." {
		t.Fatalf("empty %q", got)
	}
}

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
	if err := savePointer(Pointer{URL: server.URL, Token: "test-token"}); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	if err := cmdList(context.Background(), []string{"--repo", "mimir", "--outcome=landed"}, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "s1 · landed · 3 exchanges saved · m") {
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

func TestMCPSessionsListReturnsReceipts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sessions" {
			t.Fatalf("path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"sessions":[{"id":"s1","started_at":"2026-07-12T18:06:00Z","outcome":"discarded","model_primary":"openai/test","intent":"Explore retry backoff","capture":{"status":"saved","saved_exchanges":2,"failed_exchanges":0,"pending_exchanges":0}}]}`))
	}))
	defer server.Close()
	t.Setenv(envMimirHome, t.TempDir())
	if err := savePointer(Pointer{URL: server.URL, Token: "test-token"}); err != nil {
		t.Fatal(err)
	}
	output, err := callTool(context.Background(), "sessions_list", map[string]any{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.Text, "s1 · discarded · 2 exchanges saved · openai/test") || !strings.Contains(output.Text, "  Explore retry backoff") {
		t.Fatalf("receipts %q", output.Text)
	}
}
