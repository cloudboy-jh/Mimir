# AGENTS.md

Mimir v2 is a self-hosted Cloudflare Worker memory plane. The Worker proxies OpenRouter-compatible requests, writes full redacted exchanges to R2, indexes sessions, commits, and devices in D1, serves an agent-first query API (also as MCP), and serves a small private dashboard from the same deployment.

## Repository

- Worker API: `worker/src/` TypeScript with Hono and Wrangler. `index.ts` exports `fetch` and the cron `scheduled` handler; `app.ts` is the composition root. Feature packages under `auth/`, `config/`, `dashboard/`, `days/`, `exchanges/`, `gateway/`, `integrations/`, `machines/`, `mcp/`, `queries/`, `search/`, `sessions/`, and `shared/` own routes, domain logic, storage operations, and focused tests. `queries/` is the one read router mounted at `/api` (machine token) and `/dashboard/api` (Access); `mcp/` exposes the same query functions as MCP tools; `maintenance.ts` runs session expiry, summaries, and FTS backfill on the cron trigger. Read paths never write. `days/day-snapshot.ts` is pure.
- Dashboard: `worker/web/` Vue 3, Vite, Tailwind CSS 4, shadcn-vue/Reka UI primitives, and Vue Router. Manage dashboard dependencies with Bun.
- Dashboard data comes from the Access-protected `/dashboard/api/*` query router. Keep browser API contracts and adapters in `worker/web/src/lib/api.ts`.
- CLI: `cmd/mimir/` is the Go entrypoint and `internal/mimircli/` owns command parsing, presentation, the read commands, and the `mimir mcp` stdio bridge. Core behavior belongs to `internal/install/` (managed files, MCP configs, embedded Worker), `internal/deployment/`, `internal/mimirapi/`, `internal/harness/` (including `hooks/` for `mimir _hook`), `internal/sessionimport/`, `internal/sessions/` (reconcile), `internal/doctor/`, and `internal/tailnet/`. Keep the Go CLI standard-library-only. Human output is static or append-only; agents use `--json`.
- Pi extension: `plugins/pi/mimir.ts` overrides Pi's OpenRouter provider to use Mimir, adds exact session headers, reports lifecycle, uploads bounded reconstructed non-OpenRouter turns, and runs `mimir _hook git` for commits. Oh My Pi: `plugins/oh-my-pi/mimir.ts`. Tests run with `bun run test:plugins`.
- OpenCode plugin: `plugins/opencode/mimir.ts` reports lifecycle events, uploads bounded reconstructed non-OpenRouter exchanges from OpenCode's session store, runs `mimir _hook git`, and registers the `mimir_*` tools that forward to `/mcp`. OpenRouter proxy exchanges remain canonical. Tests run with `bun run test:plugins`.
- Hermes plugin: `plugins/hermes/` (Python, stdlib-only) uploads bounded reconstructed exchanges for direct providers from Hermes request/response/tool hooks, reports lifecycle, runs `mimir _hook git`, and registers the `mimir_*` tools. Requests routed through the managed OpenRouter redirect remain proxy-owned. Tests run with `python -m unittest discover -s plugins/hermes -p "test_*.py"`.
- Claude Code, Codex, and Cursor: manifests under `plugins/{claude-code,codex,cursor}/` invoke the hidden `mimir _hook` adapter in `internal/harness/hooks/`, which also captures commits. `mimir install` generates their MCP configs (`.mcp.json`, `mcp.json`, and `~/.cursor/mcp.json` only when absent) launching `<mimir> mcp`. Validate with `go test ./internal/harness/hooks ./internal/install ./internal/doctor`.
- Project documentation: `README.md` is canonical for installation and usage, `docs/Spec.md` for current architecture and APIs, `docs/cli.md` for the CLI, and `docs/PRODUCT.md` and `docs/DESIGN.md` for product and visual direction.
- Shared PNG assets: `assets/images/`. Worker materialization must preserve assets imported by the dashboard. Regenerate the README architecture map from `assets/images/mimir-system-map-source.svg` with `node ./scripts/generate-system-map.mjs`.
- Production binaries embed Worker sources, the compiled production dashboard, plugins, and skills. Setup and deploy always materialize that bundle by default and never require Bun; release builds must regenerate and verify `worker/web/dist/`. Arbitrary or checkout Worker source is a development override available only through explicit `--worker-dir`; never discover a checkout or Go module-cache version implicitly.
- Managed harness files are recorded in `$MIMIR_HOME/managed.json` (installation ID, CLI, harnesses, and path → SHA-256). Update only managed files whose bytes still match; preserve conflicts and never rewrite general OpenCode configuration or merge user-owned hook or MCP files.
- `AGENTS.md` and `skills/**` Markdown remain at their structural paths for automatic discovery.
- Sessions are remote D1 records. Do not add Git-backed session sync or session Markdown.
- Session titles are first-class metadata. Preserve source precedence `manual > harness > generated > derived` and the display fallback `title > intent > id`; auxiliary title requests never replace intent.
- Git status (`none`, `committed`, `pushed`) is derived from captured commits. Agents never record, mark, or end work.
- Raw exchanges and commit patches belong in R2. Searchable metadata, the FTS index, and R2 references belong in D1.

