// Package bentotui is Mimir's dependency-free terminal rendering system. It
// adapts BentoTUI's visual contracts without requiring Bubble Tea or Lipgloss.
package bentotui

type Color struct{ R, G, B uint8 }

type Theme struct {
	Text, Muted, Accent           Color
	Success, Warning, Error, Info Color
	Border, Background, Panel     Color
	Selection, SelectionText      Color
}

// Context carries the terminal capabilities used by width-aware renderers.
// Width is the maximum visible width; values <= 0 mean unconstrained.
type Context struct {
	Width int
	Color bool
	Theme Theme
}

type NamedTheme struct {
	Name  string
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
	Info: Color{85, 145, 180}, Border: Color{106, 106, 100}, Background: Color{17, 17, 16},
	Panel: Color{25, 25, 24}, Selection: Color{54, 54, 51}, SelectionText: Color{242, 242, 239},
}

var Paper = Theme{
	Text: Color{231, 229, 228}, Muted: Color{168, 162, 158}, Accent: Color{45, 212, 191},
	Success: Color{74, 222, 128}, Warning: Color{250, 204, 21}, Error: Color{248, 113, 113},
	Info: Color{125, 211, 252}, Border: Color{120, 113, 108}, Background: Color{28, 25, 23},
	Panel: Color{41, 37, 36}, Selection: Color{68, 64, 60}, SelectionText: Color{250, 250, 249},
}

func Themes() []NamedTheme {
	return []NamedTheme{{Name: "mimir", Theme: Mimir}, {Name: "paper", Theme: Paper}}
}
