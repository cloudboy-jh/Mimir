# Mimir

![Mimir](./mimir-readme.png)

Memory for your coding agent. Mimir remembers the **repo** (code index) and the **session** (what you were doing), so an agent can cold-start and pick work back up across machines.

## Install

```bash
npx skills add cloudboy-jh/Mimir@mimir -g -y
```

PromptScript has no global scope, install it per project:

```bash
npx skills add cloudboy-jh/Mimir@mimir -a promptscript -y
```

That is it. The skill teaches your agent how to run Mimir with `git` + `gh`. No product clone, no account.

Then just talk to your agent:

- "Set up Mimir."
- "Save progress."
- "Continue what I was doing on thedeck."
- "What do we know about auth in this repo?"

## What it does

Three planes. You touch none of them directly.

| plane | remembers | where |
|---|---|---|
| **session** | goal, progress, context, synced across machines | `~/.mimir/sessions/` (your private git repo) |
| **code** | repo structure and symbols for cold-start | `<repo>/.mimir/` (gitignored) |
| **control** | tiny config + audit log | `~/.mimir/config.toml`, `mimir.log` |

Sessions sync through a private `mimir-sessions` repo created under your own GitHub via existing `gh` auth. Code memory is optional and never required for sessions to work.

Agents report actions in chat with a locked mark:

```text
◆ mimir  session.push  therig-hermes-gittrix-v2
         goal: Durable/Eph adapter split
         ok · abc1234
```

The durable copy of every action lives in `~/.mimir/mimir.log`.

## Optional: code-memory binary

Only needed for repo index / recall / MCP. Sessions and control work without it.

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

No telemetry. No account. No cloud backend. Sessions live in *your* private repo.

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
