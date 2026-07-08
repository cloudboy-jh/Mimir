# Mimir MCP

`mimir serve` exposes the local `.mimir/index.json` over MCP-compatible JSON-RPC on stdin/stdout.

Config:

```json
{
  "mcpServers": {
    "mimir": {
      "command": "mimir",
      "args": ["serve"],
      "cwd": "/path/to/repo"
    }
  }
}
```

Tools:

- `mimir_status`: current indexing metrics, freshness, and timestamps.
- `mimir_recall(query, token_budget)`: ranked lexical code memory fitted to budget.
- `mimir_get_file_deps(file_path)`: immediate dependencies and downstream files.
- `mimir_locate_symbol(symbol_name)`: absolute path, line, type, and signature.
