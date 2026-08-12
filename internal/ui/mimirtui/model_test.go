package mimirtui

import (
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/cloudboy-jh/mimir/internal/pi"
	domainsessions "github.com/cloudboy-jh/mimir/internal/sessions"
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
func (a *recordingAgent) AvailableModels(context.Context) ([]pi.Model, error) {
	return []pi.Model{{Provider: "openrouter", ID: "test/model", Name: "Test model"}}, nil
}
func (a *recordingAgent) Close() error { return nil }

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
	if !strings.Contains(view, "Sessions") || !strings.Contains(view, "Ask Mimir") || len(strings.Split(view, "\n")) != 20 {
		t.Fatalf("unexpected home view:\n%s", view)
	}
	m.Handle(context.Background(), bentotui.Key{Kind: bentotui.KeyTab})
	m.Handle(context.Background(), bentotui.Key{Kind: bentotui.KeyRune, Rune: 'z'})
	view = m.View(bentotui.Screen{Width: 80, Height: 20})
	if strings.Contains(view, "recent sessions") || !strings.Contains(view, "◆ Ask Mimir") {
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
	if !strings.Contains(view, "Sessions") || !strings.Contains(view, "Ask Mimir") || !strings.Contains(view, "Ask Mimir anything about your sessions.") {
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

func TestHomeFrameTransitionsPreserveState(t *testing.T) {
	m := transitionTestModel()
	m.mu.Lock()
	wantSelected := m.currentIDLocked()
	wantQuery := m.query
	wantInput := m.input
	wantOverlay := m.overlay
	wantOverlayItem := m.overlayItem
	m.mu.Unlock()

	for _, screen := range []bentotui.Screen{
		{Width: 200, Height: 60},
		{Width: 120, Height: 40},
		{Width: 80, Height: 20},
		{Width: 48, Height: 12},
		{Width: 80, Height: 20},
		{Width: 200, Height: 60},
	} {
		view := m.View(screen)
		assertCompleteFrame(t, screen, view)
		if !strings.Contains(view, "Ask Mimir") || !strings.Contains(view, "session 06") {
			t.Fatalf("home surfaces missing at %#v:\n%s", screen, view)
		}
		m.mu.Lock()
		if got := m.currentIDLocked(); got != wantSelected {
			m.mu.Unlock()
			t.Fatalf("selected session = %q want %q", got, wantSelected)
		}
		if m.query != wantQuery || m.input != wantInput || m.overlay != wantOverlay || m.overlayItem != wantOverlayItem {
			got := []any{m.query, m.input, m.overlay, m.overlayItem}
			m.mu.Unlock()
			t.Fatalf("state changed at %#v: %#v", screen, got)
		}
		m.mu.Unlock()
	}
}

func TestAgentFrameTransitionsPreserveConversationState(t *testing.T) {
	m := transitionTestModel()
	m.mu.Lock()
	m.screen = screenAgent
	m.focus = focusAgent
	m.input = "persistent draft"
	m.streaming = "partial retained reply"
	m.busy = true
	m.agentStatus = "Thinking..."
	for index := 0; index < 80; index++ {
		m.messages = append(m.messages, chatLine{role: "agent", text: fmt.Sprintf("retained response %02d", index)})
	}
	m.agentOffset = 7
	wantMessages := append([]chatLine(nil), m.messages...)
	m.mu.Unlock()

	for _, screen := range []bentotui.Screen{{Width: 80, Height: 20}, {Width: 200, Height: 60}, {Width: 48, Height: 12}, {Width: 120, Height: 40}, {Width: 80, Height: 20}} {
		view := m.View(screen)
		assertCompleteFrame(t, screen, view)
		if strings.Contains(view, "recent sessions") {
			t.Fatalf("agent surface changed at %#v:\n%s", screen, view)
		}
		m.mu.Lock()
		if m.input != "persistent draft" || m.streaming != "partial retained reply" || !m.busy || m.agentOffset != 7 || fmt.Sprint(m.messages) != fmt.Sprint(wantMessages) {
			got := []any{m.input, m.streaming, m.busy, m.agentOffset, len(m.messages)}
			m.mu.Unlock()
			t.Fatalf("agent state changed at %#v: %#v", screen, got)
		}
		m.mu.Unlock()
	}
}

func transitionTestModel() *Model {
	items := make([]sessionui.BrowserSession, 12)
	for index := range items {
		items[index] = sessionui.BrowserSession{ID: fmt.Sprintf("ses-%02d", index+1), Title: fmt.Sprintf("session %02d resize evidence", index+1), Outcome: "landed", Harness: "opencode", Capture: "2 saved"}
	}
	m := &Model{
		options:      Options{Context: context.Background(), Out: io.Discard},
		updates:      make(chan struct{}, 1),
		items:        items,
		selected:     5,
		query:        "session",
		focus:        focusSessions,
		screen:       screenHome,
		details:      map[string]domainsessions.Detail{},
		input:        "retained input",
		overlay:      overlayCommands,
		overlayItem:  2,
		status:       "12 sessions",
		agentStatus:  "Mimir ready",
		currentModel: "test/model",
	}
	m.applyFilterLocked()
	return m
}

func assertCompleteFrame(t *testing.T, screen bentotui.Screen, view string) {
	t.Helper()
	lines := strings.Split(view, "\n")
	if len(lines) != screen.Height {
		t.Fatalf("height %d want %d at %#v", len(lines), screen.Height, screen)
	}
	for row, line := range lines {
		if width := bentotui.VisibleWidth(line); width != screen.Width {
			t.Fatalf("row %d width %d want %d at %#v: %q", row, width, screen.Width, screen, line)
		}
	}
	if !strings.Contains(lines[len(lines)-2], "─") || strings.TrimSpace(lines[len(lines)-1]) == "" {
		t.Fatalf("divider/footer displaced at %#v:\n%s", screen, view)
	}
}

func TestExpandedDetailStaysBoundedAndKeepsListContext(t *testing.T) {
	for _, screen := range []bentotui.Screen{{Width: 48, Height: 12}, {Width: 80, Height: 20}, {Width: 120, Height: 40}} {
		m := testModel()
		m.mu.Lock()
		m.focus = focusSessions
		m.expanded = true
		m.mu.Unlock()
		view := m.View(screen)
		if !strings.Contains(view, "Sessions") || !strings.Contains(view, "Evidence") {
			t.Fatalf("expanded list context missing at %#v:\n%s", screen, view)
		}
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

func TestUnavailableAssistantDoesNotBlockSessionBrowsing(t *testing.T) {
	m := New(Options{Context: context.Background(), Out: io.Discard, AgentStatus: "Mimir unavailable: executable not found", Load: func(context.Context) ([]sessionui.BrowserSession, error) {
		return []sessionui.BrowserSession{{ID: "ses-1", Title: "still browseable"}}, nil
	}})
	m.mu.Lock()
	m.items = []sessionui.BrowserSession{{ID: "ses-1", Title: "still browseable"}}
	m.applyFilterLocked()
	m.mu.Unlock()
	view := m.View(bentotui.Screen{Width: 80, Height: 20})
	if !strings.Contains(view, "still browseable") || !strings.Contains(view, "Mimir unavailable") {
		t.Fatalf("degraded TUI missing sessions or diagnostics:\n%s", view)
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

func TestEnterExpandsDetailInsideSessionList(t *testing.T) {
	m := testModel()
	m.Handle(context.Background(), bentotui.Key{Kind: bentotui.KeyTab})
	m.Handle(context.Background(), bentotui.Key{Kind: bentotui.KeyEnter})
	m.mu.Lock()
	if !m.expanded || m.screen != screenHome {
		t.Fatalf("expanded=%v screen=%d", m.expanded, m.screen)
	}
	m.mu.Unlock()
	view := m.View(bentotui.Screen{Width: 80, Height: 20})
	if !strings.Contains(view, "Evidence") || !strings.Contains(view, "recent sessions") || !strings.Contains(view, "Ask Mimir") {
		t.Fatalf("unexpected detail view:\n%s", view)
	}
	m.Handle(context.Background(), bentotui.Key{Kind: bentotui.KeyEnter})
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.expanded {
		t.Fatal("detail did not collapse")
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
	if view := m.View(bentotui.Screen{Width: 80, Height: 20}); !strings.Contains(view, "Commands") || !strings.Contains(view, "/model") {
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
	if !strings.Contains(view, "Mimir help") || !strings.Contains(view, "Sessions") || !strings.Contains(view, "Ask Mimir") {
		t.Fatalf("help overlay missing:\n%s", view)
	}
}

func TestModelCommandOpensPickerAndSwitchesSelection(t *testing.T) {
	agent := &recordingAgent{prompts: make(chan string, 1), events: make(chan pi.Envelope)}
	m := New(Options{Context: context.Background(), Out: io.Discard, Agent: agent, Load: func(context.Context) ([]sessionui.BrowserSession, error) {
		return nil, nil
	}})
	for _, r := range "/model" {
		m.Handle(context.Background(), bentotui.Key{Kind: bentotui.KeyRune, Rune: r})
	}
	m.Handle(context.Background(), bentotui.Key{Kind: bentotui.KeyEnter})
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		m.mu.Lock()
		loaded := len(m.models) == 1
		m.mu.Unlock()
		if loaded {
			break
		}
		time.Sleep(time.Millisecond)
	}
	view := m.View(bentotui.Screen{Width: 80, Height: 20})
	if !strings.Contains(view, "Choose model") || !strings.Contains(view, "openrouter/test/model") {
		t.Fatalf("model picker missing:\n%s", view)
	}
	m.Handle(context.Background(), bentotui.Key{Kind: bentotui.KeyEnter})
	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		m.mu.Lock()
		selected := m.currentModel
		m.mu.Unlock()
		if selected == "openrouter/test/model" {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("model was not switched")
}

func TestPromptSeparatesUIContextFromUserRequest(t *testing.T) {
	agent := &recordingAgent{prompts: make(chan string, 1), events: make(chan pi.Envelope)}
	m := New(Options{Context: context.Background(), Out: io.Discard, Agent: agent, CurrentModel: "anthropic/test", Load: func(context.Context) ([]sessionui.BrowserSession, error) {
		return []sessionui.BrowserSession{{ID: "ses-1", Title: "my work", Outcome: "landed"}}, nil
	}})
	m.mu.Lock()
	m.items = []sessionui.BrowserSession{{ID: "ses-1", Title: "my work", Outcome: "landed"}}
	m.applyFilterLocked()
	m.input = "what happened?"
	m.mu.Unlock()
	m.Handle(context.Background(), bentotui.Key{Kind: bentotui.KeyEnter})
	select {
	case prompt := <-agent.prompts:
		for _, value := range []string{"<mimir_ui_context>", "Selected session: ses-1", "Current model: anthropic/test", "<user_request>\nwhat happened?\n</user_request>"} {
			if !strings.Contains(prompt, value) {
				t.Fatalf("prompt missing %q: %s", value, prompt)
			}
		}
	case <-time.After(time.Second):
		t.Fatal("prompt was not sent")
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
	m.applyAgentEventLocked(pi.Envelope{Type: "agent_settled", Raw: []byte(`{"type":"agent_settled"}`)})
	if m.busy || len(m.messages) != 2 || m.messages[1].text != "It landed." {
		t.Fatalf("unexpected pi state: busy=%v messages=%#v", m.busy, m.messages)
	}
	m.mu.Unlock()
}
