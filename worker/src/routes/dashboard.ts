import type { Hono } from "hono";
import { canonicalOutcome, expireSessions, ROOT_SESSION_ACTIVITY_AT, ROOT_SESSION_COLUMNS, SESSION_COLUMNS, SESSION_TREE_CTE, updateOutcome } from "../sessions";
import { attachCaptureSummary, captureSummary, captureTreeSummary, sessionStatusResponse, TREE_CAPTURE_SUMMARY_COLUMNS } from "../storage";
import type { AppEnv } from "../types";
import { MAX_SESSION_TITLE_CHARS, normalizeSessionTitle, sessionTitleColumns, sessionTitleSearchClause, titleUpdateStatement } from "../session-titles";
import { SESSION_ID } from "../session-events";
import { ensureSessionSummary } from "../session-summary";

const FACET_LIMIT = 50;

type SessionModel = {
  name: string;
  request_count: number;
  first_seen_at: string | null;
  last_seen_at: string | null;
};

async function attachSessionModels(db: D1Database, rows: Array<Record<string, unknown>>) {
  if (!rows.length) return rows;
  const ids = rows.map((row) => String(row.id));
  const placeholders = ids.map(() => "?").join(", ");
  const grouped = await db.prepare(`SELECT session_id, model AS name, COUNT(*) AS request_count, MIN(ts) AS first_seen_at, MAX(ts) AS last_seen_at FROM exchanges WHERE capture_status = 'saved' AND model IS NOT NULL AND model <> '' AND session_id IN (${placeholders}) GROUP BY session_id, model ORDER BY first_seen_at ASC, name ASC`)
    .bind(...ids)
    .all<SessionModel & { session_id: string }>();
  const bySession = new Map<string, SessionModel[]>();
  for (const { session_id, ...model } of grouped.results) {
    const models = bySession.get(session_id) ?? [];
    models.push(model);
    bySession.set(session_id, models);
  }
  return rows.map((row) => {
    const primary = typeof row.model_primary === "string" && row.model_primary ? row.model_primary : null;
    const models = bySession.get(String(row.id)) ?? [];
    if (primary && !models.some((model) => model.name === primary)) {
      models.unshift({ name: primary, request_count: 0, first_seen_at: null, last_seen_at: null });
    } else if (primary) {
      models.sort((left, right) => left.name === primary ? -1 : right.name === primary ? 1 : (left.first_seen_at ?? "").localeCompare(right.first_seen_at ?? "") || left.name.localeCompare(right.name));
    }
    return { ...row, models };
  });
}

async function attachSessionDevices(db: D1Database, rows: Array<Record<string, unknown>>) {
  const ids = [...new Set(rows.map((row) => row.installation_id).filter((id): id is string => typeof id === "string" && id.length > 0))];
  if (!ids.length) return rows.map(({ installation_id: _installationID, ...row }) => ({ ...row, device: null }));
  const devices = await db.prepare(`SELECT installation_id AS id, name, platform, arch FROM machines WHERE installation_id IN (${ids.map(() => "?").join(", ")})`).bind(...ids).all<{ id: string; name: string; platform: string; arch: string }>();
  const byID = new Map(devices.results.map((device) => [device.id, device]));
  return rows.map(({ installation_id, ...row }) => ({ ...row, device: typeof installation_id === "string" ? byID.get(installation_id) ?? null : null }));
}

