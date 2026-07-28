package operations

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestProgressUsesStableLineFallback(t *testing.T) {
	var out bytes.Buffer
	progress := Start(context.Background(), nil, &out, "Mimir setup", []string{"Preparing"}, func() {})
	progress.Complete("Prepared")
	progress.Stop()
	text := out.String()
	if strings.Contains(text, "\x1b") || !strings.Contains(text, "Mimir setup") || !strings.Contains(text, "[✓] Prepared") {
		t.Fatalf("output %q", text)
	}
}
