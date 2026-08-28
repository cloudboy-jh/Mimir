# Mimir v2 Implementation Specification

This document describes the current implementation. Product intent is defined
in [`PRODUCT.md`](PRODUCT.md), dashboard visual direction in
[`DESIGN.md`](DESIGN.md), and installation and usage in the root
[`README.md`](../README.md).

## 1. Purpose

Mimir is a self-hosted memory plane for one developer working across coding
agents, repositories, and machines. It captures model traffic as sessions and
makes that history available through HTTP, the CLI, and the private dashboard.

The deployment runs in the developer's Cloudflare account. Mimir has no hosted
backend, account system, multi-user tenancy, or shared memory service.

## 2. System Boundary

One Cloudflare Worker provides:

- OpenAI Chat Completions and Anthropic Messages proxy routes
- Session, search, configuration, and log APIs
- Cloudflare Access-protected dashboard APIs
- Static Vue dashboard assets

The Worker uses:

- **OpenRouter** as the only model upstream
- **D1** for sessions, searchable exchange metadata, configuration, facets,
  and machine-token hashes
- **R2** for complete redacted request/response objects and session transcripts
- **Session Durable Objects** for live session lifecycle: event collection,
  liveness, the live feed, and finalization
- **Cloudflare Access** for deployed dashboard API authentication

The Go binary provides setup, login, diagnostics, local code indexing, and the
primary command-line memory client. Worker HTTP APIs remain canonical; the CLI
and harness plugins are clients of those APIs.

Local code memory remains `<repo>/.mimir/index.json`. It is never uploaded to
D1 or R2.

```mermaid
flowchart LR
    subgraph LOCAL[Developer machines]
        H[Agent harness]
        C[Go CLI]
        B[Dashboard browser]
        I[(Local code index)]
        H <--> C
        C --- I
    end

    subgraph CF[Developer's Cloudflare account]
        W[Worker proxy and canonical API]
        S[Session Durable Object]
        R[(R2 redacted objects)]
        D[(D1 metadata and state)]

        W -->|redacted exchanges| R
        W -->|search metadata and references| D
        W -.->|events and saved exchanges| S
        S -->|transcript manifest| R
        S -->|lifecycle state| D
    end

    O[OpenRouter]

    H <-->|redirected model stream| W
    H -.->|turns, heartbeats, ends| W
    C <-->|machine-token HTTP API| W
    B <-->|Access-protected dashboard API| W
    W <-->|upstream stream| O
```

## 3. Authentication

### 3.1 Machine Requests

Proxy, canonical API, and CLI requests use a per-machine token supplied as
either:

```http
Authorization: Bearer <token>
```

or:

```http
x-api-key: <token>
```

Each machine gets an independent random 32-byte token. D1 stores only its
SHA-256 hash, label, creation time, and revocation state. The plaintext token is
stored locally in `~/.mimir/token`, or under `$MIMIR_HOME`, with restrictive
permissions.

Before forwarding model requests, the Worker removes machine credentials and
all `x-mimir-*` metadata, then authenticates upstream using the
`OPENROUTER_API_KEY` Worker secret.

### 3.2 Dashboard Requests

Deployed `/dashboard/api/*` and `/dashboard/log-objects/*` routes require a
verified `Cf-Access-Jwt-Assertion`. Verification uses:

- `DASHBOARD_ACCESS_AUD`
- `DASHBOARD_ACCESS_TEAM_DOMAIN`

Localhost dashboard API requests may bypass Access for development. Static SPA
assets are served separately from dashboard API authentication.

Setup can configure the Cloudflare Access application when supplied an API
token. `mimir access` can create, repair, or attach an existing application
later.

## 4. HTTP API

### 4.1 Proxy

| Method | Route | Behavior |
| --- | --- | --- |
| `POST` | `/v1/chat/completions` | OpenAI-style Chat Completions proxy. |
| `POST` | `/v1/messages` | Anthropic-style Messages proxy. |
| `GET` | `/v1/models` | OpenRouter model-list pass-through. |
| `GET` | `/v1/key` | OpenRouter API-key metadata pass-through. |
| `GET` | `/v1/credits` | OpenRouter account-credit pass-through. |
| `POST` | `/v1/hermes/<installation-id>/chat/completions` | Installation-scoped Hermes Chat Completions proxy; capture is tagged `hermes`. |
| `GET` | `/v1/hermes/<installation-id>/{models,key,credits}` | Installation-scoped Hermes OpenRouter compatibility pass-through. |
| `POST`, `GET` | `/v1/hermes/{chat/completions,models,key,credits}` | Explicit legacy compatibility routes. |

These routes do not implement the complete OpenAI or Anthropic API surfaces.
General compatibility routes require a Mimir machine token. The listed Hermes routes
additionally accept registered Hermes OpenRouter credentials, but those credentials cannot
access session, log, dashboard, or configuration routes. Key and credit routes
expose the deployment owner's OpenRouter account metadata, matching Mimir's
personal single-owner trust model.

### 4.2 Canonical Machine API

