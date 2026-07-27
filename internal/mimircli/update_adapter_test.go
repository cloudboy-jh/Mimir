package mimircli

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/cloudboy-jh/mimir/internal/harness"
	lifecyclepkg "github.com/cloudboy-jh/mimir/internal/harness/lifecycle"
	installpkg "github.com/cloudboy-jh/mimir/internal/install"
)

func TestCmdUpdateForwardsLifecycleCheckAndPreservesJSONContract(t *testing.T) {
	old := runLifecycleUpdate
	t.Cleanup(func() { runLifecycleUpdate = old })
	called := false
	runLifecycleUpdate = func(_ context.Context, check bool) (lifecyclepkg.UpdateReport, error) {
		called = true
		if !check {
			t.Fatal("--check was not forwarded to the lifecycle service")
		}
		return lifecyclepkg.UpdateReport{
			Check:        true,
			Binary:       installpkg.UpdateBinaryReport{Status: "available", Current: "1.0.0", Latest: "2.0.0"},
			Artifacts:    installpkg.ArtifactReport{Operation: "check"},
			Integrations: harness.IntegrationReport{OpenCode: harness.IntegrationState{State: "current"}},
		}, nil
	}
	var output bytes.Buffer
	if err := cmdUpdate(context.Background(), []string{"--check", "--json"}, &output); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("lifecycle update was not called")
	}
	var report lifecyclepkg.UpdateReport
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatalf("invalid JSON output %q: %v", output.String(), err)
	}
	if !report.Check || report.Binary.Status != "available" || report.Binary.Current != "1.0.0" || report.Binary.Latest != "2.0.0" || report.Artifacts.Operation != "check" || report.Integrations.OpenCode.State != "current" {
		t.Fatalf("report = %#v", report)
	}
}

func TestCmdUpdateRejectsInvalidArgumentsBeforeLifecycle(t *testing.T) {
	old := runLifecycleUpdate
	t.Cleanup(func() { runLifecycleUpdate = old })
	runLifecycleUpdate = func(context.Context, bool) (lifecyclepkg.UpdateReport, error) {
		t.Fatal("lifecycle update called for invalid arguments")
		return lifecyclepkg.UpdateReport{}, nil
	}
	if err := cmdUpdate(context.Background(), []string{"--unknown"}, &bytes.Buffer{}); err == nil {
		t.Fatal("invalid update argument was accepted")
	}
}

func TestCmdUpdateListsArtifactsAndPluginActivation(t *testing.T) {
	old := runLifecycleUpdate
	t.Cleanup(func() { runLifecycleUpdate = old })
	runLifecycleUpdate = func(context.Context, bool) (lifecyclepkg.UpdateReport, error) {
		return lifecyclepkg.UpdateReport{
			Binary: installpkg.UpdateBinaryReport{Status: "updated", Current: "1.0.0", Latest: "1.1.0"},
			Artifacts: installpkg.ArtifactReport{ReceiptPath: "/receipt.json", Artifacts: []installpkg.ArtifactResult{
				{Status: installpkg.ArtifactUpdated, Source: "plugins/opencode/mimir.ts", Path: "/opencode/mimir.ts", Detail: "replaced managed plugin"},
				{Status: installpkg.ArtifactCurrent, Source: "skills/mimir-use/SKILL.md", Path: "/skills/SKILL.md"},
			}},
			Integrations: harness.IntegrationReport{
				OpenCode: harness.IntegrationState{State: "installed", RestartRequired: true},
			},
		}, nil
	}
	var output bytes.Buffer
	if err := cmdUpdate(context.Background(), nil, &output); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"updated  plugins/opencode/mimir.ts · /opencode/mimir.ts · replaced managed plugin",
		"current  skills/mimir-use/SKILL.md · /skills/SKILL.md",
		"Activation required:\n  OpenCode · restart OpenCode to load the updated managed plugin",
		"Deployment:\n  Worker bundle may be behind this CLI version\n  Run: mimir deploy",
	} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("output missing %q:\n%s", want, output.String())
		}
	}
}
