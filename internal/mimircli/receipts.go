package mimircli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"
)

type sessionReceipt struct {
	ID           string  `json:"id"`
	StartedAt    string  `json:"started_at"`
	State        string  `json:"state"`
	Outcome      string  `json:"outcome"`
	Model        *string `json:"model_primary"`
	Intent       *string `json:"intent"`
	Repo         *string `json:"repo"`
	RequestCount int     `json:"request_count"`
	Capture      struct {
		Status           string `json:"status"`
		SavedExchanges   int    `json:"saved_exchanges"`
		FailedExchanges  int    `json:"failed_exchanges"`
		PendingExchanges int    `json:"pending_exchanges"`
	} `json:"capture"`
}

func fetchSessionReceipts(ctx context.Context, repo, outcome string) ([]sessionReceipt, error) {
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
	data, err := remoteRequest(ctx, "GET", path, nil)
	if err != nil {
		return nil, err
	}
	var response struct {
		Sessions []sessionReceipt `json:"sessions"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, fmt.Errorf("decoding sessions: %w", err)
	}
	return response.Sessions, nil
}

// formatSessionReceipts renders sessions as compact, human-readable
// receipts: one summary line per session plus an indented intent line when
// the Worker derived one.
func formatSessionReceipts(sessions []sessionReceipt, limit int) string {
	if len(sessions) == 0 {
		return "No sessions found."
	}
	if limit > 0 && len(sessions) > limit {
		sessions = sessions[:limit]
	}
	var text strings.Builder
	for i, session := range sessions {
		if i > 0 {
			text.WriteByte('\n')
		}
		parts := []string{receiptTime(session.StartedAt), session.ID, outcomeLabel(session.Outcome), captureLabel(session), modelLabel(session.Model)}
		if session.State == "active" {
			parts = append(parts, "active")
		}
		text.WriteString(strings.Join(parts, " · "))
		if session.Intent != nil && strings.TrimSpace(*session.Intent) != "" {
			text.WriteString("\n  " + truncate(strings.TrimSpace(*session.Intent), 100))
		}
	}
	return text.String()
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

func captureLabel(session sessionReceipt) string {
	capture := session.Capture
	switch {
	case capture.PendingExchanges > 0:
		return "saving…"
	case capture.SavedExchanges > 0 && capture.FailedExchanges > 0:
		return fmt.Sprintf("%d saved · %d failed", capture.SavedExchanges, capture.FailedExchanges)
	case capture.FailedExchanges > 0:
		return "capture failed"
	case capture.SavedExchanges > 0:
		return exchangeCount(capture.SavedExchanges) + " saved"
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

func cmdList(ctx context.Context, args []string, out io.Writer) error {
	repo, outcome, limit := "", "", 20
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--repo" && i+1 < len(args):
			repo = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--repo="):
			repo = strings.TrimPrefix(args[i], "--repo=")
		case args[i] == "--outcome" && i+1 < len(args):
			outcome = args[i+1]
			i++
		case strings.HasPrefix(args[i], "--outcome="):
			outcome = strings.TrimPrefix(args[i], "--outcome=")
		case args[i] == "--limit" && i+1 < len(args):
			if _, err := fmt.Sscanf(args[i+1], "%d", &limit); err != nil || limit <= 0 {
				return fmt.Errorf("invalid --limit value")
			}
			i++
		case strings.HasPrefix(args[i], "--limit="):
			if _, err := fmt.Sscanf(strings.TrimPrefix(args[i], "--limit="), "%d", &limit); err != nil || limit <= 0 {
				return fmt.Errorf("invalid --limit value")
			}
		default:
			return fmt.Errorf("usage: mimir list [--repo name] [--outcome landed|discarded|abandoned|unresolved] [--limit 20]")
		}
	}
	if outcome != "" && !canonicalOutcome(outcome) {
		return fmt.Errorf("invalid outcome %q: must be landed, discarded, abandoned, or unresolved", outcome)
	}
	sessions, err := fetchSessionReceipts(ctx, repo, outcome)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(out, formatSessionReceipts(sessions, limit))
	return err
}
