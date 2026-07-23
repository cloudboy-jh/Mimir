package mimircli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	mimirassets "github.com/cloudboy-jh/mimir"
)

const embeddedWorkerManifestSchema = 1

type embeddedWorkerManifest struct {
	Schema int               `json:"schema"`
	Files  map[string]string `json:"files"`
}

// materializeEmbeddedWorker writes only bundled Worker inputs and required
// shared assets. Generated dependencies, Wrangler state, dashboard output,
// and unrelated files are left in place.
func materializeEmbeddedWorker() (string, error) {
	paths, err := managedInstallationPaths()
	if err != nil {
		return "", err
	}
	wranglerPath := filepath.Join(paths.Worker, "wrangler.jsonc")
	if symlink, err := pathContainsSymlink(paths.Worker, wranglerPath); err != nil {
		return "", err
	} else if symlink {
		return "", fmt.Errorf("refusing to materialize symlinked path %s", wranglerPath)
	}
	preserved := preservedWranglerVars(wranglerPath)
	manifestPath := filepath.Join(paths.MimirHome, "embedded-worker-manifest.json")
	previous, err := loadEmbeddedWorkerManifest(manifestPath)
	if err != nil {
		return "", err
	}
	metadata, err := mimirassets.BundleMetadata()
	if err != nil {
		return "", err
	}
	next := embeddedWorkerManifest{Schema: embeddedWorkerManifestSchema, Files: make(map[string]string)}
	for _, file := range metadata {
		var target, root string
		switch {
		case strings.HasPrefix(file.Path, "worker/"):
			rel := strings.TrimPrefix(file.Path, "worker/")
			target, root = filepath.Join(paths.Worker, filepath.FromSlash(rel)), paths.Worker
		case strings.HasPrefix(file.Path, "assets/images/"):
			rel := strings.TrimPrefix(file.Path, "assets/images/")
			target, root = filepath.Join(paths.SharedAssets, filepath.FromSlash(rel)), paths.SharedAssets
		default:
			continue
		}
		data, err := mimirassets.Bundle.ReadFile(file.Path)
		if err != nil {
			return "", err
		}
		if file.Path == "worker/wrangler.jsonc" && len(preserved) > 0 {
			data, err = mergeEmbeddedWranglerVars(data, preserved)
			if err != nil {
				return "", err
			}
		}
		next.Files[target] = hashBytes(data)
		if symlink, err := pathContainsSymlink(root, target); err != nil {
			return "", err
		} else if symlink {
			return "", fmt.Errorf("refusing to materialize symlinked path %s", target)
		}
		if current, err := os.ReadFile(target); err == nil && hashBytes(current) == hashBytes(data) {
			continue
		} else if err != nil && !os.IsNotExist(err) {
			return "", err
		}
		if err := writeFileAtomic(root, target, data); err != nil {
			return "", err
		}
	}
	obsolete := make([]string, 0)
	for target := range previous.Files {
		if _, bundled := next.Files[target]; !bundled {
			obsolete = append(obsolete, target)
		}
	}
	sort.Strings(obsolete)
	for _, target := range obsolete {
		priorHash := previous.Files[target]
		removed, err := removeObsoleteEmbeddedFile(paths, target, priorHash)
		if err != nil {
			return "", err
		}
		if !removed {
			next.Files[target] = priorHash
		}
	}
	if err := writeJSONAtomic(manifestPath, next); err != nil {
		return "", err
	}
	return paths.Worker, nil
}

func loadEmbeddedWorkerManifest(path string) (embeddedWorkerManifest, error) {
	empty := embeddedWorkerManifest{Schema: embeddedWorkerManifestSchema, Files: map[string]string{}}
	if symlink, err := pathContainsSymlink(filesystemRoot(path), path); err != nil {
		return empty, err
	} else if symlink {
		return empty, fmt.Errorf("refusing to read symlinked Worker manifest path %s", path)
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return empty, nil
	}
	if err != nil {
		return empty, err
	}
	var manifest embeddedWorkerManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return empty, fmt.Errorf("decoding Worker manifest: %w", err)
	}
	if manifest.Schema != embeddedWorkerManifestSchema {
		return empty, fmt.Errorf("unsupported Worker manifest schema %d", manifest.Schema)
	}
	if manifest.Files == nil {
		manifest.Files = map[string]string{}
	}
	return manifest, nil
}

func removeObsoleteEmbeddedFile(paths installationPaths, target, priorHash string) (bool, error) {
	root := ""
	for _, candidate := range []string{paths.Worker, paths.SharedAssets} {
		rel, err := filepath.Rel(candidate, target)
		if err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			root = candidate
			break
		}
	}
	if root == "" || priorHash == "" {
		return false, nil
	}
	if symlink, err := pathContainsSymlink(root, target); err != nil {
		return false, err
	} else if symlink {
		return false, nil
	}
	info, err := os.Lstat(target)
	if os.IsNotExist(err) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, nil
	}
	data, err := os.ReadFile(target)
	if err != nil {
		return false, err
	}
	if hashBytes(data) != priorHash {
		return false, nil
	}
	if symlink, err := pathContainsSymlink(root, target); err != nil {
		return false, err
	} else if symlink {
		return false, nil
	}
	if err := os.Remove(target); err != nil {
		return false, err
	}
	return true, nil
}

func mergeEmbeddedWranglerVars(data []byte, vars map[string]string) ([]byte, error) {
	var config map[string]any
	if err := json.Unmarshal(stripJSONC(data), &config); err != nil {
		return nil, err
	}
	existing, _ := config["vars"].(map[string]any)
	if existing == nil {
		existing = map[string]any{}
	}
	for key, value := range vars {
		existing[key] = value
	}
	config["vars"] = existing
	encoded, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(encoded, '\n'), nil
}
