import type { Context } from "hono";
import type { AppEnv } from "../env";

export type QueryContext = {
  db: D1Database;
  logs: R2Bucket;
  /** Origin serving the dashboard, or null when it is not reachable (no Access, not localhost). */
  dashboardOrigin: string | null;
};
export type QueryParams = Record<string, string | undefined>;

export function queryContext(c: Context<AppEnv>): QueryContext {
  const url = new URL(c.req.url);
  const local = url.hostname === "localhost" || url.hostname === "127.0.0.1";
  const access = Boolean(c.env.DASHBOARD_ACCESS_AUD && c.env.DASHBOARD_ACCESS_TEAM_DOMAIN);
  return { db: c.env.DB, logs: c.env.LOGS, dashboardOrigin: local || access ? url.origin : null };
}
