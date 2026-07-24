import { createExecutionContext, env, waitOnExecutionContext } from "cloudflare:test";
import { exportJWK, generateKeyPair, SignJWT } from "jose";
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import worker from "../src/index";
import { finalizeAcceptedExchange } from "../src/capture";

const schema = `
CREATE TABLE access_tokens (token_hash TEXT PRIMARY KEY, label TEXT NOT NULL, created_at TEXT NOT NULL, last_used_at TEXT, revoked_at TEXT);
CREATE TABLE hermes_credentials (token_hash TEXT PRIMARY KEY, created_at TEXT NOT NULL, authorized_by TEXT);
CREATE TABLE sessions (id TEXT PRIMARY KEY, started_at TEXT NOT NULL, ended_at TEXT, state TEXT NOT NULL DEFAULT 'active', last_active_at TEXT, inactive_at TEXT, harness TEXT, boundary TEXT NOT NULL, outcome TEXT NOT NULL DEFAULT 'unknown', work_outcome TEXT NOT NULL DEFAULT 'unresolved', outcome_src TEXT, outcome_updated_at TEXT, outcome_reason TEXT, repo TEXT, source_ref TEXT, model_primary TEXT, request_count INTEGER NOT NULL DEFAULT 0, tokens_in INTEGER NOT NULL DEFAULT 0, tokens_out INTEGER NOT NULL DEFAULT 0, files TEXT NOT NULL DEFAULT '[]', errors TEXT NOT NULL DEFAULT '[]', intent TEXT, log_refs TEXT NOT NULL DEFAULT '[]');
CREATE UNIQUE INDEX sessions_one_active_heuristic ON sessions(IFNULL(repo, ''), IFNULL(harness, '')) WHERE boundary = 'heuristic' AND state = 'active';
 CREATE TABLE exchanges (id TEXT PRIMARY KEY, session_id TEXT NOT NULL, ts TEXT NOT NULL, endpoint TEXT NOT NULL, model TEXT, request_excerpt TEXT NOT NULL DEFAULT '', response_excerpt TEXT NOT NULL DEFAULT '', usage_json TEXT NOT NULL DEFAULT '{}', latency_ms INTEGER NOT NULL, repo TEXT, harness TEXT, r2_key TEXT NOT NULL, provider TEXT, finish_reason TEXT, access_token_label TEXT, input_tokens INTEGER NOT NULL DEFAULT 0, output_tokens INTEGER NOT NULL DEFAULT 0, capture_status TEXT NOT NULL DEFAULT 'accepted', capture_reason TEXT, accepted_at TEXT, saved_at TEXT, failed_at TEXT, failure_code TEXT, schema_version INTEGER NOT NULL DEFAULT 1, r2_bytes INTEGER, request_kind TEXT NOT NULL DEFAULT 'primary', intent_candidate TEXT);
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

async function dashboardRequest(path: string, init?: RequestInit) {
  const ctx = createExecutionContext();
  const response = await worker.fetch(new Request(`http://localhost${path}`, init), env as Env & { OPENROUTER_API_KEY: string }, ctx);
  await waitOnExecutionContext(ctx);
  return response;
}

beforeAll(async () => {
  await env.DB.exec(schema);
});

