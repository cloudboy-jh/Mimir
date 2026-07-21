package mimircli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallOpenCodeIntegration(t *testing.T) {
	home := t.TempDir()
	if err := installOpenCodeIntegration(home, "https://mimir.example.workers.dev"); err != nil {
		t.Fatal(err)
	}
	plugin, err := os.ReadFile(filepath.Join(home, ".config", "opencode", "plugins", "mimir.ts"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(plugin), `WORKER_URL = "https://mimir.example.workers.dev"`) {
		t.Fatal("plugin does not carry the worker URL")
	}
	if strings.Contains(string(plugin), "readFileSync(join(homedir(), \".mimir\", \"token\"), \"utf8\")") == false {
		t.Fatal("plugin does not read the machine token at runtime")
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
	mcp := parsed["mcp"].(map[string]any)["mimir"].(map[string]any)
	if mcp["type"] != "local" || mcp["enabled"] != true {
		t.Fatalf("unexpected mimir MCP entry: %v", mcp)
	}
	mcpCommand := mcp["command"].([]any)
	if mcpCommand[0] != "mimir" || mcpCommand[1] != "serve" {
		t.Fatalf("unexpected MCP command: %v", mcpCommand)
	}
}

func TestUpsertOpenCodeMCPPreservesExistingConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "opencode.json")
	original := `{
  "$schema": "https://opencode.ai/config.json",
  "model": "openrouter/test-model",
  "mcp": {
    "gittrix": { "type": "local", "command": ["gittrix-mcp"], "enabled": true }
  }
}
`
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := upsertOpenCodeMCP(path); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatal(err)
	}
	if parsed["$schema"] != "https://opencode.ai/config.json" || parsed["model"] != "openrouter/test-model" {
		t.Fatal("existing config fields were lost")
	}
	mcp := parsed["mcp"].(map[string]any)
	if _, ok := mcp["gittrix"]; !ok {
		t.Fatal("existing MCP server was removed")
	}
	if _, ok := mcp["mimir"]; !ok {
		t.Fatal("mimir MCP entry missing after upsert")
	}
	if len(mcp) != 2 {
		t.Fatalf("upsert duplicated MCP entries: %v", mcp)
	}
}
