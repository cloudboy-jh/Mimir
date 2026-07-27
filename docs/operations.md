# Operations

## Deploy

Deploy only through the packaged CLI:

```bash
mimir deploy
```

The checked-in Wrangler configuration contains placeholder resource IDs and is
not a supported production deployment path. The CLI materializes the embedded
Worker/dashboard bundle, preserves owned configuration, builds the dashboard,
applies D1 migrations, and deploys.

Deployment verification must use `/whoami` and direct session APIs. Do not call
paid completion endpoints for a health check.

## Diagnose

```bash
mimir doctor --json
```

Doctor validates managed artifacts, the receipt-owned executable, Worker API
version/capabilities, OpenCode plugin load, Hermes plugin enablement, Hermes
credentials, and compatibility routes. It also reports stale files next to the
owned executable (swap leftovers and foreign junk) without deleting them. A
Worker missing required capabilities requires `mimir deploy`; invalid machine
credentials require `mimir login`.

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

The Access application must protect exactly `/dashboard` and `/dashboard/*`.
Dashboard APIs verify Access JWTs. Machine APIs remain on independent bearer
tokens and browser code never receives them.

When `--email` is supplied, automation accepts only one exact Allow policy for
that email. Existing conflicting, permissive, additional, or Bypass policies
cause an action-required error; Mimir does not modify them or report Access as
configured.
