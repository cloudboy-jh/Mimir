package operations

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/cloudboy-jh/mimir/internal/ui/appframe"
	"github.com/cloudboy-jh/mimir/internal/ui/bentotui"
)

type operationEntry struct {
	label string
	state StepState
}

type Operation struct {
	ctx       context.Context
	in, out   *os.File
	app       *operationApp
	runMu     sync.Mutex
	runCancel context.CancelFunc
	runDone   chan struct{}
}

type operationApp struct {
	mu           sync.Mutex
	out          io.Writer
	title        string
	started      time.Time
	entries      []operationEntry
	logs         []string
	active       int
	offset       int
	follow       bool
	status       string
	updates      chan struct{}
	rendered     chan struct{}
	cancel       context.CancelFunc
	cancelled    bool
	cancellable  bool
	help         bool
	tailAtScroll int
	prompt       *secretPrompt
}

type secretPrompt struct {
	label  string
	value  []rune
	result chan promptResult
}

type promptResult struct {
	value string
	err   error
}

var errPromptCancelled = errors.New("prompt cancelled")

type operationLogWriter struct {
	mu      sync.Mutex
	app     *operationApp
	pending string
}

func (w *operationLogWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.pending == "" {
		return
	}
	w.app.addLog(cleanTerminalText(w.pending))
	w.pending = ""
}

// StartOperation starts an alternate-screen operation viewport when both
// command streams are terminals. Callers retain their existing static path
// when it returns nil.
func StartOperation(ctx context.Context, in io.Reader, out io.Writer, title string, phases []string, cancel context.CancelFunc) *Operation {
	input, inputOK := in.(*os.File)
	output, outputOK := out.(*os.File)
	if !inputOK || !outputOK || !appframe.Interactive(input, output) {
		return nil
	}
	entries := make([]operationEntry, len(phases))
	for i, phase := range phases {
		entries[i] = operationEntry{label: phase, state: StepPending}
	}
	active := -1
	if len(entries) > 0 {
		active = 0
		entries[0].state = StepActive
	}
	app := &operationApp{
		out: output, title: title, started: time.Now(), entries: entries, active: active,
		follow: true, status: "running", updates: make(chan struct{}, 1), rendered: make(chan struct{}, 1), cancel: cancel, cancellable: true,
	}
	operation := &Operation{ctx: ctx, in: input, out: output, app: app}
	operation.Resume()
	if !operation.waitReady() {
		operation.Pause()
		return nil
	}
	return operation
}

func (o *Operation) waitReady() bool {
	o.runMu.Lock()
	done := o.runDone
	o.runMu.Unlock()
	if done == nil {
		return false
	}
	select {
	case <-o.app.rendered:
		return true
	case <-done:
		return false
	case <-time.After(500 * time.Millisecond):
		return false
	}
}

func (o *Operation) Complete(label string) {
	if o == nil {
		return
	}
	o.app.mu.Lock()
	if o.app.active >= 0 && o.app.active < len(o.app.entries) {
		o.app.entries[o.app.active].state = StepComplete
		if strings.TrimSpace(label) != "" {
			o.app.entries[o.app.active].label = label
		}
		o.app.active++
		if o.app.active < len(o.app.entries) {
			o.app.entries[o.app.active].state = StepActive
		} else {
			o.app.active = -1
			o.app.status = "completed"
			o.app.cancellable = false
		}
	} else if strings.TrimSpace(label) != "" {
		o.app.entries = append(o.app.entries, operationEntry{label: label, state: StepComplete})
	}
	o.app.mu.Unlock()
	o.app.notify()
}

// Status advances a dynamic operation feed. It is used by workflows whose
// branches cannot be represented by a fixed phase list, such as self-update.
func (o *Operation) Status(label string) {
	if o == nil || strings.TrimSpace(label) == "" {
		return
	}
	o.app.mu.Lock()
	if o.app.active >= 0 && o.app.active < len(o.app.entries) {
		if o.app.entries[o.app.active].label == label {
			o.app.mu.Unlock()
			return
		}
		o.app.entries[o.app.active].state = StepComplete
	}
	o.app.entries = append(o.app.entries, operationEntry{label: label, state: StepActive})
	o.app.active = len(o.app.entries) - 1
	o.app.mu.Unlock()
	o.app.notify()
}

func (o *Operation) Fail() {
	if o == nil {
		return
	}
	o.app.mu.Lock()
	if o.app.active >= 0 && o.app.active < len(o.app.entries) {
		o.app.entries[o.app.active].state = StepFailed
	}
	o.app.status = "failed"
	o.app.mu.Unlock()
	o.notifyAndWait()
}

func (o *Operation) Finish(label string) {
	if o == nil {
		return
	}
	o.app.mu.Lock()
	if o.app.active >= 0 && o.app.active < len(o.app.entries) {
		o.app.entries[o.app.active].state = StepComplete
		o.app.active = -1
	}
	if strings.TrimSpace(label) != "" {
		o.app.entries = append(o.app.entries, operationEntry{label: strings.TrimSpace(label), state: StepComplete})
	}
	o.app.status = "completed"
	o.app.cancellable = false
	o.app.mu.Unlock()
	o.notifyAndWait()
}

