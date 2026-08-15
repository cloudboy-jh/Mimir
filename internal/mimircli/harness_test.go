package mimircli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	doctorpkg "github.com/cloudboy-jh/mimir/internal/doctor"
	installpkg "github.com/cloudboy-jh/mimir/internal/install"
	"github.com/cloudboy-jh/mimir/internal/ui/selector"
)

func TestParseInstallHarnessesCanonicalizesRepeatableFlags(t *testing.T) {
	remaining, selected, explicit, err := parseInstallHarnesses([]string{"--harness", "cursor", "--json", "--harness=opencode", "--harness", "cursor"})
	if err != nil {
		t.Fatal(err)
	}
	if !explicit || strings.Join(selected, ",") != "opencode,cursor" || strings.Join(remaining, ",") != "--json" {
		t.Fatalf("remaining=%#v selected=%#v explicit=%v", remaining, selected, explicit)
	}
}

func TestInstallJSONRequiresExplicitHarnessSelection(t *testing.T) {
	err := cmdInstallIO(context.Background(), []string{"--json"}, IO{Out: &bytes.Buffer{}})
	if err == nil || !strings.Contains(err.Error(), "noninteractive installs require") {
		t.Fatalf("error = %v", err)
	}
}

func TestInstallPromptRequiresTerminalStdout(t *testing.T) {
	input, err := os.CreateTemp(t.TempDir(), "stdin")
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	old := installTerminal
	installTerminal = func(*os.File) bool { return true }
	t.Cleanup(func() { installTerminal = old })
	_, err = installHarnessSelection(IO{In: input, Out: &bytes.Buffer{}}, false, false, nil)
	if err == nil || !strings.Contains(err.Error(), "noninteractive installs require") {
		t.Fatalf("error = %v", err)
	}
}

func TestInstallPromptAllowsTerminalStdinAndStdout(t *testing.T) {
	input, err := os.CreateTemp(t.TempDir(), "stdin")
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	output, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	old := installTerminal
	installTerminal = func(*os.File) bool { return true }
	t.Cleanup(func() { installTerminal = old })
	selected, err := installHarnessSelection(IO{In: input, Out: output}, false, false, nil)
	if err != nil {
		t.Fatal(err)
	}
	defaults, err := installpkg.DetectedHarnesses()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(selected, ",") != strings.Join(defaults, ",") {
		t.Fatalf("selected = %#v, defaults = %#v", selected, defaults)
	}
}

func TestHarnessListUsesCanonicalOrderAndMarkers(t *testing.T) {
	isolatedInstallation(t, false)
	selected, _ := installpkg.NormalizeHarnesses([]string{"cursor", "opencode"})
	if _, err := installpkg.RefreshSelectedArtifacts("install", selected); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := cmdHarness(context.Background(), []string{"list"}, IO{Out: &output}); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 7 || !strings.HasPrefix(lines[0], "● OpenCode") || !strings.HasPrefix(lines[1], "○ Pi") || !strings.HasPrefix(lines[2], "○ Oh My Pi") || !strings.HasPrefix(lines[6], "● Cursor") {
		t.Fatalf("output = %q", output.String())
	}
}

func TestHarnessOverviewShowsOnlySelectedOrDetectedHarnesses(t *testing.T) {
	isolatedInstallation(t, false)
	selected, _ := installpkg.NormalizeHarnesses([]string{"opencode"})
	if _, err := installpkg.RefreshSelectedArtifacts("install", selected); err != nil {
		t.Fatal(err)
	}
	old := runHarnessDoctor
	runHarnessDoctor = func(context.Context) doctorpkg.Report {
		return doctorpkg.Report{OK: true, Checks: []doctorpkg.Check{{Name: "opencode.plugin-load", Status: "ok"}}}
	}
	t.Cleanup(func() { runHarnessDoctor = old })

	var output bytes.Buffer
	if err := cmdHarness(context.Background(), nil, IO{Out: &output}); err != nil {
		t.Fatal(err)
	}
	text := output.String()
	if !strings.Contains(text, "● OpenCode") || !strings.Contains(text, "Active") {
		t.Fatalf("output = %q", text)
	}
	harnesses, err := installpkg.Harnesses()
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(text), "\n")
	for _, harness := range harnesses {
		found := false
		for _, line := range lines[1:] {
			for _, marker := range []string{"● ", "○ "} {
				prefix := marker + harness.Name
				if strings.HasPrefix(line, prefix) && len(line) > len(prefix) && line[len(prefix)] == ' ' {
					found = true
				}
			}
		}
		if found != (harness.Selected || harness.Detected) {
			t.Fatalf("%s row present=%v, selected=%v detected=%v: %q", harness.Name, found, harness.Selected, harness.Detected, text)
		}
	}
}

