---
name: mimir-setup
description: Set up or reconnect the self-hosted Mimir Cloudflare memory plane and wire the active OpenCode or Claude Code harness. Use when the user explicitly asks to install, set up, connect, or log in to Mimir.
---

# Mimir Setup

Mimir is a personal Cloudflare Worker memory plane. Never ask for credentials in chat and never print `~/.mimir/token`.

## Procedure

1. Check for the CLI with `command -v mimir`. If absent and Go is available, run `go install github.com/cloudboy-jh/mimir/cmd/mimir@latest` and use `$(go env GOPATH)/bin/mimir` if PATH has not refreshed.
2. Run `mimir whoami`.
3. If connected, skip login.
4. If disconnected, run `mimir login --json`.
5. If it returns `cloudflare_auth_required`, tell the user Cloudflare browser approval is required and run `mimir login` in the interactive terminal.
6. If it returns `deployment_missing`, this is the first machine. Run `mimir setup --json` only when `OPENROUTER_API_KEY` already exists in the process environment.
7. If setup returns `openrouter_key_required`, tell the user to run `mimir setup` in their terminal and enter the key at the masked prompt. Do not request or transfer the key through chat.
8. Once `mimir whoami` succeeds, wire only the active harness using the matching reference below.
9. Run `mimir whoami` through the configured MCP server and report whether session boundaries are exact or heuristic.

## Harness References

- OpenCode: [`references/opencode.md`](references/opencode.md)
- Claude Code: [`references/claude-code.md`](references/claude-code.md)

Do not create Git session repositories, session Markdown, Mimir accounts, or alternate storage. Do not claim unsupported harnesses are wired.