| Method | Route | Behavior |
| --- | --- | --- |
| `GET` | `/whoami` | Return deployment URL and session/exchange counts. |
| `POST` | `/machine/associate` | Associate the authenticated token with one installation when `whoami.capabilities` includes `machine_identity_association`. |
| `GET` | `/sessions` | List up to 100 recent sessions with optional filters. |
| `GET` | `/sessions/:id` | Return one session, exchanges, files, and errors. |
| `GET` | `/sessions/:id/status` | Return the derived capture summary and human receipt, with a link when Access is configured. |
| `POST` | `/sessions/:id/end` | Idempotently end the current active generation and optionally record its outcome. Also notifies the session object, which finalizes. |
| `POST` | `/sessions/:id/events` | Append a validated session event (`turn`, `heartbeat`, `end`) to the session object. The path session ID is authoritative; events may carry a harness title. |
| `POST` | `/sessions/:id/exchanges` | Validate, redact, and persist a bounded exchange reconstructed by a trusted harness integration. |
| `GET` | `/sessions/:id/live` | WebSocket live feed from the session object: snapshot plus event broadcast. |
| `GET` | `/sessions/:id/object-state` | Read the session object's liveness projection and counters. |
| `POST` | `/sessions/:id/outcome` | Append an evidenced work-outcome event. |
| `POST` | `/sessions/:id/mark` | Deprecated legacy alias for setting an outcome. |
| `POST` | `/reconcile` | Reconcile bounded D1 capture rows against R2 and report orphans. |
| `POST` | `/search` | Search session metadata and excerpts. |
| `GET` | `/config` | Return defaults merged with persisted configuration. |
| `PUT` | `/config` | Validate and persist a partial configuration update. |
| `POST` | `/integrations/hermes/authorize` | Register a SHA-256 digest for a Hermes OpenRouter credential. |
| `POST` | `/integrations/harness-loads` | Record the source hash and receipt identity reported by a loaded managed integration. |
| `GET` | `/integrations/harness-loads` | Return harness-load reports for the authenticated machine so doctor can compare installed and active bytes. |
| `GET` | `/log/*` | Read one redacted R2 exchange object. |

Session-list filters include repository, model, outcome, and date range.

### 4.3 Dashboard API

| Method | Route | Behavior |
| --- | --- | --- |
| `GET` | `/dashboard/api/identity` | Return safe Cloudflare Access identity fields or the local-development identity. |
| `GET` | `/dashboard/api/bootstrap` | Return basic request/session totals. |
| `GET` | `/dashboard/api/log` | Cursor-paginated exchange metadata. |
| `GET` | `/dashboard/api/log/:id` | Return one exchange and its log-object URL. |
| `GET` | `/dashboard/log-objects/*` | Return one redacted R2 object. |
| `GET` | `/dashboard/api/sessions` | Filter and cursor-paginate root sessions. |
| `GET` | `/dashboard/api/sessions/:id` | Return session metadata, capture, files, aggregated errors, and outcome history. |
| `GET` | `/dashboard/api/sessions/:id/exchanges` | Filter, sort, and cursor-paginate the session subtree timeline. |
| `GET` | `/dashboard/api/sessions/:id/status` | Return the derived capture summary. |
| `POST` | `/dashboard/api/sessions/:id/outcome` | Append a user-sourced work-outcome event. |
| `POST` | `/dashboard/api/sessions/:id/mark` | Deprecated legacy alias for setting an outcome. |
| `PATCH` | `/dashboard/api/sessions/:id/title` | Set a normalized manual title of at most 200 characters. |
| `GET` | `/dashboard/api/devices` | List devices with status, activity, harnesses, and root-session counts. |
| `PATCH` | `/dashboard/api/devices/:id` | Change a device's display name. |
| `POST` | `/dashboard/api/devices/:id/revoke` | Irreversibly revoke a device through the dashboard. |
| `GET` | `/dashboard/api/overview` | Return aggregate totals and top facets. |
| `GET` | `/dashboard/api/facets` | Return filter vocabulary, optionally scoped to one session subtree. |

The Vue client reads live Access-protected dashboard APIs for Sessions,
Requests, Overview, details, R2 payload retrieval, capture status, and outcome
mutation.

`/dashboard/api/facets` returns `repos`, `apps`, `models`, `providers`, and
`finish_reasons` from saved exchanges, ordered by request frequency and bounded
to 50 values each. A `session` parameter scopes exchange facets to that session
subtree and returns no repositories. Every dashboard filter is a dropdown backed
by this vocabulary rather than a free-text exact-match field; an active filter
value outside the bounded list stays selectable so a URL filter never renders
blank.

Dashboard session records retain `model_primary` as the backward-compatible
first-model projection and add `models`, an exact-session array ordered with the
primary model first and all secondary models by first appearance. Each entry
contains the exact model identifier, saved request count, and first/last seen
timestamps. Root records never absorb models used only by supporting sessions;
supporting runs receive their own arrays. Session model search and filtering
match any saved model used by that exact session while retaining
`model_primary` as a fallback for legacy rows. The sessions table renders the
primary model with `+N models`; session detail renders the app as a parent with
every model below it.

Dashboard session errors are aggregated at read time from `exchange_errors`
joined to saved exchanges: each signature carries an occurrence count, first
and last seen timestamps, and the newest matching exchange ID. Signatures that
only exist in `session_errors` (recorded before facet projection) render with
count 1 and no timing data.

Outcome events may carry structured evidence in `evidence_json`:
`{ commit, base_commit?, patch?, repository_url?, commit_url?, ref?,
provenance?, url?, note? }`. The OpenCode plugin attaches the HEAD commit, its
parent, the branch, the normalized `origin` remote, and a bounded redacted
unified patch (20 KB cap) when an outcome is recorded from the harness; the
dashboard outcome form accepts a single commit, URL, or note entry. Remotes are
normalized to a credential-free `https` URL, and local-path remotes are
omitted. Commit values are never inferred from refs.

