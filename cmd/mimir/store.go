package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

const mimirDirName = ".mimir"
const mimirIndexFile = "index.json"
const mimirConfigFile = "config.json"

type mimirIndex struct {
	Repo          string              `json:"repo"`
	IndexedCommit string              `json:"indexed_commit"`
	Timestamp     string              `json:"timestamp"`
	Files         map[string]fileInfo `json:"files"`
	Symbols       map[string]symbol   `json:"symbols"`
}

type fileInfo struct {
	Hash         string   `json:"hash"`
	Symbols      []string `json:"symbols"`
	Dependencies []string `json:"dependencies"`
}

type symbol struct {
	Type      string `json:"type"`
	File      string `json:"file"`
	Line      int    `json:"line"`
	Signature string `json:"signature,omitempty"`
}

type mimirConfig struct {
	IgnorePaths []string `json:"ignore_paths"`
	Budget      int      `json:"budget"`
}

func indexPath(root string) string  { return filepath.Join(root, mimirDirName, mimirIndexFile) }
func configPath(root string) string { return filepath.Join(root, mimirDirName, mimirConfigFile) }

func loadIndex(root string) (mimirIndex, error) {
	data, err := os.ReadFile(indexPath(root))
	if err != nil {
		return mimirIndex{}, err
	}
	var idx mimirIndex
	if err := json.Unmarshal(data, &idx); err != nil {
		return mimirIndex{}, err
	}
	if idx.Files == nil {
		idx.Files = map[string]fileInfo{}
	}
	if idx.Symbols == nil {
		idx.Symbols = map[string]symbol{}
	}
	return idx, nil
}

func saveIndexAtomic(root string, idx mimirIndex) error {
	dir := filepath.Join(root, mimirDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(idx, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".index-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write([]byte("\n")); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpPath, indexPath(root))
}

func loadConfig(root string) mimirConfig {
	cfg := mimirConfig{Budget: 4000, IgnorePaths: []string{".git/", ".mimir/", "node_modules/", "vendor/", "dist/", "build/", "coverage/"}}
	data, err := os.ReadFile(configPath(root))
	if err != nil {
		return cfg
	}
	_ = json.Unmarshal(data, &cfg)
	if cfg.Budget <= 0 {
		cfg.Budget = 4000
	}
	return cfg
}

func missing(err error) bool { return errors.Is(err, os.ErrNotExist) }

func ignored(path string, cfg mimirConfig) bool {
	p := filepath.ToSlash(path)
	for _, raw := range cfg.IgnorePaths {
		pat := filepath.ToSlash(strings.TrimSpace(raw))
		if pat == "" {
			continue
		}
		if strings.HasSuffix(pat, "/") && strings.HasPrefix(p, pat) {
			return true
		}
		if p == pat || strings.HasPrefix(p, pat+"/") {
			return true
		}
	}
	return false
}
