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
	Path    string `json:"path"`
	Status  string `json:"status"`
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Hash    string `json:"sha256"`
}

// executablePath is a variable so tests can point installs at a temp binary.
var executablePath = os.Executable

// bootstrapExecutable copies the running binary into explicitDir (or the
// default install dir for go-run binaries). An existing different target is
// replaced only when managed.json recorded it or it is a Mimir build.
func bootstrapExecutable(explicitDir string, cli ManifestCLI, executable func() (string, error)) (BinaryReport, error) {
	sourcePath, err := executable()
	if err != nil {
		return BinaryReport{}, fmt.Errorf("locating current executable: %w", err)
	}
	if sourcePath, err = filepath.Abs(sourcePath); err != nil {
		return BinaryReport{}, err
	}
	sourceData, err := os.ReadFile(sourcePath)
	if err != nil {
		return BinaryReport{}, fmt.Errorf("reading current executable: %w", err)
	}
	report := BinaryReport{Path: sourcePath, Status: "current", Version: version, Commit: commit, Hash: hashBytes(sourceData)}
	if explicitDir == "" && !temporaryExecutable(sourcePath) {
		return report, nil
	}
	dir, err := resolveInstallDir(explicitDir)
	if err != nil {
		return BinaryReport{}, err
	}
	name := "mimir"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	target := filepath.Join(dir, name)
	report.Path = target
	if sameFilePath(sourcePath, target) {
		return report, nil
	}
	if err := rejectSymlink(target); err != nil {
		return BinaryReport{}, err
	}
	if managedByKnownPackageManager(target) {
		return BinaryReport{}, fmt.Errorf("refusing to overwrite package-manager-owned path %s", target)
	}
	info, statErr := os.Lstat(target)
	switch {
	case os.IsNotExist(statErr):
		report.Status = "installed"
	case statErr != nil:
		return BinaryReport{}, statErr
	case !info.Mode().IsRegular():
		return BinaryReport{}, fmt.Errorf("refusing to overwrite non-regular executable path %s", target)
	default:
		current, err := os.ReadFile(target)
		if err != nil {
			return BinaryReport{}, err
		}
		currentHash := hashBytes(current)
		if currentHash == report.Hash {
			return report, nil
		}
		owned := sameFilePath(cli.Path, target) && cli.SHA256 == currentHash
		if !owned && !isMimirExecutable(target) {
			return BinaryReport{}, fmt.Errorf("refusing to overwrite unowned executable %s", target)
		}
		report.Status = "updated"
	}
	if err := installExecutableCopy(target, sourceData); err != nil {
		return BinaryReport{}, fmt.Errorf("installing CLI binary: %w", err)
	}
	return report, nil
}

var isMimirExecutable = func(path string) bool {
	info, err := buildinfo.ReadFile(path)
	return err == nil && info.Path == "github.com/cloudboy-jh/mimir/cmd/mimir" && info.Main.Path == "github.com/cloudboy-jh/mimir"
}

func managedByKnownPackageManager(path string) bool {
	lower := strings.ToLower(strings.ReplaceAll(path, "\\", "/"))
	for _, marker := range []string{"/homebrew/", "/cellar/", "/linuxbrew/", "/scoop/", "/chocolatey/", "/nix/store/"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
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
		if root, err := filepath.Abs(root); err == nil && pathWithin(root, filepath.Clean(path)) {
			return true
		}
	}
	return false
}

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

func installExecutableCopy(target string, data []byte) error {
	dir := filepath.Dir(target)
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
	if err := staged.Close(); err != nil {
		return err
	}
	if err := os.Chmod(stagedPath, 0o755); err != nil {
		return err
	}
	if err := rejectSymlink(target); err != nil {
		return err
	}
	return os.Rename(stagedPath, target)
}
