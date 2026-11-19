package mimircli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/cloudboy-jh/mimir/internal/mimirapi"
	"github.com/cloudboy-jh/mimir/internal/sessionimport"
)

const importSelectionLimit = 20

type sessionImportService interface {
	Discover(context.Context, sessionimport.Options) (sessionimport.Discovery, error)
	Upload(context.Context, []sessionimport.Session) (sessionimport.Report, error)
}

var importNow = time.Now
var importServiceFactory = func(maxSessions int, connected bool) (sessionImportService, error) {
	opencode := sessionimport.NewOpenCodeAdapter()
	if maxSessions > 0 {
		opencode.MaxSessions = maxSessions
	}
	pi := sessionimport.NewPiAdapter()
	var client mimirapi.Client
	if connected {
		pointer, err := loadPointer()
		if err != nil {
			return nil, err
		}
		client = mimirapi.Client{HTTPClient: httpClient, Pointer: pointer}
	}
	service := sessionimport.New(client, opencode, pi, sessionimport.NewOhMyPiAdapter())
	return service, nil
}

type importCandidate struct {
	Harness            string `json:"harness"`
	SourceID           string `json:"source_id"`
	SessionID          string `json:"session_id"`
	Title              string `json:"title,omitempty"`
	StartedAt          string `json:"started_at,omitempty"`
	Exchanges          int    `json:"exchanges"`
	SkippedOpenRouter  int    `json:"skipped_openrouter"`
	SkippedInvalid     int    `json:"skipped_invalid"`
	ParentSessionID    string `json:"parent_session_id,omitempty"`
	ParentLinkStatus   string `json:"parent_link_status,omitempty"`
	ParentLinkReason   string `json:"parent_link_reason,omitempty"`
	ParentLinkEvidence string `json:"parent_link_evidence,omitempty"`
}

func cmdImport(ctx context.Context, args []string, ioctx IO) error {
	if len(args) > 0 && args[0] == "list" {
		return cmdImportList(ctx, args[1:], ioctx)
	}
	if len(args) > 0 && args[0] == "inspect" {
		return cmdImportInspect(ctx, args[1:], ioctx)
	}
	harness, ids, yes, jsonOutput, err := parseImportRunArgs(args)
	if err != nil {
		return err
	}
	interactive := importInteractive(ioctx) && !jsonOutput
	if harness == "" {
		if !interactive {
			return errors.New("usage: mimir import <opencode|pi|oh-my-pi> <session-id>... --yes [--json]")
		}
		harness, err = selectImportHarness(ioctx)
		if err != nil || harness == "" {
			return err
		}
	}
	if len(ids) == 0 && !interactive {
		return errors.New("usage: mimir import <opencode|pi|oh-my-pi> <session-id>... --yes [--json]")
	}
	if len(ids) > 0 && !yes && !interactive {
		return errors.New("mimir import requires --yes when input is not interactive")
	}
	service, err := importServiceFactory(importSelectionLimit, true)
	if err != nil {
		return err
	}
	discovery, err := service.Discover(ctx, sessionimport.Options{Sources: []string{harness}, SourceIDs: ids})
	if err != nil {
		return err
	}
	if err := discoveryError(discovery); err != nil {
		return err
	}
	sessions := discovery.Sessions
	if interactive && !yes {
		sessions, err = selectImportSessions(ioctx, sessions)
		if err != nil {
			return err
		}
	}
	if len(sessions) == 0 {
		return printNoImportSessions(ioctx.Out, jsonOutput, discovery)
	}
	report, err := service.Upload(ctx, sessions)
	if err != nil {
		return err
	}
	mergeDiscoveryFailures(&report, discovery)
	return printImportReport(ioctx.Out, report, jsonOutput, "Import")
}

func cmdImportList(ctx context.Context, args []string, ioctx IO) error {
	harness, jsonOutput, err := parseReadImportArgs(args, false)
	if err != nil {
		return err
	}
	service, err := importServiceFactory(importSelectionLimit, false)
	if err != nil {
		return err
	}
	options := sessionimport.Options{}
	if harness != "" {
		options.Sources = []string{harness}
	}
	discovery, err := service.Discover(ctx, options)
	if err != nil {
		return err
	}
	if jsonOutput {
		return json.NewEncoder(ioctx.Out).Encode(map[string]any{"sessions": candidateViews(discovery.Sessions), "sources": discovery.Sources})
	}
	if len(discovery.Sessions) == 0 {
		return printNoImportSessions(ioctx.Out, false, discovery)
	}
	for _, candidate := range candidateViews(discovery.Sessions) {
		if _, err := fmt.Fprintf(ioctx.Out, "%s  %-8s  %-20s  %3d exchanges  %s\n", candidate.StartedAt, candidate.Harness, candidate.SourceID, candidate.Exchanges, candidate.Title); err != nil {
			return err
		}
		if candidate.Harness == "oh-my-pi" {
			if _, err := fmt.Fprintf(ioctx.Out, "  Parent: %s [%s] %s\n", candidate.ParentSessionID, candidate.ParentLinkStatus, candidate.ParentLinkReason); err != nil {
				return err
			}
		}
	}
	return nil
}

