# Mimir

![Mimir](./mimir-readme.png)

Agent-driven code and session memory.

The agent keeps an optional map of the repo, and a working session (goal, progress, context). Sessions sync through your own private git so work continues across machines. You talk; the agent runs it; `~/.mimir` is there when you want to audit.

## User Guide

Mimir gives your agent two independent capabilities: **Session Checkpoints** (tracking what you are doing) and **Repository Memory** (indexing what the code looks like).

### 1. Sessions (Your Work Checkpoints)

Sessions are structured markdown files that track your open work narratives (Goal, Progress, State Variables, Context Briefs). They are completely decoupled from your durable codebase history, saving straight to your machine and syncing to your private git branch (`mimir-sessions`).

*   **When to use**: Before switching machines, taking a break, or handing off work to another model.
*   **What to tell your agent**:
    *   *"Save progress"* -> The agent writes a custom session file and pushes it to your private git.
    *   *"Continue what I was doing"* -> The agent pulls latest logs from your remote, lists open sessions, and restores the context.
*   **Durable Twin**: All pushes write cleanly to `~/.mimir/sessions/<machine>-<harness>-<session-id>.md`.

### 2. Repos (Your Code plane Memory)

The Code plane operates strictly on git repository roots. By compiling a local index of your symbols, dependencies, and imports, it gives your agent instant codebase search without sending massive code payloads in the chat prompt.

*   **When to use**: Onboarding an agent to a new repository, or debugging complex symbols, imports, and downstream dependencies.
*   **What to tell your agent**:
    *   *"Index this repository"* -> The agent compiles a private search index under your gignored `<repo>/.mimir/index.json`.
    *   *"What do we know about OAuth/Symbols?"* -> The agent runs instant recall queries to extract relevant hunks without bloating memory limits.

---

## Install (Agent-driven)

### Agent prompt (paste this)

```text
Install and wire Mimir for me.

1. Install the skill (not a product clone):
   npx skills add <repository-source>@mimir -g -y
   PromptScript only (no global): npx skills add <repository-source>@mimir -a promptscript -y
2. Load / follow skills/mimir (or the installed skill). Bootstrap control + session.
3. Do not ask for a session GitHub URL. Use gh auth → private login/mimir-sessions.
4. Sessions use git + gh. Binary is optional (code index / recall / MCP only).
5. Confirm with a ◆ mimir receipt and doctor/status if available. Reload harness if skills load only at session start.
```

### Skill command

```bash
npx skills add <repository-source>@mimir -g -y
```

PromptScript has no global scope — install per project:

```bash
npx skills add <repository-source>@mimir -a promptscript -y
```

Skill only for sessions. Don't clone this repo to "set up" Mimir. The skill runs on `git` + your existing `gh` auth.

## Where things go

| | |
|---|---|
| sessions | `~/.mimir/sessions/` → private `mimir-sessions` under your GitHub account |
| control | `~/.mimir/config.toml`, `mimir.log` |
| code index (optional) | `<repo>/.mimir/` (gitignored) |

In chat the agent leaves a short receipt:

```text
◆ mimir  session.push  workstation-a-hermes-core-v2
         goal: Durable/Eph adapter split
         ok · abc1234
```

Same line also lands in `~/.mimir/mimir.log`.

## Optional: index / recall / MCP

Binary only. Sessions work without it.

```bash
go install <repository-source>/cmd/mimir@latest   # ensure $(go env GOPATH)/bin is on PATH
mimir index --full
mimir recall "auth"
```

## More

- [`skills/mimir/SKILL.md`](./skills/mimir/SKILL.md) — the installable skill
- [`AGENTS.md`](./AGENTS.md) — agent operating manual
- [`spec.md`](./spec.md) — full product contract
- [`docs/mcp.md`](./docs/mcp.md) — MCP tools

Local + your private git. No Mimir account, backend, or telemetry.

<details>
<summary>Full CLI surface</summary>

```bash
mimir control init [--machine NAME]
mimir session init
mimir session push --id SLUG --harness NAME --project NAME --goal "..." [--body notes.md]
mimir session pull
mimir session list
mimir index [--full]
mimir recall <query> [--budget 4000] [--json]
mimir deps <file_path>
mimir locate <symbol_name>
mimir serve            # MCP stdio
mimir status
mimir doctor
```

</details>

<details>
<summary>Contributing (working on Mimir itself)</summary>

```bash
git clone https://github.com/<owner>/mimir.git
cd mimir
go test ./cmd/mimir
go build -o mimir ./cmd/mimir
```

</details>
