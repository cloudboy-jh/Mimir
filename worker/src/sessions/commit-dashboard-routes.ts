import type { Hono } from "hono";
import type { AppEnv } from "../env";
import { boundedLimit } from "../dashboard/cursors";
import type { GitArtifact } from "./git-artifacts";
import { normalizeRepositoryUrl } from "./git-repository";

type Capture = GitArtifact & { session_id: string; session_title: string; repo: string | null; outcome: string };
type Group = { repository_key: string; commit_sha: string; sort_at: string };
type CaptureRow = Capture & { repository_key: string; group_repository_key: string; capture_rank: number; capture_count: number };
const KEY = "COALESCE(NULLIF(a.repository_key, ''), 'session:' || a.session_id)";
const CAPTURE_COLUMNS = "a.*, COALESCE(s.title, substr(s.intent, 1, 100), s.id) AS session_title, s.repo, s.work_outcome AS outcome";
const CAPTURE_LIMIT = 50;

// URI escaping makes the cursor UTF-8 safe even for Unicode repository paths.
export function encodeCommitCursor(group: Group): string {
  return btoa(encodeURIComponent(JSON.stringify({ v: 1, t: group.sort_at, r: group.repository_key, s: group.commit_sha }))).replaceAll("+", "-").replaceAll("/", "_").replaceAll("=", "");
}
function decodeCommitCursor(value: string): Group | null {
  if (value.length > 16_384) return null;
  try {
    const raw = JSON.parse(decodeURIComponent(atob(value.replaceAll("-", "+").replaceAll("_", "/")))) as Record<string, unknown>;
    return raw.v === 1 && typeof raw.t === "string" && !Number.isNaN(Date.parse(raw.t)) && typeof raw.r === "string" && raw.r.length > 0 && typeof raw.s === "string" && /^[0-9a-f]{40}$/.test(raw.s)
      ? { sort_at: raw.t, repository_key: raw.r, commit_sha: raw.s } : null;
  } catch { return null; }
}
function publicCapture(row: CaptureRow): Capture {
  const { repository_key: _key, group_repository_key: _group, capture_rank: _rank, capture_count: _count, ...capture } = row;
  return capture;
}

