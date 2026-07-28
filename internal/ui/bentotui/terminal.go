package bentotui

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"sync"
	"time"
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
)

const (
	terminalByteUp   byte = 0x11
	terminalByteDown byte = 0x12
)

type Key struct {
	Kind KeyKind
	Rune rune
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

// Interactive reports whether both sides of a command are attached to a real
// terminal. Commands must retain a non-interactive path for pipes and agents.
func Interactive(in io.Reader, out io.Writer) bool {
	input, inputOK := in.(*os.File)
	output, outputOK := out.(*os.File)
	return inputOK && outputOK && isCharacterDevice(input) && isCharacterDevice(output) && os.Getenv("TERM") != "dumb" && os.Getenv("CI") == ""
}

// Run owns the alternate screen and terminal mode until the app exits.
func Run(ctx context.Context, in *os.File, out *os.File, app TerminalApp) error {
	state, err := enterRawMode(in, out)
	if err != nil {
		return err
	}
	defer state.restore()
	defer io.WriteString(out, "\x1b[?25h\x1b[?1049l")
	if _, err := io.WriteString(out, "\x1b[?1049h\x1b[?25l"); err != nil {
		return err
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
	if err := draw(out, app.View(screen)); err != nil {
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
			if err := draw(out, app.View(screen)); err != nil {
				return err
			}
		case <-updates:
			screen = terminalScreen(out)
			if err := draw(out, app.View(screen)); err != nil {
				return err
			}
		case <-ticker.C:
			next := terminalScreen(out)
			if next != screen || updates != nil {
				screen = next
				if err := draw(out, app.View(screen)); err != nil {
					return err
				}
			}
		}
	}
}

func draw(out io.Writer, view string) error {
	view = strings.ReplaceAll(strings.TrimRight(view, "\n"), "\n", "\r\n")
	_, err := io.WriteString(out, "\x1b[H\x1b[2J"+view+"\x1b[0m")
	return err
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
	case 0x03:
		return Key{Kind: KeyInterrupt}, true
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
		switch second {
		case 'A':
			return Key{Kind: KeyUp}, true
		case 'B':
			return Key{Kind: KeyDown}, true
		default:
			return Key{Kind: KeyEscape}, true
		}
	default:
		if value >= 0x20 {
			return Key{Kind: KeyRune, Rune: rune(value)}, true
		}
	}
	return Key{}, false
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
		case '\r', '\n':
			keys <- Key{Kind: KeyEnter}
		case 0x7f, 0x08:
			keys <- Key{Kind: KeyBackspace}
		case 0x1b:
			first, second, ok := readEscapeSequence(ctx, bytes)
			if ok && first == '[' {
				switch second {
				case 'A':
					keys <- Key{Kind: KeyUp}
				case 'B':
					keys <- Key{Kind: KeyDown}
				default:
					keys <- Key{Kind: KeyEscape}
				}
			} else {
				keys <- Key{Kind: KeyEscape}
			}
		default:
			if value >= 0x20 {
				keys <- Key{Kind: KeyRune, Rune: rune(value)}
			}
		}
	}
}

func readEscapeSequence(ctx context.Context, bytes <-chan byte) (byte, byte, bool) {
	select {
	case first := <-bytes:
		select {
		case second := <-bytes:
			return first, second, true
		case <-ctx.Done():
		case <-time.After(20 * time.Millisecond):
		}
	case <-ctx.Done():
	case <-time.After(20 * time.Millisecond):
	}
	return 0, 0, false
}

func isCharacterDevice(file *os.File) bool {
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func terminalScreen(file *os.File) Screen {
	width, height := terminalSize(file)
	if width < 20 {
		width = 80
	}
	if height < 8 {
		height = 24
	}
	return Screen{Width: width, Height: height}
}
