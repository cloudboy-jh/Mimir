# AGENTS.md

Mimir v2 is a self-hosted Cloudflare Worker memory plane. The Worker proxies OpenRouter-compatible requests, writes full redacted exchanges to R2, and indexes sessions/configuration in D1.

## Repository

- Worker: `worker/` TypeScript with Hono and Wrangler.
- CLI/MCP: `cmd/mimir/` Go, standard library only.
- Local code memory remains `<repo>/.mimir/index.json`.
- Sessions are remote D1 records. Do not add Git-backed session sync or session markdown.
- Raw exchanges belong in R2. Searchable metadata and R2 references belong in D1.

## Commands

```bash
cd worker && npm install && npm test && npm run typecheck
cd worker && npx wrangler deploy --dry-run
go test ./...
go build -o /tmp/mimir ./cmd/mimir
```

## Constraints

- The Worker HTTP API is canonical; CLI and MCP delegate to it.
- `x-mimir-session` is the authoritative session boundary.
- Redact before writing to R2.
- Preserve upstream streaming and persist with `waitUntil`.
- No UI, SaaS backend, multi-user tenancy, or code-index migration to D1.
