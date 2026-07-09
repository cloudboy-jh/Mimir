# Mimir Specification

Hermes remembers the developer. Mimir remembers the **repo** and the **session**.

Mimir is agent-driven infrastructure. Users talk to agents. Agents drive Mimir. Humans only touch a small config + log when something needs audit.

---

## Planes

| plane | root | format | engine |
|---|---|---|---|
| **code** | `<repo>/.mimir/` | index JSON (gitignored) | Go CLI / MCP (`index`, `recall`, `serve`, …) |
| **session** | `~/.mimir/sessions/` | `[machine]-[harness]-[id].md` | git + markdown; agent skill is enough |
| **control** | `~/.mimir/` | `config.toml`, `mimir.log` | agent writes; human rarely reads |

Chiron is retired as a brand. Session plane *is* what Chiron was.

---

## Core product rule

> Users never “use Mimir.” They use agents.
> Mimir leaves config + log so humans can trust what agents did.

### User speech (good)

- “Set up Mimir.”
- “Save progress before I switch machines.”
- “Continue what I was doing on thedeck.”
- “What do we know about auth in this repo?”

### User speech (bad)

- Pasting remotes the agent can discover
- Learning `mimir session push …` as daily vocabulary
- Filling install forms the environment already answers

**Don't ask the user for anything the environment already has** (git identity, `gh` login, current machine hostname heuristics, active harness, cwd project name).

---

## Install (agent-primary)

Install is three layers. Agent walks them. User confirms only real decisions.

### Layer 1 - Control plane (once per machine)

Triggered by: “Set up Mimir” / first skill load with missing config.

Agent:

1. Creates `~/.mimir/` if missing
2. Writes minimal `config.toml`
3. Ensures log path exists
4. Derives `machine` without asking when possible:
   - hostname / known aliases (`therig`, `thedeck`) if already mapped
   - else short hostname
   - only ask if collision or preference conflict
5. Logs: `control.init ok path=… machine=…`

No user paste required.

### Layer 2 - Session plane (once per person, shared across machines)

Triggered automatically when control plane is new and user is coding / has git provider auth.

**Do not ask for a GitHub URL first.**

#### Discovery order (agent)

1. Read `sessions.repo` from config if already set → clone/pull `~/.mimir/sessions`, done
2. Detect signed-in git provider:
   - `gh auth status` / `gh api user` (GitHub)
   - equivalent for other providers only if already authenticated
3. If authenticated:
   - default private repo name: `mimir-sessions` under their account/org (or `mimir-sessions` under detected login)
   - if repo **exists**: clone it
   - if repo **missing**: `gh repo create <login>/mimir-sessions --private --description "Mimir agent session sync" --confirm` then clone
4. Write remote into config, enable sessions, log receipt
5. Install/enable Mimir skill for the active harness

#### Only ask the user when

- No git auth anywhere and sessions cannot be created safely
- Multiple guis / orgs / accounts and choice is ambiguous (**one** terse choice, not a form)
- They explicitly want symlink/path overrides or a non-GitHub remote already they own under a custom name

**Never** make “paste `git@github.com:…`” the default first step for a developer who is already signed into git.

#### Refusing bad targets

- Refuse public repos for sessions
- Refuse using an existing application monorepo as the sessions remote
- Never store tokens in config (use existing `gh`/credential helper)

### Layer 3 - Code plane engine (opportunistic)

Triggered by: user wants repo memory, or agent needs index for coding work.

Agent order:

1. `mimir` already on PATH?
2. Harness/toolchain cache?
3. Install release binary or `go install` if Go present
4. Register MCP only for the **active** harness
5. Full index once if `.mimir/` missing; incremental afterwards
6. Log receipt

If binary install fails: **sessions + control still work**. Report degradation; don't fail whole product setup.

---

## Gold-path dialogue (zero paste)

**User:** Set up Mimir.

**Agent (on therig, gh = cloudboy-jh):**
Control plane ready (`machine=therig`). You're signed into GitHub as `cloudboy-jh` - I created/cloned private `cloudboy-jh/mimir-sessions` into `~/.mimir/sessions` and enabled session sync. Code indexer: install + index this repo?

**User:** Yes.
**Agent:** MCP wired, full index done. Receipts in `~/.mimir/mimir.log`.

**User (later):** Save progress.
**Agent:** Writes `therig-hermes-<slug>.md`, commits, pushes. Confirms with one log line.

**User on thedeck:** Set up Mimir / continue yesterday.
**Agent:** Same login → same remote clone/pull, different `machine`, restore without asking for a URL.

---

## Config (human UI - keep tiny)

```toml
# ~/.mimir/config.toml
machine = "therig"

[sessions]
enabled = true
repo = "https://github.com/cloudboy-jh/mimir-sessions.git"  # written by agent after create/clone
path = "~/.mimir/sessions"
# harness is per-write from the active agent; optional default only:
# default_harness = "hermes"

[code]
prefer_mcp = true
auto_index_if_stale = true

[log]
path = "~/.mimir/mimir.log"
level = "info"
```

