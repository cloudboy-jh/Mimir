package install

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	mimirassets "github.com/cloudboy-jh/mimir"
)

func installFixture(t *testing.T, harnesses ...string) (InstallationPaths, InstallReport) {
	t.Helper()
	paths := isolatedInstallation(t, false)
	return paths, installAgain(t, harnesses...)
}

func installAgain(t *testing.T, harnesses ...string) InstallReport {
	t.Helper()
	source := filepath.Join(t.TempDir(), binaryName())
	if err := os.WriteFile(source, []byte("mimir binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(filepath.Dir(filepath.Dir(source)), "bin")
	report, err := Install(bin, harnesses, func() (string, error) { return source, nil })
	if err != nil {
		t.Fatal(err)
	}
	return report
}

func bundled(t *testing.T, source string) []byte {
	t.Helper()
	data, err := mimirassets.Bundle.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func artifactFor(t *testing.T, report ArtifactReport, path string) ArtifactResult {
	t.Helper()
	for _, artifact := range report.Artifacts {
		if artifact.Path == path {
			return artifact
		}
	}
	t.Fatalf("no artifact for %s in %#v", path, report.Artifacts)
	return ArtifactResult{}
}

func TestInstallWritesAndRecordsAbsentFiles(t *testing.T) {
	paths, report := installFixture(t, "cursor")
	hooks, mcp := filepath.Join(paths.CursorHome, "hooks.json"), filepath.Join(paths.CursorHome, "mcp.json")
	for _, path := range []string{hooks, mcp} {
		if status := artifactFor(t, report.Artifacts, path).Status; status != ArtifactInstalled {
			t.Fatalf("%s status = %s", path, status)
		}
	}
	manifest, err := LoadManifest()
	if err != nil {
		t.Fatal(err)
	}
	if manifest.Files[hooks] != hashBytes(bundled(t, "plugins/cursor/hooks.json")) || manifest.Files[mcp] != hashBytes(mustReadFile(t, mcp)) {
		t.Fatalf("files = %#v", manifest.Files)
	}
	if manifest.InstallationID == "" || manifest.CLI.Path != report.Binary.Path || strings.Join(manifest.Harnesses, ",") != "cursor" {
		t.Fatalf("manifest = %#v", manifest)
	}
	var config struct {
		MCPServers map[string]struct {
			Command string   `json:"command"`
			Args    []string `json:"args"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(mustReadFile(t, mcp), &config); err != nil {
		t.Fatal(err)
	}
	if server := config.MCPServers["mimir"]; server.Command != report.Binary.Path || strings.Join(server.Args, " ") != "mcp" {
		t.Fatalf("mcp config = %#v", config)
	}
}

func TestInstallReusesRecordedSelection(t *testing.T) {
	paths, _ := installFixture(t, "codex")
	report := installAgain(t)
	if status := artifactFor(t, report.Artifacts, filepath.Join(paths.AgentPlugins, "plugins", "mimir", "mcp.json")).Status; status != ArtifactCurrent {
		t.Fatalf("status = %s", status)
	}
	if manifest, _ := LoadManifest(); strings.Join(manifest.Harnesses, ",") != "codex" {
		t.Fatalf("harnesses = %v", manifest.Harnesses)
	}
}

func TestOwnedUnmodifiedFileIsOverwritten(t *testing.T) {
	paths, _ := installFixture(t, "cursor")
	hooks := filepath.Join(paths.CursorHome, "hooks.json")
	if err := os.WriteFile(hooks, []byte("older Mimir hooks"), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest, _ := LoadManifest()
	manifest.Files[hooks] = hashBytes([]byte("older Mimir hooks"))
	if err := saveManifest(paths, manifest); err != nil {
		t.Fatal(err)
	}
	report, err := RefreshArtifacts("update")
	if err != nil {
		t.Fatal(err)
	}
	if status := artifactFor(t, report, hooks).Status; status != ArtifactUpdated {
		t.Fatalf("status = %s", status)
	}
	want := bundled(t, "plugins/cursor/hooks.json")
	if string(mustReadFile(t, hooks)) != string(want) {
		t.Fatal("owned file was not refreshed")
	}
	if manifest, _ := LoadManifest(); manifest.Files[hooks] != hashBytes(want) {
		t.Fatalf("recorded hash = %s", manifest.Files[hooks])
	}
}

func TestModifiedOwnedFileIsPreserved(t *testing.T) {
	paths, _ := installFixture(t, "cursor")
	hooks := filepath.Join(paths.CursorHome, "hooks.json")
	if err := os.WriteFile(hooks, []byte("user edit"), 0o600); err != nil {
		t.Fatal(err)
	}
	report, err := RefreshArtifacts("update")
	if err != nil {
		t.Fatal(err)
	}
	if status := artifactFor(t, report, hooks).Status; status != ArtifactModified {
		t.Fatalf("status = %s", status)
	}
	if string(mustReadFile(t, hooks)) != "user edit" {
		t.Fatal("modified file was overwritten")
	}
	if manifest, _ := LoadManifest(); manifest.Files[hooks] != hashBytes(bundled(t, "plugins/cursor/hooks.json")) {
		t.Fatal("recorded hash changed")
	}
	if ArtifactsReady(report, "cursor") {
		t.Fatal("modified hooks reported ready")
	}
}

func TestUnownedSharedMCPConfigConflictsWithSnippet(t *testing.T) {
	paths := isolatedInstallation(t, false)
	mcp := filepath.Join(paths.CursorHome, "mcp.json")
	if err := os.MkdirAll(paths.CursorHome, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mcp, []byte(`{"mcpServers":{"other":{}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	report := installAgain(t, "cursor")
	artifact := artifactFor(t, report.Artifacts, mcp)
	if artifact.Status != ArtifactConflict || !strings.Contains(artifact.Snippet, `"mcp"`) || !strings.Contains(artifact.Snippet, report.Binary.Path) {
		t.Fatalf("artifact = %#v", artifact)
	}
	if string(mustReadFile(t, mcp)) != `{"mcpServers":{"other":{}}}` {
		t.Fatal("shared file was edited")
	}
	if steps := ManualSteps(report.Artifacts); len(steps) != 1 || steps[0].Path != mcp {
		t.Fatalf("manual steps = %#v", steps)
	}
	if !ArtifactsReady(report.Artifacts, "cursor") {
		t.Fatal("MCP conflict blocked hook readiness")
	}
	if manifest, _ := LoadManifest(); manifest.Files[mcp] != "" {
		t.Fatal("unowned file was recorded")
	}
}

func TestUninstallRemovesOnlyUnmodifiedOwnedFiles(t *testing.T) {
	paths, report := installFixture(t, "claude-code", "codex")
	pluginDir := filepath.Join(paths.ClaudeCodeHome, "skills", "mimir")
	hooks := filepath.Join(pluginDir, "hooks", "hooks.json")
	if err := os.WriteFile(hooks, []byte("user edit"), 0o600); err != nil {
		t.Fatal(err)
	}
	uninstall, err := Uninstall(false)
	if err != nil {
		t.Fatal(err)
	}
	removed := 0
	for _, artifact := range uninstall.Artifacts {
		switch {
		case artifact.Path == hooks && artifact.Status != ArtifactModified:
			t.Fatalf("hooks status = %s", artifact.Status)
		case artifact.Status == ArtifactRemoved:
			removed++
		}
	}
	if removed != len(uninstall.Artifacts)-1 {
		t.Fatalf("artifacts = %#v", uninstall.Artifacts)
	}
	if string(mustReadFile(t, hooks)) != "user edit" {
		t.Fatal("modified file was removed")
	}
	gone := []string{filepath.Join(pluginDir, ".mcp.json"), filepath.Join(pluginDir, ".claude-plugin"), paths.AgentPlugins}
	if runtime.GOOS != "windows" {
		gone = append(gone, report.Binary.Path)
	}
	for _, path := range gone {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("%s still exists: %v", path, err)
		}
	}
	manifest, _ := LoadManifest()
	if len(manifest.Files) != 1 || manifest.Files[hooks] == "" || len(manifest.Harnesses) != 0 || (runtime.GOOS != "windows" && manifest.CLI.Path != "") {
		t.Fatalf("manifest = %#v", manifest)
	}
}

func TestHarnessDisableRemovesItsFiles(t *testing.T) {
	paths, _ := installFixture(t, "codex", "cursor")
	report, err := SetHarnessEnabled("cursor", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{filepath.Join(paths.CursorHome, "hooks.json"), filepath.Join(paths.CursorHome, "mcp.json")} {
		if status := artifactFor(t, report, path).Status; status != ArtifactRemoved {
			t.Fatalf("%s status = %s", path, status)
		}
	}
	manifest, _ := LoadManifest()
	if strings.Join(manifest.Harnesses, ",") != "codex" || len(manifest.Files) != 4 {
		t.Fatalf("manifest = %#v", manifest)
	}
}

func TestLegacyReceiptSeedsManifest(t *testing.T) {
	paths := isolatedInstallation(t, false)
	if err := os.MkdirAll(paths.MimirHome, 0o700); err != nil {
		t.Fatal(err)
	}
	receipt := `{"schema":3,"installation_id":"install-1","bundle_version":"1.2.3","source":"release",
		"cli":{"path":"/bin/mimir","version":"1.2.3","commit":"abc","build_date":"today","sha256":"cli-hash"},
		"harnesses":["cursor"],"artifacts":{"/home/x/.cursor/hooks.json":{"source":"plugins/cursor/hooks.json","sha256":"hooks-hash"}}}`
	legacy := map[string]string{"install-receipt.json": receipt, "install-receipt.json.lock": "", "install-log.jsonl": "{}\n"}
	for name, content := range legacy {
		if err := os.WriteFile(filepath.Join(paths.MimirHome, name), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	manifest, err := LoadManifest()
	if err != nil {
		t.Fatal(err)
	}
	if manifest.InstallationID != "install-1" || manifest.BundleVersion != "1.2.3" || manifest.CLI != (ManifestCLI{Path: "/bin/mimir", Version: "1.2.3", Commit: "abc", SHA256: "cli-hash"}) ||
		strings.Join(manifest.Harnesses, ",") != "cursor" || manifest.Files["/home/x/.cursor/hooks.json"] != "hooks-hash" {
		t.Fatalf("manifest = %#v", manifest)
	}
	for name := range legacy {
		if _, err := os.Lstat(filepath.Join(paths.MimirHome, name)); !os.IsNotExist(err) {
			t.Fatalf("%s survived migration: %v", name, err)
		}
	}
	if _, err := os.Stat(paths.Manifest); err != nil {
		t.Fatal(err)
	}
}
