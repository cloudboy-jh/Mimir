---
name: mimir-use
description: Use the Mimir memory plane before, during, and after agent work.
---

# Mimir Use

Before work, search prior attempts by problem, affected files, errors, or repository:

```bash
mimir search "query"
```

Set these headers on model requests routed through Mimir:

```text
x-mimir-session: <stable-session-id>
x-mimir-repo: <repository-name-or-url>
x-mimir-harness: <harness-name>
x-mimir-git-ref: <branch-at-session-start>
```

The session header is authoritative. Without it, Mimir groups requests heuristically by repository and time gap.

After work, mark the outcome:

```bash
mimir mark <session-id> promoted
```

Valid outcomes are `promoted`, `discarded`, `abandoned`, and `unknown`.

Use `mimir recall` only for the local code index. Use `mimir search` for remote session memory.
