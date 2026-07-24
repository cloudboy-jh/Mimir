package mimircli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	mimirassets "github.com/cloudboy-jh/mimir"
)

func TestSyncManagedArtifactsInstallAndIdempotence(t *testing.T) {
	paths := isolatedInstallation(t, true)
	oldVersion := version
	version = "9.8.7-test"
	t.Cleanup(func() { version = oldVersion })

	report, err := syncManagedArtifacts(true, "install")
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Artifacts) == 0 {
		t.Fatal("install report contained no artifacts")
	}
	for _, artifact := range report.Artifacts {
		if artifact.Status != artifactInstalled {
			t.Fatalf("%s status = %s, want installed", artifact.Path, artifact.Status)
		}
		data, err := os.ReadFile(artifact.Path)
		if err != nil {
			t.Fatal(err)
		}
		if hashBytes(data) != artifact.BundleHash {
			t.Fatalf("%s content hash does not match bundle", artifact.Path)
		}
	}
	if _, err := os.Stat(filepath.Join(paths.OpenCodeHome, "plugins", "mimir.ts")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(paths.HermesHome, "plugins", "mimir", "plugin.yaml")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(paths.HermesHome, "skills", "mimir-use", "SKILL.md")); err != nil {
		t.Fatal(err)
	}

	receipt, err := loadInstallReceipt()
	if err != nil {
		t.Fatal(err)
	}
	if receipt.BundleVersion != version || len(receipt.Artifacts) != len(report.Artifacts) {
		t.Fatalf("receipt version/artifacts = %q/%d", receipt.BundleVersion, len(receipt.Artifacts))
	}
	assertPrivateFile(t, paths.Receipt)
	assertPrivateFile(t, paths.Log)

	second, err := syncManagedArtifacts(false, "refresh")
	if err != nil {
		t.Fatal(err)
	}
	for _, artifact := range second.Artifacts {
		if artifact.Status != artifactCurrent {
			t.Fatalf("%s status = %s, want current", artifact.Path, artifact.Status)
		}
	}
	if lines := jsonLines(t, paths.Log); lines != 2 {
		t.Fatalf("install log lines = %d, want 2", lines)
	}
}

func TestSyncManagedArtifactsUpdatesOwnedPriorBytes(t *testing.T) {
	paths := isolatedInstallation(t, false)
	target := filepath.Join(paths.OpenCodeHome, "plugins", "mimir.ts")
	oldData := []byte("old bundled plugin\n")
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, oldData, 0o600); err != nil {
		t.Fatal(err)
	}
	receipt := newInstallReceipt()
	receipt.BundleVersion = "old"
	receipt.Artifacts[target] = installReceiptArtifact{Source: "plugins/opencode/mimir.ts", Hash: hashBytes(oldData)}
	if err := writeJSONAtomic(paths.Receipt, receipt); err != nil {
		t.Fatal(err)
	}

	report, err := syncManagedArtifacts(false, "update")
	if err != nil {
		t.Fatal(err)
	}
	result := resultForPath(t, report, target)
	if result.Status != artifactUpdated {
		t.Fatalf("status = %s, want updated", result.Status)
	}
	want, _ := mimirassets.Bundle.ReadFile("plugins/opencode/mimir.ts")
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatal("owned prior bytes were not updated")
	}
}

func TestSyncManagedArtifactsAdoptsIdenticalAndPreservesConflict(t *testing.T) {
	paths := isolatedInstallation(t, false)
	plugin := filepath.Join(paths.OpenCodeHome, "plugins", "mimir.ts")
	skill := filepath.Join(paths.OpenCodeHome, "skills", "mimir-use", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(plugin), 0o700); err != nil {
		t.Fatal(err)
	}
	pluginData, _ := mimirassets.Bundle.ReadFile("plugins/opencode/mimir.ts")
	if err := os.WriteFile(plugin, pluginData, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(skill), 0o700); err != nil {
		t.Fatal(err)
	}
	conflict := []byte("user-owned skill\n")
	if err := os.WriteFile(skill, conflict, 0o600); err != nil {
		t.Fatal(err)
	}

	report, err := syncManagedArtifacts(false, "refresh")
	if err != nil {
		t.Fatal(err)
	}
	if got := resultForPath(t, report, plugin).Status; got != artifactAdopted {
		t.Fatalf("identical status = %s, want adopted", got)
	}
	if got := resultForPath(t, report, skill).Status; got != artifactConflict {
		t.Fatalf("conflict status = %s, want conflict", got)
	}
	got, _ := os.ReadFile(skill)
	if string(got) != string(conflict) {
		t.Fatal("unowned conflict was changed")
	}
	receipt, err := loadInstallReceipt()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := receipt.Artifacts[plugin]; !ok {
		t.Fatal("identical file was not adopted into receipt")
	}
	if _, ok := receipt.Artifacts[skill]; ok {
		t.Fatal("conflicting file was incorrectly adopted")
	}
}

