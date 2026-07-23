# opencode Session Capture

OpenCode capture runs through the Mimir plugin at
[`plugins/opencode/mimir.ts`](../plugins/opencode/mimir.ts). It observes
completed turns inside the harness — above provider transport and
authentication — so every OpenCode provider is covered identically:
OpenRouter, the Zen subscription, Claude API keys, and Codex/ChatGPT OAuth.

## Install

Copy the plugin file into OpenCode's plugin directory:

```bash
# Global (all projects)
cp plugins/opencode/mimir.ts ~/.config/opencode/plugins/

# Or project-only
cp plugins/opencode/mimir.ts .opencode/plugins/
```

Uninstall is deleting the file. The plugin carries no credentials and no
configuration: it resolves the Worker URL and machine token from
`MIMIR_URL`/`MIMIR_TOKEN`, then `$MIMIR_HOME`, then `~/.mimir/config` and
`~/.mimir/token` as written by `mimir setup` or `mimir login`.

## What It Reports

All events go to `POST /sessions/:id/events` on the Worker and are owned by
the session Durable Object (see [`session-lifecycle.md`](session-lifecycle.md)):

- **Turn** — each completed assistant message (model, provider, token usage,
  latency), deduplicated by message ID.
- **Heartbeat** — every 60 seconds while the harness is active, plus on
  session create/update. This drives the dashboard liveness projection.
- **End** — best-effort on harness exit (SIGINT/SIGTERM) and on session
  deletion. If the process dies before delivery, the server-side silence
  timer finalizes the session within ~10 minutes regardless. Explicit end via
  `mimir session end` or the MCP `session_end` tool always works.

The plugin never throws into OpenCode: delivery failures are swallowed and
capture never interrupts the harness.

## Safety Boundary

Mimir does not automatically modify OpenCode configuration. OpenCode merges
JSON, JSONC, project, environment, and managed configuration, and rewriting
one guessed file can override user-owned provider, credential, MCP, plugin,
and command settings. The plugin is installed by the user copying one file
through OpenCode's supported plugin mechanism; the following commands never
write OpenCode files:

```bash
mimir setup
mimir login
mimir update
mimir doctor
```

Existing installations created by Mimir versions through v0.3.0 are not
automatically removed or restored because Mimir did not retain the prior user
values. Review any Mimir-created OpenCode files and provider/MCP entries
before keeping them.

`session_status` remains the authoritative proof that a real session was
saved.
