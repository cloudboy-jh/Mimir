# OpenCode Session Capture

OpenCode capture runs through the Mimir plugin at
[`plugins/opencode/mimir.ts`](../plugins/opencode/mimir.ts). It observes
completed turns inside the harness, above provider transport and
authentication. OpenRouter traffic remains canonical at the Worker proxy. For
the Zen subscription, Claude API keys, Codex/ChatGPT OAuth, and other direct
providers, the plugin reads the completed user and assistant records from
OpenCode's session store and uploads a bounded reconstructed exchange.

## Install

```bash
mimir install --harness opencode
```

The managed targets are `~/.config/opencode/plugins/mimir.ts` and the
`mimir-setup` and `mimir-use` skills under `~/.config/opencode/skills/`. Mimir
records them in `$MIMIR_HOME/managed.json` (default `~/.mimir/managed.json`).
Different existing content is a conflict and is preserved; a managed file
modified after installation is also preserved. Symlinked targets are rejected.
Restart OpenCode to load the plugin.

Manual copying from [`plugins/opencode/mimir.ts`](../plugins/opencode/mimir.ts)
is a recovery path only. The plugin carries no credentials or configuration:
it resolves the Worker URL and machine token from `MIMIR_URL`/`MIMIR_TOKEN`,
then `$MIMIR_HOME`, then `~/.mimir/config` and `~/.mimir/token` as written by
`mimir setup` or `mimir login`.

## What It Reports

Lifecycle events go to `POST /sessions/:id/events` (see
[`session-lifecycle.md`](session-lifecycle.md)):

- **Turn** — each completed assistant message (model, provider, token usage,
  latency), deduplicated by message ID.
- **Heartbeat** — every 60 seconds while the harness is active, plus on
  session create and update, with title and parent session.
- **End** — on `session.deleted`. If the process exits first, the session
  expires after `session.gap_minutes` of silence. `mimir session end <id>`
  always works.

For a completed non-OpenRouter turn, `POST /sessions/:id/exchanges` receives
the reconstructed user and assistant parts, tool parts, model/provider, token
counts, timing, finish reason, and reported error when OpenCode exposes them.
Payloads are bounded to 512 KiB and individual strings to 64 KiB; parts may be
trimmed to fit. This is a redacted durable R2 exchange with searchable D1
metadata, not a byte-for-byte provider request or response. If the
session-store read or delivery fails, only the turn event may remain.

After a shell tool whose output contains a `git commit` confirmation, the
plugin runs `mimir _hook git` with that output. On `session.idle` and
`session.deleted` it runs it with `refresh: true` to mark newly pushed commits.
The binary is found through `MIMIR_BIN`, then `executable=` in
`$MIMIR_HOME/config`, then `mimir` on `PATH`.

The plugin never throws into OpenCode: delivery failures are swallowed and
capture never interrupts the harness.

## Tools

The plugin registers `mimir_search`, `mimir_sessions`, `mimir_session`,
`mimir_turns`, `mimir_stats`, `mimir_day`, `mimir_devices`, `mimir_commits`,
and `mimir_commit_patch`. Each forwards a `tools/call` to the Worker's `/mcp`
endpoint with the machine token and returns the same JSON as the MCP tool of
the same name (see [Spec §4.4](Spec.md#44-mcp)).

## Safety Boundary

Mimir does not modify general OpenCode configuration. OpenCode merges JSON,
JSONC, project, environment, and managed configuration, and rewriting one
guessed file can override user-owned provider, credential, plugin, and command
settings. Install and update create or refresh only the exact plugin and skill
files recorded in `managed.json`. `mimir doctor` checks state without taking
ownership of conflicting files.

`mimir uninstall` removes managed plugin and skill files whose bytes still
match, preserving modified, missing, unowned, non-regular, and symlinked paths.
It does not remove OpenCode directories or configuration. The local Mimir
connection and Cloudflare deployment remain.

`mimir session status <id>` is the receipt that shows whether a real session
was saved.
