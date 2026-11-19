package install

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	mimirassets "github.com/cloudboy-jh/mimir"
)

type ArtifactStatus string

const (
	ArtifactInstalled ArtifactStatus = "installed"
	ArtifactUpdated   ArtifactStatus = "updated"
	ArtifactCurrent   ArtifactStatus = "current"
	ArtifactConflict  ArtifactStatus = "conflict"
	ArtifactModified  ArtifactStatus = "modified"
	ArtifactMissing   ArtifactStatus = "missing"
	ArtifactRemoved   ArtifactStatus = "removed"
)

// ArtifactResult is the state of one managed file. Hash is the sha256 Mimir
// manages for the path. Snippet is the exact content to merge by hand when a
// shared MCP configuration file could not be written.
type ArtifactResult struct {
	Path    string         `json:"path"`
	Source  string         `json:"source"`
	Harness string         `json:"harness,omitempty"`
	Status  ArtifactStatus `json:"status"`
	Hash    string         `json:"sha256,omitempty"`
	Detail  string         `json:"detail,omitempty"`
	Snippet string         `json:"snippet,omitempty"`
}

type ArtifactReport struct {
	Operation    string           `json:"operation"`
	ManifestPath string           `json:"manifest_path"`
	Artifacts    []ArtifactResult `json:"artifacts"`
}

// MCPSource labels the generated MCP configuration of a harness.
func MCPSource(harness string) string { return "mcp/" + harness }

type fileSpec struct {
	harness string
	source  string
	target  string
	root    string
	data    []byte
	snippet string
}

func harnessSpecs(paths InstallationPaths, executable string, selected map[string]bool) ([]fileSpec, error) {
	metadata, err := mimirassets.BundleMetadata()
	if err != nil {
		return nil, err
	}
	type bundled struct{ harness, source, target, root string }
	claudePlugin := filepath.Join(paths.ClaudeCodeHome, "skills", "mimir")
	codexPlugin := filepath.Join(paths.AgentPlugins, "plugins", "mimir")
	files := []bundled{
		{"pi", "plugins/pi/mimir.ts", filepath.Join(paths.PiHome, "extensions", "mimir.ts"), paths.PiHome},
		{"oh-my-pi", "plugins/oh-my-pi/mimir.ts", filepath.Join(paths.OhMyPiHome, "extensions", "mimir.ts"), paths.OhMyPiHome},
		{"opencode", "plugins/opencode/mimir.ts", filepath.Join(paths.OpenCodeHome, "plugins", "mimir.ts"), paths.OpenCodeHome},
		{"claude-code", "plugins/claude-code/.claude-plugin/plugin.json", filepath.Join(claudePlugin, ".claude-plugin", "plugin.json"), paths.ClaudeCodeHome},
		{"claude-code", "plugins/claude-code/hooks/hooks.json", filepath.Join(claudePlugin, "hooks", "hooks.json"), paths.ClaudeCodeHome},
		{"codex", "plugins/codex/marketplace.json", filepath.Join(paths.AgentPlugins, "marketplace.json"), paths.AgentPlugins},
		{"codex", "plugins/codex/plugin.json", filepath.Join(codexPlugin, "plugin.json"), paths.AgentPlugins},
		{"codex", "plugins/codex/hooks/hooks.json", filepath.Join(codexPlugin, "hooks", "hooks.json"), paths.AgentPlugins},
		{"cursor", "plugins/cursor/hooks.json", filepath.Join(paths.CursorHome, "hooks.json"), paths.CursorHome},
	}
	for _, file := range metadata {
		if rel, ok := strings.CutPrefix(file.Path, "skills/"); ok {
			if strings.HasPrefix(rel, "mimir-setup/") || strings.HasPrefix(rel, "mimir-use/") {
				files = append(files, bundled{"opencode", file.Path, filepath.Join(paths.OpenCodeHome, "skills", filepath.FromSlash(rel)), paths.OpenCodeHome})
			}
			if paths.HermesDetected {
				files = append(files, bundled{"hermes", file.Path, filepath.Join(paths.HermesHome, "skills", filepath.FromSlash(rel)), paths.HermesHome})
			}
		}
	}
	if paths.HermesDetected {
		for _, source := range []string{"plugins/hermes/__init__.py", "plugins/hermes/plugin.yaml"} {
			files = append(files, bundled{"hermes", source, filepath.Join(paths.HermesHome, "plugins", "mimir", filepath.Base(source)), paths.HermesHome})
		}
	}
	var specs []fileSpec
	for _, file := range files {
		if !selected[file.harness] {
			continue
		}
		data, err := mimirassets.Bundle.ReadFile(file.source)
		if err != nil {
			return nil, err
		}
		specs = append(specs, fileSpec{harness: file.harness, source: file.source, target: file.target, root: file.root, data: data})
	}
	mcp := []struct {
		harness, target, root string
		portable              bool
	}{
		{"claude-code", filepath.Join(claudePlugin, ".mcp.json"), paths.ClaudeCodeHome, false},
		{"codex", filepath.Join(codexPlugin, "mcp.json"), paths.AgentPlugins, true},
		{"cursor", filepath.Join(paths.CursorHome, "mcp.json"), paths.CursorHome, false},
	}
	for _, entry := range mcp {
		if !selected[entry.harness] || executable == "" {
			continue
		}
		data, err := mcpConfig(executable, entry.portable)
		if err != nil {
			return nil, err
		}
		specs = append(specs, fileSpec{harness: entry.harness, source: MCPSource(entry.harness), target: entry.target, root: entry.root, data: data, snippet: strings.TrimSpace(string(data))})
	}
	sort.Slice(specs, func(i, j int) bool { return specs[i].target < specs[j].target })
	return specs, nil
}

