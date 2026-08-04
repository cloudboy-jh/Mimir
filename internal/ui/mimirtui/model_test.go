package mimirtui

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/cloudboy-jh/mimir/internal/pi"
	"github.com/cloudboy-jh/mimir/internal/ui/appframe"
	"github.com/cloudboy-jh/mimir/internal/ui/bentotui"
	sessionui "github.com/cloudboy-jh/mimir/internal/ui/sessions"
)

type recordingAgent struct {
	prompts chan string
	events  chan pi.Envelope
}

func (a *recordingAgent) Events() <-chan pi.Envelope { return a.events }
func (a *recordingAgent) Prompt(_ context.Context, prompt string) (string, error) {
	a.prompts <- prompt
	return "req-1", nil
}
func (a *recordingAgent) Abort(context.Context) (string, error)            { return "req-2", nil }
func (a *recordingAgent) SetModel(context.Context, string) (string, error) { return "req-3", nil }
func (a *recordingAgent) Close() error                                     { return nil }

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

func TestHomeAndFullscreenAgentViews(t *testing.T) {
	m := testModel()
	view := m.View(bentotui.Screen{Width: 80, Height: 20})
	if !strings.Contains(view, "Sessions") || !strings.Contains(view, "Pi agent") || len(strings.Split(view, "\n")) != 20 {
		t.Fatalf("unexpected home view:\n%s", view)
	}
	m.Handle(context.Background(), bentotui.Key{Kind: bentotui.KeyTab})
	m.Handle(context.Background(), bentotui.Key{Kind: bentotui.KeyRune, Rune: 'z'})
	view = m.View(bentotui.Screen{Width: 80, Height: 20})
	if strings.Contains(view, "Sessions") || !strings.Contains(view, "◆ Pi agent") {
		t.Fatalf("unexpected fullscreen view:\n%s", view)
	}
}

func TestFullTerminalViewUsesMeasuredDimensions(t *testing.T) {
	m := testModel()
	for _, screen := range []bentotui.Screen{{Width: 120, Height: 40}, {Width: 200, Height: 60}} {
		view := m.View(screen)
		lines := strings.Split(view, "\n")
		if len(lines) != screen.Height {
			t.Fatalf("height %d for %#v", len(lines), screen)
		}
		for _, line := range lines {
			if bentotui.VisibleWidth(line) != screen.Width {
				t.Fatalf("width %d for %#v: %q", bentotui.VisibleWidth(line), screen, line)
			}
		}
	}
}

func TestHomeViewIncludesFullWordmark(t *testing.T) {
	m := testModel()
	view := m.View(bentotui.Screen{Width: 120, Height: 40})
	lines := strings.Split(view, "\n")
	if len(lines) != 40 {
		t.Fatalf("got %d lines", len(lines))
	}
	for _, row := range mimirWordmark {
		if !strings.Contains(view, row) {
			t.Fatalf("wordmark row %q missing:\n%s", row, view)
		}
	}
	if !strings.Contains(view, "Sessions") || !strings.Contains(view, "Pi agent") || !strings.Contains(view, "Ask about your memory") {
		t.Fatalf("home surfaces missing:\n%s", view)
	}
}

func TestMinimumViewUsesCompactWordmark(t *testing.T) {
	view := testModel().View(bentotui.Screen{Width: 48, Height: 12})
	if !strings.Contains(view, "MIMIR") {
		t.Fatalf("compact wordmark missing:\n%s", view)
	}
}

func TestAgentScrollMovesTowardOlderOutputOnUp(t *testing.T) {
	m := testModel()
	m.mu.Lock()
	m.focus = focusAgent
	m.mu.Unlock()
	m.Handle(context.Background(), bentotui.Key{Kind: bentotui.KeyUp})
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.agentOffset != 1 {
		t.Fatalf("agent offset %d", m.agentOffset)
	}
}

