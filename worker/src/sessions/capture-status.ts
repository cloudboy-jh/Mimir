import type { Bindings } from "../env";
import { finalizeAcceptedExchange } from "../exchanges/capture-pipeline";
import { reconcileSessionTitleStatement } from "./titles";

export type CaptureSummary = {
  status: "empty" | "pending" | "saved" | "failed" | "partial";
  saved_exchanges: number;
  failed_exchanges: number;
  pending_exchanges: number;
  last_saved_at: string | null;
};

export type ReconcileResponse = {
  scanned: number;
  database_truncated: boolean;
  database_cursor: string | null;
  finalized: { count: number; exchange_ids: string[] };
  pending: {
    count: number;
    exchange_ids: string[];
    stale_exchange_ids: string[];
  };
  swept: { count: number; exchange_ids: string[] };
  missing_saved: {
    count: number;
    exchange_ids: string[];
    session_ids: string[];
  };
  orphans: { count: number; r2_keys: string[]; cursor: string | null };
  empty_sessions_removed: { count: number; session_ids: string[] };
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
  cache_read_tokens: number;
  cache_write_tokens: number;
  request_excerpt: string;
  response_excerpt: string;
  title_candidate: string | null;
};

export function captureSummaryValues(
  saved: number,
  failed: number,
  accepted: number,
  lastSavedAt: string | null,
): CaptureSummary {
  let status: CaptureSummary["status"] = "empty";
  if (accepted > 0) status = "pending";
  else if (saved > 0 && failed > 0) status = "partial";
  else if (failed > 0) status = "failed";
  else if (saved > 0) status = "saved";
  return {
    status,
    saved_exchanges: saved,
    failed_exchanges: failed,
    pending_exchanges: accepted,
    last_saved_at: lastSavedAt,
  };
}

const EMPTY_FINALIZED_HARNESS_SESSION = `
  harness IN ('pi', 'hermes')
  AND parent_session_id IS NULL
  AND boundary = 'header'
  AND state = 'inactive'
  AND ended_at IS NOT NULL
  AND inactive_at IS NOT NULL
  AND request_count = 0
  AND tokens_in = 0
  AND tokens_out = 0
  AND cache_read_tokens = 0
  AND cache_write_tokens = 0
  AND model_primary IS NULL
  AND intent IS NULL
  AND title IS NULL
  AND title_source IS NULL
  AND NOT EXISTS (SELECT 1 FROM sessions child WHERE child.parent_session_id = sessions.id)
  AND NOT EXISTS (SELECT 1 FROM exchanges WHERE exchanges.session_id = sessions.id)
  AND NOT EXISTS (SELECT 1 FROM session_files WHERE session_files.session_id = sessions.id)
  AND NOT EXISTS (SELECT 1 FROM session_errors WHERE session_errors.session_id = sessions.id)
  AND NOT EXISTS (SELECT 1 FROM session_git_artifacts WHERE session_git_artifacts.session_id = sessions.id)
`;

async function removeEmptyFinalizedHarnessSessions(
  env: Bindings,
  limit: number,
): Promise<string[]> {
  const candidates = await env.DB.prepare(
    `SELECT id FROM sessions WHERE ${EMPTY_FINALIZED_HARNESS_SESSION} ORDER BY ended_at, id LIMIT ?`,
  )
    .bind(limit)
    .all<{ id: string }>();
  const removed: string[] = [];
  for (const candidate of candidates.results) {
    // Sessions finalized before the Durable Object removal may still own a
    // transcript manifest in R2.
    await env.LOGS.delete(`sessions/${candidate.id}/transcript.json`);
    const result = await env.DB.prepare(
      `DELETE FROM sessions WHERE id = ? AND ${EMPTY_FINALIZED_HARNESS_SESSION}`,
    )
      .bind(candidate.id)
      .run();
    if (result.meta.changes > 0) removed.push(candidate.id);
  }
  return removed;
}

