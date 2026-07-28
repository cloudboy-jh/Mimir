package bentotui

import (
	"strings"
	"testing"
)

func TestViewportBoxHasFixedBoundsAndScrollPosition(t *testing.T) {
	lines := []string{"one", "two", "three", "four", "five", "six"}
	view := ViewportBox(Context{Width: 40, Theme: Mimir}, ViewportBoxOptions{
		Title: "Mimir deploy", Right: "running · 4s", Footer: "↑/↓ scroll", Lines: lines, Offset: 2, Height: 8,
	})
	rendered := strings.Split(view, "\n")
	if len(rendered) != 8 {
		t.Fatalf("height %d:\n%s", len(rendered), view)
	}
	for _, line := range rendered {
		if width := VisibleWidth(line); width != 40 {
			t.Fatalf("width %d, want 40: %q", width, line)
		}
	}
	for _, expected := range []string{"Mimir deploy", "running · 4s", "three", "six", "3–6/6", "↑/↓ scroll"} {
		if !strings.Contains(view, expected) {
			t.Fatalf("missing %q:\n%s", expected, view)
		}
	}
	if strings.Contains(view, "one") || strings.Contains(view, "two") {
		t.Fatalf("viewport did not scroll:\n%s", view)
	}
}

func TestViewportBoxCapsWideTerminals(t *testing.T) {
	view := ViewportBox(Context{Width: 180, Theme: Mimir}, ViewportBoxOptions{Title: "Update", Height: 6})
	for _, line := range strings.Split(view, "\n") {
		if width := VisibleWidth(line); width != 110 {
			t.Fatalf("width %d, want cap 110", width)
		}
	}
}