func TestSyncManagedArtifactsMigratesExactLegacyMimirFile(t *testing.T) {
	paths := isolatedInstallation(t, false)
	target := filepath.Join(paths.OpenCodeHome, "plugins", "mimir.ts")
	legacy := []byte("exact historical Mimir plugin\n")
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	old := append([]string(nil), legacyMimirArtifactHashes["plugins/opencode/mimir.ts"]...)
	legacyMimirArtifactHashes["plugins/opencode/mimir.ts"] = append(old, hashBytes(legacy))
	t.Cleanup(func() { legacyMimirArtifactHashes["plugins/opencode/mimir.ts"] = old })

	report, err := syncManagedArtifacts(false, "update")
	if err != nil {
		t.Fatal(err)
	}
	if got := resultForPath(t, report, target).Status; got != artifactMigrated {
		t.Fatalf("status = %s, want migrated", got)
	}
	want, _ := mimirassets.Bundle.ReadFile("plugins/opencode/mimir.ts")
	if got := mustReadFile(t, target); !bytes.Equal(got, want) {
		t.Fatal("legacy file was not replaced with bundled content")
	}
	receipt, err := loadInstallReceipt()
	if err != nil || receipt.Artifacts[target].Hash != hashBytes(want) {
		t.Fatalf("legacy file was not enrolled: %#v, %v", receipt.Artifacts[target], err)
	}
}

func TestLegacyHashesIncludeV033HermesArtifacts(t *testing.T) {
	cases := map[string]string{
		"plugins/hermes/__init__.py":                           "bc969928e011416ddd38fa81d09d50b6536f2b2212cac3b7880a359e4735dc12",
		"plugins/hermes/plugin.yaml":                           "e3b43f6cdd6d8c5eec368e600083db71735a4bea474cfcb0a6b1b7d03f74f71e",
		"skills/mimir-setup/SKILL.md":                          "815a7bd4683e3ccc629368b039848d82441660cd3254566f8314be0249d4a134",
		"skills/mimir-setup/references/connection-manifest.md": "7073294becca721f70e802fe9241eebb5d17174e14c2c7c865a725a91dedd4c8",
		"skills/mimir-use/SKILL.md":                            "deeaa9eaa5874191dda4efed36b55b42ba4960f726904d4b96457922ed064708",
	}
	for source, hash := range cases {
		if !legacyMimirArtifact(source, hash) {
			t.Errorf("missing v0.3.3 legacy hash for %s", source)
		}
	}
}

func TestUninstallDoesNotDisableUnownedHermesPlugin(t *testing.T) {
	isolatedInstallation(t, true)
	oldRun := runHermesPluginCommand
	commands := 0
	runHermesPluginCommand = func(context.Context, string, ...string) error { commands++; return nil }
	t.Cleanup(func() { runHermesPluginCommand = oldRun })
	if _, err := uninstallManagedInstallation(true); err != nil {
		t.Fatal(err)
	}
	if commands != 0 {
		t.Fatalf("disabled unowned Hermes plugin %d times", commands)
	}
}

func TestSyncManagedArtifactsPreservesModifiedOwnedFile(t *testing.T) {
	paths := isolatedInstallation(t, false)
	if _, err := syncManagedArtifacts(true, "install"); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(paths.OpenCodeHome, "plugins", "mimir.ts")
	modified := []byte("locally modified plugin\n")
	if err := os.WriteFile(target, modified, 0o600); err != nil {
		t.Fatal(err)
	}

	report, err := syncManagedArtifacts(false, "refresh")
	if err != nil {
		t.Fatal(err)
	}
	if got := resultForPath(t, report, target).Status; got != artifactModified {
		t.Fatalf("status = %s, want modified", got)
	}
	got, _ := os.ReadFile(target)
	if string(got) != string(modified) {
		t.Fatal("modified owned file was changed")
	}
	receipt, err := loadInstallReceipt()
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Artifacts[target].Hash == "" {
		t.Fatal("modified artifact ownership was dropped")
	}
	second, err := syncManagedArtifacts(false, "refresh")
	if err != nil {
		t.Fatal(err)
	}
	if got := resultForPath(t, second, target).Status; got != artifactModified {
		t.Fatalf("second status = %s, want modified", got)
	}
}

