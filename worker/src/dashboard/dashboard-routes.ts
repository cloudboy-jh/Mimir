import type { Hono } from "hono";
import type { AppEnv } from "../env";

export function registerDashboardRoutes(app: Hono<AppEnv>) {
  app.get("/dashboard/auth", (c) => {
    const returnTo = c.req.query("returnTo") ?? "/dashboard/days";
    return c.redirect(returnTo.startsWith("/dashboard/") && !returnTo.startsWith("//") ? returnTo : "/dashboard/days");
  });

  app.get("/dashboard/api/identity", (c) => c.json(c.get("dashboardIdentity")));
}
