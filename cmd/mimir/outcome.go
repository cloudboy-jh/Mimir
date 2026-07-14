package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

type remoteSession struct {
	Session struct {
		ID        string `json:"id"`
		StartedAt string `json:"started_at"`
		SourceRef string `json:"source_ref"`
		Files     string `json:"files"`
	} `json:"session"`
}

// markGitOutcome only applies evidence visible in the current checkout. The
// Worker remains the source of truth; this adapter sends its conclusion there.
func markGitOutcome(ctx context.Context, id string) ([]byte, error) {
	data, err := remoteRequest(ctx, "GET", "/sessions/"+id, nil)
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
	var files []string
	_ = json.Unmarshal([]byte(remote.Session.Files), &files)
	started, err := time.Parse(time.RFC3339, remote.Session.StartedAt)
	if err != nil {
		return nil, fmt.Errorf("invalid session start time: %w", err)
	}
	commits, err := runGit(ctx, ".", "log", "--all", "--format=%H", "--since="+started.Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	outcome := "unknown"
	for _, commit := range strings.Fields(commits) {
		changed, err := runGit(ctx, ".", "show", "--format=", "--name-only", commit)
		if err != nil || !overlaps(files, strings.Fields(changed)) {
			continue
		}
		branches, err := runGit(ctx, ".", "branch", "-r", "--contains", commit)
		if err == nil && durableBranch(strings.Fields(branches)) {
			outcome = "promoted"
			break
		}
	}
	if outcome == "unknown" && remote.Session.SourceRef != "" {
		if _, err := runGit(ctx, ".", "show-ref", "--verify", "--quiet", "refs/remotes/origin/"+remote.Session.SourceRef); err != nil {
			outcome = "discarded"
		}
	}
	if outcome == "unknown" && time.Since(started) >= 7*24*time.Hour {
		outcome = "abandoned"
	}
	return remoteRequest(ctx, "POST", "/sessions/"+id+"/outcome", map[string]string{"outcome": outcome, "source": "git"})
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
