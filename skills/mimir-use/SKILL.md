---
name: mimir-use
description: Search Mimir for past agent sessions, commits, devices and activity when prior work would help.
---

# Mimir Use

Mimir records every agent session automatically. You never need to record,
mark, verify or end anything. Use Mimir only to read past work.

Search when prior work would help: a recurring error, an unfamiliar part of the
repository, "what did I do on X", or a request for activity charts. Do not
narrate routine memory access.

## Tools

When the harness exposes Mimir tools (MCP server `mimir`, or native
`mimir_*` tools in OpenCode and Hermes), use them. Otherwise run the CLI with
`--json`. Both return the same data.

| Need | Tool | CLI |
| --- | --- | --- |
| Find past work by text | `search` | `mimir search <query> [--repo R] [--from 7d] --json` |
| List sessions | `sessions` | `mimir sessions [--repo R] [--harness H] [--from 7d] [--git-status pushed] --json` |
| One session: summary, files, errors, commits | `session` | `mimir session <id> --json` |
| What was said and done | `turns` | `mimir turns <id> [--limit 8] [--cursor C] --json` |
| Counts for charts | `stats` | `mimir stats --by repo\|harness\|device\|model\|day [--from 7d] --json` |
| One day grouped into workstreams | `day` | `mimir day [YYYY-MM-DD] [--tz Zone] --json` |
| Machines and tailnet status | `devices` | `mimir devices --json` |
| Captured commits | `commits` | `mimir commits [--repo R] [--from 7d] --json` |
| A commit's patch | `commit_patch` | — |

Dates accept RFC3339, `YYYY-MM-DD`, or relative `24h` / `7d`.

Typical paths:

- "How did I fix this before?" → `search` with the error text, then `session`
  on the best hit. Read `turns` only when the summary is not enough.
- "Chart my sessions this week by repo" → one `stats` call with
  `group_by: repo`, then draw the chart or table from `groups`.
- "What happened today?" → one `day` call.

## Reading results

- `git_status` is `none`, `committed` or `pushed`; it is derived from captured
  commits, not from anyone's judgment.
- `capture.status` (`saved`, `pending`, `partial`, `failed`, `empty`) says
  whether Mimir stored the exchanges. It says nothing about whether the work
  succeeded.
- Summaries are deterministic reconstructions of saved evidence. Treat claims
  in them as evidence to check, not proof.

Present results as readable text, tables or charts. Never paste raw JSON to the
user.
