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
	screenHome screen = iota
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

type overlay uint8

const (
	overlayNone overlay = iota
	overlayCommands
	overlayThemes
	overlayHelp
)

type slashCommand struct {
	name, description string
}

var slashCommands = []slashCommand{
	{name: "/model", description: "Switch model: /model provider/model"},
	{name: "/theme", description: "Open the Mimir theme picker"},
	{name: "/help", description: "Show TUI controls"},
	{name: "/quit", description: "Exit Mimir"},
}

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
	theme       int
	overlay     overlay
	overlayItem int
	status      string
	agentStatus string
	loading     bool
}

func New(options Options) *Model {
	if options.Context == nil {
		options.Context = context.Background()
	}
	m := &Model{options: options, updates: make(chan struct{}, 1), details: map[string]domainsessions.Detail{}, focus: focusAgent, agentStatus: "ready", loading: true}
	if options.Agent != nil {
		go m.readAgent(options.Agent.Events())
	} else {
		m.agentStatus = "unavailable"
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
	if m.overlay == overlayThemes || m.overlay == overlayHelp {
		if m.overlay == overlayHelp {
			if key.Kind == bentotui.KeyEscape || key.Kind == bentotui.KeyEnter {
				m.overlay = overlayNone
			}
			return false
		}
		m.handleThemePickerLocked(key)
		return false
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
		m.screen = screenHome
		m.focus = focusAgent
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
		m.screen = screenHome
		m.focus = focusSessions
		m.detailOffset = 0
	case bentotui.KeyUp, bentotui.KeyMouseUp:
		m.detailOffset = max(0, m.detailOffset-1)
	case bentotui.KeyDown, bentotui.KeyMouseDown:
		m.detailOffset++
	case bentotui.KeyRune:
		switch key.Rune {
		case 'k':
			m.detailOffset = max(0, m.detailOffset-1)
		case 'j':
			m.detailOffset++
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
	if m.overlay == overlayCommands {
		matches := m.commandMatchesLocked()
		switch key.Kind {
		case bentotui.KeyUp, bentotui.KeyMouseUp:
			if len(matches) > 0 {
				m.overlayItem = (m.overlayItem - 1 + len(matches)) % len(matches)
			}
			return false
		case bentotui.KeyDown, bentotui.KeyMouseDown:
			if len(matches) > 0 {
				m.overlayItem = (m.overlayItem + 1) % len(matches)
			}
			return false
		case bentotui.KeyTab:
			if len(matches) > 0 {
				m.input = matches[min(m.overlayItem, len(matches)-1)].name + " "
			}
			return false
		case bentotui.KeyEnter:
			if len(matches) > 0 && strings.TrimSpace(m.input) == "/" {
				m.input = matches[min(m.overlayItem, len(matches)-1)].name
			}
			return m.submitLocked(ctx)
		}
	}
	switch key.Kind {
	case bentotui.KeyTab:
		if m.screen == screenHome {
			m.focus = focusSessions
		}
	case bentotui.KeyEscape:
		if m.overlay == overlayCommands {
			m.overlay = overlayNone
			m.input = ""
			return false
		}
		if m.screen == screenAgent && m.busy && m.options.Agent != nil {
			go m.abortAgent(ctx)
			m.agentStatus = "paused"
		}
		m.screen = screenHome
		m.focus = focusSessions
	case bentotui.KeyBackspace:
		m.input = trimLastRune(m.input)
		m.syncCommandOverlayLocked()
	case bentotui.KeyUp, bentotui.KeyMouseUp:
		m.agentOffset++
	case bentotui.KeyDown, bentotui.KeyMouseDown:
		m.agentOffset = max(0, m.agentOffset-1)
	case bentotui.KeyEnter:
		return m.submitLocked(ctx)
	case bentotui.KeyRune:
		if key.Rune == 'z' && m.screen == screenAgent && m.input == "" {
			m.screen = screenHome
			m.focus = focusSessions
			return false
		}
		m.input += string(key.Rune)
		m.syncCommandOverlayLocked()
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
		m.overlay = overlayNone
		fields := strings.Fields(input)
		switch fields[0] {
		case "/quit":
			return true
		case "/theme":
			m.overlay = overlayThemes
			m.overlayItem = m.theme
			return false
		case "/help":
			m.overlay = overlayHelp
			return false
		case "/model":
			if len(fields) != 2 || m.options.Agent == nil {
				m.status = "Usage: /model provider/model"
				return false
			}
			m.agentStatus = "switching model..."
			go m.setAgentModel(ctx, fields[1])
			return false
		default:
			m.status = "Unknown command; use /help"
			return false
		}
	}
	m.overlay = overlayNone
	m.messages = append(m.messages, chatLine{role: "you", text: input})
	if m.options.Agent == nil {
		m.agentStatus = "unavailable"
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
	m.screen = screenAgent
	m.focus = focusAgent
	m.agentStatus = "working..."
	go m.promptAgent(ctx, contextPrompt)
	return false
}

func (m *Model) syncCommandOverlayLocked() {
	if strings.HasPrefix(strings.TrimSpace(m.input), "/") {
		m.overlay = overlayCommands
		m.overlayItem = 0
		return
	}
	m.overlay = overlayNone
}

func (m *Model) commandMatchesLocked() []slashCommand {
	query := strings.ToLower(strings.TrimSpace(m.input))
	if query == "" || query == "/" {
		return append([]slashCommand(nil), slashCommands...)
	}
	matches := make([]slashCommand, 0, len(slashCommands))
	for _, command := range slashCommands {
		if strings.HasPrefix(command.name, query) {
			matches = append(matches, command)
		}
	}
	return matches
}

func (m *Model) handleThemePickerLocked(key bentotui.Key) {
	themes := bentotui.Themes()
	switch key.Kind {
	case bentotui.KeyEscape:
		m.overlay = overlayNone
	case bentotui.KeyUp:
		m.overlayItem = (m.overlayItem - 1 + len(themes)) % len(themes)
	case bentotui.KeyDown:
		m.overlayItem = (m.overlayItem + 1) % len(themes)
	case bentotui.KeyEnter:
		m.theme = m.overlayItem
		m.overlay = overlayNone
		m.status = "Theme: " + themes[m.theme].Name
	}
}

func (m *Model) promptAgent(ctx context.Context, prompt string) {
	_, err := m.options.Agent.Prompt(ctx, prompt)
	if err == nil {
		return
	}
	m.mu.Lock()
	m.busy = false
	m.agentStatus = "send failed: " + err.Error()
	m.mu.Unlock()
	m.notify()
}

func (m *Model) abortAgent(ctx context.Context) {
	if _, err := m.options.Agent.Abort(ctx); err != nil {
		m.mu.Lock()
		m.agentStatus = "pause failed: " + err.Error()
		m.mu.Unlock()
		m.notify()
	}
}

func (m *Model) setAgentModel(ctx context.Context, model string) {
	_, err := m.options.Agent.SetModel(ctx, model)
	m.mu.Lock()
	if err != nil {
		m.agentStatus = "model switch failed: " + err.Error()
	} else {
		m.agentStatus = "ready · " + model
	}
	m.mu.Unlock()
	m.notify()
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
		m.applyAgentEventLocked(event)
		m.mu.Unlock()
		m.notify()
	}
	m.mu.Lock()
	if m.options.Context.Err() == nil {
		m.busy = false
		m.agentStatus = "stopped"
	}
	m.mu.Unlock()
	m.notify()
}

func (m *Model) applyAgentEventLocked(event pi.Envelope) {
	payload := agentEventPayload(event.Raw)
	switch event.Type {
	case "agent_start", "turn_start", "message_start":
		m.busy = true
		m.agentStatus = "working..."
	case "message_update":
		assistant := mapValue(payload["assistantMessageEvent"])
		switch stringValue(assistant["type"]) {
		case "text_delta":
			delta := stringValue(assistant["delta"])
			if delta == "" {
				delta = messageText(mapValue(assistant["partial"]))
			}
			m.streaming += delta
		case "text_end":
			if content := stringValue(assistant["content"]); content != "" {
				m.streaming = content
			}
		}
	case "message_end":
		message := mapValue(payload["message"])
		if stringValue(message["role"]) == "assistant" {
			if final := messageText(message); final != "" {
				m.streaming = final
			}
			m.flushStreamingLocked()
		}
	case "turn_end", "agent_end", "agent_settled":
		m.flushStreamingLocked()
		m.busy = false
		m.agentStatus = "ready"
	case "tool_execution_start":
		name := stringValue(payload["toolName"])
		if name == "" {
			name = "tool"
		}
		m.agentStatus = "running " + name + "..."
	case "tool_execution_end":
		m.agentStatus = "working..."
	case "extension_error":
		m.agentStatus = "tool error"
		m.busy = false
	case "response":
		if event.Success != nil && !*event.Success {
			m.agentStatus = "error: " + event.Error
			m.busy = false
		}
	}
}

func (m *Model) flushStreamingLocked() {
	if text := strings.TrimSpace(m.streaming); text != "" {
		m.messages = append(m.messages, chatLine{role: "agent", text: text})
	}
	m.streaming = ""
}

func agentEventPayload(raw json.RawMessage) map[string]any {
	root := map[string]any{}
	if len(raw) == 0 || json.Unmarshal(raw, &root) != nil {
		return root
	}
	if payload := mapValue(root["payload"]); len(payload) > 0 {
		return payload
	}
	return root
}

func mapValue(value any) map[string]any {
	result, _ := value.(map[string]any)
	if result == nil {
		return map[string]any{}
	}
	return result
}

func stringValue(value any) string {
	result, _ := value.(string)
	return result
}

func messageText(message map[string]any) string {
	content, _ := message["content"].([]any)
	parts := make([]string, 0, len(content))
	for _, value := range content {
		part := mapValue(value)
		if stringValue(part["type"]) == "text" {
			if text := stringValue(part["text"]); text != "" {
				parts = append(parts, text)
			}
		}
	}
	return strings.Join(parts, "\n")
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
