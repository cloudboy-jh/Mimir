package install

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

const updateRepo = "cloudboy-jh/mimir"

// githubAPIBase is a variable so tests can point at a stub server.
var githubAPIBase = "https://api.github.com"

// downloadClient allows large release assets over slow connections; the
// shared httpClient stays tuned for quick API calls.
var downloadClient = &http.Client{Timeout: 5 * time.Minute}
var httpClient = &http.Client{Timeout: 30 * time.Second}

// executablePath is a variable so tests can point updates at a temp binary.
var executablePath = os.Executable

type githubRelease struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name string `json:"name"`
		URL  string `json:"browser_download_url"`
	} `json:"assets"`
}

func (r githubRelease) asset(name string) (string, bool) {
	for _, asset := range r.Assets {
		if asset.Name == name {
			return asset.URL, true
		}
	}
	return "", false
}

type UpdateOptions struct {
	Check        bool
	Refresh      func(context.Context, string) (ArtifactReport, error)
	AfterReplace func(context.Context, string) (ArtifactReport, error)
}

func Update(ctx context.Context, options UpdateOptions) (UpdateReport, error) {
	check := options.Check
	refresh := options.Refresh
	if refresh == nil {
		refresh = func(_ context.Context, operation string) (ArtifactReport, error) {
			return refreshManagedInstallation(true, operation)
		}
	}
	artifacts, err := checkManagedArtifacts()
	if err != nil {
		return UpdateReport{}, err
	}
	release, err := fetchLatestRelease(ctx)
	if err != nil {
		return UpdateReport{}, err
	}
	latest := strings.TrimPrefix(release.TagName, "v")
	current := strings.TrimPrefix(version, "v")
	binaryStatus := "available"
	if latest == current || semverCompare(current, latest) > 0 {
		binaryStatus = "current"
		if !check {
			artifacts, err = refresh(ctx, "update")
			if err != nil {
				return UpdateReport{}, fmt.Errorf("mimir is current, but %w", err)
			}
		}
		return newUpdateReport(check, binaryStatus, current, latest, artifacts), nil
	}
	if check {
		return newUpdateReport(true, binaryStatus, current, latest, artifacts), nil
	}
	assetName := releaseAssetName(latest, runtime.GOOS, runtime.GOARCH)
	assetURL, ok := release.asset(assetName)
	if !ok {
		return UpdateReport{}, fmt.Errorf("release %s has no asset %s", release.TagName, assetName)
	}
	checksumsURL, ok := release.asset("checksums.txt")
	if !ok {
		return UpdateReport{}, fmt.Errorf("release %s has no checksums.txt", release.TagName)
	}
	checksums, err := download(ctx, checksumsURL)
	if err != nil {
		return UpdateReport{}, err
	}
	want, ok := parseChecksum(string(checksums), assetName)
	if !ok {
		return UpdateReport{}, fmt.Errorf("checksums.txt has no entry for %s", assetName)
	}
	archive, err := download(ctx, assetURL)
	if err != nil {
		return UpdateReport{}, err
	}
	if got := sha256.Sum256(archive); !strings.EqualFold(hex.EncodeToString(got[:]), want) {
		return UpdateReport{}, fmt.Errorf("checksum mismatch for %s; aborting update", assetName)
	}
	binary, err := extractBinary(archive, runtime.GOOS)
	if err != nil {
		return UpdateReport{}, err
	}
	target, err := executablePath()
	if err != nil {
		return UpdateReport{}, err
	}
	target, err = filepath.Abs(target)
	if err != nil {
		return UpdateReport{}, err
	}
	if symlink, err := pathContainsSymlink(filesystemRoot(target), target); err != nil {
		return UpdateReport{}, err
	} else if symlink {
		return UpdateReport{}, fmt.Errorf("refusing to update symlinked executable path %s", target)
	}
	if managedByPackageManager(target) {
		return UpdateReport{}, fmt.Errorf("mimir at %s is managed by a package manager; update through it instead", target)
	}
	currentBinary, err := os.ReadFile(target)
	if err != nil {
		return UpdateReport{}, fmt.Errorf("reading current Mimir executable: %w", err)
	}
	previousHash := hashBytes(currentBinary)
	receipt, err := loadInstallReceipt()
	if err != nil {
		return UpdateReport{}, err
	}
	if receipt.CLI.Path == "" || !sameFilePath(receipt.CLI.Path, target) || receipt.CLI.Hash == "" || receipt.CLI.Hash != previousHash {
		return UpdateReport{}, fmt.Errorf("mimir update requires the receipt-owned executable; run mimir install first")
	}
	if err := installBinary(target, binary, previousHash); err != nil {
		return UpdateReport{}, fmt.Errorf("installing update: %w", err)
	}
	nextHash := hashBytes(binary)
	if err := recordManagedBinaryUpdate(target, previousHash, nextHash, latest); err != nil {
		if rollbackErr := rollbackUpdatedBinary(target, currentBinary, nextHash); rollbackErr != nil {
			return UpdateReport{}, fmt.Errorf("recording updated binary: %w; rollback failed: %v", err, rollbackErr)
		}
		return UpdateReport{}, fmt.Errorf("recording updated binary: %w", err)
	}
	afterReplace := options.AfterReplace
	if afterReplace == nil {
		afterReplace = func(_ context.Context, _ string) (ArtifactReport, error) {
			return refreshManagedInstallation(true, "update")
		}
	}
	artifacts, err = afterReplace(ctx, target)
	if err != nil {
		return UpdateReport{}, fmt.Errorf("mimir updated, but refreshing managed artifacts failed: %w", err)
	}
	return newUpdateReport(false, "updated", current, latest, artifacts), nil
}

