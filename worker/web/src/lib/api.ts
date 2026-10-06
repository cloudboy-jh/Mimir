import { fixtureDataEnabled } from "./data-source";
import { fixtureRequest } from "@/lib/fixture-provider";

export type Outcome = "landed" | "discarded" | "abandoned" | "unresolved";
export type CaptureStatus = "empty" | "pending" | "saved" | "failed" | "partial";
export type SessionLiveness = "active" | "disconnected" | "finalized";
export type RequestKind = "primary" | "title" | "summary" | "compaction";
export type ExchangeCaptureStatus = "accepted" | "saved" | "failed" | "skipped";

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

export type DeviceIdentity = {
  id: string;
  name: string;
  platform: string;
  arch: string;
};

export type Device = DeviceIdentity & {
  created_at: string;
  updated_at: string;
  last_seen_at: string | null;
  revoked_at: string | null;
  session_count: number;
  harnesses: string[];
};

export type Session = {
  id: string;
  parent_session_id: string | null;
  started_at: string;
  ended_at: string | null;
  state: "active" | "inactive";
  liveness: SessionLiveness;
  last_active_at: string | null;
  activity_at: string;
  inactive_at: string | null;
  harness: string | null;
  boundary: string;
  outcome: Outcome;
  outcome_src: "agent" | "user" | "git" | "auto" | null;
  outcome_updated_at: string | null;
  outcome_reason: string | null;
  repo: string | null;
  source_ref: string | null;
  model_primary: string | null;
  models: SessionModel[];
  request_count: number;
  tokens_in: number;
  tokens_out: number;
  cache_read_tokens?: number;
  cache_write_tokens?: number;
  title: string | null;
  title_source: string | null;
  title_updated_at: string | null;
  display_title: string | null;
  intent: string | null;
  summary_text: string | null;
  summary_status: "pending" | "ready" | "unavailable";
  summary_source: string | null;
  summary_updated_at: string | null;
  child_session_count: number;
  capture: CaptureSummary;
  device: DeviceIdentity | null;
};

export type SessionModel = {
  name: string;
  request_count: number;
  first_seen_at: string | null;
  last_seen_at: string | null;
};

export type Exchange = {
  id: string;
  session_id: string;
  ts: string;
  model: string;
  provider: string | null;
  finish_reason: string | null;
  endpoint: string;
  latency_ms: number;
  repo: string | null;
  harness: string | null;
  access_token_label: string;
  input_tokens: number;
  output_tokens: number;
  cache_read_tokens?: number;
  cache_write_tokens?: number;
  r2_key: string;
  request_excerpt?: string;
  response_excerpt?: string;
  request_kind?: RequestKind;
  capture_status?: string;
  capture_reason?: string | null;
  failure_code?: string | null;
};

export type SessionExchange = Pick<Exchange, "id" | "session_id" | "ts" | "model" | "provider" | "finish_reason" | "latency_ms" | "harness" | "input_tokens" | "output_tokens" | "cache_read_tokens" | "cache_write_tokens"> & {
  request_excerpt: string;
  response_excerpt: string;
  request_kind: RequestKind;
  capture_status: string;
  capture_reason: string | null;
  failure_code: string | null;
  tool_names?: string[];
  has_errors?: boolean;
};

export type LiveSessionTurn = {
  ts: string;
  exchange_id?: string;
  model?: string;
  provider?: string | null;
  request_kind?: "primary" | "title" | "summary" | "compaction";
  usage?: {
    input_tokens: number;
    output_tokens: number;
    cache_read_tokens?: number;
    cache_write_tokens?: number;
  };
  latency_ms?: number;
  excerpt?: string;
};

export type SessionObjectState = {
  session_id: string;
  parent_session_id: string | null;
  liveness: SessionLiveness;
  harness: string | null;
  repo: string | null;
  started_at: string;
  last_event_at: string;
  finalized_at: string | null;
  end_reason: string | null;
  turn_count: number;
  tokens_in: number;
  tokens_out: number;
  cache_read_tokens?: number;
  cache_write_tokens?: number;
};

