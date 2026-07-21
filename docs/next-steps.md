# Next Steps

This file tracks concrete implementation gaps and technical debt remaining from the Mimir v2 architecture transition. The core Cloudflare Worker backend and capture proxy are complete.

## Done

- **`mimir list`** — receipt-oriented session listing with `--repo`, `--outcome`, and `--limit` filters (`internal/mimircli/receipts.go`).
- **MCP `sessions_list`** — returns the same compact receipts as `mimir list` instead of raw JSON.
- **`sessions.intent`** — populated on capture from the first redacted user message (`deriveIntent` in `worker/src/capture.ts`); first exchange wins, searchable via `/search`.
- **`POST /search`** — the `types` parameter now filters match sources (`intent`, `excerpts`, `files`, `errors`); unknown types return 400.
- **Wrangler JSONC parsing** — `stripJSONC` tolerates comments and trailing commas before strict decoding.
- **Network timeouts** — all CLI/MCP Worker calls use a 30s `http.Client`; release downloads use a 5m client.
- **Cloudflare Access automation** — `mimir setup` provisions the `mimir-dashboard` Access application, an allow policy (`--access-email` or `MIMIR_ACCESS_EMAIL`), and writes `DASHBOARD_ACCESS_AUD`/`DASHBOARD_ACCESS_TEAM_DOMAIN` into `wrangler.jsonc` when `CLOUDFLARE_API_TOKEN` is set; otherwise it prints the manual checklist.
- **JWT verification tests** — the Access JWT path is covered with in-test RS256 keys and a stubbed JWKS endpoint (valid, wrong audience, wrong issuer, expired, garbage).
- **Index/recall coverage** — unit and end-to-end tests for `saveIndexAtomic`, `parseFile`, `score`, `rank`, `fit`, `locateSymbol`, and `queryRecall`.
- **Release automation** — `.github/workflows/release.yml` runs the full suite on `v*` tags, then GoReleaser in a gated `release` environment.
- **`mimir update`** — verified, atomic self-update from GitHub release assets with SHA-256 checks; refuses package-manager-owned installs.
- **Live dashboard** — Sessions, Requests, Overview, R2 payload detail, pagination, and user outcome updates now read the Access-protected Worker APIs.
- **`mimir deploy`** — single supported deploy path: materialize Worker, build dashboard, write the real D1 ID into the materialized config, `wrangler deploy`.
- **opencode integration** — `mimir login` writes `~/.config/opencode/plugins/mimir.ts` (OpenRouter provider routed through the Worker with session headers) and upserts the `mimir` MCP entry in `opencode.json`, both idempotent.
- **`mimir access`** — one-command dashboard Access finish: full automation with an API token (prompted inline during setup), or `--aud`/`--team-domain` application after manual app creation; automated Access apps now cover the bare Worker hostname.

## Remaining

- **`mimir browse`** — parked. A TUI requires a dependency, which conflicts with the standard-library-only CLI constraint. Decide whether the constraint gets a carve-out before implementation.

## Operational Follow-ups

- Create the `release` GitHub environment with required reviewers so the release workflow gate is active.
- Tag `v0.x.0` to exercise the release path for real.
- Fix pre-existing Windows test failures in `setup_test.go` (`TestBuildDashboard`, `TestReadCloudflareIdentity`, `TestConnectionManifestContainsNoCredential`) — all three fail on a clean checkout in this environment.
