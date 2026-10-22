// Pure day-snapshot construction shared by the Worker route and the dashboard
// fixture build. Keep this module free of Worker bindings and storage access.

export type DayOutcome = "landed" | "discarded" | "abandoned" | "unresolved";
export type DayCaptureStatus = "empty" | "pending" | "saved" | "failed" | "partial";

export const DAY_BUCKET_MINUTES = 15;
const BUCKET_MS = DAY_BUCKET_MINUTES * 60_000;

export type DayRoot = {
  id: string;
  display_title: string | null;
  title_source: string | null;
  harness: string | null;
  repo: string | null;
  source_ref: string | null;
  model_primary: string | null;
  outcome: DayOutcome;
  outcome_reason: string | null;
  state: "active" | "inactive";
  started_at: string;
  activity_at: string;
  request_count: number;
  capture: { status: DayCaptureStatus };
};

export type DayActivityBucket = {
  root_id: string;
  bucket: number;
  requests: number;
  tokens_in: number;
  tokens_out: number;
  first_at: string;
  last_at: string;
};

export type DayCommitRow = {
  root_id: string;
  commit_sha: string;
  subject: string | null;
  ref: string | null;
  capture_status: string;
  patch_files: number;
  patch_additions: number;
  patch_deletions: number;
  committed_at: string | null;
  accepted_at: string;
};

export type DayQuietReason = "empty" | "single_request";

export type DaySession = {
  id: string;
  display_title: string | null;
  harness: string | null;
  repo: string | null;
  source_ref: string | null;
  model_primary: string | null;
  outcome: DayOutcome;
  outcome_reason: string | null;
  state: "active" | "inactive";
  capture_status: DayCaptureStatus;
  started_at: string;
  first_at: string;
  last_at: string;
  requests: number;
  tokens_in: number;
  tokens_out: number;
  sub_agents: number;
  commit_count: number;
  quiet: DayQuietReason | null;
  /** Sparse `[bucket, requests]` pairs relative to the day start. */
  activity: Array<[number, number]>;
};

export type DayCommit = {
  commit_sha: string;
  subject: string | null;
  ref: string | null;
  capture_status: string;
  patch_files: number;
  patch_additions: number;
  patch_deletions: number;
  at: string;
  session_ids: string[];
};

export type DayWorkstream = {
  id: string;
  label: string;
  repo: string | null;
  source_ref: string | null;
  session_ids: string[];
  first_at: string;
  last_at: string;
  requests: number;
  tokens_in: number;
  tokens_out: number;
  sub_agents: number;
  active: boolean;
  outcomes: Record<DayOutcome, number>;
  commits: DayCommit[];
};

export type DaySnapshot = {
  date: string;
  time_zone: string;
  start: string;
  end: string;
  bucket_minutes: number;
  buckets: number;
  previous_date: string | null;
  next_date: string | null;
  totals: {
    sessions: number;
    quiet_sessions: number;
    sub_agents: number;
    requests: number;
    tokens_in: number;
    tokens_out: number;
    commits: number;
    repos: number;
    active: number;
  };
  workstreams: DayWorkstream[];
  sessions: DaySession[];
  quiet_session_ids: string[];
  attention: { active: string[]; unresolved: string[]; capture: string[] };
};

export type DaySnapshotInput = {
  date: string;
  timeZone: string;
  start: string;
  end: string;
  previousDate: string | null;
  nextDate: string | null;
  roots: DayRoot[];
  activity: DayActivityBucket[];
  subAgents: Array<{ root_id: string; sub_agents: number }>;
  commits: DayCommitRow[];
};

const formatters = new Map<string, Intl.DateTimeFormat>();

function formatter(timeZone: string) {
  let value = formatters.get(timeZone);
  if (!value) {
    value = new Intl.DateTimeFormat("en-US", {
      timeZone, hourCycle: "h23", year: "numeric", month: "2-digit", day: "2-digit", hour: "2-digit", minute: "2-digit", second: "2-digit",
    });
    formatters.set(timeZone, value);
  }
  return value;
}