func cmdImportInspect(ctx context.Context, args []string, ioctx IO) error {
	harness, ids, _, jsonOutput, err := parseImportRunArgs(args)
	if err != nil || harness == "" || len(ids) != 1 {
		return errors.New("usage: mimir import inspect <opencode|pi|oh-my-pi> <session-id> [--json]")
	}
	service, err := importServiceFactory(1, false)
	if err != nil {
		return err
	}
	discovery, err := service.Discover(ctx, sessionimport.Options{Sources: []string{harness}, SourceIDs: ids})
	if err != nil {
		return err
	}
	if err := discoveryError(discovery); err != nil {
		return err
	}
	if len(discovery.Sessions) != 1 {
		return fmt.Errorf("session not found in %s: %s", harness, ids[0])
	}
	view := candidateViews(discovery.Sessions)[0]
	if jsonOutput {
		return json.NewEncoder(ioctx.Out).Encode(view)
	}
	if view.Harness == "oh-my-pi" {
		_, err = fmt.Fprintf(ioctx.Out, "%s %s\nSession: %s\nParent: %s\nParent link: %s\nEvidence: %s\n%s\nMetadata-only repair; no exchanges or lifecycle events uploaded.\n", view.Harness, view.Title, view.SourceID, view.ParentSessionID, view.ParentLinkStatus, view.ParentLinkEvidence, view.ParentLinkReason)
		return err
	}
	_, err = fmt.Fprintf(ioctx.Out, "%s %s\nSession: %s\nStarted: %s\nExchanges: %d\n", view.Harness, view.Title, view.SourceID, view.StartedAt, view.Exchanges)
	return err
}

func cmdBackfill(ctx context.Context, args []string, ioctx IO) error {
	harness, since, all, yes, jsonOutput, err := parseBackfillArgs(args)
	if err != nil {
		return err
	}
	interactive := importInteractive(ioctx) && !jsonOutput
	if all && !yes {
		return errors.New("mimir backfill --all requires --yes")
	}
	if !all && !interactive {
		return errors.New("mimir backfill requires --all --yes when input is not interactive")
	}
	maxSessions := importSelectionLimit
	if all {
		maxSessions = sessionimport.DefaultMaxSessions
	}
	service, err := importServiceFactory(maxSessions, true)
	if err != nil {
		return err
	}
	options := sessionimport.Options{Since: since}
	if harness != "" {
		options.Sources = []string{harness}
	}
	discovery, err := service.Discover(ctx, options)
	if err != nil {
		return err
	}
	if err := discoveryError(discovery); err != nil && len(discovery.Sessions) == 0 {
		return err
	}
	sessions := discovery.Sessions
	if !all {
		sessions, err = selectImportSessions(ioctx, sessions)
		if err != nil {
			return err
		}
	}
	if len(sessions) == 0 {
		return printNoImportSessions(ioctx.Out, jsonOutput, discovery)
	}
	report, err := service.Upload(ctx, sessions)
	if err != nil {
		return err
	}
	mergeDiscoveryFailures(&report, discovery)
	return printImportReport(ioctx.Out, report, jsonOutput, "Backfill")
}

func parseImportRunArgs(args []string) (string, []string, bool, bool, error) {
	var harness string
	var ids []string
	yes, jsonOutput := false, false
	for _, arg := range args {
		switch arg {
		case "--yes":
			yes = true
		case "--json":
			jsonOutput = true
		default:
			if strings.HasPrefix(arg, "-") {
				return "", nil, false, false, fmt.Errorf("unexpected argument %s", arg)
			}
			if harness == "" {
				if !validImportHarness(arg) {
					return "", nil, false, false, fmt.Errorf("invalid import harness %q", arg)
				}
				harness = arg
			} else {
				ids = append(ids, arg)
			}
		}
	}
	return harness, ids, yes, jsonOutput, nil
}