type LiveSessionEvent = {
  version: 1;
  kind: "turn" | "heartbeat" | "end";
  session_id: string;
  harness: string | null;
  ts: string;
  turn?: Omit<LiveSessionTurn, "ts">;
  reason?: string;
};

export type SessionLiveMessage =
  | { type: "snapshot"; state: SessionObjectState; turns: LiveSessionTurn[] }
  | { type: "event"; event: LiveSessionEvent }
  | { type: "finalized"; session_id: string; reason: string; ended_at: string }
  | { type: "reopened"; session_id: string };

export type SessionError = {
  signature: string;
  count: number;
  first_seen_at: string | null;
  last_seen_at: string | null;
  latest_exchange_id: string | null;
};

export type OutcomeEvent = {
  id: string;
  outcome: Outcome;
  source: string;
  reason: string | null;
  evidence_json: string | null;
  created_at: string;
};

export type GitArtifact = {
  commit_sha: string;
  parent_commit_sha: string | null;
  committed_at: string | null;
  subject: string | null;
  repository_url: string | null;
  ref: string | null;
  provenance: string;
  patch_r2_key: string;
  patch_sha256: string;
  patch_bytes: number;
  patch_files: number;
  patch_additions: number;
  patch_deletions: number;
  capture_status: "accepted" | "saved" | "failed";
  accepted_at: string;
  saved_at: string | null;
  failed_at: string | null;
  failure_code: string | null;
  created_at: string;
};

export type CommitCapture = GitArtifact & {
  session_id: string;
  session_title: string;
  repo: string | null;
  outcome: Outcome;
};

export type CommitTimelineEntry = {
  repository_key: string;
  commit_sha: string;
  subject: string | null;
  committed_at: string | null;
  repository_url: string | null;
  ref: string | null;
  patch_files: number;
  patch_additions: number;
  patch_deletions: number;
  captures: CommitCapture[];
  capture_count: number;
  captures_next_cursor: string | null;
};

export type CommitRepository = {
  repository_key: string;
  name: string;
  host: string | null;
  commit_count: number;
  capture_count: number;
  session_id: string | null;
};
export type CommitRef = { ref: string; commit_count: number };

export type CommitFilters = {
  q?: string;
  repo?: string;
  ref?: string;
  from?: string;
  to?: string;
  session?: string;
  outcome?: Outcome;
  captureStatus?: GitArtifact["capture_status"];
  cursor?: string;
  limit?: number;
};

export type OutcomeEvidence = {
  commit?: string;
  base_commit?: string;
  patch?: string;
  provenance?: string;
  url?: string;
  note?: string;
  repository_url?: string;
  commit_url?: string;
  ref?: string;
  patch_r2_key?: string;
  patch_bytes?: number;
  patch_files?: number;
  patch_additions?: number;
  patch_deletions?: number;
};

const evidenceKeys = ["commit", "base_commit", "patch", "provenance", "url", "note", "repository_url", "commit_url", "ref", "patch_r2_key"] as const;

export function parseOutcomeEvidence(json: string | null): OutcomeEvidence | null {
  if (!json) return null;
  try {
    const parsed: unknown = JSON.parse(json);
    if (typeof parsed === "string") return { note: parsed };
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) return null;
    const record = parsed as Record<string, unknown>;
    const evidence: OutcomeEvidence = {};
    for (const key of evidenceKeys) {
      if (typeof record[key] === "string" && record[key]) evidence[key] = record[key] as string;
    }
    for (const key of ["patch_bytes", "patch_files", "patch_additions", "patch_deletions"] as const) {
      if (typeof record[key] === "number" && Number.isFinite(record[key])) evidence[key] = record[key];
    }
    return Object.keys(evidence).length ? evidence : null;
  } catch {
    return null;
  }
}