beforeEach(async () => {
  await env.DB.exec("DELETE FROM session_files; DELETE FROM session_errors; DELETE FROM exchange_files; DELETE FROM exchange_errors; DELETE FROM session_outcome_events; DELETE FROM exchanges; DELETE FROM sessions; DELETE FROM config; DELETE FROM hermes_credentials; DELETE FROM access_tokens;");
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

  it("accepts an authorized OpenRouter key only on Hermes compatibility routes", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json({ data: [] })));
    const hermesKey = "independent-hermes-openrouter-key";
    expect((await request("/v1/hermes/models", { headers: { authorization: `Bearer ${hermesKey}` } })).status).toBe(401);
    expect((await request("/integrations/hermes/authorize", {
      method: "POST",
      headers: { authorization: "Bearer machine-token", "content-type": "application/json" },
      body: JSON.stringify({ token_hash: await tokenHash(hermesKey) }),
    })).status).toBe(200);
    expect((await request("/v1/hermes/models", { headers: { authorization: `Bearer ${hermesKey}` } })).status).toBe(200);
    const upstreamHeaders = new Headers(vi.mocked(fetch).mock.calls[0][1]?.headers);
    expect(upstreamHeaders.get("authorization")).toBe(`Bearer ${hermesKey}`);
    expect((await request("/whoami", { headers: { authorization: `Bearer ${hermesKey}` } })).status).toBe(401);
    expect((await request("/v1/models", { headers: { authorization: `Bearer ${hermesKey}` } })).status).toBe(401);
  });

  it.each([
    ["/v1/models", "https://openrouter.ai/api/v1/models"],
    ["/v1/credits", "https://openrouter.ai/api/v1/credits"],
    ["/v1/key", "https://openrouter.ai/api/v1/key"],
    ["/v1/hermes/models", "https://openrouter.ai/api/v1/models"],
    ["/v1/hermes/credits", "https://openrouter.ai/api/v1/credits"],
    ["/v1/hermes/key", "https://openrouter.ai/api/v1/key"],
  ])("proxies OpenRouter compatibility route %s", async (path, upstreamURL) => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response('{"data":{}}', { status: 206, headers: { "x-upstream": "openrouter" } })));
    const response = await request(path, { headers: { authorization: "Bearer machine-token", "x-mimir-harness": "must-not-leak" } });
    expect(response.status).toBe(206);
    expect(response.headers.get("x-upstream")).toBe("openrouter");
    expect(await response.text()).toBe('{"data":{}}');
    const [url, init] = vi.mocked(fetch).mock.calls[0];
    expect(url).toBe(upstreamURL);
    const headers = new Headers((init as RequestInit).headers);
    expect(headers.get("authorization")).toBe("Bearer test-openrouter-key");
    expect(headers.get("x-mimir-harness")).toBeNull();
  });

  it("identifies transparent Hermes capture from the compatibility path", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json({ choices: [], usage: { prompt_tokens: 1, completion_tokens: 1 } })));
    await request("/v1/hermes/chat/completions", {
      method: "POST",
      headers: { authorization: "Bearer machine-token", "content-type": "application/json" },
      body: JSON.stringify({ model: "openai/test", messages: [{ role: "user", content: "Hermes route" }] }),
    });
    expect(await env.DB.prepare("SELECT harness FROM exchanges LIMIT 1").first()).toEqual({ harness: "hermes" });
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

  it("uses only primary requests to establish session intent", async () => {
    vi.stubGlobal("fetch", vi.fn().mockImplementation(() => Promise.resolve(Response.json({ choices: [], usage: { prompt_tokens: 1, completion_tokens: 1 } }))));
    const headers = { authorization: "Bearer machine-token", "content-type": "application/json", "x-mimir-session": "intent-session", "x-mimir-harness": "opencode" };
    await request("/v1/chat/completions", {
      method: "POST",
      headers: { ...headers, "x-mimir-request-kind": "title" },
      body: JSON.stringify({ model: "openai/test", messages: [{ role: "user", content: "Generate a title for this conversation:" }] }),
    });
    expect(await env.DB.prepare("SELECT intent FROM sessions WHERE id = 'intent-session'").first()).toEqual({ intent: null });
    await request("/v1/chat/completions", {
      method: "POST",
      headers: { ...headers, "x-mimir-request-kind": "primary" },
      body: JSON.stringify({ model: "openai/test", messages: [{ role: "user", content: "Fix session intent handling" }] }),
    });
    expect(await env.DB.prepare("SELECT intent FROM sessions WHERE id = 'intent-session'").first()).toEqual({ intent: "Fix session intent handling" });
    expect((await env.DB.prepare("SELECT request_kind FROM exchanges WHERE session_id = 'intent-session' ORDER BY ts, id").all()).results.map((row) => row.request_kind)).toEqual(["title", "primary"]);
  });

  it("defensively classifies title prompts and rejects invalid request kinds", async () => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json({ choices: [] })));
    await request("/v1/chat/completions", {
      method: "POST",
      headers: { authorization: "Bearer machine-token", "content-type": "application/json", "x-mimir-session": "defensive-title", "x-mimir-request-kind": "primary" },
      body: JSON.stringify({ model: "openai/test", messages: [{ role: "system", content: "You are a title generator. Output only a title." }, { role: "user", content: "Generate a title" }] }),
    });
    expect(await env.DB.prepare("SELECT intent FROM sessions WHERE id = 'defensive-title'").first()).toEqual({ intent: null });
    expect(await env.DB.prepare("SELECT request_kind FROM exchanges WHERE session_id = 'defensive-title'").first()).toEqual({ request_kind: "title" });
    const invalid = await request("/v1/chat/completions", {
      method: "POST",
      headers: { authorization: "Bearer machine-token", "content-type": "application/json", "x-mimir-request-kind": "background" },
      body: JSON.stringify({ model: "openai/test" }),
    });
    expect(invalid.status).toBe(400);
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
    expect(await env.DB.prepare("SELECT request_count, tokens_in, tokens_out, intent FROM sessions WHERE id = 'accepted-session'").first()).toEqual({ request_count: 1, tokens_in: 6, tokens_out: 2, intent: "Inspect src/recovered.ts" });
    expect(await env.DB.prepare("SELECT file FROM session_files WHERE session_id = 'accepted-session'").first()).toEqual({ file: "src/recovered.ts" });
  });

  it("reconciliation selects the earliest saved primary intent", async () => {
    await env.DB.prepare("INSERT INTO sessions(id, started_at, ended_at, state, last_active_at, boundary) VALUES ('ordered-intent', '2026-01-01T00:00:00Z', '2026-01-01T00:01:00Z', 'active', '2026-01-01T00:01:00Z', 'header')").run();
    await env.DB.prepare("INSERT INTO exchanges(id, session_id, ts, endpoint, model, latency_ms, r2_key, capture_status, accepted_at, request_kind, intent_candidate) VALUES ('older-intent', 'ordered-intent', '2026-01-01T00:00:00Z', 'chat', 'openai/test', 1, 'log/older-intent.json', 'accepted', '2026-01-01T00:00:00Z', 'primary', 'First user request')").run();
    await env.DB.prepare("INSERT INTO exchanges(id, session_id, ts, endpoint, model, latency_ms, r2_key, capture_status, accepted_at, request_kind, intent_candidate) VALUES ('newer-intent', 'ordered-intent', '2026-01-01T00:01:00Z', 'chat', 'openai/test', 1, 'log/newer-intent.json', 'accepted', '2026-01-01T00:01:00Z', 'primary', 'Later user request')").run();
    await env.LOGS.put("log/older-intent.json", "{}");
    await env.LOGS.put("log/newer-intent.json", "{}");

    await request("/reconcile", { method: "POST", headers: { authorization: "Bearer machine-token" } });

    expect(await env.DB.prepare("SELECT intent, request_count FROM sessions WHERE id = 'ordered-intent'").first()).toEqual({ intent: "First user request", request_count: 2 });
  });

  it("preserves historical session intent without persisted candidates", async () => {
    await env.DB.prepare("INSERT INTO sessions(id, started_at, ended_at, state, last_active_at, boundary, intent) VALUES ('historical-intent', '2026-01-01T00:00:00Z', '2026-01-01T00:01:00Z', 'active', '2026-01-01T00:01:00Z', 'header', 'Historical user request')").run();
    await env.DB.prepare("INSERT INTO exchanges(id, session_id, ts, endpoint, model, latency_ms, r2_key, capture_status, accepted_at, request_kind, intent_candidate) VALUES ('new-historical-exchange', 'historical-intent', '2026-01-02T00:00:00Z', 'chat', 'openai/test', 1, 'log/new-historical.json', 'accepted', '2026-01-02T00:00:00Z', 'primary', 'New user request')").run();
    await env.LOGS.put("log/new-historical.json", "{}");

    await request("/reconcile", { method: "POST", headers: { authorization: "Bearer machine-token" } });

    expect(await env.DB.prepare("SELECT intent FROM sessions WHERE id = 'historical-intent'").first()).toEqual({ intent: "Historical user request" });
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
      state: "active",
      ended_at: null,
      inactive_at: null,
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

  it("ends sessions idempotently and optionally records an outcome", async () => {
    await env.DB.prepare("INSERT INTO sessions(id, started_at, ended_at, state, last_active_at, boundary) VALUES ('end-session', '2026-01-01T00:00:00Z', '2026-01-01T00:01:00Z', 'active', '2026-01-01T00:01:00Z', 'header')").run();
    const headers = { authorization: "Bearer machine-token", "content-type": "application/json" };
    const ended = await request("/sessions/end-session/end", { method: "POST", headers, body: JSON.stringify({ outcome: "landed", reason: "verified", evidence: { commit: "abc123" } }) });
    expect(ended.status).toBe(200);
    const first = await ended.json() as { session: { state: string; ended_at: string; inactive_at: string; outcome: string; outcome_src: string; outcome_updated_at: string }; evidence: unknown };
    expect(first.session).toMatchObject({ state: "inactive", outcome: "landed", outcome_src: "agent" });
    expect(first.session.ended_at).toBe(first.session.inactive_at);
    expect(first.evidence).toEqual({ commit: "abc123" });
    expect(await env.DB.prepare("SELECT outcome, source, reason, evidence_json FROM session_outcome_events WHERE session_id = 'end-session'").first()).toEqual({ outcome: "landed", source: "agent", reason: "verified", evidence_json: '{"commit":"abc123"}' });

    const repeated = await request("/sessions/end-session/end", { method: "POST", headers, body: JSON.stringify({ outcome: "landed", reason: "verified", evidence: { commit: "abc123" } }) });
    const repeatedSession = (await repeated.json() as { session: { ended_at: string; inactive_at: string; outcome_updated_at: string } }).session;
    expect(repeatedSession).toMatchObject({ ended_at: first.session.ended_at, inactive_at: first.session.inactive_at, outcome_updated_at: first.session.outcome_updated_at });
    expect(await env.DB.prepare("SELECT COUNT(*) AS count FROM session_outcome_events WHERE session_id = 'end-session'").first()).toEqual({ count: 1 });

    const concurrent = await Promise.all([
      request("/sessions/end-session/end", { method: "POST", headers, body: JSON.stringify({ outcome: "discarded", reason: "superseded", evidence: { issue: 42 } }) }),
      request("/sessions/end-session/end", { method: "POST", headers, body: JSON.stringify({ outcome: "discarded", reason: "superseded", evidence: { issue: 42 } }) }),
    ]);
    expect(concurrent.every((response) => response.status === 200)).toBe(true);
    expect(await env.DB.prepare("SELECT COUNT(*) AS count FROM session_outcome_events WHERE session_id = 'end-session' AND outcome = 'discarded'").first()).toEqual({ count: 1 });

    expect((await request("/sessions/missing/end", { method: "POST", headers, body: "{}" })).status).toBe(404);
    expect((await request("/sessions/end-session/end", { method: "POST", headers, body: JSON.stringify({ reason: "missing outcome" }) })).status).toBe(400);
    expect((await request("/sessions/end-session/end", { method: "POST", headers, body: "null" })).status).toBe(400);
    expect((await request("/sessions/end-session/end", { method: "POST", headers, body: "[]" })).status).toBe(400);
  });

  it("does not reactivate a session when pre-end capture finishes late", async () => {
    await env.DB.prepare("INSERT INTO sessions(id, started_at, ended_at, state, last_active_at, boundary) VALUES ('end-race', '2026-01-01T00:00:00Z', '2026-01-01T00:01:00Z', 'active', '2026-01-01T00:01:00Z', 'header')").run();
    await env.DB.prepare("INSERT INTO exchanges(id, session_id, ts, endpoint, model, latency_ms, r2_key, capture_status, accepted_at, schema_version) VALUES ('end-race-exchange', 'end-race', '2026-01-01T00:01:00Z', 'chat', 'openai/test', 1, 'log/end-race.json', 'accepted', '2026-01-01T00:01:01Z', 1)").run();
    const headers = { authorization: "Bearer machine-token", "content-type": "application/json" };
    const endBody = JSON.stringify({ outcome: "landed", reason: "race verified", evidence: { test: "late-finalize" } });
    const ended = await request("/sessions/end-race/end", { method: "POST", headers, body: endBody });
    const endTime = (await ended.json() as { session: { inactive_at: string } }).session.inactive_at;

    await finalizeAcceptedExchange(env.DB, "end-race-exchange", "end-race", "2026-01-01T00:01:00Z", "2026-01-01T00:02:00Z", "opencode", "openai/test", 3, 1, 100, true);
    expect(await env.DB.prepare("SELECT state, inactive_at, request_count FROM sessions WHERE id = 'end-race'").first()).toEqual({ state: "inactive", inactive_at: endTime, request_count: 1 });
    expect((await request("/sessions/end-race/end", { method: "POST", headers, body: endBody })).status).toBe(200);
    expect(await env.DB.prepare("SELECT COUNT(*) AS count FROM session_outcome_events WHERE session_id = 'end-race'").first()).toEqual({ count: 1 });
  });

  it("turns an auto-expired session into an explicit end marker", async () => {
    await env.DB.prepare("INSERT INTO sessions(id, started_at, ended_at, state, last_active_at, inactive_at, boundary) VALUES ('expired-end-race', '2026-01-01T00:00:00Z', '2026-01-01T00:01:00Z', 'inactive', '2026-01-01T00:01:00Z', '2026-01-01T00:15:00Z', 'header')").run();
    await env.DB.prepare("INSERT INTO exchanges(id, session_id, ts, endpoint, model, latency_ms, r2_key, capture_status, accepted_at, schema_version) VALUES ('expired-end-exchange', 'expired-end-race', '2026-01-01T00:01:00Z', 'chat', 'openai/test', 1, 'log/expired-end.json', 'accepted', '2026-01-01T00:01:01Z', 1)").run();
    const headers = { authorization: "Bearer machine-token", "content-type": "application/json" };
    const ended = await request("/sessions/expired-end-race/end", { method: "POST", headers, body: "{}" });
    const explicit = (await ended.json() as { session: { ended_at: string; inactive_at: string } }).session;
    expect(explicit.ended_at).toBe(explicit.inactive_at);
    expect(explicit.inactive_at).not.toBe("2026-01-01T00:15:00Z");

    await finalizeAcceptedExchange(env.DB, "expired-end-exchange", "expired-end-race", "2026-01-01T00:01:00Z", "2026-01-01T00:20:00Z", "opencode", "openai/test", 2, 1, 50, true);
    expect(await env.DB.prepare("SELECT state, ended_at, inactive_at FROM sessions WHERE id = 'expired-end-race'").first()).toEqual({ state: "inactive", ended_at: explicit.ended_at, inactive_at: explicit.inactive_at });
  });

  it("reactivates an explicitly ended exact session for genuinely later activity", async () => {
    await env.DB.prepare("INSERT INTO sessions(id, started_at, ended_at, state, last_active_at, boundary) VALUES ('ended-reactivate', '2026-01-01T00:00:00Z', '2026-01-01T00:01:00Z', 'active', '2026-01-01T00:01:00Z', 'header')").run();
    const headers = { authorization: "Bearer machine-token", "content-type": "application/json" };
    const ended = await request("/sessions/ended-reactivate/end", { method: "POST", headers, body: "{}" });
    const endTime = (await ended.json() as { session: { inactive_at: string } }).session.inactive_at;
    const later = new Date(Date.parse(endTime) + 1_000).toISOString();
    await env.DB.prepare("INSERT INTO exchanges(id, session_id, ts, endpoint, model, latency_ms, r2_key, capture_status, accepted_at, schema_version) VALUES ('reactivate-exchange', 'ended-reactivate', ?, 'chat', 'openai/test', 1, 'log/reactivate.json', 'accepted', ?, 1)").bind(later, later).run();
    await finalizeAcceptedExchange(env.DB, "reactivate-exchange", "ended-reactivate", later, later, "opencode", "openai/test", 2, 1, 50, true);
    expect(await env.DB.prepare("SELECT state, inactive_at, last_active_at FROM sessions WHERE id = 'ended-reactivate'").first()).toEqual({ state: "active", inactive_at: null, last_active_at: later });
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

  it("serves live dashboard sessions, requests, objects, overview, and outcome updates", async () => {
    await env.DB.prepare("INSERT INTO sessions(id, started_at, ended_at, state, last_active_at, harness, boundary, repo, source_ref, model_primary, request_count, tokens_in, tokens_out, intent) VALUES ('dashboard-session', '2099-01-01T00:00:00Z', '2099-01-01T00:01:00Z', 'inactive', '2099-01-01T00:01:00Z', 'OpenCode', 'header', 'mimir', 'master', 'openai/test', 1, 7, 3, 'Connect live dashboard data')").run();
    await env.DB.prepare("INSERT INTO exchanges(id, session_id, ts, endpoint, model, latency_ms, repo, harness, r2_key, provider, finish_reason, access_token_label, input_tokens, output_tokens, capture_status, capture_reason, accepted_at, saved_at) VALUES ('dashboard-exchange', 'dashboard-session', '2099-01-01T00:00:30Z', '/v1/chat/completions', 'openai/test', 250, 'mimir', 'OpenCode', 'log/2099/01/01/dashboard-exchange.json', 'OpenAI', 'stop', 'test', 7, 3, 'saved', 'enabled', '2099-01-01T00:00:30Z', '2099-01-01T00:00:31Z')").run();
    await env.DB.prepare("INSERT INTO session_files(session_id, file) VALUES ('dashboard-session', 'worker/web/src/lib/api.ts')").run();
    await env.DB.prepare("INSERT INTO session_errors(session_id, signature) VALUES ('dashboard-session', 'example failure')").run();
    const envelope = { schema_version: 1, exchange_id: "dashboard-exchange", session_id: "dashboard-session", captured_at: "2099-01-01T00:00:30Z", endpoint: "/v1/chat/completions", request: { messages: [{ role: "user", content: "Connect live dashboard data" }] }, response: { format: "json", body: { choices: [] } } };
    await env.LOGS.put("log/2099/01/01/dashboard-exchange.json", JSON.stringify(envelope));

    const sessions = await (await dashboardRequest("/dashboard/api/sessions")).json() as { sessions: Array<{ id: string; capture: { status: string } }> };
    expect(sessions.sessions).toContainEqual(expect.objectContaining({ id: "dashboard-session", capture: expect.objectContaining({ status: "saved" }) }));

    const session = await (await dashboardRequest("/dashboard/api/sessions/dashboard-session")).json() as { files: string[]; errors: string[]; exchanges: Array<{ id: string }> };
    expect(session.files).toEqual(["worker/web/src/lib/api.ts"]);
    expect(session.errors).toEqual(["example failure"]);
    expect(session.exchanges).toContainEqual(expect.objectContaining({ id: "dashboard-exchange" }));

    const log = await (await dashboardRequest("/dashboard/api/log?limit=50")).json() as { exchanges: Array<{ id: string }>; next_cursor: string | null };
    expect(log.exchanges).toContainEqual(expect.objectContaining({ id: "dashboard-exchange" }));
    expect(log.next_cursor).toBeNull();

    const detail = await (await dashboardRequest("/dashboard/api/log/dashboard-exchange")).json() as { log_url: string };
    expect(detail.log_url).toBe("/dashboard/log-objects/log/2099/01/01/dashboard-exchange.json");
    expect(await (await dashboardRequest(detail.log_url)).json()).toEqual(envelope);

    const overview = await (await dashboardRequest("/dashboard/api/overview")).json() as { totals: { requests: number; sessions: number; saved_exchanges: number }; models: Array<{ name: string }> };
    expect(overview.totals).toMatchObject({ requests: 1, sessions: 1, saved_exchanges: 1 });
    expect(overview.models).toContainEqual(expect.objectContaining({ name: "openai/test" }));

    const updated = await dashboardRequest("/dashboard/api/sessions/dashboard-session/outcome", { method: "POST", headers: { "content-type": "application/json" }, body: JSON.stringify({ outcome: "landed", reason: "Live data verified" }) });
    expect(updated.status).toBe(200);
    expect(await updated.json()).toMatchObject({ id: "dashboard-session", outcome: "landed", outcome_src: "user", outcome_reason: "Live data verified" });

  });

  it("derives session intent from the first user message and keeps it sticky", async () => {
    vi.stubGlobal("fetch", vi.fn().mockImplementation(() => Promise.resolve(Response.json({ choices: [], usage: { prompt_tokens: 1, completion_tokens: 1 } }))));
    const headers = { authorization: "Bearer machine-token", "content-type": "application/json", "x-mimir-session": "intent-session" };
    await request("/v1/chat/completions", { method: "POST", headers, body: JSON.stringify({ model: "openai/test", messages: [{ role: "system", content: "ignored" }, { role: "user", content: "  Fix the   login redirect\nloop " }] }) });
    expect(await env.DB.prepare("SELECT intent FROM sessions WHERE id = 'intent-session'").first()).toEqual({ intent: "Fix the login redirect loop" });
    await request("/v1/chat/completions", { method: "POST", headers, body: JSON.stringify({ model: "openai/test", messages: [{ role: "user", content: "Something else entirely" }] }) });
    expect(await env.DB.prepare("SELECT intent FROM sessions WHERE id = 'intent-session'").first()).toEqual({ intent: "Fix the login redirect loop" });
    const found = await request("/search", { method: "POST", headers, body: JSON.stringify({ query: "login redirect", types: ["intent"] }) });
    const result = await found.json() as { matches: { session_id: string }[] };
    expect(result.matches.map((match) => match.session_id)).toContain("intent-session");
  });

  it("filters search matches by requested types", async () => {
    await env.DB.prepare("INSERT INTO sessions(id, started_at, state, last_active_at, boundary, intent) VALUES ('typed-session', '2026-01-01T00:00:00Z', 'inactive', '2026-01-01T00:00:00Z', 'header', 'zebra intent only')").run();
    await env.DB.prepare("INSERT INTO exchanges(id, session_id, ts, endpoint, latency_ms, r2_key, capture_status, saved_at, request_excerpt, response_excerpt) VALUES ('typed-exchange', 'typed-session', '2026-01-01T00:00:00Z', 'chat', 1, 'log/typed.json', 'saved', '2026-01-01T00:00:01Z', 'plain request', 'plain response')").run();
    await env.DB.prepare("INSERT INTO session_files(session_id, file) VALUES ('typed-session', 'src/zebra.ts')").run();
    const headers = { authorization: "Bearer machine-token", "content-type": "application/json" };
    const search = (body: unknown) => request("/search", { method: "POST", headers, body: JSON.stringify(body) });
    const files = await (await search({ query: "zebra", types: ["files"] })).json() as { matches: unknown[] };
    expect(files.matches).toHaveLength(1);
    const excerpts = await (await search({ query: "zebra", types: ["excerpts"] })).json() as { matches: unknown[] };
    expect(excerpts.matches).toHaveLength(0);
    const intent = await (await search({ query: "zebra", types: ["intent"] })).json() as { matches: unknown[] };
    expect(intent.matches).toHaveLength(1);
    const invalid = await search({ query: "zebra", types: ["bogus"] });
    expect(invalid.status).toBe(400);
  });

  it("verifies Cloudflare Access JWTs for dashboard APIs", async () => {
    const teamDomain = "https://team.cloudflareaccess.com";
    const { publicKey, privateKey } = await generateKeyPair("RS256");
    const jwk = await exportJWK(publicKey);
    jwk.kid = "test-key";
    jwk.alg = "RS256";
    vi.stubGlobal("fetch", vi.fn().mockImplementation((input: RequestInfo | URL) => {
      const url = String(input);
      if (url === `${teamDomain}/cdn-cgi/access/certs`) return Promise.resolve(Response.json({ keys: [jwk] }));
      return Promise.reject(new Error(`unexpected fetch ${url}`));
    }));
    const bindings = env as Env & { DASHBOARD_ACCESS_AUD?: string; DASHBOARD_ACCESS_TEAM_DOMAIN?: string };
    bindings.DASHBOARD_ACCESS_AUD = "test-aud";
    bindings.DASHBOARD_ACCESS_TEAM_DOMAIN = teamDomain;
    const sign = (init: { audience?: string; issuer?: string; expiration?: number } = {}) =>
      new SignJWT({})
        .setProtectedHeader({ alg: "RS256", kid: "test-key" })
        .setIssuer(init.issuer ?? teamDomain)
        .setAudience(init.audience ?? "test-aud")
        .setIssuedAt()
        .setExpirationTime(init.expiration ?? Math.floor(Date.now() / 1000) + 300)
        .sign(privateKey);
    try {
      const valid = await request("/dashboard/api/bootstrap", { headers: { "cf-access-jwt-assertion": await sign() } });
      expect(valid.status).toBe(200);
      const wrongAudience = await request("/dashboard/api/bootstrap", { headers: { "cf-access-jwt-assertion": await sign({ audience: "other-aud" }) } });
      expect(wrongAudience.status).toBe(403);
      const wrongIssuer = await request("/dashboard/api/bootstrap", { headers: { "cf-access-jwt-assertion": await sign({ issuer: "https://evil.example.com" }) } });
      expect(wrongIssuer.status).toBe(403);
      const expired = await request("/dashboard/api/bootstrap", { headers: { "cf-access-jwt-assertion": await sign({ expiration: Math.floor(Date.now() / 1000) - 300 }) } });
      expect(expired.status).toBe(403);
      const garbage = await request("/dashboard/api/bootstrap", { headers: { "cf-access-jwt-assertion": "not-a-jwt" } });
      expect(garbage.status).toBe(403);
    } finally {
      delete bindings.DASHBOARD_ACCESS_AUD;
      delete bindings.DASHBOARD_ACCESS_TEAM_DOMAIN;
    }
  });
});

describe("Session object", () => {
  const authHeaders = { authorization: "Bearer machine-token", "content-type": "application/json" };
  const postEvent = (id: string, event: Record<string, unknown>) => request(`/sessions/${id}/events`, { method: "POST", headers: authHeaders, body: JSON.stringify(event) });
  const objectState = async (id: string) => {
    const response = await request(`/sessions/${id}/object-state`, { headers: { authorization: "Bearer machine-token" } });
    return { status: response.status, body: await response.json<Record<string, unknown>>() };
  };

  it("tracks turn events and projects liveness", async () => {
    const accepted = await postEvent("object-live", { version: 1, kind: "turn", ts: new Date().toISOString(), turn: { model: "openai/test", usage: { input_tokens: 5, output_tokens: 3 }, latency_ms: 42 } });
    expect(accepted.status).toBe(200);
    const { body } = await objectState("object-live");
    expect(body).toMatchObject({ session_id: "object-live", liveness: "active", turn_count: 1, tokens_in: 5, tokens_out: 3, finalized_at: null });
  });

  it("reports the machine API version and capabilities", async () => {
		const response = await request("/whoami", { headers: { authorization: "Bearer machine-token" } });
		expect(response.status).toBe(200);
		await expect(response.json()).resolves.toMatchObject({
			service: "mimir",
			api_version: 1,
			capabilities: expect.arrayContaining(["hermes_authorization", "session_events", "session_lifecycle"]),
		});
	});

  it("deduplicates retried turn events by exchange ID", async () => {
		const event = { version: 1, kind: "turn", ts: new Date().toISOString(), turn: { exchange_id: "retry-1", model: "openai/test", usage: { input_tokens: 5, output_tokens: 3 } } };
		expect((await postEvent("object-retry", event)).status).toBe(200);
		expect((await postEvent("object-retry", { ...event, ts: new Date().toISOString() })).status).toBe(200);
		const { body } = await objectState("object-retry");
		expect(body).toMatchObject({ turn_count: 1, tokens_in: 5, tokens_out: 3 });
	});

  it("rejects invalid events and requires auth", async () => {
    expect((await postEvent("object-invalid", { version: 1, kind: "note", ts: new Date().toISOString() })).status).toBe(400);
    expect((await request("/sessions/object-invalid/events", { method: "POST", headers: { "content-type": "application/json" }, body: "{}" })).status).toBe(401);
    expect((await postEvent("object-invalid", { version: 1, kind: "turn", ts: new Date().toISOString(), turn: { usage: { input_tokens: -1, output_tokens: 0 } } })).status).toBe(400);
  });

  it("does not reopen a finalized session for duplicate turns or stale heartbeats", async () => {
		const beforeEnd = new Date(Date.now() - 1_000).toISOString();
		const turn = { version: 1, kind: "turn", ts: beforeEnd, turn: { exchange_id: "finalized-retry", model: "openai/test" } };
		await postEvent("object-finalized-retry", turn);
		await postEvent("object-finalized-retry", { version: 1, kind: "end", ts: new Date().toISOString(), reason: "done" });
		await postEvent("object-finalized-retry", turn);
		await postEvent("object-finalized-retry", { version: 1, kind: "heartbeat", ts: beforeEnd });
		const { body } = await objectState("object-finalized-retry");
		expect(body).toMatchObject({ liveness: "finalized", turn_count: 1, end_reason: "done" });
	});

  it("projects disconnected after the liveness window without finalizing", async () => {
    const stale = new Date(Date.now() - 3 * 60_000).toISOString();
    await postEvent("object-stale", { version: 1, kind: "heartbeat", ts: stale });
    const { body } = await objectState("object-stale");
    expect(body).toMatchObject({ liveness: "disconnected", finalized_at: null });
  });

  it("finalizes on an end event: transcript in R2, session inactive in D1", async () => {
    await postEvent("object-end", { version: 1, kind: "turn", ts: new Date().toISOString(), turn: { model: "openai/test", usage: { input_tokens: 2, output_tokens: 1 } } });
    const ended = await postEvent("object-end", { version: 1, kind: "end", ts: new Date().toISOString(), reason: "user closed" });
    expect(ended.status).toBe(200);
    const { body } = await objectState("object-end");
    expect(body).toMatchObject({ liveness: "finalized", end_reason: "user closed", turn_count: 1 });
    expect(typeof body.finalized_at).toBe("string");
    const session = await env.DB.prepare("SELECT state, ended_at, inactive_at FROM sessions WHERE id = 'object-end'").first<{ state: string; ended_at: string | null; inactive_at: string | null }>();
    expect(session?.state).toBe("inactive");
    expect(session?.ended_at).toBeTruthy();
    const transcript = await env.LOGS.get("sessions/object-end/transcript.json");
    expect(transcript).not.toBeNull();
    const manifest = JSON.parse(await transcript!.text());
    expect(manifest).toMatchObject({ schema_version: 1, session_id: "object-end", end_reason: "user closed", turn_count: 1, usage: { input_tokens: 2, output_tokens: 1 } });
  });

  it("reopens a finalized session when new events arrive", async () => {
    await postEvent("object-reopen", { version: 1, kind: "end", ts: new Date().toISOString() });
    expect((await objectState("object-reopen")).body.liveness).toBe("finalized");
    expect((await env.DB.prepare("SELECT state FROM sessions WHERE id = 'object-reopen'").first<{ state: string }>())?.state).toBe("inactive");
    await postEvent("object-reopen", { version: 1, kind: "turn", ts: new Date().toISOString(), turn: { model: "openai/test" } });
    const { body } = await objectState("object-reopen");
    expect(body).toMatchObject({ liveness: "active", finalized_at: null, turn_count: 1 });
    expect((await env.DB.prepare("SELECT state FROM sessions WHERE id = 'object-reopen'").first<{ state: string }>())?.state).toBe("active");
  });

  it("requires a websocket upgrade for the live feed", async () => {
    const response = await request("/sessions/object-live/live", { headers: { authorization: "Bearer machine-token" } });
    expect(response.status).toBe(426);
  });

  it("serves a live feed snapshot over websocket", async () => {
    await postEvent("object-feed", { version: 1, kind: "turn", ts: new Date().toISOString(), turn: { model: "openai/test", excerpt: "hello" } });
    const ctx = createExecutionContext();
    const response = await worker.fetch(new Request("https://mimir.test/sessions/object-feed/live", { headers: { authorization: "Bearer machine-token", upgrade: "websocket" } }), env as Env & { OPENROUTER_API_KEY: string }, ctx);
    expect(response.status).toBe(101);
    const socket = response.webSocket!;
    socket.accept();
    const message = await new Promise<string>((resolve, reject) => {
      const timer = setTimeout(() => reject(new Error("snapshot timeout")), 5_000);
      socket.addEventListener("message", (event) => {
        clearTimeout(timer);
        resolve(String(event.data));
      }, { once: true });
    });
    const snapshot = JSON.parse(message);
    expect(snapshot.type).toBe("snapshot");
    expect(snapshot.state).toMatchObject({ session_id: "object-feed", liveness: "active", turn_count: 1 });
    expect(snapshot.turns).toHaveLength(1);
    socket.close(1000);
  });

  it("reports proxied exchanges to the session object", async () => {
    const stream = 'data: {"choices":[{"delta":{"content":"hi"}}]}\n\ndata: {"usage":{"prompt_tokens":4,"completion_tokens":2}}\n\ndata: [DONE]\n';
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(new Response(stream, { status: 200, headers: { "content-type": "text/event-stream" } })));
    const response = await request("/v1/chat/completions", {
      method: "POST",
      headers: { ...authHeaders, "x-mimir-session": "object-proxied", "x-mimir-harness": "test" },
      body: JSON.stringify({ model: "openai/test", messages: [{ role: "user", content: "hello" }], stream: true }),
    });
    expect(response.status).toBe(200);
    await response.text();
    const { body } = await objectState("object-proxied");
    expect(body).toMatchObject({ session_id: "object-proxied", liveness: "active", turn_count: 1, tokens_in: 4, tokens_out: 2, harness: "test" });
  });

  it("finalizes the session object on explicit end", async () => {
    await postEvent("object-explicit-end", { version: 1, kind: "turn", ts: new Date().toISOString(), turn: { model: "openai/test" } });
    await env.DB.prepare("INSERT OR IGNORE INTO sessions(id, started_at, last_active_at, harness, boundary) VALUES ('object-explicit-end', ?, ?, 'test', 'header')").bind(new Date().toISOString(), new Date().toISOString()).run();
    const ended = await request("/sessions/object-explicit-end/end", { method: "POST", headers: { authorization: "Bearer machine-token" } });
    expect(ended.status).toBe(200);
    const { body } = await objectState("object-explicit-end");
    expect(body).toMatchObject({ liveness: "finalized", end_reason: "explicit" });
    expect(await env.LOGS.get("sessions/object-explicit-end/transcript.json")).not.toBeNull();
  });

  it("ends sessions known only to the session object", async () => {
    await postEvent("object-only-end", { version: 1, kind: "turn", ts: new Date().toISOString(), turn: { model: "openai/test" } });
    expect(await env.DB.prepare("SELECT 1 FROM sessions WHERE id = 'object-only-end'").first()).toBeNull();
    const ended = await request("/sessions/object-only-end/end", { method: "POST", headers: { authorization: "Bearer machine-token" } });
    expect(ended.status).toBe(200);
    expect((await env.DB.prepare("SELECT state FROM sessions WHERE id = 'object-only-end'").first<{ state: string }>())?.state).toBe("inactive");
    const { body } = await objectState("object-only-end");
    expect(body).toMatchObject({ liveness: "finalized", end_reason: "explicit" });
    expect(await env.LOGS.get("sessions/object-only-end/transcript.json")).not.toBeNull();
  });

  it("keeps the 404 contract for sessions unknown to D1 and the object", async () => {
    const response = await request("/sessions/object-never-seen/end", { method: "POST", headers: { authorization: "Bearer machine-token" } });
    expect(response.status).toBe(404);
  });
});
