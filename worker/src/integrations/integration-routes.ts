import type { Hono } from "hono";
import type { AppEnv } from "../env";

const HARNESS_LOAD_KEYS = [
  "version",
  "harness",
  "source_sha256",
  "bundle_version",
  "cli_version",
  "cli_commit",
  "installation_id",
] as const;

export function registerIntegrationRoutes(app: Hono<AppEnv>) {
  app.post("/integrations/hermes/authorize", async (c) => {
    const body = await c.req.json<{ token_hash?: unknown }>();
    const tokenHash =
      typeof body.token_hash === "string"
        ? body.token_hash.trim().toLowerCase()
        : "";
    if (!/^[a-f0-9]{64}$/.test(tokenHash))
      return c.json({ error: "token_hash must be a SHA-256 hex digest" }, 400);
    await c.env.DB.prepare(
      "INSERT INTO hermes_credentials(token_hash, created_at, authorized_by, installation_id) VALUES (?, ?, ?, ?) ON CONFLICT(token_hash, installation_id) DO UPDATE SET authorized_by = excluded.authorized_by",
    )
      .bind(
        tokenHash,
        new Date().toISOString(),
        c.get("tokenLabel"),
        c.get("installationID") ?? "",
      )
      .run();
    return c.json({ authorized: true });
  });

  app.post("/integrations/harness-loads", async (c) => {
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
        (key) => !(HARNESS_LOAD_KEYS as readonly string[]).includes(key),
      )
    )
      return c.json({ error: "body contains unknown fields" }, 400);
    if (values.version !== 1)
      return c.json({ error: "version must be 1" }, 400);
    if (
      typeof values.harness !== "string" ||
      !/^[a-z0-9][a-z0-9._-]{0,63}$/.test(values.harness)
    )
      return c.json({ error: "harness must be a lowercase identifier" }, 400);
    if (
      typeof values.source_sha256 !== "string" ||
      !/^[a-f0-9]{64}$/.test(values.source_sha256)
    )
      return c.json(
        { error: "source_sha256 must be a lowercase SHA-256 hex digest" },
        400,
      );
    for (const key of [
      "bundle_version",
      "cli_version",
      "cli_commit",
      "installation_id",
    ] as const) {
      const value = values[key];
      if (
        value !== undefined &&
        (typeof value !== "string" || value.length === 0 || value.length > 200)
      )
        return c.json(
          {
            error: `${key} must be a non-empty string of at most 200 characters`,
          },
          400,
        );
    }
    const reportedAt = new Date().toISOString();
    const authenticatedInstallationID = c.get("installationID");
    if (
      authenticatedInstallationID &&
      values.installation_id !== undefined &&
      values.installation_id !== authenticatedInstallationID
    )
      return c.json(
        { error: "installation_id must match the authenticated installation" },
        403,
      );
    const installationID =
      authenticatedInstallationID ?? values.installation_id ?? "";
    await c.env.DB.prepare(
      `INSERT INTO harness_loads(token_hash, token_label, harness, artifact_sha256, bundle_version, cli_version, cli_commit, installation_id, client_loaded_at, reported_at)
      VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
      ON CONFLICT(token_hash, harness, installation_id) DO UPDATE SET
        token_label = excluded.token_label,
        artifact_sha256 = excluded.artifact_sha256,
        bundle_version = excluded.bundle_version,
        cli_version = excluded.cli_version,
        cli_commit = excluded.cli_commit,
        client_loaded_at = CASE WHEN harness_loads.artifact_sha256 = excluded.artifact_sha256 THEN harness_loads.client_loaded_at ELSE excluded.client_loaded_at END,
        reported_at = excluded.reported_at`,
    )
      .bind(
        c.get("tokenHash"),
        c.get("tokenLabel"),
        values.harness,
        values.source_sha256,
        values.bundle_version ?? null,
        values.cli_version ?? null,
        values.cli_commit ?? null,
        installationID,
        reportedAt,
        reportedAt,
      )
      .run();
    const load = await c.env.DB.prepare(
      "SELECT harness, artifact_sha256, bundle_version, cli_version, cli_commit, installation_id, client_loaded_at, reported_at, token_label FROM harness_loads WHERE token_hash = ? AND harness = ? AND installation_id = ?",
    )
      .bind(c.get("tokenHash"), values.harness, installationID)
      .first();
    return c.json({ load });
  });

  app.get("/integrations/harness-loads", async (c) => {
    const installationID = c.get("installationID");
    const result = installationID
      ? await c.env.DB.prepare(
          "SELECT harness, artifact_sha256, bundle_version, cli_version, cli_commit, installation_id, client_loaded_at, reported_at, token_label FROM harness_loads WHERE installation_id = ? ORDER BY reported_at DESC, harness",
        )
          .bind(installationID)
          .all()
      : await c.env.DB.prepare(
          "SELECT harness, artifact_sha256, bundle_version, cli_version, cli_commit, installation_id, client_loaded_at, reported_at, token_label FROM harness_loads WHERE token_hash = ? ORDER BY reported_at DESC, harness",
        )
          .bind(c.get("tokenHash"))
          .all();
    return c.json({ loads: result.results });
  });
}
