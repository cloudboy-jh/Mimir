package sessions

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/cloudboy-jh/mimir/internal/mimirapi"
)

type Requester interface {
	Request(context.Context, string, string, any) ([]byte, error)
}

type Service struct {
	API          Requester
	PollSchedule []time.Duration
}

var defaultPollSchedule = []time.Duration{250 * time.Millisecond, 500 * time.Millisecond, time.Second, 2 * time.Second}

func New(api Requester) Service {
	return Service{API: api, PollSchedule: defaultPollSchedule}
}

type Status struct {
	SessionID        string  `json:"session_id"`
	State            string  `json:"state"`
	EndedAt          *string `json:"ended_at"`
	InactiveAt       *string `json:"inactive_at"`
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
	raw map[string]json.RawMessage
}

func (s Service) GetStatus(ctx context.Context, id string) (Status, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return s.GetStatusWithSchedule(ctx, id, s.PollSchedule)
}

func (s Service) GetStatusWithSchedule(ctx context.Context, id string, schedule []time.Duration) (Status, error) {
	var latest Status
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
				return Status{}, ctx.Err()
			case <-timer.C:
			}
		}
		next, err := s.ReadStatus(ctx, id)
		if err != nil {
			if !haveStatus && isNotFound(err) {
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			return Status{}, err
		}
		latest = NormalizeStatus(next)
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
	return Status{}, firstErr
}

func (s Service) ReadStatus(ctx context.Context, id string) (Status, error) {
	data, err := s.API.Request(ctx, "GET", "/sessions/"+url.PathEscape(id)+"/status", nil)
	if err != nil {
		return Status{}, err
	}
	var status Status
	if err := json.Unmarshal(data, &status); err != nil {
		return Status{}, fmt.Errorf("decoding session status: %w", err)
	}
	if err := json.Unmarshal(data, &status.raw); err != nil {
		return Status{}, fmt.Errorf("decoding raw session status: %w", err)
	}
	return status, nil
}

func isNotFound(err error) bool {
	var apiErr *mimirapi.Error
	return errors.As(err, &apiErr) && apiErr.StatusCode == 404
}

func captureTotal(status Status) int {
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

func NormalizeStatus(status Status) Status {
	if status.Receipt.Label != "" {
		return status
	}
	total := captureTotal(status)
	switch {
	case status.Capture.PendingExchanges > 0 && status.Capture.FailedExchanges > 0:
		status.Receipt.Label = "Partially saved"
		status.Receipt.Detail = fmt.Sprintf("%d saved · %d failed · %d pending", status.Capture.SavedExchanges, status.Capture.FailedExchanges, status.Capture.PendingExchanges)
	case status.Capture.PendingExchanges > 0:
		status.Receipt.Label = "Saving to Mimir..."
		status.Receipt.Detail = ExchangeCount(total)
	case status.Capture.SavedExchanges > 0 && status.Capture.FailedExchanges > 0:
		status.Receipt.Label = "Partially saved"
		status.Receipt.Detail = fmt.Sprintf("%d of %d exchanges", status.Capture.SavedExchanges, total)
	case status.Capture.FailedExchanges > 0:
		status.Receipt.Label = "Mimir couldn't save this session"
		status.Receipt.Detail = ExchangeCount(status.Capture.FailedExchanges)
	case status.Capture.SavedExchanges > 0:
		status.Receipt.Label = "Saved to Mimir"
		status.Receipt.Detail = ExchangeCount(status.Capture.SavedExchanges) + " in this session"
	default:
		status.Receipt.Label = "Not captured"
		status.Receipt.Detail = "No exchanges in this session"
	}
	return status
}

func StatusJSON(status Status) ([]byte, error) {
	result := make(map[string]json.RawMessage, len(status.raw)+2)
	for key, value := range status.raw {
		result[key] = value
	}
	receipt := map[string]json.RawMessage{}
	if raw := result["receipt"]; len(raw) > 0 && string(raw) != "null" {
		if err := json.Unmarshal(raw, &receipt); err != nil {
			return nil, fmt.Errorf("decoding raw session receipt: %w", err)
		}
	}
	receipt["label"], _ = json.Marshal(status.Receipt.Label)
	receipt["detail"], _ = json.Marshal(status.Receipt.Detail)
	receipt["action_label"], _ = json.Marshal(status.Receipt.ActionLabel)
	result["receipt"], _ = json.Marshal(receipt)
	result["dashboard_url"], _ = json.Marshal(status.DashboardURL)
	return json.Marshal(result)
}

func ReceiptText(status Status) string {
	text := ReceiptSummary(status)
	if status.DashboardURL != nil {
		label := "View session"
		if status.Receipt.ActionLabel != nil && *status.Receipt.ActionLabel != "" {
			label = *status.Receipt.ActionLabel
		}
		text += " · [" + label + "](" + *status.DashboardURL + ")"
	}
	return text
}

func EndedReceiptText(status Status) string { return "Session ended · " + ReceiptText(status) }

func ReceiptSummary(status Status) string {
	text := status.Receipt.Label
	if status.Receipt.Detail != "" {
		text += " · " + status.Receipt.Detail
	}
	return text
}

func ExchangeCount(count int) string {
	label := "exchanges"
	if count == 1 {
		label = "exchange"
	}
	return fmt.Sprintf("%d %s", count, label)
}
