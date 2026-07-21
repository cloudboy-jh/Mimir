import assert from "node:assert/strict";
import { cpSync, mkdtempSync, mkdirSync, readdirSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join, resolve } from "node:path";
import { fileURLToPath } from "node:url";
import { spawnSync } from "node:child_process";

const root = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const wrangler = join(root, "node_modules", "wrangler", "bin", "wrangler.js");
const temp = mkdtempSync(join(tmpdir(), "mimir-migrations-"));
const migrations = join(temp, "migrations");
const persistence = join(temp, "state");
const config = join(temp, "wrangler.jsonc");

function run(args, capture = false) {
  const result = spawnSync(process.execPath, [wrangler, ...args, "--config", config], {
    cwd: root,
    encoding: "utf8",
    stdio: capture ? ["ignore", "pipe", "inherit"] : "inherit",
  });
  if (result.status !== 0) {
    throw new Error(`wrangler ${args.join(" ")} failed with status ${result.status}`);
  }
  return result.stdout;
}

function applyMigrations() {
  run(["d1", "migrations", "apply", "mimir-migration-test", "--local", "--persist-to", persistence]);
}

function execute(command, json = false) {
  const args = ["d1", "execute", "mimir-migration-test", "--local", "--persist-to", persistence, "--command", command];
  if (json) args.push("--json");
  return run(args, json);
}

try {
  mkdirSync(migrations);
  for (const name of readdirSync(join(root, "migrations")).filter((name) => /^000[1-6]_.*\.sql$/.test(name))) {
    cpSync(join(root, "migrations", name), join(migrations, name));
  }
  writeFileSync(
    config,
    JSON.stringify({
      name: "mimir-migration-test",
      compatibility_date: "2026-07-15",
      d1_databases: [
        {
          binding: "DB",
          database_name: "mimir-migration-test",
          database_id: "00000000-0000-0000-0000-000000000000",
          migrations_dir: "./migrations",
        },
      ],
    }),
  );

  applyMigrations();
  execute(`
    INSERT INTO sessions(id, started_at, ended_at, boundary, outcome, outcome_src, state, last_active_at, inactive_at)
    VALUES ('legacy-session', '2026-07-15T10:00:00Z', '2026-07-15T11:00:00Z', 'header', 'promoted', 'explicit', 'inactive', '2026-07-15T11:00:00Z', '2026-07-15T11:00:00Z');
    INSERT INTO exchanges(id, session_id, ts, endpoint, latency_ms, r2_key)
    VALUES ('legacy-exchange', 'legacy-session', '2026-07-15T10:30:00Z', '/v1/chat/completions', 25, 'log/legacy.json');
  `);

  cpSync(join(root, "migrations", "0007_storage_contract.sql"), join(migrations, "0007_storage_contract.sql"));
  applyMigrations();
  execute(`
    INSERT INTO exchanges(id, session_id, ts, endpoint, latency_ms, r2_key)
    VALUES ('deployment-window-exchange', 'legacy-session', '2026-07-15T11:01:00Z', '/v1/chat/completions', 20, 'log/deployment-window.json');
  `);

  const output = JSON.parse(execute(`
    SELECT
      s.work_outcome,
      s.outcome_src,
      s.outcome_updated_at,
      e.capture_status,
      e.capture_reason,
      e.accepted_at,
      e.saved_at,
      e.schema_version,
      o.outcome AS event_outcome,
      o.source AS event_source,
      o.evidence_json,
      (SELECT capture_status FROM exchanges WHERE id = 'deployment-window-exchange') AS window_capture_status,
      (SELECT capture_reason FROM exchanges WHERE id = 'deployment-window-exchange') AS window_capture_reason,
      (SELECT accepted_at FROM exchanges WHERE id = 'deployment-window-exchange') AS window_accepted_at,
      (SELECT saved_at FROM exchanges WHERE id = 'deployment-window-exchange') AS window_saved_at,
      (SELECT schema_version FROM exchanges WHERE id = 'deployment-window-exchange') AS window_schema_version,
      (SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name IN ('exchange_files', 'exchange_errors')) AS facet_tables
    FROM sessions s
    JOIN exchanges e ON e.session_id = s.id
    JOIN session_outcome_events o ON o.session_id = s.id
    WHERE s.id = 'legacy-session' AND e.id = 'legacy-exchange' AND o.source = 'migration';
  `, true));
  const row = output.flatMap((entry) => entry.results ?? [])[0];

  assert.deepEqual(row, {
    work_outcome: "landed",
    outcome_src: "migration",
    outcome_updated_at: "2026-07-15T11:00:00Z",
    capture_status: "saved",
    capture_reason: "legacy_capture",
    accepted_at: "2026-07-15T10:30:00Z",
    saved_at: "2026-07-15T10:30:00Z",
    schema_version: 0,
    event_outcome: "landed",
    event_source: "migration",
    evidence_json: '{"legacy_outcome":"promoted"}',
    window_capture_status: "saved",
    window_capture_reason: "legacy_capture",
    window_accepted_at: "2026-07-15T11:01:00Z",
    window_saved_at: "2026-07-15T11:01:00Z",
    window_schema_version: 0,
    facet_tables: 2,
  });

  execute("UPDATE sessions SET outcome = 'discarded', outcome_src = 'git' WHERE id = 'legacy-session';");
  const compatibilityOutput = JSON.parse(execute(`
    SELECT
      s.work_outcome,
      s.outcome_src,
      s.outcome_reason,
      o.outcome AS event_outcome,
      o.source AS event_source,
      o.reason AS event_reason,
      o.evidence_json,
      (SELECT COUNT(*) FROM session_outcome_events WHERE session_id = s.id) AS event_count
    FROM sessions s
    JOIN session_outcome_events o ON o.session_id = s.id AND o.id LIKE 'legacy-update:%'
    WHERE s.id = 'legacy-session';
  `, true));
  assert.deepEqual(compatibilityOutput.flatMap((entry) => entry.results ?? [])[0], {
    work_outcome: "discarded",
    outcome_src: "git",
    outcome_reason: "Recorded by legacy Worker during deployment",
    event_outcome: "discarded",
    event_source: "git",
    event_reason: "Recorded by legacy Worker during deployment",
    evidence_json: '{"legacy_outcome":"discarded","legacy_source":"git"}',
    event_count: 2,
  });
  console.log("Populated migration verification passed.");
} finally {
  rmSync(temp, { recursive: true, force: true });
}
