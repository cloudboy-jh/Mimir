package doctor

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/cloudboy-jh/mimir/internal/install"
	"github.com/cloudboy-jh/mimir/internal/mimirapi"
	"github.com/cloudboy-jh/mimir/internal/tailnet"
)

type requesterFunc func(context.Context, string, string, any) ([]byte, error)

func (f requesterFunc) Request(ctx context.Context, method, path string, body any) ([]byte, error) {
	return f(ctx, method, path, body)
}

const currentWhoami = `{"service":"mimir","api_version":1,"capabilities":["query_api","hermes_authorization","session_events","session_lifecycle"]}`

func connectedService(t *testing.T, loads string, reportTailnet func(context.Context, tailnet.Requester) (bool, error)) Service {
	t.Helper()
	service := New(requesterFunc(func(_ context.Context, method, path string, _ any) ([]byte, error) {
		switch path {
		case "/whoami":
			return []byte(currentWhoami), nil
		case "/integrations/harness-loads":
			return []byte(loads), nil
		}
		t.Fatalf("request %s %s", method, path)
		return nil, nil
	}))
	service.LoadPointer = func() (mimirapi.Pointer, error) { return mimirapi.Pointer{URL: "https://mimir.test"}, nil }
	service.ReportTailnet = reportTailnet
	return service
}

func checkNamed(report Report, name string) (Check, bool) {
	for _, check := range report.Checks {
		if check.Name == name {
			return check, true
		}
	}
	return Check{}, false
}

func TestRunReportsArtifactsBeforeMissingConnection(t *testing.T) {
	service := New(requesterFunc(func(context.Context, string, string, any) ([]byte, error) { return nil, nil }))
	service.CheckArtifacts = func() (install.ArtifactReport, error) {
		return install.ArtifactReport{Artifacts: []install.ArtifactResult{{Source: "skills/mimir-use/SKILL.md", Path: "missing", Status: install.ArtifactMissing}}}, nil
	}
	service.LoadManifest = func() (install.Manifest, error) { return install.Manifest{}, nil }
	service.LoadPointer = func() (mimirapi.Pointer, error) { return mimirapi.Pointer{}, errors.New("not connected") }
	report := service.Run(context.Background())
	if report.OK || len(report.Checks) != 2 || report.Checks[0].Name != "managed-file skills/mimir-use/SKILL.md" || report.Checks[0].Repair != "mimir install" || report.Checks[1].Name != "connection" {
		t.Fatalf("report %#v", report)
	}
}

func TestStructuredReportGroupsOperationalState(t *testing.T) {
	report := Report{OK: false, Checks: []Check{
		{Name: "managed-file plugins/opencode/mimir.ts", Status: "failed", Detail: "modified"},
		{Name: "opencode.plugin-load", Status: "failed", Repair: "restart OpenCode"},
		{Name: "worker.bundle", Status: "ok"},
		{Name: "tailnet", Status: "skipped"},
		{Name: "cursor.mcp", Status: "warning"},
	}}
	structured := report.Structured()
	if len(structured.Installed["plugins/opencode/mimir.ts"]) != 1 || structured.Active["opencode.plugin-load"].Repair != "restart OpenCode" {
		t.Fatalf("structured = %#v", structured)
	}
	if structured.Deployed["worker.bundle"].Status != "ok" || structured.Connection["tailnet"].Status != "skipped" || structured.Diagnostics["cursor.mcp"].Status != "warning" {
		t.Fatalf("structured = %#v", structured)
	}
}

func TestValidateWorkerIdentityRequiresQueryAPI(t *testing.T) {
	if err := ValidateWorkerIdentity([]byte(`{"sessions":0,"log":0}`)); err == nil {
		t.Fatal("legacy Worker was accepted")
	}
	if err := ValidateWorkerIdentity([]byte(`{"service":"mimir","api_version":1,"capabilities":["hermes_authorization","session_events","session_lifecycle"]}`)); err == nil || !strings.Contains(err.Error(), "query_api") {
		t.Fatalf("Worker without query_api: %v", err)
	}
	if err := ValidateWorkerIdentity([]byte(currentWhoami)); err != nil {
		t.Fatal(err)
	}
}

func TestRunReportsIncompatibleWorkerWithDeployRepair(t *testing.T) {
	service := New(requesterFunc(func(context.Context, string, string, any) ([]byte, error) {
		return []byte(`{"service":"mimir","api_version":1,"capabilities":["session_events"]}`), nil
	}))
	service.CheckArtifacts = func() (install.ArtifactReport, error) { return install.ArtifactReport{}, nil }
	service.LoadManifest = func() (install.Manifest, error) { return install.Manifest{}, nil }
	service.LoadPointer = func() (mimirapi.Pointer, error) { return mimirapi.Pointer{URL: "https://mimir.test"}, nil }
	service.ReportTailnet = func(context.Context, tailnet.Requester) (bool, error) { return false, nil }
	check, _ := checkNamed(service.Run(context.Background()), "worker")
	if check.Status != "failed" || check.Repair != "mimir deploy" {
		t.Fatalf("worker check = %#v", check)
	}
}

