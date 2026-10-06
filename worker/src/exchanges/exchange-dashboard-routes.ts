import type { Hono } from "hono";
import type { AppEnv } from "../env";
import { dashboardDateRange } from "../dashboard/date-range";
import { canonicalOutcome } from "../sessions/outcomes";
import { SESSION_SUBTREE_CTE } from "../sessions/session-queries";
import {
  boundedLimit,
  decodeExchangeCursor,
  encodeExchangeCursor,
} from "../dashboard/cursors";


function exchangeFilters(query: (key: string) => string | undefined, where: string[], values: Array<string | number>) {
  const range = dashboardDateRange(query("from"), query("to"));
  if ("error" in range) return range.error;
  if (range.start) {
    where.push("julianday(ts) >= julianday(?)");
    values.push(range.start.value);
  }
  if (range.end) {
    where.push(`julianday(ts) ${range.end.exclusive ? "<" : "<="} julianday(?)`);
    values.push(range.end.value);
  }
  const kind = query("request_kind");
  if (kind && !["primary", "title", "summary", "compaction"].includes(kind)) return "invalid request_kind";
  const capture = query("capture_status");
  if (capture && !["accepted", "saved", "failed", "skipped"].includes(capture)) return "invalid capture_status";
  const errors = query("errors");
  if (errors && errors !== "true") return "invalid errors";
  const tool = query("tool");
  if (tool) {
    where.push("(exchanges.capture_status = 'saved' AND EXISTS (SELECT 1 FROM exchange_tools et WHERE et.exchange_id = exchanges.id AND et.name = ?))");
    values.push(tool);
  }
  const q = query("q");
  if (q) {
    const columns = ["request_excerpt", "response_excerpt", "id", "session_id", "model", "provider", "harness", "repo"];
    where.push(`(${columns.map((column) => `instr(lower(COALESCE(exchanges.${column}, '')), lower(?)) > 0`).join(" OR ")})`);
    values.push(...columns.map(() => q));
  }
  for (const [parameter, column] of [
    ["repo", "repo"], ["model", "model"], ["provider", "provider"],
    ["app", "harness"], ["finish_reason", "finish_reason"],
    ["request_kind", "request_kind"], ["capture_status", "capture_status"],
  ] as const) {
    const value = query(parameter);
    if (value) {
      where.push(`exchanges.${column} = ?`);
      values.push(value);
    }
  }
  if (errors) where.push("(exchanges.capture_status = 'failed' OR (exchanges.capture_status = 'saved' AND EXISTS (SELECT 1 FROM exchange_errors ee WHERE ee.exchange_id = exchanges.id)))");
  return null;
}

const EXCHANGE_COLUMNS = "id, session_id, ts, model, provider, finish_reason, endpoint, latency_ms, repo, harness, access_token_label, input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, r2_key, request_excerpt, response_excerpt, request_kind, capture_status, capture_reason, failure_code";
export function registerDashboardExchangeRoutes(app: Hono<AppEnv>) {
  app.get("/dashboard/api/log", async (c) => {
    const limit = c.req.query("limit") === undefined ? 50 : boundedLimit(c.req.query("limit"));
    const order = c.req.query("order") ?? "desc";
    if (order !== "asc" && order !== "desc") return c.json({ error: "invalid order" }, 400);
    const where: string[] = [];
    const values: Array<string | number> = [];
    const filterError = exchangeFilters((key) => c.req.query(key), where, values);
    if (filterError) return c.json({ error: filterError }, 400);
    const session = c.req.query("session");
    if (session) {
      where.push("session_id = ?");
      values.push(session);
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
    const cursor = decodeExchangeCursor(cursorValue);
    if (cursorValue && (!cursor || cursor.order !== order)) return c.json({ error: "invalid cursor" }, 400);
    if (cursor) {
      const operator = order === "desc" ? "<" : ">";
      where.push(`(ts ${operator} ? OR (ts = ? AND id ${operator} ?))`);
      values.push(cursor.ts, cursor.ts, cursor.id);
    }
    const direction = order === "desc" ? "DESC" : "ASC";
    const sql = `SELECT ${EXCHANGE_COLUMNS} FROM exchanges ${where.length ? `WHERE ${where.join(" AND ")}` : ""} ORDER BY ts ${direction}, id ${direction} LIMIT ?`;
    const rows = await c.env.DB.prepare(sql)
      .bind(...values, limit + 1)
      .all<Record<string, unknown>>();
    const hasMore = rows.results.length > limit;
    const exchanges = rows.results.slice(0, limit);
    const last = exchanges.at(-1) as { ts?: string; id?: string } | undefined;
    return c.json({
      exchanges,
      next_cursor:
        hasMore && last?.ts && last.id ? encodeExchangeCursor(last.ts, last.id, order) : null,
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
        !(await c.env.DB.prepare(`${SESSION_SUBTREE_CTE} SELECT 1 FROM subtree WHERE id = ?`)
          .bind(c.req.param("id"), scope)
          .first())
      )
        return c.json({ error: "session scope is outside requested subtree" }, 400);
      where.push("session_id = ?");
      values.push(scope);
    }
    const filterError = exchangeFilters((key) => c.req.query(key), where, values);
    if (filterError) return c.json({ error: filterError }, 400);
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
    const sql = `${SESSION_SUBTREE_CTE} SELECT ${EXCHANGE_COLUMNS} FROM exchanges WHERE ${where.join(" AND ")} ORDER BY ts ${direction}, id ${direction} LIMIT ?`;
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
