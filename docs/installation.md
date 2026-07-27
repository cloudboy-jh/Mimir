# Installation And Ownership

## Canonical Commands

```bash
# Fresh installation or repair
go run github.com/cloudboy-jh/mimir/cmd/mimir@latest install

# Upgrade a managed installation
mimir update

# Reconcile the installed version
mimir install
```

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

OpenCode receives the managed plugin and skills. The plugin injects the
receipt-owned optional MCP command at startup. Hermes receives its plugin,
skills, and bounded managed OpenRouter route; the plugin is enabled through the
Hermes CLI only after every managed plugin file is safe.

When Mimir is already connected, `mimir install` also repairs Hermes through the
normal lifecycle: it enables the safe plugin, authorizes Hermes' existing
OpenRouter credential, and installs the managed OpenRouter route. When Mimir is
not connected, installation only enables the safe plugin; it does not create a
connection or enroll credentials. `mimir setup` and `mimir login` continue to
refresh only an existing managed installation and never enroll artifacts on
their own.

Restart a harness after its plugin, skills, or injected configuration changes.

## Uninstall

```bash
mimir uninstall
mimir uninstall --keep-binary
```

Uninstall preserves the connection, machine token, materialized Worker,
install log, and Cloudflare deployment. It disables Hermes only when the
receipt proves Mimir owns the Hermes plugin. On Windows, removal of the running
verified binary is deferred until the process exits. Independently of plugin
ownership, uninstall removes an exact Mimir-managed Hermes `.env` route block
without touching `OPENROUTER_API_KEY`; malformed, modified, symlinked, or
non-regular `.env` state is preserved.
