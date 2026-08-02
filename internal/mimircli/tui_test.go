package mimircli

import (
	"context"
	"io"
	"strings"
	"testing"
)

func TestTUIRejectsArguments(t *testing.T) {
	err := cmdTUI(context.Background(), []string{"extra"}, IO{In: strings.NewReader(""), Out: io.Discard})
	if err == nil || err.Error() != "usage: mimir tui" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTUIRequiresTerminal(t *testing.T) {
	err := cmdTUI(context.Background(), nil, IO{In: strings.NewReader(""), Out: io.Discard})
	if err == nil || !strings.Contains(err.Error(), "interactive terminal") {
		t.Fatalf("unexpected error: %v", err)
	}
}
