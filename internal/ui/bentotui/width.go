package bentotui

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

type terminalUnit struct {
	raw   string
	width int
	space bool
}

// VisibleWidth reports the number of terminal cells occupied by the widest
// line in s. ANSI CSI and OSC sequences do not occupy cells.
func VisibleWidth(s string) int {
	maxWidth, width := 0, 0
	for _, unit := range terminalUnits(s) {
		if unit.raw == "\n" {
			if width > maxWidth {
				maxWidth = width
			}
			width = 0
			continue
		}
		width += unit.width
	}
	if width > maxWidth {
		maxWidth = width
	}
	return maxWidth
}

// LineWidths returns the visible width of every line, including empty lines.
func LineWidths(s string) []int {
	lines := strings.Split(s, "\n")
	widths := make([]int, len(lines))
	for i, line := range lines {
		widths[i] = VisibleWidth(line)
	}
	return widths
}

func MaxLineWidth(s string) int { return VisibleWidth(s) }

func FitsWidth(s string, width int) bool {
	if width < 0 {
		return false
	}
	for _, lineWidth := range LineWidths(s) {
		if lineWidth > width {
			return false
		}
	}
	return true
}

func PadLeft(value string, width int) string {
	return strings.Repeat(" ", max(0, width-VisibleWidth(value))) + value
}

func PadRight(value string, width int) string {
	return value + strings.Repeat(" ", max(0, width-VisibleWidth(value)))
}

// Truncate shortens s to width cells and uses an ellipsis when space permits.
func Truncate(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if VisibleWidth(s) <= width && !strings.Contains(s, "\n") {
		return s
	}
	target := width - 1
	if target < 0 {
		target = 0
	}
	var out strings.Builder
	used := 0
	styled := false
	hyperlinkOpen := false
	for _, unit := range terminalUnits(s) {
		if unit.raw == "\n" {
			break
		}
		if unit.width == 0 {
			out.WriteString(unit.raw)
			styled = styled || strings.HasPrefix(unit.raw, "\x1b[")
			if open, hyperlink := osc8State(unit.raw); hyperlink {
				hyperlinkOpen = open
			}
			continue
		}
		if used+unit.width > target {
			break
		}
		out.WriteString(unit.raw)
		used += unit.width
	}
	out.WriteRune('…')
	if styled {
		out.WriteString("\x1b[0m")
	}
	if hyperlinkOpen {
		out.WriteString("\x1b]8;;\x1b\\")
	}
	return out.String()
}

func osc8State(sequence string) (open, hyperlink bool) {
	if !strings.HasPrefix(sequence, "\x1b]8;") {
		return false, false
	}
	rest := strings.TrimPrefix(sequence, "\x1b]8;")
	rest = strings.TrimSuffix(strings.TrimSuffix(rest, "\x1b\\"), "\a")
	separator := strings.IndexByte(rest, ';')
	if separator < 0 {
		return false, true
	}
	return rest[separator+1:] != "", true
}

// Wrap folds text at whitespace where possible and hard-wraps long words.
// Explicit newlines and ANSI sequences are preserved.
func Wrap(s string, width int) []string {
	if width <= 0 {
		return strings.Split(s, "\n")
	}
	units := terminalUnits(s)
	lines := make([]string, 0, 1)
	for len(units) > 0 {
		if units[0].raw == "\n" {
			lines = append(lines, "")
			units = units[1:]
			if len(units) == 0 {
				lines = append(lines, "")
			}
			continue
		}
		used, end, lastSpace := 0, 0, -1
		for end < len(units) && units[end].raw != "\n" {
			if units[end].width > 0 && used+units[end].width > width {
				break
			}
			used += units[end].width
			if units[end].space {
				lastSpace = end
			}
			end++
		}
		if end < len(units) && units[end].raw != "\n" && lastSpace >= 0 {
			end = lastSpace
		}
		if end == 0 { // A wide rune cannot fit in a one-cell region.
			lines = append(lines, Truncate(joinUnits(units[:1]), width))
			units = units[1:]
			continue
		}
		lines = append(lines, joinUnits(units[:end]))
		units = units[end:]
		for len(units) > 0 && units[0].space {
			units = units[1:]
		}
		if len(units) > 0 && units[0].raw == "\n" {
			units = units[1:]
			if len(units) == 0 {
				lines = append(lines, "")
			}
		}
	}
	if len(lines) == 0 {
		return []string{""}
	}
	return lines
}

