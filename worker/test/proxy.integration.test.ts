import { describe, expect, it, vi } from "vitest";
import {
  addMachineToken,
  createExecutionContext,
  env,
  finalizeAcceptedExchange,
  request,
  waitOnExecutionContext,
  worker,
} from "./support";

describe("Proxy integration", () => {
  it("streams unchanged and persists redacted session data", async () => {
    const stream =
      'data: {"choices":[{"delta":{"content":"src/auth.ts failed: boom"}}]}\n\ndata: {"usage":{"prompt_tokens":5,"completion_tokens":3}}\n\ndata: [DONE]\n';
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
        authorization: "Bearer machine-token",
        "content-type": "application/json",
        "x-mimir-session": "session-1",
        "x-mimir-repo": "mimir",
        "x-mimir-harness": "test",
      },
      body: JSON.stringify({
        model: "openai/test",
        messages: [
          { role: "user", content: "token: private-value" },
          {
            role: "assistant",
            content: [
              { type: "tool_use", input: { file_path: "src/auth.ts" } },
            ],
          },
        ],
        stream: true,
      }),
    });
    expect(response.headers.get("x-mimir-capture")).toBe("scheduled");
    expect(response.headers.get("x-mimir-capture-reason")).toBe("enabled");
    expect(await response.text()).toBe(stream);
    const upstream = vi.mocked(fetch).mock.calls[0];
    const upstreamHeaders = new Headers((upstream[1] as RequestInit).headers);
    expect(upstreamHeaders.get("authorization")).toBe(
      "Bearer test-openrouter-key",
    );
    expect(upstreamHeaders.get("x-mimir-session")).toBeNull();
    const session = await env.DB.prepare(
      "SELECT request_count, tokens_in, tokens_out FROM sessions WHERE id = 'session-1'",
    ).first<{ request_count: number; tokens_in: number; tokens_out: number }>();
    expect(session).toEqual({ request_count: 1, tokens_in: 5, tokens_out: 3 });
    expect(
      await env.DB.prepare(
        "SELECT file FROM session_files WHERE session_id = 'session-1'",
      ).first<{ file: string }>(),
    ).toEqual({ file: "src/auth.ts" });
    const exchange = await env.DB.prepare(
      "SELECT id, r2_key, input_tokens, output_tokens, access_token_label, capture_status, capture_reason, schema_version, r2_bytes FROM exchanges WHERE session_id = 'session-1'",
    ).first<{
      id: string;
      r2_key: string;
      input_tokens: number;
      output_tokens: number;
      access_token_label: string;
      capture_status: string;
      capture_reason: string;
      schema_version: number;
      r2_bytes: number;
    }>();
    expect(exchange?.r2_key).toMatch(/^log\//);
    expect(exchange).toMatchObject({
      input_tokens: 5,
      output_tokens: 3,
      access_token_label: "test",
      capture_status: "saved",
      capture_reason: "enabled",
      schema_version: 1,
    });
    const object = await env.LOGS.get(exchange!.r2_key);
    const objectText = await object!.text();
    expect(objectText).not.toContain("private-value");
    expect(exchange?.r2_bytes).toBe(
      new TextEncoder().encode(objectText).byteLength,
    );
    const envelope = JSON.parse(objectText);
    expect(envelope).toMatchObject({
      schema_version: 1,
      exchange_id: exchange?.id,
      session_id: "session-1",
      declared_session_id: "session-1",
      response: {
        format: "reconstructed_sse",
        content: "src/auth.ts failed: boom",
      },
      usage: { input_tokens: 5, output_tokens: 3 },
      redaction: { version: 1 },
    });
    expect(
      await env.DB.prepare("SELECT e.search_indexed, s.body FROM exchanges e JOIN exchange_search s ON s.exchange_id = e.id WHERE e.id = ?")
        .bind(exchange!.id)
        .first(),
    ).toEqual({ search_indexed: 1, body: "src/auth.ts failed: boom" });
  });

  it("uses only primary requests to establish session intent", async () => {
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
    const headers = {
      authorization: "Bearer machine-token",
      "content-type": "application/json",
      "x-mimir-session": "intent-session",
      "x-mimir-harness": "opencode",
    };
    await request("/v1/chat/completions", {
      method: "POST",
      headers: { ...headers, "x-mimir-request-kind": "title" },
      body: JSON.stringify({
        model: "openai/test",
        messages: [
          { role: "user", content: "Generate a title for this conversation:" },
        ],
      }),
    });
    expect(
      await env.DB.prepare(
        "SELECT intent FROM sessions WHERE id = 'intent-session'",
      ).first(),
    ).toEqual({ intent: null });
    await request("/v1/chat/completions", {
      method: "POST",
      headers: { ...headers, "x-mimir-request-kind": "primary" },
      body: JSON.stringify({
        model: "openai/test",
        messages: [{ role: "user", content: "Fix session intent handling" }],
      }),
    });
    expect(
      await env.DB.prepare(
        "SELECT intent FROM sessions WHERE id = 'intent-session'",
      ).first(),
    ).toEqual({ intent: "Fix session intent handling" });
    expect(
      (
        await env.DB.prepare(
          "SELECT request_kind FROM exchanges WHERE session_id = 'intent-session' ORDER BY ts, id",
        ).all()
      ).results.map((row) => row.request_kind),
    ).toEqual(["title", "primary"]);
  });

  it("defensively classifies title prompts and rejects invalid request kinds", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(Response.json({ choices: [] })),
    );
    await request("/v1/chat/completions", {
      method: "POST",
      headers: {
        authorization: "Bearer machine-token",
        "content-type": "application/json",
        "x-mimir-session": "defensive-title",
        "x-mimir-request-kind": "primary",
      },
      body: JSON.stringify({
        model: "openai/test",
        messages: [
          {
            role: "system",
            content: "You are a title generator. Output only a title.",
          },
          { role: "user", content: "Generate a title" },
        ],
      }),
    });
    expect(
      await env.DB.prepare(
        "SELECT intent FROM sessions WHERE id = 'defensive-title'",
      ).first(),
    ).toEqual({ intent: null });
    expect(
      await env.DB.prepare(
        "SELECT request_kind FROM exchanges WHERE session_id = 'defensive-title'",
      ).first(),
    ).toEqual({ request_kind: "title" });
    const invalid = await request("/v1/chat/completions", {
      method: "POST",
      headers: {
        authorization: "Bearer machine-token",
        "content-type": "application/json",
        "x-mimir-request-kind": "background",
      },
      body: JSON.stringify({ model: "openai/test" }),
    });
    expect(invalid.status).toBe(400);
  });

  it("records an accepted exchange before the upstream archive finishes", async () => {
    let closeStream: (() => void) | undefined;
    const upstream = new ReadableStream<Uint8Array>({
      start(controller) {
        controller.enqueue(new TextEncoder().encode('{"choices":['));
        closeStream = () => {
          controller.enqueue(new TextEncoder().encode("]}"));
          controller.close();
        };
      },
    });
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockResolvedValue(
          new Response(upstream, {
            headers: { "content-type": "application/json" },
          }),
        ),
    );
    const ctx = createExecutionContext();
    const response = await worker.fetch(
      new Request("https://mimir.test/v1/chat/completions", {
        method: "POST",
        headers: {
          authorization: "Bearer machine-token",
          "content-type": "application/json",
          "x-mimir-session": "early-accepted",
        },
        body: JSON.stringify({ model: "openai/test", messages: [] }),
      }),
      env as Env & { OPENROUTER_API_KEY: string },
      ctx,
    );

    await vi.waitFor(async () => {
      expect(
        await env.DB.prepare(
          "SELECT capture_status FROM exchanges WHERE session_id = 'early-accepted'",
        ).first(),
      ).toEqual({ capture_status: "accepted" });
    });
    closeStream?.();
    await response.text();
    await waitOnExecutionContext(ctx);
    expect(
      await env.DB.prepare(
        "SELECT capture_status FROM exchanges WHERE session_id = 'early-accepted'",
      ).first(),
    ).toEqual({ capture_status: "saved" });
  });

  it("does not persist when saving is disabled", async () => {
    await env.DB.prepare(
      "INSERT INTO config(key, value) VALUES('save.enabled', 'false')",
    ).run();
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(Response.json({ choices: [] })),
    );
    const response = await request("/v1/chat/completions", {
      method: "POST",
      headers: {
        "x-api-key": "machine-token",
        "content-type": "application/json",
      },
      body: JSON.stringify({ model: "openai/test", messages: [] }),
    });
    expect(response.status).toBe(200);
    expect(response.headers.get("x-mimir-capture")).toBe("skipped");
    expect(response.headers.get("x-mimir-capture-reason")).toBe("disabled");
    expect(
      (
        await env.DB.prepare("SELECT COUNT(*) AS count FROM exchanges").first<{
          count: number;
        }>()
      )?.count,
    ).toBe(0);
    expect((await env.LOGS.list()).objects).toHaveLength(0);
  });

  it("reports excluded and bodyless capture decisions", async () => {
    await env.DB.prepare(
      "INSERT INTO config(key, value) VALUES('save.exclude_repos', '[\"private-*\"]')",
    ).run();
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(Response.json({ choices: [] })),
    );
    const excluded = await request("/v1/chat/completions", {
      method: "POST",
      headers: {
        authorization: "Bearer machine-token",
        "content-type": "application/json",
        "x-mimir-repo": "private-repo",
      },
      body: JSON.stringify({ model: "openai/test" }),
    });
    expect(excluded.headers.get("x-mimir-capture-reason")).toBe(
      "excluded_repository",
    );
    await env.DB.prepare("DELETE FROM config").run();
    vi.mocked(fetch).mockResolvedValueOnce(new Response(null, { status: 204 }));
    const bodyless = await request("/v1/chat/completions", {
      method: "POST",
      headers: {
        authorization: "Bearer machine-token",
        "content-type": "application/json",
      },
      body: JSON.stringify({ model: "openai/test" }),
    });
    expect(bodyless.headers.get("x-mimir-capture")).toBe("skipped");
    expect(bodyless.headers.get("x-mimir-capture-reason")).toBe(
      "missing_response_body",
    );
  });

  it("leaves an R2 write failure failed without session aggregates", async () => {
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockResolvedValue(
          Response.json({
            choices: [],
            usage: { prompt_tokens: 9, completion_tokens: 4 },
          }),
        ),
    );
    vi.spyOn(env.LOGS, "put").mockRejectedValueOnce(
      new Error("injected R2 failure"),
    );
    const response = await request("/v1/chat/completions", {
      method: "POST",
      headers: {
        authorization: "Bearer machine-token",
        "content-type": "application/json",
        "x-mimir-session": "failed-session",
      },
      body: JSON.stringify({ model: "openai/test" }),
    });
    expect(response.headers.get("x-mimir-capture")).toBe("scheduled");
    expect(
      await env.DB.prepare(
        "SELECT capture_status, failure_code FROM exchanges WHERE session_id = 'failed-session'",
      ).first(),
    ).toEqual({ capture_status: "failed", failure_code: "r2_write_failed" });
    expect(
      await env.DB.prepare(
        "SELECT request_count, tokens_in, tokens_out FROM sessions WHERE id = 'failed-session'",
      ).first(),
    ).toEqual({ request_count: 0, tokens_in: 0, tokens_out: 0 });
  });

  it("leaves an accepted exchange for reconciliation when D1 finalization fails", async () => {
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockResolvedValue(
          Response.json({
            choices: [],
            usage: { prompt_tokens: 6, completion_tokens: 2 },
          }),
        ),
    );
    const batch = env.DB.batch.bind(env.DB);
    let captureBatch = 0;
    vi.spyOn(env.DB, "batch").mockImplementation((statements) => {
      captureBatch += 1;
      return captureBatch === 3
        ? Promise.reject(new Error("injected D1 finalization failure"))
        : batch(statements);
    });
    await request("/v1/chat/completions", {
      method: "POST",
      headers: {
        authorization: "Bearer machine-token",
        "content-type": "application/json",
        "x-mimir-session": "accepted-session",
      },
      body: JSON.stringify({
        model: "openai/test",
        messages: [
          { role: "user", content: "Inspect src/recovered.ts" },
          {
            role: "assistant",
            content: [
              { type: "tool_use", input: { path: "src/recovered.ts" } },
            ],
          },
        ],
      }),
    });
    const accepted = await env.DB.prepare(
      "SELECT id, r2_key, capture_status FROM exchanges WHERE session_id = 'accepted-session'",
    ).first<{ id: string; r2_key: string; capture_status: string }>();
    expect(accepted?.capture_status).toBe("accepted");
    expect(await env.LOGS.get(accepted!.r2_key)).not.toBeNull();
    expect(
      await env.DB.prepare(
        "SELECT request_count FROM sessions WHERE id = 'accepted-session'",
      ).first(),
    ).toEqual({ request_count: 0 });

    await request("/reconcile", {
      method: "POST",
      headers: { authorization: "Bearer machine-token" },
    });
    expect(
      await env.DB.prepare("SELECT capture_status FROM exchanges WHERE id = ?")
        .bind(accepted!.id)
        .first(),
    ).toEqual({ capture_status: "saved" });
    expect(
      await env.DB.prepare(
        "SELECT request_count, tokens_in, tokens_out, intent FROM sessions WHERE id = 'accepted-session'",
      ).first(),
    ).toEqual({
      request_count: 1,
      tokens_in: 6,
      tokens_out: 2,
      intent: "Inspect src/recovered.ts",
    });
    expect(
      await env.DB.prepare(
        "SELECT file FROM session_files WHERE session_id = 'accepted-session'",
      ).first(),
    ).toEqual({ file: "src/recovered.ts" });
  });

  it("preserves historical session intent without persisted candidates", async () => {
    await env.DB.prepare(
      "INSERT INTO sessions(id, started_at, ended_at, state, last_active_at, boundary, intent) VALUES ('historical-intent', '2026-01-01T00:00:00Z', '2026-01-01T00:01:00Z', 'active', '2026-01-01T00:01:00Z', 'header', 'Historical user request')",
    ).run();
    await env.DB.prepare(
      "INSERT INTO exchanges(id, session_id, ts, endpoint, model, latency_ms, r2_key, capture_status, accepted_at, request_kind, intent_candidate) VALUES ('new-historical-exchange', 'historical-intent', '2026-01-02T00:00:00Z', 'chat', 'openai/test', 1, 'log/new-historical.json', 'accepted', '2026-01-02T00:00:00Z', 'primary', 'New user request')",
    ).run();
    await env.LOGS.put("log/new-historical.json", "{}");

    await request("/reconcile", {
      method: "POST",
      headers: { authorization: "Bearer machine-token" },
    });

    expect(
      await env.DB.prepare(
        "SELECT intent FROM sessions WHERE id = 'historical-intent'",
      ).first(),
    ).toEqual({ intent: "Historical user request" });
  });

  it("does not reactivate a session when pre-end capture finishes late", async () => {
    await env.DB.prepare(
      "INSERT INTO sessions(id, started_at, ended_at, state, last_active_at, boundary) VALUES ('end-race', '2026-01-01T00:00:00Z', '2026-01-01T00:01:00Z', 'active', '2026-01-01T00:01:00Z', 'header')",
    ).run();
    await env.DB.prepare(
      "INSERT INTO exchanges(id, session_id, ts, endpoint, model, latency_ms, r2_key, capture_status, accepted_at, schema_version) VALUES ('end-race-exchange', 'end-race', '2026-01-01T00:01:00Z', 'chat', 'openai/test', 1, 'log/end-race.json', 'accepted', '2026-01-01T00:01:01Z', 1)",
    ).run();
    const headers = {
      authorization: "Bearer machine-token",
      "content-type": "application/json",
    };
    const ended = await request("/sessions/end-race/end", {
      method: "POST",
      headers,
      body: "{}",
    });
    const endTime = ((await ended.json()) as { ended_at: string }).ended_at;

    await finalizeAcceptedExchange(
      env.DB,
      "end-race-exchange",
      "end-race",
      "2026-01-01T00:01:00Z",
      "2026-01-01T00:02:00Z",
      "opencode",
      "openai/test",
      3,
      1,
      0,
      0,
      100,
      true,
    );
    expect(
      await env.DB.prepare(
        "SELECT state, inactive_at, request_count FROM sessions WHERE id = 'end-race'",
      ).first(),
    ).toEqual({ state: "inactive", inactive_at: endTime, request_count: 1 });
    const again = await request("/sessions/end-race/end", {
      method: "POST",
      headers,
      body: "{}",
    });
    expect(again.status).toBe(200);
    expect(await again.json()).toEqual({ session_id: "end-race", state: "inactive", ended_at: endTime });
  });

  it("sweeps stale accepted exchanges with no R2 object to failed", async () => {
    await env.DB.prepare(
      "INSERT INTO sessions(id, started_at, state, boundary) VALUES ('sweep-session', '2026-01-01T00:00:00Z', 'inactive', 'header')",
    ).run();
    await env.DB.prepare(
      "INSERT INTO exchanges(id, session_id, ts, endpoint, model, latency_ms, r2_key, capture_status, capture_reason, accepted_at, schema_version) VALUES ('stale-accepted', 'sweep-session', '2026-01-01T00:00:00Z', 'chat', 'openai/test', 1, 'log/stale-accepted.json', 'accepted', 'enabled', '2026-01-01T00:00:01Z', 1)",
    ).run();
    await env.DB.prepare(
      "INSERT INTO exchanges(id, session_id, ts, endpoint, model, latency_ms, r2_key, capture_status, capture_reason, accepted_at, schema_version) VALUES ('fresh-accepted', 'sweep-session', ?, 'chat', 'openai/test', 1, 'log/fresh-accepted.json', 'accepted', 'enabled', ?, 1)",
    )
      .bind(new Date().toISOString(), new Date().toISOString())
      .run();
    await env.DB.prepare(
      "INSERT INTO exchanges(id, session_id, ts, endpoint, model, latency_ms, r2_key, capture_status, capture_reason, schema_version) VALUES ('ageless-accepted', 'sweep-session', '2026-01-01T00:00:00Z', 'chat', 'openai/test', 1, 'log/ageless-accepted.json', 'accepted', 'enabled', 1)",
    ).run();

    const response = await request("/reconcile", {
      method: "POST",
      headers: { authorization: "Bearer machine-token" },
    });
    const result = (await response.json()) as {
      swept: { exchange_ids: string[] };
      pending: { exchange_ids: string[]; stale_exchange_ids: string[] };
    };
    expect(result.swept.exchange_ids).toEqual(
      expect.arrayContaining(["stale-accepted", "ageless-accepted"]),
    );
    expect(result.pending.exchange_ids).toContain("fresh-accepted");
    expect(result.pending.stale_exchange_ids).toEqual([]);
    expect(
      await env.DB.prepare(
        "SELECT capture_status, capture_reason, failure_code, failed_at FROM exchanges WHERE id = 'stale-accepted'",
      ).first(),
    ).toMatchObject({
      capture_status: "failed",
      capture_reason: "reconciliation",
      failure_code: "r2_object_missing",
      failed_at: expect.any(String),
    });
    expect(
      await env.DB.prepare(
        "SELECT capture_status FROM exchanges WHERE id = 'fresh-accepted'",
      ).first(),
    ).toEqual({ capture_status: "accepted" });
  });

  it("coalesces concurrent headerless requests into one heuristic session", async () => {
    vi.stubGlobal(
      "fetch",
      vi
        .fn()
        .mockImplementation(() =>
          Promise.resolve(Response.json({ choices: [] })),
        ),
    );
    const init = {
      method: "POST",
      headers: {
        authorization: "Bearer machine-token",
        "content-type": "application/json",
      },
      body: JSON.stringify({ model: "openai/test", messages: [] }),
    };
    await Promise.all([
      request("/v1/chat/completions", init),
      request("/v1/chat/completions", init),
    ]);
    expect(
      await env.DB.prepare(
        "SELECT COUNT(*) AS sessions, SUM(request_count) AS requests FROM sessions",
      ).first(),
    ).toEqual({ sessions: 1, requests: 2 });
  });

  it("derives session intent from the first user message and keeps it sticky", async () => {
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
    const headers = {
      authorization: "Bearer machine-token",
      "content-type": "application/json",
      "x-mimir-session": "intent-session",
    };
    await request("/v1/chat/completions", {
      method: "POST",
      headers,
      body: JSON.stringify({
        model: "openai/test",
        messages: [
          { role: "system", content: "ignored" },
          { role: "user", content: "  Fix the   login redirect\nloop " },
        ],
      }),
    });
    expect(
      await env.DB.prepare(
        "SELECT intent FROM sessions WHERE id = 'intent-session'",
      ).first(),
    ).toEqual({ intent: "Fix the login redirect loop" });
    await request("/v1/chat/completions", {
      method: "POST",
      headers,
      body: JSON.stringify({
        model: "openai/test",
        messages: [{ role: "user", content: "Something else entirely" }],
      }),
    });
    expect(
      await env.DB.prepare(
        "SELECT intent FROM sessions WHERE id = 'intent-session'",
      ).first(),
    ).toEqual({ intent: "Fix the login redirect loop" });
    const found = await request("/api/sessions?q=login%20redirect", { headers });
    const result = (await found.json()) as { sessions: { id: string }[] };
    expect(result.sessions.map((session) => session.id)).toContain("intent-session");
  });
});
