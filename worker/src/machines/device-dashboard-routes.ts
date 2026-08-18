import type { Hono } from "hono";
import type { AppEnv } from "../env";

async function deviceRows(db: D1Database, installationID?: string) {
  const statement = db.prepare(`
    WITH root_sessions AS (
      SELECT installation_id, COUNT(*) AS session_count
      FROM sessions
      WHERE installation_id IS NOT NULL AND parent_session_id IS NULL
      GROUP BY installation_id
    ), device_harnesses AS (
      SELECT installation_id, json_group_array(harness) AS harnesses
      FROM (
        SELECT DISTINCT installation_id, harness
        FROM sessions
        WHERE installation_id IS NOT NULL AND harness IS NOT NULL AND harness <> ''
        ORDER BY installation_id, harness
      )
      GROUP BY installation_id
    )
    SELECT machines.installation_id AS id, machines.name, machines.platform, machines.arch,
      machines.created_at, machines.updated_at, machines.last_seen_at, machines.revoked_at,
      COALESCE(root_sessions.session_count, 0) AS session_count,
      COALESCE(device_harnesses.harnesses, '[]') AS harnesses
    FROM machines
    LEFT JOIN root_sessions ON root_sessions.installation_id = machines.installation_id
    LEFT JOIN device_harnesses ON device_harnesses.installation_id = machines.installation_id
    ${installationID ? "WHERE machines.installation_id = ?" : ""}
    ORDER BY COALESCE(machines.last_seen_at, machines.created_at) DESC, machines.installation_id
  `);
  const rows = installationID
    ? await statement.bind(installationID).all<Record<string, unknown>>()
    : await statement.all<Record<string, unknown>>();
  return rows.results.map((row) => ({
    ...row,
    harnesses: JSON.parse(String(row.harnesses)) as string[],
  }));
}

export function registerDashboardDeviceRoutes(app: Hono<AppEnv>) {
  app.get("/dashboard/api/devices", async (c) =>
    c.json({ devices: await deviceRows(c.env.DB) }),
  );

  app.patch("/dashboard/api/devices/:id", async (c) => {
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
      Object.keys(body).some((key) => key !== "name")
    )
      return c.json({ error: "body must contain only name" }, 400);
    const value = (body as Record<string, unknown>).name;
    const name = typeof value === "string" ? value.trim() : "";
    if (!name || name.length > 200)
      return c.json(
        { error: "name must be a non-empty string of at most 200 characters" },
        400,
      );
    const updated = await c.env.DB.prepare(
      "UPDATE machines SET name = ?, updated_at = ? WHERE installation_id = ?",
    )
      .bind(name, new Date().toISOString(), c.req.param("id"))
      .run();
    if (updated.meta.changes === 0)
      return c.json({ error: "device not found" }, 404);
    const [device] = await deviceRows(c.env.DB, c.req.param("id"));
    return c.json({ device });
  });

  app.post("/dashboard/api/devices/:id/revoke", async (c) => {
    const now = new Date().toISOString();
    const exists = await c.env.DB.prepare(
      "SELECT 1 FROM machines WHERE installation_id = ?",
    )
      .bind(c.req.param("id"))
      .first();
    if (!exists) return c.json({ error: "device not found" }, 404);
    await c.env.DB.batch([
      c.env.DB.prepare(
        "UPDATE machines SET revoked_at = COALESCE(revoked_at, ?), updated_at = ? WHERE installation_id = ?",
      ).bind(now, now, c.req.param("id")),
      c.env.DB.prepare(
        "UPDATE access_tokens SET revoked_at = COALESCE(revoked_at, ?) WHERE installation_id = ?",
      ).bind(now, c.req.param("id")),
    ]);
    const [device] = await deviceRows(c.env.DB, c.req.param("id"));
    return c.json({ device });
  });
}
