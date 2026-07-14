# OpenCode Wiring

OpenCode supports dynamic request headers, so this integration provides exact Mimir session boundaries.

1. Back up `~/.config/opencode/opencode.json` or `opencode.jsonc` before editing it.
2. Read the deployment URL from `mimir whoami`; do not read or print `~/.mimir/token`.
3. Preserve all unrelated configuration and add the OpenRouter provider override:

```json
{
  "$schema": "https://opencode.ai/config.json",
  "provider": {
    "openrouter": {
      "options": {
        "baseURL": "https://YOUR-MIMIR-WORKER.workers.dev/v1",
        "apiKey": "{file:~/.mimir/token}",
        "headers": {
          "x-mimir-harness": "opencode"
        }
      }
    }
  },
  "mcp": {
    "mimir": {
      "type": "local",
      "command": ["mimir", "serve"],
      "enabled": true
    }
  }
}
```

4. Create `~/.config/opencode/plugins/mimir.ts`:

```ts
import type { Plugin } from "@opencode-ai/plugin"

export const MimirPlugin: Plugin = async ({ worktree }) => {
  const repo = worktree.replace(/\\/g, "/").split("/").filter(Boolean).at(-1) ?? "unknown"
  return {
    "chat.headers": async ({ sessionID }, output) => {
      output.headers["x-mimir-session"] = sessionID
      output.headers["x-mimir-repo"] = repo
      output.headers["x-mimir-harness"] = "opencode"
    },
  }
}
```

5. Install `mimir-use` into an OpenCode skill directory if it is not already discoverable.
6. Validate the edited config against `https://opencode.ai/config.json`.
7. Restart OpenCode because provider and plugin configuration is loaded at startup.

Result: OpenCode uses exact `x-mimir-session` boundaries.
