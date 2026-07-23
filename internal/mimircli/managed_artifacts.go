package mimircli

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"

	mimirassets "github.com/cloudboy-jh/mimir"
)

const installReceiptSchema = 2

type managedArtifactStatus string

const (
	artifactInstalled       managedArtifactStatus = "installed"
	artifactAdopted         managedArtifactStatus = "adopted"
	artifactUpdated         managedArtifactStatus = "updated"
	artifactCurrent         managedArtifactStatus = "current"
	artifactIdentical       managedArtifactStatus = "identical"
	artifactOutdated        managedArtifactStatus = "outdated"
	artifactMissing         managedArtifactStatus = "missing"
	artifactModified        managedArtifactStatus = "modified"
	artifactConflict        managedArtifactStatus = "conflict"
	artifactSymlinkRejected managedArtifactStatus = "symlink-rejected"
	artifactRemoved         managedArtifactStatus = "removed"
	artifactUnowned         managedArtifactStatus = "unowned-preserved"
	artifactMissingKept     managedArtifactStatus = "missing-preserved"
	artifactModifiedKept    managedArtifactStatus = "modified-preserved"
	artifactNonRegularKept  managedArtifactStatus = "non-regular-preserved"
	artifactSymlinkKept     managedArtifactStatus = "symlink-preserved"
	artifactOwnershipKept   managedArtifactStatus = "invalid-ownership-preserved"
	artifactRemoveFailed    managedArtifactStatus = "remove-failed"
)

type installationPaths struct {
	MimirHome      string `json:"mimir_home"`
	Receipt        string `json:"receipt"`
	Log            string `json:"log"`
	OpenCodeHome   string `json:"opencode_home"`
	HermesHome     string `json:"hermes_home,omitempty"`
	HermesDetected bool   `json:"hermes_detected"`
	Worker         string `json:"worker"`
	SharedAssets   string `json:"shared_assets"`
}

type installReceiptArtifact struct {
	Source string `json:"source"`
	Hash   string `json:"sha256"`
}

type installReceipt struct {
	Schema         int                               `json:"schema"`
	InstallationID string                            `json:"installation_id,omitempty"`
	InstalledAt    string                            `json:"installed_at,omitempty"`
	UpdatedAt      string                            `json:"updated_at,omitempty"`
	Source         string                            `json:"source,omitempty"`
	Method         string                            `json:"method,omitempty"`
	CLI            installReceiptCLI                 `json:"cli"`
	BundleVersion  string                            `json:"bundle_version"`
	Artifacts      map[string]installReceiptArtifact `json:"artifacts"`
	migrated       bool
}

type installReceiptCLI struct {
	Path      string `json:"path,omitempty"`
	Version   string `json:"version,omitempty"`
	Commit    string `json:"commit,omitempty"`
	BuildDate string `json:"build_date,omitempty"`
	Hash      string `json:"sha256,omitempty"`
}

type managedArtifactResult struct {
	Path        string                `json:"path"`
	Source      string                `json:"source"`
	Status      managedArtifactStatus `json:"status"`
	Hash        string                `json:"sha256,omitempty"`
	ReceiptHash string                `json:"receipt_sha256,omitempty"`
	BundleHash  string                `json:"bundle_sha256"`
	Detail      string                `json:"detail,omitempty"`
}

type managedArtifactReport struct {
	Operation     string                  `json:"operation"`
	BeforeVersion string                  `json:"before_version"`
	AfterVersion  string                  `json:"after_version"`
	Result        string                  `json:"result,omitempty"`
	Summary       string                  `json:"summary,omitempty"`
	BundleVersion string                  `json:"bundle_version"`
	ReceiptPath   string                  `json:"receipt_path"`
	LogPath       string                  `json:"log_path"`
	Artifacts     []managedArtifactResult `json:"artifacts"`
}

type managedArtifactSpec struct {
	source     string
	target     string
	root       string
	cleanupDir string
	data       []byte
	hash       string
}

type uninstallBinaryResult struct {
	Path                     string `json:"path,omitempty"`
	PendingPath              string `json:"pending_path,omitempty"`
	Status                   string `json:"status"`
	Hash                     string `json:"sha256,omitempty"`
	DeferredUntilProcessExit bool   `json:"deferred_until_process_exit,omitempty"`
	Detail                   string `json:"detail,omitempty"`
}

