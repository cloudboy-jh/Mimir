import { describe, expect, it, vi } from "vitest";
import { dashboardRequest, env } from "./support";

async function list(query = "") {
  const response = await dashboardRequest(`/dashboard/api/sessions${query}`);
  expect(response.status).toBe(200);
  return response.json<{ sessions: Array<{ id: string; summary_text: string | null; summary_status: string }> }>();
}

describe("list summary freshness", () => {
  it("removes cached summary text from lists and search after a descendant save", async () => {
    await env.DB.prepare("INSERT INTO sessions(id, boundary, state, started_at, last_active_at, work_outcome, outcome_src, summary_text, summary_status, summary_source, summary_updated_at) VALUES ('cache-root', 'header', 'inactive', '2026-01-01T00:00:00Z', '2026-01-02T00:00:00Z', 'unresolved', 'user', 'uniquecachedfinding', 'ready', 'reconstructed:v2:cached', '2026-01-04T00:00:00Z')").run();
    await env.DB.prepare("INSERT INTO sessions(id, parent_session_id, boundary, state, started_at, last_active_at) VALUES ('cache-child', 'cache-root', 'header', 'inactive', '2026-01-02T00:00:00Z', '2026-01-03T00:00:00Z')").run();
    expect((await list("?q=uniquecachedfinding")).sessions.map(session => session.id)).toEqual(["cache-root"]);
    await env.DB.prepare("INSERT INTO exchanges(id, session_id, ts, endpoint, latency_ms, r2_key, capture_status, saved_at) VALUES ('cache-late-save', 'cache-child', '2026-01-03T00:00:00Z', 'harness', 0, 'log/cache-late-save.json', 'saved', '2026-01-05T00:00:00Z')").run();
    expect((await list()).sessions.find(session => session.id === "cache-root")).toMatchObject({ summary_text: null, summary_status: "pending" });
    expect((await list("?q=uniquecachedfinding")).sessions).toEqual([]);
  });

  it.each([0, 1000])("does not mark an old reconstruction fresh when evidence changes %i ms into an archive read", async (delay) => {
    await env.DB.prepare("INSERT INTO sessions(id, boundary, state, started_at, last_active_at, intent, work_outcome, outcome_src) VALUES ('concurrent-summary', 'header', 'inactive', '2026-01-01T00:00:00Z', '2026-01-01T00:00:00Z', 'uniqueracesummary', 'discarded', 'user')").run();
    await env.DB.prepare("INSERT INTO exchanges(id, session_id, ts, endpoint, latency_ms, r2_key, capture_status, saved_at, request_kind) VALUES ('initial-save', 'concurrent-summary', '2026-01-01T00:00:00Z', 'harness', 0, 'concurrent-summary.json', 'saved', '2026-01-01T00:00:00Z', 'primary')").run();
    await env.LOGS.put("concurrent-summary.json", JSON.stringify({ request: {}, response: {} }));
    const get = env.LOGS.get.bind(env.LOGS);
    vi.useFakeTimers({ toFake: ["Date"] });
    vi.setSystemTime(new Date("2026-02-01T00:00:00Z"));
    const archiveRead = vi.spyOn(env.LOGS, "get").mockImplementationOnce(async (...args) => {
      await env.DB.prepare("INSERT INTO exchanges(id, session_id, ts, endpoint, latency_ms, r2_key, capture_status, saved_at, request_kind) VALUES ('concurrent-save', 'concurrent-summary', '2026-01-01T00:00:00Z', 'harness', 0, 'later-save.json', 'saved', ?, 'primary')").bind(new Date(Date.now() + delay).toISOString()).run();
      vi.setSystemTime(new Date("2026-02-01T00:00:02Z"));
      return get(...args);
    });
    try {
      const response = await dashboardRequest("/dashboard/api/sessions/concurrent-summary");
      expect(response.status).toBe(200);
      expect((await list()).sessions.find(session => session.id === "concurrent-summary")).toMatchObject({ summary_text: null, summary_status: "pending" });
      expect((await list("?q=No%20observed%20verification%20output")).sessions).toEqual([]);
    } finally {
      archiveRead.mockRestore();
      vi.useRealTimers();
    }
  });

  it("does not trust a future-dated obsolete template", async () => {
    await env.DB.prepare("INSERT INTO sessions(id, boundary, state, started_at, work_outcome, outcome_src, summary_text, summary_status, summary_source, summary_updated_at) VALUES ('legacy-summary', 'header', 'inactive', '2026-01-01T00:00:00Z', 'unresolved', 'user', 'unverifiedchangedfiles', 'ready', 'generated', '2099-01-01T00:00:00Z')").run();
    expect((await list()).sessions.find(session => session.id === "legacy-summary")).toMatchObject({ summary_text: null, summary_status: "pending" });
    expect((await list("?q=unverifiedchangedfiles")).sessions).toEqual([]);
  });
});
