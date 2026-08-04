package doctor

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cloudboy-jh/mimir/internal/install"
	"github.com/cloudboy-jh/mimir/internal/mimirapi"
)

type requesterFunc func(context.Context, string, string, any) ([]byte, error)

func (f requesterFunc) Request(ctx context.Context, method, path string, body any) ([]byte, error) {
	return f(ctx, method, path, body)
}

func TestRunReportsArtifactsBeforeMissingConnection(t *testing.T) {
	service := New(requesterFunc(func(context.Context, string, string, any) ([]byte, error) { return nil, nil }))
	service.CheckArtifacts = func() (install.ArtifactReport, error) {
		return install.ArtifactReport{Artifacts: []install.ArtifactResult{{Source: "skills/mimir-use/SKILL.md", Path: "missing", Status: install.ArtifactMissing}}}, nil
	}
	service.LoadPointer = func() (mimirapi.Pointer, error) { return mimirapi.Pointer{}, errors.New("not connected") }
	report := service.Run(context.Background())
	if report.OK || len(report.Checks) != 2 || report.Checks[0].Name != "managed-artifact skills/mimir-use/SKILL.md" || report.Checks[1].Name != "connection" {
		t.Fatalf("report %#v", report)
	}
}

func TestRunTUIAddsReadinessChecksWithInjectedLookups(t *testing.T) {
	service := New(requesterFunc(func(context.Context, string, string, any) ([]byte, error) { return nil, nil }))
	service.CheckArtifacts = func() (install.ArtifactReport, error) { return install.ArtifactReport{}, nil }
	service.LoadPointer = func() (mimirapi.Pointer, error) { return mimirapi.Pointer{}, errors.New("not connected") }
	service.LookPath = func(name string) (string, error) {
		if name != "pi" {
			t.Fatalf("LookPath(%q)", name)
		}
		return "/usr/local/bin/pi", nil
	}
	service.LookupEnv = func(name string) (string, bool) {
		return map[string]string{"OPENROUTER_API_KEY": "secret"}[name], name == "OPENROUTER_API_KEY"
	}

	report := service.RunTUI(context.Background())
	if report.OK || len(report.Checks) != 3 {
		t.Fatalf("report %#v", report)
	}
	if check := report.Checks[1]; check.Name != "pi" || check.Status != "ok" || check.Detail != "/usr/local/bin/pi" {
		t.Fatalf("pi check %#v", check)
	}
	if check := report.Checks[2]; check.Name != "provider-credential" || check.Status != "ok" || check.Detail != "OPENROUTER_API_KEY is set" {
		t.Fatalf("provider check %#v", check)
	}
}

func TestRunTUIWarnsForMissingCredentialAndFailsForMissingPi(t *testing.T) {
	service := New(requesterFunc(func(context.Context, string, string, any) ([]byte, error) { return nil, nil }))
	service.CheckArtifacts = func() (install.ArtifactReport, error) { return install.ArtifactReport{}, nil }
	service.LoadPointer = func() (mimirapi.Pointer, error) { return mimirapi.Pointer{}, errors.New("not connected") }
	service.LookPath = func(string) (string, error) { return "", errors.New("missing") }
	service.LookupEnv = func(string) (string, bool) { return "", false }

	report := service.RunTUI(context.Background())
	if len(report.Checks) != 3 {
		t.Fatalf("report %#v", report)
	}
	pi := report.Checks[1]
	if pi.Status != "failed" || pi.Detail == "" || pi.Repair == "" {
		t.Fatalf("pi check %#v", pi)
	}
	provider := report.Checks[2]
	if provider.Status != "warning" || provider.Repair == "" {
		t.Fatalf("provider check %#v", provider)
	}
	for _, name := range providerCredentialEnvVars {
		if !strings.Contains(provider.Detail, name) {
			t.Fatalf("provider detail %q does not document %s", provider.Detail, name)
		}
	}
}

func TestTUIReadinessAllowsStoredPiAuthentication(t *testing.T) {
	service := New(nil)
	service.LookPath = func(string) (string, error) { return "/usr/local/bin/pi", nil }
	service.LookupEnv = func(string) (string, bool) { return "", false }
	report := service.TUIReadiness()
	if !report.OK || len(report.Checks) != 2 || report.Checks[1].Status != "warning" {
		t.Fatalf("report %#v", report)
	}
}

