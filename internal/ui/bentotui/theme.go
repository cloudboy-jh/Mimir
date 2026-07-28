// Package bentotui contains stdlib ports of the small BentoTUI rendering
// contracts Mimir uses. It intentionally contains no Bubble Tea or Lipgloss.
package bentotui

type Color struct{ R, G, B uint8 }

type Theme struct {
	Text, Muted, Accent           Color
	Success, Warning, Error, Info Color
	Border                        Color
}

var Mimir = Theme{
	Text: Color{242, 242, 239}, Muted: Color{148, 148, 141}, Accent: Color{126, 192, 164},
	Success: Color{74, 161, 113}, Warning: Color{202, 146, 46}, Error: Color{205, 79, 79},
	Info: Color{85, 145, 180}, Border: Color{106, 106, 100},
}
