package mimirtui

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/cloudboy-jh/mimir/internal/ui/appframe"
	"github.com/cloudboy-jh/mimir/internal/ui/bentotui"
	sessionui "github.com/cloudboy-jh/mimir/internal/ui/sessions"
)

func (m *Model) View(screen bentotui.Screen) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if appframe.TooSmall(screen) {
		return appframe.SmallScreen(screen)
	}
	layout := appframe.FullScreenForScreen(screen)
	themes := bentotui.Themes()
	render := appframe.New(m.options.Out).WithWidth(layout.Width)
	render.Theme = themes[m.theme%len(themes)].Theme
	lines := m.bodyLinesLocked(render, layout)
	footer := m.footerLocked(render)
	status := cleanText(m.status)
	if status == "" {
		status = fmt.Sprintf("%d sessions", len(m.visible))
	}
	view, _ := (appframe.Frame{Surface: "Terminal", Status: status, Lines: lines, Footer: footer}).RenderLayout(render.Context(), screen, layout)
	return view
}

func (m *Model) bodyLinesLocked(render appframe.Renderer, layout appframe.Layout) []string {
	if m.screen == screenDetail {
		return fitLines(m.detailLinesLocked(render, layout.BodyHeight), layout.BodyWidth, layout.BodyHeight)
	}
	if m.screen == screenAgent {
		return fitLines(m.agentLinesLocked(render, layout.BodyHeight), layout.BodyWidth, layout.BodyHeight)
	}
	available := max(6, layout.BodyHeight-1)
	sessionRows := available * m.splitRatio / 100
	sessionRows = min(max(3, sessionRows), max(3, available-3))
	agentRows := layout.BodyHeight - sessionRows - 1
	sessions := m.sessionLinesLocked(render, sessionRows)
	divider := bentotui.Divider(render.Context(), "")
	agent := m.agentLinesLocked(render, agentRows)
	lines := append(sessions, divider)
	lines = append(lines, agent...)
	return fitLines(lines, layout.BodyWidth, layout.BodyHeight)
}

func (m *Model) sessionLinesLocked(render appframe.Renderer, height int) []string {
	heading := fmt.Sprintf("◆ Sessions (%d)", len(m.visible))
	if m.focus == focusFilter || m.query != "" {
		cursor := ""
		if m.focus == focusFilter {
			cursor = "_"
		}
		heading = "/ " + cleanText(m.query) + cursor
	}
	lines := []string{heading}
	capacity := max(1, (height-1)/2)
	if len(m.visible) == 0 {
		message := "No sessions match."
		if m.loading {
			message = "Loading sessions..."
		}
		lines = append(lines, message)
		return fitLines(lines, render.Width, height)
	}
	if m.selected < m.offset {
		m.offset = m.selected
	}
	if m.selected >= m.offset+capacity {
		m.offset = m.selected - capacity + 1
	}
	end := min(len(m.visible), m.offset+capacity)
	for position := m.offset; position < end; position++ {
		item := m.items[m.visible[position]]
		lines = append(lines, strings.Split(compactSession(render, item, position == m.selected), "\n")...)
	}
	return fitLines(lines, render.Width, height)
}

func compactSession(render appframe.Renderer, item sessionui.BrowserSession, selected bool) string {
	marker := "  "
	if selected {
		marker = "› "
	}
	prefix := marker + render.OutcomeBadge(cleanText(item.Outcome)) + " "
	stat := compactCount(item.Tokens)
	if stat == "0" {
		stat = cleanText(item.Capture)
	}
	available := max(1, render.Width-bentotui.VisibleWidth(prefix))
	statWidth := bentotui.VisibleWidth(stat)
	titleWidth := available
	if stat != "" && available > statWidth+8 {
		titleWidth = available - statWidth - 2
	}
	first := prefix + bentotui.PadRight(bentotui.Truncate(displayValue(item.Title, "Untitled session"), titleWidth), titleWidth)
	if stat != "" && titleWidth < available {
		first += "  " + stat
	}
	muted := bentotui.Style{Color: render.Theme.Muted, Enabled: render.Color}
	metadata := strings.Join(nonempty(
		"repo: "+displayValue(item.Repo, "none"),
		displayValue(item.Harness, displayValue(item.Model, "unknown model")),
		displayValue(item.Started, "unknown time"),
		displayValue(item.Capture, "capture pending"),
	), " · ")
	second := "  " + muted.Render(bentotui.Truncate(metadata, max(1, render.Width-2)))
	return first + "\n" + second
}

func (m *Model) agentLinesLocked(render appframe.Renderer, height int) []string {
	if height <= 0 {
		return nil
	}
	lines := []string{"◆ Agent"}
	for _, message := range m.messages {
		prefix := "◆ "
		if message.role == "you" {
			prefix = "> "
		}
		wrapped := bentotui.WrapPreserve(prefix+cleanText(message.text), max(1, render.Width))
		lines = append(lines, wrapped...)
	}
	if m.streaming != "" {
		lines = append(lines, bentotui.WrapPreserve("◆ "+cleanText(m.streaming)+"_", max(1, render.Width))...)
	}
	input := cleanText(m.input)
	inputWidth := max(1, render.Width-3)
	input = visibleTail(input, inputWidth)
	prompt := "> " + input
	if m.focus == focusAgent {
		prompt += "_"
	}
	lines = append(lines, prompt)
	if len(lines) > height {
		maxOffset := len(lines) - height
		m.agentOffset = min(m.agentOffset, maxOffset)
		start := max(0, maxOffset-m.agentOffset)
		lines = lines[start : start+height]
	}
	return fitLines(lines, render.Width, height)
}

