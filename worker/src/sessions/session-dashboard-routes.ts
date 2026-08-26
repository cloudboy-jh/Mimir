import type { Hono } from "hono";
import { boundedLimit, decodeCursor, encodeCursor } from "../dashboard/cursors";
import type { AppEnv } from "../env";
import { ensureSessionSummary } from "./summaries";
import { SESSION_ID } from "./events";
import { expireSessions } from "./lifecycle";
import { canonicalOutcome, updateOutcome } from "./outcomes";
import {
  loadGitArtifactPatch,
  loadSessionGitArtifacts,
} from "./git-artifacts";
import {
  loadSessionDiffEvidence,
  loadSessionErrorSignatures,
  loadSessionFiles,
  loadSessionOutcomeEvents,
  loadSessionRecord,
  loadSessionStatus,
  loadSupportingSessions,
  rootSessionID,
  ROOT_SESSION_ACTIVITY_AT,
  ROOT_SESSION_COLUMNS,
  SESSION_COLUMNS,
  SESSION_SUBTREE_CTE,
  SESSION_TREE_CTE,
} from "./session-queries";
import {
  attachCaptureSummary,
  captureSummary,
  captureTreeSummary,
  sessionStatusResponse,
  TREE_CAPTURE_SUMMARY_COLUMNS,
} from "./capture-status";
import {
  MAX_SESSION_TITLE_CHARS,
  normalizeSessionTitle,
  sessionTitleColumns,
  sessionTitleSearchClause,
  titleUpdateStatement,
} from "./titles";

type SessionModel = {
  name: string;
  request_count: number;
  first_seen_at: string | null;
  last_seen_at: string | null;
};

async function attachSessionModels(
  db: D1Database,
  rows: Array<Record<string, unknown>>,
) {
  if (!rows.length) return rows;
  const ids = rows.map((row) => String(row.id));
  const placeholders = ids.map(() => "?").join(", ");
  const grouped = await db
    .prepare(
      `SELECT session_id, model AS name, COUNT(*) AS request_count, MIN(ts) AS first_seen_at, MAX(ts) AS last_seen_at FROM exchanges WHERE capture_status = 'saved' AND model IS NOT NULL AND model <> '' AND session_id IN (${placeholders}) GROUP BY session_id, model ORDER BY first_seen_at ASC, name ASC`,
    )
    .bind(...ids)
    .all<SessionModel & { session_id: string }>();
  const bySession = new Map<string, SessionModel[]>();
  for (const { session_id, ...model } of grouped.results) {
    const models = bySession.get(session_id) ?? [];
    models.push(model);
    bySession.set(session_id, models);
  }
  return rows.map((row) => {
    const primary =
      typeof row.model_primary === "string" && row.model_primary
        ? row.model_primary
        : null;
    const models = bySession.get(String(row.id)) ?? [];
    if (primary && !models.some((model) => model.name === primary)) {
      models.unshift({
        name: primary,
        request_count: 0,
        first_seen_at: null,
        last_seen_at: null,
      });
    } else if (primary) {
      models.sort((left, right) =>
        left.name === primary
          ? -1
          : right.name === primary
            ? 1
            : (left.first_seen_at ?? "").localeCompare(
                right.first_seen_at ?? "",
              ) || left.name.localeCompare(right.name),
      );
    }
    return { ...row, models };
  });
}

async function attachSessionDevices(
  db: D1Database,
  rows: Array<Record<string, unknown>>,
) {
  const ids = [
    ...new Set(
      rows
        .map((row) => row.installation_id)
        .filter((id): id is string => typeof id === "string" && id.length > 0),
    ),
  ];
  if (!ids.length)
    return rows.map(({ installation_id: _installationID, ...row }) => ({
      ...row,
      device: null,
    }));
  const devices = await db
    .prepare(
      `SELECT installation_id AS id, name, platform, arch FROM machines WHERE installation_id IN (${ids.map(() => "?").join(", ")})`,
    )
    .bind(...ids)
    .all<{ id: string; name: string; platform: string; arch: string }>();
  const byID = new Map(devices.results.map((device) => [device.id, device]));
  return rows.map(({ installation_id, ...row }) => ({
    ...row,
    device:
      typeof installation_id === "string"
        ? (byID.get(installation_id) ?? null)
        : null,
  }));
}

async function sessionObjectResponse(
  env: AppEnv["Bindings"],
  id: string,
  path: "/state" | "/feed",
  headers?: HeadersInit,
) {
  const stub = env.SESSIONS.get(env.SESSIONS.idFromName(id));
  return stub.fetch(`https://session-object${path}`, { headers });
}

