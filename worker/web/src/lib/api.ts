export type GitStatus = "none" | "committed" | "pushed";
export type CaptureStatus = "empty" | "pending" | "saved" | "failed" | "partial";

export type DashboardIdentity = {
  email: string | null;
  name: string | null;
  source: "cloudflare-access" | "local-development";
};

export type CaptureSummary = {
  status: CaptureStatus;
  saved_exchanges: number;
  failed_exchanges: number;
  pending_exchanges: number;
  last_saved_at: string | null;
};

export type DeviceIdentity = { id: string; name: string; platform: string; arch: string };

export type SessionModel = {
  name: string;
  request_count: number;
  first_seen_at: string | null;
  last_seen_at: string | null;
};

export type Session = {
  id: string;
  parent_session_id: string | null;
  started_at: string;
  ended_at: string | null;
  activity_at: string;
  state: "active" | "inactive";
  harness: string | null;
  repo: string | null;
  source_ref: string | null;
  model_primary: string | null;
  models: SessionModel[];
  title: string | null;
  title_source: string | null;
  display_title: string;
  intent: string | null;
  request_count: number;
  tokens_in: number;
  tokens_out: number;
  cache_read_tokens: number;
  cache_write_tokens: number;
  child_session_count: number;
  device: DeviceIdentity | null;
  capture: CaptureSummary;
  git_status: GitStatus;
  commit_count: number;
  goal: string | null;
};

export type SessionSummary = {
  status: "ready" | "pending" | "unavailable";
  updated_at: string | null;
  goal: string | null;
  actions: string[];
  result: string | null;
  verification: string[];
  unresolved: string[];
  partial: boolean;
  evidence: { section: string; index: number; exchange_id: string }[];
};

export type Receipt = {
  session_id: string;
  title: string;
  repo: string | null;
  harness: string | null;
  device: { id: string; name: string } | null;
  state: "active" | "inactive";
  capture: CaptureSummary;
  git_status: GitStatus;
  commit_count: number;
  goal: string | null;
  dashboard_url: string | null;
  line: string;
};

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

export type ToolCall = { name: string; input: string | null; output: string | null; error: boolean };

export type Turn = {
  exchange_id: string;
  session_id: string;
  ts: string;
  model: string | null;
  request_kind: "primary" | "title" | "summary" | "compaction";
  capture_status: string;
  user: string | null;
  assistant: string | null;
  reasoning: string | null;
  tools: ToolCall[];
  truncated: boolean;
};

export type Tailnet = {
  host_name: string | null;
  dns_name: string | null;
  tailnet_name: string | null;
  ips: string[];
  online: boolean | null;
  observed_at: string | null;
};

export type Device = DeviceIdentity & {
  created_at: string;
  updated_at: string;
  last_seen_at: string | null;
  revoked_at: string | null;
  session_count: number;
  harnesses: string[];
  tailnet: Tailnet | null;
};

export type SessionDetail = {
  session: Session;
  summary: SessionSummary | null;
  receipt: Receipt;
  files: { path: string; count: number }[];
  errors: { signature: string; count: number; last_seen_at: string | null }[];
  commits: Commit[];
  supporting_sessions: Session[];
};

export type SessionFilters = {
  q?: string;
  repo?: string;
  harness?: string;
  device?: string;
  state?: string;
  git_status?: string;
  from?: string;
  to?: string;
  limit?: number;
  cursor?: string;
};

export type StatsGroupBy = "repo" | "harness" | "device" | "model" | "day";

export type Stats = {
  from: string;
  to: string;
  group_by: StatsGroupBy;
  groups: { key: string; sessions: number; requests: number; tokens_in: number; tokens_out: number; commits: number }[];
};

export type TitleUpdate = Pick<Session, "id" | "title" | "title_source" | "display_title">;

export type DaySession = {
  id: string;
  display_title: string | null;
  harness: string | null;
  repo: string | null;
  source_ref: string | null;
  model_primary: string | null;
  git_status: GitStatus;
  state: "active" | "inactive";
  capture_status: CaptureStatus;
  started_at: string;
  first_at: string;
  last_at: string;
  requests: number;
  tokens_in: number;
  tokens_out: number;
  sub_agents: number;
  commit_count: number;
  quiet: "empty" | "single_request" | null;
  /** Sparse `[bucket, requests]` pairs counted from the local day start. */
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
  git: Record<GitStatus, number>;
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
  totals: { sessions: number; quiet_sessions: number; sub_agents: number; requests: number; tokens_in: number; tokens_out: number; commits: number; repos: number; active: number };
  workstreams: DayWorkstream[];
  sessions: DaySession[];
  quiet_session_ids: string[];
  attention: { active: string[]; capture: string[] };
};

