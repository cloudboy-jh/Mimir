package bentotui

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
)

type KeyKind uint8

const (
	KeyRune KeyKind = iota
	KeyUp
	KeyDown
	KeyEnter
	KeyEscape
	KeyBackspace
	KeyInterrupt
	KeyTab
	KeyMouseUp
	KeyMouseDown
)

type KeyModifier uint8

const (
	KeyModifierCtrl KeyModifier = 1 << iota
)

const (
	terminalByteUp        byte = 0x11
	terminalByteDown      byte = 0x12
	terminalByteCtrlUp    byte = 0x13
	terminalByteCtrlDown  byte = 0x14
	terminalByteMouseUp   byte = 0x15
	terminalByteMouseDown byte = 0x16
)

type Key struct {
	Kind      KeyKind
	Rune      rune
	Modifiers KeyModifier
}

type RunOptions struct {
	AlternateScreen bool
	Mouse           bool
}

type Screen struct {
	Width, Height int
}

type TerminalApp interface {
	View(Screen) string
	Handle(context.Context, Key) (quit bool)
}

type LiveTerminalApp interface {
	Updates() <-chan struct{}
}

// RenderedTerminalApp is notified after a frame has been written.
type RenderedTerminalApp interface {
	Rendered()
}

// Interactive reports whether both sides of a command are attached to a real
// terminal. Commands must retain a non-interactive path for pipes and agents.
func Interactive(in io.Reader, out io.Writer) bool {
	input, inputOK := in.(*os.File)
	output, outputOK := out.(*os.File)
	return inputOK && outputOK && isCharacterDevice(input) && isCharacterDevice(output) && os.Getenv("TERM") != "dumb" && os.Getenv("CI") == ""
}

// Run owns the alternate screen and terminal mode until the app exits.
func Run(ctx context.Context, in *os.File, out *os.File, app TerminalApp) error {
	return RunWithOptions(ctx, in, out, app, RunOptions{AlternateScreen: true})
}

// RunWithOptions owns the terminal mode until the app exits.
func RunWithOptions(ctx context.Context, in *os.File, out *os.File, app TerminalApp, options RunOptions) error {
	state, err := enterRawMode(in, out)
	if err != nil {
		return err
	}
	defer state.restore()
	start, cleanup := terminalControlSequences(options)
	if cleanup != "" {
		defer io.WriteString(out, cleanup)
	}
	if !options.AlternateScreen {
		defer io.WriteString(out, "\r\n")
	}
	if start != "" {
		if _, err := io.WriteString(out, start); err != nil {
			return err
		}
	}

	readCtx, cancelRead := context.WithCancel(ctx)
	var readers sync.WaitGroup
	defer func() {
		cancelRead()
		readers.Wait()
	}()
	keys := make(chan Key)
	errs := make(chan error, 1)
	readers.Add(1)
	go func() {
		defer readers.Done()
		readKeys(readCtx, in, keys, errs)
	}()
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	screen := terminalScreen(out)
	var updates <-chan struct{}
	if live, ok := app.(LiveTerminalApp); ok {
		updates = live.Updates()
	}
	renderer := terminalRenderer{out: out, noClear: !options.AlternateScreen}
	if err := drawApp(&renderer, app, screen); err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-errs:
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		case key := <-keys:
			if app.Handle(ctx, key) {
				return nil
			}
			screen = terminalScreen(out)
			if err := drawApp(&renderer, app, screen); err != nil {
				return err
			}
		case <-updates:
			screen = terminalScreen(out)
			if err := drawApp(&renderer, app, screen); err != nil {
				return err
			}
		case <-ticker.C:
			next := terminalScreen(out)
			if next != screen {
				screen = next
				renderer.reset()
				if err := drawApp(&renderer, app, screen); err != nil {
					return err
				}
			}
		}
	}
}

func terminalControlSequences(options RunOptions) (start, cleanup string) {
	if options.AlternateScreen {
		start, cleanup = "\x1b[?1049h\x1b[?25l\x1b[?7l", "\x1b[?25h\x1b[?7h\x1b[?1049l"
	}
	if options.Mouse {
		start += "\x1b[?1000h\x1b[?1006h"
		cleanup = "\x1b[?1000l\x1b[?1006l" + cleanup
	}
	return start, cleanup
}

