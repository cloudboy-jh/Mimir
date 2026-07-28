import type { Context, Hono } from "hono";
import { readConfig, validateConfigValues } from "../config";
import { buildUpstreamHeaders, proxy } from "../proxy";
import { ingestReportedExchange } from "../reported-exchanges";
import { parseSessionEvent, SESSION_ID } from "../session-events";
import { canonicalOutcome, endSession, expireSessions, ROOT_SESSION_COLUMNS, SESSION_COLUMNS, SESSION_TREE_CTE, updateOutcome } from "../sessions";
import { attachCaptureSummary, captureSummary, captureTreeSummary, reconcile, sessionStatusResponse, TREE_CAPTURE_SUMMARY_COLUMNS } from "../storage";
import type { AppEnv } from "../types";

const SEARCH_TYPES = ["intent", "excerpts", "files", "errors"] as const;
const MACHINE_API_VERSION = 1;
const MACHINE_CAPABILITIES = ["canonical_exchanges", "harness_build_identity", "hermes_authorization", "session_events", "session_lifecycle", "session_outcomes", "session_search"] as const;
const HARNESS_LOAD_NAMES = ["opencode", "hermes"] as const;
const HARNESS_LOAD_KEYS = ["version", "harness", "source_sha256", "bundle_version", "cli_version", "cli_commit", "installation_id"] as const;
type SearchType = (typeof SEARCH_TYPES)[number];

// searchTypes resolves the requested column groups, defaulting to all.
// A null entry signals an unknown type and the route rejects the request.
function searchTypes(types: string[] | undefined): (SearchType | null)[] {
  if (!types?.length) return [...SEARCH_TYPES];
  return types.map((type) => (SEARCH_TYPES as readonly string[]).includes(type) ? type as SearchType : null);
}

function clauseNeedles(type: SearchType) {
  return type === "excerpts" ? 2 : 1;
}

