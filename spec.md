# Mimir v2 — Memory Plane

**Mimir remembers.**

Mimir stops being a code indexer and becomes a memory plane: one queryable
surface over everything a developer's agents should remember. No accounts,
no dashboard, no interface. Self-hosted on the user's own Cloudflare account.

---

## 1. Memory types

| Type | What it is | Written by | Store |
|------|-----------|------------|-------|
| `code` | The repo, indexed (v1 behavior, unchanged) | `mimir index` | `.mimir/index.json` (local, v2) |
| `sessions` | Attempts with outcomes; the log cut into episodes | Indexing pass over the log | D1 |
| `log` | Full raw model traffic | The Worker, as a byproduct of proxying | R2 |

The log is not a product. The Worker proxies requests to OpenRouter and
saving them is a side effect. There is no separate proxy app, skill, or
service — deploying Mimir *is* deploying the interceptor.

---

## 2. Architecture

```
harness (opencode / Claude Code / Cursor / anything)
   │  base_url = user's Worker URL
   ▼
┌─────────────────────────────────────────────┐
│  Worker (user's Cloudflare account)         │
│  • OpenAI/Anthropic-compatible proxy        │
│    → forwards to OpenRouter, streams through│
│    → saves per config via waitUntil (zero   │
│      added latency)                         │
│  • Query endpoints (sessions, search, whoami)│
│  • Config endpoints (get/set)               │
└──────┬──────────────────────┬───────────────┘
       ▼                      ▼
      R2                     D1
   raw log            session index + config
   JSONL, date-
   partitioned
```

**Binary** (Go, zero deps) is a client only: `setup`, config sugar, queries,
`mark`. Local state is a pointer file — URL + token — nothing else.

**Ownership:** everything runs in the user's Cloudflare account. Mimir has
no backend, no tenant, nothing of ours in the loop. Single-dev per
deployment; a team is N deployments.

---

## 3. Worker

### 3.1 Proxy endpoints

- `POST /v1/chat/completions` — OpenAI-compatible
- `POST /v1/messages` — Anthropic-compatible

Behavior:

1. Authenticate bearer token.
2. Read config (KV/D1, cached, short TTL).
3. Apply save filters (§6). If excluded, pure pass-through.
4. Forward to OpenRouter (key in Worker secrets). Stream response bytes
   through untouched.
5. `waitUntil`: reassemble the exchange, apply redaction, write one JSONL
   object to R2, upsert session row in D1.

Session correlation: optional `x-mimir-session` header. Harnesses that set
it get exact boundaries. Requests without it are bucketed by heuristic at
index time (§5.1).

### 3.2 Query endpoints

- `GET /whoami` — deployment identity: URL, created date, counts per type
- `GET /sessions` — list; filters: repo, model, outcome, date range
- `GET /sessions/:id` — one episode + trace refs into R2
- `GET /log/:key` — raw log object fetch
- `POST /search` — token-budgeted retrieval across types; params: query,
  types[], budget, filters
- `GET /config` / `PUT /config`

All endpoints: same bearer token. JSON in, JSON out. This HTTP surface is
the canonical API; MCP and CLI are clients of it.

---

## 4. Schemas

### 4.1 R2 — log

```
log/YYYY/MM/DD/<ulid>.json
```

One object per request/response pair:

```json
{
  "id": "ulid",
  "ts": "iso8601",
  "session": "header value or null",
  "model": "anthropic/claude-...",
  "endpoint": "chat|messages",
  "request": { "full body, post-redaction" },
  "response": { "full body, reassembled from stream" },
  "usage": { "prompt_tokens": 0, "completion_tokens": 0 },
  "latency_ms": 0,
  "meta": { "repo": "if derivable", "harness": "if derivable" }
}
```

### 4.2 D1 — sessions

