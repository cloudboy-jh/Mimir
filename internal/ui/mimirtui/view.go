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

var mimirWordmark = []string{
	"███╗   ███╗██╗███╗   ███╗██╗██████╗ ",
	"████╗ ████║██║████╗ ████║██║██╔══██╗",
	"██╔████╔██║██║██╔████╔██║██║██████╔╝",
	"██║╚██╔╝██║██║██║╚██╔╝██║██║██╔══██╗",
	"██║ ╚═╝ ██║██║██║ ╚═╝ ██║██║██║  ██║",
	"╚═╝     ╚═╝╚═╝╚═╝     ╚═╝╚═╝╚═╝  ╚═╝",
}

func (m *Model) View(screen bentotui.Screen) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if appframe.TooSmall(screen) {
		return appframe.SmallScreen(screen)
	}
	themes := bentotui.Themes()
	render := appframe.New(m.options.Out).WithWidth(screen.Width)
	render.Theme = themes[m.theme%len(themes)].Theme
	bodyHeight := max(1, screen.Height-2)
	var body []string
	if m.screen == screenAgent {
		work := render.WithWidth(min(84, max(40, screen.Width-4)))
		body = m.centeredWorkView(work, m.agentLinesLocked(work, bodyHeight), screen.Width, bodyHeight)
	} else {
		body = m.homeLinesLocked(render, screen.Width, bodyHeight)
	}
	if m.overlay == overlayThemes || m.overlay == overlayHelp || m.overlay == overlayModels {
		body = m.drawOverlayLocked(render, body, screen.Width, bodyHeight)
	}
	layout := appframe.FullScreenForScreen(screen)
	return (appframe.HomeFrame{Lines: body, Footer: m.footerLocked(render)}).RenderLayout(render.Context(), screen, layout)
}

func (m *Model) homeLinesLocked(render appframe.Renderer, width, height int) []string {
	contentWidth := min(84, max(40, width*3/5))
	if m.expanded {
		contentWidth = min(104, max(contentWidth, width*4/5))
	}
	contentWidth = min(contentWidth, max(1, width-4))
	local := render.WithWidth(contentWidth)
	mark := wordmarkLines(local, bentotui.Screen{Width: width, Height: height + 2})
	for index, line := range mark {
		mark[index] = centerLine(line, contentWidth)
	}
	sessionRows := min(8, max(2, (height-14)/2))
	if height < 14 {
		sessionRows = 1
	}
	if m.overlay == overlayCommands {
		sessionRows = min(sessionRows, 1)
	}
	sessions := m.homeSessionLinesLocked(local, sessionRows)
	input := m.homeInputLinesLocked(local)
	commands := m.homeCommandLinesLocked(local, min(6, max(3, height/3)))
	fixed := len(mark) + 1 + len(sessions) + 1 + 1 + len(input) + len(commands) + 1
	conversationHeight := min(6, max(0, height-fixed))
	conversation := m.homeConversationLinesLocked(local, conversationHeight)

	content := append([]string{}, mark...)
	content = append(content, "")
	content = append(content, fillWidth(sessions, contentWidth)...)
	content = append(content, "", m.mimirHeaderLineLocked(local))
	content = append(content, fillWidth(conversation, contentWidth)...)
	content = append(content, input...)
	content = append(content, commands...)
	if len(content) < height {
		hint := "tab sessions · / commands"
		if strings.TrimSpace(m.status) != "" {
			hint = cleanText(m.status)
		}
		content = append(content, bentotui.PadRight(bentotui.Style{Color: local.Theme.Muted, Enabled: local.Color}.Render(hint), contentWidth))
	}
	return centerBlock(content, width, height)
}

func (m *Model) mimirHeaderLineLocked(render appframe.Renderer) string {
	heading := render.Heading("Ask Mimir")
	status := cleanText(m.agentStatus)
	if status == "" {
		status = "ready"
	}
	muted := bentotui.Style{Color: render.Theme.Muted, Enabled: render.Color}.Render(status)
	leftWidth := max(1, render.Width-bentotui.VisibleWidth(muted)-2)
	return bentotui.PadRight(bentotui.Truncate(heading, leftWidth), leftWidth) + "  " + muted
}

