# Mimir

![Mimir](./mimir-readme.png)

Hermes remembers the **developer**. Mimir remembers the **repo** and the **session**.

You talk to an agent. The agent drives Mimir. You almost never touch Mimir directly. When you need to audit something, there is a small config and a log.

**Agents:** install the **skill**, not this whole repo. See [`AGENTS.md`](./AGENTS.md).

## Install

Install the skill. No product clone required to use Mimir.

```bash
npx skills add cloudboy-jh/Mimir@mimir -g -y
```

After the skill is on the harness, bootstrap control + sessions:

```bash
mimir control init
mimir session init
mimir doctor
```

That needs the optional CLI (below) **or** the agent following the skill with `git` + `gh` only. Session sync is your private repo (`mimir-sessions`), discovered via existing `gh` auth. No Mimir account.

### Optional: code-memory binary

Only for repo index / recall / MCP:

```bash
go install github.com/cloudboy-jh/mimir/cmd/mimir@latest
```

Ensure `$(go env GOPATH)/bin` is on PATH. Then in a project:

```bash
mimir index --full
mimir recall "auth"
```

### Contributors only

Working **on** Mimir itself (not "using" it):

```bash
git clone https://github.com/cloudboy-jh/Mimir.git
cd Mimir
go test ./cmd/mimir
go build -o mimir ./cmd/mimir
```

## What it remembers

| plane | what | where |
|---|---|---|
| **code** | repo structure index for cold-start | `<repo>/.mimir/` (gitignored) |
| **session** | goal / progress / context, git-synced across machines | `~/.mimir/sessions/` |
| **control** | tiny config + audit log | `~/.mimir/config.toml`, `mimir.log` |

## How you use it

Daily intent (after skill install):

- "Save progress."
- "Continue what I was doing on thedeck."
- "What do we know about auth in this repo?"

Do not paste session remotes when `gh` already works. Do not ship “clone Mimir” as the user install path.

### Example

```text
◆ mimir  session.push  therig-hermes-gittrix-v2
         goal: Durable/Eph adapter split
         ok · abc1234
```

Every memory-bus event in chat starts with `◆ mimir`. Durable twin: `~/.mimir/mimir.log`.

## Where things live

| what | path | engine |
|---|---|---|
| skill | harness skill dir (`npx skills add`) | agent procedure |
| sessions | `~/.mimir/sessions/` | git + markdown |
| config + log | `~/.mimir/` | agent-written |
| code index | `<repo>/.mimir/` | optional binary / MCP |

Session sync does **not** depend on the code index.

| doc | use |
|---|---|
| [`AGENTS.md`](./AGENTS.md) | agent operating manual |
| [`skills/mimir/SKILL.md`](./skills/mimir/SKILL.md) | installable skill source |
| [`spec.md`](./spec.md) | full product contract |

## Dev

```bash
go test ./cmd/mimir
go run ./cmd/mimir doctor
go run ./cmd/mimir control init
go run ./cmd/mimir session init
go run ./cmd/mimir index --full
```

---

## Manual appendix

CLI surface after `go install` / build:

```bash
mimir control init [--machine NAME]
mimir session init
mimir session push --id my-work --harness cli --project myproj --goal "..."
mimir session pull
mimir session list
mimir index [--full]
mimir recall <query> [--budget 4000] [--json]
mimir deps <file_path>
mimir locate <symbol_name>
mimir serve
mimir status
mimir doctor
```

MCP: `mimir_status`, `mimir_recall`, `mimir_get_file_deps`, `mimir_locate_symbol`. See [`docs/mcp.md`](./docs/mcp.md).

No telemetry. No Mimir account. Sessions live in *your* private repo via existing `gh` auth.
