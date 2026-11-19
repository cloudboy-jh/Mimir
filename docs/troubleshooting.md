# Troubleshooting

Start with:

```bash
mimir doctor --json
```

Doctor reports each failed check, its evidence, and an exact repair command or
activation action. Do not treat a warning or an unloaded integration as
healthy, and do not invent a broader repair than the one reported.

## Common Doctor Repairs

| Failure | Repair reported by doctor |
| --- | --- |
| Local connection or connection manifest is invalid | `mimir login` |
| Worker API, capability, harness-load endpoint, or bundle is stale | `mimir deploy` |
| Managed file is missing or outdated | `mimir install` |
| Managed file is modified or conflicting | Review the preserved file; remove or restore it, then run `mimir install` |
| MCP configuration could not be written (existing `~/.cursor/mcp.json`) | Add the shown `mimir` server entry to that file |
| Pi or Oh My Pi installed but not loaded | Restart Pi or Oh My Pi |
| OpenCode installed but not loaded | Restart OpenCode |
| Hermes installed but not loaded | Restart Hermes |
| Claude Code installed but not loaded | Run `/reload-plugins` or restart Claude Code |
| Codex installed but not loaded | Review and trust the Mimir plugin hooks in `/hooks`, then restart Codex |
| Cursor installed but not loaded | Open or continue a Cursor agent session |
| Hermes plugin is disabled | `hermes plugins enable mimir` |
| Hermes managed OpenRouter route is stale | `mimir update` |
| Hermes OpenRouter credential is unavailable | Configure Hermes OpenRouter authentication |
| Tailnet report failed | Check `tailscale status`, then run `mimir doctor` again |

Run doctor again after the repair. Deployment verification uses `/whoami` and
the query API, never a paid model endpoint.

## Setup And Login States

### `cloudflare_auth_required`

Rerun the original command without `--json` and complete Wrangler's browser
approval. JSON mode cannot complete interactive authentication.

### `openrouter_key_required`

Set `OPENROUTER_API_KEY` in the process environment or rerun `mimir setup`
interactively and enter it at the masked prompt. Never pass the key as a
command argument.

### `mimir_token_required`

`mimir setup --url <url> --json` needs `MIMIR_TOKEN`. Set it in the environment
or rerun interactively for a secure prompt.

### `deployment_missing`

No Mimir D1 database was found in the selected Cloudflare account. Run
`mimir setup` for a new deployment. If the deployment is in another account,
authenticate Wrangler to that account and rerun `mimir login`.

### `deployment_url_missing`

The database exists but does not contain `deployment.url`. Run `mimir deploy`,
then rerun `mimir login`. If Wrangler cannot recover the workers.dev URL, find
it in Cloudflare and run `mimir login --url <url>`.

## Commands Exit With Code 6

The deployed Worker lacks a capability this CLI needs, such as the query API.
Run `mimir deploy` from the current binary.

## Installation Reports `action_required`

Mimir preserved a conflicting, modified, or unsafe path. Inspect the reported
file. Remove it only when it is disposable, or restore the exact Mimir version,
then run `mimir install` again. Mimir never overwrites it to clear the warning.

## Agent Has No Mimir Tools

- Claude Code and Codex: confirm `mimir harness` shows the integration as
  active and that the plugin's MCP file exists; reload or restart the harness.
- Cursor: if `~/.cursor/mcp.json` existed before install, add the `mimir`
  entry shown by `mimir harness` or doctor.
- OpenCode and Hermes: the `mimir_*` tools load with the plugin; restart the
  harness after install or update.
- Every surface needs a working connection. `mimir whoami` must succeed, and
  `mimir mcp` must be able to start from the path in the MCP configuration.

## Dashboard Access Fails

The Access application must protect exactly `/dashboard/auth` and
`/dashboard/api/*`. Protecting the bare host or `/login` blocks machine routes
or the browser handoff.

`mimir access` refuses to rewrite Bypass, permissive, additional, or
conflicting policies. Resolve them in Cloudflare, then rerun the command.

## OpenCode Capture Is Missing

Run doctor, apply the exact repair, and restart OpenCode; it does not hot-reload
plugins. Redirected OpenRouter traffic is captured at the proxy; direct-provider
capture depends on the loaded plugin.

## Hermes Capture Is Missing

Run doctor. Enable the plugin when instructed, verify Hermes OpenRouter
authentication, and restart Hermes after route or plugin changes. A Worker
missing Hermes routes needs `mimir deploy`. Do not create a custom Hermes
provider.

For direct providers, check the session receipt, not plugin health or
heartbeats. `Saved` requires a persisted exchange. Failures reported to Mimir
appear in the capture status; an offline plugin cannot report its local
failures until it reconnects.

## Commits Are Missing Or Not Marked Pushed

Commits are captured when a shell tool prints a `git commit` confirmation
inside a reachable checkout. Check that `mimir _hook git` can run: the plugin
uses `MIMIR_BIN`, then `executable=` in `$MIMIR_HOME/config`, then `mimir` on
`PATH`. You can capture a commit by hand:

```bash
echo '{"session_id":"<id>","cwd":"'"$PWD"'","harness":"opencode","shas":["<sha>"]}' | mimir _hook git
```

A commit becomes `pushed` once `git branch -r --contains <sha>` lists a
remote-tracking branch. Mimir re-checks unpushed commits at turn or session
end; fetch or push, then end or continue the session, or run `_hook git` with
`"refresh": true`.

## A Session Stays Active

A session without an `end` event becomes inactive after `session.gap_minutes`
(default 15) without activity, checked by the cron trigger every 10 minutes.
`mimir session end <id>` ends it at once.

## An Ended Session Became Active

That is intentional. New activity with the same session ID reopens it. A
genuinely new harness session uses a new ID.

## Summary Is Pending

Summaries are built when a session ends, or by the cron trigger for expired
sessions and sessions that saved new exchanges. Active sessions keep a pending
summary.

## Capture Is Pending, Partial, Or Failed

Read the receipt literally; do not infer persistence from proxy success, plugin
activity, or response headers. Check again after background writes settle:

```bash
mimir session status <id> --json
```

If failures remain, inspect Worker logs and run `mimir reconcile` without
sending a paid model request.
