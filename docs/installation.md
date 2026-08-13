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
checksum, and delegates ownership-safe installation to the downloaded binary.
Set `MIMIR_VERSION` to select a release or `MIMIR_INSTALL_DIR` to select the
binary directory. When that directory is outside `PATH`, the bootstrap prints
the exact command needed to add it.

Interactive bootstrap use prompts for detected harnesses. Automation must pass
the selection through, for example `curl -fsSL <install.sh-url> | sh -s --
--harness opencode`, or invoke downloaded `install.ps1` with `-Harness
opencode,codex`.

`go install github.com/cloudboy-jh/mimir/cmd/mimir@latest` creates only a Go
binary. It does not create an ownership receipt or install integrations. Use it
only for source-oriented workflows, then run `mimir install` to adopt a verified
binary when no other binary owner is recorded.

Interactive `mimir install` prompts once for harness selection and defaults to
detected harnesses in this order: OpenCode, Pi, Hermes, Claude Code, Codex, and
Cursor. Automation must select explicitly with repeatable `--harness <id>` or
`--harness all`; JSON and noninteractive installs never prompt. Inspect or
change the persisted selection with `mimir harness list`, `mimir harness enable
<id>`, and `mimir harness disable <id>`. Disable removes only receipt-owned,
unmodified artifacts. Modified or otherwise unsafe files remain owned and are
preserved for a later enable or full ownership-driven uninstall.

## Choose A Connection Path

| Goal | Command | Prerequisites |
| --- | --- | --- |
| Explore sample sessions locally | `mimir demo` | Mimir binary only |
| Create a new deployment | `mimir setup` | Cloudflare account, OpenRouter key, Node.js 22 with npm/npx |
| Connect another machine through Cloudflare discovery | `mimir login` | Existing deployment, Cloudflare account access, Node.js 22 with npm/npx |
| Connect directly with an existing URL and machine token | `mimir setup --url <url>` | Existing HTTPS URL and token; no Wrangler or Node.js required |
| Deploy packaged Worker/dashboard updates | `mimir deploy` | Existing deployment, Cloudflare auth, Node.js 22 with npm |
| Deploy development Worker source | `mimir deploy --worker-dir <dir>` | Packaged requirements plus Bun and valid dashboard source |

Go 1.25.2, Bun, and a source checkout are development prerequisites, not
binary-based setup prerequisites. Setup and deploy use the Worker and compiled
dashboard embedded in the running binary. They never discover source from the
current directory, parent directories, or Go module cache.

## Try The Local Demo

```bash
mimir demo
```

The demo serves fixture sessions on an ephemeral `127.0.0.1` port. It does not
read installation state or require Cloudflare, credentials, model traffic,
Node.js, Bun, Go, or a network service. Use `mimir demo --no-open` when the CLI
should print the URL without launching a browser. Fixture changes reset when the
page reloads.

## Create A Fresh Deployment

```bash
mimir setup
```

Packaged setup:

1. Materializes the embedded Worker and compiled dashboard under
   `$MIMIR_HOME/worker`.
2. Installs the packaged Wrangler dependencies with npm. It does not install
   dashboard dependencies or invoke Bun.
3. Authenticates Wrangler, opening the browser when interactive approval is
   required.
4. Creates or reuses D1 and R2, writes binding identifiers, and applies D1
   migrations.
5. Reuses an existing `OPENROUTER_API_KEY` Worker secret or reads the key from
   the environment or a masked prompt.
6. Registers this machine, deploys the Worker, verifies `/whoami`, and saves the
   local URL/token pointer.
7. Optionally configures Cloudflare Access and redeploys the Access variables.

In JSON mode, set `OPENROUTER_API_KEY` before a genuinely fresh setup. Without
the environment value or an existing Worker secret, setup returns
`openrouter_key_required`. Never pass the key as a command argument or paste it
into agent chat.

