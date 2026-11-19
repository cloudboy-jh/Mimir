# Install, Connect, And Own Mimir

## Install The Release

Use the release bootstrap rather than building the CLI from source:

```bash
curl -fsSL https://raw.githubusercontent.com/cloudboy-jh/mimir/master/install.sh | sh
```

Windows PowerShell:

```powershell
irm https://raw.githubusercontent.com/cloudboy-jh/mimir/master/install.ps1 | iex
```

The bootstrap resolves the latest stable release, verifies its GoReleaser
checksum, and hands installation to the downloaded binary. Set
`MIMIR_VERSION` to select a release or `MIMIR_INSTALL_DIR` to select the
binary directory. When that directory is outside `PATH`, the bootstrap prints
the command needed to add it.

Interactive bootstrap use prompts for detected harnesses. Automation passes the
selection through, for example `curl -fsSL <install.sh-url> | sh -s --
--harness opencode`, or runs a downloaded `install.ps1` with
`-Harness opencode,codex`.

`go install github.com/cloudboy-jh/mimir/cmd/mimir@latest` builds only a
binary and installs no integrations. Use it for source workflows, then run
`mimir install` to record it and add integrations.

Interactive `mimir install` prompts once for harnesses and defaults to the
detected ones: OpenCode, Pi, Oh My Pi, Hermes, Claude Code, Codex, and Cursor.
Automation selects with repeatable `--harness <id>` or `--harness all`; JSON
and noninteractive installs never prompt. `mimir harness` shows each
integration's state. `mimir enable|disable <pi|opencode|hermes>` and
`mimir harness enable|disable <id>` change the selection; disable removes only
managed files whose bytes are unchanged.

## Choose A Connection Path

| Goal | Command | Prerequisites |
| --- | --- | --- |
| Create a new deployment | `mimir setup` | Cloudflare account, OpenRouter key, Node.js 22 with npm/npx |
| Connect another machine through Cloudflare discovery | `mimir login` | Existing deployment, Cloudflare account access, Node.js 22 with npm/npx |
| Connect directly with an existing URL and machine token | `mimir setup --url <url>` | Existing HTTPS URL and token; no Wrangler or Node.js |
| Deploy packaged Worker/dashboard updates | `mimir deploy` | Existing deployment, Cloudflare auth, Node.js 22 with npm |
| Deploy development Worker source | `mimir deploy --worker-dir <dir>` | Packaged requirements plus Bun and valid dashboard source |

Go, Bun, and a source checkout are development prerequisites only. Setup and
deploy use the Worker and compiled dashboard embedded in the running binary and
never discover source from the current directory, parent directories, or the Go
module cache.

## Create A Fresh Deployment

```bash
mimir setup
```

Packaged setup:

1. Materializes the embedded Worker and compiled dashboard under
   `$MIMIR_HOME/worker`.
2. Installs the packaged Wrangler dependencies with npm.
3. Authenticates Wrangler, opening the browser when approval is required.
4. Creates or reuses D1 and R2, writes binding identifiers, and applies D1
   migrations.
5. Reuses an existing `OPENROUTER_API_KEY` Worker secret or reads the key from
   the environment or a masked prompt.
6. Registers this machine, deploys the Worker, verifies `/whoami`, and saves
   the URL/token pointer and the CLI path (`executable=`) in
   `$MIMIR_HOME/config`.
7. Optionally configures Cloudflare Access and redeploys the Access variables.

In JSON mode, set `OPENROUTER_API_KEY` before a fresh setup. Without the
environment value or an existing Worker secret, setup returns
`openrouter_key_required`. Never pass the key as a command argument or paste it
into agent chat.

Setup refreshes integrations only when `managed.json` already records them. It
does not enroll plugin or hook files on its own; run `mimir install` for that.

## Connect An Existing Deployment

### Cloudflare discovery

On another machine, install Mimir and run:

```bash
mimir login
```

Login first accepts a healthy saved connection. Otherwise it materializes the
Worker configuration without installing project dependencies, uses local
Wrangler or `npx wrangler` to authenticate, discovers D1 and the stored
deployment URL, registers a new machine token, verifies `/whoami`, reports the
tailnet when Tailscale is available, and saves the connection. It does not need
the OpenRouter key, Bun, Go, or dashboard build dependencies.

Setup and login use the stable `installation_id` in
`$MIMIR_HOME/managed.json` and associate the new machine token with it through
`POST /machine/associate`. The ID is the durable device identity; the hostname
is only its initial display name, which you can change on the Devices page.
Reconnecting does not overwrite an edited name.

### Direct URL and token

When Cloudflare discovery is unavailable but a valid machine token exists:

```bash
mimir setup --url https://mimir.example.workers.dev
```

Interactive setup reads the token through a secure prompt. For JSON automation,
set `MIMIR_TOKEN` in the environment before running the command. This path
verifies `/whoami`, saves the URL/token pointer, and refreshes managed
integrations without Cloudflare login, Wrangler, or provisioning.

## Configure Dashboard Access

Access is optional for setup but required to use the deployed dashboard in a
browser.

```bash
mimir access
```

One self-hosted Access application must protect exactly:

- `/dashboard/auth`
- `/dashboard/api/*`

Do not protect the bare Worker host. `/login` stays public for the browser
handoff, and machine routes (`/api`, `/mcp`, `/v1`, `/sessions`, …) stay outside
Access because they use per-machine bearer tokens.

Automation uses `CLOUDFLARE_API_TOKEN` and normally `MIMIR_ACCESS_EMAIL`. The
token needs Account Access: Apps and Policies Edit plus Account Access:
Organizations, Identity Providers, and Groups Read. Mimir accepts only one exact
Allow policy for the supplied email. It preserves Bypass, permissive,
additional, or conflicting policies and returns an action-required result
instead of rewriting them.

## Activate Harness Integrations

Installed bytes and loaded bytes are separate states. After install or update,
apply the activation action:

| Harness | Activation action |
| --- | --- |
| Pi | Restart Pi |
| Oh My Pi | Restart Oh My Pi |
| OpenCode | Restart OpenCode |
| Hermes | Restart Hermes |
| Claude Code | Run `/reload-plugins` or restart Claude Code |
| Codex | Review and trust the Mimir plugin hooks in `/hooks`, then restart Codex |
| Cursor | Open or continue an agent session; Cursor reloads `hooks.json` automatically |

Then run `mimir doctor --json`. An installed file that has not been loaded is
reported with its activation action.

### Integration details

| Harness | Managed files | How agents query Mimir |
| --- | --- | --- |
| OpenCode | `~/.config/opencode/plugins/mimir.ts`, `mimir-setup` and `mimir-use` skills | Native `mimir_*` tools |
| Pi | `~/.pi/agent/extensions/mimir.ts` | CLI with `--json` |
| Oh My Pi | `~/.omp/agent/extensions/mimir.ts` | CLI with `--json` |
| Hermes | `~/.hermes/plugins/mimir/`, skills, managed `.env` route block | Native `mimir_*` tools (toolset `mimir`) |
| Claude Code | `~/.claude/skills/mimir/` plugin manifest, `hooks/hooks.json`, `.mcp.json` | MCP server `mimir` |
| Codex | `~/.agents/plugins/marketplace.json` and `plugins/mimir/` (`plugin.json`, `hooks/hooks.json`, `mcp.json`) | MCP server `mimir` |
| Cursor | `~/.cursor/hooks.json`; `~/.cursor/mcp.json` only when absent | MCP server `mimir` |

Every MCP configuration launches `<absolute path to mimir> mcp`. When
`~/.cursor/mcp.json` already exists, Mimir leaves it alone and `mimir harness`,
the install output, and `mimir doctor` show the exact `mimir` server entry to
add by hand.

