package main

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

var errNotRepo = errors.New("not inside a git repository")

type repoInfo struct {
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
	IndexedCommit string `json:"indexed_commit"`
}

func detectRepo(ctx context.Context, dir string) (repoInfo, error) {
	root, err := runGit(ctx, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		return repoInfo{}, errNotRepo
	}

	branch, _ := runGit(ctx, root, "branch", "--show-current")
	head, _ := runGit(ctx, root, "rev-parse", "HEAD")
	remote, _ := runGit(ctx, root, "config", "--get", "remote.origin.url")

	storePath := filepath.Join(root, ".churn")
	manifestPath := filepath.Join(storePath, "index.json")
	info := repoInfo{
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

func readIndexedSHA(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	var manifest indexManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return "", err
	}
	return manifest.IndexedCommit, nil
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