Landed outcomes carrying Git evidence require an exact 40-character commit,
explicit `provenance`, and a non-empty patch. The Worker writes the redacted
patch to R2 before appending the outcome event; failed validation or persistence
leaves the prior outcome unchanged. User-authored landed outcomes without Git
evidence remain valid.

The dashboard derives changed-file counts and per-file `+`/`−` totals from the
stored patch and links a commit only from `commit_url` or a recorded
`repository_url`; without a stored remote it shows the bare SHA. Commit-derived
changed files stay separate from tool-touched files, and every session-detail
list, including supporting runs, files, and errors, is bounded with explicit
disclosure. Session detail never contacts Git hosts.

Historical import additionally preserves Git work as independent, multi-commit
session artifacts. These records do not derive or mutate the work outcome and
are retained for `landed`, `discarded`, `abandoned`, and `unresolved` sessions.
Each artifact carries a full commit SHA, optional first parent, commit time,
subject, normalized repository URL, historical ref, provenance, patch digest,
patch statistics, storage lifecycle, and R2 reference. The patch is independently
redacted and bounded before upload. Git artifact evidence supplements the
single-commit evidence that may accompany an outcome event; it does not replace
outcome history.

File and error facets are projected from structured exchange content, not from a
text scan of the payload. Files come only from tool-call arguments in either the
OpenAI `function.arguments` or Anthropic `tool_use.input` shape, read from
path-bearing keys, with dependency directories excluded and 40 kept per
exchange. Errors come only from explicit failure signals: provider error
envelopes, stream `error` events, and tool results flagged `is_error` or a
non-zero exit code, with 10 kept per exchange. Detection reads the trailing
messages of a request so a failure is not re-counted against every later
exchange that replays the transcript. Prose that merely mentions a path or the
word "error" is not a facet.

The session-note menu renders readable Markdown client-side from the session
detail and timeline responses, capped at 500 exchanges. Download remains a
transient browser export. In supported Chromium browsers, a user may instead
connect an Obsidian vault and write the same Markdown to
`<notes-folder>/<repository>/<session-date>-<session-hash>.md` before opening it
through Obsidian's desktop URI. The directory handle stays in browser-local
IndexedDB, existing notes are never overwritten, and exports contain excerpts
and metadata rather than raw R2 payloads.

## 5. Capture Lifecycle

For a supported model request, the Worker:

1. Authenticates the machine token.
2. Reads and bounds the request body at 10 MiB.
3. Parses optional Mimir session metadata.
4. Reads capture configuration and lazily expires stale sessions.
5. Replaces caller credentials with the OpenRouter Worker secret.
6. Sends the request to OpenRouter.
7. Returns the upstream response stream to the caller.
8. Uses a second stream branch and `waitUntil` for persistence.
9. Resolves the session, redacts the request, and records an accepted exchange in D1 while the archive stream is still being consumed.
10. Bounds the captured response at 20 MiB.
11. Parses ordinary JSON or reconstructs server-sent events.
12. Redacts the response and derives searchable evidence.
13. Writes the complete redacted v1 envelope to R2.
14. Finalizes the exchange as saved and updates facets and session aggregates in D1.

`x-mimir-request-kind` accepts `primary`, `title`, `summary`, or `compaction`
and defaults to `primary` for compatibility. The effective kind is persisted
with the exchange and stripped before forwarding upstream. Title, summary, and
compaction exchanges remain part of the exact session and contribute evidence
and usage, but cannot initialize or overwrite session intent. The Worker also
defensively recognizes known title-agent prompts that were mislabeled as
primary.

Capture can be disabled globally or excluded by repository/model. The same
policy applies to proxied and reported exchanges. Excluded proxy traffic is
still forwarded, while reported ingestion returns an explicit skipped response;
neither path creates exchange metadata or an R2 object for skipped traffic.

### 5.1 Capture Provenance

Mimir persists two exchange provenances with different guarantees:

- Proxy exchanges are complete redacted request/response envelopes observed on
  the supported OpenRouter transport routes. They preserve the streamed
  response subject to capture size limits.
- Reported exchanges are bounded reconstructions sent to
  `POST /sessions/:id/exchanges` by a trusted local harness integration. The
  Worker applies the same redaction, R2 persistence, searchable metadata, and
  capture-state accounting, but only fields exposed by the harness can be
  retained.

Every reported exchange carries a canonical `tool_activity` array. Each entry
contains a bounded tool name, a JSON object input, `succeeded` or `failed`
status, and an optional bounded output string. Integrations submit an empty
array when their harness does not expose tool calls. The Worker validates and
redacts this array before deriving file and error facets.

OpenCode reconstructs non-OpenRouter exchanges from its session store, including
supported message and tool parts, model/provider, usage, timing, finish reason,
and error fields. Its payload is capped at 512 KiB, individual strings at 64
KiB, and excess parts are removed. OpenRouter exchanges are not uploaded by the
plugin because the proxy record is canonical.

Historical import follows the same provenance rule. The OpenCode adapter reads
`opencode session list --format json` and `opencode export <id>`. The Pi adapter
reads regular `.jsonl` files below `$PI_CODING_AGENT_DIR/sessions`, defaulting
to `~/.pi/agent/sessions`, without following symlinks. Both adapters skip turns
whose provider is OpenRouter so a local reconstruction cannot compete with or
replace the canonical proxy exchange.

