package ui

import (
	"strings"

	"github.com/cloudboy-jh/mimir/internal/ui/bentotui"
)

type StatusItem struct {
	Title, Detail, Stat string
	Tone                bentotui.Tone
}

type SessionItem struct {
	Title, Outcome, Capture, Metadata, ID, Excerpt string
}

type CommandItem struct {
	Usage, Description string
}

func (r Renderer) Section(title, body string) string {
	return bentotui.Section(r.Context(), title, body)
}

func (r Renderer) KeyValues(title string, fields ...bentotui.Field) string {
	rows := make([]string, 0, len(fields))
	for _, field := range fields {
		value := strings.TrimSpace(field.Value)
		if value == "" {
			value = "unavailable"
		}
		rows = append(rows, bentotui.KeyValue(r.Context(), field.Label+":", value))
	}
	return bentotui.Join("\n", r.Heading(title), strings.Join(rows, "\n"))
}

func (r Renderer) Callout(tone bentotui.Tone, title, body string) string {
	return bentotui.Callout(r.Context(), tone, title, body)
}

func (r Renderer) EmptyState(title, body string) string {
	return bentotui.Inset(r.Context(), bentotui.EmptyState(r.Context(), title, body), 2, 0)
}

func (r Renderer) ActionHint(command, description string) string {
	return bentotui.ActionHint(r.Context(), command, description)
}

func (r Renderer) StatusItems(items []StatusItem) string {
	rows := make([]bentotui.Row, 0, len(items))
	for _, item := range items {
		rows = append(rows, bentotui.Row{Primary: item.Title, Secondary: item.Detail, RightStat: item.Stat, Tone: item.Tone})
	}
	return r.Rows(rows)
}

func (r Renderer) Commands(items []CommandItem) string {
	commandWidth := min(42, max(24, r.Width/2))
	muted := bentotui.Style{Color: r.Theme.Muted, Enabled: r.Color}
	accent := bentotui.Style{Color: r.Theme.Accent, Enabled: r.Color}
	var lines []string
	for _, item := range items {
		if r.Width < 68 || bentotui.VisibleWidth(item.Usage) > commandWidth {
			usageLines := bentotui.Wrap(item.Usage, max(1, r.Width-2))
			for _, line := range usageLines {
				lines = append(lines, "  "+accent.Render(line))
			}
			for _, line := range bentotui.Wrap(item.Description, max(1, r.Width-6)) {
				lines = append(lines, "      "+muted.Render(line))
			}
			continue
		}
		descriptionWidth := max(1, r.Width-commandWidth-4)
		description := bentotui.Wrap(item.Description, descriptionWidth)
		lines = append(lines, "  "+accent.Render(bentotui.PadRight(item.Usage, commandWidth))+"  "+muted.Render(description[0]))
		for _, line := range description[1:] {
			lines = append(lines, strings.Repeat(" ", commandWidth+4)+muted.Render(line))
		}
	}
	return strings.Join(lines, "\n")
}

func (r Renderer) OutcomeBadge(outcome string) string {
	variant := bentotui.VariantNeutral
	switch strings.ToLower(strings.TrimSpace(outcome)) {
	case "landed":
		variant = bentotui.VariantSuccess
	case "discarded":
		variant = bentotui.VariantDanger
	case "abandoned":
		variant = bentotui.VariantWarning
	}
	if strings.TrimSpace(outcome) == "" {
		outcome = "unresolved"
	}
	return bentotui.Badge(r.Theme, r.Color, strings.ToUpper(outcome), variant)
}

func (r Renderer) CaptureBadge(status string) string {
	variant := bentotui.VariantNeutral
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "saved":
		variant = bentotui.VariantSuccess
	case "pending":
		variant = bentotui.VariantInfo
	case "partial":
		variant = bentotui.VariantWarning
	case "failed":
		variant = bentotui.VariantDanger
	}
	if strings.TrimSpace(status) == "" {
		status = "unknown"
	}
	return bentotui.Badge(r.Theme, r.Color, strings.ToUpper(status), variant)
}

func (r Renderer) Session(item SessionItem) string {
	prefix := "  " + r.OutcomeBadge(item.Outcome) + " "
	stat := strings.TrimSpace(item.Capture)
	contentWidth := max(1, r.Width-bentotui.VisibleWidth(prefix))
	stat = bentotui.Truncate(stat, max(1, min(contentWidth/3, 24)))
	titleWidth := contentWidth
	if stat != "" {
		titleWidth = max(1, contentWidth-bentotui.VisibleWidth(stat)-2)
	}
	title := strings.TrimSpace(item.Title)
	if title == "" {
		title = "Untitled session"
	}
	titleLines := bentotui.Wrap(title, titleWidth)
	first := prefix + bentotui.PadRight(titleLines[0], titleWidth)
	if stat != "" {
		first += "  " + bentotui.Style{Color: r.Theme.Muted, Enabled: r.Color}.Render(stat)
	}
	lines := []string{first}
	indent := strings.Repeat(" ", bentotui.VisibleWidth(prefix))
	lines = append(lines, prefixedLines(indent, titleLines[1:])...)
	muted := bentotui.Style{Color: r.Theme.Muted, Enabled: r.Color}
	for _, value := range []string{item.Metadata, item.ID, item.Excerpt} {
		if strings.TrimSpace(value) == "" {
			continue
		}
		for _, line := range bentotui.Wrap(strings.TrimSpace(value), max(1, r.Width-6)) {
			lines = append(lines, "      "+muted.Render(line))
		}
	}
	return strings.Join(lines, "\n")
}

func prefixedLines(prefix string, lines []string) []string {
	result := make([]string, len(lines))
	for i, line := range lines {
		result[i] = prefix + line
	}
	return result
}

func ToneForStatus(status string) bentotui.Tone {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "ok", "ready", "saved", "installed", "updated", "current", "connected":
		return bentotui.ToneSuccess
	case "warning", "warn", "skipped", "preserved", "scheduled", "available", "pending":
		return bentotui.ToneWarn
	case "failed", "error", "conflict", "modified":
		return bentotui.ToneDanger
	default:
		return bentotui.ToneNeutral
	}
}
