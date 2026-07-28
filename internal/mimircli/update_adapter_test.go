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
	runLifecycleUpdate = func(_ context.Context, check, force bool, _ func(string)) (lifecyclepkg.UpdateReport, error) {
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
	runLifecycleUpdate = func(context.Context, bool, bool, func(string)) (lifecyclepkg.UpdateReport, error) {
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
	runLifecycleUpdate = func(context.Context, bool, bool, func(string)) (lifecyclepkg.UpdateReport, error) {
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
		"/opencode/mimir.ts",
		"plugins/opencode/mimir.ts · replaced managed plugin",
		"/skills/SKILL.md",
		"skills/mimir-use/SKILL.md",
		"Activation required",
		"OpenCode · restart OpenCode to load the updated managed plugin",
		"Deployment",
		"Worker bundle may be behind this CLI version",
		"[mimir deploy] Deploy the bundled Worker and dashboard.",
	} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("output missing %q:\n%s", want, output.String())
		}
	}
}

func TestCmdUpdateForwardsForce(t *testing.T) {
	old := runLifecycleUpdate
	t.Cleanup(func() { runLifecycleUpdate = old })
	runLifecycleUpdate = func(_ context.Context, check, force bool, _ func(string)) (lifecyclepkg.UpdateReport, error) {
		if check || !force {
			t.Fatalf("check=%v force=%v, want check=false force=true", check, force)
		}
		return lifecyclepkg.UpdateReport{Binary: installpkg.UpdateBinaryReport{Status: "updated", Current: "1.0.0", Latest: "1.1.0", Detail: "stopped Mimir process(es) 12, 34"}}, nil
	}
	var output bytes.Buffer
	if err := cmdUpdate(context.Background(), []string{"--force"}, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "stopped Mimir process(es) 12, 34") {
		t.Fatalf("output missing stop detail:\n%s", output.String())
	}
}

func TestCmdUpdateRejectsCheckWithForce(t *testing.T) {
	old := runLifecycleUpdate
	t.Cleanup(func() { runLifecycleUpdate = old })
	runLifecycleUpdate = func(context.Context, bool, bool, func(string)) (lifecyclepkg.UpdateReport, error) {
		t.Fatal("lifecycle update called for --check --force")
		return lifecyclepkg.UpdateReport{}, nil
	}
	if err := cmdUpdate(context.Background(), []string{"--check", "--force"}, &bytes.Buffer{}); err == nil {
		t.Fatal("--check --force was accepted")
	}
}

func TestCmdUpdateScheduledPrintsDeferralAndForceHint(t *testing.T) {
	old := runLifecycleUpdate
	t.Cleanup(func() { runLifecycleUpdate = old })
	runLifecycleUpdate = func(context.Context, bool, bool, func(string)) (lifecyclepkg.UpdateReport, error) {
		return lifecyclepkg.UpdateReport{
			Binary: installpkg.UpdateBinaryReport{Status: "scheduled", Current: "1.0.0", Latest: "1.1.0", Detail: "blocked by Mimir process(es) 42; the update will apply after they exit"},
		}, nil
	}
	var output bytes.Buffer
	if err := cmdUpdate(context.Background(), nil, &output); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"update to mimir 1.1.0 scheduled (current 1.0.0)",
		"blocked by Mimir process(es) 42; the update will apply after they exit",
		"[mimir update --force] Stop running Mimir processes and apply the update now.",
	} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("output missing %q:\n%s", want, output.String())
		}
	}
}
