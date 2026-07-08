package git

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

var ErrNotRepo = errors.New("not inside a git repository")

type RepoInfo struct {
	Root        string
	Branch      string
	HeadSHA     string
	Remote      string
	StorePath   string
	StoreExists bool
	IndexedSHA  string
	Stale       bool
}

type indexManifest struct {
	Repo struct {
		IndexedSHA string `json:"indexedSha"`
		HeadSHA    string `json:"headSha"`
		Stale      bool   `json:"stale"`
	} `json:"repo"`
}

func Detect(ctx context.Context, dir string) (RepoInfo, error) {
	root, err := Run(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return RepoInfo{}, ErrNotRepo
	}

	branch, _ := Run(ctx, root, "branch", "--show-current")
	head, _ := Run(ctx, root, "rev-parse", "HEAD")
	remote, _ := Run(ctx, root, "config", "--get", "remote.origin.url")

	storePath := filepath.Join(root, ".churn")
	manifestPath := filepath.Join(storePath, "index.json")
	info := RepoInfo{
		Root:        filepath.Clean(root),
		Branch:      branch,
		HeadSHA:     head,
		Remote:      remote,
		StorePath:   storePath,
		StoreExists: pathExists(manifestPath),
		Stale:       true,
	}

	if info.StoreExists {
		indexedSHA, err := readIndexedSHA(manifestPath)
		if err == nil {
			info.IndexedSHA = indexedSHA
			info.Stale = indexedSHA != head
		}
	}

	return info, nil
}

func Run(ctx context.Context, dir string, args ...string) (string, error) {
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

func readIndexedSHA(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var manifest indexManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return "", err
	}
	return manifest.Repo.IndexedSHA, nil
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