func TestSyncManagedArtifactsRemovesOnlySafeObsoletePluginAndSkill(t *testing.T) {
	paths := isolatedInstallation(t, false)
	if _, err := syncManagedArtifacts(true, "install"); err != nil {
		t.Fatal(err)
	}
	retiredPlugin := filepath.Join(paths.OpenCodeHome, "plugins", "retired-mimir.ts")
	retiredSkill := filepath.Join(paths.OpenCodeHome, "skills", "mimir-retired", "SKILL.md")
	modifiedSkill := filepath.Join(paths.OpenCodeHome, "skills", "mimir-modified", "SKILL.md")
	missingSkill := filepath.Join(paths.OpenCodeHome, "skills", "mimir-missing", "SKILL.md")
	for path, data := range map[string][]byte{
		retiredPlugin: []byte("retired plugin\n"),
		retiredSkill:  []byte("retired skill\n"),
		modifiedSkill: []byte("modified now\n"),
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	receipt, err := loadInstallReceipt()
	if err != nil {
		t.Fatal(err)
	}
	receipt.Artifacts[retiredPlugin] = installReceiptArtifact{Source: "plugins/opencode/retired-mimir.ts", Hash: hashBytes([]byte("retired plugin\n"))}
	receipt.Artifacts[retiredSkill] = installReceiptArtifact{Source: "skills/mimir-retired/SKILL.md", Hash: hashBytes([]byte("retired skill\n"))}
	receipt.Artifacts[modifiedSkill] = installReceiptArtifact{Source: "skills/mimir-modified/SKILL.md", Hash: hashBytes([]byte("prior bytes\n"))}
	receipt.Artifacts[missingSkill] = installReceiptArtifact{Source: "skills/mimir-missing/SKILL.md", Hash: hashBytes([]byte("missing bytes\n"))}
	if err := writeJSONAtomic(paths.Receipt, receipt); err != nil {
		t.Fatal(err)
	}

	report, err := syncManagedArtifacts(false, "update")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{retiredPlugin, retiredSkill} {
		if got := resultForPath(t, report, path).Status; got != artifactRemoved {
			t.Fatalf("%s status = %s, want removed", path, got)
		}
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("obsolete file remains: %s", path)
		}
	}
	if got := resultForPath(t, report, modifiedSkill).Status; got != artifactModifiedKept {
		t.Fatalf("modified status = %s", got)
	}
	if got := resultForPath(t, report, missingSkill).Status; got != artifactMissingKept {
		t.Fatalf("missing status = %s", got)
	}
	receipt, err = loadInstallReceipt()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := receipt.Artifacts[retiredPlugin]; ok {
		t.Fatal("removed plugin retained receipt ownership")
	}
	if _, ok := receipt.Artifacts[retiredSkill]; ok {
		t.Fatal("removed skill retained receipt ownership")
	}
	if receipt.Artifacts[modifiedSkill].Hash == "" || receipt.Artifacts[missingSkill].Hash == "" {
		t.Fatal("preserved obsolete paths lost receipt ownership")
	}
}

func TestSyncManagedArtifactsRestoresOnlyMissingOwnedFile(t *testing.T) {
	paths := isolatedInstallation(t, false)
	if _, err := syncManagedArtifacts(true, "install"); err != nil {
		t.Fatal(err)
	}
	owned := filepath.Join(paths.OpenCodeHome, "plugins", "mimir.ts")
	if err := os.Remove(owned); err != nil {
		t.Fatal(err)
	}
	unowned := filepath.Join(paths.OpenCodeHome, "skills", "mimir-use", "SKILL.md")
	receipt, err := loadInstallReceipt()
	if err != nil {
		t.Fatal(err)
	}
	delete(receipt.Artifacts, unowned)
	if err := os.Remove(unowned); err != nil {
		t.Fatal(err)
	}
	if err := writeJSONAtomic(paths.Receipt, receipt); err != nil {
		t.Fatal(err)
	}
	report, err := syncManagedArtifacts(false, "refresh")
	if err != nil {
		t.Fatal(err)
	}
	if got := resultForPath(t, report, owned).Status; got != artifactInstalled {
		t.Fatalf("owned status = %s, want installed", got)
	}
	if got := resultForPath(t, report, unowned).Status; got != artifactMissing {
		t.Fatalf("unowned status = %s, want missing", got)
	}
	if _, err := os.Stat(owned); err != nil {
		t.Fatal("owned missing artifact was not restored")
	}
	if _, err := os.Stat(unowned); !os.IsNotExist(err) {
		t.Fatal("unowned missing artifact was installed")
	}
}

