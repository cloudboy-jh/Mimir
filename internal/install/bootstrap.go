package install

import (
	"debug/buildinfo"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

type BinaryReport struct {
	Path      string `json:"path"`
	Status    string `json:"status"`
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildDate string `json:"build_date"`
	Hash      string `json:"sha256"`
	Source    string `json:"source"`
	Method    string `json:"method"`
}

func ResolveExecutable(receipt Receipt, executable func() (string, error)) (string, error) {
	if receipt.CLI.Path != "" && receipt.CLI.Hash != "" {
		path, err := filepath.Abs(receipt.CLI.Path)
		if err != nil {
			return "", err
		}
		if symlink, err := pathContainsSymlink(filesystemRoot(path), path); err != nil {
			return "", err
		} else if symlink {
			return "", fmt.Errorf("receipt-recorded Mimir executable is symlinked: %s", path)
		}
		info, statErr := os.Lstat(path)
		if statErr == nil && info.Mode().IsRegular() {
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return "", readErr
			}
			if hashBytes(data) == receipt.CLI.Hash {
				return path, nil
			}
		}
		return "", fmt.Errorf("receipt-recorded Mimir executable is missing or changed; run mimir install")
	}
	path, err := executable()
	if err != nil {
		return "", err
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if temporaryExecutable(path) {
		return "", fmt.Errorf("refusing to publish a temporary go-run executable; run mimir install first")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		return "", fmt.Errorf("current Mimir executable is unavailable: %s", path)
	}
	return path, nil
}

func bootstrapCurrentExecutable(explicitDir string) (BinaryReport, error) {
	return bootstrapExecutable(explicitDir, executablePath)
}

func bootstrapExecutable(explicitDir string, executable func() (string, error)) (BinaryReport, error) {
	sourcePath, err := executable()
	if err != nil {
		return BinaryReport{}, fmt.Errorf("locating current executable: %w", err)
	}
	sourcePath, err = filepath.Abs(sourcePath)
	if err != nil {
		return BinaryReport{}, err
	}
	sourceData, err := os.ReadFile(sourcePath)
	if err != nil {
		return BinaryReport{}, fmt.Errorf("reading current executable: %w", err)
	}
	temporary, target := executableIsTemporary(sourcePath), sourcePath
	method, status := "existing", "current"
	if explicitDir != "" || temporary {
		dir, err := resolveInstallDir(explicitDir)
		if err != nil {
			return BinaryReport{}, err
		}
		name := "mimir"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		target = filepath.Join(dir, name)
		if symlink, err := pathContainsSymlink(filesystemRoot(target), target); err != nil {
			return BinaryReport{}, err
		} else if symlink {
			return BinaryReport{}, fmt.Errorf("refusing to overwrite symlinked executable path %s", target)
		}
		if managedByKnownPackageManager(target) {
			return BinaryReport{}, fmt.Errorf("refusing to overwrite package-manager-owned path %s", target)
		}
		if resolved, err := filepath.EvalSymlinks(target); err == nil && managedByKnownPackageManager(resolved) {
			return BinaryReport{}, fmt.Errorf("refusing to overwrite package-manager-owned path %s", resolved)
		}
		if !sameFilePath(sourcePath, target) {
			sourceHash := hashBytes(sourceData)
			info, statErr := os.Lstat(target)
			switch {
			case statErr == nil:
				if !info.Mode().IsRegular() {
					return BinaryReport{}, fmt.Errorf("refusing to overwrite non-regular executable path %s", target)
				}
				current, err := os.ReadFile(target)
				if err != nil {
					return BinaryReport{}, err
				}
				currentHash := hashBytes(current)
				if currentHash == sourceHash {
					break
				}
				receipt, err := loadInstallReceipt()
				if err != nil {
					return BinaryReport{}, err
				}
				owned := sameFilePath(receipt.CLI.Path, target) && receipt.CLI.Hash != "" && receipt.CLI.Hash == currentHash
				if !owned && !isMimirExecutable(target) {
					return BinaryReport{}, fmt.Errorf("refusing to overwrite unowned executable %s", target)
				}
				if err := installExecutableCopy(target, sourceData, currentHash); err != nil {
					return BinaryReport{}, fmt.Errorf("installing CLI binary: %w", err)
				}
				method, status = "bootstrap-copy", "updated"
			case os.IsNotExist(statErr):
				if err := installExecutableCopy(target, sourceData, ""); err != nil {
					return BinaryReport{}, fmt.Errorf("installing CLI binary: %w", err)
				}
				method, status = "bootstrap-copy", "installed"
			default:
				return BinaryReport{}, statErr
			}
		}
	}
	source := "executable"
	if temporary {
		source = "go-run"
	}
	// Release bootstraps always pass an explicit destination and mark their
	// verified archive source. Do not depend on temp-path detection here:
	// Windows runners may expose the same temp directory through long and 8.3
	// aliases, causing a downloaded binary to look non-temporary.
	if method == "bootstrap-copy" && strings.EqualFold(strings.TrimSpace(os.Getenv("MIMIR_INSTALL_SOURCE")), "release") {
		source = "release"
	}
	targetHash := hashBytes(sourceData)
	if status == "current" {
		receipt, err := loadInstallReceipt()
		if err != nil {
			return BinaryReport{}, err
		}
		if sameFilePath(receipt.CLI.Path, target) && receipt.CLI.Hash == targetHash {
			if receipt.Source != "" {
				source = receipt.Source
			}
			if receipt.Method != "" {
				method = receipt.Method
			}
		}
	}
	return BinaryReport{Path: target, Status: status, Version: version, Commit: commit, BuildDate: date, Hash: targetHash, Source: source, Method: method}, nil
}

var isMimirExecutable = func(path string) bool {
	info, err := buildinfo.ReadFile(path)
	return err == nil && info.Path == "github.com/cloudboy-jh/mimir/cmd/mimir" && info.Main.Path == "github.com/cloudboy-jh/mimir"
}

func resolveInstallDir(explicit string) (string, error) {
	dir := strings.TrimSpace(explicit)
	if dir == "" {
		for _, key := range []string{"MIMIR_INSTALL_DIR", "GOBIN"} {
			if value := configuredGoEnv(key); value != "" {
				dir = value
				break
			}
		}
	}
	if dir == "" {
		if paths := filepath.SplitList(configuredGoEnv("GOPATH")); len(paths) > 0 && strings.TrimSpace(paths[0]) != "" {
			dir = filepath.Join(paths[0], "bin")
		}
	}
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("resolving install directory: %w", err)
		}
		dir = filepath.Join(home, "go", "bin")
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("resolving install directory: %w", err)
	}
	return dir, nil
}

