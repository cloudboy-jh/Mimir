# AGENTS.md

Mimir v2 is a self-hosted Cloudflare Worker memory plane. The Worker proxies OpenRouter-compatible requests, writes full redacted exchanges to R2, indexes sessions and configuration in D1, and serves a private dashboard from the same deployment.

## Repository

- Worker API: `worker/src/` TypeScript with Hono and Wrangler. `app.ts` is the composition root; feature packages under `auth/`, `config/`, `dashboard/`, `exchanges/`, `gateway/`, `integrations/`, `machines/`, `search/`, `sessions/`, and `shared/` own routes, domain logic, storage operations, and focused tests.
- Dashboard: `worker/web/` Vue 3, Vite, Tailwind CSS 4, shadcn-vue/Reka UI primitives, and Vue Router. Manage dashboard dependencies with Bun.
- Dashboard data comes from the Access-protected `/dashboard/api/*` routes. Keep browser API contracts and adapters in `worker/web/src/lib/api.ts`.
- CLI: `cmd/mimir/` is the Go entrypoint and `internal/mimircli/` owns command parsing, presentation, and package adapters. Core behavior belongs to `internal/install/`, `internal/deployment/`, `internal/mimirapi/`, `internal/harness/`, `internal/sessions/`, `internal/codeindex/`, `internal/search/`, and `internal/doctor/`. Keep the Go CLI standard-library-only.
- Keep terminal presentation code under `internal/ui/` with dependency direction `mimircli -> selector / receipts / lineoutput -> appframe -> bentotui`. Mimir has no terminal application, session browser, or agent TUI; the dashboard is the visual browser and agents query the machine-readable CLI. Human-only bounded selectors may use raw input and in-place redraw only when both streams are TTYs. Machine-readable and operational command output remains static or append-only. See `internal/ui/README.md`.
- Pi extension: `plugins/pi/mimir.ts` overrides Pi's OpenRouter provider to use Mimir, adds exact session headers, reports lifecycle, and uploads bounded reconstructed non-OpenRouter turns. It is installed as a receipt-owned global Pi extension. Tests run with `bun test plugins/pi/`.
- OpenCode plugin: `plugins/opencode/mimir.ts` reports lifecycle events and uploads bounded reconstructed non-OpenRouter exchanges from OpenCode's session store. OpenRouter proxy exchanges remain canonical. Single dependency-free file; tests run with `bun test plugins/opencode/`.
- Hermes plugin: `plugins/hermes/` (Python, stdlib-only) reports event-only completed-turn summaries for Nous portal and direct providers; it is liveness-only when the managed OpenRouter redirect is active. It registers `pre_api_request`, `post_llm_call`, `on_session_start`, and `on_session_finalize`. Tests run with `python -m unittest discover -s plugins/hermes -p "test_*.py"`.
- Claude Code, Codex, and Cursor: manifests under `plugins/{claude-code,codex,cursor}/` invoke the hidden `mimir _hook` adapter in `internal/harness/hooks/`. Supported prompt/completion hooks produce bounded reconstructed exchanges, not proxy transport archives. Validate with `go test ./internal/harness/hooks ./internal/install ./internal/doctor`.
- Project documentation: `README.md` is canonical for installation and usage, `docs/Spec.md` for current architecture, and `docs/PRODUCT.md` and `docs/DESIGN.md` for product and visual direction.
- Shared PNG assets: `assets/images/`. Worker materialization must preserve assets imported by the dashboard. Regenerate the README architecture map with `node ./scripts/generate-system-map.mjs`.
- Production binaries embed Worker sources, the compiled production dashboard, plugins, and skills. Setup and deploy always materialize that bundle by default and never require Bun; release builds must regenerate and verify `worker/web/dist/`. Arbitrary or checkout Worker source is a development override available only through explicit `--worker-dir`; never discover a checkout or Go module-cache version implicitly.
- `internal/demoassets/static/` is the checked-in generated fixture dashboard embedded only by `mimir demo`. Rebuild it with `bun run build:demo`; never add it to the production Worker bundle or materialization identity.
- Managed harness artifacts use `$MIMIR_HOME/install-receipt.json` ownership and `$MIMIR_HOME/install-log.jsonl`. Update only exact owned, unmodified files; preserve conflicts and never rewrite general OpenCode configuration or merge user-owned hook files.
- `AGENTS.md` and `skills/**` Markdown remain at their structural paths for automatic discovery.
- Local code memory remains `<repo>/.mimir/index.json`.
- Sessions are remote D1 records. Do not add Git-backed session sync or session Markdown.
- Session titles are first-class metadata. Preserve source precedence `manual > harness > generated > derived` and the display fallback `title > intent > id`; auxiliary title requests never replace intent.
- Raw exchanges belong in R2. Searchable metadata and R2 references belong in D1.

## Commands

```bash
# Fixture and live dashboard development from the repository root
bun run dev
bun run dev:live
bun run typecheck
bun run build

# Install and verify the complete Worker package from the repository root
npm --prefix worker ci
bun --cwd=worker/web install --frozen-lockfile
npm --prefix worker test
npm --prefix worker run typecheck
cd worker && npx wrangler deploy --dry-run

# CLI
go test ./...
go build -o /tmp/mimir ./cmd/mimir
bun test plugins/pi/ plugins/opencode/
python -m unittest discover -s plugins/hermes -p "test_*.py"

# Deploy (only supported path; never wrangler deploy from this checkout)
go run ./cmd/mimir deploy
```

Deployment verification must not call `/v1/chat/completions`, `/v1/messages`,
or any paid model. Use `/whoami` for connectivity and the direct session APIs
for session lifecycle checks.

## Dashboard Direction

- Sessions are the default route and primary product object. Requests are supporting evidence, not the center of the product.
- Keep dashboard fetch logic and live data contracts centralized in `worker/web/src/lib/api.ts`; do not store machine credentials in the browser.
- Use real browser routes for Sessions, Requests, Overview, and detail pages.
- Use IBM Plex Sans for product UI and IBM Plex Mono only for identifiers and machine values.
- Use stock Tailwind `stone`, `zinc`, `teal`, and semantic status colors. Do not create `mimir-*` color utilities or a custom brand palette.
- Teal is reserved for focus, links, and selected state. No gradients, color blending, glow, glass, pill navigation, or generic KPI card walls.
- Maintain light and dark themes, WCAG 2.2 AA contrast, keyboard operation, visible focus, and reduced-motion behavior.
- The pixel-art wordmark is the only pixel-art treatment in the application.

## Authentication

- Machine proxy and CLI requests use per-machine bearer tokens.
- Deployed dashboard API and redacted-log routes use verified Cloudflare Access JWTs through `Cf-Access-Jwt-Assertion`.
- Cloudflare Access configuration uses `DASHBOARD_ACCESS_AUD` and `DASHBOARD_ACCESS_TEAM_DOMAIN`.
- Localhost dashboard API access may bypass Access for development.
- Do not add Mimir passwords, custom browser bearer-token storage, or a separate account/session system.

## Constraints

- The Worker HTTP API is canonical; the CLI and harness plugins delegate to it.
- `x-mimir-session` is the authoritative session boundary.
- Redact before writing to R2.
- Preserve upstream streaming and persist with `waitUntil`.
- Keep the dashboard a client of the canonical Worker API when backend integration resumes.
- No SaaS backend, multi-user tenancy, team management, analytics suite, or code-index migration to D1.
