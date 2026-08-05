package sessions

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"
)

type SetOutcomeOptions struct {
	Outcome     string
	Reason      string
	Evidence    any
	EvidenceSet bool
}

type EndOptions struct {
	Outcome     string
	Reason      string
	Evidence    any
	EvidenceSet bool
}

type GitEvidence interface {
	CommitsSince(context.Context, time.Time) ([]string, error)
	FilesChanged(context.Context, string) ([]string, error)
	RemoteBranchesContaining(context.Context, string) ([]string, error)
	Patch(context.Context, string) (string, error)
	RepositoryURL(context.Context) (string, error)
	Ref(context.Context) (string, error)
}

type remoteSession struct {
	Session struct {
		ID        string `json:"id"`
		StartedAt string `json:"started_at"`
	} `json:"session"`
	Files []string `json:"files"`
}

func (s Service) SetOutcome(ctx context.Context, id string, options SetOutcomeOptions) ([]byte, error) {
	if !ValidOutcome(options.Outcome) {
		return nil, fmt.Errorf("invalid outcome %q: must be landed, discarded, abandoned, or unresolved", options.Outcome)
	}
	body := map[string]any{"outcome": options.Outcome, "source": "agent"}
	if options.Reason != "" {
		body["reason"] = options.Reason
	}
	if options.EvidenceSet {
		body["evidence"] = options.Evidence
	}
	return s.API.Request(ctx, "POST", "/sessions/"+url.PathEscape(id)+"/outcome", body)
}

func (s Service) End(ctx context.Context, id string, options EndOptions) (Status, error) {
	if options.Outcome != "" && !ValidOutcome(options.Outcome) {
		return Status{}, fmt.Errorf("invalid outcome %q: must be landed, discarded, abandoned, or unresolved", options.Outcome)
	}
	if options.Outcome == "" && options.Reason != "" {
		return Status{}, fmt.Errorf("reason requires outcome")
	}
	if options.Outcome == "" && options.EvidenceSet {
		return Status{}, fmt.Errorf("evidence requires outcome")
	}
	body := map[string]any{}
	if options.Outcome != "" {
		body["outcome"] = options.Outcome
	}
	if options.Reason != "" {
		body["reason"] = options.Reason
	}
	if options.EvidenceSet {
		body["evidence"] = options.Evidence
	}
	if _, err := s.API.Request(ctx, "POST", "/sessions/"+url.PathEscape(id)+"/end", body); err != nil {
		return Status{}, err
	}
	return s.GetStatus(ctx, id)
}

// SetGitOutcome applies only evidence visible in the supplied checkout. The
// Worker remains the source of truth for the recorded outcome.
func (s Service) SetGitOutcome(ctx context.Context, id string, git GitEvidence) ([]byte, error) {
	data, err := s.API.Request(ctx, "GET", "/sessions/"+url.PathEscape(id), nil)
	if err != nil {
		return nil, err
	}
	var remote remoteSession
	if err := json.Unmarshal(data, &remote); err != nil {
		return nil, err
	}
	if remote.Session.ID == "" {
		return nil, fmt.Errorf("session not found: %s", id)
	}
	started, err := time.Parse(time.RFC3339, remote.Session.StartedAt)
	if err != nil {
		return nil, fmt.Errorf("invalid session start time: %w", err)
	}
	commits, err := git.CommitsSince(ctx, started)
	if err != nil {
		return nil, err
	}
	options := SetOutcomeOptions{
		Outcome:     "unresolved",
		Reason:      "no durable Git evidence found for files captured in the session",
		Evidence:    "session started at " + started.Format(time.RFC3339),
		EvidenceSet: true,
	}
	for _, commit := range commits {
		changed, err := git.FilesChanged(ctx, commit)
		if err != nil || !overlaps(remote.Files, changed) {
			continue
		}
		branches, err := git.RemoteBranchesContaining(ctx, commit)
		if err == nil && durableBranch(branches) {
			options.Outcome = "landed"
			options.Reason = "a commit touching captured session files is reachable from a durable remote branch"
			evidence := map[string]any{"commit": commit, "provenance": "git"}
			if repository, repositoryErr := git.RepositoryURL(ctx); repositoryErr == nil && repository != "" {
				evidence["repository_url"] = repository
			}
			if ref, refErr := git.Ref(ctx); refErr == nil && ref != "" && ref != "HEAD" {
				evidence["ref"] = ref
			}
			if patch, patchErr := git.Patch(ctx, commit); patchErr == nil && patch != "" {
				evidence["patch"] = patch
			}
			options.Evidence = evidence
			break
		}
	}
	return s.SetOutcome(ctx, id, options)
}

func overlaps(expected, changed []string) bool {
	if len(expected) == 0 {
		return false
	}
	for _, left := range expected {
		for _, right := range changed {
			if left == right || strings.HasSuffix(left, "/"+right) || strings.HasSuffix(right, "/"+left) {
				return true
			}
		}
	}
	return false
}

func durableBranch(branches []string) bool {
	for _, branch := range branches {
		branch = strings.TrimSpace(branch)
		if strings.HasSuffix(branch, "/main") || strings.HasSuffix(branch, "/master") || strings.HasSuffix(branch, "/HEAD") {
			return true
		}
	}
	return false
}