type uninstallReport struct {
	Operation   string                  `json:"operation"`
	Result      string                  `json:"result"`
	Summary     string                  `json:"summary"`
	ReceiptPath string                  `json:"receipt_path"`
	LogPath     string                  `json:"log_path"`
	Binary      uninstallBinaryResult   `json:"binary"`
	Hermes      harnessIntegrationState `json:"hermes"`
	Artifacts   []managedArtifactResult `json:"artifacts"`
}

func managedInstallationPaths() (installationPaths, error) {
	pointer, err := pointerPath()
	if err != nil {
		return installationPaths{}, err
	}
	userHome, err := os.UserHomeDir()
	if err != nil {
		return installationPaths{}, err
	}
	mimirHome := filepath.Dir(pointer)
	opencodeHome := filepath.Join(userHome, ".config", "opencode")
	hermesHome, found, err := discoverHermesHome()
	if err != nil {
		return installationPaths{}, err
	}
	return installationPaths{
		MimirHome:      mimirHome,
		Receipt:        filepath.Join(mimirHome, "install-receipt.json"),
		Log:            filepath.Join(mimirHome, "install-log.jsonl"),
		OpenCodeHome:   opencodeHome,
		HermesHome:     hermesHome,
		HermesDetected: found,
		Worker:         filepath.Join(mimirHome, "worker"),
		SharedAssets:   filepath.Join(mimirHome, "assets", "images"),
	}, nil
}

func loadInstallReceipt() (installReceipt, error) {
	pointer, err := pointerPath()
	if err != nil {
		return installReceipt{}, err
	}
	receiptPath := filepath.Join(filepath.Dir(pointer), "install-receipt.json")
	if symlink, err := pathContainsSymlink(filesystemRoot(receiptPath), receiptPath); err != nil {
		return installReceipt{}, err
	} else if symlink {
		return installReceipt{}, errors.New("refusing to read symlinked install receipt path")
	}
	data, err := os.ReadFile(receiptPath)
	if os.IsNotExist(err) {
		return newInstallReceipt(), nil
	}
	if err != nil {
		return installReceipt{}, err
	}
	var receipt installReceipt
	if err := json.Unmarshal(data, &receipt); err != nil {
		return installReceipt{}, fmt.Errorf("decoding install receipt: %w", err)
	}
	if receipt.Schema != 1 && receipt.Schema != installReceiptSchema {
		return installReceipt{}, fmt.Errorf("unsupported install receipt schema %d", receipt.Schema)
	}
	if receipt.Schema == 1 {
		receipt.Schema = installReceiptSchema
		receipt.migrated = true
	}
	if receipt.Artifacts == nil {
		receipt.Artifacts = map[string]installReceiptArtifact{}
	}
	return receipt, nil
}

func newInstallReceipt() installReceipt {
	return installReceipt{Schema: installReceiptSchema, Artifacts: map[string]installReceiptArtifact{}}
}

type installReceiptUpdate struct {
	Source string
	Method string
	CLI    installReceiptCLI
}

// syncManagedArtifacts reconciles bundled harness files. Enrollment may create
// absent targets; refresh only touches files already owned by the receipt or
// files whose bytes are identical to the bundle.
func syncManagedArtifacts(enroll bool, operation string) (managedArtifactReport, error) {
	return reconcileManagedArtifacts(enroll, operation, true, true, false, nil)
}

func syncPreviouslyManagedArtifacts(operation string) (managedArtifactReport, error) {
	return reconcileManagedArtifacts(false, operation, true, false, true, nil)
}

func syncInstallArtifacts(update installReceiptUpdate) (managedArtifactReport, error) {
	return reconcileManagedArtifacts(true, "install", true, true, false, &update)
}

func refreshManagedInstallation(enroll bool, operation string) (managedArtifactReport, error) {
	cli, err := currentExecutableReceiptCLI()
	if err != nil {
		return managedArtifactReport{}, err
	}
	return reconcileManagedArtifacts(enroll, operation, true, true, false, &installReceiptUpdate{CLI: cli})
}

func checkManagedArtifacts() (managedArtifactReport, error) {
	return reconcileManagedArtifacts(false, "check", false, true, false, nil)
}

