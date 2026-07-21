# opencode Session Capture Setup

Date: 2026-07-20
Status: Resolved and verified end-to-end.

## Problem

opencode sessions were not being captured by Mimir, even though the deployment
was healthy and the `mimir serve` MCP server was registered and enabled in
`~/.config/opencode/opencode.json`.

Symptoms:

- `sessions_list` only showed proxy smoke tests (`gpt-4o-mini`), never real
  opencode sessions.
- The MCP tools (`whoami`, `sessions_list`, `session_status`) all worked, which
  made the setup look complete when it wasn't.

Root cause: **Mimir has no "save session" operation.** Capture happens at the
transport layer — the Worker proxies the model request, redacts the exchange,
writes it to R2, and indexes it in D1 via `waitUntil`. The MCP tools only read
and annotate what the proxy already captured. opencode's model traffic was
going to a local llama-server (`localhost:11434`) and to Anthropic directly,
so nothing ever passed through the Worker and there was nothing to capture.
Registering the MCP server alone does not enable capture.

There is also no backfill: exchanges are written at proxy time only. A session
that ran outside the proxy is gone.

## Solution

Route opencode's model traffic through the Worker's OpenRouter-compatible
proxy by adding a dedicated provider in the global opencode config
(`~/.config/opencode/opencode.json`):

```json
"provider": {
  "mimir": {
    "name": "mimir (captured)",
    "npm": "@ai-sdk/openai-compatible",
    "models": {
      "openai/gpt-4o-mini": { "name": "gpt-4o-mini (mimir)" },
      "anthropic/claude-sonnet-4.5": { "name": "claude-sonnet-4.5 (mimir)" }
    },
    "options": {
      "apiKey": "{file:C:/Users/johns/.mimir/token}",
      "baseURL": "https://<worker>.workers.dev/v1",
      "headers": { "x-mimir-harness": "opencode" }
    }
  }
}
```

Details that matter:

- The connection values come from `mimir login --json` (the connection
  manifest): `openai_base_url`, `credential_file`, `mcp_command`.
- The machine token is injected with opencode's `{file:...}` interpolation so
  the credential never sits in the config file. Never paste the token value
  into config or chat.
- Model IDs are OpenRouter IDs; the proxy passes through anything OpenRouter
  accepts. Add models as needed.
- The MCP entry was also hardened to use the absolute binary path
  (`C:\Users\johns\go\bin\mimir.exe serve`) instead of relying on PATH.
- opencode cannot send dynamic per-session headers, so `x-mimir-session` is
  not set. Mimir's inactivity fallback (default 15-minute gap, grouped by
  repo/harness) handles session boundaries. The static `x-mimir-harness`
  header improves grouping.
- opencode does not hot-reload config; restart after editing.

Capture only applies to sessions running on `mimir/*` models. Sessions on any
other provider are not captured.

## Verification

1. **Config valid:** parse `opencode.json` as JSON.
2. **Proxy auth:** `GET <worker>/v1/models` with the machine bearer token
   returns 200.
3. **End-to-end (the only real proof):** in a fresh opencode session on a
   `mimir/*` model, ask the agent to run `mimir whoami`, then to list Mimir
   sessions. A new session must appear with `N exchanges saved`.

Verified 2026-07-20: session `ses_07d7206abffe6NKwy2El2VbTkP` captured with
5 exchanges via `moonshotai/kimi-k3`, visible in the dashboard with full
request timeline and token counts.

Note: capture persists via `waitUntil` after the response streams, so receipts
can lag a few seconds. `session_status` waits for background capture before
returning. Proxy transport activity alone is never proof of persistence.