func (m *Model) detailLinesLocked(render appframe.Renderer, height int) []string {
	item, ok := m.currentLocked()
	if !ok {
		return fitLines([]string{"◆ Session", "No session selected."}, render.Width, height)
	}
	lines := []string{
		"◆ Session detail",
		displayValue(item.Title, "Untitled session"),
		"",
		"Outcome: " + render.OutcomeBadge(cleanText(item.Outcome)),
		"Capture: " + displayValue(item.Capture, "Pending"),
		"Repository: " + displayValue(item.Repo, "None"),
		"Harness: " + displayValue(item.Harness, "Unknown"),
		"Model: " + displayValue(item.Model, "Unknown"),
		"Started: " + displayValue(item.Started, "Unknown"),
		"Tokens: " + compactCount(item.Tokens),
		"Session ID: " + cleanText(item.ID),
		"",
		"Evidence",
	}
	if detail, loaded := m.details[item.ID]; loaded {
		lines = append(lines, fmt.Sprintf("Exchanges: %d", len(detail.Exchanges)), "State: "+displayValue(detail.Session.State, "Unknown"))
		if len(detail.Files) > 0 {
			lines = append(lines, "Files: "+cleanText(strings.Join(detail.Files, ", ")))
		}
		if len(detail.Errors) > 0 {
			lines = append(lines, "Errors: "+cleanText(strings.Join(detail.Errors, "; ")))
		}
	} else {
		lines = append(lines, "Loading evidence...")
	}
	maxOffset := max(0, len(lines)-height)
	m.detailOffset = min(m.detailOffset, maxOffset)
	start := max(0, maxOffset-m.detailOffset)
	return lines[start:min(len(lines), start+height)]
}

func (m *Model) footerLocked(render appframe.Renderer) string {
	if m.focus == focusOutcome {
		return "Outcome: l landed · d discarded · a abandoned · u unresolved · Esc cancel"
	}
	if m.focus == focusFilter {
		return "Type to filter · Enter keep · Esc clear"
	}
	if m.screen == screenDetail {
		return appframe.Footer(render.Context(), []appframe.Binding{{Key: "↑↓", Label: "Scroll"}, {Key: "Esc", Label: "Back"}}, []appframe.Binding{{Key: "o", Label: "Outcome"}, {Key: "q", Label: "Quit"}})
	}
	if m.focus == focusAgent || m.screen == screenAgent {
		return appframe.Footer(render.Context(), []appframe.Binding{{Key: "Enter", Label: "Send"}, {Key: "Esc", Label: "Sessions"}}, []appframe.Binding{{Key: "/", Label: "Commands"}, {Key: "z", Label: "Split"}})
	}
	return appframe.Footer(render.Context(), []appframe.Binding{{Key: "↑↓", Label: "Browse"}, {Key: "↵", Label: "Detail"}}, []appframe.Binding{{Key: "Tab", Label: "Agent"}, {Key: "q", Label: "Quit"}})
}

func fitLines(lines []string, width, height int) []string {
	result := make([]string, 0, height)
	for _, line := range lines {
		result = append(result, bentotui.Truncate(line, max(1, width)))
		if len(result) == height {
			break
		}
	}
	for len(result) < height {
		result = append(result, "")
	}
	return result
}

func compactCount(value int) string {
	switch {
	case value >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(value)/1_000_000)
	case value >= 1_000:
		return fmt.Sprintf("%.0fK", float64(value)/1_000)
	default:
		return fmt.Sprintf("%d", value)
	}
}

func visibleTail(value string, width int) string {
	if bentotui.VisibleWidth(value) <= width {
		return value
	}
	runes := []rune(value)
	for len(runes) > 0 && bentotui.VisibleWidth(string(runes)) > width {
		runes = runes[1:]
	}
	return string(runes)
}

func nonempty(values ...string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			result = append(result, value)
		}
	}
	return result
}

func displayValue(value, fallback string) string {
	value = cleanText(value)
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func cleanText(value string) string {
	var result strings.Builder
	for index := 0; index < len(value); {
		if value[index] == 0x1b {
			index++
			if index >= len(value) {
				break
			}
			switch value[index] {
			case '[':
				index++
				for index < len(value) {
					final := value[index]
					index++
					if final >= 0x40 && final <= 0x7e {
						break
					}
				}
			case ']':
				index++
				for index < len(value) {
					if value[index] == 0x07 {
						index++
						break
					}
					if value[index] == 0x1b && index+1 < len(value) && value[index+1] == '\\' {
						index += 2
						break
					}
					index++
				}
			default:
				index++
			}
			continue
		}
		r, size := utf8.DecodeRuneInString(value[index:])
		index += size
		if r == '\n' || r == '\t' || !unicode.IsControl(r) {
			result.WriteRune(r)
		}
	}
	return result.String()
}