export function validTimeZone(timeZone: string) {
  if (!timeZone || timeZone.length > 64) return false;
  try {
    formatter(timeZone);
    return true;
  } catch {
    return false;
  }
}

export function validDate(date: string) {
  if (!/^\d{4}-\d{2}-\d{2}$/.test(date)) return false;
  const parsed = new Date(`${date}T00:00:00.000Z`);
  return Number.isFinite(parsed.getTime()) && parsed.toISOString().slice(0, 10) === date;
}

function wallClock(instant: number, timeZone: string) {
  const parts: Record<string, number> = {};
  for (const part of formatter(timeZone).formatToParts(new Date(instant))) {
    if (part.type !== "literal") parts[part.type] = Number(part.value);
  }
  return parts as { year: number; month: number; day: number; hour: number; minute: number; second: number };
}

function zoneOffset(instant: number, timeZone: string) {
  const wall = wallClock(instant, timeZone);
  return Date.UTC(wall.year, wall.month - 1, wall.day, wall.hour, wall.minute, wall.second) - Math.floor(instant / 1000) * 1000;
}

function zonedMidnight(date: string, timeZone: string) {
  const wall = Date.parse(`${date}T00:00:00.000Z`);
  const guess = wall - zoneOffset(wall, timeZone);
  return wall - zoneOffset(guess, timeZone);
}

export function shiftDate(date: string, days: number) {
  const parsed = new Date(`${date}T00:00:00.000Z`);
  parsed.setUTCDate(parsed.getUTCDate() + days);
  return parsed.toISOString().slice(0, 10);
}

/** UTC instants bounding the local calendar day; end is exclusive. */
export function zonedDayBounds(date: string, timeZone: string) {
  return {
    start: new Date(zonedMidnight(date, timeZone)).toISOString(),
    end: new Date(zonedMidnight(shiftDate(date, 1), timeZone)).toISOString(),
  };
}

export function zonedDate(instant: string, timeZone: string) {
  const wall = wallClock(Date.parse(instant), timeZone);
  return `${String(wall.year).padStart(4, "0")}-${String(wall.month).padStart(2, "0")}-${String(wall.day).padStart(2, "0")}`;
}

const STOPWORDS: Record<string, true> = Object.fromEntries([
  "the", "and", "for", "with", "from", "into", "onto", "that", "this", "these", "those", "then", "than", "when", "what", "which", "where", "why", "how",
  "are", "was", "were", "been", "being", "have", "has", "had", "you", "your", "our", "its", "can", "could", "should", "would", "will", "not", "but",
  "all", "any", "some", "out", "over", "about", "just", "also", "need", "want", "lets", "let", "please", "now", "new", "via", "per",
  "fix", "add", "update", "make", "use", "check", "implement", "improve", "support", "change", "remove", "work", "run", "set", "get", "handle",
].map((word) => [word, true]));

/** Leading harness tags such as `📋 PLAN:` describe the phase, not the work. */
const TAG_PREFIX = /^[^\p{L}\p{N}]*[A-Z][A-Z0-9]{1,15}:\s*/u;
const DEFAULT_REFS: Record<string, true> = { main: true, master: true, trunk: true, develop: true, dev: true, HEAD: true };

export function workTokens(text: string | null) {
  if (!text) return new Set<string>();
  const tokens = new Set<string>();
  for (const raw of text.replace(TAG_PREFIX, "").toLowerCase().split(/[^\p{L}\p{N}]+/u)) {
    if (raw.length < 3 || STOPWORDS[raw] === true || /^\d+$/.test(raw)) continue;
    tokens.add(raw.length > 4 && raw.endsWith("s") && !raw.endsWith("ss") ? raw.slice(0, -1) : raw);
  }
  return tokens;
}

/** Same-thread test: Jaccard >= 0.5 over at least two shared work tokens. */
export function sameThread(a: Set<string>, b: Set<string>) {
  let shared = 0;
  for (const token of a) if (b.has(token)) shared++;
  return shared >= 2 && shared / (a.size + b.size - shared) >= 0.5;
}

