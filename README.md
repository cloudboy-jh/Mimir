# Mimir

![Mimir](./mimir-readme.png)

**Your coding agents forget everything. Mimir remembers.**

Mimir is a personal memory plane for developer agents. It sits between your
agent harness and OpenRouter, preserves the upstream stream, and records each
redacted exchange in infrastructure you own.

There is no Mimir account, hosted backend, or third-party database. The Worker,
database, and archive run in your Cloudflare account.

```text
OpenCode / Claude Code / any compatible client
                       |
                       v
              +----------------+
              |  Mimir Worker  |--------> OpenRouter
              +-------+--------+
                      |
             +--------+--------+
             |                 |
             v                 v
        Cloudflare D1     Cloudflare R2
        sessions, usage,  complete redacted
        search, outcomes  exchanges
```

## What You Get

- Full OpenAI- and Anthropic-compatible proxy endpoints.
- Unmodified streaming responses with asynchronous persistence.
- Queryable sessions grouped by harness-provided or heuristic boundaries.
- Request counts, token usage, models, files, errors, outcomes, and excerpts.
- Complete redacted request/response bodies in R2.
- Search across remote sessions and the current repository's local code index.
- A zero-dependency Go CLI and MCP server.
- Independent credentials for every machine, with only token hashes stored in
  D1.

The goal is not another transcript viewer. Mimir gives agents one place to ask:
what did we try, what failed, what shipped, and where is the relevant code?

## Quick Start

You need:

- a Cloudflare account;
- an OpenRouter API key;
- Go and Node.js with npm installed.

Install the CLI and deploy Mimir:

```bash
go install github.com/cloudboy-jh/mimir/cmd/mimir@latest
mimir setup --quick
```

Setup opens Cloudflare authentication when needed, creates a D1 database and R2
bucket, applies migrations, stores the OpenRouter key as a Worker secret,
deploys the Worker, creates a machine credential, and verifies the deployment.
The OpenRouter key is entered through a masked local prompt and is never written
to Mimir's configuration files.

Confirm the connection:

```bash
mimir whoami
```

Local connection state is deliberately small:

```text
~/.mimir/config   Worker URL
~/.mimir/token    machine credential, mode 0600
```

To connect another machine to the same deployment:

```bash
go install github.com/cloudboy-jh/mimir/cmd/mimir@latest
mimir login
```

`mimir login` uses ownership of the Cloudflare account to discover the existing
deployment and issue a new machine token. It does not rotate credentials on
machines that are already connected.

## Connect A Harness

Route your harness's OpenRouter traffic through the Mimir Worker and register
`mimir serve` as an MCP server.

### OpenCode

Use the [OpenCode wiring guide](skills/mimir-setup/references/opencode.md).
The integration reads the token from `~/.mimir/token`, registers the MCP server,
and adds a small plugin that sends the exact OpenCode session ID through
`x-mimir-session`.

Result: exact session boundaries, repository metadata, and Mimir tools inside
OpenCode.

### Claude Code

Use the [Claude Code wiring guide](skills/mimir-setup/references/claude-code.md).
It configures Mimir as the Anthropic-compatible gateway, reads the machine token
through `apiKeyHelper`, and registers the MCP server.

Claude Code does not expose a dynamic inference-header hook, so Mimir groups its
requests using repository and time-gap heuristics.

### Agent-Driven Setup

Install and explicitly invoke the [`mimir-setup`](skills/mimir-setup/SKILL.md)
skill. It uses the CLI's JSON setup states and the harness-specific guides
above. The procedure never asks the user to paste a credential into chat.

The [`mimir-use`](skills/mimir-use/SKILL.md) skill teaches an agent when to
search memory, inspect a session, index code, and record an outcome.

## Use It

```bash
# Deployment identity and memory counts
mimir whoami

# Recent sessions
mimir sessions

# One session with exchanges, files, errors, and R2 references
mimir session <session-id>

# Search remote session memory and the local code index
mimir search "authentication regression"

# Label what happened to an attempt
mimir mark <session-id> promoted
mimir mark <session-id> discarded

# Infer an outcome from the current Git repository
mimir outcome git <session-id>

# Inspect or update deployment-wide behavior
mimir config get
mimir config set save.exclude_repos '["scratch-*"]'

# Build and query the local repository index
mimir index
mimir recall "where are access tokens validated?"

# Run the MCP server over stdio
mimir serve
```

