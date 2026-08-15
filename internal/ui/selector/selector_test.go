package selector

import (
	"bytes"
	"strings"
	"testing"

	"github.com/cloudboy-jh/mimir/internal/ui/bentotui"
)

func TestRunMovesAndTogglesSelection(t *testing.T) {
	items := []Item{{Label: "OpenCode", Selected: true}, {Label: "Pi", Selected: true}, {Label: "Oh My Pi"}}
	input := strings.NewReader("\x1b[B\x1b[B \r")
	var output bytes.Buffer
	result, err := run(input, &output, "Mimir harnesses", items, testContext(80, false))
	if err != nil {
		t.Fatal(err)
	}
	if !result.Accepted || !result.Selected[0] || !result.Selected[1] || !result.Selected[2] {
		t.Fatalf("result = %#v", result)
	}
	if text := output.String(); !strings.Contains(text, "○ Oh My Pi") || !strings.Contains(text, "● Oh My Pi") {
		t.Fatalf("output = %q", text)
	}
}

func TestRunCancelPreservesOriginalSelection(t *testing.T) {
	items := []Item{{Label: "OpenCode", Selected: true}, {Label: "Pi"}}
	var output bytes.Buffer
	result, err := run(strings.NewReader(" q"), &output, "Mimir harnesses", items, testContext(80, false))
	if err != nil {
		t.Fatal(err)
	}
	if result.Accepted || !result.Selected[0] || result.Selected[1] {
		t.Fatalf("result = %#v", result)
	}
}

func TestRunTruncatesRowsToTerminalWidth(t *testing.T) {
	items := []Item{{Label: "OpenCode with a status that cannot fit on one row", Selected: true}}
	var output bytes.Buffer
	if _, err := run(strings.NewReader("\r"), &output, "Mimir harnesses", items, testContext(24, false)); err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(output.String(), "\n") {
		line = strings.TrimPrefix(line, "\r\x1b[2K")
		if width := bentotui.VisibleWidth(line); width > 24 {
			t.Fatalf("line width = %d: %q", width, line)
		}
	}
}

func TestRunUsesMimirColorsWhenEnabled(t *testing.T) {
	items := []Item{{Label: "OpenCode", Selected: true}}
	var output bytes.Buffer
	if _, err := run(strings.NewReader("\r"), &output, "Mimir harnesses", items, testContext(80, true)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "\x1b[") {
		t.Fatalf("output has no ANSI color: %q", output.String())
	}
}

func testContext(width int, color bool) bentotui.Context {
	return bentotui.Context{Width: width, Color: color, Theme: bentotui.Mimir}
}
