# Session Lifecycle And Harness Capture

Mimir combines capture paths around one lifecycle owner. The Worker proxy
persists full redacted OpenRouter exchanges. Pi, OpenCode, and the Claude Code,
Codex, and Cursor hook adapter can persist bounded exchanges reconstructed from
harness-visible prompts and responses. Harness integrations also report
lifecycle events; Hermes direct providers report event-only turn summaries and
suppress turns known to have traversed the proxy. One Session Durable Object
coordinates each exact session ID.

```mermaid
stateDiagram-v2
    state first_event <<choice>>
    [*] --> first_event
    first_event --> Active: heartbeat, turn, or saved exchange
    first_event --> Finalizing: end
    Active --> Disconnected: about 90 seconds silent
    Active --> Finalizing: end event or explicit end
    Disconnected --> Active: accepted activity
    Disconnected --> Finalizing: end, explicit end, or about 10 minutes total silence
    Finalizing --> Finalizing: durable write retry
    Finalizing --> Finalized: manifest and lifecycle state saved
    Finalized --> Active: accepted new activity with same ID

    note right of Disconnected
      Liveness projection only.
      Durable capture and work
      outcome remain independent.
    end note
```

`x-mimir-session` is the authoritative boundary. R2 and D1 are canonical for
saved proxy and reconstructed harness exchanges, searchable metadata, and
finalized lifecycle state. The Durable Object owns the bounded live event-turn
buffer; event excerpts are not promoted into the R2 transcript manifest or D1
search metadata.

## Starting

Sessions start lazily. There is no separate start command.

A session starts from the first activity carrying its session ID:

1. A harness start hook sends a heartbeat.
2. The first completed turn arrives if the start hook was missed.
3. The first capture-eligible proxied request carrying `x-mimir-session` is
   successfully saved and reported to the session object.

Installing Mimir or launching an idle harness does not create a session.

## Finalizing

A session finalizes through any of three triggers:

1. **End event.** A supported session-finalize hook reports an end and
   finalization begins immediately. OpenCode sends this for `session.deleted`;
   ordinary process exit relies on the silence timer.
2. **Silence timer.** Every accepted non-duplicate event re-arms a server-side
   alarm. About ten minutes without an event finalizes sessions left by a
   crash, killed terminal, laptop sleep, or network loss.
3. **Explicit request.** CLI `mimir session end <id>` finalizes the active
   generation.

All three write or rewrite `sessions/<id>/transcript.json` in R2, update the D1
lifecycle row, broadcast the final state, and let the Durable Object sleep.
Finalization failures schedule a retry.

Repeated end requests are safe. Retried turns are deduplicated by exchange ID,
and stale heartbeat retries cannot reopen a session that has already
finalized.

## Reopening

Finalization is not a tombstone. New activity carrying the same exact session
ID wakes the same object, preserves its history, and starts another active
generation. The next finalization rewrites the transcript manifest with every
saved proxy exchange still indexed for that session and aggregate plugin-turn
counters. Plugin turn payloads remain only in the bounded Durable Object live
buffer. A genuinely new harness session receives a new ID and therefore a new
object.

This is intentional: a user can resume the same harness conversation after a
clean end, a silence timeout, sleep, or disconnection.

## Liveness

Liveness is a projection from event age, independent of durable capture state
and work outcome:

- **`active`** — an event arrived within about 90 seconds.
- **`disconnected`** — the session has been silent for more than about 90
  seconds, but its finalization alarm has not fired.
- **`finalized`** — the final transcript and lifecycle write completed.

Returning activity can move `disconnected` or `finalized` back to `active`.
The ten-minute timer is a durability backstop, not a liveness promise.

## Capture Responsibilities

| Component | Responsibility |
| --- | --- |
| Worker proxy | Stream upstream responses; redact and persist full exchanges to R2/D1; report saved exchanges to the session object |
| Pi extension | Route OpenRouter through Mimir with exact session headers; persist bounded reconstructed direct-provider turns; report heartbeats, titles, and lifecycle events |
| OpenCode plugin | Persist bounded reconstructed direct-provider exchanges; report completed turns, heartbeats, titles, and supported lifecycle events |
| Hermes plugin | Report direct-provider turn summaries and lifecycle events; suppress duplicate turns for known proxied traffic |
| Claude Code, Codex, and Cursor hooks | Pair supported prompt/completion hooks into bounded reconstructed exchanges and report start/end lifecycle events |
| Session Durable Object | Coordinate liveness, retries, reopening, live feed, transcript manifests, and D1 lifecycle state |
| CLI | Primary search, inspection, outcome, explicit-end, deployment, and diagnostics surface |
| Dashboard | Access-protected session and request views backed by Worker APIs |

Event payloads contain summaries and excerpts, not transport archives.
Reconstructed harness exchanges are persisted and searchable after Worker
redaction, but can only contain fields exposed by the harness and may omit tool
activity, transport metadata, exact token use, or timing. Only traffic that
reaches the Worker proxy produces a full redacted transport exchange.

## Persistence Verification

Transport success is not proof that an exchange was saved. Use:

```bash
mimir session status <id>
```

The authoritative receipt distinguishes saved, pending, partial, failed, and
uncaptured sessions. Capture state and work outcome remain independent.
