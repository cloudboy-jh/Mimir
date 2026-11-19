import type { RequestKind } from "../exchanges/exchange-types";
import {
  assignParent,
  canMutateSession,
  endSession,
  HEURISTIC_REACTIVATION_GUARD,
  isParentConflict,
} from "./lifecycle";
import { normalizeSessionTitle, titleUpdateStatement } from "./titles";

// Session event format v1. Harness plugins and native hooks report lifecycle
// events; they update the D1 session row directly. Token counters stay
// derived from saved exchanges, so turn usage is validated but not summed.
export const SESSION_EVENT_VERSION = 1;
export const SESSION_ID = /^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$/;
export const SESSION_EVENT_KINDS = ["turn", "heartbeat", "failure", "end"] as const;
export type SessionEventKind = (typeof SESSION_EVENT_KINDS)[number];

const MAX_EXCERPT_CHARS = 500;
const MAX_END_REASON_CHARS = 2_000;
const REQUEST_KINDS = new Set(["primary", "title", "summary", "compaction"]);

export type SessionEventTurn = {
  exchange_id?: string;
  model?: string;
  provider?: string | null;
  result?: "completed" | "pending" | "failed";
  request_kind?: RequestKind;
  usage?: {
    input_tokens: number;
    output_tokens: number;
    cache_read_tokens?: number;
    cache_write_tokens?: number;
  };
  latency_ms?: number;
  excerpt?: string;
};

export type SessionEvent = {
  version: typeof SESSION_EVENT_VERSION;
  kind: SessionEventKind;
  session_id: string;
  installation_id?: string | null;
  parent_session_id?: string | null;
  harness: string | null;
  repo?: string | null;
  title?: string;
  ts: string;
  turn?: SessionEventTurn;
  reason?: string;
};

// parseSessionEvent validates an untrusted event body. The session_id supplied
// by the caller (usually the route path) is authoritative; a conflicting body
// value is rejected rather than silently rewritten.
export function parseSessionEvent(
  input: unknown,
): SessionEvent | { error: string } {
  if (typeof input !== "object" || !input || Array.isArray(input))
    return { error: "event must be an object" };
  const body = input as Record<string, unknown>;
  if (body.version !== SESSION_EVENT_VERSION)
    return { error: "unsupported event version" };
  if (!(SESSION_EVENT_KINDS as readonly string[]).includes(body.kind as string))
    return { error: "invalid event kind" };
  if (typeof body.session_id !== "string" || !SESSION_ID.test(body.session_id))
    return { error: "invalid session_id" };
  if (
    body.parent_session_id !== undefined &&
    body.parent_session_id !== null &&
    (typeof body.parent_session_id !== "string" ||
      !SESSION_ID.test(body.parent_session_id) ||
      body.parent_session_id === body.session_id)
  )
    return { error: "invalid parent_session_id" };
  if (typeof body.ts !== "string" || Number.isNaN(Date.parse(body.ts)))
    return { error: "invalid ts" };
  if (
    body.harness !== undefined &&
    body.harness !== null &&
    typeof body.harness !== "string"
  )
    return { error: "invalid harness" };
  if (
    body.repo !== undefined &&
    body.repo !== null &&
    typeof body.repo !== "string"
  )
    return { error: "invalid repo" };
  if (body.title !== undefined && normalizeSessionTitle(body.title) === null)
    return { error: "invalid title" };
  const event: SessionEvent = {
    version: SESSION_EVENT_VERSION,
    kind: body.kind as SessionEventKind,
    session_id: body.session_id,
    harness:
      typeof body.harness === "string" ? body.harness.slice(0, 128) : null,
    ts: new Date(body.ts).toISOString(),
  };
  if (typeof body.repo === "string") event.repo = body.repo.slice(0, 512);
  if (body.title !== undefined)
    event.title = normalizeSessionTitle(body.title)!;
  if (typeof body.parent_session_id === "string")
    event.parent_session_id = body.parent_session_id;
  if (body.reason !== undefined) {
    if (
      typeof body.reason !== "string" ||
      body.reason.length > MAX_END_REASON_CHARS
    )
      return { error: "invalid reason" };
    event.reason = body.reason;
  }
  if (body.turn !== undefined) {
    const turn = parseTurn(body.turn);
    if (turn && "error" in turn) return turn;
    if (turn) event.turn = turn;
  }
  if (event.kind === "failure" && !event.reason)
    event.reason = "assistant completion failed";
  if (event.kind === "end" && !event.reason) event.reason = "explicit";
  return event;
}

