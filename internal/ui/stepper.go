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
	steps   []Step
	phase   int
	opMu    sync.Mutex
	mu      sync.Mutex
	done    chan struct{}
	wg      sync.WaitGroup
	running bool
	pausing bool
	animate bool
}

type StepState string

const (
	StepPending  StepState = "pending"
	StepActive   StepState = "active"
	StepComplete StepState = "complete"
	StepFailed   StepState = "failed"
)

type Step struct {
	Label       string
	Description string
	State       StepState
	StartedAt   time.Time
	Elapsed     time.Duration
}

func StartStepper(out io.Writer, title string, phases []string) *Stepper {
	render := New(out)
	steps := make([]Step, len(phases))
	for i, phase := range phases {
		steps[i] = Step{Label: phase, State: StepPending}
	}
	if len(steps) > 0 {
		steps[0].State = StepActive
		steps[0].StartedAt = time.Now()
	}
	stepper := &Stepper{out: out, render: render, steps: steps}
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
	if s == nil {
		return
	}
	s.opMu.Lock()
	defer s.opMu.Unlock()
	s.resumeLocked()
}

func (s *Stepper) resumeLocked() {
	s.mu.Lock()
	if !s.animate || s.pausing || s.running || s.phase >= len(s.steps) {
		s.mu.Unlock()
		return
	}
	s.done = make(chan struct{})
	done, label := s.done, s.steps[s.phase].Label
	started := s.steps[s.phase].StartedAt
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
			elapsed := bentotui.Style{Color: s.render.Theme.Muted, Enabled: s.render.Color}.Render(" · " + formatElapsed(time.Since(started)))
			line := bentotui.Truncate("  "+spinner+" "+label+elapsed, s.render.Width)
			fmt.Fprintf(s.out, "\r\x1b[2K%s", line)
			select {
			case <-done:
				return
			case <-ticker.C:
			}
		}
	}()
}

func (s *Stepper) Pause() {
	if s == nil {
		return
	}
	s.opMu.Lock()
	defer s.opMu.Unlock()
	s.pauseLocked()
}

func (s *Stepper) pauseLocked() {
	if !s.animate {
		return
	}
	s.mu.Lock()
	if !s.running || s.pausing {
		s.mu.Unlock()
		return
	}
	done := s.done
	s.running = false
	s.pausing = true
	s.mu.Unlock()
	close(done)
	s.wg.Wait()
	fmt.Fprint(s.out, "\r\x1b[2K")
	s.mu.Lock()
	s.pausing = false
	s.mu.Unlock()
}

func (s *Stepper) Resume() { s.resume() }

func (s *Stepper) Complete(label string) {
	if s == nil {
		return
	}
	s.opMu.Lock()
	defer s.opMu.Unlock()
	s.pauseLocked()
	s.mu.Lock()
	elapsed := time.Duration(0)
	if s.phase < len(s.steps) {
		s.steps[s.phase].State = StepComplete
		s.steps[s.phase].Elapsed = time.Since(s.steps[s.phase].StartedAt)
		elapsed = s.steps[s.phase].Elapsed
	}
	s.phase++
	if s.phase < len(s.steps) {
		s.steps[s.phase].State = StepActive
		s.steps[s.phase].StartedAt = time.Now()
	}
	s.mu.Unlock()
	badge := bentotui.Badge(s.render.Theme, s.render.Color, "✓", bentotui.VariantSuccess)
	elapsedLabel := ""
	if elapsed >= time.Second {
		elapsedLabel = bentotui.Style{Color: s.render.Theme.Muted, Enabled: s.render.Color}.Render(" · " + formatElapsed(elapsed))
	}
	line := bentotui.Truncate("  "+badge+" "+label+elapsedLabel, s.render.Width)
	fmt.Fprintln(s.out, line)
	s.resumeLocked()
}

func (s *Stepper) Fail(label string) {
	if s == nil {
		return
	}
	s.opMu.Lock()
	defer s.opMu.Unlock()
	s.failLocked(label)
}

func (s *Stepper) failLocked(label string) {
	s.pauseLocked()
	s.mu.Lock()
	if s.phase < len(s.steps) {
		s.steps[s.phase].State = StepFailed
		s.steps[s.phase].Elapsed = time.Since(s.steps[s.phase].StartedAt)
	}
	s.mu.Unlock()
	badge := bentotui.Badge(s.render.Theme, s.render.Color, "×", bentotui.VariantDanger)
	fmt.Fprintln(s.out, bentotui.Truncate("  "+badge+" "+label, s.render.Width))
}

func (s *Stepper) FailCurrent() {
	if s == nil {
		return
	}
	s.opMu.Lock()
	defer s.opMu.Unlock()
	label := "Operation failed"
	s.mu.Lock()
	if s.phase < len(s.steps) {
		label = s.steps[s.phase].Label
	}
	s.mu.Unlock()
	s.failLocked(label)
}

func (s *Stepper) Stop() { s.Pause() }

func (s *Stepper) Steps() []Step {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Step(nil), s.steps...)
}

func formatElapsed(value time.Duration) string {
	if value < time.Second {
		return fmt.Sprintf("%dms", value.Milliseconds())
	}
	return value.Round(100 * time.Millisecond).String()
}
