package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDatabaseID(t *testing.T) {
	got := databaseID("database_id = \"123e4567-e89b-12d3-a456-426614174000\"")
	if got != "123e4567-e89b-12d3-a456-426614174000" {
		t.Fatalf("database ID %q", got)
	}
}

func TestListedDatabaseID(t *testing.T) {
	got := listedDatabaseID(`[{"uuid":"123e4567-e89b-12d3-a456-426614174000","name":"mimir"}]`, "mimir")
	if got != "123e4567-e89b-12d3-a456-426614174000" {
		t.Fatalf("database ID %q", got)
	}
}

func TestWorkerURL(t *testing.T) {
	got := workerURL("Published mimir (https://mimir.example.workers.dev)")
	if got != "https://mimir.example.workers.dev" {
		t.Fatalf("worker URL %q", got)
	}
}

func TestMaterializeWorker(t *testing.T) {
	source := t.TempDir()
	if err := os.MkdirAll(filepath.Join(source, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "wrangler.jsonc"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "src", "index.ts"), []byte("export default {}"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(envMimirHome, t.TempDir())
	target, err := materializeWorker(source)
	if err != nil {
		t.Fatal(err)
	}
	if !pathExists(filepath.Join(target, "src", "index.ts")) {
		t.Fatal("worker source was not materialized")
	}
}
