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

## After meaningful work — capture receipt

After meaningful work, get the authoritative capture receipt when the harness
exposes an exact session ID. Pi exposes it to bash as `PI_SESSION_ID`:

```bash
mimir session status "$PI_SESSION_ID" --json
```

In another harness, use its exact session ID when available. Do not guess one.

The result returns the receipt. When dashboard Access is configured, the
receipt includes `View session`. Let the harness display that result near the
completed response; do not repeat the session ID, timestamp, counts, or receipt
in agent prose unless the user explicitly asks for storage details.

Treat these as real user-visible states (never rewrite them):
- `Saving to Mimir...`
- `Partially saved`
- `Mimir couldn't save this session`

Do not call `mimir session status` during routine tool use or when no
meaningful unit of work has completed.

## Setting outcomes

Set an outcome only when completed work provides evidence:

```bash
mimir session outcome <session-id> <value> --reason "concise reason" --json
```

Canonical values:
- `landed`: the result was kept or shipped
- `discarded`: the result was deliberately rejected or reverted
- `abandoned`: work stopped without a result
- `unresolved`: no evidenced result is available

Include a concise reason and the supporting evidence. Capture state and work
outcome are independent: a saved session can remain unresolved, and landed work
is not proof that its exchanges were saved.

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
