package lifecycle

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudboy-jh/mimir/internal/install"
	"github.com/cloudboy-jh/mimir/internal/mimirapi"
)

const testInstallationID = "0123456789abcdef0123456789abcdef"

// stubService returns a Service whose file operations act on manifest only.
func stubService(t *testing.T, paths install.InstallationPaths, manifest *install.Manifest, artifacts ...install.ArtifactResult) Service {
	t.Helper()
	service := New()
	service.Paths = func() (install.InstallationPaths, error) { return paths, nil }
	service.LoadManifest = func() (install.Manifest, error) { return *manifest, nil }
	service.LoadPointer = func() (mimirapi.Pointer, error) { return mimirapi.Pointer{}, errors.New("not connected") }
	service.SaveExecutable = func(string) error { return nil }
	service.InstallFiles = func(_ string, selected []string, _ func() (string, error)) (install.InstallReport, error) {
		if selected != nil {
			manifest.Harnesses = selected
		}
		return install.InstallReport{Binary: install.BinaryReport{Path: "/bin/mimir"}, Artifacts: install.ArtifactReport{Artifacts: artifacts}}, nil
	}
	service.RefreshFiles = func(string) (install.ArtifactReport, error) {
		return install.ArtifactReport{Artifacts: artifacts}, nil
	}
	service.Hermes.RunPluginCommand = func(context.Context, string, ...string) error { return nil }
	service.Hermes.Discover = func() (string, bool, error) { return paths.HermesHome, paths.HermesDetected, nil }
	return service
}

func TestRefreshConnectedSkipsWithoutManagedHarnesses(t *testing.T) {
	service := stubService(t, install.InstallationPaths{}, &install.Manifest{})
	service.RefreshFiles = func(string) (install.ArtifactReport, error) {
		t.Fatal("refresh must not enroll files without an installation")
		return install.ArtifactReport{}, nil
	}
	report := service.RefreshConnected(context.Background(), "login")
	if !report.OK || report.Integrations.OpenCode.State != "skipped" || report.Integrations.OpenCode.Detail != notInstalled {
		t.Fatalf("report = %#v", report)
	}
}

func TestConflictingFilesFailIntegrationsWithoutFailingRefresh(t *testing.T) {
	manifest := &install.Manifest{Harnesses: []string{"pi", "opencode", "cursor"}}
	service := stubService(t, install.InstallationPaths{}, manifest,
		install.ArtifactResult{Harness: "pi", Source: "plugins/pi/mimir.ts", Status: install.ArtifactModified},
		install.ArtifactResult{Harness: "opencode", Source: "plugins/opencode/mimir.ts", Status: install.ArtifactCurrent},
		install.ArtifactResult{Harness: "cursor", Source: "plugins/cursor/hooks.json", Status: install.ArtifactInstalled},
		install.ArtifactResult{Harness: "cursor", Source: install.MCPSource("cursor"), Status: install.ArtifactConflict},
	)
	report := service.RefreshConnected(context.Background(), "login")
	if !report.OK || report.Integrations.Pi.State != "failed" || report.Integrations.OpenCode.State != "staged" {
		t.Fatalf("report = %#v", report)
	}
	if cursor := report.Integrations.Cursor; cursor.State != "staged" || cursor.RestartRequired {
		t.Fatalf("cursor = %#v", cursor)
	}
	if report.Integrations.Codex.State != "skipped" {
		t.Fatalf("codex = %#v", report.Integrations.Codex)
	}
}