The Claude Code, Codex, and Cursor command-hook adapter pairs supported prompt
and completion events. It caps each prompt and response at 512 KiB and reports
zero token counts and latency when the hook payload does not expose them. These
records are not byte-for-byte provider traffic and generally omit tool traces.
Hermes direct-provider capture remains event-only: its completed-turn summary is
kept in the bounded Session Durable Object buffer and does not produce an R2
exchange or searchable D1 exchange row.

A response larger than the capture limit can still reach the caller even when
archive persistence fails. A D1 finalization failure after the R2 write leaves
an accepted row that reconciliation can finalize; a failure before D1 accepts
the exchange can leave no durable capture record.

## 6. Redaction And Evidence

Redaction runs before R2 storage and before searchable excerpts are generated.
Built-in patterns cover common API-key, bearer-token, secret, token, and
password forms. `redact.patterns` adds user-defined regular expressions.

Mimir derives:

- Request and response excerpts, capped at 8,000 characters each
- File-like paths, capped at 100 unique values per exchange
- Error signatures, capped at 20 unique values per exchange
- Model, provider, finish reason, token usage, latency, endpoint, harness,
  repository, and machine label

Derivation is regex-based. Redaction reduces accidental retention but cannot
guarantee removal of every secret or sensitive value.

## 7. Sessions

`x-mimir-session` is authoritative when present. It must be a 1-128 character
identifier accepted by the Worker. A declared session can be resumed and
reactivated later.

Without that header, Mimir groups traffic by exact repository/harness metadata
and a configurable inactivity gap. The default gap is 15 minutes. Expiration
is lazy and runs during relevant Worker requests rather than on a timer.

Optional metadata headers are:

- `x-mimir-session`
- `x-mimir-repo`
- `x-mimir-harness`
- `x-mimir-git-ref`
- `x-mimir-request-kind`

Session intent is sticky and comes from the first saved primary exchange with
a non-empty user message. Intent candidates are stored on accepted exchanges
so reconciliation applies the same rule after an interrupted D1 finalization.

Session titles are first-class metadata with source and update time. Display
falls back through `title`, `intent`, then session ID. Source precedence is
`manual` > `harness` > `generated` > `derived`: a dashboard edit is manual, a
title on a lifecycle event is harness-supplied, a saved `request_kind: title`
response is generated, and the first saved primary intent is derived. A weaker
source cannot overwrite a stronger source; the newest value wins within the
same source rank. Title, summary, and compaction exchanges cannot initialize or
overwrite intent.

Canonical work outcomes are `landed`, `discarded`, `abandoned`, and
`unresolved`. Outcome is independent from capture: `landed` says the result was
kept, while `saved` says an exchange is durably represented in both R2 and D1.

Git artifacts are independent of both projections: their presence does not make
an outcome landed, and an unresolved session may have any number of preserved
commit artifacts within the ingestion bounds.

### 7.1 Historical Import And Gap Repair

`mimir import` is a user- and agent-facing local-history merge. Interactive
forms are `mimir import` and `mimir import <opencode|pi>`; explicit mutation is
`mimir import <opencode|pi> <id>... [--yes] [--json]`. Read-only discovery is
`mimir import list [opencode|pi] [--json]`, and one candidate can be examined
with `mimir import inspect <opencode|pi> <id> [--json]`.

`mimir backfill [opencode|pi] [--since 7d]` repairs gaps from local history. If
the harness is omitted, both supported sources are scanned. `--since` accepts a
positive Go duration, positive day shorthand, or RFC3339 timestamp. Interactive
import and backfill use a bounded selector of at most 20 candidates. In any
non-TTY context, including `--json`, import requires an explicit harness, one or
more IDs, and `--yes`; backfill requires `--all --yes`. The standard fully
noninteractive form is `mimir backfill [opencode|pi] [--since 7d] --all --yes
--json`. These requirements prevent a pipe, CI job, or agent tool call from
bulk-uploading local history by accident.

Merge identity is deterministic. A safe source session ID is retained as the
Mimir session ID; otherwise it is replaced by a harness-scoped SHA-256-derived
ID. OpenCode exchanges use safe source message IDs, and Pi exchanges use a
digest of canonical session ID, assistant timestamp, and turn index. Discovery
deduplicates requested IDs, sorts candidates deterministically, and upload
orders sessions chronologically. The reported-exchange API's duplicate handling
then makes reruns idempotent and allows backfill to repair only missing records.
Imported sessions receive historical heartbeat and end events after at least
one exchange is saved or found as a duplicate.

Git collection is a separate post-exchange step when the local session includes
a checkout directory. It considers bounded commits in the session time window,
filters to commits overlapping tool-touched paths when such paths exist, and emits
up to 15 non-empty redacted patches by default. Upload never calls an outcome
endpoint. `(root session_id, commit_sha)` is the artifact key: an exact repeat
is a duplicate, while differing patch content or metadata returns `409` and
leaves the stored artifact unchanged.

### 7.2 Session Objects

Each session is owned live by a Session Durable Object named by the session
ID. Reporters append versioned events (`turn`, `heartbeat`, `end`); capture
reports a `turn` after every saved exchange, and `/sessions/:id/end` reports
`end`. The object tracks liveness and performs the final write. R2 and D1 are
canonical for saved proxy and reported exchanges, searchable metadata, and
finalized lifecycle state. Plugin turn payloads remain in a bounded Durable Object live
buffer and are not copied into the R2 transcript manifest or D1 search data.

