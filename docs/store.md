# `.mimir/` Store

The durable store lives at the repository root and is gitignored by default.

```txt
.mimir/
├── index.json
├── config.json
└── history/
```

`index.json` is written atomically through a temporary file and rename.

```json
{
  "project": "glib-code",
  "indexed_commit": "7d58e16bc8d...",
  "timestamp": "2026-07-08T15:30:00Z",
  "files": {
    "src/core.ts": {
      "hash": "a4f89d31...",
      "symbols": ["SandboxAdapter"],
      "dependencies": ["src/adapters/types.ts", "hono"]
    }
  },
  "symbols": {
    "SandboxAdapter": {
      "type": "interface",
      "file": "src/core.ts",
      "line": 12,
      "signature": "export interface SandboxAdapter {"
    }
  }
}
```
