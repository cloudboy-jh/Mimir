# CLI Contract

The Worker HTTP API is canonical. The `mimir` CLI installs and operates Mimir,
reads memory through the query API, and bridges MCP for harnesses. It never
captures unrelated model traffic; capture comes from the proxy and harness
integrations.

`mimir --help` lists normal commands and `mimir help advanced` lists diagnostic
and integration commands.

## Query Commands

Every read command calls `/api/*` (see [Spec §4.3](Spec.md#43-query-api)) and
accepts `--json`, which prints the Worker response unchanged. Human output is a
compact table or line.

```text
mimir sessions [--q text] [--repo name] [--harness id] [--device id]
               [--state active|inactive] [--git-status none|committed|pushed]
               [--from date] [--to date] [--limit n] [--cursor c] [--json]
mimir session <id> [--json]
mimir session status <id> [--json]
mimir session end <id> [--json]
mimir turns <id> [--limit n] [--cursor c] [--order asc|desc] [--json]
mimir search <query> [--repo name] [--from date] [--to date] [--limit n] [--json]
mimir stats --by <repo|harness|device|model|day> [--from date] [--to date] [--tz zone] [--json]
mimir day [YYYY-MM-DD] [--tz zone] [--json]
mimir commits [--repo name] [--ref ref] [--q text] [--session id]
              [--from date] [--to date] [--limit n] [--cursor c] [--json]
mimir devices [--json]
```

| Command | API | Output |
| --- | --- | --- |
| `sessions` | `GET /api/sessions` | Root sessions, newest activity first: ID, activity, state, Git status, requests, repo, harness, title |
| `session <id>` | `GET /api/sessions/:id` | Session with summary, commits, files, errors, supporting sessions |
| `session status <id>` | `GET /api/sessions/:id/receipt` | The receipt line, plus the dashboard link when available |
| `session end <id>` | `POST /sessions/:id/end`, then the receipt | `Session ended · <receipt line>` |
| `turns <id>` | `GET /api/sessions/:id/turns` | Conversation turns, oldest first by default |
| `search <query>` | `GET /api/search` | Matching sessions with ranked snippets |
| `stats --by <group>` | `GET /api/stats` | Sessions, requests, tokens, and commits per group; default last 7 days |
| `day [date]` | `GET /api/days/:date` | One day grouped into workstreams; defaults to today |
| `commits` | `GET /api/commits` | Captured commits with pushed state and sessions |
| `devices` | `GET /api/devices` | Devices with platform, sessions, and tailnet status |

Dates accept RFC3339, `YYYY-MM-DD` (UTC day; `--to` is inclusive), or
relative `24h` / `7d`. `stats` and `day` default `--tz` to the local time zone.
Paged human output ends with `More: <command> --cursor <c>` when another page
exists.

The receipt is computed from stored state and returned immediately. Capture
status (`saved`, `pending`, `partial`, `failed`, `empty`) says whether Mimir
stored the exchanges; `git_status` (`none`, `committed`, `pushed`) is derived
from captured commits. Neither is a judgment of whether the work succeeded.
Ending is idempotent, and later activity with the same session ID reopens the
session.

## Set Up And Operate

```text
mimir setup [--account-id <id>] [--url <url>] [--json]
mimir login [--account-id <id>] [--url <url>] [--json]
mimir install --harness <id|all> [--json]
mimir harness [--json]
mimir enable|disable <pi|opencode|hermes>
mimir deploy [--account-id <id>] [--json]
mimir access [--token <api-token> --email <address>] [--aud <aud> --team-domain <domain>] [--json]
mimir dashboard
mimir import [harness] [session-id...]
mimir backfill [harness] [--since 7d]
mimir doctor [--json]
mimir update [--check] [--json]
mimir uninstall [--keep-binary] [--json]
mimir version [--json]
```

Harness IDs are `opencode`, `pi`, `oh-my-pi`, `hermes`, `claude-code`,
`codex`, and `cursor`. `mimir harness` shows each integration, its managed
files, activation action, and any MCP snippet to add by hand.
`mimir enable|disable` are shortcuts for Pi, OpenCode, and Hermes;
`mimir harness enable|disable <id>` manages any harness. Disable removes only
managed files whose bytes still match `$MIMIR_HOME/managed.json`.

`mimir update --check` reports whether a newer release exists; `--json` is
accepted only with `--check`. `mimir update` reruns the official installer.
Neither deploys the Worker; run `mimir deploy` afterwards.

`mimir dashboard` opens the deployment root, which lands on Day.

`mimir access --json` without an API token returns a structured manual action
with the exact Access destinations instead of an interactive checklist.

## Local Import And Backfill

`mimir import` merges local harness history into Worker session records:

```text
mimir import
mimir import <opencode|pi|oh-my-pi>
mimir import <opencode|pi|oh-my-pi> <id>... [--yes] [--json]
mimir import list [opencode|pi|oh-my-pi] [--json]
mimir import inspect <opencode|pi|oh-my-pi> <id> [--json]
```

With no harness, an interactive terminal asks for one. With a harness but no
IDs, it lists up to 20 local candidates to choose from. `--yes` accepts the
given IDs without confirmation. `list` and `inspect` are local and read-only
and need no Worker connection. In a non-TTY or with `--json`, importing
requires a harness, at least one ID, and `--yes`. Oh My Pi import restores
supporting-session parent links only.

`mimir backfill [harness] [--since 7d]` repairs gaps. Omitting the harness
scans every supported source. `--since` accepts a positive duration such as
`48h`, a day shorthand such as `7d`, or an RFC3339 timestamp. Scripts, pipes,
and JSON mode must pass `--all --yes`:

```bash
mimir backfill [opencode|pi|oh-my-pi] [--since 7d] --all --yes --json
```

OpenCode candidates come from the installed `opencode` CLI's `session list`
and `export` commands. Pi candidates are `.jsonl` sessions under
`$PI_CODING_AGENT_DIR/sessions`, falling back to `~/.pi/agent/sessions`.
Neither adapter imports OpenRouter turns: proxy exchanges stay canonical.

Import and backfill are deterministic merges. Source session and message
identity map to stable Mimir IDs, and the Worker reports an existing exchange
as a duplicate, so reruns fill gaps without duplicating saved exchanges. When
the source records a checkout directory, import also captures matching Git
commits from the session window with their pushed state. An identical
`(session, commit)` is a duplicate; different content for the same key is a
conflict and is never overwritten. Hermes history is not an import source.

## Advanced Commands

```text
mimir whoami [--json]
mimir connection
mimir reconcile
mimir config get [--json]
mimir config set <key> <json-value> [--json]
mimir harness <enable|disable> <id> [--json]
mimir mcp
mimir _hook git
mimir _hook <claude-code|codex|cursor>
```

- `whoami` verifies machine authentication and Worker capabilities.
- `connection` prints the [connection manifest](../skills/mimir-setup/references/connection-manifest.md).
- `reconcile` runs a bounded D1/R2 capture reconciliation pass.
- `mcp` is the stdio MCP bridge harness configurations launch. It reads
  newline-delimited JSON-RPC on stdin, POSTs each message to `<url>/mcp` with
  the machine token, and writes each JSON response as one stdout line (nothing
  for notifications).
- `_hook git` captures commits. It reads one JSON object on stdin:
  `{ session_id, cwd, harness, repo?, shas?, tool_output?, refresh? }`. SHAs
  come from `shas` and from `git commit` confirmations (`[branch sha] subject`)
  in `tool_output`. Each commit's redacted patch (bounded at 4 MiB) is posted
  with `pushed` set when `git branch -r --contains <sha>` is non-empty.
  `refresh: true` also re-checks the session's saved unpushed commits and
  re-posts those now on a remote. It prints
  `{ "captured": n, "pushed": n, "errors": [...] }`, exits 0 unless the input
  is invalid, and never prompts.
- `_hook <claude-code|codex|cursor>` is the hook adapter those harnesses'
  managed hook files invoke.

Plugins locate the binary through `MIMIR_BIN`, then `executable=` in
`$MIMIR_HOME/config`, then `mimir` on `PATH`.

## Errors And Exit Codes

JSON mode never prompts. Generic errors in JSON mode are written to stderr as:

```json
{"error":"description","exit_code":4}
```

Deployment state errors expose their fields directly:

```json
{"state":"deployment_url_missing","message":"run mimir deploy, then rerun mimir login","exit_code":4}
```

| Code | Meaning |
| ---: | --- |
| `0` | Success |
| `2` | Invalid invocation |
| `3` | Not connected or unauthorized |
| `4` | Remote API or runtime failure |
| `5` | Local conflict or repair required |
| `6` | Incompatible deployed Worker; run `mimir deploy` |
