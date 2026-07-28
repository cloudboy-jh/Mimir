package ui

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/cloudboy-jh/mimir/internal/ui/bentotui"
)

func TestOperationViewportTracksPhasesAndNavigation(t *testing.T) {
	cancelled := false
	app := &operationApp{
		title: "Mimir deploy", started: time.Now(), active: 0, follow: true, status: "running", updates: make(chan struct{}, 1),
		entries: []operationEntry{{label: "Preparing Worker", state: StepComplete}, {label: "Deploying Worker", state: StepActive}},
		cancel:  func() { cancelled = true },
	}
	view := app.View(bentotui.Screen{Width: 64, Height: 10})
	for _, expected := range []string{"┌─ Mimir deploy", "[✓] Preparing Worker", "[›] Deploying Worker", "ctrl+c cancel", "└"} {
		if !strings.Contains(view, expected) {
			t.Fatalf("missing %q:\n%s", expected, view)
		}
	}
	app.Handle(context.Background(), bentotui.Key{Kind: bentotui.KeyUp})
	if app.follow {
		t.Fatal("scrolling up should disable follow mode")
	}
	app.Handle(context.Background(), bentotui.Key{Kind: bentotui.KeyRune, Rune: 'f'})
	if !app.follow {
		t.Fatal("f should restore follow mode")
	}
	app.Handle(context.Background(), bentotui.Key{Kind: bentotui.KeyInterrupt})
	if !cancelled || app.status != "cancelling" {
		t.Fatalf("cancelled=%v status=%q", cancelled, app.status)
	}
}

func TestOperationStatusBuildsScrollableHistory(t *testing.T) {
	app := &operationApp{title: "Mimir update", started: time.Now(), active: -1, follow: true, status: "running", updates: make(chan struct{}, 1)}
	operation := &Operation{app: app}
	operation.Status("Checking release")
	operation.Status("Downloading release")
	operation.Status("Verifying checksum")
	if len(app.entries) != 3 || app.entries[0].state != StepComplete || app.entries[2].state != StepActive {
		t.Fatalf("entries %#v", app.entries)
	}
}

func TestOperationUpdatesPreserveManualScroll(t *testing.T) {
	app := &operationApp{title: "Mimir update", started: time.Now(), active: -1, follow: false, updates: make(chan struct{}, 1)}
	operation := &Operation{app: app}
	operation.Status("Checking release")
	app.addLog("output")
	operation.Complete("Done")
	if app.follow {
		t.Fatal("new operation output should not re-enable follow after manual scrolling")
	}
}

func TestOperationOutputSanitizesAndBoundsLogs(t *testing.T) {
	app := &operationApp{updates: make(chan struct{}, 1), active: -1}
	writer := &operationLogWriter{app: app}
	_, _ = writer.Write([]byte("\x1b[31mdeploying\x1b[0m\rcomplete\u009bunsafe\n"))
	if len(app.logs) != 2 || app.logs[0] != "deploying" || app.logs[1] != "completeunsafe" {
		t.Fatalf("logs %#v", app.logs)
	}
	for index := 0; index < 220; index++ {
		_, _ = writer.Write([]byte("line\n"))
	}
	if len(app.logs) != 200 {
		t.Fatalf("log count %d, want 200", len(app.logs))
	}
}

func TestOperationFollowUsesWrappedViewportLines(t *testing.T) {
	app := &operationApp{
		title: "Mimir deploy", started: time.Now(), active: -1, follow: true, status: "running", updates: make(chan struct{}, 1),
		logs: []string{strings.Repeat("long output ", 20), "TAIL"},
	}
	view := app.View(bentotui.Screen{Width: 40, Height: 8})
	if !strings.Contains(view, "TAIL") {
		t.Fatalf("follow mode did not reach wrapped tail:\n%s", view)
	}
}
