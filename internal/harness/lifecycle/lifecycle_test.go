package lifecycle

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudboy-jh/mimir/internal/harness"
	"github.com/cloudboy-jh/mimir/internal/harness/hermes"
	"github.com/cloudboy-jh/mimir/internal/install"
	"github.com/cloudboy-jh/mimir/internal/mimirapi"
)

func TestRefreshConnectedDoesNotEnrollWithoutReceipt(t *testing.T) {
	service := New()
	service.RefreshPreviouslyManaged = func(operation string) (install.ArtifactReport, error) {
		if operation != "login" {
			t.Fatalf("operation = %q", operation)
		}
		return install.ArtifactReport{Operation: operation}, nil
	}
	service.HasManagedReceipt = func() (bool, error) { return false, nil }
	service.LoadPointer = func() (mimirapi.Pointer, error) {
		t.Fatal("connection should not be loaded without an installation receipt")
		return mimirapi.Pointer{}, nil
	}
	report := service.RefreshConnected(context.Background(), "login")
	if !report.OK || report.Integrations.OpenCode.State != "skipped" || !strings.Contains(report.Integrations.OpenCode.Detail, "do not enroll") {
		t.Fatalf("report = %#v", report)
	}
}

func TestRefreshUsesUpdateArtifactSync(t *testing.T) {
	service := New()
	service.RefreshArtifacts = func(operation string) (install.ArtifactReport, error) {
		if operation != "update" {
			t.Fatalf("operation=%q", operation)
		}
		return install.ArtifactReport{Operation: operation}, nil
	}
	service.LoadPointer = func() (mimirapi.Pointer, error) { return mimirapi.Pointer{}, errors.New("not connected") }
	report := service.Refresh(context.Background(), "update")
	if !report.OK || report.Integrations.Hermes.State != "skipped" || report.Integrations.Hermes.Detail != "Mimir is not connected" {
		t.Fatalf("report = %#v", report)
	}
}

func TestHookArtifactConflictIsPreservedWithoutFailingRefresh(t *testing.T) {
	root := t.TempDir()
	service := New()
	service.Paths = func() (install.InstallationPaths, error) {
		return install.InstallationPaths{OpenCodeHome: root, ClaudeCodeHome: root, CodexHome: root, CursorHome: root}, nil
	}
	service.Hermes = hermes.New()
	service.LoadReceipt = func() (install.Receipt, error) {
		return install.Receipt{Harnesses: []string{"opencode", "claude-code", "codex", "cursor"}}, nil
	}
	service.Hermes.Discover = func() (string, bool, error) { return "", false, nil }
	artifacts := install.ArtifactReport{Artifacts: []install.ArtifactResult{
		{Path: filepath.Join(root, "plugins", "mimir.ts"), Source: "plugins/opencode/mimir.ts", Status: install.ArtifactCurrent},
		{Path: filepath.Join(root, "claude-hooks.json"), Source: "plugins/claude-code/hooks/hooks.json", Status: install.ArtifactConflict},
		{Path: filepath.Join(root, "codex-hooks.json"), Source: "plugins/codex/hooks.json", Status: install.ArtifactModified},
		{Path: filepath.Join(root, "cursor-hooks.json"), Source: "plugins/cursor/hooks.json", Status: install.ArtifactConflict},
	}}
	report, err := service.InstallCurrent(context.Background(), mimirapi.Pointer{URL: "https://mimir.test"}, artifacts)
	if err != nil {
		t.Fatal(err)
	}
	for label, state := range map[string]harness.IntegrationState{"Claude Code": report.ClaudeCode, "Codex": report.Codex, "Cursor": report.Cursor} {
		if state.State != "preserved" || !strings.Contains(state.Detail, "user-owned or modified") {
			t.Fatalf("%s state = %#v", label, state)
		}
	}
}

