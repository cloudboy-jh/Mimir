import { finalizeAcceptedExchange } from "./capture";
import type { Bindings } from "./types";

export type CaptureSummary = {
  saved_exchanges: number;
  failed_exchanges: number;
  pending_exchanges: number;
  last_saved_at: string | null;
  status: "empty" | "pending" | "saved" | "failed" | "partial";
};

export type CaptureReceipt = {
  label: string;
  detail: string;
  action_label: "View session" | "View details" | null;
};

export const CAPTURE_SUMMARY_COLUMNS = "(SELECT COUNT(*) FROM exchanges e WHERE e.session_id = sessions.id AND e.capture_status = 'saved') AS capture_saved_exchanges, (SELECT COUNT(*) FROM exchanges e WHERE e.session_id = sessions.id AND e.capture_status = 'failed') AS capture_failed_exchanges, (SELECT COUNT(*) FROM exchanges e WHERE e.session_id = sessions.id AND e.capture_status = 'accepted') AS capture_pending_exchanges, (SELECT MAX(e.saved_at) FROM exchanges e WHERE e.session_id = sessions.id AND e.capture_status = 'saved') AS capture_last_saved_at";

export type ReconcileResponse = {
  scanned: number;
  database_truncated: boolean;
  database_cursor: string | null;
  finalized: { count: number; exchange_ids: string[] };
  pending: { count: number; exchange_ids: string[]; stale_exchange_ids: string[] };
  missing_saved: { count: number; exchange_ids: string[]; session_ids: string[] };
  orphans: { count: number; r2_keys: string[]; cursor: string | null };
  limit: number;
};

type ReconcileRow = {
  id: string;
  session_id: string;
  ts: string;
  accepted_at: string | null;
  capture_status: "accepted" | "saved";
  r2_key: string;
  harness: string | null;
  model: string | null;
  input_tokens: number;
  output_tokens: number;
  request_excerpt: string;
  response_excerpt: string;
};

export async function captureSummary(db: D1Database, sessionId: string): Promise<CaptureSummary> {
  const row = await db.prepare("SELECT SUM(CASE WHEN capture_status = 'saved' THEN 1 ELSE 0 END) AS saved_exchanges, SUM(CASE WHEN capture_status = 'failed' THEN 1 ELSE 0 END) AS failed_exchanges, SUM(CASE WHEN capture_status = 'accepted' THEN 1 ELSE 0 END) AS accepted_exchanges, MAX(CASE WHEN capture_status = 'saved' THEN saved_at END) AS last_saved_at FROM exchanges WHERE session_id = ?")
    .bind(sessionId).first<{ saved_exchanges: number | null; failed_exchanges: number | null; accepted_exchanges: number | null; last_saved_at: string | null }>();
  return captureSummaryValues(row?.saved_exchanges ?? 0, row?.failed_exchanges ?? 0, row?.accepted_exchanges ?? 0, row?.last_saved_at ?? null);
}

export function attachCaptureSummary(row: Record<string, unknown>) {
  const saved = Number(row.capture_saved_exchanges ?? 0);
  const failed = Number(row.capture_failed_exchanges ?? 0);
  const pending = Number(row.capture_pending_exchanges ?? 0);
  const lastSaved = typeof row.capture_last_saved_at === "string" ? row.capture_last_saved_at : null;
  const { capture_saved_exchanges: _saved, capture_failed_exchanges: _failed, capture_pending_exchanges: _pending, capture_last_saved_at: _lastSaved, ...session } = row;
  return { ...session, capture: captureSummaryValues(saved, failed, pending, lastSaved) };
}