```sql
CREATE TABLE sessions (
  id            TEXT PRIMARY KEY,        -- ulid
  started_at    TEXT NOT NULL,
  ended_at      TEXT,
  boundary      TEXT NOT NULL,           -- 'header' | 'heuristic'
  outcome       TEXT NOT NULL DEFAULT 'unknown',
                -- 'promoted' | 'discarded' | 'abandoned' | 'unknown'
  outcome_src   TEXT,                    -- 'explicit' | 'git' | null
  repo          TEXT,
  model_primary TEXT,
  request_count INTEGER DEFAULT 0,
  tokens_in     INTEGER DEFAULT 0,
  tokens_out    INTEGER DEFAULT 0,
  files         TEXT,                    -- json array, derived
  errors        TEXT,                    -- json array of signatures, derived
  intent        TEXT,                    -- short annotation, optional
  log_refs      TEXT NOT NULL            -- json array of R2 keys
);

CREATE TABLE config (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

CREATE TABLE access_tokens (
  token_hash   TEXT PRIMARY KEY,
  label        TEXT NOT NULL,
  created_at   TEXT NOT NULL,
  last_used_at TEXT,
  revoked_at   TEXT
);
```

Episode fields are mechanically derived (files, errors, counts from the
log; outcome from adapters). A model may annotate `intent` on top of hard
fields — never replace them. No vibes summaries.

### 4.3 Code (unchanged this version)

`.mimir/index.json` stays local, produced by the v1 indexer. The `/search`
endpoint federates to it through the CLI/MCP client when the client is on a
machine with an index. Replatforming code into D1 is v3, contingent on
evidence it's worth it.

---

## 5. Sessions

### 5.1 Boundary

1. **Header** (`x-mimir-session`) — harness-declared. Authoritative.
2. **Heuristic fallback** — same repo + inter-request gap < `session_gap`
   (config, default 15m). Runs at index time, marked `boundary: heuristic`.

If heuristic bucketing proves garbage in practice, it gets demoted to
opt-in; header path is the contract.

### 5.2 Outcomes

Closed enum: `promoted | discarded | abandoned | unknown`.

Adapters (v2 ships two):

- **explicit** — `mimir mark <session> <outcome>`, or the same via
  MCP/HTTP. An agent can mark its own session. Lowest trust, universal.
- **git** — post-hoc pass: session's diff landed in a commit that reached
  a durable branch → `promoted`; branch deleted without merge →
  `discarded`; neither after `abandon_after` (config, default 7d) →
  `abandoned`.

Adapter interface is open: gittrix, CI, and anything else are future
adapters, not core. Each adapter stamps `outcome_src` so consumers can
filter by label quality.

Unlabeled episodes are still stored — retrieval value doesn't require the
training label.

---

## 6. Config

Lives **in the deployment** (D1 `config` table), read by the Worker
per-request. Edited through the plane like any other query — MCP, HTTP,
or CLI sugar (`mimir config set …`). Changes apply on the next request;
no redeploy. An agent can change prefs mid-session.

Keys (v2):

```
save.enabled          true
save.exclude_repos    []          # glob list
save.exclude_models   []
redact.patterns       [builtin]   # applied before R2 write; builtins cover
                                  # common key/token/secret shapes
session.gap_minutes   15
session.abandon_days  7
```

Default posture: save everything, filter by exception. It's the user's
bucket and their bill; storage is ~free.

Local `~/.mimir/config` is a pointer only:

```toml
url   = "https://mimir.<user>.workers.dev"
token = "…"
```

Tokens are independent per machine and D1 stores only their SHA-256 hashes.
`mimir login` uses Cloudflare ownership through Wrangler to discover an
existing deployment and register another machine without rotating existing
credentials or requesting the OpenRouter key again.

---

## 7. Surfaces

Mimir ships **no interface.** It's endpoints.

- **HTTP** — §3.2. Canonical.
- **MCP** — one server, tools mirror HTTP 1:1: `whoami`,
  `sessions_list`, `sessions_get`, `search`, `mark`, `config_get`,
  `config_set`. Any MCP-speaking harness gets everything. No per-harness
  integration code exists anywhere in the project.
- **CLI** — another client. `mimir sessions`, `mimir search`, `mimir mark`,
  `mimir config`. Sugar over HTTP.

