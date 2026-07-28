package bentotui

import (
	"fmt"
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

func TestVisibleWidthAndWidthPrimitives(t *testing.T) {
	colored := Style{Color: Mimir.Accent, Enabled: true}.Render("界e\u0301")
	if got := VisibleWidth(colored); got != 3 {
		t.Fatalf("VisibleWidth = %d", got)
	}
	if got := VisibleWidth("a\tb"); got != 10 {
		t.Fatalf("tab VisibleWidth = %d", got)
	}
	if got := PadLeft(colored, 5); VisibleWidth(got) != 5 || !strings.HasPrefix(got, "  ") {
		t.Fatalf("PadLeft = %q (%d)", got, VisibleWidth(got))
	}
	if got := PadRight(colored, 5); VisibleWidth(got) != 5 {
		t.Fatalf("PadRight = %q (%d)", got, VisibleWidth(got))
	}
	if got := Truncate("ab界cd", 5); got != "ab界…" || VisibleWidth(got) != 5 {
		t.Fatalf("Truncate = %q (%d)", got, VisibleWidth(got))
	}
	wrapped := Wrap("alpha beta界gamma\nlast", 8)
	if strings.Join(wrapped, "|") != "alpha|beta界ga|mma|last" {
		t.Fatalf("Wrap = %#v", wrapped)
	}
	if got := LineWidths("a\n界\n"); fmt.Sprint(got) != "[1 2 0]" {
		t.Fatalf("LineWidths = %v", got)
	}
	if got := Wrap("\n", 1); len(got) != 2 || got[0] != "" || got[1] != "" {
		t.Fatalf("trailing newline = %#v", got)
	}
	if got := Wrap("界", 1); len(got) != 1 || got[0] != "…" {
		t.Fatalf("narrow wide-rune wrap = %#v", got)
	}
	preserved := WrapPreserve("\t  alpha beta gamma", 12)
	if strings.Join(preserved, "") != "\t  alpha beta gamma" {
		t.Fatalf("WrapPreserve changed evidence: %#v", preserved)
	}
	for _, line := range preserved {
		if VisibleWidth(line) > 12 {
			t.Fatalf("WrapPreserve overflow: %q", line)
		}
	}
}

func TestTruncateClosesOSC8Hyperlink(t *testing.T) {
	linked := "\x1b]8;;https://example.com\x1b\\a very long link\x1b]8;;\x1b\\"
	got := Truncate(linked, 7)
	if !strings.HasSuffix(got, "\x1b]8;;\x1b\\") {
		t.Fatalf("truncated hyperlink was not closed: %q", got)
	}
	if VisibleWidth(got) != 7 {
		t.Fatalf("width = %d, want 7", VisibleWidth(got))
	}
}

func TestCardContextCompleteAndBounded(t *testing.T) {
	for _, width := range []int{40, 80, 120} {
		for _, color := range []bool{false, true} {
			ctx := Context{Width: width, Color: color, Theme: Mimir}
			got := CardContext(ctx, "Status 界 and a title that can become extremely long", []Field{
				{Label: "Worker endpoint", Value: "https://mimir.example/a/very/long/path\nsecond line with e\u0301 and 界界"},
				{Label: "State", Value: "ready"},
			})
			assertBounded(t, got, width)
			lines := strings.Split(got, "\n")
			if VisibleWidth(lines[0]) != width || VisibleWidth(lines[len(lines)-1]) != width || !strings.Contains(lines[0], "┐") || !strings.Contains(lines[len(lines)-1], "┘") {
				t.Fatalf("width %d incomplete card:\n%s", width, got)
			}
			for _, line := range lines[1 : len(lines)-1] {
				if VisibleWidth(line) != width || !strings.Contains(line, "│") {
					t.Fatalf("width %d malformed content line %q", width, line)
				}
			}
		}
	}
}

func TestRowsAndCompositionNeverOverflow(t *testing.T) {
	for _, width := range []int{40, 80, 120} {
		for _, color := range []bool{false, true} {
			ctx := Context{Width: width, Color: color, Theme: Mimir}
			rows := RenderRowsContext(ctx, []Row{{
				Primary:   "A long Unicode 界 worker description that wraps rather than colliding",
				Secondary: "reachable\nwith another detailed line", RightStat: "123 requests", Tone: ToneSuccess,
			}})
			parts := []string{
				rows,
				Section(ctx, "Overview", "A long body with multiline\ncontent that must wrap cleanly at terminal boundaries."),
				Divider(ctx, "Details"), KeyValue(ctx, "Long identifier", "mimir/界/"+strings.Repeat("segment-", 20)),
				Callout(ctx, ToneWarn, "Attention", strings.Repeat("Long warning text ", 15)),
				EmptyState(ctx, "No sessions", strings.Repeat("Nothing has appeared yet. ", 12)),
				ActionHint(ctx, "enter", strings.Repeat("open selected session ", 10)),
				Table(ctx, []string{"Name", "Endpoint", "State"}, [][]string{{"worker 界", strings.Repeat("https://example/", 9), "healthy"}}),
			}
			got := Inset(ctx, Stack(parts...), 0, 0)
			assertBounded(t, got, width)
		}
	}
}

func TestRenderRowsContextAlignsRightStats(t *testing.T) {
	got := RenderRowsContext(Context{Width: 40, Theme: Mimir}, []Row{
		{Primary: "short", RightStat: "1ms"},
		{Primary: "longer name", RightStat: "20ms and an excessively long statistic that must truncate"},
	})
	for _, line := range strings.Split(got, "\n") {
		if VisibleWidth(line) != 40 {
			t.Fatalf("row is not aligned: %q width=%d", line, VisibleWidth(line))
		}
	}
}

func assertBounded(t *testing.T, value string, width int) {
	t.Helper()
	for line, got := range LineWidths(value) {
		if got > width {
			t.Fatalf("line %d width %d exceeds %d: %q", line+1, got, width, strings.Split(value, "\n")[line])
		}
	}
}

func TestStyleUsesTrueColorOnlyWhenEnabled(t *testing.T) {
	plain := Style{Color: Mimir.Accent}.Render("mimir")
	colored := Style{Color: Mimir.Accent, Enabled: true}.Render("mimir")
	if plain != "mimir" || !strings.Contains(colored, "\x1b[38;2;126;192;164m") || !strings.HasSuffix(colored, "\x1b[0m") {
		t.Fatalf("plain=%q colored=%q", plain, colored)
	}
}
