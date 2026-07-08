# Churn MCP

`churn serve` exposes the local `.churn/index.json` over MCP-compatible JSON-RPC on stdin/stdout.

Config:

```json
{
  "mcpServers": {
    "churn": {
      "command": "churn",
      "args": ["serve"],
      "cwd": "/path/to/repo"
    }
  }
}
```

Tools:

- `churn_status`: current indexing metrics, freshness, and timestamps.
- `churn_recall(query, token_budget)`: ranked lexical code memory fitted to budget.
- `churn_get_file_deps(file_path)`: immediate dependencies and downstream files.
- `churn_locate_symbol(symbol_name)`: absolute path, line, type, and signature.