TUIs, GUIs, dashboards are consumers other people (including us, elsewhere)
build against the same endpoints. Never part of Mimir.

---

## 8. Setup

Onboarding is a first-class feature, not plumbing. Two paths, one
underlying mechanism.

### 8.1 `mimir setup` (human path)

Wizard, three modes on fresh run (hermes-setup shape):

- **Quick** (`--quick`, zero prompts): detect wrangler auth or run
  `wrangler login` OAuth → deploy Worker + create R2 bucket + D1 schema →
  generate bearer token → prompt for OpenRouter key → write pointer file →
  print per-harness base-URL snippets (opencode, Claude Code, Cursor) →
  verify with a whoami round-trip.
- **Full**: every option surfaced — names, region, config defaults,
  redaction patterns.
- **Minimal**: deploy with `save.enabled=false`; pure proxy until the user
  opts in.

Detects existing state: a v1 `.mimir/index.json` (keep serving it), an
existing deployment (offer reconnect instead of redeploy).

### 8.2 Agent path

The repo ships a `skills/` folder (agentskills.io format):

- **mimir-setup** — teaches an agent to run `mimir setup --quick`, edit
  the harness's own config to point at the Worker (the one thing the CLI
  can't do from outside), and verify.
- **mimir-use** — the one that matters. Teaches the habit: before starting
  work, query sessions for prior attempts touching these files/errors; on
  finish, `mark` the outcome; set `x-mimir-session`. This is the
  behavioral glue MCP alone doesn't provide — MCP exposes tools, the skill
  teaches when to reach for them.

Distribution: `npx skills add cloudboy-jh/mimir` via skills.sh reaches
20+ harnesses. Not a dependency — the skill format is the open standard,
content lives in our repo, and `mimir setup` installs the skills into the
detected harness itself as the primary path. skills.sh is passive
discovery; README documents the plain copy-the-folder method.

---

## 9. Non-goals (v2)

- Code index replatform (stays local; v3 question)
- Multi-user, sharing, token scoping
- Any UI
- gittrix / CI outcome adapters
- Fine-tuning pipeline (the log makes it *possible*; building it is out
  of scope)
- Non-OpenRouter upstreams (adapter seam exists in the Worker; only
  OpenRouter ships)

---

## 10. Build order

1. **Worker proxy + R2 log** — useful alone (one base URL for every
   machine, cost attribution, debugging). Every day it runs accumulates
   corpus.
2. **Session indexing + D1** — boundaries, derivation, explicit adapter.
3. **Query surface** — HTTP + MCP + CLI.
4. **git outcome adapter.**
5. **`mimir setup`** — wizard, both skills, harness snippets.
6. **v1 code index federation** through the client.

---

## 11. Open questions

- Heuristic session bucketing quality — ship behind a flag, evaluate on
  real traffic before making it default.
- Redaction builtin set — needs a concrete pattern list before the Worker
  writes anything to R2.
- `/search` ranking across types — v1 Mimir's ranking applies to code;
  sessions need their own relevance signals (recency, file overlap,
  error-signature match). Simplest viable: filter + recency, rank later.
# Mimir v2

Mimir is a self-hosted Cloudflare Worker memory plane. The Worker proxies OpenAI- and Anthropic-compatible traffic to OpenRouter, archives full redacted exchanges in R2, and indexes sessions/configuration in D1.

## Stores

- `.mimir/index.json`: local code index, produced by the retained Go indexer.
- D1: sessions, exchanges, outcomes, searchable excerpts, configuration, and references to raw logs.
- R2: complete redacted request/response JSON objects at `log/YYYY/MM/DD/<ulid>.json`.

## Boundaries

`x-mimir-session` is authoritative. Requests without the header are grouped by repository and a configurable fifteen-minute gap.

## Surfaces

The Worker HTTP API is canonical. The Go CLI and stdio MCP server are clients of that API. The local pointer config contains only the Worker URL and bearer token.

## Non-goals

No Git-backed session sync, compatibility layer, UI, multi-user tenancy, or code-index migration to D1.
