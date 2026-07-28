package ui

import (
	"bytes"
	"strings"
	"testing"

	"github.com/cloudboy-jh/mimir/internal/ui/bentotui"
)

func TestRendererUsesWidthOverride(t *testing.T) {
	t.Setenv("COLUMNS", "48")
	render := New(&bytes.Buffer{})
	if render.Width != 48 || render.Color {
		t.Fatalf("renderer = %#v", render)
	}
	if !bentotui.FitsWidth(render.Card("Status", bentotui.Field{Label: "Endpoint", Value: strings.Repeat("https://mimir.example/", 8)}), 48) {
		t.Fatal("card exceeded configured width")
	}
}

func TestMimirComponentsRemainBounded(t *testing.T) {
	for _, width := range []int{40, 80, 120} {
		render := Renderer{Width: width, Theme: bentotui.Mimir}
		blocks := []string{
			render.Session(SessionItem{
				Title:    "Investigate a very long agent session title with Unicode 界 evidence",
				Outcome:  "landed",
				Capture:  "14 saved · 1 failed",
				Metadata: "2026-07-27 22:10 · github.com/cloudboy-jh/mimir · openai/gpt-5.6",
				ID:       "session-with-a-very-long-machine-identifier",
				Excerpt:  strings.Repeat("Request evidence needs to wrap. ", 8),
			}),
			render.Commands([]CommandItem{{Usage: "mimir session end <id> [--outcome landed]", Description: strings.Repeat("Finalize capture and record evidence. ", 5)}}),
			render.KeyValues("Connection", bentotui.Field{Label: "Worker", Value: strings.Repeat("https://mimir.example/", 8)}),
		}
		for _, block := range blocks {
			if !bentotui.FitsWidth(block, width) {
				t.Fatalf("width %d overflow:\n%s", width, block)
			}
		}
	}
}

func TestSemanticBadgesRemainTextualWithoutColor(t *testing.T) {
	render := Renderer{Width: 80, Theme: bentotui.Mimir}
	for value, want := range map[string]string{"landed": "[LANDED]", "failed": "[FAILED]", "pending": "[PENDING]"} {
		got := render.OutcomeBadge(value)
		if value == "failed" || value == "pending" {
			got = render.CaptureBadge(value)
		}
		if got != want {
			t.Fatalf("badge %q = %q, want %q", value, got, want)
		}
	}
}
