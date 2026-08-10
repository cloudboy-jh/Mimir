# Installation And Ownership

## Canonical Commands

```bash
# Fresh installation or repair
curl -fsSL https://raw.githubusercontent.com/cloudboy-jh/mimir/master/install.sh | sh

# Upgrade a managed installation
mimir update

# Reconcile the installed version
mimir install
```

On Windows, run the release bootstrap from PowerShell:

```powershell
irm https://raw.githubusercontent.com/cloudboy-jh/mimir/master/install.ps1 | iex
```

The bootstrap resolves the latest stable release, verifies its GoReleaser
checksum, and delegates ownership-safe installation to the downloaded binary.
Set `MIMIR_VERSION` to install an explicit release or `MIMIR_INSTALL_DIR` to
select the binary directory. If that directory is outside `PATH`, the bootstrap
prints the exact command needed to add it.

The update receipt distinguishes files installed on disk from plugins loaded by
running harnesses and the Worker bundle deployed to Cloudflare. When plugin
bytes change, restart the named harness and run `mimir doctor` to verify the
active source hash. When the CLI changes, run `mimir deploy` to publish its
embedded Worker bundle; `mimir update` does not deploy automatically.

`go install github.com/cloudboy-jh/mimir/cmd/mimir@latest` only creates a Go
binary. It does not create the Mimir receipt or install integrations. A verified
Mimir binary created that way can be safely adopted by `mimir install` when no
binary owner is already recorded. If a receipt already owns another binary,
invoking `mimir install` from a different non-temporary executable repairs
managed artifacts but preserves the existing binary owner.

Setup and deploy use the Worker embedded in the running binary by default.
They never discover source from the current checkout or Go module cache.
Development or arbitrary Worker source is used only when `--worker-dir` is
provided explicitly.

## Receipt

Managed ownership lives in `$MIMIR_HOME/install-receipt.json`, defaulting to
`~/.mimir/install-receipt.json`. Operations append to
`$MIMIR_HOME/install-log.jsonl`.

Mimir:

- creates absent opted-in files;
- adopts byte-identical Mimir files;
- migrates byte-exact known historical artifacts;
- updates only receipt-owned files whose bytes are unchanged;
- preserves unknown, modified, missing, symlinked, and non-regular paths;
- removes only verified owned artifacts and binaries;
- never rewrites general OpenCode JSON/JSONC.

The receipt-owned binary is the command published to integrations. Ownership is
adopted only for an exact receipt-owned target, a binary copied to the intended
install target, or a verified Mimir executable when the receipt has no owner.
Invoking installation through another executable cannot silently transfer it.

## Harnesses

OpenCode receives the managed capture plugin and skills. Claude Code receives a
managed skills-directory plugin under `~/.claude/skills/mimir/`. Codex and
Cursor receive managed `~/.codex/hooks.json` and `~/.cursor/hooks.json` files. `CLAUDE_CONFIG_DIR`
and `CODEX_HOME` relocate the corresponding official user homes. Cursor does not
document a user-home override, so Mimir uses `~/.cursor`. Their command
hooks pair supported prompt and completion payloads into reconstructed
exchanges and report lifecycle events. Hermes receives its plugin, skills, and
bounded managed OpenRouter route; the plugin is enabled through the Hermes CLI
only after every managed plugin file is safe.

Mimir creates, adopts, or updates those hook files only under the receipt rules
above. A pre-existing different `hooks.json` is a conflict and is preserved;
Mimir does not merge or replace arbitrary harness hook configuration. Hook
delivery uses the receipt-owned `mimir _hook` adapter, a bounded private local
outbox, and the existing machine connection. Prompt/response queue and pairing
state is authenticated-encrypted at rest with a receipt-adjacent local storage
key that survives machine-token rotation; existing plaintext entries are
migrated before delivery.

When Mimir is already connected, `mimir install` also repairs Hermes through the
normal lifecycle: it enables the safe plugin, authorizes Hermes' existing
OpenRouter credential, and installs the managed OpenRouter route. When Mimir is
not connected, installation only enables the safe plugin; it does not create a
connection or enroll credentials. `mimir setup` and `mimir login` continue to
refresh only an existing managed installation and never enroll artifacts on
their own.

Claude Code can apply plugin hook changes with `/reload-plugins`; Cursor watches
and hot-reloads `hooks.json`. Other harness changes require the harness-specific
activation action reported by `mimir doctor`.

## Uninstall

```bash
mimir uninstall
mimir uninstall --keep-binary
```

Uninstall preserves the connection, machine token, materialized Worker,
install log, and Cloudflare deployment. It disables Hermes only when the
receipt proves Mimir owns the Hermes plugin. On Windows, removal of the running
verified binary is deferred until the process exits; updates use the same
deferred mechanism when the binary is locked by running Mimir processes,
recording `$MIMIR_HOME/pending-update.json` and completing the swap after
they exit (or immediately with `mimir update --force`). Independently of plugin
ownership, uninstall removes an exact Mimir-managed Hermes `.env` route block
without touching `OPENROUTER_API_KEY`; malformed, modified, symlinked, or
non-regular `.env` state is preserved.