func TestInstallReceiptIncludesBinaryLifecycleMetadata(t *testing.T) {
	paths := isolatedInstallation(t, false)
	update := installReceiptUpdate{
		Source: "go-run", Method: "bootstrap-copy",
		CLI: installReceiptCLI{Path: filepath.Join(t.TempDir(), "mimir"), Version: "1.2.3", Commit: "abc123", BuildDate: "2026-07-23", Hash: hashBytes([]byte("binary"))},
	}
	if _, err := syncInstallArtifacts(update); err != nil {
		t.Fatal(err)
	}
	receipt, err := loadInstallReceipt()
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Schema != installReceiptSchema || receipt.InstallationID == "" || receipt.InstalledAt == "" || receipt.UpdatedAt == "" {
		t.Fatalf("incomplete receipt %#v", receipt)
	}
	if receipt.Source != update.Source || receipt.Method != update.Method || receipt.CLI != update.CLI || receipt.BundleVersion == "" || len(receipt.Artifacts) == 0 {
		t.Fatalf("receipt metadata %#v", receipt)
	}
	data, err := os.ReadFile(paths.Log)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{`"operation":"install"`, `"after_version":"1.2.3"`, `"result":"success"`, `"summary":`} {
		if !bytes.Contains(data, []byte(field)) {
			t.Fatalf("install log missing %s: %s", field, data)
		}
	}
}

func TestInstallReceiptSchemaOneMigratesOnSync(t *testing.T) {
	paths := isolatedInstallation(t, false)
	legacy := map[string]any{
		"schema":         1,
		"bundle_version": "old",
		"updated_at":     "2025-01-01T00:00:00Z",
		"artifacts":      map[string]any{},
	}
	if err := writeJSONAtomic(paths.Receipt, legacy); err != nil {
		t.Fatal(err)
	}
	if _, err := syncManagedArtifacts(false, "refresh"); err != nil {
		t.Fatal(err)
	}
	receipt, err := loadInstallReceipt()
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Schema != installReceiptSchema || receipt.InstallationID == "" || receipt.InstalledAt == "" {
		t.Fatalf("legacy receipt was not migrated: %#v", receipt)
	}
}

func TestSyncManagedArtifactsRejectsSymlink(t *testing.T) {
	paths := isolatedInstallation(t, false)
	if runtime.GOOS == "windows" {
		// Windows developer mode or elevation is required to create symlinks.
		target := filepath.Join(paths.OpenCodeHome, "plugins", "mimir.ts")
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			t.Fatal(err)
		}
		real := filepath.Join(t.TempDir(), "real.ts")
		if err := os.WriteFile(real, []byte("keep\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(real, target); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		assertSymlinkRejected(t, target, real)
		return
	}
	target := filepath.Join(paths.OpenCodeHome, "plugins", "mimir.ts")
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		t.Fatal(err)
	}
	real := filepath.Join(t.TempDir(), "real.ts")
	if err := os.WriteFile(real, []byte("keep\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, target); err != nil {
		t.Fatal(err)
	}
	assertSymlinkRejected(t, target, real)
}

func TestInstallReceiptAndLogDoNotContainCredentials(t *testing.T) {
	paths := isolatedInstallation(t, false)
	secret := "super-secret-machine-token-123"
	t.Setenv("MIMIR_TOKEN", secret)
	if _, err := syncManagedArtifacts(true, secret); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{paths.Receipt, paths.Log} {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), secret) {
			t.Fatalf("%s contains credential", path)
		}
	}
}

func TestCheckManagedArtifactsDoesNotWrite(t *testing.T) {
	paths := isolatedInstallation(t, false)
	report, err := checkManagedArtifacts()
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Artifacts) == 0 || report.Artifacts[0].Status != artifactMissing {
		t.Fatalf("unexpected check report: %#v", report.Artifacts)
	}
	for _, path := range []string{paths.Receipt, paths.Log} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("check created %s", path)
		}
	}
}

func TestConnectedRefreshWithoutManagedReceiptDoesNotEnrollOrLog(t *testing.T) {
	paths := isolatedInstallation(t, true)
	report := refreshConnectedLifecycleIntegrations(t.Context(), "setup")
	if !report.OK || report.Integrations.OpenCode.State != "skipped" {
		t.Fatalf("refresh report %#v", report)
	}
	for _, path := range []string{
		paths.Receipt,
		paths.Log,
		filepath.Join(paths.OpenCodeHome, "plugins", "mimir.ts"),
		filepath.Join(paths.HermesHome, "plugins", "mimir", "plugin.yaml"),
	} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("setup-style refresh created %s", path)
		}
	}
}

