import type { Context, Hono } from "hono";
import type { AppEnv } from "../env";
import { ingestReportedExchange, ingestReportedExchangeFailure } from "../exchanges/reported-exchange-routes";
import { reconcile } from "./capture-status";
import { applySessionEvent, parseSessionEvent, SESSION_ID } from "./events";
import { ingestGitArtifacts, readGitArtifactBody } from "./git-artifacts";
import { assignParent, canMutateSession, endSession } from "./lifecycle";
import { rootSessionID } from "./session-tree";
import { buildSessionSummary } from "./summaries";

/** Builds the root's summary after the response; a root that is still active is skipped. */
function scheduleSummary(c: Context<AppEnv>, sessionID: string) {
  c.executionCtx.waitUntil(
    rootSessionID(c.env.DB, sessionID)
      .then((root) => buildSessionSummary(c.env, root))
      .catch((error) =>
        console.error(JSON.stringify({ message: "session summary failed", session_id: sessionID, error: error instanceof Error ? error.message : String(error) })),
      ),
  );
}

export function registerSessionRoutes(app: Hono<AppEnv>) {
  const requireSessionOwnership = async (c: Context<AppEnv>, next: () => Promise<void>) => {
    if (!(await canMutateSession(c.env.DB, c.req.param("id") ?? "", c.get("installationID"))))
      return c.json({ error: "session belongs to another installation" }, 403);
    await next();
  };
  app.use("/sessions/:id/end", requireSessionOwnership);
  app.use("/sessions/:id/git-artifacts", requireSessionOwnership);
  app.use("/sessions/:id/parent", requireSessionOwnership);

  app.post("/sessions/:id/events", async (c) => {
    const id = c.req.param("id");
    let body: unknown;
    try {
      body = await c.req.json();
    } catch {
      return c.json({ error: "invalid JSON body" }, 400);
    }
    const parsed = parseSessionEvent({
      ...(body && typeof body === "object" && !Array.isArray(body) ? (body as Record<string, unknown>) : {}),
      session_id: id,
    });
    if ("error" in parsed) return c.json({ error: parsed.error }, 400);
    const result = await applySessionEvent(c.env.DB, parsed, c.get("installationID"));
    if (result.ended) scheduleSummary(c, id);
    return c.json(result.body, result.status);
  });

  app.post("/sessions/:id/end", async (c) => {
    const id = c.req.param("id");
    const endedAt = new Date().toISOString();
    if (!(await endSession(c.env.DB, id, endedAt))) return c.json({ error: "session not found" }, 404);
    scheduleSummary(c, id);
    const session = await c.env.DB.prepare("SELECT ended_at FROM sessions WHERE id = ?").bind(id).first<{ ended_at: string }>();
    return c.json({ session_id: id, state: "inactive", ended_at: session!.ended_at });
  });

  app.post("/sessions/:id/exchanges", ingestReportedExchange);
  app.post("/sessions/:id/exchange-failures", ingestReportedExchangeFailure);

  app.get("/sessions/:id/parent", async (c) => {
    const id = c.req.param("id");
    if (!SESSION_ID.test(id)) return c.json({ error: "invalid session id" }, 400);
    const session = await c.env.DB.prepare("SELECT id AS session_id, parent_session_id, harness AS app FROM sessions WHERE id = ?")
      .bind(id)
      .first();
    if (!session) return c.json({ error: "session not found" }, 404);
    return c.json(session);
  });

  app.patch("/sessions/:id/parent", async (c) => {
    const id = c.req.param("id");
    if (!SESSION_ID.test(id)) return c.json({ error: "invalid session id" }, 400);
    let body: unknown;
    try {
      body = await c.req.json();
    } catch {
      return c.json({ error: "invalid JSON body" }, 400);
    }
    const parentID = body && typeof body === "object" && !Array.isArray(body) ? (body as Record<string, unknown>).parent_session_id : null;
    if (typeof parentID !== "string" || !SESSION_ID.test(parentID) || parentID === id)
      return c.json({ error: "invalid parent_session_id" }, 400);
    if (!(await c.env.DB.prepare("SELECT 1 FROM sessions WHERE id = ?").bind(id).first()))
      return c.json({ error: "session not found" }, 404);
    const rejected = await assignParent(c.env.DB, id, parentID, c.get("installationID"), false);
    if (rejected) return c.json({ error: rejected.error }, rejected.status);
    return c.json({ session_id: id, parent_session_id: parentID });
  });

  app.post("/sessions/:id/git-artifacts", async (c) => {
    const parsed = await readGitArtifactBody(c.req.raw);
    if ("error" in parsed) return c.json({ error: parsed.error }, parsed.error.endsWith("too large") ? 413 : 400);
    const result = await ingestGitArtifacts(c.env.DB, c.env.LOGS, c.req.param("id"), parsed.body);
    if (result.kind === "invalid") return c.json({ error: result.error }, 400);
    if (result.kind === "not-found") return c.json({ error: "session not found" }, 404);
    if (result.kind === "conflict")
      return c.json({ error: "Git artifact conflicts with stored commit", commit_sha: result.commit_sha }, 409);
    if (result.kind === "partial") return c.json(result, 503);
    return c.json(result, result.duplicates === result.artifacts.length ? 200 : 201);
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
}