func temporaryExecutable(path string) bool {
	roots := []string{os.TempDir(), configuredGoEnv("GOTMPDIR"), configuredGoEnv("GOCACHE")}
	if cache, err := os.UserCacheDir(); err == nil {
		roots = append(roots, filepath.Join(cache, "go-build"))
	}
	for _, root := range roots {
		root = strings.TrimSpace(root)
		if root == "" || root == "off" {
			continue
		}
		root, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(root, filepath.Clean(path))
		if err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

var executableIsTemporary = temporaryExecutable

var readGoEnv = func(key string) string {
	output, err := exec.Command("go", "env", key).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(output))
}

func configuredGoEnv(key string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	if key == "MIMIR_INSTALL_DIR" {
		return ""
	}
	return readGoEnv(key)
}

func installExecutableCopy(target string, data []byte, expectedHash string) error {
	dir := filepath.Dir(target)
	if symlink, err := pathContainsSymlink(filesystemRoot(target), target); err != nil {
		return err
	} else if symlink {
		return fmt.Errorf("refusing to install through symlinked path %s", target)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	staged, err := os.CreateTemp(dir, ".mimir-install-*")
	if err != nil {
		return err
	}
	stagedPath := staged.Name()
	defer os.Remove(stagedPath)
	if _, err := staged.Write(data); err != nil {
		_ = staged.Close()
		return err
	}
	if err := staged.Sync(); err != nil {
		_ = staged.Close()
		return err
	}
	if err := staged.Close(); err != nil {
		return err
	}
	if err := os.Chmod(stagedPath, 0o755); err != nil {
		return err
	}
	if symlink, err := pathContainsSymlink(filesystemRoot(target), target); err != nil {
		return err
	} else if symlink {
		return fmt.Errorf("refusing to replace symlinked path %s", target)
	}
	if err := validateExecutableReplacement(target, expectedHash); err != nil {
		return err
	}
	if expectedHash == "" {
		return os.Rename(stagedPath, target)
	}
	if runtime.GOOS == "windows" {
		old := target + ".old"
		_ = os.Remove(old)
		if err := os.Rename(target, old); err != nil {
			return err
		}
		if err := os.Rename(stagedPath, target); err != nil {
			_ = os.Rename(old, target)
			return err
		}
		_ = os.Remove(old)
		return nil
	}
	return os.Rename(stagedPath, target)
}

func validateExecutableReplacement(target, expectedHash string) error {
	info, err := os.Lstat(target)
	if os.IsNotExist(err) {
		if expectedHash == "" {
			return nil
		}
		return fmt.Errorf("refusing to replace executable %s: owned target disappeared", target)
	}
	if err != nil {
		return err
	}
	if expectedHash == "" {
		return fmt.Errorf("refusing to overwrite unowned executable %s", target)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("refusing to overwrite non-regular executable path %s", target)
	}
	current, err := os.ReadFile(target)
	if err != nil {
		return err
	}
	if hashBytes(current) != expectedHash {
		return fmt.Errorf("refusing to replace executable %s: current hash no longer matches install receipt", target)
	}
	return nil
}
