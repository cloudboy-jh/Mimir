---
name: mimir-setup
description: Deploy and connect the self-hosted Mimir Worker memory plane on the user's Cloudflare account.
---

# Mimir Setup

Mimir is a self-hosted Cloudflare Worker. Its Worker proxies model traffic to OpenRouter, stores full redacted exchanges in R2, and indexes sessions/configuration in D1.

## Setup

1. Install the CLI and run:

```bash
go install github.com/cloudboy-jh/mimir/cmd/mimir@latest
mimir setup --quick
```

2. Enter the OpenRouter key only at the interactive prompt. Never put it in a command argument or config file.
3. `mimir setup` authenticates with Wrangler, creates D1/R2, applies migrations, generates the Mimir bearer token, deploys the Worker, writes the local pointer, and verifies `/whoami`.
4. Use `mimir setup --minimal` for a proxy with `save.enabled=false`. Use `--full` with explicit resource-name flags where required.

On an additional machine, run `mimir login`. Wrangler authenticates ownership of the existing Cloudflare deployment and Mimir creates an independent machine token without requesting the OpenRouter key again.

Do not create Git session repositories or local session markdown. The deployment owns sessions.