func TestRunDoesNotAddTUIReadinessChecks(t *testing.T) {
	service := New(requesterFunc(func(context.Context, string, string, any) ([]byte, error) { return nil, nil }))
	service.CheckArtifacts = func() (install.ArtifactReport, error) { return install.ArtifactReport{}, nil }
	service.LoadPointer = func() (mimirapi.Pointer, error) { return mimirapi.Pointer{}, errors.New("not connected") }
	service.LookPath = func(string) (string, error) { t.Fatal("ordinary doctor checked PATH"); return "", nil }
	service.LookupEnv = func(string) (string, bool) { t.Fatal("ordinary doctor checked credentials"); return "", false }

	report := service.Run(context.Background())
	if len(report.Checks) != 1 || report.Checks[0].Name != "connection" {
		t.Fatalf("report %#v", report)
	}
}

func TestStructuredReportGroupsOperationalState(t *testing.T) {
	report := Report{OK: false, Checks: []Check{
		{Name: "managed-artifact plugins/opencode/mimir.ts", Status: "failed", Detail: "outdated", Repair: "mimir update"},
		{Name: "opencode.plugin-load", Status: "failed", Detail: "restart required", Repair: "restart OpenCode"},
		{Name: "claude-code.plugin-load", Status: "failed", Detail: "staged", Repair: "run /reload-plugins"},
		{Name: "codex.plugin-load", Status: "failed", Detail: "staged", Repair: "restart Codex"},
		{Name: "cursor.plugin-load", Status: "failed", Detail: "staged", Repair: "open a session"},
		{Name: "worker.bundle", Status: "ok", Detail: "current"},
		{Name: "connection", Status: "ok", Detail: "connected"},
	}}
	structured := report.Structured()
	if structured.OK || len(structured.Installed["plugins/opencode/mimir.ts"]) != 1 || structured.Installed["plugins/opencode/mimir.ts"][0].Detail != "outdated" {
		t.Fatalf("installed = %#v", structured.Installed)
	}
	if structured.Active["opencode.plugin-load"].Repair != "restart OpenCode" {
		t.Fatalf("active = %#v", structured.Active)
	}
	for _, name := range []string{"claude-code.plugin-load", "codex.plugin-load", "cursor.plugin-load"} {
		if _, ok := structured.Active[name]; !ok {
			t.Fatalf("%s not grouped as active: %#v", name, structured)
		}
	}
	if structured.Deployed["worker.bundle"].Status != "ok" || structured.Connection["connection"].Status != "ok" {
		t.Fatalf("structured = %#v", structured)
	}
	if len(structured.Diagnostics) != 0 {
		t.Fatalf("diagnostics = %#v", structured.Diagnostics)
	}
}

func TestValidateWorkerIdentityRejectsStaleWorker(t *testing.T) {
	if err := ValidateWorkerIdentity([]byte(`{"sessions":0,"log":0}`)); err == nil {
		t.Fatal("legacy Worker was accepted")
	}
	if err := ValidateWorkerIdentity([]byte(`{"service":"mimir","api_version":1,"capabilities":["session_events"]}`)); err == nil {
		t.Fatal("Worker missing required capabilities was accepted")
	}
}

