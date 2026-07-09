# Mimir Skill

Hermes remembers the developer. Mimir remembers the **repo** and the **session**.

Users never “use Mimir.” They talk to you. You drive Mimir. Config + log exist so humans can audit.

## Planes

| plane | root | how |
|---|---|---|
| control | `~/.mimir/` (`config.toml`, `mimir.log`) | `mimir control init` once per machine |
| session | `~/.mimir/sessions/` | git markdown; save/restore across machines |
| code | `<repo>/.mimir/` | Go CLI/MCP index + recall |

Session push does **not** depend on code index. Control + sessions still work if the binary is missing.

## Install (no research)

**Product repo (fixed, do not search):** `https://github.com/cloudboy-jh/Mimir`

```bash
git clone https://github.com/cloudboy-jh/Mimir.git
cd Mimir
go build -o mimir ./src
./mimir control init
./mimir session init
./mimir doctor
```

If already in this checkout: `go build -o mimir ./src` then the three `./mimir` commands. Windows: `.\mimir.exe`.

### After binary exists

```bash
mimir control init      # once per machine (machine derived from hostname)
mimir session init      # once; gh invents private login/mimir-sessions
mimir index --full      # optional, per repo
```

### Session remotes (zero-paste)

Do not ask for a **session** GitHub URL when `gh` works. CLI discovery:

1. `sessions.repo` in config → clone/pull
2. `gh api user` → login
3. create-or-clone private `login/mimir-sessions`
4. write config, enable sessions

Refuse: public repos, app monorepos as session remotes. Never store tokens.

### Code layer (optional)

```bash
mimir index --full
mimir index
```

Binary on PATH → register MCP for **active** harness only → `mimir index --full` once, incremental after. If binary fails: report degradation; sessions still work.

## Intent map

| user says | you do | receipt |
|---|---|---|
| Set up Mimir | control init → session init → offer code index | always |
| Save progress / checkpoint | write session md, push | always |
| Continue / restore from X | session pull, open matching md | always |
| What do we know about X in this repo? | `mimir recall` / MCP `mimir_recall` | one line if useful |
| Mid-coding structure need | MCP tools silently if needed | quiet one-liner only if user-facing |

## Session documents

Filename: `{machine}-{harness}-{session_id}.md`

```bash
mimir session push --id gittrix-v2 --harness hermes --project gittrix --goal "…" --body notes.md
mimir session pull
mimir session pull --id gittrix-v2
mimir session list
```

Fill body with current goal, state vars, progress checkboxes, context brief. Prefer rich markdown content.

## Chat receipts (locked mark)

Always lead user-facing plane events with:

```text
◆ mimir
```

Shape:

```text
◆ mimir  <plane>.<verb>  <subject>
         <optional meaning>
         <ok|warn|fail> · <metric>
```

CLI already prints receipts for control/session/index. Mirror that format when you act without the CLI (pure git skill path). Failures: `fail` + reason + `log: ~/.mimir/mimir.log`.

Never dump full `index.json` into chat.

## Log

Append-only twin lives at `~/.mimir/mimir.log`. Humans read on doubt. You write via CLI actions.

## Status / doctor

```bash
mimir status
mimir doctor
```

Agent-first debug. Expand checks: control home, config, sessions path/repo, git, gh auth, code store.

## Hard rules

- Don't ask for anything the environment already has (git identity, gh login, hostname, harness, project name).
- Don't teach session CLI as daily user vocabulary - map intent in natural language.
- Don't fold sessions into project `.mimir/`.
- Don't reintroduce a separate Chiron brand.
- No TUI, no install wizard forms, no SaaS.