func TestInstallRecordsExecutable(t *testing.T) {
	manifest := &install.Manifest{}
	service := stubService(t, install.InstallationPaths{}, manifest,
		install.ArtifactResult{Harness: "opencode", Source: "plugins/opencode/mimir.ts", Status: install.ArtifactInstalled})
	recorded := ""
	service.SaveExecutable = func(path string) error { recorded = path; return nil }
	report, err := service.Install(context.Background(), "", []string{"opencode"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if recorded != "/bin/mimir" || report.Integrations.OpenCode.State != "staged" || !report.ActionRequired || len(report.Warnings) != 0 {
		t.Fatalf("recorded=%q report=%#v", recorded, report)
	}
	service.SaveExecutable = func(string) error { return errors.New("read-only") }
	if report, err = service.Install(context.Background(), "", nil, nil); err != nil || len(report.Warnings) != 1 {
		t.Fatalf("report=%#v err=%v", report, err)
	}
}

func TestInstallStopsWhenAlreadyCancelled(t *testing.T) {
	service := stubService(t, install.InstallationPaths{}, &install.Manifest{})
	service.InstallFiles = func(string, []string, func() (string, error)) (install.InstallReport, error) {
		t.Fatal("cancelled install wrote files")
		return install.InstallReport{}, nil
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := service.Install(ctx, "", []string{"pi"}, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v", err)
	}
}

func TestDisconnectedInstallOnlyEnablesHermesPlugin(t *testing.T) {
	hermesHome := t.TempDir()
	paths := install.InstallationPaths{HermesHome: hermesHome, HermesDetected: true}
	service := stubService(t, paths, &install.Manifest{InstallationID: testInstallationID},
		install.ArtifactResult{Harness: "hermes", Source: "plugins/hermes/plugin.yaml", Status: install.ArtifactInstalled})
	enables := 0
	service.Hermes.RunPluginCommand = func(context.Context, string, ...string) error { enables++; return nil }
	service.Hermes.Request = func(context.Context, mimirapi.Pointer, string, string, any) ([]byte, error) {
		t.Fatal("disconnected install attempted Hermes authorization")
		return nil, nil
	}
	report, err := service.Install(context.Background(), "", []string{"hermes"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if enables != 1 || !strings.Contains(report.Integrations.Hermes.Detail, "connect Mimir") {
		t.Fatalf("enables=%d report=%#v", enables, report)
	}
	if _, err := os.Stat(filepath.Join(hermesHome, ".env")); !os.IsNotExist(err) {
		t.Fatalf("disconnected install created route: %v", err)
	}
}

func TestConnectedInstallConfiguresHermesRoute(t *testing.T) {
	hermesHome := t.TempDir()
	if err := os.WriteFile(filepath.Join(hermesHome, ".env"), []byte("OPENROUTER_API_KEY=hermes-key\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MIMIR_HOME", t.TempDir())
	paths := install.InstallationPaths{HermesHome: hermesHome, HermesDetected: true}
	service := stubService(t, paths, &install.Manifest{InstallationID: testInstallationID},
		install.ArtifactResult{Harness: "hermes", Source: "plugins/hermes/plugin.yaml", Status: install.ArtifactInstalled})
	service.LoadPointer = func() (mimirapi.Pointer, error) {
		return mimirapi.Pointer{URL: "https://mimir.test", Token: "machine"}, nil
	}
	authorized := false
	service.Hermes.Request = func(_ context.Context, pointer mimirapi.Pointer, method, path string, _ any) ([]byte, error) {
		authorized = pointer.Token == "machine" && method == "POST" && path == "/integrations/hermes/authorize"
		return []byte(`{"authorized":true}`), nil
	}
	report, err := service.Install(context.Background(), "", []string{"hermes"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !authorized || report.Integrations.Hermes.Provider != "openrouter" {
		t.Fatalf("authorized=%v report=%#v", authorized, report)
	}
	env, err := os.ReadFile(filepath.Join(hermesHome, ".env"))
	if err != nil || !strings.Contains(string(env), `OPENROUTER_BASE_URL="https://mimir.test/v1/hermes/`+testInstallationID+`"`) {
		t.Fatalf("Hermes env = %q, %v", env, err)
	}
}

func TestInstallRollsBackNewHermesSelectionWhenEnableFails(t *testing.T) {
	paths := install.InstallationPaths{HermesHome: t.TempDir(), HermesDetected: true}
	manifest := &install.Manifest{Harnesses: []string{"opencode"}}
	service := stubService(t, paths, manifest,
		install.ArtifactResult{Harness: "hermes", Source: "plugins/hermes/plugin.yaml", Status: install.ArtifactInstalled})
	events := []string{}
	service.Hermes.RunPluginCommand = func(_ context.Context, _ string, args ...string) error {
		events = append(events, args[0])
		if args[0] == "enable" {
			return errors.New("enable failed")
		}
		return nil
	}
	service.SelectFiles = func(_ string, selected []string) (install.ArtifactReport, error) {
		events = append(events, "rollback:"+strings.Join(selected, ","))
		return install.ArtifactReport{}, nil
	}
	_, err := service.Install(context.Background(), "", []string{"opencode", "hermes"}, nil)
	if err == nil || !strings.Contains(err.Error(), "enable failed") {
		t.Fatalf("error = %v", err)
	}
	if got := strings.Join(events, ","); got != "enable,disable,rollback:opencode" {
		t.Fatalf("ordering = %s", got)
	}
}

func TestInstallKeepsHermesSelectedWhenTeardownFails(t *testing.T) {
	paths := install.InstallationPaths{HermesHome: t.TempDir(), HermesDetected: true}
	service := stubService(t, paths, &install.Manifest{Harnesses: []string{"opencode", "hermes"}})
	service.Hermes.RunPluginCommand = func(context.Context, string, ...string) error { return errors.New("disable failed") }
	service.InstallFiles = func(string, []string, func() (string, error)) (install.InstallReport, error) {
		t.Fatal("files changed after failed Hermes teardown")
		return install.InstallReport{}, nil
	}
	if _, err := service.Install(context.Background(), "", []string{"opencode"}, nil); err == nil || !strings.Contains(err.Error(), "disable failed") {
		t.Fatalf("error = %v", err)
	}
}

func TestUninstallDisablesOwnedHermesBeforeRemovingFiles(t *testing.T) {
	hermesHome := t.TempDir()
	paths := install.InstallationPaths{HermesHome: hermesHome, HermesDetected: true}
	manifest := &install.Manifest{Files: map[string]string{filepath.Join(hermesHome, "plugins", "mimir", "plugin.yaml"): "owned"}}
	service := stubService(t, paths, manifest)
	events := []string{}
	service.Hermes.RunPluginCommand = func(context.Context, string, ...string) error {
		events = append(events, "disable")
		return nil
	}
	service.UninstallFiles = func(bool) (install.UninstallReport, error) {
		events = append(events, "files")
		return install.UninstallReport{}, nil
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
