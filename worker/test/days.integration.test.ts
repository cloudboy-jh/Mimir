import { describe, expect, it } from "vitest";
import { dashboardRequest, env } from "./support";
import type { DaySnapshot } from "../src/days/day-snapshot";

const SHA = "c".repeat(40);

async function day(date: string, tz: string) {
  const response = await dashboardRequest(`/dashboard/api/days/${date}?tz=${encodeURIComponent(tz)}`);
  expect(response.status).toBe(200);
  return await response.json() as DaySnapshot;
}

describe("day snapshot", () => {
  it("buckets by local calendar day and groups continued work into one workstream", async () => {
    await env.DB.batch([
      "INSERT INTO sessions(id, started_at, last_active_at, state, boundary, repo, source_ref, title, title_source, request_count) VALUES ('plan', '2026-10-21T08:36:00Z', '2026-10-21T09:10:00Z', 'inactive', 'header', 'compi', 'main', '📋 PLAN: Fix Windows installed-update smoke failure', 'harness', 2)",
      "INSERT INTO sessions(id, parent_session_id, started_at, last_active_at, state, boundary, repo, request_count) VALUES ('plan-agent', 'plan', '2026-10-21T08:40:00Z', '2026-10-21T08:45:00Z', 'inactive', 'header', 'compi', 1)",
      "INSERT INTO sessions(id, started_at, last_active_at, state, boundary, repo, source_ref, title, title_source, work_outcome, request_count) VALUES ('smoke', '2026-10-22T02:30:00Z', '2026-10-22T03:00:00Z', 'inactive', 'header', 'compi', 'main', '🪟 SMOKE: Fix Windows installed-update smoke script', 'harness', 'landed', 2)",
      "INSERT INTO sessions(id, started_at, last_active_at, state, boundary, repo, source_ref, title, title_source, request_count) VALUES ('other', '2026-10-21T15:00:00Z', '2026-10-21T15:20:00Z', 'inactive', 'header', 'compi', 'main', 'Design dashboard color tokens', 'harness', 2)",
      "INSERT INTO sessions(id, started_at, last_active_at, state, boundary, request_count) VALUES ('probe', '2026-10-21T16:00:00Z', '2026-10-21T16:00:05Z', 'inactive', 'header', 1)",
      "INSERT INTO sessions(id, started_at, last_active_at, state, boundary, repo, intent, request_count) VALUES ('yesterday', '2026-10-21T04:00:00Z', '2026-10-21T04:30:00Z', 'inactive', 'header', 'compi', 'Late night work', 2)",
      "INSERT INTO exchanges(id, session_id, ts, endpoint, latency_ms, r2_key, capture_status, input_tokens, output_tokens) VALUES ('plan-1', 'plan', '2026-10-21T08:36:10Z', 'chat', 1, 'log/plan-1.json', 'saved', 100, 10), ('plan-2', 'plan', '2026-10-21T09:10:00Z', 'chat', 1, 'log/plan-2.json', 'failed', 100, 10), ('agent-1', 'plan-agent', '2026-10-21T08:41:00Z', 'chat', 1, 'log/agent-1.json', 'saved', 50, 5), ('smoke-1', 'smoke', '2026-10-22T02:31:00Z', 'chat', 1, 'log/smoke-1.json', 'saved', 10, 1), ('smoke-2', 'smoke', '2026-10-22T03:00:00Z', 'chat', 1, 'log/smoke-2.json', 'saved', 10, 1), ('other-1', 'other', '2026-10-21T15:01:00Z', 'chat', 1, 'log/other-1.json', 'saved', 1, 1), ('other-2', 'other', '2026-10-21T15:20:00Z', 'chat', 1, 'log/other-2.json', 'saved', 1, 1), ('probe-1', 'probe', '2026-10-21T16:00:05Z', 'chat', 1, 'log/probe-1.json', 'saved', 1, 1), ('yesterday-1', 'yesterday', '2026-10-21T04:10:00Z', 'chat', 1, 'log/yesterday-1.json', 'saved', 1, 1)",
      `INSERT INTO session_git_artifacts(session_id, commit_sha, committed_at, subject, ref, provenance, patch_r2_key, patch_sha256, patch_bytes, patch_files, patch_additions, patch_deletions, capture_status, accepted_at, created_at) VALUES ('smoke', '${SHA}', '2026-10-22T02:59:00Z', 'Fix smoke script', 'main', 'git', 'sessions/smoke/patch', '${"d".repeat(64)}', 10, 1, 3, 1, 'saved', '2026-10-22T02:59:30Z', '2026-10-22T02:59:30Z')`,
    ].map((sql) => env.DB.prepare(sql)));

    const snapshot = await day("2026-10-21", "America/Chicago");
    expect(snapshot).toMatchObject({
      start: "2026-10-21T05:00:00.000Z",
      end: "2026-10-22T05:00:00.000Z",
      buckets: 96,
      previous_date: "2026-10-20",
      next_date: null,
      totals: { sessions: 4, quiet_sessions: 1, sub_agents: 1, requests: 8, commits: 1, repos: 1, active: 0 },
      quiet_session_ids: ["probe"],
    });
    expect(snapshot.workstreams.map((stream) => stream.session_ids)).toEqual([["plan", "smoke"], ["other"]]);
    expect(snapshot.workstreams[0]).toMatchObject({
      repo: "compi", source_ref: "main", requests: 5, sub_agents: 1,
      outcomes: { landed: 1, unresolved: 1, discarded: 0, abandoned: 0 },
      commits: [{ commit_sha: SHA, subject: "Fix smoke script", session_ids: ["smoke"], patch_additions: 3 }],
    });
    expect(snapshot.attention).toEqual({ active: [], unresolved: ["other", "plan"], capture: ["plan"] });
    const plan = snapshot.sessions.find((session) => session.id === "plan")!;
    // 08:36Z is 03:36 CDT, the 15th quarter-hour of the local day.
    expect(plan).toMatchObject({ requests: 3, tokens_in: 250, first_at: "2026-10-21T08:36:10Z", activity: [[14, 2], [16, 1]] });

    const previous = await day("2026-10-20", "America/Chicago");
    expect(previous.sessions.map((session) => session.id)).toEqual(["yesterday"]);
    expect(previous.next_date).toBe("2026-10-21");
    const utc = await day("2026-10-22", "UTC");
    expect(utc.workstreams.map((stream) => stream.session_ids)).toEqual([["smoke"]]);
  });

  it("follows daylight-saving day length and rejects malformed selections", async () => {
    expect(await day("2026-11-01", "America/New_York")).toMatchObject({
      start: "2026-11-01T04:00:00.000Z", end: "2026-11-02T05:00:00.000Z", buckets: 100, sessions: [], workstreams: [],
    });
    expect((await dashboardRequest("/dashboard/api/days/2026-02-30?tz=UTC")).status).toBe(400);
    expect((await dashboardRequest("/dashboard/api/days/2026-10-21?tz=Mars%2FOlympus")).status).toBe(400);
  });
});
