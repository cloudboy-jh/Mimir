# Control plane

Root: `~/.mimir/` (override with `MIMIR_HOME` for tests/install isolation).

| file | role |
|---|---|
| `config.toml` | tiny human-readable config; agent owns most writes |
| `mimir.log` | append-only audit log |
| `sessions/` | session plane working tree (usually a private git clone) |

## Schema

```toml
machine = "therig"

[sessions]
enabled = true
repo = "https://github.com/<login>/mimir-sessions.git"
path = "~/.mimir/sessions"
# default_harness = "hermes"

[code]
prefer_mcp = true
auto_index_if_stale = true

[log]
path = "~/.mimir/mimir.log"
level = "info"
```

## Log line shape

```
<rfc3339>  <plane.verb>  <detail>  <ok|warn|fail>
```

Chat receipts (`◆ mimir …`) are the ephemeral twin; this file is durable.
