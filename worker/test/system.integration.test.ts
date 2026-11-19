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
      capabilities: [
        "canonical_exchanges",
        "harness_build_identity",
        "hermes_authorization",
        "machine_identity_association",
        "mcp",
        "query_api",
        "session_events",
        "session_git_artifacts",
        "session_lifecycle",
        "session_titles",
      ],
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

  it("records the calling device's tailnet identity and peer reachability", async () => {
    await addMachineToken("a".repeat(32), "tailnet-a");
    await addMachineToken("b".repeat(32), "tailnet-b");
    const report = (token: string, body: unknown) =>
      request("/machine/tailnet", {
        method: "POST",
        headers: { authorization: `Bearer ${token}`, "content-type": "application/json" },
        body: JSON.stringify(body),
      });
    expect(
      await (
        await report("tailnet-b", {
          self: { host_name: "laptop", dns_name: "Laptop.tail1234.ts.net.", tailnet_name: "example.com", ips: ["100.64.0.2"] },
          peers: [],
        })
      ).json(),
    ).toEqual({ ok: true });
    const desktop = await report("tailnet-a", {
      self: { host_name: "desktop", dns_name: "desktop.tail1234.ts.net.", tailnet_name: "example.com", ips: ["100.64.0.1", "fd7a::1"] },
      peers: [
        { dns_name: "laptop.tail1234.ts.net.", host_name: "laptop", online: false, last_seen: "2026-11-18T00:00:00Z" },
        { dns_name: "phone.tail1234.ts.net.", host_name: "phone", online: true, last_seen: null },
      ],
    });
    expect(desktop.status).toBe(200);
    const devices = (await (await request("/api/devices", { headers: { authorization: "Bearer machine-token" } })).json()) as {
      devices: Array<{ id: string; tailnet: Record<string, unknown> | null }>;
    };
    const byID = new Map(devices.devices.map((device) => [device.id, device.tailnet]));
    expect(byID.get("a".repeat(32))).toEqual({
      host_name: "desktop",
      dns_name: "desktop.tail1234.ts.net",
      tailnet_name: "example.com",
      ips: ["100.64.0.1", "fd7a::1"],
      online: true,
      observed_at: expect.any(String),
    });
    expect(byID.get("b".repeat(32))).toMatchObject({ host_name: "laptop", dns_name: "laptop.tail1234.ts.net", online: false });
    expect((await report("machine-token", { self: {}, peers: [] })).status).toBe(409);
    expect((await report("tailnet-a", { self: { ips: "100.64.0.1" } })).status).toBe(400);
    expect((await report("tailnet-a", { self: {}, peers: [{ dns_name: "x", online: "yes" }] })).status).toBe(400);
  });

  it("requires Cloudflare Access for dashboard routes and serves the local identity", async () => {
    expect((await request("/dashboard/api/devices")).status).toBe(403);
    expect((await request("/dashboard/api/devices", { headers: { authorization: "Bearer machine-token" } })).status).toBe(403);
    expect((await request("/dashboard/auth?returnTo=%2Fdashboard%2Fsessions")).status).toBe(403);
    expect((await request("/api/devices")).status).toBe(401);
    expect(await (await dashboardRequest("/dashboard/api/identity")).json()).toEqual({
      email: null,
      name: "Local development",
      source: "local-development",
    });
    const handoff = await dashboardRequest("/dashboard/auth?returnTo=%2Fdashboard%2Fsessions%2Fsession-1");
    expect(handoff.status).toBe(302);
    expect(handoff.headers.get("location")).toBe("/dashboard/sessions/session-1");
  });

  it("verifies Cloudflare Access JWTs for dashboard APIs", async () => {
    const teamDomain = "https://team.cloudflareaccess.com";
    const { publicKey, privateKey } = await generateKeyPair("RS256");
    const jwk = await exportJWK(publicKey);
    jwk.kid = "test-key";
    jwk.alg = "RS256";
    vi.stubGlobal(
      "fetch",
      vi.fn().mockImplementation((input: RequestInfo | URL) => {
        const url = String(input);
        if (url === `${teamDomain}/cdn-cgi/access/certs`) return Promise.resolve(Response.json({ keys: [jwk] }));
        return Promise.reject(new Error(`unexpected fetch ${url}`));
      }),
    );
    const bindings = env as Env & { DASHBOARD_ACCESS_AUD?: string; DASHBOARD_ACCESS_TEAM_DOMAIN?: string };
    bindings.DASHBOARD_ACCESS_AUD = "test-aud";
    bindings.DASHBOARD_ACCESS_TEAM_DOMAIN = teamDomain;
    const sign = (init: { audience?: string; issuer?: string; expiration?: number } = {}) =>
      new SignJWT({ email: " dashboard@example.com ", name: "Dashboard User", unsafe: "not exposed" })
        .setProtectedHeader({ alg: "RS256", kid: "test-key" })
        .setIssuer(init.issuer ?? teamDomain)
        .setAudience(init.audience ?? "test-aud")
        .setIssuedAt()
        .setExpirationTime(init.expiration ?? Math.floor(Date.now() / 1000) + 300)
        .sign(privateKey);
    try {
      const assertion = (token: string) => ({ headers: { "cf-access-jwt-assertion": token } });
      expect((await request("/dashboard/api/devices", assertion(await sign()))).status).toBe(200);
      expect(await (await request("/dashboard/api/identity", assertion(await sign()))).json()).toEqual({
        email: "dashboard@example.com",
        name: "Dashboard User",
        source: "cloudflare-access",
      });
      for (const token of [
        await sign({ audience: "other-aud" }),
        await sign({ issuer: "https://evil.example.com" }),
        await sign({ expiration: Math.floor(Date.now() / 1000) - 300 }),
        "not-a-jwt",
      ])
        expect((await request("/dashboard/api/devices", assertion(token))).status).toBe(403);
      await env.DB.prepare("INSERT INTO sessions(id, started_at, boundary) VALUES ('access-receipt', '2026-01-01T00:00:00Z', 'header')").run();
      const receipt = await request("/api/sessions/access-receipt/receipt", { headers: { authorization: "Bearer machine-token" } });
      expect(((await receipt.json()) as { dashboard_url: string }).dashboard_url).toBe("https://mimir.test/dashboard/sessions/access-receipt");
    } finally {
      delete bindings.DASHBOARD_ACCESS_AUD;
      delete bindings.DASHBOARD_ACCESS_TEAM_DOMAIN;
    }
  });
});
