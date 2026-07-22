---
name: mimir-setup
description: Set up or reconnect the self-hosted Mimir Cloudflare memory plane and connect the active agent harness. Use when the user explicitly asks to install, set up, connect, or log in to Mimir.
---

# Mimir Setup

Mimir is a personal Cloudflare Worker memory plane. Never ask for credentials in chat and never print `~/.mimir/token`.

## Procedure

1. Verify Go, Node.js with npm, and Bun are available. Setup needs all three toolchains to materialize and build the Worker package.
2. Check for `mimir`. If absent, run `go install github.com/cloudboy-jh/mimir/cmd/mimir@latest`.
3. Run `mimir login --json`.
4. If it returns `cloudflare_auth_required`, tell the user browser approval is required and run interactive `mimir login`.
5. If it returns `deployment_missing`, run `mimir setup --json` only when `OPENROUTER_API_KEY` exists in the process environment.
6. If setup returns `openrouter_key_required`, tell the user to run interactive `mimir setup` and enter the key at the masked prompt. Never request or transfer the key through chat.
7. Read the `connection` object from setup/login, or run the internal `mimir connection` command after an existing setup.
8. If the active harness is Hermes, run `mimir doctor --json`. Setup/login transparently redirect its built-in OpenRouter provider; never create a custom provider. If doctor reports stale wiring, run `mimir update`. Tell the user to restart Hermes after repair.
9. For OpenCode and harnesses without a bundled integration, configure the OpenAI or Anthropic provider with the corresponding base URL through the harness's supported configuration flow. Supply authentication using `credential_file`, `credential_command`, or the harness's secure secret input. Do not print or place the credential value in ordinary configuration or chat. Never directly rewrite OpenCode JSON, JSONC, plugins, commands, or MCP entries.
10. Register the returned `mcp_command` as the harness's local Mimir MCP server.
11. If the harness supports dynamic request headers, derive and add any names listed in `optional_headers`. Never use header names or placeholder text as literal values. These improve grouping but are not required.
12. Install `mimir-use` in the harness's skill directory and validate the harness configuration using its native validation command or schema.

Mimir owns the explicitly bundled Hermes integration. The connection manifest is the contract for OpenCode and other harnesses; do not invent additional harness-specific Worker behavior.

Do not create Git session repositories, session Markdown, Mimir accounts, alternate storage, lifecycle hooks, or routine user workflows.
