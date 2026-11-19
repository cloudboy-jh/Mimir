package mimircli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	doctorpkg "github.com/cloudboy-jh/mimir/internal/doctor"
	installpkg "github.com/cloudboy-jh/mimir/internal/install"
)

func terminalFiles(t *testing.T, input string) (*os.File, *os.File) {
	t.Helper()
	in, err := os.CreateTemp(t.TempDir(), "stdin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := in.WriteString(input); err != nil {
		t.Fatal(err)
	}
	if _, err := in.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	out, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { in.Close(); out.Close() })
	old := installTerminal
	installTerminal = func(*os.File) bool { return true }
	t.Cleanup(func() { installTerminal = old })
	return in, out
}

func TestInstallPromptRequiresTerminalStdout(t *testing.T) {
	in, _ := terminalFiles(t, "")
	if _, err := promptHarnessSelection(IO{In: in, Out: &bytes.Buffer{}}, false); err == nil || !strings.Contains(err.Error(), "noninteractive installs require") {
		t.Fatalf("error = %v", err)
	}
}

func TestInstallPromptReadsCommaSeparatedIDs(t *testing.T) {
	isolatedInstallation(t, false)
	in, out := terminalFiles(t, "cursor, pi\n")
	selected, err := promptHarnessSelection(IO{In: in, Out: out}, false)
	if err != nil || strings.Join(selected, ",") != "pi,cursor" {
		t.Fatalf("selected = %v, err = %v", selected, err)
	}
}

func stubHarnessDoctor(t *testing.T, checks ...doctorpkg.Check) {
	t.Helper()
	old := runHarnessDoctor
	t.Cleanup(func() { runHarnessDoctor = old })
	runHarnessDoctor = func(context.Context) doctorpkg.Report { return doctorpkg.Report{Checks: checks} }
}

func TestHarnessOverviewListsSelectedAndDetectedHarnesses(t *testing.T) {
	paths := isolatedInstallation(t, false)
	if _, err := installpkg.SelectArtifacts("install", []string{"cursor"}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", "")
	if err := os.MkdirAll(paths.CodexHome, 0o700); err != nil {
		t.Fatal(err)
	}
	stubHarnessDoctor(t, doctorpkg.Check{Name: "cursor.plugin-load", Status: "ok"})
	var output bytes.Buffer
	if err := cmdHarness(context.Background(), nil, IO{Out: &output}); err != nil {
		t.Fatal(err)
	}
	text := output.String()
	for _, want := range []string{"Mimir harnesses\n", "  active    Cursor\n", "  detected  Codex\n"} {
		if !strings.Contains(text, want) {
			t.Fatalf("output missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "OpenCode") {
		t.Fatalf("undetected harness listed:\n%s", text)
	}
}

func TestHarnessOverviewJSONReportsPendingActivation(t *testing.T) {
	isolatedInstallation(t, false)
	t.Setenv("PATH", "")
	if _, err := installpkg.SelectArtifacts("install", []string{"cursor"}); err != nil {
		t.Fatal(err)
	}
	stubHarnessDoctor(t, doctorpkg.Check{Name: "cursor.plugin-load", Status: "failed", Detail: "no load", Repair: "open a session"})
	var output bytes.Buffer
	if err := cmdHarness(context.Background(), []string{"--json"}, IO{Out: &output}); err != nil {
		t.Fatal(err)
	}
	var views []harnessView
	if err := json.Unmarshal(output.Bytes(), &views); err != nil {
		t.Fatal(err)
	}
	if len(views) != 1 || views[0].Status != "installed" || views[0].Action != "open a session" {
		t.Fatalf("views = %#v", views)
	}
}

func TestHarnessOverviewShowsManualMCPStep(t *testing.T) {
	report := installpkg.ArtifactReport{Artifacts: []installpkg.ArtifactResult{
		{Harness: "cursor", Source: installpkg.MCPSource("cursor"), Path: "/home/u/.cursor/mcp.json", Status: installpkg.ArtifactConflict, Snippet: `{"mcpServers":{"mimir":{}}}`},
		{Harness: "cursor", Source: "plugins/cursor/hooks.json", Status: installpkg.ArtifactCurrent},
	}}
	views := harnessViews([]installpkg.Harness{{ID: "cursor", Name: "Cursor", Selected: true}}, report, nil, map[string]doctorpkg.State{})
	if len(views) != 1 || views[0].Status != "installed" || views[0].MCPPath != "/home/u/.cursor/mcp.json" {
		t.Fatalf("views = %#v", views)
	}
	lines := strings.Join(manualMCPLines(report), "\n")
	if !strings.Contains(lines, "Manual MCP setup (Cursor): add the mimir server to /home/u/.cursor/mcp.json:") || !strings.Contains(lines, `{"mcpServers":{"mimir":{}}}`) {
		t.Fatalf("manual lines = %q", lines)
	}
}

func TestHarnessDisableRemovesManagedFiles(t *testing.T) {
	paths := isolatedInstallation(t, false)
	if _, err := installpkg.SelectArtifacts("install", []string{"cursor", "codex"}); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := cmdHarness(context.Background(), []string{"disable", "cursor"}, IO{Out: &output}); err != nil {
		t.Fatal(err)
	}
	if output.String() != "Cursor disabled\n" {
		t.Fatalf("output = %q", output.String())
	}
	if _, err := os.Stat(filepath.Join(paths.CursorHome, "hooks.json")); !os.IsNotExist(err) {
		t.Fatalf("cursor hooks survived: %v", err)
	}
	if manifest, _ := installpkg.LoadManifest(); strings.Join(manifest.Harnesses, ",") != "codex" {
		t.Fatalf("harnesses = %v", manifest.Harnesses)
	}
}

func TestPrimaryHarnessRejectsSecondaryHarness(t *testing.T) {
	err := cmdPrimaryHarness(context.Background(), "disable", []string{"cursor"}, IO{Out: &bytes.Buffer{}})
	if err == nil || !strings.Contains(err.Error(), "use mimir harness") {
		t.Fatalf("error = %v", err)
	}
}