async function attachSessionLiveness(
  env: AppEnv["Bindings"],
  rows: Array<Record<string, unknown>>,
) {
  return Promise.all(
    rows.map(async (row) => {
      if (row.state !== "active") return { ...row, liveness: "finalized" };
      try {
        const response = await sessionObjectResponse(
          env,
          String(row.id),
          "/state",
        );
        if (!response.ok) return { ...row, liveness: "disconnected" };
        const state = await response.json<{ liveness?: unknown }>();
        const liveness =
          state.liveness === "active" ||
          state.liveness === "disconnected" ||
          state.liveness === "finalized"
            ? state.liveness
            : "disconnected";
        return { ...row, liveness };
      } catch {
        return { ...row, liveness: "disconnected" };
      }
    }),
  );
}

export function registerDashboardSessionRoutes(app: Hono<AppEnv>) {
  app.get("/dashboard/api/sessions", async (c) => {
    await expireSessions(c.env.DB);
    const where = ["sessions.parent_session_id IS NULL"];
    const values: Array<string | number> = [];
    const q = c.req.query("q");
    if (q) {
      where.push(
        `(${sessionTitleSearchClause("sessions")} OR instr(lower(COALESCE(sessions.repo, '')), lower(?)) > 0 OR instr(lower(COALESCE(sessions.harness, '')), lower(?)) > 0 OR instr(lower(COALESCE(sessions.model_primary, '')), lower(?)) > 0 OR EXISTS (SELECT 1 FROM exchanges model_search WHERE model_search.session_id = sessions.id AND model_search.capture_status = 'saved' AND instr(lower(COALESCE(model_search.model, '')), lower(?)) > 0) OR instr(lower(sessions.id), lower(?)) > 0)`,
      );
      values.push(q, q, q, q, q, q, q);
    }
    for (const [parameter, column] of [
      ["repo", "repo"],
      ["app", "harness"],
    ] as const) {
      const value = c.req.query(parameter);
      if (value) {
        where.push(`sessions.${column} = ?`);
        values.push(value);
      }
    }
    const model = c.req.query("model");
    if (model) {
      where.push(
        "(sessions.model_primary = ? OR EXISTS (SELECT 1 FROM exchanges model_filter WHERE model_filter.session_id = sessions.id AND model_filter.capture_status = 'saved' AND model_filter.model = ?))",
      );
      values.push(model, model);
    }
    const outcome = c.req.query("outcome");
    if (outcome) {
      const canonical = canonicalOutcome(outcome);
      if (!canonical) return c.json({ error: "invalid outcome" }, 400);
      where.push("sessions.work_outcome = ?");
      values.push(canonical);
    }
    for (const [parameter, operator] of [
      ["from", ">="],
      ["to", "<="],
    ] as const) {
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
      where.push(
        `(${ROOT_SESSION_ACTIVITY_AT} < ? OR (${ROOT_SESSION_ACTIVITY_AT} = ? AND sessions.id < ?))`,
      );
      values.push(cursor.ts, cursor.ts, cursor.id);
    }
    const limit = boundedLimit(c.req.query("limit"));
    const result = await c.env.DB.prepare(
      `${SESSION_TREE_CTE} SELECT ${ROOT_SESSION_COLUMNS}, ${TREE_CAPTURE_SUMMARY_COLUMNS} FROM sessions WHERE ${where.join(" AND ")} ORDER BY ${ROOT_SESSION_ACTIVITY_AT} DESC, sessions.id DESC LIMIT ?`,
    )
      .bind(...values, limit + 1)
      .all<Record<string, unknown>>();
    const hasMore = result.results.length > limit;
    const pageRoots = result.results.slice(0, limit);
    const rootIDs = pageRoots.map((row) => String(row.id));
    // Descendants of the current page ride along so the dashboard can render
    // sub-agent tree rows without a second round trip. Rows are flat with
    // parent_session_id intact; the client builds the hierarchy.
    const descendantResult = rootIDs.length
      ? await c.env.DB.prepare(
          `WITH RECURSIVE rooted(id) AS (SELECT id FROM sessions WHERE id IN (${rootIDs.map(() => "?").join(", ")}) AND parent_session_id IS NULL UNION ALL SELECT sessions.id FROM sessions JOIN rooted ON sessions.parent_session_id = rooted.id) SELECT ${SESSION_COLUMNS} FROM sessions WHERE id IN (SELECT id FROM rooted) AND parent_session_id IS NOT NULL ORDER BY id ASC`,
        )
          .bind(...rootIDs)
          .all<Record<string, unknown>>()
      : { results: [] as Array<Record<string, unknown>> };
    const sessions = await attachSessionLiveness(
      c.env,
      await attachSessionDevices(
        c.env.DB,
        await attachSessionModels(
          c.env.DB,
          pageRoots.map(attachCaptureSummary),
        ),
      ),
    );
    const descendants = await attachSessionLiveness(
      c.env,
      await attachSessionDevices(
        c.env.DB,
        await attachSessionModels(
          c.env.DB,
          descendantResult.results.map(attachCaptureSummary),
        ),
      ),
    );
    const last = sessions.at(-1) as
      { activity_at?: string; id?: string } | undefined;
    return c.json({
      sessions,
      descendants,
      next_cursor:
        hasMore && last?.activity_at && last.id
          ? encodeCursor(last.activity_at, last.id)
          : null,
    });
  });

  app.get("/dashboard/api/sessions/:id", async (c) => {
    await expireSessions(c.env.DB);
    const id = c.req.param("id");
    const session = await loadSessionRecord(c.env.DB, id);
    if (!session) return c.json({ error: "session not found" }, 404);
    const outcomeRoot = await rootSessionID(c.env.DB, id);
    const [
      files,
      signatures,
      aggregates,
      latestLinks,
      capture,
      outcomeEvents,
      children,
      gitArtifacts,
    ] = await Promise.all([
      loadSessionFiles(c.env.DB, id),
      loadSessionErrorSignatures(c.env.DB, id),
      c.env.DB.prepare(
        `${SESSION_SUBTREE_CTE} SELECT ee.signature, COUNT(DISTINCT ee.exchange_id) AS count, MIN(e.ts) AS first_seen_at, MAX(e.ts) AS last_seen_at FROM exchange_errors ee JOIN exchanges e ON e.id = ee.exchange_id WHERE ee.session_id IN (SELECT id FROM subtree) AND e.capture_status = 'saved' GROUP BY ee.signature`,
      )
        .bind(id)
        .all<{
          signature: string;
          count: number;
          first_seen_at: string | null;
          last_seen_at: string | null;
        }>(),
      c.env.DB.prepare(
        `${SESSION_SUBTREE_CTE} SELECT ee.signature, ee.exchange_id FROM exchange_errors ee JOIN exchanges e ON e.id = ee.exchange_id WHERE ee.session_id IN (SELECT id FROM subtree) AND e.capture_status = 'saved' ORDER BY e.ts DESC, e.id DESC`,
      )
        .bind(id)
        .all<{ signature: string; exchange_id: string }>(),
      captureTreeSummary(c.env.DB, id),
      loadSessionOutcomeEvents(c.env.DB, outcomeRoot),
      loadSupportingSessions(c.env.DB, id),
      loadSessionGitArtifacts(c.env.DB, id),
    ]);
    const aggregateBySignature = new Map(
      aggregates.results.map((row) => [row.signature, row]),
    );
    const latestBySignature = new Map<string, string>();
    for (const row of latestLinks.results) {
      if (!latestBySignature.has(row.signature))
        latestBySignature.set(row.signature, row.exchange_id);
    }
    const errors = signatures.map((signature) => {
      const aggregate = aggregateBySignature.get(signature);
      return {
        signature,
        count: aggregate?.count ?? 1,
        first_seen_at: aggregate?.first_seen_at ?? null,
        last_seen_at: aggregate?.last_seen_at ?? null,
        latest_exchange_id: latestBySignature.get(signature) ?? null,
      };
    });
    const summarized = await ensureSessionSummary(
      c.env.DB,
      session,
      files.length,
      errors.length,
    );
    const modeled = await attachSessionDevices(
      c.env.DB,
      await attachSessionModels(c.env.DB, [summarized, ...children]),
    );
    return c.json({
      session: modeled[0],
      capture,
      outcome_events: outcomeEvents,
      supporting_sessions: modeled.slice(1),
      files,
      errors,
      git_artifacts: gitArtifacts,
    });
  });

  app.get(
    "/dashboard/api/sessions/:id/git-artifacts/:commit/patch",
    async (c) => {
      const patch = await loadGitArtifactPatch(
        c.env.DB,
        c.env.LOGS,
        c.req.param("id"),
        c.req.param("commit"),
      );
      if (patch.kind === "invalid")
        return c.json({ error: "invalid commit SHA" }, 400);
      if (patch.kind === "session-not-found")
        return c.json({ error: "session not found" }, 404);
      if (patch.kind === "artifact-not-found")
        return c.json({ error: "Git artifact not found" }, 404);
      if (patch.kind === "artifact-unavailable")
        return c.json({ error: "Git artifact patch is not saved" }, 409);
      if (patch.kind === "patch-not-found")
        return c.json({ error: "Git artifact patch not found" }, 404);
      return new Response(patch.body, {
        headers: {
          "content-type": "text/plain; charset=utf-8",
          "cache-control": "private, no-store",
        },
      });
    },
  );

  app.get("/dashboard/api/sessions/:id/diff", async (c) => {
    const diff = await loadSessionDiffEvidence(
      c.env.DB,
      c.env.LOGS,
      c.req.param("id"),
    );
    if (diff.kind === "session-not-found")
      return c.json({ error: "session not found" }, 404);
    if (diff.kind === "missing-artifact")
      return c.json({ error: "diff artifact not found" }, 404);
    if (diff.kind === "unavailable")
      return c.json({ error: "diff unavailable" }, 404);
    return new Response(diff.body, {
      headers: {
        "content-type": "text/plain; charset=utf-8",
        "cache-control": "private, no-store",
      },
    });
  });

  app.get("/dashboard/api/sessions/:id/status", async (c) => {
    const session = await loadSessionStatus(c.env.DB, c.req.param("id"));
    if (!session) return c.json({ error: "session not found" }, 404);
    const capture = await captureSummary(c.env.DB, c.req.param("id"));
    c.header("cache-control", "no-store");
    return c.json(
      sessionStatusResponse(
        c.req.url,
        c.req.param("id"),
        capture,
        session,
        true,
      ),
    );
  });

  app.post("/dashboard/api/sessions/:id/mark", async (c) => {
    const body = await c.req.json<{
      outcome?: string;
      source?: string;
      reason?: unknown;
      evidence?: unknown;
    }>();
    return updateOutcome(c, { ...body, source: "user" }, "user");
  });

  app.post("/dashboard/api/sessions/:id/outcome", async (c) => {
    const body = await c.req.json<{
      outcome?: string;
      source?: string;
      reason?: unknown;
      evidence?: unknown;
    }>();
    return updateOutcome(c, { ...body, source: "user" }, "user");
  });

  app.get("/dashboard/api/sessions/:id/object-state", async (c) => {
    const id = c.req.param("id");
    if (!SESSION_ID.test(id))
      return c.json({ error: "invalid session id" }, 400);
    if (
      !(await c.env.DB.prepare("SELECT 1 FROM sessions WHERE id = ?")
        .bind(id)
        .first())
    )
      return c.json({ error: "session not found" }, 404);
    const response = await sessionObjectResponse(c.env, id, "/state");
    c.header("cache-control", "no-store");
    return new Response(response.body, {
      status: response.status,
      headers: {
        "content-type": "application/json",
        "cache-control": "no-store",
      },
    });
  });

  app.get("/dashboard/api/sessions/:id/live", async (c) => {
    if ((c.req.header("upgrade") ?? "").toLowerCase() !== "websocket")
      return c.json({ error: "websocket upgrade required" }, 426);
    const id = c.req.param("id");
    if (!SESSION_ID.test(id))
      return c.json({ error: "invalid session id" }, 400);
    if (
      !(await c.env.DB.prepare("SELECT 1 FROM sessions WHERE id = ?")
        .bind(id)
        .first())
    )
      return c.json({ error: "session not found" }, 404);
    return sessionObjectResponse(c.env, id, "/feed", { Upgrade: "websocket" });
  });

  app.patch("/dashboard/api/sessions/:id/title", async (c) => {
    let body: unknown;
    try {
      body = await c.req.json();
    } catch {
      return c.json({ error: "invalid JSON body" }, 400);
    }
    if (
      !body ||
      typeof body !== "object" ||
      Array.isArray(body) ||
      Object.keys(body).some((key) => key !== "title")
    )
      return c.json({ error: "body must contain only title" }, 400);
    const value = (body as Record<string, unknown>).title;
    if (typeof value !== "string")
      return c.json({ error: "title must be a string" }, 400);
    const title = normalizeSessionTitle(value);
    if (!title)
      return c.json(
        {
          error: `title must be non-empty and at most ${MAX_SESSION_TITLE_CHARS} characters`,
        },
        400,
      );
    const id = c.req.param("id");
    if (
      !(await c.env.DB.prepare("SELECT 1 FROM sessions WHERE id = ?")
        .bind(id)
        .first())
    )
      return c.json({ error: "session not found" }, 404);
    const now = new Date().toISOString();
    await titleUpdateStatement(c.env.DB, id, title, "manual", now).run();
    const session = await c.env.DB.prepare(
      `SELECT id, intent, ${sessionTitleColumns("sessions")} FROM sessions WHERE id = ?`,
    )
      .bind(id)
      .first();
    return c.json({ session });
  });
}
