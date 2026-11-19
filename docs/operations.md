# Operations

## Deploy

Deploy only through the packaged CLI:

```bash
mimir deploy
```

The checked-in Wrangler configuration contains placeholder resource IDs and is
not a supported production deployment path. The CLI materializes the embedded
Worker and precompiled dashboard, preserves its generated configuration,
applies D1 migrations, and deploys. The deployment includes a cron trigger
(`*/10 * * * *`) for maintenance.

Deployment verification uses `/whoami` and the query API. Do not call paid
completion endpoints for a health check.

## Diagnose

```bash
mimir doctor --json
```

Doctor reads local state and the Worker; it changes nothing locally. It checks
managed files (including MCP configurations), the connection and connection
manifest, Worker API version, capabilities and bundle identity, loaded harness
versions, and Hermes plugin, route, and credential state. It also reports this
machine's tailnet to the Worker when Tailscale is available. Use the exact
repair it reports: connection failures use `mimir login`, a stale Worker uses
`mimir deploy`, and missing or outdated managed files use `mimir install`. See
[troubleshooting](troubleshooting.md).

## Maintenance

A cron trigger runs every 10 minutes. Each step is bounded and runs even when
an earlier step fails:

1. Sessions silent longer than `session.gap_minutes` (default 15) become
   inactive.
2. Up to 10 pending summaries of inactive root sessions are built.
3. Up to 20 saved exchanges missing from the full-text index are indexed.

Read requests never write. Failures are logged as structured JSON in Worker
logs. `mimir reconcile` runs a separate bounded D1/R2 capture reconciliation
pass on demand.

## Update

```bash
mimir update --check
mimir update
mimir deploy
```

`mimir update` reruns the official installer, which verifies the release
checksum and refreshes managed files. On Windows a hidden PowerShell installer
waits for the running `mimir` to exit before replacing it. Then run
`mimir deploy` from the updated binary to apply its bundled Worker, dashboard,
and D1 migrations. Release publication and `mimir update` never deploy to your
Cloudflare account. Reactivate the integrations as described in
[installation](installation.md#activate-harness-integrations).

## Access

```bash
mimir access
```

The Access application must protect exactly `/dashboard/auth` and
`/dashboard/api/*`. The public `/login` route provides the browser handoff,
and dashboard APIs verify Access JWTs. Machine routes, including `/api/*` and
`/mcp`, stay outside Access and use per-machine bearer tokens; browser code
never receives them.

When `--email` is supplied, automation accepts only one exact Allow policy for
that email. Conflicting, permissive, additional, or Bypass policies cause an
action-required error; Mimir does not modify them or report Access as
configured. Do not protect the bare Worker host.

## Devices

The Devices page and `mimir devices` list registered devices with name,
platform, last-seen time, apps, session count, and tailnet status (Tailscale
name, IPs, online). Renaming changes only the display label; the
`installation_id` and session associations stay.

Tailnet status comes from `tailscale status --json`, reported by
`mimir login`, `mimir doctor`, and harness session starts at most once every 10
minutes. A device marks itself online when it reports and updates the online
state of every peer whose Tailscale DNS name matches another device. Reporting
is silent when Tailscale is not installed or not logged in.

Revocation is irreversible. It disables every machine token and Hermes
credential associated with that device while keeping the device, sessions, and
history. Setup and login never reactivate a revoked installation; to use the
machine again, enroll it with a new installation identity.

## Local Development

From the repository root:

```bash
npm --prefix worker ci
bun --cwd=worker/web install --frozen-lockfile
bun run dev
```

`bun run dev` applies local D1 migrations, runs `wrangler dev` on
`127.0.0.1:8787`, and serves the dashboard with Vite HMR, proxying
`/dashboard/api` and `/dashboard/auth` to the local Worker. Local requests use
a marked development identity, never machine credentials in the browser.
`bun run dev:web` and `bun run dev:api` run either half alone.

Build and check the production dashboard:

```bash
bun run build
bun run check:production-assets
bun run typecheck
```

Production assets are built into `worker/web/dist` and embedded in release
binaries.

CI and release builds also verify the CLI's embedded deployment inputs:

```bash
go run ./scripts/check-embedded-worker.go
```

This creates an isolated temporary `MIMIR_HOME`, uses the packaged Worker
materializer, installs locked Worker dependencies, and runs Wrangler
`deploy --dry-run` from that directory. It needs Go, Node, and npm, but no Bun,
Cloudflare authentication, or remote resources.

Run the local test suites:

```bash
npm --prefix worker test
npm --prefix worker run typecheck
bun run test:plugins
bun run typecheck:plugins
python -m unittest discover -s plugins/hermes -p "test_*.py"
go test ./...
```

These are local tests, not deployment verification.