export function registerMachineRoutes(app: Hono<AppEnv>) {
  app.get("/whoami", async (c) => {
    const [sessions, exchanges] = await Promise.all([
      c.env.DB.prepare("SELECT COUNT(*) AS count FROM sessions WHERE parent_session_id IS NULL").first<{ count: number }>(),
      c.env.DB.prepare("SELECT COUNT(*) AS count FROM exchanges WHERE capture_status = 'saved'").first<{ count: number }>(),
    ]);
		return c.json({
			service: "mimir",
			api_version: MACHINE_API_VERSION,
			capabilities: MACHINE_CAPABILITIES,
			url: new URL(c.req.url).origin,
			bundle_version: c.env.MIMIR_BUNDLE_VERSION ?? null,
			bundle_sha256: c.env.MIMIR_BUNDLE_SHA256 ?? null,
			sessions: sessions?.count ?? 0,
			log: exchanges?.count ?? 0,
		});
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
    where.push("parent_session_id IS NULL");
    const sql = `${SESSION_TREE_CTE} SELECT ${ROOT_SESSION_COLUMNS}, ${TREE_CAPTURE_SUMMARY_COLUMNS} FROM sessions WHERE ${where.join(" AND ")} ORDER BY sessions.started_at DESC LIMIT 100`;
    const results = await c.env.DB.prepare(sql).bind(...values).all<Record<string, unknown>>();
    return c.json({ sessions: results.results.map(attachCaptureSummary) });
  });

  app.get("/sessions/:id", async (c) => {
    await expireSessions(c.env.DB);
    const requested = await c.env.DB.prepare("SELECT parent_session_id FROM sessions WHERE id = ?").bind(c.req.param("id")).first<{ parent_session_id: string | null }>();
    const session = requested?.parent_session_id
      ? await c.env.DB.prepare(`SELECT ${SESSION_COLUMNS} FROM sessions WHERE id = ?`).bind(c.req.param("id")).first()
      : await c.env.DB.prepare(`${SESSION_TREE_CTE} SELECT ${ROOT_SESSION_COLUMNS} FROM sessions WHERE sessions.id = ?`).bind(c.req.param("id")).first();
    if (!session) return c.json({ error: "session not found" }, 404);
    const subtree = "WITH RECURSIVE subtree(id) AS (SELECT ? UNION ALL SELECT sessions.id FROM sessions JOIN subtree ON sessions.parent_session_id = subtree.id)";
    const [exchanges, files, errors, capture, outcomeEvents, children] = await Promise.all([
      c.env.DB.prepare(`${subtree} SELECT * FROM exchanges WHERE session_id IN (SELECT id FROM subtree) ORDER BY ts`).bind(c.req.param("id")).all(),
      c.env.DB.prepare(`${subtree} SELECT DISTINCT file FROM session_files WHERE session_id IN (SELECT id FROM subtree) ORDER BY file`).bind(c.req.param("id")).all<{ file: string }>(),
      c.env.DB.prepare(`${subtree} SELECT DISTINCT signature FROM session_errors WHERE session_id IN (SELECT id FROM subtree) ORDER BY signature`).bind(c.req.param("id")).all<{ signature: string }>(),
      captureTreeSummary(c.env.DB, c.req.param("id")),
      c.env.DB.prepare("SELECT id, outcome, source, reason, evidence_json, created_at FROM session_outcome_events WHERE session_id = ? ORDER BY created_at DESC").bind(c.req.param("id")).all(),
      c.env.DB.prepare(`${subtree} SELECT ${SESSION_COLUMNS} FROM sessions WHERE id IN (SELECT id FROM subtree) AND id <> ? ORDER BY started_at`).bind(c.req.param("id"), c.req.param("id")).all(),
    ]);
    return c.json({ session, capture, outcome_events: outcomeEvents.results, supporting_sessions: children.results, files: files.results.map((row) => row.file), errors: errors.results.map((row) => row.signature), exchanges: exchanges.results });
  });

  app.get("/sessions/:id/status", async (c) => {
    const session = await c.env.DB.prepare("SELECT state, ended_at, inactive_at, work_outcome AS outcome, outcome_src, outcome_updated_at, outcome_reason FROM sessions WHERE id = ?").bind(c.req.param("id")).first();
    if (!session) return c.json({ error: "session not found" }, 404);
    const capture = await captureSummary(c.env.DB, c.req.param("id"));
    const hostname = new URL(c.req.url).hostname;
    const dashboardAvailable = (hostname === "localhost" || hostname === "127.0.0.1") || !!(c.env.DASHBOARD_ACCESS_AUD && c.env.DASHBOARD_ACCESS_TEAM_DOMAIN);
    c.header("cache-control", "no-store");
    return c.json(sessionStatusResponse(c.req.url, c.req.param("id"), capture, session, dashboardAvailable));
  });

  app.post("/sessions/:id/mark", async (c) => {
    const body = await c.req.json<{ outcome?: string; source?: string; reason?: unknown; evidence?: unknown }>();
    return updateOutcome(c, { ...body, source: "agent" }, "agent");
  });

  app.post("/sessions/:id/outcome", async (c) => {
    const body = await c.req.json<{ outcome?: string; source?: string; reason?: unknown; evidence?: unknown }>();
    return updateOutcome(c, { ...body, source: "agent" }, "agent");
  });

  app.post("/sessions/:id/end", (c) => endSession(c, "agent"));

  app.post("/sessions/:id/exchanges", ingestReportedExchange);

  // Session object surface. Reporters (harness plugins, native harness
  // reporting) append events here; the session ID in the path is
  // authoritative. The live feed streams object state to subscribers.
  app.post("/sessions/:id/events", async (c) => {
    const id = c.req.param("id");
    let body: unknown;
    try {
      body = await c.req.json();
    } catch {
      return c.json({ error: "invalid JSON body" }, 400);
    }
    const parsed = parseSessionEvent({ ...(typeof body === "object" && body && !Array.isArray(body) ? body as Record<string, unknown> : {}), session_id: id });
    if ("error" in parsed) return c.json({ error: parsed.error }, 400);
    const stub = c.env.SESSIONS.get(c.env.SESSIONS.idFromName(id));
    const response = await stub.fetch("https://session-object/event", { method: "POST", body: JSON.stringify(parsed) });
    return new Response(response.body, { status: response.status, headers: { "content-type": "application/json" } });
  });

  app.get("/sessions/:id/live", async (c) => {
    if ((c.req.header("upgrade") ?? "").toLowerCase() !== "websocket") return c.json({ error: "websocket upgrade required" }, 426);
    const id = c.req.param("id");
    if (!SESSION_ID.test(id)) return c.json({ error: "invalid session id" }, 400);
    const stub = c.env.SESSIONS.get(c.env.SESSIONS.idFromName(id));
    return stub.fetch("https://session-object/feed", { headers: { Upgrade: "websocket" } });
  });

  app.get("/sessions/:id/object-state", async (c) => {
    const id = c.req.param("id");
    if (!SESSION_ID.test(id)) return c.json({ error: "invalid session id" }, 400);
    const stub = c.env.SESSIONS.get(c.env.SESSIONS.idFromName(id));
    const response = await stub.fetch("https://session-object/state");
    return new Response(response.body, { status: response.status, headers: { "content-type": "application/json" } });
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
    const clauses: string[] = [];
    const values: string[] = [];
    const needle = query;
    for (const type of searchTypes(body.types)) {
      if (!type) return c.json({ error: "invalid search type" }, 400);
      if (type === "intent") clauses.push("instr(lower(s.intent), lower(?)) > 0");
      if (type === "excerpts") clauses.push("(instr(lower(e.request_excerpt), lower(?)) > 0 OR instr(lower(e.response_excerpt), lower(?)) > 0)");
      if (type === "files") clauses.push("EXISTS (SELECT 1 FROM session_files sf WHERE sf.session_id = s.id AND instr(lower(sf.file), lower(?)) > 0)");
      if (type === "errors") clauses.push("EXISTS (SELECT 1 FROM session_errors se WHERE se.session_id = s.id AND instr(lower(se.signature), lower(?)) > 0)");
      values.push(...Array(clauseNeedles(type)).fill(needle));
    }
    const where = [`(${clauses.join(" OR ")})`];
    if (filters.repo) {
      where.push("root.repo = ?");
      values.push(filters.repo);
    }
    if (filters.outcome) {
      const canonical = canonicalOutcome(filters.outcome);
      if (!canonical) return c.json({ error: "invalid outcome" }, 400);
      where.push("root.work_outcome = ?");
      values.push(canonical);
    }
    where.push("e.capture_status = 'saved'");
    const sql = `${SESSION_TREE_CTE} SELECT root.id AS session_id, root.started_at, root.work_outcome AS outcome, root.repo, root.model_primary, e.id AS exchange_id, e.request_excerpt, e.response_excerpt, e.r2_key FROM sessions s JOIN session_tree ON session_tree.id = s.id JOIN sessions root ON root.id = session_tree.root_id JOIN exchanges e ON e.session_id = s.id WHERE ${where.join(" AND ")} ORDER BY root.started_at DESC LIMIT 50`;
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

  app.post("/integrations/hermes/authorize", async (c) => {
    const body = await c.req.json<{ token_hash?: unknown }>();
    const tokenHash = typeof body.token_hash === "string" ? body.token_hash.trim().toLowerCase() : "";
    if (!/^[a-f0-9]{64}$/.test(tokenHash)) return c.json({ error: "token_hash must be a SHA-256 hex digest" }, 400);
    await c.env.DB.prepare("INSERT INTO hermes_credentials(token_hash, created_at, authorized_by) VALUES (?, ?, ?) ON CONFLICT(token_hash) DO UPDATE SET authorized_by = excluded.authorized_by")
      .bind(tokenHash, new Date().toISOString(), c.get("tokenLabel")).run();
    return c.json({ authorized: true });
  });

  app.post("/integrations/harness-loads", async (c) => {
    let body: unknown;
    try {
      body = await c.req.json();
    } catch {
      return c.json({ error: "invalid JSON body" }, 400);
    }
    if (!body || typeof body !== "object" || Array.isArray(body)) return c.json({ error: "body must be an object" }, 400);
    const values = body as Record<string, unknown>;
    if (Object.keys(values).some((key) => !(HARNESS_LOAD_KEYS as readonly string[]).includes(key))) {
      return c.json({ error: "body contains unknown fields" }, 400);
    }
    if (values.version !== 1) {
      return c.json({ error: "version must be 1" }, 400);
    }
    if (typeof values.harness !== "string" || !(HARNESS_LOAD_NAMES as readonly string[]).includes(values.harness)) {
      return c.json({ error: "harness must be opencode or hermes" }, 400);
    }
    if (typeof values.source_sha256 !== "string" || !/^[a-f0-9]{64}$/.test(values.source_sha256)) {
      return c.json({ error: "source_sha256 must be a lowercase SHA-256 hex digest" }, 400);
    }
    for (const key of ["bundle_version", "cli_version", "cli_commit", "installation_id"] as const) {
      const value = values[key];
      if (value !== undefined && (typeof value !== "string" || value.length === 0 || value.length > 200)) {
        return c.json({ error: `${key} must be a non-empty string of at most 200 characters` }, 400);
      }
    }
    const reportedAt = new Date().toISOString();
    const installationID = values.installation_id ?? "";
    await c.env.DB.prepare(`INSERT INTO harness_loads(token_hash, token_label, harness, artifact_sha256, bundle_version, cli_version, cli_commit, installation_id, client_loaded_at, reported_at)
      VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
      ON CONFLICT(token_hash, harness, installation_id) DO UPDATE SET
        token_label = excluded.token_label,
        artifact_sha256 = excluded.artifact_sha256,
        bundle_version = excluded.bundle_version,
        cli_version = excluded.cli_version,
        cli_commit = excluded.cli_commit,
        client_loaded_at = CASE WHEN harness_loads.artifact_sha256 = excluded.artifact_sha256 THEN harness_loads.client_loaded_at ELSE excluded.client_loaded_at END,
        reported_at = excluded.reported_at`)
      .bind(c.get("tokenHash"), c.get("tokenLabel"), values.harness, values.source_sha256, values.bundle_version ?? null, values.cli_version ?? null, values.cli_commit ?? null, installationID, reportedAt, reportedAt).run();
    const load = await c.env.DB.prepare("SELECT harness, artifact_sha256, bundle_version, cli_version, cli_commit, installation_id, client_loaded_at, reported_at, token_label FROM harness_loads WHERE token_hash = ? AND harness = ? AND installation_id = ?")
      .bind(c.get("tokenHash"), values.harness, installationID).first();
    return c.json({ load });
  });

  app.get("/integrations/harness-loads", async (c) => {
    const result = await c.env.DB.prepare("SELECT harness, artifact_sha256, bundle_version, cli_version, cli_commit, installation_id, client_loaded_at, reported_at, token_label FROM harness_loads WHERE token_hash = ? ORDER BY reported_at DESC, harness")
      .bind(c.get("tokenHash")).all();
    return c.json({ loads: result.results });
  });

  app.post("/v1/chat/completions", (c) => proxy(c, "chat"));
  app.post("/v1/messages", (c) => proxy(c, "messages"));
  app.get("/v1/models", (c) => proxyOpenRouterGet(c, "/models"));
  app.get("/v1/credits", (c) => proxyOpenRouterGet(c, "/credits"));
  app.get("/v1/key", (c) => proxyOpenRouterGet(c, "/key"));

  // Hermes can override its built-in OpenRouter base URL but cannot attach
  // dynamic per-provider metadata. The path-scoped compatibility surface
  // keeps the normal OpenRouter model picker while identifying capture
  // without a user-visible custom provider.
  app.post("/v1/hermes/chat/completions", (c) => proxy(c, "chat", { harness: "hermes" }));
  app.get("/v1/hermes/models", (c) => proxyOpenRouterGet(c, "/models"));
  app.get("/v1/hermes/credits", (c) => proxyOpenRouterGet(c, "/credits"));
  app.get("/v1/hermes/key", (c) => proxyOpenRouterGet(c, "/key"));
}

async function proxyOpenRouterGet(c: Context<AppEnv>, path: "/models" | "/credits" | "/key") {
  const response = await fetch(`https://openrouter.ai/api/v1${path}`, { headers: buildUpstreamHeaders(c.req.raw.headers, c.get("upstreamOpenRouterKey") ?? c.env.OPENROUTER_API_KEY) });
  return new Response(response.body, response);
}
