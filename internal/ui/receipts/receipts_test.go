package receiptui

import (
	"bytes"
	"strings"
	"testing"

	domainsessions "github.com/cloudboy-jh/mimir/internal/sessions"
)

func TestRenderStaticReceipt(t *testing.T) {
	title := "Readable session title"
	receipt := domainsessions.Receipt{ID: "session-1", Outcome: "landed", StartedAt: "2026-07-28T12:00:00Z", Title: &title}
	receipt.Capture.SavedExchanges = 1
	var out bytes.Buffer
	if err := Render(&out, []domainsessions.Receipt{receipt}, 20); err != nil {
		t.Fatal(err)
	}
	if text := out.String(); !strings.Contains(text, "[LANDED] Readable session title") || !strings.Contains(text, "1 exchange saved") {
		t.Fatalf("output %q", text)
	}
}

func TestRenderPreservesMixedCaptureState(t *testing.T) {
	receipt := domainsessions.Receipt{ID: "session-1"}
	receipt.Capture.SavedExchanges, receipt.Capture.FailedExchanges, receipt.Capture.PendingExchanges = 12, 2, 1
	var out bytes.Buffer
	if err := Render(&out, []domainsessions.Receipt{receipt}, 20); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "12 saved · 2 failed · 1 pending") {
		t.Fatalf("output %q", out.String())
	}
}
