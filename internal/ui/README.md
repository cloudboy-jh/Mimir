# Terminal UI

The terminal UI has one-way dependencies:

```text
mimircli -> sessions / operations / receipts -> appframe -> bentotui
```

- `bentotui/` owns dependency-free terminal input, configurable inline or alternate-screen lifecycle,
  ANSI-aware width handling, themes, and low-level rendering primitives.
- `appframe/` owns Mimir's interactive shells: compact command surfaces use an
  anchored frame with a preferred size of 80x20 and a minimum size of 48x12;
  the persistent terminal uses the measured full terminal dimensions. It owns
  headers, footers, binding layout, viewport dimensions, and static rendering.
- `sessions/` owns session list, filtering, detail, and session key handling.
- `mimirtui/` owns the persistent centered home surface, sessions, Ask Mimir
  input and scrollback, command/model/theme overlays, outcome prompts, and the
  private agent-runtime event mapping.
- `operations/` owns deploy, update, setup, install, and login progress,
  bounded command output, scrolling, follow mode, cancellation, and the stable
  line-oriented fallback.
- `receipts/` maps canonical session receipts into static CLI and session
  browser models.

Interactive frames always begin at terminal row zero and column zero. Compact
frames never center, indent, add margins, grow beyond 80x20, add nested boxes,
or put scroll ranges in the divider. The persistent `mimir tui` frame is the
AltScreen exception and consumes the measured terminal dimensions. Content
scrolls inside the body.

Command files choose TUI, static, or JSON mode and map domain events. They must
not implement borders, dimensions, key handling, wrapping, or ANSI behavior.
Domain packages must not import UI packages.

To add a stateful command surface:

1. Put its model, view, keys, and tests in a focused directory under this one.
2. Render through `appframe.Frame` and `appframe.Footer`.
3. Keep no more than four contextual footer actions.
4. Preserve static output for pipes, CI, small terminals, and JSON mode.
5. Add exact 80x20 and 48x12 rendering tests.