func TestRefreshManagedInstallationDoesNotTransferCLIToDifferentExecutable(t *testing.T) {
	paths := isolatedInstallation(t, false)
	oldCLI := installReceiptCLI{Path: filepath.Join(t.TempDir(), "old-mimir"), Version: "1.0.0", Commit: "old", BuildDate: "old-date", Hash: hashBytes([]byte("old"))}
	if _, err := syncInstallArtifacts(installReceiptUpdate{Source: "release", Method: "bootstrap-copy", CLI: oldCLI}); err != nil {
		t.Fatal(err)
	}
	before, err := loadInstallReceipt()
	if err != nil {
		t.Fatal(err)
	}
	newExecutable := filepath.Join(t.TempDir(), "mimir")
	newBytes := []byte("new executable")
	if err := os.WriteFile(newExecutable, newBytes, 0o755); err != nil {
		t.Fatal(err)
	}
	oldExecutablePath := executablePath
	oldVersion, oldCommit, oldDate := version, commit, date
	executablePath = func() (string, error) { return newExecutable, nil }
	SetBuildInfo("2.0.0", "new-commit", "new-date")
	t.Cleanup(func() {
		executablePath = oldExecutablePath
		SetBuildInfo(oldVersion, oldCommit, oldDate)
	})
	report, err := refreshManagedInstallation(true, "update")
	if err != nil {
		t.Fatal(err)
	}
	after, err := loadInstallReceipt()
	if err != nil {
		t.Fatal(err)
	}
	if report.BeforeVersion != "1.0.0" || report.AfterVersion != "1.0.0" {
		t.Fatalf("version transition = %q -> %q", report.BeforeVersion, report.AfterVersion)
	}
	if after.CLI != oldCLI {
		t.Fatalf("CLI ownership transferred: %#v", after.CLI)
	}
	if after.InstallationID != before.InstallationID || after.InstalledAt != before.InstalledAt || after.Source != before.Source || after.Method != before.Method {
		t.Fatalf("installation identity changed: before=%#v after=%#v", before, after)
	}
	if lines := jsonLines(t, paths.Log); lines != 2 {
		t.Fatalf("install log lines = %d, want 2", lines)
	}
}

func TestRefreshManagedInstallationDoesNotAdoptReplacedBinaryInPlace(t *testing.T) {
	isolatedInstallation(t, false)
	binary := filepath.Join(t.TempDir(), "mimir")
	oldBytes := []byte("old executable")
	newBytes := []byte("new executable")
	if err := os.WriteFile(binary, oldBytes, 0o755); err != nil {
		t.Fatal(err)
	}
	oldCLI := installReceiptCLI{Path: binary, Version: "1.0.0", Hash: hashBytes(oldBytes)}
	if _, err := syncInstallArtifacts(installReceiptUpdate{Source: "release", Method: "bootstrap-copy", CLI: oldCLI}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binary, newBytes, 0o755); err != nil {
		t.Fatal(err)
	}
	oldExecutablePath := executablePath
	oldVersion, oldCommit, oldDate := version, commit, date
	executablePath = func() (string, error) { return binary, nil }
	SetBuildInfo("2.0.0", "new", "new-date")
	t.Cleanup(func() {
		executablePath = oldExecutablePath
		SetBuildInfo(oldVersion, oldCommit, oldDate)
	})
	if _, err := refreshManagedInstallation(true, "update"); err != nil {
		t.Fatal(err)
	}
	receipt, err := loadInstallReceipt()
	if err != nil {
		t.Fatal(err)
	}
	if receipt.CLI != oldCLI || receipt.Method != "bootstrap-copy" {
		t.Fatalf("in-place receipt %#v", receipt)
	}
}

func TestPathContainsSymlinkChecksAncestorAboveManagedRoot(t *testing.T) {
	realParent := t.TempDir()
	linkParent := filepath.Join(t.TempDir(), "linked-parent")
	if err := os.Symlink(realParent, linkParent); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	root := filepath.Join(linkParent, "managed")
	target := filepath.Join(root, "nested", "file")
	got, err := pathContainsSymlink(root, target)
	if err != nil {
		t.Fatal(err)
	}
	if !got {
		t.Fatal("symlink above the managed root was not detected")
	}
}