func TestInstallCurrentReportsManagedPiAndOpenCodeCapturePlugins(t *testing.T) {
	root := t.TempDir()
	service := New()
	service.Paths = func() (install.InstallationPaths, error) {
		return install.InstallationPaths{PiHome: root, OpenCodeHome: root}, nil
	}
	service.Hermes = hermes.New()
	service.Hermes.Discover = func() (string, bool, error) { return "", false, nil }
	service.LoadReceipt = func() (install.Receipt, error) { return install.Receipt{}, errors.New("not installed") }
	artifacts := install.ArtifactReport{Artifacts: []install.ArtifactResult{
		{Path: filepath.Join(root, "extensions", "mimir.ts"), Source: "plugins/pi/mimir.ts", Status: install.ArtifactCurrent},
		{Path: filepath.Join(root, "plugins", "mimir.ts"), Source: "plugins/opencode/mimir.ts", Status: install.ArtifactCurrent},
	}}
	report, err := service.InstallCurrent(context.Background(), mimirapi.Pointer{URL: "https://mimir.test", Token: "machine"}, artifacts)
	if err != nil {
		t.Fatal(err)
	}
	if report.Pi.State != "staged" || report.Pi.Provider != "openrouter" || report.Pi.Scope != "all-providers" || !report.Pi.RestartRequired {
		t.Fatalf("Pi state %#v", report.Pi)
	}
	if report.OpenCode.State != "staged" || report.OpenCode.Scope != "capture" || !report.OpenCode.RestartRequired || !strings.Contains(report.OpenCode.Detail, "unverified") {
		t.Fatalf("OpenCode state %#v", report.OpenCode)
	}
	if report.OhMyPi.State != "skipped" || !strings.Contains(report.OhMyPi.Detail, "unavailable") {
		t.Fatalf("Oh My Pi state %#v", report.OhMyPi)
	}
}

func TestArtifactsReadyRejectsConflicts(t *testing.T) {
	report := install.ArtifactReport{Artifacts: []install.ArtifactResult{
		{Source: "plugins/hermes/__init__.py", Status: install.ArtifactCurrent},
		{Source: "plugins/hermes/plugin.yaml", Status: install.ArtifactConflict},
	}}
	if install.ArtifactsReady(report, "", hermes.ArtifactSourcePrefixes()...) {
		t.Fatal("conflicting Hermes plugin was considered ready")
	}
}

func TestInstallMaterializesBeforeReadinessAndProviderConfiguration(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "mimir")
	data := []byte("binary")
	if err := os.WriteFile(binary, data, 0o755); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(data)
	root, hermesHome := t.TempDir(), t.TempDir()
	events := []string{}
	service := New()
	service.LoadPointer = func() (mimirapi.Pointer, error) { return mimirapi.Pointer{}, errors.New("not connected") }
	service.InstallFiles = func(string, func() (string, error)) (install.InstallReport, error) {
		events = append(events, "files")
		return install.InstallReport{Artifacts: install.ArtifactReport{Artifacts: []install.ArtifactResult{
			{Path: filepath.Join(root, "plugins", "mimir.ts"), Source: "plugins/opencode/mimir.ts", Status: install.ArtifactInstalled},
			{Path: filepath.Join(hermesHome, "plugins", "mimir", "plugin.yaml"), Source: "plugins/hermes/plugin.yaml", Status: install.ArtifactInstalled},
		}}}, nil
	}
	service.Paths = func() (install.InstallationPaths, error) {
		return install.InstallationPaths{OpenCodeHome: root, HermesHome: hermesHome, HermesDetected: true}, nil
	}
	service.LoadReceipt = func() (install.Receipt, error) {
		return testReceipt(t, binary, hex.EncodeToString(hash[:]), nil), nil
	}
	service.Hermes.RunPluginCommand = func(context.Context, string, ...string) error {
		events = append(events, "hermes")
		return nil
	}
	report, err := service.Install(context.Background(), "", func() (string, error) { return binary, nil })
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(events, ","); got != "files,hermes" {
		t.Fatalf("ordering = %s", got)
	}
	if !report.OpenCodeReady || !report.HermesReady || !report.ActionRequired || report.OpenCode.State != "staged" || report.Hermes.State != "staged" {
		t.Fatalf("report = %#v", report)
	}
}

func TestInstallStopsBeforeMechanicalCommitWhenAlreadyCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	service := New()
	calls := 0
	service.InstallFiles = func(string, func() (string, error)) (install.InstallReport, error) {
		calls++
		return install.InstallReport{}, nil
	}
	_, err := service.Install(ctx, "", func() (string, error) { return "", nil })
	if !errors.Is(err, context.Canceled) || calls != 0 {
		t.Fatalf("error=%v install calls=%d", err, calls)
	}
}

