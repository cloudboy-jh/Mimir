package bentotui

import (
	"strings"
	"testing"
)

func TestRenderersRemainStructuredWithoutColor(t *testing.T) {
	if got := Badge(Mimir, false, "PASS", VariantSuccess); got != "[PASS]" {
		t.Fatalf("badge = %q", got)
	}
	card := Card(Mimir, false, "Ready", []Field{{Label: "Worker", Value: "https://mimir.example"}})
	if !strings.Contains(card, "┌─ Ready") || !strings.Contains(card, "│ Worker") || !strings.Contains(card, "└") {
		t.Fatalf("card = %q", card)
	}
	rows := RenderRows(Mimir, false, []Row{{Primary: "Worker", Secondary: "reachable", Tone: ToneSuccess}})
	if rows != "  [✓] Worker\n      reachable" {
		t.Fatalf("rows = %q", rows)
	}
}

func TestStyleUsesTrueColorOnlyWhenEnabled(t *testing.T) {
	plain := Style{Color: Mimir.Accent}.Render("mimir")
	colored := Style{Color: Mimir.Accent, Enabled: true}.Render("mimir")
	if plain != "mimir" || !strings.Contains(colored, "\x1b[38;2;126;192;164m") || !strings.HasSuffix(colored, "\x1b[0m") {
		t.Fatalf("plain=%q colored=%q", plain, colored)
	}
}
