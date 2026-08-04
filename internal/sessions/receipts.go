package sessions

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
)

type Receipt struct {
	ID             string  `json:"id"`
	StartedAt      string  `json:"started_at"`
	State          string  `json:"state"`
	Outcome        string  `json:"outcome"`
	Model          *string `json:"model_primary"`
	Title          *string `json:"title"`
	TitleSource    *string `json:"title_source"`
	TitleUpdatedAt *string `json:"title_updated_at"`
	DisplayTitle   *string `json:"display_title"`
	Intent         *string `json:"intent"`
	Repo           *string `json:"repo"`
	Harness        *string `json:"harness"`
	RequestCount   int     `json:"request_count"`
	TokensIn       int     `json:"tokens_in"`
	TokensOut      int     `json:"tokens_out"`
	Capture        struct {
		Status           string `json:"status"`
		SavedExchanges   int    `json:"saved_exchanges"`
		FailedExchanges  int    `json:"failed_exchanges"`
		PendingExchanges int    `json:"pending_exchanges"`
	} `json:"capture"`
}

func (s Service) FetchReceipts(ctx context.Context, repo, outcome string) ([]Receipt, error) {
	data, err := s.API.Request(ctx, "GET", receiptsPath(repo, outcome), nil)
	if err != nil {
		return nil, err
	}
	var response struct {
		Sessions []Receipt `json:"sessions"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, fmt.Errorf("decoding sessions: %w", err)
	}
	return response.Sessions, nil
}

// FetchReceiptsJSON preserves the Worker's response envelope and every session
// field while applying the CLI's presentation limit to the sessions array.
func (s Service) FetchReceiptsJSON(ctx context.Context, repo, outcome string, limit int) ([]byte, error) {
	data, err := s.API.Request(ctx, "GET", receiptsPath(repo, outcome), nil)
	if err != nil {
		return nil, err
	}
	var response map[string]json.RawMessage
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, fmt.Errorf("decoding sessions: %w", err)
	}
	var receipts []json.RawMessage
	if err := json.Unmarshal(response["sessions"], &receipts); err != nil {
		return nil, fmt.Errorf("decoding sessions: %w", err)
	}
	if limit > 0 && len(receipts) > limit {
		receipts = receipts[:limit]
	}
	encoded, err := json.Marshal(receipts)
	if err != nil {
		return nil, err
	}
	response["sessions"] = encoded
	return json.Marshal(response)
}

func receiptsPath(repo, outcome string) string {
	query := url.Values{}
	if repo != "" {
		query.Set("repo", repo)
	}
	if outcome != "" {
		query.Set("outcome", outcome)
	}
	path := "/sessions"
	if len(query) > 0 {
		path += "?" + query.Encode()
	}
	return path
}

func FormatReceipts(receipts []Receipt, limit int) string {
	if len(receipts) == 0 {
		return "No sessions found."
	}
	if limit > 0 && len(receipts) > limit {
		receipts = receipts[:limit]
	}
	var text strings.Builder
	for i, receipt := range receipts {
		if i > 0 {
			text.WriteByte('\n')
		}
		parts := []string{receiptTime(receipt.StartedAt), receipt.ID, outcomeLabel(receipt.Outcome), captureLabel(receipt), modelLabel(receipt.Model)}
		if receipt.State == "active" {
			parts = append(parts, "active")
		}
		text.WriteString(strings.Join(parts, " · "))
		text.WriteString("\n  " + truncate(DisplayTitle(receipt.DisplayTitle, receipt.Title, receipt.Intent), 100))
	}
	return text.String()
}

func DisplayTitle(displayTitle, title, intent *string) string {
	for _, value := range []*string{displayTitle, title, intent} {
		if value != nil && strings.TrimSpace(*value) != "" {
			return strings.TrimSpace(*value)
		}
	}
	return "Untitled session"
}

func ValidOutcome(outcome string) bool {
	switch outcome {
	case "landed", "discarded", "abandoned", "unresolved":
		return true
	default:
		return false
	}
}

func receiptTime(value string) string {
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return value
	}
	return parsed.Local().Format("2006-01-02 15:04")
}

func outcomeLabel(outcome string) string {
	if outcome == "" {
		return "unresolved"
	}
	return outcome
}

func captureLabel(receipt Receipt) string {
	capture := receipt.Capture
	switch {
	case capture.PendingExchanges > 0:
		return "saving…"
	case capture.SavedExchanges > 0 && capture.FailedExchanges > 0:
		return fmt.Sprintf("%d saved · %d failed", capture.SavedExchanges, capture.FailedExchanges)
	case capture.FailedExchanges > 0:
		return "capture failed"
	case capture.SavedExchanges > 0:
		return ExchangeCount(capture.SavedExchanges) + " saved"
	default:
		return "not captured"
	}
}

func modelLabel(model *string) string {
	if model == nil || *model == "" {
		return "unknown model"
	}
	return *model
}

func truncate(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return strings.TrimSpace(value[:limit-1]) + "…"
}
