package ui

import (
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/cloudboy-jh/mimir/internal/ui/bentotui"
)

type Stepper struct {
	out     io.Writer
	render  Renderer
	phases  []string
	phase   int
	mu      sync.Mutex
	done    chan struct{}
	wg      sync.WaitGroup
	running bool
	animate bool
}

func StartStepper(out io.Writer, title string, phases []string) *Stepper {
	render := New(out)
	stepper := &Stepper{out: out, render: render, phases: append([]string(nil), phases...)}
	if file, ok := out.(*os.File); ok {
		if info, err := file.Stat(); err == nil && info.Mode()&os.ModeCharDevice != 0 && os.Getenv("TERM") != "dumb" {
			stepper.animate = true
		}
	}
	fmt.Fprintln(out, render.Heading(title))
	fmt.Fprintln(out)
	stepper.resume()
	return stepper
}

func (s *Stepper) resume() {
	if s == nil || !s.animate || s.phase >= len(s.phases) {
		return
	}
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.done = make(chan struct{})
	done, label := s.done, s.phases[s.phase]
	s.running = true
	s.wg.Add(1)
	s.mu.Unlock()
	go func() {
		defer s.wg.Done()
		frames := []rune{'⠋', '⠙', '⠹', '⠸', '⠼', '⠴', '⠦', '⠧', '⠇', '⠏'}
		ticker := time.NewTicker(80 * time.Millisecond)
		defer ticker.Stop()
		for i := 0; ; i++ {
			spinner := bentotui.Style{Color: s.render.Theme.Accent, Bold: true, Enabled: s.render.Color}.Render(string(frames[i%len(frames)]))
			fmt.Fprintf(s.out, "\r\x1b[2K  %s %s", spinner, label)
			select {
			case <-done:
				return
			case <-ticker.C:
			}
		}
	}()
}

func (s *Stepper) Pause() {
	if s == nil || !s.animate {
		return
	}
	s.mu.Lock()
	if !s.running {
		s.mu.Unlock()
		return
	}
	done := s.done
	s.running = false
	s.mu.Unlock()
	close(done)
	s.wg.Wait()
	fmt.Fprint(s.out, "\r\x1b[2K")
}

func (s *Stepper) Resume() { s.resume() }

func (s *Stepper) Complete(label string) {
	if s == nil {
		return
	}
	s.Pause()
	badge := bentotui.Badge(s.render.Theme, s.render.Color, "✓", bentotui.VariantSuccess)
	fmt.Fprintf(s.out, "  %s %s\n", badge, label)
	s.phase++
	s.resume()
}

func (s *Stepper) Fail(label string) {
	if s == nil {
		return
	}
	s.Pause()
	badge := bentotui.Badge(s.render.Theme, s.render.Color, "×", bentotui.VariantDanger)
	fmt.Fprintf(s.out, "  %s %s\n", badge, label)
}

func (s *Stepper) FailCurrent() {
	label := "Operation failed"
	if s != nil && s.phase < len(s.phases) {
		label = s.phases[s.phase]
	}
	s.Fail(label)
}

func (s *Stepper) Stop() { s.Pause() }
