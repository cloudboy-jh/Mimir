package ui

import (
	"io"
	"os"
	"strings"

	"github.com/cloudboy-jh/mimir/internal/ui/bentotui"
)

type Renderer struct {
	Out   io.Writer
	Color bool
	Theme bentotui.Theme
}

func New(out io.Writer) Renderer {
	return Renderer{Out: out, Color: ColorEnabled(out), Theme: bentotui.Mimir}
}

func ColorEnabled(out io.Writer) bool {
	if _, disabled := os.LookupEnv("NO_COLOR"); disabled || os.Getenv("TERM") == "dumb" {
		return false
	}
	file, ok := out.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func (r Renderer) Heading(title string) string {
	return bentotui.Style{Color: r.Theme.Accent, Bold: true, Enabled: r.Color}.Render("◆ " + title)
}

func (r Renderer) Card(title string, fields ...bentotui.Field) string {
	return bentotui.Card(r.Theme, r.Color, title, fields)
}

func (r Renderer) Prompt(label string) string {
	label = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(label), ":"))
	name := bentotui.Style{Color: r.Theme.Text, Bold: true, Enabled: r.Color}.Render(label)
	marker := bentotui.Style{Color: r.Theme.Accent, Bold: true, Enabled: r.Color}.Render("›")
	return "  " + name + "\n  " + marker + " "
}

func (r Renderer) Rows(rows []bentotui.Row) string {
	return bentotui.RenderRows(r.Theme, r.Color, rows)
}

func CleanDetail(value string) string { return strings.TrimSpace(value) }
