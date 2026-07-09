# AGENTS.md

This file is the operating manual for any coding agent working **with** or **on** Mimir.

Read this first. Prefer natural language intent over raw CLI. Users talk to you; you drive Mimir.

---

## What Mimir is

Hermes remembers the **developer**.
Mimir remembers the **repo** and the **session**.

Three planes:

| plane   | path                 | job                                              |
|---------|----------------------|--------------------------------------------------|
| control | `~/.mimir/`          | `config.toml` + `mimir.log` (human audit surface) |
| session | `~/.mimir/sessions/` | markdown work log, synced via private git        |
| code    | `<repo>/.mimir/`     | index + recall (CLI / MCP)                       |

**Hard rule:** session push/pull does **not** depend on code index or `mimir serve`.
If the binary is missing, control + sessions can still work via this skill + git/`gh`.

---

## User speak → what you do

| User says | You do |
|-----------|--------|
| "Set up Mimir" | Full install path below (control → session → optional code) |
| "Save progress" / "checkpoint" | Session write + push. Always show `◆ mimir` receipt. |
| "Continue what I was doing on X" | Session pull, open matching session file(s), resume. |
| "What do we know about Y in this repo?" | Code recall (`mimir recall` or MCP). Receipt only if user-facing. |
| Mid-coding structure lookup | MCP silently if fine; one-line receipt only if useful. |

### Do not ask for

- GitHub clone URLs when `gh` is signed in
- Hostnames when hostname is available
- Harness names when the active agent harness is known
- Project names when cwd/repo name is known
- Install form fields the environment already answers

### Only ask when

- No `gh` (or equivalent) auth and sessions cannot be created safely
- Multiple accounts/orgs and the choice is ambiguous (**one** short choice)
- Explicit custom remote / path override requested

---

## Install (zero paste)

Run in order. User confirms only real decisions.

### 1. Control plane (once per machine)

```bash
mimir control init
# optional override:
# mimir control init --machine therig
```

If binary missing: create `~/.mimir/`, write minimal `config.toml` (machine = short hostname; map `therig` / `thedeck` when hostname matches), touch log. Same outcome.

Log: `control.init machine=… path=… ok`

### 2. Session plane (once per person)

```bash
mimir session init
```

Discovery order (CLI already does this; mirror if bare-handed):

1. `sessions.repo` already in config → clone/pull to `~/.mimir/sessions`
2. Else `gh api user` → login
3. If `login/mimir-sessions` exists → clone (must be **private**)
4. If missing → `gh repo create login/mimir-sessions --private --description "Mimir agent session sync"` then clone
5. Write remote into config, `sessions.enabled = true`

**Refuse:** public session remotes, application monorepos as session remotes, tokens in config.

```bash
# only if user owns a custom private remote already and insists:
mimir session init --repo <url>
```

### 3. Code plane (optional, per repo)

```bash
# binary: PATH → go install / release → else degrade, keep sessions
mimir index --full    # first time in this repo
# later:
mimir index           # incremental
```

Register MCP only for the **active** harness if the user wants repo memory:

```json
{
  "mcpServers": {
    "mimir": {
      "command": "mimir",
      "args": ["serve"]
    }
  }
}
```

---

## Daily session ops

```bash
# save (write markdown, commit, push)
mimir session push \
  --id <slug> \
  --harness <hermes|opencode|…> \
  --project <name> \
  --goal "<one line>" \
  --body notes.md

# restore
mimir session pull
mimir session pull --id <slug>
mimir session list
```

Session filename: `{machine}-{harness}-{session_id}.md`

Fill real content: current goal, state vars, progress checkboxes, context brief. Do not leave empty templates as the final "save."

---

## Chat receipts (locked)

Every user-facing plane event starts with exactly:

```text
◆ mimir
```

Shape:

```text
◆ mimir  <plane>.<verb>  <subject>
         <optional meaning>
         <ok|warn|fail> · <metric>
```

Examples:

```text
◆ mimir  control.init  machine=therig
         sessions on · code mcp optional
         ok

◆ mimir  session.push  therig-hermes-gittrix-v2
         goal: adapter split
         ok · abc1234

◆ mimir  session.push  fail
         reason: no gh auth · sessions disabled
         log: ~/.mimir/mimir.log
```

Failures always: `fail` + reason + log pointer.
Never dump full `index.json` into chat.

Durable twin of every action: `~/.mimir/mimir.log`.

---

## Debug

```bash
mimir status   # control + repo store snapshot
mimir doctor   # home, config, sessions, git, gh, store
```

---

## Working on this repository

Module: `github.com/cloudboy-jh/mimir`  
Main package: `./src`  
Tests: `go test ./src`  
Build: `go build -o mimir ./src`

Spec is source of truth: `spec.md`  
Skill (same content family as this file): `skills/mimir/SKILL.md`

### Design constraints

- Agent-primary. Thin CLI is scaffolding, not daily human vocabulary.
- No TUI, no install wizard forms, no SaaS accounts, no telemetry for core.
- No second brand for sessions (Chiron is retired).
- Sessions never live under project `.mimir/`.
- Stdlib-first. Do not add deps for small jobs.
- Keep `config.toml` tiny and human-readable in under 20 seconds.

### When changing behavior

1. Update `spec.md` if contract changes.
2. Update this file + `skills/mimir/SKILL.md` if agent procedure changes.
3. Keep README agent-dialogue first; CLI in the manual appendix only.
4. Tests for control/session path + format stay green: `go test ./src`.

---

## Quick start script (agent)

```bash
# 1) ensure binary (optional if only doing sessions via git)
command -v mimir >/dev/null || go build -o mimir ./src

# 2) control + sessions
mimir control init
mimir session init

# 3) optional code memory for cwd repo
mimir doctor
mimir index --full
```

Then tell the user what happened with `◆ mimir` receipts. Done.