func rollbackUpdatedBinary(target string, previousBinary []byte, updatedHash string) error {
	data, err := os.ReadFile(target)
	if err != nil {
		return err
	}
	if hashBytes(data) != updatedHash {
		return fmt.Errorf("refusing to roll back changed executable %s", target)
	}
	old := target + ".old"
	if oldData, oldErr := os.ReadFile(old); oldErr == nil && hashBytes(oldData) == hashBytes(previousBinary) {
		failed := target + ".rollback"
		_ = os.Remove(failed)
		if err := os.Rename(target, failed); err != nil {
			return err
		}
		if err := os.Rename(old, target); err != nil {
			_ = os.Rename(failed, target)
			return err
		}
		_ = os.Remove(failed)
		return nil
	}
	return installBinary(target, previousBinary, updatedHash)
}

type UpdateReport struct {
	Check     bool                  `json:"check"`
	Binary    UpdateBinaryReport    `json:"binary"`
	Artifacts managedArtifactReport `json:"artifacts"`
}

type UpdateBinaryReport struct {
	Status  string `json:"status"`
	Current string `json:"current_version"`
	Latest  string `json:"latest_version"`
}

func newUpdateReport(check bool, status, current, latest string, artifacts managedArtifactReport) UpdateReport {
	return UpdateReport{Check: check, Binary: UpdateBinaryReport{Status: status, Current: current, Latest: latest}, Artifacts: artifacts}
}

func semverCompare(left, right string) int {
	return semverCompareRelease(left, right)
}

func releaseAssetName(version, goos, goarch string) string {
	return releaseAssetNameForPlatform(version, goos, goarch)
}

func fetchLatestRelease(ctx context.Context) (githubRelease, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", githubAPIBase+"/repos/"+updateRepo+"/releases/latest", nil)
	if err != nil {
		return githubRelease{}, err
	}
	req.Header.Set("accept", "application/vnd.github+json")
	req.Header.Set("user-agent", "mimir-cli")
	data, err := do(httpClient, req)
	if err != nil {
		return githubRelease{}, fmt.Errorf("checking for updates: %w", err)
	}
	var release githubRelease
	if err := json.Unmarshal(data, &release); err != nil || release.TagName == "" {
		return githubRelease{}, fmt.Errorf("checking for updates: invalid GitHub response")
	}
	return release, nil
}

func download(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("user-agent", "mimir-cli")
	return do(downloadClient, req)
}

func do(client *http.Client, req *http.Request) ([]byte, error) {
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return nil, fmt.Errorf("GET %s: %s", req.URL, res.Status)
	}
	return data, nil
}

func parseChecksum(checksums, assetName string) (string, bool) {
	return parseReleaseChecksum(checksums, assetName)
}

func extractBinary(archive []byte, goos string) ([]byte, error) {
	return extractReleaseBinary(archive, goos)
}

// managedByPackageManager detects installs owned by brew, scoop, chocolatey,
// or nix; replacing those binaries directly would corrupt the manager's
// bookkeeping.
func managedByPackageManager(path string) bool {
	return managedByKnownPackageManager(path)
}

// installBinary atomically swaps the running binary. On Windows a running
// executable can be renamed but not replaced, so the current binary is moved
// aside first; the leftover is removed on the next update.
func installBinary(target string, binary []byte, requiredHash ...string) error {
	dir := filepath.Dir(target)
	if symlink, err := pathContainsSymlink(filesystemRoot(target), target); err != nil {
		return err
	} else if symlink {
		return fmt.Errorf("refusing to update symlinked executable path %s", target)
	}
	info, err := os.Lstat(target)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("refusing to update non-regular executable path %s", target)
	}
	current, err := os.ReadFile(target)
	if err != nil {
		return err
	}
	expectedHash := hashBytes(current)
	if len(requiredHash) > 0 {
		if expectedHash != requiredHash[0] {
			return fmt.Errorf("refusing to replace changed executable %s", target)
		}
		expectedHash = requiredHash[0]
	}
	staged, err := os.CreateTemp(dir, ".mimir-update-*")
	if err != nil {
		return err
	}
	stagedPath := staged.Name()
	defer os.Remove(stagedPath)
	if _, err := staged.Write(binary); err != nil {
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
		return fmt.Errorf("refusing to replace symlinked executable path %s", target)
	}
	if err := validateExecutableReplacement(target, expectedHash); err != nil {
		return err
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
