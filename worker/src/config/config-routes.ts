import type { Hono } from "hono";
import { readConfig, validateConfigValues } from "./config-store";
import type { AppEnv } from "../env";

export function registerConfigRoutes(app: Hono<AppEnv>) {
  app.get("/config", async (c) => c.json(await readConfig(c.env.DB)));

  app.put("/config", async (c) => {
    const values = await c.req.json<Record<string, unknown>>();
    const validation = validateConfigValues(values);
    if (validation) return c.json({ error: validation }, 400);
    const statements = Object.entries(values).map(([key, value]) =>
      c.env.DB.prepare(
        "INSERT INTO config(key, value) VALUES(?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value",
      ).bind(key, JSON.stringify(value)),
    );
    if (statements.length) await c.env.DB.batch(statements);
    return c.json(await readConfig(c.env.DB));
  });
}
