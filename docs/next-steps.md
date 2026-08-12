# Next Steps

This file tracks concrete unfinished work and operational decisions. Completed
architecture-transition work belongs in git history and the implementation
specification rather than an expanding completion log.

## Active Implementation Work

OpenRouter optionality, meaningful `setup --quick` behavior, and Durable Object
retention cleanup are separate follow-ups because they change setup or session
lifecycle contracts.

### Release hold: candidate validation

Installed-binary transcript fixtures, account-bound resource discovery,
receipt-safe junction migration, isolated CI homes, the full release matrix,
immutable candidates, protected evidence approval, and fail-closed tag
promotion are implemented. Do not tag the next release until the remaining
operator evidence is captured from the final committed candidate:

- Validate human and `--json` install, update, doctor, and deploy transcripts
  against a real Cloudflare account without calling a paid model endpoint.
- Exercise clean and existing Mimir homes, custom resource names, stale cached
  metadata, a failed deploy and successful recovery, and a clean owned-artifact
  doctor result.
- Submit transcript hashes through the protected `release` environment as
  documented in [`release-evidence.md`](release-evidence.md).

### Adaptive full-terminal Mimir TUI

The adaptive AltScreen transition, measured full-terminal layout, centered
sessions-and-Ask-Mimir home surface, state-preserving reload behavior, and
small-terminal fallback are implemented. Ask Mimir now opens a dedicated
conversation view, carries selected-session context into prompts, streams Pi
events, exposes Mimir tools and model selection, and leaves session browsing
available when Pi cannot start. Deterministic transitions, lifecycle cleanup,
redraw coalescing, and Windows Unicode input are covered. The remaining work is
manual cross-platform validation.

The compact `80x20` constraint remains appropriate for temporary setup, deploy,
login, and lightweight session-browser surfaces, but it wastes space and
complicates the persistent sessions-and-Pi application. Install and update use
plain line-oriented output; `mimir tui` uses the measured terminal dimensions
and AltScreen lifecycle.

Remaining work:

1. **Complete manual terminal validation.**
   - Run the full Go tests, vet, and CLI build.
   - Manually resize Windows Terminal horizontally and vertically while
     browsing, filtering, inspecting detail, typing, streaming, and toggling
     fullscreen.
2. **Keep the terminal contract aligned.**
   - `feat.md`, `internal/ui/README.md`, the README, and CLI docs now describe
     `mimir tui` as the adaptive AltScreen exception to compact command surfaces.
   - Update those documents only when behavior changes.

## Operational Follow-ups

- After the first release that attaches `install.sh` and `install.ps1`, switch
  the README and installation guide from the branch bootstrap URLs to the
  release-attached `releases/latest/download` assets.
- Add required-reviewer protection to the existing GitHub `release`
  environment.
- Complete first deployed desktop/TUI verification for Hermes and real
  install, activation, capture, resume, compaction, offline retry, update, and
  uninstall validation for Claude Code, Codex, and Cursor on each supported
  operating system. Include Ask Mimir startup, generated extension loading,
  model switching, streaming, and the Pi-unavailable fallback. Hook
  installation remains staged until doctor observes a matching harness load.
- Define a recommended reconciliation cadence and an explicit policy for stale
  accepted rows and orphaned R2 objects.
- Keep `docs/Spec.md` synchronized with the live Access-protected dashboard and
  session-object behavior. The former Access/mock-backed wording is already
  corrected.

## Parked Decisions

- **Generalized harness provider router** — superseded by
  [`session-lifecycle.md`](session-lifecycle.md). Capture moves to the
  conversation layer (harness plugins reporting to session objects) instead of
  a harness × provider routing matrix. The proxy remains only for API-key
  providers with redirectable base URLs. Do not intercept TLS, impersonate
  OAuth clients, or turn machine tokens into provider credentials.

## Recently Closed

- Made Ask Mimir session-native with selected-session context, private Mimir
  tools, model discovery and switching, streaming conversation state, and a
  dedicated conversation view.
- Made TUI startup resilient: Mimir now loads its generated Pi extension and
  keeps session browsing usable with an actionable status when Pi is absent or
  fails its startup handshake.
- Enrolled newly introduced managed hook artifacts during update without
  weakening receipt ownership or conflict preservation.
- Preserved commit and diff evidence through outcome normalization, OpenCode
  reporting, CLI presentation, and dashboard rendering.
- Shipped receipt-owned Claude Code, Codex, and Cursor hooks, first-class
  session titles, Access-protected live session state, and native macOS and
  Windows CI coverage. Canonical macOS `/var`, `/tmp`, and `/etc` filesystem
  aliases are accepted without allowing user-controlled symlink targets.
