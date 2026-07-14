package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
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

func TestListedSecret(t *testing.T) {
	if !listedSecret(`[{"name":"OPENROUTER_API_KEY","type":"secret_text"}]`, "OPENROUTER_API_KEY") {
		t.Fatal("secret not found")
	}
	if listedSecret(`[]`, "OPENROUTER_API_KEY") {
		t.Fatal("missing secret found")
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

func TestConnectExistingEndpointJSON(t *testing.T) {
	t.Setenv(envMimirHome, t.TempDir())
	t.Setenv("MIMIR_TOKEN", "machine-token")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/whoami" || r.Header.Get("Authorization") != "Bearer machine-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		fmt.Fprint(w, `{"sessions":0,"log":0}`)
	}))
	defer server.Close()
	var output bytes.Buffer
	if err := setup(context.Background(), []string{"--url", server.URL, "--json"}, IO{In: bytes.NewBuffer(nil), Out: &output, Err: &output}); err != nil {
		t.Fatal(err)
	}
	var result struct {
		State      string             `json:"state"`
		URL        string             `json:"url"`
		Connection connectionManifest `json:"connection"`
	}
	if err := json.Unmarshal(output.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result.State != "connected" || result.URL != server.URL || result.Connection.OpenAIBaseURL != server.URL+"/v1" {
		t.Fatalf("result %#v", result)
	}
	pointer, err := loadPointer()
	if err != nil {
		t.Fatal(err)
	}
	if pointer.Token != "machine-token" {
		t.Fatal("machine token was not persisted")
	}
	configPath, _ := pointerPath()
	config, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(config, []byte("machine-token")) {
		t.Fatal("token leaked into pointer config")
	}
}

func TestConnectExistingEndpointJSONNeedsToken(t *testing.T) {
	t.Setenv(envMimirHome, t.TempDir())
	t.Setenv("MIMIR_TOKEN", "")
	err := setup(context.Background(), []string{"--url", "https://mimir.example.workers.dev", "--json"}, IO{In: bytes.NewBuffer(nil), Out: &bytes.Buffer{}, Err: &bytes.Buffer{}})
	state, ok := err.(setupStateError)
	if !ok || state.State != "mimir_token_required" {
		t.Fatalf("error %#v", err)
	}
}
