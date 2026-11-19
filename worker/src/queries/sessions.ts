import { ftsQuery } from "../search/search-index";
import { ROOT_TREE_CTE, SUBTREE_CTE } from "../sessions/session-tree";
import { storedSummary } from "../sessions/summaries";
import { MAX_SESSION_TITLE_CHARS, normalizeSessionTitle, reconcileSessionTitleStatement, titleUpdateStatement } from "../sessions/titles";
import { boundedLimit, decodeCursor, encodeCursor } from "../shared/cursors";
import { dateRange } from "../shared/date-range";
import { QueryError } from "../shared/query-error";
import { commitRows } from "./commits";
import type { QueryContext, QueryParams } from "./query-context";
import { DISPLAY_TITLE_SQL, loadSessions, type GitStatus, type Session } from "./session-records";

export type Receipt = {
  session_id: string;
  title: string;
  repo: string | null;
  harness: string | null;
  device: { id: string; name: string } | null;
  state: "active" | "inactive";
  capture: Session["capture"];
  git_status: GitStatus;
  commit_count: number;
  goal: string | null;
  dashboard_url: string | null;
  line: string;
};

const SUBTREE_HAS = (table: string, condition: string) =>
  `EXISTS (SELECT 1 FROM ${table} x JOIN session_tree st ON st.id = x.session_id WHERE st.root_id = sessions.id AND ${condition})`;
const SAVED_COMMIT = SUBTREE_HAS("session_git_artifacts", "x.capture_status = 'saved'");
const PUSHED_COMMIT = SUBTREE_HAS("session_git_artifacts", "x.capture_status = 'saved' AND x.pushed = 1");
const GIT_STATUS_FILTERS: Record<GitStatus, string> = {
  none: `NOT ${SAVED_COMMIT}`,
  committed: `${SAVED_COMMIT} AND NOT ${PUSHED_COMMIT}`,
  pushed: PUSHED_COMMIT,
};
const SAVED = SUBTREE_HAS("exchanges", "x.capture_status = 'saved'");
const FAILED = SUBTREE_HAS("exchanges", "x.capture_status = 'failed'");
const PENDING = SUBTREE_HAS("exchanges", "x.capture_status = 'accepted'");
const CAPTURE_FILTERS: Record<string, string> = {
  empty: `NOT (${SAVED} OR ${FAILED} OR ${PENDING})`,
  pending: PENDING,
  saved: `${SAVED} AND NOT ${FAILED} AND NOT ${PENDING}`,
  failed: `${FAILED} AND NOT ${SAVED} AND NOT ${PENDING}`,
  partial: `${FAILED} AND ${SAVED} AND NOT ${PENDING}`,
};

function oneOf<T extends string>(value: string | undefined, allowed: readonly T[], name: string): T | undefined {
  if (value === undefined || value === "") return undefined;
  if (!(allowed as readonly string[]).includes(value)) throw new QueryError(400, `invalid ${name}`);
  return value as T;
}

