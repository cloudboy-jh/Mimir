---
name: mimir
description: Agent memory for the repo (code index/recall) and the session (save/restore via private git). Use when setting up Mimir, saving progress across machines, continuing work, or recalling repository structure. Install via skills CLI - do not clone Mimir just to use it.
---

# Mimir Skill

Hermes remembers the **developer**. Mimir remembers the **repo** and the **session**.

Users talk to you. You drive Mimir. Config + log exist so humans can audit.

## Install (this is how agents get Mimir)

**Install the skill. Do not clone the product repo to "set up" Mimir.**

```bash
npx skills add <repository-source>@mimir -g -y
```

That is enough for session + control procedures (git + `gh` + skill). Reload the harness if your agent only picks up skills on start.

**PromptScript** rejects global installs (`-g`). Install it project-scoped instead:

```bash
npx skills add <repository-source>@mimir -a promptscript -y
```

### Optional binary (code plane only)

Install the `mimir` CLI when you need index / recall / MCP in a repo:

```bash
go install github.com/cloudboy-jh/mimir/cmd/mimir@latest
```

Ensure `$(go env GOPATH)/bin` is on PATH so `mimir` resolves.

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

Without binary: run the same steps with `gh` + `git clone/pull` yourself and write config. Exact file formats (config.toml, session frontmatter, log line, manual flow) are in [`references/formats.md`](references/formats.md).

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

## Daily user speech & Agent Execution Playbook

This contains the mapping from human natural language expressions to exact agent actions.

| User Intent | Trigger Condition | Step-by-Step Agent Action |
|---|---|---|
| **Save / Checkpoint** | "Save progress", "checkpoint", "save my session" | 1. Parse current project details from context.<br>2. Run `mimir session push --id <slug> --harness hermes --goal "<what-was-done>" --body <notes-path-or-text>` (always commit progress to private git).<br>3. Emit the locked `◆ mimir session.push  <id>` receipt back to chat. |
| **Continue / Restore** | "Continue yesterday", "restore workstation", "pull sesh" | 1. Run `mimir session pull` to pull latest markdown trackers from your private sync remote.<br>2. List sessions using `mimir session list`. <br>3. Open and locate the matching `{machine}-{harness}-{session_id}.md` file to feed goal and context back into active chat layers.<br>4. Emit `◆ mimir session.pull  <id>` receipt. |
| **Index Repository** | "index this", "add memory for code", "wire workspace" | 1. Ensure `mimir` CLI binary exists.<br>2. Run `mimir index` (or parse `--full` if `.mimir` directory is missing entirely).<br>3. Print compiler result + `◆ mimir code.index <repo> <mode>` receipt. |
| **Query Code** | "recall symbol", "where is X", "what is Y" | 1. Resolve query strings.<br>2. Run `mimir recall "<query-text>"`. <br>3. Format hit lists neatly under `◆ mimir code.recall <query>`. |

Fill session bodies for real: goal, state, progress, context. Not empty templates. When creating notes or checkpoint descriptors, inspect your environment's local change logs (`git status`, recent diff titles, etc.) to flesh out the narrative, ensuring the next machine can restore immediately.

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
- Do not ask for anything the environment already has (`gh` login, hostname, harness).
- **Code Plane Identity details repo-only**: The code plane represents repositories as the absolute unit of truth (`repo`), never high-level "projects". CLI index configurations store, load, and transmit `"repo"` metadata (the basename of the git root directory) rather than `"project"` tags. Modifying active repositories must use GitTrix ephemeral workouts for dry-runs.
- No second brand (Chiron). No TUI. No SaaS. Sessions never under project `.mimir/`.

---

## Safety boundary

What this skill does and does not touch. Everything is local + your own private git.

**Reads:** `gh` auth identity, hostname, cwd repo name, files in the current repo (for indexing only when the binary is used).

**Writes:**
- `~/.mimir/` — `config.toml`, `mimir.log`, and `sessions/` clone.
- One **private** GitHub repo `mimir-sessions` under your own account (created only if missing).
- `<repo>/.mimir/` — code index, gitignored, only when you run `mimir index`.

**Never:**
- Stores tokens or secrets (auth comes from your existing `gh` / git credential helper).
- Creates or pushes to public repos.
- Uses an application monorepo as the session remote.
- Deletes your files, force-pushes, or touches paths outside `~/.mimir/` and the current repo's `.mimir/`.
- Sends telemetry or calls a Mimir backend (there is none).

Commands the agent may run: `git` (clone/pull/add/commit/push within `~/.mimir/sessions`), `gh` (auth check, repo view/create), `mimir` (local CLI), `go install` (only if you opt into the binary).