function parseTurn(
  input: unknown,
): SessionEventTurn | { error: string } | null {
  if (typeof input !== "object" || !input || Array.isArray(input))
    return { error: "invalid turn" };
  const body = input as Record<string, unknown>;
  const turn: SessionEventTurn = {};
  if (body.exchange_id !== undefined) {
    if (typeof body.exchange_id !== "string" || body.exchange_id.length > 128)
      return { error: "invalid turn exchange_id" };
    turn.exchange_id = body.exchange_id;
  }
  if (body.model !== undefined) {
    if (typeof body.model !== "string" || body.model.length > 256)
      return { error: "invalid turn model" };
    turn.model = body.model;
  }
  if (body.provider !== undefined && body.provider !== null) {
    if (typeof body.provider !== "string" || body.provider.length > 256)
      return { error: "invalid turn provider" };
    turn.provider = body.provider;
  }
  if (body.result !== undefined) {
    if (
      body.result !== "completed" &&
      body.result !== "pending" &&
      body.result !== "failed"
    )
      return { error: "invalid turn result" };
    turn.result = body.result;
  }
  if (body.request_kind !== undefined) {
    if (!REQUEST_KINDS.has(body.request_kind as string))
      return { error: "invalid turn request_kind" };
    turn.request_kind = body.request_kind as RequestKind;
  }
  if (body.usage !== undefined) {
    const usage = body.usage as Record<string, unknown>;
    if (typeof usage !== "object" || !usage)
      return { error: "invalid turn usage" };
    const inputTokens = usage.input_tokens;
    const outputTokens = usage.output_tokens;
    const cacheReadTokens = usage.cache_read_tokens ?? 0;
    const cacheWriteTokens = usage.cache_write_tokens ?? 0;
    if (
      typeof inputTokens !== "number" ||
      typeof outputTokens !== "number" ||
      typeof cacheReadTokens !== "number" ||
      typeof cacheWriteTokens !== "number" ||
      inputTokens < 0 ||
      outputTokens < 0 ||
      cacheReadTokens < 0 ||
      cacheWriteTokens < 0
    )
      return { error: "invalid turn usage" };
    turn.usage = {
      input_tokens: Math.floor(inputTokens),
      output_tokens: Math.floor(outputTokens),
      cache_read_tokens: Math.floor(cacheReadTokens),
      cache_write_tokens: Math.floor(cacheWriteTokens),
    };
  }
  if (body.latency_ms !== undefined) {
    if (typeof body.latency_ms !== "number" || body.latency_ms < 0)
      return { error: "invalid turn latency_ms" };
    turn.latency_ms = Math.floor(body.latency_ms);
  }
  if (body.excerpt !== undefined) {
    if (typeof body.excerpt !== "string")
      return { error: "invalid turn excerpt" };
    turn.excerpt = body.excerpt.slice(0, MAX_EXCERPT_CHARS);
  }
  return turn;
}

export type SessionEventResult =
  | { status: 200; body: { accepted: true; session_id: string; state: "active" | "inactive" }; ended: boolean }
  | { status: 403 | 404 | 409; body: { error: string }; ended: false };

// applySessionEvent records one lifecycle event. Activity on an ended session
// reopens it, except heartbeats that predate the end. `ended` asks the caller
// to build the session summary.
export async function applySessionEvent(
  db: D1Database,
  event: SessionEvent,
  installationID: string | null,
): Promise<SessionEventResult> {
  const id = event.session_id;
  const rejected = (status: 403 | 404 | 409, error: string): SessionEventResult => ({ status, body: { error }, ended: false });
  if (!(await canMutateSession(db, id, installationID)))
    return rejected(409, "session belongs to another installation");
  try {
    await db.batch([
      db
        .prepare(
          "INSERT OR IGNORE INTO sessions(id, installation_id, started_at, last_active_at, harness, boundary, repo, model_primary) VALUES (?, ?, ?, ?, ?, 'header', ?, ?)",
        )
        .bind(id, installationID, event.ts, event.ts, event.harness, event.repo ?? null, event.turn?.model ?? null),
      db
        .prepare(
          "UPDATE sessions SET installation_id = COALESCE(installation_id, ?) WHERE id = ? AND ? IS NOT NULL AND installation_id IS NULL",
        )
        .bind(installationID, id, installationID),
    ]);
  } catch (error) {
    if (isParentConflict(error)) return rejected(409, "invalid session parent relationship");
    throw error;
  }
  const owner = await db
    .prepare("SELECT installation_id FROM sessions WHERE id = ?")
    .bind(id)
    .first<{ installation_id: string | null }>();
  if (installationID && owner?.installation_id !== installationID)
    return rejected(409, "session belongs to another installation");
  if (event.parent_session_id) {
    const parent = await assignParent(db, id, event.parent_session_id, installationID, true);
    if (parent) return rejected(parent.status === 404 ? 409 : parent.status, parent.error);
  }
  const title = event.title ? [titleUpdateStatement(db, id, event.title, "harness", event.ts)] : [];
  if (event.kind === "end") {
    if (title.length) await db.batch(title);
    await endSession(db, id, new Date().toISOString());
    return { status: 200, body: { accepted: true, session_id: id, state: "inactive" }, ended: true };
  }
  const reopen = `NOT (state = 'inactive' AND ?2 = 'heartbeat' AND inactive_at IS NOT NULL AND julianday(?1) <= julianday(inactive_at)) AND ${HEURISTIC_REACTIVATION_GUARD}`;
  const [, state] = await db.batch<{ state: "active" | "inactive" }>([
    db
      .prepare(
        `UPDATE sessions SET state = CASE WHEN ${reopen} THEN 'active' ELSE state END, inactive_at = CASE WHEN ${reopen} THEN NULL ELSE inactive_at END, last_active_at = CASE WHEN last_active_at IS NULL OR last_active_at < ?1 THEN ?1 ELSE last_active_at END, harness = COALESCE(harness, ?3), repo = COALESCE(repo, ?4), model_primary = COALESCE(model_primary, ?5) WHERE id = ?6`,
      )
      .bind(event.ts, event.kind, event.harness, event.repo ?? null, event.turn?.model ?? null, id),
    db.prepare("SELECT state FROM sessions WHERE id = ?").bind(id),
    ...title,
  ]);
  return { status: 200, body: { accepted: true, session_id: id, state: state!.results[0]?.state ?? "active" }, ended: false };
}