// listSessions returns root sessions, newest subtree activity first. `from`/`to`
// select sessions active within the range.
export async function listSessions(ctx: QueryContext, params: QueryParams) {
  const where = ["sessions.parent_session_id IS NULL"];
  const values: Array<string | number> = [];
  const q = params.q?.trim();
  if (q) {
    const matched = "EXISTS (SELECT 1 FROM session_tree st JOIN sessions m ON m.id = st.id WHERE st.root_id = sessions.id AND (instr(lower(COALESCE(m.title, '')), lower(?)) > 0 OR instr(lower(COALESCE(m.intent, '')), lower(?)) > 0 OR instr(lower(COALESCE(m.repo, '')), lower(?)) > 0 OR instr(lower(m.id), lower(?)) > 0))";
    values.push(q, q, q, q);
    const fts = ftsQuery(q);
    if (fts) {
      where.push(`(${matched} OR sessions.id IN (SELECT st.root_id FROM exchange_search JOIN exchanges e ON e.id = exchange_search.exchange_id JOIN session_tree st ON st.id = e.session_id WHERE exchange_search MATCH ? AND e.capture_status = 'saved'))`);
      values.push(fts);
    } else where.push(matched);
  }
  if (params.repo) {
    where.push("sessions.repo = ?");
    values.push(params.repo);
  }
  if (params.harness) {
    where.push("EXISTS (SELECT 1 FROM session_tree st JOIN sessions m ON m.id = st.id WHERE st.root_id = sessions.id AND m.harness = ?)");
    values.push(params.harness);
  }
  if (params.device) {
    where.push("sessions.installation_id IN (SELECT installation_id FROM machines WHERE installation_id = ? OR name = ?)");
    values.push(params.device, params.device);
  }
  const state = oneOf(params.state, ["active", "inactive"] as const, "state");
  if (state) {
    where.push("sessions.state = ?");
    values.push(state);
  }
  const gitStatus = oneOf(params.git_status, ["none", "committed", "pushed"] as const, "git_status");
  if (gitStatus) where.push(`(${GIT_STATUS_FILTERS[gitStatus]})`);
  const capture = oneOf(params.capture, ["empty", "pending", "saved", "failed", "partial"] as const, "capture");
  if (capture) where.push(`(${CAPTURE_FILTERS[capture]})`);
  const range = dateRange(params.from, params.to);
  if (range.start) {
    where.push("julianday(root_activity.activity_at) >= julianday(?)");
    values.push(range.start);
  }
  if (range.end) {
    where.push("julianday(sessions.started_at) < julianday(?)");
    values.push(range.end);
  }
  const cursor = decodeCursor(params.cursor, (value) =>
    typeof value.t === "string" && typeof value.id === "string" ? { t: value.t, id: value.id } : null,
  );
  if (cursor) {
    where.push("(root_activity.activity_at < ? OR (root_activity.activity_at = ? AND sessions.id < ?))");
    values.push(cursor.t, cursor.t, cursor.id);
  }
  const limit = boundedLimit(params.limit, 20, 100);
  const rows = await ctx.db
    .prepare(
      `${ROOT_TREE_CTE} SELECT sessions.id, root_activity.activity_at FROM sessions JOIN root_activity ON root_activity.root_id = sessions.id WHERE ${where.join(" AND ")} ORDER BY root_activity.activity_at DESC, sessions.id DESC LIMIT ?`,
    )
    .bind(...values, limit + 1)
    .all<{ id: string; activity_at: string }>();
  const page = rows.results.slice(0, limit);
  const last = page.at(-1);
  return {
    sessions: await loadSessions(ctx.db, page.map((row) => row.id)),
    next_cursor: rows.results.length > limit && last ? encodeCursor({ t: last.activity_at, id: last.id }) : null,
  };
}

async function requireSession(ctx: QueryContext, id: string) {
  const [session] = await loadSessions(ctx.db, [id]);
  if (!session) throw new QueryError(404, "session not found");
  return session;
}

function plural(count: number, noun: string) {
  return `${count} ${noun}${count === 1 ? "" : "s"}`;
}

export function receiptLine(session: Pick<Session, "capture" | "commit_count">) {
  const { capture } = session;
  const lead: Record<Session["capture"]["status"], string> = {
    saved: `Saved to Mimir · ${plural(capture.saved_exchanges, "exchange")}`,
    pending: `Saving to Mimir… · ${capture.pending_exchanges} pending`,
    partial: `Partially saved · ${capture.saved_exchanges} saved · ${capture.failed_exchanges} failed`,
    failed: `Mimir couldn't save this session · ${capture.failed_exchanges} failed`,
    empty: "Not captured",
  };
  return session.commit_count ? `${lead[capture.status]} · ${plural(session.commit_count, "commit")}` : lead[capture.status];
}

export function buildReceipt(ctx: QueryContext, session: Session): Receipt {
  return {
    session_id: session.id,
    title: session.display_title,
    repo: session.repo,
    harness: session.harness,
    device: session.device ? { id: session.device.id, name: session.device.name } : null,
    state: session.state,
    capture: session.capture,
    git_status: session.git_status,
    commit_count: session.commit_count,
    goal: session.goal,
    dashboard_url: ctx.dashboardOrigin ? `${ctx.dashboardOrigin}/dashboard/sessions/${encodeURIComponent(session.id)}` : null,
    line: receiptLine(session),
  };
}

export async function getReceipt(ctx: QueryContext, id: string) {
  return buildReceipt(ctx, await requireSession(ctx, id));
}

