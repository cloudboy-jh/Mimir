package terminalui

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
	layout := appframe.ForScreen(screen)
	themes := bentotui.Themes()
	render := appframe.New(m.options.Out).WithWidth(layout.Width)
	render.Theme = themes[m.theme%len(themes)].Theme
	lines := m.bodyLinesLocked(render, layout)
	footer := m.footerLocked(render)
	status := cleanText(m.status)
	if status == "" {
		status = fmt.Sprintf("%d sessions", len(m.visible))
	}
	view, _ := (appframe.Frame{Surface: "Terminal", Status: status, Lines: lines, Footer: footer}).Render(render.Context(), screen)
	return view
}

func (m *Model) bodyLinesLocked(render appframe.Renderer, layout appframe.Layout) []string {
	if m.fullscreen {
		return fitLines(m.agentLinesLocked(render, layout.BodyHeight), layout.BodyWidth, layout.BodyHeight)
	}
	sessionRows := min(max(3, m.splitRows), max(3, layout.BodyHeight-4))
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
	capacity := max(1, height-1)
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
		lines = append(lines, compactSession(render, item, position == m.selected))
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
	meta := cleanText(item.Harness)
	if meta == "" {
		meta = cleanText(item.Model)
	}
	right := strings.TrimSpace(strings.Join(nonempty(meta, stat), "  "))
	available := max(1, render.Width-bentotui.VisibleWidth(prefix))
	if right != "" && available > bentotui.VisibleWidth(right)+8 {
		titleWidth := available - bentotui.VisibleWidth(right) - 2
		return prefix + bentotui.PadRight(bentotui.Truncate(cleanText(item.Title), titleWidth), titleWidth) + "  " + right
	}
	return prefix + bentotui.Truncate(cleanText(item.Title), available)
}

func (m *Model) agentLinesLocked(render appframe.Renderer, height int) []string {
	if height <= 0 {
		return nil
	}
	if m.detail {
		return m.detailLinesLocked(render, height)
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
	lines := []string{"◆ Session " + cleanText(item.ID), fmt.Sprintf("%s · %s · %s", cleanText(item.Capture), strings.ToUpper(cleanText(item.Outcome)), cleanText(item.Model))}
	if detail, loaded := m.details[item.ID]; loaded {
		lines = append(lines, fmt.Sprintf("%d exchanges · %s", len(detail.Exchanges), detail.Session.State))
		if len(detail.Files) > 0 {
			lines = append(lines, "Files: "+cleanText(strings.Join(detail.Files, ", ")))
		}
		if len(detail.Errors) > 0 {
			lines = append(lines, "Errors: "+cleanText(strings.Join(detail.Errors, "; ")))
		}
	} else {
		lines = append(lines, "Loading evidence...")
	}
	return fitLines(lines, render.Width, height)
}

func (m *Model) footerLocked(render appframe.Renderer) string {
	if m.focus == focusOutcome {
		return "Outcome: l landed · d discarded · a abandoned · u unresolved · Esc cancel"
	}
	if m.focus == focusFilter {
		return "Type to filter · Enter keep · Esc clear"
	}
	if m.focus == focusAgent || m.fullscreen {
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
