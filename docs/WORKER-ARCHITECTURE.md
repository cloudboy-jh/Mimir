# Worker Architecture

## Decision

Mimir is one Cloudflare Worker deployment: an OpenRouter-compatible capture
gateway, harness ingestion API, query API and MCP server, scheduled
maintenance, and the Access-protected dashboard, over shared D1, R2, and
Workers Assets bindings.

One Worker is enough because these capabilities share authentication, session
identity, storage, deployment, and consistency boundaries. Separate services
would add network and operational failure modes without a useful product
boundary.

## Entry points

`worker/wrangler.jsonc` points Cloudflare at `worker/src/index.ts` and declares
a cron trigger (`*/10 * * * *`).

`worker/src/index.ts` is the deployment adapter. It exports one handler with:

- `fetch`: the Hono application;
- `scheduled`: `runMaintenance` from `maintenance.ts`.

`worker/src/app.ts` is the composition root. It installs error handling and
authentication, registers each route group, and mounts the query router twice:

```ts
const app = new Hono<AppEnv>();

app.onError(/* QueryError → { error } with its status; others → 500 */);
app.notFound(/* { error: "not found" } */);
app.use("*", authenticate);

registerMachineRoutes(app);
registerProxyRoutes(app);
registerSessionRoutes(app);
registerConfigRoutes(app);
registerIntegrationRoutes(app);
registerDashboardRoutes(app);
registerMcpRoutes(app);

const queries = queryRouter();
app.route("/api", queries);
app.route("/dashboard/api", queries);

export default app;
```

A reader should understand the deployed service from `app.ts` in under a minute.

## Capabilities

### Model gateway

The `/v1/*` surface forwards supported OpenRouter-compatible requests,
preserves upstream streaming, and captures with `waitUntil` without delaying
the response.

### Harness ingestion

Harness integrations report lifecycle events, bounded reconstructed exchanges,
and Git commits. Ingestion validates the payload, redacts sensitive material,
writes raw exchanges and patches to R2, and writes metadata to D1. Events write
D1 directly.

### Query API and MCP

`queries/query-routes.ts` builds one Hono router for `/api` (machine token) and
`/dashboard/api` (Cloudflare Access). Handlers call query functions in
`queries/*` and never write, except explicit title, device-name, and
revocation edits. `mcp/mcp-routes.ts` exposes the same query functions as MCP
tools over stateless Streamable HTTP; it never calls the Worker over HTTP.

### Maintenance

`maintenance.ts` runs on the cron trigger: expire silent sessions, build
pending summaries, and backfill the full-text index, each bounded and isolated
from the others' failures. Read paths stay read-only because this work happens
here.

### Dashboard

The Worker serves the Vue dashboard as static assets and the Access handoff
(`dashboard/dashboard-routes.ts`: `/dashboard/auth`, `/dashboard/api/identity`).
Every dashboard data request goes through the shared query router.

## Source organization

```text
worker/src/
├── index.ts                  fetch + scheduled exports
├── app.ts                    middleware and route composition
├── env.ts                    bindings and Hono environment
├── maintenance.ts            cron: expiry, summaries, search backfill
├── auth/                     machine token, Hermes credential, and Access verification
├── config/                   /config routes and the config store
├── gateway/                  /v1 proxy routes and upstream streaming
├── exchanges/                capture pipeline, redaction, evidence, response decoding,
│                             reported exchanges, archive reads, transcript/turn building
├── sessions/                 write routes, events, lifecycle, titles, summaries,
│                             capture status and reconcile, Git artifacts, session tree
├── queries/                  query router and sessions, turns, search, stats, days,
│                             devices, commits query functions
├── mcp/                      MCP endpoint and tool definitions
├── search/                   FTS5 indexing and query building
├── days/                     pure day snapshot builder
├── machines/                 /whoami, /machine/associate, /machine/tailnet
├── integrations/             Hermes authorization and harness-load reports
├── dashboard/                Access handoff and identity
└── shared/                   cursors, date ranges, query errors, ULIDs
```

HTTP adapters live with the capability they expose. Pure helpers and focused
tests sit beside their owner; cross-capability contract suites live under
`worker/test/`. Feature directories are internal ownership boundaries, not
services.

## Module rules

### Route modules translate HTTP

A route handler parses and validates input, enforces authorization, calls a
domain or query function, and translates the result into an HTTP response. It
does not implement session identity, title precedence, Git status, redaction,
or capture eligibility on its own.

### Domain and query modules own invariants

Rules that must stay identical everywhere live in one function: resolving a
session to its root, deriving capture and Git status, building receipts and
turns, and applying title precedence. The dashboard, `/api`, and MCP all use
the same query functions. Do not add generic repositories, service containers,
or one-class-per-file abstractions.

### Storage remains explicit

Direct D1 and R2 operations are fine when local and obvious. Repeated queries
that encode a product invariant move behind a named function.

### Dependencies point inward

```text
index.ts -> app.ts -> routes -> domain and query functions -> storage bindings
index.ts -> maintenance.ts -> domain functions -> storage bindings
```

Domain modules never import route modules or dashboard code. Dashboard code is
never a second implementation of backend behavior.

## Harness conformance requirements

Every harness that reports tool-bearing exchanges provides a representative
fixture covering:

- exact session identity;
- repository and Git ref metadata;
- first user intent;
- model and provider;
- file-reading and file-writing tool calls;
- a failed tool call;
- title and lifecycle events.

The Worker integration test proves that the fixture produces one session with
correct searchable files and errors.

## Non-goals

- multiple Worker deployments;
- internal HTTP calls between Mimir capabilities;
- a generic repository or dependency-injection framework;
- a second account or session system;
- dashboard-owned backend behavior;
- new storage boundaries solely for source organization.

## Verification standard

A Worker change is complete when:

- Worker and dashboard typechecks pass (`npm --prefix worker run typecheck`);
- the Worker test suite passes (`npm --prefix worker test`);
- plugin and harness tests pass;
- `cd worker && npx wrangler deploy --dry-run` succeeds;
- API paths and response contracts in [`Spec.md`](Spec.md) remain covered.