func TestInstallFinishesIntegrationReconciliationAfterMechanicalCommit(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	root, hermesHome := t.TempDir(), t.TempDir()
	service := New()
	service.LoadReceipt = func() (install.Receipt, error) { return testReceipt(t, "", "", nil), nil }
	service.InstallFiles = func(string, func() (string, error)) (install.InstallReport, error) {
		cancel()
		return install.InstallReport{Artifacts: install.ArtifactReport{Artifacts: []install.ArtifactResult{
			{Path: filepath.Join(root, "plugins", "mimir.ts"), Source: "plugins/opencode/mimir.ts", Status: install.ArtifactInstalled},
			{Path: filepath.Join(hermesHome, "plugins", "mimir", "plugin.yaml"), Source: "plugins/hermes/plugin.yaml", Status: install.ArtifactInstalled},
		}}}, nil
	}
	service.Paths = func() (install.InstallationPaths, error) {
		return install.InstallationPaths{OpenCodeHome: root, HermesHome: hermesHome, HermesDetected: true}, nil
	}
	service.LoadPointer = func() (mimirapi.Pointer, error) { return mimirapi.Pointer{}, errors.New("not connected") }
	called := false
	service.Hermes.RunPluginCommand = func(callCtx context.Context, _ string, _ ...string) error {
		called = true
		if err := callCtx.Err(); err != nil {
			t.Fatalf("post-commit context was cancelled: %v", err)
		}
		return nil
	}
	if _, err := service.Install(ctx, "", func() (string, error) { return "", nil }); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("post-commit integration reconciliation did not run")
	}
}

func TestHookArtifactStateIsStagedUntilLoadAndCursorDoesNotRequireRestart(t *testing.T) {
	root := t.TempDir()
	artifacts := install.ArtifactReport{Artifacts: []install.ArtifactResult{{Path: filepath.Join(root, "hooks.json"), Source: "plugins/cursor/hooks.json", Status: install.ArtifactCurrent}}}
	state := hookArtifactState(artifacts, root, "plugins/cursor/", "Cursor")
	if state.State != "staged" || state.RestartRequired || !strings.Contains(state.Detail, "unverified") || !strings.Contains(state.Detail, "reloads") {
		t.Fatalf("state = %#v", state)
	}
}

func TestConnectedInstallRepairsHermesThroughLifecycle(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "mimir")
	data := []byte("binary")
	if err := os.WriteFile(binary, data, 0o755); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(data)
	root, hermesHome := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(hermesHome, ".env"), []byte("OPENROUTER_API_KEY=hermes-key\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	service := New()
	service.InstallFiles = func(string, func() (string, error)) (install.InstallReport, error) {
		return install.InstallReport{Artifacts: install.ArtifactReport{Artifacts: []install.ArtifactResult{
			{Path: filepath.Join(root, "plugins", "mimir.ts"), Source: "plugins/opencode/mimir.ts", Status: install.ArtifactInstalled},
			{Path: filepath.Join(hermesHome, "plugins", "mimir", "plugin.yaml"), Source: "plugins/hermes/plugin.yaml", Status: install.ArtifactInstalled},
		}}}, nil
	}
	service.Paths = func() (install.InstallationPaths, error) {
		return install.InstallationPaths{OpenCodeHome: root, HermesHome: hermesHome, HermesDetected: true}, nil
	}
	service.LoadPointer = func() (mimirapi.Pointer, error) {
		return mimirapi.Pointer{URL: "https://mimir.test", Token: "machine"}, nil
	}
	service.LoadReceipt = func() (install.Receipt, error) {
		return testReceipt(t, binary, hex.EncodeToString(hash[:]), nil), nil
	}
	service.Hermes.Discover = func() (string, bool, error) { return hermesHome, true, nil }
	enabled, authorized := false, false
	service.Hermes.RunPluginCommand = func(context.Context, string, ...string) error {
		enabled = true
		return nil
	}
	service.Hermes.Request = func(_ context.Context, pointer mimirapi.Pointer, method, path string, body any) ([]byte, error) {
		if pointer.Token != "machine" || method != "POST" || path != "/integrations/hermes/authorize" {
			t.Fatalf("authorization request = %#v %s %s", pointer, method, path)
		}
		authorized = true
		return []byte(`{"authorized":true}`), nil
	}
	report, err := service.Install(context.Background(), "", func() (string, error) { return binary, nil })
	if err != nil {
		t.Fatal(err)
	}
	if !enabled || !authorized || report.Hermes.Provider != "openrouter" || !strings.Contains(report.Hermes.Detail, "OpenRouter proxy") {
		t.Fatalf("enabled=%v authorized=%v report=%#v", enabled, authorized, report)
	}
	env, err := os.ReadFile(filepath.Join(hermesHome, ".env"))
	if err != nil || !strings.Contains(string(env), `OPENROUTER_BASE_URL="https://mimir.test/v1/hermes/0123456789abcdef0123456789abcdef"`) {
		t.Fatalf("Hermes env = %q, %v", env, err)
	}
}

