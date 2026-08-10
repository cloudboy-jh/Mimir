package mimircli

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/cloudboy-jh/mimir/internal/harness"
	lifecyclepkg "github.com/cloudboy-jh/mimir/internal/harness/lifecycle"
	installpkg "github.com/cloudboy-jh/mimir/internal/install"
)

func TestCmdInstallOutputIsExactAndHidesSuccessfulArtifacts(t *testing.T) {
	old := runLifecycleInstall
	t.Cleanup(func() { runLifecycleInstall = old })
	runLifecycleInstall = func(_ context.Context, _ string, _ func(string)) (lifecyclepkg.InstallReport, error) {
		return lifecyclepkg.InstallReport{
			Binary: installpkg.BinaryReport{Status: "installed", Version: "1.2.3", Path: "/bin/mimir"},
			Artifacts: installpkg.ArtifactReport{Artifacts: []installpkg.ArtifactResult{
				{Status: installpkg.ArtifactInstalled, Path: "/opencode/mimir.ts"},
				{Status: installpkg.ArtifactConflict, Path: "/claude/hooks.json", Detail: "existing hook preserved"},
			}},
			OpenCode:   harness.IntegrationState{State: "staged", RestartRequired: true},
			ClaudeCode: harness.IntegrationState{State: "failed", Detail: "hook conflict"},
		}, nil
	}
	var output bytes.Buffer
	if err := cmdInstallIO(context.Background(), nil, IO{Out: &output}); err != nil {
		t.Fatal(err)
	}
	want := "Installing Mimir...\n" +
		"Mimir 1.2.3 installed: /bin/mimir\n" +
		"Needs attention:\n" +
		"  /claude/hooks.json (conflict): existing hook preserved\n" +
		"  Claude Code: hook conflict\n" +
		"Next: restart OpenCode to load Mimir.\n"
	if output.String() != want {
		t.Fatalf("output = %q, want %q", output.String(), want)
	}
	if bytes.Contains(output.Bytes(), []byte("/opencode/mimir.ts")) {
		t.Fatalf("successful artifact noise remained:\n%s", output.String())
	}
}

func TestCmdInstallCurrentOutputIsExact(t *testing.T) {
	old := runLifecycleInstall
	t.Cleanup(func() { runLifecycleInstall = old })
	runLifecycleInstall = func(context.Context, string, func(string)) (lifecyclepkg.InstallReport, error) {
		return lifecyclepkg.InstallReport{Binary: installpkg.BinaryReport{Status: "current", Version: "1.2.3", Path: "/bin/mimir"}}, nil
	}
	var output bytes.Buffer
	if err := cmdInstallIO(context.Background(), nil, IO{Out: &output}); err != nil {
		t.Fatal(err)
	}
	if want := "Installing Mimir...\nMimir 1.2.3 already installed: /bin/mimir\n"; output.String() != want {
		t.Fatalf("output = %q, want %q", output.String(), want)
	}
}

func TestCmdInstallFailureLeavesOneActivityLine(t *testing.T) {
	old := runLifecycleInstall
	t.Cleanup(func() { runLifecycleInstall = old })
	runLifecycleInstall = func(context.Context, string, func(string)) (lifecyclepkg.InstallReport, error) {
		return lifecyclepkg.InstallReport{}, errors.New("install failed")
	}
	var output bytes.Buffer
	err := cmdInstallIO(context.Background(), nil, IO{Out: &output})
	if err == nil || err.Error() != "install failed" {
		t.Fatalf("error = %v", err)
	}
	if want := "Installing Mimir...\n"; output.String() != want {
		t.Fatalf("output = %q, want %q", output.String(), want)
	}
}
