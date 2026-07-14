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

Call `mark` only when there is evidence for `promoted`, `discarded`, or `abandoned`. Leaving an outcome `unknown` is correct when the result is not yet durable.

Code recall remains local. Use the harness's Mimir MCP tools rather than asking the user to operate the CLI.
