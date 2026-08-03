package mimirtui

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/cloudboy-jh/mimir/internal/pi"
	domainsessions "github.com/cloudboy-jh/mimir/internal/sessions"
	"github.com/cloudboy-jh/mimir/internal/ui/bentotui"
	sessionui "github.com/cloudboy-jh/mimir/internal/ui/sessions"
)

type Agent interface {
	Events() <-chan pi.Envelope
	Prompt(context.Context, string) (string, error)
	Abort(context.Context) (string, error)
	SetModel(context.Context, string) (string, error)
	Close() error
}

type Options struct {
	Context    context.Context
	Out        io.Writer
	Agent      Agent
	Load       func(context.Context) ([]sessionui.BrowserSession, error)
	GetDetail  func(context.Context, string) (domainsessions.Detail, error)
	SetOutcome func(context.Context, string, domainsessions.SetOutcomeOptions) error
}

type screen uint8

const (
	screenSplit screen = iota
	screenAgent
	screenDetail
)

type focus uint8

const (
	focusSessions focus = iota
	focusFilter
	focusAgent
	focusOutcome
)

type chatLine struct{ role, text string }

type Model struct {
	mu      sync.Mutex
	options Options
	updates chan struct{}

	items        []sessionui.BrowserSession
	visible      []int
	selected     int
	offset       int
	query        string
	focus        focus
	screen       screen
	details      map[string]domainsessions.Detail
	detailOffset int

	input       string
	messages    []chatLine
	streaming   string
	agentOffset int
	busy        bool
	splitRatio  int
	theme       int
	status      string
	loading     bool
}

func New(options Options) *Model {
	if options.Context == nil {
		options.Context = context.Background()
	}
	m := &Model{options: options, updates: make(chan struct{}, 1), details: map[string]domainsessions.Detail{}, splitRatio: 55, loading: true}
	m.messages = []chatLine{{role: "agent", text: "Ask about the selected session or search across Mimir."}}
	if options.Agent != nil {
		go m.readAgent(options.Agent.Events())
	} else {
		m.status = "Agent unavailable"
	}
	go m.reload()
	return m
}

func (m *Model) Updates() <-chan struct{} { return m.updates }

func (m *Model) Close() error {
	if m.options.Agent == nil {
		return nil
	}
	return m.options.Agent.Close()
}

func (m *Model) notify() {
	select {
	case m.updates <- struct{}{}:
	default:
	}
}

func (m *Model) reload() {
	if m.options.Load == nil {
		m.mu.Lock()
		m.loading = false
		m.status = "Session loader unavailable"
		m.mu.Unlock()
		m.notify()
		return
	}
	items, err := m.options.Load(m.options.Context)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.loading = false
	if err != nil {
		m.status = "Load failed: " + err.Error()
		m.notify()
		return
	}
	selectedID := m.currentIDLocked()
	m.items = append([]sessionui.BrowserSession(nil), items...)
	m.applyFilterLocked()
	if selectedID != "" {
		for position, index := range m.visible {
			if m.items[index].ID == selectedID {
				m.selected = position
				break
			}
		}
	}
	m.status = fmt.Sprintf("%d sessions", len(m.visible))
	m.notify()
}

func (m *Model) Handle(ctx context.Context, key bentotui.Key) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if key.Kind == bentotui.KeyInterrupt {
		return true
	}
	if m.focus == focusFilter {
		m.handleFilterLocked(key)
		return false
	}
	if m.focus == focusOutcome {
		m.handleOutcomeLocked(ctx, key)
		return false
	}
	if m.screen == screenDetail {
		m.handleDetailLocked(key)
		return false
	}
	if m.focus == focusAgent || m.screen == screenAgent {
		return m.handleAgentLocked(ctx, key)
	}
	return m.handleSessionsLocked(ctx, key)
}

func (m *Model) handleSessionsLocked(ctx context.Context, key bentotui.Key) bool {
	if key.Kind == bentotui.KeyTab {
		m.screen = screenSplit
		m.focus = focusAgent
		return false
	}
	if key.Modifiers&bentotui.KeyModifierCtrl != 0 && (key.Kind == bentotui.KeyUp || key.Kind == bentotui.KeyDown) {
		if key.Kind == bentotui.KeyUp {
			m.splitRatio -= 5
		} else {
			m.splitRatio += 5
		}
		m.splitRatio = max(25, min(75, m.splitRatio))
		return false
	}
	switch key.Kind {
	case bentotui.KeyUp, bentotui.KeyMouseUp:
		m.moveLocked(-1)
	case bentotui.KeyDown, bentotui.KeyMouseDown:
		m.moveLocked(1)
	case bentotui.KeyEnter:
		if id := m.currentIDLocked(); id != "" {
			m.screen = screenDetail
			m.detailOffset = 0
			go m.loadDetail(id)
		}
	case bentotui.KeyEscape:
		if m.query != "" {
			m.query = ""
			m.applyFilterLocked()
		}
	case bentotui.KeyRune:
		switch key.Rune {
		case 'q':
			return true
		case 'j':
			m.moveLocked(1)
		case 'k':
			m.moveLocked(-1)
		case '/':
			m.focus = focusFilter
		case 'o':
			if m.currentIDLocked() != "" {
				m.focus = focusOutcome
			}
		case 'r':
			if !m.loading {
				m.loading = true
				go m.reload()
			}
		case 'z':
			m.screen = screenAgent
			m.focus = focusAgent
		case 't':
			m.theme = (m.theme + 1) % len(bentotui.Themes())
		}
	}
	return false
}

