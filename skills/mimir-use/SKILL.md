---
name: mimir-use
description: Use the Mimir memory plane automatically before, during, and after agent work.
---

# Mimir Use

Mimir is agent infrastructure. Do not ask the user to run Mimir commands during normal work.

Before substantial work, search Mimir with the problem, affected files, or
error signature. Prefer `mimir search <query> --json` when shell execution is
available; otherwise use the MCP `search` tool. Inspect relevant results with
`mimir session get <id> --json` or MCP `sessions_get`. Use the returned evidence
without narrating routine memory access.

Mimir-owned adapters supply transport metadata automatically. Generic harness
integrations provide whichever dynamic values they support:

```text
x-mimir-session: <stable-session-id when supported>
x-mimir-repo: <repository-name-or-url when supported>
x-mimir-harness: <harness-name>
x-mimir-git-ref: <branch-at-session-start when supported>
x-mimir-request-kind: <primary|title|summary|compaction>
```

Exact session identity is optional. Harnesses without dynamic request headers use Mimir's inactivity fallback automatically.
Auxiliary model requests are infrastructure behavior; agents must not compensate for them through prompts, MCP calls, or guessed session IDs.

Hermes desktop and TUI use Mimir's installed transparent OpenRouter route and
bundled Hermes plugin. Do not create a custom provider. The proxy captures
OpenRouter turns; the plugin runs liveness-only on that route to avoid
duplicates, and reports turns for direct Nous, Anthropic OAuth, Codex, and
other provider transports that bypass the proxy.

Proxy use and a scheduled `x-mimir-capture` response header are not proof that an exchange was saved. Never report persistence from transport activity alone.

After meaningful work, when the exact session ID is available, run
`mimir session status <id> --json` or call MCP `session_status`. Both wait
briefly for background capture and return the authoritative receipt. When
dashboard Access is configured, the receipt includes `View session`. Let the
harness display that result near the completed response; do not repeat the
session ID, timestamp, counts, or receipt in agent prose unless the user
explicitly asks for storage details.

Treat `Saving to Mimir...`, `Partially saved`, and `Mimir couldn't save this session` as real user-visible states. Never rewrite them as saved. Do not call `session_status` during routine tool use or when no meaningful unit of work has completed.

Set an outcome only when completed work provides evidence. Use
`mimir session outcome <id> <value> --reason <text> --evidence <json> --json` or MCP
`session_set_outcome` with one canonical value:

- `landed`: the result was kept or shipped
- `discarded`: the result was deliberately rejected or reverted
- `abandoned`: work stopped without a result
- `unresolved`: no evidenced result is available

Include a concise reason and the supporting evidence. Capture state and work outcome are independent: a saved session can remain unresolved, and landed work is not proof that its exchanges were saved.

When the user explicitly asks to end, close, or finalize the session, use
`mimir session end <id> --json` or MCP `session_end` with the exact session ID.
Include the evidenced outcome and reason when available, then return its
receipt. Do not end a session merely because one task or response finished; an
ended exact session may be reactivated by later traffic.

Code recall remains local. Operate the CLI or MCP automatically rather than
asking the user to run routine memory commands.
