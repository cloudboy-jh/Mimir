# Hermes Session Capture Setup

Date: 2026-07-22
Status: Implemented; pending first deployed desktop/TUI verification.

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
the Worker and are not captured. Supporting them would require a Hermes-native
global transport hook or separate protocol/authentication integrations; Mimir
does not intercept TLS traffic.

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
