import type { Hono } from "hono";
import type { AppEnv } from "../env";
import { attachCaptureSummary, TREE_CAPTURE_SUMMARY_COLUMNS } from "../sessions/capture-status";
import { expireSessions } from "../sessions/lifecycle";
import { autoResolveStaleOutcomes } from "../sessions/outcomes";
import { ROOT_SESSION_COLUMNS, SESSION_TREE_CTE } from "../sessions/session-queries";
import {
  buildDaySnapshot,
  DAY_BUCKET_MINUTES,
  validDate,
  validTimeZone,
  zonedDate,
  zonedDayBounds,
  type DayActivityBucket,
  type DayCommitRow,
  type DayRoot,
} from "./day-snapshot";

const IN_DAY = (column: string) => `julianday(${column}) >= julianday(?1) AND julianday(${column}) < julianday(?2)`;
const DAY_ROOT_IDS = `SELECT session_tree.root_id FROM exchanges e JOIN session_tree ON session_tree.id = e.session_id WHERE ${IN_DAY("e.ts")}
  UNION SELECT session_tree.root_id FROM session_git_artifacts a JOIN session_tree ON session_tree.id = a.session_id WHERE ${IN_DAY("COALESCE(a.committed_at, a.accepted_at)")}
  UNION SELECT id FROM sessions WHERE parent_session_id IS NULL AND ${IN_DAY("started_at")}`;

export function registerDashboardDayRoutes(app: Hono<AppEnv>) {
  app.get("/dashboard/api/days/:date", async (c) => {
    const date = c.req.param("date");
    const timeZone = c.req.query("tz") ?? "UTC";
    if (!validDate(date)) return c.json({ error: "invalid date" }, 400);
    if (!validTimeZone(timeZone)) return c.json({ error: "invalid time zone" }, 400);
    await expireSessions(c.env.DB);
    await autoResolveStaleOutcomes(c.env);
    const { start, end } = zonedDayBounds(date, timeZone);
    const db = c.env.DB;
    const [roots, activity, subAgents, commits, neighbors] = await db.batch<Record<string, unknown>>([
      db.prepare(`${SESSION_TREE_CTE} SELECT ${ROOT_SESSION_COLUMNS}, ${TREE_CAPTURE_SUMMARY_COLUMNS} FROM sessions WHERE sessions.parent_session_id IS NULL AND sessions.id IN (${DAY_ROOT_IDS})`).bind(start, end),
      db.prepare(`${SESSION_TREE_CTE} SELECT session_tree.root_id AS root_id, CAST((julianday(e.ts) - julianday(?1)) * ${(24 * 60) / DAY_BUCKET_MINUTES} AS INTEGER) AS bucket, COUNT(*) AS requests, COALESCE(SUM(e.input_tokens), 0) AS tokens_in, COALESCE(SUM(e.output_tokens), 0) AS tokens_out, MIN(e.ts) AS first_at, MAX(e.ts) AS last_at FROM exchanges e JOIN session_tree ON session_tree.id = e.session_id WHERE ${IN_DAY("e.ts")} GROUP BY session_tree.root_id, bucket`).bind(start, end),
      db.prepare(`${SESSION_TREE_CTE} SELECT session_tree.root_id AS root_id, COUNT(DISTINCT e.session_id) AS sub_agents FROM exchanges e JOIN session_tree ON session_tree.id = e.session_id WHERE e.session_id <> session_tree.root_id AND ${IN_DAY("e.ts")} GROUP BY session_tree.root_id`).bind(start, end),
      db.prepare(`${SESSION_TREE_CTE} SELECT session_tree.root_id AS root_id, a.commit_sha, a.subject, a.ref, a.capture_status, a.patch_files, a.patch_additions, a.patch_deletions, a.committed_at, a.accepted_at FROM session_git_artifacts a JOIN session_tree ON session_tree.id = a.session_id WHERE ${IN_DAY("COALESCE(a.committed_at, a.accepted_at)")}`).bind(start, end),
      db.prepare(`SELECT
        (SELECT MAX(at) FROM (SELECT MAX(ts) AS at FROM exchanges WHERE julianday(ts) < julianday(?1) UNION ALL SELECT MAX(started_at) FROM sessions WHERE parent_session_id IS NULL AND julianday(started_at) < julianday(?1))) AS previous_at,
        (SELECT MIN(at) FROM (SELECT MIN(ts) AS at FROM exchanges WHERE julianday(ts) >= julianday(?2) UNION ALL SELECT MIN(started_at) FROM sessions WHERE parent_session_id IS NULL AND julianday(started_at) >= julianday(?2))) AS next_at`).bind(start, end),
    ]);
    const edges = neighbors.results[0] as { previous_at: string | null; next_at: string | null } | undefined;
    return c.json(buildDaySnapshot({
      date,
      timeZone,
      start,
      end,
      previousDate: edges?.previous_at ? zonedDate(edges.previous_at, timeZone) : null,
      nextDate: edges?.next_at ? zonedDate(edges.next_at, timeZone) : null,
      roots: roots.results.map(attachCaptureSummary) as unknown as DayRoot[],
      activity: activity.results as unknown as DayActivityBucket[],
      subAgents: subAgents.results as unknown as Array<{ root_id: string; sub_agents: number }>,
      commits: commits.results as unknown as DayCommitRow[],
    }));
  });
}