func TestPathContainsSymlinkAllowsDarwinSystemAlias(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS filesystem alias")
	}
	target := filepath.Join(os.TempDir(), "mimir", "file")
	got, err := pathContainsSymlink(filesystemRoot(target), target)
	if err != nil {
		t.Fatal(err)
	}
	if got {
		t.Fatalf("macOS system alias was treated as an unsafe symlink: %s", target)
	}
}

func TestUninstallRemovesUnchangedPreservesModifiedAndUpdatesReceiptAndLog(t *testing.T) {
	paths := isolatedInstallation(t, true)
	oldRunHermesPluginCommand := runHermesPluginCommand
	runHermesPluginCommand = func(context.Context, string, ...string) error { return nil }
	t.Cleanup(func() { runHermesPluginCommand = oldRunHermesPluginCommand })
	binary := filepath.Join(t.TempDir(), "mimir")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	binaryData := []byte("managed test binary")
	if runtime.GOOS == "windows" {
		oldLauncher := launchDeferredBinaryRemoval
		launchDeferredBinaryRemoval = func(int, string) error { return nil }
		t.Cleanup(func() { launchDeferredBinaryRemoval = oldLauncher })
	}
	if err := os.WriteFile(binary, binaryData, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := syncInstallArtifacts(installReceiptUpdate{
		Source: "go-run", Method: "bootstrap-copy",
		CLI: installReceiptCLI{Path: binary, Hash: hashBytes(binaryData)},
	}); err != nil {
		t.Fatal(err)
	}
	modifiedPath := filepath.Join(paths.OpenCodeHome, "plugins", "mimir.ts")
	modifiedData := []byte("local plugin changes\n")
	if err := os.WriteFile(modifiedPath, modifiedData, 0o600); err != nil {
		t.Fatal(err)
	}
	missingPath := filepath.Join(paths.OpenCodeHome, "skills", "mimir-use", "SKILL.md")
	if err := os.Remove(missingPath); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(paths.MimirHome, "config")
	token := filepath.Join(paths.MimirHome, "token")
	workerFile := filepath.Join(paths.Worker, "wrangler.jsonc")
	for path, data := range map[string][]byte{config: []byte("url\n"), token: []byte("secret\n"), workerFile: []byte("{}\n")} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	report, err := uninstallManagedInstallation(false)
	if err != nil {
		t.Fatal(err)
	}
	if got := resultForUninstallPath(t, report, modifiedPath).Status; got != artifactModifiedKept {
		t.Fatalf("modified status = %s", got)
	}
	if got := resultForUninstallPath(t, report, missingPath).Status; got != artifactMissingKept {
		t.Fatalf("missing status = %s", got)
	}
	removed := 0
	for _, artifact := range report.Artifacts {
		if artifact.Status != artifactRemoved {
			continue
		}
		removed++
		if _, err := os.Lstat(artifact.Path); !os.IsNotExist(err) {
			t.Fatalf("unchanged artifact still exists: %s", artifact.Path)
		}
	}
	if removed == 0 {
		t.Fatal("no unchanged artifacts were removed")
	}
	for _, dir := range []string{
		filepath.Join(paths.OpenCodeHome, "skills", "mimir-setup"),
		filepath.Join(paths.OpenCodeHome, "skills", "mimir-use"),
		filepath.Join(paths.HermesHome, "skills", "mimir-setup"),
		filepath.Join(paths.HermesHome, "skills", "mimir-use"),
		filepath.Join(paths.HermesHome, "plugins", "mimir"),
	} {
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Fatalf("empty Mimir-specific directory remains: %s", dir)
		}
	}
	for _, dir := range []string{
		filepath.Join(paths.OpenCodeHome, "skills"),
		filepath.Join(paths.OpenCodeHome, "plugins"),
		filepath.Join(paths.HermesHome, "skills"),
		filepath.Join(paths.HermesHome, "plugins"),
	} {
		if info, err := os.Stat(dir); err != nil || !info.IsDir() {
			t.Fatalf("parent harness directory was removed: %s, %v", dir, err)
		}
	}
	if got, err := os.ReadFile(modifiedPath); err != nil || string(got) != string(modifiedData) {
		t.Fatalf("modified file = %q, %v", got, err)
	}
	receipt, err := loadInstallReceipt()
	if err != nil {
		t.Fatal(err)
	}
	if len(receipt.Artifacts) != 2 || receipt.Artifacts[modifiedPath].Hash == "" || receipt.Artifacts[missingPath].Hash == "" {
		t.Fatalf("preserved receipt entries = %#v", receipt.Artifacts)
	}
	for _, path := range []string{config, token, workerFile, paths.Log, paths.Receipt} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("preserved path %s: %v", path, err)
		}
	}
	if runtime.GOOS == "windows" {
		if report.Result != "partial" || report.Binary.Status != "pending-removal" || !strings.Contains(report.Binary.PendingPath, ".uninstall") {
			t.Fatalf("binary report %#v", report.Binary)
		}
		if receipt.CLI.Path != report.Binary.PendingPath || receipt.CLI.Path == binary {
			t.Fatalf("pending CLI receipt = %#v", receipt.CLI)
		}
	} else if report.Binary.Status != "removed" {
		t.Fatalf("binary status = %s", report.Binary.Status)
	}
	logData, err := os.ReadFile(paths.Log)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(logData, []byte(`"operation":"uninstall"`)) || !bytes.Contains(logData, []byte(`"binary":`)) || !bytes.Contains(logData, []byte(`"hermes":`)) || !bytes.Contains(logData, []byte(`"summary":`)) {
		t.Fatalf("uninstall log missing report: %s", logData)
	}
}

