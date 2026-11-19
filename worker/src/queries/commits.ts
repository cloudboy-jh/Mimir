import { normalizeRepositoryUrl } from "../sessions/git-repository";
import { rootSessionID } from "../sessions/session-tree";
import { boundedLimit, decodeCursor, encodeCursor } from "../shared/cursors";
import { dateRange } from "../shared/date-range";
import { QueryError } from "../shared/query-error";
import type { QueryContext, QueryParams } from "./query-context";
import { DISPLAY_TITLE_SQL } from "./session-records";

export type Commit = {
  session_id: string;
  commit_sha: string;
  parent_commit_sha: string | null;
  subject: string | null;
  ref: string | null;
  repository_url: string | null;
  repository_key: string | null;
  committed_at: string | null;
  accepted_at: string;
  capture_status: string;
  pushed: boolean;
  pushed_at: string | null;
  patch_sha256: string | null;
  patch_bytes: number;
  patch_files: number;
  patch_additions: number;
  patch_deletions: number;
};

const KEY = "COALESCE(NULLIF(a.repository_key, ''), 'session:' || a.session_id)";

function toCommit(row: Record<string, unknown>): Commit {
  return {
    session_id: String(row.session_id),
    commit_sha: String(row.commit_sha),
    parent_commit_sha: (row.parent_commit_sha as string | null) ?? null,
    subject: (row.subject as string | null) ?? null,
    ref: (row.ref as string | null) ?? null,
    repository_url: (row.repository_url as string | null) ?? null,
    repository_key: (row.repository_key as string | null) || null,
    committed_at: (row.committed_at as string | null) ?? null,
    accepted_at: String(row.accepted_at),
    capture_status: String(row.capture_status),
    pushed: Number(row.pushed) === 1,
    pushed_at: (row.pushed_at as string | null) ?? null,
    patch_sha256: (row.patch_sha256 as string | null) ?? null,
    patch_bytes: Number(row.patch_bytes),
    patch_files: Number(row.patch_files),
    patch_additions: Number(row.patch_additions),
    patch_deletions: Number(row.patch_deletions),
  };
}

/** Runs a query selecting `session_git_artifacts` rows and projects them into commits. */
export async function commitRows(db: D1Database, sql: string, values: unknown[]) {
  const rows = await db.prepare(sql).bind(...values).all<Record<string, unknown>>();
  return rows.results.map(toCommit);
}

// listCommits groups saved and pending commit captures by repository identity
// and SHA, newest first. Each commit lists every session that captured it.
export async function listCommits(ctx: QueryContext, params: QueryParams) {
  const where: string[] = [];
  const values: string[] = [];
  const q = params.q?.trim();
  if (q) {
    if (q.length > 500) throw new QueryError(400, "q too long");
    where.push(`instr(lower(a.commit_sha || ' ' || COALESCE(a.subject, '') || ' ' || ${KEY} || ' ' || COALESCE(a.ref, '')), lower(?)) > 0`);
    values.push(q);
  }
  if (params.repo) {
    where.push(`(${KEY} = ? OR EXISTS (SELECT 1 FROM sessions s WHERE s.id = a.session_id AND s.repo = ?))`);
    values.push(normalizeRepositoryUrl(params.repo) ?? params.repo, params.repo);
  }
  if (params.ref) {
    where.push("a.ref = ?");
    values.push(params.ref);
  }
  if (params.session) {
    where.push("a.session_id = ?");
    values.push(await rootSessionID(ctx.db, params.session));
  }
  const range = dateRange(params.from, params.to);
  if (range.start) {
    where.push("julianday(COALESCE(a.committed_at, a.accepted_at)) >= julianday(?)");
    values.push(range.start);
  }
  if (range.end) {
    where.push("julianday(COALESCE(a.committed_at, a.accepted_at)) < julianday(?)");
    values.push(range.end);
  }
  const cursor = decodeCursor(params.cursor, (value) =>
    typeof value.t === "string" && typeof value.r === "string" && typeof value.s === "string" ? { t: value.t, r: value.r, s: value.s } : null,
  );
  const limit = boundedLimit(params.limit, 20, 100);
  const groups = await ctx.db
    .prepare(
      `WITH grouped AS (SELECT ${KEY} AS repository_key, a.commit_sha, MIN(COALESCE(a.committed_at, a.accepted_at)) AS sort_at FROM session_git_artifacts a ${where.length ? `WHERE ${where.join(" AND ")}` : ""} GROUP BY ${KEY}, a.commit_sha)
      SELECT * FROM grouped ${cursor ? "WHERE (sort_at, repository_key, commit_sha) < (?, ?, ?)" : ""} ORDER BY sort_at DESC, repository_key DESC, commit_sha DESC LIMIT ?`,
    )
    .bind(...values, ...(cursor ? [cursor.t, cursor.r, cursor.s] : []), limit + 1)
    .all<{ repository_key: string; commit_sha: string; sort_at: string }>();
  const page = groups.results.slice(0, limit);
  if (!page.length) return { commits: [], next_cursor: null };
  const captures = await ctx.db
    .prepare(
      `WITH selected(repository_key, commit_sha) AS (SELECT json_extract(value, '$[0]'), json_extract(value, '$[1]') FROM json_each(?))
      SELECT a.*, ${KEY} AS group_key, ${DISPLAY_TITLE_SQL("s")} AS display_title FROM session_git_artifacts a JOIN selected g ON g.repository_key = ${KEY} AND g.commit_sha = a.commit_sha JOIN sessions s ON s.id = a.session_id ORDER BY a.capture_status = 'saved' DESC, a.accepted_at, a.session_id`,
    )
    .bind(JSON.stringify(page.map((group) => [group.repository_key, group.commit_sha])))
    .all<Record<string, unknown>>();
  const byGroup = new Map<string, Array<Record<string, unknown>>>();
  for (const row of captures.results) {
    const key = `${row.group_key}\u0000${row.commit_sha}`;
    byGroup.set(key, [...(byGroup.get(key) ?? []), row]);
  }
  const last = page.at(-1)!;
  return {
    commits: page.map((group) => {
      const rows = byGroup.get(`${group.repository_key}\u0000${group.commit_sha}`) ?? [];
      return {
        ...toCommit(rows[0]!),
        repository_key: group.repository_key,
        pushed: rows.some((row) => Number(row.pushed) === 1),
        sessions: rows.map((row) => ({ id: String(row.session_id), display_title: String(row.display_title) })),
      };
    }),
    next_cursor: groups.results.length > limit ? encodeCursor({ t: last.sort_at, r: last.repository_key, s: last.commit_sha }) : null,
  };
}

/** Parses `max_bytes`; null means the whole patch. */
export function patchLimit(value: string | undefined, fallback: number | null) {
  if (value === undefined || value === "") return fallback;
  const parsed = Number(value);
  if (!Number.isInteger(parsed) || parsed < 1) throw new QueryError(400, "invalid max_bytes");
  return parsed;
}
