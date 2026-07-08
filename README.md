# Churn

Hermes remembers you. Churn remembers your code.

Churn is a Go-first, local-first code memory CLI/TUI for AI coding agents. It will index a repo into a durable `.churn/` store, keep that store fresh with git-aware incremental updates, and expose token-budgeted recall through CLI and MCP surfaces.

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

Churn is the code-memory layer. It is intentionally smaller than a full workspace app: open the TUI, see whether the current repo has memory, then use the CLI/MCP surfaces to index, recall, and serve context.

Current foundation:

- safe git repo detection
- Cobra command surface
- BentoTUI/Bubble Tea shell
- custom Churn theme
- status/doctor/index/recall/serve scaffolds

```bash
go test ./...
go run ./cmd/churn --version
go run ./cmd/churn doctor
go run ./cmd/churn status
go run ./cmd/churn tui
```

Core commands:

```bash
churn
churn tui
churn index [--full]
churn status
churn recall <query> [--budget 4000] [--json]
churn serve
churn doctor
```

No telemetry. No account. No cloud backend. Core behavior stays offline-first.
