package mimircli

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func stubLatestRelease(t *testing.T, latest string) {
	t.Helper()
	old := latestRelease
	t.Cleanup(func() { latestRelease = old })
	latestRelease = func(context.Context) (string, error) { return latest, nil }
}

func stubRunInstaller(t *testing.T, finished bool, dir *string) {
	t.Helper()
	old := runInstaller
	t.Cleanup(func() { runInstaller = old })
	runInstaller = func(_ context.Context, _ IO, target string) (bool, error) {
		*dir = target
		return finished, nil
	}
}

func TestCmdUpdateCheckOutput(t *testing.T) {
	for _, test := range []struct{ latest, want string }{
		{"999.0.0", "Mimir update available: " + strings.TrimPrefix(version, "v") + " -> 999.0.0\n"},
		{strings.TrimPrefix(version, "v"), "Mimir is up to date: " + strings.TrimPrefix(version, "v") + "\n"},
	} {
		stubLatestRelease(t, test.latest)
		var output bytes.Buffer
		if err := cmdUpdateIO(context.Background(), []string{"--check"}, IO{Out: &output}); err != nil {
			t.Fatal(err)
		}
		if output.String() != test.want {
			t.Fatalf("output = %q, want %q", output.String(), test.want)
		}
	}
}

func TestCmdUpdateCheckJSON(t *testing.T) {
	stubLatestRelease(t, "999.0.0")
	var output bytes.Buffer
	if err := cmdUpdateIO(context.Background(), []string{"--check", "--json"}, IO{Out: &output}); err != nil {
		t.Fatal(err)
	}
	var report map[string]any
	if err := json.Unmarshal(output.Bytes(), &report); err != nil {
		t.Fatal(err)
	}
	if report["check"] != true || report["latest"] != "999.0.0" || report["status"] != "available" || report["current"] != strings.TrimPrefix(version, "v") {
		t.Fatalf("report = %#v", report)
	}
}

func TestCmdUpdateRejectsRemovedFlags(t *testing.T) {
	for _, args := range [][]string{{"--force"}, {"--json"}, {"extra"}} {
		if err := cmdUpdateIO(context.Background(), args, IO{Out: &bytes.Buffer{}}); err == nil || !strings.Contains(err.Error(), "usage: mimir update") {
			t.Fatalf("args %v error = %v", args, err)
		}
	}
}

func TestCmdUpdateRunsInstallerIntoExecutableDir(t *testing.T) {
	isolatedInstallation(t, false)
	executable := filepath.Join(t.TempDir(), "mimir")
	old := executablePath
	executablePath = func() (string, error) { return executable, nil }
	t.Cleanup(func() { executablePath = old })
	var dir string
	stubRunInstaller(t, false, &dir)
	var output bytes.Buffer
	if err := cmdUpdateIO(context.Background(), nil, IO{Out: &output}); err != nil {
		t.Fatal(err)
	}
	if dir != filepath.Dir(executable) || !strings.Contains(output.String(), "continues in the background") {
		t.Fatalf("dir=%q output=%q", dir, output.String())
	}
}