A session finalizes when any of three triggers fires: an `end` event, a
10-minute silence alarm (re-armed by every accepted non-duplicate event), or
the explicit end route. Supported finalize hooks end immediately; ordinary
process exits and hard deaths finalize within ~10 minutes. Finalization writes a session transcript manifest to
`sessions/<id>/transcript.json` in R2 and marks the D1 session inactive using
the same generation semantics as the explicit end route. It is idempotent per
active period and retries on failure.

Liveness is a read-time projection from heartbeat age: `active` within 90
seconds, `disconnected` past 90 seconds but not yet finalized, and
`finalized` after the final write. The silence timer is a durability
backstop, not a UX promise.

New events on a finalized session reopen it: the same object wakes, D1 flips
back to `active`, and history continues. Finalized is a state, not a
tombstone.

Session responses expose the latest outcome projection:| Field | Contract |
| --- | --- |
| `state` | Session activity: `active` or `inactive`. |
| `outcome` | Canonical work outcome from `work_outcome`; defaults to `unresolved`. |
| `outcome_src` | Source of the latest event: `agent`, `user`, `git`, or migration backfill. |
| `outcome_reason` | Evidence supplied with the latest event, or `null`. |
| `outcome_updated_at` | Timestamp of the latest event, or `null`. |

Each exchange has `capture_status` `accepted`, `saved`, or `failed`, plus
`capture_reason`, `accepted_at`, `saved_at`, `failed_at`, `failure_code`,
`schema_version`, and `r2_bytes`. Session detail and status APIs derive a
separate capture object with `saved_exchanges`, `failed_exchanges`,
`pending_exchanges`, `last_saved_at`, and status `empty`, `pending`, `saved`,
`failed`, or `partial`.
`pending` means at least one accepted exchange remains; `partial` means both
saved and failed exchanges exist; `empty` means none of those states exists.
The Worker does not infer work outcomes from capture success.

Status responses also contain a compact `receipt` with `label`, `detail`, and
`action_label`. When Cloudflare Access is configured, they include a
credential-free `dashboard_url` under `/dashboard/sessions/:id` and a `View
session` or `View details` action. Without Access configuration, both fields are
`null` rather than advertising a broken link. Receipt copy is intended for
harness tool chrome: `Saved to Mimir · 14 exchanges in this session`. Raw IDs,
timestamps, and failure codes remain available in API data and, when linked, on
the session page rather than in normal agent prose. Status responses use
`Cache-Control: no-store`.

Outcome changes append immutable events containing `id`, `session_id`,
`outcome`, `source`, optional `reason`, optional `evidence_json`, and
`created_at`. The session fields above cache the latest event for listing and
filtering; a later event supersedes the projection without deleting history.
Machine-token outcome routes assign source `agent`, and Access-protected
dashboard routes assign source `user`; caller-supplied source values cannot
override that attribution. `git` is reserved for trusted automated evidence,
and `migration` identifies the legacy backfill.

## 8. Search And Configuration

Remote search uses SQL substring matching over session intent, exchange
excerpts, normalized files, and error signatures. It supports repository and
outcome filters, orders results by recency, and applies an approximate response
budget. It is not semantic or vector search and does not read complete R2
objects.

CLI search federates remote results with local code recall when a usable
`.mimir/index.json` exists in the current repository.

Supported configuration keys are:

| Key | Purpose |
| --- | --- |
| `save.enabled` | Enable or disable persistence. |
| `save.exclude_repos` | Repository exclusion patterns. |
| `save.exclude_models` | Model exclusion patterns. |
| `redact.patterns` | Additional redaction expressions. |
| `session.gap_minutes` | Heuristic inactivity gap. |

Configuration is stored in D1 and takes effect without redeployment.

## 9. Storage Model

### 9.1 R2

Each saved exchange is one redacted JSON object under:

```text
log/YYYY/MM/DD/<ulid>.json
```

New writes use the versioned v1 envelope:

```json
{
  "schema_version": 1,
  "exchange_id": "<ulid>",
  "session_id": "<resolved-session-id>",
  "declared_session_id": "<header-value-or-null>",
  "captured_at": "<rfc3339>",
  "endpoint": "chat",
  "request": {},
  "response": { "format": "json", "body": {} },
  "metadata": {
    "repo": "<repo-or-null>",
    "harness": "<harness-or-null>",
    "git_ref": "<git-ref-or-null>",
    "model": "<model>",
    "provider": "<provider-or-null>",
    "finish_reason": "<reason-or-null>"
  },
  "usage": { "input_tokens": 0, "output_tokens": 0 },
  "latency_ms": 0,
  "redaction": { "version": 1 }
}
```

The resolved `session_id`, not only the caller-declared header, is stored in the
object. Reconstructed streams use `response.format: "reconstructed_sse"` with
`content` and `events` instead of `body`. Request and response are redacted
before this write; repository, harness, and Git metadata are intentionally
stored as searchable identifiers. D1 keeps the envelope version, R2 key, and
byte count beside searchable metadata.

Saved Git patches are separate text objects under:

```text
sessions/<root-session-id>/git/<commit-sha>/<patch-sha256>.patch
```