// mcpConfig renders the stdio server entry that launches `<mimir> mcp`.
// Codex plugins use the Agent Plugins mcp.json schema, which requires a type.
func mcpConfig(executable string, portable bool) ([]byte, error) {
	server := map[string]any{"command": executable, "args": []string{"mcp"}}
	document := map[string]any{"mcpServers": map[string]any{"mimir": server}}
	if portable {
		server["type"] = "stdio"
		document["$schema"] = "https://agent-plugins.org/schemas/1.0.0/mcp.schema.json"
	}
	data, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

// ownedDirs are directories Mimir creates and may remove once empty.
func ownedDirs(paths InstallationPaths) []string {
	dirs := []string{
		filepath.Join(paths.ClaudeCodeHome, "skills", "mimir"),
		paths.AgentPlugins,
		filepath.Join(paths.OpenCodeHome, "skills", "mimir-setup"),
		filepath.Join(paths.OpenCodeHome, "skills", "mimir-use"),
	}
	if paths.HermesHome != "" {
		dirs = append(dirs,
			filepath.Join(paths.HermesHome, "plugins", "mimir"),
			filepath.Join(paths.HermesHome, "skills", "mimir-setup"),
			filepath.Join(paths.HermesHome, "skills", "mimir-use"),
		)
	}
	return dirs
}

func pruneEmptyDirs(paths InstallationPaths, target string) {
	for _, owned := range ownedDirs(paths) {
		if !pathWithin(owned, target) {
			continue
		}
		for dir := filepath.Dir(target); ; dir = filepath.Dir(dir) {
			info, err := os.Lstat(dir)
			if err != nil || isLink(info) || !info.IsDir() || os.Remove(dir) != nil || sameFilePath(dir, owned) {
				return
			}
		}
	}
}

// reconcileFile applies the ownership rule: write only when the target is
// absent or still holds the bytes Mimir recorded. It returns the hash to record
// ("" when the path is not owned).
func reconcileFile(spec fileSpec, recorded string, write bool) (ArtifactResult, string) {
	want := hashBytes(spec.data)
	result := ArtifactResult{Path: spec.target, Source: spec.source, Harness: spec.harness, Hash: recorded}
	conflict := func(status ArtifactStatus, detail string) (ArtifactResult, string) {
		result.Status, result.Detail, result.Snippet = status, detail, spec.snippet
		return result, recorded
	}
	if symlink, err := pathContainsSymlink(spec.root, spec.target); err != nil {
		return conflict(ArtifactConflict, err.Error())
	} else if symlink {
		return conflict(ArtifactConflict, "refusing to manage a symlinked path")
	}
	info, err := os.Lstat(spec.target)
	current := ""
	switch {
	case os.IsNotExist(err):
	case err != nil:
		return conflict(ArtifactConflict, err.Error())
	case !info.Mode().IsRegular():
		return conflict(ArtifactConflict, "target is not a regular file")
	default:
		data, err := os.ReadFile(spec.target)
		if err != nil {
			return conflict(ArtifactConflict, err.Error())
		}
		current = hashBytes(data)
	}
	switch {
	case current == want:
		result.Status, result.Hash = ArtifactCurrent, want
		return result, want
	case current != "" && recorded == "":
		return conflict(ArtifactConflict, "file exists and is not managed by Mimir; preserved")
	case current != "" && current != recorded:
		return conflict(ArtifactModified, "file changed since Mimir wrote it; preserved")
	case !write:
		if current == "" {
			result.Status = ArtifactMissing
		} else {
			result.Status = ArtifactCurrent
		}
		return result, recorded
	}
	if err := writeFileAtomic(spec.root, spec.target, spec.data); err != nil {
		return conflict(ArtifactConflict, err.Error())
	}
	result.Status, result.Hash = ArtifactInstalled, want
	if current != "" {
		result.Status = ArtifactUpdated
	}
	return result, want
}

// removeOwned deletes target only while it still holds the recorded bytes.
// keep reports whether the manifest must keep tracking the path.
func removeOwned(target, recorded string) (result ArtifactResult, keep bool) {
	result = ArtifactResult{Path: target, Hash: recorded}
	if err := rejectSymlink(target); err != nil {
		result.Status, result.Detail = ArtifactConflict, err.Error()
		return result, true
	}
	info, err := os.Lstat(target)
	if os.IsNotExist(err) {
		result.Status = ArtifactMissing
		return result, false
	}
	if err != nil {
		result.Status, result.Detail = ArtifactConflict, err.Error()
		return result, true
	}
	if !info.Mode().IsRegular() {
		result.Status, result.Detail = ArtifactConflict, "target is not a regular file"
		return result, true
	}
	data, err := os.ReadFile(target)
	if err != nil {
		result.Status, result.Detail = ArtifactConflict, err.Error()
		return result, true
	}
	if hashBytes(data) != recorded {
		result.Status, result.Detail = ArtifactModified, "file changed since Mimir wrote it; preserved"
		return result, true
	}
	if err := os.Remove(target); err != nil {
		result.Status, result.Detail = ArtifactConflict, err.Error()
		return result, true
	}
	result.Status = ArtifactRemoved
	return result, false
}

// syncArtifacts reconciles the files of the selected harnesses (nil keeps the
// recorded selection). Without write it only reports state against the
// manifest. With write it also removes owned files that are no longer
// selected and saves the manifest.
func syncArtifacts(paths InstallationPaths, manifest *Manifest, operation string, selection []string, write bool) (ArtifactReport, error) {
	report := ArtifactReport{Operation: operation, ManifestPath: paths.Manifest, Artifacts: []ArtifactResult{}}
	if selection == nil {
		selection = manifest.Harnesses
	}
	selected := stringSet(selection)
	if selected["pi"] && selected["oh-my-pi"] && sameFilePath(paths.PiHome, paths.OhMyPiHome) {
		return report, fmt.Errorf("Pi and Oh My Pi resolve to the same agent directory; set OMP_PROFILE or OMP_CODING_AGENT_DIR before selecting both")
	}
	executable := manifest.CLI.Path
	if executable == "" {
		executable = currentExecutable()
	}
	specs, err := harnessSpecs(paths, executable, selected)
	if err != nil {
		return report, err
	}
	wanted := make(map[string]bool, len(specs))
	for _, spec := range specs {
		wanted[spec.target] = true
		result, record := reconcileFile(spec, manifest.Files[spec.target], write)
		report.Artifacts = append(report.Artifacts, result)
		if write && record != "" {
			manifest.Files[spec.target] = record
		}
	}
	if !write {
		return report, nil
	}
	targets := make([]string, 0, len(manifest.Files))
	for target := range manifest.Files {
		if !wanted[target] {
			targets = append(targets, target)
		}
	}
	sort.Strings(targets)
	for _, target := range targets {
		result, keep := removeOwned(target, manifest.Files[target])
		result.Detail = strings.TrimPrefix(result.Detail+"; no longer selected", "; ")
		report.Artifacts = append(report.Artifacts, result)
		if !keep {
			delete(manifest.Files, target)
			pruneEmptyDirs(paths, target)
		}
	}
	normalized, err := NormalizeHarnesses(selection)
	if err != nil {
		return report, err
	}
	manifest.Harnesses = normalized
	manifest.BundleVersion = version
	if manifest.InstallationID == "" {
		if manifest.InstallationID, err = newInstallationID(); err != nil {
			return report, err
		}
	}
	return report, saveManifest(paths, *manifest)
}

func currentExecutable() string {
	path, err := executablePath()
	if err != nil {
		return ""
	}
	if path, err = filepath.Abs(path); err != nil || temporaryExecutable(path) {
		return ""
	}
	return path
}

func withManifest(fn func(InstallationPaths, *Manifest) (ArtifactReport, error)) (ArtifactReport, error) {
	paths, err := Paths()
	if err != nil {
		return ArtifactReport{}, err
	}
	manifest, err := loadManifest(paths)
	if err != nil {
		return ArtifactReport{}, err
	}
	return fn(paths, &manifest)
}

// CheckArtifacts reports the selected harness files without writing.
func CheckArtifacts() (ArtifactReport, error) {
	return withManifest(func(paths InstallationPaths, manifest *Manifest) (ArtifactReport, error) {
		return syncArtifacts(paths, manifest, "check", nil, false)
	})
}

// RefreshArtifacts rewrites the files of the recorded harness selection. It
// does nothing when no harness has been installed.
func RefreshArtifacts(operation string) (ArtifactReport, error) {
	return withManifest(func(paths InstallationPaths, manifest *Manifest) (ArtifactReport, error) {
		if len(manifest.Harnesses) == 0 {
			return ArtifactReport{Operation: operation, ManifestPath: paths.Manifest, Artifacts: []ArtifactResult{}}, nil
		}
		return syncArtifacts(paths, manifest, operation, nil, true)
	})
}

// SelectArtifacts installs the files of exactly the given harnesses and
// removes unmodified files of every other harness.
func SelectArtifacts(operation string, harnesses []string) (ArtifactReport, error) {
	return withManifest(func(paths InstallationPaths, manifest *Manifest) (ArtifactReport, error) {
		return syncArtifacts(paths, manifest, operation, append([]string{}, harnesses...), true)
	})
}

func SetHarnessEnabled(id string, enabled bool) (ArtifactReport, error) {
	ids, err := NormalizeHarnesses([]string{id})
	if err != nil {
		return ArtifactReport{}, err
	}
	if len(ids) != 1 {
		return ArtifactReport{}, fmt.Errorf("enable or disable requires one harness id")
	}
	manifest, err := LoadManifest()
	if err != nil {
		return ArtifactReport{}, err
	}
	selected := stringSet(manifest.Harnesses)
	selected[ids[0]] = enabled
	selection := []string{}
	for _, harness := range canonicalHarnesses {
		if selected[harness.ID] {
			selection = append(selection, harness.ID)
		}
	}
	operation := "harness-disable"
	if enabled {
		operation = "harness-enable"
	}
	return SelectArtifacts(operation, selection)
}

func ArtifactUsable(status ArtifactStatus) bool {
	switch status {
	case ArtifactInstalled, ArtifactUpdated, ArtifactCurrent:
		return true
	}
	return false
}

func ArtifactIssueCount(report ArtifactReport) int {
	issues := 0
	for _, artifact := range report.Artifacts {
		if !ArtifactUsable(artifact.Status) && artifact.Status != ArtifactRemoved {
			issues++
		}
	}
	return issues
}

// ArtifactsReady reports whether at least one artifact of the harness exists
// and all its non-MCP artifacts are usable. MCP configuration conflicts are
// manual steps and never block the capture integration.
func ArtifactsReady(report ArtifactReport, harness string) bool {
	found := false
	for _, artifact := range report.Artifacts {
		if artifact.Harness != harness || artifact.Source == MCPSource(harness) || artifact.Status == ArtifactRemoved {
			continue
		}
		if !ArtifactUsable(artifact.Status) {
			return false
		}
		found = true
	}
	return found
}

// ManualSteps returns the MCP snippets the user must merge by hand.
func ManualSteps(report ArtifactReport) []ArtifactResult {
	var steps []ArtifactResult
	for _, artifact := range report.Artifacts {
		if artifact.Snippet != "" && !ArtifactUsable(artifact.Status) {
			steps = append(steps, artifact)
		}
	}
	return steps
}
