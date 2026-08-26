import type { Hono } from "hono";
import type { AppEnv } from "../env";
import { canonicalOutcome } from "../sessions/outcomes";
import { SESSION_SUBTREE_CTE } from "../sessions/session-queries";
import {
  boundedLimit,
  decodeCursor,
  decodeExchangeCursor,
  encodeCursor,
  encodeExchangeCursor,
} from "../dashboard/cursors";

export function registerDashboardExchangeRoutes(app: Hono<AppEnv>) {
  app.get("/dashboard/api/log", async (c) => {
    const limit = Math.max(
      1,
      Math.min(Number(c.req.query("limit") ?? 50), 100),
    );
    const where: string[] = ["capture_status = 'saved'"];
    const values: string[] = [];
    for (const [field, column] of [
      ["repo", "repo"],
      ["model", "model"],
      ["provider", "provider"],
      ["app", "harness"],
      ["session", "session_id"],
      ["finish_reason", "finish_reason"],
    ] as const) {
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
      where.push(
        "session_id IN (SELECT id FROM sessions WHERE work_outcome = ?)",
      );
      values.push(canonical);
    }
    const cursorValue = c.req.query("cursor");
    const cursor = decodeCursor(cursorValue);
    if (cursorValue && !cursor) return c.json({ error: "invalid cursor" }, 400);
    if (cursor) {
      where.push("(ts < ? OR (ts = ? AND id < ?))");
      values.push(cursor.ts, cursor.ts, cursor.id);
    }
    const sql = `SELECT id, session_id, ts, model, provider, finish_reason, endpoint, latency_ms, repo, harness, access_token_label, input_tokens, output_tokens, r2_key FROM exchanges ${where.length ? `WHERE ${where.join(" AND ")}` : ""} ORDER BY ts DESC, id DESC LIMIT ?`;
    const rows = await c.env.DB.prepare(sql)
      .bind(...values, limit + 1)
      .all<Record<string, unknown>>();
    const hasMore = rows.results.length > limit;
    const exchanges = rows.results.slice(0, limit);
    const last = exchanges.at(-1) as { ts?: string; id?: string } | undefined;
    return c.json({
      exchanges,
      next_cursor:
        hasMore && last?.ts && last.id ? encodeCursor(last.ts, last.id) : null,
    });
  });

  app.get("/dashboard/api/log/:id", async (c) => {
    const exchange = await c.env.DB.prepare(
      "SELECT * FROM exchanges WHERE id = ?",
    )
      .bind(c.req.param("id"))
      .first<Record<string, unknown>>();
    if (!exchange) return c.json({ error: "exchange not found" }, 404);
    return c.json({
      exchange,
      log_url: `/dashboard/log-objects/${exchange.r2_key}`,
    });
  });

  app.get("/dashboard/log-objects/*", async (c) => {
    const key = c.req.path.replace(/^\/dashboard\/log-objects\//, "");
    if (!key.startsWith("log/"))
      return c.json({ error: "invalid log key" }, 400);
    const object = await c.env.LOGS.get(key);
    if (!object) return c.json({ error: "log not found" }, 404);
    return new Response(object.body, {
      headers: {
        "content-type": "application/json",
        "cache-control": "no-store",
      },
    });
  });

  app.get("/dashboard/api/sessions/:id/exchanges", async (c) => {
    const order = c.req.query("order") ?? "desc";
    if (order !== "asc" && order !== "desc")
      return c.json({ error: "invalid order" }, 400);
    if (
      !(await c.env.DB.prepare("SELECT 1 FROM sessions WHERE id = ?")
        .bind(c.req.param("id"))
        .first())
    )
      return c.json({ error: "session not found" }, 404);
    const where = ["session_id IN (SELECT id FROM subtree)"];
    const values: Array<string | number> = [c.req.param("id")];
    // A session scope restricts the timeline to one session's own exchanges.
    // When omitted, the historical merged subtree view is preserved.
    const scope = c.req.query("session");
    if (scope) {
      if (
        !(await c.env.DB.prepare("SELECT 1 FROM sessions WHERE id = ?")
          .bind(scope)
          .first())
      )
        return c.json({ error: "session not found" }, 404);
      where[0] = "session_id = ?";
      values[0] = scope;
    }
    const q = c.req.query("q");
    if (q) {
      where.push(
        "(instr(lower(request_excerpt), lower(?)) > 0 OR instr(lower(exchanges.id), lower(?)) > 0)",
      );
      values.push(q, q);
    }
    for (const [parameter, column] of [
      ["model", "model"],
      ["provider", "provider"],
      ["app", "harness"],
      ["finish_reason", "finish_reason"],
    ] as const) {
      const value = c.req.query(parameter);
      if (value) {
        where.push(`${column} = ?`);
        values.push(value);
      }
    }
    const cursorValue = c.req.query("cursor");
    const cursor = decodeExchangeCursor(cursorValue);
    if (cursorValue && (!cursor || cursor.order !== order))
      return c.json({ error: "invalid cursor" }, 400);
    if (cursor) {
      const operator = order === "desc" ? "<" : ">";
      where.push(
        `(ts ${operator} ? OR (ts = ? AND exchanges.id ${operator} ?))`,
      );
      values.push(cursor.ts, cursor.ts, cursor.id);
    }
    const direction = order === "desc" ? "DESC" : "ASC";
    const limit = boundedLimit(c.req.query("limit"));
    const sql = `${SESSION_SUBTREE_CTE} SELECT id, session_id, ts, model, provider, finish_reason, latency_ms, harness, input_tokens, output_tokens, request_excerpt, capture_status, capture_reason, failure_code FROM exchanges WHERE ${where.join(" AND ")} ORDER BY ts ${direction}, id ${direction} LIMIT ?`;
    const result = await c.env.DB.prepare(sql)
      .bind(...values, limit + 1)
      .all<Record<string, unknown>>();
    const hasMore = result.results.length > limit;
    const exchanges = result.results.slice(0, limit);
    const last = exchanges.at(-1) as { ts?: string; id?: string } | undefined;
    return c.json({
      exchanges,
      next_cursor:
        hasMore && last?.ts && last.id
          ? encodeExchangeCursor(last.ts, last.id, order)
          : null,
    });
  });
}