Setup refreshes integrations only when `mimir install` already created a managed
receipt. It does not silently enroll global plugin or hook files. Run
`mimir install` first when installation reports that artifacts are absent.

## Connect An Existing Deployment

### Cloudflare discovery

On another machine, install Mimir and run:

```bash
mimir login
```

Login first verifies a healthy saved connection. Otherwise it materializes the
Worker configuration without installing project dependencies, uses local
Wrangler or `npx wrangler` to authenticate, discovers D1 and the stored
deployment URL, registers a fresh machine token, verifies `/whoami`, and saves
the connection. It does not need the OpenRouter key, Bun, Go, or dashboard build
dependencies.

Setup and login create a stable local `installation_id` when the Worker's
`/whoami` response advertises `machine_identity_association`, then associate the
new machine token through `POST /machine/associate`. The ID is the durable
device identity; the initial hostname is only its display name and can be
changed later in Dashboard Settings. Reconnecting does not overwrite an edited
name. A token cannot be moved to another installation after its first
association.

### Direct URL and token

When Cloudflare discovery is unavailable but a valid machine token already
exists, connect directly:

```bash
mimir setup --url https://mimir.example.workers.dev
```

Interactive setup reads the token through a secure prompt. For JSON automation,
set `MIMIR_TOKEN` through the shell or CI secret mechanism before running the
command. This path verifies `/whoami`, saves the URL/token pointer, and refreshes
existing managed integrations without Cloudflare login, Wrangler, D1 discovery,
or provisioning.

## Configure Dashboard Access

Access is optional for setup completion but required before the deployed private
dashboard API and redacted objects are usable in a browser.

```bash
mimir access
```

One self-hosted Access application must protect exactly:

- `/dashboard/auth`
- `/dashboard/api/*`
- `/dashboard/log-objects/*`

Do not protect the bare Worker host. `/login` must remain public for the branded
handoff, and machine routes must remain outside Access because they use
per-machine bearer tokens.

Automation uses `CLOUDFLARE_API_TOKEN` and normally `MIMIR_ACCESS_EMAIL`. The
token needs Account Access: Apps and Policies Edit plus Account Access:
Organizations, Identity Providers, and Groups Read. Mimir accepts only one exact
Allow policy for the supplied email. It preserves permissive, Bypass,
additional, or otherwise conflicting policies and returns an action-required
result instead of rewriting them.

## Activate Harness Integrations

Installed bytes and actively loaded bytes are separate states. After install or
update, apply the activation action for the active harness:

| Harness | Activation action |
| --- | --- |
| Pi | Restart Pi |
| OpenCode | Restart OpenCode |
| Hermes | Restart Hermes |
| Claude Code | Run `/reload-plugins` or restart Claude Code |
| Codex | Restart Codex |
| Cursor | Open or continue an agent session; Cursor reloads `hooks.json` automatically |

Then run `mimir doctor --json`. A current file that has not been loaded is
reported as staged rather than healthy.

Pi receives a managed global capture extension at
`~/.pi/agent/extensions/mimir.ts` (or
`$PI_CODING_AGENT_DIR/extensions/mimir.ts`) and the shared skill. OpenCode
receives the managed capture plugin and skills. Hermes receives its plugin,
skills, and bounded managed OpenRouter route. Claude Code receives a
skills-directory plugin; Codex and Cursor receive managed hook manifests. A
different existing hook file is preserved as a conflict, never merged or
overwritten.

## Verify A Real Session

After doctor reports the deployment and active integration as healthy:

1. Start a new session in the reloaded harness.
2. Send one normal prompt through the provider you intend to use.
3. Run `mimir list --json` and copy the resulting session ID.
4. Inspect it with `mimir session get <id> --json`.
5. Confirm persistence with `mimir session status <id> --json`.
6. Open `mimir dashboard` and verify the same session is present.

The session should show the device registered by setup or login. Device names
are labels; changing one does not alter the session's stable device association.

