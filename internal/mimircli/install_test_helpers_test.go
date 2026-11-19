package mimircli

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	installpkg "github.com/cloudboy-jh/mimir/internal/install"
)

func isolatedInstallation(t *testing.T, hermes bool) installpkg.InstallationPaths {
	t.Helper()
	home := t.TempDir()
	mimirHome := filepath.Join(home, "mimir-state")
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("MIMIR_HOME", mimirHome)
	if hermes {
		t.Setenv("HERMES_HOME", filepath.Join(home, "hermes"))
	} else {
		t.Setenv("HERMES_HOME", "")
		if runtime.GOOS == "windows" {
			t.Setenv("LOCALAPPDATA", filepath.Join(home, "local-app-data"))
		}
	}
	paths, err := installpkg.Paths()
	if err != nil {
		t.Fatal(err)
	}
	return paths
}

func writeTestManifest(t *testing.T, paths installpkg.InstallationPaths, content string) {
	t.Helper()
	if err := os.MkdirAll(paths.MimirHome, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(paths.Manifest, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}
