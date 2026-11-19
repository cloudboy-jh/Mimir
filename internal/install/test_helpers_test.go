package install

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func isolatedInstallation(t *testing.T, hermes bool) InstallationPaths {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("MIMIR_HOME", filepath.Join(home, "mimir-state"))
	t.Setenv("PI_CODING_AGENT_DIR", "")
	t.Setenv("CLAUDE_CONFIG_DIR", "")
	t.Setenv("CODEX_HOME", "")
	if hermes {
		t.Setenv("HERMES_HOME", filepath.Join(home, "hermes"))
	} else {
		t.Setenv("HERMES_HOME", "")
		if runtime.GOOS == "windows" {
			t.Setenv("LOCALAPPDATA", filepath.Join(home, "local-app-data"))
		}
	}
	paths, err := Paths()
	if err != nil {
		t.Fatal(err)
	}
	return paths
}

func assertPrivateFile(t *testing.T, path string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("%s permissions = %o, want 600", path, info.Mode().Perm())
	}
}
