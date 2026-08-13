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

  cpSync(join(root, "migrations", "0008_request_kind.sql"), join(migrations, "0008_request_kind.sql"));
  applyMigrations();

  cpSync(join(root, "migrations", "0009_hermes_credentials.sql"), join(migrations, "0009_hermes_credentials.sql"));
  cpSync(join(root, "migrations", "0010_harness_loads.sql"), join(migrations, "0010_harness_loads.sql"));
  cpSync(join(root, "migrations", "0011_session_hierarchy.sql"), join(migrations, "0011_session_hierarchy.sql"));
  applyMigrations();
  execute(`
    INSERT INTO hermes_credentials(token_hash, created_at, authorized_by)
    VALUES ('hermes-hash', '2026-07-15T12:00:00Z', 'migration-test');
    INSERT INTO harness_loads(token_hash, token_label, harness, artifact_sha256, installation_id, client_loaded_at, reported_at)
    VALUES ('machine-hash', 'migration-test', 'opencode', '${"a".repeat(64)}', 'install-1', '2026-07-15T12:00:00Z', '2026-07-15T12:00:00Z');
  `);

  cpSync(join(root, "migrations", "0012_session_titles.sql"), join(migrations, "0012_session_titles.sql"));
  cpSync(join(root, "migrations", "0013_generic_harness_loads.sql"), join(migrations, "0013_generic_harness_loads.sql"));
  applyMigrations();
  execute(`
    INSERT INTO harness_loads(token_hash, token_label, harness, artifact_sha256, installation_id, client_loaded_at, reported_at)
    VALUES ('machine-hash', 'migration-test', 'claude-code', '${"b".repeat(64)}', 'install-1', '2026-07-15T12:01:00Z', '2026-07-15T12:01:00Z');
    INSERT INTO sessions(id, started_at, boundary, state, last_active_at)
    VALUES ('null-activity-session', '2026-07-15T12:02:00Z', 'header', 'inactive', NULL);
  `);

  cpSync(join(root, "migrations", "0014_session_activity_order.sql"), join(migrations, "0014_session_activity_order.sql"));
  applyMigrations();

  cpSync(join(root, "migrations", "0015_device_identity.sql"), join(migrations, "0015_device_identity.sql"));
  applyMigrations();
  execute(`
    INSERT INTO machines(installation_id, name, platform, arch, created_at, updated_at)
    VALUES ('install-1', 'Migration device', 'linux', 'amd64', '2026-07-15T12:03:00Z', '2026-07-15T12:03:00Z');
    UPDATE access_tokens SET installation_id = 'install-1' WHERE token_hash = 'machine-hash';
    UPDATE sessions SET installation_id = 'install-1' WHERE id = 'null-activity-session';
    UPDATE hermes_credentials SET installation_id = 'install-1' WHERE token_hash = 'hermes-hash';
    INSERT INTO hermes_credentials(token_hash, created_at, authorized_by)
    VALUES ('legacy-hermes-hash', '2026-07-15T12:05:00Z', 'migration-test');
  `);

  cpSync(join(root, "migrations", "0016_scoped_hermes_credentials.sql"), join(migrations, "0016_scoped_hermes_credentials.sql"));
  applyMigrations();
  execute(`
    INSERT INTO hermes_credentials(token_hash, installation_id, created_at, authorized_by)
    VALUES ('hermes-hash', 'install-2', '2026-07-15T12:04:00Z', 'migration-test');
    INSERT INTO machines(installation_id, name, platform, arch, created_at, updated_at, revoked_at)
    VALUES ('revoked-install', 'Dashboard name', 'linux', 'amd64', '2026-07-15T12:06:00Z', '2026-07-15T12:06:00Z', '2026-07-15T12:07:00Z');
    INSERT INTO access_tokens(token_hash, label, installation_id, created_at, revoked_at)
    VALUES ('revoked-token', 'Dashboard name', 'revoked-install', '2026-07-15T12:06:00Z', '2026-07-15T12:07:00Z');
    INSERT INTO machines(installation_id, name, platform, arch, created_at, updated_at)
    VALUES ('active-install', 'Active dashboard name', 'linux', 'amd64', '2026-07-15T12:06:00Z', '2026-07-15T12:06:00Z');
    INSERT INTO access_tokens(token_hash, label, installation_id, created_at, revoked_at)
    VALUES ('revoked-active-token', 'Active dashboard name', 'active-install', '2026-07-15T12:06:00Z', '2026-07-15T12:07:00Z');
    INSERT INTO machines(installation_id, name, platform, arch, created_at, updated_at)
    VALUES ('revoked-install', 'Deployment hostname', 'darwin', 'arm64', '2026-07-15T12:08:00Z', '2026-07-15T12:08:00Z')
    ON CONFLICT(installation_id) DO UPDATE SET platform = excluded.platform, arch = excluded.arch, updated_at = excluded.updated_at
    WHERE machines.revoked_at IS NULL;
    INSERT INTO access_tokens(token_hash, label, installation_id, created_at)
    SELECT 'new-random-token', 'Deployment hostname', 'revoked-install', '2026-07-15T12:08:00Z'
    WHERE EXISTS (SELECT 1 FROM machines WHERE installation_id = 'revoked-install' AND revoked_at IS NULL)
    ON CONFLICT(token_hash) DO UPDATE SET label = excluded.label, installation_id = excluded.installation_id
    WHERE access_tokens.installation_id IS NULL OR access_tokens.installation_id = excluded.installation_id;
    INSERT INTO machines(installation_id, name, platform, arch, created_at, updated_at)
    VALUES ('active-install', 'Deployment hostname', 'darwin', 'arm64', '2026-07-15T12:08:00Z', '2026-07-15T12:08:00Z')
    ON CONFLICT(installation_id) DO UPDATE SET platform = excluded.platform, arch = excluded.arch, updated_at = excluded.updated_at
    WHERE machines.revoked_at IS NULL;
    INSERT INTO access_tokens(token_hash, label, installation_id, created_at)
    SELECT 'revoked-active-token', 'Deployment hostname', 'active-install', '2026-07-15T12:08:00Z'
    WHERE EXISTS (SELECT 1 FROM machines WHERE installation_id = 'active-install' AND revoked_at IS NULL)
    ON CONFLICT(token_hash) DO UPDATE SET label = excluded.label, installation_id = excluded.installation_id
    WHERE access_tokens.installation_id IS NULL OR access_tokens.installation_id = excluded.installation_id;
    INSERT INTO access_tokens(token_hash, label, installation_id, created_at)
    SELECT 'revoked-token', 'Deployment hostname', 'revoked-install', '2026-07-15T12:08:00Z'
    WHERE EXISTS (SELECT 1 FROM machines WHERE installation_id = 'revoked-install' AND revoked_at IS NULL)
    ON CONFLICT(token_hash) DO UPDATE SET label = excluded.label, installation_id = excluded.installation_id
    WHERE access_tokens.installation_id IS NULL OR access_tokens.installation_id = excluded.installation_id;
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
      (SELECT request_kind FROM exchanges WHERE id = 'deployment-window-exchange') AS window_request_kind,
      (SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name IN ('exchange_files', 'exchange_errors')) AS facet_tables,
      (SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'hermes_credentials') AS hermes_credentials_table,
      (SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'harness_loads') AS harness_loads_table,
      (SELECT COUNT(*) FROM pragma_table_info('sessions') WHERE name = 'parent_session_id') AS session_parent_column,
      (SELECT COUNT(*) FROM pragma_table_info('sessions') WHERE name IN ('title', 'title_source', 'title_updated_at')) AS session_title_columns,
      (SELECT COUNT(*) FROM pragma_table_info('exchanges') WHERE name = 'title_candidate') AS exchange_title_column,
      (SELECT artifact_sha256 FROM harness_loads WHERE token_hash = 'machine-hash' AND harness = 'opencode') AS harness_artifact_sha256,
      (SELECT COUNT(*) FROM harness_loads WHERE token_hash = 'machine-hash') AS harness_load_count,
      (SELECT last_active_at FROM sessions WHERE id = 'null-activity-session') AS backfilled_activity_at,
       (SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = 'sessions_parent_last_active') AS activity_index,
       (SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'machines') AS machines_table,
       (SELECT installation_id FROM sessions WHERE id = 'null-activity-session') AS session_installation_id,
       (SELECT MIN(installation_id) FROM hermes_credentials WHERE token_hash = 'hermes-hash') AS hermes_installation_id,
       (SELECT COUNT(*) FROM hermes_credentials WHERE token_hash = 'hermes-hash') AS hermes_installation_count,
       (SELECT COUNT(*) FROM hermes_credentials WHERE token_hash = 'legacy-hermes-hash') AS legacy_hermes_count,
       (SELECT COUNT(*) FROM pragma_table_info('hermes_credentials') WHERE pk > 0) AS hermes_primary_key_columns,
       (SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = 'sessions_one_active_heuristic' AND sql LIKE '%installation_id%') AS heuristic_installation_key,
       (SELECT name FROM machines WHERE installation_id = 'revoked-install') AS revoked_machine_name,
       (SELECT revoked_at FROM machines WHERE installation_id = 'revoked-install') AS machine_revoked_at,
       (SELECT revoked_at FROM access_tokens WHERE token_hash = 'revoked-token') AS token_revoked_at,
       (SELECT COUNT(*) FROM access_tokens WHERE token_hash = 'new-random-token') AS new_revoked_install_token_count,
       (SELECT name FROM machines WHERE installation_id = 'active-install') AS active_machine_name,
       (SELECT platform FROM machines WHERE installation_id = 'active-install') AS active_machine_platform,
       (SELECT revoked_at FROM access_tokens WHERE token_hash = 'revoked-active-token') AS active_machine_token_revoked_at
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
    window_request_kind: "primary",
    facet_tables: 2,
    hermes_credentials_table: 1,
    harness_loads_table: 1,
    session_parent_column: 1,
    session_title_columns: 3,
    exchange_title_column: 1,
    harness_artifact_sha256: "a".repeat(64),
    harness_load_count: 2,
    backfilled_activity_at: "2026-07-15T12:02:00Z",
    activity_index: 1,
    machines_table: 1,
    session_installation_id: "install-1",
    hermes_installation_id: "install-1",
    hermes_installation_count: 2,
    legacy_hermes_count: 0,
    hermes_primary_key_columns: 2,
    heuristic_installation_key: 1,
    revoked_machine_name: "Dashboard name",
    machine_revoked_at: "2026-07-15T12:07:00Z",
    token_revoked_at: "2026-07-15T12:07:00Z",
    new_revoked_install_token_count: 0,
    active_machine_name: "Active dashboard name",
    active_machine_platform: "darwin",
    active_machine_token_revoked_at: "2026-07-15T12:07:00Z",
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
