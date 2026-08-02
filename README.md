<p align="center">
  <img src="assets/images/mimir-readme.png" width="620" alt="Mimir">
</p>

# Private memory for coding agents

Mimir records what your coding agents attempted, which models and files were
involved, what failed, and whether the work actually landed. The Worker,
storage, and private dashboard run inside your Cloudflare account.

```bash
go run github.com/cloudboy-jh/mimir/cmd/mimir@latest install
mimir setup
```

Setup provisions D1 and R2, builds and deploys the Worker and dashboard, stores
your OpenRouter key through a masked prompt, and connects the current machine.

Requirements: a Cloudflare account, an OpenRouter API key, Go 1.25+, Node.js 22
with npm, and Bun. To connect another machine to the same deployment, install
the CLI there and run `mimir login`.

## What Mimir remembers

A session is one episode of agent work, not a bag of disconnected requests.
Mimir reconstructs the session so you can see:

- the task, repository, app, models, duration, and token use;
- full redacted exchanges captured through the model proxy;
- supporting runs, tool-touched files, real error signals, and model switches;
- whether durable capture succeeded;
- whether the work landed, was discarded, was abandoned, or remains unresolved.

That makes prior work useful before the next attempt:

```text
Search: "token validation"

Previous session
  outcome     discarded
  models      gpt-5.6-sol, claude-opus-5
  files       auth.ts, proxy.ts
  error       token validation failed

Result: the next agent avoids the same dead end.
```

Mimir deliberately keeps two facts separate:

- **Capture state** describes durable memory: Empty, Pending, Saved, Failed, or
  Partial.
- **Work outcome** describes the result: Landed, Discarded, Abandoned, or
  Unresolved.

## How Mimir works

![Coding agents send either complete proxied model traffic or lightweight harness events into a private Mimir Worker. The Worker redacts and organizes the data, stores full exchanges in R2 and session metadata in D1, and serves the CLI and private dashboard.](assets/images/mimir-system-map.svg)

There are two inputs to one session record:

1. **Proxied model traffic** carries complete OpenRouter requests and streaming
   responses. The Worker preserves streaming, redacts the exchange, writes the
   full object to R2, and indexes searchable metadata in D1.
2. **Harness events** carry turns, heartbeats, session ends, and evidenced work
   outcomes from OpenCode and Hermes. They keep sessions live and cover provider
   traffic that does not pass through the proxy, but they are not full transport
   archives.

`x-mimir-session` is the authoritative session boundary when available. Traffic
without an exact session ID uses bounded inactivity grouping.

| Traffic path | Full redacted exchange | Session lifecycle | Searchable exchange metadata |
| --- | --- | --- | --- |
| Redirected OpenRouter | Yes, after persistence succeeds | Yes | Yes |
| OpenCode OAuth, subscription, or direct provider | No | Plugin events | No |
| Hermes Nous portal, OAuth, or direct provider | No | Plugin events | No |
| Other tools using Mimir proxy URLs | Yes, after persistence succeeds | Capture events only | Yes |

Hermes' managed OpenRouter route suppresses duplicate plugin turns for known
proxied requests. A scheduled capture response only means persistence was
queued. `mimir session status` is the authority for durable capture.

## Use the memory

Search before starting another attempt:

```bash
mimir search "token validation" --json
```

Inspect a complete session record:

```bash
mimir session get <id> --json
```

Verify durable capture:

```bash
mimir session status <id> --json
```

Record an evidenced result:

```bash
mimir session outcome <id> landed --reason "merged in PR 42"
```

Open the private dashboard:

```bash
mimir dashboard
```

The dashboard leads with sessions. Requests remain supporting evidence, one
click away when you need the raw record.

## Connect an agent

### OpenCode

The installer manages the Mimir plugin and skills without rewriting general
OpenCode JSON or JSONC. The plugin reports turns, lifecycle events, model
switches, and Git outcome evidence over HTTP. Restart OpenCode after an install
or update. See [OpenCode capture setup](docs/opencode-capture-setup.md).

### Hermes desktop and TUI

Mimir redirects Hermes' built-in OpenRouter provider through `/v1/hermes` and
enables a plugin for direct-provider turns and lifecycle events. Restart Hermes
after an install or update. See [Hermes capture setup](docs/hermes-capture-setup.md).

### Other harnesses and tools

```bash
mimir connection
```

The connection manifest supplies proxy base URLs, credential sources, and
supported metadata headers. The CLI inspects and controls memory; it does not
capture unrelated model traffic.

## Your account, your data

There is no Mimir account, hosted backend, shared memory service, or browser
machine-token storage.

- The Worker and dashboard run in your Cloudflare account.
- Full redacted proxy exchanges live in R2.
- Searchable metadata, configuration, and lifecycle state live in D1.
- A Session Durable Object coordinates liveness, retries, reopening, and
  transcript finalization.
- Dashboard APIs and redacted-log routes require verified Cloudflare Access
  JWTs. Machine APIs use independent per-machine bearer tokens.
- Local code recall stays in `<repo>/.mimir/index.json` and is never uploaded.

Redaction runs before R2 persistence and excerpt generation. It reduces
accidental retention, but it cannot guarantee removal of every secret.

## Command reference

```text
mimir install                         reconcile managed local artifacts
mimir setup [--quick]                 provision and deploy Mimir
mimir login                           connect another machine
mimir deploy [--worker-dir DIR]       deploy packaged Worker and dashboard changes
mimir dashboard                       open the private dashboard
mimir tui                             open the persistent sessions + agent terminal
mimir list [filters] [--json]         browse recent sessions (interactive on a TTY)
mimir search <query> [--json]         search saved session memory
mimir session get <id> [--json]       inspect one session
mimir session status <id> [--json]    verify durable capture
mimir session end <id> [--json]       finalize the active generation
mimir session outcome <id> <value>    record an evidenced work outcome
mimir doctor [--json] [--tui]         validate deployment, integrations, and optional TUI prerequisites
mimir update [--check]                update Mimir and managed integrations
mimir uninstall [--keep-binary]       remove verified managed artifacts
```

Deploy only with `mimir deploy`. The packaged Worker and dashboard are always
the default source; arbitrary source requires explicit `--worker-dir <path>`.
Run `mimir help advanced` for code recall, connection, configuration, and
diagnostic commands.

## Dashboard development

Run the dashboard against the deterministic development dataset:

```bash
npm --prefix worker ci
bun --cwd=worker/web install --frozen-lockfile
bun run dev
```

The fixture dataset covers multi-model sessions, supporting runs, commits,
diffs, errors, outcome history, and empty states. It runs Vite with HMR on
`127.0.0.1:5173` without requiring local Cloudflare bindings.

Use `bun run dev:live` to apply local D1 migrations and run the dashboard
against the Worker on `127.0.0.1:8787`. Vite proxies the Access handoff,
dashboard APIs, and log-object requests to the Worker. Local requests use the
clearly marked development identity and never require browser machine
credentials.

## Documentation

- [Installation and managed-file ownership](docs/installation.md)
- [CLI and machine-readable contract](docs/cli.md)
- [Session lifecycle and capture boundaries](docs/session-lifecycle.md)
- [Implementation, APIs, storage, and security](docs/Spec.md)
- [Operations](docs/operations.md) and [troubleshooting](docs/troubleshooting.md)
- [Product direction](docs/PRODUCT.md) and [dashboard design](docs/DESIGN.md)
