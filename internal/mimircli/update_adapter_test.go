package mimircli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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

func TestCmdUpdatePrintsOnlyIssuesAndRequiredActions(t *testing.T) {
	old := runLifecycleUpdate
	t.Cleanup(func() { runLifecycleUpdate = old })
	runLifecycleUpdate = func(context.Context, bool, bool, func(string)) (lifecyclepkg.UpdateReport, error) {
		return lifecyclepkg.UpdateReport{
			Binary: installpkg.UpdateBinaryReport{Status: "updated", Current: "1.0.0", Latest: "1.1.0"},
			Artifacts: installpkg.ArtifactReport{ReceiptPath: "/receipt.json", Artifacts: []installpkg.ArtifactResult{
				{Status: installpkg.ArtifactUpdated, Source: "plugins/opencode/mimir.ts", Path: "/opencode/mimir.ts", Detail: "replaced managed plugin"},
				{Status: installpkg.ArtifactCurrent, Source: "skills/mimir-use/SKILL.md", Path: "/skills/SKILL.md"},
				{Status: installpkg.ArtifactUpdated, Source: "plugins/claude-code/hooks/hooks.json", Path: "/claude/managed-hooks.json"},
				{Status: installpkg.ArtifactConflict, Source: "plugins/claude-code/hooks/hooks.json", Path: "/claude/hooks.json", Detail: "existing hook preserved"},
			}},
			Integrations: harness.IntegrationReport{
				OpenCode:   harness.IntegrationState{State: "installed", RestartRequired: true},
				ClaudeCode: harness.IntegrationState{State: "installed", RestartRequired: true},
			},
		}, nil
	}
	var output bytes.Buffer
	if err := cmdUpdate(context.Background(), nil, &output); err != nil {
		t.Fatal(err)
	}
	want := "==> Updating Mimir\n" +
		"OK  Mimir updated: 1.0.0 -> 1.1.0\n" +
		"WARN Needs attention\n" +
		"    /claude/hooks.json (conflict): existing hook preserved\n" +
		"WARN Restart required: OpenCode, Claude Code\n" +
		"NEXT mimir deploy\n"
	if output.String() != want {
		t.Fatalf("output = %q, want %q", output.String(), want)
	}
	for _, hidden := range []string{"/opencode/mimir.ts", "/skills/SKILL.md", "/claude/managed-hooks.json", "/receipt.json"} {
		if strings.Contains(output.String(), hidden) {
			t.Fatalf("successful artifact noise %q remained:\n%s", hidden, output.String())
		}
	}
}

func TestCmdUpdateCurrentOutputIsExact(t *testing.T) {
	old := runLifecycleUpdate
	t.Cleanup(func() { runLifecycleUpdate = old })
	runLifecycleUpdate = func(context.Context, bool, bool, func(string)) (lifecyclepkg.UpdateReport, error) {
		return lifecyclepkg.UpdateReport{
			Binary: installpkg.UpdateBinaryReport{Status: "current", Current: "1.1.0", Latest: "1.1.0"},
			Artifacts: installpkg.ArtifactReport{Artifacts: []installpkg.ArtifactResult{
				{Status: installpkg.ArtifactCurrent, Source: "plugins/opencode/mimir.ts", Path: "/opencode/mimir.ts"},
			}},
			Integrations: harness.IntegrationReport{OpenCode: harness.IntegrationState{RestartRequired: true}},
		}, nil
	}
	var output bytes.Buffer
	if err := cmdUpdate(context.Background(), nil, &output); err != nil {
		t.Fatal(err)
	}
	if want := "==> Updating Mimir\nOK  Mimir is up to date: 1.1.0\n"; output.String() != want {
		t.Fatalf("output = %q, want %q", output.String(), want)
	}
}

func TestCmdUpdateProgressRemainsLineOriented(t *testing.T) {
	old := runLifecycleUpdate
	t.Cleanup(func() { runLifecycleUpdate = old })
	runLifecycleUpdate = func(_ context.Context, _ bool, _ bool, progress func(string)) (lifecyclepkg.UpdateReport, error) {
		for _, message := range []string{"Checking managed artifacts", "Checking latest release", "Downloading checksums", "Downloading Mimir 1.1.0", "Replacing Mimir executable", "Refreshing managed integrations"} {
			progress(message)
		}
		return lifecyclepkg.UpdateReport{Binary: installpkg.UpdateBinaryReport{Status: "updated", Current: "1.0.0", Latest: "1.1.0"}}, nil
	}
	var output bytes.Buffer
	if err := cmdUpdate(context.Background(), nil, &output); err != nil {
		t.Fatal(err)
	}
	if want := "==> Updating Mimir\n==> Checking managed artifacts\n==> Checking latest release\n==> Downloading checksums\n==> Downloading Mimir 1.1.0\n==> Replacing Mimir executable\n==> Refreshing managed integrations\nOK  Mimir updated: 1.0.0 -> 1.1.0\nNEXT mimir deploy\n"; output.String() != want {
		t.Fatalf("output = %q, want plain line output %q", output.String(), want)
	}
}

func TestUpdateRestartNamesTrackContentChanges(t *testing.T) {
	integrations := harness.IntegrationReport{OpenCode: harness.IntegrationState{RestartRequired: true}}
	for _, test := range []struct {
		name   string
		status installpkg.ArtifactStatus
		want   int
	}{
		{name: "adopted ownership", status: installpkg.ArtifactAdopted, want: 0},
		{name: "current content", status: installpkg.ArtifactCurrent, want: 0},
		{name: "installed content", status: installpkg.ArtifactInstalled, want: 1},
		{name: "updated content", status: installpkg.ArtifactUpdated, want: 1},
		{name: "removed content", status: installpkg.ArtifactRemoved, want: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			artifacts := installpkg.ArtifactReport{Artifacts: []installpkg.ArtifactResult{{
				Status: test.status, Source: "plugins/opencode/mimir.ts", Path: "/opencode/mimir.ts",
			}}}
			if got := len(updateRestartNames(artifacts, integrations)); got != test.want {
				t.Fatalf("restart count = %d, want %d", got, test.want)
			}
		})
	}
}

func TestCmdUpdateFailureLeavesOneActivityLine(t *testing.T) {
	old := runLifecycleUpdate
	t.Cleanup(func() { runLifecycleUpdate = old })
	runLifecycleUpdate = func(context.Context, bool, bool, func(string)) (lifecyclepkg.UpdateReport, error) {
		return lifecyclepkg.UpdateReport{}, errors.New("update failed")
	}
	var output bytes.Buffer
	err := cmdUpdate(context.Background(), nil, &output)
	if err == nil || err.Error() != "update failed" {
		t.Fatalf("error = %v", err)
	}
	if want := "==> Updating Mimir\n"; output.String() != want {
		t.Fatalf("output = %q, want %q", output.String(), want)
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
	want := "==> Updating Mimir\n" +
		"OK  Mimir update scheduled: 1.0.0 -> 1.1.0\n" +
		"    blocked by Mimir process(es) 42; the update will apply after they exit\n" +
		"NEXT mimir update --force\n"
	if output.String() != want {
		t.Fatalf("output = %q, want %q", output.String(), want)
	}
}