The Worker applies configured redaction before hashing and writing the UTF-8
patch. The deterministic key makes an exact retry idempotent. R2 custom metadata
records the root session ID, commit SHA, and patch SHA-256; patch content is not
stored in D1. The dashboard patch endpoint streams only an artifact whose D1
capture state is `saved`, with private no-store caching.

D1 first records an accepted exchange, then R2 receives the envelope, then D1
finalizes it as saved and updates session aggregates. A bounded reconcile pass
checks accepted and saved rows with R2 `HEAD` requests. Accepted rows with an
object are finalized idempotently. Accepted rows without one stay pending for
15 minutes; after that they can never finish, so reconcile marks them failed
with `r2_object_missing` and reports them as swept instead of leaving sessions
permanently pending. Saved rows missing their object become
failed with `r2_object_missing`, and affected session aggregates are rebuilt
from saved rows. Schema-v1 file and error facets are retained per exchange, so
that rebuild excludes facets belonging only to missing objects. Sessions that
contain legacy v0 exchanges retain their existing aggregate facets because v0
did not record exchange-level provenance. A separate bounded R2 listing reports
keys absent from D1 as orphans; reconcile does not import or delete them.
Independent D1 and R2 cursors and a bounded limit make repeated runs safe.
Each pass scans at most 100 D1 rows and 100 R2 keys to stay within Worker
binding-operation limits.

Legacy v0 objects are the existing unversioned shape with top-level `id`, `ts`,
`session`, `request`, `response`, `usage`, and `meta`. Migration marks their D1
rows saved with `schema_version = 0`, `accepted_at = ts`, and `saved_at = ts`.
Objects remain readable as stored and are not rewritten during migration or
reconciliation.

### 9.2 D1

The migration sequence defines:

- `sessions`: identity, title provenance, timing, boundary, lifecycle, context, usage, and latest outcome projection
- `exchanges`: searchable metadata, request kind, title candidates, capture lifecycle, usage, latency, and R2 reference
- `exchange_files`: schema-v1 file facets with exchange-level provenance
- `exchange_errors`: schema-v1 error signatures with exchange-level provenance
- `session_outcome_events`: immutable outcome, source, reason, and timestamp history
- `session_git_artifacts`: independent per-commit metadata, patch digest and
  statistics, R2 key, and `accepted`/`saved`/`failed` storage lifecycle
- `session_files`: normalized file facets
- `session_errors`: normalized error facets
- `config`: persisted deployment configuration
- `machines`: stable installation identities, editable device names, platform metadata, activity, and revocation state
- `access_tokens`: machine-token hashes and lifecycle fields
- `hermes_credentials`: OpenRouter credential hashes authorized only for the Hermes proxy surface
- `harness_loads`: loaded integration source hashes and receipt identity used by diagnostics

D1 remains the searchable source of truth. R2 remains the complete redacted
archive.

Git artifact ingestion accepts version 1 bodies containing 1-50 commits and no
unknown fields. It resolves supporting-session IDs to the root session before
storage. D1 first inserts the metadata row as `accepted`, R2 then receives the
redacted patch, and D1 finalizes the row as `saved`. An R2 failure marks the row
`failed` with `r2_put_failed` and remains retryable; an interrupted D1 finalize
leaves an accepted row and returns a partial failure. The primary key is
`(session_id, commit_sha)`. A retry is a duplicate only when the patch digest,
deterministic R2 key, parent, timestamp, subject, repository URL, ref, and
provenance all match; otherwise ingestion returns a conflict without replacing
the existing artifact.

The machine API accepts `POST /sessions/:id/git-artifacts` with the exact body
`{ "version": 1, "commits": [...] }`. Each commit requires `commit_sha` and
`patch`; it may also contain `parent_commit_sha`, `committed_at`, `subject`,
`repository_url`, `ref`, and `provenance`. Omitted nullable metadata becomes
`null`, and omitted provenance defaults to `git`. The endpoint is
installation-owned, accepts a
maximum 5 MiB body, returns `201` when any artifact is newly saved, `200` when
all are duplicates, `409` for a conflicting commit, and `503` with per-commit
failures for a partial persistence result. Canonical session detail includes
the ordered `git_artifacts` metadata array. The Access-protected dashboard reads
patch content from `GET /dashboard/api/sessions/:id/git-artifacts/:commit/patch`;
machine session detail never embeds patch bodies.

### 9.3 Device Identity And Association

`installation_id` is a locally generated, stable 32-character lowercase
hexadecimal identifier for an installed Mimir instance. It is the machine key
used by tokens, sessions, harness-load reports, and scoped Hermes credentials;
it is not a hostname or user-facing label. `machines.name` is initialized from
the hostname and is independently editable in the dashboard. Re-association
refreshes platform metadata but does not overwrite an existing name.

Clients discover association support through the
`machine_identity_association` capability returned by `/whoami`, then send the
exact version-1 shape `version`, `installation_id`, `name`, `platform`, and
`arch` to `POST /machine/associate`. Association is first-writer: an
unassociated token may bind to one installation, repeated association with that
installation is idempotent, and an attempt to move it to another installation
returns `409`. The endpoint cannot rename a device.

Authenticated capture and lifecycle traffic carries the installation derived
from the token or scoped Hermes credential. A new session records it; an
existing exact session with no installation may acquire it once, but traffic
from another installation cannot replace it. Heuristic sessions are grouped by
installation as well as repository and harness. Dashboard session responses
resolve the stored installation to its current device name without changing the
session association.

