# feat: Mimir terminal TUI

## Context

Mimir has three surfaces today:

- **Web dashboard** — visual browsing, filters, outcome controls. Great for
  deep inspection but requires a browser.
- **CLI verbs** — `mimir list`, `mimir session get`, `mimir search`. Each is a
  one-shot process that prints output and exits. No state between invocations.
  You can't browse through sessions, drill in, and jump back without retyping
  commands.
- **Existing TUI plumbing** — `internal/ui/sessions/browser.go`, `appframe/`,
  and the dependency-free `bentotui/` runtime provide the shared terminal shell.

We need a persistent terminal interface — a browseable, stateful view of
Mimir's data with an embedded agent for natural language querying.

## Design principles

- **Sessions on top, agent below** — the session list is the primary data
  surface. The agent is a persistent query interface underneath it. Both are
  visible at the same time.
- **Stateful navigation** — arrow keys, mouse, or search all update the same
  cursor. No re-invocation.
- **Ask Mimir is context-aware** — the assistant sees what session you're looking at
  and can answer questions about it without you specifying the ID.
- **Two layouts** — split view (default) and fullscreen agent (toggle). No
  modal, no overlap, no third layout.
- **Footer is the only chrome** — anchored at the bottom, shows keybinds and
  status.
- **CLI verbs are replaced** — the TUI absorbs `list`, `session get`,
  `search`, `outcome`. The CLI still works for scripting but the TUI is the
  interactive surface.
- **Theme support** via BentoTUI's existing theme system.

## Layout

Default (split view):

```
┌──────────────────────────────────────────────┐
│ ◆ Sessions (25)                              │
│                                              │
│  [UNRES] buzz eval              hermes  1.4M │
│  [LANDED] dashboard rebuild    opencode 481K │
│  [DISCARDED] buzz acp           hermes    0  │
│  [UNRES] termius setup          hermes  1.9M │
│  [UNRES] perf review            hermes    0  │
│                                              │
├──────────────────────────────────────────────┤
│ ◆ Ask Mimir                                  │
│                                              │
│  > what happened in this session?            │
│                                              │
│  ◆ Session 01KYX...                          │
│    20 exchanges · deepseek-v4-flash          │
│    Capture: Saved · Outcome: Unresolved      │
│                                              │
│  > _                                         │
├──────────────────────────────────────────────┤
│  ↑↓ browse  ↵ detail  /search  z fullscreen  │
└──────────────────────────────────────────────┘
```

Fullscreen Ask Mimir (toggled with `z`):

```
┌──────────────────────────────────────────────┐
│ ◆ Ask Mimir                                  │
│                                              │
│  > show me all sessions from this week       │
│                                              │
│  ◆ 5 sessions this week:                     │
│                                              │
│  Jul 31 — buzz eval (unresolved)             │
│  Jul 29 — dashboard rebuild (landed)         │
│  Jul 28 — CLI terminal UI (discarded)        │
│  Jul 27 — OpenCode capture (landed)          │
│  Jul 26 — domain service split (landed)      │
│                                              │
│  Want me to pull evidence on any of these?   │
│                                              │
│  > _                                         │
├──────────────────────────────────────────────┤
│  z split  /search  q quit                    │
└──────────────────────────────────────────────┘
```

## Interaction model

### Browsing sessions (top panel)

- `↑ ↓` or mouse scroll to move cursor through the session list
- `enter` to toggle expanded detail for the selected session (inline in the
  list or replacing the bottom panel)
- `/` to focus a search bar that filters the list in real-time
- `o` to set outcome (inline prompt: "Landed / Discarded / Abandoned / Unresolved")
- Selected session is highlighted; its ID is the agent's contextual reference

### Ask Mimir (bottom panel)

- Type a question or command at the `> _` prompt
- Mimir knows the current session context (selected ID, search filter)
- Examples:
  - "what's the evidence for this session?"
  - "summarize everything I landed this week"
  - "show me the model switches in this session"
  - "mark this as landed with reason: dashboard shipped"
