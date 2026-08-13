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

`mimir list` always emits bounded static text. Agents and scripts should use
`mimir list --json`; the installed skill instructs agents to parse that output
and present readable results rather than exposing raw JSON.

`mimir demo` serves the embedded fixture dashboard on a random loopback port and
opens it in the default browser. It does not read connection state or machine
credentials. `--no-open` leaves browser launch to the caller. The command runs
until Ctrl+C and all in-browser changes reset on reload.

Mimir has no separate terminal application. The dashboard is the visual browser, and
installed agents query the machine-readable CLI directly. Setup and login may
use bounded operational progress and secure prompts; install, update, deploy,
and ordinary human output remain concise and line-oriented. JSON output stays
machine-readable and `mimir update --check` stays concise.

The Pi extension routes OpenRouter requests through Mimir with exact Pi session
metadata and uploads bounded reconstructed exchanges for direct providers. The
OpenCode integration renders authoritative status and outcome tool results as
compact Mimir receipts in the agent transcript. Agents still consume canonical
machine APIs; human-visible answers are formatted by the harness.

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