Revoking a device timestamps the machine and all access tokens associated with
its installation. Subsequent machine-token requests and both scoped and legacy
Hermes credential authentication for that installation fail. Revocation does
not delete or detach historical sessions, exchanges, or the device row, and a
later rename only changes its display label. The dashboard has no restore
operation. Privileged setup/login registration never clears machine or token
revocation; a token generated for a revoked installation is not registered and
fails connection verification. Reconnect the physical machine with a new
installation identity instead of reusing the revoked identity.

### 9.4 Local Index

`mimir index` writes `<repo>/.mimir/index.json` atomically. It indexes Git
working files for selected programming-language extensions and records hashes,
regex-derived symbols, and dependencies. `mimir recall` performs deterministic
text ranking within an approximate character budget.

The local index is optional and independent from remote session storage.

## 10. Client And Harness Access

The CLI is the primary agent-facing memory client. It delegates search,
session inspection, local-history import and backfill, outcome updates,
explicit ending, configuration, and
diagnostics to the canonical Worker HTTP API. `mimir session status` performs a
bounded settle/poll while capture is pending and returns the authoritative
receipt without upgrading a still-pending final read optimistically.

Pi, OpenCode, Hermes, Claude Code, Codex, and Cursor integrations are capture
and lifecycle adapters, not alternate memory servers. Pi, OpenCode, and the command-hook
adapter can report reconstructed exchanges; Hermes direct providers report only
turn summaries and lifecycle events. Future harness-native search or control
access must call the canonical Worker API through the harness's supported
extension surface; Mimir does not spawn a local protocol server.

Ending a session sets it inactive and records the explicit end timestamp for
the current active generation. It does not alter capture state. A genuinely
later exchange carrying the same exact session header may reactivate it and
begin another generation, while repeated end calls remain idempotent.

During migration, deprecated API aliases accept `promoted` for
`landed` and `unknown` for `unresolved`. Canonical APIs, projections, filters,
and dashboard copy emit canonical values.

## 11. Setup And Login

`mimir setup`:

1. Materializes the Worker embedded in the running binary under
   `~/.mimir/worker` or `$MIMIR_HOME/worker`. Arbitrary source, including a
   development checkout, is available only through explicit `--worker-dir`.
   The current directory, parent directories, and Go module cache are never
   searched for an implicit Worker source.
2. Installs packaged Worker/Wrangler dependencies with npm. Dashboard
   dependencies and Bun are used only for an explicit `--worker-dir`
   development override; packaged setup uses the embedded compiled dashboard.
3. Authenticates Wrangler.
4. Creates or reuses D1 and R2.
5. Rewrites deployment binding identifiers.
6. Applies D1 migrations.
7. Registers the local machine token.
8. Reuses or stores the OpenRouter Worker secret.
9. Deploys and verifies the Worker.
10. Saves the local URL/token pointer.
11. Refreshes exact receipt-owned Pi, OpenCode, Claude Code, Codex, Cursor, and,
    when detected, Hermes artifacts without rewriting general harness config.
    Without an existing receipt containing managed artifacts, setup does not
    create an installation identity, install log, or global harness files.
12. Returns a harness-neutral connection manifest.

`mimir login` first accepts an already healthy saved connection. Its discovery
path materializes configuration without proactively installing dependencies,
then uses Wrangler to authenticate with Cloudflare, discover the deployment,
register a new machine token, and return the same connection manifest. Managed
installation and update may touch exact
opted-in plugin and skill files recorded in `install-receipt.json`; they do not
modify general OpenCode JSON/JSONC, providers, credentials, or commands.
Pi uses a receipt-owned global extension under its configured agent directory;
the extension overrides only Pi's OpenRouter provider at runtime, adds exact
session metadata, and reconstructs direct-provider turns. OpenCode integration
uses the managed plugin and OpenCode's supported plugin loading flow.

`mimir install` enrolls safe absent or byte-identical Claude Code plugin files
under `~/.claude/skills/mimir/` and hook manifests at `~/.codex/hooks.json` and
`~/.cursor/hooks.json`. It preserves a different existing file rather than
merging or replacing user-owned hook configuration. The manifests invoke the
receipt-owned hidden `mimir _hook` adapter, which writes a bounded private,
authenticated-encrypted outbox under `$MIMIR_HOME` before bounded best-effort
delivery. `CLAUDE_CONFIG_DIR` and `CODEX_HOME` override their official user
homes; Cursor has no documented equivalent. Harness start hooks also
report the embedded manifest hash so `mimir doctor` can distinguish installed
bytes from the active loaded version. Activation follows the harness-supported
reload path; Cursor hot-reloads its hooks file.

When Hermes desktop or TUI is installed, the same lifecycle commands append a
Mimir-owned block to the active Hermes profile `.env`. It redirects the built-in
OpenRouter provider to `/v1/hermes/<installation-id>` while preserving Hermes' OpenRouter key. The CLI
registers only the key's SHA-256 digest in D1. The Worker
implements Hermes' required chat, model, key, and credit routes and derives the
`hermes` capture tag from the route. No custom provider or model-list copy is
created. Each digest is registered independently for the stable installation ID, so the
same OpenRouter key can be used by multiple devices and revoking one device does not
revoke another. The unscoped legacy route succeeds only when exactly one active matching
credential exists. Registered credentials are accepted only on the explicit Hermes routes, so
Hermes features that still call OpenRouter directly never leak a Mimir machine
credential. Migration `0016` preserves credentials already associated with an
installation and retires unassociated legacy credential rows rather than
keeping an indefinitely valid unscoped credential. Hermes requests are
forwarded with the same OpenRouter credential they presented rather than
charging the Worker's default key. Direct Hermes
providers remain outside the Worker proxy because their requests do not reach
it; the bundled Hermes plugin captures their completed-turn summaries and
lifecycle events from inside the harness.