func TestHarnessOverviewUsesInteractiveChecklist(t *testing.T) {
	isolatedInstallation(t, false)
	selected, _ := installpkg.NormalizeHarnesses([]string{"opencode"})
	if _, err := installpkg.RefreshSelectedArtifacts("install", selected); err != nil {
		t.Fatal(err)
	}
	oldTerminal := installTerminal
	installTerminal = func(*os.File) bool { return true }
	t.Cleanup(func() { installTerminal = oldTerminal })
	oldSelector := runHarnessSelector
	runHarnessSelector = func(_ *os.File, _ *os.File, title string, items []selector.Item) (selector.Result, error) {
		if title != "Mimir harnesses" || len(items) == 0 || !strings.Contains(items[0].Label, "OpenCode") || !items[0].Selected {
			t.Fatalf("title=%q items=%#v", title, items)
		}
		return selector.Result{Selected: make([]bool, len(items)), Accepted: true}, nil
	}
	t.Cleanup(func() { runHarnessSelector = oldSelector })
	oldAvailable := harnessSelectorAvailable
	harnessSelectorAvailable = func(*os.File, *os.File) bool { return true }
	t.Cleanup(func() { harnessSelectorAvailable = oldAvailable })
	oldDoctor := runHarnessDoctor
	runHarnessDoctor = func(context.Context) doctorpkg.Report { return doctorpkg.Report{OK: true} }
	t.Cleanup(func() { runHarnessDoctor = oldDoctor })

	input, err := os.CreateTemp(t.TempDir(), "stdin")
	if err != nil {
		t.Fatal(err)
	}
	defer input.Close()
	output, err := os.CreateTemp(t.TempDir(), "stdout")
	if err != nil {
		t.Fatal(err)
	}
	defer output.Close()
	if err := cmdHarness(context.Background(), nil, IO{In: input, Out: output}); err != nil {
		t.Fatal(err)
	}
	harnesses, err := installpkg.Harnesses()
	if err != nil {
		t.Fatal(err)
	}
	for _, harness := range harnesses {
		if harness.Selected {
			t.Fatalf("%s remains selected", harness.Name)
		}
	}
}

func TestHarnessOverviewJSONIncludesCombinedState(t *testing.T) {
	isolatedInstallation(t, false)
	selected, _ := installpkg.NormalizeHarnesses([]string{"pi"})
	if _, err := installpkg.RefreshSelectedArtifacts("install", selected); err != nil {
		t.Fatal(err)
	}
	old := runHarnessDoctor
	runHarnessDoctor = func(context.Context) doctorpkg.Report {
		return doctorpkg.Report{OK: false, Checks: []doctorpkg.Check{{Name: "pi.plugin-load", Status: "failed", Detail: "no active load", Repair: "restart Pi"}}}
	}
	t.Cleanup(func() { runHarnessDoctor = old })

	var output bytes.Buffer
	if err := cmdHarness(context.Background(), []string{"--json"}, IO{Out: &output}); err != nil {
		t.Fatal(err)
	}
	var views []harnessView
	if err := json.Unmarshal(output.Bytes(), &views); err != nil {
		t.Fatal(err)
	}
	var pi harnessView
	for _, view := range views {
		if view.ID == "pi" {
			pi = view
		}
	}
	if pi.ID != "pi" || pi.Status != "installed" || !pi.Installed || pi.Active || pi.ActivationAction != "restart Pi" {
		t.Fatalf("Pi view = %#v (all views %#v)", pi, views)
	}
}

