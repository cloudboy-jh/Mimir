# Churn MCP

`churn serve` is the planned stdio MCP surface for read-only access to the local `.churn/` store.

Target config:

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

Planned tools:

- `churn_recall(query, token_budget?)`
- `churn_context()`
- `churn_symbols(name?)`
- `churn_findings(severity?, path?)`
- `churn_deps(path?)`
- `churn_status()`