- Responses render inline with styled blocks (badges, cards, lists)

### Layout switching

- `z` toggles between split view and fullscreen agent
- Split view ratio is adjustable with `ctrl+↑` / `ctrl+↓`

## Technical approach

### Private agent runtime

Pi is private implementation plumbing, not a product surface. Spawn
`pi --mode rpc` as a subprocess through Mimir's standard-library-only
`internal/pi` client:

- Communicate over JSONL stdin/stdout
- Manage lifecycle with BentoTUI's live-update channel (start, send, read loop, stop)
- Soft-pause on Esc in fullscreen mode, hard-stop on Ctrl+C or `/quit`
- Session persistence: pi handles its own session files
- pi is found via PATH — `mimir doctor` validates it's available

### Mimir agent tools

The embedded pi agent is given tools that wrap Mimir's own API:

- `list_sessions(filters)` — query the Worker for sessions
- `get_session(id)` — session detail with evidence, outcomes, model tree
- `search_memory(query)` — full-text search across saved sessions
- `set_outcome(id, outcome, reason, evidence)` — record an outcome
- `doctor_check()` — run doctor diagnostics inline

These are registered as pi custom tools. The agent decides when to call them.
The tools use Mimir's existing worker API — no new backend needed.

### Slash commands

Only what shouldn't hit the agent loop:

- `/model <name>` — switch the pi agent's model
- `/quit` — hard stop and exit
- `/theme` — cycle theme (or open palette)
- `/help` — show available slash commands

### Shell construction

The TUI is a single dependency-free BentoTUI model with two content zones:

```
┌─ sessions/resizable split ──────────────────┐
│  list.Model (session list, scrollable)       │
├─ agent area ────────────────────────────────┤
│  list.Model (agent chat scrollback)          │
│  textinput (agent input prompt)              │
├─ footer ────────────────────────────────────┤
│  bar.Model (keybinds, status)               │
└──────────────────────────────────────────────┘
```

- The split between sessions and agent is a divider that can be dragged or
  key-resized (`ctrl+↑` / `ctrl+↓`)
- Fullscreen agent mode hides the sessions panel entirely
- No AltScreen in the BentoTUI sense — fullscreen is just the agent zone
  expanding to fill the content area

### BentoTUI primitives to use

Provided by `internal/ui/bentotui` and `internal/ui/appframe`:

- `bar` — footer with left/center/right slots and keybind cards
- `list` — session list and agent scrollback
- `card` — inline structured content blocks (session detail, evidence)
- `badge` — outcome badges (LANDED / DISCARDED / UNRESOLVED)
- `surface` — canvas fill and compositing
- `theme` — theme engine, theme cycling, ThemeChangedMsg

### Files to create / modify

```
internal/ui/terminal/
  model.go           # BentoTUI model: sessions list, agent scrollback,
                     # input buffer, footer state, pi process ref, split ratio
  model_test.go
  view.go            # View: compose sessions + agent + footer zones

internal/pi/
  client.go          # PiProcess lifecycle (port from glib internal/pi/client.go)
  jsonl.go           # JSONL record reader (port from glib internal/pi/jsonl.go)

internal/mimircli/
  tui.go             # Command adapter: wire services, tools, theme, and Pi
```

### Setup / first-launch

The TUI depends on `pi` being available on PATH and API keys configured for
the pi agent. `mimir doctor --tui` validates:

1. `pi` binary found (install command if missing)
2. At least one provider env var set (`ANTHROPIC_API_KEY`, `OPENAI_API_KEY`, etc.)

If either check fails, the TUI shows a clear message with the fix instead of
a cryptic error.

## Non-goals

- No mode machine or room switching — two layouts, same model
- No overlap / floating panels
- No AltScreen — user can scroll terminal history after quitting
- No persistent TUI state across restarts — pi handles its own sessions
- No replacing the web dashboard — the TUI and dashboard complement each other