async function deviceRows(db: D1Database, installationID?: string) {
  const statement = db.prepare(`
    WITH root_sessions AS (
      SELECT installation_id, COUNT(*) AS session_count
      FROM sessions
      WHERE installation_id IS NOT NULL AND parent_session_id IS NULL
      GROUP BY installation_id
    ), device_harnesses AS (
      SELECT installation_id, json_group_array(harness) AS harnesses
      FROM (
        SELECT DISTINCT installation_id, harness
        FROM sessions
        WHERE installation_id IS NOT NULL AND harness IS NOT NULL AND harness <> ''
        ORDER BY installation_id, harness
      )
      GROUP BY installation_id
    )
    SELECT machines.installation_id AS id, machines.name, machines.platform, machines.arch,
      machines.created_at, machines.updated_at, machines.last_seen_at, machines.revoked_at,
      COALESCE(root_sessions.session_count, 0) AS session_count,
      COALESCE(device_harnesses.harnesses, '[]') AS harnesses
    FROM machines
    LEFT JOIN root_sessions ON root_sessions.installation_id = machines.installation_id
    LEFT JOIN device_harnesses ON device_harnesses.installation_id = machines.installation_id
    ${installationID ? "WHERE machines.installation_id = ?" : ""}
    ORDER BY COALESCE(machines.last_seen_at, machines.created_at) DESC, machines.installation_id
  `);
  const rows = installationID
    ? await statement.bind(installationID).all<Record<string, unknown>>()
    : await statement.all<Record<string, unknown>>();
  return rows.results.map((row) => ({ ...row, harnesses: JSON.parse(String(row.harnesses)) as string[] }));
}

async function sessionObjectResponse(env: AppEnv["Bindings"], id: string, path: "/state" | "/feed", headers?: HeadersInit) {
  const stub = env.SESSIONS.get(env.SESSIONS.idFromName(id));
  return stub.fetch(`https://session-object${path}`, { headers });
}

async function attachSessionLiveness(env: AppEnv["Bindings"], rows: Array<Record<string, unknown>>) {
  return Promise.all(rows.map(async (row) => {
    if (row.state !== "active") return { ...row, liveness: "finalized" };
    try {
      const response = await sessionObjectResponse(env, String(row.id), "/state");
      if (!response.ok) return { ...row, liveness: "disconnected" };
      const state = await response.json<{ liveness?: unknown }>();
      const liveness = state.liveness === "active" || state.liveness === "disconnected" || state.liveness === "finalized"
        ? state.liveness
        : "disconnected";
      return { ...row, liveness };
    } catch {
      return { ...row, liveness: "disconnected" };
    }
  }));
}

