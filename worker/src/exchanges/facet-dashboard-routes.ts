import type { Hono } from "hono";
import type { AppEnv } from "../env";
import { SESSION_SUBTREE_CTE } from "../sessions/session-queries";

const FACET_LIMIT = 50;

export function registerDashboardFacetRoutes(app: Hono<AppEnv>) {
  app.get("/dashboard/api/facets", async (c) => {
    const sessionId = c.req.query("session");
    const scope = sessionId
      ? {
          cte: `${SESSION_SUBTREE_CTE} `,
          where: "AND session_id IN (SELECT id FROM subtree)",
          values: [sessionId],
        }
      : { cte: "", where: "", values: [] as string[] };
    const exchangeFacet = (column: string) =>
      c.env.DB.prepare(
        `${scope.cte}SELECT ${column} AS value, COUNT(*) AS requests FROM exchanges WHERE capture_status = 'saved' AND ${column} IS NOT NULL AND ${column} <> '' ${scope.where} GROUP BY ${column} ORDER BY requests DESC, value ASC LIMIT ${FACET_LIMIT}`,
      )
        .bind(...scope.values)
        .all<{ value: string }>();
    const sessionFacet = (column: string) =>
      c.env.DB.prepare(
        `SELECT ${column} AS value, COUNT(*) AS sessions FROM sessions WHERE ${column} IS NOT NULL AND ${column} <> '' GROUP BY ${column} ORDER BY sessions DESC, value ASC LIMIT ${FACET_LIMIT}`,
      ).all<{ value: string }>();
    const [repos, apps, models, providers, finishReasons] = await Promise.all([
      sessionId
        ? Promise.resolve({ results: [] as Array<{ value: string }> })
        : sessionFacet("repo"),
      exchangeFacet("harness"),
      exchangeFacet("model"),
      exchangeFacet("provider"),
      exchangeFacet("finish_reason"),
    ]);
    return c.json({
      repos: repos.results.map((row) => row.value),
      apps: apps.results.map((row) => row.value),
      models: models.results.map((row) => row.value),
      providers: providers.results.map((row) => row.value),
      finish_reasons: finishReasons.results.map((row) => row.value),
    });
  });

  app.get("/dashboard/api/overview", async (c) => {
    const [totals, models, providers, apps] = await Promise.all([
      c.env.DB.prepare(
        "SELECT COUNT(*) AS requests, (SELECT COUNT(*) FROM sessions WHERE parent_session_id IS NULL) AS sessions, COALESCE(SUM(CASE WHEN capture_status = 'saved' THEN 1 ELSE 0 END), 0) AS saved_exchanges, COALESCE(SUM(CASE WHEN capture_status = 'failed' THEN 1 ELSE 0 END), 0) AS capture_failures, COALESCE(SUM(CASE WHEN capture_status = 'saved' THEN input_tokens ELSE 0 END), 0) AS input_tokens, COALESCE(SUM(CASE WHEN capture_status = 'saved' THEN output_tokens ELSE 0 END), 0) AS output_tokens FROM exchanges",
      ).first(),
      c.env.DB.prepare(
        "SELECT model AS name, COUNT(*) AS requests FROM exchanges WHERE capture_status = 'saved' GROUP BY model ORDER BY requests DESC LIMIT 6",
      ).all(),
      c.env.DB.prepare(
        "SELECT COALESCE(provider, 'Unknown') AS name, COUNT(*) AS requests FROM exchanges WHERE capture_status = 'saved' GROUP BY provider ORDER BY requests DESC LIMIT 6",
      ).all(),
      c.env.DB.prepare(
        "SELECT COALESCE(harness, 'Unknown') AS name, COUNT(*) AS requests FROM exchanges WHERE capture_status = 'saved' GROUP BY harness ORDER BY requests DESC LIMIT 6",
      ).all(),
    ]);
    return c.json({
      totals,
      models: models.results,
      providers: providers.results,
      apps: apps.results,
    });
  });
}
