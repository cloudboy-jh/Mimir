# Mimir

![Mimir](./mimir-readme.png)

Hermes remembers the **developer**. Mimir remembers the **repo** and the **session**.

You talk to an agent. The agent drives Mimir. You almost never touch Mimir directly — when you need to audit something, there’s a small config and a log.

## What it remembers

**Code** — indexes the repo you’re in so agents can cold-start without re-reading the whole tree. Lives in `<repo>/.mimir/` (gitignored). Served over CLI + MCP.

**Session** — saves what you were doing (goal, progress, context) as markdown and syncs it through *your* private git repo so another machine can pick up the same work. Lives in `~/.mimir/sessions/`.

**Control** — `~/.mimir/config.toml` + `mimir.log`. Tiny. Agent writes; human reads on doubt.

## How you use it

Say things like:

- “Set up Mimir.”
- “Save progress.”
- “Continue what I was doing on thedeck.”
- “What do we know about auth in this repo?”

Don’t paste remotes, fill install forms, or learn CLI verbs as daily vocabulary. The agent uses `gh`, hostname, and cwd — you confirm only real decisions.

### Example

**You:** Set up Mimir.

**Agent:** Control plane ready (`machine=therig`). GitHub as `you` — bound private `you/mimir-sessions` → `~/.mimir/sessions`. Index this repo?

**You:** Yes.

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

**You (later):** Save progress.

```text
◆ mimir  session.push  therig-hermes-gittrix-v2
         goal: Durable/Eph adapter split
         ok · abc1234
```

**You on another machine:** Continue yesterday on therig.

**Agent:** Same login → pull → restore. No URL paste.

Every memory-bus event in chat starts with `◆ mimir`. Durable twin: `~/.mimir/mimir.log`.

## Where things live

| what | path | engine |
|---|---|---|
| code index | `<repo>/.mimir/` | Go CLI / MCP |
| sessions | `~/.mimir/sessions/` | git + markdown |
| config + log | `~/.mimir/` | agent-written |

Session sync does **not** depend on the code index. If the binary isn’t installed, sessions and control still work.

Full contract: [`spec.md`](./spec.md) · Agent skill: [`skills/mimir/SKILL.md`](./skills/mimir/SKILL.md)

## Dev

```bash
go test ./src
go run ./src doctor
go run ./src control init
go run ./src session init   # prefers gh create/clone of private mimir-sessions
go run ./src index --full
go run ./src recall "indexer" --budget 1200
```

---

## Manual appendix

Worth reading only if you’re wiring Mimir without an agent.

```bash
# once per machine
mimir control init [--machine NAME]

# once per person — discovers gh, creates/clones private mimir-sessions
mimir session init
mimir session push --id my-work --harness cli --project myproj --goal "…"
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

MCP: `mimir_status`, `mimir_recall`, `mimir_get_file_deps`, `mimir_locate_symbol` — [`docs/mcp.md`](./docs/mcp.md).

No telemetry. No Mimir account. Sessions live in *your* private repo via existing `gh` auth.
