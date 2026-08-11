package install

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cloudboy-jh/mimir/internal/jsonconfig"
)

func workerDir(explicit string) (string, error) {
	if explicit != "" {
		return filepath.Abs(explicit)
	}
	return materializeEmbeddedWorker()
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
	sourceAbs, err := filepath.Abs(source)
	if err != nil {
		return "", err
	}
	targetAbs, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	if err := requireMaterializationDir(sourceAbs, sourceAbs, false); err != nil {
		return "", fmt.Errorf("invalid Worker source: %w", err)
	}
	if filepath.Clean(sourceAbs) == filepath.Clean(targetAbs) {
		return target, nil
	}
	if pathWithin(sourceAbs, targetAbs) || pathWithin(targetAbs, sourceAbs) {
		return "", fmt.Errorf("Worker source and destination overlap")
	}
	if err := requireMaterializationDir(targetAbs, targetAbs, true); err != nil {
		return "", fmt.Errorf("invalid Worker destination: %w", err)
	}
	wranglerTarget := filepath.Join(targetAbs, "wrangler.jsonc")
	if err := validateMaterializationDestination(targetAbs, wranglerTarget); err != nil {
		return "", err
	}
	preserved := preservedWranglerVars(wranglerTarget)
	if err := filepath.WalkDir(sourceAbs, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(sourceAbs, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		if entry.IsDir() {
			if entry.Name() == "node_modules" || entry.Name() == ".wrangler" {
				return filepath.SkipDir
			}
			if err := requireMaterializationDir(sourceAbs, path, false); err != nil {
				return err
			}
			return requireMaterializationDir(targetAbs, filepath.Join(targetAbs, rel), true)
		}
		if strings.HasPrefix(rel, "node_modules"+string(filepath.Separator)) {
			return nil
		}
		data, err := readMaterializationFile(sourceAbs, path)
		if err != nil {
			return err
		}
		return writeMaterializationFile(targetAbs, filepath.Join(targetAbs, rel), data)
	}); err != nil {
		return "", err
	}
	assetSourceRoot := filepath.Join(filepath.Dir(sourceAbs), "assets", "images")
	assetTargetRoot := filepath.Join(filepath.Dir(targetAbs), "assets", "images")
	for _, name := range []string{"mimir-readme.png", "mimir-favicon-32.png", "mimir-favicon-180.png"} {
		assetSource := filepath.Join(assetSourceRoot, name)
		if _, err := os.Lstat(assetSource); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return "", err
		}
		data, err := readMaterializationFile(assetSourceRoot, assetSource)
		if err != nil {
			return "", err
		}
		if err := requireMaterializationDir(assetTargetRoot, assetTargetRoot, true); err != nil {
			return "", err
		}
		if err := writeMaterializationFile(assetTargetRoot, filepath.Join(assetTargetRoot, name), data); err != nil {
			return "", err
		}
	}
	if len(preserved) > 0 {
		if err := updateMaterializedWranglerVars(targetAbs, wranglerTarget, preserved); err != nil {
			return "", err
		}
	}
	return target, nil
}

func pathWithin(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func requireMaterializationDir(root, path string, create bool) error {
	if symlink, err := pathContainsSymlink(root, path); err != nil {
		return err
	} else if symlink {
		return fmt.Errorf("refusing symlinked directory %s", path)
	}
	if create {
		if err := os.MkdirAll(path, 0o700); err != nil {
			return err
		}
		if symlink, err := pathContainsSymlink(root, path); err != nil {
			return err
		} else if symlink {
			return fmt.Errorf("refusing symlinked directory %s", path)
		}
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("directory path is non-directory: %s", path)
	}
	return nil
}

func readMaterializationFile(root, path string) ([]byte, error) {
	if symlink, err := pathContainsSymlink(root, path); err != nil {
		return nil, err
	} else if symlink {
		return nil, fmt.Errorf("refusing symlinked source file %s", path)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("source file is non-regular: %s", path)
	}
	return os.ReadFile(path)
}

func writeMaterializationFile(root, path string, data []byte) error {
	if err := validateMaterializationDestination(root, path); err != nil {
		return err
	}
	return writeFileAtomic(root, path, data)
}

func validateMaterializationDestination(root, path string) error {
	if symlink, err := pathContainsSymlink(root, path); err != nil {
		return err
	} else if symlink {
		return fmt.Errorf("refusing symlinked destination file %s", path)
	}
	if info, err := os.Lstat(path); err == nil && !info.Mode().IsRegular() {
		return fmt.Errorf("refusing non-regular destination file %s", path)
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func updateMaterializedWranglerVars(root, path string, vars map[string]string) error {
	data, err := readMaterializationFile(root, path)
	if err != nil {
		return err
	}
	var config map[string]any
	if err := json.Unmarshal(jsonconfig.StripJSONC(data), &config); err != nil {
		return err
	}
	existing, _ := config["vars"].(map[string]any)
	if existing == nil {
		existing = map[string]any{}
	}
	for key, value := range vars {
		existing[key] = value
	}
	config["vars"] = existing
	data, err = json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	return writeMaterializationFile(root, path, append(data, '\n'))
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
	if json.Unmarshal(jsonconfig.StripJSONC(data), &config) != nil {
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
	if wranglerReady && strings.TrimSpace(string(marker)) == hash {
		return nil
	}
	if _, err := runCommand(ctx, dir, nil, "npm", "ci", "--silent"); err != nil {
		return err
	}
	return os.WriteFile(markerPath, []byte(hash+"\n"), 0o600)
}

func ensureDashboardDependencies(ctx context.Context, dir string) error {
	hash, err := dashboardDependencyHash(dir)
	if err != nil {
		return err
	}
	webDir := filepath.Join(dir, "web")
	markerPath := filepath.Join(webDir, ".mimir-dependencies")
	marker, _ := os.ReadFile(markerPath)
	viteReady := pathExists(filepath.Join(webDir, "node_modules", ".bin", "vite")) || pathExists(filepath.Join(webDir, "node_modules", ".bin", "vite.cmd"))
	if viteReady && strings.TrimSpace(string(marker)) == hash {
		return nil
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
	return fmt.Sprintf("%x", sha256.Sum256(lock)), nil
}

func dashboardDependencyHash(dir string) (string, error) {
	webLock, err := os.ReadFile(filepath.Join(dir, "web", "bun.lock"))
	if err != nil {
		return "", fmt.Errorf("reading dashboard Bun lock: %w", err)
	}
	return fmt.Sprintf("%x", sha256.Sum256(webLock)), nil
}
