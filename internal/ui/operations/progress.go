package operations

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/cloudboy-jh/mimir/internal/ui/appframe"
	"github.com/cloudboy-jh/mimir/internal/ui/bentotui"
)

// Progress selects the shared operation TUI on capable terminals and a stable
// line-oriented renderer everywhere else.
type Progress struct {
	operation *Operation
	out       io.Writer
	title     string
	phases    []string
	phase     int
}

func Start(ctx context.Context, in io.Reader, out io.Writer, title string, phases []string, cancel context.CancelFunc) *Progress {
	progress := &Progress{out: out, title: title, phases: append([]string(nil), phases...)}
	if operation := StartOperation(ctx, in, out, title, phases, cancel); operation != nil {
		progress.operation = operation
		return progress
	}
	if out != nil {
		render := appframe.New(out)
		_, _ = fmt.Fprintf(out, "%s\n\n", render.Heading(title))
	}
	return progress
}

func (p *Progress) Pause() {
	if p != nil && p.operation != nil {
		p.operation.Pause()
	}
}

func (p *Progress) Resume() {
	if p != nil && p.operation != nil {
		p.operation.Resume()
	}
}

// PromptSecret collects a secret inside the active application frame. The
// boolean is false when the line-oriented fallback owns presentation.
func (p *Progress) PromptSecret(label string) (value string, handled bool, err error) {
	if p == nil || p.operation == nil {
		return "", false, nil
	}
	value, err = p.operation.PromptSecret(label)
	return value, true, err
}

// Handoff visibly suspends the app while an external interactive process owns
// the terminal, then restores the same operation state.
func (p *Progress) Handoff(label string, action func() error) error {
	if p == nil || p.operation == nil {
		return action()
	}
	return p.operation.Handoff(label, action)
}

func (p *Progress) Stop() {
	if p != nil && p.operation != nil {
		p.operation.Stop()
	}
}

func (p *Progress) Complete(label string) {
	if p == nil {
		return
	}
	if p.operation != nil {
		p.operation.Complete(label)
		return
	}
	p.phase++
	p.writeLine("✓", bentotui.VariantSuccess, label)
}

func (p *Progress) Status(label string) {
	if p == nil || strings.TrimSpace(label) == "" {
		return
	}
	if p.operation != nil {
		p.operation.Status(label)
		return
	}
	p.writeLine("›", bentotui.VariantInfo, label)
}

func (p *Progress) Fail() {
	if p == nil {
		return
	}
	if p.operation != nil {
		p.operation.Fail()
		return
	}
	label := "Operation failed"
	if p.phase < len(p.phases) {
		label = p.phases[p.phase]
	}
	p.writeLine("×", bentotui.VariantDanger, label)
}

func (p *Progress) Finish(label string) {
	if p != nil && p.operation != nil {
		p.operation.Finish(label)
	}
}

func (p *Progress) Commit() {
	if p != nil && p.operation != nil {
		p.operation.Commit()
	}
}

func (p *Progress) Output() io.Writer {
	if p == nil || p.operation == nil {
		return nil
	}
	return p.operation.Output()
}

func (p *Progress) writeLine(marker string, variant bentotui.Variant, label string) {
	if p.out == nil {
		return
	}
	render := appframe.New(p.out)
	badge := bentotui.Badge(render.Theme, render.Color, marker, variant)
	_, _ = fmt.Fprintln(p.out, "  "+badge+" "+strings.TrimSpace(label))
}