## Commands

```bash
# Dashboard against a local Worker (wrangler dev on 8787 + Vite) from the repository root
bun run dev
bun run typecheck
bun run build
bun run check:production-assets

# Install and verify the complete Worker package from the repository root
npm --prefix worker ci
bun --cwd=worker/web install --frozen-lockfile
npm --prefix worker test        # vitest with the Workers pool; integration suites in worker/test/
npm --prefix worker run typecheck
cd worker && npx wrangler deploy --dry-run

# CLI and plugins
go test ./...
go build -o /tmp/mimir ./cmd/mimir
bun run test:plugins
python -m unittest discover -s plugins/hermes -p "test_*.py"

# Deploy (only supported path; never wrangler deploy from this checkout)
go run ./cmd/mimir deploy
```

Deployment verification must not call `/v1/chat/completions`, `/v1/messages`,
or any paid model. Use `/whoami` for connectivity and the query API for
session checks.

## Dashboard Direction

- Three pages: Day (default route: one local calendar day of sessions grouped into workstreams), Sessions (list and session page), and Devices. Sessions remain the primary object; requests are evidence inside a session.
- Every fact the dashboard shows comes from the shared query API, so agents can get it too. Do not add dashboard-only backend routes.
- Keep dashboard fetch logic and data contracts centralized in `worker/web/src/lib/api.ts`; do not store machine credentials in the browser.
- Use real browser routes for each page and the session detail.
- Use IBM Plex Sans for product UI and IBM Plex Mono only for identifiers and machine values.
- Use stock Tailwind `stone`, `zinc`, `teal`, and semantic status colors. Do not create `mimir-*` color utilities or a custom brand palette. The one exception is the Day chart, which colors slices by app with stock Tailwind hues defined in `worker/web/src/lib/day.ts`.
- Teal is reserved for focus, links, and selected state. No gradients, color blending, glow, glass, pill navigation, or generic KPI card walls.
- Maintain light and dark themes, WCAG 2.2 AA contrast, keyboard operation, visible focus, and reduced-motion behavior.
- The pixel-art wordmark is the only pixel-art treatment in the application.

## Authentication

- Machine proxy, query, MCP, and CLI requests use per-machine bearer tokens.
- Deployed `/dashboard/auth` and `/dashboard/api/*` use verified Cloudflare Access JWTs through `Cf-Access-Jwt-Assertion`.
- Cloudflare Access configuration uses `DASHBOARD_ACCESS_AUD` and `DASHBOARD_ACCESS_TEAM_DOMAIN`.
- Localhost dashboard API access may bypass Access for development.
- Do not add Mimir passwords, custom browser bearer-token storage, or a separate account/session system.

## Constraints

- The Worker HTTP API is canonical; the CLI, MCP bridge, harness plugins, and dashboard delegate to it.
- `x-mimir-session` is the authoritative session boundary.
- Redact before writing to R2.
- Preserve upstream streaming and persist with `waitUntil`.
- Read paths never write; background work belongs in the cron maintenance handler.
- No SaaS backend, multi-user tenancy, team management, or analytics suite.
