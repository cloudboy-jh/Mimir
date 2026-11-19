import type { Bindings } from "../env";
import { readArchives } from "../exchanges/archives";
import { searchText, type Envelope } from "../exchanges/transcript";

const BACKFILL_BATCH = 20;

// The insert is guarded by the exchange's `search_indexed` flag, so capture,
// reconciliation and the backfill can race without duplicating rows.
export function searchIndexStatements(db: D1Database, exchangeID: string, body: string) {
  return [
    db
      .prepare(
        "INSERT INTO exchange_search(exchange_id, body) SELECT ?1, ?2 WHERE ?2 <> '' AND EXISTS (SELECT 1 FROM exchanges WHERE id = ?1 AND capture_status = 'saved' AND search_indexed = 0)",
      )
      .bind(exchangeID, body),
    db.prepare("UPDATE exchanges SET search_indexed = 1 WHERE id = ? AND capture_status = 'saved'").bind(exchangeID),
  ];
}

/** Best-effort indexing at capture time; the scheduled backfill retries misses. */
export async function indexExchange(db: D1Database, exchangeID: string, envelope: Envelope) {
  try {
    await db.batch(searchIndexStatements(db, exchangeID, searchText(envelope)));
  } catch (error) {
    console.error(
      JSON.stringify({
        message: "exchange search indexing failed",
        exchange_id: exchangeID,
        error: error instanceof Error ? error.message : String(error),
      }),
    );
  }
}

/** Indexes a bounded batch of saved, unindexed exchanges. Returns the number indexed. */
export async function backfillSearchIndex(env: Bindings, limit = BACKFILL_BATCH) {
  const rows = await env.DB.prepare(
    "SELECT id, r2_key, request_excerpt, response_excerpt FROM exchanges WHERE capture_status = 'saved' AND search_indexed = 0 ORDER BY ts DESC LIMIT ?",
  )
    .bind(limit)
    .all<{ id: string; r2_key: string; request_excerpt: string; response_excerpt: string }>();
  if (!rows.results.length) return 0;
  const envelopes = await readArchives(env.LOGS, rows.results.map((row) => row.r2_key));
  // Missing or oversized archives remain searchable through their D1 excerpts.
  await env.DB.batch(
    rows.results.flatMap((row, index) => {
      const envelope = envelopes[index];
      const body = envelope ? searchText(envelope) : `${row.request_excerpt}\n${row.response_excerpt}`.trim();
      return searchIndexStatements(env.DB, row.id, body);
    }),
  );
  return rows.results.length;
}

/** Builds an FTS5 query matching every word of free text; null when it has none. */
export function ftsQuery(text: string): string | null {
  const words = text.match(/[\p{L}\p{N}_]+/gu)?.slice(0, 16);
  return words?.length ? words.map((word) => `"${word}"`).join(" ") : null;
}