// WrapPreserve hard-wraps text without removing or normalizing whitespace.
// It is intended for source code and other evidence where every character is
// meaningful.
func WrapPreserve(s string, width int) []string {
	if width <= 0 {
		return strings.Split(s, "\n")
	}
	units := terminalUnits(s)
	lines := make([]string, 0, 1)
	var line strings.Builder
	used := 0
	flush := func() {
		lines = append(lines, line.String())
		line.Reset()
		used = 0
	}
	for _, unit := range units {
		if unit.raw == "\n" {
			flush()
			continue
		}
		if unit.width > 0 && used > 0 && used+unit.width > width {
			flush()
		}
		if unit.width > width {
			line.WriteString(Truncate(unit.raw, width))
			flush()
			continue
		}
		line.WriteString(unit.raw)
		used += unit.width
	}
	if line.Len() > 0 || len(lines) == 0 {
		flush()
	}
	return lines
}

func joinUnits(units []terminalUnit) string {
	var out strings.Builder
	for _, unit := range units {
		out.WriteString(unit.raw)
	}
	return strings.TrimRight(out.String(), " \t")
}

func terminalUnits(s string) []terminalUnit {
	units := make([]terminalUnit, 0, len(s))
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			end := ansiEnd(s, i)
			units = append(units, terminalUnit{raw: s[i:end]})
			i = end
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		raw := s[i : i+size]
		i += size
		if r == '\r' {
			continue
		}
		if r == '\n' {
			units = append(units, terminalUnit{raw: "\n"})
			continue
		}
		w := runeCellWidth(r)
		if w == 0 && len(units) > 0 && units[len(units)-1].raw != "\n" {
			units[len(units)-1].raw += raw
			continue
		}
		units = append(units, terminalUnit{raw: raw, width: w, space: unicode.IsSpace(r)})
	}
	return units
}

func ansiEnd(s string, start int) int {
	if start+1 >= len(s) {
		return start + 1
	}
	switch s[start+1] {
	case '[': // Control Sequence Introducer.
		for i := start + 2; i < len(s); i++ {
			if s[i] >= 0x40 && s[i] <= 0x7e {
				return i + 1
			}
		}
	case ']': // Operating System Command, terminated by BEL or ST.
		for i := start + 2; i < len(s); i++ {
			if s[i] == 0x07 {
				return i + 1
			}
			if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '\\' {
				return i + 2
			}
		}
	}
	return min(len(s), start+2)
}

func runeCellWidth(r rune) int {
	if r == '\t' {
		// A raw tab advances by at most eight cells on conventional terminals.
		// Using the upper bound keeps width-constrained output conservative.
		return 8
	}
	if r == 0 || r < 0x20 || (r >= 0x7f && r < 0xa0) || r == 0x200d ||
		unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) ||
		(r >= 0xfe00 && r <= 0xfe0f) || (r >= 0x1f3fb && r <= 0x1f3ff) {
		return 0
	}
	// Conservative wcwidth-style ranges covering CJK, fullwidth forms, and
	// the emoji blocks commonly emitted by CLIs.
	if r >= 0x1100 && (r <= 0x115f || r == 0x2329 || r == 0x232a ||
		(r >= 0x2e80 && r <= 0xa4cf && r != 0x303f) ||
		(r >= 0xac00 && r <= 0xd7a3) || (r >= 0xf900 && r <= 0xfaff) ||
		(r >= 0xfe10 && r <= 0xfe19) || (r >= 0xfe30 && r <= 0xfe6f) ||
		(r >= 0xff00 && r <= 0xff60) || (r >= 0xffe0 && r <= 0xffe6) ||
		(r >= 0x1f000 && r <= 0x1faff) || (r >= 0x20000 && r <= 0x3fffd)) {
		return 2
	}
	return 1
}
