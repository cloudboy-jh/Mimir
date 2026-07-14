# Mimir

![Mimir](./mimir-readme.png)

Mimir is a self-hosted memory plane for developer agents.

```text
harness → Mimir Worker → OpenRouter
              ├─ D1: sessions, searchable metadata, config
              └─ R2: full redacted request/response archive
```

The Go binary is a client and local code indexer. The Worker is the product.

## Setup

Install the CLI, then provision and connect a deployment:

```bash
go install github.com/cloudboy-jh/mimir/cmd/mimir@latest
mimir setup --quick
```

It materializes its versioned Worker package under `~/.mimir/worker`, authenticates through Wrangler, provisions D1 and R2, applies migrations, prompts for the OpenRouter key, deploys the Worker, writes `~/.mimir/config`, and verifies `/whoami`. Use `--minimal` to deploy with persistence disabled, or pass `--worker-dir` to use a local Worker checkout.

The local deployment URL is stored in `~/.mimir/config`; the machine credential is isolated in `~/.mimir/token` with mode `0600` so supported harnesses can reference it without copying the value into their configuration.

Connect another machine to the same deployment through the owning Cloudflare account:

```bash
go install github.com/cloudboy-jh/mimir/cmd/mimir@latest
mimir login
```

Each machine receives an independent token. Only its SHA-256 hash is stored in D1, so connecting another machine does not invalidate existing machines.

## Components

- `worker/`: Hono Worker, OpenRouter-compatible proxy, HTTP API, D1/R2 persistence.
- `cmd/mimir/`: Go CLI and MCP client, plus the retained local code indexer.
- `skills/mimir-setup/`: deployment procedure.
- `skills/mimir-use/`: agent operating procedure.

For agent-driven installation, explicitly install and invoke `mimir-setup`. It uses `mimir setup --json` and `mimir login --json`, then follows a verified OpenCode or Claude Code wiring reference. Credentials are never requested through chat.

## Development

```bash
cd worker
npm install
npm test
npm run typecheck
npx wrangler deploy --dry-run

go test ./...
go build -o /tmp/mimir ./cmd/mimir
```

## API

All endpoints accept `Authorization: Bearer <machine-token>`. Anthropic-compatible clients may send the same token through `x-api-key`.

- `POST /v1/chat/completions`
- `POST /v1/messages`
- `GET /v1/models`
- `GET /whoami`
- `GET /sessions`
- `GET /sessions/:id`
- `POST /sessions/:id/mark`
- `POST /sessions/:id/outcome`
- `GET /log/:key`
- `POST /search`
- `GET /config`
- `PUT /config`
