# Connection Manifest

`mimir setup --json`, `mimir login --json`, and `mimir connection` return the
harness-neutral connection contract:

```json
{
  "openai_base_url": "https://mimir.example.workers.dev/v1",
  "anthropic_base_url": "https://mimir.example.workers.dev",
  "credential_file": "/Users/example/.mimir/token",
  "credential_command": ["cat", "/Users/example/.mimir/token"],
  "optional_headers": ["x-mimir-session", "x-mimir-repo", "x-mimir-harness", "x-mimir-git-ref", "x-mimir-request-kind"]
}
```

Paths are resolved absolute paths and may differ when `MIMIR_HOME` is set.
Apply the OpenAI or Anthropic base URL supported by the active harness. Supply
authentication through `credential_file`, `credential_command`, or the
harness's secure secret input without printing or copying the credential value
into ordinary configuration.

Preserve unrelated harness configuration and validate the result with the
harness's native command or schema.

Optional headers improve session identity and grouping when the harness exposes
dynamic request metadata:

- `x-mimir-session`: stable session ID
- `x-mimir-repo`: repository name or URL
- `x-mimir-harness`: harness name
- `x-mimir-git-ref`: branch or source reference at session start
- `x-mimir-request-kind`: `primary`, `title`, `summary`, or `compaction`; only
  `primary` can establish session intent

The manifest covers capture. To let an unsupported harness query Mimir, add a
stdio MCP server named `mimir` that runs `<absolute path to mimir> mcp`; it
uses the same machine token.

The bundled integrations install their required values automatically. Without
an exact session ID, Mimir groups proxied traffic by repository, harness, and
device with a configurable inactivity gap that defaults to 15 minutes.
