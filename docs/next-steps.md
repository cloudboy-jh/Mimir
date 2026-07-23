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

### Session lifecycle and harness capture

The architecture is defined in [`session-lifecycle.md`](session-lifecycle.md).
Foundation (event format, Session Durable Object, proxy reporting, session
object routes) is implemented, as are both reporters: the OpenCode plugin and
the Hermes plugin. Remaining build order:

- **Dashboard live view + liveness badges.** Consume what already shipped:
  - Session detail page (`worker/web` sessions detail route): subscribe to
    the live feed, render turns as they stream in, show finalization when it
    happens.
  - Sessions list: liveness badge per active session — `active` (<90s
    heartbeat), `disconnected` (silence, unfinalized), `finalized` — from the
    object-state projection.
  - **Gotcha:** `/sessions/:id/live` and `/sessions/:id/object-state` are
    machine-token routes; the browser must not hold machine tokens. Add
    Access-protected equivalents under `/dashboard/api/sessions/:id/live`
    (websocket passthrough to the object) and
    `/dashboard/api/sessions/:id/object-state`, registered in
    `worker/src/routes/dashboard.ts`. Dashboard API routes already bypass
    Access on localhost for development.
  - Session objects only exist for sessions that reported events; history
    views keep reading D1 exactly as today. The live feed is additive.

### Installer portability

- Add native macOS and Windows CI smoke jobs for install, update, and uninstall.
  Cross-compilation and Linux tests do not exercise platform filesystem aliases
  or Windows executable replacement behavior.
- Keep the smoke environment isolated from real harness configuration and
  verify the receipt, installed binary, managed artifacts, and cleanup result.

## Operational Follow-ups

- Add required-reviewer protection to the existing GitHub `release`
  environment.
- Define a recommended reconciliation cadence and an explicit policy for stale
  accepted rows and orphaned R2 objects.
- Correct stale statements in `docs/Spec.md` that still describe Access setup
  as absent and the dashboard as mock-backed.

## Parked Decisions

- **Generalized harness provider router** — superseded by
  [`session-lifecycle.md`](session-lifecycle.md). Capture moves to the
  conversation layer (harness plugins reporting to session objects) instead of
  a harness × provider routing matrix. The proxy remains only for API-key
  providers with redirectable base URLs. Do not intercept TLS, impersonate
  OAuth clients, or turn machine tokens into provider credentials.
- **`mimir browse`** — keep parked unless the standard-library-only CLI
  constraint receives an explicit TUI dependency carve-out. The live dashboard
  and `mimir list` already provide session access.

## Recently Closed

- Live Access-protected dashboard data, request-log cursor pagination, R2
  payload detail, and outcome updates.
- Exact Cloudflare Access destinations at `/dashboard` and `/dashboard/*`;
  machine API routes remain outside Access.
- Tagged GoReleaser delivery with checksummed cross-platform assets and GitHub
  build provenance, exercised successfully through `v0.3.2`.
- Windows setup-test portability and installed-version reporting.
- Human-readable Worker logs for successful exchange capture.
- Automatic refresh for live Sessions, Requests, and Overview dashboard data.
- Explicit idempotent session ending through the machine API, CLI, MCP, and
  `/mimir-end-session <session-id>`, including safe handling of late capture
  finalization and concurrent retries.
- Release CI now verifies migrations, the dashboard production build, the
  deployable Worker bundle, Go modules, and GoReleaser configuration. Release
  archives are self-contained and carry GitHub build provenance attestations.
- The managed installer lifecycle now owns only exact receipt-tracked binaries,
  plugins, and skills; preserves conflicts, local edits, connection state, and
  deployment state; and rejects unsafe symlink targets. The `v0.3.2` release
  includes the first public `mimir install` command and macOS bootstrap support.
