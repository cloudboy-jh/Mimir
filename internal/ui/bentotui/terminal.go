package bentotui

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"
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

	keys := make(chan Key)
	errs := make(chan error, 1)
	go readKeys(in, keys, errs)
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()

	screen := terminalScreen(out)
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
		case <-ticker.C:
			next := terminalScreen(out)
			if next != screen {
				screen = next
				if err := draw(out, app.View(screen)); err != nil {
					return err
				}
			}
		}
	}
}

func draw(out io.Writer, view string) error {
	_, err := io.WriteString(out, "\x1b[H\x1b[2J"+strings.TrimRight(view, "\n")+"\x1b[0m")
	return err
}

func readKeys(in io.Reader, keys chan<- Key, errs chan<- error) {
	bytes := make(chan byte)
	go func() {
		buffer := make([]byte, 1)
		for {
			if _, err := io.ReadFull(in, buffer); err != nil {
				errs <- err
				return
			}
			bytes <- buffer[0]
		}
	}()
	for {
		value := <-bytes
		switch value {
		case 0x03:
			keys <- Key{Kind: KeyInterrupt}
		case '\r', '\n':
			keys <- Key{Kind: KeyEnter}
		case 0x7f, 0x08:
			keys <- Key{Kind: KeyBackspace}
		case 0x1b:
			first, second, ok := readEscapeSequence(bytes)
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

func readEscapeSequence(bytes <-chan byte) (byte, byte, bool) {
	select {
	case first := <-bytes:
		select {
		case second := <-bytes:
			return first, second, true
		case <-time.After(20 * time.Millisecond):
		}
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
