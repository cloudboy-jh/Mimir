# Git artifacts and outcome evidence by harness

The exact session ID, not a recent commit or the last active session, is the
join key. Commit hooks only attempt artifact delivery. A successful hook, a
push, or an outcome event cannot prove that a redacted patch reached R2. The
canonical check is `mimir session get <exact-id> --json`: find each SHA in
`git_artifacts` with `capture_status: "saved"` and `patch_sha256`, then check
the latest `outcome_events` entry and its `evidence_json`. The bundled
`mimir-use` skill specifies the repair and explicit-end sequence.

| Harness | Exact identity and commit signal | Artifact path | Outcome path / limitation |
| --- | --- | --- | --- |
| Pi | Host session ID exported as `MIMIR_SESSION_ID` (`PI_SESSION_ID` fallback); successful tool result's Git SHA, checked against commits since that turn's baseline | Extension uploads bounded redacted patches; exact-session CLI capture repairs misses | CLI; no native outcome tool. Automatic capture needs recognizable output. |
| Oh My Pi | Session manager ID exported as `MIMIR_SESSION_ID`; successful tool result's Git SHA, checked against the turn's baseline | Extension upload; exact-session CLI repair | CLI; no native outcome tool. Subsessions must not be conflated with the root. |
| OpenCode | Tool hook's session ID and tool-call ID; Git confirmation SHA and pre-tool baseline | Plugin upload; exact-session CLI repair | Native `mimir_session_outcome` / `mimir_session_status` use current context. Git HEAD alone does not establish that the session made a commit. |
| Hermes | Tool hooks' session ID and tool-call ID, pre-tool HEAD and Git output SHA | Plugin upload; exact-session CLI repair | CLI; plugin does not assert a landed outcome on commit. Hook delivery can be interrupted by process exit. |
| Claude Code | Hook payload `session_id`; `PostToolUse` Bash/PowerShell command and Git confirmation SHA | Receipt-owned hook calls `mimir _hook`; exact-session CLI repair | CLI; agent shell is not guaranteed to receive the hook's session ID. Other execution tools or unrecognized shell command shapes can be missed. |
| Codex | Hook payload `session_id`; `PostToolUse` Bash command and Git confirmation SHA | Receipt-owned hook; exact-session CLI repair | CLI; plugin hooks must be trusted/enabled. Shell session ID is not guaranteed. |
| Cursor | Hook payload `conversation_id` (or `session_id`); `afterShellExecution` command and Git confirmation SHA | Receipt-owned hook; exact-session CLI repair | CLI; shell session ID and exit status are not guaranteed by hook API. Ambiguous working directories are rejected. |

Each automatic capture path requires a matching successful tool event, an
unambiguous checkout, and a collectible bounded patch. Commits made outside
those hooks, shell wrappers the parser cannot attribute, failed or truncated
delivery, and abrupt process termination require explicit CLI capture with the
exact ID. If the agent cannot establish that ID unambiguously, it must leave
the artifact and outcome untouched rather than select a session by recency.
Uncommitted work has no commit artifact; its outcome can still carry truthful
non-Git evidence. Neither commit nor push nor session end happens automatically
as part of the skill's workflow.