func (o *Operation) Commit() {
	if o == nil {
		return
	}
	o.app.mu.Lock()
	o.app.cancellable = false
	o.app.mu.Unlock()
	o.app.notify()
}

func (o *Operation) Pause() {
	if o == nil {
		return
	}
	o.runMu.Lock()
	cancel, done := o.runCancel, o.runDone
	o.runCancel, o.runDone = nil, nil
	o.runMu.Unlock()
	if cancel != nil {
		cancel()
		<-done
	}
}

func (o *Operation) Resume() {
	if o == nil || o.ctx.Err() != nil {
		return
	}
	o.runMu.Lock()
	if o.runCancel != nil {
		o.runMu.Unlock()
		return
	}
	runCtx, cancel := context.WithCancel(o.ctx)
	done := make(chan struct{})
	o.runCancel, o.runDone = cancel, done
	o.runMu.Unlock()
	go func() {
		defer close(done)
		tickCtx, stopTicks := context.WithCancel(runCtx)
		defer stopTicks()
		go o.app.tick(tickCtx)
		err := bentotui.Run(runCtx, o.in, o.out, o.app)
		if err != nil && !errors.Is(err, context.Canceled) {
			o.app.mu.Lock()
			o.app.status = "terminal error"
			o.app.mu.Unlock()
		}
	}()
}

func (a *operationApp) tick(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			a.notify()
		}
	}
}

func (o *Operation) Stop() { o.Pause() }

func (o *Operation) PromptSecret(label string) (string, error) {
	if o == nil {
		return "", errors.New("interactive prompt unavailable")
	}
	request := &secretPrompt{label: strings.TrimSpace(label), result: make(chan promptResult, 1)}
	o.app.mu.Lock()
	if o.app.prompt != nil {
		o.app.mu.Unlock()
		return "", errors.New("interactive prompt already active")
	}
	o.app.prompt = request
	o.app.mu.Unlock()
	o.app.notify()
	select {
	case result := <-request.result:
		return result.value, result.err
	case <-o.ctx.Done():
		return "", o.ctx.Err()
	}
}

func (o *Operation) Handoff(label string, action func() error) error {
	if o == nil {
		return action()
	}
	o.app.mu.Lock()
	o.app.status = "external"
	if strings.TrimSpace(label) != "" {
		o.app.logs = append(o.app.logs, strings.TrimSpace(label))
	}
	o.app.mu.Unlock()
	o.notifyAndWait()
	o.Pause()
	err := action()
	if o.ctx.Err() == nil {
		o.Resume()
		o.app.mu.Lock()
		o.app.status = "running"
		o.app.mu.Unlock()
		o.app.notify()
	}
	return err
}

func (o *Operation) Output() io.Writer {
	if o == nil {
		return nil
	}
	return &operationLogWriter{app: o.app}
}

func (a *operationApp) Updates() <-chan struct{} { return a.updates }

func (a *operationApp) Rendered() {
	select {
	case a.rendered <- struct{}{}:
	default:
	}
}

func (a *operationApp) notify() {
	select {
	case a.updates <- struct{}{}:
	default:
	}
}

func (o *Operation) notifyAndWait() {
	if o == nil {
		return
	}
	for {
		select {
		case <-o.app.rendered:
		default:
			o.app.notify()
			if !o.running() {
				return
			}
			select {
			case <-o.app.rendered:
			case <-time.After(250 * time.Millisecond):
			}
			return
		}
	}
}

func (o *Operation) running() bool {
	o.runMu.Lock()
	defer o.runMu.Unlock()
	return o.runCancel != nil
}

func (a *operationApp) Handle(_ context.Context, key bentotui.Key) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.prompt != nil {
		switch key.Kind {
		case bentotui.KeyRune:
			a.prompt.value = append(a.prompt.value, key.Rune)
		case bentotui.KeyBackspace:
			if len(a.prompt.value) > 0 {
				a.prompt.value = a.prompt.value[:len(a.prompt.value)-1]
			}
		case bentotui.KeyEnter:
			request := a.prompt
			a.prompt = nil
			request.result <- promptResult{value: string(request.value)}
		case bentotui.KeyEscape, bentotui.KeyInterrupt:
			request := a.prompt
			a.prompt = nil
			request.result <- promptResult{err: errPromptCancelled}
		}
		return false
	}
	if a.help {
		if key.Kind == bentotui.KeyEscape {
			a.help = false
		}
		return false
	}
	switch key.Kind {
	case bentotui.KeyUp:
		a.follow = false
		a.tailAtScroll = len(a.entries) + len(a.logs)
		a.offset = max(0, a.offset-1)
	case bentotui.KeyDown:
		a.offset++
	case bentotui.KeyInterrupt:
		if a.cancellable && !a.cancelled && a.cancel != nil {
			a.cancelled = true
			a.status = "cancelling"
			a.cancel()
		}
	case bentotui.KeyRune:
		switch key.Rune {
		case 'k':
			a.follow = false
			a.tailAtScroll = len(a.entries) + len(a.logs)
			a.offset = max(0, a.offset-1)
		case 'j':
			a.offset++
		case 'g':
			a.follow, a.offset = false, 0
		case 'G', 'f':
			a.follow = true
		case '?':
			a.help = true
		}
	case bentotui.KeyEscape:
		a.help = false
	}
	return false
}

