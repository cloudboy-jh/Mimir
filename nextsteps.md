# Next Steps

Churn is currently at the foundation + TUI-home milestone from the Go-first 3.0 spec.

## Where we are

- Go module initialized as `github.com/cloudboy-jh/churn`.
- CLI entrypoint exists at `cmd/churn`.
- Safe git layer exists under `internal/git` using `exec.CommandContext(ctx, "git", args...)` only.
- CLI surface is wired with Cobra:
  - `churn`
  - `churn tui`
  - `churn status`
  - `churn doctor`
  - `churn index [--full]`
  - `churn recall <query>`
  - `churn serve`
  - `churn config`
  - `churn model`
- BentoTUI/Bubble Tea shell exists under `internal/app`.
- TUI direction is now intentionally minimal: a sexy homescreen, not a full workspace app.
- Churn theme is registered under `internal/ui` using the red/orange brand direction.
- README now frames the product as durable repo memory and includes the stack narrative:
  - `gittrix` routes writes
  - `churn` supplies durable memory
  - `glib-code` sandboxes execution
  - human approves promotion
- CI runs `go test ./...`.
- GoReleaser config is stubbed.

## Next implementation phases

### 1. Durable `.churn/` store

Implement the first real full-index path.

- Create `.churn/` at repo root.
- Write atomically:
  - `.churn/index.json`
  - `.churn/context.json`
  - `.churn/map/files.json`
  - `.churn/map/symbols.json`
  - `.churn/map/deps.json`
  - `.churn/findings.json`
- Add internal store package for schema structs and atomic JSON writes.
- Keep `.churn/` gitignored by default.

### 2. Full indexer

Turn `churn index --full` from scaffold into real indexing.

- Verify current directory is inside a git repo.
- Resolve repo root, branch, HEAD SHA, remote.
- Collect trackable files.
- Skip binary/generated/oversized files.
- Extract file metadata:
  - path
  - language
  - size
  - content hash
  - last SHA
- Add first-pass Go symbol extraction.
- Write store with indexed SHA == HEAD.

### 3. Incremental indexer

Implement `churn index` without `--full`.

- Load `.churn/index.json`.
- Compare indexed SHA to HEAD.
- Use `git diff --name-status <indexedSha> HEAD`.
- Reindex changed files only.
- Remove deleted files from maps.
- Force full reindex when config/schema hash changes.

### 4. Recall engine

Turn `churn recall <query>` into real retrieval.

- Load `.churn/` store.
- Rank with lexical scoring first:
  - exact path match
  - exact symbol match
  - filename/query overlap
  - symbol/query overlap
  - summary/query overlap
  - dependency-neighbor boost
  - finding severity boost
- Respect `--budget`.
- Support stable `--json` output.
- Warn when store is stale.

### 5. MCP server

Turn `churn serve` into a real stdio MCP server.

- Expose read-only tools:
  - `churn_recall`
  - `churn_context`
  - `churn_symbols`
  - `churn_findings`
  - `churn_deps`
  - `churn_status`
- Never mutate the store from MCP in 3.0.
- Include exact config snippets for opencode, Claude Code, Cursor, and Gemini CLI.

### 6. Polish the TUI after the engine exists

Keep the TUI as a minimal homescreen.

- Show real store stats once indexing exists.
- Show fresh/stale state from actual `.churn/index.json`.
- Show MCP-ready status when the store exists.
- Avoid turning it into a giant app shell; CLI/MCP are the real surfaces.

## Hard constraints

- No telemetry.
- No login/account.
- No cloud backend.
- No hidden network calls.
- No shell interpolation.
- No embeddings/vector DB in 3.0.
- No old Ink UI port.
- Windows paths must keep working.
- Engine, TUI, and MCP stay independent.
