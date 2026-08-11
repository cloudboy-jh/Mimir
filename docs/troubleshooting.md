# Troubleshooting

Start with:

```bash
mimir doctor --json
```

Doctor is read-only. It reports the failed check, evidence, and an exact repair
command or activation action. Do not treat a warning or staged integration as
healthy, and do not invent a broader repair than the one reported.

## Common Doctor Repairs

| Failure | Repair reported by doctor |
| --- | --- |
| Local connection or connection manifest is invalid | `mimir login` |
| Worker API, required capability, harness-load endpoint, or bundle is stale | `mimir deploy` |
| Managed artifact is missing or outdated | `mimir install` |
| Managed artifact is modified or conflicting | Review the preserved file; remove or restore it, then run `mimir install` |
| OpenCode integration is installed but staged | Restart OpenCode |
| Hermes integration is installed but staged | Restart Hermes |
| Claude Code integration is installed but staged | Run `/reload-plugins` or restart Claude Code |
| Codex integration is installed but staged | Restart Codex |
| Cursor integration is installed but staged | Open or continue a Cursor agent session |
| Hermes plugin is disabled | `hermes plugins enable mimir` |
| Hermes managed OpenRouter route is stale | `mimir update` |
| Hermes OpenRouter credential is unavailable | Configure Hermes OpenRouter authentication |

Run doctor again after the repair. Deployment verification must use `/whoami`
or direct session APIs, not a paid model endpoint.

## Setup And Login States

### `cloudflare_auth_required`

Rerun the original Mimir command without `--json` and complete Wrangler's
browser approval. JSON mode cannot complete interactive authentication.

### `openrouter_key_required`

Set `OPENROUTER_API_KEY` in the process environment or rerun `mimir setup`
interactively and enter it through the masked prompt. Never pass the key as a
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
then rerun `mimir login`. If Wrangler cannot recover the workers.dev URL from
deploy output, find the URL in Cloudflare and run `mimir login --url <url>`.

## Installation Reports `action_required`

Mimir preserved a conflicting, modified, or unsafe path. Inspect the reported
file. Remove it only when it is disposable, or restore the exact Mimir-owned
version, then run `mimir install` again. Mimir will not overwrite it to clear the
warning.

## Dashboard Access Fails

The Access application must protect exactly `/dashboard/auth`,
`/dashboard/api/*`, and `/dashboard/log-objects/*`. Do not protect the bare host
or `/login`; that blocks machine routes or prevents the browser handoff.

`mimir access` refuses to rewrite Bypass, permissive, additional, or otherwise
conflicting policies. Resolve them in Cloudflare, then rerun the command. Browser
code never receives a Mimir machine token.

## OpenCode Capture Is Missing

Run doctor, repair the exact managed-artifact or Worker issue, and restart
OpenCode. OpenCode does not hot-reload plugin changes. Redirected OpenRouter
traffic is captured at the proxy; direct-provider capture depends on the loaded
plugin.

## Hermes Capture Is Missing

Run doctor. Enable the plugin when instructed, verify Hermes OpenRouter
authentication, and restart Hermes after route or plugin changes. A stale Worker
missing Hermes endpoints requires `mimir deploy`. Do not create a custom Hermes
provider.

Hermes direct-provider turns are event-only. They keep session lifecycle current
but do not create searchable request/response exchange objects.

## Session Is Disconnected

`disconnected` means no event arrived for about 90 seconds and the session has
not finalized. Activity can restore `active`. About ten minutes of silence
finalizes the session. This is independent of whether exchanges were saved.

## A Finalized Session Became Active

That is intentional. New activity with the same exact session ID reopens the
same history. A genuinely new harness session must use a new ID.

## Capture Is Pending, Partial, Or Failed

Treat the receipt literally. Do not infer persistence from proxy success,
plugin activity, or response headers. Retry:

```bash
mimir session status <id> --json
```

If failures remain after background writes settle, inspect Worker logs and the
R2/D1 bindings without sending a paid model request.
