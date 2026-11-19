import { readArchives } from "../exchanges/archives";
import { buildTurnContents, type ToolCall } from "../exchanges/transcript";
import { SUBTREE_CTE } from "../sessions/session-tree";
import { boundedLimit, decodeCursor, encodeCursor } from "../shared/cursors";
import { QueryError } from "../shared/query-error";
import type { QueryContext, QueryParams } from "./query-context";

const REQUEST_KINDS = ["primary", "title", "summary", "compaction"] as const;
type RequestKind = (typeof REQUEST_KINDS)[number];

export type Turn = {
  exchange_id: string;
  session_id: string;
  ts: string;
  model: string | null;
  request_kind: RequestKind;
  capture_status: string;
  user: string | null;
  assistant: string | null;
  reasoning: string | null;
  tools: ToolCall[];
  truncated: boolean;
};

type ExchangeRow = { id: string; session_id: string; ts: string; model: string | null; request_kind: RequestKind; capture_status: string; r2_key: string };

// listTurns reads one page of subtree exchanges and rebuilds their turns from
// the R2 archives (bounded reads, two at a time). Within a page, replayed
// context is removed and tool results are paired with earlier calls.
export async function listTurns(ctx: QueryContext, id: string, params: QueryParams) {
  const order = params.order ?? "asc";
  if (order !== "asc" && order !== "desc") throw new QueryError(400, "invalid order");
  const kinds = (params.kinds ?? "primary,compaction").split(",").map((kind) => kind.trim()).filter(Boolean);
  if (!kinds.length || kinds.some((kind) => !(REQUEST_KINDS as readonly string[]).includes(kind))) throw new QueryError(400, "invalid kinds");
  const limit = boundedLimit(params.limit, 8, 50);
  const cursor = decodeCursor(params.cursor, (value) =>
    typeof value.t === "string" && typeof value.id === "string" && value.o === order ? { t: value.t, id: value.id } : null,
  );
  if (!(await ctx.db.prepare("SELECT 1 FROM sessions WHERE id = ?").bind(id).first())) throw new QueryError(404, "session not found");
  const operator = order === "asc" ? ">" : "<";
  const direction = order === "asc" ? "ASC" : "DESC";
  const rows = await ctx.db
    .prepare(
      `${SUBTREE_CTE} SELECT id, session_id, ts, model, request_kind, capture_status, r2_key FROM exchanges WHERE session_id IN (SELECT id FROM subtree) AND request_kind IN (SELECT value FROM json_each(?)) ${cursor ? `AND (ts ${operator} ? OR (ts = ? AND id ${operator} ?))` : ""} ORDER BY ts ${direction}, id ${direction} LIMIT ?`,
    )
    .bind(id, JSON.stringify(kinds), ...(cursor ? [cursor.t, cursor.t, cursor.id] : []), limit + 1)
    .all<ExchangeRow>();
  const page = rows.results.slice(0, limit);
  const chronological = order === "asc" ? page : [...page].reverse();
  const envelopes = await readArchives(
    ctx.logs,
    chronological.map((row) => (row.capture_status === "saved" ? row.r2_key : "")),
  );
  const contents = buildTurnContents(
    chronological.map((row, index) => ({ exchange_id: row.id, session_id: row.session_id, primary: row.request_kind === "primary", envelope: envelopes[index] ?? null })),
  );
  const last = page.at(-1);
  return {
    turns: page.map((row): Turn => {
      const content = contents.get(row.id);
      return {
        exchange_id: row.id,
        session_id: row.session_id,
        ts: row.ts,
        model: row.model,
        request_kind: row.request_kind,
        capture_status: row.capture_status,
        user: content?.user ?? null,
        assistant: content?.assistant ?? null,
        reasoning: content?.reasoning ?? null,
        tools: content?.tools ?? [],
        // A saved exchange without readable content exceeded the archive bound or is missing.
        truncated: content?.truncated ?? row.capture_status === "saved",
      };
    }),
    next_cursor: rows.results.length > limit && last ? encodeCursor({ t: last.ts, id: last.id, o: order }) : null,
  };
}
