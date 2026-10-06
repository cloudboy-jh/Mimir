# Operations

## Deploy

Deploy only through the packaged CLI:

```bash
mimir deploy
```

The checked-in Wrangler configuration contains placeholder resource IDs and is
not a supported production deployment path. The CLI materializes the embedded
Worker and precompiled dashboard bundle, preserves owned configuration, applies
D1 migrations, and deploys.

Deployment verification must use `/whoami` and direct session APIs. Do not call
paid completion endpoints for a health check.

## Diagnose

```bash
mimir doctor --json
```

Doctor is read-only. It validates managed artifacts, Worker API
version/capabilities and bundle identity, active harness loads, Hermes plugin
enablement, Hermes credentials, and compatibility routes. It also reports stale
files next to the owned executable without deleting them. Use the exact repair
it reports: connection failures use `mimir login`, stale Worker state uses
`mimir deploy`, and missing or outdated managed artifacts use `mimir install`.
See [troubleshooting](troubleshooting.md) for activation and recovery states.

## Update

```bash
mimir update --check
mimir update
mimir update --force
```

For an existing deployment, update the binary first, then run `mimir deploy`
from the updated binary to apply its bundled Worker, production dashboard, and
D1 migrations. Release publication and `mimir update` do not deploy anything to
your Cloudflare account. If Windows reports a scheduled update, finish the
binary swap before deploying. Reactivate the installed harness integrations as
described in [installation](installation.md#integration-details).

Release archives are verified against published checksums before replacement.
The updater requires the receipt-owned executable, records the verified new
hash, refreshes integrations, and guards rollback against concurrent binary
replacement. On Windows, when the executable is locked by another Mimir
process or an antivirus filter, the update is deferred: the verified binary is
staged, `pending-update.json` is recorded, and a detached helper completes the
swap once the lock clears. `--force` stops sibling Mimir processes and applies
the update immediately.

## Access

```bash
mimir access
```

The Access application must protect exactly `/dashboard/auth`,
`/dashboard/api/*`, and `/dashboard/log-objects/*`. The public `/login` route
provides the branded handoff, while dashboard APIs and redacted objects verify
Access JWTs. Machine APIs remain on independent bearer tokens and browser code
never receives them.

When `--email` is supplied, automation accepts only one exact Allow policy for
that email. Existing conflicting, permissive, additional, or Bypass policies
cause an action-required error; Mimir does not modify them or report Access as
configured.

Do not protect the bare Worker host. `/login` remains public for the browser
handoff, while machine routes remain outside Access and continue to use
per-machine bearer tokens.

## Devices

Dashboard Settings lists registered devices and their current name, platform,
last-seen time, observed harnesses, session count, and revocation state. Renaming
changes only the display label; `installation_id` and historical session
associations remain unchanged.

Revocation is irreversible in the dashboard. It disables every machine token
and installation-scoped Hermes credential associated with that device, while
retaining the device, sessions, and captured history for inspection. Setup and
login do not reactivate the stable installation or its tokens; registration for
that identity remains unusable and connection verification fails. To use the
physical machine again, enroll it with a new installation identity. Dashboard
renames remain available and change only the retained display label.

## Local Development

From the repository root, run the dashboard against the deterministic fixture
dataset with:

```bash
npm --prefix worker ci
bun --cwd=worker/web install --frozen-lockfile
bun run dev
```

The fixture covers multi-provider conversations, replay suppression, paired tools
and reasoning, auxiliary/compaction requests, supporting branches, factual
summaries, pending/failed capture, unknown payloads, and legitimate repeated turns.
Commits include cross-session associations, divergent same-SHA patches, separate
repositories, rename/deletion/binary/mode-only changes, and unavailable captures.
All exchanges and Git history are fictional and labeled as sample data.

Vite serves fixtures with HMR on `localhost:5173`. Use `bun run dev:live` to
apply local D1 migrations and run the dashboard against the Worker on
`127.0.0.1:8787`. Vite proxies Access handoff, dashboard APIs, and log-object
requests; local requests use a marked development identity, never machine
credentials stored in the browser.

After dashboard/fixture changes, rebuild both embedded variants and verify their
separation:

```bash
bun run build:assets
bun run verify:dashboard-builds
```

Production assets are built into `worker/web/dist`; the separate demo build
goes to `internal/demoassets/static`. Production builds exclude the fixture
provider and sample dataset. `mimir demo --no-open` serves only the generated
fixture bundle without Cloudflare. Its default rich session is
`ses_fixture_multi_model_result`; `/commits` exposes the repository-first
captured-commit browser. The sample bundle must never enter production assets.

CI and release builds also verify the CLI's embedded deployment inputs:

```bash
go run ./scripts/check-embedded-worker.go
```

This creates an isolated temporary `MIMIR_HOME`, uses the default packaged
Worker materializer, installs locked Worker dependencies, and runs Wrangler
`deploy --dry-run` from that materialized directory. It requires Go, Node, and
npm, but no Bun, Cloudflare authentication, or remote resources. A successful
checkout dry-run alone does not prove the binary contains every imported module.

Validate the capture and installer surfaces from the repository root:

```bash
npm --prefix worker test -- src/config.test.ts src/session-titles.test.ts
bun test plugins/pi/ plugins/opencode/
python -m unittest discover -s plugins/hermes -p "test_*.py"
go test ./internal/harness/hooks ./internal/install ./internal/doctor
npm --prefix worker run typecheck
```

These are local tests, not deployment verification. Use `/whoami` and direct
session APIs for deployment checks, never paid completion routes.
