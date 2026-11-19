# Hermes Session Capture

## Two capture paths

Hermes capture has two cooperating paths:

1. **Proxy path** — redirects Hermes' built-in OpenRouter provider through the
   Worker. This archives the full redacted provider exchange.
2. **Plugin path** — [`plugins/hermes/`](../plugins/hermes/) uses Hermes'
   request, response, and tool hooks for Codex, Nous portal, OAuth, and other
   providers whose traffic does not traverse the proxy. It sends bounded
   reconstructed exchanges to `/sessions/:id/exchanges`; supported tool
   interactions are attached to their provider request. These are not
   byte-for-byte provider transport archives.

Classification is per API request using `pre_api_request` provider/base-URL
metadata, not a session-wide setting. Requests through the managed OpenRouter
redirect remain proxy-owned. A session can use both paths without the plugin
uploading duplicates of proxied calls. Direct activity activates the plugin's
heartbeat and session-end reporting; a session using only the proxy leaves
lifecycle with the proxy. `on_session_start` emits nothing.

Hermes hook payloads are sanitized and bounded by Hermes before the plugin
receives them. When Hermes truncates a large provider request, the plugin
records the actual user message as a `user_turn` reconstruction rather than
claiming it saved the complete provider prompt. The Worker redacts
reconstructed request, response, and tool data before writing R2 and indexing
D1. The capture receipt comes from persisted exchange state, never from
plugin-load health or a heartbeat. Failed uploads reported to the Worker are
failures; an offline plugin cannot report a local failure until it reconnects.

Hermes history is not imported; `mimir import` and `mimir backfill` support
OpenCode, Pi, and Oh My Pi.

## Commits and tools

After a terminal tool result containing a `git commit` confirmation, the
plugin runs `mimir _hook git` with that output, and on finalize runs it with
`refresh: true` to mark newly pushed commits. The binary is found through
`MIMIR_BIN`, then `executable=` in `$MIMIR_HOME/config`, then `mimir` on
`PATH`.

The plugin registers `mimir_search`, `mimir_sessions`, `mimir_session`,
`mimir_turns`, `mimir_stats`, `mimir_day`, `mimir_devices`, `mimir_commits`,
and `mimir_commit_patch` in the `mimir` toolset. Each forwards a `tools/call`
to the Worker's `/mcp` endpoint and returns the same JSON as the MCP tool of
the same name.

## Install

`mimir install --harness hermes` writes the plugin under the detected Hermes
home (`~/.hermes/plugins/mimir/` or the active Windows Hermes home), the
`mimir-setup` and `mimir-use` skills, and the `.env` route block, and records
the files in `$MIMIR_HOME/managed.json`. Updates refresh only files whose bytes
still match; different or locally modified files are preserved and symlinked
targets are rejected. `mimir uninstall` removes only unchanged managed files
and leaves the Mimir connection and Cloudflare deployment intact.

The plugin carries no credentials; it resolves the Worker URL and machine token
from `MIMIR_URL`/`MIMIR_TOKEN`, `$MIMIR_HOME`, or `~/.mimir/` like the CLI.
Delivery never blocks or raises into Hermes. When the process dies before an
end event, the session expires after `session.gap_minutes` of silence.

## OpenRouter route

Mimir redirects Hermes' built-in OpenRouter provider instead of registering a
custom provider. Install and update detect the active Hermes home and maintain
a block at the end of its `.env`:

```dotenv
# >>> mimir managed openrouter route
OPENROUTER_BASE_URL="https://<worker>.workers.dev/v1/hermes/<installation-id>"
# <<< mimir managed openrouter route
```

Hermes keeps its existing `OPENROUTER_API_KEY`. Mimir never replaces it with a
machine token, because some Hermes auxiliary tools still call OpenRouter's
fixed URL and would leak the Mimir credential. Existing dotenv assignments are
preserved; the managed block is last so the base URL wins, and updates replace
only that block.

The CLI registers the OpenRouter key's SHA-256 digest for the installation ID
in `managed.json` using machine authentication. The raw key is not stored in
D1. One digest may be registered for several installations without coupling
their revocation.

Hermes uses its ordinary OpenRouter model picker. There is no `mimir`
provider, duplicate model catalog, or model-name migration.

## Worker compatibility surface

Hermes resolves account and model metadata against the configured OpenRouter
base URL, so the Worker exposes:

- `POST /v1/hermes/<installation-id>/chat/completions`
- `GET /v1/hermes/<installation-id>/models`
- `GET /v1/hermes/<installation-id>/key`
- `GET /v1/hermes/<installation-id>/credits`

Each route accepts a Mimir machine token or the OpenRouter credential whose
digest was registered for that installation. The unscoped `/v1/hermes/*`
routes work only when exactly one active installation matches the credential.
OpenRouter-key authentication is limited to these routes; it cannot read
sessions or configuration. The Worker sends its configured credential upstream
for machine authentication and the presented Hermes credential otherwise. GET
responses stream through unchanged. The chat route tags capture with the
`hermes` harness when no explicit header is present.

## Supported boundary

The proxy captures Hermes' effective OpenRouter provider, including mid-session
model changes. Direct Nous, Anthropic OAuth, OpenAI Codex, Gemini, and other
transports bypass the proxy and are captured through plugin hooks where they
expose request/response content. Mimir does not intercept TLS. A direct request
whose hooks expose no content cannot be saved.

Hermes auxiliary tools that hard-code OpenRouter's URL remain outside the
redirect.

Desktop and TUI share a Hermes profile, so both are grouped under the `hermes`
harness. The plugin supplies Hermes' session ID for reconstructed exchanges;
proxy traffic without an exact ID uses inactivity grouping. Run `mimir update`
after changing Hermes profiles to refresh managed files and credentials.

## Verification

1. Run `mimir doctor` to check installation and proxy connectivity; this does
   not verify that a direct conversation was saved.
2. Restart Hermes after a plugin or route change.
3. Start a fresh Hermes session on a direct Codex model. Ask it to use a tool
   and answer, then check `mimir session status <id>` for a saved receipt and
   `mimir turns <id> --json` for the user, assistant, and tool content.
4. Check a session that mixes direct and proxied requests for duplicate
   exchanges.
5. Ask Hermes to call `mimir_search` and confirm it returns results.