func TestFilterAndAgentContext(t *testing.T) {
	m := testModel()
	m.Handle(context.Background(), bentotui.Key{Kind: bentotui.KeyTab})
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

func TestEnterOpensFullDetailAndEscapeReturnsToSplit(t *testing.T) {
	m := testModel()
	m.Handle(context.Background(), bentotui.Key{Kind: bentotui.KeyTab})
	m.Handle(context.Background(), bentotui.Key{Kind: bentotui.KeyEnter})
	m.mu.Lock()
	if m.screen != screenDetail {
		t.Fatalf("screen %d, want detail", m.screen)
	}
	m.mu.Unlock()
	view := m.View(bentotui.Screen{Width: 80, Height: 20})
	if !strings.Contains(view, "Session detail") || strings.Contains(view, "recent sessions") {
		t.Fatalf("unexpected detail view:\n%s", view)
	}
	m.Handle(context.Background(), bentotui.Key{Kind: bentotui.KeyEscape})
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.screen != screenHome || m.focus != focusSessions {
		t.Fatalf("screen=%d focus=%d after escape", m.screen, m.focus)
	}
}

func TestHomeSessionRowsIncludeOutcomeAndUsage(t *testing.T) {
	view := testModel().View(bentotui.Screen{Width: 80, Height: 20})
	if !strings.Contains(view, "╭") || !strings.Contains(view, "[LANDED]") || !strings.Contains(view, "dashboard rebuild") || !strings.Contains(view, "481K") {
		t.Fatalf("session summary missing:\n%s", view)
	}
}

func TestSessionOutcomeBadgesUseSemanticColors(t *testing.T) {
	m := testModel()
	render := appframe.Renderer{Out: io.Discard, Color: true, Width: 72, Theme: bentotui.Mimir}
	view := strings.Join(m.homeSessionLinesLocked(render, 2), "\n")
	landed := bentotui.Mimir.Success
	unresolved := bentotui.Mimir.Muted
	if !strings.Contains(view, fmt.Sprintf("38;2;%d;%d;%dm[LANDED]", landed.R, landed.G, landed.B)) {
		t.Fatalf("landed color missing:\n%s", view)
	}
	if !strings.Contains(view, fmt.Sprintf("38;2;%d;%d;%dm[UNRESOLVED]", unresolved.R, unresolved.G, unresolved.B)) {
		t.Fatalf("unresolved color missing:\n%s", view)
	}
}

func TestSlashOpensCommandsAndThemePicker(t *testing.T) {
	m := testModel()
	m.Handle(context.Background(), bentotui.Key{Kind: bentotui.KeyRune, Rune: '/'})
	if view := m.View(bentotui.Screen{Width: 80, Height: 20}); !strings.Contains(view, "Commands") || !strings.Contains(view, "/theme") {
		t.Fatalf("command overlay missing:\n%s", view)
	}
	for _, r := range "theme" {
		m.Handle(context.Background(), bentotui.Key{Kind: bentotui.KeyRune, Rune: r})
	}
	m.Handle(context.Background(), bentotui.Key{Kind: bentotui.KeyEnter})
	if view := m.View(bentotui.Screen{Width: 80, Height: 20}); !strings.Contains(view, "Themes") || !strings.Contains(view, "mimir") {
		t.Fatalf("theme picker missing:\n%s", view)
	}
}

func TestSlashHelpOpensTUIHelp(t *testing.T) {
	m := testModel()
	for _, r := range "/help" {
		m.Handle(context.Background(), bentotui.Key{Kind: bentotui.KeyRune, Rune: r})
	}
	m.Handle(context.Background(), bentotui.Key{Kind: bentotui.KeyEnter})
	view := m.View(bentotui.Screen{Width: 80, Height: 20})
	if !strings.Contains(view, "Mimir help") || !strings.Contains(view, "Sessions") || !strings.Contains(view, "Pi") {
		t.Fatalf("help overlay missing:\n%s", view)
	}
}

func TestPiPromptOpensAgentViewAndReceivesReply(t *testing.T) {
	agent := &recordingAgent{prompts: make(chan string, 1), events: make(chan pi.Envelope)}
	m := New(Options{Context: context.Background(), Out: io.Discard, Agent: agent, Load: func(context.Context) ([]sessionui.BrowserSession, error) {
		return []sessionui.BrowserSession{{ID: "ses-1", Title: "test session"}}, nil
	}})
	m.mu.Lock()
	m.input = "what happened?"
	m.mu.Unlock()
	m.Handle(context.Background(), bentotui.Key{Kind: bentotui.KeyEnter})
	select {
	case prompt := <-agent.prompts:
		if !strings.Contains(prompt, "what happened?") {
			t.Fatalf("prompt missing input: %q", prompt)
		}
	case <-time.After(time.Second):
		t.Fatal("pi prompt was not sent")
	}
	m.mu.Lock()
	if m.screen != screenAgent || !m.busy {
		t.Fatalf("screen=%d busy=%v", m.screen, m.busy)
	}
	m.applyAgentEventLocked(pi.Envelope{Type: "message_update", Raw: []byte(`{"type":"message_update","assistantMessageEvent":{"type":"text_delta","delta":"It landed."}}`)})
	m.applyAgentEventLocked(pi.Envelope{Type: "turn_end", Raw: []byte(`{"type":"turn_end"}`)})
	if m.busy || len(m.messages) != 2 || m.messages[1].text != "It landed." {
		t.Fatalf("unexpected pi state: busy=%v messages=%#v", m.busy, m.messages)
	}
	m.mu.Unlock()
}
