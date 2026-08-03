package bentotui

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"
)

func TestReadKeysDecodesNavigationAndInterrupt(t *testing.T) {
	keys := decodeTestKeys([]byte{'j', 0x1b, '[', 'A', '\r', 0x03})
	want := []Key{{Kind: KeyRune, Rune: 'j'}, {Kind: KeyUp}, {Kind: KeyEnter}, {Kind: KeyInterrupt}}
	for index, expected := range want {
		select {
		case got := <-keys:
			if got != expected {
				t.Fatalf("key %d: got %#v want %#v", index, got, expected)
			}
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for key %d", index)
		}
	}
}

func TestReadKeysDecodesCtrlArrowsTabAndUTF8(t *testing.T) {
	input := append([]byte{0x1b, '[', '1', ';', '5', 'A', 0x1b, '[', '1', ';', '5', 'B', '\t'}, []byte("é界")...)
	keys := decodeTestKeys(input)
	want := []Key{
		{Kind: KeyUp, Modifiers: KeyModifierCtrl},
		{Kind: KeyDown, Modifiers: KeyModifierCtrl},
		{Kind: KeyTab},
		{Kind: KeyRune, Rune: 'é'},
		{Kind: KeyRune, Rune: '界'},
	}
	for index, expected := range want {
		select {
		case got := <-keys:
			if got != expected {
				t.Fatalf("key %d: got %#v want %#v", index, got, expected)
			}
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for key %d", index)
		}
	}
}

func TestReadKeysPreservesEscapeAndDownArrow(t *testing.T) {
	keys := decodeTestKeys([]byte{0x1b, '[', 'B', 0x1b})
	for index, expected := range []Key{{Kind: KeyDown}, {Kind: KeyEscape}} {
		select {
		case got := <-keys:
			if got != expected {
				t.Fatalf("key %d: got %#v want %#v", index, got, expected)
			}
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for key %d", index)
		}
	}
}

func decodeTestKeys(values []byte) <-chan Key {
	keys := make(chan Key, len(values))
	input := make(chan byte, len(values))
	for _, value := range values {
		input <- value
	}
	close(input)
	go decodeKeys(context.Background(), input, keys)
	return keys
}

func TestInlineModeOmitsAlternateScreenCursorAndClearSequences(t *testing.T) {
	start, cleanup := terminalControlSequences(RunOptions{AlternateScreen: false})
	if start != "" || cleanup != "" {
		t.Fatalf("inline controls: start %q cleanup %q", start, cleanup)
	}

	var out bytes.Buffer
	renderer := terminalRenderer{out: &out, noClear: true}
	if err := renderer.draw("inline"); err != nil {
		t.Fatal(err)
	}
	for _, sequence := range []string{"\x1b[?1049h", "\x1b[?1049l", "\x1b[?25l", "\x1b[?25h", "\x1b[2J"} {
		if strings.Contains(out.String(), sequence) {
			t.Fatalf("inline output contains %q: %q", sequence, out.String())
		}
	}
}

func TestInlineRendererRedrawsReservedLines(t *testing.T) {
	var out bytes.Buffer
	renderer := terminalRenderer{out: &out, noClear: true}
	if err := renderer.draw("one\ntwo\nthree"); err != nil {
		t.Fatal(err)
	}
	if err := renderer.draw("next\nframe\nhere"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "\r\x1b[2A\x1b[Jnext") {
		t.Fatalf("inline redraw did not return to its anchor: %q", out.String())
	}
	if strings.Contains(out.String(), "\x1b[H") {
		t.Fatalf("inline redraw moved to screen home: %q", out.String())
	}
}

func TestRunDefaultsPreserveAlternateScreenControls(t *testing.T) {
	start, cleanup := terminalControlSequences(RunOptions{AlternateScreen: true})
	if start != "\x1b[?1049h\x1b[?25l\x1b[?7l" || cleanup != "\x1b[?25h\x1b[?7h\x1b[?1049l" {
		t.Fatalf("alternate screen controls: start %q cleanup %q", start, cleanup)
	}
}

func TestMouseControlsAndWheelSequences(t *testing.T) {
	start, cleanup := terminalControlSequences(RunOptions{Mouse: true})
	if !strings.Contains(start, "\x1b[?1000h") || !strings.Contains(cleanup, "\x1b[?1000l") {
		t.Fatalf("mouse controls: start %q cleanup %q", start, cleanup)
	}
	keys := decodeTestKeys([]byte("\x1b[<64;10;4M\x1b[<65;10;4M"))
	if got := <-keys; got.Kind != KeyMouseUp {
		t.Fatalf("wheel up: %#v", got)
	}
	if got := <-keys; got.Kind != KeyMouseDown {
		t.Fatalf("wheel down: %#v", got)
	}
}

func TestDrawUsesCarriageReturnNewlinesForRawTerminals(t *testing.T) {
	var out bytes.Buffer
	if err := draw(&out, "one\ntwo"); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(out.Bytes(), []byte("one\r\ntwo")) {
		t.Fatalf("draw output %q", out.String())
	}
}

func TestTerminalRendererClearsOnlyInitialFrame(t *testing.T) {
	var out bytes.Buffer
	renderer := terminalRenderer{out: &out}
	if err := renderer.draw("one\ntwo"); err != nil {
		t.Fatal(err)
	}
	if err := renderer.draw("three\nfour"); err != nil {
		t.Fatal(err)
	}

	want := "\x1b[H\x1b[2Jone\r\ntwo\x1b[0m\x1b[Hthree\r\nfour\x1b[0m"
	if got := out.String(); got != want {
		t.Fatalf("draw sequence %q, want %q", got, want)
	}
}

func TestTerminalRendererSkipsIdenticalFrames(t *testing.T) {
	var out bytes.Buffer
	renderer := terminalRenderer{out: &out}
	if err := renderer.draw("same\n"); err != nil {
		t.Fatal(err)
	}
	first := out.String()
	if err := renderer.draw("same"); err != nil {
		t.Fatal(err)
	}

	if got := out.String(); got != first {
		t.Fatalf("identical frame emitted output: got %q want %q", got, first)
	}
}

func TestTerminalRendererClearsAfterResizeReset(t *testing.T) {
	var out bytes.Buffer
	renderer := terminalRenderer{out: &out}
	if err := renderer.draw("large"); err != nil {
		t.Fatal(err)
	}
	renderer.reset()
	if err := renderer.draw("small"); err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(out.String(), "\x1b[2J"); got != 2 {
		t.Fatalf("clear count %d, want 2", got)
	}
}
