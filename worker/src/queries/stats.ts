import { validTimeZone, zonedDate } from "../days/day-snapshot";
import { ROOT_TREE_CTE } from "../sessions/session-tree";
import { dateRange } from "../shared/date-range";
import { QueryError } from "../shared/query-error";
import type { QueryContext, QueryParams } from "./query-context";

const GROUPS = ["repo", "harness", "device", "model", "day"] as const;
type GroupBy = (typeof GROUPS)[number];

// SQL key per grouping; `day` groups by absolute 15-minute bucket (every real
// UTC offset is a multiple of 15 minutes) and is mapped to a zoned date in JS.
// The epsilon keeps instants on a bucket boundary from rounding down.
const BUCKETS_PER_DAY = 96;
const UNIX_EPOCH_JULIAN_DAY = 2440587.5;
const KEYS: Record<GroupBy, { exchange: string; commit: string }> = {
  repo: { exchange: "root.repo", commit: "root.repo" },
  harness: { exchange: "COALESCE(e.harness, root.harness)", commit: "root.harness" },
  device: { exchange: "COALESCE(m.name, root.installation_id)", commit: "COALESCE(m.name, root.installation_id)" },
  model: { exchange: "e.model", commit: "root.model_primary" },
  day: {
    exchange: `CAST(julianday(e.ts) * ${BUCKETS_PER_DAY} + 1e-6 AS INTEGER)`,
    commit: `CAST(julianday(COALESCE(a.committed_at, a.accepted_at)) * ${BUCKETS_PER_DAY} + 1e-6 AS INTEGER)`,
  },
};

type Group = { key: string; sessions: number; requests: number; tokens_in: number; tokens_out: number; commits: number };

// stats aggregates saved exchanges and saved commits in the range by group.
// Sessions count distinct root sessions with activity in the group.
export async function stats(ctx: QueryContext, params: QueryParams) {
  const groupBy = params.group_by as GroupBy;
  if (!(GROUPS as readonly string[]).includes(groupBy)) throw new QueryError(400, "group_by must be one of repo, harness, device, model, day");
  const timeZone = params.tz || "UTC";
  if (!validTimeZone(timeZone)) throw new QueryError(400, "invalid tz");
  const now = Date.now();
  const range = dateRange(params.from || "7d", params.to, now);
  const from = range.start!;
  const to = range.end ?? new Date(now).toISOString();
  const key = KEYS[groupBy];
  const [exchanges, commits] = await ctx.db.batch<Record<string, unknown>>([
    ctx.db
      .prepare(
        `${ROOT_TREE_CTE} SELECT ${key.exchange} AS key, session_tree.root_id, COUNT(*) AS requests, SUM(e.input_tokens) AS tokens_in, SUM(e.output_tokens) AS tokens_out FROM exchanges e JOIN session_tree ON session_tree.id = e.session_id JOIN sessions root ON root.id = session_tree.root_id LEFT JOIN machines m ON m.installation_id = root.installation_id WHERE e.capture_status = 'saved' AND julianday(e.ts) >= julianday(?1) AND julianday(e.ts) < julianday(?2) GROUP BY 1, 2`,
      )
      .bind(from, to),
    ctx.db
      .prepare(
        `${ROOT_TREE_CTE} SELECT ${key.commit} AS key, a.commit_sha FROM session_git_artifacts a JOIN session_tree ON session_tree.id = a.session_id JOIN sessions root ON root.id = session_tree.root_id LEFT JOIN machines m ON m.installation_id = root.installation_id WHERE a.capture_status = 'saved' AND julianday(COALESCE(a.committed_at, a.accepted_at)) >= julianday(?1) AND julianday(COALESCE(a.committed_at, a.accepted_at)) < julianday(?2) GROUP BY 1, 2`,
      )
      .bind(from, to),
  ]);
  const label = (value: unknown) => {
    if (groupBy !== "day") return value === null || value === "" ? "unknown" : String(value);
    const instant = (Number(value) / BUCKETS_PER_DAY - UNIX_EPOCH_JULIAN_DAY) * 86_400_000;
    return zonedDate(new Date(Math.round(instant)).toISOString(), timeZone);
  };
  const groups = new Map<string, Group & { roots: Set<string>; shas: Set<string> }>();
  const group = (value: unknown) => {
    const name = label(value);
    let entry = groups.get(name);
    if (!entry) {
      entry = { key: name, sessions: 0, requests: 0, tokens_in: 0, tokens_out: 0, commits: 0, roots: new Set(), shas: new Set() };
      groups.set(name, entry);
    }
    return entry;
  };
  for (const row of exchanges!.results as Array<{ key: unknown; root_id: string; requests: number; tokens_in: number; tokens_out: number }>) {
    const entry = group(row.key);
    entry.roots.add(row.root_id);
    entry.requests += Number(row.requests);
    entry.tokens_in += Number(row.tokens_in);
    entry.tokens_out += Number(row.tokens_out);
  }
  for (const row of commits!.results as Array<{ key: unknown; commit_sha: string }>) group(row.key).shas.add(row.commit_sha);
  const result = [...groups.values()].map(({ roots, shas, ...entry }): Group => ({ ...entry, sessions: roots.size, commits: shas.size }));
  result.sort(groupBy === "day" ? (a, b) => a.key.localeCompare(b.key) : (a, b) => b.sessions - a.sessions || b.requests - a.requests || a.key.localeCompare(b.key));
  return { from, to, group_by: groupBy, groups: result };
}
