package install

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestMaterializeWorker(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "worker")
	if err := os.MkdirAll(filepath.Join(source, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "wrangler.jsonc"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "src", "index.ts"), []byte("export default {}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "assets", "images"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"mimir-readme.png", "mimir-favicon-32.png", "mimir-favicon-180.png"} {
		if err := os.WriteFile(filepath.Join(root, "assets", "images", name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("MIMIR_HOME", t.TempDir())
	target, err := materializeWorker(source)
	if err != nil {
		t.Fatal(err)
	}
	if !pathExists(filepath.Join(target, "src", "index.ts")) {
		t.Fatal("worker source was not materialized")
	}
	for _, name := range []string{"mimir-readme.png", "mimir-favicon-32.png", "mimir-favicon-180.png"} {
		data, err := os.ReadFile(filepath.Join(filepath.Dir(target), "assets", "images", name))
		if err != nil || string(data) != name {
			t.Fatalf("shared asset %s was not materialized: %q, %v", name, data, err)
		}
	}
	if err := updateWranglerVars(filepath.Join(target, "wrangler.jsonc"), map[string]string{"DASHBOARD_ACCESS_AUD": "aud-1", "DASHBOARD_ACCESS_TEAM_DOMAIN": "https://team.cloudflareaccess.com"}); err != nil {
		t.Fatal(err)
	}
	if _, err := materializeWorker(source); err != nil {
		t.Fatal(err)
	}
	vars := preservedWranglerVars(filepath.Join(target, "wrangler.jsonc"))
	if vars["DASHBOARD_ACCESS_AUD"] != "aud-1" || vars["DASHBOARD_ACCESS_TEAM_DOMAIN"] != "https://team.cloudflareaccess.com" {
		t.Fatalf("access vars were not preserved: %v", vars)
	}
}

func TestMaterializeWorkerRejectsSymlinkedSourceFile(t *testing.T) {
	root, source := testWorkerSource(t)
	external := filepath.Join(root, "external.ts")
	if err := os.WriteFile(external, []byte("external"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, filepath.Join(source, "src", "link.ts")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	t.Setenv("MIMIR_HOME", t.TempDir())
	if _, err := materializeWorker(source); err == nil {
		t.Fatal("symlinked Worker source file was accepted")
	}
}

func TestMaterializeWorkerRejectsSymlinkedRoots(t *testing.T) {
	_, source := testWorkerSource(t)

	t.Run("source", func(t *testing.T) {
		link := filepath.Join(t.TempDir(), "worker-link")
		if err := os.Symlink(source, link); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		t.Setenv("MIMIR_HOME", t.TempDir())
		if _, err := materializeWorker(link); err == nil {
			t.Fatal("symlinked Worker source root was accepted")
		}
	})

	t.Run("destination", func(t *testing.T) {
		home, external := t.TempDir(), t.TempDir()
		if err := os.Symlink(external, filepath.Join(home, "worker")); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		t.Setenv("MIMIR_HOME", home)
		if _, err := materializeWorker(source); err == nil {
			t.Fatal("symlinked Worker destination root was accepted")
		}
	})
}

func TestMaterializeWorkerRejectsUnsafeDestinationFiles(t *testing.T) {
	_, source := testWorkerSource(t)

	t.Run("symlink", func(t *testing.T) {
		home, external := t.TempDir(), filepath.Join(t.TempDir(), "external.ts")
		if err := os.WriteFile(external, []byte("keep"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(home, "worker", "src"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(external, filepath.Join(home, "worker", "src", "index.ts")); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		t.Setenv("MIMIR_HOME", home)
		if _, err := materializeWorker(source); err == nil {
			t.Fatal("symlinked Worker destination file was accepted")
		}
		data, err := os.ReadFile(external)
		if err != nil || string(data) != "keep" {
			t.Fatalf("symlink target changed: %q, %v", data, err)
		}
	})

	t.Run("non-regular", func(t *testing.T) {
		home := t.TempDir()
		if err := os.MkdirAll(filepath.Join(home, "worker", "src", "index.ts"), 0o700); err != nil {
			t.Fatal(err)
		}
		t.Setenv("MIMIR_HOME", home)
		if _, err := materializeWorker(source); err == nil {
			t.Fatal("non-regular Worker destination file was accepted")
		}
	})
}

func TestMaterializeWorkerRejectsUnsafeAssets(t *testing.T) {
	root, source := testWorkerSource(t)
	assetRoot := filepath.Join(root, "assets", "images")
	if err := os.MkdirAll(assetRoot, 0o700); err != nil {
		t.Fatal(err)
	}

	t.Run("non-regular source", func(t *testing.T) {
		if err := os.Mkdir(filepath.Join(assetRoot, "mimir-readme.png"), 0o700); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Remove(filepath.Join(assetRoot, "mimir-readme.png")) })
		t.Setenv("MIMIR_HOME", t.TempDir())
		if _, err := materializeWorker(source); err == nil {
			t.Fatal("non-regular shared asset source was accepted")
		}
	})

	t.Run("symlink destination", func(t *testing.T) {
		assetSource := filepath.Join(assetRoot, "mimir-favicon-32.png")
		if err := os.WriteFile(assetSource, []byte("asset"), 0o600); err != nil {
			t.Fatal(err)
		}
		home, external := t.TempDir(), filepath.Join(t.TempDir(), "external.png")
		if err := os.WriteFile(external, []byte("keep"), 0o600); err != nil {
			t.Fatal(err)
		}
		destinationRoot := filepath.Join(home, "assets", "images")
		if err := os.MkdirAll(destinationRoot, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(external, filepath.Join(destinationRoot, "mimir-favicon-32.png")); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		t.Setenv("MIMIR_HOME", home)
		if _, err := materializeWorker(source); err == nil {
			t.Fatal("symlinked shared asset destination was accepted")
		}
		data, err := os.ReadFile(external)
		if err != nil || string(data) != "keep" {
			t.Fatalf("asset symlink target changed: %q, %v", data, err)
		}
	})
}

func TestMaterializationFilesStayWithinRoot(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(filepath.Dir(root), "outside")
	if err := writeMaterializationFile(root, outside, []byte("no")); err == nil {
		t.Fatal("materialization write escaped its root")
	}
}

func TestMaterializeWorkerRejectsOverlappingRoots(t *testing.T) {
	_, source := testWorkerSource(t)
	t.Setenv("MIMIR_HOME", filepath.Join(source, "nested"))
	if _, err := materializeWorker(source); err == nil {
		t.Fatal("overlapping Worker source and destination were accepted")
	}
}

func testWorkerSource(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	source := filepath.Join(root, "worker")
	if err := os.MkdirAll(filepath.Join(source, "src"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "wrangler.jsonc"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "src", "index.ts"), []byte("export default {}"), 0o600); err != nil {
		t.Fatal(err)
	}
	return root, source
}

func TestWorkerDependencyHashTracksPackageLock(t *testing.T) {
	dir := t.TempDir()
	lock := filepath.Join(dir, "package-lock.json")
	if err := os.WriteFile(lock, []byte(`{"lockfileVersion":3}`), 0o600); err != nil {
		t.Fatal(err)
	}
	first, err := workerDependencyHash(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lock, []byte(`{"lockfileVersion":3,"packages":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := workerDependencyHash(dir)
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatal("dependency hash did not change with package lock")
	}
}

func TestEnsureWorkerDependenciesDoesNotInvokeBun(t *testing.T) {
	dir, bin := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package-lock.json"), []byte(`{"lockfileVersion":3}`), 0o600); err != nil {
		t.Fatal(err)
	}
	logPath := filepath.Join(t.TempDir(), "commands.log")
	npm := filepath.Join(bin, "npm")
	script := "#!/bin/sh\necho npm \"$@\" >> \"$MIMIR_COMMAND_LOG\"\nmkdir -p node_modules/.bin\ntouch node_modules/.bin/wrangler\n"
	bun := filepath.Join(bin, "bun")
	bunScript := "#!/bin/sh\necho bun >> \"$MIMIR_COMMAND_LOG\"\nexit 9\n"
	if runtime.GOOS == "windows" {
		npm += ".cmd"
		script = "@echo off\r\necho npm %*>>\"%MIMIR_COMMAND_LOG%\"\r\nif not exist node_modules\\.bin mkdir node_modules\\.bin\r\ntype nul > node_modules\\.bin\\wrangler.cmd\r\n"
		bun += ".cmd"
		bunScript = "@echo off\r\necho bun>>\"%MIMIR_COMMAND_LOG%\"\r\nexit /b 9\r\n"
	}
	for path, content := range map[string]string{npm: script, bun: bunScript} {
		if err := os.WriteFile(path, []byte(content), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("MIMIR_COMMAND_LOG", logPath)
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	for range 2 {
		if err := ensureWorkerDependencies(context.Background(), dir); err != nil {
			t.Fatal(err)
		}
	}
	commands, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(commands)); got != "npm ci --silent" {
		t.Fatalf("dependency commands = %q", got)
	}
}

func TestBuildDashboard(t *testing.T) {
	dir := t.TempDir()
	web, bin := filepath.Join(dir, "web"), filepath.Join(dir, "bin")
	if err := os.MkdirAll(web, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	bun, script := filepath.Join(bin, "bun"), "#!/bin/sh\n[ \"$1 $2\" = \"run build\" ] || exit 2\nmkdir -p dist\ntouch dist/index.html\n"
	if runtime.GOOS == "windows" {
		bun += ".cmd"
		script = "@echo off\r\nif not \"%1 %2\"==\"run build\" exit /b 2\r\nif not exist dist mkdir dist\r\ntype nul > dist\\index.html\r\n"
	}
	if err := os.WriteFile(bun, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	if err := buildDashboard(context.Background(), dir); err != nil {
		t.Fatal(err)
	}
	if !pathExists(filepath.Join(web, "dist", "index.html")) {
		t.Fatal("dashboard was not built")
	}
}
