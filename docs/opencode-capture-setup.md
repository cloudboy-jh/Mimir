# opencode Session Capture

Mimir owns the opencode adapter. `mimir setup`, `mimir login`, and
`mimir update` install or refresh:

- `~/.config/opencode/plugins/mimir.ts`
- the `openrouter` provider route through the Mimir Worker
- a `{file:...}` reference to the machine token
- the absolute local `mimir serve` MCP command
- `/mimir-end-session`

The installer preserves unrelated opencode configuration and is idempotent.
Do not create a second Mimir provider or copy the integration by hand.

The adapter adds stable session, repository, and harness metadata to Mimir
traffic. It also marks each request as `primary`, `title`, `summary`, or
`compaction`. The Worker captures auxiliary requests as supporting evidence,
but only a primary request can establish the session intent. A defensive
server-side classifier prevents known title-agent prompts from poisoning intent
when a stale or third-party adapter labels them incorrectly.

Run:

```bash
mimir login
mimir doctor
```

Restart opencode after installation because it does not hot-reload provider or
plugin configuration. Capture applies to models using opencode's `openrouter`
provider. `mimir doctor` validates static wiring and Worker connectivity without
spending model tokens; `session_status` remains the authoritative proof that a
real session was saved.
