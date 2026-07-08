# `.churn/` Store

The durable store lives at the repository root and is gitignored by default.

```txt
.churn/
├── index.json
├── context.json
├── map/
│   ├── files.json
│   ├── deps.json
│   └── symbols.json
├── findings.json
└── history/
```

Phase 4 implements atomic writes and the first full-index path.