func drawApp(renderer *terminalRenderer, app TerminalApp, screen Screen) error {
	if err := renderer.draw(app.View(screen)); err != nil {
		return err
	}
	if rendered, ok := app.(RenderedTerminalApp); ok {
		rendered.Rendered()
	}
	return nil
}

type terminalRenderer struct {
	out     io.Writer
	frame   string
	drawn   bool
	noClear bool
	lines   int
}

func (r *terminalRenderer) reset() {
	if r.noClear {
		r.frame = ""
		return
	}
	r.drawn = false
}

func (r *terminalRenderer) draw(view string) error {
	frame := strings.ReplaceAll(strings.TrimRight(view, "\n"), "\n", "\r\n")
	if r.drawn && frame == r.frame {
		return nil
	}
	prefix := ""
	if r.noClear {
		if r.drawn {
			prefix = "\r"
			if r.lines > 1 {
				prefix += fmt.Sprintf("\x1b[%dA", r.lines-1)
			}
			prefix += "\x1b[J"
		}
	} else {
		prefix = "\x1b[H"
		if !r.drawn {
			prefix += "\x1b[2J"
		}
	}
	if _, err := io.WriteString(r.out, prefix+frame+"\x1b[0m"); err != nil {
		return err
	}
	r.frame = frame
	r.drawn = true
	r.lines = strings.Count(frame, "\r\n") + 1
	return nil
}

func draw(out io.Writer, view string) error {
	return (&terminalRenderer{out: out}).draw(view)
}

func readKeys(ctx context.Context, in *os.File, keys chan<- Key, errs chan<- error) {
	for {
		value, err := readTerminalByte(ctx, in)
		if err != nil {
			select {
			case errs <- err:
			case <-ctx.Done():
			}
			return
		}
		key, ok := terminalKey(ctx, in, value)
		if !ok {
			continue
		}
		select {
		case keys <- key:
		case <-ctx.Done():
			return
		}
	}
}

func terminalKey(ctx context.Context, in *os.File, value byte) (Key, bool) {
	switch value {
	case terminalByteUp:
		return Key{Kind: KeyUp}, true
	case terminalByteDown:
		return Key{Kind: KeyDown}, true
	case terminalByteCtrlUp:
		return Key{Kind: KeyUp, Modifiers: KeyModifierCtrl}, true
	case terminalByteCtrlDown:
		return Key{Kind: KeyDown, Modifiers: KeyModifierCtrl}, true
	case terminalByteMouseUp:
		return Key{Kind: KeyMouseUp}, true
	case terminalByteMouseDown:
		return Key{Kind: KeyMouseDown}, true
	case 0x03:
		return Key{Kind: KeyInterrupt}, true
	case '\t':
		return Key{Kind: KeyTab}, true
	case '\r', '\n':
		return Key{Kind: KeyEnter}, true
	case 0x7f, 0x08:
		return Key{Kind: KeyBackspace}, true
	case 0x1b:
		sequenceCtx, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
		defer cancel()
		first, err := readTerminalByte(sequenceCtx, in)
		if err != nil {
			return Key{Kind: KeyEscape}, true
		}
		second, err := readTerminalByte(sequenceCtx, in)
		if err != nil || first != '[' {
			return Key{Kind: KeyEscape}, true
		}
		sequence := []byte{first, second}
		if second == '<' {
			for len(sequence) < 32 {
				next, err := readTerminalByte(sequenceCtx, in)
				if err != nil {
					break
				}
				sequence = append(sequence, next)
				if next == 'M' || next == 'm' {
					break
				}
			}
		} else if second == '1' {
			for len(sequence) < 5 {
				next, err := readTerminalByte(sequenceCtx, in)
				if err != nil {
					break
				}
				sequence = append(sequence, next)
			}
		}
		return escapeKey(sequence), true
	default:
		if value >= 0x20 {
			return terminalRune(ctx, in, value), true
		}
	}
	return Key{}, false
}

