import {
  createExecutionContext,
  env,
  waitOnExecutionContext,
} from "cloudflare:test";
import { afterEach, beforeAll, beforeEach, vi } from "vitest";
import worker from "../src/index";
import { finalizeAcceptedExchange } from "../src/exchanges/capture-pipeline";

const schema = `
CREATE TABLE machines (installation_id TEXT PRIMARY KEY, name TEXT NOT NULL, platform TEXT NOT NULL, arch TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL, last_seen_at TEXT, revoked_at TEXT);
CREATE TABLE access_tokens (token_hash TEXT PRIMARY KEY, label TEXT NOT NULL, created_at TEXT NOT NULL, last_used_at TEXT, revoked_at TEXT, installation_id TEXT REFERENCES machines(installation_id));
CREATE TABLE hermes_credentials (token_hash TEXT NOT NULL, installation_id TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, authorized_by TEXT, PRIMARY KEY (token_hash, installation_id));
CREATE TABLE harness_loads (token_hash TEXT NOT NULL, token_label TEXT NOT NULL, harness TEXT NOT NULL CHECK (length(harness) BETWEEN 1 AND 64 AND substr(harness, 1, 1) GLOB '[a-z0-9]' AND harness NOT GLOB '*[^a-z0-9._-]*'), artifact_sha256 TEXT NOT NULL CHECK (length(artifact_sha256) = 64 AND artifact_sha256 NOT GLOB '*[^0-9a-f]*'), bundle_version TEXT, cli_version TEXT, cli_commit TEXT, installation_id TEXT NOT NULL DEFAULT '', client_loaded_at TEXT NOT NULL, reported_at TEXT NOT NULL, PRIMARY KEY (token_hash, harness, installation_id));
CREATE TABLE sessions (id TEXT PRIMARY KEY, parent_session_id TEXT REFERENCES sessions(id), installation_id TEXT REFERENCES machines(installation_id), started_at TEXT NOT NULL, ended_at TEXT, state TEXT NOT NULL DEFAULT 'active', last_active_at TEXT, inactive_at TEXT, harness TEXT, boundary TEXT NOT NULL, outcome TEXT NOT NULL DEFAULT 'unknown', work_outcome TEXT NOT NULL DEFAULT 'unresolved', outcome_src TEXT, outcome_updated_at TEXT, outcome_reason TEXT, repo TEXT, source_ref TEXT, model_primary TEXT, request_count INTEGER NOT NULL DEFAULT 0, tokens_in INTEGER NOT NULL DEFAULT 0, tokens_out INTEGER NOT NULL DEFAULT 0, files TEXT NOT NULL DEFAULT '[]', errors TEXT NOT NULL DEFAULT '[]', intent TEXT, summary_text TEXT, summary_status TEXT NOT NULL DEFAULT 'pending', summary_source TEXT, summary_updated_at TEXT, title TEXT, title_source TEXT CHECK (title_source IN ('manual', 'harness', 'generated', 'derived')), title_updated_at TEXT, log_refs TEXT NOT NULL DEFAULT '[]');
CREATE UNIQUE INDEX sessions_one_active_heuristic ON sessions(IFNULL(repo, ''), IFNULL(harness, ''), IFNULL(installation_id, '')) WHERE boundary = 'heuristic' AND state = 'active';
CREATE TABLE exchanges (id TEXT PRIMARY KEY, session_id TEXT NOT NULL, ts TEXT NOT NULL, endpoint TEXT NOT NULL, model TEXT, request_excerpt TEXT NOT NULL DEFAULT '', response_excerpt TEXT NOT NULL DEFAULT '', usage_json TEXT NOT NULL DEFAULT '{}', latency_ms INTEGER NOT NULL, repo TEXT, harness TEXT, r2_key TEXT NOT NULL, provider TEXT, finish_reason TEXT, access_token_label TEXT, input_tokens INTEGER NOT NULL DEFAULT 0, output_tokens INTEGER NOT NULL DEFAULT 0, capture_status TEXT NOT NULL DEFAULT 'accepted', capture_reason TEXT, accepted_at TEXT, saved_at TEXT, failed_at TEXT, failure_code TEXT, schema_version INTEGER NOT NULL DEFAULT 1, r2_bytes INTEGER, request_kind TEXT NOT NULL DEFAULT 'primary', intent_candidate TEXT, title_candidate TEXT);
CREATE TABLE config (key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE session_files (session_id TEXT NOT NULL, file TEXT NOT NULL, PRIMARY KEY(session_id, file));
CREATE TABLE session_errors (session_id TEXT NOT NULL, signature TEXT NOT NULL, PRIMARY KEY(session_id, signature));
CREATE TABLE exchange_files (exchange_id TEXT NOT NULL, session_id TEXT NOT NULL, file TEXT NOT NULL, PRIMARY KEY(exchange_id, file));
CREATE TABLE exchange_errors (exchange_id TEXT NOT NULL, session_id TEXT NOT NULL, signature TEXT NOT NULL, PRIMARY KEY(exchange_id, signature));
CREATE TABLE session_outcome_events (id TEXT PRIMARY KEY, session_id TEXT NOT NULL, outcome TEXT NOT NULL, source TEXT NOT NULL, reason TEXT, evidence_json TEXT, created_at TEXT NOT NULL);
`;

