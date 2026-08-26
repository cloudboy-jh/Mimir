package sessionimport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/cloudboy-jh/mimir/internal/mimirapi"
)

type requester interface {
	RequestWithHeaders(context.Context, string, string, any, http.Header) ([]byte, error)
}

type Service struct {
	Client    requester
	Sources   []Source
	Artifacts ArtifactCollector
}

func New(client mimirapi.Client, sources ...Source) Service {
	return Service{Client: client, Sources: sources, Artifacts: CheckoutArtifactCollector{}}
}

// Discover returns normalized candidates without uploading or mutating them.
func (s Service) Discover(ctx context.Context, options Options) (Discovery, error) {
	var result Discovery
	for _, source := range s.Sources {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if !selected(options.Sources, source.Name()) {
			continue
		}
		var sessions []Session
		var err error
		if configurable, ok := source.(OptionSource); ok {
			sessions, err = configurable.DiscoverWithOptions(ctx, options)
		} else {
			sessions, err = source.Discover(ctx)
		}
		if err != nil {
			result.Sources = append(result.Sources, SourceReport{Source: source.Name(), Status: "failed", Error: err.Error()})
			continue
		}
		sessions = filterSessions(sessions, options)
		sortSessions(sessions)
		result.Sessions = append(result.Sessions, sessions...)
		result.Sources = append(result.Sources, SourceReport{Source: source.Name(), Status: "ok", Count: len(sessions)})
	}
	sortSessions(result.Sessions)
	return result, nil
}

// Import discovers and uploads candidates. Passing no options preserves the
// original all-sources behavior; one options value selects an import subset.
func (s Service) Import(ctx context.Context, values ...Options) (Report, error) {
	if len(values) > 1 {
		return Report{}, errors.New("session import accepts at most one options value")
	}
	var options Options
	if len(values) == 1 {
		options = values[0]
	}
	discovery, err := s.Discover(ctx, options)
	if err != nil {
		return Report{}, err
	}
	report, err := s.Upload(ctx, discovery.Sessions)
	report.SessionsDiscovered = len(discovery.Sessions)
	for _, source := range discovery.Sources {
		if source.Status == "failed" {
			report.SourcesFailed++
			report.Sessions = append(report.Sessions, SessionReport{Source: source.Source, Status: "failed", Error: source.Error})
		}
	}
	return report, err
}

// Upload imports already-discovered candidates, allowing callers to inspect
// and interactively select sessions before any network mutation occurs.
func (s Service) Upload(ctx context.Context, sessions []Session) (Report, error) {
	if s.Client == nil {
		return Report{}, errors.New("session import requires a Mimir client")
	}
	sessions = append([]Session(nil), sessions...)
	sortSessions(sessions)
	report := Report{SessionsDiscovered: len(sessions)}
	for _, session := range sessions {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		item := s.upload(ctx, session.Harness, session)
		report.Sessions = append(report.Sessions, item)
		report.ExchangesUploaded += item.ExchangesUploaded
		report.ExchangesSaved += item.ExchangesSaved
		report.ExchangesDuplicate += item.ExchangesDuplicate
		report.ExchangesSkipped += item.ExchangesSkipped
		report.SkippedOpenRouter += item.SkippedOpenRouter
		report.SkippedInvalid += item.SkippedInvalid
		report.GitArtifactsFound += item.GitArtifactsFound
		report.GitArtifactsSaved += item.GitArtifactsSaved
		report.GitArtifactsDuplicate += item.GitArtifactsDuplicate
		switch item.Status {
		case "imported":
			report.SessionsImported++
		case "partial":
			report.SessionsPartial++
		case "skipped":
			report.SessionsSkipped++
		default:
			report.SessionsFailed++
		}
	}
	return report, nil
}

