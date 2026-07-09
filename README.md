# Mimir

![Mimir](./mimir-readme.png)

Hermes remembers the **developer**. Mimir remembers the **repo** and the **session**.

You talk to an agent. The agent drives Mimir. You almost never touch Mimir directly. When you need to audit something, there is a small config and a log.

**For agents:** read [`AGENTS.md`](./AGENTS.md) first.

## Install

Repo: `https://github.com/cloudboy-jh/Mimir`  
Needs: Go 1.25+, git, `gh` signed in. No Mimir account.

```bash
git clone https://github.com/cloudboy-jh/Mimir.git
cd Mimir
go build -o mimir ./src
./mimir control init
./mimir session init
./mimir doctor
```

Windows PowerShell: `.\mimir.exe` instead of `./mimir`. Put the binary on PATH if you want a global `mimir`.

Agents: do **not** search the web for Mimir. Use this repo URL and [`AGENTS.md`](./AGENTS.md). Session remotes are discovered via `gh` (`mimir session init`); do not invent or guess those.

Per-repo code memory when you want it:

```bash
mimir index --full
mimir recall "auth"
```

## What it remembers

| plane | what | where |
|---|---|---|
| **code** | repo structure index for cold-start | `<repo>/.mimir/` (gitignored) |
| **session** | goal / progress / context, git-synced across machines | `~/.mimir/sessions/` |
| **control** | tiny config + audit log | `~/.mimir/config.toml`, `mimir.log` |

## How you use it (after install)

Daily intent goes to the agent. Install itself is the block above, not a vague product search.

- "Save progress."
- "Continue what I was doing on thedeck."
- "What do we know about auth in this repo?"

Do not paste session remotes when `gh` already works. Do not make CLI the daily vocabulary after install.

### Example

After install (`control init` + `session init`), first index of a repo:

```text
◆ mimir  control.init  machine=therig
         sessions on · code mcp optional
         ok

◆ mimir  session.init  https://github.com/you/mimir-sessions.git
         private repo created · cloned
         ok

◆ mimir  code.index  my-repo  full
         +128 files · 210ms · sha 7d58e16
```

**You:** Save progress.

```text
◆ mimir  session.push  therig-hermes-gittrix-v2
         goal: Durable/Eph adapter split
         ok · abc1234
```

**You on another machine:** Continue yesterday on therig.

**Agent:** Same login, pull, restore. No URL paste.

Every memory-bus event in chat starts with `◆ mimir`. Durable twin: `~/.mimir/mimir.log`.

## Where things live

| what | path | engine |
|---|---|---|
| code index | `<repo>/.mimir/` | Go CLI / MCP |
| sessions | `~/.mimir/sessions/` | git + markdown |
| config + log | `~/.mimir/` | agent-written |

Session sync does **not** depend on the code index. If the binary is not installed, sessions and control can still work.

| doc | use |
|---|---|
| [`AGENTS.md`](./AGENTS.md) | agent operating manual |
| [`spec.md`](./spec.md) | full product contract |
| [`skills/mimir/SKILL.md`](./skills/mimir/SKILL.md) | harness skill |

## Dev

```bash
go test ./src
go run ./src doctor
go run ./src control init
go run ./src session init
go run ./src index --full
go run ./src recall "indexer" --budget 1200
```

---

## Manual appendix

Worth reading only if you are wiring Mimir without an agent.

```bash
# once per machine
mimir control init [--machine NAME]

# once per person (discovers gh, creates/clones private mimir-sessions)
mimir session init
mimir session push --id my-work --harness cli --project myproj --goal "..."
mimir session pull
mimir session list

# per repo
mimir index [--full]
mimir recall <query> [--budget 4000] [--json]
mimir deps <file_path>
mimir locate <symbol_name>
mimir serve    # MCP stdio
mimir status
mimir doctor
```

MCP: `mimir_status`, `mimir_recall`, `mimir_get_file_deps`, `mimir_locate_symbol`. See [`docs/mcp.md`](./docs/mcp.md).

No telemetry. No Mimir account. Sessions live in *your* private repo via existing `gh` auth.