// Outcome updates often refine the reason without replacing the Git result.
// Keep explicit replacement evidence authoritative, but recover the latest
// commit for the same outcome when an update contains only a note or nothing.
export function currentOutcomeEvidence(events: OutcomeEvent[], outcome: Outcome): OutcomeEvidence | null {
  const latest = events[0];
  if (!latest) return null;
  const current = parseOutcomeEvidence(latest.evidence_json);
  if (current?.commit || current?.commit_url || current?.url || current?.patch || current?.patch_r2_key) return current;
  if (latest.source === "user") return current;
  const prior = events.slice(1).find((event) => event.outcome === outcome && parseOutcomeEvidence(event.evidence_json)?.commit);
  const git = prior ? parseOutcomeEvidence(prior.evidence_json) : null;
  if (!git) return current;
  return current?.note ? { ...git, note: current.note } : git;
}

export type OutcomeCommitEvidence = {
  event: OutcomeEvent;
  evidence: OutcomeEvidence;
};

export function outcomeCommitEvidence(
  events: OutcomeEvent[],
): OutcomeCommitEvidence[] {
  const commits = new Set<string>();
  const history: OutcomeCommitEvidence[] = [];
  for (const event of events) {
    const evidence = parseOutcomeEvidence(event.evidence_json);
    const commit = evidence?.commit?.trim().toLowerCase();
    if (!evidence || !commit || commits.has(commit)) continue;
    commits.add(commit);
    history.push({ event, evidence });
  }
  return history;
}

export type SessionSummary = {
  goal: string | null;
  actions: string[];
  result: string | null;
  verification: string[];
  unresolved: string[];
  partial: boolean;
  source: "reconstructed";
  evidence?: Array<{
    section: "goal" | "actions" | "result" | "verification" | "unresolved";
    index: number;
    href: string;
    label: string;
  }>;
};

export type SessionDetail = {
  session: Omit<Session, "capture" | "liveness">;
  capture: CaptureSummary;
  supporting_sessions: Array<Omit<Session, "capture" | "child_session_count" | "liveness" | "activity_at">>;
  outcome_events: OutcomeEvent[];
  files: string[];
  errors: SessionError[];
  git_artifacts: GitArtifact[];
  summary?: SessionSummary;
};

export type SessionFilters = {
  q?: string;
  repo?: string;
  outcome?: Outcome;
  app?: string;
  model?: string;
  from?: string;
  to?: string;
  state?: "active" | "inactive";
  capture?: CaptureStatus;
  provider?: string;
  errors?: "true";
  commits?: "true";
  cursor?: string;
  limit?: number;
};

export type ExchangeFilters = {
  q?: string;
  repo?: string;
  model?: string;
  provider?: string;
  app?: string;
  finishReason?: string;
  session?: string;
  from?: string;
  to?: string;
  requestKind?: RequestKind;
  captureStatus?: ExchangeCaptureStatus;
  tool?: string;
  errors?: "true";
  order?: "asc" | "desc";
  cursor?: string;
  limit?: number;
};

export type SessionExchangeFilters = ExchangeFilters;

export type Facets = {
  repos: string[];
  apps: string[];
  models: string[];
  providers: string[];
  finish_reasons: string[];
  tools?: string[];
};

export type Overview = {
  totals: { requests: number; sessions: number; saved_exchanges: number; capture_failures: number; input_tokens: number; output_tokens: number };
  models: Array<{ name: string; requests: number }>;
  providers: Array<{ name: string; requests: number }>;
  apps: Array<{ name: string; requests: number }>;
};

export type LogEnvelope = {
  schema_version: number;
  exchange_id: string;
  session_id: string;
  captured_at: string;
  endpoint: string;
  request: unknown;
  response: { format: "json"; body: unknown } | { format: "reconstructed_sse"; content: unknown; events: unknown };
  tool_activity?: Array<{
    name: string;
    input: Record<string, unknown>;
    status: "succeeded" | "failed";
    output?: unknown;
  }>;
};

