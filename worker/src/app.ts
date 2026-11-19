import { Hono } from "hono";
import { authenticate } from "./auth/auth-middleware";
import { registerConfigRoutes } from "./config/config-routes";
import { registerDashboardRoutes } from "./dashboard/dashboard-routes";
import type { AppEnv } from "./env";
import { registerProxyRoutes } from "./gateway/openrouter-routes";
import { registerIntegrationRoutes } from "./integrations/integration-routes";
import { registerMachineRoutes } from "./machines/machine-routes";
import { registerMcpRoutes } from "./mcp/mcp-routes";
import { deviceEditRouter, queryRouter } from "./queries/query-routes";
import { registerSessionRoutes } from "./sessions/session-routes";
import { QueryError } from "./shared/query-error";

const app = new Hono<AppEnv>();

app.onError((error, c) => {
  if (error instanceof QueryError) return c.json({ error: error.message }, error.status);
  console.error(JSON.stringify({ message: "request failed", error: error.message, method: c.req.method, path: c.req.path }));
  return c.json({ error: "internal server error" }, 500);
});
app.notFound((c) => c.json({ error: "not found" }, 404));
app.use("*", authenticate);

registerMachineRoutes(app);
registerProxyRoutes(app);
registerSessionRoutes(app);
registerConfigRoutes(app);
registerIntegrationRoutes(app);
registerDashboardRoutes(app);
registerMcpRoutes(app);

const queries = queryRouter();
app.route("/api", queries);
app.route("/dashboard/api", queries);
app.route("/dashboard/api", deviceEditRouter());

export default app;
