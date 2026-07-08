# Mimir

![Mimir](./mimir-readme.png)

Hermes remembers the developer. Chiron remembers the session. Mimir remembers the code.

Mimir is a zero-dependency, local-first code memory layer for autonomous AI agents and workspaces. It indexes a repository into a gitignored `.mimir/` store, keeps that store fresh with git-aware incremental updates, and exposes token-budgeted recall through CLI and MCP surfaces.

## How it fits

```txt
gittrix routes agent writes into ephemeral workspaces
        ↓
mimir supplies durable repo memory those agents can read
        ↓
glib-code sandboxes execution
        ↓
human approves promotion
```

Mimir is headless on purpose: no TUI, no panels, no background account, no API key. Agents ask it for code memory; editors handle visualization.

Current foundation:

- stdlib-only command surface
- atomic `.mimir/index.json` writes
- lexical symbol/import extraction
- token-budgeted recall
- MCP stdio tools

```bash
go test ./...
go run ./src --version
go run ./src doctor
go run ./src status
go run ./src index --full
go run ./src recall "indexer" --budget 1200
```

Core commands:

```bash
mimir status
mimir index [--full]
mimir recall <query> [--budget 4000] [--json]
mimir deps <file_path>
mimir locate <symbol_name>
mimir serve
mimir doctor
```

MCP tools:

- `mimir_status`
- `mimir_recall(query, token_budget)`
- `mimir_get_file_deps(file_path)`
- `mimir_locate_symbol(symbol_name)`

No telemetry. No account. No cloud backend. Core behavior stays offline-first.
