import { Hono } from "hono";
import { authenticate } from "./auth";
import { registerDashboardRoutes } from "./routes/dashboard";
import { registerMachineRoutes } from "./routes/machine";
import type { AppEnv } from "./types";

const app = new Hono<AppEnv>();

app.onError((error, c) => {
  console.error(JSON.stringify({ message: "request failed", error: error.message, method: c.req.method, path: c.req.path }));
  return c.json({ error: "internal server error" }, 500);
});

app.use("*", authenticate);

registerDashboardRoutes(app);
registerMachineRoutes(app);

export { requestToken } from "./auth";
export { deriveSessionFields, extractUsage, parseCapturedResponse, readBoundedText, redact } from "./capture";
export { decideCapture, shouldSave, validateConfigValues } from "./config";
export { buildUpstreamHeaders } from "./proxy";
export { app };
export default app;
