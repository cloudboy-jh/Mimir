# Churn — New Repo Build Spec

Owner: Jack Horton (`cloudboy-jh`)  
New repo: `github.com/cloudboy-jh/churn`  
Legacy repo: phase current repo out as `churn-1.0` / archived v1-v2 line  
Package: `churn-cli`  
Runtime: Go-first CLI/TUI  
TUI: BentoTUI + Bubble Tea  
License: MIT  
Default branch: `master`

---

## 0. Decision

Start Churn fresh in a new repo instead of incrementally converting the current Bun/Ink codebase.

The current repo has too much historical shape baked in:

- Bun + TypeScript + Ink UI stack
- v1/v2 “scan and hand off findings” product framing
- command routing and UI phases coupled in `src/index.tsx`
- old package/repo metadata from `cloudboyjh1/churn2.0`

The new Churn should be built as the actual 2026 product:

> **Hermes remembers you. Churn remembers your code.**

Churn is no longer primarily a one-shot code analyzer. It is durable, local-first, per-repo code memory that any agent can retrieve from across sessions.

---

## 1. Product definition

Churn is a local-first context engine for AI coding agents.

It indexes a repository into a durable `.churn/` store, keeps that store fresh with git-aware incremental updates, and exposes retrieval surfaces agents can query instead of re-reading the whole codebase every session.

Primary user value:

- agents stop rebooting cold
- repo facts persist between sessions
- context is cheap to refresh
- retrieval is token-budgeted
- everything stays local unless the user chooses a cloud model

Lead with:

- durable code memory
- repo-aware context retrieval
- MCP integration
- local-first privacy
- agent-agnostic workflow

Do **not** lead with:

- dead code linting
- unused import detection
- “AI code review” as the main product
- one-shot report handoff

Those can exist as secondary workflows later.

---

## 2. Stack

### Language/runtime

Use Go for the new repo.

Reasons:

- Churn is now a CLI/TUI/daemon-style tool.
- BentoTUI is Go-native.
- MCP stdio servers are clean in Go.
- Native binaries are cleaner than bundling Bun + JS + a Go TUI.
- Local-first indexing benefits from fast filesystem/git operations.

### TUI

Use BentoTUI pinned, not floating latest.

```bash
go get github.com/cloudboy-jh/bentotui@v0.6.2
```

Use BentoTUI according to its architecture:

- import stable packages directly:
  - `github.com/cloudboy-jh/bentotui/theme`
  - `github.com/cloudboy-jh/bentotui/theme/styles`
  - `github.com/cloudboy-jh/bentotui/registry/rooms`
- copy-and-own bricks into the repo:
  - `card`
  - `bar`
  - `list`
  - `input`
  - `progress`
  - `badge`
  - `dialog`
  - `wordmark`
  - `surface`
  - `tabs` if needed
  - `toast` if needed

Do not build a custom terminal UI framework. Use Bento defaults first.

### Suggested Go deps

Keep dependencies tight.

Core:

- `github.com/cloudboy-jh/bentotui v0.6.2`
- `charm.land/bubbletea/v2`
- `charm.land/bubbles/v2`
- `charm.land/lipgloss/v2`
- `github.com/spf13/cobra` for CLI routing, or stdlib `flag` if command surface stays simple
- `github.com/go-git/go-git/v5` only if shelling to git becomes painful

Prefer invoking `git` with safe args for exact compatibility with installed Git behavior. No shell interpolation. Use `exec.CommandContext` with explicit args.

AI providers can start minimal:

- OpenAI-compatible HTTP client implemented directly with stdlib
- Ollama HTTP client implemented directly with stdlib
- Anthropic/Gemini later if needed

Do not add huge SDKs unless they remove real complexity.

---

## 3. Repo structure

