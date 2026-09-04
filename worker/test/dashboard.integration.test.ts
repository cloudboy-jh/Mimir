import { exportJWK, generateKeyPair, SignJWT } from "jose";
import { describe, expect, it, vi } from "vitest";
import { autoResolveStaleOutcomes } from "../src/sessions/outcomes";
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

describe("Dashboard integration", () => {
  it("persists authenticated device identity and serves device dashboard APIs", async () => {
    const now = "2026-08-13T10:00:00Z";
    await env.DB.prepare(
      "INSERT INTO machines(installation_id, name, platform, arch, created_at, updated_at) VALUES ('install-1', 'Workstation', 'windows', 'amd64', ?, ?)",
    )
      .bind(now, now)
      .run();
    await env.DB.prepare(
      "INSERT INTO access_tokens(token_hash, label, created_at, installation_id) VALUES (?, 'device', ?, 'install-1')",
    )
      .bind(await tokenHash("device-token"), now)
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

    await request("/v1/chat/completions", {
      method: "POST",
      headers: {
        authorization: "Bearer device-token",
        "content-type": "application/json",
        "x-mimir-session": "device-session",
        "x-mimir-harness": "opencode",
      },
      body: JSON.stringify({
        model: "openai/test",
        messages: [{ role: "user", content: "Device identity" }],
      }),
    });
    await env.DB.exec(`
        INSERT INTO sessions(id, installation_id, started_at, boundary, harness) VALUES ('device-root', 'install-1', '2026-08-13T10:01:00Z', 'header', 'codex');
        INSERT INTO sessions(id, parent_session_id, installation_id, started_at, boundary, harness) VALUES ('device-child', 'device-root', 'install-1', '2026-08-13T10:02:00Z', 'header', 'opencode');
        INSERT INTO sessions(id, parent_session_id, installation_id, started_at, boundary, harness) VALUES ('device-empty-harness', 'device-root', 'install-1', '2026-08-13T10:03:00Z', 'header', '');
      `);
    await env.DB.prepare(
      "INSERT INTO harness_loads(token_hash, token_label, harness, artifact_sha256, installation_id, client_loaded_at, reported_at) VALUES (?, 'device', 'hermes', ?, 'install-1', ?, ?)",
    )
      .bind(await tokenHash("device-token"), "a".repeat(64), now, now)
      .run();
    expect(
      await env.DB.prepare(
        "SELECT installation_id FROM sessions WHERE id = 'device-session'",
      ).first(),
    ).toEqual({ installation_id: "install-1" });
    expect(
      await env.DB.prepare(
        "SELECT last_seen_at FROM machines WHERE installation_id = 'install-1'",
      ).first<{ last_seen_at: string | null }>(),
    ).toEqual({ last_seen_at: expect.any(String) });

    const listed = (await (
      await dashboardRequest("/dashboard/api/sessions")
    ).json()) as { sessions: Array<Record<string, unknown>> };
    expect(listed.sessions[0]).toMatchObject({
      id: "device-session",
      device: {
        id: "install-1",
        name: "Workstation",
        platform: "windows",
        arch: "amd64",
      },
    });
    const detail = (await (
      await dashboardRequest("/dashboard/api/sessions/device-session")
    ).json()) as { session: Record<string, unknown> };
    expect(detail.session).toMatchObject({
      device: { id: "install-1", name: "Workstation" },
    });

    expect(
      await (await dashboardRequest("/dashboard/api/devices")).json(),
    ).toMatchObject({
      devices: [
        {
          id: "install-1",
          name: "Workstation",
          session_count: 2,
          harnesses: ["codex", "opencode"],
        },
      ],
    });
    const renamed = await dashboardRequest("/dashboard/api/devices/install-1", {
      method: "PATCH",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ name: "Desk PC" }),
    });
    expect(await renamed.json()).toMatchObject({
      device: {
        id: "install-1",
        name: "Desk PC",
        session_count: 2,
        harnesses: ["codex", "opencode"],
      },
    });

    const revoked = await dashboardRequest(
      "/dashboard/api/devices/install-1/revoke",
      { method: "POST" },
    );
    expect(await revoked.json()).toMatchObject({
      device: {
        id: "install-1",
        revoked_at: expect.any(String),
        session_count: 2,
        harnesses: ["codex", "opencode"],
      },
    });
    expect(
      (
        await request("/whoami", {
          headers: { authorization: "Bearer device-token" },
        })
      ).status,
    ).toBe(401);
    expect(
      await env.DB.prepare(
        "SELECT installation_id FROM sessions WHERE id = 'device-session'",
      ).first(),
    ).toEqual({ installation_id: "install-1" });
  });

  it("requires Cloudflare Access for dashboard APIs", async () => {
    const response = await request("/dashboard/api/bootstrap");
    expect(response.status).toBe(403);
    expect(
      (await request("/dashboard/auth?returnTo=%2Fdashboard%2Fsessions"))
        .status,
    ).toBe(403);
  });

  it("exposes a clearly marked local dashboard identity", async () => {
    const response = await dashboardRequest("/dashboard/api/identity");
    expect(response.status).toBe(200);
    expect(await response.json()).toEqual({
      email: null,
      name: "Local development",
      source: "local-development",
    });
    const handoff = await dashboardRequest(
      "/dashboard/auth?returnTo=%2Fdashboard%2Fsessions%2Fsession-1",
    );
    expect(handoff.status).toBe(302);
    expect(handoff.headers.get("location")).toBe(
      "/dashboard/sessions/session-1",
    );
  });

  it("filters and paginates root dashboard sessions", async () => {
    await env.DB.exec(`
        INSERT INTO sessions(id, started_at, state, harness, boundary, work_outcome, repo, model_primary, intent) VALUES ('match-new', '2026-07-27T10:03:00Z', 'inactive', 'opencode', 'header', 'landed', 'mimir', 'openai/gpt-5', 'Fix dashboard needle');
        INSERT INTO sessions(id, started_at, state, harness, boundary, work_outcome, repo, model_primary, intent) VALUES ('match-old', '2026-07-27T10:02:00Z', 'inactive', 'opencode', 'header', 'landed', 'mimir', 'openai/gpt-5', 'Review dashboard needle');
        INSERT INTO sessions(id, started_at, state, harness, boundary, work_outcome, repo, model_primary, intent) VALUES ('wrong-outcome', '2026-07-27T10:01:00Z', 'inactive', 'opencode', 'header', 'discarded', 'mimir', 'openai/gpt-5', 'Fix dashboard needle');
        INSERT INTO sessions(id, started_at, state, harness, boundary, work_outcome, repo, model_primary, intent) VALUES ('wrong-app', '2026-07-27T10:00:00Z', 'inactive', 'hermes', 'header', 'landed', 'mimir', 'openai/gpt-5', 'Fix dashboard needle');
        INSERT INTO sessions(id, parent_session_id, started_at, state, harness, boundary, work_outcome, repo, model_primary, intent) VALUES ('supporting-match', 'match-new', '2026-07-27T10:04:00Z', 'inactive', 'opencode', 'header', 'landed', 'mimir', 'openai/gpt-5', 'Fix dashboard needle');
        INSERT INTO exchanges(id, session_id, ts, endpoint, model, latency_ms, r2_key, capture_status) VALUES ('match-new-exchange', 'match-new', '2026-07-27T10:03:10Z', 'chat', 'openai/gpt-5', 1, 'log/match-new.json', 'saved');
        INSERT INTO exchanges(id, session_id, ts, endpoint, model, latency_ms, r2_key, capture_status) VALUES ('match-old-exchange', 'match-old', '2026-07-27T10:02:10Z', 'chat', 'openai/gpt-5', 1, 'log/match-old.json', 'saved');
        INSERT INTO exchanges(id, session_id, ts, endpoint, model, latency_ms, r2_key, capture_status) VALUES ('wrong-outcome-exchange', 'wrong-outcome', '2026-07-27T10:01:10Z', 'chat', 'openai/gpt-5', 1, 'log/wrong-outcome.json', 'saved');
        INSERT INTO exchanges(id, session_id, ts, endpoint, model, latency_ms, r2_key, capture_status) VALUES ('wrong-app-exchange', 'wrong-app', '2026-07-27T10:00:10Z', 'chat', 'openai/gpt-5', 1, 'log/wrong-app.json', 'saved');
      `);
    const filters =
      "q=NEEDLE&repo=mimir&outcome=landed&app=opencode&model=openai%2Fgpt-5&from=2026-07-27T10%3A02%3A00Z&to=2026-07-27T10%3A03%3A00Z&limit=1";
    const firstResponse = await dashboardRequest(
      `/dashboard/api/sessions?${filters}`,
    );
    expect(firstResponse.status).toBe(200);
    const first = (await firstResponse.json()) as {
      sessions: Array<{
        id: string;
        child_session_count: number;
        activity_at: string;
      }>;
      next_cursor: string | null;
    };
    expect(first.sessions).toEqual([
      expect.objectContaining({
        id: "match-new",
        child_session_count: 1,
        activity_at: "2026-07-27T10:04:00Z",
      }),
    ]);
    expect(first.next_cursor).toEqual(expect.any(String));

    const second = (await (
      await dashboardRequest(
        `/dashboard/api/sessions?${filters}&cursor=${encodeURIComponent(first.next_cursor!)}`,
      )
    ).json()) as {
      sessions: Array<{ id: string }>;
      next_cursor: string | null;
    };
    expect(second.sessions.map((session) => session.id)).toEqual(["match-old"]);
    expect(second.next_cursor).toBeNull();
    expect(
      (await dashboardRequest("/dashboard/api/sessions?outcome=not-real"))
        .status,
    ).toBe(400);
    expect(
      (await dashboardRequest("/dashboard/api/sessions?cursor=not-a-cursor"))
        .status,
    ).toBe(400);
    expect(
      (await dashboardRequest("/dashboard/api/log?cursor=not-a-cursor")).status,
    ).toBe(400);
  });

  it("reports exact-session model usage and isolates supporting-run models", async () => {
    await env.DB.exec(`
        INSERT INTO sessions(id, started_at, state, harness, boundary, repo, model_primary, intent) VALUES ('multi-root', '2026-07-27T11:00:00Z', 'inactive', 'opencode', 'header', 'mimir', 'gpt-5.6-sol', 'Swap models while coding');
        INSERT INTO sessions(id, parent_session_id, started_at, state, harness, boundary, repo, model_primary, intent) VALUES ('multi-child', 'multi-root', '2026-07-27T11:01:00Z', 'inactive', 'opencode', 'header', 'mimir', 'child-only-model', 'Supporting review');
        INSERT INTO exchanges(id, session_id, ts, endpoint, model, latency_ms, r2_key, capture_status) VALUES ('multi-1', 'multi-root', '2026-07-27T11:00:10Z', 'chat', 'gpt-5.6-sol', 1, 'log/multi-1.json', 'saved');
        INSERT INTO exchanges(id, session_id, ts, endpoint, model, latency_ms, r2_key, capture_status) VALUES ('multi-2', 'multi-root', '2026-07-27T11:00:20Z', 'chat', 'claude-opus-5', 1, 'log/multi-2.json', 'saved');
        INSERT INTO exchanges(id, session_id, ts, endpoint, model, latency_ms, r2_key, capture_status) VALUES ('multi-3', 'multi-root', '2026-07-27T11:00:30Z', 'chat', 'gpt-5.6-sol', 1, 'log/multi-3.json', 'saved');
        INSERT INTO exchanges(id, session_id, ts, endpoint, model, latency_ms, r2_key, capture_status) VALUES ('multi-child-1', 'multi-child', '2026-07-27T11:01:10Z', 'chat', 'child-only-model', 1, 'log/multi-child.json', 'saved');
      `);

    type Model = {
      name: string;
      request_count: number;
      first_seen_at: string;
      last_seen_at: string;
    };
    const listed = (await (
      await dashboardRequest(
        "/dashboard/api/sessions?q=claude-opus-5&model=claude-opus-5",
      )
    ).json()) as { sessions: Array<{ id: string; models: Model[] }> };
    expect(listed.sessions).toHaveLength(1);
    expect(listed.sessions[0]).toMatchObject({ id: "multi-root" });
    expect(listed.sessions[0].models).toEqual([
      {
        name: "gpt-5.6-sol",
        request_count: 2,
        first_seen_at: "2026-07-27T11:00:10Z",
        last_seen_at: "2026-07-27T11:00:30Z",
      },
      {
        name: "claude-opus-5",
        request_count: 1,
        first_seen_at: "2026-07-27T11:00:20Z",
        last_seen_at: "2026-07-27T11:00:20Z",
      },
    ]);
    expect(listed.sessions[0].models.map((model) => model.name)).not.toContain(
      "child-only-model",
    );
    expect(
      (
        (await (
          await dashboardRequest(
            "/dashboard/api/sessions?model=child-only-model",
          )
        ).json()) as { sessions: unknown[] }
      ).sessions,
    ).toEqual([]);

    const detail = (await (
      await dashboardRequest("/dashboard/api/sessions/multi-root")
    ).json()) as {
      session: { models: Model[] };
      supporting_sessions: Array<{ id: string; models: Model[] }>;
    };
    expect(detail.session.models.map((model) => model.name)).toEqual([
      "gpt-5.6-sol",
      "claude-opus-5",
    ]);
    expect(detail.supporting_sessions).toEqual([
      expect.objectContaining({
        id: "multi-child",
        models: [
          expect.objectContaining({
            name: "child-only-model",
            request_count: 1,
          }),
        ],
      }),
    ]);
  });

  it("paginates and filters session timelines in either order", async () => {
    await env.DB.exec(`
        INSERT INTO sessions(id, started_at, state, boundary) VALUES ('timeline-root', '2026-07-27T10:00:00Z', 'inactive', 'header');
        INSERT INTO sessions(id, parent_session_id, started_at, state, boundary) VALUES ('timeline-child', 'timeline-root', '2026-07-27T10:01:00Z', 'inactive', 'header');
        INSERT INTO sessions(id, started_at, state, boundary) VALUES ('timeline-other', '2026-07-27T10:00:00Z', 'inactive', 'header');
        INSERT INTO exchanges(id, session_id, ts, endpoint, model, request_excerpt, latency_ms, harness, r2_key, provider, finish_reason, capture_status) VALUES ('exchange-1', 'timeline-root', '2026-07-27T10:00:00Z', 'chat', 'model-a', 'first matching request', 1, 'opencode', 'log/1.json', 'provider-a', 'stop', 'saved');
        INSERT INTO exchanges(id, session_id, ts, endpoint, model, request_excerpt, latency_ms, harness, r2_key, provider, finish_reason, capture_status) VALUES ('exchange-2a', 'timeline-child', '2026-07-27T10:01:00Z', 'chat', 'model-a', 'second matching request', 1, 'opencode', 'log/2a.json', 'provider-a', 'stop', 'saved');
        INSERT INTO exchanges(id, session_id, ts, endpoint, model, request_excerpt, latency_ms, harness, r2_key, provider, finish_reason, capture_status) VALUES ('exchange-2b', 'timeline-child', '2026-07-27T10:01:00Z', 'chat', 'model-b', 'different request', 1, 'hermes', 'log/2b.json', 'provider-b', 'length', 'saved');
        INSERT INTO exchanges(id, session_id, ts, endpoint, model, request_excerpt, latency_ms, harness, r2_key, provider, finish_reason, capture_status) VALUES ('exchange-3', 'timeline-root', '2026-07-27T10:02:00Z', 'chat', 'model-a', 'third matching request', 1, 'opencode', 'log/3.json', 'provider-a', 'stop', 'saved');
        INSERT INTO exchanges(id, session_id, ts, endpoint, model, request_excerpt, latency_ms, harness, r2_key, provider, finish_reason, capture_status) VALUES ('other-exchange', 'timeline-other', '2026-07-27T10:03:00Z', 'chat', 'model-a', 'outside subtree', 1, 'opencode', 'log/other.json', 'provider-a', 'stop', 'saved');
      `);

    const first = (await (
      await dashboardRequest(
        "/dashboard/api/sessions/timeline-root/exchanges?limit=2",
      )
    ).json()) as {
      exchanges: Array<{ id: string }>;
      next_cursor: string | null;
    };
    expect(first.exchanges.map((exchange) => exchange.id)).toEqual([
      "exchange-3",
      "exchange-2b",
    ]);
    expect(first.next_cursor).toEqual(expect.any(String));
    const second = (await (
      await dashboardRequest(
        `/dashboard/api/sessions/timeline-root/exchanges?limit=2&cursor=${encodeURIComponent(first.next_cursor!)}`,
      )
    ).json()) as {
      exchanges: Array<{ id: string }>;
      next_cursor: string | null;
    };
    expect(second.exchanges.map((exchange) => exchange.id)).toEqual([
      "exchange-2a",
      "exchange-1",
    ]);
    expect(second.next_cursor).toBeNull();

    const scoped = (await (
      await dashboardRequest(
        "/dashboard/api/sessions/timeline-root/exchanges?session=timeline-child",
      )
    ).json()) as {
      exchanges: Array<{ id: string }>;
    };
    expect(scoped.exchanges.map((exchange) => exchange.id)).toEqual([
      "exchange-2b",
      "exchange-2a",
    ]);

    const ascending = (await (
      await dashboardRequest(
        "/dashboard/api/sessions/timeline-root/exchanges?order=asc&limit=2",
      )
    ).json()) as {
      exchanges: Array<{ id: string }>;
      next_cursor: string | null;
    };
    expect(ascending.exchanges.map((exchange) => exchange.id)).toEqual([
      "exchange-1",
      "exchange-2a",
    ]);
    expect(
      (
        await dashboardRequest(
          `/dashboard/api/sessions/timeline-root/exchanges?cursor=${encodeURIComponent(ascending.next_cursor!)}`,
        )
      ).status,
    ).toBe(400);
    const filtered = (await (
      await dashboardRequest(
        "/dashboard/api/sessions/timeline-root/exchanges?q=matching&model=model-a&provider=provider-a&app=opencode&finish_reason=stop&limit=1",
      )
    ).json()) as {
      exchanges: Array<{ id: string }>;
      next_cursor: string | null;
    };
    expect(filtered.exchanges.map((exchange) => exchange.id)).toEqual([
      "exchange-3",
    ]);
    expect(filtered.next_cursor).toEqual(expect.any(String));
    expect(
      (
        await dashboardRequest(
          "/dashboard/api/sessions/timeline-root/exchanges?order=sideways",
        )
      ).status,
    ).toBe(400);
    expect(
      (
        await dashboardRequest(
          "/dashboard/api/sessions/timeline-root/exchanges?cursor=not-a-cursor",
        )
      ).status,
    ).toBe(400);
  });

  it("aggregates session error counts, recency, and request links from saved exchanges", async () => {
    await env.DB.exec(`
        INSERT INTO sessions(id, started_at, state, boundary) VALUES ('errors-root', '2026-07-27T10:00:00Z', 'inactive', 'header');
        INSERT INTO sessions(id, parent_session_id, started_at, state, boundary) VALUES ('errors-child', 'errors-root', '2026-07-27T10:01:00Z', 'inactive', 'header');
        INSERT INTO exchanges(id, session_id, ts, endpoint, latency_ms, r2_key, capture_status, saved_at) VALUES ('err-1', 'errors-root', '2026-07-27T10:00:30Z', 'chat', 1, 'log/err-1.json', 'saved', '2026-07-27T10:00:31Z');
        INSERT INTO exchanges(id, session_id, ts, endpoint, latency_ms, r2_key, capture_status, saved_at) VALUES ('err-2', 'errors-child', '2026-07-27T10:01:30Z', 'chat', 1, 'log/err-2.json', 'saved', '2026-07-27T10:01:31Z');
        INSERT INTO exchanges(id, session_id, ts, endpoint, latency_ms, r2_key, capture_status, accepted_at) VALUES ('err-3', 'errors-root', '2026-07-27T10:02:30Z', 'chat', 1, 'log/err-3.json', 'accepted', '2026-07-27T10:02:30Z');
        INSERT INTO exchange_errors(exchange_id, session_id, signature) VALUES ('err-1', 'errors-root', 'token expired');
        INSERT INTO exchange_errors(exchange_id, session_id, signature) VALUES ('err-2', 'errors-child', 'token expired');
        INSERT INTO exchange_errors(exchange_id, session_id, signature) VALUES ('err-2', 'errors-child', 'disk full');
        INSERT INTO exchange_errors(exchange_id, session_id, signature) VALUES ('err-3', 'errors-root', 'pending only');
        INSERT INTO session_errors(session_id, signature) VALUES ('errors-root', 'token expired');
        INSERT INTO session_errors(session_id, signature) VALUES ('errors-child', 'token expired');
        INSERT INTO session_errors(session_id, signature) VALUES ('errors-child', 'disk full');
        INSERT INTO session_errors(session_id, signature) VALUES ('errors-root', 'legacy signature');
      `);

    const detail = (await (
      await dashboardRequest("/dashboard/api/sessions/errors-root")
    ).json()) as {
      errors: Array<{
        signature: string;
        count: number;
        first_seen_at: string | null;
        last_seen_at: string | null;
        latest_exchange_id: string | null;
      }>;
    };
    expect(detail.errors).toEqual([
      {
        signature: "disk full",
        count: 1,
        first_seen_at: "2026-07-27T10:01:30Z",
        last_seen_at: "2026-07-27T10:01:30Z",
        latest_exchange_id: "err-2",
      },
      {
        signature: "legacy signature",
        count: 1,
        first_seen_at: null,
        last_seen_at: null,
        latest_exchange_id: null,
      },
      {
        signature: "token expired",
        count: 2,
        first_seen_at: "2026-07-27T10:00:30Z",
        last_seen_at: "2026-07-27T10:01:30Z",
        latest_exchange_id: "err-2",
      },
    ]);
    expect(detail.errors.map((error) => error.signature)).not.toContain(
      "pending only",
    );
  });

  it("serves live dashboard sessions, requests, objects, overview, and outcome updates", async () => {
    await env.DB.prepare(
      "INSERT INTO sessions(id, started_at, ended_at, state, last_active_at, harness, boundary, repo, source_ref, model_primary, request_count, tokens_in, tokens_out, intent) VALUES ('dashboard-session', '2099-01-01T00:00:00Z', '2099-01-01T00:01:00Z', 'inactive', '2099-01-01T00:01:00Z', 'OpenCode', 'header', 'mimir', 'master', 'openai/test', 1, 7, 3, 'Connect live dashboard data')",
    ).run();
    await env.DB.prepare(
      "INSERT INTO exchanges(id, session_id, ts, endpoint, model, latency_ms, repo, harness, r2_key, provider, finish_reason, access_token_label, input_tokens, output_tokens, capture_status, capture_reason, accepted_at, saved_at) VALUES ('dashboard-exchange', 'dashboard-session', '2099-01-01T00:00:30Z', '/v1/chat/completions', 'openai/test', 250, 'mimir', 'OpenCode', 'log/2099/01/01/dashboard-exchange.json', 'OpenAI', 'stop', 'test', 7, 3, 'saved', 'enabled', '2099-01-01T00:00:30Z', '2099-01-01T00:00:31Z')",
    ).run();
    await env.DB.prepare(
      "INSERT INTO session_files(session_id, file) VALUES ('dashboard-session', 'worker/web/src/lib/api.ts')",
    ).run();
    await env.DB.prepare(
      "INSERT INTO session_errors(session_id, signature) VALUES ('dashboard-session', 'example failure')",
    ).run();
    const envelope = {
      schema_version: 1,
      exchange_id: "dashboard-exchange",
      session_id: "dashboard-session",
      captured_at: "2099-01-01T00:00:30Z",
      endpoint: "/v1/chat/completions",
      request: {
        messages: [{ role: "user", content: "Connect live dashboard data" }],
      },
      response: { format: "json", body: { choices: [] } },
    };
    await env.LOGS.put(
      "log/2099/01/01/dashboard-exchange.json",
      JSON.stringify(envelope),
    );

    const sessions = (await (
      await dashboardRequest("/dashboard/api/sessions")
    ).json()) as {
      sessions: Array<{ id: string; capture: { status: string } }>;
    };
    expect(sessions.sessions).toContainEqual(
      expect.objectContaining({
        id: "dashboard-session",
        capture: expect.objectContaining({ status: "saved" }),
      }),
    );

    const session = (await (
      await dashboardRequest("/dashboard/api/sessions/dashboard-session")
    ).json()) as {
      files: string[];
      errors: Array<{
        signature: string;
        count: number;
        latest_exchange_id: string | null;
      }>;
    } & Record<string, unknown>;
    expect(session.files).toEqual(["worker/web/src/lib/api.ts"]);
    expect(session.errors).toEqual([
      {
        signature: "example failure",
        count: 1,
        first_seen_at: null,
        last_seen_at: null,
        latest_exchange_id: null,
      },
    ]);
    expect(session).not.toHaveProperty("exchanges");
    const timeline = (await (
      await dashboardRequest(
        "/dashboard/api/sessions/dashboard-session/exchanges",
      )
    ).json()) as { exchanges: Array<{ id: string }> };
    expect(timeline.exchanges).toContainEqual(
      expect.objectContaining({ id: "dashboard-exchange" }),
    );

    const log = (await (
      await dashboardRequest("/dashboard/api/log?limit=50")
    ).json()) as {
      exchanges: Array<{ id: string }>;
      next_cursor: string | null;
    };
    expect(log.exchanges).toContainEqual(
      expect.objectContaining({ id: "dashboard-exchange" }),
    );
    expect(log.next_cursor).toBeNull();

    const detail = (await (
      await dashboardRequest("/dashboard/api/log/dashboard-exchange")
    ).json()) as { log_url: string };
    expect(detail.log_url).toBe(
      "/dashboard/log-objects/log/2099/01/01/dashboard-exchange.json",
    );
    expect(await (await dashboardRequest(detail.log_url)).json()).toEqual(
      envelope,
    );

    const overview = (await (
      await dashboardRequest("/dashboard/api/overview")
    ).json()) as {
      totals: { requests: number; sessions: number; saved_exchanges: number };
      models: Array<{ name: string }>;
    };
    expect(overview.totals).toMatchObject({
      requests: 1,
      sessions: 1,
      saved_exchanges: 1,
    });
    expect(overview.models).toContainEqual(
      expect.objectContaining({ name: "openai/test" }),
    );

    const updated = await dashboardRequest(
      "/dashboard/api/sessions/dashboard-session/outcome",
      {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({
          outcome: "landed",
          reason: "Live data verified",
        }),
      },
    );
    expect(updated.status).toBe(200);
    expect(await updated.json()).toMatchObject({
      id: "dashboard-session",
      outcome: "landed",
      outcome_src: "user",
      outcome_reason: "Live data verified",
    });
  });

  it("updates selected root outcomes atomically", async () => {
    await env.DB.exec(`
      INSERT INTO sessions(id, started_at, state, boundary) VALUES ('bulk-a', '2026-09-01T00:00:00Z', 'inactive', 'header');
      INSERT INTO sessions(id, started_at, state, boundary) VALUES ('bulk-b', '2026-09-01T00:00:00Z', 'inactive', 'header');
      INSERT INTO sessions(id, parent_session_id, started_at, state, boundary) VALUES ('bulk-child', 'bulk-a', '2026-09-01T00:00:01Z', 'inactive', 'header');
    `);
    const response = await dashboardRequest("/dashboard/api/sessions/outcomes", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({
        session_ids: ["bulk-a", "bulk-b", "bulk-a"],
        outcome: "discarded",
        reason: "Superseded",
      }),
    });
    expect(response.status).toBe(200);
    expect(await response.json()).toMatchObject({
      updated: [
        { id: "bulk-a", outcome: "discarded" },
        { id: "bulk-b", outcome: "discarded" },
      ],
    });
    expect(
      await env.DB.prepare(
        "SELECT COUNT(*) AS count FROM session_outcome_events WHERE session_id IN ('bulk-a', 'bulk-b') AND source = 'user'",
      ).first(),
    ).toEqual({ count: 2 });

    const rejected = await dashboardRequest(
      "/dashboard/api/sessions/outcomes",
      {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({
          session_ids: ["bulk-a", "bulk-child"],
          outcome: "landed",
        }),
      },
    );
    expect(rejected.status).toBe(400);
    expect(
      await env.DB.prepare(
        "SELECT work_outcome FROM sessions WHERE id = 'bulk-a'",
      ).first(),
    ).toEqual({ work_outcome: "discarded" });
  });

  it("auto-resolves stale sessions only with retrievable commit evidence", async () => {
    const artifact = (session: string, commit: string, key: string) =>
      env.DB.prepare(
        "INSERT INTO session_git_artifacts(session_id, commit_sha, provenance, patch_r2_key, patch_sha256, patch_bytes, patch_files, patch_additions, patch_deletions, capture_status, accepted_at, saved_at, created_at) VALUES (?, ?, 'git', ?, ?, 10, 1, 1, 0, 'saved', '2026-09-01T00:00:00Z', '2026-09-01T00:00:01Z', '2026-09-01T00:00:00Z')",
      ).bind(session, commit, key, "f".repeat(64));
    await env.DB.batch([
      env.DB.prepare(
        "INSERT INTO sessions(id, started_at, last_active_at, state, boundary) VALUES ('auto-root', '2026-09-01T00:00:00Z', '2026-09-01T00:00:00Z', 'inactive', 'header')",
      ),
      env.DB.prepare(
        "INSERT INTO sessions(id, started_at, last_active_at, state, boundary, outcome_src) VALUES ('manual-root', '2026-09-01T00:00:00Z', '2026-09-01T00:00:00Z', 'inactive', 'header', 'user')",
      ),
      env.DB.prepare(
        "INSERT INTO sessions(id, started_at, last_active_at, state, boundary) VALUES ('recent-tree', '2026-09-01T00:00:00Z', '2026-09-01T00:00:00Z', 'inactive', 'header')",
      ),
      env.DB.prepare(
        "INSERT INTO sessions(id, parent_session_id, started_at, last_active_at, state, boundary) VALUES ('recent-child', 'recent-tree', '2026-09-03T12:00:00Z', '2026-09-03T12:00:00Z', 'inactive', 'header')",
      ),
      artifact("auto-root", "a".repeat(40), "sessions/auto-root/git/a/patch.patch"),
      artifact("manual-root", "b".repeat(40), "sessions/manual-root/git/b/patch.patch"),
      artifact("recent-tree", "c".repeat(40), "sessions/recent-tree/git/c/patch.patch"),
    ]);
    await Promise.all([
      env.LOGS.put("sessions/auto-root/git/a/patch.patch", "patch"),
      env.LOGS.put("sessions/manual-root/git/b/patch.patch", "patch"),
      env.LOGS.put("sessions/recent-tree/git/c/patch.patch", "patch"),
    ]);

    await expect(
      autoResolveStaleOutcomes(env, "2026-09-04T00:00:00Z"),
    ).resolves.toEqual({ count: 1, session_ids: ["auto-root"] });
    expect(
      await env.DB.prepare(
        "SELECT work_outcome, outcome_src FROM sessions WHERE id = 'auto-root'",
      ).first(),
    ).toEqual({ work_outcome: "landed", outcome_src: "auto" });
    expect(
      await env.DB.prepare(
        "SELECT work_outcome FROM sessions WHERE id IN ('manual-root', 'recent-tree') ORDER BY id",
      ).all(),
    ).toMatchObject({
      results: [
        { work_outcome: "unresolved" },
        { work_outcome: "unresolved" },
      ],
    });
    await expect(
      autoResolveStaleOutcomes(env, "2026-09-04T00:00:00Z"),
    ).resolves.toEqual({ count: 0, session_ids: [] });
  });

  it("serves filter facets from saved traffic, ordered by frequency and scoped on request", async () => {
    await env.DB.batch([
      env.DB.prepare(
        "INSERT INTO sessions(id, started_at, state, last_active_at, boundary, repo, harness, model_primary) VALUES ('facet-root', '2026-05-01T00:00:00Z', 'inactive', '2026-05-01T00:00:00Z', 'header', 'mimir', 'OpenCode', 'anthropic/claude')",
      ),
      env.DB.prepare(
        "INSERT INTO sessions(id, parent_session_id, started_at, state, last_active_at, boundary, repo) VALUES ('facet-child', 'facet-root', '2026-05-01T00:01:00Z', 'inactive', '2026-05-01T00:01:00Z', 'header', 'mimir')",
      ),
      env.DB.prepare(
        "INSERT INTO sessions(id, started_at, state, last_active_at, boundary, repo, harness) VALUES ('facet-other', '2026-05-01T00:02:00Z', 'inactive', '2026-05-01T00:02:00Z', 'header', 'other-repo', 'Hermes')",
      ),
      env.DB.prepare(
        "INSERT INTO exchanges(id, session_id, ts, endpoint, model, latency_ms, r2_key, provider, harness, finish_reason, capture_status, saved_at) VALUES ('facet-1', 'facet-root', '2026-05-01T00:00:10Z', 'chat', 'anthropic/claude', 1, 'log/f1.json', 'anthropic', 'OpenCode', 'stop', 'saved', '2026-05-01T00:00:11Z')",
      ),
      env.DB.prepare(
        "INSERT INTO exchanges(id, session_id, ts, endpoint, model, latency_ms, r2_key, provider, harness, finish_reason, capture_status, saved_at) VALUES ('facet-2', 'facet-child', '2026-05-01T00:01:10Z', 'chat', 'anthropic/claude', 1, 'log/f2.json', 'anthropic', 'OpenCode', 'tool-calls', 'saved', '2026-05-01T00:01:11Z')",
      ),
      env.DB.prepare(
        "INSERT INTO exchanges(id, session_id, ts, endpoint, model, latency_ms, r2_key, provider, harness, finish_reason, capture_status, saved_at) VALUES ('facet-3', 'facet-other', '2026-05-01T00:02:10Z', 'chat', 'openai/gpt', 1, 'log/f3.json', 'openai', 'Hermes', 'stop', 'saved', '2026-05-01T00:02:11Z')",
      ),
      env.DB.prepare(
        "INSERT INTO exchanges(id, session_id, ts, endpoint, model, latency_ms, r2_key, provider, harness, finish_reason, capture_status) VALUES ('facet-pending', 'facet-other', '2026-05-01T00:03:10Z', 'chat', 'never/surfaced', 1, 'log/f4.json', 'ghost-provider', 'Ghost', 'length', 'pending')",
      ),
    ]);

    type Facets = {
      repos: string[];
      apps: string[];
      models: string[];
      providers: string[];
      finish_reasons: string[];
    };
    const facets = (await (
      await dashboardRequest("/dashboard/api/facets")
    ).json()) as Facets;
    expect(facets.models).toContain("anthropic/claude");
    expect(facets.models).not.toContain("never/surfaced");
    expect(facets.providers).not.toContain("ghost-provider");
    expect(facets.repos).toContain("mimir");
    expect(facets.repos).toContain("other-repo");
    expect(facets.models.indexOf("anthropic/claude")).toBeLessThan(
      facets.models.indexOf("openai/gpt"),
    );

    const scoped = (await (
      await dashboardRequest("/dashboard/api/facets?session=facet-root")
    ).json()) as Facets;
    expect(scoped.models).toEqual(["anthropic/claude"]);
    expect([...scoped.finish_reasons].sort()).toEqual(["stop", "tool-calls"]);
    expect(scoped.apps).toEqual(["OpenCode"]);
    expect(scoped.repos).toEqual([]);
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
        if (url === `${teamDomain}/cdn-cgi/access/certs`)
          return Promise.resolve(Response.json({ keys: [jwk] }));
        return Promise.reject(new Error(`unexpected fetch ${url}`));
      }),
    );
    const bindings = env as Env & {
      DASHBOARD_ACCESS_AUD?: string;
      DASHBOARD_ACCESS_TEAM_DOMAIN?: string;
    };
    bindings.DASHBOARD_ACCESS_AUD = "test-aud";
    bindings.DASHBOARD_ACCESS_TEAM_DOMAIN = teamDomain;
    const sign = (
      init: { audience?: string; issuer?: string; expiration?: number } = {},
    ) =>
      new SignJWT({
        email: " dashboard@example.com ",
        name: "Dashboard User",
        unsafe: "not exposed",
      })
        .setProtectedHeader({ alg: "RS256", kid: "test-key" })
        .setIssuer(init.issuer ?? teamDomain)
        .setAudience(init.audience ?? "test-aud")
        .setIssuedAt()
        .setExpirationTime(
          init.expiration ?? Math.floor(Date.now() / 1000) + 300,
        )
        .sign(privateKey);
    try {
      const valid = await request("/dashboard/api/bootstrap", {
        headers: { "cf-access-jwt-assertion": await sign() },
      });
      expect(valid.status).toBe(200);
      const identity = await request("/dashboard/api/identity", {
        headers: { "cf-access-jwt-assertion": await sign() },
      });
      expect(await identity.json()).toEqual({
        email: "dashboard@example.com",
        name: "Dashboard User",
        source: "cloudflare-access",
      });
      const wrongAudience = await request("/dashboard/api/bootstrap", {
        headers: {
          "cf-access-jwt-assertion": await sign({ audience: "other-aud" }),
        },
      });
      expect(wrongAudience.status).toBe(403);
      const wrongIssuer = await request("/dashboard/api/bootstrap", {
        headers: {
          "cf-access-jwt-assertion": await sign({
            issuer: "https://evil.example.com",
          }),
        },
      });
      expect(wrongIssuer.status).toBe(403);
      const expired = await request("/dashboard/api/bootstrap", {
        headers: {
          "cf-access-jwt-assertion": await sign({
            expiration: Math.floor(Date.now() / 1000) - 300,
          }),
        },
      });
      expect(expired.status).toBe(403);
      const garbage = await request("/dashboard/api/bootstrap", {
        headers: { "cf-access-jwt-assertion": "not-a-jwt" },
      });
      expect(garbage.status).toBe(403);
    } finally {
      delete bindings.DASHBOARD_ACCESS_AUD;
      delete bindings.DASHBOARD_ACCESS_TEAM_DOMAIN;
    }
  });
});
