import { exportJWK, generateKeyPair, SignJWT } from "jose";
import { describe, expect, it, vi } from "vitest";
import {
  addMachineToken,
  createExecutionContext,
  dashboardRequest,
  env,
  finalizeAcceptedExchange,
  request,
  tokenHash,
  waitOnExecutionContext,
  worker,
} from "./support";

describe("System integration", () => {
  it("rejects unauthenticated requests", async () => {
    const response = await request("/whoami");
    expect(response.status).toBe(401);
  });

  it("reports the machine API version and capabilities", async () => {
    const response = await request("/whoami", {
      headers: { authorization: "Bearer machine-token" },
    });
    expect(response.status).toBe(200);
    await expect(response.json()).resolves.toMatchObject({
      service: "mimir",
      api_version: 1,
      capabilities: expect.arrayContaining([
        "harness_build_identity",
        "hermes_authorization",
        "machine_identity_association",
        "session_events",
        "session_lifecycle",
      ]),
    });
  });

  it("associates an unclaimed token once and updates machine metadata without replacing its name", async () => {
    const headers = {
      authorization: "Bearer machine-token",
      "content-type": "application/json",
    };
    const installationID = "a".repeat(32);
    const body = {
      version: 1,
      installation_id: installationID,
      name: "Original",
      platform: "linux",
      arch: "amd64",
    };

    expect(
      (
        await request("/machine/associate", {
          method: "POST",
          headers,
          body: JSON.stringify(body),
        })
      ).status,
    ).toBe(200);
    expect(
      (
        await request("/machine/associate", {
          method: "POST",
          headers,
          body: JSON.stringify({
            ...body,
            name: "Replacement",
            platform: "darwin",
            arch: "arm64",
          }),
        })
      ).status,
    ).toBe(200);
    expect(
      await env.DB.prepare(
        "SELECT name, platform, arch FROM machines WHERE installation_id = ?",
      )
        .bind(installationID)
        .first(),
    ).toEqual({ name: "Original", platform: "darwin", arch: "arm64" });
    expect(
      await env.DB.prepare(
        "SELECT installation_id FROM access_tokens WHERE token_hash = ?",
      )
        .bind(await tokenHash("machine-token"))
        .first(),
    ).toEqual({ installation_id: installationID });

    const conflictID = "b".repeat(32);
    expect(
      (
        await request("/machine/associate", {
          method: "POST",
          headers,
          body: JSON.stringify({ ...body, installation_id: conflictID }),
        })
      ).status,
    ).toBe(409);
    expect(
      await env.DB.prepare(
        "SELECT installation_id FROM access_tokens WHERE token_hash = ?",
      )
        .bind(await tokenHash("machine-token"))
        .first(),
    ).toEqual({ installation_id: installationID });
    expect(
      await env.DB.prepare(
        "SELECT installation_id FROM machines WHERE installation_id = ?",
      )
        .bind(conflictID)
        .first(),
    ).toBeNull();
  });

  it("leaves an unclaimed token unassociated when the target machine is revoked", async () => {
    const installationID = "c".repeat(32);
    const revokedAt = "2026-08-13T01:00:00Z";
    await env.DB.prepare(
      "INSERT INTO machines(installation_id, name, platform, arch, created_at, updated_at, revoked_at) VALUES (?, 'Revoked', 'linux', 'amd64', ?, ?, ?)",
    )
      .bind(installationID, revokedAt, revokedAt, revokedAt)
      .run();

    const response = await request("/machine/associate", {
      method: "POST",
      headers: {
        authorization: "Bearer machine-token",
        "content-type": "application/json",
      },
      body: JSON.stringify({
        version: 1,
        installation_id: installationID,
        name: "Replacement",
        platform: "darwin",
        arch: "arm64",
      }),
    });

    expect(response.status).toBe(409);
    expect(
      await env.DB.prepare(
        "SELECT installation_id FROM access_tokens WHERE token_hash = ?",
      )
        .bind(await tokenHash("machine-token"))
        .first(),
    ).toEqual({ installation_id: null });
    expect(
      await env.DB.prepare(
        "SELECT name, platform, arch, revoked_at FROM machines WHERE installation_id = ?",
      )
        .bind(installationID)
        .first(),
    ).toEqual({
      name: "Revoked",
      platform: "linux",
      arch: "amd64",
      revoked_at: revokedAt,
    });
  });

  it.each([
    [
      "extra fields",
      {
        version: 1,
        installation_id: "a".repeat(32),
        name: "Machine",
        platform: "linux",
        arch: "amd64",
        token_hash: "secret",
      },
    ],
    [
      "invalid installation",
      {
        version: 1,
        installation_id: "A".repeat(32),
        name: "Machine",
        platform: "linux",
        arch: "amd64",
      },
    ],
    [
      "control characters",
      {
        version: 1,
        installation_id: "a".repeat(32),
        name: "Bad\nName",
        platform: "linux",
        arch: "amd64",
      },
    ],
    [
      "long name",
      {
        version: 1,
        installation_id: "a".repeat(32),
        name: "x".repeat(201),
        platform: "linux",
        arch: "amd64",
      },
    ],
    [
      "invalid platform",
      {
        version: 1,
        installation_id: "a".repeat(32),
        name: "Machine",
        platform: "Windows",
        arch: "amd64",
      },
    ],
  ])("rejects machine association with %s", async (_case, body) => {
    const response = await request("/machine/associate", {
      method: "POST",
      headers: {
        authorization: "Bearer machine-token",
        "content-type": "application/json",
      },
      body: JSON.stringify(body),
    });
    expect(response.status).toBe(400);
  });
});
