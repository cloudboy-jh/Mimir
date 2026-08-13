package mimircli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	installpkg "github.com/cloudboy-jh/mimir/internal/install"
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
	if len(lines) != 6 || !strings.HasPrefix(lines[0], "● OpenCode") || !strings.HasPrefix(lines[1], "○ Pi") || !strings.HasPrefix(lines[5], "● Cursor") {
		t.Fatalf("output = %q", output.String())
	}
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