func uninstallManagedInstallation(keepBinary bool) (uninstallReport, error) {
	paths, err := managedInstallationPaths()
	if err != nil {
		return uninstallReport{}, err
	}
	receipt, err := loadInstallReceipt()
	if err != nil {
		return uninstallReport{}, err
	}
	specs, err := bundledManagedArtifacts(paths)
	if err != nil {
		return uninstallReport{}, err
	}
	byTarget := make(map[string]managedArtifactSpec, len(specs))
	for _, spec := range specs {
		byTarget[spec.target] = spec
	}
	for target, owned := range receipt.Artifacts {
		if _, exists := byTarget[target]; exists {
			continue
		}
		if spec, ok := receiptManagedArtifactSpec(paths, target, owned); ok {
			byTarget[target] = spec
		} else {
			byTarget[target] = managedArtifactSpec{source: owned.Source, target: target}
		}
	}
	targets := make([]string, 0, len(byTarget))
	for target := range byTarget {
		targets = append(targets, target)
	}
	sort.Strings(targets)

	report := uninstallReport{Operation: "uninstall", ReceiptPath: paths.Receipt, LogPath: paths.Log}
	for _, target := range targets {
		spec := byTarget[target]
		owned, isOwned := receipt.Artifacts[target]
		result, removed := uninstallManagedArtifact(spec, owned, isOwned)
		report.Artifacts = append(report.Artifacts, result)
		if removed {
			delete(receipt.Artifacts, target)
			cleanupManagedArtifactDirs(spec)
		} else if result.Status == artifactMissingKept {
			cleanupManagedArtifactDirs(spec)
		}
	}
	report.Hermes = uninstallHermesIntegration()
	report.Binary = uninstallManagedBinary(receipt.CLI, receipt.Method, keepBinary)
	if report.Binary.Status == "removed" {
		receipt.CLI = installReceiptCLI{}
	} else if report.Binary.Status == "pending-removal" || report.Binary.Status == "pending-removal-unscheduled" {
		receipt.CLI.Path = report.Binary.PendingPath
	}
	receipt.Schema = installReceiptSchema
	receipt.UpdatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	if err := writeJSONAtomic(paths.Receipt, receipt); err != nil {
		return report, err
	}
	report.Result, report.Summary = uninstallResultSummary(report, keepBinary)
	if err := appendUninstallLog(paths.Log, report); err != nil {
		return report, err
	}
	return report, nil
}

func receiptManagedArtifactSpec(paths installationPaths, target string, owned installReceiptArtifact) (managedArtifactSpec, bool) {
	var expected, root, managedDir string
	source := filepath.ToSlash(filepath.Clean(owned.Source))
	switch {
	case strings.HasPrefix(source, "plugins/opencode/") && filepath.Base(source) == strings.TrimPrefix(source, "plugins/opencode/"):
		root = paths.OpenCodeHome
		expected = filepath.Join(root, "plugins", filepath.Base(source))
	case strings.HasPrefix(source, "plugins/hermes/") && filepath.Base(source) == strings.TrimPrefix(source, "plugins/hermes/"):
		root = paths.HermesHome
		managedDir = filepath.Join(root, "plugins", "mimir")
		expected = filepath.Join(managedDir, filepath.Base(source))
	case strings.HasPrefix(source, "skills/mimir-"):
		rel := strings.TrimPrefix(source, "skills/")
		parts := strings.Split(rel, "/")
		if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
			return managedArtifactSpec{}, false
		}
		for _, candidate := range []string{paths.OpenCodeHome, paths.HermesHome} {
			candidateTarget := filepath.Join(candidate, "skills", filepath.FromSlash(rel))
			if sameFilePath(candidateTarget, target) {
				root = candidate
				expected = candidateTarget
				managedDir = filepath.Join(root, "skills", parts[0])
				break
			}
		}
	default:
		return managedArtifactSpec{}, false
	}
	if root == "" || !sameFilePath(expected, target) {
		return managedArtifactSpec{}, false
	}
	spec := managedArtifactSpec{source: owned.Source, target: expected, root: root, hash: owned.Hash}
	if managedDir != "" {
		spec.cleanupDir = managedDir
	}
	return spec, true
}

