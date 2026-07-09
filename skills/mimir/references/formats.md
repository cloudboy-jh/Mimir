# Binary-less formats

Use these when the `mimir` CLI is not installed. Write the files yourself with `gh` + `git`.

## config.toml

Path: `~/.mimir/config.toml`

```toml
machine = "therig"

[sessions]
enabled = true
repo = "https://github.com/<login>/mimir-sessions.git"
path = "~/.mimir/sessions"
# default_harness = "hermes"

[code]
prefer_mcp = true
auto_index_if_stale = true

[log]
path = "~/.mimir/mimir.log"
level = "info"
```

Rules:

- `machine` = short hostname; map to `therig` / `thedeck` if the hostname matches.
- `repo` is written only after the private `mimir-sessions` remote is created/cloned.
- `enabled = false` until sessions are actually bound.
- Never put tokens here. Auth comes from `gh` / git credential helper.

## Session document

Path: `~/.mimir/sessions/{machine}-{harness}-{session_id}.md`

```markdown
---
session_id: gittrix-v2-refactor
machine: therig
harness: hermes
project: gittrix
timestamp: 2026-07-09T10:00:00-07:00
status: active
---

# Session: gittrix / gittrix-v2-refactor

## Current Goal
Split the Durable/Ephemeral sandbox adapter.

## State Variables
- branch: feat/adapter-split
- entrypoint: src/core.ts

## Progress
- [x] Extract SandboxAdapter interface
- [ ] Wire LocalSandbox
- [ ] Tests

## Context Brief
Anything the next machine needs to resume without re-reading the whole repo.
```

Frontmatter rules:

- `session_id`: alnum, dash, underscore, dot. No spaces.
- `harness`: the active agent (`hermes`, `opencode`, ...); fall back to `agent`.
- `timestamp`: RFC3339.
- `status`: `active` while in progress.

## Log line

Path: `~/.mimir/mimir.log` (append-only)

```
2026-07-09T10:15:01Z  session.push     therig-hermes-gittrix-v2 ok sha=abc123
```

Shape: `<rfc3339>  <plane.verb>  <detail>  <ok|warn|fail>`

## Manual session flow (no binary)

```bash
# init once per person
gh api user --jq .login                       # -> LOGIN
gh repo view LOGIN/mimir-sessions >/dev/null 2>&1 \
  || gh repo create LOGIN/mimir-sessions --private --description "Mimir agent session sync"
git clone https://github.com/LOGIN/mimir-sessions.git ~/.mimir/sessions
# then write ~/.mimir/config.toml with repo + enabled=true

# save
# write the {machine}-{harness}-{id}.md file, then:
git -C ~/.mimir/sessions add .
git -C ~/.mimir/sessions commit -m "session: {machine}-{harness}-{id}"
git -C ~/.mimir/sessions push

# restore
git -C ~/.mimir/sessions pull --ff-only
```

Refuse public remotes and application monorepos as session targets.
