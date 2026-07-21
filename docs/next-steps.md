# Next Steps

This file tracks concrete unfinished work and operational decisions. Completed
architecture-transition work belongs in git history and the implementation
specification rather than an expanding completion log.

## Active Implementation Work

### MCP conformance

- Validate the JSON-RPC version, request IDs, method parameters, and tool
  arguments.
- Return tool execution failures as MCP tool results with `isError: true` where
  required by the protocol.
- Add compatibility tests using current opencode and at least one additional
  MCP client while retaining newline-delimited and legacy `Content-Length`
  input coverage.

### Lifecycle configuration

- Remove `session.abandon_days` from the public configuration contract or
  implement and document the lifecycle process that consumes it. Do not keep
  accepting a setting with no behavioral effect.

### opencode installer portability

- Resolve the machine credential through the same `MIMIR_HOME`-aware path used
  by the connection manifest instead of hard-coding `~/.mimir/token`.
- Use the absolute current executable path in the generated MCP command, or
  explicitly document that the integration depends on `mimir` being on
  `PATH`.
- Update `docs/opencode-capture-setup.md` to describe the generated OpenRouter
  plugin, dynamic `x-mimir-session` headers, credential and MCP command
  resolution, and `/mimir-end-session <session-id>`.

## Operational Follow-ups

- Add required-reviewer protection to the existing GitHub `release`
  environment.
- Define a recommended reconciliation cadence and an explicit policy for stale
  accepted rows and orphaned R2 objects.
- Correct stale statements in `docs/Spec.md` that still describe Access setup
  as absent and the dashboard as mock-backed.

## Parked Decisions

- **`mimir browse`** — keep parked unless the standard-library-only CLI
  constraint receives an explicit TUI dependency carve-out. The live dashboard
  and `mimir list` already provide session access.

## Recently Closed

- Live Access-protected dashboard data, request-log cursor pagination, R2
  payload detail, and outcome updates.
- Exact Cloudflare Access destinations at `/dashboard` and `/dashboard/*`;
  machine API routes remain outside Access.
- Tagged GoReleaser delivery with checksummed cross-platform assets, exercised
  successfully through `v0.1.5`.
- Windows setup-test portability and installed-version reporting.
- Human-readable Worker logs for successful exchange capture.
- Automatic refresh for live Sessions, Requests, and Overview dashboard data.
- Explicit idempotent session ending through the machine API, CLI, MCP, and
  `/mimir-end-session <session-id>`, including safe handling of late capture
  finalization and concurrent retries.
- Release CI now verifies migrations, the dashboard production build, the
  deployable Worker bundle, Go modules, and GoReleaser configuration. Release
  archives are self-contained and carry GitHub build provenance attestations.
- `v0.2.0` publishes the post-`v0.1.5` version reporting, capture logging,
  dashboard refresh, and explicit session-ending changes.
