import { describe, expect, it } from "vitest";
import { addMachineToken, createExecutionContext, env, request, waitOnExecutionContext, worker } from "./support";

const headers = { authorization: "Bearer machine-token", "content-type": "application/json" };

function event(id: string, body: Record<string, unknown>, init: Record<string, string> = headers) {
  return request(`/sessions/${id}/events`, { method: "POST", headers: init, body: JSON.stringify({ version: 1, ...body }) });
}

function session(id: string) {
  return env.DB.prepare(
    "SELECT state, started_at, last_active_at, ended_at, inactive_at, harness, repo, model_primary, title, title_source, parent_session_id, requested_parent_session_id, request_count, tokens_in FROM sessions WHERE id = ?",
  )
    .bind(id)
    .first<Record<string, unknown>>();
}

describe("Session events without a Durable Object", () => {
  it("records turns, heartbeats, failures and end directly in D1 and reopens on later activity", async () => {
    const turn = await event("events-a", {
      kind: "turn",
      ts: "2026-08-01T10:00:00Z",
      harness: "opencode",
      repo: "mimir",
      title: "Harness title",
      turn: { exchange_id: "msg-1", model: "openai/gpt-5", usage: { input_tokens: 10, output_tokens: 3 } },
    });
    expect(turn.status).toBe(200);
    expect(await turn.json()).toEqual({ accepted: true, session_id: "events-a", state: "active" });
    expect(await session("events-a")).toMatchObject({
      state: "active",
      started_at: "2026-08-01T10:00:00.000Z",
      last_active_at: "2026-08-01T10:00:00.000Z",
      harness: "opencode",
      repo: "mimir",
      model_primary: "openai/gpt-5",
      title: "Harness title",
      title_source: "harness",
      // Token counters are derived from saved exchanges, never from event usage.
      request_count: 0,
      tokens_in: 0,
    });
    await event("events-a", { kind: "heartbeat", ts: "2026-08-01T10:05:00Z", harness: "opencode" });
    await event("events-a", { kind: "failure", ts: "2026-08-01T10:06:00Z", reason: "provider error" });
    expect(await session("events-a")).toMatchObject({ state: "active", last_active_at: "2026-08-01T10:06:00.000Z" });

    const ended = await event("events-a", { kind: "end", ts: "2026-08-01T10:07:00Z" });
    expect(await ended.json()).toEqual({ accepted: true, session_id: "events-a", state: "inactive" });
    const afterEnd = (await session("events-a"))!;
    expect(afterEnd).toMatchObject({ state: "inactive", ended_at: expect.any(String) });
    expect(afterEnd.inactive_at).toBe(afterEnd.ended_at);
    expect(await env.DB.prepare("SELECT summary_status FROM sessions WHERE id = 'events-a'").first()).toEqual({ summary_status: "unavailable" });

    // A heartbeat that predates the end does not reopen the session.
    await event("events-a", { kind: "heartbeat", ts: "2026-08-01T10:06:30Z" });
    expect(await session("events-a")).toMatchObject({ state: "inactive", inactive_at: afterEnd.inactive_at });
    const later = new Date(Date.parse(String(afterEnd.inactive_at)) + 60_000).toISOString();
    const reopened = await event("events-a", { kind: "heartbeat", ts: later });
    expect(await reopened.json()).toMatchObject({ state: "active" });
    expect(await session("events-a")).toMatchObject({ state: "active", inactive_at: null, last_active_at: later });
    expect((await event("events-a", { kind: "end", ts: later })).status).toBe(200);
    expect((await event("events-a", { kind: "end", ts: later })).status).toBe(200);
    expect(await session("events-a")).toMatchObject({ state: "inactive" });
  });

  it("links parents from events before and after the parent exists", async () => {
    await event("events-child", { kind: "heartbeat", ts: "2026-08-01T10:00:00Z", parent_session_id: "events-parent" });
    expect(await session("events-child")).toMatchObject({ parent_session_id: null, requested_parent_session_id: "events-parent" });
    await event("events-parent", { kind: "heartbeat", ts: "2026-08-01T10:00:01Z" });
    expect(await session("events-child")).toMatchObject({ parent_session_id: "events-parent" });
    const conflict = await event("events-child", { kind: "heartbeat", ts: "2026-08-01T10:00:02Z", parent_session_id: "events-other" });
    expect(conflict.status).toBe(409);
    expect((await event("events-parent", { kind: "heartbeat", ts: "2026-08-01T10:00:03Z", parent_session_id: "events-child" })).status).toBe(409);
    expect((await event("events-child", { kind: "heartbeat", ts: "nope" })).status).toBe(400);
    expect((await event("events-child", { kind: "bogus", ts: "2026-08-01T10:00:00Z" })).status).toBe(400);
  });

  it("rejects events and ends from another installation", async () => {
    await addMachineToken("install-a", "machine-a");
    await addMachineToken("install-b", "machine-b");
    const owner = { ...headers, authorization: "Bearer machine-a" };
    const other = { ...headers, authorization: "Bearer machine-b" };
    expect((await event("owned-events", { kind: "heartbeat", ts: "2026-08-01T10:00:00Z" }, owner)).status).toBe(200);
    expect(await env.DB.prepare("SELECT installation_id FROM sessions WHERE id = 'owned-events'").first()).toEqual({ installation_id: "install-a" });
    expect((await event("owned-events", { kind: "heartbeat", ts: "2026-08-01T10:01:00Z" }, other)).status).toBe(409);
    expect((await request("/sessions/owned-events/end", { method: "POST", headers: other, body: "{}" })).status).toBe(403);
    expect(await session("owned-events")).toMatchObject({ state: "active", last_active_at: "2026-08-01T10:00:00.000Z" });
    expect((await request("/sessions/owned-events/end", { method: "POST", headers: owner, body: "{}" })).status).toBe(200);
  });

  it("ends sessions idempotently, ignores outcome fields and builds the summary", async () => {
    await env.DB.prepare(
      "INSERT INTO sessions(id, started_at, last_active_at, boundary, intent) VALUES ('end-summary', '2026-08-01T10:00:00Z', '2026-08-01T10:00:00Z', 'header', 'Ship the feature')",
    ).run();
    await env.DB.prepare(
      "INSERT INTO exchanges(id, session_id, ts, endpoint, latency_ms, r2_key, capture_status, accepted_at, saved_at, request_kind, intent_candidate) VALUES ('end-summary-1', 'end-summary', '2026-08-01T10:00:00Z', 'harness', 1, 'log/end-summary-1.json', 'saved', '2026-08-01T10:00:00Z', '2026-08-01T10:00:01Z', 'primary', 'Ship the feature')",
    ).run();
    await env.LOGS.put(
      "log/end-summary-1.json",
      JSON.stringify({ exchange_id: "end-summary-1", request: { messages: [{ role: "user", content: "Ship the feature" }] }, response: { format: "json", body: { role: "assistant", content: "Shipped." } } }),
    );
    const ended = await request("/sessions/end-summary/end", { method: "POST", headers, body: JSON.stringify({ outcome: "landed" }) });
    expect(ended.status).toBe(200);
    const first = (await ended.json()) as { ended_at: string };
    expect(first).toEqual({ session_id: "end-summary", state: "inactive", ended_at: expect.any(String) });
    const again = await request("/sessions/end-summary/end", { method: "POST", headers, body: "{}" });
    expect(await again.json()).toEqual(first);
    expect((await request("/sessions/missing-session/end", { method: "POST", headers, body: "{}" })).status).toBe(404);
    const detail = (await (await request("/api/sessions/end-summary", { headers })).json()) as Record<string, any>;
    expect(detail.summary).toMatchObject({
      status: "ready",
      goal: "Ship the feature",
      result: "Shipped.",
      evidence: expect.arrayContaining([
        { section: "goal", index: 0, exchange_id: "end-summary-1" },
        { section: "result", index: 0, exchange_id: "end-summary-1" },
      ]),
    });
    expect(detail.session.goal).toBe("Ship the feature");
    expect(await env.DB.prepare("SELECT work_outcome, outcome_src FROM sessions WHERE id = 'end-summary'").first()).toEqual({ work_outcome: "unresolved", outcome_src: null });
  });

  it("runs scheduled maintenance: expiry, summaries and search backfill", async () => {
    const stale = new Date(Date.now() - 60 * 60_000).toISOString();
    await env.DB.exec(`
      INSERT INTO sessions(id, started_at, last_active_at, state, boundary, intent) VALUES ('cron-stale', '${stale}', '${stale}', 'active', 'header', 'Stale work');
      INSERT INTO sessions(id, started_at, last_active_at, state, boundary) VALUES ('cron-fresh', '${new Date().toISOString()}', '${new Date().toISOString()}', 'active', 'header');
      INSERT INTO exchanges(id, session_id, ts, endpoint, latency_ms, r2_key, capture_status, accepted_at, saved_at, request_excerpt, response_excerpt) VALUES ('cron-archived', 'cron-stale', '${stale}', 'harness', 1, 'log/cron-archived.json', 'saved', '${stale}', '${stale}', '', '');
      INSERT INTO exchanges(id, session_id, ts, endpoint, latency_ms, r2_key, capture_status, accepted_at, saved_at, request_excerpt, response_excerpt) VALUES ('cron-missing', 'cron-stale', '${stale}', 'harness', 1, 'log/cron-missing.json', 'saved', '${stale}', '${stale}', 'excerpt zeppelin', '');
    `);
    await env.LOGS.put(
      "log/cron-archived.json",
      JSON.stringify({ exchange_id: "cron-archived", request: { messages: [{ role: "user", content: "Find the platypus" }] }, response: { format: "json", body: { role: "assistant", content: "Found it." } } }),
    );
    const ctx = createExecutionContext();
    await worker.scheduled!({ cron: "*/10 * * * *", scheduledTime: Date.now(), noRetry() {} } as ScheduledController, env as never, ctx);
    await waitOnExecutionContext(ctx);
    expect(await session("cron-stale")).toMatchObject({ state: "inactive", ended_at: stale });
    expect(await session("cron-fresh")).toMatchObject({ state: "active" });
    expect(await env.DB.prepare("SELECT summary_status, json_extract(summary_json, '$.goal') AS goal FROM sessions WHERE id = 'cron-stale'").first()).toEqual({
      summary_status: "ready",
      goal: "Stale work",
    });
    expect((await env.DB.prepare("SELECT id, search_indexed FROM exchanges ORDER BY id").all()).results).toEqual([
      { id: "cron-archived", search_indexed: 1 },
      { id: "cron-missing", search_indexed: 1 },
    ]);
    const found = (await (await request("/api/search?q=platypus", { headers })).json()) as { results: Array<{ session: { id: string } }> };
    expect(found.results.map((result) => result.session.id)).toEqual(["cron-stale"]);
    const fallback = (await (await request("/api/search?q=zeppelin", { headers })).json()) as { results: Array<{ hits: Array<{ exchange_id: string }> }> };
    expect(fallback.results[0]!.hits.map((hit) => hit.exchange_id)).toEqual(["cron-missing"]);
  });
});
