# Mimir Specification (Headless)

Mimir is a zero-dependency, local-first code memory layer for autonomous AI agents and workspaces.

It solves cold boot by indexing repository structure, symbols, and imports into `.mimir/index.json`, then serving ranked, token-budgeted memory through CLI and MCP.

> Hermes remembers the developer. Chiron remembers the session. Mimir remembers the code.

## Layout

```txt
[your-project]/
├── .mimir/
│   ├── index.json
│   ├── config.json
│   └── history/
└── src/
```

`.mimir/` is project-local and gitignored.

## Commands

```bash
mimir status
mimir index [--full]
mimir recall "<query>" [--budget 4000] [--json]
mimir deps <file_path>
mimir locate <symbol_name>
mimir serve
```

## MCP Tools

- `mimir_status`
- `mimir_recall(query, token_budget)`
- `mimir_get_file_deps(file_path)`
- `mimir_locate_symbol(symbol_name)`

## Rules

- Fast before smart.
- No TUI UI bloat.
- No baseline embeddings or external services.
- Atomic writes for `.mimir/index.json`.
- Local-first. No telemetry. No API key.