Few keys. Readable in <20 seconds. Agent owns most writes.

---

## Session document

Filename: `{machine}-{harness}-{session_id}.md`

```markdown
---
session_id: gittrix-v2-refactor
machine: therig
harness: hermes
project: gittrix
timestamp: 2026-07-09T10:00:00-07:00
status: active
---

# Session: …

## Current Goal
…

## State Variables
…

## Progress
- [ ] …

## Context Brief
…
```

Push/pull is **intent-driven** (“save / restore / continue from X”), not CLI-driven by the user.

---

## Log (human audit)

Append-only. Examples:

```
2026-07-09T10:12:01Z  control.init     machine=therig ok
2026-07-09T10:12:08Z  session.init     create cloudboy-jh/mimir-sessions private ok
2026-07-09T10:12:10Z  session.clone    path=~/.mimir/sessions ok
2026-07-09T10:14:22Z  code.index       glib-code incremental files=12 84ms
2026-07-09T10:15:01Z  session.push     therig-hermes-gittrix-v2 ok sha=abc123
```

---

## Surfaces summary

| surface | who | when |
|---|---|---|
| Agent skill / MCP | primary driver | daily |
| config.toml | human + agent | rare |
| mimir.log | human audit | on doubt |
| `mimir status/doctor` | agent first | debug |
| thin CLI session subcommands | optional scaffolding | never required UX |

---

## Inline chat surface

User-readable receipts in agent chat (like a clean diff header - not MCP JSON, not a robot dump).

### Mark (locked)

```text
◆ mimir
```

- Always leading.
- Exactly that sequence: diamond + space + lowercase `mimir`.
- One mark. No second glyph, no emoji stack, no ASCII box art for routine events.
- Skimmable: any line starting with `◆ mimir` is a memory-bus event.

### Shape

```text
◆ mimir  <plane>.<verb>  <subject>
         <optional human meaning line>
         <ok|warn|fail> · <hash|ms|metric>
```

One line when enough; second/third only for meaning or metrics. Never paste full `index.json`.

### Verbs

| plane | verb | when user sees it |
|---|---|---|
| `session` | `push` | user asked to save / agent saved on request |
| `session` | `pull` | user asked to continue / restore |
| `session` | `init` | first bind of sessions remote |
| `code` | `index` | index ran (prefer one-liner if agent-initiated) |
| `code` | `recall` | only if answer is for the user, not pure internal |
| `control` | `init` | first machine setup |

### Templates

```text
◆ mimir  session.push  therig-hermes-gittrix-v2
         goal: Durable/Eph adapter split
         ok · abc1234

◆ mimir  session.pull  thedeck-opencode-gittrix-v2
         machine: thedeck → therig
         ok · restored

◆ mimir  code.index  glib-code  incremental
         +12 files · 84ms · sha 7d58e16

◆ mimir  code.recall  SandboxAdapter
         src/core.ts:12  interface SandboxAdapter
         src/adapters/local.ts:4  LocalSandbox

◆ mimir  control.init  machine=therig
         sessions on · code mcp optional
         ok

◆ mimir  session.push  fail
         reason: no gh auth · sessions disabled
         log: ~/.mimir/mimir.log
```

### Placement

- User intent (save / continue / setup) → always show receipt.
- Background index/recall mid-coding → one line unless the user asked about structure.
- Failures always explicit with `fail` + reason + log pointer.
- Discord / gateways: same mark; multi-line inside a fenced code block so spacing holds.
- `mimir.log` lines are the durable twin (timestamped); chat is the ephemeral projection. Log may use plain text without the diamond if needed; chat prefers `◆ mimir`.

### Non-goals for the mark

- Alternate monograms (`[m]`, `::m::`, multi-line logos) as routine UI
- Brand headers taller than 3 lines
- Color as the only signal (eye should still work in plain mono)

---

## Non-goals

- Second brand for sessions (Chiron)
- TUI / install wizard
- Asking developers to paste remotes when `gh` (or equivalent) already works
- SaaS, accounts, telemetry for core
- Folding sessions into project `.mimir/`
- Making session push depend on code index or `mimir serve`

---

## Implementation order

1. This spec as source of truth
2. Control plane paths + config schema + log format
3. Session skill: discover/`gh create`/clone/push/pull - agent-first
4. Go binary: keep for code plane only
5. README framed as agent dialogue + short manual appendix for non-agent installs

## CLI scaffolding (optional, agent-facing)

Thin commands exist so agents and tests have a stable surface. Users never need them as daily vocabulary.

```bash
mimir control init [--machine NAME]
mimir control status
mimir session init [--repo URL]   # prefers gh discovery
mimir session push --id SLUG [--harness NAME] [--project NAME] [--goal TEXT] [--body PATH|-]
mimir session pull [--id SLUG]
mimir session list
mimir status
mimir doctor
mimir index [--full]
mimir recall <query>
mimir serve
```
