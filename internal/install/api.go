package install

import (
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	mimirassets "github.com/cloudboy-jh/mimir"
)

type ArtifactStatus = managedArtifactStatus
type ArtifactResult = managedArtifactResult
type ArtifactReport = managedArtifactReport

type BundleIdentity struct {
	Version string `json:"version"`
	SHA256  string `json:"sha256"`
}

func EmbeddedWorkerIdentity() (BundleIdentity, error) {
	metadata, err := mimirassets.BundleMetadata()
	if err != nil {
		return BundleIdentity{}, err
	}
	hash := sha256.New()
	for _, file := range metadata {
		if !strings.HasPrefix(file.Path, "worker/") && !strings.HasPrefix(file.Path, "assets/images/") {
			continue
		}
		_, _ = fmt.Fprintf(hash, "%s\x00%s\n", file.Path, file.SHA256)
	}
	return BundleIdentity{Version: version, SHA256: fmt.Sprintf("%x", hash.Sum(nil))}, nil
}

type InstallationPaths = installationPaths
type Receipt = installReceipt
type UninstallBinaryResult = uninstallBinaryResult
type UninstallReport = uninstallReport

const (
	ArtifactInstalled       = artifactInstalled
	ArtifactAdopted         = artifactAdopted
	ArtifactMigrated        = artifactMigrated
	ArtifactUpdated         = artifactUpdated
	ArtifactCurrent         = artifactCurrent
	ArtifactIdentical       = artifactIdentical
	ArtifactOutdated        = artifactOutdated
	ArtifactMissing         = artifactMissing
	ArtifactModified        = artifactModified
	ArtifactConflict        = artifactConflict
	ArtifactSymlinkRejected = artifactSymlinkRejected
	ArtifactRemoved         = artifactRemoved
	ArtifactUnowned         = artifactUnowned
)

func Paths() (InstallationPaths, error)       { return managedInstallationPaths() }
func LoadReceipt() (Receipt, error)           { return loadInstallReceipt() }
func CheckArtifacts() (ArtifactReport, error) { return checkManagedArtifacts() }
func RefreshArtifacts(operation string) (ArtifactReport, error) {
	return syncManagedArtifacts(true, operation)
}
func RefreshSelectedArtifacts(operation string, harnesses []string) (ArtifactReport, error) {
	return reconcileManagedArtifacts(true, operation, true, true, false, nil, &harnesses)
}
func SetHarnessEnabled(id string, enabled bool) (ArtifactReport, error) {
	ids, err := NormalizeHarnesses([]string{id})
	if err != nil {
		return ArtifactReport{}, err
	}
	if len(ids) != 1 {
		return ArtifactReport{}, fmt.Errorf("enable or disable requires one harness id")
	}
	return setHarnessEnabled(ids[0], enabled)
}
func SetHarnessSelection(harnesses []string) error {
	normalized, err := NormalizeHarnesses(harnesses)
	if err != nil {
		return err
	}
	return setHarnessSelection(normalized)
}
func RefreshPreviouslyManagedArtifacts(operation string) (ArtifactReport, error) {
	return syncPreviouslyManagedArtifacts(operation)
}
func HasManagedReceipt() (bool, error) { return hasManagedInstallReceipt() }

// FinalizePendingUpdate completes a deferred binary swap recorded by a
// previous update once the executable is no longer locked, then reconciles
// the install receipt and removes stale swap artifacts. Best effort.
func FinalizePendingUpdate() (bool, error) {
	applied, _, err := finalizePendingUpdate()
	return applied, err
}

// PendingUpdateExists reports whether a deferred update marker is present.
func PendingUpdateExists() (bool, error) {
	paths, err := managedInstallationPaths()
	if err != nil {
		return false, err
	}
	_, found, err := loadPendingUpdate(paths)
	return found, err
}

// RemoveCurrentExecutableAfterExit schedules deletion of a detached update
// helper after that helper exits. It is used only by the hidden Windows update
// helper command.
func currentUpdateHelperPath() (string, error) {
	path, err := executablePath()
	if err != nil {
		return "", err
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return "", err
	}
	temp, err := filepath.Abs(os.TempDir())
	if err != nil {
		return "", err
	}
	if !sameFilePath(filepath.Dir(path), temp) || !strings.HasPrefix(filepath.Base(path), "mimir-update-helper-") {
		return "", fmt.Errorf("refusing to run update helper from non-helper executable %s", path)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("refusing to use non-regular update helper %s", path)
	}
	return path, nil
}

// ValidateCurrentUpdateHelper rejects direct/manual invocation of the hidden
// helper command from the installed binary.
func ValidateCurrentUpdateHelper() error {
	_, err := currentUpdateHelperPath()
	return err
}

func RemoveCurrentExecutableAfterExit() error {
	path, err := currentUpdateHelperPath()
	if err != nil {
		return err
	}
	return launchDeferredBinaryRemoval(os.Getpid(), path)
}

// CleanupStaleUpdateArtifacts removes receipt-owned swap leftovers once the
// processes that mapped them have exited. It is cheap when no leftovers exist.
func CleanupStaleUpdateArtifacts() {
	receipt, err := loadInstallReceipt()
	if err != nil || receipt.CLI.Path == "" {
		return
	}
	cleanupStaleSwapArtifacts(receipt.CLI.Path)
}
func Uninstall(keepBinary bool) (UninstallReport, error) {
	return uninstallManagedInstallation(keepBinary)
}
func WorkerDir(explicit string) (string, error)       { return workerDir(explicit) }
func MaterializeWorker(source string) (string, error) { return materializeWorker(source) }
func EnsureWorkerDependencies(ctx context.Context, dir string) error {
	return ensureWorkerDependencies(ctx, dir)
}
func EnsureDashboardDependencies(ctx context.Context, dir string) error {
	return ensureDashboardDependencies(ctx, dir)
}
func BuildDashboard(ctx context.Context, dir string) error { return buildDashboard(ctx, dir) }
func ArtifactCounts(report ArtifactReport) map[ArtifactStatus]int {
	return managedArtifactCounts(report)
}
func ArtifactIssueCount(report ArtifactReport) int { return artifactIssueCount(report) }
func ArtifactSummary(report ArtifactReport) string { return artifactSummary(report) }

// ArtifactsReady reports whether at least one matching artifact exists and all
// matching artifacts are usable by their integration provider.
func ArtifactsReady(report ArtifactReport, root string, sourcePrefixes ...string) bool {
	found := false
	for _, artifact := range report.Artifacts {
		if root != "" {
			rel, err := filepath.Rel(root, artifact.Path)
			if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
				continue
			}
		}
		matched := false
		for _, prefix := range sourcePrefixes {
			if strings.HasPrefix(filepath.ToSlash(artifact.Source), prefix) {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		found = true
		switch artifact.Status {
		case ArtifactCurrent, ArtifactInstalled, ArtifactAdopted, ArtifactMigrated, ArtifactUpdated:
		default:
			return false
		}
	}
	return found
}
