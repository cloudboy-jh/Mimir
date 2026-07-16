---
name: mimir-use
description: Use the Mimir memory plane automatically before, during, and after agent work.
---

# Mimir Use

Mimir is agent infrastructure. Do not ask the user to run Mimir commands during normal work.

Before substantial work, call the Mimir `search` MCP tool with the problem, affected files, or error signature. Inspect relevant results with `sessions_get`. Use the returned evidence without narrating routine memory access.

The harness supplies transport metadata automatically:

```text
x-mimir-session: <stable-session-id when supported>
x-mimir-repo: <repository-name-or-url when supported>
x-mimir-harness: <harness-name>
x-mimir-git-ref: <branch-at-session-start when supported>
```

Exact session identity is optional. Harnesses without dynamic request headers use Mimir's inactivity fallback automatically.

Proxy use and a scheduled `x-mimir-capture` response header are not proof that an exchange was saved. Never report persistence from transport activity alone.

After meaningful work, when the exact session ID is available, call `session_status`. Report the session ID, saved exchange count, and last saved timestamp from that result. Do not add noisy status messages during routine tool use or when no meaningful unit of work has completed.

Set an outcome only when the completed work provides evidence. Use `session_set_outcome` with one canonical value:

- `landed`: the result was kept or shipped
- `discarded`: the result was deliberately rejected or reverted
- `abandoned`: work stopped without a result
- `unresolved`: no evidenced result is available

Include a concise reason and the supporting evidence. Capture state and work outcome are independent: a saved session can remain unresolved, and landed work is not proof that its exchanges were saved.

Code recall remains local. Use the harness's Mimir MCP tools rather than asking the user to operate the CLI.
