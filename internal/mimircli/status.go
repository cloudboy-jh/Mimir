package mimircli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"time"
)

type sessionStatus struct {
	SessionID        string  `json:"session_id"`
	Outcome          string  `json:"outcome"`
	OutcomeSource    *string `json:"outcome_src"`
	OutcomeUpdatedAt *string `json:"outcome_updated_at"`
	OutcomeReason    *string `json:"outcome_reason"`
	DashboardURL     *string `json:"dashboard_url"`
	Capture          struct {
		Status           string  `json:"status"`
		SavedExchanges   int     `json:"saved_exchanges"`
		FailedExchanges  int     `json:"failed_exchanges"`
		PendingExchanges int     `json:"pending_exchanges"`
		LastSavedAt      *string `json:"last_saved_at"`
	} `json:"capture"`
	Receipt struct {
		Label       string  `json:"label"`
		Detail      string  `json:"detail"`
		ActionLabel *string `json:"action_label"`
	} `json:"receipt"`
	raw map[string]any
}

var sessionStatusPollSchedule = []time.Duration{250 * time.Millisecond, 500 * time.Millisecond, time.Second, 2 * time.Second}

func getSessionStatus(ctx context.Context, id string) (sessionStatus, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return getSessionStatusWithSchedule(ctx, id, sessionStatusPollSchedule)
}

func getSessionStatusWithSchedule(ctx context.Context, id string, schedule []time.Duration) (sessionStatus, error) {
	var latest sessionStatus
	var firstErr error
	haveStatus := false
	initialTotal := 0
	var initialLastSaved *string
	observedPending, recentSave := false, false
	for attempt := 0; attempt <= len(schedule); attempt++ {
		if attempt > 0 {
			timer := time.NewTimer(schedule[attempt-1])
			select {
			case <-ctx.Done():
				timer.Stop()
				return sessionStatus{}, ctx.Err()
			case <-timer.C:
			}
		}
		next, err := readSessionStatus(ctx, id)
		if err != nil {
			if !haveStatus && isNotFound(err) {
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			return sessionStatus{}, err
		}
		latest = normalizeSessionStatus(next)
		if !haveStatus {
			haveStatus = true
			initialTotal = captureTotal(latest)
			initialLastSaved = latest.Capture.LastSavedAt
			observedPending = latest.Capture.PendingExchanges > 0
			recentSave = initialLastSaved != nil && recentlySaved(*initialLastSaved)
			continue
		}
		observedPending = observedPending || latest.Capture.PendingExchanges > 0
		changed := captureTotal(latest) != initialTotal || !sameStringPointer(latest.Capture.LastSavedAt, initialLastSaved)
		if latest.Capture.PendingExchanges == 0 && (observedPending || changed || recentSave) {
			return latest, nil
		}
	}
	if haveStatus {
		return latest, nil
	}
	return sessionStatus{}, firstErr
}

func isNotFound(err error) bool {
	apiErr, ok := err.(*apiError)
	return ok && apiErr.StatusCode == 404
}

func captureTotal(status sessionStatus) int {
	return status.Capture.SavedExchanges + status.Capture.FailedExchanges + status.Capture.PendingExchanges
}

func recentlySaved(value string) bool {
	savedAt, err := time.Parse(time.RFC3339, value)
	return err == nil && time.Since(savedAt) >= 0 && time.Since(savedAt) <= 30*time.Second
}

func sameStringPointer(left, right *string) bool {
	if left == nil || right == nil {
		return left == right
	}
	return *left == *right
}

func readSessionStatus(ctx context.Context, id string) (sessionStatus, error) {
	data, err := remoteRequest(ctx, "GET", "/sessions/"+url.PathEscape(id)+"/status", nil)
	if err != nil {
		return sessionStatus{}, err
	}
	var status sessionStatus
	if err := json.Unmarshal(data, &status); err != nil {
		return sessionStatus{}, fmt.Errorf("decoding session status: %w", err)
	}
	if err := json.Unmarshal(data, &status.raw); err != nil {
		return sessionStatus{}, fmt.Errorf("decoding raw session status: %w", err)
	}
	return status, nil
}

func normalizeSessionStatus(status sessionStatus) sessionStatus {
	if status.Receipt.Label == "" {
		total := captureTotal(status)
		switch {
		case status.Capture.PendingExchanges > 0 && status.Capture.FailedExchanges > 0:
			status.Receipt.Label = "Partially saved"
			status.Receipt.Detail = fmt.Sprintf("%d saved · %d failed · %d pending", status.Capture.SavedExchanges, status.Capture.FailedExchanges, status.Capture.PendingExchanges)
		case status.Capture.PendingExchanges > 0:
			status.Receipt.Label = "Saving to Mimir..."
			status.Receipt.Detail = exchangeCount(total)
		case status.Capture.SavedExchanges > 0 && status.Capture.FailedExchanges > 0:
			status.Receipt.Label = "Partially saved"
			status.Receipt.Detail = fmt.Sprintf("%d of %d exchanges", status.Capture.SavedExchanges, total)
		case status.Capture.FailedExchanges > 0:
			status.Receipt.Label = "Mimir couldn't save this session"
			status.Receipt.Detail = exchangeCount(status.Capture.FailedExchanges)
		case status.Capture.SavedExchanges > 0:
			status.Receipt.Label = "Saved to Mimir"
			status.Receipt.Detail = exchangeCount(status.Capture.SavedExchanges) + " in this session"
		default:
			status.Receipt.Label = "Not captured"
			status.Receipt.Detail = "No exchanges in this session"
		}
	}
	return status
}

func exchangeCount(count int) string {
	label := "exchanges"
	if count == 1 {
		label = "exchange"
	}
	return fmt.Sprintf("%d %s", count, label)
}

func sessionStatusJSON(status sessionStatus) ([]byte, error) {
	result := make(map[string]any, len(status.raw)+2)
	for key, value := range status.raw {
		result[key] = value
	}
	receipt, _ := result["receipt"].(map[string]any)
	if receipt == nil {
		receipt = map[string]any{}
	}
	receipt["label"] = status.Receipt.Label
	receipt["detail"] = status.Receipt.Detail
	receipt["action_label"] = status.Receipt.ActionLabel
	result["receipt"] = receipt
	result["dashboard_url"] = status.DashboardURL
	return json.Marshal(result)
}

func receiptText(status sessionStatus) string {
	text := receiptSummary(status)
	if status.DashboardURL != nil {
		label := "View session"
		if status.Receipt.ActionLabel != nil && *status.Receipt.ActionLabel != "" {
			label = *status.Receipt.ActionLabel
		}
		text += " · [" + label + "](" + *status.DashboardURL + ")"
	}
	return text
}

func receiptSummary(status sessionStatus) string {
	text := status.Receipt.Label
	if status.Receipt.Detail != "" {
		text += " · " + status.Receipt.Detail
	}
	return text
}
