# Session plane

Root: `~/.mimir/sessions/` (configured via control `sessions.path`).

## Document name

`{machine}-{harness}-{session_id}.md`

## Frontmatter

```yaml
---
session_id: gittrix-v2-refactor
machine: therig
harness: hermes
project: gittrix
timestamp: 2026-07-09T10:00:00-07:00
status: active
---
```

Body sections: Current Goal, State Variables, Progress, Context Brief.

## Agent flow

1. `mimir session init` — discover/create/clone private `mimir-sessions` via `gh`; never default to “paste remote”.
2. `mimir session push --id …` — write markdown, commit, push.
3. `mimir session pull` — fetch latest; restore without URL paste on a second machine.

## Refusals

- Public session remotes
- Application monorepos as session remotes
- Tokens in config

Push/pull do **not** require code plane index or `mimir serve`.