func uninstallManagedArtifact(spec managedArtifactSpec, owned installReceiptArtifact, isOwned bool) (managedArtifactResult, bool) {
	result := managedArtifactResult{Path: spec.target, Source: spec.source, BundleHash: spec.hash}
	if !isOwned {
		result.Status = artifactUnowned
		result.Detail = "not owned by the install receipt"
		return result, false
	}
	result.ReceiptHash = owned.Hash
	if spec.root == "" {
		result.Status = artifactOwnershipKept
		result.Detail = "receipt entry is not a recognized Mimir plugin or skill path"
		return result, false
	}
	if symlink, err := pathContainsSymlink(spec.root, spec.target); err != nil {
		result.Status = artifactRemoveFailed
		result.Detail = err.Error()
		return result, false
	} else if symlink {
		result.Status = artifactSymlinkKept
		result.Detail = "receipt-owned path contains a symlink"
		return result, false
	}
	info, err := os.Lstat(spec.target)
	if os.IsNotExist(err) {
		result.Status = artifactMissingKept
		result.Detail = "receipt-owned file is missing"
		return result, false
	}
	if err != nil {
		result.Status = artifactRemoveFailed
		result.Detail = err.Error()
		return result, false
	}
	if !info.Mode().IsRegular() {
		result.Status = artifactNonRegularKept
		result.Detail = "receipt-owned path is not a regular file"
		return result, false
	}
	data, err := os.ReadFile(spec.target)
	if err != nil {
		result.Status = artifactRemoveFailed
		result.Detail = err.Error()
		return result, false
	}
	result.Hash = hashBytes(data)
	if owned.Hash == "" || result.Hash != owned.Hash {
		result.Status = artifactModifiedKept
		result.Detail = "current hash differs from the install receipt"
		return result, false
	}
	if err := os.Remove(spec.target); err != nil {
		result.Status = artifactRemoveFailed
		result.Detail = err.Error()
		return result, false
	}
	result.Status = artifactRemoved
	return result, true
}

func cleanupManagedArtifactDirs(spec managedArtifactSpec) {
	if spec.cleanupDir == "" {
		return
	}
	for dir := filepath.Dir(spec.target); ; dir = filepath.Dir(dir) {
		info, err := os.Lstat(dir)
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() || os.Remove(dir) != nil {
			return
		}
		if sameFilePath(dir, spec.cleanupDir) {
			return
		}
		if parent := filepath.Dir(dir); parent == dir {
			return
		}
	}
}

func uninstallManagedBinary(cli installReceiptCLI, method string, keep bool) uninstallBinaryResult {
	return uninstallManagedBinaryForPlatform(cli, method, keep, runtime.GOOS == "windows", launchDeferredBinaryRemoval)
}

func uninstallManagedBinaryForPlatform(cli installReceiptCLI, method string, keep, windows bool, launch func(int, string) error) uninstallBinaryResult {
	result := uninstallBinaryResult{Path: cli.Path}
	if keep {
		result.Status = "kept"
		result.Detail = "--keep-binary was specified"
		return result
	}
	if strings.TrimSpace(cli.Path) == "" || strings.TrimSpace(cli.Hash) == "" {
		result.Status = "unmanaged-preserved"
		result.Detail = "install receipt does not identify a managed binary"
		return result
	}
	path, err := filepath.Abs(cli.Path)
	if err != nil {
		result.Status, result.Detail = "invalid-path-preserved", err.Error()
		return result
	}
	result.Path = path
	if managedByPackageManager(path) {
		result.Status = "package-manager-preserved"
		result.Detail = "binary path is owned by a package manager"
		return result
	}
	if method != "bootstrap-copy" {
		result.Status = "externally-managed-preserved"
		result.Detail = "install receipt does not identify a binary copied by Mimir"
		return result
	}
	root := string(filepath.Separator)
	if volume := filepath.VolumeName(path); volume != "" {
		root = volume + string(filepath.Separator)
	}
	if symlink, err := pathContainsSymlink(root, path); err != nil {
		result.Status, result.Detail = "remove-failed", err.Error()
		return result
	} else if symlink {
		result.Status = "symlink-preserved"
		result.Detail = "binary path contains a symlink"
		return result
	}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		result.Status = "missing-preserved"
		return result
	}
	if err != nil {
		result.Status, result.Detail = "remove-failed", err.Error()
		return result
	}
	if !info.Mode().IsRegular() {
		result.Status = "non-regular-preserved"
		return result
	}
	data, err := os.ReadFile(path)
	if err != nil {
		result.Status, result.Detail = "remove-failed", err.Error()
		return result
	}
	result.Hash = hashBytes(data)
	if result.Hash != cli.Hash {
		result.Status = "modified-preserved"
		result.Detail = "current hash differs from the install receipt"
		return result
	}
	if windows {
		deferred := path + ".uninstall"
		if _, err := os.Lstat(deferred); err == nil || !os.IsNotExist(err) {
			deferred = fmt.Sprintf("%s.%d.uninstall", path, time.Now().UnixNano())
		}
		if err := os.Rename(path, deferred); err != nil {
			result.Status, result.Detail = "remove-failed", err.Error()
			return result
		}
		result.PendingPath = deferred
		if err := launch(os.Getpid(), deferred); err != nil {
			result.Status = "pending-removal-unscheduled"
			result.Detail = "verified binary renamed and preserved, but launching deferred cleanup failed: " + err.Error()
			return result
		}
		result.Status = "pending-removal"
		result.DeferredUntilProcessExit = true
		result.Detail = "verified binary renamed; deletion is deferred until this process exits"
		return result
	}
	if err := os.Remove(path); err != nil {
		result.Status, result.Detail = "remove-failed", err.Error()
		return result
	}
	result.Status = "removed"
	return result
}