func TestWindowsBinaryUninstallLaunchesCleanupForRenamedPath(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "mimir.exe")
	data := []byte("managed binary")
	if err := os.WriteFile(binary, data, 0o755); err != nil {
		t.Fatal(err)
	}
	var launchedPID int
	var launchedPath string
	result := uninstallManagedBinaryForPlatform(installReceiptCLI{Path: binary, Hash: hashBytes(data)}, "bootstrap-copy", false, true, func(pid int, path string) error {
		launchedPID, launchedPath = pid, path
		return nil
	})
	if result.Status != "pending-removal" || !result.DeferredUntilProcessExit || result.PendingPath == "" || launchedPID != os.Getpid() || launchedPath != result.PendingPath {
		t.Fatalf("result = %#v, launch = (%d, %q)", result, launchedPID, launchedPath)
	}
	if _, err := os.Stat(binary); !os.IsNotExist(err) {
		t.Fatalf("original binary remains: %v", err)
	}
	if _, err := os.Stat(result.PendingPath); err != nil {
		t.Fatalf("renamed binary missing: %v", err)
	}
}

func TestWindowsBinaryUninstallReportsCleanupLaunchFailure(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "mimir.exe")
	data := []byte("managed binary")
	if err := os.WriteFile(binary, data, 0o755); err != nil {
		t.Fatal(err)
	}
	result := uninstallManagedBinaryForPlatform(installReceiptCLI{Path: binary, Hash: hashBytes(data)}, "bootstrap-copy", false, true, func(int, string) error {
		return errors.New("launcher unavailable")
	})
	if result.Status != "pending-removal-unscheduled" || result.DeferredUntilProcessExit || !strings.Contains(result.Detail, "preserved") || !strings.Contains(result.Detail, "launcher unavailable") {
		t.Fatalf("result = %#v", result)
	}
	report := uninstallReport{Binary: result}
	if got, _ := uninstallResultSummary(report, false); got != "partial" {
		t.Fatalf("uninstall result = %q, want partial", got)
	}
	if _, err := os.Stat(result.PendingPath); err != nil {
		t.Fatalf("renamed binary was not preserved: %v", err)
	}
}

