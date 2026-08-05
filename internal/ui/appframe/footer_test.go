package appframe

import (
	"strings"
	"testing"

	"github.com/cloudboy-jh/mimir/internal/ui/bentotui"
)

func TestFooterKeepsPrimaryAndExitBindings(t *testing.T) {
	footer := Footer(bentotui.Context{Width: 80, Theme: bentotui.Mimir},
		[]Binding{{Key: "↑↓", Label: "Scroll"}, {Key: "F", Label: "Follow"}},
		[]Binding{{Key: "?", Label: "Help"}, {Key: "^C", Label: "Cancel"}},
	)
	for _, value := range []string{"↑↓ Scroll", "F Follow", "? Help", "^C Cancel"} {
		if !strings.Contains(footer, value) {
			t.Fatalf("missing %q in %q", value, footer)
		}
	}
}

func TestFooterShowsAtMostFourBindings(t *testing.T) {
	footer := Footer(bentotui.Context{Width: 80, Theme: bentotui.Mimir},
		[]Binding{{Key: "↑↓", Label: "Navigate"}, {Key: "↵", Label: "Open"}, {Key: "/", Label: "Filter"}},
		[]Binding{{Key: "?", Label: "Help"}, {Key: "q", Label: "Quit"}},
	)
	if strings.Contains(footer, "/ Filter") || !strings.Contains(footer, "q Quit") {
		t.Fatalf("unexpected binding priority %q", footer)
	}
}

func TestFooterStatusKeepsActionsAndShowsRuntimeState(t *testing.T) {
	footer := FooterStatus(bentotui.Context{Width: 120, Theme: bentotui.Mimir},
		[]Binding{{Key: "↑↓", Label: "Browse"}, {Key: "↵", Label: "Details"}},
		"3/25 · LANDED · Mimir ready · anthropic/sonnet",
		[]Binding{{Key: "/", Label: "Filter"}, {Key: "Tab", Label: "Ask"}},
	)
	for _, value := range []string{"↑↓ Browse", "↵ Details", "3/25 · LANDED", "/ Filter", "Tab Ask"} {
		if !strings.Contains(footer, value) {
			t.Fatalf("missing %q in %q", value, footer)
		}
	}
}
