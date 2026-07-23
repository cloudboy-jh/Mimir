# Hermes Session Capture Setup

Date: 2026-07-22
Status: Implemented; pending first deployed desktop/TUI verification.

## Two capture paths

Hermes capture has two cooperating paths:

1. **Proxy path** (this document) — redirects Hermes' built-in OpenRouter
   provider through the Worker. Richest capture: token usage, full redacted
   exchange archives.
2. **Plugin path** — [`plugins/hermes/`](../plugins/hermes/) is a Hermes
   plugin (Hermes' own plugin system, no upstream changes) that reports
   turns, heartbeats, and session ends to `/sessions/:id/events`. It covers
   the providers the proxy cannot reach: Nous portal account, direct
   providers, anything not routed through the Worker.

The plugin decides its mode once at startup: when the managed OpenRouter
redirect is active (`OPENROUTER_BASE_URL` points at the Mimir Worker) it runs
**liveness-only** — heartbeats and ends, no turn events, because the proxy is
already capturing turns and double reporting would inflate the session. When
Hermes talks to providers directly it runs **full mode** and reports every
completed turn (via Hermes' `post_llm_call` hook) plus session lifecycle
(`on_session_start`, `on_session_end`, `on_session_reset`,
`on_session_finalize`).

Install: copy [`plugins/hermes/`](../plugins/hermes/) into the plugins
directory under the Hermes home (`~/.hermes/plugins/` or
`%LOCALAPPDATA%/hermes/plugins` on Windows). Uninstall: delete the directory.
The plugin carries no credentials; it resolves the Worker URL and machine
token from `MIMIR_URL`/`MIMIR_TOKEN`, `$MIMIR_HOME`, or `~/.mimir/` exactly
like the CLI. Delivery is best-effort and never blocks Hermes; the
server-side silence timer finalizes sessions even when the process dies
before an end event lands.

## Design

Mimir redirects Hermes' built-in OpenRouter provider instead of registering a
custom provider. `mimir setup`, `mimir login`, and `mimir update` detect the
active Hermes home and maintain a block at the end of its `.env`:

```dotenv
# >>> mimir managed openrouter route
OPENROUTER_BASE_URL="https://<worker>.workers.dev/v1/hermes"
# <<< mimir managed openrouter route
```

Hermes keeps its existing `OPENROUTER_API_KEY`. Mimir never replaces that value
with a machine token because some Hermes auxiliary tools still call OpenRouter's
fixed URL; replacing it would leak the Mimir credential. Existing dotenv
assignments are preserved. The managed block is last so the base URL takes
precedence, and updates replace only that block.

During installation, the CLI registers the OpenRouter key's SHA-256 digest with
the Worker using machine authentication. The raw key is not stored in D1.

Hermes uses the ordinary OpenRouter model picker. There is no `mimir` provider,
duplicate model catalog, or model-name migration.

## Worker compatibility surface

Hermes resolves account and model metadata against the configured OpenRouter
base URL, not only Chat Completions. The Worker therefore exposes:

- `POST /v1/hermes/chat/completions`
- `GET /v1/hermes/models`
- `GET /v1/hermes/key`
- `GET /v1/hermes/credits`

Each route accepts either a Mimir machine token or an OpenRouter credential whose
digest was registered by the CLI. OpenRouter-key authentication is restricted to `/v1/hermes/*`; it cannot
read sessions, logs, or configuration. The Worker sends its configured
credential upstream when machine authentication is used, and the presented
Hermes credential otherwise. GET responses stream through unchanged. The chat
route supplies `hermes` as the capture harness when no explicit header is
available.

## Supported boundary

Capture applies whenever Hermes' effective provider is `openrouter`, including
mid-session switches between OpenRouter models. MCP does not perform capture.

Direct Nous, Anthropic OAuth, Codex, Gemini, and other provider transports bypass
the Worker and are not captured **by the proxy** — install the Hermes plugin
(above) to capture them from inside the harness. Mimir does not intercept TLS
traffic.

Hermes auxiliary tools that hard-code OpenRouter's URL also remain direct and
uncaptured. They retain the real OpenRouter credential, so they continue working
without exposing a Mimir machine token.

Desktop and TUI use the same Hermes profile, so a static installation cannot
reliably distinguish them. Both are grouped under the `hermes` harness. Hermes
does not send an exact Mimir session ID, so session boundaries use the inactivity
fallback. Run `mimir update` after changing Hermes profiles so the new profile's
credential and base URL are registered.

## Verification

1. Run `mimir doctor`; it checks the managed dotenv route and the Hermes models,
   key, and credits endpoints without invoking a model.
2. Restart Hermes because it does not hot-reload its environment.
3. Start a fresh session on an OpenRouter model and switch to another OpenRouter
   model mid-session.
4. Confirm the exchanges appear under the `hermes` harness. Durable session
   status, not transport activity alone, is proof of persistence.
