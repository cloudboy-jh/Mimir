package bentotui

import "strings"

// Stack vertically combines non-empty blocks with a blank line between them.
func Stack(blocks ...string) string { return Join("\n\n", blocks...) }

// Join combines non-empty blocks with separator.
func Join(separator string, blocks ...string) string {
	kept := make([]string, 0, len(blocks))
	for _, block := range blocks {
		if block != "" {
			kept = append(kept, block)
		}
	}
	return strings.Join(kept, separator)
}

// Inset indents a block and reserves right-side space within ctx.Width.
func Inset(ctx Context, block string, left, right int) string {
	left, right = max(0, left), max(0, right)
	available := ctx.Width
	if available > 0 {
		available = max(1, available-left-right)
	}
	prefix := strings.Repeat(" ", left)
	var lines []string
	for _, source := range strings.Split(block, "\n") {
		wrapped := []string{source}
		if available > 0 {
			wrapped = Wrap(source, available)
		}
		for _, line := range wrapped {
			lines = append(lines, prefix+line)
		}
	}
	return strings.Join(lines, "\n")
}

func Section(ctx Context, title, body string) string {
	ctx = ctx.normalized()
	heading := Style{Color: ctx.Theme.Text, Bold: true, Enabled: ctx.Color}.Render(title)
	if ctx.Width > 0 {
		heading = Truncate(heading, ctx.Width)
	}
	return Join("\n", heading, wrapBlock(body, ctx.Width))
}

func Divider(ctx Context, label string) string {
	ctx = ctx.normalized()
	width := ctx.Width
	if width <= 0 {
		width = max(1, VisibleWidth(label)+4)
	}
	border := Style{Color: ctx.Theme.Border, Enabled: ctx.Color}
	if label == "" {
		return border.Render(strings.Repeat("─", width))
	}
	label = Truncate(label, max(1, width-4))
	return border.Render("──") + " " + label + " " + border.Render(strings.Repeat("─", max(0, width-VisibleWidth(label)-4)))
}

func KeyValue(ctx Context, key, value string) string {
	ctx = ctx.normalized()
	keyWidth := min(16, max(1, ctx.Width/3))
	if ctx.Width <= 0 {
		keyWidth = max(1, VisibleWidth(key))
	}
	prefixWidth := keyWidth + 2
	valueWidth := ctx.Width - prefixWidth
	if ctx.Width <= 0 {
		valueWidth = max(1, VisibleWidth(value))
	}
	keys, values := Wrap(key, keyWidth), Wrap(value, max(1, valueWidth))
	lines := make([]string, max(len(keys), len(values)))
	muted := Style{Color: ctx.Theme.Muted, Enabled: ctx.Color}
	for i := range lines {
		k, v := "", ""
		if i < len(keys) {
			k = keys[i]
		}
		if i < len(values) {
			v = values[i]
		}
		lines[i] = muted.Render(PadRight(k, keyWidth)) + "  " + v
	}
	return strings.Join(lines, "\n")
}

func Callout(ctx Context, tone Tone, title, body string) string {
	ctx = ctx.normalized()
	marker, variant := "·", VariantNeutral
	switch tone {
	case ToneInfo:
		marker, variant = "i", VariantInfo
	case ToneSuccess:
		marker, variant = "✓", VariantSuccess
	case ToneWarn:
		marker, variant = "!", VariantWarning
	case ToneDanger:
		marker, variant = "×", VariantDanger
	}
	prefix := Badge(ctx.Theme, ctx.Color, marker, variant) + " "
	available := ctx.Width - VisibleWidth(prefix)
	if ctx.Width <= 0 {
		available = max(1, VisibleWidth(title))
	}
	heading := Style{Color: ctx.Theme.Text, Bold: true, Enabled: ctx.Color}.Render(Truncate(title, max(1, available)))
	body = strings.TrimSpace(body)
	if body == "" {
		return prefix + heading
	}
	return Join("\n", prefix+heading, Inset(Context{Width: ctx.Width}, wrapBlock(body, max(1, ctx.Width-2)), 2, 0))
}

func EmptyState(ctx Context, title, body string) string {
	ctx = ctx.normalized()
	muted := Style{Color: ctx.Theme.Muted, Enabled: ctx.Color}
	return Section(ctx, title, muted.Render(wrapBlock(body, ctx.Width)))
}

func ActionHint(ctx Context, key, description string) string {
	ctx = ctx.normalized()
	prefix := Style{Color: ctx.Theme.Accent, Bold: true, Enabled: ctx.Color}.Render("["+key+"]") + " "
	available := ctx.Width - VisibleWidth(prefix)
	if ctx.Width <= 0 {
		available = max(1, VisibleWidth(description))
	}
	lines := Wrap(description, max(1, available))
	for i := range lines {
		if i == 0 {
			lines[i] = prefix + lines[i]
		} else {
			lines[i] = strings.Repeat(" ", VisibleWidth(prefix)) + lines[i]
		}
	}
	return strings.Join(lines, "\n")
}

// Table renders a compact table. Columns share the available width and cells
// wrap independently, so every output line remains within ctx.Width.
func Table(ctx Context, headers []string, rows [][]string) string {
	ctx = ctx.normalized()
	columns := len(headers)
	for _, row := range rows {
		columns = max(columns, len(row))
	}
	if columns == 0 {
		return ""
	}
	gap := 2
	width := ctx.Width
	if width <= 0 {
		width = 80
	}
	usable := max(columns, width-gap*(columns-1))
	columnWidths := make([]int, columns)
	for i := range columnWidths {
		columnWidths[i] = usable / columns
		if i < usable%columns {
			columnWidths[i]++
		}
	}
	var rendered []string
	if len(headers) > 0 {
		headerStyle := Style{Color: ctx.Theme.Text, Bold: true, Enabled: ctx.Color}
		rendered = append(rendered, renderTableRow(headers, columnWidths, gap, headerStyle))
		rendered = append(rendered, Style{Color: ctx.Theme.Border, Enabled: ctx.Color}.Render(strings.Repeat("─", min(width, usable+gap*(columns-1)))))
	}
	for _, row := range rows {
		rendered = append(rendered, renderTableRow(row, columnWidths, gap, Style{}))
	}
	return strings.Join(rendered, "\n")
}

func renderTableRow(cells []string, widths []int, gap int, style Style) string {
	wrapped := make([][]string, len(widths))
	height := 1
	for i := range widths {
		cell := ""
		if i < len(cells) {
			cell = cells[i]
		}
		wrapped[i] = Wrap(cell, widths[i])
		height = max(height, len(wrapped[i]))
	}
	lines := make([]string, height)
	for line := 0; line < height; line++ {
		parts := make([]string, len(widths))
		for column := range widths {
			cell := ""
			if line < len(wrapped[column]) {
				cell = wrapped[column][line]
			}
			if style.Bold || style.Enabled {
				cell = style.Render(cell)
			}
			parts[column] = PadRight(cell, widths[column])
		}
		lines[line] = strings.TrimRight(strings.Join(parts, strings.Repeat(" ", gap)), " ")
	}
	return strings.Join(lines, "\n")
}

func wrapBlock(block string, width int) string {
	if width <= 0 {
		return block
	}
	var lines []string
	for _, line := range strings.Split(block, "\n") {
		lines = append(lines, Wrap(line, width)...)
	}
	return strings.Join(lines, "\n")
}
