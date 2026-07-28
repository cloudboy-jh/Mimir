// Package bentotui is Mimir's dependency-free terminal rendering system. It
// adapts BentoTUI's visual contracts without requiring Bubble Tea or Lipgloss.
package bentotui

type Color struct{ R, G, B uint8 }

type Theme struct {
	Text, Muted, Accent           Color
	Success, Warning, Error, Info Color
	Border                        Color
}

// Context carries the terminal capabilities used by width-aware renderers.
// Width is the maximum visible width; values <= 0 mean unconstrained.
type Context struct {
	Width int
	Color bool
	Theme Theme
}

func (c Context) normalized() Context {
	if c.Theme == (Theme{}) {
		c.Theme = Mimir
	}
	return c
}

var Mimir = Theme{
	Text: Color{242, 242, 239}, Muted: Color{148, 148, 141}, Accent: Color{126, 192, 164},
	Success: Color{74, 161, 113}, Warning: Color{202, 146, 46}, Error: Color{205, 79, 79},
	Info: Color{85, 145, 180}, Border: Color{106, 106, 100},
}
