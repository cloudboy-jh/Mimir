# Hermes Session Capture Setup

Date: 2026-07-21
Status: Configured; pending first live-session verification.

## Problem

Hermes (TUI and desktop app) sessions were partially captured. The default
model ran through the Mimir Worker via the legacy `model` block in
`config.yaml`, but any session running on a model selected from a built-in
provider (`openrouter`, `nous`, etc.) bypassed the Worker entirely. Provider
and model switches — including automatic fallback switches mid-session —
silently dropped capture.

Root cause is the same as opencode (see `opencode-capture-setup.md`): capture
is a side effect of the proxy path. There is no save operation and no
backfill. Registering the MCP server (`mimir serve`) only provides read and
annotate tools; it never writes exchanges. Any request that does not pass
through the Worker is not captured.

The Hermes-specific wrinkle: Hermes supports multiple simultaneous providers
and mid-session `/model` switching, so a single `model.base_url` override
covers exactly one provider slot. Everything else needs a named custom
provider so captured models are selectable everywhere the picker appears.

## Solution

Add Mimir as a named custom provider in Hermes `config.yaml`:

```yaml
custom_providers:
  - name: mimir
    display_name: Mimir (captured)
    base_url: https://<worker>.workers.dev/v1
    api_key: ${MIMIR_API_KEY}
    api_mode: chat_completions
    extra_headers:
      x-mimir-harness: hermes-desktop
    models:
      - moonshotai/kimi-k3
      - google/gemini-3.5-flash
      - anthropic/claude-sonnet-4.5
      # ... any OpenRouter model IDs you want captured
```

Details that matter:

- **Use `extra_headers`, not `default_headers`.** The Hermes config
  normalizer silently drops `default_headers` from `custom_providers`
  entries. `extra_headers` is the only key that survives normalization; it
  merges into the OpenAI client's default headers on every request, matched
  by `base_url` (trailing-slash insensitive). This merge runs at startup AND
  on every mid-session `/model` switch, so switching between models inside
  the Mimir provider keeps capture active.
- **Model IDs are OpenRouter IDs**, passed through verbatim. Add any model
  OpenRouter accepts.
- **The API key is the machine bearer token.** Use `${MIMIR_API_KEY}` env
  interpolation (set in Hermes' `.env`); never paste the token into config
  or chat. The token value comes from `mimir login --json` /
  `~/.mimir/token`.
- **Harness header values are free-form.** Use distinct values per harness
  flavor (`hermes` for TUI, `hermes-desktop` for the desktop app) so the
  dashboard and `sessions_list` group them separately. The legacy `model`
  block's `default_headers` is a separate path and still works for the
  single default provider slot.
- Hermes does not send `x-mimir-session` (no dynamic per-session headers),
  so session boundaries fall back to harness + inactivity gap (default 15
  minutes). Use `session_end` from the mimir-use skill to close a session
  explicitly.
- Hermes does not hot-reload config; restart after editing.

## Usage rule

Pick models from the **Mimir (captured)** provider in `/model`. That is the
entire discipline. Models selected from built-in providers (openrouter,
nous, ...) are direct and uncaptured — by design, same rule as opencode:
capture only applies to sessions on mimir/* models.

## Verification

1. **Config valid:** `hermes config check` passes.
2. **Proxy auth:** `GET <worker>/v1/models` with the machine bearer token
   returns 200.
3. **End-to-end (the only real proof):** restart Hermes, start a fresh
   session, `/model` → Mimir (captured) → any model, converse a few turns,
   then run `mimir whoami` and list sessions via the MCP tools. A new
   session with the configured `x-mimir-harness` value and `N exchanges
   saved` must appear.
4. **Per-response signal:** the Worker stamps `x-mimir-capture: saved` (or
   `skipped` with a reason header) on every proxied response. A `skipped`
   verdict with a filter reason means the save config in D1 rejected it —
   check `/config`, not the harness.

Note: capture persists via `waitUntil` after the response streams, so
receipts can lag a few seconds. `session_status` waits for background
capture before returning. Proxy transport activity alone is never proof of
persistence.
