---
name: mimir-use
description: Use the Mimir memory plane automatically before, during, and after agent work.
---

# Mimir Use

Mimir is agent infrastructure. Do not ask the user to run Mimir commands during
normal work. Run them yourself via the bash tool.

Use `--json` on every Mimir command so you can parse structured output. Present
results as formatted, readable text to the user — never dump raw JSON.

## Before work — search

Before substantial work, search Mimir with the problem, affected files, or
error signature:

```bash
mimir search <query> --json
```

Parse the JSON matches, extract session IDs, and inspect relevant results:

```bash
mimir session get <session-id> --json
```

Synthesize what you find into a brief summary for yourself. Do not narrate
routine memory access to the user.

## Transport metadata

Mimir-owned adapters supply transport metadata automatically:

```text
x-mimir-session: <stable-session-id when supported>
x-mimir-repo: <repository-name-or-url when supported>
x-mimir-harness: <harness-name>
x-mimir-git-ref: <branch-at-session-start when supported>
x-mimir-request-kind: <primary|title|summary|compaction>
```

Exact session identity is optional. Harnesses without dynamic request headers
use Mimir's inactivity fallback automatically. Auxiliary model requests are
infrastructure behavior; agents must not compensate for them through prompts or
guessed session IDs.

Proxy use and a scheduled `x-mimir-capture` response header are not proof that
an exchange was saved. Never report persistence from transport activity alone.

## After meaningful work — record outcome, then receipt

Before the final response for meaningful work, record one canonical outcome and
then fetch the authoritative capture receipt. Do not leave completed work
`unresolved`.

Prefer a native `mimir_session_outcome` tool when the harness provides one.
Mimir's Pi and Oh My Pi adapters expose the exact current identity as
`MIMIR_SESSION_ID`; Pi's host-provided `PI_SESSION_ID` remains a fallback:

```bash
session_id="${MIMIR_SESSION_ID:-${PI_SESSION_ID:-}}"
if [ -n "$session_id" ]; then
  mimir session outcome "$session_id" landed --reason "implemented and verified" --json
  mimir session status "$session_id" --json
fi
```

In another harness, use its exact session ID when available. Never infer or
guess one. If no exact identity is available, skip mutation rather than
updating the wrong session.

Canonical outcomes:
- `landed`: the completed result was kept or shipped
- `discarded`: the result was deliberately rejected or reverted
- `abandoned`: work stopped without a result
- `unresolved`: no evidenced result is available

Choose from observed evidence. Include a concise reason and supporting evidence
when available. A passing behavioral check, accepted deliverable, retained
change, merge, or deployment supports `landed`; merely attempting work does
not. Record `abandoned` for an evidenced stop without a result. Use
`unresolved` only when the result genuinely cannot be established.

Explicit evidence is stronger than automatic lifecycle inference. If a harness
cannot expose an exact identity, the Worker resolves the generation
deterministically at finalization: a clean completed primary turn becomes
`landed`; a failed, pending, or absent terminal signal becomes `abandoned`.
Resuming a finalized exact session starts a new `unresolved` generation while
preserving the prior outcome history.

Outcome must be recorded before the receipt so the returned status reflects
both projections. Capture and work outcome remain independent: a saved session
can be unresolved, and landed work is not proof that its exchanges were saved.

The status result returns the receipt. When dashboard Access is configured, the
receipt includes `View session`. Let the harness display that result near the
completed response; do not repeat the session ID, timestamp, counts, or receipt
in agent prose unless the user explicitly asks for storage details.

Treat these as real user-visible states (never rewrite them):
- `Saving to Mimir...`
- `Partially saved`
- `Mimir couldn't save this session`

Do not call `mimir session status` during routine tool use or when no meaningful
unit of work has completed.

## Ending a session

When the user explicitly asks to end, close, or finalize the session:

```bash
mimir session end <session-id> --json [--outcome <value>] [--reason "text"]
```

Include the evidenced outcome and reason when available, then return its
receipt. Do not end a session merely because one task or response finished; an
ended exact session may be reactivated by later traffic.

## Listing sessions

When the user asks about recent sessions:

```bash
mimir list --json [--limit 10] [--outcome landed|discarded|abandoned|unresolved]
```

Format the output as a readable list with titles, outcomes, models, and recency.

## Recovering local harness history

When the user asks to recover a session that was missed while a plugin was
disabled or uninstalled, inspect local candidates first:

```bash
mimir import list <opencode|pi> --json
mimir import inspect <opencode|pi> <session-id> --json
```

Import only the exact sessions the user identified:

```bash
mimir import <opencode|pi> <session-id>... --yes --json
```

Use broad backfill only when the user explicitly requests gap repair. Always
scope automation and acknowledge the bulk operation explicitly:

```bash
mimir backfill [opencode|pi] [--since 7d] --all --yes --json
```

Import and backfill are idempotent merges. OpenRouter proxy exchanges remain
canonical; reconstructed local turns fill gaps. Discovered Git commits are
preserved independently of outcome, including when a session stays unresolved.

## Local code recall

```bash
mimir recall <query> --json
```

Code recall remains local. Operate the CLI automatically rather than asking the
user to run routine memory commands.

## Rules

- Always use `--json` for machine parsing; always present formatted output.
- Never dump raw JSON in your response.
- Do not narrate routine memory lookups — just use the evidence.
- When the user asks "show my sessions" or similar, run the command and format
  the result. Do not ask them to run it.
