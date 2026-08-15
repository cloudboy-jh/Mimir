# Terminal Output

Terminal presentation has one-way dependencies:

```text
mimircli -> selector / receipts / lineoutput -> appframe -> bentotui
```

- `bentotui/` owns dependency-free ANSI-aware width handling, themes, and
  static rendering utilities.
- `appframe/` owns bounded static human formatting.
- `lineoutput/` owns semantic append-only output for operational commands.
- `receipts/` maps canonical session receipts into bounded static CLI output.
- `selector/` owns bounded human-only checklist input and redraw behavior.

Mimir has no terminal application, alternate screen, session browser, or
embedded agent UI. The private dashboard is the visual session surface;
installed agents query Mimir through the skill and machine-readable CLI. A
bounded checklist may use raw input and redraw only for a human command with
TTY input and output. Setup/login use append-only progress plus ordinary secure
prompts.

Command files choose static human or JSON output and map domain events. Domain
packages must not import UI packages.

## Operational output contract

- Operational human output is append-only: one semantic event per line, no
  redraws, alternate screen, cursor control, or raw subprocess JSON. Bounded
  human-only selectors are the sole redraw exception.
- `==>` marks phases, `OK` completion, `WARN` actionable warnings, `FAIL`
  failures, and `NEXT` required commands. ANSI color is decoration only.
- Color follows terminal capability detection and honors `NO_COLOR`,
  `TERM=dumb`, and redirected output.
- `--json` emits one machine-readable document and no human progress or ANSI.
- Human errors contain a concise message and command-level remediation.
