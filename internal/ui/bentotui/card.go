package bentotui

import "strings"

type Field struct{ Label, Value string }

func Card(theme Theme, enabled bool, title string, fields []Field) string {
	width := VisibleWidth(title) + 4
	for _, field := range fields {
		line := VisibleWidth(field.Label) + VisibleWidth(field.Value) + 7
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
	return CardContext(Context{Width: width, Color: enabled, Theme: theme}, title, fields)
}

// CardContext renders a complete, fixed-width card and wraps every field.
func CardContext(ctx Context, title string, fields []Field) string {
	ctx = ctx.normalized()
	width := ctx.Width
	if width <= 0 {
		width = 76
	}
	if width < 4 {
		return strings.Join(Wrap(title, width), "\n")
	}
	border := Style{Color: ctx.Theme.Border, Enabled: ctx.Color}
	heading := Style{Color: ctx.Theme.Text, Bold: true, Enabled: ctx.Color}
	muted := Style{Color: ctx.Theme.Muted, Enabled: ctx.Color}
	inner := width - 4
	cardTitle := Truncate(title, width-5)
	var out strings.Builder
	out.WriteString(border.Render("┌─ ") + heading.Render(cardTitle) + border.Render(" "+strings.Repeat("─", max(0, width-VisibleWidth(cardTitle)-5))+"┐"))
	for _, field := range fields {
		labelWidth := min(11, max(1, inner/3))
		valueWidth := inner - labelWidth - 1
		labelLines, valueLines := Wrap(field.Label, labelWidth), Wrap(field.Value, valueWidth)
		lineCount := max(len(labelLines), len(valueLines))
		for i := 0; i < lineCount; i++ {
			label, value := "", ""
			if i < len(labelLines) {
				label = labelLines[i]
			}
			if i < len(valueLines) {
				value = valueLines[i]
			}
			out.WriteString("\n" + border.Render("│") + " " + muted.Render(PadRight(label, labelWidth)) + " " + PadRight(value, valueWidth) + " " + border.Render("│"))
		}
	}
	out.WriteString("\n" + border.Render("└"+strings.Repeat("─", width-2)+"┘"))
	return out.String()
}

// CardWithContext is the descriptive alias for CardContext.
func CardWithContext(ctx Context, title string, fields []Field) string {
	return CardContext(ctx, title, fields)
}
