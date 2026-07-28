package sessionui

import (
	"strings"
	"testing"

	domainsessions "github.com/cloudboy-jh/mimir/internal/sessions"
)

func TestItemsBuildDashboardLinksAndCaptureLabels(t *testing.T) {
	receipt := domainsessions.Receipt{ID: "session/id", Outcome: "landed", StartedAt: "2026-07-28T12:00:00Z"}
	receipt.Capture.SavedExchanges = 3
	items := Items([]domainsessions.Receipt{receipt}, "https://mimir.example", 20)
	if len(items) != 1 || items[0].Capture != "3 exchanges saved" || !strings.Contains(items[0].DashboardURL, "session%2Fid") {
		t.Fatalf("items %#v", items)
	}
}

func TestItemsPreserveMixedCaptureState(t *testing.T) {
	receipt := domainsessions.Receipt{ID: "session-1"}
	receipt.Capture.SavedExchanges, receipt.Capture.FailedExchanges, receipt.Capture.PendingExchanges = 12, 2, 1
	items := Items([]domainsessions.Receipt{receipt}, "", 20)
	if items[0].Capture != "12 saved · 2 failed · 1 pending" {
		t.Fatalf("capture %q", items[0].Capture)
	}
}
