# Next Steps

This file tracks concrete implementation gaps and technical debt remaining from the Mimir v2 architecture transition. The core Cloudflare Worker backend and capture proxy are complete; the remaining work focuses on CLI listing, test coverage, and environment hardening.

## 1. Implement Session Listing & Browsing
- **`mimir list`**: Write a standard-library-only command displaying the 20 most recent captures and outcomes as compact, human-readable receipts.
- **MCP `sessions_list`**: Upgrade the tool to return identical receipt-oriented summaries instead of raw, unformatted JSON.
- **`mimir browse`**: Plan the BentoTUI interactive terminal browser for rich keyboard navigation, filtering, and deep-linking into mock/live dashboards.

## 2. Technical Debt & API Cleanup
- **`sessions.intent`**: Connect intent extraction to the capture lifecycle or remove the dead property field from queries/indexes.
- **`POST /search`**: Align request contract parameter deserialization on the Worker; remove or implement ignored search filtering parameters.
- **Wrangler JSONC Parsing**: Fix parser crash when trailing commas or comments exist in `wrangler.jsonc` (currently parsed as strict JSON).
- **Network Timeouts**: Add bounded HTTP timeouts on all CLI and MCP requests to the Worker.

## 3. Harden Audits & Security
- **Cloudflare Access Automation**: Standardize setup of the Access application and configuration of `DASHBOARD_ACCESS_AUD` and `DASHBOARD_ACCESS_TEAM_DOMAIN`.
- **JWT Verification Tests**: Cover the Cloudflare Access JWT validation path with automated tests (currently untested in Worker integration suite).
- **Index/Recall Coverage**: Add unit and execution tests for local code indexing (`saveIndexAtomic`, symbol ranking, lookup fit, and lexical scoring).

## 4. Release & Delivery
- **Release Automation**: Configure Gated GitHub actions triggering GoReleaser builds for tagged releases.
- **`mimir update`**: Add secure, atomic local binary self-updating from stable compiled checksum paths.
