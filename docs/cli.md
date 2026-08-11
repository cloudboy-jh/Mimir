# CLI Contract

The Worker HTTP API is canonical. The Mimir CLI is the primary agent-facing
client for searching, inspecting, and controlling memory.

## Machine-Readable Commands

```bash
mimir whoami --json
mimir list --json
mimir search <query> --json
mimir session get <id> --json
mimir session status <id> --json
mimir session outcome <id> <landed|discarded|abandoned|unresolved> --reason <text> --evidence <json> --json
mimir session end <id> --outcome <value> --reason <text> --evidence <json> --json
mimir config get --json
mimir config set <key> <json-value> --json
mimir access --json
```

JSON mode never prompts. Successful remote commands preserve the canonical
Worker response rather than hiding new fields behind a CLI-specific model.
`session status` and `session end` normalize the receipt while retaining future
Worker fields. `mimir access --json` without an API token returns a structured
manual action with the exact dashboard destinations and does not print an
interactive checklist.

`mimir list` opens a lightweight session browser when both stdin and stdout are
terminals. Redirected output and `--no-interactive` retain the static text
format. Agents and scripts should use `mimir list --json`; JSON mode never emits
terminal control sequences.

`mimir demo` serves the embedded fixture dashboard on a random loopback port and
opens it in the default browser. It does not read connection state or machine
credentials. `--no-open` leaves browser launch to the caller. The command runs
until Ctrl+C and all in-browser changes reset on reload.

`mimir tui` opens the persistent sessions and Pi home surface in the terminal's
alternate screen. It consumes the current terminal dimensions, restores the
original screen on exit, and shows a bounded message below 48x12. `Tab` moves
between session browsing and Pi input. In the session list, `/` filters,
`Enter` opens detail, `o` records an outcome, and `z` expands the agent. In the
Pi input, `/` opens commands and `/theme` opens the Mimir theme picker. The agent
receives the selected session as context and uses private tool adapters for
session listing, detail, search, outcomes, and diagnostics. Run
`mimir doctor --tui` to validate Pi and provider credentials. The centered home
surface keeps recent sessions above the Pi prompt.

Temporary stateful human commands use one top-left-anchored 80x20 application
frame (48x12 minimum). Session browsing, deploy, setup, and login share its
header, scrollable body, and contextual footer. Arrow keys or `j`/`k` scroll,
`g`/`G` jump to either end, `f` resumes following live output, and Ctrl+C
cancels while it is still safe to stop. Install and update always use concise,
line-oriented output. Install protects its receipt-owned reconciliation from
interruption; update accepts Ctrl+C before binary replacement and ignores it
while post-commit reconciliation finishes. JSON output remains machine-readable
and `mimir update --check` stays concise.

The OpenCode integration renders authoritative status and outcome tool results
as compact Mimir receipts in the agent transcript. Agents still consume the
canonical machine APIs; the compact receipt is the human-visible tool result.

Generic errors in JSON mode are written to stderr as:

```json
{"error":"description","exit_code":4}
```

Deployment state errors expose their fields directly so callers do not need to
decode JSON stored inside `error`:

```json
{"state":"deployment_url_missing","message":"run mimir deploy, then rerun mimir login","exit_code":4}
```

## Exit Codes

| Code | Meaning |
| ---: | --- |
| `0` | Success |
| `2` | Invalid invocation |
| `3` | Not connected or unauthorized |
| `4` | Remote API or runtime failure |
| `5` | Local conflict or repair required |
| `6` | Incompatible deployed Worker |

## Command Discovery

`mimir --help` lists normal commands and `mimir help advanced` lists diagnostic
and harness-facing commands. `mimir session <id>` remains an alias for
`mimir session get <id>`.

## Capture And Control

CLI commands inspect and control memory; they do not capture unrelated model
traffic. Capture comes from redirected proxy traffic and harness plugins.
An explicit end can finalize a session, but later activity carrying the same
session ID intentionally reopens it.

Use `mimir session status <id> --json` as the persistence authority. Proxy
responses, scheduled capture headers, and plugin delivery attempts are not
proof that an exchange was saved.