export function registerDashboardRoutes(app: Hono<AppEnv>) {
  app.get("/dashboard/auth", (c) => {
    const returnTo = c.req.query("returnTo") ?? "/dashboard/sessions";
    return c.redirect(returnTo.startsWith("/dashboard/") && !returnTo.startsWith("//") ? returnTo : "/dashboard/sessions");
  });

  app.get("/dashboard/api/identity", (c) => c.json(c.get("dashboardIdentity")));

  app.get("/dashboard/api/bootstrap", async (c) => {
    const [captures, sessions, latest] = await Promise.all([
      c.env.DB.prepare("SELECT COUNT(*) AS requests, SUM(CASE WHEN capture_status = 'saved' THEN 1 ELSE 0 END) AS saved_exchanges, SUM(CASE WHEN capture_status = 'failed' THEN 1 ELSE 0 END) AS capture_failures FROM exchanges").first(),
      c.env.DB.prepare("SELECT COUNT(*) AS count FROM sessions WHERE parent_session_id IS NULL").first<{ count: number }>(),
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
    const cursorValue = c.req.query("cursor");
    const cursor = decodeCursor(cursorValue);
    if (cursorValue && !cursor) return c.json({ error: "invalid cursor" }, 400);
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
    const where = ["sessions.parent_session_id IS NULL"];
    const values: Array<string | number> = [];
    const q = c.req.query("q");
    if (q) {
      where.push(`(${sessionTitleSearchClause("sessions")} OR instr(lower(COALESCE(sessions.repo, '')), lower(?)) > 0 OR instr(lower(COALESCE(sessions.harness, '')), lower(?)) > 0 OR instr(lower(COALESCE(sessions.model_primary, '')), lower(?)) > 0 OR EXISTS (SELECT 1 FROM exchanges model_search WHERE model_search.session_id = sessions.id AND model_search.capture_status = 'saved' AND instr(lower(COALESCE(model_search.model, '')), lower(?)) > 0) OR instr(lower(sessions.id), lower(?)) > 0)`);
      values.push(q, q, q, q, q, q, q);
    }
    for (const [parameter, column] of [["repo", "repo"], ["app", "harness"]] as const) {
      const value = c.req.query(parameter);
      if (value) {
        where.push(`sessions.${column} = ?`);
        values.push(value);
      }
    }
    const model = c.req.query("model");
    if (model) {
      where.push("(sessions.model_primary = ? OR EXISTS (SELECT 1 FROM exchanges model_filter WHERE model_filter.session_id = sessions.id AND model_filter.capture_status = 'saved' AND model_filter.model = ?))");
      values.push(model, model);
    }
    const outcome = c.req.query("outcome");
    if (outcome) {
      const canonical = canonicalOutcome(outcome);
      if (!canonical) return c.json({ error: "invalid outcome" }, 400);
      where.push("sessions.work_outcome = ?");
      values.push(canonical);
    }
    for (const [parameter, operator] of [["from", ">="], ["to", "<="]] as const) {
      const value = c.req.query(parameter);
      if (value) {
        where.push(`sessions.started_at ${operator} ?`);
        values.push(value);
      }
    }
    const cursorValue = c.req.query("cursor");
    const cursor = decodeCursor(cursorValue);
    if (cursorValue && !cursor) return c.json({ error: "invalid cursor" }, 400);
    if (cursor) {
      where.push(`(${ROOT_SESSION_ACTIVITY_AT} < ? OR (${ROOT_SESSION_ACTIVITY_AT} = ? AND sessions.id < ?))`);
      values.push(cursor.ts, cursor.ts, cursor.id);
    }
    const limit = boundedLimit(c.req.query("limit"));
    const result = await c.env.DB.prepare(`${SESSION_TREE_CTE} SELECT ${ROOT_SESSION_COLUMNS}, ${TREE_CAPTURE_SUMMARY_COLUMNS} FROM sessions WHERE ${where.join(" AND ")} ORDER BY ${ROOT_SESSION_ACTIVITY_AT} DESC, sessions.id DESC LIMIT ?`).bind(...values, limit + 1).all<Record<string, unknown>>();
    const hasMore = result.results.length > limit;
    const sessions = await attachSessionLiveness(c.env, await attachSessionDevices(c.env.DB, await attachSessionModels(c.env.DB, result.results.slice(0, limit).map(attachCaptureSummary))));
    const last = sessions.at(-1) as { activity_at?: string; id?: string } | undefined;
    return c.json({ sessions, next_cursor: hasMore && last?.activity_at && last.id ? encodeCursor(last.activity_at, last.id) : null });
  });

  app.get("/dashboard/api/sessions/:id/exchanges", async (c) => {
    const order = c.req.query("order") ?? "desc";
    if (order !== "asc" && order !== "desc") return c.json({ error: "invalid order" }, 400);
    if (!await c.env.DB.prepare("SELECT 1 FROM sessions WHERE id = ?").bind(c.req.param("id")).first()) return c.json({ error: "session not found" }, 404);
    const subtree = "WITH RECURSIVE subtree(id) AS (SELECT ? UNION ALL SELECT sessions.id FROM sessions JOIN subtree ON sessions.parent_session_id = subtree.id)";
    const where = ["session_id IN (SELECT id FROM subtree)"];
    const values: Array<string | number> = [c.req.param("id")];
    const q = c.req.query("q");
    if (q) {
      where.push("(instr(lower(request_excerpt), lower(?)) > 0 OR instr(lower(exchanges.id), lower(?)) > 0)");
      values.push(q, q);
    }
    for (const [parameter, column] of [["model", "model"], ["provider", "provider"], ["app", "harness"], ["finish_reason", "finish_reason"]] as const) {
      const value = c.req.query(parameter);
      if (value) {
        where.push(`${column} = ?`);
        values.push(value);
      }
    }
    const cursorValue = c.req.query("cursor");
    const cursor = decodeExchangeCursor(cursorValue);
    if (cursorValue && (!cursor || cursor.order !== order)) return c.json({ error: "invalid cursor" }, 400);
    if (cursor) {
      const operator = order === "desc" ? "<" : ">";
      where.push(`(ts ${operator} ? OR (ts = ? AND exchanges.id ${operator} ?))`);
      values.push(cursor.ts, cursor.ts, cursor.id);
    }
    const direction = order === "desc" ? "DESC" : "ASC";
    const limit = boundedLimit(c.req.query("limit"));
    const sql = `${subtree} SELECT id, session_id, ts, model, provider, finish_reason, latency_ms, harness, input_tokens, output_tokens, request_excerpt, capture_status, capture_reason, failure_code FROM exchanges WHERE ${where.join(" AND ")} ORDER BY ts ${direction}, id ${direction} LIMIT ?`;
    const result = await c.env.DB.prepare(sql).bind(...values, limit + 1).all<Record<string, unknown>>();
    const hasMore = result.results.length > limit;
    const exchanges = result.results.slice(0, limit);
    const last = exchanges.at(-1) as { ts?: string; id?: string } | undefined;
    return c.json({ exchanges, next_cursor: hasMore && last?.ts && last.id ? encodeExchangeCursor(last.ts, last.id, order) : null });
  });

  app.get("/dashboard/api/sessions/:id", async (c) => {
    await expireSessions(c.env.DB);
    const requested = await c.env.DB.prepare("SELECT parent_session_id FROM sessions WHERE id = ?").bind(c.req.param("id")).first<{ parent_session_id: string | null }>();
    const session = requested?.parent_session_id
      ? await c.env.DB.prepare(`SELECT ${SESSION_COLUMNS} FROM sessions WHERE id = ?`).bind(c.req.param("id")).first()
      : await c.env.DB.prepare(`${SESSION_TREE_CTE} SELECT ${ROOT_SESSION_COLUMNS} FROM sessions WHERE sessions.id = ?`).bind(c.req.param("id")).first();
    if (!session) return c.json({ error: "session not found" }, 404);
    const outcomeRoot = await c.env.DB.prepare("WITH RECURSIVE ancestors(id, parent_session_id) AS (SELECT id, parent_session_id FROM sessions WHERE id = ? UNION ALL SELECT sessions.id, sessions.parent_session_id FROM sessions JOIN ancestors ON sessions.id = ancestors.parent_session_id) SELECT id FROM ancestors WHERE parent_session_id IS NULL LIMIT 1").bind(c.req.param("id")).first<{ id: string }>();
    const subtree = "WITH RECURSIVE subtree(id) AS (SELECT ? UNION ALL SELECT sessions.id FROM sessions JOIN subtree ON sessions.parent_session_id = subtree.id)";
    const [files, signatures, aggregates, latestLinks, capture, outcomeEvents, children] = await Promise.all([
      c.env.DB.prepare(`${subtree} SELECT DISTINCT file FROM session_files WHERE session_id IN (SELECT id FROM subtree) ORDER BY file`).bind(c.req.param("id")).all<{ file: string }>(),
      c.env.DB.prepare(`${subtree} SELECT DISTINCT signature FROM session_errors WHERE session_id IN (SELECT id FROM subtree) ORDER BY signature`).bind(c.req.param("id")).all<{ signature: string }>(),
      c.env.DB.prepare(`${subtree} SELECT ee.signature, COUNT(DISTINCT ee.exchange_id) AS count, MIN(e.ts) AS first_seen_at, MAX(e.ts) AS last_seen_at FROM exchange_errors ee JOIN exchanges e ON e.id = ee.exchange_id WHERE ee.session_id IN (SELECT id FROM subtree) AND e.capture_status = 'saved' GROUP BY ee.signature`).bind(c.req.param("id")).all<{ signature: string; count: number; first_seen_at: string | null; last_seen_at: string | null }>(),
      c.env.DB.prepare(`${subtree} SELECT ee.signature, ee.exchange_id FROM exchange_errors ee JOIN exchanges e ON e.id = ee.exchange_id WHERE ee.session_id IN (SELECT id FROM subtree) AND e.capture_status = 'saved' ORDER BY e.ts DESC, e.id DESC`).bind(c.req.param("id")).all<{ signature: string; exchange_id: string }>(),
      captureTreeSummary(c.env.DB, c.req.param("id")),
      c.env.DB.prepare("SELECT id, outcome, source, reason, evidence_json, created_at FROM session_outcome_events WHERE session_id = ? ORDER BY created_at DESC").bind(outcomeRoot?.id ?? c.req.param("id")).all(),
      c.env.DB.prepare(`${subtree} SELECT ${SESSION_COLUMNS} FROM sessions WHERE id IN (SELECT id FROM subtree) AND id <> ? ORDER BY started_at`).bind(c.req.param("id"), c.req.param("id")).all(),
    ]);
    const aggregateBySignature = new Map(aggregates.results.map((row) => [row.signature, row]));
    const latestBySignature = new Map<string, string>();
    for (const row of latestLinks.results) {
      if (!latestBySignature.has(row.signature)) latestBySignature.set(row.signature, row.exchange_id);
    }
    const errors = signatures.results.map(({ signature }) => {
      const aggregate = aggregateBySignature.get(signature);
      return {
        signature,
        count: aggregate?.count ?? 1,
        first_seen_at: aggregate?.first_seen_at ?? null,
        last_seen_at: aggregate?.last_seen_at ?? null,
        latest_exchange_id: latestBySignature.get(signature) ?? null,
      };
    });
    const summarized = await ensureSessionSummary(c.env.DB, session as Record<string, unknown>, files.results.length, errors.length);
    const modeled = await attachSessionDevices(c.env.DB, await attachSessionModels(c.env.DB, [summarized, ...children.results]));
    return c.json({ session: modeled[0], capture, outcome_events: outcomeEvents.results, supporting_sessions: modeled.slice(1), files: files.results.map((row) => row.file), errors });
  });

  app.get("/dashboard/api/sessions/:id/diff", async (c) => {
    const id = c.req.param("id");
    const root = await c.env.DB.prepare("WITH RECURSIVE ancestors(id, parent_session_id) AS (SELECT id, parent_session_id FROM sessions WHERE id = ? UNION ALL SELECT sessions.id, sessions.parent_session_id FROM sessions JOIN ancestors ON sessions.id = ancestors.parent_session_id) SELECT id FROM ancestors WHERE parent_session_id IS NULL LIMIT 1").bind(id).first<{ id: string }>();
    if (!root) return c.json({ error: "session not found" }, 404);
    const events = await c.env.DB.prepare("SELECT evidence_json FROM session_outcome_events WHERE session_id = ? AND evidence_json IS NOT NULL ORDER BY created_at DESC, rowid DESC").bind(root.id).all<{ evidence_json: string }>();
    for (const event of events.results) {
      let evidence: Record<string, unknown>;
      try { evidence = JSON.parse(event.evidence_json) as Record<string, unknown>; } catch { continue; }
      if (typeof evidence.patch_r2_key === "string") {
        const prefix = `sessions/${root.id}/diffs/`;
        if (!evidence.patch_r2_key.startsWith(prefix)) continue;
        const object = await c.env.LOGS.get(evidence.patch_r2_key);
        if (!object) return c.json({ error: "diff artifact not found" }, 404);
        return new Response(object.body, { headers: { "content-type": "text/plain; charset=utf-8", "cache-control": "private, no-store" } });
      }
      if (typeof evidence.patch === "string" && evidence.patch) {
        return new Response(evidence.patch, { headers: { "content-type": "text/plain; charset=utf-8", "cache-control": "private, no-store" } });
      }
    }
    return c.json({ error: "diff unavailable" }, 404);
  });

  app.get("/dashboard/api/sessions/:id/status", async (c) => {
    const session = await c.env.DB.prepare(`SELECT state, ended_at, inactive_at, work_outcome AS outcome, outcome_src, outcome_updated_at, outcome_reason, ${sessionTitleColumns("sessions")} FROM sessions WHERE id = ?`).bind(c.req.param("id")).first();
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

  app.get("/dashboard/api/sessions/:id/object-state", async (c) => {
    const id = c.req.param("id");
    if (!SESSION_ID.test(id)) return c.json({ error: "invalid session id" }, 400);
    if (!await c.env.DB.prepare("SELECT 1 FROM sessions WHERE id = ?").bind(id).first()) return c.json({ error: "session not found" }, 404);
    const response = await sessionObjectResponse(c.env, id, "/state");
    c.header("cache-control", "no-store");
    return new Response(response.body, { status: response.status, headers: { "content-type": "application/json", "cache-control": "no-store" } });
  });

  app.get("/dashboard/api/sessions/:id/live", async (c) => {
    if ((c.req.header("upgrade") ?? "").toLowerCase() !== "websocket") return c.json({ error: "websocket upgrade required" }, 426);
    const id = c.req.param("id");
    if (!SESSION_ID.test(id)) return c.json({ error: "invalid session id" }, 400);
    if (!await c.env.DB.prepare("SELECT 1 FROM sessions WHERE id = ?").bind(id).first()) return c.json({ error: "session not found" }, 404);
    return sessionObjectResponse(c.env, id, "/feed", { Upgrade: "websocket" });
  });

  app.patch("/dashboard/api/sessions/:id/title", async (c) => {
    let body: unknown;
    try {
      body = await c.req.json();
    } catch {
      return c.json({ error: "invalid JSON body" }, 400);
    }
    if (!body || typeof body !== "object" || Array.isArray(body) || Object.keys(body).some((key) => key !== "title")) return c.json({ error: "body must contain only title" }, 400);
    const value = (body as Record<string, unknown>).title;
    if (typeof value !== "string") return c.json({ error: "title must be a string" }, 400);
    const title = normalizeSessionTitle(value);
    if (!title) return c.json({ error: `title must be non-empty and at most ${MAX_SESSION_TITLE_CHARS} characters` }, 400);
    const id = c.req.param("id");
    if (!await c.env.DB.prepare("SELECT 1 FROM sessions WHERE id = ?").bind(id).first()) return c.json({ error: "session not found" }, 404);
    const now = new Date().toISOString();
    await titleUpdateStatement(c.env.DB, id, title, "manual", now).run();
    const session = await c.env.DB.prepare(`SELECT id, intent, ${sessionTitleColumns("sessions")} FROM sessions WHERE id = ?`).bind(id).first();
    return c.json({ session });
  });

  app.get("/dashboard/api/devices", async (c) => {
    return c.json({ devices: await deviceRows(c.env.DB) });
  });

  app.patch("/dashboard/api/devices/:id", async (c) => {
    let body: unknown;
    try { body = await c.req.json(); } catch { return c.json({ error: "invalid JSON body" }, 400); }
    if (!body || typeof body !== "object" || Array.isArray(body) || Object.keys(body).some((key) => key !== "name")) return c.json({ error: "body must contain only name" }, 400);
    const value = (body as Record<string, unknown>).name;
    const name = typeof value === "string" ? value.trim() : "";
    if (!name || name.length > 200) return c.json({ error: "name must be a non-empty string of at most 200 characters" }, 400);
    const updated = await c.env.DB.prepare("UPDATE machines SET name = ?, updated_at = ? WHERE installation_id = ?").bind(name, new Date().toISOString(), c.req.param("id")).run();
    if (updated.meta.changes === 0) return c.json({ error: "device not found" }, 404);
    const [device] = await deviceRows(c.env.DB, c.req.param("id"));
    return c.json({ device });
  });

  app.post("/dashboard/api/devices/:id/revoke", async (c) => {
    const now = new Date().toISOString();
    const exists = await c.env.DB.prepare("SELECT 1 FROM machines WHERE installation_id = ?").bind(c.req.param("id")).first();
    if (!exists) return c.json({ error: "device not found" }, 404);
    await c.env.DB.batch([
      c.env.DB.prepare("UPDATE machines SET revoked_at = COALESCE(revoked_at, ?), updated_at = ? WHERE installation_id = ?").bind(now, now, c.req.param("id")),
      c.env.DB.prepare("UPDATE access_tokens SET revoked_at = COALESCE(revoked_at, ?) WHERE installation_id = ?").bind(now, c.req.param("id")),
    ]);
    const [device] = await deviceRows(c.env.DB, c.req.param("id"));
    return c.json({ device });
  });

  // Facets back the dashboard's filter dropdowns. Values come from real stored
  // traffic ordered by frequency so the common choice is first, and each list is
  // bounded so a long-lived deployment cannot return an unusable payload.
  app.get("/dashboard/api/facets", async (c) => {
    const sessionId = c.req.query("session");
    const scope = sessionId
      ? { cte: "WITH RECURSIVE subtree(id) AS (SELECT ? UNION ALL SELECT sessions.id FROM sessions JOIN subtree ON sessions.parent_session_id = subtree.id) ", where: "AND session_id IN (SELECT id FROM subtree)", values: [sessionId] }
      : { cte: "", where: "", values: [] as string[] };
    const exchangeFacet = (column: string) =>
      c.env.DB.prepare(`${scope.cte}SELECT ${column} AS value, COUNT(*) AS requests FROM exchanges WHERE capture_status = 'saved' AND ${column} IS NOT NULL AND ${column} <> '' ${scope.where} GROUP BY ${column} ORDER BY requests DESC, value ASC LIMIT ${FACET_LIMIT}`)
        .bind(...scope.values)
        .all<{ value: string }>();
    const sessionFacet = (column: string) =>
      c.env.DB.prepare(`SELECT ${column} AS value, COUNT(*) AS sessions FROM sessions WHERE ${column} IS NOT NULL AND ${column} <> '' GROUP BY ${column} ORDER BY sessions DESC, value ASC LIMIT ${FACET_LIMIT}`).all<{ value: string }>();
    const [repos, apps, models, providers, finishReasons] = await Promise.all([
      sessionId ? Promise.resolve({ results: [] as Array<{ value: string }> }) : sessionFacet("repo"),
      exchangeFacet("harness"),
      exchangeFacet("model"),
      exchangeFacet("provider"),
      exchangeFacet("finish_reason"),
    ]);
    const values = (rows: { results: Array<{ value: string }> }) => rows.results.map((row) => row.value);
    return c.json({ repos: values(repos), apps: values(apps), models: values(models), providers: values(providers), finish_reasons: values(finishReasons) });
  });

  app.get("/dashboard/api/overview", async (c) => {
    const [totals, models, providers, apps] = await Promise.all([
      c.env.DB.prepare("SELECT COUNT(*) AS requests, (SELECT COUNT(*) FROM sessions WHERE parent_session_id IS NULL) AS sessions, COALESCE(SUM(CASE WHEN capture_status = 'saved' THEN 1 ELSE 0 END), 0) AS saved_exchanges, COALESCE(SUM(CASE WHEN capture_status = 'failed' THEN 1 ELSE 0 END), 0) AS capture_failures, COALESCE(SUM(CASE WHEN capture_status = 'saved' THEN input_tokens ELSE 0 END), 0) AS input_tokens, COALESCE(SUM(CASE WHEN capture_status = 'saved' THEN output_tokens ELSE 0 END), 0) AS output_tokens FROM exchanges").first(),
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

function encodeExchangeCursor(ts: string, id: string, order: "asc" | "desc") {
  return btoa(JSON.stringify({ ts, id, order })).replaceAll("+", "-").replaceAll("/", "_").replaceAll("=", "");
}

function decodeCursor(value: string | undefined) {
  if (!value) return null;
  try {
    const padded = value.replaceAll("-", "+").replaceAll("_", "/") + "===".slice((value.length + 3) % 4);
    const cursor = JSON.parse(atob(padded)) as { ts?: unknown; id?: unknown };
    return typeof cursor.ts === "string" && cursor.ts.length > 0 && typeof cursor.id === "string" && cursor.id.length > 0 ? { ts: cursor.ts, id: cursor.id } : null;
  } catch {
    return null;
  }
}

function decodeExchangeCursor(value: string | undefined) {
  if (!value) return null;
  try {
    const padded = value.replaceAll("-", "+").replaceAll("_", "/") + "===".slice((value.length + 3) % 4);
    const cursor = JSON.parse(atob(padded)) as { ts?: unknown; id?: unknown; order?: unknown };
    return typeof cursor.ts === "string" && cursor.ts.length > 0 && typeof cursor.id === "string" && cursor.id.length > 0 && (cursor.order === "asc" || cursor.order === "desc")
      ? { ts: cursor.ts, id: cursor.id, order: cursor.order }
      : null;
  } catch {
    return null;
  }
}

function boundedLimit(value: string | undefined) {
  const parsed = value === undefined ? 25 : Number(value);
  return Math.max(1, Math.min(Number.isFinite(parsed) ? Math.trunc(parsed) : 25, 100));
}
