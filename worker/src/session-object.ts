import { parseSessionEvent, type SessionEvent, type SessionEventTurn } from "./session-events";
import type { Bindings } from "./types";

// The Session Durable Object owns one live session. Reporters (proxy capture,
// harness plugins) append events; the object tracks liveness, serves the
// dashboard live feed, and performs the final write when the session ends.
// It is a buffer and coordinator: R2 and D1 remain canonical storage.

export const SESSION_SILENCE_MS = 10 * 60_000;
export const SESSION_LIVENESS_MS = 90_000;
export const SESSION_FINALIZE_RETRY_MS = 60_000;
const MAX_STORED_TURNS = 500;

type StoredTurn = SessionEventTurn & { ts: string };

type SessionMeta = {
  sessionId: string;
  harness: string | null;
  repo: string | null;
  startedAt: string;
  lastEventAt: string;
  finalizedAt: string | null;
  endReason: string | null;
  turnCount: number;
  tokensIn: number;
  tokensOut: number;
};

export type SessionLiveness = "active" | "disconnected" | "finalized";

export class SessionObject implements DurableObject {
  private meta: SessionMeta | null = null;
  private turns: StoredTurn[] | null = null;

  constructor(private ctx: DurableObjectState, private env: Bindings) {}

  async fetch(request: Request): Promise<Response> {
    const url = new URL(request.url);
    if (request.method === "POST" && url.pathname === "/event") return this.handleEvent(request);
    if (request.method === "GET" && url.pathname === "/state") return Response.json(await this.currentState());
    if (request.method === "GET" && url.pathname === "/feed") return this.handleFeed(request);
    return Response.json({ error: "not found" }, { status: 404 });
  }

  private async handleEvent(request: Request): Promise<Response> {
    let parsed: SessionEvent | { error: string };
    try {
      parsed = parseSessionEvent(await request.json());
    } catch {
      return Response.json({ error: "invalid JSON body" }, { status: 400 });
    }
    if ("error" in parsed) return Response.json({ error: parsed.error }, { status: 400 });
    await this.ensureLoaded(parsed.session_id);
    await this.applyEvent(parsed);
    if (parsed.kind === "end") {
      try {
        await this.finalize(parsed.reason ?? "explicit");
      } catch (error) {
        await this.ctx.storage.setAlarm(Date.now() + SESSION_FINALIZE_RETRY_MS);
        console.error(JSON.stringify({ message: "session finalize failed, retry scheduled", session_id: parsed.session_id, error: error instanceof Error ? error.message : String(error) }));
        return Response.json({ accepted: true, finalizing: false });
      }
    }
    return Response.json({ accepted: true, state: await this.currentState() });
  }

  private async applyEvent(event: SessionEvent): Promise<void> {
    const meta = this.meta!;
		let duplicateTurn = false;
		let dedupKey: string | null = null;
		if (event.kind === "turn" && event.turn?.exchange_id) {
			dedupKey = `exchange:${event.turn.exchange_id}`;
			duplicateTurn = await this.ctx.storage.get<boolean>(dedupKey) === true
				|| this.turns!.some((turn) => turn.exchange_id === event.turn?.exchange_id);
		}
		if (duplicateTurn) return;
		if (meta.finalizedAt) {
			if (event.kind === "end") return;
			if (event.kind === "heartbeat" && Date.parse(event.ts) <= Date.parse(meta.finalizedAt)) return;
			await this.reopen();
		}
    meta.lastEventAt = event.ts;
    if (event.harness) meta.harness = meta.harness ?? event.harness;
    if (event.repo) meta.repo = meta.repo ?? event.repo;
    if (event.kind === "turn" && event.turn) {
		meta.turnCount += 1;
		meta.tokensIn += event.turn.usage?.input_tokens ?? 0;
		meta.tokensOut += event.turn.usage?.output_tokens ?? 0;
		this.turns!.push({ ...event.turn, ts: event.ts });
		if (this.turns!.length > MAX_STORED_TURNS) this.turns = this.turns!.slice(-MAX_STORED_TURNS);
		await this.ctx.storage.put("turns", this.turns);
    }
    await this.ctx.storage.put("meta", meta);
    await this.ctx.storage.setAlarm(Date.parse(event.ts) + SESSION_SILENCE_MS);
		if (dedupKey) await this.ctx.storage.put(dedupKey, true);
    this.broadcast({ type: "event", event });
  }

  // reopen continues a finalized session: new activity on the same session ID
  // wakes the same object and the session flips back to active with its full
  // history. Finalized is a state, not a tombstone.
  private async reopen(): Promise<void> {
    const meta = this.meta!;
    meta.finalizedAt = null;
    meta.endReason = null;
    await this.env.DB.prepare("UPDATE sessions SET state = 'active', inactive_at = NULL WHERE id = ?").bind(meta.sessionId).run();
    this.broadcast({ type: "reopened", session_id: meta.sessionId });
  }

  async alarm(): Promise<void> {
    await this.ensureLoaded();
    const meta = this.meta!;
    if (meta.finalizedAt) return;
    const silentFor = Date.now() - Date.parse(meta.lastEventAt);
    if (silentFor < SESSION_SILENCE_MS) {
      await this.ctx.storage.setAlarm(Date.parse(meta.lastEventAt) + SESSION_SILENCE_MS);
      return;
    }
    try {
      await this.finalize("silence");
    } catch (error) {
      await this.ctx.storage.setAlarm(Date.now() + SESSION_FINALIZE_RETRY_MS);
      console.error(JSON.stringify({ message: "session finalize failed, retry scheduled", session_id: meta.sessionId, error: error instanceof Error ? error.message : String(error) }));
    }
  }

