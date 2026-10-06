import type { CommitCapture, CommitTimelineEntry, CommitRepository } from "./api";

export function fixtureDateMatches(timestamp: string, params: URLSearchParams): boolean {
  const from = params.get("from");
  const to = params.get("to");
  if (from && !Number.isFinite(Date.parse(from)) || to && !Number.isFinite(Date.parse(to))) throw new Error("Invalid fixture date filter.");
  const ts = Date.parse(timestamp);
  if (from && ts < Date.parse(from)) return false;
  if (to) {
    const end = Date.parse(to);
    if (/^\d{4}-\d{2}-\d{2}$/.test(to) ? ts >= end + 86_400_000 : ts > end) return false;
  }
  return true;
}

export function fixtureRepositoryKey(url: string | null, sessionId: string): string {
  if (!url) return `session:${sessionId}`;
  const remote = new URL(url);
  let path = remote.pathname.replace(/\/+$/, "").replace(/\.git$/i, "");
  if (remote.hostname === "github.com" || remote.hostname === "bitbucket.org") path = path.toLowerCase();
  return `https://${remote.hostname.toLowerCase()}${path}`;
}

export function fixtureRepositoryPage(captures: CommitCapture[], params: URLSearchParams) {
  const groups = new Map<string, { option: CommitRepository; commits: Set<string> }>();
  for (const capture of captures) {
    const key = fixtureRepositoryKey(capture.repository_url, capture.session_id);
    let group = groups.get(key);
    if (!group) {
      const remote = key.startsWith("session:") ? null : new URL(key);
      group = { option: { repository_key: key, name: remote ? remote.pathname.slice(1) : `Unknown repository: ${capture.session_title}`,
        host: remote?.host ?? null, session_id: remote ? null : capture.session_id, commit_count: 0, capture_count: 0 }, commits: new Set() };
      groups.set(key, group);
    }
    group.commits.add(capture.commit_sha);
    group.option.commit_count = group.commits.size;
    group.option.capture_count++;
  }
  const cursor = params.get("cursor");
  if (cursor && cursor.length > 4096) throw new Error("Invalid repository cursor.");
  const rank = (key: string) => key.startsWith("session:") ? 1 : 0;
  const ordered = [...groups.values()].map((group) => group.option)
    .sort((a, b) => rank(a.repository_key) - rank(b.repository_key) || (a.repository_key < b.repository_key ? -1 : a.repository_key > b.repository_key ? 1 : 0))
    .filter((option) => !cursor || rank(option.repository_key) > rank(cursor) || rank(option.repository_key) === rank(cursor) && option.repository_key > cursor);
  const limit = Math.min(50, Math.max(1, Math.trunc(Number(params.get("limit")) || 25)));
  const repositories = ordered.slice(0, limit);
  return { repositories, next_cursor: ordered.length > limit ? repositories.at(-1)!.repository_key : null };
}

export function fixtureRefPage(captures: CommitCapture[], params: URLSearchParams) {
  const repo = params.get("repo"), cursor = params.get("cursor");
  if (!repo || repo.length > 4096 || (cursor && cursor.length > 4096)) throw new Error("Invalid repository selection.");
  const refs = new Map<string, Set<string>>();
  for (const capture of captures) {
    if (fixtureRepositoryKey(capture.repository_url, capture.session_id) !== repo || !capture.ref || (cursor && capture.ref <= cursor)) continue;
    const commits = refs.get(capture.ref) ?? new Set<string>();
    commits.add(capture.commit_sha); refs.set(capture.ref, commits);
  }
  const ordered = [...refs].sort(([a], [b]) => a < b ? -1 : a > b ? 1 : 0).map(([ref, commits]) => ({ ref, commit_count: commits.size }));
  const limit = Math.min(50, Math.max(1, Math.trunc(Number(params.get("limit")) || 25)));
  const page = ordered.slice(0, limit);
  return { refs: page, next_cursor: ordered.length > limit ? page.at(-1)!.ref : null };
}

