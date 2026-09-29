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

## After meaningful work — verify Git, outcome, then receipt

Before the final response, use the **exact canonical session ID**. Pi and Oh My
Pi export `MIMIR_SESSION_ID` to tool processes; Pi can also use its host's
`PI_SESSION_ID`. OpenCode supplies the current ID to its native
`mimir_session_outcome` and `mimir_session_status` tools. Hermes, Claude Code,
Codex, and Cursor supply IDs to their installed hooks, but do not guarantee an
ID to the agent's shell. If the shell has no exact ID, compare the active
harness/session identity with `mimir list --json` and `mimir session get <id>
--json` using repository, intent, and timing. Mutate only an **unambiguous**
match. For root-session work, use the exact root session ID; never substitute a
subsession. Recency alone, a matching repository alone, or a Git commit alone never
identifies a session. If still ambiguous, report that verification could not
be performed; do not update a guessed session.

Automatic artifact capture is best-effort and is **not** evidence of a saved
artifact. For every relevant commit actually produced by this work, from the
checkout containing that commit, resolve the full SHA and capture/verify the
artifact independently of the outcome:

```text
git rev-parse --verify <committed-ref>^{commit}
mimir session git capture <exact-session-id> <full-lowercase-40-character-sha> --json
mimir session get <exact-session-id> --json
mimir session git verify <exact-session-id> <full-lowercase-40-character-sha> --json
```

Repeat for each relevant SHA (not just the latest HEAD). The capture command
is safe to retry; a push, HTTP success, or an attempted automatic upload is
not proof of persistence. In the canonical `git_artifacts` array, verify an
entry for each SHA with `capture_status: "saved"` and a nonempty
`patch_sha256`. If it is missing, accepted, or failed, retry capture and read
it back. After the artifact is saved, require `mimir session git verify
<exact-session-id> <full-lowercase-40-character-sha> --json` to succeed with
`verified: true`. It independently fetches the stored patch and checks its
SHA256 digest, byte count, multiline Git patch format, and file/addition/deletion
statistics against canonical metadata. Saved status alone is not verification.
Never claim the patch is verified until this check succeeds. Do not create a
Git artifact or cite a commit for uncommitted or read-only work.

Repair an explicitly identified corrupted existing artifact only from a trusted
local commit SHA in its checkout, using the existing canonical `patch_sha256`
as the expected digest:

```text
mimir session git repair <exact-session-id> <full-lowercase-40-character-sha> --expected-digest <oldsha256> --json
mimir session get <exact-session-id> --json
mimir session git verify <exact-session-id> <full-lowercase-40-character-sha> --json
```

The expected digest guards against replacing a concurrently changed artifact;
repair preserves an audit of the prior artifact. Confirm the repair audit and
new digest, then require independent verification again. Do not automatically
repair capture conflicts caused by unrelated metadata or content mismatches.
Investigate the mismatch first; a conflict alone is not evidence of corruption.
No bulk repair or guessed mutations: target only the exact session and SHA,
and for root-session work target the exact root session. If identity, the trusted
local SHA, or the expected digest cannot be established, leave it untouched.

Record the outcome **separately**, after checking the result and independently
verifying every relevant saved Git artifact. OpenCode's
native outcome tool accepts `commit` (full SHA) and `evidence` (a JSON object
encoded as a string); use it only for the exact current OpenCode session. In
other harnesses use the CLI:

```text
mimir session outcome <exact-session-id> <value> --reason "observed result" --evidence '<json-object>' --json
mimir session get <exact-session-id> --json
```

For a pushed commit, evidence should contain `commit` (full SHA),
credential-free `repository_url`, credential-free `commit_url`, `ref`, and a
`note` describing only verification actually observed (for example, a
confirmed push and passing checks). Derive URLs from a verified remote and
omit them if no safe browsable URL exists. Do not claim a push from a local
commit, or tests passing without running them. If several commits matter,
capture each one; cite each in verifiable outcome evidence rather than
silently implying only the last commit exists. For no-commit work, use truthful
`note` or `url` evidence, without inventing a SHA. Read back the latest
`outcome_events` entry and confirm its `evidence_json`, outcome, and reason.
Outcome mutations can be retried; artifact upload and outcome mutation are
independent operations, not a single atomic receipt.

For example, after independently confirming the push and checks, pass this as
one JSON argument (or as the native OpenCode tool's `evidence` string):

```json
{"commit":"<full-sha>","repository_url":"https://github.com/owner/repo","commit_url":"https://github.com/owner/repo/commit/<full-sha>","ref":"main","note":"confirmed on origin/main; checks passed"}
```

Replace every placeholder with observed values; this is not a claim that
either action happened in the current session. For multiple commits, include
an additional `commits` array with full SHAs and associated URLs/refs when
known, while retaining a primary `commit` field.

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
cannot expose an exact identity, the Worker projects the generation
deterministically at finalization: a clean completed primary turn remains
`unresolved` without work-result evidence; a failed, pending, or absent terminal
signal becomes `abandoned`. A stale unresolved root may later become `landed`
only with saved Git commit evidence and a retrievable patch.
Resuming a finalized exact session starts a new `unresolved` generation while
preserving the prior outcome history.

Outcome must be verified before fetching the receipt so the returned status
reflects both projections. Capture and work outcome remain independent: a
saved session can be unresolved, and landed work is not proof that its
exchanges or Git patch were saved. Use `mimir session status <id> --json` (or
OpenCode's native status tool) after the read-back; if status times out, use
`mimir session get <id> --json` for the canonical state instead.

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

Only when the user explicitly asks to end, close, or finalize the session,
run `mimir session git verify <exact-session-id> <full-lowercase-40-character-sha>
--json` for every relevant saved Git artifact, verify the latest outcome event contains
the intended evidence, and the session's current state via `mimir session get
<id> --json`. Capture missing artifacts and verify them; correct missing outcome
evidence before ending. Repair corrupted existing artifacts only with the trusted
local SHA and expected digest as described above. If identity or
persistence remains unverified, do not claim the workflow completed. Then:

```text
mimir session end <exact-session-id> --json
mimir session get <exact-session-id> --json
mimir session git verify <exact-session-id> <full-lowercase-40-character-sha> --json
```

Repeat verification for every relevant SHA. Confirm the returned session is
inactive, every artifact is still saved and independently verified, and
the intended outcome evidence remains. An end-command timeout is not proof
that it failed; read the canonical session before retrying. Do not commit,
push, or end automatically. An ended exact session may reactivate on later
traffic.

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

Hermes history is not an `import` or `backfill` source today. Do not promise
automatic recovery of older uncaptured Hermes sessions. Retained local Hermes
history would need a separate opt-in importer.

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
