import type { Hono } from "hono";
import { canonicalOutcome, expireSessions, SESSION_COLUMNS, updateOutcome } from "../sessions";
import { attachCaptureSummary, CAPTURE_SUMMARY_COLUMNS, captureSummary, sessionStatusResponse } from "../storage";
import type { AppEnv } from "../types";

export function registerDashboardRoutes(app: Hono<AppEnv>) {
  app.get("/dashboard/api/bootstrap", async (c) => {
    const [captures, sessions, latest] = await Promise.all([
      c.env.DB.prepare("SELECT COUNT(*) AS requests, SUM(CASE WHEN capture_status = 'saved' THEN 1 ELSE 0 END) AS saved_exchanges, SUM(CASE WHEN capture_status = 'failed' THEN 1 ELSE 0 END) AS capture_failures FROM exchanges").first(),
      c.env.DB.prepare("SELECT COUNT(*) AS count FROM sessions").first<{ count: number }>(),
      c.env.DB.prepare("SELECT ts FROM exchanges WHERE capture_status = 'saved' ORDER BY ts DESC LIMIT 1").first<{ ts: string }>(),
    ]);
    return c.json({ requests: captures?.requests ?? 0, saved_exchanges: captures?.saved_exchanges ?? 0, capture_failures: captures?.capture_failures ?? 0, sessions: sessions?.count ?? 0, latest_request_at: latest?.ts ?? null });
  });

  app.get("/dashboard/api/log", async (c) => {
    const limit = Math.max(1, Math.min(Number(c.req.query("limit") ?? 50), 100));
    const where: string[] = ["capture_status = 'saved'"];
    const values: string[] = [];
    for (const [field, column] of [["repo", "repo"], ["model", "model"], ["provider", "provider"], ["app", "harness"], ["session", "session_id"], ["finish_reason", "finish_reason"]] as const) {
      const value = c.req.query(field);
      if (value) {
        where.push(`${column} = ?`);
        values.push(value);
      }
    }
    const from = c.req.query("from");
    if (from) {
      where.push("ts >= ?");
      values.push(from);
    }
    const to = c.req.query("to");
    if (to) {
      where.push("ts <= ?");
      values.push(to);
    }
    const outcome = c.req.query("outcome");
    if (outcome) {
      const canonical = canonicalOutcome(outcome);
      if (!canonical) return c.json({ error: "invalid outcome" }, 400);
      where.push("session_id IN (SELECT id FROM sessions WHERE work_outcome = ?)");
      values.push(canonical);
    }
    const cursor = decodeCursor(c.req.query("cursor"));
    if (cursor) {
      where.push("(ts < ? OR (ts = ? AND id < ?))");
      values.push(cursor.ts, cursor.ts, cursor.id);
    }
    const sql = `SELECT id, session_id, ts, model, provider, finish_reason, endpoint, latency_ms, repo, harness, access_token_label, input_tokens, output_tokens, r2_key FROM exchanges ${where.length ? `WHERE ${where.join(" AND ")}` : ""} ORDER BY ts DESC, id DESC LIMIT ?`;
    const rows = await c.env.DB.prepare(sql).bind(...values, limit + 1).all<Record<string, unknown>>();
    const hasMore = rows.results.length > limit;
    const exchanges = rows.results.slice(0, limit);
    const last = exchanges.at(-1) as { ts?: string; id?: string } | undefined;
    return c.json({ exchanges, next_cursor: hasMore && last?.ts && last.id ? encodeCursor(last.ts, last.id) : null });
  });

  app.get("/dashboard/api/log/:id", async (c) => {
    const exchange = await c.env.DB.prepare("SELECT * FROM exchanges WHERE id = ?").bind(c.req.param("id")).first<Record<string, unknown>>();
    if (!exchange) return c.json({ error: "exchange not found" }, 404);
    return c.json({ exchange, log_url: `/dashboard/log-objects/${exchange.r2_key}` });
  });

  app.get("/dashboard/log-objects/*", async (c) => {
    const key = c.req.path.replace(/^\/dashboard\/log-objects\//, "");
    if (!key.startsWith("log/")) return c.json({ error: "invalid log key" }, 400);
    const object = await c.env.LOGS.get(key);
    if (!object) return c.json({ error: "log not found" }, 404);
    return new Response(object.body, { headers: { "content-type": "application/json", "cache-control": "no-store" } });
  });

  app.get("/dashboard/api/sessions", async (c) => {
    await expireSessions(c.env.DB);
    const result = await c.env.DB.prepare(`SELECT ${SESSION_COLUMNS}, ${CAPTURE_SUMMARY_COLUMNS} FROM sessions ORDER BY started_at DESC LIMIT 100`).all<Record<string, unknown>>();
    return c.json({ sessions: result.results.map(attachCaptureSummary) });
  });

  app.get("/dashboard/api/sessions/:id", async (c) => {
    await expireSessions(c.env.DB);
    const session = await c.env.DB.prepare(`SELECT ${SESSION_COLUMNS} FROM sessions WHERE id = ?`).bind(c.req.param("id")).first();
    if (!session) return c.json({ error: "session not found" }, 404);
    const [exchanges, files, errors, capture, outcomeEvents] = await Promise.all([
      c.env.DB.prepare("SELECT id, ts, model, provider, finish_reason, latency_ms, harness, input_tokens, output_tokens, capture_status, capture_reason, failure_code FROM exchanges WHERE session_id = ? ORDER BY ts").bind(c.req.param("id")).all(),
      c.env.DB.prepare("SELECT file FROM session_files WHERE session_id = ? ORDER BY file").bind(c.req.param("id")).all<{ file: string }>(),
      c.env.DB.prepare("SELECT signature FROM session_errors WHERE session_id = ? ORDER BY signature").bind(c.req.param("id")).all<{ signature: string }>(),
      captureSummary(c.env.DB, c.req.param("id")),
      c.env.DB.prepare("SELECT id, outcome, source, reason, evidence_json, created_at FROM session_outcome_events WHERE session_id = ? ORDER BY created_at DESC").bind(c.req.param("id")).all(),
    ]);
    return c.json({ session, capture, outcome_events: outcomeEvents.results, exchanges: exchanges.results, files: files.results.map((row) => row.file), errors: errors.results.map((row) => row.signature) });
  });

  app.get("/dashboard/api/sessions/:id/status", async (c) => {
    const session = await c.env.DB.prepare("SELECT work_outcome AS outcome, outcome_src, outcome_updated_at, outcome_reason FROM sessions WHERE id = ?").bind(c.req.param("id")).first();
    if (!session) return c.json({ error: "session not found" }, 404);
    const capture = await captureSummary(c.env.DB, c.req.param("id"));
    c.header("cache-control", "no-store");
    return c.json(sessionStatusResponse(c.req.url, c.req.param("id"), capture, session, true));
  });

  app.post("/dashboard/api/sessions/:id/mark", async (c) => {
    const body = await c.req.json<{ outcome?: string; source?: string; reason?: unknown; evidence?: unknown }>();
    return updateOutcome(c, { ...body, source: "user" }, "user");
  });

  app.post("/dashboard/api/sessions/:id/outcome", async (c) => {
    const body = await c.req.json<{ outcome?: string; source?: string; reason?: unknown; evidence?: unknown }>();
    return updateOutcome(c, { ...body, source: "user" }, "user");
  });

  app.get("/dashboard/api/overview", async (c) => {
    const [totals, models, providers, apps] = await Promise.all([
      c.env.DB.prepare("SELECT COUNT(*) AS requests, COUNT(DISTINCT session_id) AS sessions, COALESCE(SUM(CASE WHEN capture_status = 'saved' THEN 1 ELSE 0 END), 0) AS saved_exchanges, COALESCE(SUM(CASE WHEN capture_status = 'failed' THEN 1 ELSE 0 END), 0) AS capture_failures, COALESCE(SUM(CASE WHEN capture_status = 'saved' THEN input_tokens ELSE 0 END), 0) AS input_tokens, COALESCE(SUM(CASE WHEN capture_status = 'saved' THEN output_tokens ELSE 0 END), 0) AS output_tokens FROM exchanges").first(),
      c.env.DB.prepare("SELECT model AS name, COUNT(*) AS requests FROM exchanges WHERE capture_status = 'saved' GROUP BY model ORDER BY requests DESC LIMIT 6").all(),
      c.env.DB.prepare("SELECT COALESCE(provider, 'Unknown') AS name, COUNT(*) AS requests FROM exchanges WHERE capture_status = 'saved' GROUP BY provider ORDER BY requests DESC LIMIT 6").all(),
      c.env.DB.prepare("SELECT COALESCE(harness, 'Unknown') AS name, COUNT(*) AS requests FROM exchanges WHERE capture_status = 'saved' GROUP BY harness ORDER BY requests DESC LIMIT 6").all(),
    ]);
    return c.json({ totals, models: models.results, providers: providers.results, apps: apps.results });
  });
}

function encodeCursor(ts: string, id: string) {
  return btoa(JSON.stringify({ ts, id })).replaceAll("+", "-").replaceAll("/", "_").replaceAll("=", "");
}

function decodeCursor(value: string | undefined) {
  if (!value) return null;
  try {
    const padded = value.replaceAll("-", "+").replaceAll("_", "/") + "===".slice((value.length + 3) % 4);
    const cursor = JSON.parse(atob(padded)) as { ts?: unknown; id?: unknown };
    return typeof cursor.ts === "string" && typeof cursor.id === "string" ? { ts: cursor.ts, id: cursor.id } : null;
  } catch {
    return null;
  }
}
