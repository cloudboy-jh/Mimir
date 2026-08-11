# Next Steps

This file tracks concrete unfinished work and operational decisions. Completed
architecture-transition work belongs in git history and the implementation
specification rather than an expanding completion log.

## Active Implementation Work

### Binary-first onboarding

Land the remaining onboarding work as one reviewable commit:

1. **`docs(onboarding): rewrite setup and recovery guidance`**
   - Lead with the binary installers and local demo, then describe fresh
     deployment and existing-deployment connection separately.
   - Split prerequisites by workflow and document capture fidelity, current
     Cloudflare free-tier units, Access configuration, harness reload behavior,
     and the first real-session verification flow accurately.
   - Add concise after-setup and troubleshooting paths. Keep `mimir doctor`
     read-only and direct users to its exact repair commands.

OpenRouter optionality, meaningful `setup --quick` behavior, and Durable Object
retention cleanup are separate follow-ups because they change setup or session
lifecycle contracts. Do not fold them into these two commits.

### Adaptive full-terminal Mimir TUI

The adaptive AltScreen transition, measured full-terminal layout, centered
sessions-and-Ask-Mimir home surface, state-preserving reload behavior, and
small-terminal fallback are implemented. Ask Mimir now opens a dedicated
conversation view, carries selected-session context into prompts, streams Pi
events, exposes Mimir tools and model selection, and leaves session browsing
available when Pi cannot start. The remaining work is terminal hardening and
cross-platform validation.

The compact `80x20` constraint remains appropriate for temporary setup, deploy,
login, and lightweight session-browser surfaces, but it wastes space and
complicates the persistent sessions-and-Pi application. Install and update use
plain line-oriented output; `mimir tui` uses the measured terminal dimensions
and AltScreen lifecycle.

Remaining work:

1. **Finish deterministic resize coverage.**
   - Existing tests verify bounded output at `48x12`, `80x20`, `120x40`, and
     `200x60`; add complete-frame assertions at each size.
   - Exercise large-to-small, small-to-large, supported-to-unsupported-to-supported,
     and home-to-fullscreen-to-resized-to-home transitions.
   - Verify no stale lines, wrapping, broken borders, or displaced footer.
   - Verify selection, filters, input, scrolling, overlays, and streaming
     responses survive every transition.
   - Add terminal transcript tests for AltScreen entry/exit, home, clear,
     wrapping disable/restore, mouse cleanup, and failure cleanup.
2. **Harden the resize/render cycle.**
   - Coalesce rapid dimension changes so resize, API updates, and Pi events do
     not cause redundant redraw storms.
   - On a size change, invalidate the old frame, move home, clear AltScreen, and
     render one complete frame using only the new dimensions.
   - Disable automatic terminal wrapping while active and restore it on exit.
3. **Complete manual terminal validation.**
   - Run the full Go tests, vet, and CLI build.
   - Manually resize Windows Terminal horizontally and vertically while
     browsing, filtering, inspecting detail, typing, streaming, and toggling
     fullscreen.
4. **Keep the terminal contract aligned.**
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