export class ApiError extends Error {
  constructor(message: string, readonly status: number) {
    super(message);
  }
}

async function request<T>(path: string, init: RequestInit = {}, maxBytes?: number): Promise<T> {
  if (fixtureDataEnabled) return fixtureRequest<T>(path, init);
  const response = await fetch(path, {
    cache: "no-store",
    credentials: "same-origin",
    redirect: "manual",
    ...init,
    headers: {
      accept: "application/json",
      "X-Requested-With": "XMLHttpRequest",
      ...init.headers,
    },
  });
  if (response.type === "opaqueredirect" || (response.status >= 300 && response.status < 400) || (response.ok && !response.headers.get("content-type")?.includes("application/json"))) {
    notifyAuthRequired();
    throw new ApiError("Cloudflare Access authentication required.", 403);
  }
  if (!response.ok) {
    const body = await response.json().catch(() => null) as { error?: string } | null;
    const fallback = response.status === 403 ? "Cloudflare Access denied this request." : `Request failed (${response.status}).`;
    if ((response.status === 401 || response.status === 403) && typeof window !== "undefined") {
      notifyAuthRequired();
    }
    throw new ApiError(body?.error ?? fallback, response.status);
  }
  if (maxBytes !== undefined && response.body) {
    const tooLargeMessage = "This archive exceeds the conversation size limit. Open the raw request to inspect the full saved evidence.";
    if (Number(response.headers.get("content-length")) > maxBytes) {
      await response.body.cancel();
      throw new ApiError(tooLargeMessage, 413);
    }
    const reader = response.body.getReader();
    const decoder = new TextDecoder();
    const chunks: string[] = [];
    let bytes = 0;
    try {
      while (true) {
        const chunk = await reader.read();
        if (chunk.done) break;
        bytes += chunk.value.byteLength;
        if (bytes > maxBytes) {
          await reader.cancel();
          throw new ApiError(tooLargeMessage, 413);
        }
        chunks.push(decoder.decode(chunk.value, { stream: true }));
      }
      chunks.push(decoder.decode());
    } finally {
      reader.releaseLock();
    }
    return JSON.parse(chunks.join("")) as T;
  }
  return response.json() as Promise<T>;
}

export function errorMessage(error: unknown, fallback: string) {
  return error instanceof Error ? error.message : fallback;
}

export async function getIdentity(signal?: AbortSignal) {
  return request<DashboardIdentity>("/dashboard/api/identity", { signal });
}

function notifyAuthRequired() {
  if (typeof window !== "undefined") window.dispatchEvent(new CustomEvent("mimir:auth-required"));
}

export async function listSessions(filters: SessionFilters = {}, signal?: AbortSignal) {
  const query = new URLSearchParams();
  for (const [key, value] of Object.entries(filters)) {
    if (value !== undefined && value !== "") query.set(key, String(value));
  }
  return request<{ sessions: Session[]; descendants: Session[]; next_cursor: string | null }>(`/dashboard/api/sessions?${query}`, { signal });
}

export async function listCommits(filters: CommitFilters = {}, signal?: AbortSignal) {
  const query = new URLSearchParams();
  for (const [key, value] of Object.entries(filters)) {
    if (value !== undefined && value !== "") query.set(key === "captureStatus" ? "capture_status" : key, String(value));
  }
  return request<{ commits: CommitTimelineEntry[]; next_cursor: string | null }>(`/dashboard/api/commits?${query}`, { signal });
}

export async function listCommitRepositories(cursor?: string, signal?: AbortSignal) {
  const query = new URLSearchParams({ limit: "50" });
  if (cursor) query.set("cursor", cursor);
  return request<{ repositories: CommitRepository[]; next_cursor: string | null }>(`/dashboard/api/commits/repositories?${query}`, { signal });
}