func uninstallResultSummary(report uninstallReport, keepBinary bool) (string, string) {
	removed, preserved := 0, 0
	for _, artifact := range report.Artifacts {
		if artifact.Status == artifactRemoved {
			removed++
		} else if artifact.Status != artifactUnowned {
			preserved++
		}
	}
	warning := preserved > 0 || report.Hermes.State == "preserved" || report.Binary.Status == "modified-preserved" || report.Binary.Status == "package-manager-preserved" || report.Binary.Status == "externally-managed-preserved" || report.Binary.Status == "remove-failed" || report.Binary.Status == "symlink-preserved" || report.Binary.Status == "non-regular-preserved" || report.Binary.Status == "pending-removal-unscheduled"
	result := "success"
	if report.Binary.Status == "pending-removal" || report.Binary.Status == "pending-removal-unscheduled" {
		result = "partial"
	} else if warning {
		result = "warning"
	}
	binary := report.Binary.Status
	if keepBinary {
		binary = "kept"
	}
	return result, fmt.Sprintf("Managed artifacts: %d removed, %d preserved; binary: %s", removed, preserved, binary)
}

func reconcileManagedArtifacts(enroll bool, operation string, write, adoptIdentical, requirePriorOwnership bool, installUpdate *installReceiptUpdate) (managedArtifactReport, error) {
	paths, err := managedInstallationPaths()
	if err != nil {
		return managedArtifactReport{}, err
	}
	operation = safeInstallOperation(operation)
	receipt, err := loadInstallReceipt()
	if err != nil {
		return managedArtifactReport{}, err
	}
	specs, err := bundledManagedArtifacts(paths)
	if err != nil {
		return managedArtifactReport{}, err
	}
	report := managedArtifactReport{
		Operation: operation, BundleVersion: version, ReceiptPath: paths.Receipt,
		LogPath: paths.Log, Artifacts: make([]managedArtifactResult, 0, len(specs)),
		BeforeVersion: receipt.CLI.Version, AfterVersion: receipt.CLI.Version,
	}
	if requirePriorOwnership {
		exists, err := regularFileExists(paths.Receipt)
		if err != nil {
			return report, err
		}
		if !exists || len(receipt.Artifacts) == 0 {
			write = false
			installUpdate = nil
		}
	}
	receiptChanged := receipt.migrated
	for _, spec := range specs {
		result, own, changed, err := reconcileManagedArtifact(spec, receipt.Artifacts[spec.target], enroll, write, adoptIdentical)
		if err != nil {
			return report, err
		}
		report.Artifacts = append(report.Artifacts, result)
		if own && write {
			receipt.Artifacts[spec.target] = installReceiptArtifact{Source: spec.source, Hash: spec.hash}
			receiptChanged = receiptChanged || changed
		}
	}
	if write {
		currentTargets := make(map[string]struct{}, len(specs))
		for _, spec := range specs {
			currentTargets[spec.target] = struct{}{}
		}
		obsolete := make([]string, 0)
		for target := range receipt.Artifacts {
			if _, current := currentTargets[target]; !current {
				obsolete = append(obsolete, target)
			}
		}
		sort.Strings(obsolete)
		for _, target := range obsolete {
			owned := receipt.Artifacts[target]
			spec, recognized := receiptManagedArtifactSpec(paths, target, owned)
			if !recognized {
				spec = managedArtifactSpec{source: owned.Source, target: target}
			}
			result, removed := uninstallManagedArtifact(spec, owned, true)
			if result.Detail == "" {
				result.Detail = "obsolete managed artifact"
			}
			report.Artifacts = append(report.Artifacts, result)
			if removed {
				delete(receipt.Artifacts, target)
				receiptChanged = true
				cleanupManagedArtifactDirs(spec)
			}
		}
	}
	if write {
		if installUpdate != nil {
			if installUpdate.Source != "" || installUpdate.Method != "" {
				receipt.Source = installUpdate.Source
				receipt.Method = installUpdate.Method
			}
			receipt.CLI = installUpdate.CLI
			report.AfterVersion = installUpdate.CLI.Version
			receiptChanged = true
		}
		if receipt.InstallationID == "" {
			receipt.InstallationID, err = newInstallationID()
			if err != nil {
				return report, err
			}
			receiptChanged = true
		}
		now := time.Now().UTC().Format(time.RFC3339Nano)
		if receipt.InstalledAt == "" {
			receipt.InstalledAt = now
			receiptChanged = true
		}
		if receiptChanged || receipt.BundleVersion != version {
			receipt.BundleVersion = version
			receipt.UpdatedAt = now
			if err := writeJSONAtomic(paths.Receipt, receipt); err != nil {
				return report, err
			}
		}
		report.Result = "success"
		if artifactIssueCount(report) > 0 {
			report.Result = "warning"
		}
		report.Summary = artifactSummary(report)
		if err := appendInstallLog(paths.Log, report); err != nil {
			return report, err
		}
	}
	return report, nil
}