export async function tokenHash(token: string) {
  const digest = await crypto.subtle.digest(
    "SHA-256",
    new TextEncoder().encode(token),
  );
  return Array.from(new Uint8Array(digest), (byte) =>
    byte.toString(16).padStart(2, "0"),
  ).join("");
}

export async function addMachineToken(installationID: string, token: string) {
  const now = "2026-08-13T00:00:00Z";
  await env.DB.prepare(
    "INSERT OR IGNORE INTO machines(installation_id, name, platform, arch, created_at, updated_at) VALUES (?, ?, 'test', 'test', ?, ?)",
  )
    .bind(installationID, installationID, now, now)
    .run();
  await env.DB.prepare(
    "INSERT INTO access_tokens(token_hash, label, created_at, installation_id) VALUES (?, ?, ?, ?)",
  )
    .bind(await tokenHash(token), token, now, installationID)
    .run();
}

export async function request(path: string, init?: RequestInit) {
  const ctx = createExecutionContext();
  const response = await worker.fetch(
    new Request(`https://mimir.test${path}`, init),
    env as Env & { OPENROUTER_API_KEY: string },
    ctx,
  );
  await waitOnExecutionContext(ctx);
  return response;
}

export async function dashboardRequest(path: string, init?: RequestInit) {
  const ctx = createExecutionContext();
  const response = await worker.fetch(
    new Request(`http://localhost${path}`, init),
    env as Env & { OPENROUTER_API_KEY: string },
    ctx,
  );
  await waitOnExecutionContext(ctx);
  return response;
}

beforeAll(async () => {
  await env.DB.exec(schema);
});

beforeEach(async () => {
  await env.DB.exec(
    "DELETE FROM session_files; DELETE FROM session_errors; DELETE FROM exchange_files; DELETE FROM exchange_errors; DELETE FROM session_outcome_events; DELETE FROM exchanges; DELETE FROM sessions; DELETE FROM config; DELETE FROM harness_loads; DELETE FROM hermes_credentials; DELETE FROM access_tokens; DELETE FROM machines;",
  );
  await env.DB.prepare(
    "INSERT INTO access_tokens(token_hash, label, created_at) VALUES (?, 'test', '2026-01-01T00:00:00Z')",
  )
    .bind(await tokenHash("machine-token"))
    .run();
  const objects = await env.LOGS.list();
  await Promise.all(
    objects.objects.map((object) => env.LOGS.delete(object.key)),
  );
});

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

export {
  createExecutionContext,
  env,
  finalizeAcceptedExchange,
  waitOnExecutionContext,
  worker,
};
