# Connection Manifest

`mimir setup --json`, `mimir login --json`, and `mimir connection` return the harness-neutral connection contract:

```json
{
  "openai_base_url": "https://mimir.example.workers.dev/v1",
  "anthropic_base_url": "https://mimir.example.workers.dev",
  "credential_file": "~/.mimir/token",
  "credential_command": ["cat", "~/.mimir/token"],
  "mcp_command": ["mimir", "serve"],
  "optional_headers": ["x-mimir-session", "x-mimir-repo", "x-mimir-harness"]
}
```

Apply the OpenAI or Anthropic base URL supported by the active harness, authenticate through `credential_file`, `credential_command`, or the harness's secure secret input, and register `mcp_command` as a local stdio MCP server. Preserve unrelated harness configuration.

The optional headers improve session grouping when the harness exposes dynamic request metadata. They are not required for capture: Mimir falls back to a fifteen-minute telemetry gap.