  // finalize performs the final write: a session transcript manifest in R2
  // and the session lifecycle update in D1. Idempotent per active period.
  private async finalize(reason: string): Promise<void> {
    const meta = this.meta!;
    if (meta.finalizedAt) return;
    const now = new Date().toISOString();
    const exchanges = await this.env.DB.prepare("SELECT id, ts, endpoint, model, provider, request_kind, input_tokens, output_tokens, latency_ms, r2_key, r2_bytes, capture_status FROM exchanges WHERE session_id = ? ORDER BY ts, id").bind(meta.sessionId).all();
    const transcript = {
      schema_version: 1,
      session_id: meta.sessionId,
      started_at: meta.startedAt,
      ended_at: now,
      harness: meta.harness,
      repo: meta.repo,
      end_reason: reason,
      turn_count: meta.turnCount,
      usage: { input_tokens: meta.tokensIn, output_tokens: meta.tokensOut },
      exchanges: exchanges.results,
    };
    await this.env.LOGS.put(`sessions/${meta.sessionId}/transcript.json`, JSON.stringify(transcript), { httpMetadata: { contentType: "application/json" } });
    await this.env.DB.batch([
      this.env.DB.prepare("INSERT OR IGNORE INTO sessions(id, started_at, last_active_at, harness, boundary, repo) VALUES (?, ?, ?, ?, 'header', ?)").bind(meta.sessionId, meta.startedAt, meta.lastEventAt, meta.harness, meta.repo),
      this.env.DB.prepare("UPDATE sessions SET state = 'inactive', ended_at = CASE WHEN inactive_at IS NULL OR ended_at IS NULL OR ended_at <> inactive_at THEN ? ELSE ended_at END, inactive_at = CASE WHEN inactive_at IS NULL OR ended_at IS NULL OR ended_at <> inactive_at THEN ? ELSE inactive_at END, last_active_at = CASE WHEN last_active_at IS NULL OR last_active_at < ? THEN ? ELSE last_active_at END, harness = COALESCE(harness, ?), repo = COALESCE(repo, ?) WHERE id = ?").bind(now, now, meta.lastEventAt, meta.lastEventAt, meta.harness, meta.repo, meta.sessionId),
    ]);
    meta.finalizedAt = now;
    meta.endReason = reason;
    await this.ctx.storage.put("meta", meta);
    this.broadcast({ type: "finalized", session_id: meta.sessionId, reason, ended_at: now });
  }

  // currentState projects liveness from heartbeat age at read time. The
  // silence timer is a durability backstop, not a UX promise: a session with
  // no events for SESSION_LIVENESS_MS displays as disconnected while the
  // alarm still owns finalization.
  private async currentState() {
    await this.ensureLoaded();
    const meta = this.meta!;
    const liveness: SessionLiveness = meta.finalizedAt
      ? "finalized"
      : Date.now() - Date.parse(meta.lastEventAt) > SESSION_LIVENESS_MS
        ? "disconnected"
        : "active";
    return {
      session_id: meta.sessionId,
      liveness,
      harness: meta.harness,
      repo: meta.repo,
      started_at: meta.startedAt,
      last_event_at: meta.lastEventAt,
      finalized_at: meta.finalizedAt,
      end_reason: meta.endReason,
      turn_count: meta.turnCount,
      tokens_in: meta.tokensIn,
      tokens_out: meta.tokensOut,
    };
  }

  private async handleFeed(request: Request): Promise<Response> {
    if (request.headers.get("Upgrade") !== "websocket") return Response.json({ error: "websocket upgrade required" }, { status: 426 });
    await this.ensureLoaded();
    const pair = new WebSocketPair();
    this.ctx.acceptWebSocket(pair[1]);
    pair[1].send(JSON.stringify({ type: "snapshot", state: await this.currentState(), turns: this.turns }));
    return new Response(null, { status: 101, webSocket: pair[0] });
  }

  private broadcast(message: Record<string, unknown>): void {
    const payload = JSON.stringify(message);
    for (const socket of this.ctx.getWebSockets()) {
      try {
        socket.send(payload);
      } catch {
        try { socket.close(1011, "broadcast failed"); } catch { /* already closed */ }
      }
    }
  }

  async webSocketMessage(_ws: WebSocket, _message: string | ArrayBuffer): Promise<void> {
    // The feed is server-push only; client messages are ignored.
  }

  async webSocketClose(_ws: WebSocket): Promise<void> {}

  async webSocketError(_ws: WebSocket): Promise<void> {}

  private async ensureLoaded(sessionId?: string): Promise<void> {
    if (this.meta && this.turns) return;
    const [meta, turns] = await Promise.all([
      this.ctx.storage.get<SessionMeta>("meta"),
      this.ctx.storage.get<StoredTurn[]>("turns"),
    ]);
    const now = new Date().toISOString();
    this.meta = meta ?? {
      sessionId: sessionId ?? "unknown",
      harness: null,
      repo: null,
      startedAt: now,
      lastEventAt: now,
      finalizedAt: null,
      endReason: null,
      turnCount: 0,
      tokensIn: 0,
      tokensOut: 0,
    };
    this.turns = turns ?? [];
  }
}