export function registerDashboardCommitRoutes(app: Hono<AppEnv>) {
  app.get("/dashboard/api/commits/repositories", async (c) => {
    const cursor = c.req.query("cursor");
    if (cursor && cursor.length > 4096) return c.json({ error: "invalid repository cursor" }, 400);
    const limit = Math.min(boundedLimit(c.req.query("limit")), 50);
    const rows = await c.env.DB.prepare(`WITH repositories AS (
      SELECT ${KEY} AS repository_key, COUNT(DISTINCT a.commit_sha) AS commit_count,
        COUNT(*) AS capture_count, MIN(COALESCE(s.title, s.intent, s.id)) AS session_title
      FROM session_git_artifacts a JOIN sessions s ON s.id = a.session_id GROUP BY ${KEY}
    ) SELECT *, CASE WHEN repository_key LIKE 'session:%' THEN 1 ELSE 0 END AS unknown
      FROM repositories ${cursor ? "WHERE (CASE WHEN repository_key LIKE 'session:%' THEN 1 ELSE 0 END, repository_key) > (?, ?)" : ""}
      ORDER BY unknown, repository_key LIMIT ?`)
      .bind(...(cursor ? [cursor.startsWith("session:") ? 1 : 0, cursor] : []), limit + 1)
      .all<{ repository_key: string; commit_count: number; capture_count: number; session_title: string; unknown: number }>();
    const page = rows.results.slice(0, limit);
    return c.json({
      repositories: page.map((row) => {
        const url = normalizeRepositoryUrl(row.repository_key);
        const remote = url ? new URL(url) : null;
        return { repository_key: row.repository_key, name: remote ? remote.pathname.slice(1) : `Unknown repository: ${row.session_title}`,
          host: remote?.host ?? null, commit_count: row.commit_count, capture_count: row.capture_count,
          session_id: row.unknown ? row.repository_key.slice(8) : null };
      }),
      next_cursor: rows.results.length > limit ? page[page.length - 1]!.repository_key : null,
    });
  });

  app.get("/dashboard/api/commits/refs", async (c) => {
    const repo = c.req.query("repo"), cursor = c.req.query("cursor");
    if (!repo || repo.length > 4096 || (cursor && cursor.length > 4096)) return c.json({ error: "invalid repository selection" }, 400);
    const limit = Math.min(boundedLimit(c.req.query("limit")), 50);
    const rows = await c.env.DB.prepare(`SELECT a.ref, COUNT(DISTINCT a.commit_sha) AS commit_count
      FROM session_git_artifacts a WHERE ${KEY} = ? AND a.ref IS NOT NULL AND a.ref <> ''
      ${cursor ? "AND a.ref > ?" : ""} GROUP BY a.ref ORDER BY a.ref LIMIT ?`)
      .bind(normalizeRepositoryUrl(repo) ?? repo, ...(cursor ? [cursor] : []), limit + 1)
      .all<{ ref: string; commit_count: number }>();
    const page = rows.results.slice(0, limit);
    return c.json({ refs: page, next_cursor: rows.results.length > limit ? page[page.length - 1]!.ref : null });
  });

  app.get("/dashboard/api/commits", async (c) => {
    const where: string[] = [], values: string[] = [];
    const q = c.req.query("q")?.trim();
    if (q) {
      if (q.length > 500) return c.json({ error: "search too long" }, 400);
      where.push(`instr(lower(a.commit_sha || ' ' || COALESCE(a.subject, '') || ' ' || ${KEY} || ' ' || COALESCE(a.ref, '') || ' ' || COALESCE(s.title, s.intent, s.id) || ' ' || COALESCE(s.repo, '')), lower(?)) > 0`);
      values.push(q);
    }
    const repo = c.req.query("repo");
    if (repo) { where.push(`${KEY} = ?`); values.push(normalizeRepositoryUrl(repo) ?? repo); }
    for (const [param, column] of [["ref", "a.ref"], ["session", "a.session_id"]] as const) {
      const value = c.req.query(param);
      if (value) { where.push(`${column} = ?`); values.push(value); }
    }
    const outcome = c.req.query("outcome");
    if (outcome) {
      if (!["landed", "discarded", "abandoned", "unresolved"].includes(outcome)) return c.json({ error: "invalid outcome" }, 400);
      where.push("s.work_outcome = ?"); values.push(outcome);
    }
    const capture = c.req.query("capture_status");
    if (capture) {
      if (!["accepted", "saved", "failed"].includes(capture)) return c.json({ error: "invalid capture_status" }, 400);
      where.push("a.capture_status = ?"); values.push(capture);
    }
    let from: string | undefined, to: string | undefined;
    for (const param of ["from", "to"] as const) {
      const value = c.req.query(param);
      if (!value) continue;
      if (Number.isNaN(Date.parse(value))) return c.json({ error: `invalid ${param}` }, 400);
      const iso = new Date(value).toISOString();
      if (param === "from") from = iso; else to = iso;
      where.push(`COALESCE(a.committed_at, a.created_at) ${param === "from" ? ">=" : "<="} ?`); values.push(iso);
    }
    if (from && to && from > to) return c.json({ error: "from must not exceed to" }, 400);
    const rawCursor = c.req.query("cursor");
    const cursor = rawCursor ? decodeCommitCursor(rawCursor) : null;
    if (rawCursor && !cursor) return c.json({ error: "invalid cursor" }, 400);
    const cursorWhere = cursor ? "AND (g.sort_at, g.repository_key, g.commit_sha) < (?, ?, ?)" : "";
    const limit = Math.min(boundedLimit(c.req.query("limit")), 25);
    const groups = await c.env.DB.prepare(`WITH grouped AS (
      SELECT ${KEY} AS repository_key, a.commit_sha, MIN(COALESCE(a.committed_at, a.created_at)) AS sort_at
      FROM session_git_artifacts a GROUP BY ${KEY}, a.commit_sha
    ) SELECT g.* FROM grouped g WHERE EXISTS (
      SELECT 1 FROM session_git_artifacts a JOIN sessions s ON s.id = a.session_id
      WHERE ${KEY} = g.repository_key AND a.commit_sha = g.commit_sha ${where.length ? `AND ${where.join(" AND ")}` : ""}
    ) ${cursorWhere} ORDER BY g.sort_at DESC, g.repository_key DESC, g.commit_sha DESC LIMIT ?`)
      .bind(...values, ...(cursor ? [cursor.sort_at, cursor.repository_key, cursor.commit_sha] : []), limit + 1).all<Group>();
    const page = groups.results.slice(0, limit);
    if (!page.length) return c.json({ commits: [], next_cursor: null });
    const selected = page.map(() => "(?, ?)").join(", ");
    const captures = await c.env.DB.prepare(`WITH selected(repository_key, commit_sha) AS (VALUES ${selected}), ranked AS (
      SELECT ${CAPTURE_COLUMNS}, ${KEY} AS group_repository_key,
        ROW_NUMBER() OVER (PARTITION BY ${KEY}, a.commit_sha ORDER BY a.session_id) AS capture_rank,
        COUNT(*) OVER (PARTITION BY ${KEY}, a.commit_sha) AS capture_count
      FROM session_git_artifacts a JOIN selected g ON g.repository_key = ${KEY} AND g.commit_sha = a.commit_sha
      JOIN sessions s ON s.id = a.session_id
    ) SELECT * FROM ranked WHERE capture_rank <= ? ORDER BY group_repository_key, commit_sha, session_id`)
      .bind(...page.flatMap((group) => [group.repository_key, group.commit_sha]), CAPTURE_LIMIT).all<CaptureRow>();
    const byGroup = new Map<string, CaptureRow[]>();
    for (const row of captures.results) {
      const key = JSON.stringify([row.group_repository_key, row.commit_sha]);
      const entries = byGroup.get(key);
      if (entries) entries.push(row); else byGroup.set(key, [row]);
    }
    const commits = page.map((group) => {
      const rows = byGroup.get(JSON.stringify([group.repository_key, group.commit_sha])) ?? [];
      const first = rows[0];
      return {
        repository_key: group.repository_key, commit_sha: group.commit_sha,
        subject: first?.subject ?? null, committed_at: first?.committed_at ?? null,
        repository_url: normalizeRepositoryUrl(first?.repository_url), ref: first?.ref ?? null,
        patch_files: first?.patch_files ?? 0, patch_additions: first?.patch_additions ?? 0, patch_deletions: first?.patch_deletions ?? 0,
        captures: rows.map(publicCapture), capture_count: first?.capture_count ?? 0,
        captures_next_cursor: first && first.capture_count > rows.length ? rows[rows.length - 1]!.session_id : null,
      };
    });
    return c.json({ commits, next_cursor: groups.results.length > limit ? encodeCommitCursor(page[page.length - 1]!) : null });
  });

  app.get("/dashboard/api/commits/captures", async (c) => {
    const repo = c.req.query("repo"), commit = c.req.query("commit"), cursor = c.req.query("cursor");
    if (!repo || repo.length > 4096 || !commit || !/^[0-9a-f]{40}$/.test(commit) || (cursor && cursor.length > 500)) return c.json({ error: "invalid capture selection" }, 400);
    const limit = Math.min(boundedLimit(c.req.query("limit")), CAPTURE_LIMIT);
    const rows = await c.env.DB.prepare(`SELECT ${CAPTURE_COLUMNS}, ${KEY} AS group_repository_key FROM session_git_artifacts a JOIN sessions s ON s.id = a.session_id
      WHERE ${KEY} = ? AND a.commit_sha = ? ${cursor ? "AND a.session_id > ?" : ""} ORDER BY a.session_id LIMIT ?`)
      .bind(repo, commit, ...(cursor ? [cursor] : []), limit + 1).all<CaptureRow>();
    const captures = rows.results.slice(0, limit);
    return c.json({ captures: captures.map(publicCapture), next_cursor: rows.results.length > limit ? captures[captures.length - 1]!.session_id : null });
  });
}
