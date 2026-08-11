---
name: mimir-setup
description: Set up or reconnect the self-hosted Mimir Cloudflare memory plane and connect the active agent harness. Use when the user explicitly asks to install, set up, connect, or log in to Mimir.
---

# Mimir Setup

Mimir is a personal Cloudflare Worker memory plane. Never ask for credentials in
chat, print `$MIMIR_HOME/token`, or pass secrets as command arguments.

## Procedure

1. Determine the requested path: local demo, fresh deployment, Cloudflare
   discovery login, or direct URL/token connection. Do not run login first when
   the user explicitly requested a fresh deployment.
2. If `mimir` is absent, install the checksum-verified release with the platform
   bootstrap from the repository README. Do not require Go or build from the Go
   module cache. If Mimir exists, run `mimir install --json` to reconcile managed
   files. Stop when `action_required` is true and preserve every reported
   conflict.
3. For a local preview, run `mimir demo --no-open`, return the loopback URL, and
   stop. Demo needs no Cloudflare, model credential, Node.js, Bun, or Go.
4. For a fresh deployment, verify Node.js 22 with npm/npx is available, then run
   `mimir setup --json` only when `OPENROUTER_API_KEY` is already in the process
   environment. Packaged setup uses the embedded Worker and compiled dashboard;
   it does not require Bun or Go.
5. If setup returns `cloudflare_auth_required`, tell the user browser approval is
   required and run interactive `mimir setup`. If it returns
   `openrouter_key_required`, tell the user to rerun interactive setup and enter
   the key at the masked prompt. Never request or transfer the key through chat.
6. For another machine or a reconnect, verify Node.js with npm/npx is available
   and run `mimir login --json`. If it returns `cloudflare_auth_required`, run
   interactive `mimir login`. If it returns `deployment_missing`, do not create
   resources unless the user asked for a fresh deployment. If it returns
   `deployment_url_missing`, run `mimir deploy`, then rerun login. If deploy
   still cannot recover the URL, find it in Cloudflare and run
   `mimir login --url <url>`.
7. For a direct endpoint, use `mimir setup --url <https-url>`. Supply
   `MIMIR_TOKEN` only through the process environment or interactive secure
   prompt. This path does not require Wrangler or Node.js.
8. Read the `connection` object from setup/login, or run the internal
   `mimir connection` command after an existing setup. For unsupported harnesses,
   apply only the returned proxy URL, credential source, and optional dynamic
   header names through the harness's supported configuration flow.
9. Run `mimir doctor --json` and apply only its exact repair. Common repairs are
   `mimir login` for connection state, `mimir deploy` for stale Worker state,
   `mimir install` for missing managed artifacts, `mimir update` for stale Hermes
   wiring, and `hermes plugins enable mimir` for a disabled Hermes plugin.
10. Apply the harness activation action reported by doctor: restart OpenCode or
    Hermes, run `/reload-plugins` or restart Claude Code, restart Codex, or open
    or continue a Cursor session. Run doctor again to verify the active hash.
11. Verify the first normal agent session with `mimir list --json`,
    `mimir session get <id> --json`, and `mimir session status <id> --json`.
    Never call a paid model endpoint merely to test deployment connectivity.

Mimir may update or uninstall exact opted-in files only when ownership is
recorded in `$MIMIR_HOME/install-receipt.json`. Preserve conflicts and locally
modified files. Setup/login refresh only an existing managed installation and do
not silently enroll global hook files. The connection manifest remains the
contract for provider configuration; do not invent additional harness-specific
Worker behavior.

Do not create Git session repositories, session Markdown, Mimir accounts,
alternate storage, lifecycle hooks, custom Hermes providers, or routine user
workflows.
