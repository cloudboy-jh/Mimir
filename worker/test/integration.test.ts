import { createExecutionContext, env, waitOnExecutionContext } from "cloudflare:test";
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import worker from "../src/index";

const schema = `
CREATE TABLE access_tokens (token_hash TEXT PRIMARY KEY, label TEXT NOT NULL, created_at TEXT NOT NULL, last_used_at TEXT, revoked_at TEXT);
CREATE TABLE sessions (id TEXT PRIMARY KEY, started_at TEXT NOT NULL, ended_at TEXT, state TEXT NOT NULL DEFAULT 'active', last_active_at TEXT, inactive_at TEXT, harness TEXT, boundary TEXT NOT NULL, outcome TEXT NOT NULL DEFAULT 'unknown', work_outcome TEXT NOT NULL DEFAULT 'unresolved', outcome_src TEXT, outcome_updated_at TEXT, outcome_reason TEXT, repo TEXT, source_ref TEXT, model_primary TEXT, request_count INTEGER NOT NULL DEFAULT 0, tokens_in INTEGER NOT NULL DEFAULT 0, tokens_out INTEGER NOT NULL DEFAULT 0, files TEXT NOT NULL DEFAULT '[]', errors TEXT NOT NULL DEFAULT '[]', intent TEXT, log_refs TEXT NOT NULL DEFAULT '[]');
CREATE UNIQUE INDEX sessions_one_active_heuristic ON sessions(IFNULL(repo, ''), IFNULL(harness, '')) WHERE boundary = 'heuristic' AND state = 'active';
 CREATE TABLE exchanges (id TEXT PRIMARY KEY, session_id TEXT NOT NULL, ts TEXT NOT NULL, endpoint TEXT NOT NULL, model TEXT, request_excerpt TEXT NOT NULL DEFAULT '', response_excerpt TEXT NOT NULL DEFAULT '', usage_json TEXT NOT NULL DEFAULT '{}', latency_ms INTEGER NOT NULL, repo TEXT, harness TEXT, r2_key TEXT NOT NULL, provider TEXT, finish_reason TEXT, access_token_label TEXT, input_tokens INTEGER NOT NULL DEFAULT 0, output_tokens INTEGER NOT NULL DEFAULT 0, capture_status TEXT NOT NULL DEFAULT 'accepted', capture_reason TEXT, accepted_at TEXT, saved_at TEXT, failed_at TEXT, failure_code TEXT, schema_version INTEGER NOT NULL DEFAULT 1, r2_bytes INTEGER);
CREATE TABLE config (key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE session_files (session_id TEXT NOT NULL, file TEXT NOT NULL, PRIMARY KEY(session_id, file));
CREATE TABLE session_errors (session_id TEXT NOT NULL, signature TEXT NOT NULL, PRIMARY KEY(session_id, signature));
CREATE TABLE exchange_files (exchange_id TEXT NOT NULL, session_id TEXT NOT NULL, file TEXT NOT NULL, PRIMARY KEY(exchange_id, file));
CREATE TABLE exchange_errors (exchange_id TEXT NOT NULL, session_id TEXT NOT NULL, signature TEXT NOT NULL, PRIMARY KEY(exchange_id, signature));
CREATE TABLE session_outcome_events (id TEXT PRIMARY KEY, session_id TEXT NOT NULL, outcome TEXT NOT NULL, source TEXT NOT NULL, reason TEXT, evidence_json TEXT, created_at TEXT NOT NULL);
`;

async function tokenHash(token: string) {
  const digest = await crypto.subtle.digest("SHA-256", new TextEncoder().encode(token));
  return Array.from(new Uint8Array(digest), (byte) => byte.toString(16).padStart(2, "0")).join("");
}

async function request(path: string, init?: RequestInit) {
  const ctx = createExecutionContext();
  const response = await worker.fetch(new Request(`https://mimir.test${path}`, init), env as Env & { OPENROUTER_API_KEY: string }, ctx);
  await waitOnExecutionContext(ctx);
  return response;
}

beforeAll(async () => {
  await env.DB.exec(schema);
});

