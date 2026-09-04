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

describe("Reported exchange integration", () => {
  it("persists canonical harness exchanges to redacted R2 and indexed D1", async () => {
    await env.DB.prepare(
      "INSERT INTO config(key, value) VALUES('redact.patterns', '[\"customer-[0-9]+\"]')",
    ).run();
    const response = await request("/sessions/reported-session/exchanges", {
      method: "POST",
      headers: {
        authorization: "Bearer machine-token",
        "content-type": "application/json",
        "x-mimir-harness": "opencode",
        "x-mimir-repo": "mimir",
        "x-mimir-git-ref": "feature/reported",
      },
      body: JSON.stringify({
        exchange_id: "reported-exchange-1",
        ts: "2026-07-26T12:00:00Z",
        model: "openai/gpt-5",
        provider: "OpenAI",
        request: {
          messages: [
            {
              role: "user",
              content:
                "Fix src/reported.ts with token: private-value for customer-123",
            },
            {
              role: "assistant",
              tool_calls: [
                {
                  function: {
                    name: "edit",
                    arguments: '{"path":"src/reported.ts"}',
                  },
                },
              ],
            },
          ],
        },
        response: {
          choices: [
            {
              message: { content: "src/reported.ts failed: compile error" },
              finish_reason: "stop",
            },
          ],
          error: { code: "compile_failed", message: "compile error" },
        },
        tool_activity: [
          {
            name: "read",
            input: { path: "src/reported.ts" },
            status: "succeeded",
            output: "file loaded",
          },
          {
            name: "edit",
            input: { path: "src/reported.ts" },
            status: "failed",
            output: "compile_failed: compile error",
          },
        ],
        usage: { input_tokens: 12, output_tokens: 4, cache_read_tokens: 5, cache_write_tokens: 2 },
        latency_ms: 321,
        request_kind: "primary",
      }),
    });
    expect(response.status).toBe(201);
    expect(await response.json()).toEqual({
      exchange_id: "reported-exchange-1",
      session_id: "reported-session",
      capture_status: "saved",
      duplicate: false,
    });

    const exchange = await env.DB.prepare(
      "SELECT session_id, model, provider, finish_reason, input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, latency_ms, request_kind, capture_status, capture_reason, r2_key, r2_bytes FROM exchanges WHERE id = 'reported-exchange-1'",
    ).first<{ r2_key: string; r2_bytes: number } & Record<string, unknown>>();
    expect(exchange).toMatchObject({
      session_id: "reported-session",
      model: "openai/gpt-5",
      provider: "OpenAI",
      finish_reason: "stop",
      input_tokens: 12,
      output_tokens: 4,
      cache_read_tokens: 5,
      cache_write_tokens: 2,
      latency_ms: 321,
      request_kind: "primary",
      capture_status: "saved",
      capture_reason: "enabled",
    });
    expect(
      await env.DB.prepare(
        "SELECT request_count, tokens_in, tokens_out, cache_read_tokens, cache_write_tokens, model_primary, intent FROM sessions WHERE id = 'reported-session'",
      ).first(),
    ).toEqual({
      request_count: 1,
      tokens_in: 12,
      tokens_out: 4,
      cache_read_tokens: 5,
      cache_write_tokens: 2,
      model_primary: "openai/gpt-5",
      intent: "Fix src/reported.ts with token: [REDACTED] for [REDACTED]",
    });
    expect(
      await env.DB.prepare(
        "SELECT file FROM session_files WHERE session_id = 'reported-session'",
      ).first(),
    ).toEqual({ file: "src/reported.ts" });
    expect(
      await env.DB.prepare(
        "SELECT signature FROM session_errors WHERE session_id = 'reported-session'",
      ).first(),
    ).toEqual({ signature: "compile_failed: compile error" });

    const objectText = await (await env.LOGS.get(exchange!.r2_key))!.text();
    expect(objectText).not.toContain("private-value");
    expect(objectText).not.toContain("customer-123");
    expect(exchange!.r2_bytes).toBe(
      new TextEncoder().encode(objectText).byteLength,
    );
    expect(JSON.parse(objectText)).toMatchObject({
      schema_version: 1,
      exchange_id: "reported-exchange-1",
      session_id: "reported-session",
      endpoint: "harness",
      request: {
        messages: [
          {
            content:
              "Fix src/reported.ts with token: [REDACTED] for [REDACTED]",
          },
          { role: "assistant" },
        ],
      },
      response: {
        format: "json",
        body: { choices: [{ finish_reason: "stop" }] },
      },
      tool_activity: [
        {
          name: "read",
          input: { path: "src/reported.ts" },
          status: "succeeded",
        },
        {
          name: "edit",
          input: { path: "src/reported.ts" },
          status: "failed",
          output: "compile_failed: compile error",
        },
      ],
      usage: { input_tokens: 12, output_tokens: 4, cache_read_tokens: 5, cache_write_tokens: 2 },
      redaction: { version: 1 },
    });
    const state = (await (
      await request("/sessions/reported-session/object-state", {
        headers: { authorization: "Bearer machine-token" },
      })
    ).json()) as Record<string, unknown>;
    expect(state).toMatchObject({
      turn_count: 1,
      tokens_in: 12,
      tokens_out: 4,
      cache_read_tokens: 5,
      cache_write_tokens: 2,
    });
  });

  it.each(["pi", "oh-my-pi", "opencode"])(
    "conforms %s tool-bearing exchanges through ingestion and diff retrieval",
    async (harness) => {
      const sessionID = `${harness}-conformance`;
      const patch =
        "diff --git a/src/conformance.ts b/src/conformance.ts\n--- a/src/conformance.ts\n+++ b/src/conformance.ts\n@@ -1 +1 @@\n-old\n+new\n";
      const headers = {
        authorization: "Bearer machine-token",
        "content-type": "application/json",
        "x-mimir-harness": harness,
        "x-mimir-repo": "mimir",
        "x-mimir-git-ref": "feature/conformance",
      };
      const lifecycle = await request(`/sessions/${sessionID}/events`, {
        method: "POST",
        headers,
        body: JSON.stringify({
          version: 1,
          kind: "heartbeat",
          ts: "2026-08-17T11:59:59Z",
          harness,
          repo: "mimir",
          title: `${harness} conformance`,
        }),
      });
      expect(lifecycle.status).toBe(200);
      const exchange = await request(`/sessions/${sessionID}/exchanges`, {
        method: "POST",
        headers,
        body: JSON.stringify({
          exchange_id: `${harness}:conformance`,
          ts: "2026-08-17T12:00:00Z",
          model: "openai/gpt-5",
          provider: "openrouter",
          request_kind: "primary",
          request: {
            messages: [{ role: "user", content: "Update src/conformance.ts" }],
          },
          response: { role: "assistant", content: "attempted update" },
          tool_activity: [
            {
              name: "read",
              input: { path: "src/conformance.ts" },
              status: "succeeded",
              output: "old",
            },
            {
              name: "edit",
              input: { path: "src/conformance.ts" },
              status: "failed",
              output: "Error: write failed",
            },
          ],
          usage: { input_tokens: 8, output_tokens: 3 },
          latency_ms: 42,
          title: `${harness} conformance`,
        }),
      });
      expect(exchange.status).toBe(201);
      expect(
        await env.DB.prepare(
          "SELECT COUNT(*) AS count FROM sessions WHERE id = ?",
        )
          .bind(sessionID)
          .first(),
      ).toEqual({ count: 1 });
      expect(
        await env.DB.prepare(
          "SELECT repo, source_ref, harness, model_primary, intent, title, title_source FROM sessions WHERE id = ?",
        )
          .bind(sessionID)
          .first(),
      ).toEqual({
        repo: "mimir",
        source_ref: "feature/conformance",
        harness,
        model_primary: "openai/gpt-5",
        intent: "Update src/conformance.ts",
        title: `${harness} conformance`,
        title_source: "harness",
      });
      expect(
        (
          await env.DB.prepare(
            "SELECT file FROM session_files WHERE session_id = ?",
          )
            .bind(sessionID)
            .all<{ file: string }>()
        ).results,
      ).toEqual([{ file: "src/conformance.ts" }]);
      expect(
        (
          await env.DB.prepare(
            "SELECT signature FROM session_errors WHERE session_id = ?",
          )
            .bind(sessionID)
            .all<{ signature: string }>()
        ).results,
      ).toEqual([{ signature: "Error: write failed" }]);

      const outcome = await request(`/sessions/${sessionID}/outcome`, {
        method: "POST",
        headers: {
          authorization: "Bearer machine-token",
          "content-type": "application/json",
        },
        body: JSON.stringify({
          outcome: "landed",
          reason: "fixture landed",
          evidence: { commit: "a".repeat(40), provenance: harness, patch },
        }),
      });
      expect(outcome.status).toBe(200);
      const diff = await dashboardRequest(
        `/dashboard/api/sessions/${sessionID}/diff`,
      );
      expect(diff.status).toBe(200);
      expect(await diff.text()).toBe(patch);
    },
  );

  it.each([
    ["disabled", "save.enabled", false, {}, "openai/gpt-5"],
    [
      "excluded_repository",
      "save.exclude_repos",
      ["private-*"],
      { "x-mimir-repo": "private-repo" },
      "openai/gpt-5",
    ],
    [
      "excluded_model",
      "save.exclude_models",
      ["anthropic/*"],
      {},
      "anthropic/claude-sonnet",
    ],
  ])(
    "skips reported exchanges excluded by %s capture policy",
    async (reason, key, value, metadataHeaders, model) => {
      await env.DB.prepare("INSERT INTO config(key, value) VALUES(?, ?)")
        .bind(key, JSON.stringify(value))
        .run();
      const response = await request(`/sessions/reported-${reason}/exchanges`, {
        method: "POST",
        headers: {
          authorization: "Bearer machine-token",
          "content-type": "application/json",
          "x-mimir-harness": "opencode",
          ...metadataHeaders,
        },
        body: JSON.stringify({
          exchange_id: `reported-${reason}`,
          ts: "2026-07-26T12:00:00Z",
          model,
          request: {
            messages: [{ role: "user", content: "Must not persist" }],
          },
          response: { output: "Must not persist" },
          tool_activity: [],
          usage: { input_tokens: 2, output_tokens: 1 },
          latency_ms: 10,
          request_kind: "primary",
        }),
      });

      expect(response.status).toBe(200);
      expect(await response.json()).toEqual({
        exchange_id: `reported-${reason}`,
        session_id: `reported-${reason}`,
        capture_status: "skipped",
        capture_reason: reason,
        duplicate: false,
      });
      expect(
        await env.DB.prepare(
          "SELECT COUNT(*) AS count FROM exchanges WHERE id = ?",
        )
          .bind(`reported-${reason}`)
          .first(),
      ).toEqual({ count: 0 });
      expect(
        await env.DB.prepare(
          "SELECT COUNT(*) AS count FROM sessions WHERE id = ?",
        )
          .bind(`reported-${reason}`)
          .first(),
      ).toEqual({ count: 0 });
      expect((await env.LOGS.list()).objects).toHaveLength(0);
    },
  );

  it("deduplicates canonical harness exchange retries without changing aggregates or DO turns", async () => {
    const payload = {
      exchange_id: "reported-duplicate-1",
      ts: "2026-07-26T12:01:00Z",
      model: "openai/gpt-5",
      request: { messages: [{ role: "user", content: "Retry once" }] },
      response: { output: "done" },
      tool_activity: [],
      usage: { input_tokens: 5, output_tokens: 2 },
      latency_ms: 100,
      request_kind: "primary",
    };
    const init = {
      method: "POST",
      headers: {
        authorization: "Bearer machine-token",
        "content-type": "application/json",
        "x-mimir-harness": "hermes",
      },
      body: JSON.stringify(payload),
    };
    expect(
      (await request("/sessions/reported-duplicate/exchanges", init)).status,
    ).toBe(201);
    const duplicate = await request(
      "/sessions/reported-duplicate/exchanges",
      init,
    );
    expect(duplicate.status).toBe(200);
    expect(await duplicate.json()).toMatchObject({
      capture_status: "saved",
      duplicate: true,
    });
    expect(
      await env.DB.prepare(
        "SELECT COUNT(*) AS count FROM exchanges WHERE id = 'reported-duplicate-1'",
      ).first(),
    ).toEqual({ count: 1 });
    expect(
      await env.DB.prepare(
        "SELECT request_count, tokens_in, tokens_out FROM sessions WHERE id = 'reported-duplicate'",
      ).first(),
    ).toEqual({ request_count: 1, tokens_in: 5, tokens_out: 2 });
    const state = (await (
      await request("/sessions/reported-duplicate/object-state", {
        headers: { authorization: "Bearer machine-token" },
      })
    ).json()) as Record<string, unknown>;
    expect(state).toMatchObject({ turn_count: 1, tokens_in: 5, tokens_out: 2 });
  });

  it("retries reported exchanges after a failed R2 write", async () => {
    vi.spyOn(env.LOGS, "put").mockRejectedValueOnce(
      new Error("injected R2 failure"),
    );
    const payload = {
      exchange_id: "reported-retry-after-failure",
      ts: "2026-07-26T12:02:00Z",
      model: "openai/gpt-5",
      request: { messages: [{ role: "user", content: "Persist this retry" }] },
      response: { output: "saved" },
      tool_activity: [],
      usage: { input_tokens: 3, output_tokens: 1 },
      latency_ms: 50,
      request_kind: "primary",
    };
    const init = {
      method: "POST",
      headers: {
        authorization: "Bearer machine-token",
        "content-type": "application/json",
        "x-mimir-harness": "opencode",
      },
      body: JSON.stringify(payload),
    };
    expect(
      (await request("/sessions/reported-retry/exchanges", init)).status,
    ).toBe(500);
    expect(
      (
        await env.DB.prepare(
          "SELECT capture_status FROM exchanges WHERE id = 'reported-retry-after-failure'",
        ).first()
      )?.capture_status,
    ).toBe("failed");
    expect(
      (await request("/sessions/reported-retry/exchanges", init)).status,
    ).toBe(201);
    expect(
      await env.DB.prepare(
        "SELECT capture_status FROM exchanges WHERE id = 'reported-retry-after-failure'",
      ).first(),
    ).toEqual({ capture_status: "saved" });
    expect(
      await env.DB.prepare(
        "SELECT request_count, tokens_in, tokens_out FROM sessions WHERE id = 'reported-retry'",
      ).first(),
    ).toEqual({ request_count: 1, tokens_in: 3, tokens_out: 1 });
  });

  it("requires machine authentication for canonical harness exchanges", async () => {
    const response = await request("/sessions/reported-auth/exchanges", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: "{}",
    });
    expect(response.status).toBe(401);
    expect(
      await env.DB.prepare("SELECT COUNT(*) AS count FROM exchanges").first(),
    ).toEqual({ count: 0 });
  });

  it.each([
    ["invalid JSON", "{"],
    ["an invalid session ID", JSON.stringify({}), "bad!session"],
    [
      "unknown fields",
      JSON.stringify({
        exchange_id: "bad-1",
        ts: "2026-07-26T12:00:00Z",
        model: "test",
        request: {},
        response: {},
        tool_activity: [],
        usage: { input_tokens: 1, output_tokens: 1 },
        latency_ms: 1,
        request_kind: "primary",
        extra: true,
      }),
    ],
    [
      "missing response",
      JSON.stringify({
        exchange_id: "bad-2",
        ts: "2026-07-26T12:00:00Z",
        model: "test",
        request: {},
        tool_activity: [],
        usage: { input_tokens: 1, output_tokens: 1 },
        latency_ms: 1,
        request_kind: "primary",
      }),
    ],
    [
      "invalid usage",
      JSON.stringify({
        exchange_id: "bad-3",
        ts: "2026-07-26T12:00:00Z",
        model: "test",
        request: {},
        response: {},
        tool_activity: [],
        usage: { input_tokens: -1, output_tokens: 1 },
        latency_ms: 1,
        request_kind: "primary",
      }),
    ],
    [
      "non-finite JSON numbers",
      '{"exchange_id":"bad-4","ts":"2026-07-26T12:00:00Z","model":"test","request":{"value":1e400},"response":{},"tool_activity":[],"usage":{"input_tokens":1,"output_tokens":1},"latency_ms":1,"request_kind":"primary"}',
    ],
  ])(
    "rejects %s for canonical harness exchanges",
    async (_case, body, session = "reported-validation") => {
      const response = await request(`/sessions/${session}/exchanges`, {
        method: "POST",
        headers: {
          authorization: "Bearer machine-token",
          "content-type": "application/json",
        },
        body,
      });
      expect(response.status).toBe(400);
      expect(
        await env.DB.prepare("SELECT COUNT(*) AS count FROM exchanges").first(),
      ).toEqual({ count: 0 });
    },
  );
});