export function captureReceipt(summary: CaptureSummary): CaptureReceipt {
  const total = summary.saved_exchanges + summary.failed_exchanges + summary.pending_exchanges;
  if (summary.pending_exchanges > 0 && summary.failed_exchanges > 0) {
    return { label: "Partially saved", detail: `${summary.saved_exchanges} saved · ${summary.failed_exchanges} failed · ${summary.pending_exchanges} pending`, action_label: "View details" };
  }
  if (summary.status === "pending") {
    return { label: "Saving to Mimir...", detail: exchangeDetail(total), action_label: "View session" };
  }
  if (summary.status === "partial") {
    return { label: "Partially saved", detail: `${summary.saved_exchanges} of ${total} exchanges`, action_label: "View details" };
  }
  if (summary.status === "failed") {
    return { label: "Mimir couldn't save this session", detail: exchangeDetail(summary.failed_exchanges), action_label: "View details" };
  }
  if (summary.status === "saved") {
    return { label: "Saved to Mimir", detail: `${exchangeDetail(summary.saved_exchanges)} in this session`, action_label: "View session" };
  }
  return { label: "Not captured", detail: "No exchanges in this session", action_label: "View session" };
}

export function sessionStatusResponse(requestURL: string, sessionID: string, summary: CaptureSummary, session: Record<string, unknown>, dashboardAvailable: boolean) {
  const receipt = captureReceipt(summary);
  return {
    session_id: sessionID,
    capture: summary,
    ...session,
    receipt: dashboardAvailable ? receipt : { ...receipt, action_label: null },
    dashboard_url: dashboardAvailable ? new URL(`/dashboard/sessions/${encodeURIComponent(sessionID)}`, requestURL).toString() : null,
  };
}

function captureSummaryValues(saved: number, failed: number, accepted: number, lastSavedAt: string | null): CaptureSummary {
  let status: CaptureSummary["status"] = "empty";
  if (accepted > 0) status = "pending";
  else if (saved > 0 && failed > 0) status = "partial";
  else if (failed > 0) status = "failed";
  else if (saved > 0) status = "saved";
  return { saved_exchanges: saved, failed_exchanges: failed, pending_exchanges: accepted, last_saved_at: lastSavedAt, status };
}

function exchangeDetail(count: number) {
  return `${count} ${count === 1 ? "exchange" : "exchanges"}`;
}