```txt
churn/
├── cmd/
│   └── churn/
│       └── main.go
├── internal/
│   ├── app/                 # Bubble Tea app model, routing, screens
│   ├── bricks/              # copied BentoTUI bricks, app-owned
│   ├── cli/                 # cobra/std command definitions
│   ├── config/              # global + repo config loading
│   ├── context/             # project detection: framework, tools, conventions
│   ├── git/                 # safe git operations
│   ├── indexer/             # durable store build/update logic
│   ├── recall/              # retrieval/ranking/token budgeting
│   ├── mcp/                 # stdio MCP server
│   ├── symbols/             # symbol/export extraction
│   ├── deps/                # dependency graph extraction
│   ├── findings/            # optional AI/static findings
│   ├── models/              # AI provider abstraction
│   ├── ui/                  # TUI layout helpers/theme mapping
│   └── version/             # build version metadata
├── pkg/
│   └── churn/               # small public API only if needed
├── docs/
│   ├── mcp.md
│   ├── store.md
│   ├── recall.md
│   └── migration.md
├── testdata/
│   └── repos/
├── .github/
│   └── workflows/
│       └── ci.yml
├── go.mod
├── go.sum
├── README.md
├── CHANGELOG.md
├── LICENSE
└── goreleaser.yml
```

Keep `internal/` boundaries real. UI should not own indexing logic. Indexing should not import UI.

---

## 4. Command surface

### Primary commands

```bash
churn                         # open BentoTUI home screen
churn tui                     # explicit TUI launch
churn index [--full]          # build/update .churn store
churn status                  # show freshness, size, indexed SHA vs HEAD
churn recall <query>          # retrieve token-budgeted context
churn serve                   # start MCP stdio server
```

### Secondary commands

```bash
churn ask <question>          # optional: answer using indexed context + selected model
churn model                   # configure model provider
churn config                  # show/edit local config via TUI or print JSON
churn doctor                  # check git, store, model config, MCP config
```

### Legacy compatibility commands

Only add these if needed for old users:

```bash
churn analyze                 # alias/wrapper around index + findings
churn run                     # alias/wrapper around analyze
churn pass                    # legacy one-shot handoff, de-emphasized
```

Do not let legacy commands shape the new architecture.

---

## 5. `.churn/` store

Create the store at the repository root.

Default recommendation: `.churn/` is gitignored by default. Users can opt into committing it later.

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
    └── <commit-sha>.json