Session outcomes use a closed vocabulary:

| Outcome | Meaning |
| --- | --- |
| `promoted` | The work reached a durable branch or was explicitly accepted. |
| `discarded` | The attempt was rejected or its branch was deleted. |
| `abandoned` | The attempt expired without a durable result. |
| `unknown` | No reliable outcome is available yet. |

## How Capture Works

1. A client sends an OpenAI or Anthropic request to the Mimir Worker.
2. Mimir authenticates the machine token and replaces it with the OpenRouter
   secret before forwarding the request.
3. The upstream response stream is split: one branch goes directly to the
   client, while the other is reconstructed in `waitUntil`.
4. Mimir redacts the request and response before writing the complete exchange
   to R2.
5. D1 receives the searchable session metadata and the R2 object reference.

`x-mimir-session` is authoritative when a harness can provide it. Without the
header, Mimir groups requests by repository and a configurable inactivity gap.

## Storage And Security

| Data | Location |
| --- | --- |
| Full redacted exchanges | Your R2 bucket |
| Sessions and searchable metadata | Your D1 database |
| Deployment configuration | Your D1 database |
| OpenRouter credential | Encrypted Worker secret |
| Machine credentials | Local files; SHA-256 hashes in D1 |
| Repository code index | `<repo>/.mimir/index.json` |

Built-in redaction covers common keys, tokens, bearer credentials, passwords,
and secrets. Add deployment-wide patterns without redeploying:

```bash
mimir config set redact.patterns '["builtin", "customer-[0-9]+"]'
```

Saving is enabled by default. It can be filtered by repository or model, or
disabled entirely:

```bash
mimir config set save.exclude_models '["openai/gpt-4o-mini"]'
mimir config set save.enabled false
```

## HTTP API

The Worker API is canonical; the CLI and MCP server are clients of it. Send the
machine token as `Authorization: Bearer <token>`. Anthropic-compatible clients
may send the same token through `x-api-key`.

| Method | Path | Purpose |
| --- | --- | --- |
| `POST` | `/v1/chat/completions` | OpenAI-compatible proxy |
| `POST` | `/v1/messages` | Anthropic-compatible proxy |
| `GET` | `/v1/models` | OpenRouter model catalog |
| `GET` | `/whoami` | Deployment identity and counts |
| `GET` | `/sessions` | Filtered recent sessions |
| `GET` | `/sessions/:id` | Session details and exchanges |
| `POST` | `/sessions/:id/mark` | Set an explicit outcome |
| `POST` | `/sessions/:id/outcome` | Apply an outcome adapter |
| `GET` | `/log/:key` | Read a redacted R2 exchange |
| `POST` | `/search` | Token-budgeted session search |
| `GET` | `/config` | Read deployment configuration |
| `PUT` | `/config` | Update deployment configuration |

## Scope

Mimir is intentionally opinionated:

- It is a personal, single-developer deployment.
- Cloudflare Workers, D1, and R2 are required.
- OpenRouter is the upstream model gateway.
- There is no hosted service, team tenancy, or dashboard.
- Raw exchanges live in R2; searchable metadata lives in D1.
- Code indexing stays local until there is evidence that moving it is useful.

See [`spec.md`](spec.md) for the complete design and [`next-steps.md`](next-steps.md)
for explicitly deferred work.

## Development

```bash
cd worker
npm install
npm test
npm run typecheck
npx wrangler deploy --dry-run

cd ..
go test ./...
go build -o /tmp/mimir ./cmd/mimir
```

Repository layout:

| Path | Contents |
| --- | --- |
| `worker/` | Hono Worker, D1 migrations, R2 capture, and tests |
| `cmd/mimir/` | Go CLI, MCP server, setup flow, and local code indexer |
| `skills/mimir-setup/` | Safe deployment and harness wiring procedure |
| `skills/mimir-use/` | Agent memory operating procedure |
