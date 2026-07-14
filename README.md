# Mimir

![Mimir](./mimir-readme.png)

**Your coding agents forget everything. Mimir remembers.**

Mimir is a personal memory plane for developer agents. It runs in your own
Cloudflare account, sits between your agent harness and OpenRouter, and records
redacted model exchanges without interrupting the upstream stream.

There is no Mimir account, hosted backend, team workspace, or routine workflow
to manage. Set it up once and keep working in your agent harness.

```text
Any OpenAI / Anthropic-compatible harness
          |
          v
  +----------------+
  |  Mimir Worker  |-------> OpenRouter
  +-------+--------+
          |
     +----+----+
     |         |
     v         v
    D1        R2
  memory   redacted archive
```

## Set Up

You need a Cloudflare account, an OpenRouter API key, Go, and Node.js with npm.

```bash
go install github.com/cloudboy-jh/mimir/cmd/mimir@latest
mimir setup
```

Mimir will:

- authenticate with Cloudflare;
- provision D1 and R2 in your account;
- store the OpenRouter key as a Worker secret;
- deploy and verify the Worker;
- produce a standard connection manifest for the active harness;
- create a machine-specific credential.

Setup shows one status indicator while it works and one summary when it is
finished. The `mimir-setup` skill applies the returned OpenAI/Anthropic base
URLs, credential-file path, MCP command, and optional telemetry headers to the
active harness. Mimir does not contain per-harness backend code.

## Connect Another Machine

```bash
go install github.com/cloudboy-jh/mimir/cmd/mimir@latest
mimir login
```

Login discovers the existing deployment through your Cloudflare account,
registers an independent credential for the new machine, and returns the same
connection manifest. Existing machine credentials remain valid.

That is the complete human workflow. Memory capture and retrieval happen in the
background through the configured gateway and MCP server.

## What Happens In The Background

Each completed model request becomes one exchange:

1. Mimir authenticates the local machine credential.
2. It replaces that credential with the OpenRouter Worker secret.
3. The response stream continues directly to the harness.
4. A second stream branch is reconstructed and redacted asynchronously.
5. R2 receives the complete redacted exchange.
6. D1 receives searchable metadata, usage, files, errors, and the R2 reference.

Agents search this memory through MCP when prior work is relevant. Users do not
need to search, index, label, or manage sessions manually.

## Session Lifecycle

A Mimir session is derived from proxy telemetry.

- Harnesses may send `x-mimir-session` for exact conversation identity.
- Harnesses that cannot send it work without an adapter; Mimir groups their
  traffic using optional repository/harness metadata and a 15-minute gap.
- Inactive sessions are identified from the log when memory is queried or new
  traffic arrives.
- Resuming an exact session ID reactivates the same Mimir session.
- Outcomes remain `unknown` until Git or an agent has evidence otherwise.

Every completed exchange is persisted immediately and retained indefinitely.
There is no retention policy, compaction service, queue, or shared storage
system. This is deliberately optimized for one developer per deployment.

## Ownership And Security

| Data | Location |
| --- | --- |
| Full redacted exchanges | Your R2 bucket |
| Sessions and searchable metadata | Your D1 database |
| OpenRouter credential | Encrypted Worker secret |
| Machine credential | `~/.mimir/token`, mode `0600` |
| Worker URL | `~/.mimir/config` |
| Repository code index | `<repo>/.mimir/index.json` |

Machine credentials are independent and only their SHA-256 hashes are stored in
D1. Built-in redaction covers common keys, bearer credentials, tokens,
passwords, and secrets before anything is written to R2.

At personal scale, direct writes are intentionally boring. One model request
produces one R2 object and one D1 batch. R2 includes one million writes and 10 GB
of storage per month before usage charges; no batching infrastructure is needed.

## Scope

- Personal, single-developer deployments
- Cloudflare Workers, D1, and R2
- OpenRouter as the model gateway
- Any OpenAI- or Anthropic-compatible harness
- Permanent raw memory in the user's account
- No hosted service, tenancy, dashboard, or normal post-setup CLI workflow

See [`spec.md`](spec.md) for the full design and
[`next-steps.md`](next-steps.md) for deferred work.

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
