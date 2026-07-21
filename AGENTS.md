# AGENTS.md

Mimir v2 is a self-hosted Cloudflare Worker memory plane. The Worker proxies OpenRouter-compatible requests, writes full redacted exchanges to R2, indexes sessions and configuration in D1, and serves a private dashboard from the same deployment.

## Repository

- Worker API: `worker/src/` TypeScript with Hono and Wrangler. `app.ts` assembles middleware and routes; `routes/`, `auth.ts`, `proxy.ts`, `capture.ts`, `sessions.ts`, and `config.ts` own backend behavior.
- Dashboard: `worker/web/` Vue 3, Vite, Tailwind CSS 4, shadcn-vue/Reka UI primitives, and Vue Router. Manage dashboard dependencies with Bun.
- Dashboard data comes from the Access-protected `/dashboard/api/*` routes. Keep browser API contracts and adapters in `worker/web/src/lib/api.ts`.
- CLI/MCP: `cmd/mimir/` is the Go entrypoint. Focused implementation files live in `internal/mimircli/`, including `mcp.go`, `client.go`, `connection.go`, `index.go`, `recall.go`, and deployment helpers. Keep the Go CLI standard-library-only.
- Project documentation: `README.md` is canonical for installation and usage, `docs/Spec.md` for current architecture, and `docs/PRODUCT.md` and `docs/DESIGN.md` for product and visual direction.
- Shared PNG assets: `assets/images/`. Worker materialization must preserve assets imported by the dashboard.
- `AGENTS.md` and `skills/**` Markdown remain at their structural paths for automatic discovery.
- Local code memory remains `<repo>/.mimir/index.json`.
- Sessions are remote D1 records. Do not add Git-backed session sync or session Markdown.
- Raw exchanges belong in R2. Searchable metadata and R2 references belong in D1.

## Commands

```bash
# Dashboard-only development from the repository root
bun run dev
bun run typecheck
bun run build

# Install and verify the complete Worker package from the repository root
npm --prefix worker ci
bun --cwd=worker/web install --frozen-lockfile
npm --prefix worker test
npm --prefix worker run typecheck
cd worker && npx wrangler deploy --dry-run

# CLI/MCP
go test ./...
go build -o /tmp/mimir ./cmd/mimir

# Deploy (only supported path; never wrangler deploy from this checkout)
go run ./cmd/mimir deploy
```

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

- Machine proxy, CLI, and MCP requests use per-machine bearer tokens.
- Deployed dashboard API and redacted-log routes use verified Cloudflare Access JWTs through `Cf-Access-Jwt-Assertion`.
- Cloudflare Access configuration uses `DASHBOARD_ACCESS_AUD` and `DASHBOARD_ACCESS_TEAM_DOMAIN`.
- Localhost dashboard API access may bypass Access for development.
- Do not add Mimir passwords, custom browser bearer-token storage, or a separate account/session system.

## Constraints

- The Worker HTTP API is canonical; CLI and MCP delegate to it.
- `x-mimir-session` is the authoritative session boundary.
- Redact before writing to R2.
- Preserve upstream streaming and persist with `waitUntil`.
- Keep the dashboard a client of the canonical Worker API when backend integration resumes.
- No SaaS backend, multi-user tenancy, team management, analytics suite, or code-index migration to D1.