beforeEach(async () => {
  await env.DB.exec("DELETE FROM session_files; DELETE FROM session_errors; DELETE FROM exchange_files; DELETE FROM exchange_errors; DELETE FROM session_outcome_events; DELETE FROM exchanges; DELETE FROM sessions; DELETE FROM config; DELETE FROM access_tokens;");
  await env.DB.prepare("INSERT INTO access_tokens(token_hash, label, created_at) VALUES (?, 'test', '2026-01-01T00:00:00Z')").bind(await tokenHash("machine-token")).run();
  const objects = await env.LOGS.list();
  await Promise.all(objects.objects.map((object) => env.LOGS.delete(object.key)));
});

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe("Worker integration", () => {
  it("rejects unauthenticated requests", async () => {
    const response = await request("/whoami");
    expect(response.status).toBe(401);
  });

  it("streams unchanged and persists redacted session data", async () => {
    const stream = 'data: {"choices":[{"delta":{"content":"src/auth.ts failed: boom"}}]}\n\ndata: {"usage":{"prompt_tokens":5,"completion_tokens":3}}\n\ndata: [DONE]\n';
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(stream, { status: 200, headers: { "content-type": "text/event-stream" } })));
    const response = await request("/v1/chat/completions", {
      method: "POST",
      headers: { authorization: "Bearer machine-token", "content-type": "application/json", "x-mimir-session": "session-1", "x-mimir-repo": "mimir", "x-mimir-harness": "test" },
      body: JSON.stringify({ model: "openai/test", messages: [{ role: "user", content: "token: private-value" }], stream: true }),
    });
    expect(response.headers.get("x-mimir-capture")).toBe("scheduled");
    expect(response.headers.get("x-mimir-capture-reason")).toBe("enabled");
    expect(await response.text()).toBe(stream);
    const upstream = vi.mocked(fetch).mock.calls[0];
    const upstreamHeaders = new Headers((upstream[1] as RequestInit).headers);
    expect(upstreamHeaders.get("authorization")).toBe("Bearer test-openrouter-key");
    expect(upstreamHeaders.get("x-mimir-session")).toBeNull();
    const session = await env.DB.prepare("SELECT request_count, tokens_in, tokens_out FROM sessions WHERE id = 'session-1'").first<{ request_count: number; tokens_in: number; tokens_out: number }>();
    expect(session).toEqual({ request_count: 1, tokens_in: 5, tokens_out: 3 });
    expect(await env.DB.prepare("SELECT file FROM session_files WHERE session_id = 'session-1'").first<{ file: string }>()).toEqual({ file: "src/auth.ts" });
    const exchange = await env.DB.prepare("SELECT id, r2_key, input_tokens, output_tokens, access_token_label, capture_status, capture_reason, schema_version, r2_bytes FROM exchanges WHERE session_id = 'session-1'").first<{ id: string; r2_key: string; input_tokens: number; output_tokens: number; access_token_label: string; capture_status: string; capture_reason: string; schema_version: number; r2_bytes: number }>();
    expect(exchange?.r2_key).toMatch(/^log\//);
    expect(exchange).toMatchObject({ input_tokens: 5, output_tokens: 3, access_token_label: "test", capture_status: "saved", capture_reason: "enabled", schema_version: 1 });
    const object = await env.LOGS.get(exchange!.r2_key);
    const objectText = await object!.text();
    expect(objectText).not.toContain("private-value");
    expect(exchange?.r2_bytes).toBe(new TextEncoder().encode(objectText).byteLength);
    const envelope = JSON.parse(objectText);
    expect(envelope).toMatchObject({ schema_version: 1, exchange_id: exchange?.id, session_id: "session-1", declared_session_id: "session-1", response: { format: "reconstructed_sse", content: "src/auth.ts failed: boom" }, usage: { input_tokens: 5, output_tokens: 3 }, redaction: { version: 1 } });
  });

  it("records an accepted exchange before the upstream archive finishes", async () => {
    let closeStream: (() => void) | undefined;
    const upstream = new ReadableStream<Uint8Array>({
      start(controller) {
        controller.enqueue(new TextEncoder().encode('{"choices":['));
        closeStream = () => {
          controller.enqueue(new TextEncoder().encode(']}'));
          controller.close();
        };
      },
    });
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(upstream, { headers: { "content-type": "application/json" } })));
    const ctx = createExecutionContext();
    const response = await worker.fetch(new Request("https://mimir.test/v1/chat/completions", {
      method: "POST",
      headers: { authorization: "Bearer machine-token", "content-type": "application/json", "x-mimir-session": "early-accepted" },
      body: JSON.stringify({ model: "openai/test", messages: [] }),
    }), env as Env & { OPENROUTER_API_KEY: string }, ctx);

    await vi.waitFor(async () => {
      expect(await env.DB.prepare("SELECT capture_status FROM exchanges WHERE session_id = 'early-accepted'").first()).toEqual({ capture_status: "accepted" });
    });
    closeStream?.();
    await response.text();
    await waitOnExecutionContext(ctx);
    expect(await env.DB.prepare("SELECT capture_status FROM exchanges WHERE session_id = 'early-accepted'").first()).toEqual({ capture_status: "saved" });
  });

  it("does not persist when saving is disabled", async () => {
    await env.DB.prepare("INSERT INTO config(key, value) VALUES('save.enabled', 'false')").run();
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json({ choices: [] })));
    const response = await request("/v1/chat/completions", { method: "POST", headers: { "x-api-key": "machine-token", "content-type": "application/json" }, body: JSON.stringify({ model: "openai/test", messages: [] }) });
    expect(response.status).toBe(200);
    expect(response.headers.get("x-mimir-capture")).toBe("skipped");
    expect(response.headers.get("x-mimir-capture-reason")).toBe("disabled");
    expect((await env.DB.prepare("SELECT COUNT(*) AS count FROM exchanges").first<{ count: number }>())?.count).toBe(0);
    expect((await env.LOGS.list()).objects).toHaveLength(0);
  });

  it("reports excluded and bodyless capture decisions", async () => {
    await env.DB.prepare("INSERT INTO config(key, value) VALUES('save.exclude_repos', '[\"private-*\"]')").run();
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json({ choices: [] })));
    const excluded = await request("/v1/chat/completions", { method: "POST", headers: { authorization: "Bearer machine-token", "content-type": "application/json", "x-mimir-repo": "private-repo" }, body: JSON.stringify({ model: "openai/test" }) });
    expect(excluded.headers.get("x-mimir-capture-reason")).toBe("excluded_repository");
    await env.DB.prepare("DELETE FROM config").run();
    vi.mocked(fetch).mockResolvedValueOnce(new Response(null, { status: 204 }));
    const bodyless = await request("/v1/chat/completions", { method: "POST", headers: { authorization: "Bearer machine-token", "content-type": "application/json" }, body: JSON.stringify({ model: "openai/test" }) });
    expect(bodyless.headers.get("x-mimir-capture")).toBe("skipped");
    expect(bodyless.headers.get("x-mimir-capture-reason")).toBe("missing_response_body");
  });

  it("leaves an R2 write failure failed without session aggregates", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json({ choices: [], usage: { prompt_tokens: 9, completion_tokens: 4 } })));
    vi.spyOn(env.LOGS, "put").mockRejectedValueOnce(new Error("injected R2 failure"));
    const response = await request("/v1/chat/completions", { method: "POST", headers: { authorization: "Bearer machine-token", "content-type": "application/json", "x-mimir-session": "failed-session" }, body: JSON.stringify({ model: "openai/test" }) });
    expect(response.headers.get("x-mimir-capture")).toBe("scheduled");
    expect(await env.DB.prepare("SELECT capture_status, failure_code FROM exchanges WHERE session_id = 'failed-session'").first()).toEqual({ capture_status: "failed", failure_code: "r2_write_failed" });
    expect(await env.DB.prepare("SELECT request_count, tokens_in, tokens_out FROM sessions WHERE id = 'failed-session'").first()).toEqual({ request_count: 0, tokens_in: 0, tokens_out: 0 });
  });

  it("leaves an accepted exchange for reconciliation when D1 finalization fails", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json({ choices: [], usage: { prompt_tokens: 6, completion_tokens: 2 } })));
    const batch = env.DB.batch.bind(env.DB);
    vi.spyOn(env.DB, "batch").mockImplementationOnce(batch).mockRejectedValueOnce(new Error("injected D1 finalization failure"));
    await request("/v1/chat/completions", { method: "POST", headers: { authorization: "Bearer machine-token", "content-type": "application/json", "x-mimir-session": "accepted-session" }, body: JSON.stringify({ model: "openai/test", messages: [{ role: "user", content: "Inspect src/recovered.ts" }] }) });
    const accepted = await env.DB.prepare("SELECT id, r2_key, capture_status FROM exchanges WHERE session_id = 'accepted-session'").first<{ id: string; r2_key: string; capture_status: string }>();
    expect(accepted?.capture_status).toBe("accepted");
    expect(await env.LOGS.get(accepted!.r2_key)).not.toBeNull();
    expect(await env.DB.prepare("SELECT request_count FROM sessions WHERE id = 'accepted-session'").first()).toEqual({ request_count: 0 });

    await request("/reconcile", { method: "POST", headers: { authorization: "Bearer machine-token" } });
    expect(await env.DB.prepare("SELECT capture_status FROM exchanges WHERE id = ?").bind(accepted!.id).first()).toEqual({ capture_status: "saved" });
    expect(await env.DB.prepare("SELECT request_count, tokens_in, tokens_out FROM sessions WHERE id = 'accepted-session'").first()).toEqual({ request_count: 1, tokens_in: 6, tokens_out: 2 });
    expect(await env.DB.prepare("SELECT file FROM session_files WHERE session_id = 'accepted-session'").first()).toEqual({ file: "src/recovered.ts" });
  });

  it("reopens an inactive exact session on the next exchange", async () => {
    vi.stubGlobal("fetch", vi.fn().mockImplementation(() => Promise.resolve(Response.json({ choices: [], usage: { prompt_tokens: 2, completion_tokens: 1 } }))));
    await env.DB.prepare("INSERT INTO sessions(id, started_at, ended_at, state, last_active_at, inactive_at, boundary) VALUES ('session-lifecycle', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', 'inactive', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', 'header')").run();
    const headers = { authorization: "Bearer machine-token", "content-type": "application/json", "x-mimir-session": "session-lifecycle", "x-mimir-harness": "test" };
    await request("/v1/chat/completions", { method: "POST", headers, body: JSON.stringify({ model: "openai/test", messages: [] }) });
    expect(await env.DB.prepare("SELECT state, request_count FROM sessions WHERE id = 'session-lifecycle'").first()).toEqual({ state: "active", request_count: 1 });
  });

  it("marks stale sessions inactive when memory is queried", async () => {
    await env.DB.prepare("INSERT INTO sessions(id, started_at, ended_at, state, last_active_at, boundary) VALUES ('stale', '2020-01-01T00:00:00Z', '2020-01-01T00:00:00Z', 'active', '2020-01-01T00:00:00Z', 'heuristic')").run();
    const response = await request("/sessions", { headers: { authorization: "Bearer machine-token" } });
    expect(response.status).toBe(200);
    expect((await env.DB.prepare("SELECT state FROM sessions WHERE id = 'stale'").first<{ state: string }>())?.state).toBe("inactive");
  });

  it("returns capture status and maps legacy outcomes with an audit event", async () => {
    await env.DB.prepare("INSERT INTO sessions(id, started_at, state, last_active_at, boundary) VALUES ('status-session', '2026-01-01T00:00:00Z', 'active', '2026-01-01T00:00:00Z', 'header')").run();
    await env.DB.prepare("INSERT INTO exchanges(id, session_id, ts, endpoint, latency_ms, r2_key, capture_status, capture_reason, accepted_at, saved_at, schema_version) VALUES ('saved-status', 'status-session', '2026-01-01T00:00:00Z', 'chat', 1, 'log/status.json', 'saved', 'enabled', '2026-01-01T00:00:00Z', '2026-01-01T00:00:01Z', 1)").run();
    const status = await request("/sessions/status-session/status", { headers: { authorization: "Bearer machine-token" } });
    expect(await status.json()).toEqual({
      session_id: "status-session",
      capture: { saved_exchanges: 1, failed_exchanges: 0, pending_exchanges: 0, last_saved_at: "2026-01-01T00:00:01Z", status: "saved" },
      outcome: "unresolved",
      outcome_src: null,
      outcome_updated_at: null,
      outcome_reason: null,
      receipt: { label: "Saved to Mimir", detail: "1 exchange in this session", action_label: null },
      dashboard_url: null,
    });

    await env.DB.prepare("INSERT INTO sessions(id, started_at, state, last_active_at, boundary) VALUES ('pending-status', '2026-01-01T00:00:00Z', 'active', '2026-01-01T00:00:00Z', 'header')").run();
    await env.DB.prepare("INSERT INTO exchanges(id, session_id, ts, endpoint, latency_ms, r2_key, capture_status, accepted_at, schema_version) VALUES ('pending-exchange', 'pending-status', '2026-01-01T00:00:00Z', 'chat', 1, 'log/pending.json', 'accepted', '2026-01-01T00:00:00Z', 1)").run();
    const pendingStatus = await request("/sessions/pending-status/status", { headers: { authorization: "Bearer machine-token" } });
    expect(await pendingStatus.json()).toMatchObject({
      capture: { status: "pending", pending_exchanges: 1 },
      receipt: { label: "Saving to Mimir...", detail: "1 exchange", action_label: null },
      dashboard_url: null,
    });

    const marked = await request("/sessions/status-session/outcome", { method: "POST", headers: { authorization: "Bearer machine-token", "content-type": "application/json" }, body: JSON.stringify({ outcome: "promoted", source: "git", reason: "merged", evidence: { commit: "abc123" } }) });
    expect(await marked.json()).toMatchObject({ outcome: "landed", outcome_src: "agent", outcome_reason: "merged", evidence: { commit: "abc123" } });
    expect(await env.DB.prepare("SELECT work_outcome, outcome FROM sessions WHERE id = 'status-session'").first()).toEqual({ work_outcome: "landed", outcome: "promoted" });
    expect(await env.DB.prepare("SELECT outcome, source, reason, evidence_json FROM session_outcome_events WHERE session_id = 'status-session'").first()).toEqual({ outcome: "landed", source: "agent", reason: "merged", evidence_json: '{"commit":"abc123"}' });
    const detail = await request("/sessions/status-session", { headers: { authorization: "Bearer machine-token" } });
    expect((await detail.json() as { session: { outcome: string } }).session.outcome).toBe("landed");
  });

  it("reconciles accepted objects and missing saved objects without deletion", async () => {
    await env.DB.prepare("INSERT INTO sessions(id, started_at, state, last_active_at, inactive_at, boundary, request_count, tokens_in, tokens_out) VALUES ('reconcile-session', '2026-01-01T00:00:00Z', 'inactive', '2026-01-01T00:00:00Z', '2026-01-01T00:15:00Z', 'header', 1, 7, 3)").run();
    await env.DB.prepare("INSERT INTO exchanges(id, session_id, ts, endpoint, model, latency_ms, r2_key, input_tokens, output_tokens, capture_status, capture_reason, accepted_at, schema_version) VALUES ('accepted-object', 'reconcile-session', '2026-01-01T00:00:00Z', 'chat', 'openai/test', 1, 'log/accepted-object.json', 5, 2, 'accepted', 'enabled', '2026-01-01T00:00:00Z', 1)").run();
    await env.DB.prepare("INSERT INTO exchange_files(exchange_id, session_id, file) VALUES ('accepted-object', 'reconcile-session', 'kept.ts')").run();
    await env.LOGS.put("log/accepted-object.json", "{}");
    await env.DB.prepare("INSERT INTO exchanges(id, session_id, ts, endpoint, model, latency_ms, r2_key, input_tokens, output_tokens, capture_status, capture_reason, accepted_at, saved_at, schema_version) VALUES ('missing-saved', 'reconcile-session', '2026-01-01T00:00:01Z', 'chat', 'openai/test', 1, 'log/missing-saved.json', 7, 3, 'saved', 'enabled', '2026-01-01T00:00:01Z', '2026-01-01T00:00:02Z', 1)").run();
    await env.DB.prepare("INSERT INTO exchange_files(exchange_id, session_id, file) VALUES ('missing-saved', 'reconcile-session', 'stale.ts')").run();
    await env.DB.prepare("INSERT INTO session_files(session_id, file) VALUES ('reconcile-session', 'stale.ts')").run();
    await env.DB.prepare("INSERT INTO sessions(id, started_at, state, last_active_at, inactive_at, boundary) VALUES ('recent-session', '2026-01-01T00:00:00Z', 'inactive', '2026-01-01T00:00:00Z', '2026-01-01T00:15:00Z', 'header')").run();
    await env.DB.prepare("INSERT INTO exchanges(id, session_id, ts, endpoint, model, latency_ms, r2_key, capture_status, capture_reason, accepted_at, schema_version) VALUES ('recent-object', 'recent-session', '2026-01-01T00:00:00Z', 'chat', 'openai/test', 1, 'log/recent-object.json', 'accepted', 'enabled', ?, 1)").bind(new Date().toISOString()).run();
    await env.LOGS.put("log/recent-object.json", "{}");
    await env.DB.prepare("INSERT INTO sessions(id, started_at, state, last_active_at, inactive_at, boundary, repo, harness) VALUES ('old-heuristic', '2026-01-01T00:00:00Z', 'inactive', '2026-01-01T00:00:00Z', '2026-01-01T00:15:00Z', 'heuristic', 'repo', 'agent')").run();
    await env.DB.prepare("INSERT INTO sessions(id, started_at, state, last_active_at, boundary, repo, harness) VALUES ('active-heuristic', '2026-01-01T00:20:00Z', 'active', '2026-01-01T00:20:00Z', 'heuristic', 'repo', 'agent')").run();
    await env.DB.prepare("INSERT INTO exchanges(id, session_id, ts, endpoint, model, latency_ms, r2_key, capture_status, capture_reason, accepted_at, schema_version) VALUES ('heuristic-object', 'old-heuristic', '2026-01-01T00:00:00Z', 'chat', 'openai/test', 1, 'log/heuristic-object.json', 'accepted', 'enabled', ?, 1)").bind(new Date().toISOString()).run();
    await env.LOGS.put("log/heuristic-object.json", "{}");
    const response = await request("/reconcile?limit=1000", { method: "POST", headers: { authorization: "Bearer machine-token" } });
    const result = await response.json() as { limit: number; finalized: { exchange_ids: string[] }; missing_saved: { exchange_ids: string[] } };
    expect(result.limit).toBe(100);
    expect(result.finalized.exchange_ids).toContain("accepted-object");
    expect(result.finalized.exchange_ids).toContain("recent-object");
    expect(result.finalized.exchange_ids).toContain("heuristic-object");
    expect(result.missing_saved.exchange_ids).toContain("missing-saved");
    expect(await env.DB.prepare("SELECT capture_status FROM exchanges WHERE id = 'accepted-object'").first()).toEqual({ capture_status: "saved" });
    expect(await env.DB.prepare("SELECT capture_status, failure_code FROM exchanges WHERE id = 'missing-saved'").first()).toEqual({ capture_status: "failed", failure_code: "r2_object_missing" });
    expect(await env.DB.prepare("SELECT saved_at, r2_bytes FROM exchanges WHERE id = 'missing-saved'").first()).toEqual({ saved_at: null, r2_bytes: null });
    expect(await env.DB.prepare("SELECT request_count, tokens_in, tokens_out FROM sessions WHERE id = 'reconcile-session'").first()).toEqual({ request_count: 1, tokens_in: 5, tokens_out: 2 });
    expect(await env.DB.prepare("SELECT state, last_active_at, inactive_at FROM sessions WHERE id = 'reconcile-session'").first()).toEqual({ state: "inactive", last_active_at: "2026-01-01T00:00:00Z", inactive_at: "2026-01-01T00:15:00Z" });
    expect(await env.DB.prepare("SELECT state, inactive_at FROM sessions WHERE id = 'recent-session'").first()).toEqual({ state: "active", inactive_at: null });
    expect(await env.DB.prepare("SELECT state, inactive_at FROM sessions WHERE id = 'old-heuristic'").first()).toEqual({ state: "inactive", inactive_at: "2026-01-01T00:15:00Z" });
    expect((await env.DB.prepare("SELECT file FROM session_files WHERE session_id = 'reconcile-session' ORDER BY file").all<{ file: string }>()).results).toEqual([{ file: "kept.ts" }]);
    expect(await env.LOGS.get("log/accepted-object.json")).not.toBeNull();
  });

  it("coalesces concurrent headerless requests into one heuristic session", async () => {
    vi.stubGlobal("fetch", vi.fn().mockImplementation(() => Promise.resolve(Response.json({ choices: [] }))));
    const init = { method: "POST", headers: { authorization: "Bearer machine-token", "content-type": "application/json" }, body: JSON.stringify({ model: "openai/test", messages: [] }) };
    await Promise.all([request("/v1/chat/completions", init), request("/v1/chat/completions", init)]);
    expect(await env.DB.prepare("SELECT COUNT(*) AS sessions, SUM(request_count) AS requests FROM sessions").first()).toEqual({ sessions: 1, requests: 2 });
  });

  it("requires Cloudflare Access for dashboard APIs", async () => {
    const response = await request("/dashboard/api/bootstrap");
    expect(response.status).toBe(403);
  });
});