func (m *Model) handleDetailLocked(key bentotui.Key) {
	switch key.Kind {
	case bentotui.KeyEscape:
		m.screen = screenSplit
		m.focus = focusSessions
		m.detailOffset = 0
	case bentotui.KeyUp, bentotui.KeyMouseUp:
		m.detailOffset++
	case bentotui.KeyDown, bentotui.KeyMouseDown:
		m.detailOffset = max(0, m.detailOffset-1)
	case bentotui.KeyRune:
		switch key.Rune {
		case 'k':
			m.detailOffset++
		case 'j':
			m.detailOffset = max(0, m.detailOffset-1)
		case 'o':
			if m.currentIDLocked() != "" {
				m.focus = focusOutcome
			}
		}
	}
}

func (m *Model) handleFilterLocked(key bentotui.Key) {
	switch key.Kind {
	case bentotui.KeyEscape:
		m.query = ""
		m.focus = focusSessions
		m.applyFilterLocked()
	case bentotui.KeyEnter:
		m.focus = focusSessions
	case bentotui.KeyBackspace:
		m.query = trimLastRune(m.query)
		m.applyFilterLocked()
	case bentotui.KeyRune:
		m.query += string(key.Rune)
		m.applyFilterLocked()
	}
}

func (m *Model) handleOutcomeLocked(ctx context.Context, key bentotui.Key) {
	if key.Kind == bentotui.KeyEscape {
		m.focus = focusSessions
		return
	}
	if key.Kind != bentotui.KeyRune {
		return
	}
	outcomes := map[rune]string{'l': "landed", 'd': "discarded", 'a': "abandoned", 'u': "unresolved"}
	outcome, ok := outcomes[key.Rune]
	if !ok {
		return
	}
	id := m.currentIDLocked()
	m.focus = focusSessions
	m.status = "Setting outcome..."
	go func() {
		err := error(nil)
		if m.options.SetOutcome == nil {
			err = fmt.Errorf("outcome service unavailable")
		} else {
			err = m.options.SetOutcome(ctx, id, domainsessions.SetOutcomeOptions{Outcome: outcome})
		}
		m.mu.Lock()
		if err != nil {
			m.status = "Outcome failed: " + err.Error()
		} else {
			for i := range m.items {
				if m.items[i].ID == id {
					m.items[i].Outcome = outcome
				}
			}
			m.status = "Outcome: " + outcome
		}
		m.mu.Unlock()
		m.notify()
	}()
}

func (m *Model) handleAgentLocked(ctx context.Context, key bentotui.Key) bool {
	switch key.Kind {
	case bentotui.KeyTab:
		if m.screen == screenSplit {
			m.focus = focusSessions
		}
	case bentotui.KeyEscape:
		if m.screen == screenAgent && m.busy && m.options.Agent != nil {
			go m.abortAgent(ctx)
			m.status = "Agent paused"
		}
		m.screen = screenSplit
		m.focus = focusSessions
	case bentotui.KeyBackspace:
		m.input = trimLastRune(m.input)
	case bentotui.KeyUp, bentotui.KeyMouseUp:
		m.agentOffset++
	case bentotui.KeyDown, bentotui.KeyMouseDown:
		m.agentOffset = max(0, m.agentOffset-1)
	case bentotui.KeyEnter:
		return m.submitLocked(ctx)
	case bentotui.KeyRune:
		if key.Rune == 'z' && m.screen == screenAgent && m.input == "" {
			m.screen = screenSplit
			m.focus = focusSessions
			return false
		}
		m.input += string(key.Rune)
	}
	return false
}

