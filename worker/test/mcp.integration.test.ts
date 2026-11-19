import { describe, expect, it } from "vitest";
import { env, request } from "./support";

type Json = Record<string, any>;

async function rpc(body: unknown, token = "machine-token") {
  const response = await request("/mcp", {
    method: "POST",
    headers: { authorization: `Bearer ${token}`, "content-type": "application/json", accept: "application/json, text/event-stream" },
    body: typeof body === "string" ? body : JSON.stringify(body),
  });
  const text = await response.text();
  return { status: response.status, body: text ? (JSON.parse(text) as Json) : null };
}

const call = (id: number, name: string, args?: unknown) => rpc({ jsonrpc: "2.0", id, method: "tools/call", params: { name, arguments: args } });

describe("MCP endpoint", () => {
  it("negotiates initialize, acknowledges notifications and answers ping", async () => {
    const init = await rpc({ jsonrpc: "2.0", id: 1, method: "initialize", params: { protocolVersion: "2025-03-26", capabilities: {}, clientInfo: { name: "test", version: "1" } } });
    expect(init).toEqual({
      status: 200,
      body: { jsonrpc: "2.0", id: 1, result: { protocolVersion: "2025-03-26", capabilities: { tools: {} }, serverInfo: { name: "mimir", version: "dev" } } },
    });
    expect((await rpc({ jsonrpc: "2.0", id: "s", method: "initialize", params: { protocolVersion: "1999-01-01" } })).body!.result.protocolVersion).toBe("2025-06-18");
    expect(await rpc({ jsonrpc: "2.0", method: "notifications/initialized" })).toEqual({ status: 202, body: null });
    expect(await rpc({ jsonrpc: "2.0", id: 2, method: "ping" })).toEqual({ status: 200, body: { jsonrpc: "2.0", id: 2, result: {} } });
  });

  it("lists every tool with a description and input schema", async () => {
    const listed = await rpc({ jsonrpc: "2.0", id: 3, method: "tools/list" });
    const tools = listed.body!.result.tools as Json[];
    expect(tools.map((tool) => tool.name)).toEqual(["search", "sessions", "session", "turns", "stats", "day", "devices", "commits", "commit_patch"]);
    for (const tool of tools) {
      expect(tool.description.length).toBeGreaterThan(20);
      expect(tool.inputSchema.type).toBe("object");
    }
    expect(tools.find((tool) => tool.name === "stats")!.inputSchema.required).toEqual(["group_by"]);
    expect(tools.find((tool) => tool.name === "commit_patch")!.inputSchema.required).toEqual(["session_id", "sha"]);
  });

  it("calls the same query functions as the HTTP API", async () => {
    await env.DB.exec(`
      INSERT INTO sessions(id, started_at, last_active_at, state, boundary, repo, intent) VALUES ('mcp-session', '2026-09-01T09:00:00.000Z', '2026-09-01T09:00:00.000Z', 'inactive', 'header', 'mimir', 'Tune the cache');
      INSERT INTO exchanges(id, session_id, ts, endpoint, latency_ms, r2_key, capture_status, accepted_at, saved_at) VALUES ('mcp-ex', 'mcp-session', '2026-09-01T09:00:00.000Z', 'chat', 1, 'log/mcp-ex.json', 'saved', '2026-09-01T09:00:00.000Z', '2026-09-01T09:00:01.000Z');
      INSERT INTO exchange_search(exchange_id, body) VALUES ('mcp-ex', 'cache eviction policy');
      INSERT INTO session_git_artifacts(session_id, commit_sha, provenance, patch_r2_key, patch_sha256, patch_bytes, patch_files, patch_additions, patch_deletions, capture_status, accepted_at, saved_at, created_at, repository_key) VALUES ('mcp-session', '${"c".repeat(40)}', 'git', 'sessions/mcp-session/git/${"c".repeat(40)}/p.patch', '${"0".repeat(64)}', 12, 1, 1, 0, 'saved', '2026-09-01T09:00:00.000Z', '2026-09-01T09:00:00.000Z', '2026-09-01T09:00:00.000Z', 'session:mcp-session');
    `);
    await env.LOGS.put(`sessions/mcp-session/git/${"c".repeat(40)}/p.patch`, "+cache line\n");
    await env.LOGS.put("log/mcp-ex.json", JSON.stringify({ exchange_id: "mcp-ex", request: { messages: [{ role: "user", content: "Tune it" }] }, response: { format: "json", body: { role: "assistant", content: "Tuned." } } }));
    const http = async (path: string) => (await request(path, { headers: { authorization: "Bearer machine-token" } })).json();
    const structured = async (name: string, args?: unknown) => {
      const result = (await call(10, name, args)).body!.result;
      expect(result.isError).toBeUndefined();
      expect(JSON.parse(result.content[0].text)).toEqual(result.structuredContent);
      return result.structuredContent;
    };
    expect(await structured("search", { query: "cache" })).toEqual(await http("/api/search?q=cache"));
    expect(await structured("sessions", { repo: "mimir", limit: 5 })).toEqual(await http("/api/sessions?repo=mimir&limit=5"));
    expect(await structured("session", { id: "mcp-session" })).toEqual(await http("/api/sessions/mcp-session"));
    expect(await structured("turns", { id: "mcp-session", order: "desc" })).toEqual(await http("/api/sessions/mcp-session/turns?order=desc"));
    expect(await structured("stats", { group_by: "repo", from: "2026-09-01", to: "2026-09-01" })).toMatchObject({ groups: [{ key: "mimir", sessions: 1, commits: 1 }] });
    expect(await structured("day", { date: "2026-09-01" })).toEqual(await http("/api/days/2026-09-01"));
    expect((await structured("day", {})).date).toMatch(/^\d{4}-\d{2}-\d{2}$/);
    expect(await structured("devices")).toEqual(await http("/api/devices"));
    expect(await structured("commits", { session: "mcp-session" })).toEqual(await http("/api/commits?session=mcp-session"));
    expect(await structured("commit_patch", { session_id: "mcp-session", sha: "c".repeat(40), max_bytes: 6 })).toEqual({
      session_id: "mcp-session",
      sha: "c".repeat(40),
      patch: "+cache",
      truncated: true,
      patch_bytes: 12,
    });
  });

  it("reports tool failures as results and protocol failures as JSON-RPC errors", async () => {
    const failure = (await call(20, "session", { id: "missing" })).body!;
    expect(failure).toEqual({ jsonrpc: "2.0", id: 20, result: { content: [{ type: "text", text: "session not found" }], isError: true } });
    expect((await call(21, "stats", {})).body!.result).toMatchObject({ isError: true, content: [{ text: "missing required argument: group_by" }] });
    expect((await call(22, "devices", { extra: 1 })).body!.result).toMatchObject({ isError: true, content: [{ text: "unknown argument: extra" }] });
    expect((await call(23, "nope")).body!.error).toMatchObject({ code: -32602 });
    expect((await rpc({ jsonrpc: "2.0", id: 24, method: "resources/list" })).body).toEqual({ jsonrpc: "2.0", id: 24, error: { code: -32601, message: "method not found: resources/list" } });
    expect(await rpc("{not json")).toMatchObject({ status: 400, body: { error: { code: -32700 } } });
    expect(await rpc({ id: 1, method: "ping" })).toMatchObject({ status: 400, body: { error: { code: -32600 } } });
    const get = await request("/mcp", { headers: { authorization: "Bearer machine-token" } });
    expect(get.status).toBe(405);
    expect((await rpc({ jsonrpc: "2.0", id: 1, method: "ping" }, "wrong-token")).status).toBe(401);
  });
});