```

### `index.json`

```json
{
  "schemaVersion": "3.0.0",
  "repo": {
    "root": "/absolute/path/to/repo",
    "remote": "git@github.com:cloudboy-jh/example.git",
    "branch": "master",
    "indexedSha": "abc123",
    "headSha": "abc123",
    "stale": false
  },
  "createdAt": "2026-07-08T00:00:00Z",
  "updatedAt": "2026-07-08T00:00:00Z",
  "configHash": "sha256:...",
  "stats": {
    "files": 120,
    "symbols": 560,
    "deps": 340,
    "findings": 12,
    "bytes": 932000
  }
}
```

### `context.json`

```json
{
  "schemaVersion": "3.0.0",
  "language": "Go",
  "frameworks": ["Bubble Tea"],
  "tooling": ["go", "goreleaser"],
  "packageManagers": [],
  "testCommands": ["go test ./..."],
  "buildCommands": ["go build ./cmd/churn"],
  "conventions": [
    "internal packages for implementation",
    "no shell interpolation",
    "local-first store under .churn"
  ]
}
```

### `map/files.json`

```json
{
  "files": [
    {
      "path": "internal/indexer/index.go",
      "language": "Go",
      "sizeBytes": 4200,
      "contentHash": "sha256:...",
      "lastSha": "abc123",
      "lastTouchedAt": "2026-07-08T00:00:00Z",
      "exports": ["Build", "Update"],
      "imports": ["internal/git", "internal/context"],
      "complexity": 14,
      "summary": "Builds and updates the durable .churn store."
    }
  ]
}
```

### `map/symbols.json`

```json
{
  "symbols": [
    {
      "name": "Build",
      "kind": "function",
      "path": "internal/indexer/index.go",
      "line": 34,
      "signature": "func Build(ctx context.Context, opts Options) (*Result, error)",
      "summary": "Runs a full repository index."
    }
  ]
}
```

### `map/deps.json`

```json
{
  "edges": [
    ["internal/app/model.go", "internal/indexer/index.go"]
  ],
  "orphans": [],
  "unusedPackages": []
}
```

### `findings.json`

```json
{
  "findings": [
    {
      "id": "sha256:...",
      "severity": "HIGH",
      "category": "security",
      "path": "internal/git/git.go",
      "line": 88,
      "message": "Avoid shell interpolation for git command args.",
      "suggestedFix": "Use exec.CommandContext with explicit args."
    }
  ]
}
```

---

## 6. Indexing behavior

### Full index

`churn index --full` should:

1. verify current directory is inside a git repo
2. resolve repo root
3. create `.churn/` if missing
4. detect project context
5. collect trackable files
6. skip ignored, binary, generated, oversized files
7. extract file metadata
8. extract symbols
9. build dependency map
10. optionally run findings pass
11. write store atomically
12. update `index.json` with current HEAD SHA

Use atomic writes:

- write `file.tmp`
- fsync when practical
- rename to final path

Never leave half-written JSON if the process dies.

### Incremental index

`churn index` without `--full` should:

1. load `.churn/index.json`
2. compare `indexedSha` with current `HEAD`
3. if missing store, run full index
4. if config hash changed, run full index
5. get changed files between `indexedSha` and `HEAD`
6. reindex only changed files
7. remove deleted files from maps
8. merge new file/symbol/dependency records
9. update `indexedSha`

Use git commands safely:

```go
exec.CommandContext(ctx, "git", "diff", "--name-status", indexedSha, "HEAD")
```

No `sh -c`. No string-built command execution.

### File limits

Defaults:

- max file size: `100KB`
- skip binary files
- skip generated files:
  - `*.min.js`
  - `*.map`
  - `*.lock` unless package manager info is needed
  - generated protobufs
  - vendored dependencies
  - `node_modules/`
  - `.git/`
  - `.churn/`
  - `dist/`
  - `build/`
  - `coverage/`

These limits can be configurable later. Start conservative.

---

## 7. Recall behavior

`churn recall <query>` is retrieval, not analysis.

It should query the durable store and return the smallest useful context slice.

Inputs:

```bash
churn recall "how does auth work"
churn recall internal/indexer/index.go
churn recall Build
churn recall "MCP server tools" --budget 4000
churn recall "dependency graph" --json
```

Output should include:

- relevant files
- relevant symbols
- dependency edges
- project conventions/context
- current findings when relevant
- freshness warning if store is stale

JSON output shape:

```json
{
  "query": "how does auth work",
  "budget": 4000,
  "stale": false,
  "context": {
    "language": "Go",
    "frameworks": ["Bubble Tea"]
  },
  "files": [],
  "symbols": [],
  "deps": [],
  "findings": [],
  "notes": []
}
```

Initial ranking can be simple:

- exact path match
- exact symbol match
- filename/query token overlap
- symbol/query token overlap
- summary/query token overlap
- dependency-neighbor boost
- finding severity boost

Do not start with embeddings. Add them later only if lexical retrieval is insufficient.

---

## 8. MCP server

`churn serve` starts a stdio MCP server.

Expose tools:

- `churn_recall(query, token_budget?)`
- `churn_context()`
- `churn_symbols(name?)`
- `churn_findings(severity?, path?)`
- `churn_deps(path?)`
- `churn_status()`

MCP tool behavior:

- read `.churn/` store from current repo
- never mutate store unless a future `churn_index` tool is explicitly added
- return compact JSON + human-readable summaries
- include stale warning when `indexedSha != HEAD`

Example MCP config for agents:

```json
{
  "mcpServers": {
    "churn": {
      "command": "churn",
      "args": ["serve"],
      "cwd": "/path/to/repo"
    }
  }
}
```

---

## 9. TUI design

The TUI is not a wrapper around old screens. It is the primary product surface for the new Churn.

Use BentoTUI’s app-shell/detail-view patterns.

### Main screens

Home:

- repo name
- branch
- indexed SHA vs HEAD
- stale/fresh status
- store size
- primary actions

Actions:

- Index repo
- Recall context
- Serve MCP
- Status
- Model/config
- Doctor

Index screen:

- progress by phase
- changed files count
- current file batch
- elapsed time
- final stats

Recall screen:

- input prompt
- results list
- detail pane
- token budget indicator
- stale warning if needed

Status screen:

- indexed SHA
- HEAD SHA
- stale true/false
- files/symbols/deps/findings counts
- `.churn/` disk size
- last indexed time

MCP screen:

- setup instructions
- config snippet
- server status when launched

Config screen:

- model provider
- API key status, never raw key display
- Ollama status
- store options

### Keybinds

Keep it simple:

- `q` quit
- `esc` back
- `enter` select
- `/` recall/search
- `i` index
- `s` status
- `m` MCP setup
- `?` help

### Theme

Churn brand color remains vibrant red/orange.

Map to Bento theme via custom theme or Bento preset override.

Preferred Churn tokens:

```txt
primary:   #ff5656
secondary: #ff8585
text:      #f2e9e4
muted:     #a6adc8
success:   #a6e3a1
info:      #8ab4f8
warning:   #f9e2af
error:     #f38ba8
background:#11111b
surface:   #181825
```

Use BentoTUI `theme.BaseTheme` to register a `churn` theme.

---

## 10. AI/model layer

AI is optional for the core product.

Core indexing and recall must work offline without any API key.

Use models only for:

- optional file summaries
- optional findings
- optional natural-language answer synthesis over recalled context

Model providers:

M1:

- Ollama
- OpenAI-compatible endpoint

Later:

- Anthropic
- Gemini
- OpenRouter

Config location:

```txt
~/.config/churn/config.json
```

Example:

```json
{
  "defaultModel": {
    "provider": "ollama",
    "model": "qwen2.5-coder:7b",
    "baseURL": "http://localhost:11434"
  },
  "openai": {
    "baseURL": "https://api.openai.com/v1"
  }
}
```

Store API keys securely where practical. If using config file initially, avoid printing secrets and set restrictive permissions.

---

## 11. Migration from old repo

The old repo should become legacy.

Recommended path:

1. Rename/archive old repo as `churn-1.0` or `churn-legacy`.
2. Keep old npm package history intact.
3. Create new repo at `cloudboy-jh/churn`.
4. Put this spec in the new repo as `docs/build-spec.md` or `SPEC.md`.
5. Build fresh Go codebase.
6. Publish new binaries as Churn 3.x.
7. Decide npm strategy later:
   - keep `churn-cli` package as a native binary installer, or
   - move distribution to GitHub Releases/Homebrew/Scoop first.

Do not copy old Ink components.

Can reference old repo for:

- command names
- theme colors
- model provider UX
- generated report ideas
- lessons learned around file limits and command injection

Do not port old architecture wholesale.

---

## 12. Release/distribution

Use GoReleaser.

Targets:

- macOS amd64
- macOS arm64
- Linux amd64
- Linux arm64
- Windows amd64
- Windows arm64 optional

Distribution channels:

M1:

- GitHub Releases

M2:

- Homebrew tap
- Scoop manifest

M3:

- npm `churn-cli` wrapper that downloads/execs native binary

The npm package should not ship the entire Go source. It should install the right binary for platform/arch.

---

## 13. Milestones

### M0 — Repo foundation

Acceptance:

- new repo created
- Go module initialized
- CI runs `go test ./...`
- GoReleaser config stubbed
- BentoTUI pinned
- Churn custom theme registered
- basic `churn --version` works

### M1 — BentoTUI shell

Acceptance:

- `churn` opens full-screen TUI
- home screen shows repo detection
- status card shows branch + HEAD SHA
- keybind footer works
- no indexing yet required

### M2 — Durable store full index

Acceptance:

- `churn index --full` creates `.churn/`
- writes `index.json`, `context.json`, `map/files.json`, `map/symbols.json`, `map/deps.json`
- `churn status` shows indexed SHA == HEAD
- TUI index screen can run full index

### M3 — Incremental index

Acceptance:

- edit file, run `churn index`
- only changed/deleted files are processed
- store merges updates correctly
- config hash invalidation forces full reindex

### M4 — Recall

Acceptance:

- `churn recall "query"` returns relevant context, not the whole store
- supports `--json`
- supports `--budget`
- TUI recall screen works

### M5 — MCP server

Acceptance:

- `churn serve` starts MCP stdio server
- exposes recall/context/symbols/findings/deps/status tools
- works in Claude/Cursor-style MCP config
- docs include exact config snippet

### M6 — Optional AI findings / ask

Acceptance:

- Ollama works offline
- OpenAI-compatible endpoint works when configured
- `churn ask` uses recall first, then model synthesis
- no model required for index/status/recall

### M7 — Docs and 3.0 release

Acceptance:

- README leads with durable code memory
- docs explain `.churn/` store
- docs explain MCP setup
- migration note from old Churn exists
- version `3.0.0`
- release binaries published

---

## 14. Acceptance criteria for 3.0.0

- `churn` launches BentoTUI.
- `churn index --full` builds a valid `.churn/` store in a real repo.
- `churn status` reports indexed SHA, HEAD SHA, stale status, store stats.
- Editing a file then running `churn index` reindexes only changed files.
- `churn recall "how does X work"` returns a relevant token-budgeted context slice.
- `churn serve` exposes MCP tools over stdio.
- At least one real MCP-capable agent can call `churn_recall` successfully.
- Core functionality works offline with no API key.
- No telemetry.
- No account/login.
- No cloud backend.
- Git commands use safe arg arrays, never shell interpolation.
- Windows paths work.
- Release artifacts exist for macOS/Linux/Windows.

---

## 15. Non-goals

Do not build these in 3.0:

- hosted/cloud sync
- team-shared memory
- web dashboard
- vector DB dependency
- full semantic embeddings pipeline
- SaaS auth
- old Ink UI compatibility
- old report-first workflow as primary UX

---

## 16. Engineering rules

- Keep the engine independent of the TUI.
- Keep MCP independent of the TUI.
- Use atomic writes for store files.
- Keep JSON schemas versioned.
- No command injection footguns.
- No telemetry.
- No hidden network calls.
- Default to offline behavior.
- Prefer lexical retrieval before embeddings.
- Avoid big dependencies unless they delete real complexity.
- Keep master releasable after each milestone.

---

## 17. First implementation prompt

Use this to start the new repo:

```txt
Build the new Churn repo from scratch as a Go-first local code memory CLI/TUI.

Create a Go module for github.com/cloudboy-jh/churn. Use BentoTUI v0.6.2 for the TUI, with a custom Churn theme based on #ff5656. Create a basic CLI with commands: churn, churn tui, churn index --full, churn status, churn recall, churn serve, churn doctor. For the first milestone, implement repo detection, version output, the BentoTUI home screen, and a status screen that shows repo root, branch, HEAD SHA, and whether .churn/index.json exists. Do not implement full indexing yet. Keep indexing interfaces stubbed behind internal/indexer. Keep UI separate from engine packages. Use safe git execution with exec.CommandContext and explicit args only. Add go test ./... CI.
```

---

## 18. Legacy repo phase-out note

The existing Bun/TypeScript/Ink repo should be treated as the v1/v2 historical implementation.

Recommended final README note for old repo:

```md
# Churn Legacy

This repository contains the original Bun/Ink implementation of Churn.

Active development has moved to github.com/cloudboy-jh/churn, a Go + BentoTUI rewrite focused on durable local code memory, MCP retrieval, and agent-agnostic context.

This repo is maintained only for historical reference and old v1/v2 users.
```
