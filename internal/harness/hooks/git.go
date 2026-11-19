package hooks

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/cloudboy-jh/mimir/internal/sessionimport"
)

// GitInput is the `mimir _hook git` stdin payload. Plugins send it after shell
// tools that printed a commit confirmation, and with Refresh at session end.
type GitInput struct {
	SessionID  string   `json:"session_id"`
	CWD        string   `json:"cwd"`
	Harness    string   `json:"harness"`
	Repo       string   `json:"repo,omitempty"`
	SHAs       []string `json:"shas,omitempty"`
	ToolOutput string   `json:"tool_output,omitempty"`
	Refresh    bool     `json:"refresh,omitempty"`
}

// GitResult is printed on stdout by `mimir _hook git`.
type GitResult struct {
	Captured int      `json:"captured"`
	Pushed   int      `json:"pushed"`
	Errors   []string `json:"errors"`
}

var harnessName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)

// DecodeGitInput validates one JSON object; any error means invalid input.
func DecodeGitInput(input io.Reader) (GitInput, error) {
	data, err := io.ReadAll(io.LimitReader(input, MaxInputBytes+1))
	if err != nil {
		return GitInput{}, err
	}
	if len(data) > MaxInputBytes {
		return GitInput{}, fmt.Errorf("git hook input exceeds %d bytes", MaxInputBytes)
	}
	var parsed GitInput
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&parsed); err != nil {
		return GitInput{}, fmt.Errorf("decoding git hook input: %w", err)
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return GitInput{}, errors.New("git hook input must contain one JSON object")
	}
	parsed.SessionID = strings.TrimSpace(parsed.SessionID)
	parsed.CWD = strings.TrimSpace(parsed.CWD)
	parsed.Harness = strings.TrimSpace(parsed.Harness)
	switch {
	case !safeID.MatchString(parsed.SessionID):
		return GitInput{}, errors.New("git hook input needs a valid session_id")
	case parsed.CWD == "" || !filepath.IsAbs(parsed.CWD):
		return GitInput{}, errors.New("git hook input needs an absolute cwd")
	case !harnessName.MatchString(parsed.Harness):
		return GitInput{}, errors.New("git hook input needs a harness id")
	}
	for _, sha := range parsed.SHAs {
		if !commitHint.MatchString(strings.ToLower(strings.TrimSpace(sha))) {
			return GitInput{}, fmt.Errorf("invalid commit sha %q", sha)
		}
	}
	return parsed, nil
}

// commitHints returns the SHAs named explicitly or printed by `git commit`
// (`[branch sha] subject`), lowercased and de-duplicated in input order.
func commitHints(shas []string, toolOutput string) []string {
	var hints []string
	seen := map[string]bool{}
	add := func(value string) {
		value = strings.ToLower(strings.TrimSpace(value))
		if commitHint.MatchString(value) && !seen[value] {
			seen[value] = true
			hints = append(hints, value)
		}
	}
	for _, sha := range shas {
		add(sha)
	}
	for _, match := range commitConfirmation.FindAllStringSubmatch(toolOutput, -1) {
		add(match[1])
	}
	return hints
}

