// Package lineoutput renders stable, append-only command transcripts.
package lineoutput

import (
	"fmt"
	"io"
	"strings"

	"github.com/cloudboy-jh/mimir/internal/ui/appframe"
	"github.com/cloudboy-jh/mimir/internal/ui/bentotui"
)

// Renderer is deliberately stateless: every event appends exactly one line.
// ANSI styling is enabled only for terminals and never changes the plain text.
type Renderer struct {
	out   io.Writer
	color bool
	theme bentotui.Theme
}

func New(out io.Writer) Renderer {
	return Renderer{out: out, color: appframe.ColorEnabled(out), theme: bentotui.Mimir}
}

func (r Renderer) Phase(message string) error {
	return r.write(r.theme.Accent, true, "==> ", message)
}

func (r Renderer) Success(message string) error {
	return r.write(r.theme.Success, true, "OK  ", message)
}

func (r Renderer) Warning(message string) error {
	return r.write(r.theme.Warning, true, "WARN ", message)
}

func (r Renderer) Failure(message string) error {
	return r.write(r.theme.Error, true, "FAIL ", message)
}

func (r Renderer) Detail(message string) error {
	_, err := fmt.Fprintln(r.out, "    "+strings.TrimSpace(message))
	return err
}

func (r Renderer) Next(command string) error {
	return r.write(r.theme.Accent, true, "NEXT ", command)
}

func (r Renderer) write(color bentotui.Color, bold bool, prefix, message string) error {
	style := bentotui.Style{Color: color, Bold: bold, Enabled: r.color}
	_, err := fmt.Fprintln(r.out, style.Render(prefix+strings.TrimSpace(message)))
	return err
}
