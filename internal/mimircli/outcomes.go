package mimircli

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

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

var _ sessions.GitEvidence = checkoutGitEvidence{}