func TestRunReportsTailnet(t *testing.T) {
	for _, test := range []struct {
		reported bool
		err      error
		want     string
	}{{true, nil, "ok"}, {false, nil, "skipped"}, {false, errors.New("tailscale failed"), "failed"}} {
		service := connectedService(t, `{"loads":[]}`, func(context.Context, tailnet.Requester) (bool, error) { return test.reported, test.err })
		service.CheckArtifacts = func() (install.ArtifactReport, error) { return install.ArtifactReport{}, nil }
		service.LoadManifest = func() (install.Manifest, error) { return install.Manifest{}, nil }
		if check, _ := checkNamed(service.Run(context.Background()), "tailnet"); check.Status != test.want {
			t.Fatalf("tailnet check = %#v, want %s", check, test.want)
		}
	}
}

func TestHarnessLoadChecksCompareManagedHashes(t *testing.T) {
	service := connectedService(t, `{"loads":[
		{"harness":"pi","artifact_sha256":"pi-hash","installation_id":"install-1","reported_at":"2026-07-26T10:00:00Z"},
		{"harness":"hermes","artifact_sha256":"hermes-old","installation_id":"install-1","reported_at":"2026-07-26T10:00:00Z"},
		{"harness":"hermes","artifact_sha256":"hermes-hash","installation_id":"install-2","reported_at":"2026-07-26T11:00:00Z"}
	]}`, func(context.Context, tailnet.Requester) (bool, error) { return false, nil })
	service.CheckArtifacts = func() (install.ArtifactReport, error) {
		return install.ArtifactReport{Artifacts: []install.ArtifactResult{
			{Path: "/pi/mimir.ts", Source: "plugins/pi/mimir.ts", Harness: "pi", Status: install.ArtifactCurrent},
			{Path: "/hermes/__init__.py", Source: "plugins/hermes/__init__.py", Harness: "hermes", Status: install.ArtifactCurrent},
			{Path: "/cursor/hooks.json", Source: "plugins/cursor/hooks.json", Harness: "cursor", Status: install.ArtifactCurrent},
			{Path: "/cursor/mcp.json", Source: install.MCPSource("cursor"), Harness: "cursor", Status: install.ArtifactConflict, Snippet: `{"mcpServers":{}}`},
		}}, nil
	}
	service.LoadManifest = func() (install.Manifest, error) {
		return install.Manifest{InstallationID: "install-1", Files: map[string]string{
			"/pi/mimir.ts": "pi-hash", "/hermes/__init__.py": "hermes-hash", "/cursor/hooks.json": "cursor-hash",
		}}, nil
	}
	report := service.Run(context.Background())
	if check, _ := checkNamed(report, "pi.plugin-load"); check.Status != "ok" {
		t.Fatalf("pi = %#v", check)
	}
	if check, _ := checkNamed(report, "hermes.plugin-load"); check.Status != "failed" || check.Repair != "restart Hermes" || !strings.Contains(check.Detail, "hermes-old") {
		t.Fatalf("hermes = %#v", check)
	}
	if check, _ := checkNamed(report, "cursor.plugin-load"); check.Status != "failed" || strings.Contains(strings.ToLower(check.Repair), "restart") {
		t.Fatalf("cursor = %#v", check)
	}
	if check, _ := checkNamed(report, "cursor.mcp"); check.Status != "warning" || !strings.Contains(check.Repair, `{"mcpServers":{}}`) {
		t.Fatalf("cursor mcp = %#v", check)
	}
}

func TestHarnessLoadChecksSkipLegacyEndpoint(t *testing.T) {
	service := New(requesterFunc(func(_ context.Context, _ string, path string, _ any) ([]byte, error) {
		if path == "/whoami" {
			return []byte(currentWhoami), nil
		}
		return nil, &mimirapi.Error{StatusCode: 404, Status: "404 Not Found"}
	}))
	artifacts := install.ArtifactReport{Artifacts: []install.ArtifactResult{{Path: "/oc/mimir.ts", Source: "plugins/opencode/mimir.ts", Status: install.ArtifactCurrent}}}
	var checks []Check
	service.addHarnessLoadChecks(context.Background(), artifacts, install.Manifest{Files: map[string]string{"/oc/mimir.ts": "hash"}}, func(name, status, detail, repair string) {
		checks = append(checks, Check{Name: name, Status: status, Detail: detail, Repair: repair})
	})
	if len(checks) != 1 || checks[0].Name != "harness-loads" || checks[0].Status != "skipped" {
		t.Fatalf("checks %#v", checks)
	}
}
