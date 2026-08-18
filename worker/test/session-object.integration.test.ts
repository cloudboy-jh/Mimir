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

describe("Session object", () => {
  const authHeaders = {
    authorization: "Bearer machine-token",
    "content-type": "application/json",
  };

  const postEvent = (id: string, event: Record<string, unknown>) =>
    request(`/sessions/${id}/events`, {
      method: "POST",
      headers: authHeaders,
      body: JSON.stringify(event),
    });

  const postInstallationEvent = (
    token: string,
    id: string,
    event: Record<string, unknown>,
  ) =>
    request(`/sessions/${id}/events`, {
      method: "POST",
      headers: {
        authorization: `Bearer ${token}`,
        "content-type": "application/json",
      },
      body: JSON.stringify(event),
    });

  const objectState = async (id: string) => {
    const response = await request(`/sessions/${id}/object-state`, {
      headers: { authorization: "Bearer machine-token" },
    });
    return {
      status: response.status,
      body: await response.json<Record<string, unknown>>(),
    };
  };

  async function installOwnershipFixtures() {
    const now = "2026-08-13T10:00:00Z";
    await env.DB.exec(`
        INSERT INTO machines(installation_id, name, platform, arch, created_at, updated_at) VALUES ('event-install-1', 'One', 'linux', 'amd64', '${now}', '${now}');
        INSERT INTO machines(installation_id, name, platform, arch, created_at, updated_at) VALUES ('event-install-2', 'Two', 'linux', 'amd64', '${now}', '${now}');
      `);
    await env.DB.prepare(
      "INSERT INTO access_tokens(token_hash, label, created_at, installation_id) VALUES (?, 'one', ?, 'event-install-1')",
    )
      .bind(await tokenHash("event-one-token"), now)
      .run();
    await env.DB.prepare(
      "INSERT INTO access_tokens(token_hash, label, created_at, installation_id) VALUES (?, 'two', ?, 'event-install-2')",
    )
      .bind(await tokenHash("event-two-token"), now)
      .run();
  }

  it("tracks turn events and projects liveness", async () => {
    const accepted = await postEvent("object-live", {
      version: 1,
      kind: "turn",
      ts: new Date().toISOString(),
      turn: {
        model: "openai/test",
        usage: { input_tokens: 5, output_tokens: 3 },
        latency_ms: 42,
      },
    });
    expect(accepted.status).toBe(200);
    const { body } = await objectState("object-live");
    expect(body).toMatchObject({
      session_id: "object-live",
      liveness: "active",
      turn_count: 1,
      tokens_in: 5,
      tokens_out: 3,
      finalized_at: null,
    });
    expect(
      await env.DB.prepare(
        "SELECT state, harness, model_primary FROM sessions WHERE id = 'object-live'",
      ).first(),
    ).toEqual({ state: "active", harness: null, model_primary: "openai/test" });
    const listed = (await (
      await dashboardRequest("/dashboard/api/sessions")
    ).json()) as { sessions: Array<{ id: string; liveness: string }> };
    expect(listed.sessions).toContainEqual(
      expect.objectContaining({ id: "object-live", liveness: "active" }),
    );
  });

  it("rejects a conflicting installation heartbeat without reopening or mutating the session", async () => {
    await installOwnershipFixtures();
    const id = "object-owner-heartbeat";
    await postInstallationEvent("event-one-token", id, {
      version: 1,
      kind: "end",
      ts: "2026-08-13T10:01:00Z",
      reason: "owned end",
    });
    const beforeState = (await objectState(id)).body;
    const beforeSession = await env.DB.prepare(
      "SELECT installation_id, state, ended_at, inactive_at, last_active_at FROM sessions WHERE id = ?",
    )
      .bind(id)
      .first();
    const beforeTranscript = await (await env.LOGS.get(
      `sessions/${id}/transcript.json`,
    ))!.text();

    const conflict = await postInstallationEvent("event-two-token", id, {
      version: 1,
      kind: "heartbeat",
      ts: "2026-08-13T10:02:00Z",
      title: "Must not apply",
    });

    expect(conflict.status).toBe(409);
    expect((await objectState(id)).body).toEqual(beforeState);
    expect(
      await env.DB.prepare(
        "SELECT installation_id, state, ended_at, inactive_at, last_active_at FROM sessions WHERE id = ?",
      )
        .bind(id)
        .first(),
    ).toEqual(beforeSession);
    expect(
      await (await env.LOGS.get(`sessions/${id}/transcript.json`))!.text(),
    ).toBe(beforeTranscript);
  });

  it("rejects a conflicting installation turn without mutating object or D1 state", async () => {
    await installOwnershipFixtures();
    const id = "object-owner-turn";
    await env.DB.prepare(
      "INSERT INTO sessions(id, started_at, state, boundary) VALUES (?, '2026-08-13T10:00:00Z', 'active', 'header')",
    )
      .bind(id)
      .run();
    const firstEventAt = new Date().toISOString();
    await postInstallationEvent("event-one-token", id, {
      version: 1,
      kind: "heartbeat",
      ts: firstEventAt,
    });
    const beforeState = (await objectState(id)).body;
    const beforeSession = await env.DB.prepare(
      "SELECT installation_id, state, last_active_at, model_primary FROM sessions WHERE id = ?",
    )
      .bind(id)
      .first();
    expect(beforeSession).toMatchObject({ installation_id: "event-install-1" });

    const conflict = await postInstallationEvent("event-two-token", id, {
      version: 1,
      kind: "turn",
      ts: new Date(Date.parse(firstEventAt) + 1_000).toISOString(),
      turn: {
        model: "openai/conflict",
        usage: { input_tokens: 7, output_tokens: 3 },
      },
    });

    expect(conflict.status).toBe(409);
    expect((await objectState(id)).body).toEqual(beforeState);
    expect(
      await env.DB.prepare(
        "SELECT installation_id, state, last_active_at, model_primary FROM sessions WHERE id = ?",
      )
        .bind(id)
        .first(),
    ).toEqual(beforeSession);
    expect(await env.LOGS.get(`sessions/${id}/transcript.json`)).toBeNull();
  });

  it("rejects a conflicting installation end without finalizing or writing a transcript", async () => {
    await installOwnershipFixtures();
    const id = "object-owner-end";
    const firstEventAt = new Date().toISOString();
    await postInstallationEvent("event-one-token", id, {
      version: 1,
      kind: "heartbeat",
      ts: firstEventAt,
    });
    const beforeState = (await objectState(id)).body;
    const beforeSession = await env.DB.prepare(
      "SELECT installation_id, state, ended_at, inactive_at, last_active_at FROM sessions WHERE id = ?",
    )
      .bind(id)
      .first();

    const conflict = await postInstallationEvent("event-two-token", id, {
      version: 1,
      kind: "end",
      ts: new Date(Date.parse(firstEventAt) + 1_000).toISOString(),
      reason: "conflicting end",
    });

    expect(conflict.status).toBe(409);
    expect((await objectState(id)).body).toEqual(beforeState);
    expect(
      await env.DB.prepare(
        "SELECT installation_id, state, ended_at, inactive_at, last_active_at FROM sessions WHERE id = ?",
      )
        .bind(id)
        .first(),
    ).toEqual(beforeSession);
    expect(await env.LOGS.get(`sessions/${id}/transcript.json`)).toBeNull();
  });

  it("serves object state through Access-protected dashboard routes and leaves D1-only history intact", async () => {
    const stale = new Date(Date.now() - 3 * 60_000).toISOString();
    await postEvent("dashboard-object-state", {
      version: 1,
      kind: "heartbeat",
      ts: stale,
    });
    await env.DB.prepare(
      "INSERT INTO sessions(id, started_at, state, boundary) VALUES ('historical-only', '2026-01-01T00:00:00Z', 'inactive', 'header')",
    ).run();
    await env.DB.prepare(
      "INSERT INTO sessions(id, started_at, state, boundary) VALUES ('d1-active-only', '2026-01-02T00:00:00Z', 'active', 'header')",
    ).run();

    const state = await dashboardRequest(
      "/dashboard/api/sessions/dashboard-object-state/object-state",
    );
    expect(state.status).toBe(200);
    expect(await state.json()).toMatchObject({
      session_id: "dashboard-object-state",
      liveness: "disconnected",
    });
    expect(
      (
        await dashboardRequest(
          "/dashboard/api/sessions/historical-only/object-state",
        )
      ).status,
    ).toBe(404);

    const listed = (await (
      await dashboardRequest("/dashboard/api/sessions")
    ).json()) as { sessions: Array<{ id: string; liveness: string }> };
    expect(listed.sessions).toContainEqual(
      expect.objectContaining({
        id: "dashboard-object-state",
        liveness: "disconnected",
      }),
    );
    expect(listed.sessions).toContainEqual(
      expect.objectContaining({
        id: "d1-active-only",
        liveness: "disconnected",
      }),
    );
    expect(listed.sessions).toContainEqual(
      expect.objectContaining({ id: "historical-only", liveness: "finalized" }),
    );
    expect(
      (
        await request(
          "/dashboard/api/sessions/dashboard-object-state/object-state",
        )
      ).status,
    ).toBe(403);
  });

  it("deduplicates retried turn events by exchange ID", async () => {
    const event = {
      version: 1,
      kind: "turn",
      ts: new Date().toISOString(),
      turn: {
        exchange_id: "retry-1",
        model: "openai/test",
        usage: { input_tokens: 5, output_tokens: 3 },
      },
    };
    expect((await postEvent("object-retry", event)).status).toBe(200);
    expect(
      (
        await postEvent("object-retry", {
          ...event,
          ts: new Date().toISOString(),
        })
      ).status,
    ).toBe(200);
    const { body } = await objectState("object-retry");
    expect(body).toMatchObject({ turn_count: 1, tokens_in: 5, tokens_out: 3 });
  });

  it("rejects invalid events and requires auth", async () => {
    expect(
      (
        await postEvent("object-invalid", {
          version: 1,
          kind: "note",
          ts: new Date().toISOString(),
        })
      ).status,
    ).toBe(400);
    expect(
      (
        await request("/sessions/object-invalid/events", {
          method: "POST",
          headers: { "content-type": "application/json" },
          body: "{}",
        })
      ).status,
    ).toBe(401);
    expect(
      (
        await postEvent("object-invalid", {
          version: 1,
          kind: "turn",
          ts: new Date().toISOString(),
          turn: { usage: { input_tokens: -1, output_tokens: 0 } },
        })
      ).status,
    ).toBe(400);
  });

  it("does not reopen a finalized session for duplicate turns or stale heartbeats", async () => {
    const beforeEnd = new Date(Date.now() - 1_000).toISOString();
    const turn = {
      version: 1,
      kind: "turn",
      ts: beforeEnd,
      turn: { exchange_id: "finalized-retry", model: "openai/test" },
    };
    await postEvent("object-finalized-retry", turn);
    await postEvent("object-finalized-retry", {
      version: 1,
      kind: "end",
      ts: new Date().toISOString(),
      reason: "done",
    });
    await postEvent("object-finalized-retry", turn);
    await postEvent("object-finalized-retry", {
      version: 1,
      kind: "heartbeat",
      ts: beforeEnd,
    });
    const { body } = await objectState("object-finalized-retry");
    expect(body).toMatchObject({
      liveness: "finalized",
      turn_count: 1,
      end_reason: "done",
    });
  });

  it("projects disconnected after the liveness window without finalizing", async () => {
    const stale = new Date(Date.now() - 3 * 60_000).toISOString();
    await postEvent("object-stale", {
      version: 1,
      kind: "heartbeat",
      ts: stale,
    });
    const { body } = await objectState("object-stale");
    expect(body).toMatchObject({
      liveness: "disconnected",
      finalized_at: null,
    });
  });

  it("finalizes on an end event: transcript in R2, session inactive in D1", async () => {
    await postEvent("object-end", {
      version: 1,
      kind: "turn",
      ts: new Date().toISOString(),
      turn: {
        model: "openai/test",
        usage: { input_tokens: 2, output_tokens: 1 },
      },
    });
    const ended = await postEvent("object-end", {
      version: 1,
      kind: "end",
      ts: new Date().toISOString(),
      reason: "user closed",
    });
    expect(ended.status).toBe(200);
    const { body } = await objectState("object-end");
    expect(body).toMatchObject({
      liveness: "finalized",
      end_reason: "user closed",
      turn_count: 1,
    });
    expect(typeof body.finalized_at).toBe("string");
    const session = await env.DB.prepare(
      "SELECT state, ended_at, inactive_at FROM sessions WHERE id = 'object-end'",
    ).first<{
      state: string;
      ended_at: string | null;
      inactive_at: string | null;
    }>();
    expect(session?.state).toBe("inactive");
    expect(session?.ended_at).toBeTruthy();
    const transcript = await env.LOGS.get(
      "sessions/object-end/transcript.json",
    );
    expect(transcript).not.toBeNull();
    const manifest = JSON.parse(await transcript!.text());
    expect(manifest).toMatchObject({
      schema_version: 1,
      session_id: "object-end",
      end_reason: "user closed",
      turn_count: 1,
      usage: { input_tokens: 2, output_tokens: 1 },
    });
  });

  it("reopens a finalized session when new events arrive", async () => {
    await postEvent("object-reopen", {
      version: 1,
      kind: "end",
      ts: new Date().toISOString(),
    });
    expect((await objectState("object-reopen")).body.liveness).toBe(
      "finalized",
    );
    expect(
      (
        await env.DB.prepare(
          "SELECT state FROM sessions WHERE id = 'object-reopen'",
        ).first<{ state: string }>()
      )?.state,
    ).toBe("inactive");
    await postEvent("object-reopen", {
      version: 1,
      kind: "turn",
      ts: new Date().toISOString(),
      turn: { model: "openai/test" },
    });
    const { body } = await objectState("object-reopen");
    expect(body).toMatchObject({
      liveness: "active",
      finalized_at: null,
      turn_count: 1,
    });
    expect(
      (
        await env.DB.prepare(
          "SELECT state FROM sessions WHERE id = 'object-reopen'",
        ).first<{ state: string }>()
      )?.state,
    ).toBe("active");
  });

  it("requires a websocket upgrade for the live feed", async () => {
    const response = await request("/sessions/object-live/live", {
      headers: { authorization: "Bearer machine-token" },
    });
    expect(response.status).toBe(426);
  });

  it("serves a live feed snapshot over websocket", async () => {
    await postEvent("object-feed", {
      version: 1,
      kind: "turn",
      ts: new Date().toISOString(),
      turn: { model: "openai/test", excerpt: "hello" },
    });
    const ctx = createExecutionContext();
    const response = await worker.fetch(
      new Request("https://mimir.test/sessions/object-feed/live", {
        headers: {
          authorization: "Bearer machine-token",
          upgrade: "websocket",
        },
      }),
      env as Env & { OPENROUTER_API_KEY: string },
      ctx,
    );
    expect(response.status).toBe(101);
    const socket = response.webSocket!;
    socket.accept();
    const message = await new Promise<string>((resolve, reject) => {
      const timer = setTimeout(
        () => reject(new Error("snapshot timeout")),
        5_000,
      );
      socket.addEventListener(
        "message",
        (event) => {
          clearTimeout(timer);
          resolve(String(event.data));
        },
        { once: true },
      );
    });
    const snapshot = JSON.parse(message);
    expect(snapshot.type).toBe("snapshot");
    expect(snapshot.state).toMatchObject({
      session_id: "object-feed",
      liveness: "active",
      turn_count: 1,
    });
    expect(snapshot.turns).toHaveLength(1);
    socket.close(1000);
  });

  it("passes dashboard WebSockets through to the session object without machine credentials", async () => {
    await postEvent("dashboard-feed", {
      version: 1,
      kind: "turn",
      ts: new Date().toISOString(),
      turn: { model: "openai/test", excerpt: "dashboard live turn" },
    });
    const ctx = createExecutionContext();
    const response = await worker.fetch(
      new Request(
        "http://localhost/dashboard/api/sessions/dashboard-feed/live",
        { headers: { upgrade: "websocket" } },
      ),
      env as Env & { OPENROUTER_API_KEY: string },
      ctx,
    );
    expect(response.status).toBe(101);
    const socket = response.webSocket!;
    socket.accept();
    const message = await new Promise<string>((resolve, reject) => {
      const timer = setTimeout(
        () => reject(new Error("dashboard snapshot timeout")),
        5_000,
      );
      socket.addEventListener(
        "message",
        (event) => {
          clearTimeout(timer);
          resolve(String(event.data));
        },
        { once: true },
      );
    });
    expect(JSON.parse(message)).toMatchObject({
      type: "snapshot",
      state: { session_id: "dashboard-feed", liveness: "active" },
      turns: [{ excerpt: "dashboard live turn" }],
    });
    const liveMessages = new Promise<Array<Record<string, unknown>>>(
      (resolve, reject) => {
        const messages: Array<Record<string, unknown>> = [];
        const timer = setTimeout(
          () => reject(new Error("dashboard live events timeout")),
          5_000,
        );
        socket.addEventListener("message", (event) => {
          const parsed = JSON.parse(String(event.data)) as Record<
            string,
            unknown
          >;
          messages.push(parsed);
          if (parsed.type === "finalized") {
            clearTimeout(timer);
            resolve(messages);
          }
        });
      },
    );
    const closed = new Promise<CloseEvent>((resolve) =>
      socket.addEventListener("close", resolve, { once: true }),
    );
    await postEvent("dashboard-feed", {
      version: 1,
      kind: "turn",
      ts: new Date().toISOString(),
      turn: { model: "openai/test", excerpt: "streamed turn" },
    });
    await postEvent("dashboard-feed", {
      version: 1,
      kind: "end",
      ts: new Date().toISOString(),
      reason: "dashboard test",
    });
    const streamed = await liveMessages;
    expect(streamed).toContainEqual(
      expect.objectContaining({
        type: "event",
        event: expect.objectContaining({
          kind: "turn",
          turn: expect.objectContaining({ excerpt: "streamed turn" }),
        }),
      }),
    );
    expect(streamed).toContainEqual(
      expect.objectContaining({ type: "finalized", reason: "dashboard test" }),
    );
    await expect(closed).resolves.toMatchObject({
      code: 1000,
      reason: "session finalized",
    });
    await waitOnExecutionContext(ctx);

    const finalizedFeed = await worker.fetch(
      new Request(
        "http://localhost/dashboard/api/sessions/dashboard-feed/live",
        { headers: { upgrade: "websocket" } },
      ),
      env as Env & { OPENROUTER_API_KEY: string },
      createExecutionContext(),
    );
    expect(finalizedFeed.status).toBe(409);

    expect(
      (
        await request("/dashboard/api/sessions/dashboard-feed/live", {
          headers: { upgrade: "websocket" },
        })
      ).status,
    ).toBe(403);
    expect(
      (await dashboardRequest("/dashboard/api/sessions/dashboard-feed/live"))
        .status,
    ).toBe(426);
  });

  it("reports proxied exchanges to the session object", async () => {
    const stream =
      'data: {"choices":[{"delta":{"content":"hi"}}]}\n\ndata: {"usage":{"prompt_tokens":4,"completion_tokens":2}}\n\ndata: [DONE]\n';
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockResolvedValue(
          new Response(stream, {
            status: 200,
            headers: { "content-type": "text/event-stream" },
          }),
        ),
    );
    const response = await request("/v1/chat/completions", {
      method: "POST",
      headers: {
        ...authHeaders,
        "x-mimir-session": "object-proxied",
        "x-mimir-harness": "test",
      },
      body: JSON.stringify({
        model: "openai/test",
        messages: [{ role: "user", content: "hello" }],
        stream: true,
      }),
    });
    expect(response.status).toBe(200);
    await response.text();
    const { body } = await objectState("object-proxied");
    expect(body).toMatchObject({
      session_id: "object-proxied",
      liveness: "active",
      turn_count: 1,
      tokens_in: 4,
      tokens_out: 2,
      harness: "test",
    });
  });

  it("finalizes the session object on explicit end", async () => {
    await postEvent("object-explicit-end", {
      version: 1,
      kind: "turn",
      ts: new Date().toISOString(),
      turn: { model: "openai/test" },
    });
    await env.DB.prepare(
      "INSERT OR IGNORE INTO sessions(id, started_at, last_active_at, harness, boundary) VALUES ('object-explicit-end', ?, ?, 'test', 'header')",
    )
      .bind(new Date().toISOString(), new Date().toISOString())
      .run();
    const ended = await request("/sessions/object-explicit-end/end", {
      method: "POST",
      headers: { authorization: "Bearer machine-token" },
    });
    expect(ended.status).toBe(200);
    const { body } = await objectState("object-explicit-end");
    expect(body).toMatchObject({
      liveness: "finalized",
      end_reason: "explicit",
    });
    expect(
      await env.LOGS.get("sessions/object-explicit-end/transcript.json"),
    ).not.toBeNull();
  });

  it("ends sessions materialized by live events", async () => {
    await postEvent("object-only-end", {
      version: 1,
      kind: "turn",
      ts: new Date().toISOString(),
      turn: { model: "openai/test" },
    });
    expect(
      (
        await env.DB.prepare(
          "SELECT state FROM sessions WHERE id = 'object-only-end'",
        ).first<{ state: string }>()
      )?.state,
    ).toBe("active");
    const ended = await request("/sessions/object-only-end/end", {
      method: "POST",
      headers: { authorization: "Bearer machine-token" },
    });
    expect(ended.status).toBe(200);
    expect(
      (
        await env.DB.prepare(
          "SELECT state FROM sessions WHERE id = 'object-only-end'",
        ).first<{ state: string }>()
      )?.state,
    ).toBe("inactive");
    const { body } = await objectState("object-only-end");
    expect(body).toMatchObject({
      liveness: "finalized",
      end_reason: "explicit",
    });
    expect(
      await env.LOGS.get("sessions/object-only-end/transcript.json"),
    ).not.toBeNull();
  });

  it("keeps the 404 contract for sessions unknown to D1 and the object", async () => {
    const response = await request("/sessions/object-never-seen/end", {
      method: "POST",
      headers: { authorization: "Bearer machine-token" },
    });
    expect(response.status).toBe(404);
  });

  it("stores complete outcome patches in R2 and generates finalized summaries", async () => {
    await env.DB.prepare(
      "INSERT INTO sessions(id, started_at, ended_at, state, last_active_at, inactive_at, harness, boundary, intent, request_count) VALUES ('full-diff-session', '2026-08-14T10:00:00Z', '2026-08-14T10:10:00Z', 'inactive', '2026-08-14T10:10:00Z', '2026-08-14T10:10:00Z', 'oh-my-pi', 'header', 'Implement the complete diff view', 4)",
    ).run();
    const patch = `diff --git a/a.ts b/a.ts\n--- a/a.ts\n+++ b/a.ts\n@@ -1,1 +1,2 @@\n-old\n+new\n+${"x".repeat(40_000)}\n`;
    const outcome = await request("/sessions/full-diff-session/outcome", {
      method: "POST",
      headers: {
        authorization: "Bearer machine-token",
        "content-type": "application/json",
      },
      body: JSON.stringify({
        outcome: "landed",
        reason: "The complete diff view shipped",
        evidence: { commit: "abc123", patch },
      }),
    });
    expect(outcome.status).toBe(200);
    const stored = await env.DB.prepare(
      "SELECT evidence_json FROM session_outcome_events WHERE session_id = 'full-diff-session'",
    ).first<{ evidence_json: string }>();
    const evidence = JSON.parse(stored!.evidence_json);
    expect(evidence.patch).toBeUndefined();
    expect(evidence.patch_r2_key).toMatch(
      /^sessions\/full-diff-session\/diffs\/.+\.patch$/,
    );
    expect(await env.LOGS.get(evidence.patch_r2_key)).not.toBeNull();
    const diff = await dashboardRequest(
      "/dashboard/api/sessions/full-diff-session/diff",
    );
    expect(diff.status).toBe(200);
    expect(await diff.text()).toBe(patch);
    const detail = (await (
      await dashboardRequest("/dashboard/api/sessions/full-diff-session")
    ).json()) as any;
    expect(detail.session).toMatchObject({
      summary_status: "ready",
      summary_source: "generated",
    });
    expect(detail.session.summary_text).toContain(
      "The complete diff view shipped.",
    );
  });
});
