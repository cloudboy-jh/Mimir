import type { Hono } from "hono";
import { expireSessions, SESSION_COLUMNS, updateOutcome } from "../sessions";
import type { AppEnv } from "../types";

export function registerDashboardRoutes(app: Hono<AppEnv>) {
  app.get("/dashboard/api/bootstrap", async (c) => {
    const [requests, sessions, latest] = await Promise.all([
      c.env.DB.prepare("SELECT COUNT(*) AS count FROM exchanges").first<{ count: number }>(),
      c.env.DB.prepare("SELECT COUNT(*) AS count FROM sessions").first<{ count: number }>(),
      c.env.DB.prepare("SELECT ts FROM exchanges ORDER BY ts DESC LIMIT 1").first<{ ts: string }>(),
    ]);
    return c.json({ requests: requests?.count ?? 0, sessions: sessions?.count ?? 0, latest_request_at: latest?.ts ?? null });
  });

  app.get("/dashboard/api/log", async (c) => {
    const limit = Math.max(1, Math.min(Number(c.req.query("limit") ?? 50), 100));
    const where: string[] = [];
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
      where.push("session_id IN (SELECT id FROM sessions WHERE outcome = ?)");
      values.push(outcome);
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
    const result = await c.env.DB.prepare(`SELECT ${SESSION_COLUMNS} FROM sessions ORDER BY started_at DESC LIMIT 100`).all();
    return c.json({ sessions: result.results });
  });

  app.get("/dashboard/api/sessions/:id", async (c) => {
    await expireSessions(c.env.DB);
    const session = await c.env.DB.prepare(`SELECT ${SESSION_COLUMNS} FROM sessions WHERE id = ?`).bind(c.req.param("id")).first();
    if (!session) return c.json({ error: "session not found" }, 404);
    const [exchanges, files, errors] = await Promise.all([
      c.env.DB.prepare("SELECT id, ts, model, provider, finish_reason, latency_ms, harness, input_tokens, output_tokens FROM exchanges WHERE session_id = ? ORDER BY ts").bind(c.req.param("id")).all(),
      c.env.DB.prepare("SELECT file FROM session_files WHERE session_id = ? ORDER BY file").bind(c.req.param("id")).all<{ file: string }>(),
      c.env.DB.prepare("SELECT signature FROM session_errors WHERE session_id = ? ORDER BY signature").bind(c.req.param("id")).all<{ signature: string }>(),
    ]);
    return c.json({ session, exchanges: exchanges.results, files: files.results.map((row) => row.file), errors: errors.results.map((row) => row.signature) });
  });

  app.post("/dashboard/api/sessions/:id/mark", async (c) => {
    const body = await c.req.json<{ outcome?: string }>();
    return updateOutcome(c, body.outcome, "explicit");
  });

  app.get("/dashboard/api/overview", async (c) => {
    const [totals, models, providers, apps] = await Promise.all([
      c.env.DB.prepare("SELECT COUNT(*) AS requests, COUNT(DISTINCT session_id) AS sessions, COALESCE(SUM(input_tokens), 0) AS input_tokens, COALESCE(SUM(output_tokens), 0) AS output_tokens FROM exchanges").first(),
      c.env.DB.prepare("SELECT model AS name, COUNT(*) AS requests FROM exchanges GROUP BY model ORDER BY requests DESC LIMIT 6").all(),
      c.env.DB.prepare("SELECT COALESCE(provider, 'Unknown') AS name, COUNT(*) AS requests FROM exchanges GROUP BY provider ORDER BY requests DESC LIMIT 6").all(),
      c.env.DB.prepare("SELECT COALESCE(harness, 'Unknown') AS name, COUNT(*) AS requests FROM exchanges GROUP BY harness ORDER BY requests DESC LIMIT 6").all(),
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
