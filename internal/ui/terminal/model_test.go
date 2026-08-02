package terminalui

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/cloudboy-jh/mimir/internal/ui/bentotui"
	sessionui "github.com/cloudboy-jh/mimir/internal/ui/sessions"
)

func testModel() *Model {
	m := New(Options{Context: context.Background(), Out: io.Discard, Load: func(context.Context) ([]sessionui.BrowserSession, error) {
		return []sessionui.BrowserSession{{ID: "ses-1", Title: "dashboard rebuild", Outcome: "landed", Harness: "opencode", Tokens: 481000, Capture: "2 saved"}, {ID: "ses-2", Title: "buzz eval", Outcome: "unresolved"}}, nil
	}})
	// Make constructor loading deterministic without sleeping.
	m.mu.Lock()
	m.items = []sessionui.BrowserSession{{ID: "ses-1", Title: "dashboard rebuild", Outcome: "landed", Harness: "opencode", Tokens: 481000, Capture: "2 saved"}, {ID: "ses-2", Title: "buzz eval", Outcome: "unresolved"}}
	m.loading = false
	m.applyFilterLocked()
	m.mu.Unlock()
	return m
}

func TestViewSplitAndFullscreen(t *testing.T) {
	m := testModel()
	view := m.View(bentotui.Screen{Width: 80, Height: 20})
	if !strings.Contains(view, "Sessions (2)") || !strings.Contains(view, "◆ Agent") || len(strings.Split(view, "\n")) != 20 {
		t.Fatalf("unexpected split view:\n%s", view)
	}
	m.Handle(context.Background(), bentotui.Key{Kind: bentotui.KeyRune, Rune: 'z'})
	view = m.View(bentotui.Screen{Width: 80, Height: 20})
	if strings.Contains(view, "Sessions (2)") || !strings.Contains(view, "◆ Agent") {
		t.Fatalf("unexpected fullscreen view:\n%s", view)
	}
}

func TestFilterAndAgentContext(t *testing.T) {
	m := testModel()
	m.Handle(context.Background(), bentotui.Key{Kind: bentotui.KeyRune, Rune: '/'})
	for _, r := range "buzz" {
		m.Handle(context.Background(), bentotui.Key{Kind: bentotui.KeyRune, Rune: r})
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if len(m.visible) != 1 || m.currentIDLocked() != "ses-2" {
		t.Fatalf("filter did not update cursor: %#v", m.visible)
	}
}

func TestMinimumScreenIsBounded(t *testing.T) {
	view := testModel().View(bentotui.Screen{Width: 48, Height: 12})
	lines := strings.Split(view, "\n")
	if len(lines) != 12 {
		t.Fatalf("got %d lines", len(lines))
	}
	for _, line := range lines {
		if bentotui.VisibleWidth(line) != 48 {
			t.Fatalf("line width %d: %q", bentotui.VisibleWidth(line), line)
		}
	}
}

func TestCleanTextStripsTerminalControls(t *testing.T) {
	got := cleanText("safe\x1b]52;c;ZXZpbA==\x07\x1b[2J text")
	if got != "safe text" {
		t.Fatalf("got %q", got)
	}
}

func TestVisibleTailKeepsNewestInput(t *testing.T) {
	if got := visibleTail("abcdefgh", 4); got != "efgh" {
		t.Fatalf("got %q", got)
	}
}