export async function getSession(ctx: QueryContext, id: string) {
  const session = await requireSession(ctx, id);
  const db = ctx.db;
  const [summary, files, errors, descendants] = await db.batch<Record<string, unknown>>([
    db.prepare("SELECT summary_status, summary_json, summary_updated_at FROM sessions WHERE id = ?").bind(id),
    db.prepare(
      `${SUBTREE_CTE} SELECT sf.file AS path, (SELECT COUNT(DISTINCT ef.exchange_id) FROM exchange_files ef JOIN exchanges e ON e.id = ef.exchange_id WHERE ef.file = sf.file AND ef.session_id IN (SELECT id FROM subtree) AND e.capture_status = 'saved') AS count FROM (SELECT DISTINCT file FROM session_files WHERE session_id IN (SELECT id FROM subtree)) sf ORDER BY count DESC, path LIMIT 200`,
    ).bind(id),
    db.prepare(
      `${SUBTREE_CTE} SELECT se.signature, (SELECT COUNT(DISTINCT ee.exchange_id) FROM exchange_errors ee JOIN exchanges e ON e.id = ee.exchange_id WHERE ee.signature = se.signature AND ee.session_id IN (SELECT id FROM subtree) AND e.capture_status = 'saved') AS count, (SELECT MAX(e.ts) FROM exchange_errors ee JOIN exchanges e ON e.id = ee.exchange_id WHERE ee.signature = se.signature AND ee.session_id IN (SELECT id FROM subtree) AND e.capture_status = 'saved') AS last_seen_at FROM (SELECT DISTINCT signature FROM session_errors WHERE session_id IN (SELECT id FROM subtree)) se ORDER BY count DESC, se.signature LIMIT 100`,
    ).bind(id),
    db.prepare(`${SUBTREE_CTE} SELECT id FROM sessions WHERE id IN (SELECT id FROM subtree) AND id <> ?2 ORDER BY started_at, id`).bind(id, id),
  ]);
  const commits = await commitRows(db, `${SUBTREE_CTE} SELECT a.* FROM session_git_artifacts a WHERE a.session_id IN (SELECT id FROM subtree) ORDER BY COALESCE(a.committed_at, a.created_at), a.created_at, a.commit_sha`, [id]);
  return {
    session,
    summary: storedSummary(summary!.results[0] as Parameters<typeof storedSummary>[0]),
    receipt: buildReceipt(ctx, session),
    files: files!.results as Array<{ path: string; count: number }>,
    errors: errors!.results as Array<{ signature: string; count: number; last_seen_at: string | null }>,
    commits,
    supporting_sessions: await loadSessions(db, (descendants!.results as Array<{ id: string }>).map((row) => row.id)),
  };
}

// setSessionTitle records a manual title; null clears it and restores the
// best generated or derived title.
export async function setSessionTitle(ctx: QueryContext, id: string, body: unknown) {
  if (!body || typeof body !== "object" || Array.isArray(body) || Object.keys(body).some((key) => key !== "title"))
    throw new QueryError(400, "body must contain only title");
  const value = (body as Record<string, unknown>).title;
  if (value !== null && typeof value !== "string") throw new QueryError(400, "title must be a string or null");
  const title = value === null ? null : normalizeSessionTitle(value);
  if (value !== null && !title) throw new QueryError(400, `title must be non-empty and at most ${MAX_SESSION_TITLE_CHARS} characters`);
  if (!(await ctx.db.prepare("SELECT 1 FROM sessions WHERE id = ?").bind(id).first())) throw new QueryError(404, "session not found");
  const now = new Date().toISOString();
  await ctx.db.batch(
    title
      ? [titleUpdateStatement(ctx.db, id, title, "manual", now)]
      : [
          ctx.db.prepare("UPDATE sessions SET title = NULL, title_source = NULL, title_updated_at = NULL WHERE id = ? AND title_source = 'manual'").bind(id),
          reconcileSessionTitleStatement(ctx.db, id, now),
        ],
  );
  return (await ctx.db
    .prepare(`SELECT id, title, title_source, ${DISPLAY_TITLE_SQL("sessions")} AS display_title FROM sessions WHERE id = ?`)
    .bind(id)
    .first())!;
}