func wordmarkLines(render appframe.Renderer, screen bentotui.Screen) []string {
	mark := mimirWordmark
	if screen.Height < 28 || screen.Width < 48 {
		mark = []string{"MIMIR"}
	}
	style := bentotui.Style{Color: render.Theme.Accent, Bold: true, Enabled: render.Color}
	lines := make([]string, len(mark))
	for index, line := range mark {
		lines[index] = bentotui.Truncate(style.Render(line), render.Width)
	}
	return lines
}

func (m *Model) homeSessionLinesLocked(render appframe.Renderer, rows int) []string {
	heading := "Sessions"
	if m.focus == focusFilter || m.query != "" {
		heading = "Filter: " + cleanText(m.query)
	}
	inner := max(1, render.Width-2)
	border := bentotui.Style{Color: render.Theme.Border, Background: render.Theme.Panel, HasBackground: true, Enabled: render.Color}
	base := bentotui.Style{Color: render.Theme.Text, Background: render.Theme.Panel, HasBackground: true, Enabled: render.Color}
	muted := bentotui.Style{Color: render.Theme.Muted, Background: render.Theme.Panel, HasBackground: true, Enabled: render.Color}
	accent := bentotui.Style{Color: render.Theme.Accent, Background: render.Theme.Panel, HasBackground: true, Bold: true, Enabled: render.Color}
	selectedStyle := bentotui.Style{Color: render.Theme.SelectionText, Background: render.Theme.Selection, HasBackground: true, Bold: true, Enabled: render.Color}
	panelRow := func(content string, style bentotui.Style) string {
		return border.Render("│") + style.Render(bentotui.PadRight(bentotui.Truncate(content, inner), inner)) + border.Render("│")
	}
	count := fmt.Sprintf("%d", len(m.visible))
	headingWidth := max(1, inner-bentotui.VisibleWidth(count)-2)
	header := bentotui.PadRight(bentotui.Truncate("  "+heading, headingWidth), headingWidth) + "  " + count
	lines := []string{
		border.Render("╭" + strings.Repeat("─", inner) + "╮"),
		panelRow(header, accent),
		panelRow("  recent sessions · enter opens evidence", muted),
	}
	if len(m.visible) == 0 {
		message := "No sessions match"
		if m.loading {
			message = "Loading sessions"
		}
		lines = append(lines, panelRow("  "+message, base))
		return append(lines, border.Render("╰"+strings.Repeat("─", inner)+"╯"))
	}
	if m.selected < m.offset {
		m.offset = m.selected
	}
	if m.selected >= m.offset+rows {
		m.offset = m.selected - rows + 1
	}
	end := min(len(m.visible), m.offset+rows)
	for position := m.offset; position < end; position++ {
		item := m.items[m.visible[position]]
		marker := "  "
		rowStyle := base
		rowBackground := render.Theme.Panel
		if position == m.selected {
			marker = "> "
			rowStyle = selectedStyle
			rowBackground = render.Theme.Selection
		}
		outcomeName := strings.ToLower(displayValue(cleanText(item.Outcome), "unresolved"))
		outcome := bentotui.PadRight("["+strings.ToUpper(outcomeName)+"]", 13)
		outcomeStyle := bentotui.Style{Color: outcomeBadgeColor(render.Theme, outcomeName), Background: rowBackground, HasBackground: true, Bold: true, Enabled: render.Color}
		prefixWidth := bentotui.VisibleWidth(marker) + bentotui.VisibleWidth(outcome) + 1
		tailWidth := max(1, inner-bentotui.VisibleWidth(marker)-bentotui.VisibleWidth(outcome))
		stat := compactCount(item.Tokens)
		if stat == "0" {
			stat = cleanText(item.Capture)
		}
		statWidth := min(12, max(0, bentotui.VisibleWidth(stat)))
		available := max(1, inner-prefixWidth-statWidth-2)
		tail := " " + bentotui.PadRight(bentotui.Truncate(displayValue(item.Title, "Untitled session"), available), available)
		if stat != "" {
			tail += "  " + bentotui.PadRight(bentotui.Truncate(stat, statWidth), statWidth)
		}
		tail = bentotui.PadRight(bentotui.Truncate(tail, tailWidth), tailWidth)
		rendered := rowStyle.Render(marker) + outcomeStyle.Render(outcome) + rowStyle.Render(tail)
		lines = append(lines, border.Render("│")+rendered+border.Render("│"))
		if position == m.selected && m.expanded {
			for _, detailLine := range m.inlineDetailLinesLocked(render, 6) {
				lines = append(lines, panelRow("    "+detailLine, muted))
			}
		}
	}
	for len(lines) < rows+3 {
		lines = append(lines, panelRow("", base))
	}
	return append(lines, border.Render("╰"+strings.Repeat("─", inner)+"╯"))
}