func TestUninstallPreservesSymlinkedArtifact(t *testing.T) {
	paths := isolatedInstallation(t, false)
	if _, err := syncManagedArtifacts(true, "install"); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(paths.OpenCodeHome, "plugins", "mimir.ts")
	real := filepath.Join(t.TempDir(), "real.ts")
	if err := os.WriteFile(real, []byte("keep\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, target); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	report, err := uninstallManagedInstallation(true)
	if err != nil {
		t.Fatal(err)
	}
	if got := resultForUninstallPath(t, report, target).Status; got != artifactSymlinkKept {
		t.Fatalf("status = %s", got)
	}
	if info, err := os.Lstat(target); err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("symlink was not preserved: %v", err)
	}
	if got, _ := os.ReadFile(real); string(got) != "keep\n" {
		t.Fatal("symlink destination was changed")
	}
	receipt, err := loadInstallReceipt()
	if err != nil || receipt.Artifacts[target].Hash == "" {
		t.Fatalf("symlink ownership was not retained: %#v, %v", receipt.Artifacts, err)
	}
}

func TestUninstallKeepBinaryAndHashMismatch(t *testing.T) {
	for _, test := range []struct {
		name       string
		keep       bool
		modify     bool
		wantStatus string
	}{
		{name: "keep binary", keep: true, wantStatus: "kept"},
		{name: "hash mismatch", modify: true, wantStatus: "modified-preserved"},
	} {
		t.Run(test.name, func(t *testing.T) {
			isolatedInstallation(t, false)
			binary := filepath.Join(t.TempDir(), "mimir")
			original := []byte("managed binary")
			if err := os.WriteFile(binary, original, 0o755); err != nil {
				t.Fatal(err)
			}
			receipt := newInstallReceipt()
			receipt.Method = "bootstrap-copy"
			receipt.CLI = installReceiptCLI{Path: binary, Hash: hashBytes(original)}
			paths, _ := managedInstallationPaths()
			if err := writeJSONAtomic(paths.Receipt, receipt); err != nil {
				t.Fatal(err)
			}
			if test.modify {
				if err := os.WriteFile(binary, []byte("changed binary"), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			report, err := uninstallManagedInstallation(test.keep)
			if err != nil {
				t.Fatal(err)
			}
			if report.Binary.Status != test.wantStatus {
				t.Fatalf("status = %s, want %s", report.Binary.Status, test.wantStatus)
			}
			if _, err := os.Stat(binary); err != nil {
				t.Fatalf("test binary was removed: %v", err)
			}
		})
	}
}

func TestUninstallPreservesPackageManagerBinary(t *testing.T) {
	isolatedInstallation(t, false)
	binary := filepath.Join(t.TempDir(), "scoop", "apps", "mimir", "current", "mimir.exe")
	data := []byte("package managed binary")
	if err := os.MkdirAll(filepath.Dir(binary), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binary, data, 0o755); err != nil {
		t.Fatal(err)
	}
	paths, _ := managedInstallationPaths()
	receipt := newInstallReceipt()
	receipt.Method = "bootstrap-copy"
	receipt.CLI = installReceiptCLI{Path: binary, Hash: hashBytes(data)}
	if err := writeJSONAtomic(paths.Receipt, receipt); err != nil {
		t.Fatal(err)
	}
	report, err := uninstallManagedInstallation(false)
	if err != nil {
		t.Fatal(err)
	}
	if report.Binary.Status != "package-manager-preserved" {
		t.Fatalf("status = %s", report.Binary.Status)
	}
	if _, err := os.Stat(binary); err != nil {
		t.Fatalf("package-manager binary was removed: %v", err)
	}
}

func assertSymlinkRejected(t *testing.T, target, real string) {
	t.Helper()
	report, err := syncManagedArtifacts(true, "install")
	if err != nil {
		t.Fatal(err)
	}
	if got := resultForPath(t, report, target).Status; got != artifactSymlinkRejected {
		t.Fatalf("status = %s, want symlink-rejected", got)
	}
	data, _ := os.ReadFile(real)
	if string(data) != "keep\n" {
		t.Fatal("symlink destination was changed")
	}
}

func isolatedInstallation(t *testing.T, hermes bool) installationPaths {
	t.Helper()
	home := t.TempDir()
	mimirHome := filepath.Join(home, "mimir-state")
	hermesHome := filepath.Join(home, "hermes")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("MIMIR_HOME", mimirHome)
	if hermes {
		t.Setenv("HERMES_HOME", hermesHome)
	} else {
		t.Setenv("HERMES_HOME", "")
		// Avoid detecting a platform-default Hermes installation under temp HOME.
		if runtime.GOOS == "windows" {
			t.Setenv("LOCALAPPDATA", filepath.Join(home, "local-app-data"))
		}
	}
	paths, err := managedInstallationPaths()
	if err != nil {
		t.Fatal(err)
	}
	return paths
}

func resultForPath(t *testing.T, report managedArtifactReport, path string) managedArtifactResult {
	t.Helper()
	for _, result := range report.Artifacts {
		if result.Path == path {
			return result
		}
	}
	t.Fatalf("no result for %s", path)
	return managedArtifactResult{}
}

func resultForUninstallPath(t *testing.T, report uninstallReport, path string) managedArtifactResult {
	t.Helper()
	for _, result := range report.Artifacts {
		if result.Path == path {
			return result
		}
	}
	t.Fatalf("no uninstall result for %s", path)
	return managedArtifactResult{}
}

func jsonLines(t *testing.T, path string) int {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	count := 0
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var value any
		if err := json.Unmarshal(scanner.Bytes(), &value); err != nil {
			t.Fatalf("invalid JSON log line: %v", err)
		}
		count++
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	return count
}

func assertPrivateFile(t *testing.T, path string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("%s permissions = %o, want 600", path, info.Mode().Perm())
	}
}
