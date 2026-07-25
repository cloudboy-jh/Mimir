package mimircli

import (
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