func currentExecutableReceiptCLI() (installReceiptCLI, error) {
	path, err := executablePath()
	if err != nil {
		return installReceiptCLI{}, fmt.Errorf("locating current executable: %w", err)
	}
	path, err = filepath.Abs(path)
	if err != nil {
		return installReceiptCLI{}, err
	}
	if symlink, err := pathContainsSymlink(filesystemRoot(path), path); err != nil {
		return installReceiptCLI{}, err
	} else if symlink {
		return installReceiptCLI{}, fmt.Errorf("refusing to record symlinked executable %s", path)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return installReceiptCLI{}, err
	}
	if !info.Mode().IsRegular() {
		return installReceiptCLI{}, fmt.Errorf("current executable is not a regular file: %s", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return installReceiptCLI{}, err
	}
	return installReceiptCLI{Path: path, Version: version, Commit: commit, BuildDate: date, Hash: hashBytes(data)}, nil
}

func regularFileExists(path string) (bool, error) {
	if symlink, err := pathContainsSymlink(filesystemRoot(path), path); err != nil {
		return false, err
	} else if symlink {
		return false, fmt.Errorf("refusing symlinked path %s", path)
	}
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return info.Mode().IsRegular(), nil
}

func hasManagedInstallReceipt() (bool, error) {
	paths, err := managedInstallationPaths()
	if err != nil {
		return false, err
	}
	exists, err := regularFileExists(paths.Receipt)
	if err != nil || !exists {
		return false, err
	}
	receipt, err := loadInstallReceipt()
	if err != nil {
		return false, err
	}
	return len(receipt.Artifacts) > 0, nil
}

func newInstallationID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("creating installation ID: %w", err)
	}
	return hex.EncodeToString(value), nil
}

