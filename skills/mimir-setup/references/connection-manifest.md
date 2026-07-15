# Connection Manifest

`mimir setup --json`, `mimir login --json`, and `mimir connection` return the
harness-neutral connection contract:

```json
{
  "openai_base_url": "https://mimir.example.workers.dev/v1",
  "anthropic_base_url": "https://mimir.example.workers.dev",
  "credential_file": "/Users/example/.mimir/token",
  "credential_command": ["cat", "/Users/example/.mimir/token"],
  "mcp_command": ["/Users/example/go/bin/mimir", "serve"],
  "optional_headers": ["x-mimir-session", "x-mimir-repo", "x-mimir-harness"]
}
```

Paths are resolved absolute paths and may differ when `MIMIR_HOME` is set.
Apply the OpenAI or Anthropic base URL supported by the active harness. Supply
authentication through `credential_file`, `credential_command`, or the
harness's secure secret input without printing or copying the credential value
into ordinary configuration.

Register `mcp_command` as a local stdio MCP server. Preserve unrelated harness
configuration and validate the result with the harness's native command or
schema.

Optional headers improve session identity and grouping when the harness exposes
dynamic request metadata:

- `x-mimir-session`: stable session ID
- `x-mimir-repo`: repository name or URL
- `x-mimir-harness`: harness name

The Worker also accepts `x-mimir-git-ref` when a harness can provide the source
branch. Without an exact session ID, Mimir falls back to repository/harness
grouping with a configurable inactivity gap that defaults to 15 minutes.
