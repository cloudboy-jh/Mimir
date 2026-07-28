package ui

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cloudboy-jh/mimir/internal/ui/bentotui"
)

func TestSessionBrowserListFilterAndDetail(t *testing.T) {
	var out strings.Builder
	browser := NewSessionBrowser(SessionBrowserOptions{Out: &out, Items: []BrowserSession{
		{Title: "Fix capture", Outcome: "landed", Capture: "3 exchanges saved", Started: "2026-07-28 14:00", Repo: "mimir", Model: "gpt", ID: "session-1", DashboardURL: "https://mimir.example/dashboard/sessions/session-1"},
		{Title: "Index docs", Outcome: "unresolved", Capture: "saving", Started: "2026-07-28 13:00", Repo: "docs", Model: "sonnet", ID: "session-2"},
	}})
	screen := bentotui.Screen{Width: 80, Height: 24}

	view := browser.View(screen)
	for _, expected := range []string{"Sessions  2 results", "[LANDED] Fix capture", "session-2", "enter open"} {
		if !strings.Contains(view, expected) {
			t.Fatalf("list missing %q:\n%s", expected, view)
		}
	}

	browser.Handle(context.Background(), bentotui.Key{Kind: bentotui.KeyRune, Rune: '/'})
	for _, value := range "docs" {
		browser.Handle(context.Background(), bentotui.Key{Kind: bentotui.KeyRune, Rune: value})
	}
	view = browser.View(screen)
	if !strings.Contains(view, "Sessions  1 results") || strings.Contains(view, "Fix capture") {
		t.Fatalf("filter not applied:\n%s", view)
	}
	browser.Handle(context.Background(), bentotui.Key{Kind: bentotui.KeyEnter})
	browser.Handle(context.Background(), bentotui.Key{Kind: bentotui.KeyEnter})
	view = browser.View(screen)
	if !strings.Contains(view, "Session detail") || !strings.Contains(view, "Index docs") || !strings.Contains(view, "session-2") {
		t.Fatalf("detail missing fields:\n%s", view)
	}
	browser.Handle(context.Background(), bentotui.Key{Kind: bentotui.KeyEscape})
	if !strings.Contains(browser.View(screen), "Sessions  1 results") {
		t.Fatal("escape did not preserve the filtered list")
	}
}

func TestSessionBrowserActionsAndRefreshFailure(t *testing.T) {
	var copied, opened string
	browser := NewSessionBrowser(SessionBrowserOptions{
		Items:   []BrowserSession{{Title: "One", ID: "s1", DashboardURL: "https://example.com/s1"}},
		Copy:    func(value string) error { copied = value; return nil },
		Open:    func(_ context.Context, value string) error { opened = value; return nil },
		Refresh: func(context.Context) ([]BrowserSession, error) { return nil, errors.New("offline") },
	})
	browser.Handle(context.Background(), bentotui.Key{Kind: bentotui.KeyRune, Rune: 'y'})
	browser.Handle(context.Background(), bentotui.Key{Kind: bentotui.KeyRune, Rune: 'o'})
	if copied != "s1" || opened != "https://example.com/s1" {
		t.Fatalf("copied %q opened %q", copied, opened)
	}
	browser.Handle(context.Background(), bentotui.Key{Kind: bentotui.KeyRune, Rune: 'r'})
	if !strings.Contains(browser.View(bentotui.Screen{Width: 80, Height: 24}), "Refresh failed: offline") {
		t.Fatal("refresh failure was not surfaced")
	}
	if !browser.Handle(context.Background(), bentotui.Key{Kind: bentotui.KeyInterrupt}) {
		t.Fatal("interrupt should quit")
	}
}

func TestSessionBrowserStaysWithinNarrowWidth(t *testing.T) {
	browser := NewSessionBrowser(SessionBrowserOptions{Items: []BrowserSession{{
		Title: strings.Repeat("wide title ", 20), Outcome: "abandoned", Capture: "10 saved · 2 failed",
		Started: "2026-07-28 14:00", Repo: strings.Repeat("repository", 8), Model: "model", ID: strings.Repeat("id", 40),
	}}})
	view := browser.View(bentotui.Screen{Width: 40, Height: 16})
	for _, line := range strings.Split(view, "\n") {
		if bentotui.VisibleWidth(line) > 40 {
			t.Fatalf("line width %d: %q", bentotui.VisibleWidth(line), line)
		}
	}
}