export async function listCommitRefs(repo: string, cursor?: string, signal?: AbortSignal) {
  const query = new URLSearchParams({ repo, limit: "50" });
  if (cursor) query.set("cursor", cursor);
  return request<{ refs: CommitRef[]; next_cursor: string | null }>(`/dashboard/api/commits/refs?${query}`, { signal });
}

export async function listCommitCaptures(repo: string, commit: string, cursor?: string, signal?: AbortSignal) {
  const query = new URLSearchParams({ repo, commit, limit: "50" });
  if (cursor) query.set("cursor", cursor);
  return request<{ captures: CommitCapture[]; next_cursor: string | null }>(`/dashboard/api/commits/captures?${query}`, { signal });
}

export async function getSession(id: string, signal?: AbortSignal) {
  return request<SessionDetail>(`/dashboard/api/sessions/${encodeURIComponent(id)}`, { signal });
}

export async function getSessionDiff(id: string, signal?: AbortSignal) {
  if (fixtureDataEnabled) {
    const detail = await getSession(id, signal);
    const patch = currentOutcomeEvidence(detail.outcome_events, detail.session.outcome)?.patch;
    if (!patch) throw new ApiError("Diff unavailable.", 404);
    return patch;
  }
  const response = await fetch(`/dashboard/api/sessions/${encodeURIComponent(id)}/diff`, { signal, cache: "no-store", credentials: "same-origin", redirect: "manual" });
  if (!response.ok) {
    const body = await response.json().catch(() => null) as { error?: string } | null;
    throw new ApiError(body?.error ?? `Request failed (${response.status}).`, response.status);
  }
  return response.text();
}

export async function getSessionGitArtifactPatch(id: string, commit: string, signal?: AbortSignal) {
  if (fixtureDataEnabled) return fixtureRequest<string>(`/dashboard/api/sessions/${encodeURIComponent(id)}/git-artifacts/${encodeURIComponent(commit)}/patch`, { signal });
  const response = await fetch(
    `/dashboard/api/sessions/${encodeURIComponent(id)}/git-artifacts/${encodeURIComponent(commit)}/patch`,
    { signal, cache: "no-store", credentials: "same-origin", redirect: "manual" },
  );
  if (!response.ok) {
    const body = await response.json().catch(() => null) as { error?: string } | null;
    throw new ApiError(body?.error ?? `Request failed (${response.status}).`, response.status);
  }
  return response.text();
}

export async function getSessionObjectState(id: string, signal?: AbortSignal) {
  return request<SessionObjectState>(`/dashboard/api/sessions/${encodeURIComponent(id)}/object-state`, { signal });
}

export function connectSessionLive(id: string, onMessage: (message: SessionLiveMessage) => void) {
  if (fixtureDataEnabled || typeof window === "undefined") return null;
  const url = new URL(`/dashboard/api/sessions/${encodeURIComponent(id)}/live`, window.location.href);
  url.protocol = url.protocol === "https:" ? "wss:" : "ws:";
  const socket = new WebSocket(url);
  socket.addEventListener("message", (event) => {
    try {
      onMessage(JSON.parse(String(event.data)) as SessionLiveMessage);
    } catch {
      // Ignore malformed frames and keep the live connection available.
    }
  });
  return socket;
}

export type SessionTitleUpdate = Pick<Session, "id" | "title" | "title_source" | "title_updated_at" | "display_title">;

export async function setSessionTitle(id: string, title: string, signal?: AbortSignal) {
  const result = await request<{ session: SessionTitleUpdate }>(`/dashboard/api/sessions/${encodeURIComponent(id)}/title`, {
    method: "PATCH",
    signal,
    headers: { "content-type": "application/json" },
    body: JSON.stringify({ title: title.trim() }),
  });
  return result.session;
}

