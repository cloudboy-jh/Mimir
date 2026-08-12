package install

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const updateRepo = "cloudboy-jh/mimir"

// errExecutableLocked marks rename failures caused by another Mimir process
// (or an antivirus filter) holding the running executable image on Windows.
var errExecutableLocked = errors.New("executable is locked by another Mimir process")

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
	Check bool
	// Force stops sibling Mimir processes (Windows) so the binary swap can
	// proceed immediately instead of being deferred.
	Force        bool
	Progress     func(string)
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
	options.progress("Checking managed artifacts")
	artifacts, err := checkManagedArtifacts()
	if err != nil {
		return UpdateReport{}, err
	}
	// Complete any deferred swap left by an earlier locked update before
	// deciding what this run needs to do.
	appliedVersion, blockedPendingVersion := "", ""
	if !check {
		if err := ctx.Err(); err != nil {
			return UpdateReport{}, err
		}
		options.progress("Finalizing pending update")
		applied, pendingVersion, err := finalizePendingUpdate()
		if err != nil {
			return UpdateReport{}, fmt.Errorf("finalizing pending update: %w", err)
		}
		if applied {
			appliedVersion = pendingVersion
			bounded, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
			defer cancel()
			ctx = bounded
		} else {
			blockedPendingVersion = pendingVersion
		}
	}
	options.progress("Checking latest release")
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
			options.progress("Refreshing managed integrations")
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
	if blockedPendingVersion != "" && blockedPendingVersion == latest {
		report := newUpdateReport(false, "scheduled", current, latest, artifacts)
		report.Binary.Detail = "verified update is staged and waiting for the executable lock to clear"
		return report, nil
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
	afterReplace := options.AfterReplace
	if afterReplace == nil {
		afterReplace = func(_ context.Context, _ string) (ArtifactReport, error) {
			return refreshManagedInstallation(true, "update")
		}
	}
	// A deferred swap finalized above may already have placed this exact
	// release on disk; the download and swap can be skipped entirely.
	if appliedVersion != "" && appliedVersion == latest {
		options.progress("Refreshing managed integrations")
		postCommit, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
		artifacts, err = afterReplace(postCommit, target)
		cancel()
		if err != nil {
			return UpdateReport{}, fmt.Errorf("mimir updated, but refreshing managed artifacts failed: %w", err)
		}
		report := newUpdateReport(false, "updated", current, latest, artifacts)
		report.Binary.Detail = "applied deferred update"
		return report, nil
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
	options.progress("Downloading checksums")
	checksums, err := download(ctx, checksumsURL)
	if err != nil {
		return UpdateReport{}, err
	}
	want, ok := parseChecksum(string(checksums), assetName)
	if !ok {
		return UpdateReport{}, fmt.Errorf("checksums.txt has no entry for %s", assetName)
	}
	options.progress("Downloading Mimir " + latest)
	archive, err := download(ctx, assetURL)
	if err != nil {
		return UpdateReport{}, err
	}
	options.progress("Verifying release checksum")
	if got := sha256.Sum256(archive); !strings.EqualFold(hex.EncodeToString(got[:]), want) {
		return UpdateReport{}, fmt.Errorf("checksum mismatch for %s; aborting update", assetName)
	}
	options.progress("Extracting release binary")
	binary, err := extractBinary(archive, runtime.GOOS)
	if err != nil {
		return UpdateReport{}, err
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
	if err := ctx.Err(); err != nil {
		return UpdateReport{}, err
	}
	cleanupStaleSwapArtifacts(target)
	stopped := []int{}
	if options.Force {
		options.progress("Stopping running Mimir processes")
		stopped = stopSiblingMimirProcesses(os.Getpid(), target)
	}
	options.progress("Replacing Mimir executable")
	if err := installBinary(target, binary, previousHash); err != nil {
		if errors.Is(err, errExecutableLocked) {
			options.progress("Scheduling deferred update")
			detail, scheduleErr := scheduleDeferredSwap(target, binary, previousHash, latest)
			if scheduleErr != nil {
				return UpdateReport{}, fmt.Errorf("installing update: %w; scheduling a deferred update also failed: %v", err, scheduleErr)
			}
			report := newUpdateReport(false, "scheduled", current, latest, artifacts)
			report.Binary.Detail = detail
			return report, nil
		}
		return UpdateReport{}, fmt.Errorf("installing update: %w", err)
	}
	nextHash := hashBytes(binary)
	options.progress("Recording managed binary")
	if err := recordManagedBinaryUpdate(target, previousHash, nextHash, latest); err != nil {
		if rollbackErr := rollbackUpdatedBinary(target, currentBinary, nextHash); rollbackErr != nil {
			return UpdateReport{}, fmt.Errorf("recording updated binary: %w; rollback failed: %v", err, rollbackErr)
		}
		return UpdateReport{}, fmt.Errorf("recording updated binary: %w", err)
	}
	options.progress("Refreshing managed integrations")
	postCommit, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Minute)
	artifacts, err = afterReplace(postCommit, target)
	cancel()
	if err != nil {
		return UpdateReport{}, fmt.Errorf("mimir updated, but refreshing managed artifacts failed: %w", err)
	}
	report := newUpdateReport(false, "updated", current, latest, artifacts)
	if len(stopped) > 0 {
		report.Binary.Detail = "stopped Mimir process(es) " + joinInts(stopped)
	}
	return report, nil
}

func (o UpdateOptions) progress(message string) {
	if o.Progress != nil {
		o.Progress(message)
	}
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
	Detail  string `json:"detail,omitempty"`
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
	base := strings.TrimRight(strings.TrimSpace(os.Getenv("MIMIR_GITHUB_API_URL")), "/")
	if base == "" {
		base = githubAPIBase
	}
	req, err := http.NewRequestWithContext(ctx, "GET", base+"/repos/"+updateRepo+"/releases/latest", nil)
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
// executable can usually be renamed but not replaced, and sibling Mimir
// processes or antivirus filters can deny even the rename, so the platform
// swap retries transient lock failures and reports a persistent lock as
// errExecutableLocked; the caller then defers the swap.
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
	return platformSwap(target, stagedPath, expectedHash, hashBytes(binary))
}

// pendingUpdate records a verified binary that could not be swapped into
// place because the running executable was locked. The detached helper and
// later CLI runs retry the swap; whichever succeeds first repairs the receipt
// through recordManagedBinaryUpdate.
type pendingUpdate struct {
	Schema       int    `json:"schema"`
	Target       string `json:"target"`
	Staged       string `json:"staged"`
	PreviousHash string `json:"previous_hash"`
	NewHash      string `json:"new_hash"`
	Version      string `json:"version"`
	CreatedAt    string `json:"created_at"`
}

func pendingUpdatePath(paths installationPaths) string {
	return filepath.Join(paths.MimirHome, "pending-update.json")
}

func loadPendingUpdate(paths installationPaths) (pendingUpdate, bool, error) {
	path := pendingUpdatePath(paths)
	if symlink, err := pathContainsSymlink(filesystemRoot(path), path); err != nil {
		return pendingUpdate{}, false, err
	} else if symlink {
		return pendingUpdate{}, false, fmt.Errorf("refusing to read symlinked pending update marker")
	}
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return pendingUpdate{}, false, nil
	}
	if err != nil {
		return pendingUpdate{}, false, err
	}
	var pending pendingUpdate
	if err := json.Unmarshal(data, &pending); err != nil {
		return pendingUpdate{}, false, fmt.Errorf("decoding pending update: %w", err)
	}
	if pending.Schema != 1 || pending.Target == "" || pending.Staged == "" || pending.PreviousHash == "" || pending.NewHash == "" || pending.Version == "" {
		return pendingUpdate{}, false, fmt.Errorf("invalid pending update marker")
	}
	return pending, true, nil
}

