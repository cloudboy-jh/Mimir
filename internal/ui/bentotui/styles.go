package bentotui

import (
	"fmt"
	"strings"
)

type Style struct {
	Color   Color
	Bold    bool
	Dim     bool
	Enabled bool
}

func (s Style) Render(text string) string {
	if !s.Enabled {
		return text
	}
	weight := ""
	if s.Bold {
		weight = "1;"
	} else if s.Dim {
		weight = "2;"
	}
	return fmt.Sprintf("\x1b[%s38;2;%d;%d;%dm%s\x1b[0m", weight, s.Color.R, s.Color.G, s.Color.B, text)
}

func PadRight(value string, width int) string {
	runes := []rune(value)
	if len(runes) >= width {
		return value
	}
	return value + strings.Repeat(" ", width-len(runes))
}
