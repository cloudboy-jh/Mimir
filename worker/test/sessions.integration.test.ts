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

describe("Sessions integration", () => {
  it("lists root sessions with aggregated supporting-run evidence and roots child outcomes", async () => {
    await env.DB.exec(`
        INSERT INTO sessions(id, started_at, boundary, state, request_count, tokens_in, tokens_out, intent) VALUES ('root-session', '2026-07-27T10:00:00Z', 'header', 'inactive', 1, 10, 5, 'Ship the feature');
        INSERT INTO sessions(id, parent_session_id, started_at, boundary, state, request_count, tokens_in, tokens_out, intent) VALUES ('child-session', 'root-session', '2026-07-27T10:01:00Z', 'header', 'inactive', 1, 20, 7, 'Research the implementation');
        INSERT INTO exchanges(id, session_id, ts, endpoint, latency_ms, r2_key, capture_status, saved_at) VALUES ('root-exchange', 'root-session', '2026-07-27T10:00:30Z', 'harness', 1, 'log/root.json', 'saved', '2026-07-27T10:00:31Z');
        INSERT INTO exchanges(id, session_id, ts, endpoint, latency_ms, r2_key, capture_status, saved_at) VALUES ('child-exchange', 'child-session', '2026-07-27T10:01:30Z', 'harness', 1, 'log/child.json', 'saved', '2026-07-27T10:01:31Z');
      `);

    const listed = (await (
      await dashboardRequest("/dashboard/api/sessions")
    ).json()) as { sessions: Array<Record<string, unknown>> };
    expect(listed.sessions).toHaveLength(1);
    expect(listed.sessions[0]).toMatchObject({
      id: "root-session",
      child_session_count: 1,
      request_count: 2,
      tokens_in: 30,
      tokens_out: 12,
      capture: { saved_exchanges: 2 },
    });

    const detail = (await (
      await dashboardRequest("/dashboard/api/sessions/root-session")
    ).json()) as { supporting_sessions: Array<{ id: string }> } & Record<
      string,
      unknown
    >;
    expect(detail.supporting_sessions).toContainEqual(
      expect.objectContaining({ id: "child-session" }),
    );
    expect(detail).not.toHaveProperty("exchanges");
    const timeline = (await (
      await dashboardRequest(
        "/dashboard/api/sessions/root-session/exchanges?order=asc",
      )
    ).json()) as { exchanges: Array<{ id: string }> };
    expect(timeline.exchanges.map((exchange) => exchange.id)).toEqual([
      "root-exchange",
      "child-exchange",
    ]);

    const outcome = await request("/sessions/child-session/outcome", {
      method: "POST",
      headers: {
        authorization: "Bearer machine-token",
        "content-type": "application/json",
      },
      body: JSON.stringify({ outcome: "landed", reason: "kept by parent" }),
    });
    expect(await outcome.json()).toMatchObject({
      id: "root-session",
      outcome: "landed",
    });
    expect(
      await env.DB.prepare(
        "SELECT work_outcome FROM sessions WHERE id = 'root-session'",
      ).first(),
    ).toMatchObject({ work_outcome: "landed" });
    expect(
      await env.DB.prepare(
        "SELECT work_outcome FROM sessions WHERE id = 'child-session'",
      ).first(),
    ).toMatchObject({ work_outcome: "unresolved" });
  });

  it("orders root sessions by the latest activity in their session tree", async () => {
    await env.DB.exec(`
        INSERT INTO sessions(id, started_at, state, last_active_at, boundary) VALUES ('older-root', '2026-07-27T10:00:00Z', 'inactive', '2026-07-27T10:05:00Z', 'header');
        INSERT INTO sessions(id, parent_session_id, started_at, state, last_active_at, boundary) VALUES ('reopened-supporting', 'older-root', '2026-07-27T10:01:00Z', 'active', '2026-07-27T12:00:00Z', 'header');
        INSERT INTO sessions(id, started_at, state, last_active_at, boundary) VALUES ('newer-root', '2026-07-27T11:00:00Z', 'inactive', '2026-07-27T11:05:00Z', 'header');
      `);

    const dashboard = (await (
      await dashboardRequest("/dashboard/api/sessions")
    ).json()) as { sessions: Array<{ id: string; activity_at: string }> };
    expect(dashboard.sessions).toEqual([
      expect.objectContaining({
        id: "older-root",
        activity_at: "2026-07-27T12:00:00Z",
      }),
      expect.objectContaining({
        id: "newer-root",
        activity_at: "2026-07-27T11:05:00Z",
      }),
    ]);

    const machine = (await (
      await request("/sessions", {
        headers: { authorization: "Bearer machine-token" },
      })
    ).json()) as { sessions: Array<{ id: string; activity_at: string }> };
    expect(machine.sessions.map((session) => session.id)).toEqual([
      "older-root",
      "newer-root",
    ]);
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

  it("marks stale sessions inactive when memory is queried", async () => {
    await env.DB.prepare(
      "INSERT INTO sessions(id, started_at, ended_at, state, last_active_at, boundary) VALUES ('stale', '2020-01-01T00:00:00Z', '2020-01-01T00:00:00Z', 'active', '2020-01-01T00:00:00Z', 'heuristic')",
    ).run();
    const response = await request("/sessions", {
      headers: { authorization: "Bearer machine-token" },
    });
    expect(response.status).toBe(200);
    expect(
      (
        await env.DB.prepare(
          "SELECT state FROM sessions WHERE id = 'stale'",
        ).first<{ state: string }>()
      )?.state,
    ).toBe("inactive");
  });

  it("rejects cross-installation mark, outcome, and end mutations before changes or object events", async () => {
    await addMachineToken("install-a", "machine-a");
    await addMachineToken("install-b", "machine-b");
    await env.DB.exec(`
        INSERT INTO sessions(id, installation_id, started_at, state, last_active_at, boundary) VALUES ('owned-mark-root', 'install-a', '2026-08-13T00:00:00Z', 'active', '2026-08-13T00:00:00Z', 'header');
        INSERT INTO sessions(id, parent_session_id, started_at, state, last_active_at, boundary) VALUES ('owned-mark-child', 'owned-mark-root', '2026-08-13T00:00:01Z', 'active', '2026-08-13T00:00:01Z', 'header');
        INSERT INTO sessions(id, installation_id, started_at, state, last_active_at, boundary) VALUES ('owned-outcome-root', 'install-a', '2026-08-13T00:00:00Z', 'active', '2026-08-13T00:00:00Z', 'header');
        INSERT INTO sessions(id, parent_session_id, started_at, state, last_active_at, boundary) VALUES ('owned-outcome-child', 'owned-outcome-root', '2026-08-13T00:00:01Z', 'active', '2026-08-13T00:00:01Z', 'header');
        INSERT INTO sessions(id, installation_id, started_at, state, last_active_at, boundary) VALUES ('owned-end-root', 'install-a', '2026-08-13T00:00:00Z', 'active', '2026-08-13T00:00:00Z', 'header');
        INSERT INTO sessions(id, parent_session_id, started_at, state, last_active_at, boundary) VALUES ('owned-end-child', 'owned-end-root', '2026-08-13T00:00:01Z', 'active', '2026-08-13T00:00:01Z', 'header');
      `);
    const ownerHeaders = {
      authorization: "Bearer machine-a",
      "content-type": "application/json",
    };
    await request("/sessions/owned-end-child/events", {
      method: "POST",
      headers: ownerHeaders,
      body: JSON.stringify({
        version: 1,
        kind: "heartbeat",
        ts: new Date().toISOString(),
      }),
    });
    const beforeObject = await (
      await request("/sessions/owned-end-child/object-state", {
        headers: ownerHeaders,
      })
    ).json();
    const otherHeaders = {
      authorization: "Bearer machine-b",
      "content-type": "application/json",
    };

    const responses = await Promise.all([
      request("/sessions/owned-mark-child/mark", {
        method: "POST",
        headers: otherHeaders,
        body: JSON.stringify({ outcome: "landed" }),
      }),
      request("/sessions/owned-outcome-child/outcome", {
        method: "POST",
        headers: otherHeaders,
        body: JSON.stringify({ outcome: "discarded" }),
      }),
      request("/sessions/owned-end-child/end", {
        method: "POST",
        headers: otherHeaders,
        body: JSON.stringify({ outcome: "abandoned" }),
      }),
    ]);

    expect(responses.map((response) => response.status)).toEqual([
      403, 403, 403,
    ]);
    expect(
      await env.DB.prepare(
        "SELECT work_outcome FROM sessions WHERE id = 'owned-mark-root'",
      ).first(),
    ).toEqual({ work_outcome: "unresolved" });
    expect(
      await env.DB.prepare(
        "SELECT work_outcome FROM sessions WHERE id = 'owned-outcome-root'",
      ).first(),
    ).toEqual({ work_outcome: "unresolved" });
    expect(
      await env.DB.prepare(
        "SELECT state, ended_at FROM sessions WHERE id = 'owned-end-child'",
      ).first(),
    ).toEqual({ state: "active", ended_at: null });
    expect(
      await env.DB.prepare(
        "SELECT COUNT(*) AS count FROM session_outcome_events",
      ).first(),
    ).toEqual({ count: 0 });
    expect(
      await (
        await request("/sessions/owned-end-child/object-state", {
          headers: ownerHeaders,
        })
      ).json(),
    ).toEqual(beforeObject);
  });

  it("allows the owning installation to mark, set outcome, and end sessions", async () => {
    await addMachineToken("install-a", "machine-a");
    await env.DB.exec(`
        INSERT INTO sessions(id, installation_id, started_at, state, last_active_at, boundary) VALUES ('same-mark', 'install-a', '2026-08-13T00:00:00Z', 'active', '2026-08-13T00:00:00Z', 'header');
        INSERT INTO sessions(id, installation_id, started_at, state, last_active_at, boundary) VALUES ('same-outcome', 'install-a', '2026-08-13T00:00:00Z', 'active', '2026-08-13T00:00:00Z', 'header');
        INSERT INTO sessions(id, installation_id, started_at, state, last_active_at, boundary) VALUES ('same-end', 'install-a', '2026-08-13T00:00:00Z', 'active', '2026-08-13T00:00:00Z', 'header');
      `);
    const headers = {
      authorization: "Bearer machine-a",
      "content-type": "application/json",
    };

    expect(
      (
        await request("/sessions/same-mark/mark", {
          method: "POST",
          headers,
          body: JSON.stringify({ outcome: "landed" }),
        })
      ).status,
    ).toBe(200);
    expect(
      (
        await request("/sessions/same-outcome/outcome", {
          method: "POST",
          headers,
          body: JSON.stringify({ outcome: "discarded" }),
        })
      ).status,
    ).toBe(200);
    expect(
      (
        await request("/sessions/same-end/end", {
          method: "POST",
          headers,
          body: "{}",
        })
      ).status,
    ).toBe(200);
    expect(
      await env.DB.prepare(
        "SELECT work_outcome FROM sessions WHERE id = 'same-mark'",
      ).first(),
    ).toEqual({ work_outcome: "landed" });
    expect(
      await env.DB.prepare(
        "SELECT work_outcome FROM sessions WHERE id = 'same-outcome'",
      ).first(),
    ).toEqual({ work_outcome: "discarded" });
    expect(
      await env.DB.prepare(
        "SELECT state FROM sessions WHERE id = 'same-end'",
      ).first(),
    ).toEqual({ state: "inactive" });
  });

  it("ends sessions idempotently and optionally records an outcome", async () => {
    await env.DB.prepare(
      "INSERT INTO sessions(id, started_at, ended_at, state, last_active_at, boundary) VALUES ('end-session', '2026-01-01T00:00:00Z', '2026-01-01T00:01:00Z', 'active', '2026-01-01T00:01:00Z', 'header')",
    ).run();
    const headers = {
      authorization: "Bearer machine-token",
      "content-type": "application/json",
    };
    const ended = await request("/sessions/end-session/end", {
      method: "POST",
      headers,
      body: JSON.stringify({
        outcome: "landed",
        reason: "verified",
        evidence: { commit: "abc123" },
      }),
    });
    expect(ended.status).toBe(200);
    const first = (await ended.json()) as {
      session: {
        state: string;
        ended_at: string;
        inactive_at: string;
        outcome: string;
        outcome_src: string;
        outcome_updated_at: string;
      };
      evidence: unknown;
    };
    expect(first.session).toMatchObject({
      state: "inactive",
      outcome: "landed",
      outcome_src: "agent",
    });
    expect(first.session.ended_at).toBe(first.session.inactive_at);
    expect(first.evidence).toEqual({ commit: "abc123" });
    expect(
      await env.DB.prepare(
        "SELECT outcome, source, reason, evidence_json FROM session_outcome_events WHERE session_id = 'end-session'",
      ).first(),
    ).toEqual({
      outcome: "landed",
      source: "agent",
      reason: "verified",
      evidence_json: '{"commit":"abc123"}',
    });

    const repeated = await request("/sessions/end-session/end", {
      method: "POST",
      headers,
      body: JSON.stringify({
        outcome: "landed",
        reason: "verified",
        evidence: { commit: "abc123" },
      }),
    });
    const repeatedSession = (
      (await repeated.json()) as {
        session: {
          ended_at: string;
          inactive_at: string;
          outcome_updated_at: string;
        };
      }
    ).session;
    expect(repeatedSession).toMatchObject({
      ended_at: first.session.ended_at,
      inactive_at: first.session.inactive_at,
      outcome_updated_at: first.session.outcome_updated_at,
    });
    expect(
      await env.DB.prepare(
        "SELECT COUNT(*) AS count FROM session_outcome_events WHERE session_id = 'end-session'",
      ).first(),
    ).toEqual({ count: 1 });

    const concurrent = await Promise.all([
      request("/sessions/end-session/end", {
        method: "POST",
        headers,
        body: JSON.stringify({
          outcome: "discarded",
          reason: "superseded",
          evidence: { issue: 42 },
        }),
      }),
      request("/sessions/end-session/end", {
        method: "POST",
        headers,
        body: JSON.stringify({
          outcome: "discarded",
          reason: "superseded",
          evidence: { issue: 42 },
        }),
      }),
    ]);
    expect(concurrent.every((response) => response.status === 200)).toBe(true);
    expect(
      await env.DB.prepare(
        "SELECT COUNT(*) AS count FROM session_outcome_events WHERE session_id = 'end-session' AND outcome = 'discarded'",
      ).first(),
    ).toEqual({ count: 1 });

    expect(
      (
        await request("/sessions/missing/end", {
          method: "POST",
          headers,
          body: "{}",
        })
      ).status,
    ).toBe(404);
    expect(
      (
        await request("/sessions/end-session/end", {
          method: "POST",
          headers,
          body: JSON.stringify({ reason: "missing outcome" }),
        })
      ).status,
    ).toBe(400);
    expect(
      (
        await request("/sessions/end-session/end", {
          method: "POST",
          headers,
          body: "null",
        })
      ).status,
    ).toBe(400);
    expect(
      (
        await request("/sessions/end-session/end", {
          method: "POST",
          headers,
          body: "[]",
        })
      ).status,
    ).toBe(400);
  });

  it("rejects incomplete landed Git evidence without changing the prior outcome", async () => {
    await env.DB.prepare(
      "INSERT INTO sessions(id, started_at, state, last_active_at, boundary, work_outcome, outcome) VALUES ('git-evidence', '2026-08-17T12:00:00Z', 'inactive', '2026-08-17T12:00:00Z', 'header', 'discarded', 'reverted')",
    ).run();
    const headers = {
      authorization: "Bearer machine-token",
      "content-type": "application/json",
    };
    const incomplete = await request("/sessions/git-evidence/outcome", {
      method: "POST",
      headers,
      body: JSON.stringify({
        outcome: "landed",
        evidence: { commit: "a".repeat(40), provenance: "opencode-plugin" },
      }),
    });
    expect(incomplete.status).toBe(400);
    expect(await incomplete.json()).toEqual({
      error: "landed Git outcomes require a retrievable patch",
    });
    expect(
      await env.DB.prepare(
        "SELECT work_outcome FROM sessions WHERE id = 'git-evidence'",
      ).first(),
    ).toEqual({ work_outcome: "discarded" });
    expect(
      await env.DB.prepare(
        "SELECT COUNT(*) AS count FROM session_outcome_events WHERE session_id = 'git-evidence'",
      ).first(),
    ).toEqual({ count: 0 });

    const patch =
      "diff --git a/a.ts b/a.ts\n--- a/a.ts\n+++ b/a.ts\n@@ -1 +1 @@\n-old\n+new\n";
    const complete = await request("/sessions/git-evidence/outcome", {
      method: "POST",
      headers,
      body: JSON.stringify({
        outcome: "landed",
        evidence: {
          commit: "a".repeat(40),
          provenance: "opencode-plugin",
          patch,
        },
      }),
    });
    expect(complete.status).toBe(200);
    const completeBody: unknown = await complete.json();
    if (
      !completeBody ||
      typeof completeBody !== "object" ||
      !("evidence" in completeBody) ||
      !completeBody.evidence ||
      typeof completeBody.evidence !== "object" ||
      !("patch_r2_key" in completeBody.evidence) ||
      typeof completeBody.evidence.patch_r2_key !== "string"
    ) {
      throw new Error("outcome response did not include patch_r2_key");
    }
    expect(
      await env.LOGS.get(completeBody.evidence.patch_r2_key),
    ).not.toBeNull();
    const diff = await dashboardRequest(
      "/dashboard/api/sessions/git-evidence/diff",
    );
    expect(diff.status).toBe(200);
    expect(await diff.text()).toBe(patch);
  });

  it("preserves structured evidence when ending with the current root outcome", async () => {
    await env.DB.exec(`
        INSERT INTO sessions(id, started_at, state, last_active_at, boundary) VALUES ('evidence-root', '2026-01-01T00:00:00Z', 'active', '2026-01-01T00:00:00Z', 'header');
        INSERT INTO sessions(id, parent_session_id, started_at, state, last_active_at, boundary) VALUES ('evidence-child', 'evidence-root', '2026-01-01T00:00:01Z', 'active', '2026-01-01T00:00:01Z', 'header');
      `);
    const headers = {
      authorization: "Bearer machine-token",
      "content-type": "application/json",
    };
    const evidence = {
      commit: "abc123",
      checks: ["worker tests", "typecheck"],
    };
    await request("/sessions/evidence-child/outcome", {
      method: "POST",
      headers,
      body: JSON.stringify({ outcome: "landed", evidence }),
    });

    const ended = await request("/sessions/evidence-child/end", {
      method: "POST",
      headers,
      body: JSON.stringify({ outcome: "landed", reason: "complete" }),
    });

    expect(await ended.json()).toMatchObject({
      session: { state: "inactive" },
      evidence,
    });
    expect(
      await env.DB.prepare(
        "SELECT work_outcome FROM sessions WHERE id = 'evidence-root'",
      ).first(),
    ).toEqual({ work_outcome: "landed" });
    expect(
      await env.DB.prepare(
        "SELECT outcome, reason, evidence_json FROM session_outcome_events WHERE session_id = 'evidence-root' AND id LIKE 'end_%'",
      ).first(),
    ).toEqual({
      outcome: "landed",
      reason: "complete",
      evidence_json: JSON.stringify(evidence),
    });
  });

  it("does not inherit evidence when ending with a different outcome", async () => {
    await env.DB.prepare(
      "INSERT INTO sessions(id, started_at, state, last_active_at, boundary) VALUES ('different-end-outcome', '2026-01-01T00:00:00Z', 'active', '2026-01-01T00:00:00Z', 'header')",
    ).run();
    const headers = {
      authorization: "Bearer machine-token",
      "content-type": "application/json",
    };
    await request("/sessions/different-end-outcome/outcome", {
      method: "POST",
      headers,
      body: JSON.stringify({
        outcome: "landed",
        evidence: { commit: "abc123" },
      }),
    });

    const ended = await request("/sessions/different-end-outcome/end", {
      method: "POST",
      headers,
      body: JSON.stringify({ outcome: "discarded", reason: "rejected" }),
    });

    expect(await ended.json()).toMatchObject({
      session: { outcome: "discarded" },
      evidence: null,
    });
    expect(
      await env.DB.prepare(
        "SELECT outcome, evidence_json FROM session_outcome_events WHERE session_id = 'different-end-outcome' AND id LIKE 'end_%'",
      ).first(),
    ).toEqual({ outcome: "discarded", evidence_json: null });
  });

  it("keeps evidence-preserving end idempotent", async () => {
    await env.DB.prepare(
      "INSERT INTO sessions(id, started_at, state, last_active_at, boundary) VALUES ('idempotent-evidence-end', '2026-01-01T00:00:00Z', 'active', '2026-01-01T00:00:00Z', 'header')",
    ).run();
    const headers = {
      authorization: "Bearer machine-token",
      "content-type": "application/json",
    };
    const evidence = { commit: "def456" };
    await request("/sessions/idempotent-evidence-end/outcome", {
      method: "POST",
      headers,
      body: JSON.stringify({ outcome: "landed", evidence }),
    });
    const body = JSON.stringify({ outcome: "landed", reason: "complete" });

    const first = (await (
      await request("/sessions/idempotent-evidence-end/end", {
        method: "POST",
        headers,
        body,
      })
    ).json()) as { session: { outcome_updated_at: string }; evidence: unknown };
    const repeated = (await (
      await request("/sessions/idempotent-evidence-end/end", {
        method: "POST",
        headers,
        body,
      })
    ).json()) as { session: { outcome_updated_at: string }; evidence: unknown };

    expect(first.evidence).toEqual(evidence);
    expect(repeated).toMatchObject({
      session: { outcome_updated_at: first.session.outcome_updated_at },
      evidence,
    });
    expect(
      await env.DB.prepare(
        "SELECT COUNT(*) AS count FROM session_outcome_events WHERE session_id = 'idempotent-evidence-end' AND id LIKE 'end_%'",
      ).first(),
    ).toEqual({ count: 1 });
  });

  it("turns an auto-expired session into an explicit end marker", async () => {
    await env.DB.prepare(
      "INSERT INTO sessions(id, started_at, ended_at, state, last_active_at, inactive_at, boundary) VALUES ('expired-end-race', '2026-01-01T00:00:00Z', '2026-01-01T00:01:00Z', 'inactive', '2026-01-01T00:01:00Z', '2026-01-01T00:15:00Z', 'header')",
    ).run();
    await env.DB.prepare(
      "INSERT INTO exchanges(id, session_id, ts, endpoint, model, latency_ms, r2_key, capture_status, accepted_at, schema_version) VALUES ('expired-end-exchange', 'expired-end-race', '2026-01-01T00:01:00Z', 'chat', 'openai/test', 1, 'log/expired-end.json', 'accepted', '2026-01-01T00:01:01Z', 1)",
    ).run();
    const headers = {
      authorization: "Bearer machine-token",
      "content-type": "application/json",
    };
    const ended = await request("/sessions/expired-end-race/end", {
      method: "POST",
      headers,
      body: "{}",
    });
    const explicit = (
      (await ended.json()) as {
        session: { ended_at: string; inactive_at: string };
      }
    ).session;
    expect(explicit.ended_at).toBe(explicit.inactive_at);
    expect(explicit.inactive_at).not.toBe("2026-01-01T00:15:00Z");

    await finalizeAcceptedExchange(
      env.DB,
      "expired-end-exchange",
      "expired-end-race",
      "2026-01-01T00:01:00Z",
      "2026-01-01T00:20:00Z",
      "opencode",
      "openai/test",
      2,
      1,
      50,
      true,
    );
    expect(
      await env.DB.prepare(
        "SELECT state, ended_at, inactive_at FROM sessions WHERE id = 'expired-end-race'",
      ).first(),
    ).toEqual({
      state: "inactive",
      ended_at: explicit.ended_at,
      inactive_at: explicit.inactive_at,
    });
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
    const endTime = (
      (await ended.json()) as { session: { inactive_at: string } }
    ).session.inactive_at;
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
      50,
      true,
    );
    expect(
      await env.DB.prepare(
        "SELECT state, inactive_at, last_active_at FROM sessions WHERE id = 'ended-reactivate'",
      ).first(),
    ).toEqual({ state: "active", inactive_at: null, last_active_at: later });
    const dashboard = (await (
      await dashboardRequest("/dashboard/api/sessions")
    ).json()) as { sessions: Array<{ id: string }> };
    expect(dashboard.sessions.map((session) => session.id)).toEqual([
      "ended-reactivate",
      "newer-unreopened",
    ]);
    const machine = (await (
      await request("/sessions", {
        headers: { authorization: "Bearer machine-token" },
      })
    ).json()) as { sessions: Array<{ id: string }> };
    expect(machine.sessions.map((session) => session.id)).toEqual([
      "ended-reactivate",
      "newer-unreopened",
    ]);
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

  it("removes only strict empty finalized Pi roots and their transcripts", async () => {
    await env.DB.exec(`
      INSERT INTO sessions(id, started_at, ended_at, state, last_active_at, inactive_at, boundary, harness) VALUES ('empty-pi', '2026-01-01T00:00:00Z', '2026-01-01T00:01:00Z', 'inactive', '2026-01-01T00:01:00Z', '2026-01-01T00:01:00Z', 'header', 'pi');
      INSERT INTO sessions(id, started_at, ended_at, state, last_active_at, inactive_at, boundary, harness, title, title_source) VALUES ('named-pi', '2026-01-01T00:00:00Z', '2026-01-01T00:01:00Z', 'inactive', '2026-01-01T00:01:00Z', '2026-01-01T00:01:00Z', 'header', 'pi', 'Kept', 'harness');
      INSERT INTO sessions(id, started_at, ended_at, state, last_active_at, inactive_at, boundary, harness) VALUES ('empty-omp', '2026-01-01T00:00:00Z', '2026-01-01T00:01:00Z', 'inactive', '2026-01-01T00:01:00Z', '2026-01-01T00:01:00Z', 'header', 'oh-my-pi');
      INSERT INTO sessions(id, started_at, ended_at, state, last_active_at, inactive_at, boundary, harness) VALUES ('empty-active-pi', '2026-01-01T00:00:00Z', '2026-01-01T00:01:00Z', 'active', '2026-01-01T00:01:00Z', '2026-01-01T00:01:00Z', 'header', 'pi');
    `);
    for (const id of ["empty-pi", "named-pi", "empty-omp", "empty-active-pi"]) {
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
      count: 1,
      session_ids: ["empty-pi"],
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
      await request("/sessions", {
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
    const search = await request("/search", {
      method: "POST",
      headers,
      body: JSON.stringify({ query: "title storage", types: ["title"] }),
    });
    expect(
      ((await search.json()) as { matches: Array<Record<string, unknown>> })
        .matches,
    ).toContainEqual(
      expect.objectContaining({
        session_id: "title-projection",
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
    expect(await manual.json()).toMatchObject({
      session: {
        title: "Manual title",
        title_source: "manual",
        display_title: "Manual title",
      },
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