func (s Service) upload(ctx context.Context, source string, session Session) SessionReport {
	report := SessionReport{Source: source, SourceID: session.SourceID, SessionID: session.ID, Status: "failed", SkippedOpenRouter: session.SkippedOpenRouter, SkippedInvalid: session.SkippedInvalid}
	if session.ID == "" || session.Harness == "" {
		report.Error = "invalid discovered session"
		return report
	}
	headers := make(http.Header)
	headers.Set("x-mimir-harness", session.Harness)
	if session.Repo != "" {
		headers.Set("x-mimir-repo", session.Repo)
	}
	path := "/sessions/" + url.PathEscape(session.ID)
	for _, exchange := range session.Exchanges {
		data, err := s.Client.RequestWithHeaders(ctx, http.MethodPost, path+"/exchanges", exchange, headers)
		if err != nil {
			report.Error = appendError(report.Error, fmt.Sprintf("uploading exchange %s: %v", exchange.ExchangeID, err))
			continue
		}
		classification, err := classifyExchangeResponse(data)
		if err != nil {
			report.Error = appendError(report.Error, fmt.Sprintf("classifying exchange %s: %v", exchange.ExchangeID, err))
			continue
		}
		report.ExchangesUploaded++
		switch classification {
		case "saved":
			report.ExchangesSaved++
		case "duplicate":
			report.ExchangesDuplicate++
		case "skipped":
			report.ExchangesSkipped++
		}
	}
	accepted := report.ExchangesSaved + report.ExchangesDuplicate
	if accepted > 0 {
		started := session.StartedAt
		if started.IsZero() {
			started = session.Exchanges[0].Timestamp
		}
		ended := session.EndedAt
		if ended.IsZero() {
			ended = session.Exchanges[len(session.Exchanges)-1].Timestamp
		}
		if err := s.event(ctx, session, headers, "heartbeat", started, ""); err != nil {
			report.Error = appendError(report.Error, "uploading heartbeat: "+err.Error())
		}
		if err := s.event(ctx, session, headers, "end", ended, "historical-import"); err != nil {
			report.Error = appendError(report.Error, "uploading end event: "+err.Error())
		}
	}
	s.uploadArtifacts(ctx, session, &report)
	if report.Error != "" {
		report.Status = "failed"
	} else if report.GitArtifactError != "" {
		report.Status = "partial"
	} else if accepted == 0 {
		report.Status = "skipped"
	} else {
		report.Status = "imported"
	}
	return report
}

