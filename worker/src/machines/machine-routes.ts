import type { Hono } from "hono";
import type { AppEnv } from "../env";

const MACHINE_API_VERSION = 1;
const MACHINE_CAPABILITIES = [
  "canonical_exchanges",
  "harness_build_identity",
  "hermes_authorization",
  "machine_identity_association",
  "session_events",
  "session_lifecycle",
  "session_outcomes",
  "session_search",
  "session_titles",
] as const;
const MACHINE_ASSOCIATION_KEYS = [
  "version",
  "installation_id",
  "name",
  "platform",
  "arch",
] as const;

export function registerMachineRoutes(app: Hono<AppEnv>) {
  app.get("/whoami", async (c) => {
    const [sessions, exchanges] = await Promise.all([
      c.env.DB.prepare(
        "SELECT COUNT(*) AS count FROM sessions WHERE parent_session_id IS NULL",
      ).first<{ count: number }>(),
      c.env.DB.prepare(
        "SELECT COUNT(*) AS count FROM exchanges WHERE capture_status = 'saved'",
      ).first<{ count: number }>(),
    ]);
    return c.json({
      service: "mimir",
      api_version: MACHINE_API_VERSION,
      capabilities: MACHINE_CAPABILITIES,
      url: new URL(c.req.url).origin,
      bundle_version: c.env.MIMIR_BUNDLE_VERSION ?? null,
      bundle_sha256: c.env.MIMIR_BUNDLE_SHA256 ?? null,
      sessions: sessions?.count ?? 0,
      log: exchanges?.count ?? 0,
    });
  });

  app.post("/machine/associate", async (c) => {
    let body: unknown;
    try {
      body = await c.req.json();
    } catch {
      return c.json({ error: "invalid JSON body" }, 400);
    }
    if (!body || typeof body !== "object" || Array.isArray(body))
      return c.json({ error: "body must be an object" }, 400);
    const values = body as Record<string, unknown>;
    if (
      Object.keys(values).some(
        (key) => !(MACHINE_ASSOCIATION_KEYS as readonly string[]).includes(key),
      ) ||
      Object.keys(values).length !== MACHINE_ASSOCIATION_KEYS.length
    ) {
      return c.json(
        {
          error:
            "body must contain exactly version, installation_id, name, platform, and arch",
        },
        400,
      );
    }
    if (values.version !== 1)
      return c.json({ error: "version must be 1" }, 400);
    if (
      typeof values.installation_id !== "string" ||
      !/^[a-f0-9]{32}$/.test(values.installation_id)
    ) {
      return c.json(
        {
          error: "installation_id must be 32 lowercase hexadecimal characters",
        },
        400,
      );
    }
    if (
      typeof values.name !== "string" ||
      values.name.length === 0 ||
      values.name.length > 200 ||
      /[\p{Cc}]/u.test(values.name)
    ) {
      return c.json(
        {
          error:
            "name must be a non-empty string of at most 200 characters without control characters",
        },
        400,
      );
    }
    for (const key of ["platform", "arch"] as const) {
      if (
        typeof values[key] !== "string" ||
        !/^[a-z0-9][a-z0-9._-]{0,63}$/.test(values[key])
      ) {
        return c.json({ error: `${key} must be a lowercase identifier` }, 400);
      }
    }

    const now = new Date().toISOString();
    await c.env.DB.batch([
      c.env.DB.prepare(
        `INSERT INTO machines(installation_id, name, platform, arch, created_at, updated_at)
       SELECT ?, ?, ?, ?, ?, ?
       WHERE EXISTS (SELECT 1 FROM access_tokens WHERE token_hash = ? AND (installation_id IS NULL OR installation_id = ?))
        ON CONFLICT(installation_id) DO UPDATE SET platform = excluded.platform, arch = excluded.arch, updated_at = excluded.updated_at
        WHERE machines.revoked_at IS NULL`,
      ).bind(
        values.installation_id,
        values.name,
        values.platform,
        values.arch,
        now,
        now,
        c.get("tokenHash"),
        values.installation_id,
      ),
      c.env.DB.prepare(
        `UPDATE access_tokens SET installation_id = ?
        WHERE token_hash = ? AND (installation_id IS NULL OR installation_id = ?)
          AND EXISTS (SELECT 1 FROM machines WHERE installation_id = ? AND revoked_at IS NULL)`,
      ).bind(
        values.installation_id,
        c.get("tokenHash"),
        values.installation_id,
        values.installation_id,
      ),
    ]);
    const token = await c.env.DB.prepare(
      "SELECT installation_id FROM access_tokens WHERE token_hash = ?",
    )
      .bind(c.get("tokenHash"))
      .first<{ installation_id: string | null }>();
    if (token?.installation_id !== values.installation_id)
      return c.json(
        { error: "token is already associated with another installation" },
        409,
      );
    return c.json({
      associated: true,
      installation_id: values.installation_id,
    });
  });
}
