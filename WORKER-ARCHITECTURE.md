# Worker Architecture

Status: implemented. This document records the authoritative Worker architecture.

## Decision

Mimir remains one Cloudflare Worker deployment. It is a private memory-plane service, not only an OpenRouter proxy. The deployment combines an OpenRouter-compatible capture gateway, harness ingestion API, session control plane, and Access-protected dashboard over shared D1, R2, Durable Object, and Workers Assets bindings.

One Worker is sufficient because these capabilities share authentication, session identity, storage, deployment, and consistency boundaries. Splitting them into separately deployed services would add network and operational failure modes without establishing a useful product boundary.

Codebase maturity will come from explicit composition, capability-owned modules, centralized invariants, and contract tests—not additional services or generic abstractions.

## Deployment entry points

`worker/wrangler.jsonc` points Cloudflare at `worker/src/index.ts`.

`worker/src/index.ts` is the deployment adapter. It should remain small and export:

- the Hono application as the default Worker handler;
- the `SessionObject` Durable Object class;
- intentionally public helpers required by tests or embedded builds.

`worker/src/app.ts` is the application composition root. It should show the complete HTTP architecture without containing route implementation.

The intended shape is:

```ts
const app = new Hono<AppEnv>();

installErrorHandling(app);
installAuthentication(app);

registerMachineRoutes(app);
registerProxyRoutes(app);
registerSessionRoutes(app);
registerSearchRoutes(app);
registerConfigRoutes(app);
registerIntegrationRoutes(app);
registerDashboardAuthRoutes(app);
registerDashboardSessionRoutes(app);
registerDashboardExchangeRoutes(app);
registerDashboardDeviceRoutes(app);
registerDashboardFacetRoutes(app);

export default app;
```

A reader should be able to understand the deployed service from `app.ts` in under a minute.

## Worker capabilities

### Model gateway

The `/v1/*` surface forwards supported OpenRouter-compatible requests, preserves upstream streaming, and schedules capture without delaying the model response.

The proxy is one ingress path into Mimir. It is not the Worker’s complete identity.

### Harness ingestion

Harness adapters report exact lifecycle events and bounded reconstructed exchanges when proxy transport is unavailable. Ingestion validates the payload, redacts sensitive material, writes raw exchanges to R2, and derives searchable metadata for D1.

### Session control plane

The `/sessions/*` surface owns session lifecycle, titles, hierarchy, outcomes, capture status, live state, and reconciliation. `x-mimir-session` remains the authoritative session boundary.

The `SessionObject` Durable Object coordinates one live session. D1 and R2 remain canonical durable storage.

### Machine API

Machine-authenticated endpoints provide identity, association, configuration, search, integration health, and operational access for the CLI and harnesses.

Machine authentication is an API audience, not a domain boundary. It should not cause unrelated capabilities to share one route module.

### Dashboard

The Worker serves the Vue dashboard and its Access-protected APIs from the same deployment. The dashboard is a client of the canonical Worker API and may expose richer human-facing projections without creating a second session model.

## Source organization

The Worker source is organized by product capability:

```text
worker/src/
├── index.ts                  Cloudflare deployment exports
├── app.ts                    middleware and route composition
├── env.ts                    Worker bindings and Hono environment
├── auth/
│   └── auth-middleware.ts
├── config/
│   ├── config-routes.ts
│   └── config-store.ts
├── gateway/
│   ├── openrouter-routes.ts
│   └── upstream-proxy.ts
├── exchanges/
│   ├── capture-pipeline.ts
│   ├── evidence.ts
│   ├── redaction.ts
│   ├── response-codec.ts
│   ├── reported-exchange-routes.ts
│   ├── reported-exchange-schema.ts
│   └── *-dashboard-routes.ts
├── sessions/
│   ├── session-routes.ts
│   ├── session-dashboard-routes.ts
│   ├── session-object.ts
│   ├── session-queries.ts
│   ├── lifecycle.ts
│   ├── outcomes.ts
│   ├── capture-status.ts
│   ├── events.ts
│   ├── titles.ts
│   └── summaries.ts
├── machines/
│   ├── machine-routes.ts
│   └── device-dashboard-routes.ts
├── integrations/
│   └── integration-routes.ts
├── search/
│   └── search-routes.ts
├── dashboard/
│   ├── dashboard-shell-routes.ts
│   └── cursors.ts
└── shared/
    └── ulid.ts
```