func TestHarnessOverviewPrefersHermesDiagnosticFailureOverOldActiveLoad(t *testing.T) {
	isolatedInstallation(t, true)
	selected, _ := installpkg.NormalizeHarnesses([]string{"hermes"})
	if _, err := installpkg.RefreshSelectedArtifacts("install", selected); err != nil {
		t.Fatal(err)
	}
	old := runHarnessDoctor
	runHarnessDoctor = func(context.Context) doctorpkg.Report {
		return doctorpkg.Report{OK: false, Checks: []doctorpkg.Check{
			{Name: "hermes.plugin-load", Status: "ok"},
			{Name: "hermes.plugin", Status: "failed", Detail: "Mimir plugin is disabled"},
		}}
	}
	t.Cleanup(func() { runHarnessDoctor = old })

	var output bytes.Buffer
	if err := cmdHarness(context.Background(), []string{"--json"}, IO{Out: &output}); err != nil {
		t.Fatal(err)
	}
	var views []harnessView
	if err := json.Unmarshal(output.Bytes(), &views); err != nil {
		t.Fatal(err)
	}
	for _, view := range views {
		if view.ID == "hermes" {
			if view.Status != "needs_attention" || view.Active || !strings.Contains(view.Detail, "disabled") {
				t.Fatalf("Hermes view = %#v", view)
			}
			return
		}
	}
	t.Fatal("Hermes view not found")
}

func TestPrimaryHarnessIDAcceptsCaseAndSeparators(t *testing.T) {
	for input, want := range map[string]string{
		"PI":        "pi",
		"OpenCode":  "opencode",
		"open-code": "opencode",
		"open_code": "opencode",
		"Hermes":    "hermes",
	} {
		got, err := primaryHarnessID(input)
		if err != nil || got != want {
			t.Fatalf("primaryHarnessID(%q) = %q, %v; want %q", input, got, err, want)
		}
	}
	if _, err := primaryHarnessID("codex"); err == nil || !strings.Contains(err.Error(), "mimir harness") {
		t.Fatalf("error = %v", err)
	}
}

func TestTopLevelDisableResolvesFriendlyPrimaryHarnessName(t *testing.T) {
	isolatedInstallation(t, false)
	selected, _ := installpkg.NormalizeHarnesses([]string{"opencode", "pi"})
	if _, err := installpkg.RefreshSelectedArtifacts("install", selected); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := ExecuteIO(context.Background(), []string{"disable", "Open-Code"}, IO{Out: &output}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "OpenCode disabled") {
		t.Fatalf("output = %q", output.String())
	}
	harnesses, err := installpkg.Harnesses()
	if err != nil {
		t.Fatal(err)
	}
	for _, harness := range harnesses {
		if harness.ID == "opencode" && harness.Selected {
			t.Fatal("OpenCode remains selected")
		}
		if harness.ID == "pi" && !harness.Selected {
			t.Fatal("Pi was unexpectedly disabled")
		}
	}
}

func TestLegacyHarnessDisablePreservesCanonicalIDOutput(t *testing.T) {
	isolatedInstallation(t, false)
	selected, _ := installpkg.NormalizeHarnesses([]string{"opencode"})
	if _, err := installpkg.RefreshSelectedArtifacts("install", selected); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := cmdHarness(context.Background(), []string{"disable", "opencode"}, IO{Out: &output}); err != nil {
		t.Fatal(err)
	}
	if output.String() != "opencode disabled\n" {
		t.Fatalf("output = %q", output.String())
	}
}