func savePendingUpdate(paths installationPaths, pending pendingUpdate) error {
	if err := os.MkdirAll(paths.MimirHome, 0o755); err != nil {
		return err
	}
	path := pendingUpdatePath(paths)
	if symlink, err := pathContainsSymlink(filesystemRoot(path), path); err != nil {
		return err
	} else if symlink {
		return fmt.Errorf("refusing to write symlinked pending update marker")
	}
	return writeJSONAtomic(path, pending)
}

func removePendingUpdate(paths installationPaths) error {
	if err := os.Remove(pendingUpdatePath(paths)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func validatePendingUpdateOwnership(pending pendingUpdate) (installReceipt, error) {
	receipt, err := loadInstallReceipt()
	if err != nil {
		return installReceipt{}, err
	}
	if receipt.CLI.Path == "" || !sameFilePath(receipt.CLI.Path, pending.Target) {
		return installReceipt{}, fmt.Errorf("pending update target is not the receipt-owned executable")
	}
	if receipt.CLI.Hash != pending.PreviousHash && receipt.CLI.Hash != pending.NewHash {
		return installReceipt{}, fmt.Errorf("pending update hashes do not match the install receipt")
	}
	if !filepath.IsAbs(pending.Target) || !filepath.IsAbs(pending.Staged) || !sameFilePath(filepath.Dir(pending.Target), filepath.Dir(pending.Staged)) || !strings.HasPrefix(filepath.Base(pending.Staged), ".mimir-update-pending-") {
		return installReceipt{}, fmt.Errorf("pending update staged path is invalid")
	}
	if symlink, err := pathContainsSymlink(filesystemRoot(pending.Target), pending.Target); err != nil {
		return installReceipt{}, err
	} else if symlink {
		return installReceipt{}, fmt.Errorf("pending update target contains a symlink")
	}
	return receipt, nil
}

func readVerifiedPendingFile(path, expectedHash, label string) ([]byte, error) {
	if symlink, err := pathContainsSymlink(filesystemRoot(path), path); err != nil {
		return nil, err
	} else if symlink {
		return nil, fmt.Errorf("pending update %s contains a symlink", label)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("pending update %s is not a regular file", label)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if hashBytes(data) != expectedHash {
		return nil, fmt.Errorf("pending update %s hash changed", label)
	}
	return data, nil
}

// finalizePendingUpdate applies a deferred swap once the executable is free.
// It validates the receipt, target, staged path, and both hashes immediately
// before replacement, then reports whether the on-disk binary now holds the
// pending version.
func finalizePendingUpdate() (bool, string, error) {
	paths, err := managedInstallationPaths()
	if err != nil {
		return false, "", err
	}
	pending, found, err := loadPendingUpdate(paths)
	if err != nil || !found {
		return false, "", err
	}
	receipt, err := validatePendingUpdateOwnership(pending)
	if err != nil {
		return false, "", err
	}
	current, err := readVerifiedPendingFile(pending.Target, pending.PreviousHash, "target")
	if err != nil && os.IsNotExist(err) {
		if recoverErr := platformRecoverTarget(pending.Target, pending.PreviousHash); recoverErr != nil {
			return false, "", fmt.Errorf("recovering interrupted update: %w", recoverErr)
		}
		current, err = readVerifiedPendingFile(pending.Target, pending.PreviousHash, "target")
	}
	if err != nil {
		// The helper may already have placed the new binary before it could
		// refresh the receipt, so accept that exact verified state as well.
		current, err = readVerifiedPendingFile(pending.Target, pending.NewHash, "target")
		if err != nil {
			return false, "", err
		}
	}
	diskHash := hashBytes(current)
	if receipt.CLI.Hash == pending.NewHash && diskHash != pending.NewHash {
		return false, "", fmt.Errorf("pending update receipt is ahead of the executable")
	}
	if diskHash == pending.PreviousHash {
		if _, err := readVerifiedPendingFile(pending.Staged, pending.NewHash, "staged binary"); err != nil {
			return false, "", err
		}
		if err := platformSwap(pending.Target, pending.Staged, pending.PreviousHash, pending.NewHash); err != nil {
			if errors.Is(err, errExecutableLocked) {
				return false, pending.Version, nil
			}
			return false, "", err
		}
	}
	if receipt.CLI.Hash != pending.NewHash {
		if err := recordManagedBinaryUpdate(pending.Target, pending.PreviousHash, pending.NewHash, pending.Version); err != nil {
			return false, "", err
		}
	}
	if err := removePendingUpdate(paths); err != nil {
		return false, "", err
	}
	cleanupStaleSwapArtifacts(pending.Target)
	return true, pending.Version, nil
}

// cleanupStaleSwapArtifacts best-effort removes swap leftovers next to the
// owned executable: the renamed-aside .old binary (deletable only after the
// processes that mapped it have exited), rollback files, orphaned staged
// temps, and a staged .new with no pending marker. Foreign junk such as
// .bak or linker ~ files is reported by doctor instead of deleted.
func cleanupStaleSwapArtifacts(target string) {
	entries, readDirErr := os.ReadDir(filepath.Dir(target))
	hasCandidate := pathExists(target+".old") || pathExists(target+".rollback")
	if !hasCandidate && readDirErr == nil {
		cutoff := time.Now().Add(-time.Hour)
		for _, entry := range entries {
			name := entry.Name()
			if !strings.HasPrefix(name, ".mimir-update-") && !strings.HasPrefix(name, ".mimir-install-") {
				continue
			}
			info, err := entry.Info()
			if err == nil && !info.ModTime().After(cutoff) {
				hasCandidate = true
				break
			}
		}
	}
	if !hasCandidate {
		return
	}
	receipt, err := loadInstallReceipt()
	if err != nil || receipt.CLI.Path == "" || receipt.CLI.Hash == "" || !sameFilePath(receipt.CLI.Path, target) {
		return
	}
	if symlink, err := pathContainsSymlink(filesystemRoot(target), target); err != nil || symlink {
		return
	}
	data, err := os.ReadFile(target)
	if err != nil || hashBytes(data) != receipt.CLI.Hash {
		return
	}
	_ = os.Remove(target + ".old")
	_ = os.Remove(target + ".rollback")
	pendingStaged := ""
	paths, err := managedInstallationPaths()
	if err == nil {
		pending, found, pendingErr := loadPendingUpdate(paths)
		if pendingErr != nil {
			return
		}
		if found && sameFilePath(pending.Target, target) {
			pendingStaged = pending.Staged
		}
	}
	if readDirErr != nil {
		return
	}
	cutoff := time.Now().Add(-time.Hour)
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, ".mimir-update-") && !strings.HasPrefix(name, ".mimir-install-") {
			continue
		}
		info, err := entry.Info()
		if err != nil || info.ModTime().After(cutoff) {
			continue
		}
		path := filepath.Join(filepath.Dir(target), name)
		if pendingStaged != "" && sameFilePath(path, pendingStaged) {
			continue
		}
		_ = os.Remove(path)
	}
}

func joinInts(values []int) string {
	parts := make([]string, len(values))
	for i, value := range values {
		parts[i] = strconv.Itoa(value)
	}
	return strings.Join(parts, ", ")
}
