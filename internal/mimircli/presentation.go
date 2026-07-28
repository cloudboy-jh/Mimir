package mimircli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	doctorpkg "github.com/cloudboy-jh/mimir/internal/doctor"
	lifecyclepkg "github.com/cloudboy-jh/mimir/internal/harness/lifecycle"
	"github.com/cloudboy-jh/mimir/internal/sessions"
	cliui "github.com/cloudboy-jh/mimir/internal/ui"
	"github.com/cloudboy-jh/mimir/internal/ui/bentotui"
)

func renderDoctor(out io.Writer, report doctorpkg.Report) error {
	render := cliui.New(out)
	rows := make([]cliui.StatusItem, 0, len(report.Checks))
	for _, check := range report.Checks {
		row := cliui.StatusItem{Title: check.Name, Detail: check.Detail, Stat: check.Status, Tone: cliui.ToneForStatus(check.Status)}
		if check.Repair != "" {
			if row.Detail != "" {
				row.Detail += " · "
			}
			row.Detail += "repair: " + check.Repair
		}
		rows = append(rows, row)
	}
	_, err := fmt.Fprintf(out, "%s\n\n%s\n", render.Heading("Mimir doctor"), render.StatusItems(rows))
	return err
}

func renderInstall(out io.Writer, report lifecyclepkg.InstallReport) error {
	render := cliui.New(out)
	fields := []bentotui.Field{
		{Label: "Binary", Value: report.Binary.Status + " · " + report.Binary.Path},
		{Label: "OpenCode", Value: report.OpenCode.State},
		{Label: "Hermes", Value: report.Hermes.State},
		{Label: "Install log", Value: report.Artifacts.LogPath},
	}
	if _, err := fmt.Fprintln(out, render.Card("Installation complete", fields...)); err != nil {
		return err
	}
	rows := make([]cliui.StatusItem, 0, len(report.Artifacts.Artifacts))
	for _, artifact := range report.Artifacts.Artifacts {
		status := string(artifact.Status)
		rows = append(rows, cliui.StatusItem{Title: artifact.Path, Stat: status, Tone: cliui.ToneForStatus(status)})
	}
	if len(rows) > 0 {
		_, err := fmt.Fprintf(out, "\n%s\n", render.StatusItems(rows))
		return err
	}
	return nil
}

func renderReceipts(out io.Writer, receipts []sessions.Receipt, limit int) error {
	if limit > 0 && len(receipts) > limit {
		receipts = receipts[:limit]
	}
	if len(receipts) == 0 {
		render := cliui.New(out)
		_, err := fmt.Fprintln(out, render.EmptyState("No sessions found", "Captured model traffic will appear here as work sessions."))
		return err
	}
	render := cliui.New(out)
	if _, err := fmt.Fprintf(out, "%s\n\n", render.Heading("Sessions")); err != nil {
		return err
	}
	for i, receipt := range receipts {
		outcome := receipt.Outcome
		if outcome == "" {
			outcome = "unresolved"
		}
		title := receipt.ID
		if receipt.Intent != nil && strings.TrimSpace(*receipt.Intent) != "" {
			title = strings.TrimSpace(*receipt.Intent)
		}
		repo := "No repository"
		if receipt.Repo != nil && *receipt.Repo != "" {
			repo = *receipt.Repo
		}
		model := "unknown model"
		if receipt.Model != nil && *receipt.Model != "" {
			model = *receipt.Model
		}
		started := receipt.StartedAt
		if parsed, err := time.Parse(time.RFC3339, started); err == nil {
			started = parsed.Local().Format("2006-01-02 15:04")
		}
		capture := sessions.ExchangeCount(receipt.Capture.SavedExchanges) + " saved"
		if receipt.Capture.PendingExchanges > 0 {
			capture = "saving"
		} else if receipt.Capture.FailedExchanges > 0 {
			capture = fmt.Sprintf("%d saved · %d failed", receipt.Capture.SavedExchanges, receipt.Capture.FailedExchanges)
		}
		fmt.Fprintln(out, render.Session(cliui.SessionItem{
			Title: title, Outcome: outcome, Capture: capture,
			Metadata: strings.Join([]string{started, repo, model}, " · "), ID: receipt.ID,
		}))
		if i < len(receipts)-1 {
			fmt.Fprintln(out)
		}
	}
	return nil
}

func renderSearch(out io.Writer, data []byte) error {
	var result struct {
		Query   string `json:"query"`
		Matches []struct {
			SessionID       string `json:"session_id"`
			Outcome         string `json:"outcome"`
			Repo            string `json:"repo"`
			Model           string `json:"model_primary"`
			RequestExcerpt  string `json:"request_excerpt"`
			ResponseExcerpt string `json:"response_excerpt"`
		} `json:"matches"`
		Code string `json:"code"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return err
	}
	render := cliui.New(out)
	fmt.Fprintf(out, "%s\n", render.Heading("Search · "+result.Query))
	if len(result.Matches) == 0 && strings.TrimSpace(result.Code) == "" {
		_, err := fmt.Fprintln(out, "\n"+render.EmptyState("No matches found", "Try a broader query or search a repository, model, or session ID."))
		return err
	}
	for _, match := range result.Matches {
		excerpt := strings.TrimSpace(match.RequestExcerpt)
		if excerpt == "" {
			excerpt = strings.TrimSpace(match.ResponseExcerpt)
		}
		fmt.Fprintf(out, "\n%s\n", render.Session(cliui.SessionItem{
			Title: match.SessionID, Outcome: match.Outcome,
			Metadata: strings.Join([]string{emptyFallback(match.Repo, "No repository"), emptyFallback(match.Model, "unknown model")}, " · "),
			Excerpt:  excerpt,
		}))
	}
	if code := strings.Trim(result.Code, "\r\n"); code != "" {
		var lines []string
		for _, line := range strings.Split(code, "\n") {
			lines = append(lines, bentotui.WrapPreserve(line, render.Width)...)
		}
		fmt.Fprintf(out, "\n%s\n%s\n", render.Heading("Local code"), strings.Join(lines, "\n"))
	}
	return nil
}

func renderEndedReceipt(out io.Writer, status sessions.Status) error {
	render := cliui.New(out)
	fields := []bentotui.Field{
		{Label: "Capture", Value: sessions.ReceiptSummary(status)},
		{Label: "Outcome", Value: render.OutcomeBadge(emptyFallback(status.Outcome, "unresolved"))},
		{Label: "Session", Value: status.SessionID},
	}
	if status.DashboardURL != nil && *status.DashboardURL != "" {
		fields = append(fields, bentotui.Field{Label: "Details", Value: *status.DashboardURL})
	}
	_, err := fmt.Fprintln(out, render.Card("Capture finalized", fields...))
	return err
}

func emptyFallback(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