export async function reconcile(env: Bindings, requestedLimit: number, r2Cursor?: string, databaseCursor?: string, scanDatabase = true, scanR2 = true): Promise<ReconcileResponse> {
  const limit = Math.max(1, Math.min(Number.isFinite(requestedLimit) ? Math.floor(requestedLimit) : 100, 100));
  const decodedCursor = decodeDatabaseCursor(databaseCursor);
  const queried = scanDatabase
    ? await env.DB.prepare(`SELECT id, session_id, ts, accepted_at, capture_status, r2_key, harness, model, input_tokens, output_tokens, request_excerpt, response_excerpt FROM exchanges WHERE capture_status IN ('accepted', 'saved') ${decodedCursor ? "AND id < ?" : ""} ORDER BY id DESC LIMIT ?`)
      .bind(...(decodedCursor ? [decodedCursor, limit + 1] : [limit + 1])).all<ReconcileRow>()
    : { results: [] as ReconcileRow[] };
  const rows = queried.results.slice(0, limit);
  const lastDatabaseRow = rows.at(-1);
  const finalized: string[] = [];
  const pending: string[] = [];
  const stalePending: string[] = [];
  const missingSaved: string[] = [];
  const affectedSessions = new Set<string>();
  const now = new Date().toISOString();
  const staleCutoff = Date.now() - 15 * 60_000;

  for (const row of rows) {
    const object = await env.LOGS.head(row.r2_key);
    if (row.capture_status === "accepted") {
      if (!object) {
        pending.push(row.id);
        if (row.accepted_at && Date.parse(row.accepted_at) < staleCutoff) stalePending.push(row.id);
        continue;
      }
      const recent = !!row.accepted_at && Date.parse(row.accepted_at) >= staleCutoff;
      await finalizeAcceptedExchange(env.DB, row.id, row.session_id, row.ts, now, row.harness, row.model ?? "", row.input_tokens, row.output_tokens, object.size, recent, null);
      finalized.push(row.id);
      continue;
    }
    if (!object) {
      await env.DB.prepare("UPDATE exchanges SET capture_status = 'failed', capture_reason = 'reconciliation', saved_at = NULL, failed_at = ?, failure_code = 'r2_object_missing', r2_bytes = NULL WHERE id = ? AND capture_status = 'saved'").bind(now, row.id).run();
      missingSaved.push(row.id);
      affectedSessions.add(row.session_id);
    }
  }

  if (affectedSessions.size) {
    for (const sessionId of affectedSessions) {
      const hasLegacy = await env.DB.prepare("SELECT 1 FROM exchanges WHERE session_id = ? AND capture_status = 'saved' AND schema_version = 0 LIMIT 1").bind(sessionId).first();
      const statements = [
        env.DB.prepare("UPDATE sessions SET request_count = (SELECT COUNT(*) FROM exchanges WHERE session_id = ? AND capture_status = 'saved'), tokens_in = COALESCE((SELECT SUM(input_tokens) FROM exchanges WHERE session_id = ? AND capture_status = 'saved'), 0), tokens_out = COALESCE((SELECT SUM(output_tokens) FROM exchanges WHERE session_id = ? AND capture_status = 'saved'), 0) WHERE id = ?").bind(sessionId, sessionId, sessionId, sessionId),
      ];
      if (!hasLegacy) {
        statements.push(
          env.DB.prepare("DELETE FROM session_files WHERE session_id = ?").bind(sessionId),
          env.DB.prepare("DELETE FROM session_errors WHERE session_id = ?").bind(sessionId),
          env.DB.prepare("INSERT OR IGNORE INTO session_files(session_id, file) SELECT ef.session_id, ef.file FROM exchange_files ef JOIN exchanges e ON e.id = ef.exchange_id WHERE ef.session_id = ? AND e.capture_status = 'saved'").bind(sessionId),
          env.DB.prepare("INSERT OR IGNORE INTO session_errors(session_id, signature) SELECT ee.session_id, ee.signature FROM exchange_errors ee JOIN exchanges e ON e.id = ee.exchange_id WHERE ee.session_id = ? AND e.capture_status = 'saved'").bind(sessionId),
        );
      }
      await env.DB.batch(statements);
    }
  }

  const listed = scanR2 ? await env.LOGS.list({ prefix: "log/", limit, cursor: r2Cursor }) : { objects: [], truncated: false, cursor: undefined };
  const known = new Set<string>();
  for (let offset = 0; offset < listed.objects.length; offset += 90) {
    const keys = listed.objects.slice(offset, offset + 90).map((object) => object.key);
    if (!keys.length) continue;
    const placeholders = keys.map(() => "?").join(", ");
    const found = await env.DB.prepare(`SELECT r2_key FROM exchanges WHERE r2_key IN (${placeholders})`).bind(...keys).all<{ r2_key: string }>();
    for (const row of found.results) known.add(row.r2_key);
  }
  const orphans = listed.objects.map((object) => object.key).filter((key) => !known.has(key));

  return {
    scanned: rows.length,
    database_truncated: queried.results.length > limit,
    database_cursor: queried.results.length > limit && lastDatabaseRow ? encodeDatabaseCursor(lastDatabaseRow.id) : null,
    finalized: { count: finalized.length, exchange_ids: finalized },
    pending: { count: pending.length, exchange_ids: pending, stale_exchange_ids: stalePending },
    missing_saved: { count: missingSaved.length, exchange_ids: missingSaved, session_ids: [...affectedSessions] },
    orphans: { count: orphans.length, r2_keys: orphans, cursor: listed.truncated ? listed.cursor ?? null : null },
    limit,
  };
}

function encodeDatabaseCursor(id: string) {
  return btoa(JSON.stringify({ id })).replaceAll("+", "-").replaceAll("/", "_").replaceAll("=", "");
}

function decodeDatabaseCursor(value: string | undefined) {
  if (!value) return null;
  try {
    const padded = value.replaceAll("-", "+").replaceAll("_", "/") + "===".slice((value.length + 3) % 4);
    const parsed = JSON.parse(atob(padded)) as { id?: unknown };
    if (typeof parsed.id === "string") return parsed.id;
  } catch {
    // Invalid cursors restart the bounded database scan.
  }
  return null;
}
