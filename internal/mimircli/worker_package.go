package mimircli

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

func workerDir(explicit string) (string, error) {
	if explicit != "" {
		return filepath.Abs(explicit)
	}
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}
	for {
		for _, candidate := range []string{dir, filepath.Join(dir, "worker")} {
			if pathExists(filepath.Join(candidate, "wrangler.jsonc")) {
				return candidate, nil
			}
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	cache, err := exec.Command("go", "env", "GOMODCACHE").Output()
	if err == nil {
		matches, _ := filepath.Glob(filepath.Join(strings.TrimSpace(string(cache)), "github.com", "cloudboy-jh", "mimir@*", "worker", "wrangler.jsonc"))
		if len(matches) > 0 {
			sort.Strings(matches)
			return filepath.Dir(matches[len(matches)-1]), nil
		}
	}
	return "", fmt.Errorf("could not find the Mimir Worker package; install with go install github.com/cloudboy-jh/mimir/cmd/mimir@latest or pass --worker-dir")
}

// materializeWorker keeps Wrangler's generated config and node modules outside
// the source checkout and Go's read-only module cache. Configured Worker vars
// (dashboard Access) survive re-materialization.
func materializeWorker(source string) (string, error) {
	pointer, err := pointerPath()
	if err != nil {
		return "", err
	}
	target := filepath.Join(filepath.Dir(pointer), "worker")
	preserved := preservedWranglerVars(filepath.Join(target, "wrangler.jsonc"))
	if err := filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return os.MkdirAll(target, 0o700)
		}
		if entry.IsDir() {
			if entry.Name() == "node_modules" || entry.Name() == ".wrangler" {
				return filepath.SkipDir
			}
			return os.MkdirAll(filepath.Join(target, rel), 0o700)
		}
		if strings.HasPrefix(rel, "node_modules"+string(filepath.Separator)) {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(target, rel), data, 0o600)
	}); err != nil {
		return "", err
	}
	for _, name := range []string{"mimir-readme.png", "mimir-favicon-32.png", "mimir-favicon-180.png"} {
		assetSource := filepath.Join(filepath.Dir(source), "assets", "images", name)
		if !pathExists(assetSource) {
			continue
		}
		assetTarget := filepath.Join(filepath.Dir(target), "assets", "images", name)
		if err := os.MkdirAll(filepath.Dir(assetTarget), 0o700); err != nil {
			return "", err
		}
		data, err := os.ReadFile(assetSource)
		if err != nil {
			return "", err
		}
		if err := os.WriteFile(assetTarget, data, 0o600); err != nil {
			return "", err
		}
	}
	if len(preserved) > 0 {
		if err := updateWranglerVars(filepath.Join(target, "wrangler.jsonc"), preserved); err != nil {
			return "", err
		}
	}
	return target, nil
}

// preservedWranglerVars reads Worker vars that must survive re-materialization
// from an existing materialized config.
func preservedWranglerVars(path string) map[string]string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var config struct {
		Vars map[string]any `json:"vars"`
	}
	if json.Unmarshal(stripJSONC(data), &config) != nil {
		return nil
	}
	preserved := map[string]string{}
	for _, key := range []string{"DASHBOARD_ACCESS_AUD", "DASHBOARD_ACCESS_TEAM_DOMAIN"} {
		if value, ok := config.Vars[key].(string); ok && value != "" {
			preserved[key] = value
		}
	}
	return preserved
}

func ensureWorkerDependencies(ctx context.Context, dir string) error {
	hash, err := workerDependencyHash(dir)
	if err != nil {
		return err
	}
	markerPath := filepath.Join(dir, ".mimir-dependencies")
	marker, _ := os.ReadFile(markerPath)
	wranglerReady := pathExists(filepath.Join(dir, "node_modules", ".bin", "wrangler")) || pathExists(filepath.Join(dir, "node_modules", ".bin", "wrangler.cmd"))
	webReady := pathExists(filepath.Join(dir, "web", "node_modules", ".bin", "vite")) || pathExists(filepath.Join(dir, "web", "node_modules", ".bin", "vite.cmd"))
	if wranglerReady && webReady && strings.TrimSpace(string(marker)) == hash {
		return nil
	}
	if _, err := runCommand(ctx, dir, nil, "npm", "ci", "--silent"); err != nil {
		return err
	}
	if _, err := runCommand(ctx, filepath.Join(dir, "web"), nil, "bun", "install", "--frozen-lockfile"); err != nil {
		return err
	}
	return os.WriteFile(markerPath, []byte(hash+"\n"), 0o600)
}

func buildDashboard(ctx context.Context, dir string) error {
	_, err := runCommand(ctx, filepath.Join(dir, "web"), nil, "bun", "run", "build")
	return err
}

func workerDependencyHash(dir string) (string, error) {
	lock, err := os.ReadFile(filepath.Join(dir, "package-lock.json"))
	if err != nil {
		return "", fmt.Errorf("reading Worker package lock: %w", err)
	}
	webLock, err := os.ReadFile(filepath.Join(dir, "web", "bun.lock"))
	if err != nil {
		return "", fmt.Errorf("reading dashboard Bun lock: %w", err)
	}
	return fmt.Sprintf("%x", sha256.Sum256(append(lock, webLock...))), nil
}
