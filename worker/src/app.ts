import { Hono } from "hono";
import { authenticate } from "./auth/auth-middleware";
import { registerConfigRoutes } from "./config/config-routes";
import { registerDashboardAuthRoutes } from "./dashboard/dashboard-shell-routes";
import type { AppEnv } from "./env";
import { registerDashboardExchangeRoutes } from "./exchanges/exchange-dashboard-routes";
import { registerDashboardFacetRoutes } from "./exchanges/facet-dashboard-routes";
import { registerProxyRoutes } from "./gateway/openrouter-routes";
import { registerIntegrationRoutes } from "./integrations/integration-routes";
import { registerDashboardDeviceRoutes } from "./machines/device-dashboard-routes";
import { registerMachineRoutes } from "./machines/machine-routes";
import { registerSearchRoutes } from "./search/search-routes";
import { registerDashboardSessionRoutes } from "./sessions/session-dashboard-routes";
import { registerSessionRoutes } from "./sessions/session-routes";

const app = new Hono<AppEnv>();

installErrorHandling(app);
installAuthentication(app);

registerMachineRoutes(app);
registerProxyRoutes(app);
registerSessionRoutes(app);
registerSearchRoutes(app);
registerConfigRoutes(app);
registerIntegrationRoutes(app);
registerDashboardAuthRoutes(app);
registerDashboardSessionRoutes(app);
registerDashboardExchangeRoutes(app);
registerDashboardDeviceRoutes(app);
registerDashboardFacetRoutes(app);

function installErrorHandling(target: Hono<AppEnv>) {
  target.onError((error, c) => {
    console.error(
      JSON.stringify({
        message: "request failed",
        error: error.message,
        method: c.req.method,
        path: c.req.path,
      }),
    );
    return c.json({ error: "internal server error" }, 500);
  });
}

function installAuthentication(target: Hono<AppEnv>) {
  target.use("*", authenticate);
}

export { app };
export default app;
