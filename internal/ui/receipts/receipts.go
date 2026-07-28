package receiptui

import (
	"fmt"
	"io"
	"strings"
	"time"

	domainsessions "github.com/cloudboy-jh/mimir/internal/sessions"
	"github.com/cloudboy-jh/mimir/internal/ui/appframe"
)

func Render(out io.Writer, receipts []domainsessions.Receipt, limit int) error {
	if limit > 0 && len(receipts) > limit {
		receipts = receipts[:limit]
	}
	render := appframe.New(out)
	if len(receipts) == 0 {
		_, err := fmt.Fprintln(out, render.EmptyState("No sessions found", "Captured model traffic will appear here as work sessions."))
		return err
	}
	if _, err := fmt.Fprintf(out, "%s\n\n", render.Heading("Sessions")); err != nil {
		return err
	}
	for index, receipt := range receipts {
		title := receipt.ID
		if receipt.Intent != nil && strings.TrimSpace(*receipt.Intent) != "" {
			title = strings.TrimSpace(*receipt.Intent)
		}
		capture := domainsessions.ExchangeCount(receipt.Capture.SavedExchanges) + " saved"
		if receipt.Capture.FailedExchanges > 0 && receipt.Capture.PendingExchanges > 0 {
			capture = fmt.Sprintf("%d saved · %d failed · %d pending", receipt.Capture.SavedExchanges, receipt.Capture.FailedExchanges, receipt.Capture.PendingExchanges)
		} else if receipt.Capture.PendingExchanges > 0 {
			capture = "saving"
		} else if receipt.Capture.FailedExchanges > 0 {
			capture = fmt.Sprintf("%d saved · %d failed", receipt.Capture.SavedExchanges, receipt.Capture.FailedExchanges)
		}
		fmt.Fprintln(out, render.Session(appframe.SessionItem{
			Title: title, Outcome: fallback(receipt.Outcome, "unresolved"), Capture: capture,
			Metadata: strings.Join([]string{displayTime(receipt.StartedAt), fallback(pointerValue(receipt.Repo), "No repository"), fallback(pointerValue(receipt.Model), "unknown model")}, " · "), ID: receipt.ID,
		}))
		if index < len(receipts)-1 {
			fmt.Fprintln(out)
		}
	}
	return nil
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

func fallback(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
