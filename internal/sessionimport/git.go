package sessionimport

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const defaultMaxGitCandidates = 200
const defaultMaxGitCommandBytes = 8 << 20

type GitArtifact struct {
	CommitSHA       string  `json:"commit_sha"`
	ParentCommitSHA *string `json:"parent_commit_sha"`
	CommittedAt     string  `json:"committed_at"`
	Subject         string  `json:"subject"`
	Patch           string  `json:"patch"`
	RepositoryURL   string  `json:"repository_url,omitempty"`
	Ref             string  `json:"ref,omitempty"`
}

type ArtifactCollector interface {
	Collect(context.Context, Session) ([]GitArtifact, error)
}

type GitCommandFunc func(context.Context, string, ...string) ([]byte, error)

type CheckoutArtifactCollector struct {
	Command         GitCommandFunc
	MaxArtifacts    int
	MaxCandidates   int
	MaxPatchBytes   int
	MaxCommandBytes int
}

type gitCandidate struct {
	SHA       string
	Parent    string
	Committed time.Time
	Subject   string
	Ref       string
}

func (c CheckoutArtifactCollector) Collect(ctx context.Context, session Session) ([]GitArtifact, error) {
	if session.Directory == "" || session.StartedAt.IsZero() {
		return nil, nil
	}
	maxArtifacts := c.MaxArtifacts
	if maxArtifacts <= 0 {
		maxArtifacts = DefaultMaxGitArtifacts
	}
	if maxArtifacts > 50 {
		maxArtifacts = 50
	}
	maxCandidates := c.MaxCandidates
	if maxCandidates <= 0 {
		maxCandidates = defaultMaxGitCandidates
	}
	rootData, err := c.run(ctx, session.Directory, 64<<10, "rev-parse", "--show-toplevel")
	if err != nil {
		return nil, fmt.Errorf("locating checkout: %w", err)
	}
	root := strings.TrimSpace(string(rootData))
	if root == "" {
		return nil, errors.New("Git checkout has no root")
	}
	end := session.EndedAt
	if end.IsZero() {
		end = session.StartedAt
		if len(session.Exchanges) != 0 {
			end = session.Exchanges[len(session.Exchanges)-1].Timestamp
		}
	}
	format := "%H%x00%P%x00%cI%x00%s%x00%D%x1e"
	logData, err := c.run(ctx, root, c.commandLimit(), "log", "--all", "--since="+session.StartedAt.UTC().Format(time.RFC3339), "--until="+end.UTC().Format(time.RFC3339), "--format="+format, "-n", fmt.Sprint(maxCandidates))
	if err != nil {
		return nil, fmt.Errorf("listing commits: %w", err)
	}
	candidates := parseGitCandidates(logData)
	paths := sessionToolPaths(session)
	origin := ""
	if data, err := c.run(ctx, root, 64<<10, "remote", "get-url", "origin"); err == nil {
		origin = normalizeGitOrigin(string(data))
	}
	artifacts := make([]GitArtifact, 0, min(maxArtifacts, len(candidates)))
	for _, candidate := range candidates {
		if err := ctx.Err(); err != nil {
			return artifacts, err
		}
		if len(paths) != 0 {
			filesData, err := c.run(ctx, root, 1<<20, "diff-tree", "--no-commit-id", "--name-only", "-r", "--root", candidate.SHA)
			if err == nil && !overlapsPaths(paths, strings.Fields(string(filesData))) {
				continue
			}
		}
		patchData, err := c.run(ctx, root, c.patchLimit(), "show", "--format=", "--patch", "--unified=3", "--no-ext-diff", candidate.SHA)
		if err != nil {
			continue
		}
		patch := redactGitPatch(string(patchData))
		if strings.TrimSpace(patch) == "" {
			continue
		}
		artifact := GitArtifact{CommitSHA: candidate.SHA, CommittedAt: jsTimestamp(candidate.Committed), Subject: sanitizeSubject(candidate.Subject), Patch: patch, RepositoryURL: origin, Ref: candidate.Ref}
		if candidate.Parent != "" {
			parent := candidate.Parent
			artifact.ParentCommitSHA = &parent
		}
		artifacts = append(artifacts, artifact)
		if len(artifacts) == maxArtifacts {
			break
		}
	}
	return artifacts, nil
}

func (c CheckoutArtifactCollector) run(ctx context.Context, directory string, maxBytes int, args ...string) ([]byte, error) {
	if c.Command != nil {
		data, err := c.Command(ctx, directory, args...)
		if len(data) > maxBytes {
			return nil, fmt.Errorf("git output exceeds %d bytes", maxBytes)
		}
		return data, err
	}
	command := exec.CommandContext(ctx, "git", args...)
	command.Dir = directory
	command.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	stdout := &limitBuffer{remaining: maxBytes}
	stderr := &limitBuffer{remaining: 64 << 10}
	command.Stdout, command.Stderr = stdout, stderr
	if err := command.Run(); err != nil {
		if errors.Is(stdout.err, errOutputLimit) {
			return nil, fmt.Errorf("git output exceeds %d bytes", maxBytes)
		}
		if detail := strings.TrimSpace(stderr.String()); detail != "" {
			return nil, fmt.Errorf("git %s: %s: %w", strings.Join(args, " "), detail, err)
		}
		return nil, err
	}
	return stdout.Bytes(), nil
}

