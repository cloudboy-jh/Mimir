# opencode Session Capture

Mimir does not automatically modify OpenCode configuration. This is a hard
safety boundary: OpenCode merges JSON, JSONC, project, environment, and managed
configuration, and rewriting one guessed file can override user-owned provider,
credential, MCP, plugin, and command settings.

The following commands never write OpenCode files:

```bash
mimir setup
mimir login
mimir update
mimir doctor
```

Run `mimir connection` to print the harness-neutral Worker URLs, local
credential source, absolute MCP command, and optional metadata header names.
Apply those values through OpenCode's supported configuration flow. Mimir will
not commandeer the built-in `openrouter` provider, replace `mcp.mimir`, or write
plugins and commands on the user's behalf.

Existing installations created by Mimir versions through v0.3.0 are not
automatically removed or restored because Mimir did not retain the prior user
values. Review any Mimir-created OpenCode files and provider/MCP entries before
keeping them.

`session_status` remains the authoritative proof that a real session was saved.
