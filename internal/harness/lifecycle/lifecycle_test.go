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

func TestRefreshUsesNonEnrollingArtifactSync(t *testing.T) {
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

func TestInstallCurrentReportsManagedOpenCodeCapturePlugin(t *testing.T) {
	root := t.TempDir()
	service := New()
	service.Paths = func() (install.InstallationPaths, error) { return install.InstallationPaths{OpenCodeHome: root}, nil }
	service.Hermes = hermes.New()
	service.Hermes.Discover = func() (string, bool, error) { return "", false, nil }
	artifacts := install.ArtifactReport{Artifacts: []install.ArtifactResult{{
		Path: filepath.Join(root, "plugins", "mimir.ts"), Source: "plugins/opencode/mimir.ts", Status: install.ArtifactCurrent,
	}}}
	report, err := service.InstallCurrent(context.Background(), mimirapi.Pointer{URL: "https://mimir.test", Token: "machine"}, artifacts)
	if err != nil {
		t.Fatal(err)
	}
	if report.OpenCode.State != "installed" || report.OpenCode.Scope != "capture" || !report.OpenCode.RestartRequired || !strings.Contains(report.OpenCode.Detail, "capture plugin") {
		t.Fatalf("OpenCode state %#v", report.OpenCode)
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
	if !report.OpenCodeReady || !report.HermesReady || report.ActionRequired {
		t.Fatalf("report = %#v", report)
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
	if err != nil || !strings.Contains(string(env), `OPENROUTER_BASE_URL="https://mimir.test/v1/hermes"`) {
		t.Fatalf("Hermes env = %q, %v", env, err)
	}
}

func TestDisconnectedInstallOnlyEnablesSafeHermesPlugin(t *testing.T) {
	hermesHome := t.TempDir()
	service := New()
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
		"schema":    2,
		"cli":       map[string]string{"path": path, "sha256": hash},
		"artifacts": owned,
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
