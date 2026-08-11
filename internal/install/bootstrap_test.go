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

func TestBootstrapTemporaryReleaseRecordsReleaseSource(t *testing.T) {
	isolatedInstallation(t, false)
	t.Setenv("MIMIR_INSTALL_SOURCE", "release")
	source := filepath.Join(t.TempDir(), "mimir")
	if runtime.GOOS == "windows" {
		source += ".exe"
	}
	if err := os.WriteFile(source, []byte("release binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	report, err := Install(t.TempDir(), func() (string, error) { return source, nil })
	if err != nil {
		t.Fatal(err)
	}
	if report.Binary.Source != "release" || report.Binary.Method != "bootstrap-copy" {
		t.Fatalf("report = %#v", report)
	}
	receipt, err := loadInstallReceipt()
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Source != "release" || receipt.Method != "bootstrap-copy" {
		t.Fatalf("receipt = %#v", receipt)
	}
}

func TestBootstrapReleaseSourceDoesNotDependOnTemporaryPathDetection(t *testing.T) {
	isolatedInstallation(t, false)
	t.Setenv("MIMIR_INSTALL_SOURCE", "release")
	source := filepath.Join(t.TempDir(), "mimir")
	if runtime.GOOS == "windows" {
		source += ".exe"
	}
	if err := os.WriteFile(source, []byte("release binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	oldTemporary := executableIsTemporary
	executableIsTemporary = func(string) bool { return false }
	t.Cleanup(func() { executableIsTemporary = oldTemporary })
	report, err := Install(t.TempDir(), func() (string, error) { return source, nil })
	if err != nil {
		t.Fatal(err)
	}
	if report.Binary.Source != "release" || report.Binary.Method != "bootstrap-copy" {
		t.Fatalf("report = %#v", report)
	}
	receipt, err := loadInstallReceipt()
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Source != "release" || receipt.Method != "bootstrap-copy" {
		t.Fatalf("receipt = %#v", receipt)
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

func TestInstallExecutableCopyFreshAndReplacement(t *testing.T) {
	target := filepath.Join(t.TempDir(), "new", "mimir")
	if runtime.GOOS == "windows" {
		target += ".exe"
	}
	if err := installExecutableCopy(target, []byte("first"), ""); err != nil {
		t.Fatal(err)
	}
	if err := installExecutableCopy(target, []byte("second"), hashBytes([]byte("first"))); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != "second" {
		t.Fatalf("installed binary = %q, %v", data, err)
	}
}

func TestBootstrapCurrentExecutableRejectsUnownedDifferentTarget(t *testing.T) {
	isolate := isolatedInstallation(t, false)
	_ = isolate
	sourceDir, targetDir := t.TempDir(), t.TempDir()
	name := "mimir"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	source, target := filepath.Join(sourceDir, name), filepath.Join(targetDir, name)
	if err := os.WriteFile(source, []byte("new binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("someone else's binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := bootstrapExecutable(targetDir, func() (string, error) { return source, nil }); err == nil || !strings.Contains(err.Error(), "unowned executable") {
		t.Fatalf("error = %v", err)
	}
	if got, _ := os.ReadFile(target); string(got) != "someone else's binary" {
		t.Fatalf("unowned target changed to %q", got)
	}
}

func TestBootstrapCurrentExecutableDoesNotRewriteTarget(t *testing.T) {
	paths := isolatedInstallation(t, false)
	dir := t.TempDir()
	name := "mimir"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	target := filepath.Join(dir, name)
	data := []byte("binary")
	if err := os.WriteFile(target, data, 0o755); err != nil {
		t.Fatal(err)
	}
	stamp := time.Unix(1_700_000_000, 0)
	if err := os.Chtimes(target, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	receipt := newInstallReceipt()
	receipt.Source, receipt.Method = "release", "bootstrap-copy"
	receipt.CLI = installReceiptCLI{Path: target, Hash: hashBytes(data)}
	if err := writeJSONAtomic(paths.Receipt, receipt); err != nil {
		t.Fatal(err)
	}
	report, err := bootstrapExecutable(dir, func() (string, error) { return target, nil })
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(target)
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != "current" || report.Source != "release" || report.Method != "bootstrap-copy" || !info.ModTime().Equal(stamp) {
		t.Fatalf("report=%#v modtime=%s", report, info.ModTime())
	}
}

func TestBootstrapCurrentExecutableReplacesVerifiedUnownedMimir(t *testing.T) {
	isolatedInstallation(t, false)
	sourceDir, targetDir := t.TempDir(), t.TempDir()
	name := "mimir"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	source, target := filepath.Join(sourceDir, name), filepath.Join(targetDir, name)
	if err := os.WriteFile(source, []byte("new Mimir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(target, []byte("go-installed Mimir"), 0o755); err != nil {
		t.Fatal(err)
	}
	old := isMimirExecutable
	isMimirExecutable = func(path string) bool { return path == target }
	t.Cleanup(func() { isMimirExecutable = old })
	report, err := bootstrapExecutable(targetDir, func() (string, error) { return source, nil })
	if err != nil {
		t.Fatal(err)
	}
	if report.Status != "updated" || report.Method != "bootstrap-copy" {
		t.Fatalf("report %#v", report)
	}
	if got := mustReadFile(t, target); string(got) != "new Mimir" {
		t.Fatalf("target = %q", got)
	}
}

func TestInstallExecutableCopyRejectsChangedOwnedTarget(t *testing.T) {
	target := filepath.Join(t.TempDir(), "mimir")
	if err := os.WriteFile(target, []byte("changed"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := installExecutableCopy(target, []byte("new"), hashBytes([]byte("receipt bytes"))); err == nil || !strings.Contains(err.Error(), "no longer matches") {
		t.Fatalf("error = %v", err)
	}
}
