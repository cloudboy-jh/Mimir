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
