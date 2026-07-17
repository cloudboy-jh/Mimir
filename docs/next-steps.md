# Next Steps

This file tracks concrete gaps in the current implementation. It is not a
second product specification.

## 1. Connect The Dashboard

- Replace mock data with the existing `/dashboard/api/*` endpoints.
- Add loading, empty, unavailable, and Access-denied states.
- Connect capture status and evidenced outcome controls.
- Add cursor pagination and filter serialization.
- Keep `worker/web/src/lib/mock.ts` as the only fixture source until the live
  design is explicitly approved.

## 2. Verify Dashboard Routing

Browser routes now live under `/dashboard/*`, while canonical machine APIs keep
the `/sessions*` namespace. Keep deployment-level route tests covering direct
session receipt links, static assets, machine authentication, and Access APIs.

## 3. Finish Cloudflare Access Setup

- Document or automate creation of the dashboard Access application.
- Configure `DASHBOARD_ACCESS_AUD` and `DASHBOARD_ACCESS_TEAM_DOMAIN` safely.
- Verify static asset protection and dashboard API protection together.
- Keep localhost development access without adding a Mimir password system.

## 4. Release And Update Distribution

- Publish tagged releases with checksums.
- Make the installed binary independent from a retained Go module cache.
- Add a verified, atomic `mimir update` flow using a stable executable path.
- Expose machine-readable version/update diagnostics for setup skills.

## 5. Harden MCP Integration

- Add integration tests against current OpenCode, Hermes, and other supported
  MCP clients.
- Validate JSON-RPC versions, IDs, methods, and tool arguments completely.
- Return tool execution failures as MCP tool errors where appropriate.
- Add bounded HTTP timeouts.
- Publish tested harness recipes without putting harness-specific behavior in
  the Worker.

## 6. Harden Capture And Search

- Define operational cadence and orphan cleanup policy for reconciliation.
- Add capture-failure alerting and operational views to Worker observability.
- Decide whether large-response capture should truncate or remain all-or-none.
- Replace ignored search fields or remove them from the request contract.
- Decide whether `session.abandon_days` should drive an explicit lifecycle job
  or be removed.
- Evaluate full-text search before considering vectors or embeddings.

## Boundaries

Do not add SaaS tenancy, team management, a separate backend, browser bearer
token storage, Git-backed session sync, or migration of local code indexes into
D1.
