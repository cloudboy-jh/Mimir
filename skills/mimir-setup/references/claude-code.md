# Claude Code Wiring

Claude Code can use Mimir as its Anthropic-compatible gateway, but it does not expose a dynamic per-conversation inference-header hook. Sessions therefore use Mimir's heuristic boundary.

1. Back up `~/.claude/settings.json` before editing it.
2. Read the deployment URL from `mimir whoami`; do not read or print `~/.mimir/token`.
3. Preserve unrelated settings and merge:

```json
{
  "$schema": "https://json.schemastore.org/claude-code-settings.json",
  "apiKeyHelper": "cat \"$HOME/.mimir/token\"",
  "env": {
    "ANTHROPIC_BASE_URL": "https://YOUR-MIMIR-WORKER.workers.dev",
    "ANTHROPIC_CUSTOM_HEADERS": "x-mimir-harness: claude-code"
  }
}
```

`apiKeyHelper` supplies the local machine token without copying it into Claude settings. Mimir accepts either generated authentication header and replaces it before forwarding to OpenRouter.

4. Register MCP:

```bash
claude mcp add --scope user mimir -- mimir serve
```

5. Install `mimir-use` under `~/.claude/skills/mimir-use/SKILL.md` if it is not already discoverable.
6. Run `claude doctor` and correct any configuration validation errors.

Result: Claude Code uses heuristic session boundaries with static harness metadata.
