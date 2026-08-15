// Package selector provides a bounded, human-only terminal checklist.
package selector

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/cloudboy-jh/mimir/internal/ui/appframe"
	"github.com/cloudboy-jh/mimir/internal/ui/bentotui"
)

type Item struct {
	Label    string
	Selected bool
}

type Result struct {
	Selected []bool
	Accepted bool
}

type key int

const (
	keyUnknown key = iota
	keyUp
	keyDown
	keyToggle
	keyAccept
	keyCancel
)

// Available reports whether both streams support interactive terminal modes.
func Available(in, out *os.File) bool {
	return terminalAvailable(in, out)
}

// Run displays a checklist until the user applies or cancels it.
func Run(in, out *os.File, title string, items []Item) (Result, error) {
	if len(items) == 0 {
		return Result{}, errors.New("selector requires at least one item")
	}
	restore, err := prepareTerminal(in, out)
	if err != nil {
		return Result{}, err
	}
	defer restore()
	if _, err := io.WriteString(out, "\x1b[?25l"); err != nil {
		return Result{}, err
	}
	defer func() { _, _ = io.WriteString(out, "\x1b[?25h") }()
	frame := appframe.New(out)
	return run(in, out, title, items, frame.Context())
}

func run(in io.Reader, out io.Writer, title string, items []Item, ui bentotui.Context) (Result, error) {
	selected := make([]bool, len(items))
	original := make([]bool, len(items))
	for i, item := range items {
		selected[i] = item.Selected
		original[i] = item.Selected
	}
	focus := 0
	lines := len(items) + 2
	first := true
	for {
		if err := render(out, title, items, selected, focus, lines, first, ui); err != nil {
			return Result{}, err
		}
		first = false
		pressed, err := readKey(in)
		if err != nil {
			return Result{}, err
		}
		switch pressed {
		case keyUp:
			focus = (focus - 1 + len(items)) % len(items)
		case keyDown:
			focus = (focus + 1) % len(items)
		case keyToggle:
			selected[focus] = !selected[focus]
		case keyAccept:
			return Result{Selected: selected, Accepted: true}, nil
		case keyCancel:
			return Result{Selected: original}, nil
		}
	}
}

func render(out io.Writer, title string, items []Item, selected []bool, focus, lines int, first bool, ui bentotui.Context) error {
	if !first {
		if _, err := fmt.Fprintf(out, "\x1b[%dA", lines); err != nil {
			return err
		}
	}
	accent := bentotui.Style{Color: ui.Theme.Accent, Bold: true, Enabled: ui.Color}
	text := bentotui.Style{Color: ui.Theme.Text, Enabled: ui.Color}
	muted := bentotui.Style{Color: ui.Theme.Muted, Enabled: ui.Color}
	success := bentotui.Style{Color: ui.Theme.Success, Bold: true, Enabled: ui.Color}
	rows := make([]string, 0, lines)
	rows = append(rows, bentotui.Truncate(accent.Render(title), ui.Width))
	for i, item := range items {
		cursor := "  "
		if i == focus {
			cursor = accent.Render("›") + " "
		}
		marker := muted.Render("○")
		if selected[i] {
			marker = success.Render("●")
		}
		label := text.Render(item.Label)
		if i == focus {
			label = bentotui.Style{Color: ui.Theme.SelectionText, Bold: true, Enabled: ui.Color}.Render(item.Label)
		}
		rows = append(rows, bentotui.Truncate(cursor+marker+" "+label, ui.Width))
	}
	hint := "↑/↓ move  Space toggle  Enter apply  q cancel"
	hintStyle := muted
	rows = append(rows, bentotui.Truncate(hintStyle.Render(hint), ui.Width))
	for _, row := range rows {
		if _, err := fmt.Fprintf(out, "\r\x1b[2K%s\n", row); err != nil {
			return err
		}
	}
	return nil
}

func readKey(in io.Reader) (key, error) {
	var b [1]byte
	if _, err := io.ReadFull(in, b[:]); err != nil {
		return keyUnknown, err
	}
	switch b[0] {
	case ' ', 'x':
		return keyToggle, nil
	case '\r', '\n':
		return keyAccept, nil
	case 'q', 'Q', 0x03:
		return keyCancel, nil
	case 'k', 'K':
		return keyUp, nil
	case 'j', 'J':
		return keyDown, nil
	case 0x1b:
		var sequence [2]byte
		if _, err := io.ReadFull(in, sequence[:]); err != nil {
			return keyUnknown, err
		}
		if sequence[0] == '[' {
			switch sequence[1] {
			case 'A':
				return keyUp, nil
			case 'B':
				return keyDown, nil
			}
		}
	}
	return keyUnknown, nil
}
