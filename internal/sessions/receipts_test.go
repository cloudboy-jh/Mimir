package sessions

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

type receiptRequester struct {
	data []byte
	path string
}

func strptr(value string) *string { return &value }

func TestFormatReceipts(t *testing.T) {
	started := "2026-07-12T18:06:00Z"
	local, err := time.Parse(time.RFC3339, started)
	if err != nil {
		t.Fatal(err)
	}
	stamp := local.Local().Format("2006-01-02 15:04")
	receipts := []Receipt{
		{ID: "01JZ3A2KPM", StartedAt: started, Outcome: "landed", Model: strptr("claude-sonnet-4"), DisplayTitle: strptr("Login redirect repair"), Title: strptr("Login redirect repair"), Intent: strptr("Fix the login redirect loop")},
		{ID: "01JZ3B9XYZ", StartedAt: started, State: "active"},
	}
	receipts[0].Capture.Status, receipts[0].Capture.SavedExchanges = "saved", 12
	receipts[1].Capture.Status, receipts[1].Capture.PendingExchanges = "pending", 1
	text := FormatReceipts(receipts, 20)
	lines := strings.Split(text, "\n")
	if len(lines) != 4 {
		t.Fatalf("lines=%d: %q", len(lines), text)
	}
	wantFirst := stamp + " · 01JZ3A2KPM · landed · 12 exchanges saved · claude-sonnet-4"
	if lines[0] != wantFirst || lines[1] != "  Login redirect repair" {
		t.Fatalf("lines = %#v", lines)
	}
	if !strings.Contains(lines[2], "01JZ3B9XYZ · unresolved · saving… · unknown model · active") || lines[3] != "  Untitled session" {
		t.Fatalf("second session lines %q", lines[2:])
	}
}

func TestDisplayTitlePrecedence(t *testing.T) {
	for _, test := range []struct {
		name                   string
		display, title, intent *string
		want                   string
	}{
		{name: "display title", display: strptr(" Display "), title: strptr("Title"), intent: strptr("Intent"), want: "Display"},
		{name: "title", title: strptr(" Title "), intent: strptr("Intent"), want: "Title"},
		{name: "intent", intent: strptr(" Intent "), want: "Intent"},
		{name: "fallback", display: strptr(" "), want: "Untitled session"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := DisplayTitle(test.display, test.title, test.intent); got != test.want {
				t.Fatalf("DisplayTitle() = %q, want %q", got, test.want)
			}
		})
	}
}

func TestFormatReceiptsHonorsLimit(t *testing.T) {
	receipts := []Receipt{{ID: "a"}, {ID: "b"}, {ID: "c"}}
	text := FormatReceipts(receipts, 2)
	if strings.Contains(text, " · c · ") || !strings.Contains(text, " · b · ") {
		t.Fatalf("limit not applied: %q", text)
	}
	if got := FormatReceipts(nil, 20); got != "No sessions found." {
		t.Fatalf("empty %q", got)
	}
}

func (r *receiptRequester) Request(_ context.Context, _, path string, _ any) ([]byte, error) {
	r.path = path
	return r.data, nil
}

func TestFetchReceiptsJSONPreservesCanonicalFieldsAndAppliesLimit(t *testing.T) {
	requester := &receiptRequester{data: []byte(`{"sessions":[{"id":"one","future":"kept"},{"id":"two"}],"cursor":"next"}`)}
	data, err := New(requester).FetchReceiptsJSON(context.Background(), "owner/repo", "landed", 1)
	if err != nil {
		t.Fatal(err)
	}
	if requester.path != "/sessions?outcome=landed&repo=owner%2Frepo" {
		t.Fatalf("path = %q", requester.path)
	}
	var response struct {
		Sessions []map[string]any `json:"sessions"`
		Cursor   string           `json:"cursor"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		t.Fatal(err)
	}
	if len(response.Sessions) != 1 || response.Sessions[0]["future"] != "kept" || response.Cursor != "next" {
		t.Fatalf("response = %#v", response)
	}
}