func TestDisconnectedInstallOnlyEnablesSafeHermesPlugin(t *testing.T) {
	hermesHome := t.TempDir()
	service := New()
	service.LoadReceipt = func() (install.Receipt, error) { return testReceipt(t, "", "", nil), nil }
	service.InstallFiles = func(string, func() (string, error)) (install.InstallReport, error) {
		return install.InstallReport{Artifacts: install.ArtifactReport{Artifacts: []install.ArtifactResult{{
			Path: filepath.Join(hermesHome, "plugins", "mimir", "plugin.yaml"), Source: "plugins/hermes/plugin.yaml", Status: install.ArtifactInstalled,
		}}}}, nil
	}
	service.Paths = func() (install.InstallationPaths, error) {
		return install.InstallationPaths{HermesHome: hermesHome, HermesDetected: true}, nil
	}
	service.LoadPointer = func() (mimirapi.Pointer, error) { return mimirapi.Pointer{}, errors.New("not connected") }
	enables := 0
	service.Hermes.RunPluginCommand = func(context.Context, string, ...string) error { enables++; return nil }
	service.Hermes.Request = func(context.Context, mimirapi.Pointer, string, string, any) ([]byte, error) {
		t.Fatal("disconnected install attempted Hermes authorization")
		return nil, nil
	}
	report, err := service.Install(context.Background(), "", func() (string, error) { return "", nil })
	if err != nil {
		t.Fatal(err)
	}
	if enables != 1 || !strings.Contains(report.Hermes.Detail, "connect Mimir") {
		t.Fatalf("enables=%d report=%#v", enables, report)
	}
	if _, err := os.Stat(filepath.Join(hermesHome, ".env")); !os.IsNotExist(err) {
		t.Fatalf("disconnected install created route: %v", err)
	}
}

func TestInstallDoesNotConfigureProviderUntilArtifactsAreReady(t *testing.T) {
	root, hermesHome := t.TempDir(), t.TempDir()
	providerCalls := 0
	service := New()
	service.LoadReceipt = func() (install.Receipt, error) { return testReceipt(t, "", "", nil), nil }
	service.InstallFiles = func(string, func() (string, error)) (install.InstallReport, error) {
		return install.InstallReport{Artifacts: install.ArtifactReport{Artifacts: []install.ArtifactResult{
			{Path: filepath.Join(root, "plugins", "mimir.ts"), Source: "plugins/opencode/mimir.ts", Status: install.ArtifactConflict},
			{Path: filepath.Join(hermesHome, "plugins", "mimir", "plugin.yaml"), Source: "plugins/hermes/plugin.yaml", Status: install.ArtifactConflict},
		}}}, nil
	}
	service.Paths = func() (install.InstallationPaths, error) {
		return install.InstallationPaths{OpenCodeHome: root, HermesHome: hermesHome, HermesDetected: true}, nil
	}
	service.Hermes.RunPluginCommand = func(context.Context, string, ...string) error { providerCalls++; return nil }
	report, err := service.Install(context.Background(), "", func() (string, error) { return "", nil })
	if err != nil {
		t.Fatal(err)
	}
	if providerCalls != 0 || !report.ActionRequired || report.OpenCodeReady || report.HermesReady {
		t.Fatalf("provider calls=%d report=%#v", providerCalls, report)
	}
}