`mimir session status` is authoritative. Proxy success, plugin activity, or a
scheduled capture header only proves that persistence was queued. Deployment
health checks use `/whoami` and direct session APIs, never a paid completion
endpoint.

## Capture Fidelity

| Path | Saved evidence |
| --- | --- |
| Redirected OpenRouter and other Mimir proxy traffic | Complete redacted request/response transport exchange, streamed upstream and archived in R2 |
| Pi direct providers and subscription providers | Bounded reconstruction from Pi's completed turn; not byte-for-byte provider transport |
| OpenCode OAuth, subscription, or direct providers | Bounded reconstruction from OpenCode's session store; not byte-for-byte provider transport |
| Claude Code, Codex, and Cursor supported hooks | Bounded prompt/assistant reconstruction; tool traces and usage may be unavailable |
| Hermes Nous portal, OAuth, and direct providers | Event-only completed-turn summary; no searchable exchange object |

OpenRouter plugin uploads are suppressed when proxy capture is canonical.
Reconstructed exchanges are redacted and persisted like proxy exchanges when
delivery succeeds. Event-only summaries keep session lifecycle current but do
not create R2 exchange objects or searchable D1 exchange rows.

## Cloudflare Free Plan Units

Mimir consumes shared account allowances rather than a dedicated Mimir quota.
Cloudflare documented these relevant Free plan units in August 2026:

| Product | Included usage |
| --- | --- |
| Workers | 100,000 requests/day; 10 ms CPU/invocation |
| D1 | 5 million rows read/day; 100,000 rows written/day; 5 GB total storage |
| R2 Standard | 10 GB-month/month; 1 million Class A operations/month; 10 million Class B operations/month; free egress |
| SQLite Durable Objects | 100,000 requests/day; 13,000 GB-s/day; 5 million rows read/day; 100,000 rows written/day; 5 GB total storage |

These are account-level limits shared with other applications. On the Free plan,
Cloudflare can reject additional operations after a daily allowance is reached.
The units and product availability can change. Verify the current official
[Workers](https://developers.cloudflare.com/workers/platform/pricing/),
[D1](https://developers.cloudflare.com/d1/platform/pricing/),
[R2](https://developers.cloudflare.com/r2/pricing/), and [Durable
Objects](https://developers.cloudflare.com/durable-objects/platform/pricing/)
pages before planning capacity. Access is configured through Cloudflare Zero
Trust and has separate plan and [account
limits](https://developers.cloudflare.com/cloudflare-one/account-limits/).

## Managed Ownership

Managed ownership lives in `$MIMIR_HOME/install-receipt.json`, defaulting to
`~/.mimir/install-receipt.json`. Operations append to
`$MIMIR_HOME/install-log.jsonl`.

Mimir:

- creates absent opted-in files;
- adopts byte-identical Mimir files;
- migrates byte-exact known historical artifacts;
- updates only receipt-owned files whose bytes are unchanged;
- preserves unknown, modified, missing, symlinked, and non-regular paths;
- removes only verified owned artifacts and binaries;
- never rewrites general OpenCode JSON/JSONC.

The receipt-owned binary is the command published to integrations. Ownership is
adopted only for an exact receipt target, a binary copied to the intended
installation path, or a verified Mimir executable when the receipt has no owner.
Invoking installation through another executable cannot silently transfer it.

## Update And Uninstall

```bash
mimir update
mimir install
mimir uninstall
mimir uninstall --keep-binary
```

`mimir update` refreshes the binary and managed integrations but does not deploy
the embedded Worker. Run `mimir deploy` when doctor reports a stale bundle or
Worker capability. Uninstall preserves the connection, machine token,
materialized Worker, install log, and Cloudflare deployment. It removes only
verified receipt-owned files whose current bytes still match the receipt.

On Windows, locked binary replacement and removal use a verified deferred helper.
`mimir update --force` stops sibling processes using the exact managed
executable and applies the update immediately.