Configured agent homes change these paths: `PI_CODING_AGENT_DIR`,
`OMP_CODING_AGENT_DIR`, active OMP and Hermes profiles, and
`CLAUDE_CONFIG_DIR`. On Windows, Hermes defaults to
`%LOCALAPPDATA%\hermes\plugins\mimir\`.

Pi's extension routes its OpenRouter provider through Mimir with exact
session, repository, and harness headers and uploads bounded completed turns
for direct and subscription providers, including tool results Pi exposes. It
exports `MIMIR_SESSION_ID`.

Oh My Pi starts lifecycle heartbeats on the first real turn, not on an idle
draft, captures OpenRouter and direct-provider evidence, and exports
`MIMIR_SESSION_ID`. Sub-agents link to their parent through
`ctx.parentSessionId` or, when unavailable, the bounded session artifact tree;
parentage is never inferred from forks, prompt text, or timing. Sub-agents stay
separate sessions under a root whose totals include them.

OpenCode's plugin uploads bounded reconstructed non-OpenRouter exchanges from
its session store and reports lifecycle events, titles, and model switches. It
never rewrites general OpenCode JSON or JSONC. See
[OpenCode capture](opencode-capture-setup.md).

Hermes redirects its built-in OpenRouter provider through the
installation-scoped `/v1/hermes/<installation-id>` route. For Codex, Nous,
OAuth, and other direct providers its plugin uploads bounded request/response
reconstructions and supported tool results. See
[Hermes capture](hermes-capture-setup.md).

Claude Code, Codex, and Cursor hooks invoke the hidden `mimir _hook` adapter.
It pairs prompt and completion hooks into bounded exchanges, queues delivery in
an encrypted outbox when the Worker is unavailable, captures commits after
shell tools, and re-checks pushed commits at Stop and session end. Existing
different Claude Code or Cursor hook files are conflicts, never merged or
overwritten. Codex uses an isolated personal marketplace plugin under
`~/.agents/plugins`.

Every integration captures commits with `mimir _hook git`, found through
`MIMIR_BIN`, then `executable=` in `$MIMIR_HOME/config`, then `PATH`.

## Verify A Real Session

After doctor reports the deployment and integration as healthy:

1. Start a new session in the reloaded harness.
2. Send one normal prompt through the provider you intend to use.
3. Run `mimir sessions --json` and copy the session ID.
4. Inspect it with `mimir session <id> --json`.
5. Check the receipt with `mimir session status <id>`.
6. After configuring Access, open `mimir dashboard` and find the same session.

The receipt reflects stored state when you ask. `Saving to Mimir… · N pending`
means exchanges were accepted but not yet saved; ask again shortly. Proxy
success or plugin activity alone does not prove an exchange was saved.
Deployment checks use `/whoami` and the query API, never a paid completion
endpoint.

## Capture Fidelity

| Path | Saved evidence |
| --- | --- |
| Redirected OpenRouter and other Mimir proxy traffic | Complete redacted request/response exchange, streamed upstream and archived in R2 |
| Pi and Oh My Pi direct and subscription providers | Bounded reconstruction from the completed turn |
| OpenCode OAuth, subscription, or direct providers | Bounded reconstruction from OpenCode's session store |
| Claude Code and Cursor hooks | Bounded prompt/assistant reconstruction; tool traces and usage may be unavailable |
| Codex hooks | Bounded prompt/assistant reconstruction plus token usage when the transcript has a matching record |
| Hermes Nous portal, OAuth, Codex, and other direct providers | Bounded reconstruction of Hermes-observed requests, responses, and supported tool results |

Reconstructions are not byte-for-byte provider transport. OpenRouter plugin
uploads are suppressed when proxy capture is canonical. A heartbeat or
plugin-load report does not prove any exchange was saved. `mimir import`
supports OpenCode, Pi, and Oh My Pi (parent links); Hermes history is not
imported.

## Cloudflare Free Plan Units

Mimir consumes shared account allowances rather than a dedicated quota.
Cloudflare documented these relevant Free plan units in August 2026:

| Product | Included usage |
| --- | --- |
| Workers | 100,000 requests/day; 10 ms CPU/invocation |
| D1 | 5 million rows read/day; 100,000 rows written/day; 5 GB total storage |
| R2 Standard | 10 GB-month/month; 1 million Class A operations/month; 10 million Class B operations/month; free egress |

The cron maintenance trigger runs every 10 minutes and counts toward Worker
requests. These limits are shared with other applications, and Cloudflare can
reject operations after a daily allowance is reached. Verify the current
[Workers](https://developers.cloudflare.com/workers/platform/pricing/),
[D1](https://developers.cloudflare.com/d1/platform/pricing/), and
[R2](https://developers.cloudflare.com/r2/pricing/) pricing before planning
capacity. Access is configured through Cloudflare Zero Trust and has separate
[account limits](https://developers.cloudflare.com/cloudflare-one/account-limits/).

## Managed Files

`$MIMIR_HOME/managed.json` (default `~/.mimir/managed.json`) records the
installation ID, bundle version, the installed CLI path and hash, the selected
harnesses, and every file Mimir wrote with the SHA-256 of its bytes.

Mimir:

- creates absent files and treats byte-identical files as current;
- updates a file only while its bytes still match the recorded hash;
- preserves and reports different (`conflict`) and locally changed
  (`modified`) files;
- refuses symlinked and non-regular paths;
- removes only files whose bytes still match;
- never rewrites general OpenCode JSON/JSONC or merges user-owned hook files.

## Update And Uninstall

```bash
mimir update --check
mimir update
mimir deploy
mimir uninstall
mimir uninstall --keep-binary
```

`mimir update` reruns the official installer, which verifies the release and
refreshes managed files. It does not deploy the Worker; run `mimir deploy` to
apply the release's bundled Worker, dashboard, and D1 migrations.

Uninstall removes managed files whose bytes still match and, unless
`--keep-binary`, the recorded binary when its hash matches and no package
manager owns it. On Windows the running binary stays and the command prints
the path to delete after it exits. The connection, machine token, materialized
Worker, and Cloudflare deployment remain.
