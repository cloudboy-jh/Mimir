package mimircli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIntegrationSummaryExplainsHermesScope(t *testing.T) {
	report := harnessIntegrationReport{
		Hermes: harnessIntegrationState{State: "installed", Provider: "openrouter", Scope: "all-providers", RestartRequired: true},
	}
	summary := integrationSummary(report)
	for _, want := range []string{"Hermes capture installed", "restart Hermes", "OpenRouter proxy plus direct providers"} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary missing %q: %s", want, summary)
		}
	}
}

func TestIntegrationSummarySkipsAbsentHermes(t *testing.T) {
	if summary := integrationSummary(harnessIntegrationReport{Hermes: harnessIntegrationState{State: "skipped"}}); summary != "" {
		t.Fatalf("unexpected summary %q", summary)
	}
}

func TestHarnessRefreshConfiguresOpenCodeThroughSupportedCLI(t *testing.T) {
	isolatedInstallation(t, false)
	binary := filepath.Join(t.TempDir(), "mimir")
	data := []byte("binary")
	if err := os.WriteFile(binary, data, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := syncInstallArtifacts(installReceiptUpdate{Source: "test", Method: "bootstrap-copy", CLI: installReceiptCLI{Path: binary, Hash: hashBytes(data)}}); err != nil {
		t.Fatal(err)
	}
	oldFind := findOpenCode
	findOpenCode = func() (string, error) { return "opencode-test", nil }
	t.Cleanup(func() { findOpenCode = oldFind })
	artifacts, err := checkManagedArtifacts()
	if err != nil {
		t.Fatal(err)
	}
	report, err := installCurrentHarnessIntegrations(context.Background(), artifacts)
	if err != nil {
		t.Fatal(err)
	}
	if report.OpenCode.State != "installed" || !report.OpenCode.RestartRequired {
		t.Fatalf("OpenCode state %#v", report.OpenCode)
	}
	if !strings.Contains(report.OpenCode.Detail, binary+" serve") {
		t.Fatalf("OpenCode MCP detail %#v", report.OpenCode)
	}
}

func TestHarnessArtifactsReadyRejectsConflicts(t *testing.T) {
	report := managedArtifactReport{Artifacts: []managedArtifactResult{
		{Source: "plugins/hermes/__init__.py", Status: artifactCurrent},
		{Source: "plugins/hermes/plugin.yaml", Status: artifactConflict},
	}}
	if harnessArtifactsReady(report, "", "plugins/hermes/") {
		t.Fatal("conflicting Hermes plugin was considered ready")
	}
}
