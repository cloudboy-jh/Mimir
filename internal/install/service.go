package install

import (
	"fmt"
	"os"
	"runtime"
	"sort"
)

type InstallReport struct {
	Binary    BinaryReport   `json:"binary"`
	Artifacts ArtifactReport `json:"artifacts"`
}

// Install installs the CLI binary and the files of the selected harnesses.
// A nil selection keeps the recorded selection.
func Install(explicitDir string, harnesses []string, executable func() (string, error)) (InstallReport, error) {
	paths, err := Paths()
	if err != nil {
		return InstallReport{}, err
	}
	manifest, err := loadManifest(paths)
	if err != nil {
		return InstallReport{}, err
	}
	binary, err := bootstrapExecutable(explicitDir, manifest.CLI, executable)
	if err != nil {
		return InstallReport{}, err
	}
	manifest.CLI = ManifestCLI{Path: binary.Path, Version: binary.Version, Commit: binary.Commit, SHA256: binary.Hash}
	artifacts, err := syncArtifacts(paths, &manifest, "install", harnesses, true)
	if err != nil {
		return InstallReport{}, err
	}
	return InstallReport{Binary: binary, Artifacts: artifacts}, nil
}

type UninstallBinaryResult struct {
	Path   string `json:"path,omitempty"`
	Status string `json:"status"`
	Detail string `json:"detail,omitempty"`
}

type UninstallReport struct {
	ManifestPath string                `json:"manifest_path"`
	Binary       UninstallBinaryResult `json:"binary"`
	Artifacts    []ArtifactResult      `json:"artifacts"`
}

// Uninstall removes every owned file that still holds the recorded bytes and,
// unless keepBinary, the recorded CLI binary.
func Uninstall(keepBinary bool) (UninstallReport, error) {
	paths, err := Paths()
	if err != nil {
		return UninstallReport{}, err
	}
	manifest, err := loadManifest(paths)
	if err != nil {
		return UninstallReport{}, err
	}
	report := UninstallReport{ManifestPath: paths.Manifest, Artifacts: []ArtifactResult{}}
	targets := make([]string, 0, len(manifest.Files))
	for target := range manifest.Files {
		targets = append(targets, target)
	}
	sort.Strings(targets)
	for _, target := range targets {
		result, keep := removeOwned(target, manifest.Files[target])
		report.Artifacts = append(report.Artifacts, result)
		if !keep {
			delete(manifest.Files, target)
			pruneEmptyDirs(paths, target)
		}
	}
	manifest.Harnesses = []string{}
	report.Binary = removeBinary(manifest.CLI, keepBinary)
	if report.Binary.Status == "removed" {
		manifest.CLI = ManifestCLI{}
	}
	return report, saveManifest(paths, manifest)
}

func removeBinary(cli ManifestCLI, keep bool) UninstallBinaryResult {
	result := UninstallBinaryResult{Path: cli.Path}
	switch {
	case cli.Path == "" || cli.SHA256 == "":
		result.Status, result.Detail = "kept", "no installed binary recorded"
		return result
	case keep:
		result.Status, result.Detail = "kept", "--keep-binary"
		return result
	case managedByKnownPackageManager(cli.Path):
		result.Status, result.Detail = "kept", "installed by a package manager"
		return result
	}
	if err := rejectSymlink(cli.Path); err != nil {
		result.Status, result.Detail = "kept", err.Error()
		return result
	}
	data, err := os.ReadFile(cli.Path)
	if os.IsNotExist(err) {
		result.Status = "missing"
		return result
	}
	if err != nil {
		result.Status, result.Detail = "kept", err.Error()
		return result
	}
	if hashBytes(data) != cli.SHA256 {
		result.Status, result.Detail = "kept", "binary changed since Mimir installed it"
		return result
	}
	if runtime.GOOS == "windows" {
		result.Status, result.Detail = "kept", fmt.Sprintf("delete %s after this command exits", cli.Path)
		return result
	}
	if err := os.Remove(cli.Path); err != nil {
		result.Status, result.Detail = "kept", err.Error()
		return result
	}
	result.Status = "removed"
	return result
}
