---
name: mimir-use
description: Use the Mimir memory plane before, during, and after agent work.
---

# Mimir Use

Before work, search prior attempts by problem, affected files, errors, or repository:

```bash
mimir search "query"
```

Harness setup is responsible for transport headers. Do not claim a skill can inject them by itself:

```text
x-mimir-session: <stable-session-id>
x-mimir-repo: <repository-name-or-url>
x-mimir-harness: <harness-name>
x-mimir-git-ref: <branch-at-session-start>
```

The session header is authoritative. OpenCode's Mimir plugin injects it dynamically. Harnesses without a dynamic inference-header hook are grouped heuristically by repository and time gap.

After work, mark the outcome:

```bash
mimir mark <session-id> promoted
```

Valid outcomes are `promoted`, `discarded`, `abandoned`, and `unknown`.

Use `mimir recall` only for the local code index. Use `mimir search` for remote session memory.