func (c CheckoutArtifactCollector) commandLimit() int {
	if c.MaxCommandBytes > 0 {
		return c.MaxCommandBytes
	}
	return defaultMaxGitCommandBytes
}

func (c CheckoutArtifactCollector) patchLimit() int {
	if c.MaxPatchBytes > 0 {
		return c.MaxPatchBytes
	}
	return DefaultMaxGitPatchBytes
}

func parseGitCandidates(data []byte) []gitCandidate {
	records := bytes.Split(data, []byte{0x1e})
	result := make([]gitCandidate, 0, len(records))
	for _, record := range records {
		fields := bytes.Split(bytes.TrimSpace(record), []byte{0})
		if len(fields) != 5 {
			continue
		}
		sha := strings.ToLower(string(fields[0]))
		if !fullGitSHA.MatchString(sha) {
			continue
		}
		committed, err := time.Parse(time.RFC3339, string(fields[2]))
		if err != nil {
			continue
		}
		parents := strings.Fields(string(fields[1]))
		parent := ""
		if len(parents) != 0 && fullGitSHA.MatchString(parents[0]) {
			parent = parents[0]
		}
		result = append(result, gitCandidate{SHA: sha, Parent: parent, Committed: committed, Subject: string(fields[3]), Ref: historicalRef(string(fields[4]))})
	}
	return result
}

var fullGitSHA = regexp.MustCompile(`^[0-9a-f]{40}$`)
var gitPatchSecrets = []*regexp.Regexp{
	regexp.MustCompile(`(?:sk|pk|rk)_[A-Za-z0-9_-]{16,}`),
	regexp.MustCompile(`(?i)(Bearer\s+)[A-Za-z0-9._-]+`),
	regexp.MustCompile(`(?i)((?:api[_-]?key|token|secret|password)["']?\s*[:=]\s*["']?)[^\s,"'}]+`),
}

func redactGitPatch(value string) string {
	value = gitPatchSecrets[0].ReplaceAllString(value, "[REDACTED]")
	value = gitPatchSecrets[1].ReplaceAllString(value, "${1}[REDACTED]")
	return gitPatchSecrets[2].ReplaceAllString(value, "${1}[REDACTED]")
}

func normalizeGitOrigin(raw string) string {
	value := strings.TrimSpace(raw)
	if match := regexp.MustCompile(`^[A-Za-z0-9._-]+@([^:/]+):(.+)$`).FindStringSubmatch(value); len(match) == 3 && !strings.HasPrefix(match[2], "/") {
		value = "https://" + match[1] + "/" + match[2]
	} else {
		value = regexp.MustCompile(`^ssh://(?:[^@/]+@)?`).ReplaceAllString(value, "https://")
		value = regexp.MustCompile(`^(?:git|http)://`).ReplaceAllString(value, "https://")
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" {
		return ""
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.Fragment = ""
	parsed.Path = strings.TrimSuffix(strings.TrimRight(parsed.Path, "/"), ".git")
	if parsed.Path == "" {
		return ""
	}
	return "https://" + strings.ToLower(parsed.Host) + parsed.EscapedPath()
}

func historicalRef(decorations string) string {
	for _, value := range strings.Split(decorations, ",") {
		value = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(value), "tag: "))
		if strings.Contains(value, " -> ") {
			value = strings.TrimSpace(strings.SplitN(value, " -> ", 2)[1])
		}
		if value != "" && value != "HEAD" {
			return truncateUTF8(value, 512)
		}
	}
	return ""
}

func sessionToolPaths(session Session) []string {
	set := map[string]struct{}{}
	for _, exchange := range session.Exchanges {
		for _, tool := range exchange.Tools {
			collectPathValues(tool.Input, "", set)
		}
	}
	paths := make([]string, 0, len(set))
	for path := range set {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	return paths
}

func collectPathValues(value any, key string, paths map[string]struct{}) {
	switch typed := value.(type) {
	case map[string]any:
		for childKey, child := range typed {
			collectPathValues(child, childKey, paths)
		}
	case []any:
		for _, child := range typed {
			collectPathValues(child, key, paths)
		}
	case string:
		lower := strings.ToLower(key)
		if strings.Contains(lower, "path") || strings.Contains(lower, "file") {
			if normalized := normalizeArtifactPath(typed); normalized != "" {
				paths[normalized] = struct{}{}
			}
		}
	}
}

func normalizeArtifactPath(value string) string {
	value = strings.TrimSpace(strings.ReplaceAll(value, "\\", "/"))
	value = strings.TrimPrefix(filepath.ToSlash(filepath.Clean(value)), "./")
	if value == "." || strings.ContainsRune(value, '\x00') {
		return ""
	}
	return strings.TrimPrefix(value, "/")
}

func overlapsPaths(wanted, changed []string) bool {
	for _, changedPath := range changed {
		changedPath = normalizeArtifactPath(changedPath)
		for _, wantedPath := range wanted {
			if changedPath == wantedPath || strings.HasSuffix(wantedPath, "/"+changedPath) || strings.HasSuffix(changedPath, "/"+wantedPath) {
				return true
			}
		}
	}
	return false
}

func sanitizeSubject(value string) string {
	value = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, value)
	return truncateUTF8(value, 500)
}

func jsTimestamp(value time.Time) string {
	return value.UTC().Format("2006-01-02T15:04:05.000Z")
}
