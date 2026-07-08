# Churn

Hermes remembers you. Churn remembers your code.

Churn is a zero-dependency, local-first code memory layer for autonomous AI agents and workspaces. It indexes a repository into a gitignored `.churn/` store, keeps that store fresh with git-aware incremental updates, and exposes token-budgeted recall through CLI and MCP surfaces.

## How it fits

Jack's 2026 agent tooling stack is one loop:

```txt
gittrix routes agent writes into ephemeral workspaces
        ↓
churn supplies durable repo memory those agents can read
        ↓
glib-code sandboxes execution
        ↓
human approves promotion
```

Churn is the code-memory layer. It is headless on purpose: no TUI, no panels, no background account, no API key. Agents ask it for code memory; editors handle visualization.

Current foundation:

- safe git repo detection
- stdlib-only command surface
- atomic `.churn/index.json` writes
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
churn status
churn index [--full]
churn recall <query> [--budget 4000] [--json]
churn deps <file_path>
churn locate <symbol_name>
churn serve
churn doctor
```

MCP tools:

- `churn_status`
- `churn_recall(query, token_budget)`
- `churn_get_file_deps(file_path)`
- `churn_locate_symbol(symbol_name)`

No telemetry. No account. No cloud backend. Core behavior stays offline-first.
