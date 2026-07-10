# Mimir

![Mimir](./mimir-readme.png)

Agent-driven repository and session memory.

Mimir acts as local infrastructure for software development agents, providing structured session backups and zero-bloat repository indexing.

---

## How it Works

| Dimension | Scope | Backing Storage | Sync Channel |
| :--- | :--- | :--- | :--- |
| **Session** | Narrative work context (Goals, Progress, Meta) | `~/.mimir/sessions/sessions/` | Private Git (`mimir-sessions`) |
| **Repo** | Code syntax index (Symbols, Imports, Dependencies) | `<repo>/.mimir/` | Local / Gitignored |

---

## Workspace Dry-Runs

Before executing files edits or code modifications on durable assets, run them inside an isolated workspace using GitTrix:
1. Start an ephemeral sandbox: `gittrix session start "<task>" "/local/repo" <branch>`
2. Work directly in the transient subdirectory.
3. Review changes cleanly: `gittrix session diff`
4. Promote back to long-term storage only after verification: `gittrix promote`

---

## Daily UX

Your agent handles all Mimir operations in natural language inside chat.

*   **Set up Mimir** -> Initializes configuration and hooks your private sync repo.
*   **Save progress** -> Writes the active work narrative and commits it to your private git.
*   **Continue yesterday** -> Pulls latest sessions and restores the open context.
*   **Index this repo** -> Compiles local codebase charts for prompt-free symbol lookup.
*   **Recall <query>** -> Queries local symbol signatures instantly.

---

## Install (Agent-driven)

Paste this directly to your agent to install and wire Mimir:

```text
Install and wire Mimir for me.

1. Install the core skill (not a product clone):
   npx skills add cloudboy-jh/Mimir@mimir -g -y
2. Load skill and execute bootstrap: control init -> session init.
3. Automatically configure private sync under your authenticated GitHub (login/mimir-sessions).
4. Print a ◆ mimir control.init receipt and run status/doctor.
```

If you prefer custom scoping, install the skill locally:

```bash
npx skills add cloudboy-jh/Mimir@mimir -a promptscript -y
```

Installing the skill is sufficient for session sync (Git + GitHub). Go CLI indexing binary is optional:
```bash
go install github.com/cloudboy-jh/mimir/cmd/mimir@latest   # ensure $(go env GOPATH)/bin is on PATH
```

---

## Receipts (Locked UX)

Every action logs a thin, standardized receipt in terminal chat:

```text
◆ mimir  session.push  workstation-a-hermes-core-v2
         goal: split adapters
         ok · abc1234
```

Durable traces are recorded in `~/.mimir/mimir.log`. Core index binaries do not leak telemetry.

---

## Contributor Manual

To hack on the Mimir Go CLI engine directly:

```bash
git clone https://github.com/cloudboy-jh/mimir.git
cd mimir
go test ./cmd/mimir/...
go build -o mimir ./cmd/mimir
```
