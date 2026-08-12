package deployment

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDeploymentStateRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MIMIR_HOME", home)
	want := DeploymentState{
		AccountID:  "account-id",
		WorkerName: "custom-worker", DatabaseName: "custom-db", DatabaseID: "database-uuid",
		BucketName: "custom-logs", URL: "https://mimir.example.workers.dev/", VerifiedAt: "2026-08-12T00:00:00Z",
	}
	if err := SaveState(want); err != nil {
		t.Fatal(err)
	}
	got, err := LoadState()
	if err != nil {
		t.Fatal(err)
	}
	if got.Schema != deploymentStateSchema || got.AccountID != want.AccountID || got.WorkerName != want.WorkerName || got.DatabaseName != want.DatabaseName || got.DatabaseID != want.DatabaseID || got.URL != "https://mimir.example.workers.dev" {
		t.Fatalf("state = %#v", got)
	}
	if _, err := os.Stat(filepath.Join(home, "cloudflare-deployment.json")); err != nil {
		t.Fatal(err)
	}
	want.DatabaseID = "replacement-uuid"
	if err := SaveState(want); err != nil {
		t.Fatal(err)
	}
	got, err = LoadState()
	if err != nil || got.DatabaseID != want.DatabaseID {
		t.Fatalf("replacement state = %#v, error = %v", got, err)
	}
}

func TestLoadDeploymentStateMissing(t *testing.T) {
	t.Setenv("MIMIR_HOME", t.TempDir())
	state, err := LoadState()
	if err != nil || state != (DeploymentState{}) {
		t.Fatalf("state = %#v, error = %v", state, err)
	}
}

func TestLoadDeploymentStateRejectsUnsupportedSchema(t *testing.T) {
	home := t.TempDir()
	t.Setenv("MIMIR_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "cloudflare-deployment.json"), []byte(`{"schema":9}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadState(); err == nil {
		t.Fatal("unsupported schema accepted")
	}
}
