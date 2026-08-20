package sessionimport

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestCheckoutArtifactCollectorFiltersNormalizesAndRedacts(t *testing.T) {
	start := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	first := strings.Repeat("a", 40)
	second := strings.Repeat("b", 40)
	parent := strings.Repeat("c", 40)
	var logArgs string
	collector := CheckoutArtifactCollector{Command: func(_ context.Context, directory string, args ...string) ([]byte, error) {
		switch args[0] {
		case "rev-parse":
			if directory != "/recorded/checkout" {
				t.Fatalf("initial directory = %q", directory)
			}
			return []byte("/repo\n"), nil
		case "log":
			logArgs = strings.Join(args, " ")
			return []byte(fmt.Sprintf("%s\x00%s\x002026-08-20T11:00:00+00:00\x00target work\x00HEAD -> main, origin/main\x1e%s\x00\x002026-08-20T11:30:00+00:00\x00other work\x00other\x1e", first, parent, second)), nil
		case "remote":
			return []byte("https://token:secret@GitHub.com/acme/repo.git?credential=x#fragment\n"), nil
		case "diff-tree":
			if args[len(args)-1] == first {
				return []byte("src/target.go\n"), nil
			}
			return []byte("src/unrelated.go\n"), nil
		case "show":
			return []byte("diff --git a/src/target.go b/src/target.go\n+Bearer secret-token\n+api_key=supersecretvalue\n"), nil
		default:
			return nil, fmt.Errorf("unexpected git command %v", args)
		}
	}}
	session := Session{Directory: "/recorded/checkout", StartedAt: start, EndedAt: start.Add(time.Hour), Exchanges: []Exchange{{Tools: []ToolActivity{{Name: "edit", Input: map[string]any{"file_path": "/repo/src/target.go"}}}}}}
	artifacts, err := collector.Collect(context.Background(), session)
	if err != nil {
		t.Fatal(err)
	}
	if len(artifacts) != 1 {
		t.Fatalf("artifacts = %#v", artifacts)
	}
	artifact := artifacts[0]
	if artifact.CommitSHA != first || artifact.ParentCommitSHA == nil || *artifact.ParentCommitSHA != parent || artifact.Ref != "main" {
		t.Fatalf("artifact identity = %#v", artifact)
	}
	if artifact.CommittedAt != "2026-08-20T11:00:00.000Z" || artifact.RepositoryURL != "https://github.com/acme/repo" {
		t.Fatalf("artifact metadata = %#v", artifact)
	}
	if strings.Contains(artifact.Patch, "secret-token") || strings.Contains(artifact.Patch, "supersecretvalue") || !strings.Contains(artifact.Patch, "[REDACTED]") {
		t.Fatalf("patch = %q", artifact.Patch)
	}
	if !strings.Contains(logArgs, "--since=2026-08-20T10:00:00Z") || !strings.Contains(logArgs, "--until=2026-08-20T11:00:00Z") {
		t.Fatalf("log args = %q", logArgs)
	}
}

func TestCheckoutArtifactCollectorSkipsOversizedPatch(t *testing.T) {
	start := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	sha := strings.Repeat("d", 40)
	collector := CheckoutArtifactCollector{MaxArtifacts: 1, MaxPatchBytes: 8, Command: func(_ context.Context, _ string, args ...string) ([]byte, error) {
		switch args[0] {
		case "rev-parse":
			return []byte("/repo"), nil
		case "log":
			return []byte(fmt.Sprintf("%s\x00\x002026-08-20T10:01:00Z\x00one\x00main\x1e", sha)), nil
		case "remote":
			return nil, fmt.Errorf("no remote")
		case "show":
			return []byte("1234567émore"), nil
		default:
			return nil, fmt.Errorf("unexpected command")
		}
	}}
	artifacts, err := collector.Collect(context.Background(), Session{Directory: "/repo", StartedAt: start})
	if err != nil {
		t.Fatal(err)
	}
	if len(artifacts) != 0 {
		t.Fatalf("artifacts = %#v", artifacts)
	}
}

func TestNormalizeGitOriginRejectsCredentialsAndLocalPaths(t *testing.T) {
	if got := normalizeGitOrigin("git@github.com:acme/repo.git"); got != "https://github.com/acme/repo" {
		t.Fatalf("scp origin = %q", got)
	}
	if got := normalizeGitOrigin("https://user:pass@example.com/org/repo.git"); got != "https://example.com/org/repo" {
		t.Fatalf("credential origin = %q", got)
	}
	if got := normalizeGitOrigin("C:/secret/repo"); got != "" {
		t.Fatalf("local origin = %q", got)
	}
}
