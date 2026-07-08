# Churn Product Spec

## Product

Churn is durable local code memory for AI coding agents.

Agents should not wake up cold every session, re-read the same repository, rebuild the same mental model, and waste context budget relearning old facts. Churn indexes a repo once, keeps that memory fresh with git-aware updates, and exposes small, useful context slices when agents need them.

Positioning:

> Hermes remembers you. Churn remembers your code.

## What Churn is

- A local-first code memory layer.
- A repo-aware context retrieval system.
- A CLI/MCP utility agents can query.
- A minimal TUI homescreen for repo memory status.
- An agent-agnostic tool, not a single-agent workflow.

## What Churn is not

- Not a hosted SaaS product.
- Not a web dashboard.
- Not a generic linter.
- Not primarily an AI code reviewer.
- Not a full workspace application like glib.
- Not an old Ink/Bun app port.

## Core idea

Every repository gets a local `.churn/` store.

That store contains the durable facts agents usually rediscover manually:

- repo identity and freshness
- project language/framework/tooling
- file map
- symbol map
- dependency edges
- findings when available
- summaries when available
- current indexed commit SHA

The store is cheap to refresh because Churn uses git history and diffs to update only changed files after the first full index.

## User value

- Agents stop rebooting cold.
- Repo facts persist between sessions.
- Context retrieval is cheaper than full repo rereads.
- Users keep control because the store is local.
- Teams can plug any MCP-capable agent into the same repo memory.
- The tool works offline unless the user explicitly configures a model.

## Primary surfaces

### CLI

The CLI is the main user/admin surface.

Core commands:

```bash
churn index [--full]
churn status
churn recall <query>
churn serve
churn doctor
```

### MCP

MCP is the main agent surface.

Agents should be able to ask Churn for context instead of grepping or reading the entire repo.

Planned tools:

- `churn_recall`
- `churn_context`
- `churn_symbols`
- `churn_findings`
- `churn_deps`
- `churn_status`

### TUI

The TUI should stay minimal.

It is a polished home/status screen, not a giant application shell. It should communicate:

- what repo Churn sees
- whether repo memory exists
- whether memory is fresh/stale
- which commands matter next
- how Churn fits the agent-tooling stack

## Product stack narrative

Churn sits inside Jack's 2026 agent tooling loop:

```txt
gittrix routes agent writes into ephemeral workspaces
        ↓
churn supplies durable repo memory those agents can read
        ↓
glib-code sandboxes execution
        ↓
human approves promotion
```

Churn owns the code-memory layer only. It should not absorb gittrix's write routing or glib-code's sandbox responsibilities.

## Experience principles

- Local-first by default.
- No telemetry.
- No account.
- No hidden network calls.
- Fast before clever.
- Lexical retrieval before embeddings.
- Useful small context slices over giant dumps.
- CLI/MCP first, TUI second.
- Minimal UI, premium feel.
- Engine, TUI, and MCP stay independent.

## Retrieval behavior

`churn recall <query>` should return the smallest useful slice of repo memory.

Examples:

```bash
churn recall "how does auth work"
churn recall internal/indexer/index.go
churn recall Build
churn recall "MCP server tools" --budget 4000
churn recall "dependency graph" --json
```

Initial ranking should be simple and explainable:

- exact path match
- exact symbol match
- filename/query token overlap
- symbol/query token overlap
- summary/query token overlap
- dependency-neighbor boost
- finding severity boost

Embeddings are out of scope until lexical retrieval proves insufficient.

## Store behavior

The `.churn/` store should be gitignored by default.

Expected shape:

```txt
.churn/
├── index.json
├── context.json
├── map/
│   ├── files.json
│   ├── deps.json
│   └── symbols.json
├── findings.json
└── history/
```

Store writes must be atomic. Churn should never leave half-written JSON if a process dies.

## Success criteria for 3.0

- Churn builds a valid local `.churn/` store.
- Churn can incrementally refresh that store from git diffs.
- Churn can recall relevant context from that store.
- Churn can serve recall/context/status over MCP.
- At least one MCP-capable agent can call `churn_recall` successfully.
- Core functionality works with no API key.
- Windows, macOS, and Linux binaries are available.

## Non-goals for 3.0

- Cloud sync.
- Team-shared hosted memory.
- Web dashboard.
- SaaS auth.
- Vector DB dependency.
- Full embeddings pipeline.
- Full AI code review product.
- Legacy Ink UI compatibility.