func parseReadImportArgs(args []string, requireHarness bool) (string, bool, error) {
	harness, jsonOutput := "", false
	for _, arg := range args {
		if arg == "--json" {
			jsonOutput = true
			continue
		}
		if harness != "" || !validImportHarness(arg) {
			return "", false, errors.New("usage: mimir import list [opencode|pi|oh-my-pi] [--json]")
		}
		harness = arg
	}
	if requireHarness && harness == "" {
		return "", false, errors.New("import harness is required")
	}
	return harness, jsonOutput, nil
}

func parseBackfillArgs(args []string) (string, time.Time, bool, bool, bool, error) {
	var harness string
	var since time.Time
	all, yes, jsonOutput := false, false, false
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--all":
			all = true
		case "--yes":
			yes = true
		case "--json":
			jsonOutput = true
		case "--since":
			if i+1 >= len(args) {
				return "", time.Time{}, false, false, false, errors.New("--since requires a duration or RFC3339 timestamp")
			}
			i++
			parsed, err := parseImportSince(args[i])
			if err != nil {
				return "", time.Time{}, false, false, false, err
			}
			since = parsed
		default:
			if harness != "" || !validImportHarness(args[i]) {
				return "", time.Time{}, false, false, false, fmt.Errorf("unexpected argument %s", args[i])
			}
			harness = args[i]
		}
	}
	return harness, since, all, yes, jsonOutput, nil
}

func parseImportSince(value string) (time.Time, error) {
	if parsed, err := time.Parse(time.RFC3339, value); err == nil {
		return parsed, nil
	}
	if strings.HasSuffix(value, "d") {
		days, err := strconv.Atoi(strings.TrimSuffix(value, "d"))
		if err == nil && days > 0 {
			return importNow().UTC().Add(-time.Duration(days) * 24 * time.Hour), nil
		}
	}
	duration, err := time.ParseDuration(value)
	if err != nil || duration <= 0 {
		return time.Time{}, fmt.Errorf("invalid --since value %q", value)
	}
	return importNow().UTC().Add(-duration), nil
}

func validImportHarness(value string) bool { return value == "opencode" || value == "pi" || value == "oh-my-pi" }

func importInteractive(ioctx IO) bool {
	in, inputOK := ioctx.In.(*os.File)
	out, outputOK := ioctx.Out.(*os.File)
	return inputOK && outputOK && isTerminal(in) && isTerminal(out)
}

var importHarnesses = []struct{ id, label string }{
	{"opencode", "OpenCode"},
	{"pi", "Pi"},
	{"oh-my-pi", "Oh My Pi (parent links only)"},
}

// selectImportHarness prints a numbered list and reads one number; an empty
// answer cancels.
func selectImportHarness(ioctx IO) (string, error) {
	fmt.Fprintln(ioctx.Out, "Import from harness:")
	for i, harness := range importHarnesses {
		fmt.Fprintf(ioctx.Out, "  %d. %s\n", i+1, harness.label)
	}
	answer, err := promptValue(ioctx, "Harness number (Enter to cancel): ")
	if err != nil || answer == "" {
		return "", err
	}
	choice, err := strconv.Atoi(answer)
	if err != nil || choice < 1 || choice > len(importHarnesses) {
		return "", fmt.Errorf("invalid harness selection %q", answer)
	}
	return importHarnesses[choice-1].id, nil
}

// selectImportSessions prints a numbered list of the newest candidates and
// reads comma-separated numbers or "all"; an empty answer selects none.
func selectImportSessions(ioctx IO, sessions []sessionimport.Session) ([]sessionimport.Session, error) {
	if len(sessions) == 0 {
		return nil, nil
	}
	if len(sessions) > importSelectionLimit {
		sessions = sessions[len(sessions)-importSelectionLimit:]
	}
	fmt.Fprintln(ioctx.Out, "Sessions to import:")
	for i, session := range sessions {
		label := fmt.Sprintf("%-8s  %-16s  %3d  %s", session.Harness, session.StartedAt.Local().Format("2006-01-02 15:04"), len(session.Exchanges), session.Title)
		if session.Harness == "oh-my-pi" {
			label = fmt.Sprintf("%s  %s -> %s [%s] %s", session.Harness, session.SourceID, session.ParentSessionID, session.ParentLinkStatus, session.ParentLinkReason)
		}
		fmt.Fprintf(ioctx.Out, "  %2d. %s\n", i+1, label)
	}
	answer, err := promptValue(ioctx, "Sessions (numbers separated by commas, all, or Enter for none): ")
	if err != nil {
		return nil, err
	}
	indexes, err := parseImportSelection(answer, len(sessions))
	if err != nil {
		return nil, err
	}
	chosen := make([]sessionimport.Session, 0, len(indexes))
	for _, index := range indexes {
		chosen = append(chosen, sessions[index])
	}
	return chosen, nil
}

