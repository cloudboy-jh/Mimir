import type { Context, Hono } from "hono";
import type { AppEnv } from "../env";
import { buildUpstreamHeaders, proxy } from "./upstream-proxy";

export function registerProxyRoutes(app: Hono<AppEnv>) {
  app.post("/v1/chat/completions", (c) => proxy(c, "chat"));
  app.post("/v1/messages", (c) => proxy(c, "messages"));
  app.get("/v1/models", (c) => proxyOpenRouterGet(c, "/models"));
  app.get("/v1/credits", (c) => proxyOpenRouterGet(c, "/credits"));
  app.get("/v1/key", (c) => proxyOpenRouterGet(c, "/key"));

  app.post("/v1/hermes/chat/completions", (c) =>
    proxy(c, "chat", { harness: "hermes" }),
  );
  app.get("/v1/hermes/models", (c) => proxyOpenRouterGet(c, "/models"));
  app.get("/v1/hermes/credits", (c) => proxyOpenRouterGet(c, "/credits"));
  app.get("/v1/hermes/key", (c) => proxyOpenRouterGet(c, "/key"));
  app.post("/v1/hermes/:installationID/chat/completions", (c) =>
    scopedHermesRoute(c, () => proxy(c, "chat", { harness: "hermes" })),
  );
  app.get("/v1/hermes/:installationID/models", (c) =>
    scopedHermesRoute(c, () => proxyOpenRouterGet(c, "/models")),
  );
  app.get("/v1/hermes/:installationID/credits", (c) =>
    scopedHermesRoute(c, () => proxyOpenRouterGet(c, "/credits")),
  );
  app.get("/v1/hermes/:installationID/key", (c) =>
    scopedHermesRoute(c, () => proxyOpenRouterGet(c, "/key")),
  );
}

function scopedHermesRoute(c: Context<AppEnv>, next: () => Promise<Response>) {
  return /^[a-f0-9]{32}$/.test(c.req.param("installationID") ?? "")
    ? next()
    : c.notFound();
}

async function proxyOpenRouterGet(
  c: Context<AppEnv>,
  path: "/models" | "/credits" | "/key",
) {
  const response = await fetch(`https://openrouter.ai/api/v1${path}`, {
    headers: buildUpstreamHeaders(
      c.req.raw.headers,
      c.get("upstreamOpenRouterKey") ?? c.env.OPENROUTER_API_KEY,
    ),
  });
  return new Response(response.body, response);
}
