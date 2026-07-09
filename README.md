# Mimir

![Mimir](./mimir-readme.png)

Agent-driven code and session memory.

The agent keeps an optional map of the repo, and a working session (goal, progress, context). Sessions sync through your own private git so work continues across machines. You talk; the agent runs it; `~/.mimir` is there when you want to audit.

## Install

```bash
npx skills add cloudboy-jh/Mimir@mimir -g -y
```

PromptScript has no global scope — install per project:

```bash
npx skills add cloudboy-jh/Mimir@mimir -a promptscript -y
```

Skill only for sessions. Don't clone this repo to "set up" Mimir. The skill runs on `git` + your existing `gh` auth.

Then say things like:

- Set up Mimir.
- Save progress.
- Continue what I was doing on thedeck.
- What do we know about auth in this repo?

## Where things go

| | |
|---|---|
| sessions | `~/.mimir/sessions/` → private `mimir-sessions` under your GitHub account |
| control | `~/.mimir/config.toml`, `mimir.log` |
| code index (optional) | `<repo>/.mimir/` (gitignored) |

In chat the agent leaves a short receipt:

```text
◆ mimir  session.push  therig-hermes-gittrix-v2
         goal: Durable/Eph adapter split
         ok · abc1234
```

Same line also lands in `~/.mimir/mimir.log`.

## Optional: index / recall / MCP

Binary only. Sessions work without it.

```bash
go install github.com/cloudboy-jh/mimir/cmd/mimir@latest   # ensure $(go env GOPATH)/bin is on PATH
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
git clone https://github.com/cloudboy-jh/Mimir.git
cd Mimir
go test ./cmd/mimir
go build -o mimir ./cmd/mimir
```

</details>
