package install

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestResolveInstallDirPrecedence(t *testing.T) {
	oldReadGoEnv := readGoEnv
	readGoEnv = func(string) string { return "" }
	t.Cleanup(func() { readGoEnv = oldReadGoEnv })
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("MIMIR_INSTALL_DIR", filepath.Join(home, "mimir-bin"))
	t.Setenv("GOBIN", filepath.Join(home, "go-bin"))
	t.Setenv("GOPATH", filepath.Join(home, "go-path")+string(os.PathListSeparator)+filepath.Join(home, "other"))
	if got, _ := resolveInstallDir(""); got != filepath.Join(home, "mimir-bin") {
		t.Fatalf("MIMIR_INSTALL_DIR target = %q", got)
	}
	t.Setenv("MIMIR_INSTALL_DIR", "")
	if got, _ := resolveInstallDir(""); got != filepath.Join(home, "go-bin") {
		t.Fatalf("GOBIN target = %q", got)
	}
	t.Setenv("GOBIN", "")
	if got, _ := resolveInstallDir(""); got != filepath.Join(home, "go-path", "bin") {
		t.Fatalf("GOPATH target = %q", got)
	}
}

func TestTemporaryExecutableRecognizesGoBuildCache(t *testing.T) {
	cache := filepath.Join(filepath.Dir(os.TempDir()), "mimir-test-go-cache")
	t.Setenv("GOCACHE", cache)
	if !temporaryExecutable(filepath.Join(cache, "d6", "build-id-d", "mimir.exe")) {
		t.Fatal("go-run executable under GOCACHE was not temporary")
	}
}

func TestResolveInstallDirUsesPersistedGoEnv(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MIMIR_INSTALL_DIR", "")
	t.Setenv("GOBIN", "")
	t.Setenv("GOPATH", "")
	oldReadGoEnv := readGoEnv
	readGoEnv = func(key string) string {
		if key == "GOBIN" {
			return filepath.Join(home, "persisted-bin")
		}
		return ""
	}
	t.Cleanup(func() { readGoEnv = oldReadGoEnv })
	if got, err := resolveInstallDir(""); err != nil || got != filepath.Join(home, "persisted-bin") {
		t.Fatalf("install dir = %q, %v", got, err)
	}
}

func TestTemporaryExecutableUsesPersistedGoCache(t *testing.T) {
	cache := filepath.Join(filepath.Dir(os.TempDir()), "persisted-go-cache")
	t.Setenv("GOCACHE", "")
	t.Setenv("GOTMPDIR", "")
	oldReadGoEnv := readGoEnv
	readGoEnv = func(key string) string {
		if key == "GOCACHE" {
			return cache
		}
		return ""
	}
	t.Cleanup(func() { readGoEnv = oldReadGoEnv })
	if !temporaryExecutable(filepath.Join(cache, "ab", "build-d", "mimir.exe")) {
		t.Fatal("persisted GOCACHE executable was not temporary")
	}
}

func binaryName() string {
	if runtime.GOOS == "windows" {
		return "mimir.exe"
	}
	return "mimir"
}

func TestInstallExecutableCopyFreshAndReplacement(t *testing.T) {
	target := filepath.Join(t.TempDir(), "new", binaryName())
	for _, content := range []string{"first", "second"} {
		if err := installExecutableCopy(target, []byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "second" {
		t.Fatalf("installed binary = %q, %v", data, err)
	}
}

func TestBootstrapRejectsUnownedDifferentTarget(t *testing.T) {
	isolatedInstallation(t, false)
	source, target := filepath.Join(t.TempDir(), binaryName()), filepath.Join(t.TempDir(), binaryName())
	if err := os.WriteFile(source, []byte("new binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("someone else's binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := bootstrapExecutable(filepath.Dir(target), ManifestCLI{}, func() (string, error) { return source, nil }); err == nil || !strings.Contains(err.Error(), "unowned executable") {
		t.Fatalf("error = %v", err)
	}
	if got := mustReadFile(t, target); string(got) != "someone else's binary" {
		t.Fatalf("unowned target changed to %q", got)
	}
}

func TestBootstrapReplacesRecordedTarget(t *testing.T) {
	isolatedInstallation(t, false)
	source, target := filepath.Join(t.TempDir(), binaryName()), filepath.Join(t.TempDir(), binaryName())
	if err := os.WriteFile(source, []byte("new binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("old binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	cli := ManifestCLI{Path: target, SHA256: hashBytes([]byte("old binary"))}
	report, err := bootstrapExecutable(filepath.Dir(target), cli, func() (string, error) { return source, nil })
	if err != nil || report.Status != "updated" || report.Path != target {
		t.Fatalf("report=%#v err=%v", report, err)
	}
	if got := mustReadFile(t, target); string(got) != "new binary" {
		t.Fatalf("target = %q", got)
	}
}

func TestBootstrapDoesNotRewriteCurrentTarget(t *testing.T) {
	isolatedInstallation(t, false)
	target := filepath.Join(t.TempDir(), binaryName())
	if err := os.WriteFile(target, []byte("binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	stamp := time.Unix(1_700_000_000, 0)
	if err := os.Chtimes(target, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	report, err := bootstrapExecutable(filepath.Dir(target), ManifestCLI{}, func() (string, error) { return target, nil })
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(target)
	if err != nil || report.Status != "current" || !info.ModTime().Equal(stamp) {
		t.Fatalf("report=%#v info=%v err=%v", report, info, err)
	}
}

func TestBootstrapReplacesVerifiedUnownedMimir(t *testing.T) {
	isolatedInstallation(t, false)
	source, target := filepath.Join(t.TempDir(), binaryName()), filepath.Join(t.TempDir(), binaryName())
	if err := os.WriteFile(source, []byte("new Mimir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("go-installed Mimir"), 0o755); err != nil {
		t.Fatal(err)
	}
	old := isMimirExecutable
	isMimirExecutable = func(path string) bool { return path == target }
	t.Cleanup(func() { isMimirExecutable = old })
	report, err := bootstrapExecutable(filepath.Dir(target), ManifestCLI{}, func() (string, error) { return source, nil })
	if err != nil || report.Status != "updated" {
		t.Fatalf("report=%#v err=%v", report, err)
	}
	if got := mustReadFile(t, target); string(got) != "new Mimir" {
		t.Fatalf("target = %q", got)
	}
}
