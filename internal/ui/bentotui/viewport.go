package bentotui

import "strings"

type ViewportBoxOptions struct {
	Title, Right, Footer string
	Lines                []string
	Offset, Height       int
}

// ViewportBox renders one bounded box with a fixed header and footer. Only the
// body scrolls; callers own selection and follow behavior.
func ViewportBox(ctx Context, options ViewportBoxOptions) string {
	ctx = ctx.normalized()
	width := ctx.Width
	if width <= 0 {
		width = 80
	}
	width = min(width, 110)
	width = max(width, 20)
	height := options.Height
	if height <= 0 {
		height = 16
	}
	height = max(6, height)
	bodyHeight := max(2, height-4)
	inner := width - 2

	lines := make([]string, 0, len(options.Lines))
	for _, source := range options.Lines {
		lines = append(lines, WrapPreserve(source, inner-2)...)
	}
	maxOffset := max(0, len(lines)-bodyHeight)
	offset := min(max(0, options.Offset), maxOffset)
	end := min(len(lines), offset+bodyHeight)
	visible := lines[offset:end]
	for len(visible) < bodyHeight {
		visible = append(visible, "")
	}

	border := Style{Color: ctx.Theme.Border, Enabled: ctx.Color}
	muted := Style{Color: ctx.Theme.Muted, Enabled: ctx.Color}
	top := borderedLabelLine(border, width, "┌─", options.Title, options.Right, "┐")
	result := []string{top}
	for _, line := range visible {
		content := " " + Truncate(line, inner-2) + " "
		result = append(result, border.Render("│")+PadRight(content, inner)+border.Render("│"))
	}
	position := ""
	if len(lines) > bodyHeight {
		position = formatPosition(offset, end, len(lines))
	}
	result = append(result, borderedLabelLine(border, width, "├", "", position, "┤"))
	footer := muted.Render(Truncate(options.Footer, inner-2))
	result = append(result, border.Render("│")+PadRight(" "+footer+" ", inner)+border.Render("│"))
	result = append(result, border.Render("└"+strings.Repeat("─", inner)+"┘"))
	return strings.Join(result, "\n")
}

func borderedLabelLine(border Style, width int, left, title, right, end string) string {
	inner := width - VisibleWidth(left) - VisibleWidth(end)
	title = strings.TrimSpace(title)
	right = strings.TrimSpace(right)
	leftLabel := ""
	if title != "" {
		leftLabel = " " + title + " "
	}
	rightLabel := ""
	if right != "" {
		rightLabel = " " + right + " "
	}
	available := max(0, inner-VisibleWidth(leftLabel)-VisibleWidth(rightLabel))
	if available == 0 && rightLabel != "" {
		rightLabel = ""
		available = max(0, inner-VisibleWidth(leftLabel))
	}
	leftLabel = Truncate(leftLabel, inner-VisibleWidth(rightLabel))
	return border.Render(left) + leftLabel + border.Render(strings.Repeat("─", available)) + rightLabel + border.Render(end)
}

func formatPosition(offset, end, total int) string {
	if total == 0 {
		return ""
	}
	return itoa(offset+1) + "–" + itoa(end) + "/" + itoa(total)
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	var digits [20]byte
	index := len(digits)
	for value > 0 {
		index--
		digits[index] = byte('0' + value%10)
		value /= 10
	}
	return string(digits[index:])
}