export type Patch = { text: string; truncated: boolean; bytes: number };

export class ApiError extends Error {
  constructor(message: string, readonly status: number) {
    super(message);
  }
}

const BASE = "/dashboard/api";
const PATCH_MAX_BYTES = 4 * 1024 * 1024;

async function send(path: string, init: RequestInit, accept: string): Promise<Response> {
  const response = await fetch(`${BASE}${path}`, {
    cache: "no-store",
    credentials: "same-origin",
    redirect: "manual",
    ...init,
    headers: { accept, "X-Requested-With": "XMLHttpRequest", ...init.headers },
  });
  // Cloudflare Access answers an expired session with a redirect or its HTML login page.
  if (response.type === "opaqueredirect" || (response.status >= 300 && response.status < 400) || (response.ok && response.headers.get("content-type")?.includes("text/html"))) {
    window.dispatchEvent(new CustomEvent("mimir:auth-required"));
    throw new ApiError("Cloudflare Access authentication required.", 403);
  }
  if (!response.ok) {
    const body = await response.json().catch(() => null) as { error?: string } | null;
    if (response.status === 401 || response.status === 403) window.dispatchEvent(new CustomEvent("mimir:auth-required"));
    const fallback = response.status === 403 ? "Cloudflare Access denied this request." : `Request failed (${response.status}).`;
    throw new ApiError(body?.error ?? fallback, response.status);
  }
  return response;
}

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  return (await send(path, init, "application/json")).json() as Promise<T>;
}

function json(method: string, body: unknown, signal?: AbortSignal): RequestInit {
  return { method, signal, headers: { "content-type": "application/json" }, body: JSON.stringify(body) };
}

function query(params: Record<string, string | number | undefined>) {
  const search = new URLSearchParams();
  for (const [key, value] of Object.entries(params)) if (value !== undefined && value !== "") search.set(key, String(value));
  const text = search.toString();
  return text ? `?${text}` : "";
}

const sessionPath = (id: string) => `/sessions/${encodeURIComponent(id)}`;

export function errorMessage(error: unknown, fallback: string) {
  return error instanceof Error ? error.message : fallback;
}

export function getIdentity(signal?: AbortSignal) {
  return request<DashboardIdentity>("/identity", { signal });
}

export function listSessions(filters: SessionFilters, signal?: AbortSignal) {
  return request<{ sessions: Session[]; next_cursor: string | null }>(`/sessions${query(filters)}`, { signal });
}

export function getSession(id: string, signal?: AbortSignal) {
  return request<SessionDetail>(sessionPath(id), { signal });
}

export function listTurns(id: string, options: { cursor?: string; limit?: number; order: "asc" | "desc" }, signal?: AbortSignal) {
  return request<{ turns: Turn[]; next_cursor: string | null }>(`${sessionPath(id)}/turns${query(options)}`, { signal });
}

export function setSessionTitle(id: string, title: string | null, signal?: AbortSignal) {
  return request<TitleUpdate>(`${sessionPath(id)}/title`, json("PATCH", { title }, signal));
}

export async function getCommitPatch(sessionId: string, sha: string, signal?: AbortSignal): Promise<Patch> {
  const response = await send(`${sessionPath(sessionId)}/commits/${encodeURIComponent(sha)}/patch${query({ max_bytes: PATCH_MAX_BYTES })}`, { signal }, "text/plain");
  return {
    text: await response.text(),
    truncated: response.headers.get("x-mimir-patch-truncated") === "true",
    bytes: Number(response.headers.get("x-mimir-patch-bytes")) || 0,
  };
}

export function getStats(groupBy: StatsGroupBy, range: { from?: string; to?: string } = {}, signal?: AbortSignal) {
  return request<Stats>(`/stats${query({ group_by: groupBy, ...range })}`, { signal });
}

export function getDay(date: string, timeZone: string, signal?: AbortSignal) {
  return request<DaySnapshot>(`/days/${encodeURIComponent(date)}${query({ tz: timeZone })}`, { signal });
}

export function listDevices(signal?: AbortSignal) {
  return request<{ devices: Device[] }>("/devices", { signal });
}

export function renameDevice(id: string, name: string, signal?: AbortSignal) {
  return request<Device>(`/devices/${encodeURIComponent(id)}`, json("PATCH", { name: name.trim() }, signal));
}

export function revokeDevice(id: string, signal?: AbortSignal) {
  return request<Device>(`/devices/${encodeURIComponent(id)}/revoke`, { method: "POST", signal });
}