func (s Service) git(ctx context.Context, dir string, args ...string) (string, error) {
	if s.Git != nil {
		return s.Git(ctx, dir, args...)
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	output, err := cmd.Output()
	return strings.TrimSpace(string(output)), err
}

func (s Service) collector() Collector {
	if s.Collector != nil {
		return s.Collector
	}
	return sessionimport.CheckoutArtifactCollector{MaxPatchBytes: 4 << 20}
}

// pushed reports whether any remote-tracking branch contains sha.
func (s Service) pushed(ctx context.Context, dir, sha string) bool {
	output, err := s.git(ctx, dir, "branch", "-r", "--contains", sha)
	return err == nil && output != ""
}

// collectCommits resolves each hint to the exact commit Git printed and
// collects its bounded redacted patch with the current pushed state.
func (s Service) collectCommits(ctx context.Context, dir string, hints []string) ([]sessionimport.GitArtifact, []string) {
	var artifacts []sessionimport.GitArtifact
	var failures []string
	for _, hint := range hints {
		sha := hint
		if len(hint) < 40 {
			resolved, err := s.git(ctx, dir, "rev-parse", "--verify", hint+"^{commit}")
			if err != nil || len(resolved) != 40 || !strings.HasPrefix(resolved, hint) {
				failures = append(failures, hint+": commit not found in "+dir)
				continue
			}
			sha = resolved
		}
		artifact, err := s.collector().CollectCommit(ctx, dir, sha)
		if err != nil || artifact.CommitSHA != sha {
			failures = append(failures, fmt.Sprintf("%s: %v", sha, err))
			continue
		}
		artifact.Pushed = s.pushed(ctx, dir, sha)
		artifacts = append(artifacts, artifact)
	}
	return artifacts, failures
}

// CaptureGit runs the `mimir _hook git` contract: capture named or printed
// commits, then optionally re-check the session's unpushed commits.
func (s Service) CaptureGit(ctx context.Context, input GitInput) GitResult {
	result := GitResult{Errors: []string{}}
	if s.API == nil {
		result.Errors = append(result.Errors, "Mimir is not connected")
		return result
	}
	repo := input.Repo
	if repo == "" {
		repo = repoName(input.CWD)
	}
	session := sessionimport.Session{ID: input.SessionID, Harness: input.Harness, Repo: repo}
	hints := commitHints(input.SHAs, input.ToolOutput)
	artifacts, failures := s.collectCommits(ctx, input.CWD, hints)
	result.Errors = append(result.Errors, failures...)
	captured := map[string]bool{}
	for start := 0; start < len(artifacts); start += 50 {
		batch := artifacts[start:min(start+50, len(artifacts))]
		if _, err := sessionimport.New(*s.API).UploadGitArtifacts(ctx, session, batch); err != nil {
			result.Errors = append(result.Errors, err.Error())
			continue
		}
		for _, artifact := range batch {
			captured[artifact.CommitSHA] = true
			result.Captured++
			if artifact.Pushed {
				result.Pushed++
			}
		}
	}
	if input.Refresh {
		s.refreshPushed(ctx, session, input.CWD, captured, &result)
	}
	return result
}

// refreshPushed re-posts saved commits of the session tree that were
// unpushed when captured and are now on a remote-tracking branch.
func (s Service) refreshPushed(ctx context.Context, session sessionimport.Session, dir string, skip map[string]bool, result *GitResult) {
	data, err := s.API.Request(ctx, http.MethodGet, "/api/sessions/"+url.PathEscape(session.ID), nil)
	if err != nil {
		result.Errors = append(result.Errors, "refresh: "+err.Error())
		return
	}
	var detail struct {
		Commits []struct {
			SessionID     string `json:"session_id"`
			CommitSHA     string `json:"commit_sha"`
			CaptureStatus string `json:"capture_status"`
			Pushed        bool   `json:"pushed"`
		} `json:"commits"`
	}
	if err := json.Unmarshal(data, &detail); err != nil {
		result.Errors = append(result.Errors, "refresh: invalid session detail")
		return
	}
	bySession := map[string][]sessionimport.GitArtifact{}
	var order []string
	for _, commit := range detail.Commits {
		if commit.Pushed || commit.CaptureStatus != "saved" || skip[commit.CommitSHA] || commit.SessionID == "" || !s.pushed(ctx, dir, commit.CommitSHA) {
			continue
		}
		artifact, err := s.collector().CollectCommit(ctx, dir, commit.CommitSHA)
		if err != nil {
			result.Errors = append(result.Errors, fmt.Sprintf("refresh %s: %v", commit.CommitSHA, err))
			continue
		}
		artifact.Pushed = true
		skip[commit.CommitSHA] = true
		if _, exists := bySession[commit.SessionID]; !exists {
			order = append(order, commit.SessionID)
		}
		bySession[commit.SessionID] = append(bySession[commit.SessionID], artifact)
	}
	for _, id := range order {
		artifacts := bySession[id]
		target := sessionimport.Session{ID: id, Harness: session.Harness, Repo: session.Repo}
		for start := 0; start < len(artifacts); start += 50 {
			batch := artifacts[start:min(start+50, len(artifacts))]
			if _, err := sessionimport.New(*s.API).UploadGitArtifacts(ctx, target, batch); err != nil {
				result.Errors = append(result.Errors, "refresh: "+err.Error())
				continue
			}
			result.Captured += len(batch)
			result.Pushed += len(batch)
		}
	}
}
