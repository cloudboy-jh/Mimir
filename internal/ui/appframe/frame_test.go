package appframe

import (
	"bytes"
	"strings"
	"testing"

	"github.com/cloudboy-jh/mimir/internal/ui/bentotui"
)

func TestInteractiveRejectsNonTerminalStreams(t *testing.T) {
	if Interactive(bytes.NewBuffer(nil), &bytes.Buffer{}) {
		t.Fatal("buffers must use static output")
	}
}

func TestFrameUsesAnchoredGlobalDimensions(t *testing.T) {
	view, _ := (Frame{Surface: "Deploy", Status: "running", Lines: []string{"working"}, Footer: "^C Cancel"}).Render(
		bentotui.Context{Theme: bentotui.Mimir}, bentotui.Screen{Width: 140, Height: 50},
	)
	lines := strings.Split(view, "\n")
	if len(lines) != PreferredHeight {
		t.Fatalf("height %d", len(lines))
	}
	for _, line := range lines {
		if width := bentotui.VisibleWidth(line); width != PreferredWidth {
			t.Fatalf("width %d: %q", width, line)
		}
		if strings.HasPrefix(line, " ") {
			t.Fatalf("frame was indented: %q", line)
		}
	}
}

func TestFrameShrinksWithoutCentering(t *testing.T) {
	view, _ := (Frame{Surface: "Sessions"}).Render(bentotui.Context{Theme: bentotui.Mimir}, bentotui.Screen{Width: 52, Height: 12})
	lines := strings.Split(view, "\n")
	if len(lines) != 12 || bentotui.VisibleWidth(lines[0]) != 52 || !strings.HasPrefix(lines[0], "┌") {
		t.Fatalf("unexpected frame:\n%s", view)
	}
}

func TestLayoutUsesGlobalMaximumAndMinimumFallbackBoundary(t *testing.T) {
	layout := ForScreen(bentotui.Screen{Width: 200, Height: 80})
	if layout.Width != 80 || layout.Height != 20 || layout.BodyHeight != 16 {
		t.Fatalf("layout %#v", layout)
	}
	small := ForScreen(bentotui.Screen{Width: 48, Height: 12})
	if small.Width != 48 || small.Height != 12 || small.BodyHeight != 8 {
		t.Fatalf("small layout %#v", small)
	}
}