func bundledManagedArtifacts(paths installationPaths) ([]managedArtifactSpec, error) {
	metadata, err := mimirassets.BundleMetadata()
	if err != nil {
		return nil, err
	}
	targets := []struct{ source, target, root string }{
		{"plugins/opencode/mimir.ts", filepath.Join(paths.OpenCodeHome, "plugins", "mimir.ts"), paths.OpenCodeHome},
	}
	for _, skill := range []string{"mimir-setup", "mimir-use"} {
		prefix := "skills/" + skill + "/"
		for _, file := range metadata {
			if strings.HasPrefix(file.Path, prefix) {
				rel := strings.TrimPrefix(file.Path, "skills/")
				targets = append(targets, struct{ source, target, root string }{file.Path, filepath.Join(paths.OpenCodeHome, "skills", filepath.FromSlash(rel)), paths.OpenCodeHome})
			}
		}
	}
	if paths.HermesDetected {
		for _, source := range []string{"plugins/hermes/__init__.py", "plugins/hermes/plugin.yaml"} {
			targets = append(targets, struct{ source, target, root string }{source, filepath.Join(paths.HermesHome, "plugins", "mimir", filepath.Base(source)), paths.HermesHome})
		}
		for _, file := range metadata {
			if strings.HasPrefix(file.Path, "skills/") {
				rel := strings.TrimPrefix(file.Path, "skills/")
				targets = append(targets, struct{ source, target, root string }{file.Path, filepath.Join(paths.HermesHome, "skills", filepath.FromSlash(rel)), paths.HermesHome})
			}
		}
	}
	specs := make([]managedArtifactSpec, 0, len(targets))
	for _, target := range targets {
		data, err := mimirassets.Bundle.ReadFile(target.source)
		if err != nil {
			return nil, err
		}
		cleanupDir := ""
		if strings.HasPrefix(target.source, "skills/mimir-setup/") {
			cleanupDir = filepath.Join(target.root, "skills", "mimir-setup")
		} else if strings.HasPrefix(target.source, "skills/mimir-use/") {
			cleanupDir = filepath.Join(target.root, "skills", "mimir-use")
		} else if strings.HasPrefix(target.source, "plugins/hermes/") {
			cleanupDir = filepath.Join(target.root, "plugins", "mimir")
		}
		specs = append(specs, managedArtifactSpec{source: target.source, target: target.target, root: target.root, cleanupDir: cleanupDir, data: data, hash: hashBytes(data)})
	}
	sort.Slice(specs, func(i, j int) bool { return specs[i].target < specs[j].target })
	return specs, nil
}

func safeInstallOperation(operation string) string {
	switch operation {
	case "setup", "login", "install", "update", "refresh", "repair", "check", "test":
		return operation
	default:
		return "sync"
	}
}

func reconcileManagedArtifact(spec managedArtifactSpec, owned installReceiptArtifact, enroll, write, adoptIdentical bool) (managedArtifactResult, bool, bool, error) {
	result := managedArtifactResult{Path: spec.target, Source: spec.source, BundleHash: spec.hash}
	if symlink, err := pathContainsSymlink(spec.root, spec.target); err != nil {
		return result, false, false, err
	} else if symlink {
		result.Status = artifactSymlinkRejected
		result.Detail = "refusing to manage a symlinked path"
		return result, false, false, nil
	}
	info, err := os.Lstat(spec.target)
	if os.IsNotExist(err) {
		if !enroll && owned.Hash == "" {
			result.Status = artifactMissing
			return result, false, false, nil
		}
		if write {
			if err := writeFileAtomic(spec.root, spec.target, spec.data); err != nil {
				return result, false, false, err
			}
		}
		result.Status, result.Hash = artifactInstalled, spec.hash
		return result, true, true, nil
	}
	if err != nil {
		return result, false, false, err
	}
	if !info.Mode().IsRegular() {
		result.Status = artifactConflict
		result.Detail = "target is not a regular file"
		return result, false, false, nil
	}
	data, err := os.ReadFile(spec.target)
	if err != nil {
		return result, false, false, err
	}
	currentHash := hashBytes(data)
	result.Hash = currentHash
	if currentHash == spec.hash {
		if owned.Hash == "" {
			if !write || !adoptIdentical {
				result.Status = artifactIdentical
				return result, false, false, nil
			}
			result.Status = artifactAdopted
			return result, true, true, nil
		}
		result.Status = artifactCurrent
		return result, true, owned.Hash != spec.hash || owned.Source != spec.source, nil
	}
	if owned.Hash == "" {
		result.Status = artifactConflict
		result.Detail = "unowned file differs from bundled content"
		return result, false, false, nil
	}
	if currentHash != owned.Hash {
		result.Status = artifactModified
		result.Detail = "receipt-owned file was modified; preserving it"
		return result, false, false, nil
	}
	if !write {
		result.Status = artifactOutdated
		return result, false, false, nil
	}
	if write {
		if err := writeFileAtomic(spec.root, spec.target, spec.data); err != nil {
			return result, false, false, err
		}
	}
	result.Status, result.Hash = artifactUpdated, spec.hash
	return result, true, true, nil
}

func hashBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum)
}

func pathContainsSymlink(root, target string) (bool, error) {
	root, err := filepath.Abs(root)
	if err != nil {
		return false, err
	}
	target, err = filepath.Abs(target)
	if err != nil {
		return false, err
	}
	rel, err := filepath.Rel(root, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return false, fmt.Errorf("managed artifact path escapes installation root")
	}
	volumeRoot := filesystemRoot(target)
	fromVolume, err := filepath.Rel(volumeRoot, target)
	if err != nil || fromVolume == ".." || strings.HasPrefix(fromVolume, ".."+string(filepath.Separator)) {
		return false, fmt.Errorf("path escapes filesystem root")
	}
	current := volumeRoot
	parts := []string{}
	if fromVolume != "." {
		parts = strings.Split(fromVolume, string(filepath.Separator))
	}
	for _, part := range append([]string{""}, parts...) {
		if part != "" {
			current = filepath.Join(current, part)
		}
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return false, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return true, nil
		}
	}
	return false, nil
}

func filesystemRoot(path string) string {
	volume := filepath.VolumeName(filepath.Clean(path))
	if volume != "" {
		return volume + string(filepath.Separator)
	}
	return string(filepath.Separator)
}

func writeFileAtomic(root, path string, data []byte) error {
	if symlink, err := pathContainsSymlink(root, path); err != nil {
		return err
	} else if symlink {
		return fmt.Errorf("refusing to write symlinked path %s", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if symlink, err := pathContainsSymlink(root, path); err != nil {
		return err
	} else if symlink {
		return fmt.Errorf("refusing to write symlinked path %s", path)
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".mimir-artifact-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if symlink, err := pathContainsSymlink(root, path); err != nil {
		return err
	} else if symlink {
		return fmt.Errorf("refusing to replace symlinked path %s", path)
	}
	return os.Rename(tempPath, path)
}

func writeJSONAtomic(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return writeFileAtomic(filepath.Dir(path), path, data)
}

func appendInstallLog(path string, report managedArtifactReport) error {
	data, err := json.Marshal(struct {
		Timestamp string `json:"timestamp"`
		managedArtifactReport
	}{Timestamp: time.Now().UTC().Format(time.RFC3339Nano), managedArtifactReport: report})
	if err != nil {
		return err
	}
	if symlink, err := pathContainsSymlink(filesystemRoot(path), path); err != nil {
		return err
	} else if symlink {
		return errors.New("refusing to append to symlinked install log path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if symlink, err := pathContainsSymlink(filesystemRoot(path), path); err != nil {
		return err
	} else if symlink {
		return errors.New("refusing to append to symlinked install log path")
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	if err := file.Chmod(0o600); err != nil {
		return err
	}
	_, err = file.Write(append(data, '\n'))
	return err
}

func appendUninstallLog(path string, report uninstallReport) error {
	data, err := json.Marshal(struct {
		Timestamp string `json:"timestamp"`
		uninstallReport
	}{Timestamp: time.Now().UTC().Format(time.RFC3339Nano), uninstallReport: report})
	if err != nil {
		return err
	}
	if symlink, err := pathContainsSymlink(filesystemRoot(path), path); err != nil {
		return err
	} else if symlink {
		return errors.New("refusing to append to symlinked install log path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if symlink, err := pathContainsSymlink(filesystemRoot(path), path); err != nil {
		return err
	} else if symlink {
		return errors.New("refusing to append to symlinked install log path")
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	if err := file.Chmod(0o600); err != nil {
		return err
	}
	_, err = file.Write(append(data, '\n'))
	return err
}
