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
	"github.com/cloudboy-jh/mimir/internal/ui/lineoutput"
	"github.com/cloudboy-jh/mimir/internal/ui/selector"
)

const importSelectionLimit = 20

type sessionImportService interface {
	Discover(context.Context, sessionimport.Options) (sessionimport.Discovery, error)
	Upload(context.Context, []sessionimport.Session) (sessionimport.Report, error)
}

var importNow = time.Now
var importSelectorAvailable = selector.Available
var runImportSelector = selector.Run
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
	service := sessionimport.New(client, opencode, pi)
	return service, nil
}

type importCandidate struct {
	Harness           string `json:"harness"`
	SourceID          string `json:"source_id"`
	SessionID         string `json:"session_id"`
	Title             string `json:"title,omitempty"`
	StartedAt         string `json:"started_at,omitempty"`
	Exchanges         int    `json:"exchanges"`
	SkippedOpenRouter int    `json:"skipped_openrouter"`
	SkippedInvalid    int    `json:"skipped_invalid"`
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
			return errors.New("usage: mimir import <opencode|pi> <session-id>... --yes [--json]")
		}
		harness, err = selectImportHarness(ioctx)
		if err != nil || harness == "" {
			return err
		}
	}
	if len(ids) == 0 && !interactive {
		return errors.New("usage: mimir import <opencode|pi> <session-id>... --yes [--json]")
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
		sessions, err = selectImportSessions(ioctx, sessions, len(ids) > 0)
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
	}
	return nil
}

func cmdImportInspect(ctx context.Context, args []string, ioctx IO) error {
	harness, ids, _, jsonOutput, err := parseImportRunArgs(args)
	if err != nil || harness == "" || len(ids) != 1 {
		return errors.New("usage: mimir import inspect <opencode|pi> <session-id> [--json]")
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
		sessions, err = selectImportSessions(ioctx, sessions, false)
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
			return "", false, errors.New("usage: mimir import list [opencode|pi] [--json]")
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

func validImportHarness(value string) bool { return value == "opencode" || value == "pi" }

func importInteractive(ioctx IO) bool {
	in, inputOK := ioctx.In.(*os.File)
	out, outputOK := ioctx.Out.(*os.File)
	return inputOK && outputOK && isTerminal(in) && isTerminal(out) && importSelectorAvailable(in, out)
}

func selectImportHarness(ioctx IO) (string, error) {
	in := ioctx.In.(*os.File)
	out := ioctx.Out.(*os.File)
	result, err := runImportSelector(in, out, "Import from harness", []selector.Item{{Label: "OpenCode"}, {Label: "Pi"}})
	if err != nil || !result.Accepted {
		return "", err
	}
	if len(result.Selected) != 2 {
		return "", errors.New("harness selector returned an invalid selection")
	}
	selected := ""
	for i, chosen := range result.Selected {
		if !chosen {
			continue
		}
		if selected != "" {
			return "", errors.New("select exactly one harness")
		}
		selected = []string{"opencode", "pi"}[i]
	}
	if selected == "" {
		return "", errors.New("select one harness")
	}
	return selected, nil
}

func selectImportSessions(ioctx IO, sessions []sessionimport.Session, selected bool) ([]sessionimport.Session, error) {
	if len(sessions) == 0 {
		return nil, nil
	}
	if len(sessions) > importSelectionLimit {
		sessions = sessions[len(sessions)-importSelectionLimit:]
	}
	items := make([]selector.Item, len(sessions))
	for i, session := range sessions {
		label := fmt.Sprintf("%-8s  %-16s  %3d  %s", session.Harness, session.StartedAt.Local().Format("2006-01-02 15:04"), len(session.Exchanges), session.Title)
		items[i] = selector.Item{Label: label, Selected: selected}
	}
	result, err := runImportSelector(ioctx.In.(*os.File), ioctx.Out.(*os.File), "Sessions to import", items)
	if err != nil || !result.Accepted {
		return nil, err
	}
	if len(result.Selected) != len(sessions) {
		return nil, errors.New("session selector returned an invalid selection")
	}
	chosen := make([]sessionimport.Session, 0, len(sessions))
	for i, include := range result.Selected {
		if include {
			chosen = append(chosen, sessions[i])
		}
	}
	return chosen, nil
}

func candidateViews(sessions []sessionimport.Session) []importCandidate {
	views := make([]importCandidate, 0, len(sessions))
	for _, session := range sessions {
		started := ""
		if !session.StartedAt.IsZero() {
			started = session.StartedAt.UTC().Format(time.RFC3339)
		}
		views = append(views, importCandidate{Harness: session.Harness, SourceID: session.SourceID, SessionID: session.ID, Title: session.Title, StartedAt: started, Exchanges: len(session.Exchanges), SkippedOpenRouter: session.SkippedOpenRouter, SkippedInvalid: session.SkippedInvalid})
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
	render := lineoutput.New(out)
	summary := fmt.Sprintf("%s complete: %d imported, %d partial, %d already present, %d skipped, %d failed", operation, report.SessionsImported, report.SessionsPartial, report.ExchangesDuplicate, report.SessionsSkipped, report.SessionsFailed+report.SourcesFailed)
	var err error
	if report.SessionsPartial > 0 || report.SessionsFailed > 0 || report.SourcesFailed > 0 {
		err = render.Warning(summary)
	} else {
		err = render.Success(summary)
	}
	if err != nil {
		return err
	}
	if report.GitArtifactsFound > 0 {
		if err := render.Detail(fmt.Sprintf("Git artifacts: %d saved, %d already present", report.GitArtifactsSaved, report.GitArtifactsDuplicate)); err != nil {
			return err
		}
	}
	for _, session := range report.Sessions {
		detail := session.Error
		if detail == "" {
			detail = session.GitArtifactError
		}
		if detail != "" {
			if err := render.Detail(fmt.Sprintf("%s %s: %s", session.Source, session.SourceID, detail)); err != nil {
				return err
			}
		}
	}
	return nil
}
