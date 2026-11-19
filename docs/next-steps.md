# Next Steps

Concrete unfinished work and open operational decisions. Finished work belongs
in git history and the implementation specification.

## Validation

### MCP registration and tools

1. Confirm Claude Code loads the `mimir` MCP server from the generated
   `.mcp.json` in the Mimir plugin after `/reload-plugins`, and that each tool
   returns results.
2. Confirm Codex loads the generated `mcp.json` from the Mimir marketplace
   plugin with the Agent Plugins schema.
3. Confirm Cursor picks up a freshly written `~/.cursor/mcp.json`, and that
   the snippet shown by `mimir harness` and doctor works when the file already
   existed.
4. Confirm OpenCode and Hermes expose the `mimir_*` tools after restart.

### Commit capture

1. Validate `mimir _hook git` from every integration against real harness
   sessions: commit confirmations in shell output, pushed detection, and the
   `refresh` pass at turn and session end (OpenCode `session.idle`, Hermes
   finalize, Pi and Oh My Pi shutdown, Claude Code/Codex/Cursor Stop and
   session end).
2. Confirm binary resolution through `MIMIR_BIN`, `executable=` in
   `$MIMIR_HOME/config`, and `PATH` on macOS, Linux, and Windows.

### Pi capture

1. Validate extension loading, OpenRouter routing, exact session grouping,
   direct-provider reconstruction, reload, resume, compaction, and shutdown on
   macOS, Linux, and Windows.
2. Confirm `mimir doctor --json` observes the matching Pi harness-load hash
   after restart.
3. Confirm modified or conflicting extension files are preserved by install,
   update, and uninstall.

### Every harness

Complete real install, activation, capture, resume, compaction, offline retry,
update, and uninstall validation for Pi, Oh My Pi, OpenCode, Hermes, Claude
Code, Codex, and Cursor on each supported operating system.

## Build

- The dashboard production build warns about a large chunk. Split the bundle
  (route-level code splitting or manual chunks) until the warning clears.

## Operational Follow-ups

- After the first release that attaches `install.sh` and `install.ps1`, switch
  the README and installation guide from the branch bootstrap URLs to the
  release-attached `releases/latest/download` assets.
- Add required-reviewer protection to the GitHub `release` environment.
- Define a recommended `mimir reconcile` cadence (it is not part of the cron
  maintenance) and an explicit policy for stale accepted rows and orphaned R2
  objects.

## Parked Decisions

- **Generalized harness provider router.** Capture lives in the conversation
  layer (harness integrations reporting exchanges and events) rather than a
  harness × provider routing matrix. The proxy remains only for API-key
  providers with redirectable base URLs. Do not intercept TLS, impersonate
  OAuth clients, or turn machine tokens into provider credentials.