HTTP adapters live with the capability they expose. Pure helpers and focused
tests remain beside their owner. New files should be introduced only for a
coherent responsibility, not to satisfy a layer template.

## Module rules

### Route modules translate HTTP

A route handler should normally:

1. parse and validate input;
2. enforce the relevant authorization rule;
3. call a domain operation;
4. translate the result into an HTTP response.

Route modules may contain small response projections. They should not independently implement session identity, title precedence, outcome precedence, redaction, or capture eligibility.

### Domain modules own invariants

Rules that must remain identical across the machine API and dashboard belong in shared domain functions. Examples include:

- resolving a child session to its root;
- loading session status and capture state;
- updating an outcome without violating source precedence;
- retrieving session exchanges and diff evidence;
- validating and deriving exchange facets.

Share behavior when two surfaces must agree. Do not introduce generic repositories, service containers, or one-class-per-file abstractions.

### Storage remains explicit

Direct D1 and R2 operations are acceptable when they are local and obvious. Repeated queries that encode a product invariant should move behind a named domain operation. A generic data-access layer would hide useful SQL without protecting correctness.

### Dependencies point inward

The intended dependency direction is:

```text
index.ts -> app.ts -> routes -> domain operations -> storage bindings
                         \-> response projection
```

Domain modules must not import route modules or dashboard code. Dashboard code must not become a second implementation of backend session behavior.

## Implemented structure

`app.ts` registers machine identity, gateway, session, search, configuration,
integration, and dashboard adapters explicitly. Each adapter lives in its
owning feature package rather than a global `routes/` tree.

The broad capture and session modules are split by responsibility: capture
orchestration, response decoding, redaction, evidence derivation, payload
validation, session queries, lifecycle, outcomes, and capture status have
separate owners. Tests live beside these modules; cross-capability contract
suites remain under `worker/test/`.

The deployment remains one Worker. Feature directories are internal ownership
boundaries, not services.

## Refactoring record

### Structural commit

- split `routes/machine.ts` by capability;
- split `routes/dashboard.ts` by dashboard capability;
- make `app.ts` explicitly register every route group;
- split the integration suite along the same capability boundaries;
- preserve every URL, payload, response, authorization rule, and storage operation.

This commit must be behavior-neutral.

### Deduplication commit

- identify machine and dashboard operations that must agree;
- move shared session resolution, status, exchange, and diff behavior into existing domain modules;
- remove duplicate SQL only where it represents the same product invariant;
- keep HTTP-specific projections in route modules.

### Contract-hardening commit

- define canonical normalized tool activity for harness-reported exchanges;
- require a conformance fixture for each tool-bearing harness;
- verify file and error facets through the real ingestion path;
- make failed Git outcome inference non-destructive;
- require landed Git outcomes to carry retrievable commit and patch evidence.

Structural movement and behavioral hardening should remain separate commits so regressions are reviewable and bisectable.

## Harness conformance requirements

Every harness that reports tool-bearing exchanges must provide a representative fixture covering:

- exact session identity;
- repository and Git ref metadata;
- first user intent;
- model and provider;
- file-reading and file-writing tool calls;
- a failed tool call;
- title and lifecycle events.

The Worker integration test must prove that the fixture produces one session, correct searchable files and errors, and usable Git diff evidence. Adding a harness without this contract should fail CI.

## Non-goals

This architecture does not introduce:

- multiple Worker deployments;
- internal HTTP calls between Mimir capabilities;
- a generic repository or dependency-injection framework;
- a second account or session system;
- dashboard-owned backend behavior;
- new D1, R2, or Durable Object boundaries solely for source organization.

## Verification standard

A structural refactor is complete only when:

- the existing Worker and dashboard typechecks pass;
- the complete Worker test suite passes;
- plugin and harness tests pass;
- Wrangler dry-run succeeds;
- existing API paths and response contracts remain covered;
- production and demo asset verification remains clean;
- no migration or deployment configuration changes appear unless explicitly required.

The resulting codebase should remain a modular monolith: one deployable Worker with obvious internal boundaries and one authoritative composition root.
