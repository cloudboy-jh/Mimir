# Terminal UI

The terminal UI has one-way dependencies:

```text
mimircli -> sessions / operations / receipts -> appframe -> bentotui
```

- `bentotui/` owns dependency-free terminal input, configurable inline or alternate-screen lifecycle,
  ANSI-aware width handling, themes, and low-level rendering primitives.
- `appframe/` owns Mimir's single interactive shell: an anchored frame with a
  preferred size of 80x20 and a minimum size of 48x12. It owns headers,
  footers, binding layout, viewport dimensions, and static product rendering.
- `sessions/` owns session list, filtering, detail, and session key handling.
- `terminal/` owns the persistent split sessions/agent surface, focus, agent
  scrollback, outcome prompts, layout switching, and Pi event mapping.
- `operations/` owns deploy, update, setup, install, and login progress,
  bounded command output, scrolling, follow mode, cancellation, and the stable
  line-oriented fallback.
- `receipts/` maps canonical session receipts into static CLI and session
  browser models.

Interactive frames always begin at terminal row zero and column zero. Never
center, indent, add margins, grow beyond 80x20, add nested boxes, or put scroll
ranges in the divider. Content scrolls inside the fixed body.

Command files choose TUI, static, or JSON mode and map domain events. They must
not implement borders, dimensions, key handling, wrapping, or ANSI behavior.
Domain packages must not import UI packages.

To add a stateful command surface:

1. Put its model, view, keys, and tests in a focused directory under this one.
2. Render through `appframe.Frame` and `appframe.Footer`.
3. Keep no more than four contextual footer actions.
4. Preserve static output for pipes, CI, small terminals, and JSON mode.
5. Add exact 80x20 and 48x12 rendering tests.