func (m *Model) inlineDetailLinesLocked(render appframe.Renderer, limit int) []string {
	item, ok := m.currentLocked()
	if !ok {
		return nil
	}
	lines := []string{
		strings.Join(nonempty("Evidence", displayValue(item.Repo, ""), displayValue(item.Harness, ""), displayValue(item.Model, "")), " · "),
	}
	if detail, loaded := m.details[item.ID]; loaded {
		lines = append(lines, fmt.Sprintf("%d exchanges · %s · %d saved · %d failed", len(detail.Exchanges), displayValue(detail.Session.State, "unknown"), detail.Capture.SavedExchanges, detail.Capture.FailedExchanges))
		if detail.Session.OutcomeReason != nil && strings.TrimSpace(*detail.Session.OutcomeReason) != "" {
			lines = append(lines, "Outcome: "+cleanText(*detail.Session.OutcomeReason))
		}
		if len(detail.Files) > 0 {
			lines = append(lines, "Files: "+cleanText(strings.Join(detail.Files, ", ")))
		}
		if len(detail.Errors) > 0 {
			lines = append(lines, "Errors: "+cleanText(strings.Join(detail.Errors, "; ")))
		}
		if len(detail.Exchanges) > 0 {
			exchange := detail.Exchanges[len(detail.Exchanges)-1]
			finish := ""
			if exchange.FinishReason != nil {
				finish = *exchange.FinishReason
			}
			lines = append(lines, strings.Join(nonempty("Latest", exchange.Model, fmt.Sprintf("%dms", exchange.LatencyMS), finish), " · "))
			if excerpt := strings.TrimSpace(exchange.ResponseExcerpt); excerpt != "" {
				lines = append(lines, "↳ "+cleanText(excerpt))
			}
		}
	} else {
		lines = append(lines, "Loading captured evidence...")
	}
	if len(lines) > limit {
		lines = lines[:limit]
	}
	for index := range lines {
		lines[index] = bentotui.Truncate(lines[index], max(1, render.Width-8))
	}
	return lines
}

func outcomeBadgeColor(theme bentotui.Theme, outcome string) bentotui.Color {
	switch strings.ToLower(strings.TrimSpace(outcome)) {
	case "landed":
		return theme.Success
	case "discarded":
		return theme.Error
	case "abandoned":
		return theme.Warning
	default:
		return theme.Muted
	}
}

func (m *Model) homeCommandLinesLocked(render appframe.Renderer, limit int) []string {
	if m.overlay != overlayCommands {
		return nil
	}
	matches := m.commandMatchesLocked()
	if len(matches) == 0 {
		return []string{bentotui.Style{Color: render.Theme.Muted, Enabled: render.Color}.Render("No matching commands")}
	}
	limit = min(limit, len(matches))
	nameWidth := 10
	lines := make([]string, 0, limit+1)
	for index := 0; index < limit; index++ {
		command := matches[index]
		marker := "  "
		style := bentotui.Style{Color: render.Theme.Muted, Enabled: render.Color}
		if index == min(m.overlayItem, len(matches)-1) {
			marker = "› "
			style = bentotui.Style{Color: render.Theme.Accent, Bold: true, Enabled: render.Color}
		}
		left := bentotui.PadRight(command.name, nameWidth)
		description := bentotui.Truncate(command.description, max(1, render.Width-nameWidth-3))
		lines = append(lines, bentotui.PadRight(style.Render(marker+left+" "+description), render.Width))
	}
	hint := bentotui.Style{Color: render.Theme.Muted, Enabled: render.Color}.Render("↑↓ choose · tab complete · enter run · esc close")
	return append(lines, bentotui.PadRight(bentotui.Truncate(hint, render.Width), render.Width))
}

