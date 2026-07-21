export type Outcome = "landed" | "discarded" | "abandoned" | "unresolved";
export type CaptureStatus = "empty" | "pending" | "saved" | "failed" | "partial";

export type CaptureSummary = {
  status: CaptureStatus;
  saved_exchanges: number;
  failed_exchanges: number;
  pending_exchanges: number;
  last_saved_at: string | null;
};

export type Session = {
  id: string;
  started_at: string;
  ended_at: string | null;
  state: "active" | "inactive";
  last_active_at: string | null;
  inactive_at: string | null;
  harness: string | null;
  boundary: string;
  outcome: Outcome;
  outcome_src: "agent" | "user" | "git" | null;
  outcome_updated_at: string | null;
  outcome_reason: string | null;
  repo: string | null;
  source_ref: string | null;
  model_primary: string | null;
  request_count: number;
  tokens_in: number;
  tokens_out: number;
  intent: string | null;
  capture: CaptureSummary;
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
  r2_key: string;
};

export type SessionExchange = Pick<Exchange, "id" | "ts" | "model" | "provider" | "finish_reason" | "latency_ms" | "harness" | "input_tokens" | "output_tokens"> & {
  request_excerpt: string;
  capture_status: string;
  capture_reason: string | null;
  failure_code: string | null;
};

export type SessionDetail = {
  session: Omit<Session, "capture">;
  capture: CaptureSummary;
  outcome_events: Array<{ id: string; outcome: Outcome; source: string; reason: string | null; evidence_json: string | null; created_at: string }>;
  exchanges: SessionExchange[];
  files: string[];
  errors: string[];
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
};

export class ApiError extends Error {
  constructor(message: string, readonly status: number) {
    super(message);
  }
}

async function request<T>(path: string, init: RequestInit = {}): Promise<T> {
  const response = await fetch(path, {
    cache: "no-store",
    credentials: "same-origin",
    ...init,
    headers: { accept: "application/json", ...init.headers },
  });
  if (!response.ok) {
    const body = await response.json().catch(() => null) as { error?: string } | null;
    const fallback = response.status === 403 ? "Cloudflare Access denied this request." : `Request failed (${response.status}).`;
    throw new ApiError(body?.error ?? fallback, response.status);
  }
  return response.json() as Promise<T>;
}

export function errorMessage(error: unknown, fallback: string) {
  return error instanceof Error ? error.message : fallback;
}

export async function listSessions(signal?: AbortSignal) {
  return (await request<{ sessions: Session[] }>("/dashboard/api/sessions", { signal })).sessions;
}

export async function getSession(id: string, signal?: AbortSignal) {
  return request<SessionDetail>(`/dashboard/api/sessions/${encodeURIComponent(id)}`, { signal });
}

export async function setSessionOutcome(id: string, outcome: Outcome, reason: string, signal?: AbortSignal) {
  return request<{ id: string; outcome: Outcome }>(`/dashboard/api/sessions/${encodeURIComponent(id)}/outcome`, {
    method: "POST",
    signal,
    headers: { "content-type": "application/json" },
    body: JSON.stringify({ outcome, reason: reason.trim() || undefined }),
  });
}

export async function listExchanges(filters: { cursor?: string; provider?: string; app?: string; limit?: number } = {}, signal?: AbortSignal) {
  const query = new URLSearchParams();
  if (filters.cursor) query.set("cursor", filters.cursor);
  if (filters.provider) query.set("provider", filters.provider);
  if (filters.app) query.set("app", filters.app);
  query.set("limit", String(filters.limit ?? 50));
  return request<{ exchanges: Exchange[]; next_cursor: string | null }>(`/dashboard/api/log?${query}`, { signal });
}

export async function getExchange(id: string, signal?: AbortSignal) {
  const detail = await request<{ exchange: Exchange; log_url: string }>(`/dashboard/api/log/${encodeURIComponent(id)}`, { signal });
  const envelope = await request<LogEnvelope>(detail.log_url, { signal });
  return { exchange: detail.exchange, envelope };
}

export async function getOverview(signal?: AbortSignal) {
  return request<Overview>("/dashboard/api/overview", { signal });
}
