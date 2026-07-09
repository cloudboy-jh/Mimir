---
name: mimir
description: Agent memory for the repo (code index/recall) and the session (save/restore via private git). Use when setting up Mimir, saving progress across machines, continuing work, or recalling repository structure. Install via skills CLI - do not clone Mimir just to use it.
---

# Mimir Skill

Hermes remembers the **developer**. Mimir remembers the **repo** and the **session**.

Users talk to you. You drive Mimir. Config + log exist so humans can audit.

## Install (this is how agents get Mimir)

Same pattern as Cloudflare / docs skills. **Install the skill. Do not clone the product repo to "set up" Mimir.**

```bash
npx skills add cloudboy-jh/Mimir@mimir -g -y
```

That is enough for session + control procedures (git + `gh` + skill). Reload the harness if your agent only picks up skills on start.

### Optional binary (code plane only)

Install the `mimir` CLI when you need index / recall / MCP in a repo:

```bash
go install github.com/cloudboy-jh/mimir/cmd/mimir@latest
```

If that path fails (old tree), build without cloning for long-term use after a short fetch:

```bash
GOBIN="$(go env GOPATH)/bin"
go install github.com/cloudboy-jh/mimir/cmd/mimir@latest
# requires GOBIN on PATH as `mimir`
```

Contributors working **on** Mimir itself use the product checkout. End users and agents using Mimir as infrastructure should only need the skill (+ optional binary).

---

## Planes

| plane | root | how |
|---|---|---|
| control | `~/.mimir/` | `config.toml` + `mimir.log` |
| session | `~/.mimir/sessions/` | git markdown; private `mimir-sessions` via `gh` |
| code | `<repo>/.mimir/` | binary CLI / MCP |

Session push does **not** need the binary or MCP. Control + sessions work with skill + git + `gh`.

---

## Bootstrap (after skill is installed)

### Control (once per machine)

```bash
mimir control init
```

If binary missing: create `~/.mimir/`, write `config.toml` with `machine` = short hostname (aliases `therig` / `thedeck` when hostname matches), ensure `mimir.log` exists. Same outcome.

### Session (once per person)

```bash
mimir session init
```

**Do not ask for a session GitHub URL.** Discovery:

1. `sessions.repo` in config → clone/pull `~/.mimir/sessions`
2. `gh api user` → login
3. private `login/mimir-sessions` create-or-clone
4. write config, enable sessions

Refuse: public remotes, app monorepos as session remotes, tokens in config.

Only hand-prompt when: no `gh` auth, multi-account ambiguity (one short choice), or explicit custom remote.

Without binary: run the same steps with `gh` + `git clone/pull` yourself and write config.

### Code (optional, per repo you want indexed)

Binary required.

```bash
mimir index --full
mimir recall "query"
```

MCP for the **active** harness only:

```json
{
  "mcpServers": {
    "mimir": {
      "command": "mimir",
      "args": ["serve"]
    }
  }
}
```

---

## Daily user speech

| User says | You do |
|---|---|
| install / wire Mimir | `npx skills add cloudboy-jh/Mimir@mimir -g -y` then control + session init |
| save progress / checkpoint | session write + push; always `◆ mimir` receipt |
| continue from X | session pull; open matching `*.md` |
| what do we know about Y | `mimir recall` / MCP |
| mid coding structure | MCP quiet unless user needs an answer |

Fill session bodies for real: goal, state, progress, context. Not empty templates.

---

## Receipts (locked)

User-facing plane events lead with:

```text
◆ mimir  <plane>.<verb>  <subject>
         <optional meaning>
         <ok|warn|fail> · <metric>
```

Failures: `fail` + reason + `log: ~/.mimir/mimir.log`.  
Never dump `index.json` into chat.

---

## Session CLI (scaffolding; users never need this vocabulary)

```bash
mimir session push --id <slug> --harness <name> --project <name> --goal "..." --body notes.md
mimir session pull
mimir session list
mimir doctor
```

Filename: `{machine}-{harness}-{session_id}.md`

---

## Hard rules

- Skill install != product clone.
- Do not web-search for "how to install Mimir." Use this skill + the install block above.
- Do not ask for anything the environment already has (`gh` login, hostname, harness, project name).
- No second brand (Chiron). No TUI. No SaaS. Sessions never under project `.mimir/`.
