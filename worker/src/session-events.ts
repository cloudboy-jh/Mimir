import type { Bindings } from "./types";
import type { RequestKind } from "./capture";

// Session event format v1. Reporters (the proxy, harness plugins, native
// harness reporting) deliver these to the Session Durable Object. The object
// owns liveness and finalization; R2/D1 remain canonical storage.
export const SESSION_EVENT_VERSION = 1;
export const SESSION_ID = /^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$/;
export const SESSION_EVENT_KINDS = ["turn", "heartbeat", "end"] as const;
export type SessionEventKind = (typeof SESSION_EVENT_KINDS)[number];

const MAX_EXCERPT_CHARS = 500;
const MAX_END_REASON_CHARS = 2_000;
const REQUEST_KINDS = new Set(["primary", "title", "summary", "compaction"]);

export type SessionEventTurn = {
  exchange_id?: string;
  model?: string;
  provider?: string | null;
  request_kind?: RequestKind;
  usage?: { input_tokens: number; output_tokens: number };
  latency_ms?: number;
  excerpt?: string;
};

export type SessionEvent = {
  version: typeof SESSION_EVENT_VERSION;
  kind: SessionEventKind;
  session_id: string;
  harness: string | null;
  repo?: string | null;
  ts: string;
  turn?: SessionEventTurn;
  reason?: string;
};

// parseSessionEvent validates an untrusted event body. The session_id supplied
// by the caller (usually the route path) is authoritative; a conflicting body
// value is rejected rather than silently rewritten.
export function parseSessionEvent(input: unknown): SessionEvent | { error: string } {
  if (typeof input !== "object" || !input || Array.isArray(input)) return { error: "event must be an object" };
  const body = input as Record<string, unknown>;
  if (body.version !== SESSION_EVENT_VERSION) return { error: "unsupported event version" };
  if (!(SESSION_EVENT_KINDS as readonly string[]).includes(body.kind as string)) return { error: "invalid event kind" };
  if (typeof body.session_id !== "string" || !SESSION_ID.test(body.session_id)) return { error: "invalid session_id" };
  if (typeof body.ts !== "string" || Number.isNaN(Date.parse(body.ts))) return { error: "invalid ts" };
  if (body.harness !== undefined && body.harness !== null && typeof body.harness !== "string") return { error: "invalid harness" };
  if (body.repo !== undefined && body.repo !== null && typeof body.repo !== "string") return { error: "invalid repo" };
  const event: SessionEvent = {
    version: SESSION_EVENT_VERSION,
    kind: body.kind as SessionEventKind,
    session_id: body.session_id,
    harness: typeof body.harness === "string" ? body.harness.slice(0, 128) : null,
    ts: new Date(body.ts).toISOString(),
  };
  if (typeof body.repo === "string") event.repo = body.repo.slice(0, 512);
  if (body.reason !== undefined) {
    if (typeof body.reason !== "string" || body.reason.length > MAX_END_REASON_CHARS) return { error: "invalid reason" };
    event.reason = body.reason;
  }
  if (body.turn !== undefined) {
    const turn = parseTurn(body.turn);
    if (turn && "error" in turn) return turn;
    if (turn) event.turn = turn;
  }
  if (event.kind === "end" && !event.reason) event.reason = "explicit";
  return event;
}

function parseTurn(input: unknown): SessionEventTurn | { error: string } | null {
  if (typeof input !== "object" || !input || Array.isArray(input)) return { error: "invalid turn" };
  const body = input as Record<string, unknown>;
  const turn: SessionEventTurn = {};
  if (body.exchange_id !== undefined) {
    if (typeof body.exchange_id !== "string" || body.exchange_id.length > 128) return { error: "invalid turn exchange_id" };
    turn.exchange_id = body.exchange_id;
  }
  if (body.model !== undefined) {
    if (typeof body.model !== "string" || body.model.length > 256) return { error: "invalid turn model" };
    turn.model = body.model;
  }
  if (body.provider !== undefined && body.provider !== null) {
    if (typeof body.provider !== "string" || body.provider.length > 256) return { error: "invalid turn provider" };
    turn.provider = body.provider;
  }
  if (body.request_kind !== undefined) {
    if (!REQUEST_KINDS.has(body.request_kind as string)) return { error: "invalid turn request_kind" };
    turn.request_kind = body.request_kind as RequestKind;
  }
  if (body.usage !== undefined) {
    const usage = body.usage as Record<string, unknown>;
    if (typeof usage !== "object" || !usage) return { error: "invalid turn usage" };
    const inputTokens = usage.input_tokens;
    const outputTokens = usage.output_tokens;
    if (typeof inputTokens !== "number" || typeof outputTokens !== "number" || inputTokens < 0 || outputTokens < 0) return { error: "invalid turn usage" };
    turn.usage = { input_tokens: Math.floor(inputTokens), output_tokens: Math.floor(outputTokens) };
  }
  if (body.latency_ms !== undefined) {
    if (typeof body.latency_ms !== "number" || body.latency_ms < 0) return { error: "invalid turn latency_ms" };
    turn.latency_ms = Math.floor(body.latency_ms);
  }
  if (body.excerpt !== undefined) {
    if (typeof body.excerpt !== "string") return { error: "invalid turn excerpt" };
    turn.excerpt = body.excerpt.slice(0, MAX_EXCERPT_CHARS);
  }
  return turn;
}

// reportSessionEvent delivers an event to the owning Session Durable Object.
// Reporting is best-effort: capture and request paths must never fail because
// the session object is unavailable.
export async function reportSessionEvent(env: Bindings, event: SessionEvent): Promise<void> {
  try {
    const stub = env.SESSIONS.get(env.SESSIONS.idFromName(event.session_id));
    const response = await stub.fetch("https://session-object/event", { method: "POST", body: JSON.stringify(event) });
    if (!response.ok) console.error(JSON.stringify({ message: "session event rejected", session_id: event.session_id, kind: event.kind, status: response.status }));
  } catch (error) {
    console.error(JSON.stringify({ message: "session event delivery failed", session_id: event.session_id, kind: event.kind, error: error instanceof Error ? error.message : String(error) }));
  }
}