func TestHarnessSelectionDoesNotDisableCurrentHarnessWhenEnableFails(t *testing.T) {
	paths := isolatedInstallation(t, true)
	selected, _ := installpkg.NormalizeHarnesses([]string{"opencode"})
	if _, err := installpkg.RefreshSelectedArtifacts("install", selected); err != nil {
		t.Fatal(err)
	}
	conflict := filepath.Join(paths.HermesHome, "plugins", "mimir", "plugin.yaml")
	if err := os.MkdirAll(filepath.Dir(conflict), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(conflict, []byte("name: user-plugin\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	current, err := installpkg.Harnesses()
	if err != nil {
		t.Fatal(err)
	}
	err = applyHarnessSelection(context.Background(), current, []string{"hermes"}, &bytes.Buffer{})
	if err == nil {
		t.Fatal("expected Hermes enable conflict")
	}
	after, err := installpkg.Harnesses()
	if err != nil {
		t.Fatal(err)
	}
	for _, harness := range after {
		if harness.ID == "opencode" && !harness.Selected {
			t.Fatal("OpenCode was disabled after Hermes enable failed")
		}
	}
}

func TestHarnessSelectionRestoresEarlierDisableWhenLaterDisableFails(t *testing.T) {
	isolatedInstallation(t, true)
	selected, _ := installpkg.NormalizeHarnesses([]string{"opencode", "hermes"})
	if _, err := installpkg.RefreshSelectedArtifacts("install", selected); err != nil {
		t.Fatal(err)
	}
	oldRunHermesPluginCommand := runHermesPluginCommand
	runHermesPluginCommand = func(context.Context, string, ...string) error { return errors.New("disable failed") }
	t.Cleanup(func() { runHermesPluginCommand = oldRunHermesPluginCommand })
	current, err := installpkg.Harnesses()
	if err != nil {
		t.Fatal(err)
	}
	if err := applyHarnessSelection(context.Background(), current, nil, &bytes.Buffer{}); err == nil {
		t.Fatal("expected Hermes disable failure")
	}
	after, err := installpkg.Harnesses()
	if err != nil {
		t.Fatal(err)
	}
	for _, harness := range after {
		if (harness.ID == "opencode" || harness.ID == "hermes") && !harness.Selected {
			t.Fatalf("%s was not restored after disable failure", harness.ID)
		}
	}
}

func TestHarnessOverviewReportsPreservedDeselectedSkill(t *testing.T) {
	paths := isolatedInstallation(t, false)
	selected, _ := installpkg.NormalizeHarnesses([]string{"opencode"})
	report, err := installpkg.RefreshSelectedArtifacts("install", selected)
	if err != nil {
		t.Fatal(err)
	}
	skill := ""
	for _, artifact := range report.Artifacts {
		if artifact.Source == "skills/mimir-use/SKILL.md" && strings.HasPrefix(artifact.Path, paths.OpenCodeHome) {
			skill = artifact.Path
			break
		}
	}
	if skill == "" {
		t.Fatal("OpenCode skill artifact not found")
	}
	if err := os.WriteFile(skill, []byte("user changes\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := cmdHarness(context.Background(), []string{"disable", "opencode"}, IO{Out: &bytes.Buffer{}}); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := cmdHarness(context.Background(), []string{"--json"}, IO{Out: &output}); err != nil {
		t.Fatal(err)
	}
	var views []harnessView
	if err := json.Unmarshal(output.Bytes(), &views); err != nil {
		t.Fatal(err)
	}
	for _, view := range views {
		if view.ID == "opencode" {
			if view.Selected || !view.Installed || view.Status != "needs_attention" || !strings.Contains(view.Detail, "preserved") {
				t.Fatalf("OpenCode view = %#v", view)
			}
			return
		}
	}
	t.Fatal("preserved OpenCode integration was hidden")
}

func TestHarnessEnableHermesRollsBackConflictWithoutEnabling(t *testing.T) {
	paths := isolatedInstallation(t, true)
	selected, _ := installpkg.NormalizeHarnesses([]string{"opencode"})
	if _, err := installpkg.RefreshSelectedArtifacts("install", selected); err != nil {
		t.Fatal(err)
	}
	conflict := filepath.Join(paths.HermesHome, "plugins", "mimir", "plugin.yaml")
	if err := os.MkdirAll(filepath.Dir(conflict), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(conflict, []byte("name: user-plugin\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	oldRunHermesPluginCommand := runHermesPluginCommand
	commands := 0
	runHermesPluginCommand = func(context.Context, string, ...string) error { commands++; return nil }
	t.Cleanup(func() { runHermesPluginCommand = oldRunHermesPluginCommand })

	err := cmdHarness(context.Background(), []string{"enable", "hermes", "--json"}, IO{Out: &bytes.Buffer{}})
	if err == nil || !strings.Contains(err.Error(), "not ready or receipt-owned") {
		t.Fatalf("error = %v", err)
	}
	if commands != 0 {
		t.Fatalf("Hermes plugin commands = %d, want 0", commands)
	}
	harnesses, err := installpkg.Harnesses()
	if err != nil {
		t.Fatal(err)
	}
	for _, harness := range harnesses {
		if harness.ID == "hermes" && harness.Selected {
			t.Fatal("Hermes selection was not rolled back")
		}
	}
	data, err := os.ReadFile(conflict)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "name: user-plugin\n" {
		t.Fatalf("conflicting artifact = %q", data)
	}
}