func (m *Model) submitLocked(ctx context.Context) bool {
	input := strings.TrimSpace(m.input)
	if input == "" {
		return false
	}
	m.input = ""
	if strings.HasPrefix(input, "/") {
		fields := strings.Fields(input)
		switch fields[0] {
		case "/quit":
			return true
		case "/theme":
			m.theme = (m.theme + 1) % len(bentotui.Themes())
			m.status = "Theme: " + bentotui.Themes()[m.theme].Name
			return false
		case "/help":
			m.messages = append(m.messages, chatLine{role: "agent", text: "/model provider/model · /theme · /help · /quit"})
			return false
		case "/model":
			if len(fields) != 2 || m.options.Agent == nil {
				m.status = "Usage: /model provider/model"
				return false
			}
			m.status = "Switching model to " + fields[1]
			go m.setAgentModel(ctx, fields[1])
			return false
		default:
			m.status = "Unknown command; use /help"
			return false
		}
	}
	m.messages = append(m.messages, chatLine{role: "you", text: input})
	if m.options.Agent == nil {
		m.status = "Agent unavailable"
		return false
	}
	contextPrompt := "Mimir terminal context:\n"
	if current, ok := m.currentLocked(); ok {
		contextPrompt += fmt.Sprintf("Selected session: %s (%s), title %q.\n", current.ID, current.Outcome, current.Title)
	}
	if m.query != "" {
		contextPrompt += fmt.Sprintf("Current session filter: %q.\n", m.query)
	}
	contextPrompt += "Use the Mimir tools for factual claims and mutations.\n\nUser: " + input
	m.busy = true
	m.streaming = ""
	m.status = "Agent working..."
	go m.promptAgent(ctx, contextPrompt)
	return false
}

func (m *Model) promptAgent(ctx context.Context, prompt string) {
	_, err := m.options.Agent.Prompt(ctx, prompt)
	if err == nil {
		return
	}
	m.mu.Lock()
	m.busy = false
	m.status = "Agent send failed: " + err.Error()
	m.mu.Unlock()
	m.notify()
}

func (m *Model) abortAgent(ctx context.Context) {
	if _, err := m.options.Agent.Abort(ctx); err != nil {
		m.mu.Lock()
		m.status = "Agent pause failed: " + err.Error()
		m.mu.Unlock()
		m.notify()
	}
}

func (m *Model) setAgentModel(ctx context.Context, model string) {
	if _, err := m.options.Agent.SetModel(ctx, model); err != nil {
		m.mu.Lock()
		m.status = "Model switch failed: " + err.Error()
		m.mu.Unlock()
		m.notify()
	}
}

func (m *Model) loadDetail(id string) {
	if m.options.GetDetail == nil {
		return
	}
	detail, err := m.options.GetDetail(m.options.Context, id)
	m.mu.Lock()
	if err != nil {
		m.status = "Detail failed: " + err.Error()
	} else {
		m.details[id] = detail
	}
	m.mu.Unlock()
	m.notify()
}

func (m *Model) readAgent(events <-chan pi.Envelope) {
	for event := range events {
		m.mu.Lock()
		switch event.Type {
		case "message_update":
			var payload struct {
				Assistant struct {
					Type  string `json:"type"`
					Delta string `json:"delta"`
				} `json:"assistantMessageEvent"`
			}
			if json.Unmarshal(event.Raw, &payload) == nil && payload.Assistant.Type == "text_delta" {
				m.streaming += payload.Assistant.Delta
			}
		case "agent_end":
			if strings.TrimSpace(m.streaming) != "" {
				m.messages = append(m.messages, chatLine{role: "agent", text: strings.TrimSpace(m.streaming)})
				m.streaming = ""
			}
		case "agent_settled":
			m.busy = false
			m.status = "Agent ready"
		case "extension_error":
			m.status = "Agent tool error"
		case "response":
			if event.Success != nil && !*event.Success {
				m.status = "Agent error: " + event.Error
				m.busy = false
			}
		}
		m.mu.Unlock()
		m.notify()
	}
	m.mu.Lock()
	if m.options.Context.Err() == nil {
		m.busy = false
		m.status = "Agent stopped"
	}
	m.mu.Unlock()
	m.notify()
}

func (m *Model) applyFilterLocked() {
	query := strings.ToLower(strings.TrimSpace(m.query))
	m.visible = m.visible[:0]
	for i, item := range m.items {
		haystack := strings.ToLower(strings.Join([]string{item.Title, item.Outcome, item.Repo, item.Model, item.Harness, item.ID}, " "))
		if query == "" || strings.Contains(haystack, query) {
			m.visible = append(m.visible, i)
		}
	}
	m.selected = min(max(0, m.selected), max(0, len(m.visible)-1))
	m.offset = min(m.offset, m.selected)
}

func (m *Model) moveLocked(delta int) {
	if len(m.visible) > 0 {
		m.selected = min(len(m.visible)-1, max(0, m.selected+delta))
		if m.screen == screenDetail {
			id := m.currentIDLocked()
			if _, loaded := m.details[id]; !loaded {
				go m.loadDetail(id)
			}
		}
	}
}

func (m *Model) currentLocked() (sessionui.BrowserSession, bool) {
	if len(m.visible) == 0 || m.selected >= len(m.visible) {
		return sessionui.BrowserSession{}, false
	}
	return m.items[m.visible[m.selected]], true
}

func (m *Model) currentIDLocked() string {
	item, _ := m.currentLocked()
	return item.ID
}

func trimLastRune(value string) string {
	if value == "" {
		return value
	}
	_, size := utf8.DecodeLastRuneInString(value)
	return value[:len(value)-size]
}