Explicit `mimir update` enrolls safe absent or byte-identical bundled harness
files and refreshes the Hermes integration. `mimir doctor` validates its route,
credential, and Worker compatibility without making a paid model request.
Updates preserve unowned conflicts and receipt-owned files changed by the user.
Operations are appended to `install-log.jsonl`. `mimir uninstall` removes only
regular, non-symlink receipt-owned plugin and skill files whose current hash
matches the receipt. It retains and reports every conflict, updates the receipt
atomically, and removes the binary only when its receipt method, path, and hash
show that Mimir installed it. The local connection, token, materialized Worker,
install log, and Cloudflare deployment remain in place. On Windows, uninstall
renames the verified running binary to an `.uninstall` path and launches a
detached standard-user cleanup process that deletes that exact path after the
uninstall process exits. A launch failure preserves the renamed binary and its
receipt ownership and makes the uninstall result partial.

Updates on Windows use the same deference when the receipt-owned executable is
locked by running Mimir processes or an antivirus filter: the updater retries
the rename, and on a persistent lock stages the verified binary next to the
target, records `$MIMIR_HOME/pending-update.json`, and launches a detached
standard-user helper that retries the receipt/path/hash-validated swap once the
executable lock clears. Any later CLI start is a second finalization path, and
the report status is `scheduled` rather than a failure. `mimir update --force`
stops sibling processes using the exact managed executable and swaps
immediately. Swap leftovers (`.old`, rollback files, orphaned staged temps) are
removed once no process holds them; foreign junk next to the executable is only
reported by doctor, never deleted.

`mimir deploy` is the only supported path for shipping Worker or dashboard
changes after setup. It materializes the packaged Worker and precompiled
dashboard, writes the discovered D1 database ID into the materialized config,
and runs `wrangler deploy`. Bun is needed only when an explicit `--worker-dir`
development override must compile dashboard source. The checked-in
`wrangler.jsonc` intentionally keeps a placeholder database ID; never deploy
from a source checkout without that override.

The manifest contains OpenAI and Anthropic base URLs, an absolute credential
path and command, and optional session metadata header names. For harnesses
without a bundled integration, the setup skill or user applies that manifest
using the harness's own secure configuration system.

Cloudflare Access protects the dashboard's authentication handoff, API, and
redacted objects with one self-hosted application covering exactly
`/dashboard/auth`, `/dashboard/api/*`, and `/dashboard/log-objects/*`. The
public `/login` route contains no private data; it sends the browser through
the protected handoff before returning to the dashboard. Access paths are exact
matches, so all three destinations are required. A bare-hostname application
would block the machine API (`/v1`, `/sessions`, ...) that the proxy and CLI
use. Machine routes stay outside Access and are authenticated by the Worker
with bearer tokens. Setup prompts for an optional Cloudflare API token and
automates the application when provided; `mimir access` runs the same
automation later (correcting wrong destinations in place), or applies a
manually created application's AUD tag and team domain via `--aud` and
`--team-domain`.

## 12. Dashboard Status

The Vue 3 dashboard is built and deployed as Worker static assets. It includes
Sessions, Requests, Overview, and detail routes with light/dark themes and the
design system defined in [`DESIGN.md`](DESIGN.md).

The dashboard uses `/dashboard/*` for browser routes and keeps `/sessions*`
reserved for the canonical machine API. Direct loads and receipt links use
`/dashboard/sessions/:id`. Sessions, request metadata, overview aggregates, and
outcome mutation read the Access-protected dashboard API. Full redacted request
and response payloads are loaded from R2 through `/dashboard/log-objects/*`.
`mimir dashboard` opens the branded `/login` handoff and returns to
`/dashboard/sessions` after Access authentication.

## 13. Observability

Wrangler observability is enabled for Worker logs and traces. Logs use full
head sampling and traces use 1% head sampling. This telemetry stays in the
developer's Cloudflare account.

## 14. Non-Goals

- Mimir-hosted SaaS infrastructure
- Multi-user tenancy, teams, roles, or account management
- Custom browser passwords or browser bearer-token storage
- Git-backed session synchronization or session Markdown
- Uploading local code indexes to D1
- Vector search, embeddings, or a semantic search service
- Direct model upstreams other than OpenRouter
- A general analytics suite
- Automatic retention or deletion workflows
- Automatic outcome inference services

## 15. Known Incomplete Work

The implementation priorities are tracked in [`next-steps.md`](next-steps.md).
The largest current gaps are release operations and capture/search lifecycle
hardening.

## 16. Validation

The exact local validation commands for these surfaces are:

```bash
npm --prefix worker test -- src/config.test.ts src/session-titles.test.ts
bun test plugins/pi/ plugins/opencode/
python -m unittest discover -s plugins/hermes -p "test_*.py"
go test ./internal/harness/hooks ./internal/install ./internal/doctor
npm --prefix worker run typecheck
```

Deployment verification is separate and must not invoke
`/v1/chat/completions`, `/v1/messages`, or another paid model route. Use
`/whoami` for connectivity and direct session APIs for lifecycle checks.
