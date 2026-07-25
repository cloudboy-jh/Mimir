package mimirapi

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestPointerRoundTrip(t *testing.T) {
	t.Setenv(EnvHome, t.TempDir())
	want := Pointer{URL: "https://mimir.example.workers.dev", Token: "secret"}
	if err := SavePointer(want); err != nil {
		t.Fatal(err)
	}
	got, err := LoadPointer()
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestSavePointerAtomicallyReplacesRestrictiveFiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv(EnvHome, home)
	if err := SavePointer(Pointer{URL: "https://old.example", Token: "old"}); err != nil {
		t.Fatal(err)
	}
	if err := SavePointer(Pointer{URL: "https://new.example/", Token: "new"}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"config", "token"} {
		info, err := os.Stat(filepath.Join(home, name))
		if err != nil {
			t.Fatal(err)
		}
		if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode = %o", name, info.Mode().Perm())
		}
	}
	matches, err := filepath.Glob(filepath.Join(home, ".mimir-pointer-*"))
	if err != nil || len(matches) != 0 {
		t.Fatalf("temporary files = %v, %v", matches, err)
	}
}

func TestSavePointerRollsBackPairWhenTokenReplacementFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv(EnvHome, home)
	old := Pointer{URL: "https://old.example", Token: "old"}
	if err := SavePointer(old); err != nil {
		t.Fatal(err)
	}
	originalRename := commitPointerWrite
	calls := 0
	commitPointerWrite = func(oldPath, newPath string) error {
		calls++
		if calls == 2 {
			return errors.New("injected token replacement failure")
		}
		return os.Rename(oldPath, newPath)
	}
	t.Cleanup(func() { commitPointerWrite = originalRename })
	if err := SavePointer(Pointer{URL: "https://new.example", Token: "new"}); err == nil || !strings.Contains(err.Error(), "injected") {
		t.Fatalf("error = %v", err)
	}
	got, err := LoadPointer()
	if err != nil {
		t.Fatal(err)
	}
	if got != old {
		t.Fatalf("pointer pair partially committed: %#v", got)
	}
	for _, name := range []string{"config", "token"} {
		info, err := os.Stat(filepath.Join(home, name))
		if err != nil {
			t.Fatal(err)
		}
		if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
			t.Fatalf("%s mode = %o", name, info.Mode().Perm())
		}
	}
}

func TestSavePointerRemovesFirstNewFileWhenSecondReplacementFails(t *testing.T) {
	home := t.TempDir()
	t.Setenv(EnvHome, home)
	originalRename := commitPointerWrite
	calls := 0
	commitPointerWrite = func(oldPath, newPath string) error {
		calls++
		if calls == 2 {
			return errors.New("injected token replacement failure")
		}
		return os.Rename(oldPath, newPath)
	}
	t.Cleanup(func() { commitPointerWrite = originalRename })
	if err := SavePointer(Pointer{URL: "https://new.example", Token: "new"}); err == nil {
		t.Fatal("injected failure was ignored")
	}
	for _, name := range []string{"config", "token"} {
		if _, err := os.Lstat(filepath.Join(home, name)); !os.IsNotExist(err) {
			t.Fatalf("partial pointer file %s remains: %v", name, err)
		}
	}
}

func TestSavePointerRejectsUnsafeComponents(t *testing.T) {
	pointer := Pointer{URL: "https://mimir.example", Token: "secret"}
	t.Run("non-directory home", func(t *testing.T) {
		home := filepath.Join(t.TempDir(), "home")
		if err := os.WriteFile(home, []byte("not a directory"), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv(EnvHome, home)
		if err := SavePointer(pointer); err == nil || !strings.Contains(err.Error(), "MIMIR_HOME") {
			t.Fatalf("error = %v", err)
		}
	})
	for _, name := range []string{"config", "token"} {
		name := name
		t.Run("non-regular "+name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv(EnvHome, home)
			if err := os.Mkdir(filepath.Join(home, name), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := SavePointer(pointer); err == nil || !strings.Contains(err.Error(), "non-regular") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestSavePointerRejectsSymlinkedComponents(t *testing.T) {
	pointer := Pointer{URL: "https://mimir.example", Token: "secret"}
	t.Run("home", func(t *testing.T) {
		target := t.TempDir()
		home := filepath.Join(t.TempDir(), "home")
		if err := os.Symlink(target, home); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		t.Setenv(EnvHome, home)
		if err := SavePointer(pointer); err == nil || !strings.Contains(err.Error(), "MIMIR_HOME") {
			t.Fatalf("error = %v", err)
		}
	})
	for _, name := range []string{"config", "token"} {
		name := name
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv(EnvHome, home)
			target := filepath.Join(t.TempDir(), "target")
			if err := os.WriteFile(target, []byte("preserve"), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, filepath.Join(home, name)); err != nil {
				t.Skipf("symlinks unavailable: %v", err)
			}
			if err := SavePointer(pointer); err == nil || !strings.Contains(err.Error(), "symlinked") {
				t.Fatalf("error = %v", err)
			}
			if name == "token" {
				if _, err := os.Stat(filepath.Join(home, "config")); !os.IsNotExist(err) {
					t.Fatalf("config changed before unsafe token was rejected: %v", err)
				}
			}
			data, err := os.ReadFile(target)
			if err != nil || string(data) != "preserve" {
				t.Fatalf("target = %q, %v", data, err)
			}
		})
	}
}
