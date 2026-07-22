package mimircli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const openCodeAdapterVersion = 1

const openCodePlugin = `import type { Plugin } from "@opencode-ai/plugin";

// mimir-opencode-adapter-version: 1
export default (async ({ directory }) => {
  const repo = directory.split(/[\\/]/).filter(Boolean).pop() ?? "unknown";
  return {
    "chat.headers": async (input, output) => {
      if (input.model.providerID !== "openrouter" && input.model.providerID !== "mimir") return;

      const requestKind = input.agent === "title" || input.agent === "summary" || input.agent === "compaction"
        ? input.agent
        : "primary";
      output.headers["x-mimir-session"] = input.sessionID;
      output.headers["x-mimir-repo"] = repo;
      output.headers["x-mimir-harness"] = "opencode";
      output.headers["x-mimir-request-kind"] = requestKind;
    },
  };
}) satisfies Plugin;
`

const openCodeEndSessionCommand = `---
description: Summarize, record the outcome, and end the current Mimir session.
---

End the current Mimir session.

1. Use $ARGUMENTS as the required exact session ID. If it is empty, stop and ask for the ID. Never guess an ID.
2. Summarize the completed work and gather concrete evidence such as tests, deployments, pull requests, or commit SHAs.
3. Choose landed, discarded, abandoned, or unresolved based only on that evidence.
4. Call the Mimir MCP session_end tool with the session ID, outcome, concise reason, and evidence.
5. Return the session_end receipt exactly. Do not claim the session was saved unless the receipt says it was saved.
`

func installCurrentOpenCodeIntegration() error {
	pointer, err := loadPointer()
	if err != nil {
		return err
	}
	manifest, err := currentConnectionManifest(pointer.URL)
	if err != nil {
		return err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	return installOpenCodeIntegration(home, manifest)
}

// installOpenCodeIntegration installs Mimir-owned OpenCode files and upserts
// only the provider and MCP values that Mimir owns.
func installOpenCodeIntegration(home string, manifest connectionManifest) error {
	configDir := filepath.Join(home, ".config", "opencode")
	if err := os.MkdirAll(filepath.Join(configDir, "plugins"), 0o700); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Join(configDir, "commands"), 0o700); err != nil {
		return err
	}
	if err := writeFileIfChanged(filepath.Join(configDir, "plugins", "mimir.ts"), []byte(openCodePlugin), 0o600); err != nil {
		return fmt.Errorf("writing opencode plugin: %w", err)
	}
	if err := writeFileIfChanged(filepath.Join(configDir, "commands", "mimir-end-session.md"), []byte(openCodeEndSessionCommand), 0o600); err != nil {
		return fmt.Errorf("writing opencode command: %w", err)
	}
	return upsertOpenCodeConfig(filepath.Join(configDir, "opencode.json"), manifest)
}

func upsertOpenCodeConfig(path string, manifest connectionManifest) error {
	config := map[string]any{}
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, &config); err != nil {
			return fmt.Errorf("reading opencode config: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	providers := object(config, "provider")
	openrouter := object(providers, "openrouter")
	options := object(openrouter, "options")
	options["baseURL"] = manifest.OpenAIBaseURL
	options["apiKey"] = "{file:" + filepath.ToSlash(manifest.CredentialFile) + "}"
	openrouter["options"] = options
	providers["openrouter"] = openrouter
	config["provider"] = providers

	mcp := object(config, "mcp")
	mcp["mimir"] = map[string]any{
		"type":    "local",
		"command": manifest.MCPCommand,
		"enabled": true,
	}
	config["mcp"] = mcp

	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return writeFileIfChanged(path, append(data, '\n'), 0o600)
}

func object(parent map[string]any, key string) map[string]any {
	value, _ := parent[key].(map[string]any)
	if value == nil {
		value = map[string]any{}
	}
	return value
}

func writeFileIfChanged(path string, data []byte, mode os.FileMode) error {
	if current, err := os.ReadFile(path); err == nil && bytes.Equal(current, data) {
		return os.Chmod(path, mode)
	}
	temp, err := os.CreateTemp(filepath.Dir(path), ".mimir-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)
	if err := temp.Chmod(mode); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempPath, path)
}