export function buildDaySnapshot(input: DaySnapshotInput): DaySnapshot {
  const buckets = Math.max(1, Math.round((Date.parse(input.end) - Date.parse(input.start)) / BUCKET_MS));
  const activity = new Map<string, DayActivityBucket[]>();
  for (const row of input.activity) {
    const rows = activity.get(row.root_id) ?? [];
    rows.push(row);
    activity.set(row.root_id, rows);
  }
  const subAgents = new Map(input.subAgents.map((row) => [row.root_id, Number(row.sub_agents) || 0]));
  const commitsByRoot = new Map<string, DayCommitRow[]>();
  for (const row of input.commits) {
    const rows = commitsByRoot.get(row.root_id) ?? [];
    rows.push(row);
    commitsByRoot.set(row.root_id, rows);
  }

  const sessions: DaySession[] = input.roots.map((root) => {
    const rows = activity.get(root.id) ?? [];
    const commitCount = new Set((commitsByRoot.get(root.id) ?? []).map((row) => row.commit_sha)).size;
    const requests = rows.reduce((sum, row) => sum + Number(row.requests), 0);
    const totalRequests = Math.max(Number(root.request_count) || 0, requests);
    const quiet: DayQuietReason | null = root.state === "active" || commitCount > 0 || root.outcome === "landed" || totalRequests > 1
      ? null
      : totalRequests === 0 ? "empty" : "single_request";
    const firstAt = rows.reduce<string | null>((min, row) => (!min || row.first_at < min ? row.first_at : min), null);
    const lastAt = rows.reduce<string | null>((max, row) => (!max || row.last_at > max ? row.last_at : max), null);
    return {
      id: root.id,
      display_title: root.display_title && root.display_title !== root.id ? root.display_title : null,
      harness: root.harness,
      repo: root.repo,
      source_ref: root.source_ref,
      model_primary: root.model_primary,
      outcome: root.outcome,
      outcome_reason: root.outcome_reason,
      state: root.state,
      capture_status: root.capture.status,
      started_at: root.started_at,
      first_at: firstAt ?? root.started_at,
      last_at: lastAt ?? root.started_at,
      requests,
      tokens_in: rows.reduce((sum, row) => sum + Number(row.tokens_in), 0),
      tokens_out: rows.reduce((sum, row) => sum + Number(row.tokens_out), 0),
      sub_agents: subAgents.get(root.id) ?? 0,
      commit_count: commitCount,
      quiet,
      activity: rows
        .filter((row) => row.bucket >= 0 && row.bucket < buckets)
        .sort((a, b) => a.bucket - b.bucket)
        .map((row): [number, number] => [row.bucket, Number(row.requests)]),
    };
  }).sort((a, b) => (a.last_at < b.last_at ? 1 : a.last_at > b.last_at ? -1 : a.id < b.id ? 1 : -1));

  const rootsByID = new Map(input.roots.map((root) => [root.id, root]));
  const working = sessions.filter((session) => !session.quiet);
  const parent = new Map(working.map((session) => [session.id, session.id]));
  const find = (id: string): string => {
    let current = id;
    while (parent.get(current) !== current) current = parent.get(current)!;
    parent.set(id, current);
    return current;
  };
  const union = (a: string, b: string) => {
    const left = find(a), right = find(b);
    if (left !== right) parent.set(left < right ? right : left, left < right ? left : right);
  };
  const tokens = new Map(working.map((session) => [session.id, workTokens(session.display_title)]));
  for (let i = 0; i < working.length; i++) {
    for (let j = i + 1; j < working.length; j++) {
      const a = working[i]!, b = working[j]!;
      if (a.repo !== b.repo) continue;
      const branch = a.repo !== null && a.source_ref !== null && a.source_ref === b.source_ref && DEFAULT_REFS[a.source_ref] !== true;
      if (branch || sameThread(tokens.get(a.id)!, tokens.get(b.id)!)) union(a.id, b.id);
    }
  }
  const commitOwners = new Map<string, string>();
  for (const row of input.commits) {
    if (!parent.has(row.root_id)) continue;
    const owner = commitOwners.get(row.commit_sha);
    if (owner && rootsByID.get(owner)?.repo === rootsByID.get(row.root_id)?.repo) union(owner, row.root_id);
    else if (!owner) commitOwners.set(row.commit_sha, row.root_id);
  }

  const groups = new Map<string, DaySession[]>();
  for (const session of working) {
    const key = find(session.id);
    const members = groups.get(key) ?? [];
    members.push(session);
    groups.set(key, members);
  }
  const workstreams: DayWorkstream[] = [...groups.values()].map((members) => {
    members.sort((a, b) => (a.first_at < b.first_at ? -1 : a.first_at > b.first_at ? 1 : 0));
    const dominant = members.reduce((best, session) => (session.requests > best.requests ? session : best), members[0]!);
    const refs = new Set(members.map((session) => session.source_ref));
    const ids = new Set(members.map((session) => session.id));
    const commits = new Map<string, DayCommit>();
    for (const row of input.commits) {
      if (!ids.has(row.root_id)) continue;
      const existing = commits.get(row.commit_sha);
      if (existing) {
        if (!existing.session_ids.includes(row.root_id)) existing.session_ids.push(row.root_id);
        if (row.capture_status === "saved") existing.capture_status = "saved";
        continue;
      }
      commits.set(row.commit_sha, {
        commit_sha: row.commit_sha, subject: row.subject, ref: row.ref, capture_status: row.capture_status,
        patch_files: Number(row.patch_files), patch_additions: Number(row.patch_additions), patch_deletions: Number(row.patch_deletions),
        at: row.committed_at ?? row.accepted_at, session_ids: [row.root_id],
      });
    }
    const outcomes: Record<DayOutcome, number> = { landed: 0, discarded: 0, abandoned: 0, unresolved: 0 };
    for (const session of members) outcomes[session.outcome]++;
    return {
      id: members[0]!.id,
      label: dominant.display_title ?? "Untitled session",
      repo: dominant.repo,
      source_ref: refs.size === 1 ? dominant.source_ref : null,
      session_ids: members.map((session) => session.id),
      first_at: members[0]!.first_at,
      last_at: members.reduce((max, session) => (session.last_at > max ? session.last_at : max), members[0]!.last_at),
      requests: members.reduce((sum, session) => sum + session.requests, 0),
      tokens_in: members.reduce((sum, session) => sum + session.tokens_in, 0),
      tokens_out: members.reduce((sum, session) => sum + session.tokens_out, 0),
      sub_agents: members.reduce((sum, session) => sum + session.sub_agents, 0),
      active: members.some((session) => session.state === "active"),
      outcomes,
      commits: [...commits.values()].sort((a, b) => (a.at < b.at ? -1 : a.at > b.at ? 1 : 0)),
    };
  }).sort((a, b) => Number(b.active) - Number(a.active) || (a.last_at < b.last_at ? 1 : a.last_at > b.last_at ? -1 : 0));

  const quiet = sessions.filter((session) => session.quiet);
  return {
    date: input.date,
    time_zone: input.timeZone,
    start: input.start,
    end: input.end,
    bucket_minutes: DAY_BUCKET_MINUTES,
    buckets,
    previous_date: input.previousDate,
    next_date: input.nextDate,
    totals: {
      sessions: sessions.length,
      quiet_sessions: quiet.length,
      sub_agents: sessions.reduce((sum, session) => sum + session.sub_agents, 0),
      requests: sessions.reduce((sum, session) => sum + session.requests, 0),
      tokens_in: sessions.reduce((sum, session) => sum + session.tokens_in, 0),
      tokens_out: sessions.reduce((sum, session) => sum + session.tokens_out, 0),
      commits: new Set(input.commits.map((row) => row.commit_sha)).size,
      repos: new Set(sessions.map((session) => session.repo).filter((repo) => repo !== null)).size,
      active: sessions.filter((session) => session.state === "active").length,
    },
    workstreams,
    sessions,
    quiet_session_ids: quiet.map((session) => session.id),
    attention: {
      active: working.filter((session) => session.state === "active").map((session) => session.id),
      unresolved: working.filter((session) => session.state === "inactive" && session.outcome === "unresolved").map((session) => session.id),
      capture: sessions.filter((session) => session.capture_status === "failed" || session.capture_status === "partial").map((session) => session.id),
    },
  };
}
