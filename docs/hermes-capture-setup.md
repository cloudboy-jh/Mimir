# Hermes Session Capture Setup

Date: 2026-09-27
Status: Implemented; verified with a fresh Hermes Desktop session on direct OpenAI Codex, including an archived terminal tool result and a saved receipt.

## Two capture paths

Hermes capture has two cooperating paths:

1. **Proxy path** — redirects Hermes' built-in OpenRouter provider through
   the Worker. This archives the full redacted provider exchange.
2. **Plugin path** — [`plugins/hermes/`](../plugins/hermes/) uses Hermes'
   request, response, and tool hooks for Codex, Nous portal, OAuth, and
   other providers whose traffic does not traverse the proxy. It sends bounded
   reconstructed exchanges to `/sessions/:id/exchanges`; supported tool
   interactions are attached to their provider request. These are not
   byte-for-byte provider transport archives.

Classification is per API request using `pre_api_request` provider/base-URL
metadata, not a session-wide provider setting. Requests through the managed
OpenRouter redirect remain proxy-owned. A session can use both paths without
the plugin uploading duplicates of proxied calls. Direct activity activates
the plugin's heartbeat and session-end lifecycle reporting; a session using
only the proxy leaves lifecycle ownership with the proxy. `on_session_start`
intentionally emits nothing.

Hermes hook payloads are sanitized and bounded by Hermes before the plugin
receives them. When Hermes truncates a large provider request, the plugin
records the actual user message as a `user_turn` reconstruction rather than
claiming it saved the complete provider prompt. It cannot reconstruct content
absent from the hooks. The Worker redacts reconstructed request, response, and
tool data before writing R2 and indexing D1. The capture receipt comes from
persisted exchange state, never from plugin-load health, a heartbeat, or work
outcome. Failed uploads reported to the Worker are failures, not successful
turns; a plugin that is offline cannot tell the Worker about a local failure
until reconnect.

Historical Hermes sessions are **not** imported automatically. Hermes may
retain message and tool history in its local `state.db`, but the current
`mimir import` and `mimir backfill` commands support only Pi and OpenCode.
Recovering retained older Hermes sessions would require a separate, explicit
opt-in importer; deleted history cannot be recovered.

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
like the CLI. Delivery must not block or raise into Hermes. The server-side
silence timer finalizes sessions even when the process dies before an end
event arrives; finalization does not prove that any exchange was saved.

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

The proxy captures Hermes' effective OpenRouter provider, including
mid-session model changes. Direct Nous, Anthropic OAuth, OpenAI Codex, Gemini,
and other transports bypass the proxy and are captured through Hermes'
plugin hooks where they expose request/response content. Mimir does not
intercept TLS traffic. A direct-provider request with unavailable hook content
cannot be represented as a saved exchange.

Hermes auxiliary tools that hard-code OpenRouter's URL remain outside the
managed redirect. The plugin captures only calls exposed by Hermes' supported
hooks; it does not claim to archive unobserved auxiliary traffic.

Desktop and TUI use the same Hermes profile, so a static installation cannot
reliably distinguish them. Both are grouped under the `hermes` harness. The
plugin supplies Hermes' session ID for reconstructed exchanges; proxy traffic
without an exact ID still uses the inactivity fallback. Run `mimir update`
after changing Hermes profiles to refresh managed artifacts and credentials.

## Verification

1. Run `mimir doctor` to check installation and proxy connectivity; this
   does not verify that a direct conversation was saved.
2. Restart Hermes after a plugin or route change.
3. Start a fresh Hermes Desktop session on a direct Codex model. Ask it to
   use a tool and answer; check `mimir session status <id> --json` for a saved
   receipt, `mimir session get <id> --json` for indexed exchanges, and the
   redacted archive for user, assistant, and supported tool content.
4. Check a session that switches between direct and proxied requests for
   duplicate exchanges. Work outcome remains independent from capture.