func TestInstallRollsBackNewHermesSelectionWhenEnableFails(t *testing.T) {
	hermesHome := t.TempDir()
	events := []string{}
	service := New()
	receipt := testReceipt(t, "", "", nil)
	receipt.Harnesses = []string{"opencode"}
	service.LoadReceipt = func() (install.Receipt, error) { return receipt, nil }
	service.InstallHarnessFiles = func(string, []string, func() (string, error)) (install.InstallReport, error) {
		events = append(events, "files")
		return install.InstallReport{Artifacts: install.ArtifactReport{Artifacts: []install.ArtifactResult{{
			Path: filepath.Join(hermesHome, "plugins", "mimir", "plugin.yaml"), Source: "plugins/hermes/plugin.yaml", Status: install.ArtifactInstalled,
		}}}}, nil
	}
	service.Paths = func() (install.InstallationPaths, error) {
		return install.InstallationPaths{HermesHome: hermesHome, HermesDetected: true}, nil
	}
	service.LoadPointer = func() (mimirapi.Pointer, error) { return mimirapi.Pointer{}, errors.New("not connected") }
	service.Hermes.RunPluginCommand = func(_ context.Context, _ string, args ...string) error {
		events = append(events, args[0])
		if args[0] == "enable" {
			return errors.New("enable failed")
		}
		return nil
	}
	service.Hermes.Discover = func() (string, bool, error) { return hermesHome, true, nil }
	service.RefreshSelected = func(_ string, selected []string) (install.ArtifactReport, error) {
		events = append(events, "rollback:"+strings.Join(selected, ","))
		return install.ArtifactReport{}, nil
	}
	_, err := service.InstallSelected(context.Background(), "", []string{"opencode", "hermes"}, func() (string, error) { return "", nil })
	if err == nil || !strings.Contains(err.Error(), "enable failed") {
		t.Fatalf("error = %v", err)
	}
	if got := strings.Join(events, ","); got != "files,enable,disable,rollback:opencode" {
		t.Fatalf("ordering = %s", got)
	}
}

func TestInstallDoesNotCommitHermesDeselectionWhenTeardownFails(t *testing.T) {
	events := []string{}
	service := New()
	receipt := testReceipt(t, "", "", nil)
	receipt.Harnesses = []string{"opencode", "hermes"}
	service.LoadReceipt = func() (install.Receipt, error) { return receipt, nil }
	service.Paths = func() (install.InstallationPaths, error) {
		return install.InstallationPaths{HermesHome: t.TempDir(), HermesDetected: true}, nil
	}
	service.Hermes.RunPluginCommand = func(context.Context, string, ...string) error {
		events = append(events, "disable")
		return errors.New("disable failed")
	}
	service.InstallHarnessFiles = func(string, []string, func() (string, error)) (install.InstallReport, error) {
		events = append(events, "files")
		return install.InstallReport{}, nil
	}
	_, err := service.InstallSelected(context.Background(), "", []string{"opencode"}, func() (string, error) { return "", nil })
	if err == nil || !strings.Contains(err.Error(), "disable failed") {
		t.Fatalf("error = %v", err)
	}
	if got := strings.Join(events, ","); got != "disable" {
		t.Fatalf("ordering = %s", got)
	}
}

func TestUninstallDisablesOwnedHermesBeforeRemovingFiles(t *testing.T) {
	hermesHome := t.TempDir()
	events := []string{}
	service := New()
	service.Paths = func() (install.InstallationPaths, error) {
		return install.InstallationPaths{HermesHome: hermesHome, HermesDetected: true}, nil
	}
	service.LoadReceipt = func() (install.Receipt, error) {
		return testReceipt(t, "", "", map[string]string{"plugin": "plugins/hermes/plugin.yaml"}), nil
	}
	service.Hermes.RunPluginCommand = func(context.Context, string, ...string) error {
		events = append(events, "disable")
		return nil
	}
	service.UninstallFiles = func(bool) (install.UninstallReport, error) {
		events = append(events, "files")
		return install.UninstallReport{Operation: "uninstall", Result: "success"}, nil
	}
	service.Hermes.Discover = func() (string, bool, error) {
		events = append(events, "remove-config")
		return hermesHome, true, nil
	}
	if _, err := service.Uninstall(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(events, ","); got != "disable,files,remove-config" {
		t.Fatalf("ordering = %s", got)
	}
}

func testReceipt(t *testing.T, path, hash string, artifacts map[string]string) install.Receipt {
	t.Helper()
	owned := map[string]map[string]string{}
	for target, source := range artifacts {
		owned[target] = map[string]string{"source": source, "sha256": "owned"}
	}
	data, err := json.Marshal(map[string]any{
		"schema":          2,
		"installation_id": "0123456789abcdef0123456789abcdef",
		"cli":             map[string]string{"path": path, "sha256": hash},
		"artifacts":       owned,
	})
	if err != nil {
		t.Fatal(err)
	}
	var receipt install.Receipt
	if err := json.Unmarshal(data, &receipt); err != nil {
		t.Fatal(err)
	}
	return receipt
}
