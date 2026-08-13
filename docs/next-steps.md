# Next Steps

This file tracks concrete unfinished work and operational decisions. Completed
architecture-transition work belongs in git history and the implementation
specification rather than an expanding completion log.

## Active Implementation Work

OpenRouter optionality, meaningful `setup --quick` behavior, and Durable Object
retention cleanup are separate follow-ups because they change setup or session
lifecycle contracts.

### Pi capture validation

Pi uses a managed extension rather than a separate Mimir terminal application.
The extension routes Pi's OpenRouter provider through Mimir with exact session
metadata and reports bounded reconstructed exchanges for direct providers.
The installed skill and CLI remain the query surface.

Remaining work:

1. Validate extension loading, OpenRouter routing, exact session grouping,
   direct-provider reconstruction, reload, resume, compaction, and shutdown on
   macOS, Linux, and Windows.
2. Confirm `mimir doctor --json` observes the matching Pi harness-load hash
   after restart.
3. Confirm modified or conflicting user extension files are preserved by
   install, update, and uninstall.

## Operational Follow-ups

- After the first release that attaches `install.sh` and `install.ps1`, switch
  the README and installation guide from the branch bootstrap URLs to the
  release-attached `releases/latest/download` assets.
- Add required-reviewer protection to the existing GitHub `release`
  environment.
- Complete real install, activation, capture, resume, compaction, offline retry,
  update, and uninstall validation for Pi, OpenCode, Hermes, Claude Code, Codex,
  and Cursor on each supported operating system. Hook and extension installation
  remains staged until doctor observes a matching harness load.
- Define a recommended reconciliation cadence and an explicit policy for stale
  accepted rows and orphaned R2 objects.
- Keep `docs/Spec.md` synchronized with the live Access-protected dashboard and
  session-object behavior.

## Parked Decisions

- **Generalized harness provider router** — superseded by
  [`session-lifecycle.md`](session-lifecycle.md). Capture moves to the
  conversation layer (harness plugins reporting to session objects) instead of
  a harness × provider routing matrix. The proxy remains only for API-key
  providers with redirectable base URLs. Do not intercept TLS, impersonate
  OAuth clients, or turn machine tokens into provider credentials.
