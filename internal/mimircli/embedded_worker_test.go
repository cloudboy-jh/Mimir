package mimircli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	mimirassets "github.com/cloudboy-jh/mimir"
)

func TestMaterializeEmbeddedWorkerPreservesStateAndAccessVars(t *testing.T) {
	paths := isolatedInstallation(t, false)
	if err := os.MkdirAll(filepath.Join(paths.Worker, "node_modules", "generated"), 0o700); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(paths.Worker, "node_modules", "generated", "state")
	if err := os.WriteFile(state, []byte("preserve"), 0o600); err != nil {
		t.Fatal(err)
	}
	wrangler := filepath.Join(paths.Worker, "wrangler.jsonc")
	if err := os.WriteFile(wrangler, []byte(`{"vars":{"DASHBOARD_ACCESS_AUD":"aud-secret","DASHBOARD_ACCESS_TEAM_DOMAIN":"team.example"}}`), 0o600); err != nil {
		t.Fatal(err)
	}

	dir, err := materializeEmbeddedWorker()
	if err != nil {
		t.Fatal(err)
	}
	if dir != paths.Worker {
		t.Fatalf("worker dir = %s, want %s", dir, paths.Worker)
	}
	if got, err := os.ReadFile(filepath.Join(paths.Worker, "src", "index.ts")); err != nil || len(got) == 0 {
		t.Fatalf("worker source missing: %v", err)
	}
	if got, err := os.ReadFile(state); err != nil || string(got) != "preserve" {
		t.Fatalf("generated state was not preserved: %q, %v", got, err)
	}
	config, err := os.ReadFile(wrangler)
	if err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"aud-secret", "team.example"} {
		if !strings.Contains(string(config), value) {
			t.Fatalf("wrangler config lost %q", value)
		}
	}
	if _, err := os.Stat(filepath.Join(paths.Worker, "src", "capture.test.ts")); !os.IsNotExist(err) {
		t.Fatal("Worker tests were materialized")
	}
	if _, err := os.Stat(filepath.Join(paths.Worker, "web", "dist")); !os.IsNotExist(err) {
		t.Fatal("dashboard dist was materialized")
	}
	if _, err := os.Stat(filepath.Join(paths.SharedAssets, "mimir-readme.png")); err != nil {
		t.Fatalf("shared asset missing: %v", err)
	}
}

func TestWorkerDirDefaultsToEmbeddedWorker(t *testing.T) {
	paths := isolatedInstallation(t, false)
	t.Chdir(t.TempDir())

	dir, err := workerDir("")
	if err != nil {
		t.Fatal(err)
	}
	if dir != paths.Worker {
		t.Fatalf("worker dir = %s, want embedded target %s", dir, paths.Worker)
	}
	if !pathExists(filepath.Join(dir, "src", "index.ts")) {
		t.Fatal("embedded Worker was not materialized")
	}
	rematerialized, err := materializeWorker(dir)
	if err != nil {
		t.Fatal(err)
	}
	if rematerialized != dir {
		t.Fatalf("rematerialized dir = %s, want %s", rematerialized, dir)
	}
}

func TestWorkerDirUsesMimirCheckoutAsDevelopmentOverride(t *testing.T) {
	root := t.TempDir()
	worker := filepath.Join(root, "worker")
	if err := os.MkdirAll(worker, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module github.com/cloudboy-jh/mimir\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worker, "wrangler.jsonc"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(worker)

	dir, err := workerDir("")
	if err != nil {
		t.Fatal(err)
	}
	if dir != worker {
		t.Fatalf("worker dir = %s, want checkout override %s", dir, worker)
	}
}

func TestWorkerDirIgnoresUnrelatedCheckout(t *testing.T) {
	paths := isolatedInstallation(t, false)
	root := t.TempDir()
	worker := filepath.Join(root, "worker")
	if err := os.MkdirAll(worker, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/not-mimir\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(worker, "wrangler.jsonc"), []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(worker)

	dir, err := workerDir("")
	if err != nil {
		t.Fatal(err)
	}
	if dir != paths.Worker {
		t.Fatalf("worker dir = %s, want embedded target %s", dir, paths.Worker)
	}
}

func TestWorkerDirUsesExplicitDevelopmentOverride(t *testing.T) {
	explicit := filepath.Join(t.TempDir(), "worker")
	dir, err := workerDir(explicit)
	if err != nil {
		t.Fatal(err)
	}
	want, err := filepath.Abs(explicit)
	if err != nil {
		t.Fatal(err)
	}
	if dir != want {
		t.Fatalf("worker dir = %s, want explicit override %s", dir, want)
	}
}

func TestMaterializeEmbeddedWorkerRemovesOnlyUnchangedManifestOwnedObsoleteFiles(t *testing.T) {
	paths := isolatedInstallation(t, false)
	if _, err := materializeEmbeddedWorker(); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(paths.MimirHome, "embedded-worker-manifest.json")
	manifest, err := loadEmbeddedWorkerManifest(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	unchangedMigration := filepath.Join(paths.Worker, "migrations", "9998_obsolete.sql")
	modifiedSource := []byte("old bundled source\n")
	modified := filepath.Join(paths.Worker, "src", "obsolete.ts")
	untrackedMigration := filepath.Join(paths.Worker, "migrations", "9999_untracked.sql")
	for path, data := range map[string][]byte{
		unchangedMigration: []byte("old migration\n"),
		modified:           modifiedSource,
		untrackedMigration: []byte("untracked migration\n"),
	} {
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	manifest.Files[unchangedMigration] = hashBytes([]byte("old migration\n"))
	manifest.Files[modified] = hashBytes(modifiedSource)
	if err := writeJSONAtomic(manifestPath, manifest); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(modified, []byte("locally modified\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := materializeEmbeddedWorker(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(unchangedMigration); !os.IsNotExist(err) {
		t.Fatalf("unchanged manifest-owned migration was not removed: %v", err)
	}
	for _, path := range []string{modified, untrackedMigration} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("preserved file %s: %v", path, err)
		}
	}
	next, err := loadEmbeddedWorkerManifest(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := next.Files[unchangedMigration]; ok {
		t.Fatal("removed obsolete migration remains in manifest")
	}
	if next.Files[modified] != hashBytes(modifiedSource) {
		t.Fatal("modified obsolete file lost its prior ownership hash")
	}
	assertPrivateFile(t, manifestPath)
}

func TestProductionBundleExcludesUnusedFiles(t *testing.T) {
	metadata, err := mimirassets.BundleMetadata()
	if err != nil {
		t.Fatal(err)
	}
	paths := make(map[string]bool, len(metadata))
	for _, file := range metadata {
		paths[file.Path] = true
	}
	for _, excluded := range []string{"assets/images/mimir-favicon.png", "worker/web/components.json"} {
		if paths[excluded] {
			t.Fatalf("unused file %s is embedded", excluded)
		}
	}
	if !paths["worker/worker-configuration.d.ts"] {
		t.Fatal("worker-configuration.d.ts is not embedded")
	}
}