func terminalRune(ctx context.Context, in *os.File, first byte) Key {
	encoded := []byte{first}
	runeCtx, cancel := context.WithTimeout(ctx, 20*time.Millisecond)
	defer cancel()
	for !utf8.FullRune(encoded) && len(encoded) < utf8.UTFMax {
		next, err := readTerminalByte(runeCtx, in)
		if err != nil {
			break
		}
		encoded = append(encoded, next)
	}
	value, _ := utf8.DecodeRune(encoded)
	return Key{Kind: KeyRune, Rune: value}
}

func decodeKeys(ctx context.Context, bytes <-chan byte, keys chan<- Key) {
	for {
		var value byte
		select {
		case next, ok := <-bytes:
			if !ok {
				return
			}
			value = next
		case <-ctx.Done():
			return
		}
		switch value {
		case 0x03:
			keys <- Key{Kind: KeyInterrupt}
		case '\t':
			keys <- Key{Kind: KeyTab}
		case '\r', '\n':
			keys <- Key{Kind: KeyEnter}
		case 0x7f, 0x08:
			keys <- Key{Kind: KeyBackspace}
		case 0x1b:
			keys <- escapeKey(readEscapeSequence(ctx, bytes))
		default:
			if value >= 0x20 {
				keys <- decodeRune(ctx, bytes, value)
			}
		}
	}
}

func escapeKey(sequence []byte) Key {
	if string(sequence) == "[A" {
		return Key{Kind: KeyUp}
	}
	if string(sequence) == "[B" {
		return Key{Kind: KeyDown}
	}
	if string(sequence) == "[1;5A" {
		return Key{Kind: KeyUp, Modifiers: KeyModifierCtrl}
	}
	if string(sequence) == "[1;5B" {
		return Key{Kind: KeyDown, Modifiers: KeyModifierCtrl}
	}
	var button, x, y int
	var final rune
	if _, err := fmt.Sscanf(string(sequence), "[<%d;%d;%d%c", &button, &x, &y, &final); err == nil && final == 'M' {
		switch button {
		case 64:
			return Key{Kind: KeyMouseUp}
		case 65:
			return Key{Kind: KeyMouseDown}
		}
	}
	return Key{Kind: KeyEscape}
}

func readEscapeSequence(ctx context.Context, bytes <-chan byte) []byte {
	sequence := make([]byte, 0, 5)
	for len(sequence) < 2 || (len(sequence) < 5 && len(sequence) > 1 && sequence[1] == '1') {
		next, ok := readDecoderByte(ctx, bytes, 20*time.Millisecond)
		if !ok {
			break
		}
		sequence = append(sequence, next)
		if len(sequence) >= 2 && sequence[1] == '<' {
			for len(sequence) < 32 {
				next, ok := readDecoderByte(ctx, bytes, 20*time.Millisecond)
				if !ok {
					break
				}
				sequence = append(sequence, next)
				if next == 'M' || next == 'm' {
					break
				}
			}
			break
		}
	}
	return sequence
}

func decodeRune(ctx context.Context, bytes <-chan byte, first byte) Key {
	encoded := []byte{first}
	for !utf8.FullRune(encoded) && len(encoded) < utf8.UTFMax {
		next, ok := readDecoderByte(ctx, bytes, 20*time.Millisecond)
		if !ok {
			break
		}
		encoded = append(encoded, next)
	}
	value, _ := utf8.DecodeRune(encoded)
	return Key{Kind: KeyRune, Rune: value}
}

func readDecoderByte(ctx context.Context, bytes <-chan byte, timeout time.Duration) (byte, bool) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case value, ok := <-bytes:
		return value, ok
	case <-ctx.Done():
	case <-timer.C:
	}
	return 0, false
}

func isCharacterDevice(file *os.File) bool {
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func terminalScreen(file *os.File) Screen {
	width, height := terminalSize(file)
	if width <= 0 {
		width = 80
	}
	if height <= 0 {
		height = 24
	}
	return Screen{Width: width, Height: height}
}

// ScreenFor returns the current terminal dimensions with stable fallbacks.
func ScreenFor(file *os.File) Screen { return terminalScreen(file) }

// Dimensions returns measured terminal dimensions without fallbacks.
func Dimensions(file *os.File) Screen {
	width, height := terminalSize(file)
	return Screen{Width: width, Height: height}
}
