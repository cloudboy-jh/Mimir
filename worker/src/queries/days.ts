import {
  buildDaySnapshot,
  DAY_BUCKET_MINUTES,
  validDate,
  validTimeZone,
  zonedDate,
  zonedDayBounds,
  type DayActivityBucket,
  type DayCommitRow,
} from "../days/day-snapshot";
import { ROOT_TREE_CTE } from "../sessions/session-tree";
import { QueryError } from "../shared/query-error";
import type { QueryContext } from "./query-context";
import { loadSessions } from "./session-records";

const IN_DAY = (column: string) => `julianday(${column}) >= julianday(?1) AND julianday(${column}) < julianday(?2)`;
const DAY_ROOT_IDS = `SELECT session_tree.root_id FROM exchanges e JOIN session_tree ON session_tree.id = e.session_id WHERE ${IN_DAY("e.ts")}
  UNION SELECT session_tree.root_id FROM session_git_artifacts a JOIN session_tree ON session_tree.id = a.session_id WHERE ${IN_DAY("COALESCE(a.committed_at, a.accepted_at)")}
  UNION SELECT id FROM sessions WHERE parent_session_id IS NULL AND ${IN_DAY("started_at")}`;

/** Today's calendar date in a time zone. */
export function today(timeZone: string) {
  return zonedDate(new Date().toISOString(), timeZone);
}

// daySnapshot groups one local calendar day of root sessions into workstreams.
export async function daySnapshot(ctx: QueryContext, date: string, timeZone: string) {
  if (!validDate(date)) throw new QueryError(400, "invalid date");
  if (!validTimeZone(timeZone)) throw new QueryError(400, "invalid tz");
  const { start, end } = zonedDayBounds(date, timeZone);
  const db = ctx.db;
  const [roots, activity, subAgents, commits, neighbors] = await db.batch<Record<string, unknown>>([
    db.prepare(`${ROOT_TREE_CTE} ${DAY_ROOT_IDS}`).bind(start, end),
    db.prepare(`${ROOT_TREE_CTE} SELECT session_tree.root_id AS root_id, CAST((julianday(e.ts) - julianday(?1)) * ${(24 * 60) / DAY_BUCKET_MINUTES} AS INTEGER) AS bucket, COUNT(*) AS requests, COALESCE(SUM(e.input_tokens), 0) AS tokens_in, COALESCE(SUM(e.output_tokens), 0) AS tokens_out, MIN(e.ts) AS first_at, MAX(e.ts) AS last_at FROM exchanges e JOIN session_tree ON session_tree.id = e.session_id WHERE ${IN_DAY("e.ts")} GROUP BY session_tree.root_id, bucket`).bind(start, end),
    db.prepare(`${ROOT_TREE_CTE} SELECT session_tree.root_id AS root_id, COUNT(DISTINCT e.session_id) AS sub_agents FROM exchanges e JOIN session_tree ON session_tree.id = e.session_id WHERE e.session_id <> session_tree.root_id AND ${IN_DAY("e.ts")} GROUP BY session_tree.root_id`).bind(start, end),
    db.prepare(`${ROOT_TREE_CTE} SELECT session_tree.root_id AS root_id, a.commit_sha, a.subject, a.ref, a.capture_status, a.pushed, a.patch_files, a.patch_additions, a.patch_deletions, a.committed_at, a.accepted_at FROM session_git_artifacts a JOIN session_tree ON session_tree.id = a.session_id WHERE ${IN_DAY("COALESCE(a.committed_at, a.accepted_at)")}`).bind(start, end),
    db.prepare(`SELECT
      (SELECT MAX(at) FROM (SELECT MAX(ts) AS at FROM exchanges WHERE julianday(ts) < julianday(?1) UNION ALL SELECT MAX(started_at) FROM sessions WHERE parent_session_id IS NULL AND julianday(started_at) < julianday(?1))) AS previous_at,
      (SELECT MIN(at) FROM (SELECT MIN(ts) AS at FROM exchanges WHERE julianday(ts) >= julianday(?2) UNION ALL SELECT MIN(started_at) FROM sessions WHERE parent_session_id IS NULL AND julianday(started_at) >= julianday(?2))) AS next_at`).bind(start, end),
  ]);
  const sessions = await loadSessions(db, (roots!.results as Array<{ root_id: string }>).map((row) => row.root_id));
  const edges = neighbors!.results[0] as { previous_at: string | null; next_at: string | null } | undefined;
  return buildDaySnapshot({
    date,
    timeZone,
    start,
    end,
    previousDate: edges?.previous_at ? zonedDate(edges.previous_at, timeZone) : null,
    nextDate: edges?.next_at ? zonedDate(edges.next_at, timeZone) : null,
    roots: sessions.map((session) => ({
      id: session.id,
      display_title: session.display_title,
      title_source: session.title_source,
      harness: session.harness,
      repo: session.repo,
      source_ref: session.source_ref,
      model_primary: session.model_primary,
      git_status: session.git_status,
      state: session.state,
      started_at: session.started_at,
      activity_at: session.activity_at,
      request_count: session.request_count,
      capture: { status: session.capture.status },
    })),
    activity: activity!.results as unknown as DayActivityBucket[],
    subAgents: subAgents!.results as unknown as Array<{ root_id: string; sub_agents: number }>,
    commits: commits!.results as unknown as DayCommitRow[],
  });
}
