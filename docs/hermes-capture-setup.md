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
   turns, heartbeats, and session ends to `/sessions/:id/events` after direct
   provider activity is detected. It covers
   the providers the proxy cannot reach: Nous portal account, direct
   providers, and anything not routed through the Worker. These are bounded
   event summaries, not persisted request/response exchange objects.

The plugin classifies each turn from `pre_api_request` provider and base-URL
metadata. A session using only the managed OpenRouter redirect emits no plugin
heartbeat, turn, or end events because the proxy owns that session lifecycle.
`on_session_start` intentionally emits nothing. The first direct-provider
classification is sticky for that session and emits an activation heartbeat;
completed direct turns follow through `post_llm_call`, and
`on_session_finalize` emits an end only after direct evidence. If Hermes misses
the pre-request hook, the completed turn falls back to the configured route.
Mixed sessions continue to suppress their proxied turns while retaining the
direct lifecycle. Finalize always clears classification state, including
unconsumed turn routes. Turn deduplication is scoped by session ID and turn ID.

The plugin registers exactly `pre_api_request`, `post_llm_call`,
`on_session_start`, and `on_session_finalize`. It does not register
`on_session_end` or `on_session_reset`.

The canonical installer embeds the plugin and enrolls its exact files under
the detected Hermes home (`~/.hermes/plugins/mimir/` or the active Windows
Hermes home). `mimir update` refreshes only unchanged, receipt-owned files;
different or locally modified files are preserved and symlinked targets are
rejected. Manual copying from [`plugins/hermes/`](../plugins/hermes/) is a
recovery path only. `mimir uninstall` removes only unchanged receipt-owned
plugin and skill files, preserves conflicts, and leaves the local Mimir
connection and Cloudflare deployment intact.
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
OPENROUTER_BASE_URL="https://<worker>.workers.dev/v1/hermes/<installation-id>"
# <<< mimir managed openrouter route
```

Hermes keeps its existing `OPENROUTER_API_KEY`. Mimir never replaces that value
with a machine token because some Hermes auxiliary tools still call OpenRouter's
fixed URL; replacing it would leak the Mimir credential. Existing dotenv
assignments are preserved. The managed block is last so the base URL takes
precedence, and updates replace only that block.

During installation, the CLI reads the stable installation ID from its managed
receipt and registers the OpenRouter key's SHA-256 digest for that installation
using machine authentication. The raw key is not stored in D1. One digest may be
registered for multiple installations without coupling their revocation state.

Hermes uses the ordinary OpenRouter model picker. There is no `mimir` provider,
duplicate model catalog, or model-name migration.

## Worker compatibility surface

Hermes resolves account and model metadata against the configured OpenRouter
base URL, not only Chat Completions. The Worker therefore exposes:

- `POST /v1/hermes/<installation-id>/chat/completions`
- `GET /v1/hermes/<installation-id>/models`
- `GET /v1/hermes/<installation-id>/key`
- `GET /v1/hermes/<installation-id>/credits`

Each route accepts either a Mimir machine token or the OpenRouter credential whose
digest was registered for that exact installation. The explicit legacy unscoped
routes remain available only when exactly one active installation matches the
credential. OpenRouter-key authentication is restricted to these routes; it cannot
read sessions, logs, or configuration. The Worker sends its configured
credential upstream when machine authentication is used, and the presented
Hermes credential otherwise. GET responses stream through unchanged. The chat
route supplies `hermes` as the capture harness when no explicit header is
available.

The scoped-credential migration preserves credentials already associated with
an installation and drops legacy unassociated rows rather than retaining an
indefinitely valid unscoped credential. Reauthorize Hermes from an active
installation if a retired legacy credential is still needed.

## Supported boundary

Capture applies whenever Hermes' effective provider is `openrouter`, including
mid-session switches between OpenRouter models.

Direct Nous, Anthropic OAuth, Codex, Gemini, and other provider transports bypass
the Worker and are not captured **by the proxy** — install the Hermes plugin
(above) to retain completed-turn event summaries from inside the harness. Their
request and response bodies are not written to R2 or indexed as searchable
exchanges. Mimir does not intercept TLS traffic.

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
