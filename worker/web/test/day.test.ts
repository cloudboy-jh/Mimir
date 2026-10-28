import { describe, expect, it } from "vitest";

import type { DaySession, DaySnapshot, DayWorkstream } from "../src/lib/api";
import { dayLevel, TOP_WORKSTREAMS } from "../src/lib/day";

function session(id: string, activity: Array<[number, number]>, extra: Partial<DaySession> = {}): DaySession {
  return {
    id, display_title: `Session ${id}`, harness: "oh-my-pi", repo: "mimir", source_ref: "main", model_primary: "claude-opus-5-5",
    outcome: "unresolved", outcome_reason: null, state: "inactive", capture_status: "saved", started_at: "2026-10-21T10:00:00Z",
    first_at: "2026-10-21T10:00:00Z", last_at: "2026-10-21T11:00:00Z", requests: activity.reduce((sum, [, count]) => sum + count, 0),
    tokens_in: 0, tokens_out: 0, sub_agents: 0, commit_count: 0, quiet: null, activity, ...extra,
  };
}

function stream(id: string, sessionIds: string[]): DayWorkstream {
  return {
    id, label: `Stream ${id}`, repo: "mimir", source_ref: "main", session_ids: sessionIds, first_at: "", last_at: "", requests: 0,
    tokens_in: 0, tokens_out: 0, sub_agents: 0, active: false, outcomes: { landed: 0, discarded: 0, abandoned: 0, unresolved: 0 }, commits: [],
  };
}

function day(sessions: DaySession[], workstreams: DayWorkstream[]): DaySnapshot {
  return {
    date: "2026-10-21", time_zone: "UTC", start: "", end: "", bucket_minutes: 15, buckets: 96, previous_date: null, next_date: null,
    totals: { sessions: sessions.length, quiet_sessions: 0, sub_agents: 0, requests: 0, tokens_in: 0, tokens_out: 0, commits: 0, repos: 1, active: 0 },
    workstreams, sessions, quiet_session_ids: sessions.filter((item) => item.quiet).map((item) => item.id),
    attention: { active: [], unresolved: [], capture: [] },
  };
}

describe("dayLevel", () => {
  it("adds each session's active blocks so a workstream equals its zoomed total", () => {
    const snapshot = day([session("a", [[4, 2], [5, 1]]), session("b", [[5, 3], [6, 1]])], [stream("a", ["a", "b"])]);
    const whole = dayLevel(snapshot, null);
    expect(whole.items.map((item) => [item.key, item.minutes])).toEqual([["a", 60]]);
    expect(whole.items[0]!.open).toEqual({ focus: "a" });

    const zoomed = dayLevel(snapshot, "a");
    expect(zoomed.title).toBe("Stream a");
    expect(zoomed.items.map((item) => [item.key, item.minutes, item.open])).toEqual([
      ["b", 30, { session: "b" }],
      ["a", 30, { session: "a" }],
    ]);
  });

  it("folds workstreams beyond the top five into Other only when two or more remain", () => {
    const build = (count: number) => {
      const sessions = Array.from({ length: count }, (_, index) => session(`s${index}`, Array.from({ length: count - index }, (_, bucket): [number, number] => [bucket, 1])));
      return day(sessions, sessions.map((item) => stream(item.id, [item.id])));
    };
    expect(dayLevel(build(TOP_WORKSTREAMS + 1), null).items.map((item) => item.kind)).toEqual(Array(TOP_WORKSTREAMS + 1).fill("stream"));

    const folded = build(TOP_WORKSTREAMS + 3);
    const whole = dayLevel(folded, null);
    expect(whole.items.map((item) => item.key)).toEqual(["s0", "s1", "s2", "s3", "s4", "other"]);
    expect(whole.items.at(-1)).toMatchObject({ label: "3 more workstreams", sessionIds: ["s5", "s6", "s7"], minutes: 90 });
    expect(dayLevel(folded, "other").items.map((item) => item.key)).toEqual(["s5", "s6", "s7"]);
  });

  it("keeps quick sessions out of outcome prompts and falls back to the day for unknown focus", () => {
    const snapshot = day([session("work", [[1, 2]]), session("probe", [[2, 1]], { quiet: "single_request" })], [stream("work", ["work"])]);
    const whole = dayLevel(snapshot, null);
    expect(whole.items.map((item) => [item.key, item.unresolvedIds])).toEqual([["work", ["work"]], ["quick", []]]);
    expect(dayLevel(snapshot, "missing").focus).toBeNull();
    expect(dayLevel(snapshot, "work").focus).toBeNull();
  });
});