func (a *operationApp) View(screen bentotui.Screen) string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if appframe.TooSmall(screen) {
		return appframe.SmallScreen(screen)
	}
	render := appframe.New(a.out).WithWidth(appframe.ForScreen(screen).Width)
	if a.help {
		footer := appframe.Footer(render.Context(), []appframe.Binding{{Key: "Esc", Label: "Back"}}, nil)
		view, _ := (appframe.Frame{Surface: "Help", Lines: []string{"Keyboard", "", "↑/↓ or j/k  Scroll output", "g/G          Beginning or end", "f            Follow live output", "Ctrl+C       Cancel while safe"}, Footer: footer}).Render(render.Context(), screen)
		return view
	}
	lines := make([]string, 0, len(a.entries))
	for _, entry := range a.entries {
		marker, variant := "·", bentotui.VariantNeutral
		switch entry.state {
		case StepActive:
			marker, variant = "›", bentotui.VariantInfo
		case StepComplete:
			marker, variant = "✓", bentotui.VariantSuccess
		case StepFailed:
			marker, variant = "×", bentotui.VariantDanger
		}
		lines = append(lines, " "+bentotui.Badge(render.Theme, render.Color, marker, variant)+" "+entry.label)
	}
	for _, line := range a.logs {
		lines = append(lines, "   "+line)
	}
	if a.prompt != nil {
		lines = append(lines, "", " "+a.prompt.label, " "+strings.Repeat("•", len(a.prompt.value))+"_")
	}
	layout := appframe.ForScreen(screen)
	lineWidth := layout.BodyWidth
	wrapped := make([]string, 0, len(lines))
	for _, line := range lines {
		wrapped = append(wrapped, bentotui.WrapPreserve(line, lineWidth)...)
	}
	lines = wrapped
	bodyHeight := layout.BodyHeight
	if a.follow {
		a.offset = max(0, len(lines)-bodyHeight)
	} else {
		a.offset = min(a.offset, max(0, len(lines)-bodyHeight))
	}
	right := a.status + " · " + formatElapsed(time.Since(a.started))
	newer := max(0, len(a.entries)+len(a.logs)-a.tailAtScroll)
	followLabel := "Follow"
	if !a.follow && newer > 0 {
		followLabel = fmt.Sprintf("Jump live · %d newer", newer)
	}
	leftBindings := []appframe.Binding{{Key: "↑↓", Label: "Scroll"}, {Key: "f", Label: followLabel}}
	rightBindings := []appframe.Binding{{Key: "?", Label: "Help"}}
	if a.prompt != nil {
		leftBindings = []appframe.Binding{{Key: "Enter", Label: "Continue"}}
		rightBindings = []appframe.Binding{{Key: "Esc", Label: "Cancel"}}
	}
	if a.cancellable {
		rightBindings = append(rightBindings, appframe.Binding{Key: "^C", Label: "Cancel"})
	}
	footer := appframe.Footer(render.Context(), leftBindings, rightBindings)
	view, offset := (appframe.Frame{Surface: surfaceName(a.title), Status: right, Footer: footer, Lines: lines, Offset: a.offset, Follow: a.follow}).Render(render.Context(), screen)
	a.offset = offset
	return view
}

func surfaceName(title string) string {
	title = strings.TrimSpace(strings.TrimPrefix(title, "Mimir "))
	if title == "" {
		return "Operation"
	}
	return strings.ToUpper(title[:1]) + title[1:]
}

func (w *operationLogWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	source := w.pending + string(data)
	source = strings.ReplaceAll(source, "\r", "\n")
	parts := strings.Split(source, "\n")
	w.pending = parts[len(parts)-1]
	for _, part := range parts[:len(parts)-1] {
		w.app.addLog(cleanTerminalText(part))
	}
	return len(data), nil
}

func (a *operationApp) addLog(line string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	a.mu.Lock()
	a.logs = append(a.logs, line)
	if len(a.logs) > 200 {
		a.logs = append([]string(nil), a.logs[len(a.logs)-200:]...)
	}
	a.mu.Unlock()
	a.notify()
}

func cleanTerminalText(value string) string {
	var result strings.Builder
	for index := 0; index < len(value); {
		if value[index] == 0x1b {
			index++
			if index < len(value) && value[index] == '[' {
				index++
				for index < len(value) {
					current := value[index]
					index++
					if current >= 0x40 && current <= 0x7e {
						break
					}
				}
				continue
			}
			continue
		}
		r, size := utf8.DecodeRuneInString(value[index:])
		if r == utf8.RuneError && size == 1 {
			index++
			continue
		}
		if r == '\t' || (r >= 0x20 && !(r >= 0x80 && r <= 0x9f)) {
			result.WriteRune(r)
		}
		index += size
	}
	return result.String()
}

func (a *operationApp) String() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return fmt.Sprintf("%s: %s", a.title, a.status)
}