func (m *Model) homeConversationLinesLocked(render appframe.Renderer, height int) []string {
	if height <= 0 {
		return nil
	}
	lines := make([]string, 0, height)
	for _, message := range m.messages {
		prefix := "◆ "
		if message.role == "you" {
			prefix = "> "
		}
		lines = append(lines, bentotui.WrapPreserve(prefix+cleanText(message.text), render.Width)...)
	}
	if m.streaming != "" {
		lines = append(lines, bentotui.WrapPreserve("◆ "+cleanText(m.streaming)+"_", render.Width)...)
	}
	if len(lines) > height {
		lines = lines[len(lines)-height:]
	}
	return lines
}

func (m *Model) homeInputLinesLocked(render appframe.Renderer) []string {
	borderColor := render.Theme.Border
	if m.focus == focusAgent {
		borderColor = render.Theme.Accent
	}
	border := bentotui.Style{Color: borderColor, Enabled: render.Color}
	muted := bentotui.Style{Color: render.Theme.Muted, Enabled: render.Color}
	inner := max(1, render.Width-3)
	inputWidth := max(1, inner-3)
	value := visibleTail(cleanText(m.input), inputWidth)
	if value == "" {
		value = muted.Render("Ask Mimir anything about your sessions.")
	}
	if m.focus == focusAgent {
		value += "_"
	}
	context := "No session selected"
	if current, ok := m.currentLocked(); ok {
		context = displayValue(current.Title, current.ID)
	}
	row := func(value string) string {
		return border.Render("┃") + bentotui.PadRight(bentotui.Truncate("  "+value, inner), inner)
	}
	return []string{
		row(""),
		row("> " + value),
		row(muted.Render(bentotui.Truncate(context, max(1, inner-2)))),
		row(""),
	}
}

func (m *Model) centeredWorkView(render appframe.Renderer, content []string, width, height int) []string {
	contentWidth := min(84, max(40, width-4))
	return centerBlock(fitLines(content, contentWidth, min(height, len(content))), width, height)
}

func (m *Model) drawOverlayLocked(render appframe.Renderer, body []string, width, height int) []string {
	panelWidth := min(48, max(30, width-8))
	local := render.WithWidth(panelWidth)
	var title string
	var items []string
	if m.overlay == overlayThemes {
		title = "Themes"
		for index, theme := range bentotui.Themes() {
			marker := "  "
			if index == m.overlayItem {
				marker = "› "
			}
			items = append(items, marker+theme.Name)
		}
		items = append(items, "", "↑↓ choose · enter apply · esc close")
	} else if m.overlay == overlayModels {
		title = "Choose model"
		if len(m.models) == 0 {
			items = []string{"Loading available models...", "", "esc close"}
		} else {
			start := max(0, min(m.overlayItem-4, len(m.models)-8))
			end := min(len(m.models), start+8)
			for index := start; index < end; index++ {
				model := m.models[index]
				marker := "  "
				if index == m.overlayItem {
					marker = "› "
				}
				items = append(items, marker+model.Provider+"/"+model.ID)
			}
			items = append(items, "", "↑↓ choose · enter switch · esc close")
		}
	} else {
		title = "Mimir help"
		items = []string{
			"Home",
			"  tab switch focus · ctrl+c quit",
			"Sessions",
			"  ↑↓ browse · / filter · enter detail",
			"  o outcome · r reload · z fullscreen agent",
			"Ask Mimir",
			"  enter send · ↑↓ scroll · / commands",
			"  esc sessions",
			"",
			"enter or esc close",
		}
	}
	panel := boxedPanel(local, title, items)
	start := max(0, (height-len(panel))/2)
	left := max(0, (width-panelWidth)/2)
	result := append([]string(nil), body...)
	for index, line := range panel {
		if start+index >= len(result) {
			break
		}
		result[start+index] = strings.Repeat(" ", left) + line
	}
	return result
}

func boxedPanel(render appframe.Renderer, title string, items []string) []string {
	border := bentotui.Style{Color: render.Theme.Border, Enabled: render.Color}
	accent := bentotui.Style{Color: render.Theme.Accent, Bold: true, Enabled: render.Color}
	inner := max(1, render.Width-2)
	lines := []string{border.Render("┌" + strings.Repeat("─", inner) + "┐")}
	rows := append([]string{accent.Render(title), ""}, items...)
	for _, item := range rows {
		lines = append(lines, border.Render("│")+bentotui.PadRight(" "+bentotui.Truncate(item, max(1, inner-2)), inner)+border.Render("│"))
	}
	return append(lines, border.Render("└"+strings.Repeat("─", inner)+"┘"))
}