export async function reconcile(
  env: Bindings,
  requestedLimit: number,
  r2Cursor?: string,
  databaseCursor?: string,
  scanDatabase = true,
  scanR2 = true,
): Promise<ReconcileResponse> {
  const limit = Math.max(
    1,
    Math.min(
      Number.isFinite(requestedLimit) ? Math.floor(requestedLimit) : 100,
      100,
    ),
  );
  const emptySessionsRemoved = scanDatabase
    ? await removeEmptyFinalizedHarnessSessions(env, limit)
    : [];
  const decodedCursor = decodeDatabaseCursor(databaseCursor);
  const queried = scanDatabase
    ? await env.DB.prepare(
        `SELECT id, session_id, ts, accepted_at, capture_status, r2_key, harness, model, input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, request_excerpt, response_excerpt, title_candidate FROM exchanges WHERE capture_status IN ('accepted', 'saved') ${decodedCursor ? "AND id < ?" : ""} ORDER BY id DESC LIMIT ?`,
      )
        .bind(...(decodedCursor ? [decodedCursor, limit + 1] : [limit + 1]))
        .all<ReconcileRow>()
    : { results: [] as ReconcileRow[] };
  const rows = queried.results.slice(0, limit);
  const lastDatabaseRow = rows.at(-1);
  const finalized: string[] = [];
  const pending: string[] = [];
  const stalePending: string[] = [];
  const swept: string[] = [];
  const missingSaved: string[] = [];
  const affectedSessions = new Set<string>();
  const now = new Date().toISOString();
  const staleCutoff = Date.now() - 15 * 60_000;

  for (const row of rows) {
    const object = await env.LOGS.head(row.r2_key);
    if (row.capture_status === "accepted") {
      if (!object) {
        // A stale accepted row with no R2 object can never finish: the
        // finalize write was lost. Mark it failed so sessions stop reporting
        // permanent pending state. Recent rows stay pending; their write may
        // still be in flight.
        const stale =
          !row.accepted_at || Date.parse(row.accepted_at) < staleCutoff;
        if (stale) {
          const update = await env.DB.prepare(
            "UPDATE exchanges SET capture_status = 'failed', capture_reason = 'reconciliation', failed_at = ?, failure_code = 'r2_object_missing' WHERE id = ? AND capture_status = 'accepted'",
          )
            .bind(now, row.id)
            .run();
          if (update.meta.changes > 0) {
            swept.push(row.id);
            continue;
          }
        }
        pending.push(row.id);
        if (stale) stalePending.push(row.id);
        continue;
      }
      const recent =
        !!row.accepted_at && Date.parse(row.accepted_at) >= staleCutoff;
      await finalizeAcceptedExchange(
        env.DB,
        row.id,
        row.session_id,
        row.ts,
        now,
        row.harness,
        row.model ?? "",
        row.input_tokens,
        row.output_tokens,
        row.cache_read_tokens,
        row.cache_write_tokens,
        object.size,
        recent,
        row.title_candidate,
      );
      finalized.push(row.id);
      continue;
    }
    if (!object) {
      await env.DB.prepare(
        "UPDATE exchanges SET capture_status = 'failed', capture_reason = 'reconciliation', saved_at = NULL, failed_at = ?, failure_code = 'r2_object_missing', r2_bytes = NULL WHERE id = ? AND capture_status = 'saved'",
      )
        .bind(now, row.id)
        .run();
      missingSaved.push(row.id);
      affectedSessions.add(row.session_id);
    }
  }

  if (affectedSessions.size) {
    for (const sessionId of affectedSessions) {
      const hasLegacy = await env.DB.prepare(
        "SELECT 1 FROM exchanges WHERE session_id = ? AND capture_status = 'saved' AND schema_version = 0 LIMIT 1",
      )
        .bind(sessionId)
        .first();
      const statements = [
        env.DB.prepare(
          "UPDATE sessions SET request_count = (SELECT COUNT(*) FROM exchanges WHERE session_id = ? AND capture_status = 'saved'), tokens_in = COALESCE((SELECT SUM(input_tokens) FROM exchanges WHERE session_id = ? AND capture_status = 'saved'), 0), tokens_out = COALESCE((SELECT SUM(output_tokens) FROM exchanges WHERE session_id = ? AND capture_status = 'saved'), 0), cache_read_tokens = COALESCE((SELECT SUM(cache_read_tokens) FROM exchanges WHERE session_id = ? AND capture_status = 'saved'), 0), cache_write_tokens = COALESCE((SELECT SUM(cache_write_tokens) FROM exchanges WHERE session_id = ? AND capture_status = 'saved'), 0) WHERE id = ?",
        ).bind(sessionId, sessionId, sessionId, sessionId, sessionId, sessionId),
        reconcileSessionTitleStatement(env.DB, sessionId, now),
      ];
      if (!hasLegacy) {
        statements.push(
          env.DB.prepare("DELETE FROM session_files WHERE session_id = ?").bind(
            sessionId,
          ),
          env.DB.prepare(
            "DELETE FROM session_errors WHERE session_id = ?",
          ).bind(sessionId),
          env.DB.prepare(
            "INSERT OR IGNORE INTO session_files(session_id, file) SELECT ef.session_id, ef.file FROM exchange_files ef JOIN exchanges e ON e.id = ef.exchange_id WHERE ef.session_id = ? AND e.capture_status = 'saved'",
          ).bind(sessionId),
          env.DB.prepare(
            "INSERT OR IGNORE INTO session_errors(session_id, signature) SELECT ee.session_id, ee.signature FROM exchange_errors ee JOIN exchanges e ON e.id = ee.exchange_id WHERE ee.session_id = ? AND e.capture_status = 'saved'",
          ).bind(sessionId),
        );
      }
      await env.DB.batch(statements);
    }
  }

  const listed = scanR2
    ? await env.LOGS.list({ prefix: "log/", limit, cursor: r2Cursor })
    : { objects: [], truncated: false, cursor: undefined };
  const known = new Set<string>();
  for (let offset = 0; offset < listed.objects.length; offset += 90) {
    const keys = listed.objects
      .slice(offset, offset + 90)
      .map((object) => object.key);
    if (!keys.length) continue;
    const placeholders = keys.map(() => "?").join(", ");
    const found = await env.DB.prepare(
      `SELECT r2_key FROM exchanges WHERE r2_key IN (${placeholders})`,
    )
      .bind(...keys)
      .all<{ r2_key: string }>();
    for (const row of found.results) known.add(row.r2_key);
  }
  const orphans = listed.objects
    .map((object) => object.key)
    .filter((key) => !known.has(key));

  return {
    scanned: rows.length,
    database_truncated: queried.results.length > limit,
    database_cursor:
      queried.results.length > limit && lastDatabaseRow
        ? encodeDatabaseCursor(lastDatabaseRow.id)
        : null,
    finalized: { count: finalized.length, exchange_ids: finalized },
    pending: {
      count: pending.length,
      exchange_ids: pending,
      stale_exchange_ids: stalePending,
    },
    swept: { count: swept.length, exchange_ids: swept },
    missing_saved: {
      count: missingSaved.length,
      exchange_ids: missingSaved,
      session_ids: [...affectedSessions],
    },
    orphans: {
      count: orphans.length,
      r2_keys: orphans,
      cursor: listed.truncated ? (listed.cursor ?? null) : null,
    },
    empty_sessions_removed: {
      count: emptySessionsRemoved.length,
      session_ids: emptySessionsRemoved,
    },
    limit,
  };
}

function encodeDatabaseCursor(id: string) {
  return btoa(JSON.stringify({ id }))
    .replaceAll("+", "-")
    .replaceAll("/", "_")
    .replaceAll("=", "");
}

function decodeDatabaseCursor(value: string | undefined) {
  if (!value) return null;
  try {
    const padded =
      value.replaceAll("-", "+").replaceAll("_", "/") +
      "===".slice((value.length + 3) % 4);
    const parsed = JSON.parse(atob(padded)) as { id?: unknown };
    if (typeof parsed.id === "string") return parsed.id;
  } catch {
    // Invalid cursors restart the bounded database scan.
  }
  return null;
}
