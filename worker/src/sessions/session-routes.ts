import type { Context, Hono } from "hono";
import type { AppEnv } from "../env";
import { ingestReportedExchange } from "../exchanges/reported-exchange-routes";
import { parseSessionEvent, SESSION_ID } from "./events";
import { canMutateSession, expireSessions } from "./lifecycle";
import {
  ingestGitArtifacts,
  loadSessionGitArtifacts,
  readGitArtifactBody,
} from "./git-artifacts";
import { canonicalOutcome, endSession, updateOutcome } from "./outcomes";
import {
  loadSessionErrorSignatures,
  loadSessionFiles,
  loadSessionOutcomeEvents,
  loadSessionRecord,
  loadSessionStatus,
  loadSupportingSessions,
  rootSessionID,
  ROOT_SESSION_ACTIVITY_AT,
  ROOT_SESSION_COLUMNS,
  SESSION_SUBTREE_CTE,
  SESSION_TREE_CTE,
} from "./session-queries";
import {
  attachCaptureSummary,
  captureSummary,
  captureTreeSummary,
  reconcile,
  sessionStatusResponse,
  TREE_CAPTURE_SUMMARY_COLUMNS,
} from "./capture-status";

export function registerSessionRoutes(app: Hono<AppEnv>) {
  app.get("/sessions", async (c) => {
    await expireSessions(c.env.DB);
    const where: string[] = [];
    const values: string[] = [];
    for (const [field, column] of [
      ["repo", "repo"],
      ["model", "model_primary"],
    ] as const) {
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
    const sql = `${SESSION_TREE_CTE} SELECT ${ROOT_SESSION_COLUMNS}, ${TREE_CAPTURE_SUMMARY_COLUMNS} FROM sessions WHERE ${where.join(" AND ")} ORDER BY ${ROOT_SESSION_ACTIVITY_AT} DESC, sessions.id DESC LIMIT 100`;
    const results = await c.env.DB.prepare(sql)
      .bind(...values)
      .all<Record<string, unknown>>();
    return c.json({ sessions: results.results.map(attachCaptureSummary) });
  });

  app.get("/sessions/:id", async (c) => {
    await expireSessions(c.env.DB);
    const id = c.req.param("id");
    const session = await loadSessionRecord(c.env.DB, id);
    if (!session) return c.json({ error: "session not found" }, 404);
    const outcomeRoot = await rootSessionID(c.env.DB, id);
    const [
      exchanges,
      files,
      errors,
      capture,
      outcomeEvents,
      children,
      gitArtifacts,
    ] = await Promise.all([
        c.env.DB.prepare(
          `${SESSION_SUBTREE_CTE} SELECT * FROM exchanges WHERE session_id IN (SELECT id FROM subtree) ORDER BY ts`,
        )
          .bind(id)
          .all(),
        loadSessionFiles(c.env.DB, id),
        loadSessionErrorSignatures(c.env.DB, id),
        captureTreeSummary(c.env.DB, id),
        loadSessionOutcomeEvents(c.env.DB, outcomeRoot),
        loadSupportingSessions(c.env.DB, id),
        loadSessionGitArtifacts(c.env.DB, id),
      ]);
    return c.json({
      session,
      capture,
      outcome_events: outcomeEvents,
      supporting_sessions: children,
      files,
      errors,
      exchanges: exchanges.results,
      git_artifacts: gitArtifacts,
    });
  });

  app.get("/sessions/:id/status", async (c) => {
    const session = await loadSessionStatus(c.env.DB, c.req.param("id"));
    if (!session) return c.json({ error: "session not found" }, 404);
    const capture = await captureSummary(c.env.DB, c.req.param("id"));
    const hostname = new URL(c.req.url).hostname;
    const dashboardAvailable =
      hostname === "localhost" ||
      hostname === "127.0.0.1" ||
      !!(c.env.DASHBOARD_ACCESS_AUD && c.env.DASHBOARD_ACCESS_TEAM_DOMAIN);
    c.header("cache-control", "no-store");
    return c.json(
      sessionStatusResponse(
        c.req.url,
        c.req.param("id"),
        capture,
        session,
        dashboardAvailable,
      ),
    );
  });

  const requireSessionOwnership = async (
    c: Context<AppEnv>,
    next: () => Promise<void>,
  ) => {
    if (
      !(await canMutateSession(
        c.env.DB,
        c.req.param("id") ?? "",
        c.get("installationID"),
      ))
    ) {
      return c.json({ error: "session belongs to another installation" }, 403);
    }
    await next();
  };
  app.use("/sessions/:id/mark", requireSessionOwnership);
  app.use("/sessions/:id/outcome", requireSessionOwnership);
  app.use("/sessions/:id/end", requireSessionOwnership);
  app.use("/sessions/:id/git-artifacts", requireSessionOwnership);

  app.post("/sessions/:id/mark", async (c) => {
    const body = await c.req.json<{
      outcome?: string;
      source?: string;
      reason?: unknown;
      evidence?: unknown;
    }>();
    return updateOutcome(c, { ...body, source: "agent" }, "agent");
  });

  app.post("/sessions/:id/outcome", async (c) => {
    const body = await c.req.json<{
      outcome?: string;
      source?: string;
      reason?: unknown;
      evidence?: unknown;
    }>();
    return updateOutcome(c, { ...body, source: "agent" }, "agent");
  });

  app.post("/sessions/:id/end", (c) => endSession(c, "agent"));
  app.post("/sessions/:id/exchanges", ingestReportedExchange);

  app.post("/sessions/:id/git-artifacts", async (c) => {
    const parsed = await readGitArtifactBody(c.req.raw);
    if ("error" in parsed)
      return c.json(
        { error: parsed.error },
        parsed.error.endsWith("too large") ? 413 : 400,
      );
    const result = await ingestGitArtifacts(
      c.env.DB,
      c.env.LOGS,
      c.req.param("id"),
      parsed.body,
    );
    if (result.kind === "invalid") return c.json({ error: result.error }, 400);
    if (result.kind === "not-found")
      return c.json({ error: "session not found" }, 404);
    if (result.kind === "conflict")
      return c.json(
        {
          error: "Git artifact conflicts with stored commit",
          commit_sha: result.commit_sha,
        },
        409,
      );
    if (result.kind === "partial") return c.json(result, 503);
    return c.json(
      result,
      result.duplicates === result.artifacts.length ? 200 : 201,
    );
  });

  app.post("/sessions/:id/events", async (c) => {
    const id = c.req.param("id");
    let body: unknown;
    try {
      body = await c.req.json();
    } catch {
      return c.json({ error: "invalid JSON body" }, 400);
    }
    const parsed = parseSessionEvent({
      ...(typeof body === "object" && body && !Array.isArray(body)
        ? (body as Record<string, unknown>)
        : {}),
      session_id: id,
    });
    if ("error" in parsed) return c.json({ error: parsed.error }, 400);
    const stub = c.env.SESSIONS.get(c.env.SESSIONS.idFromName(id));
    const installationID = c.get("installationID");
    const response = await stub.fetch("https://session-object/event", {
      method: "POST",
      headers: installationID
        ? { "x-mimir-installation": installationID }
        : undefined,
      body: JSON.stringify(parsed),
    });
    return new Response(response.body, {
      status: response.status,
      headers: { "content-type": "application/json" },
    });
  });

  app.get("/sessions/:id/live", async (c) => {
    if ((c.req.header("upgrade") ?? "").toLowerCase() !== "websocket")
      return c.json({ error: "websocket upgrade required" }, 426);
    const id = c.req.param("id");
    if (!SESSION_ID.test(id))
      return c.json({ error: "invalid session id" }, 400);
    const stub = c.env.SESSIONS.get(c.env.SESSIONS.idFromName(id));
    return stub.fetch("https://session-object/feed", {
      headers: { Upgrade: "websocket" },
    });
  });

  app.get("/sessions/:id/object-state", async (c) => {
    const id = c.req.param("id");
    if (!SESSION_ID.test(id))
      return c.json({ error: "invalid session id" }, 400);
    const stub = c.env.SESSIONS.get(c.env.SESSIONS.idFromName(id));
    const response = await stub.fetch("https://session-object/state");
    return new Response(response.body, {
      status: response.status,
      headers: { "content-type": "application/json" },
    });
  });

  app.post("/reconcile", async (c) =>
    c.json(
      await reconcile(
        c.env,
        Number(c.req.query("limit") ?? 100),
        c.req.query("cursor"),
        c.req.query("database_cursor"),
        c.req.query("scan_database") !== "false",
        c.req.query("scan_r2") !== "false",
      ),
    ),
  );

  app.get("/log/*", async (c) => {
    const key = c.req.path.replace(/^\/log\//, "");
    if (!key.startsWith("log/"))
      return c.json({ error: "invalid log key" }, 400);
    const object = await c.env.LOGS.get(key);
    if (!object) return c.json({ error: "log not found" }, 404);
    return new Response(object.body, {
      headers: { "content-type": "application/json" },
    });
  });
}
