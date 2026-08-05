package mimircli

import (
	"context"
	"fmt"
	"net/url"
	"os/exec"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/cloudboy-jh/mimir/internal/sessions"
)

func runGit(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		if text == "" {
			return "", err
		}
		return "", fmt.Errorf("git %s: %s: %w", strings.Join(args, " "), text, err)
	}
	return text, nil
}

type checkoutGitEvidence struct {
	Dir string
}

func (g checkoutGitEvidence) CommitsSince(ctx context.Context, started time.Time) ([]string, error) {
	output, err := runGit(ctx, g.Dir, "log", "--all", "--format=%H", "--since="+started.Format(time.RFC3339))
	return strings.Fields(output), err
}

func (g checkoutGitEvidence) FilesChanged(ctx context.Context, commit string) ([]string, error) {
	output, err := runGit(ctx, g.Dir, "show", "--format=", "--name-only", commit)
	return strings.Fields(output), err
}

func (g checkoutGitEvidence) RemoteBranchesContaining(ctx context.Context, commit string) ([]string, error) {
	output, err := runGit(ctx, g.Dir, "branch", "-r", "--contains", commit)
	return strings.Fields(output), err
}

func (g checkoutGitEvidence) Patch(ctx context.Context, commit string) (string, error) {
	output, err := runGit(ctx, g.Dir, "show", "--format=", "--patch", "--unified=3", commit)
	if err != nil {
		return "", err
	}
	return boundedGitEvidence(redactGitEvidence(output), 20*1024), nil
}

func (g checkoutGitEvidence) RepositoryURL(ctx context.Context) (string, error) {
	output, err := runGit(ctx, g.Dir, "remote", "get-url", "origin")
	if err != nil {
		return "", err
	}
	return normalizeGitRemote(output), nil
}

func (g checkoutGitEvidence) Ref(ctx context.Context) (string, error) {
	return runGit(ctx, g.Dir, "rev-parse", "--abbrev-ref", "HEAD")
}

var gitEvidenceSecrets = []*regexp.Regexp{
	regexp.MustCompile(`(?:sk|pk|rk)_[A-Za-z0-9_-]{16,}`),
	regexp.MustCompile(`(?i)(Bearer\s+)[A-Za-z0-9._-]+`),
	regexp.MustCompile(`(?i)((?:api[_-]?key|token|secret|password)["']?\s*[:=]\s*["']?)[^\s,"'}]+`),
}

func redactGitEvidence(value string) string {
	value = gitEvidenceSecrets[0].ReplaceAllString(value, "[REDACTED]")
	value = gitEvidenceSecrets[1].ReplaceAllString(value, "${1}[REDACTED]")
	return gitEvidenceSecrets[2].ReplaceAllString(value, "${1}[REDACTED]")
}

func boundedGitEvidence(value string, maxBytes int) string {
	if len(value) <= maxBytes {
		return value
	}
	value = value[:maxBytes]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}

func normalizeGitRemote(raw string) string {
	value := strings.TrimSpace(raw)
	if match := regexp.MustCompile(`^[A-Za-z0-9._-]+@([^:/]+):(.+)$`).FindStringSubmatch(value); len(match) == 3 && !strings.HasPrefix(match[2], "/") {
		value = "https://" + match[1] + "/" + match[2]
	} else {
		value = regexp.MustCompile(`^ssh://(?:[^@/]+@)?`).ReplaceAllString(value, "https://")
		value = regexp.MustCompile(`^git://`).ReplaceAllString(value, "https://")
		value = regexp.MustCompile(`^http://`).ReplaceAllString(value, "https://")
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || !strings.Contains(parsed.Hostname(), ".") {
		return ""
	}
	path := strings.TrimSuffix(strings.TrimRight(parsed.Path, "/"), ".git")
	if path == "" || path == "/" {
		return ""
	}
	return "https://" + parsed.Hostname() + path
}

var _ sessions.GitEvidence = checkoutGitEvidence{}
