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