// parseImportSelection turns "1, 3" or "all" into distinct zero-based indexes
// in list order.
func parseImportSelection(answer string, count int) ([]int, error) {
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return nil, nil
	}
	selected := make([]bool, count)
	if strings.EqualFold(answer, "all") {
		for i := range selected {
			selected[i] = true
		}
	} else {
		for _, field := range strings.Split(answer, ",") {
			field = strings.TrimSpace(field)
			number, err := strconv.Atoi(field)
			if err != nil || number < 1 || number > count {
				return nil, fmt.Errorf("invalid session selection %q", field)
			}
			selected[number-1] = true
		}
	}
	indexes := make([]int, 0, count)
	for i, include := range selected {
		if include {
			indexes = append(indexes, i)
		}
	}
	return indexes, nil
}

func candidateViews(sessions []sessionimport.Session) []importCandidate {
	views := make([]importCandidate, 0, len(sessions))
	for _, session := range sessions {
		started := ""
		if !session.StartedAt.IsZero() {
			started = session.StartedAt.UTC().Format(time.RFC3339)
		}
		views = append(views, importCandidate{Harness: session.Harness, SourceID: session.SourceID, SessionID: session.ID, Title: session.Title, StartedAt: started, Exchanges: len(session.Exchanges), SkippedOpenRouter: session.SkippedOpenRouter, SkippedInvalid: session.SkippedInvalid, ParentSessionID: session.ParentSessionID, ParentLinkStatus: session.ParentLinkStatus, ParentLinkReason: session.ParentLinkReason, ParentLinkEvidence: session.ParentLinkEvidence})
	}
	return views
}

func discoveryError(discovery sessionimport.Discovery) error {
	var failures []string
	for _, source := range discovery.Sources {
		if source.Status == "failed" {
			failures = append(failures, source.Source+": "+source.Error)
		}
	}
	if len(failures) == 0 {
		return nil
	}
	return errors.New(strings.Join(failures, "; "))
}

func mergeDiscoveryFailures(report *sessionimport.Report, discovery sessionimport.Discovery) {
	for _, source := range discovery.Sources {
		if source.Status != "failed" {
			continue
		}
		report.SourcesFailed++
		report.Sessions = append(report.Sessions, sessionimport.SessionReport{Source: source.Source, Status: "failed", Error: source.Error})
	}
}

func printNoImportSessions(out io.Writer, jsonOutput bool, discovery sessionimport.Discovery) error {
	if jsonOutput {
		return json.NewEncoder(out).Encode(map[string]any{"sessions": []importCandidate{}, "sources": discovery.Sources})
	}
	_, err := fmt.Fprintln(out, "No local sessions matched.")
	return err
}

func printImportReport(out io.Writer, report sessionimport.Report, jsonOutput bool, operation string) error {
	if jsonOutput {
		return json.NewEncoder(out).Encode(report)
	}
	status := "✓"
	if report.SessionsPartial > 0 || report.SessionsFailed > 0 || report.SourcesFailed > 0 {
		status = "!"
	}
	lines := []string{fmt.Sprintf("%s %s complete: %d imported, %d partial, %d already present, %d skipped, %d failed", status, operation, report.SessionsImported, report.SessionsPartial, report.ExchangesDuplicate, report.SessionsSkipped, report.SessionsFailed+report.SourcesFailed)}
	if report.GitArtifactsFound > 0 {
		lines = append(lines, fmt.Sprintf("  Git artifacts: %d saved, %d already present", report.GitArtifactsSaved, report.GitArtifactsDuplicate))
	}
	for _, session := range report.Sessions {
		if session.ParentLinkStatus != "" {
			lines = append(lines, fmt.Sprintf("  %s %s -> %s [%s]: %s", session.Source, session.SourceID, session.ParentSessionID, session.ParentLinkStatus, session.ParentLinkReason))
		}
		detail := session.Error
		if detail == "" {
			detail = session.GitArtifactError
		}
		if detail != "" {
			lines = append(lines, fmt.Sprintf("  %s %s: %s", session.Source, session.SourceID, detail))
		}
	}
	_, err := fmt.Fprintln(out, strings.Join(lines, "\n"))
	return err
}
