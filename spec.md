# Churn 3.0 Specification (Headless)

Churn is a zero-dependency, local-first code memory layer for autonomous AI agents and workspaces.

It solves cold boot by indexing repository structure, symbols, and imports into `.churn/index.json`, then serving ranked, token-budgeted memory through CLI and MCP.

> Hermes remembers the developer. Chiron remembers the session. Churn remembers the code.

## Layout

```txt
[your-project]/
├── .churn/
│   ├── index.json
│   ├── config.json
│   └── history/
└── src/
```

`.churn/` is project-local and gitignored.

## Commands

```bash
churn status
churn index [--full]
churn recall "<query>" [--budget 4000] [--json]
churn deps <file_path>
churn locate <symbol_name>
churn serve
```

## MCP Tools

- `churn_status`
- `churn_recall(query, token_budget)`
- `churn_get_file_deps(file_path)`
- `churn_locate_symbol(symbol_name)`

## Rules

- Fast before smart.
- No TUI UI bloat.
- No baseline embeddings or external services.
- Atomic writes for `.churn/index.json`.
- Local-first. No telemetry. No API key.
