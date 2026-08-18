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

describe("Integrations integration", () => {
  it("accepts an authorized OpenRouter key only on Hermes compatibility routes", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(Response.json({ data: [] })),
    );
    const hermesKey = "independent-hermes-openrouter-key";
    expect(
      (
        await request("/v1/hermes/models", {
          headers: { authorization: `Bearer ${hermesKey}` },
        })
      ).status,
    ).toBe(401);
    expect(
      (
        await request("/integrations/hermes/authorize", {
          method: "POST",
          headers: {
            authorization: "Bearer machine-token",
            "content-type": "application/json",
          },
          body: JSON.stringify({ token_hash: await tokenHash(hermesKey) }),
        })
      ).status,
    ).toBe(200);
    expect(
      (
        await request("/v1/hermes/models", {
          headers: { authorization: `Bearer ${hermesKey}` },
        })
      ).status,
    ).toBe(200);
    const upstreamHeaders = new Headers(
      vi.mocked(fetch).mock.calls[0][1]?.headers,
    );
    expect(upstreamHeaders.get("authorization")).toBe(`Bearer ${hermesKey}`);
    expect(
      (
        await request("/whoami", {
          headers: { authorization: `Bearer ${hermesKey}` },
        })
      ).status,
    ).toBe(401);
    expect(
      (
        await request("/v1/models", {
          headers: { authorization: `Bearer ${hermesKey}` },
        })
      ).status,
    ).toBe(401);
  });

  it("binds Hermes and harness loads to the authenticated installation", async () => {
    const now = "2026-08-13T10:00:00Z";
    await env.DB.prepare(
      "INSERT INTO machines(installation_id, name, platform, arch, created_at, updated_at) VALUES ('install-1', 'Device', 'linux', 'arm64', ?, ?)",
    )
      .bind(now, now)
      .run();
    await env.DB.prepare(
      "INSERT INTO access_tokens(token_hash, label, created_at, installation_id) VALUES (?, 'device', ?, 'install-1')",
    )
      .bind(await tokenHash("device-token"), now)
      .run();
    const headers = {
      authorization: "Bearer device-token",
      "content-type": "application/json",
    };

    const mismatch = await request("/integrations/harness-loads", {
      method: "POST",
      headers,
      body: JSON.stringify({
        version: 1,
        harness: "opencode",
        source_sha256: "a".repeat(64),
        installation_id: "install-2",
      }),
    });
    expect(mismatch.status).toBe(403);
    await request("/integrations/harness-loads", {
      method: "POST",
      headers,
      body: JSON.stringify({
        version: 1,
        harness: "opencode",
        source_sha256: "a".repeat(64),
      }),
    });
    expect(
      await env.DB.prepare("SELECT installation_id FROM harness_loads").first(),
    ).toEqual({ installation_id: "install-1" });

    const hermesKey = "device-hermes-key";
    await request("/integrations/hermes/authorize", {
      method: "POST",
      headers,
      body: JSON.stringify({ token_hash: await tokenHash(hermesKey) }),
    });
    expect(
      await env.DB.prepare(
        "SELECT installation_id FROM hermes_credentials",
      ).first(),
    ).toEqual({ installation_id: "install-1" });
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockImplementation(() =>
          Promise.resolve(
            Response.json({
              choices: [],
              usage: { prompt_tokens: 1, completion_tokens: 1 },
            }),
          ),
        ),
    );
    await request("/v1/hermes/chat/completions", {
      method: "POST",
      headers: {
        authorization: `Bearer ${hermesKey}`,
        "content-type": "application/json",
        "x-mimir-session": "hermes-device-session",
      },
      body: JSON.stringify({ model: "openai/test", messages: [] }),
    });
    expect(
      await env.DB.prepare(
        "SELECT installation_id FROM sessions WHERE id = 'hermes-device-session'",
      ).first(),
    ).toEqual({ installation_id: "install-1" });
  });

  it("scopes one Hermes key to multiple installations and isolates revocation", async () => {
    const now = "2026-08-13T10:00:00Z";
    const firstID = "11111111111111111111111111111111";
    const secondID = "22222222222222222222222222222222";
    const hermesKey = "shared-hermes-key";
    await env.DB.exec(`
        INSERT INTO machines(installation_id, name, platform, arch, created_at, updated_at) VALUES ('${firstID}', 'One', 'linux', 'amd64', '${now}', '${now}');
        INSERT INTO machines(installation_id, name, platform, arch, created_at, updated_at) VALUES ('${secondID}', 'Two', 'linux', 'amd64', '${now}', '${now}');
      `);
    await env.DB.prepare(
      "INSERT INTO access_tokens(token_hash, label, created_at, installation_id) VALUES (?, 'one', ?, ?)",
    )
      .bind(await tokenHash("one-device-token"), now, firstID)
      .run();
    await env.DB.prepare(
      "INSERT INTO access_tokens(token_hash, label, created_at, installation_id) VALUES (?, 'two', ?, ?)",
    )
      .bind(await tokenHash("two-device-token"), now, secondID)
      .run();
    for (const token of ["one-device-token", "two-device-token"]) {
      expect(
        (
          await request("/integrations/hermes/authorize", {
            method: "POST",
            headers: {
              authorization: `Bearer ${token}`,
              "content-type": "application/json",
            },
            body: JSON.stringify({ token_hash: await tokenHash(hermesKey) }),
          })
        ).status,
      ).toBe(200);
    }
    expect(
      await env.DB.prepare(
        "SELECT COUNT(*) AS count FROM hermes_credentials WHERE token_hash = ?",
      )
        .bind(await tokenHash(hermesKey))
        .first(),
    ).toEqual({ count: 2 });

    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(Response.json({ data: [] })),
    );
    const providerHeaders = { authorization: `Bearer ${hermesKey}` };
    expect(
      (
        await request(`/v1/hermes/${firstID}/models`, {
          headers: providerHeaders,
        })
      ).status,
    ).toBe(200);
    expect(
      (
        await request(`/v1/hermes/${secondID}/models`, {
          headers: providerHeaders,
        })
      ).status,
    ).toBe(200);
    expect(
      (await request("/v1/hermes/models", { headers: providerHeaders })).status,
    ).toBe(401);
    expect(
      (
        await request(`/v1/hermes/${"3".repeat(32)}/models`, {
          headers: providerHeaders,
        })
      ).status,
    ).toBe(401);

    await env.DB.prepare(
      "UPDATE machines SET revoked_at = ? WHERE installation_id = ?",
    )
      .bind(now, firstID)
      .run();
    expect(
      (
        await request(`/v1/hermes/${firstID}/models`, {
          headers: providerHeaders,
        })
      ).status,
    ).toBe(401);
    expect(
      (
        await request(`/v1/hermes/${secondID}/models`, {
          headers: providerHeaders,
        })
      ).status,
    ).toBe(200);
    expect(
      (await request("/v1/hermes/models", { headers: providerHeaders })).status,
    ).toBe(200);
  });

  it("restricts scoped Hermes routes to the machine token installation", async () => {
    const now = "2026-08-13T10:00:00Z";
    const firstID = "11111111111111111111111111111111";
    const secondID = "22222222222222222222222222222222";
    await env.DB.exec(`
        INSERT INTO machines(installation_id, name, platform, arch, created_at, updated_at) VALUES ('${firstID}', 'One', 'linux', 'amd64', '${now}', '${now}');
        INSERT INTO machines(installation_id, name, platform, arch, created_at, updated_at) VALUES ('${secondID}', 'Two', 'linux', 'amd64', '${now}', '${now}');
      `);
    await env.DB.prepare(
      "INSERT INTO access_tokens(token_hash, label, created_at, installation_id) VALUES (?, 'one', ?, ?)",
    )
      .bind(await tokenHash("one-device-token"), now, firstID)
      .run();
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(Response.json({ data: [] })),
    );

    const scopedHeaders = { authorization: "Bearer one-device-token" };
    expect(
      (
        await request(`/v1/hermes/${firstID}/models`, {
          headers: scopedHeaders,
        })
      ).status,
    ).toBe(200);
    expect(
      (
        await request(`/v1/hermes/${secondID}/models`, {
          headers: scopedHeaders,
        })
      ).status,
    ).toBe(403);
    expect(
      (
        await request(`/v1/hermes/${secondID}/models/extra`, {
          headers: scopedHeaders,
        })
      ).status,
    ).toBe(404);
    expect(
      (
        await request(`/v1/hermes/${"A".repeat(32)}/models`, {
          headers: scopedHeaders,
        })
      ).status,
    ).toBe(404);
    expect(
      (
        await request(`/v1/hermes/${secondID}/models`, {
          headers: { authorization: "Bearer machine-token" },
        })
      ).status,
    ).toBe(200);
    expect(vi.mocked(fetch)).toHaveBeenCalledTimes(2);
  });

  it("requires machine authentication for harness loads", async () => {
    expect((await request("/integrations/harness-loads")).status).toBe(401);
    expect(
      (
        await request("/integrations/harness-loads", {
          method: "POST",
          headers: { "content-type": "application/json" },
          body: JSON.stringify({
            version: 1,
            harness: "opencode",
            source_sha256: "a".repeat(64),
          }),
        })
      ).status,
    ).toBe(401);
  });

  it("isolates harness loads by machine token", async () => {
    await env.DB.prepare(
      "INSERT INTO access_tokens(token_hash, label, created_at) VALUES (?, 'other', '2026-01-01T00:00:00Z')",
    )
      .bind(await tokenHash("other-token"))
      .run();
    const post = (token: string, harness: string, buildID: string) =>
      request("/integrations/harness-loads", {
        method: "POST",
        headers: {
          authorization: `Bearer ${token}`,
          "content-type": "application/json",
        },
        body: JSON.stringify({ version: 1, harness, source_sha256: buildID }),
      });
    await post("machine-token", "opencode", "a".repeat(64));
    await post("other-token", "hermes", "b".repeat(64));

    const own = (await (
      await request("/integrations/harness-loads", {
        headers: { authorization: "Bearer machine-token" },
      })
    ).json()) as { loads: Array<{ harness: string; token_label: string }> };
    const other = (await (
      await request("/integrations/harness-loads", {
        headers: { authorization: "Bearer other-token" },
      })
    ).json()) as { loads: Array<{ harness: string; token_label: string }> };
    expect(own.loads).toEqual([
      expect.objectContaining({ harness: "opencode", token_label: "test" }),
    ]);
    expect(other.loads).toEqual([
      expect.objectContaining({ harness: "hermes", token_label: "other" }),
    ]);
  });

  it("accepts bounded first-party harness identifiers", async () => {
    for (const harness of ["claude-code", "codex", "cursor"]) {
      const response = await request("/integrations/harness-loads", {
        method: "POST",
        headers: {
          authorization: "Bearer machine-token",
          "content-type": "application/json",
        },
        body: JSON.stringify({
          version: 1,
          harness,
          source_sha256: "a".repeat(64),
        }),
      });
      expect(response.status).toBe(200);
    }
    const loads = await env.DB.prepare(
      "SELECT harness FROM harness_loads ORDER BY harness",
    ).all();
    expect(loads.results).toEqual([
      { harness: "claude-code" },
      { harness: "codex" },
      { harness: "cursor" },
    ]);
  });

  it.each([
    ["invalid JSON", "{"],
    ["a non-object body", "null"],
    [
      "a missing version",
      JSON.stringify({ harness: "opencode", source_sha256: "a".repeat(64) }),
    ],
    [
      "an unsupported version",
      JSON.stringify({
        version: 2,
        harness: "opencode",
        source_sha256: "a".repeat(64),
      }),
    ],
    [
      "an invalid harness",
      JSON.stringify({
        version: 1,
        harness: "Claude Code",
        source_sha256: "a".repeat(64),
      }),
    ],
    [
      "a missing source hash",
      JSON.stringify({ version: 1, harness: "opencode" }),
    ],
    [
      "an uppercase source hash",
      JSON.stringify({
        version: 1,
        harness: "opencode",
        source_sha256: "A".repeat(64),
      }),
    ],
    [
      "a short source hash",
      JSON.stringify({
        version: 1,
        harness: "opencode",
        source_sha256: "a".repeat(63),
      }),
    ],
    [
      "a non-string source hash",
      JSON.stringify({ version: 1, harness: "opencode", source_sha256: 123 }),
    ],
    [
      "an empty optional identity",
      JSON.stringify({
        version: 1,
        harness: "opencode",
        source_sha256: "a".repeat(64),
        installation_id: "",
      }),
    ],
    [
      "unknown fields",
      JSON.stringify({
        version: 1,
        harness: "opencode",
        source_sha256: "a".repeat(64),
        loaded_at: "2026-01-01T00:00:00Z",
      }),
    ],
  ])("rejects %s for harness loads", async (_case, body) => {
    const response = await request("/integrations/harness-loads", {
      method: "POST",
      headers: {
        authorization: "Bearer machine-token",
        "content-type": "application/json",
      },
      body,
    });
    expect(response.status).toBe(400);
    expect(
      await env.DB.prepare(
        "SELECT COUNT(*) AS count FROM harness_loads",
      ).first(),
    ).toEqual({ count: 0 });
  });

  it("identifies transparent Hermes capture from the compatibility path", async () => {
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockResolvedValue(
          Response.json({
            choices: [],
            usage: { prompt_tokens: 1, completion_tokens: 1 },
          }),
        ),
    );
    await request("/v1/hermes/chat/completions", {
      method: "POST",
      headers: {
        authorization: "Bearer machine-token",
        "content-type": "application/json",
      },
      body: JSON.stringify({
        model: "openai/test",
        messages: [{ role: "user", content: "Hermes route" }],
      }),
    });
    expect(
      await env.DB.prepare("SELECT harness FROM exchanges LIMIT 1").first(),
    ).toEqual({ harness: "hermes" });
  });
});
