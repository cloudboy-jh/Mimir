package ui

import (
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/cloudboy-jh/mimir/internal/ui/bentotui"
)

type Renderer struct {
	Out   io.Writer
	Color bool
	Width int
	Theme bentotui.Theme
}

func New(out io.Writer) Renderer {
	return Renderer{Out: out, Color: ColorEnabled(out), Width: outputWidth(out), Theme: bentotui.Mimir}
}

func (r Renderer) Context() bentotui.Context {
	return bentotui.Context{Width: r.Width, Color: r.Color, Theme: r.Theme}
}

func (r Renderer) WithWidth(width int) Renderer {
	r.Width = max(20, width)
	return r
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
	return bentotui.Truncate(bentotui.Style{Color: r.Theme.Accent, Bold: true, Enabled: r.Color}.Render("◆ "+title), r.Width)
}

func (r Renderer) Card(title string, fields ...bentotui.Field) string {
	width := min(r.Width, 76)
	return bentotui.CardContext(bentotui.Context{Width: width, Color: r.Color, Theme: r.Theme}, title, fields)
}

func (r Renderer) Prompt(label string) string {
	label = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(label), ":"))
	name := bentotui.Style{Color: r.Theme.Text, Bold: true, Enabled: r.Color}.Render(label)
	marker := bentotui.Style{Color: r.Theme.Accent, Bold: true, Enabled: r.Color}.Render("›")
	return "  " + name + "\n  " + marker + " "
}

func (r Renderer) Rows(rows []bentotui.Row) string {
	return bentotui.RenderRowsContext(r.Context(), rows)
}

func CleanDetail(value string) string { return strings.TrimSpace(value) }

func outputWidth(out io.Writer) int {
	if value := strings.TrimSpace(os.Getenv("COLUMNS")); value != "" {
		if width, err := strconv.Atoi(value); err == nil && width >= 20 {
			return width
		}
	}
	if file, ok := out.(*os.File); ok {
		if width := terminalWidth(file); width >= 20 {
			return width
		}
	}
	return 80
}
