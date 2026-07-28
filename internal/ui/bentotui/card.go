package bentotui

import "strings"

type Field struct{ Label, Value string }

func Card(theme Theme, enabled bool, title string, fields []Field) string {
	width := len([]rune(title)) + 4
	for _, field := range fields {
		line := len([]rune(field.Label)) + len([]rune(field.Value)) + 7
		if line > width {
			width = line
		}
	}
	if width < 34 {
		width = 34
	}
	if width > 76 {
		width = 76
	}
	border := Style{Color: theme.Border, Enabled: enabled}
	heading := Style{Color: theme.Text, Bold: true, Enabled: enabled}
	muted := Style{Color: theme.Muted, Enabled: enabled}
	var out strings.Builder
	out.WriteString(border.Render("┌─ ") + heading.Render(title) + border.Render(" "+strings.Repeat("─", max(0, width-len([]rune(title))-4))))
	for _, field := range fields {
		out.WriteString("\n" + border.Render("│") + " " + muted.Render(PadRight(field.Label, 11)) + " " + field.Value)
	}
	out.WriteString("\n" + border.Render("└"+strings.Repeat("─", width-1)))
	return out.String()
}
