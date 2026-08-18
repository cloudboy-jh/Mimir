import type { Hono } from "hono";
import type { AppEnv } from "../env";

export function registerDashboardAuthRoutes(app: Hono<AppEnv>) {
  app.get("/dashboard/auth", (c) => {
    const returnTo = c.req.query("returnTo") ?? "/dashboard/sessions";
    return c.redirect(
      returnTo.startsWith("/dashboard/") && !returnTo.startsWith("//")
        ? returnTo
        : "/dashboard/sessions",
    );
  });

  app.get("/dashboard/api/identity", (c) => c.json(c.get("dashboardIdentity")));

  app.get("/dashboard/api/bootstrap", async (c) => {
    const [captures, sessions, latest] = await Promise.all([
      c.env.DB.prepare(
        "SELECT COUNT(*) AS requests, SUM(CASE WHEN capture_status = 'saved' THEN 1 ELSE 0 END) AS saved_exchanges, SUM(CASE WHEN capture_status = 'failed' THEN 1 ELSE 0 END) AS capture_failures FROM exchanges",
      ).first(),
      c.env.DB.prepare(
        "SELECT COUNT(*) AS count FROM sessions WHERE parent_session_id IS NULL",
      ).first<{ count: number }>(),
      c.env.DB.prepare(
        "SELECT ts FROM exchanges WHERE capture_status = 'saved' ORDER BY ts DESC LIMIT 1",
      ).first<{ ts: string }>(),
    ]);
    return c.json({
      requests: captures?.requests ?? 0,
      saved_exchanges: captures?.saved_exchanges ?? 0,
      capture_failures: captures?.capture_failures ?? 0,
      sessions: sessions?.count ?? 0,
      latest_request_at: latest?.ts ?? null,
    });
  });
}
