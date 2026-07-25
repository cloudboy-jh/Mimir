package install

import (
	"context"
	"path/filepath"
	"strings"
)

type ArtifactStatus = managedArtifactStatus
type ArtifactResult = managedArtifactResult
type ArtifactReport = managedArtifactReport
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
	return syncManagedArtifacts(false, operation)
}
func RefreshPreviouslyManagedArtifacts(operation string) (ArtifactReport, error) {
	return syncPreviouslyManagedArtifacts(operation)
}
func HasManagedReceipt() (bool, error) { return hasManagedInstallReceipt() }
func Uninstall(keepBinary bool) (UninstallReport, error) {
	return uninstallManagedInstallation(keepBinary)
}
func WorkerDir(explicit string) (string, error)       { return workerDir(explicit) }
func MaterializeWorker(source string) (string, error) { return materializeWorker(source) }
func EnsureWorkerDependencies(ctx context.Context, dir string) error {
	return ensureWorkerDependencies(ctx, dir)
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
