package mimircli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testConnectionManifest(home string) connectionManifest {
	return connectionManifest{
		OpenAIBaseURL:   "https://mimir.example.workers.dev/v1",
		CredentialFile:  filepath.Join(home, ".mimir", "token"),
		MCPCommand:      []string{filepath.Join(home, "bin", "mimir"), "serve"},
		OptionalHeaders: []string{"x-mimir-session", "x-mimir-request-kind"},
	}
}

func TestInstallOpenCodeIntegration(t *testing.T) {
	home := t.TempDir()
	manifest := testConnectionManifest(home)
	if err := installOpenCodeIntegration(home, manifest); err != nil {
		t.Fatal(err)
	}
	plugin, err := os.ReadFile(filepath.Join(home, ".config", "opencode", "plugins", "mimir.ts"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"mimir-opencode-adapter-version: 1", `providerID !== "openrouter"`, `providerID !== "mimir"`, `"x-mimir-session"`, `"x-mimir-request-kind"`, `: "primary"`} {
		if !strings.Contains(string(plugin), want) {
			t.Fatalf("plugin missing %q", want)
		}
	}
	if strings.Contains(string(plugin), "token") || strings.Contains(string(plugin), "WORKER_URL") {
		t.Fatal("plugin should not contain credentials or connection configuration")
	}
	commandFile, err := os.ReadFile(filepath.Join(home, ".config", "opencode", "commands", "mimir-end-session.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"$ARGUMENTS as the required exact session ID", "session_end", "Never guess an ID", "Return the session_end receipt exactly"} {
		if !strings.Contains(string(commandFile), want) {
			t.Fatalf("command missing %q", want)
		}
	}
	config, err := os.ReadFile(filepath.Join(home, ".config", "opencode", "opencode.json"))
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(config, &parsed); err != nil {
		t.Fatal(err)
	}
	options := parsed["provider"].(map[string]any)["openrouter"].(map[string]any)["options"].(map[string]any)
	if options["baseURL"] != manifest.OpenAIBaseURL || options["apiKey"] != "{file:"+filepath.ToSlash(manifest.CredentialFile)+"}" {
		t.Fatalf("unexpected provider options: %v", options)
	}
	mcp := parsed["mcp"].(map[string]any)["mimir"].(map[string]any)
	if mcp["type"] != "local" || mcp["enabled"] != true {
		t.Fatalf("unexpected mimir MCP entry: %v", mcp)
	}
	mcpCommand := mcp["command"].([]any)
	if mcpCommand[0] != manifest.MCPCommand[0] || mcpCommand[1] != "serve" {
		t.Fatalf("unexpected MCP command: %v", mcpCommand)
	}
}

func TestUpsertOpenCodeConfigPreservesExistingConfigAndIsIdempotent(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "opencode.json")
	original := `{
  "$schema": "https://opencode.ai/config.json",
  "model": "openrouter/test-model",
  "provider": {"openrouter": {"options": {"timeout": 1000}, "models": {"test-model": {}}}},
  "mcp": {"gittrix": {"type": "local", "command": ["gittrix-mcp"], "enabled": true}}
}
`
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	manifest := testConnectionManifest(home)
	if err := upsertOpenCodeConfig(path, manifest); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := upsertOpenCodeConfig(path, manifest); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("idempotent upsert changed config bytes")
	}
	var parsed map[string]any
	if err := json.Unmarshal(second, &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed["$schema"] != "https://opencode.ai/config.json" || parsed["model"] != "openrouter/test-model" {
		t.Fatal("existing config fields were lost")
	}
	openrouter := parsed["provider"].(map[string]any)["openrouter"].(map[string]any)
	if openrouter["models"] == nil || openrouter["options"].(map[string]any)["timeout"] != float64(1000) {
		t.Fatal("existing provider configuration was lost")
	}
	mcp := parsed["mcp"].(map[string]any)
	if len(mcp) != 2 || mcp["gittrix"] == nil || mcp["mimir"] == nil {
		t.Fatalf("existing MCP configuration was not preserved: %v", mcp)
	}
}
