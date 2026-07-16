import type { Hono } from "hono";
import { readConfig, validateConfigValues } from "../config";
import { buildUpstreamHeaders, proxy } from "../proxy";
import { canonicalOutcome, expireSessions, SESSION_COLUMNS, updateOutcome } from "../sessions";
import { attachCaptureSummary, CAPTURE_SUMMARY_COLUMNS, captureSummary, reconcile } from "../storage";
import type { AppEnv } from "../types";

export function registerMachineRoutes(app: Hono<AppEnv>) {
  app.get("/whoami", async (c) => {
    const [sessions, exchanges] = await Promise.all([
      c.env.DB.prepare("SELECT COUNT(*) AS count FROM sessions").first<{ count: number }>(),
      c.env.DB.prepare("SELECT COUNT(*) AS count FROM exchanges WHERE capture_status = 'saved'").first<{ count: number }>(),
    ]);
    return c.json({ url: new URL(c.req.url).origin, sessions: sessions?.count ?? 0, log: exchanges?.count ?? 0 });
  });

  app.get("/sessions", async (c) => {
    await expireSessions(c.env.DB);
    const where: string[] = [];
    const values: string[] = [];
    for (const [field, column] of [["repo", "repo"], ["model", "model_primary"]] as const) {
      const value = c.req.query(field);
      if (value) {
        where.push(`${column} = ?`);
        values.push(value);
      }
    }
    const outcome = c.req.query("outcome");
    if (outcome) {
      const canonical = canonicalOutcome(outcome);
      if (!canonical) return c.json({ error: "invalid outcome" }, 400);
      where.push("work_outcome = ?");
      values.push(canonical);
    }
    const from = c.req.query("from");
    if (from) {
      where.push("started_at >= ?");
      values.push(from);
    }
    const to = c.req.query("to");
    if (to) {
      where.push("started_at <= ?");
      values.push(to);
    }
    const sql = `SELECT ${SESSION_COLUMNS}, ${CAPTURE_SUMMARY_COLUMNS} FROM sessions ${where.length ? `WHERE ${where.join(" AND ")}` : ""} ORDER BY started_at DESC LIMIT 100`;
    const results = await c.env.DB.prepare(sql).bind(...values).all<Record<string, unknown>>();
    return c.json({ sessions: results.results.map(attachCaptureSummary) });
  });

  app.get("/sessions/:id", async (c) => {
    await expireSessions(c.env.DB);
    const session = await c.env.DB.prepare(`SELECT ${SESSION_COLUMNS} FROM sessions WHERE id = ?`).bind(c.req.param("id")).first();
    if (!session) return c.json({ error: "session not found" }, 404);
    const [exchanges, files, errors, capture, outcomeEvents] = await Promise.all([
      c.env.DB.prepare("SELECT * FROM exchanges WHERE session_id = ? ORDER BY ts").bind(c.req.param("id")).all(),
      c.env.DB.prepare("SELECT file FROM session_files WHERE session_id = ? ORDER BY file").bind(c.req.param("id")).all<{ file: string }>(),
      c.env.DB.prepare("SELECT signature FROM session_errors WHERE session_id = ? ORDER BY signature").bind(c.req.param("id")).all<{ signature: string }>(),
      captureSummary(c.env.DB, c.req.param("id")),
      c.env.DB.prepare("SELECT id, outcome, source, reason, evidence_json, created_at FROM session_outcome_events WHERE session_id = ? ORDER BY created_at DESC").bind(c.req.param("id")).all(),
    ]);
    return c.json({ session, capture, outcome_events: outcomeEvents.results, files: files.results.map((row) => row.file), errors: errors.results.map((row) => row.signature), exchanges: exchanges.results });
  });

  app.get("/sessions/:id/status", async (c) => {
    const session = await c.env.DB.prepare("SELECT work_outcome AS outcome, outcome_src, outcome_updated_at, outcome_reason FROM sessions WHERE id = ?").bind(c.req.param("id")).first();
    if (!session) return c.json({ error: "session not found" }, 404);
    return c.json({ session_id: c.req.param("id"), capture: await captureSummary(c.env.DB, c.req.param("id")), ...session });
  });

  app.post("/sessions/:id/mark", async (c) => {
    const body = await c.req.json<{ outcome?: string; source?: string; reason?: unknown; evidence?: unknown }>();
    return updateOutcome(c, { ...body, source: "agent" }, "agent");
  });

  app.post("/sessions/:id/outcome", async (c) => {
    const body = await c.req.json<{ outcome?: string; source?: string; reason?: unknown; evidence?: unknown }>();
    return updateOutcome(c, { ...body, source: "agent" }, "agent");
  });

  app.post("/reconcile", async (c) => c.json(await reconcile(
    c.env,
    Number(c.req.query("limit") ?? 100),
    c.req.query("cursor"),
    c.req.query("database_cursor"),
    c.req.query("scan_database") !== "false",
    c.req.query("scan_r2") !== "false",
  )));

  app.get("/log/*", async (c) => {
    const key = c.req.path.replace(/^\/log\//, "");
    if (!key.startsWith("log/")) return c.json({ error: "invalid log key" }, 400);
    const object = await c.env.LOGS.get(key);
    if (!object) return c.json({ error: "log not found" }, 404);
    return new Response(object.body, { headers: { "content-type": "application/json" } });
  });

  app.post("/search", async (c) => {
    await expireSessions(c.env.DB);
    const body = await c.req.json<{ query?: string; types?: string[]; budget?: number; filters?: { repo?: string; outcome?: string } }>();
    const query = body.query?.trim() ?? "";
    const budget = Math.max(1, Math.min(body.budget ?? 4000, 16000));
    const filters = body.filters ?? {};
    const where = ["(s.intent LIKE ? OR e.request_excerpt LIKE ? OR e.response_excerpt LIKE ? OR EXISTS (SELECT 1 FROM session_files sf WHERE sf.session_id = s.id AND sf.file LIKE ?) OR EXISTS (SELECT 1 FROM session_errors se WHERE se.session_id = s.id AND se.signature LIKE ?))"];
    const needle = `%${query}%`;
    const values: string[] = [needle, needle, needle, needle, needle];
    if (filters.repo) {
      where.push("s.repo = ?");
      values.push(filters.repo);
    }
    if (filters.outcome) {
      const canonical = canonicalOutcome(filters.outcome);
      if (!canonical) return c.json({ error: "invalid outcome" }, 400);
      where.push("s.work_outcome = ?");
      values.push(canonical);
    }
    where.push("e.capture_status = 'saved'");
    const sql = `SELECT s.id AS session_id, s.started_at, s.work_outcome AS outcome, s.repo, s.model_primary, e.id AS exchange_id, e.request_excerpt, e.response_excerpt, e.r2_key FROM sessions s JOIN exchanges e ON e.session_id = s.id WHERE ${where.join(" AND ")} ORDER BY s.started_at DESC LIMIT 50`;
    const result = await c.env.DB.prepare(sql).bind(...values).all<Record<string, unknown>>();
    const matches: Record<string, unknown>[] = [];
    let used = 0;
    for (const row of result.results) {
      const cost = JSON.stringify(row).length;
      if (matches.length && used + cost > budget * 4) break;
      matches.push(row);
      used += cost;
    }
    return c.json({ query, budget, matches });
  });

  app.get("/config", async (c) => c.json(await readConfig(c.env.DB)));

  app.put("/config", async (c) => {
    const values = await c.req.json<Record<string, unknown>>();
    const validation = validateConfigValues(values);
    if (validation) return c.json({ error: validation }, 400);
    const statements = Object.entries(values).map(([key, value]) => c.env.DB.prepare("INSERT INTO config(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value").bind(key, JSON.stringify(value)));
    if (statements.length) await c.env.DB.batch(statements);
    return c.json(await readConfig(c.env.DB));
  });

  app.post("/v1/chat/completions", (c) => proxy(c, "chat"));
  app.post("/v1/messages", (c) => proxy(c, "messages"));
  app.get("/v1/models", async (c) => {
    const response = await fetch("https://openrouter.ai/api/v1/models", { headers: buildUpstreamHeaders(c.req.raw.headers, c.env.OPENROUTER_API_KEY) });
    return new Response(response.body, response);
  });
}