func TestHarnessLoadChecksCompareActivePluginHashes(t *testing.T) {
	service := New(requesterFunc(func(_ context.Context, method, path string, _ any) ([]byte, error) {
		if method != "GET" || path != "/integrations/harness-loads" {
			t.Fatalf("request %s %s", method, path)
		}
		return []byte(`{"loads":[
			{"harness":"opencode","artifact_sha256":"opencode-current","installation_id":"install-1","reported_at":"2026-07-26T10:00:00Z"},
			{"harness":"hermes","artifact_sha256":"hermes-old","installation_id":"install-1","reported_at":"2026-07-26T10:00:00Z"},
			{"harness":"hermes","artifact_sha256":"other-install","installation_id":"install-2","reported_at":"2026-07-26T11:00:00Z"}
		]}`), nil
	}))
	service.LoadReceipt = func() (install.Receipt, error) { return install.Receipt{InstallationID: "install-1"}, nil }
	artifacts := install.ArtifactReport{Artifacts: []install.ArtifactResult{
		{Source: "plugins/opencode/mimir.ts", Status: install.ArtifactCurrent, BundleHash: "opencode-current"},
		{Source: "plugins/hermes/__init__.py", Status: install.ArtifactCurrent, BundleHash: "hermes-current"},
	}}
	var checks []Check
	service.addHarnessLoadChecks(context.Background(), artifacts, func(name, status, detail, repair string) {
		checks = append(checks, Check{Name: name, Status: status, Detail: detail, Repair: repair})
	})
	if len(checks) != 2 {
		t.Fatalf("checks %#v", checks)
	}
	if checks[0].Name != "opencode.plugin-load" || checks[0].Status != "ok" || !strings.Contains(checks[0].Detail, "installed, active, and current") {
		t.Fatalf("OpenCode check %#v", checks[0])
	}
	if checks[1].Name != "hermes.plugin-load" || checks[1].Status != "failed" || checks[1].Repair != "restart Hermes" || !strings.Contains(checks[1].Detail, "active integration") {
		t.Fatalf("Hermes check %#v", checks[1])
	}
}

func TestHarnessLoadChecksTreatLegacyEndpointAsUnknown(t *testing.T) {
	service := New(requesterFunc(func(context.Context, string, string, any) ([]byte, error) {
		return nil, &mimirapi.Error{StatusCode: 404, Status: "404 Not Found"}
	}))
	service.LoadReceipt = func() (install.Receipt, error) { return install.Receipt{InstallationID: "install-1"}, nil }
	artifacts := install.ArtifactReport{Artifacts: []install.ArtifactResult{{
		Source: "plugins/opencode/mimir.ts", Status: install.ArtifactCurrent, BundleHash: "current",
	}}}
	var checks []Check
	service.addHarnessLoadChecks(context.Background(), artifacts, func(name, status, detail, repair string) {
		checks = append(checks, Check{Name: name, Status: status, Detail: detail, Repair: repair})
	})
	if len(checks) != 1 || checks[0].Status != "skipped" || !strings.Contains(checks[0].Detail, "active version unknown") {
		t.Fatalf("checks %#v", checks)
	}
}

func TestHarnessLoadChecksRemainStagedWhenNoLoadReported(t *testing.T) {
	service := New(requesterFunc(func(context.Context, string, string, any) ([]byte, error) {
		return []byte(`{"loads":[]}`), nil
	}))
	service.LoadReceipt = func() (install.Receipt, error) { return install.Receipt{InstallationID: "install-1"}, nil }
	artifacts := install.ArtifactReport{Artifacts: []install.ArtifactResult{{
		Source: "plugins/hermes/__init__.py", Status: install.ArtifactCurrent, BundleHash: "current",
	}}}
	var checks []Check
	service.addHarnessLoadChecks(context.Background(), artifacts, func(name, status, detail, repair string) {
		checks = append(checks, Check{Name: name, Status: status, Detail: detail, Repair: repair})
	})
	if len(checks) != 1 || checks[0].Status != "failed" || checks[0].Repair != "restart Hermes" || !strings.Contains(checks[0].Detail, "staged") {
		t.Fatalf("checks %#v", checks)
	}
}

func TestCursorLoadCheckUsesHotReloadAction(t *testing.T) {
	service := New(requesterFunc(func(context.Context, string, string, any) ([]byte, error) {
		return []byte(`{"loads":[]}`), nil
	}))
	service.LoadReceipt = func() (install.Receipt, error) { return install.Receipt{InstallationID: "install-1"}, nil }
	artifacts := install.ArtifactReport{Artifacts: []install.ArtifactResult{{Source: "plugins/cursor/hooks.json", Status: install.ArtifactCurrent, BundleHash: "current"}}}
	var checks []Check
	service.addHarnessLoadChecks(context.Background(), artifacts, func(name, status, detail, repair string) {
		checks = append(checks, Check{Name: name, Status: status, Detail: detail, Repair: repair})
	})
	if len(checks) != 1 || strings.Contains(strings.ToLower(checks[0].Detail+checks[0].Repair), "restart") || !strings.Contains(checks[0].Repair, "reloads hooks.json") {
		t.Fatalf("checks %#v", checks)
	}
}
