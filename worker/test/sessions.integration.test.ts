import { exportJWK, generateKeyPair, SignJWT } from "jose";
import { describe, expect, it, vi } from "vitest";
import { resolveSession } from "../src/sessions/lifecycle";
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

describe("Sessions integration", () => {
  it("enforces both parent endpoint owners and rejects missing, conflicting, and cyclic parents atomically", async () => {
    await addMachineToken("parent-owner", "parent-owner-token");
    await addMachineToken("other-owner", "other-owner-token");
    await env.DB.exec(`
      INSERT INTO sessions(id, installation_id, started_at, state, boundary) VALUES ('link-a', 'parent-owner', '2026-08-13T10:00:00Z', 'inactive', 'header');
      INSERT INTO sessions(id, installation_id, started_at, state, boundary) VALUES ('link-b', 'parent-owner', '2026-08-13T10:00:00Z', 'inactive', 'header');
      INSERT INTO sessions(id, installation_id, started_at, state, boundary) VALUES ('link-c', 'parent-owner', '2026-08-13T10:00:00Z', 'inactive', 'header');
      INSERT INTO sessions(id, installation_id, started_at, state, boundary) VALUES ('link-foreign', 'other-owner', '2026-08-13T10:00:00Z', 'inactive', 'header');
    `);
    const link = (child: string, parent: string, token = "parent-owner-token") =>
      request(`/sessions/${child}/parent`, {
        method: "PATCH",
        headers: { authorization: `Bearer ${token}`, "content-type": "application/json" },
        body: JSON.stringify({ parent_session_id: parent }),
      });
    expect((await link("link-a", "missing-parent")).status).toBe(404);
    expect((await link("missing-child", "link-a")).status).toBe(404);
    expect((await link("link-a", "link-foreign")).status).toBe(403);
    expect((await link("link-foreign", "link-a")).status).toBe(403);
    expect((await link("link-a", "link-a")).status).toBe(400);
    const competing = await Promise.all([link("link-a", "link-b"), link("link-b", "link-a")]);
    expect(competing.map((response) => response.status).sort()).toEqual([200, 409]);
    const linked = await env.DB.prepare(
      "SELECT id, parent_session_id FROM sessions WHERE id IN ('link-a', 'link-b') AND parent_session_id IS NOT NULL",
    ).first<{ id: string; parent_session_id: string }>();
    expect(linked).not.toBeNull();
    expect((await link(linked!.id, "link-c")).status).toBe(409);
    expect((await link("link-c", linked!.id)).status).toBe(200);
    expect((await link(linked!.parent_session_id, "link-c")).status).toBe(409);
    await expect(env.DB.prepare(
      "UPDATE sessions SET parent_session_id = ? WHERE id = ?",
    ).bind("link-c", linked!.parent_session_id).run()).rejects.toThrow();
    await expect(env.DB.prepare(
      "UPDATE sessions SET parent_session_id = NULL WHERE id = ?",
    ).bind(linked!.id).run()).rejects.toThrow();
    expect(await env.DB.prepare(
      "SELECT parent_session_id FROM sessions WHERE id = 'link-foreign'",
    ).first()).toEqual({ parent_session_id: null });
  });

  it("adopts a freshly registered exact Hermes session before heuristic fallback", async () => {
    await env.DB.exec(`
      INSERT INTO sessions(id, started_at, last_active_at, state, harness, boundary, repo) VALUES ('hermes-exact', '2026-09-04T10:00:00Z', '2026-09-04T10:00:00Z', 'active', 'hermes', 'header', 'mimir');
      INSERT INTO sessions(id, started_at, last_active_at, state, harness, boundary, repo) VALUES ('hermes-heuristic', '2026-09-04T09:59:00Z', '2026-09-04T09:59:50Z', 'active', 'hermes', 'heuristic', 'mimir');
    `);

    await expect(
      resolveSession(
        env.DB,
        null,
        "mimir",
        "hermes",
        null,
        "openai/test",
        "2026-09-04T10:00:05Z",
      ),
    ).resolves.toEqual({ id: "hermes-exact" });
    await expect(
      resolveSession(
        env.DB,
        null,
        "mimir",
        "hermes",
        null,
        "openai/test",
        "2026-09-04T10:00:31Z",
      ),
    ).resolves.toEqual({ id: "hermes-heuristic" });
  });

  it("keeps heuristic sessions separate by installation while preserving null identity", async () => {
    const now = "2026-08-13T10:00:00Z";
    await env.DB.exec(`
        INSERT INTO machines(installation_id, name, platform, arch, created_at, updated_at) VALUES ('install-1', 'One', 'linux', 'amd64', '${now}', '${now}');
        INSERT INTO machines(installation_id, name, platform, arch, created_at, updated_at) VALUES ('install-2', 'Two', 'linux', 'amd64', '${now}', '${now}');
      `);
    await env.DB.prepare(
      "INSERT INTO access_tokens(token_hash, label, created_at, installation_id) VALUES (?, 'one', ?, 'install-1')",
    )
      .bind(await tokenHash("one-token"), now)
      .run();
    await env.DB.prepare(
      "INSERT INTO access_tokens(token_hash, label, created_at, installation_id) VALUES (?, 'two', ?, 'install-2')",
    )
      .bind(await tokenHash("two-token"), now)
      .run();
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
    const send = (token: string) =>
      request("/v1/chat/completions", {
        method: "POST",
        headers: {
          authorization: `Bearer ${token}`,
          "content-type": "application/json",
          "x-mimir-repo": "repo",
          "x-mimir-harness": "agent",
        },
        body: JSON.stringify({ model: "openai/test", messages: [] }),
      });
    await send("one-token");
    await send("two-token");
    await send("machine-token");
    expect(
      await env.DB.prepare(
        "SELECT COUNT(*) AS count FROM sessions WHERE boundary = 'heuristic'",
      ).first(),
    ).toEqual({ count: 3 });
  });

  it("records and lists the authenticated machine token's loaded harness builds", async () => {
    const firstBuild = "a".repeat(64);
    const replacementBuild = "b".repeat(64);
    const headers = {
      authorization: "Bearer machine-token",
      "content-type": "application/json",
    };
    const firstPayload = {
      version: 1,
      harness: "opencode",
      source_sha256: firstBuild,
      bundle_version: "v1",
      cli_version: "1.2.3",
      cli_commit: "abc123",
      installation_id: "install-1",
    };
    const first = await request("/integrations/harness-loads", {
      method: "POST",
      headers,
      body: JSON.stringify(firstPayload),
    });
    expect(first.status).toBe(200);
    const firstBody = (await first.json()) as {
      load: { client_loaded_at: string; reported_at: string };
    };
    expect(firstBody).toMatchObject({
      load: {
        harness: "opencode",
        artifact_sha256: firstBuild,
        bundle_version: "v1",
        cli_version: "1.2.3",
        cli_commit: "abc123",
        installation_id: "install-1",
        token_label: "test",
        client_loaded_at: expect.any(String),
        reported_at: expect.any(String),
      },
    });

    const repeated = await request("/integrations/harness-loads", {
      method: "POST",
      headers,
      body: JSON.stringify(firstPayload),
    });
    expect(repeated.status).toBe(200);
    expect(
      await env.DB.prepare(
        "SELECT COUNT(*) AS count FROM harness_loads",
      ).first(),
    ).toEqual({ count: 1 });
    const repeatedLoad = (
      (await repeated.json()) as {
        load: { client_loaded_at: string; reported_at: string };
      }
    ).load;
    expect(repeatedLoad.client_loaded_at).toBe(firstBody.load.client_loaded_at);
    expect(repeatedLoad.reported_at >= firstBody.load.reported_at).toBe(true);

    await request("/integrations/harness-loads", {
      method: "POST",
      headers,
      body: JSON.stringify({
        version: 1,
        harness: "opencode",
        source_sha256: replacementBuild,
        installation_id: "install-1",
      }),
    });
    await request("/integrations/harness-loads", {
      method: "POST",
      headers,
      body: JSON.stringify({
        version: 1,
        harness: "hermes",
        source_sha256: firstBuild,
      }),
    });
    const listed = await request("/integrations/harness-loads", {
      headers: { authorization: "Bearer machine-token" },
    });
    expect(listed.status).toBe(200);
    expect(await listed.json()).toEqual({
      loads: expect.arrayContaining([
        expect.objectContaining({
          harness: "hermes",
          artifact_sha256: firstBuild,
          installation_id: "",
          client_loaded_at: expect.any(String),
          reported_at: expect.any(String),
          token_label: "test",
        }),
        expect.objectContaining({
          harness: "opencode",
          artifact_sha256: replacementBuild,
          installation_id: "install-1",
          client_loaded_at: expect.any(String),
          reported_at: expect.any(String),
          token_label: "test",
        }),
      ]),
    });
  });

  it.each([
    ["/v1/models", "https://openrouter.ai/api/v1/models"],
    ["/v1/credits", "https://openrouter.ai/api/v1/credits"],
    ["/v1/key", "https://openrouter.ai/api/v1/key"],
    ["/v1/hermes/models", "https://openrouter.ai/api/v1/models"],
    ["/v1/hermes/credits", "https://openrouter.ai/api/v1/credits"],
    ["/v1/hermes/key", "https://openrouter.ai/api/v1/key"],
  ])("proxies OpenRouter compatibility route %s", async (path, upstreamURL) => {
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockResolvedValue(
          new Response('{"data":{}}', {
            status: 206,
            headers: { "x-upstream": "openrouter" },
          }),
        ),
    );
    const response = await request(path, {
      headers: {
        authorization: "Bearer machine-token",
        "x-mimir-harness": "must-not-leak",
      },
    });
    expect(response.status).toBe(206);
    expect(response.headers.get("x-upstream")).toBe("openrouter");
    expect(await response.text()).toBe('{"data":{}}');
    const [url, init] = vi.mocked(fetch).mock.calls[0];
    expect(url).toBe(upstreamURL);
    const headers = new Headers((init as RequestInit).headers);
    expect(headers.get("authorization")).toBe("Bearer test-openrouter-key");
    expect(headers.get("x-mimir-harness")).toBeNull();
  });

  it("reconciliation selects the earliest saved primary intent", async () => {
    await env.DB.prepare(
      "INSERT INTO sessions(id, started_at, ended_at, state, last_active_at, boundary) VALUES ('ordered-intent', '2026-01-01T00:00:00Z', '2026-01-01T00:01:00Z', 'active', '2026-01-01T00:01:00Z', 'header')",
    ).run();
    await env.DB.prepare(
      "INSERT INTO exchanges(id, session_id, ts, endpoint, model, latency_ms, r2_key, capture_status, accepted_at, request_kind, intent_candidate) VALUES ('older-intent', 'ordered-intent', '2026-01-01T00:00:00Z', 'chat', 'openai/test', 1, 'log/older-intent.json', 'accepted', '2026-01-01T00:00:00Z', 'primary', 'First user request')",
    ).run();
    await env.DB.prepare(
      "INSERT INTO exchanges(id, session_id, ts, endpoint, model, latency_ms, r2_key, capture_status, accepted_at, request_kind, intent_candidate) VALUES ('newer-intent', 'ordered-intent', '2026-01-01T00:01:00Z', 'chat', 'openai/test', 1, 'log/newer-intent.json', 'accepted', '2026-01-01T00:01:00Z', 'primary', 'Later user request')",
    ).run();
    await env.LOGS.put("log/older-intent.json", "{}");
    await env.LOGS.put("log/newer-intent.json", "{}");

    await request("/reconcile", {
      method: "POST",
      headers: { authorization: "Bearer machine-token" },
    });

    expect(
      await env.DB.prepare(
        "SELECT intent, request_count FROM sessions WHERE id = 'ordered-intent'",
      ).first(),
    ).toEqual({ intent: "First user request", request_count: 2 });
  });

  it("reopens an inactive exact session on the next exchange", async () => {
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockImplementation(() =>
          Promise.resolve(
            Response.json({
              choices: [],
              usage: { prompt_tokens: 2, completion_tokens: 1 },
            }),
          ),
        ),
    );
    await env.DB.prepare(
      "INSERT INTO sessions(id, started_at, ended_at, state, last_active_at, inactive_at, boundary) VALUES ('session-lifecycle', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', 'inactive', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', 'header')",
    ).run();
    const headers = {
      authorization: "Bearer machine-token",
      "content-type": "application/json",
      "x-mimir-session": "session-lifecycle",
      "x-mimir-harness": "test",
    };
    await request("/v1/chat/completions", {
      method: "POST",
      headers,
      body: JSON.stringify({ model: "openai/test", messages: [] }),
    });
    expect(
      await env.DB.prepare(
        "SELECT state, request_count FROM sessions WHERE id = 'session-lifecycle'",
      ).first(),
    ).toEqual({ state: "active", request_count: 1 });
  });

  it("reactivates an explicitly ended exact session for genuinely later activity", async () => {
    await env.DB.prepare(
      "INSERT INTO sessions(id, started_at, ended_at, state, last_active_at, boundary) VALUES ('ended-reactivate', '2026-01-01T00:00:00Z', '2026-01-01T00:01:00Z', 'active', '2026-01-01T00:01:00Z', 'header')",
    ).run();
    const headers = {
      authorization: "Bearer machine-token",
      "content-type": "application/json",
    };
    const ended = await request("/sessions/ended-reactivate/end", {
      method: "POST",
      headers,
      body: "{}",
    });
    const endTime = ((await ended.json()) as { ended_at: string }).ended_at;
    const later = new Date(Date.parse(endTime) + 1_000).toISOString();
    await env.DB.prepare(
      "INSERT INTO exchanges(id, session_id, ts, endpoint, model, latency_ms, r2_key, capture_status, accepted_at, schema_version) VALUES ('reactivate-exchange', 'ended-reactivate', ?, 'chat', 'openai/test', 1, 'log/reactivate.json', 'accepted', ?, 1)",
    )
      .bind(later, later)
      .run();
    await env.DB.prepare(
      "INSERT INTO sessions(id, started_at, state, last_active_at, boundary) VALUES ('newer-unreopened', ?, 'inactive', ?, 'header')",
    )
      .bind(endTime, endTime)
      .run();
    await finalizeAcceptedExchange(
      env.DB,
      "reactivate-exchange",
      "ended-reactivate",
      later,
      later,
      "opencode",
      "openai/test",
      2,
      1,
      0,
      0,
      50,
      true,
    );
    expect(
      await env.DB.prepare(
        "SELECT state, inactive_at, last_active_at FROM sessions WHERE id = 'ended-reactivate'",
      ).first(),
    ).toEqual({ state: "active", inactive_at: null, last_active_at: later });
    for (const listed of [await dashboardRequest("/dashboard/api/sessions"), await request("/api/sessions", { headers })]) {
      const body = (await listed.json()) as { sessions: Array<{ id: string; state: string; ended_at: string | null }> };
      expect(body.sessions.map(({ id, state, ended_at }) => ({ id, state, ended_at }))).toEqual([
        { id: "ended-reactivate", state: "active", ended_at: null },
        { id: "newer-unreopened", state: "inactive", ended_at: null },
      ]);
    }
  });

  it("reconciles accepted objects and missing saved objects without deletion", async () => {
    await env.DB.prepare(
      "INSERT INTO sessions(id, started_at, state, last_active_at, inactive_at, boundary, request_count, tokens_in, tokens_out) VALUES ('reconcile-session', '2026-01-01T00:00:00Z', 'inactive', '2026-01-01T00:00:00Z', '2026-01-01T00:15:00Z', 'header', 1, 7, 3)",
    ).run();
    await env.DB.prepare(
      "INSERT INTO exchanges(id, session_id, ts, endpoint, model, latency_ms, r2_key, input_tokens, output_tokens, capture_status, capture_reason, accepted_at, schema_version) VALUES ('accepted-object', 'reconcile-session', '2026-01-01T00:00:00Z', 'chat', 'openai/test', 1, 'log/accepted-object.json', 5, 2, 'accepted', 'enabled', '2026-01-01T00:00:00Z', 1)",
    ).run();
    await env.DB.prepare(
      "INSERT INTO exchange_files(exchange_id, session_id, file) VALUES ('accepted-object', 'reconcile-session', 'kept.ts')",
    ).run();
    await env.LOGS.put("log/accepted-object.json", "{}");
    await env.DB.prepare(
      "INSERT INTO exchanges(id, session_id, ts, endpoint, model, latency_ms, r2_key, input_tokens, output_tokens, capture_status, capture_reason, accepted_at, saved_at, schema_version) VALUES ('missing-saved', 'reconcile-session', '2026-01-01T00:00:01Z', 'chat', 'openai/test', 1, 'log/missing-saved.json', 7, 3, 'saved', 'enabled', '2026-01-01T00:00:01Z', '2026-01-01T00:00:02Z', 1)",
    ).run();
    await env.DB.prepare(
      "INSERT INTO exchange_files(exchange_id, session_id, file) VALUES ('missing-saved', 'reconcile-session', 'stale.ts')",
    ).run();
    await env.DB.prepare(
      "INSERT INTO session_files(session_id, file) VALUES ('reconcile-session', 'stale.ts')",
    ).run();
    await env.DB.prepare(
      "INSERT INTO sessions(id, started_at, state, last_active_at, inactive_at, boundary) VALUES ('recent-session', '2026-01-01T00:00:00Z', 'inactive', '2026-01-01T00:00:00Z', '2026-01-01T00:15:00Z', 'header')",
    ).run();
    await env.DB.prepare(
      "INSERT INTO exchanges(id, session_id, ts, endpoint, model, latency_ms, r2_key, capture_status, capture_reason, accepted_at, schema_version) VALUES ('recent-object', 'recent-session', '2026-01-01T00:00:00Z', 'chat', 'openai/test', 1, 'log/recent-object.json', 'accepted', 'enabled', ?, 1)",
    )
      .bind(new Date().toISOString())
      .run();
    await env.LOGS.put("log/recent-object.json", "{}");
    await env.DB.prepare(
      "INSERT INTO sessions(id, started_at, state, last_active_at, inactive_at, boundary, repo, harness) VALUES ('old-heuristic', '2026-01-01T00:00:00Z', 'inactive', '2026-01-01T00:00:00Z', '2026-01-01T00:15:00Z', 'heuristic', 'repo', 'agent')",
    ).run();
    await env.DB.prepare(
      "INSERT INTO sessions(id, started_at, state, last_active_at, boundary, repo, harness) VALUES ('active-heuristic', '2026-01-01T00:20:00Z', 'active', '2026-01-01T00:20:00Z', 'heuristic', 'repo', 'agent')",
    ).run();
    await env.DB.prepare(
      "INSERT INTO exchanges(id, session_id, ts, endpoint, model, latency_ms, r2_key, capture_status, capture_reason, accepted_at, schema_version) VALUES ('heuristic-object', 'old-heuristic', '2026-01-01T00:00:00Z', 'chat', 'openai/test', 1, 'log/heuristic-object.json', 'accepted', 'enabled', ?, 1)",
    )
      .bind(new Date().toISOString())
      .run();
    await env.LOGS.put("log/heuristic-object.json", "{}");
    const response = await request("/reconcile?limit=1000", {
      method: "POST",
      headers: { authorization: "Bearer machine-token" },
    });
    const result = (await response.json()) as {
      limit: number;
      finalized: { exchange_ids: string[] };
      missing_saved: { exchange_ids: string[] };
    };
    expect(result.limit).toBe(100);
    expect(result.finalized.exchange_ids).toContain("accepted-object");
    expect(result.finalized.exchange_ids).toContain("recent-object");
    expect(result.finalized.exchange_ids).toContain("heuristic-object");
    expect(result.missing_saved.exchange_ids).toContain("missing-saved");
    expect(
      await env.DB.prepare(
        "SELECT capture_status FROM exchanges WHERE id = 'accepted-object'",
      ).first(),
    ).toEqual({ capture_status: "saved" });
    expect(
      await env.DB.prepare(
        "SELECT capture_status, failure_code FROM exchanges WHERE id = 'missing-saved'",
      ).first(),
    ).toEqual({ capture_status: "failed", failure_code: "r2_object_missing" });
    expect(
      await env.DB.prepare(
        "SELECT saved_at, r2_bytes FROM exchanges WHERE id = 'missing-saved'",
      ).first(),
    ).toEqual({ saved_at: null, r2_bytes: null });
    expect(
      await env.DB.prepare(
        "SELECT request_count, tokens_in, tokens_out FROM sessions WHERE id = 'reconcile-session'",
      ).first(),
    ).toEqual({ request_count: 1, tokens_in: 5, tokens_out: 2 });
    expect(
      await env.DB.prepare(
        "SELECT state, last_active_at, inactive_at FROM sessions WHERE id = 'reconcile-session'",
      ).first(),
    ).toEqual({
      state: "inactive",
      last_active_at: "2026-01-01T00:00:00Z",
      inactive_at: "2026-01-01T00:15:00Z",
    });
    expect(
      await env.DB.prepare(
        "SELECT state, inactive_at FROM sessions WHERE id = 'recent-session'",
      ).first(),
    ).toEqual({ state: "active", inactive_at: null });
    expect(
      await env.DB.prepare(
        "SELECT state, inactive_at FROM sessions WHERE id = 'old-heuristic'",
      ).first(),
    ).toEqual({ state: "inactive", inactive_at: "2026-01-01T00:15:00Z" });
    expect(
      (
        await env.DB.prepare(
          "SELECT file FROM session_files WHERE session_id = 'reconcile-session' ORDER BY file",
        ).all<{ file: string }>()
      ).results,
    ).toEqual([{ file: "kept.ts" }]);
    expect(await env.LOGS.get("log/accepted-object.json")).not.toBeNull();
  });

  it("removes only strict empty finalized Pi and Hermes roots and their transcripts", async () => {
    await env.DB.exec(`
      INSERT INTO sessions(id, started_at, ended_at, state, last_active_at, inactive_at, boundary, harness) VALUES ('empty-pi', '2026-01-01T00:00:00Z', '2026-01-01T00:01:00Z', 'inactive', '2026-01-01T00:01:00Z', '2026-01-01T00:01:00Z', 'header', 'pi');
      INSERT INTO sessions(id, started_at, ended_at, state, last_active_at, inactive_at, boundary, harness) VALUES ('empty-hermes', '2026-01-01T00:00:00Z', '2026-01-01T00:01:00Z', 'inactive', '2026-01-01T00:01:00Z', '2026-01-01T00:01:00Z', 'header', 'hermes');
      INSERT INTO sessions(id, started_at, ended_at, state, last_active_at, inactive_at, boundary, harness, title, title_source) VALUES ('named-pi', '2026-01-01T00:00:00Z', '2026-01-01T00:01:00Z', 'inactive', '2026-01-01T00:01:00Z', '2026-01-01T00:01:00Z', 'header', 'pi', 'Kept', 'harness');
      INSERT INTO sessions(id, started_at, ended_at, state, last_active_at, inactive_at, boundary, harness) VALUES ('empty-omp', '2026-01-01T00:00:00Z', '2026-01-01T00:01:00Z', 'inactive', '2026-01-01T00:01:00Z', '2026-01-01T00:01:00Z', 'header', 'oh-my-pi');
      INSERT INTO sessions(id, started_at, ended_at, state, last_active_at, inactive_at, boundary, harness) VALUES ('empty-active-pi', '2026-01-01T00:00:00Z', '2026-01-01T00:01:00Z', 'active', '2026-01-01T00:01:00Z', '2026-01-01T00:01:00Z', 'header', 'pi');
    `);
    for (const id of ["empty-pi", "empty-hermes", "named-pi", "empty-omp", "empty-active-pi"]) {
      await env.LOGS.put(`sessions/${id}/transcript.json`, "{}");
    }

    const response = await request("/reconcile", {
      method: "POST",
      headers: { authorization: "Bearer machine-token" },
    });
    const result = (await response.json()) as {
      empty_sessions_removed: { count: number; session_ids: string[] };
    };

    expect(result.empty_sessions_removed).toEqual({
      count: 2,
      session_ids: ["empty-hermes", "empty-pi"],
    });
    expect(
      await env.DB.prepare("SELECT id FROM sessions ORDER BY id").all<{
        id: string;
      }>(),
    ).toMatchObject({
      results: [
        { id: "empty-active-pi" },
        { id: "empty-omp" },
        { id: "named-pi" },
      ],
    });
    expect(
      await env.LOGS.get("sessions/empty-pi/transcript.json"),
    ).toBeNull();
    expect(
      await env.LOGS.get("sessions/empty-hermes/transcript.json"),
    ).toBeNull();
    expect(
      await env.LOGS.get("sessions/named-pi/transcript.json"),
    ).not.toBeNull();
    expect(
      await env.LOGS.get("sessions/empty-omp/transcript.json"),
    ).not.toBeNull();
    expect(
      await env.LOGS.get("sessions/empty-active-pi/transcript.json"),
    ).not.toBeNull();
  });
  it("projects derived and generated titles only from successfully saved redacted exchanges", async () => {
    await env.DB.prepare(
      "INSERT INTO config(key, value) VALUES('redact.patterns', '[\"customer-[0-9]+\"]')",
    ).run();
    const headers = {
      authorization: "Bearer machine-token",
      "content-type": "application/json",
      "x-mimir-harness": "opencode",
    };
    const report = (session: string, payload: Record<string, unknown>) =>
      request(`/sessions/${session}/exchanges`, {
        method: "POST",
        headers,
        body: JSON.stringify(payload),
      });
    const base = {
      ts: "2026-07-26T12:00:00Z",
      model: "openai/test",
      tool_activity: [],
      usage: { input_tokens: 2, output_tokens: 1 },
      latency_ms: 10,
    };

    expect(
      (
        await report("title-projection", {
          ...base,
          exchange_id: "title-primary",
          request_kind: "primary",
          request: {
            messages: [
              { role: "user", content: "Implement durable title precedence" },
            ],
          },
          response: { choices: [] },
        })
      ).status,
    ).toBe(201);
    expect(
      await env.DB.prepare(
        "SELECT intent, title, title_source FROM sessions WHERE id = 'title-projection'",
      ).first(),
    ).toEqual({
      intent: "Implement durable title precedence",
      title: "Implement durable title precedence",
      title_source: "derived",
    });

    expect(
      (
        await report("title-projection", {
          ...base,
          ts: "2026-07-26T12:01:00Z",
          exchange_id: "title-generated",
          request_kind: "title",
          request: { messages: [] },
          response: {
            choices: [
              { message: { content: "Fix customer-123 title storage" } },
            ],
          },
        })
      ).status,
    ).toBe(201);
    expect(
      await env.DB.prepare(
        "SELECT intent, title, title_source FROM sessions WHERE id = 'title-projection'",
      ).first(),
    ).toEqual({
      intent: "Implement durable title precedence",
      title: "Fix [REDACTED] title storage",
      title_source: "generated",
    });

    const listed = (await (
      await request("/api/sessions?q=title%20storage", {
        headers: { authorization: "Bearer machine-token" },
      })
    ).json()) as { sessions: Array<Record<string, unknown>> };
    expect(listed.sessions).toContainEqual(
      expect.objectContaining({
        id: "title-projection",
        title: "Fix [REDACTED] title storage",
        display_title: "Fix [REDACTED] title storage",
      }),
    );

    vi.spyOn(env.LOGS, "put").mockRejectedValueOnce(
      new Error("injected title R2 failure"),
    );
    expect(
      (
        await report("failed-title-projection", {
          ...base,
          exchange_id: "title-failed",
          request_kind: "title",
          request: {},
          response: { choices: [{ message: { content: "Must not project" } }] },
        })
      ).status,
    ).toBe(500);
    expect(
      await env.DB.prepare(
        "SELECT title, title_source FROM sessions WHERE id = 'failed-title-projection'",
      ).first(),
    ).toEqual({ title: null, title_source: null });
  });

  it("enforces manual, harness, generated, and derived title precedence", async () => {
    const authHeaders = {
      authorization: "Bearer machine-token",
      "content-type": "application/json",
    };
    await request("/sessions/title-precedence/events", {
      method: "POST",
      headers: authHeaders,
      body: JSON.stringify({
        version: 1,
        kind: "heartbeat",
        ts: "2026-07-26T12:00:00Z",
        title: "Harness title",
      }),
    });
    expect(
      await env.DB.prepare(
        "SELECT title, title_source FROM sessions WHERE id = 'title-precedence'",
      ).first(),
    ).toEqual({ title: "Harness title", title_source: "harness" });

    const generated = await request("/sessions/title-precedence/exchanges", {
      method: "POST",
      headers: authHeaders,
      body: JSON.stringify({
        exchange_id: "precedence-generated",
        ts: "2026-07-26T12:00:30Z",
        model: "openai/test",
        request: {},
        response: { choices: [{ message: { content: "Generated title" } }] },
        tool_activity: [],
        usage: { input_tokens: 1, output_tokens: 1 },
        latency_ms: 1,
        request_kind: "title",
      }),
    });
    expect(generated.status).toBe(201);
    expect(
      await env.DB.prepare(
        "SELECT title, title_source FROM sessions WHERE id = 'title-precedence'",
      ).first(),
    ).toEqual({ title: "Harness title", title_source: "harness" });

    const manual = await dashboardRequest(
      "/dashboard/api/sessions/title-precedence/title",
      {
        method: "PATCH",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ title: "  Manual   title  " }),
      },
    );
    expect(manual.status).toBe(200);
    expect(await manual.json()).toEqual({
      id: "title-precedence",
      title: "Manual title",
      title_source: "manual",
      display_title: "Manual title",
    });

    await request("/sessions/title-precedence/events", {
      method: "POST",
      headers: authHeaders,
      body: JSON.stringify({
        version: 1,
        kind: "heartbeat",
        ts: "2026-07-26T12:01:00Z",
        title: "Later harness title",
      }),
    });
    expect(
      await env.DB.prepare(
        "SELECT title, title_source FROM sessions WHERE id = 'title-precedence'",
      ).first(),
    ).toEqual({ title: "Manual title", title_source: "manual" });

    expect(
      (
        await dashboardRequest(
          "/dashboard/api/sessions/title-precedence/title",
          {
            method: "PATCH",
            headers: { "content-type": "application/json" },
            body: JSON.stringify({ title: " " }),
          },
        )
      ).status,
    ).toBe(400);
    expect(
      (
        await dashboardRequest(
          "/dashboard/api/sessions/title-precedence/title",
          {
            method: "PATCH",
            headers: { "content-type": "application/json" },
            body: JSON.stringify({ title: "x".repeat(201) }),
          },
        )
      ).status,
    ).toBe(400);
    expect(
      (
        await dashboardRequest("/dashboard/api/sessions/missing/title", {
          method: "PATCH",
          headers: { "content-type": "application/json" },
          body: JSON.stringify({ title: "Valid" }),
        })
      ).status,
    ).toBe(404);
    expect(
      (
        await request("/dashboard/api/sessions/title-precedence/title", {
          method: "PATCH",
          headers: { "content-type": "application/json" },
          body: JSON.stringify({ title: "Unauthorized" }),
        })
      ).status,
    ).toBe(403);
    const cleared = await request("/api/sessions/title-precedence/title", {
      method: "PATCH",
      headers: authHeaders,
      body: JSON.stringify({ title: null }),
    });
    expect(await cleared.json()).toEqual({
      id: "title-precedence",
      title: "Generated title",
      title_source: "generated",
      display_title: "Generated title",
    });
  });

  it("reconciles a persisted generated title candidate after R2 succeeds", async () => {
    await env.DB.prepare(
      "INSERT INTO sessions(id, started_at, state, last_active_at, boundary) VALUES ('reconciled-title', '2026-01-01T00:00:00Z', 'active', '2026-01-01T00:00:00Z', 'header')",
    ).run();
    await env.DB.prepare(
      "INSERT INTO exchanges(id, session_id, ts, endpoint, model, latency_ms, r2_key, capture_status, accepted_at, request_kind, title_candidate) VALUES ('reconciled-title-exchange', 'reconciled-title', '2026-01-01T00:00:00Z', 'chat', 'openai/test', 1, 'log/reconciled-title.json', 'accepted', '2026-01-01T00:00:00Z', 'title', 'Recovered generated title')",
    ).run();
    await env.LOGS.put("log/reconciled-title.json", "{}");

    await request("/reconcile", {
      method: "POST",
      headers: { authorization: "Bearer machine-token" },
    });

    expect(
      await env.DB.prepare(
        "SELECT title, title_source, title_updated_at FROM sessions WHERE id = 'reconciled-title'",
      ).first(),
    ).toEqual({
      title: "Recovered generated title",
      title_source: "generated",
      title_updated_at: "2026-01-01T00:00:00Z",
    });
  });

  it("falls back from a missing generated title object without clearing title locks", async () => {
    await env.DB.exec(`
        INSERT INTO sessions(id, started_at, state, last_active_at, boundary, intent, title, title_source, title_updated_at) VALUES ('missing-generated-title', '2026-01-01T00:00:00Z', 'active', '2026-01-01T00:00:00Z', 'header', 'Preserved session intent', 'Missing generated title', 'generated', '2026-01-01T00:01:00Z');
        INSERT INTO exchanges(id, session_id, ts, endpoint, model, latency_ms, r2_key, capture_status, saved_at, request_kind, title_candidate) VALUES ('missing-generated-title-exchange', 'missing-generated-title', '2026-01-01T00:01:00Z', 'chat', 'openai/test', 1, 'log/missing-generated-title.json', 'saved', '2026-01-01T00:01:01Z', 'title', 'Missing generated title');
        INSERT INTO sessions(id, started_at, state, last_active_at, boundary, intent, title, title_source, title_updated_at) VALUES ('locked-missing-title', '2026-01-01T00:00:00Z', 'active', '2026-01-01T00:00:00Z', 'header', 'Locked intent', 'Manual lock', 'manual', '2026-01-01T00:02:00Z');
        INSERT INTO exchanges(id, session_id, ts, endpoint, model, latency_ms, r2_key, capture_status, saved_at, request_kind, title_candidate) VALUES ('locked-missing-title-exchange', 'locked-missing-title', '2026-01-01T00:01:00Z', 'chat', 'openai/test', 1, 'log/locked-missing-title.json', 'saved', '2026-01-01T00:01:01Z', 'title', 'Missing generated title');
      `);

    await request("/reconcile", {
      method: "POST",
      headers: { authorization: "Bearer machine-token" },
    });

    expect(
      await env.DB.prepare(
        "SELECT title, title_source FROM sessions WHERE id = 'missing-generated-title'",
      ).first(),
    ).toEqual({ title: "Preserved session intent", title_source: "derived" });
    expect(
      await env.DB.prepare(
        "SELECT title, title_source FROM sessions WHERE id = 'locked-missing-title'",
      ).first(),
    ).toEqual({ title: "Manual lock", title_source: "manual" });
  });
});