export async function setSessionOutcome(id: string, outcome: Outcome, reason: string, evidence?: OutcomeEvidence, signal?: AbortSignal) {
  return request<{ id: string; outcome: Outcome }>(`/dashboard/api/sessions/${encodeURIComponent(id)}/outcome`, {
    method: "POST",
    signal,
    headers: { "content-type": "application/json" },
    body: JSON.stringify({ outcome, reason: reason.trim() || undefined, ...(evidence ? { evidence } : {}) }),
  });
}

export async function setSessionsOutcome(
  sessionIds: string[],
  outcome: Outcome,
  reason: string,
  signal?: AbortSignal,
) {
  return request<{ updated: Array<{ id: string; outcome: Outcome }> }>(
    "/dashboard/api/sessions/outcomes",
    {
      method: "POST",
      signal,
      headers: { "content-type": "application/json" },
      body: JSON.stringify({
        session_ids: sessionIds,
        outcome,
        reason: reason.trim() || undefined,
      }),
    },
  );
}

function exchangeQuery(filters: ExchangeFilters, defaultLimit: number) {
  const query = new URLSearchParams();
  const names: Record<string, string> = { finishReason: "finish_reason", requestKind: "request_kind", captureStatus: "capture_status" };
  for (const [key, value] of Object.entries(filters)) {
    if (value !== undefined && value !== "") query.set(names[key] ?? key, String(value));
  }
  if (!query.has("limit")) query.set("limit", String(defaultLimit));
  return query;
}

export async function listSessionExchanges(id: string, filters: SessionExchangeFilters = {}, signal?: AbortSignal) {
  return request<{ exchanges: SessionExchange[]; next_cursor: string | null }>(`/dashboard/api/sessions/${encodeURIComponent(id)}/exchanges?${exchangeQuery(filters, 25)}`, { signal });
}

export async function listExchanges(filters: ExchangeFilters = {}, signal?: AbortSignal) {
  return request<{ exchanges: Exchange[]; next_cursor: string | null }>(`/dashboard/api/log?${exchangeQuery(filters, 50)}`, { signal });
}

export async function getExchange(id: string, signal?: AbortSignal) {
  const detail = await request<{ exchange: Exchange; log_url: string }>(`/dashboard/api/log/${encodeURIComponent(id)}`, { signal });
  const envelope = await request<LogEnvelope>(detail.log_url, { signal });
  return { exchange: detail.exchange, envelope };
}

export async function getConversationExchange(id: string, signal?: AbortSignal) {
  const detail = await request<{ exchange: Exchange; log_url: string }>(`/dashboard/api/log/${encodeURIComponent(id)}`, { signal });
  const envelope = await request<LogEnvelope>(detail.log_url, { signal }, 2 * 1024 * 1024);
  return { exchange: detail.exchange, envelope };
}

export async function getFacets(sessionId?: string, signal?: AbortSignal, scope: "own" | "tree" = "tree") {
  const query = new URLSearchParams();
  if (sessionId) {
    query.set("session", sessionId);
    query.set("scope", scope);
  }
  return request<Facets>(`/dashboard/api/facets?${query}`, { signal });
}

export async function getOverview(signal?: AbortSignal) {
  return request<Overview>("/dashboard/api/overview", { signal });
}

export async function listDevices(signal?: AbortSignal) {
  return request<{ devices: Device[] }>("/dashboard/api/devices", { signal });
}

export async function renameDevice(id: string, name: string, signal?: AbortSignal) {
  return request<{ device: Device }>(`/dashboard/api/devices/${encodeURIComponent(id)}`, {
    method: "PATCH",
    signal,
    headers: { "content-type": "application/json" },
    body: JSON.stringify({ name: name.trim() }),
  });
}

export async function revokeDevice(id: string, signal?: AbortSignal) {
  return request<{ device: Device }>(`/dashboard/api/devices/${encodeURIComponent(id)}/revoke`, {
    method: "POST",
    signal,
  });
}
