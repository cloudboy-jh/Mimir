package sessionui

import (
	"fmt"
	"net/url"
	"strings"
	"time"

	domainsessions "github.com/cloudboy-jh/mimir/internal/sessions"
)

func Items(values []domainsessions.Receipt, dashboardBaseURL string, limit int) []BrowserSession {
	if limit > 0 && len(values) > limit {
		values = values[:limit]
	}
	items := make([]BrowserSession, 0, len(values))
	for _, receipt := range values {
		title := domainsessions.DisplayTitle(receipt.DisplayTitle, receipt.Title, receipt.Intent)
		dashboardURL := ""
		if dashboardBaseURL != "" {
			dashboardURL = strings.TrimRight(dashboardBaseURL, "/") + "/dashboard/sessions/" + url.PathEscape(receipt.ID)
		}
		items = append(items, BrowserSession{
			Title: title, Outcome: fallbackValue(receipt.Outcome, "unresolved"), Capture: captureLabel(receipt), Started: displayTime(receipt.StartedAt),
			Repo: pointerValue(receipt.Repo), Model: pointerValue(receipt.Model), Harness: pointerValue(receipt.Harness), Tokens: receipt.TokensIn + receipt.TokensOut,
			ID: receipt.ID, DashboardURL: dashboardURL,
		})
	}
	return items
}

func captureLabel(receipt domainsessions.Receipt) string {
	switch {
	case receipt.Capture.FailedExchanges > 0 && (receipt.Capture.SavedExchanges > 0 || receipt.Capture.PendingExchanges > 0):
		label := fmt.Sprintf("%d saved · %d failed", receipt.Capture.SavedExchanges, receipt.Capture.FailedExchanges)
		if receipt.Capture.PendingExchanges > 0 {
			label += fmt.Sprintf(" · %d pending", receipt.Capture.PendingExchanges)
		}
		return label
	case receipt.Capture.PendingExchanges > 0:
		return "saving"
	case receipt.Capture.FailedExchanges > 0:
		return "capture failed"
	case receipt.Capture.SavedExchanges > 0:
		return domainsessions.ExchangeCount(receipt.Capture.SavedExchanges) + " saved"
	default:
		return "not captured"
	}
}

func displayTime(value string) string {
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed.Local().Format("2006-01-02 15:04")
	}
	return value
}

func pointerValue(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}

func fallbackValue(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
