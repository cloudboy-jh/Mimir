package sessionui

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

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
	for _, expected := range []string{"Mimir · Sessions", "2 results", "[LANDED] Fix capture", "session-2", "↵ Open"} {
		if !strings.Contains(view, expected) {
			t.Fatalf("list missing %q:\n%s", expected, view)
		}
	}

	browser.Handle(context.Background(), bentotui.Key{Kind: bentotui.KeyRune, Rune: '/'})
	for _, value := range "docs" {
		browser.Handle(context.Background(), bentotui.Key{Kind: bentotui.KeyRune, Rune: value})
	}
	view = browser.View(screen)
	if !strings.Contains(view, "1 results") || strings.Contains(view, "Fix capture") {
		t.Fatalf("filter not applied:\n%s", view)
	}
	browser.Handle(context.Background(), bentotui.Key{Kind: bentotui.KeyEnter})
	browser.Handle(context.Background(), bentotui.Key{Kind: bentotui.KeyEnter})
	view = browser.View(screen)
	if !strings.Contains(view, "Mimir · Session") || !strings.Contains(view, "Index docs") || !strings.Contains(view, "session-2") {
		t.Fatalf("detail missing fields:\n%s", view)
	}
	browser.Handle(context.Background(), bentotui.Key{Kind: bentotui.KeyEscape})
	if !strings.Contains(browser.View(screen), "1 results") {
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
	select {
	case <-browser.Updates():
	case <-time.After(time.Second):
		t.Fatal("refresh did not complete")
	}
	if !strings.Contains(browser.View(bentotui.Screen{Width: 80, Height: 24}), "Refresh failed: offline") {
		t.Fatal("refresh failure was not surfaced")
	}
	if !browser.Handle(context.Background(), bentotui.Key{Kind: bentotui.KeyInterrupt}) {
		t.Fatal("interrupt should quit")
	}
}

func TestSessionBrowserLoadsAsynchronouslyAndCoalescesRefresh(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	browser := NewSessionBrowser(SessionBrowserOptions{
		Load: true,
		Refresh: func(context.Context) ([]BrowserSession, error) {
			calls.Add(1)
			close(started)
			<-release
			return []BrowserSession{{Title: "Loaded", ID: "s1"}}, nil
		},
	})
	screen := bentotui.Screen{Width: 80, Height: 24}
	if view := browser.View(screen); !strings.Contains(view, "Loading sessions") {
		t.Fatalf("initial loading state missing:\n%s", view)
	}
	<-started
	browser.Handle(context.Background(), bentotui.Key{Kind: bentotui.KeyRune, Rune: 'r'})
	browser.Handle(context.Background(), bentotui.Key{Kind: bentotui.KeyRune, Rune: 'r'})
	if got := calls.Load(); got != 1 {
		t.Fatalf("refresh calls = %d, want 1", got)
	}
	close(release)
	select {
	case <-browser.Updates():
	case <-time.After(time.Second):
		t.Fatal("load did not complete")
	}
	if view := browser.View(screen); !strings.Contains(view, "Loaded") {
		t.Fatalf("loaded item missing:\n%s", view)
	}
}

func TestSessionBrowserPreservesItemsDuringRefresh(t *testing.T) {
	release := make(chan struct{})
	browser := NewSessionBrowser(SessionBrowserOptions{
		Items: []BrowserSession{{Title: "Existing", ID: "s1"}},
		Refresh: func(context.Context) ([]BrowserSession, error) {
			<-release
			return []BrowserSession{{Title: "Updated", ID: "s2"}}, nil
		},
	})
	browser.Handle(context.Background(), bentotui.Key{Kind: bentotui.KeyRune, Rune: 'r'})
	if view := browser.View(bentotui.Screen{Width: 80, Height: 24}); !strings.Contains(view, "Existing") || !strings.Contains(view, "loading") {
		t.Fatalf("existing items not retained during refresh:\n%s", view)
	}
	close(release)
	<-browser.Updates()
}

func TestSessionBrowserRefreshUsesCancelableContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	browser := NewSessionBrowser(SessionBrowserOptions{
		Context: ctx,
		Load:    true,
		Refresh: func(ctx context.Context) ([]BrowserSession, error) {
			defer close(done)
			<-ctx.Done()
			return nil, ctx.Err()
		},
	})
	_ = browser.View(bentotui.Screen{Width: 80, Height: 24})
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("fetch context was not canceled")
	}
}

func TestSessionDetailSupportsViewportBounds(t *testing.T) {
	browser := NewSessionBrowser(SessionBrowserOptions{Items: []BrowserSession{{Title: strings.Repeat("Detail ", 10), ID: "s1", DashboardURL: "https://example.com/" + strings.Repeat("path/", 20)}}})
	browser.Handle(context.Background(), bentotui.Key{Kind: bentotui.KeyEnter})
	_ = browser.View(bentotui.Screen{Width: 48, Height: 12})
	if browser.detailMax == 0 {
		t.Fatal("expected scrollable detail")
	}
	browser.Handle(context.Background(), bentotui.Key{Kind: bentotui.KeyRune, Rune: 'G'})
	if browser.detailTop != browser.detailMax {
		t.Fatalf("detailTop=%d max=%d", browser.detailTop, browser.detailMax)
	}
	browser.Handle(context.Background(), bentotui.Key{Kind: bentotui.KeyRune, Rune: 'g'})
	if browser.detailTop != 0 {
		t.Fatalf("detailTop=%d", browser.detailTop)
	}
}

func TestSessionBrowserStaysWithinNarrowWidth(t *testing.T) {
	browser := NewSessionBrowser(SessionBrowserOptions{Items: []BrowserSession{{
		Title: strings.Repeat("wide title ", 20), Outcome: "abandoned", Capture: "10 saved · 2 failed",
		Started: "2026-07-28 14:00", Repo: strings.Repeat("repository", 8), Model: "model", ID: strings.Repeat("id", 40),
	}}})
	view := browser.View(bentotui.Screen{Width: 48, Height: 16})
	for _, line := range strings.Split(view, "\n") {
		if bentotui.VisibleWidth(line) > 48 {
			t.Fatalf("line width %d: %q", bentotui.VisibleWidth(line), line)
		}
	}
}

func TestSessionBrowserUsesGlobalAnchoredFrame(t *testing.T) {
	browser := NewSessionBrowser(SessionBrowserOptions{Items: []BrowserSession{{Title: "One", ID: "s1"}}})
	for _, test := range []struct {
		screen        bentotui.Screen
		width, height int
	}{
		{bentotui.Screen{Width: 140, Height: 40}, 80, 20},
		{bentotui.Screen{Width: 48, Height: 12}, 48, 12},
	} {
		view := browser.View(test.screen)
		lines := strings.Split(view, "\n")
		if len(lines) != test.height {
			t.Fatalf("height %d", len(lines))
		}
		for _, line := range lines {
			if bentotui.VisibleWidth(line) != test.width || strings.HasPrefix(line, " ") {
				t.Fatalf("unanchored global frame line %q", line)
			}
		}
	}
}