func classifyExchangeResponse(data []byte) (string, error) {
	var response struct {
		CaptureStatus string `json:"capture_status"`
		Duplicate     bool   `json:"duplicate"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return "", fmt.Errorf("decoding response: %w", err)
	}
	if response.Duplicate {
		return "duplicate", nil
	}
	switch response.CaptureStatus {
	case "saved":
		return "saved", nil
	case "skipped":
		return "skipped", nil
	default:
		return "", fmt.Errorf("unexpected capture status %q", response.CaptureStatus)
	}
}

func (s Service) uploadArtifacts(ctx context.Context, session Session, report *SessionReport) {
	collector := s.Artifacts
	if collector == nil {
		collector = CheckoutArtifactCollector{}
	}
	if session.Directory == "" {
		return
	}
	artifacts, err := collector.Collect(ctx, session)
	if err != nil {
		report.GitArtifactError = err.Error()
		return
	}
	report.GitArtifactsFound = len(artifacts)
	if len(artifacts) == 0 {
		return
	}
	result, err := s.UploadGitArtifacts(ctx, session, artifacts)
	if err != nil {
		report.GitArtifactError = err.Error()
		return
	}
	report.GitArtifactsFound = result.ArtifactsFound
	report.GitArtifactsSaved = result.ArtifactsSaved
	report.GitArtifactsDuplicate = result.ArtifactsDuplicate
}

// UploadGitArtifacts uploads commit evidence independently of exchange and
// outcome handling. It never calls an outcome endpoint.
func (s Service) UploadGitArtifacts(ctx context.Context, session Session, artifacts []GitArtifact) (GitArtifactUpload, error) {
	result := GitArtifactUpload{ArtifactsFound: len(artifacts)}
	if s.Client == nil {
		return result, errors.New("session import requires a Mimir client")
	}
	if session.ID == "" {
		return result, errors.New("Git artifact upload requires a session id")
	}
	if len(artifacts) == 0 {
		return result, nil
	}
	if len(artifacts) > 50 {
		return result, errors.New("Git artifact upload exceeds 50 commits")
	}
	for _, artifact := range artifacts {
		if !fullGitSHA.MatchString(artifact.CommitSHA) || artifact.ParentCommitSHA != nil && !fullGitSHA.MatchString(*artifact.ParentCommitSHA) {
			return result, errors.New("Git artifact contains an invalid full commit SHA")
		}
		if artifact.Patch == "" {
			return result, errors.New("Git artifact patch is empty")
		}
	}
	headers := make(http.Header)
	if session.Harness != "" {
		headers.Set("x-mimir-harness", session.Harness)
	}
	if session.Repo != "" {
		headers.Set("x-mimir-repo", session.Repo)
	}
	body := struct {
		Version int           `json:"version"`
		Commits []GitArtifact `json:"commits"`
	}{Version: 1, Commits: artifacts}
	encoded, err := json.Marshal(body)
	if err != nil {
		return result, err
	}
	if len(encoded) > 5<<20 {
		return result, errors.New("Git artifact upload exceeds 5 MiB")
	}
	data, err := s.Client.RequestWithHeaders(ctx, http.MethodPost, "/sessions/"+url.PathEscape(session.ID)+"/git-artifacts", body, headers)
	if err != nil {
		return result, err
	}
	var response struct {
		Artifacts  []json.RawMessage `json:"artifacts"`
		Duplicates int               `json:"duplicates"`
	}
	if err := json.Unmarshal(data, &response); err != nil || response.Duplicates < 0 || response.Duplicates > len(response.Artifacts) {
		return result, errors.New("invalid Git artifact response")
	}
	result.ArtifactsDuplicate = response.Duplicates
	result.ArtifactsSaved = len(response.Artifacts) - response.Duplicates
	return result, nil
}

func (s Service) event(ctx context.Context, session Session, headers http.Header, kind string, at time.Time, reason string) error {
	if at.IsZero() {
		return errors.New("session has no timestamp")
	}
	body := map[string]any{"version": 1, "kind": kind, "session_id": session.ID, "harness": session.Harness, "ts": at.UTC().Format(time.RFC3339Nano)}
	if session.Repo != "" {
		body["repo"] = session.Repo
	}
	if title := truncateUTF8(strings.TrimSpace(session.Title), 200); title != "" {
		body["title"] = title
	}
	if reason != "" {
		body["reason"] = reason
	}
	_, err := s.Client.RequestWithHeaders(ctx, http.MethodPost, "/sessions/"+url.PathEscape(session.ID)+"/events", body, headers)
	return err
}

func selected(values []string, candidate string) bool {
	if len(values) == 0 {
		return true
	}
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), candidate) {
			return true
		}
	}
	return false
}

func filterSessions(sessions []Session, options Options) []Session {
	result := make([]Session, 0, len(sessions))
	for _, session := range sessions {
		if len(options.SourceIDs) != 0 && !exactSelected(options.SourceIDs, session.SourceID) {
			continue
		}
		if !options.Since.IsZero() && (session.StartedAt.IsZero() || session.StartedAt.Before(options.Since)) {
			continue
		}
		if !options.Before.IsZero() && (session.StartedAt.IsZero() || !session.StartedAt.Before(options.Before)) {
			continue
		}
		result = append(result, session)
	}
	return result
}

func exactSelected(values []string, candidate string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == candidate {
			return true
		}
	}
	return false
}

func sortSessions(sessions []Session) {
	sort.SliceStable(sessions, func(i, j int) bool {
		if sessions[i].StartedAt.Equal(sessions[j].StartedAt) {
			if sessions[i].Harness == sessions[j].Harness {
				return sessions[i].ID < sessions[j].ID
			}
			return sessions[i].Harness < sessions[j].Harness
		}
		return sessions[i].StartedAt.Before(sessions[j].StartedAt)
	})
}

func appendError(existing, next string) string {
	if existing == "" {
		return next
	}
	return existing + "; " + next
}
