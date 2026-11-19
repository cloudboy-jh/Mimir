package mimircli

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/cloudboy-jh/mimir/internal/harness"
	lifecyclepkg "github.com/cloudboy-jh/mimir/internal/harness/lifecycle"
	installpkg "github.com/cloudboy-jh/mimir/internal/install"
)

func stubLifecycleInstall(t *testing.T, report lifecyclepkg.InstallReport, selected *[]string) {
	t.Helper()
	old := runLifecycleInstall
	t.Cleanup(func() { runLifecycleInstall = old })
	runLifecycleInstall = func(_ context.Context, _ string, harnesses []string) (lifecyclepkg.InstallReport, error) {
		if selected != nil {
			*selected = harnesses
		}
		return report, nil
	}
}

func TestCmdInstallOutputIsExactAndHidesSuccessfulArtifacts(t *testing.T) {
	isolatedInstallation(t, false)
	stubLifecycleInstall(t, lifecyclepkg.InstallReport{
		Binary: installpkg.BinaryReport{Status: "installed", Version: "1.2.3", Path: "/bin/mimir"},
		Artifacts: installpkg.ArtifactReport{Artifacts: []installpkg.ArtifactResult{
			{Status: installpkg.ArtifactInstalled, Path: "/opencode/mimir.ts"},
			{Status: installpkg.ArtifactConflict, Path: "/claude/hooks.json", Detail: "existing hook preserved"},
			{Harness: "cursor", Status: installpkg.ArtifactConflict, Path: "/cursor/mcp.json", Snippet: "{\n  \"mcpServers\": {}\n}"},
		}},
		Integrations: harness.IntegrationReport{
			OpenCode:   harness.IntegrationState{State: "staged", RestartRequired: true},
			ClaudeCode: harness.IntegrationState{State: "failed", Detail: "hook conflict"},
		},
	}, nil)
	var output bytes.Buffer
	if err := cmdInstallIO(context.Background(), []string{"--harness", "all"}, IO{Out: &output}); err != nil {
		t.Fatal(err)
	}
	want := "Mimir 1.2.3 installed: /bin/mimir\n" +
		"Needs attention\n" +
		"  conflict  /claude/hooks.json: existing hook preserved\n" +
		"  failed    Claude Code: hook conflict\n" +
		"Manual MCP setup (Cursor): add the mimir server to /cursor/mcp.json:\n" +
		"    {\n" +
		"      \"mcpServers\": {}\n" +
		"    }\n" +
		"Next: restart OpenCode to load Mimir\n"
	if output.String() != want {
		t.Fatalf("output = %q, want %q", output.String(), want)
	}
}

func TestCmdInstallCurrentOutputIsExact(t *testing.T) {
	isolatedInstallation(t, false)
	stubLifecycleInstall(t, lifecyclepkg.InstallReport{Binary: installpkg.BinaryReport{Status: "current", Version: "1.2.3", Path: "/bin/mimir"}}, nil)
	var output bytes.Buffer
	if err := cmdInstallIO(context.Background(), []string{"--harness=all"}, IO{Out: &output}); err != nil {
		t.Fatal(err)
	}
	if want := "Mimir 1.2.3 already installed: /bin/mimir\n"; output.String() != want {
		t.Fatalf("output = %q, want %q", output.String(), want)
	}
}

func TestCmdInstallCanonicalizesRepeatableHarnessFlags(t *testing.T) {
	isolatedInstallation(t, false)
	var selected []string
	stubLifecycleInstall(t, lifecyclepkg.InstallReport{}, &selected)
	if err := cmdInstallIO(context.Background(), []string{"--harness", "cursor", "--harness=pi", "--json"}, IO{Out: &bytes.Buffer{}}); err != nil {
		t.Fatal(err)
	}
	if strings.Join(selected, ",") != "pi,cursor" {
		t.Fatalf("selected = %v", selected)
	}
}

func TestCmdInstallNoninteractiveRequiresHarnessUnlessRecorded(t *testing.T) {
	paths := isolatedInstallation(t, false)
	var selected []string
	stubLifecycleInstall(t, lifecyclepkg.InstallReport{}, &selected)
	err := cmdInstallIO(context.Background(), []string{"--json"}, IO{In: strings.NewReader(""), Out: &bytes.Buffer{}})
	if err == nil || !strings.Contains(err.Error(), "--harness") {
		t.Fatalf("error = %v", err)
	}
	writeTestManifest(t, paths, `{"harnesses":["codex"],"files":{}}`)
	selected = []string{"sentinel"}
	if err := cmdInstallIO(context.Background(), []string{"--json"}, IO{Out: &bytes.Buffer{}}); err != nil {
		t.Fatal(err)
	}
	if selected != nil {
		t.Fatalf("recorded selection was not reused: %v", selected)
	}
}

func TestCmdInstallRejectsMissingFlagValues(t *testing.T) {
	for _, args := range [][]string{{"--bin-dir"}, {"--bin-dir="}, {"--harness"}, {"--bin-dir", "--json"}} {
		if err := cmdInstallIO(context.Background(), args, IO{Out: &bytes.Buffer{}}); err == nil || !strings.Contains(err.Error(), "requires a value") {
			t.Fatalf("args %v error = %v", args, err)
		}
	}
}
