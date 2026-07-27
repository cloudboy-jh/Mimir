# Troubleshooting

## Installation reports `action_required`

Mimir preserved a conflicting, modified, or unsafe path. Inspect the reported
file. Remove it only if it is disposable, or restore the exact Mimir-owned
version, then run `mimir install` again. Mimir will not overwrite it to make the
warning disappear.

## OpenCode capture is missing

Run `mimir doctor --json`, repair any managed-artifact issue it reports, and
restart OpenCode. The managed plugin reports turns and lifecycle events to the
Worker over HTTP; OpenCode does not hot-reload plugin changes.

## Hermes capture is missing

Run `mimir doctor --json`. Confirm the Mimir plugin is enabled and restart
Hermes. A stale Worker missing Hermes authorization endpoints requires
`mimir deploy`. Do not create a custom Hermes provider.

## Session is disconnected

`disconnected` means no event arrived for about 90 seconds and the session has
not finalized. Activity can restore `active`. About ten minutes of silence
finalizes the session. This is independent of whether exchanges were saved.

## A finalized session became active

That is intentional. New activity with the same exact session ID reopens the
same history. A genuinely new harness session must use a new ID.

## Capture receipt is pending, partial, or failed

Treat the receipt literally. Do not infer persistence from proxy success,
plugin activity, or response headers. Retry `mimir session status <id> --json`
after background writes settle. If failures remain, inspect Worker logs and R2/
D1 bindings without sending a paid model request.