export function fixtureCommitPage(captures: CommitCapture[], params: URLSearchParams) {
  const grouped = new Map<string, { entry: CommitTimelineEntry; sortAt: string }>();
  for (const capture of captures) {
    const key = fixtureRepositoryKey(capture.repository_url, capture.session_id);
    const groupKey = `${key}\n${capture.commit_sha}`;
    const sortAt = capture.committed_at ?? capture.created_at;
    const group = grouped.get(groupKey);
    if (group) {
      group.entry.captures.push(capture);
      group.entry.capture_count++;
      if (sortAt < group.sortAt) group.sortAt = sortAt;
    } else {
      grouped.set(groupKey, { sortAt, entry: {
        repository_key: key, commit_sha: capture.commit_sha, subject: capture.subject,
        committed_at: capture.committed_at, repository_url: capture.repository_url, ref: capture.ref,
        patch_files: capture.patch_files, patch_additions: capture.patch_additions, patch_deletions: capture.patch_deletions,
        captures: [capture], capture_count: 1, captures_next_cursor: null,
      } });
    }
  }
  const q = (params.get("q") ?? "").toLowerCase();
  const matching = [...grouped.values()].filter(({ entry }) => entry.captures.some((capture) => {
    if (!fixtureDateMatches(capture.committed_at ?? capture.created_at, params)) return false;
    const repo = params.get("repo");
    if (repo && repo !== entry.repository_key && repo !== capture.repo) return false;
    for (const [parameter, field] of [["session", "session_id"], ["ref", "ref"], ["outcome", "outcome"], ["capture_status", "capture_status"]] as const) {
      const value = params.get(parameter);
      if (value && capture[field] !== value) return false;
    }
    return !q || [capture.commit_sha, capture.subject, entry.repository_key, capture.ref, capture.session_title, capture.repo].some((field) => field?.toLowerCase().includes(q));
  })).sort((a, b) => b.sortAt.localeCompare(a.sortAt) || b.entry.repository_key.localeCompare(a.entry.repository_key) || b.entry.commit_sha.localeCompare(a.entry.commit_sha));
  let after = matching;
  const cursor = params.get("cursor");
  if (cursor) {
    const value = JSON.parse(decodeURIComponent(atob(cursor.replace(/-/g, "+").replace(/_/g, "/")))) as { v: number; t: string; r: string; s: string };
    if (value.v !== 1 || !value.t || !value.r || !value.s) throw new Error("Invalid fixture commit cursor.");
    after = matching.filter(({ entry, sortAt }) => sortAt < value.t || (sortAt === value.t && (entry.repository_key < value.r || (entry.repository_key === value.r && entry.commit_sha < value.s))));
  }
  const limit = Math.min(25, Math.max(1, Math.trunc(Number(params.get("limit")) || 25)));
  const page = after.slice(0, limit);
  const last = page.at(-1);
  return {
    commits: page.map(({ entry }) => {
      const captures = [...entry.captures].sort((a, b) => a.session_id.localeCompare(b.session_id));
      return { ...entry, captures: captures.slice(0, 50), captures_next_cursor: captures.length > 50 ? captures[49]!.session_id : null };
    }),
    next_cursor: after.length > limit && last ? btoa(encodeURIComponent(JSON.stringify({ v: 1, t: last.sortAt, r: last.entry.repository_key, s: last.entry.commit_sha }))).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "") : null,
  };
}

export function fixtureRowPage<T extends { id: string }>(rows: T[], params: URLSearchParams, timestamp: (row: T) => string, defaultLimit = 25, sessions = false) {
  const order = sessions ? "desc" : params.get("order") ?? "desc";
  if (order !== "asc" && order !== "desc") throw new Error("Invalid fixture request order.");
  const asc = order === "asc";
  const ordered = [...rows].sort((a, b) => {
    const comparison = timestamp(a).localeCompare(timestamp(b)) || a.id.localeCompare(b.id);
    return asc ? comparison : -comparison;
  });
  let filtered = ordered;
  const cursor = params.get("cursor");
  if (cursor) {
    const value = JSON.parse(atob(cursor.replace(/-/g, "+").replace(/_/g, "/"))) as { ts: string; id: string; order?: string };
    if (!value.ts || !value.id || (!sessions && value.order !== order)) throw new Error("Invalid fixture pagination cursor.");
    filtered = ordered.filter((row) => {
      const comparison = timestamp(row).localeCompare(value.ts) || row.id.localeCompare(value.id);
      return asc ? comparison > 0 : comparison < 0;
    });
  }
  const rawLimit = params.has("limit") ? Number(params.get("limit")) : defaultLimit;
  const limit = Number.isFinite(rawLimit) ? Math.max(1, Math.min(100, Math.trunc(rawLimit))) : 25;
  const page = filtered.slice(0, limit);
  const last = page.at(-1);
  const next_cursor = filtered.length > limit && last
    ? btoa(JSON.stringify({ ts: timestamp(last), id: last.id, ...(!sessions ? { order } : {}) })).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "")
    : null;
  return { page, next_cursor };
}