func centerBlock(lines []string, width, height int) []string {
	result := make([]string, 0, height)
	top := max(0, (height-len(lines))/2)
	for len(result) < top {
		result = append(result, strings.Repeat(" ", width))
	}
	for _, line := range lines {
		if len(result) == height {
			break
		}
		line = bentotui.Truncate(line, width)
		left := max(0, (width-bentotui.VisibleWidth(line))/2)
		result = append(result, bentotui.PadRight(strings.Repeat(" ", left)+line, width))
	}
	for len(result) < height {
		result = append(result, strings.Repeat(" ", width))
	}
	return result
}

func centerLine(line string, width int) string {
	line = bentotui.Truncate(line, width)
	left := max(0, (width-bentotui.VisibleWidth(line))/2)
	return bentotui.PadRight(strings.Repeat(" ", left)+line, width)
}

func fillWidth(lines []string, width int) []string {
	result := make([]string, len(lines))
	for index, line := range lines {
		result[index] = bentotui.PadRight(bentotui.Truncate(line, width), width)
	}
	return result
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
	status := cleanText(m.agentStatus)
	if status == "" {
		status = "Agent ready"
	}
	muted := bentotui.Style{Color: render.Theme.Muted, Enabled: render.Color}
	lines := []string{render.Heading("Ask Mimir"), muted.Render(status), ""}
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
	lines = append(lines, "")
	lines = append(lines, m.homeCommandLinesLocked(render, 8)...)
	lines = append(lines, m.homeInputLinesLocked(render)...)
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
	start := min(max(0, m.detailOffset), maxOffset)
	return lines[start:min(len(lines), start+height)]
}

func (m *Model) footerLocked(render appframe.Renderer) string {
	if m.focus == focusOutcome {
		return appframe.Footer(render.Context(), []appframe.Binding{{Key: "l", Label: "Landed"}, {Key: "d", Label: "Discarded"}}, []appframe.Binding{{Key: "a", Label: "Abandoned"}, {Key: "u", Label: "Unresolved"}})
	}
	if m.focus == focusFilter {
		return appframe.FooterStatus(render.Context(), []appframe.Binding{{Key: "Type", Label: "Filter"}}, cleanText(m.query), []appframe.Binding{{Key: "Enter", Label: "Apply"}, {Key: "Esc", Label: "Clear"}})
	}
	status := m.footerStatusLocked()
	if m.focus == focusAgent || m.screen == screenAgent {
		if m.screen == screenAgent {
			escapeLabel := "Sessions"
			if m.busy {
				escapeLabel = "Stop"
			}
			return appframe.FooterStatus(render.Context(), []appframe.Binding{{Key: "Enter", Label: "Send"}, {Key: "Esc", Label: escapeLabel}}, status, []appframe.Binding{{Key: "/", Label: "Commands"}, {Key: "^C", Label: "Quit"}})
		}
		return appframe.FooterStatus(render.Context(), []appframe.Binding{{Key: "Enter", Label: "Send"}, {Key: "Tab", Label: "Sessions"}}, status, []appframe.Binding{{Key: "/", Label: "Commands"}, {Key: "^C", Label: "Quit"}})
	}
	detailLabel := "Details"
	if m.expanded {
		detailLabel = "Collapse"
	}
	return appframe.FooterStatus(render.Context(), []appframe.Binding{{Key: "↑↓", Label: "Browse"}, {Key: "↵", Label: detailLabel}}, status, []appframe.Binding{{Key: "/", Label: "Filter"}, {Key: "Tab", Label: "Ask"}})
}

func (m *Model) footerStatusLocked() string {
	parts := make([]string, 0, 4)
	if len(m.visible) > 0 {
		parts = append(parts, fmt.Sprintf("%d/%d", m.selected+1, len(m.visible)))
		if current, ok := m.currentLocked(); ok && strings.TrimSpace(current.Outcome) != "" {
			parts = append(parts, strings.ToUpper(current.Outcome))
		}
	}
	if m.focus == focusAgent || m.screen == screenAgent {
		parts = append(parts, cleanText(m.agentStatus))
		if m.currentModel != "" {
			parts = append(parts, m.currentModel)
		}
	} else if strings.TrimSpace(m.status) != "" {
		parts = append(parts, cleanText(m.status))
	}
	return strings.Join(parts, " · ")
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
