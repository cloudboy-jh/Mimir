import { createExecutionContext, env, waitOnExecutionContext } from "cloudflare:test";
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from "vitest";
import worker from "../src/index";

const schema = `
CREATE TABLE access_tokens (token_hash TEXT PRIMARY KEY, label TEXT NOT NULL, created_at TEXT NOT NULL, last_used_at TEXT, revoked_at TEXT);
CREATE TABLE sessions (id TEXT PRIMARY KEY, started_at TEXT NOT NULL, ended_at TEXT, boundary TEXT NOT NULL, outcome TEXT NOT NULL DEFAULT 'unknown', outcome_src TEXT, repo TEXT, source_ref TEXT, model_primary TEXT, request_count INTEGER NOT NULL DEFAULT 0, tokens_in INTEGER NOT NULL DEFAULT 0, tokens_out INTEGER NOT NULL DEFAULT 0, files TEXT NOT NULL DEFAULT '[]', errors TEXT NOT NULL DEFAULT '[]', intent TEXT, log_refs TEXT NOT NULL DEFAULT '[]');
CREATE TABLE exchanges (id TEXT PRIMARY KEY, session_id TEXT NOT NULL, ts TEXT NOT NULL, endpoint TEXT NOT NULL, model TEXT, request_excerpt TEXT NOT NULL DEFAULT '', response_excerpt TEXT NOT NULL DEFAULT '', usage_json TEXT NOT NULL DEFAULT '{}', latency_ms INTEGER NOT NULL, repo TEXT, harness TEXT, r2_key TEXT NOT NULL);
CREATE TABLE config (key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE session_files (session_id TEXT NOT NULL, file TEXT NOT NULL, PRIMARY KEY(session_id, file));
CREATE TABLE session_errors (session_id TEXT NOT NULL, signature TEXT NOT NULL, PRIMARY KEY(session_id, signature));
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
  await env.DB.exec("DELETE FROM session_files; DELETE FROM session_errors; DELETE FROM exchanges; DELETE FROM sessions; DELETE FROM config; DELETE FROM access_tokens;");
  await env.DB.prepare("INSERT INTO access_tokens(token_hash, label, created_at) VALUES (?, 'test', '2026-01-01T00:00:00Z')").bind(await tokenHash("machine-token")).run();
  const objects = await env.LOGS.list();
  await Promise.all(objects.objects.map((object) => env.LOGS.delete(object.key)));
});

afterEach(() => vi.unstubAllGlobals());

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
    expect(await response.text()).toBe(stream);
    const upstream = vi.mocked(fetch).mock.calls[0];
    const upstreamHeaders = new Headers((upstream[1] as RequestInit).headers);
    expect(upstreamHeaders.get("authorization")).toBe("Bearer test-openrouter-key");
    expect(upstreamHeaders.get("x-mimir-session")).toBeNull();
    const session = await env.DB.prepare("SELECT request_count, tokens_in, tokens_out FROM sessions WHERE id = 'session-1'").first<{ request_count: number; tokens_in: number; tokens_out: number }>();
    expect(session).toEqual({ request_count: 1, tokens_in: 5, tokens_out: 3 });
    expect(await env.DB.prepare("SELECT file FROM session_files WHERE session_id = 'session-1'").first<{ file: string }>()).toEqual({ file: "src/auth.ts" });
    const exchange = await env.DB.prepare("SELECT r2_key FROM exchanges WHERE session_id = 'session-1'").first<{ r2_key: string }>();
    expect(exchange?.r2_key).toMatch(/^log\//);
    const object = await env.LOGS.get(exchange!.r2_key);
    expect(await object!.text()).not.toContain("private-value");
  });

  it("does not persist when saving is disabled", async () => {
    await env.DB.prepare("INSERT INTO config(key, value) VALUES('save.enabled', 'false')").run();
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(Response.json({ choices: [] })));
    const response = await request("/v1/chat/completions", { method: "POST", headers: { "x-api-key": "machine-token", "content-type": "application/json" }, body: JSON.stringify({ model: "openai/test", messages: [] }) });
    expect(response.status).toBe(200);
    expect((await env.DB.prepare("SELECT COUNT(*) AS count FROM exchanges").first<{ count: number }>())?.count).toBe(0);
    expect((await env.LOGS.list()).objects).toHaveLength(0);
  });
});
