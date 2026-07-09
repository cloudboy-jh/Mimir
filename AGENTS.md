# AGENTS.md

Operating manual for any coding agent **using** Mimir or working **on** this repo.

---

## Product vs skill

| | |
|---|---|
| **Use Mimir** | Install the skill. Do **not** clone this repo. |
| **Hack on Mimir** | Clone this repo (contributors only). |

Canonical skill install:

```bash
npx skills add cloudboy-jh/Mimir@mimir -g -y
```

Source of the skill: `skills/mimir/SKILL.md` in this repo (for packaging only).

Think Cloudflare / docs skills: knowledge + procedure land in the agent, not a second app checkout in the user’s projects.

---

## What Mimir is

Hermes remembers the **developer**.
Mimir remembers the **repo** and the **session**.

| plane | path | job |
|---|---|---|
| control | `~/.mimir/` | `config.toml` + `mimir.log` |
| session | `~/.mimir/sessions/` | markdown work log, private git sync |
| code | `<repo>/.mimir/` | index + recall (optional binary / MCP) |

Session push/pull does **not** depend on code index or `mimir serve`.

---

## Install path (agents)

### 1. Skill (always)

```bash
npx skills add cloudboy-jh/Mimir@mimir -g -y
```

Reload harness if skills are loaded only at session start.

### 2. Control + session

```bash
mimir control init
mimir session init
```

If the binary is missing, do the same with `gh` + `git` per the skill (private `login/mimir-sessions`, config under `~/.mimir/`). **Do not** ask the user to paste session remotes when `gh` works.

### 3. Code binary (only if index/recall/MCP needed)

```bash
go install github.com/cloudboy-jh/mimir/cmd/mimir@latest
```

Then per repo: `mimir index --full` · MCP `mimir serve` on the active harness only.

---

## User speak (daily)

| User says | You do |
|---|---|
| install / enable Mimir | skill add → control init → session init (binary only if code memory wanted) |
| save / checkpoint | session push + `◆ mimir` receipt |
| continue on machine X | session pull + open matching md |
| what do we know about Y | `mimir recall` / MCP |
| mid-coding structure | MCP quietly |

### Do not

- Clone `Mimir` just so the user can run agents with memory
- Web-search for random install blogs when the skill exists
- Ask for session APIs/URLs when `gh` is signed in
- Ask for hostname / harness / project when environment has them

### Only ask when

- No `gh` auth and sessions cannot be created safely
- Multi-account ambiguity (one short choice)
- Explicit custom session remote

---

## Receipts

```text
◆ mimir  <plane>.<verb>  <subject>
         <optional meaning>
         <ok|warn|fail> · <metric>
```

Fail: reason + `log: ~/.mimir/mimir.log`. Never dump `index.json`.

---

## Working on this repository

```bash
go test ./cmd/mimir
go build -o mimir ./cmd/mimir
```

- Module: `github.com/cloudboy-jh/mimir`
- CLI main: `./cmd/mimir`
- Spec: `spec.md`
- Skill published from: `skills/mimir/SKILL.md`

Design constraints: agent-primary, stdlib-first, no TUI/SaaS/telemetry for core, no Chiron brand, sessions never under project `.mimir/`.

When behavior changes: update `spec.md`, this file, and `skills/mimir/SKILL.md`. Keep README skill-first.
