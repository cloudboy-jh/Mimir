import { ftsQuery } from "../search/search-index";
import { ROOT_TREE_CTE } from "../sessions/session-tree";
import { boundedLimit } from "../shared/cursors";
import { dateRange } from "../shared/date-range";
import { QueryError } from "../shared/query-error";
import type { QueryContext, QueryParams } from "./query-context";
import { loadSessions, type Session } from "./session-records";

const HITS_PER_SESSION = 3;
const CANDIDATE_HITS = 500;

export type SearchResult = {
  session: Session;
  score: number;
  hits: Array<{ exchange_id: string; ts: string; snippet: string }>;
};

// searchSessions ranks saved exchange text with FTS5 bm25 and groups hits by
// root session; a session scores as its best hit.
export async function searchSessions(ctx: QueryContext, params: QueryParams) {
  const query = params.q?.trim() ?? "";
  if (!query) throw new QueryError(400, "q is required");
  const match = ftsQuery(query);
  if (!match) throw new QueryError(400, "q must contain a word");
  const limit = boundedLimit(params.limit, 10, 50);
  const where = ["exchange_search MATCH ?", "e.capture_status = 'saved'"];
  const values: string[] = [match];
  if (params.repo) {
    where.push("root.repo = ?");
    values.push(params.repo);
  }
  const range = dateRange(params.from, params.to);
  if (range.start) {
    where.push("julianday(e.ts) >= julianday(?)");
    values.push(range.start);
  }
  if (range.end) {
    where.push("julianday(e.ts) < julianday(?)");
    values.push(range.end);
  }
  const rows = await ctx.db
    .prepare(
      `${ROOT_TREE_CTE} SELECT session_tree.root_id, e.id AS exchange_id, e.ts, bm25(exchange_search) AS rank, snippet(exchange_search, 1, '**', '**', '…', 16) AS snippet FROM exchange_search JOIN exchanges e ON e.id = exchange_search.exchange_id JOIN session_tree ON session_tree.id = e.session_id JOIN sessions root ON root.id = session_tree.root_id WHERE ${where.join(" AND ")} ORDER BY rank LIMIT ${CANDIDATE_HITS}`,
    )
    .bind(...values)
    .all<{ root_id: string; exchange_id: string; ts: string; rank: number; snippet: string }>();
  const grouped = new Map<string, { score: number; hits: SearchResult["hits"] }>();
  for (const row of rows.results) {
    const group = grouped.get(row.root_id);
    if (!group) {
      if (grouped.size === limit) continue;
      grouped.set(row.root_id, { score: -row.rank, hits: [{ exchange_id: row.exchange_id, ts: row.ts, snippet: row.snippet }] });
    } else if (group.hits.length < HITS_PER_SESSION) group.hits.push({ exchange_id: row.exchange_id, ts: row.ts, snippet: row.snippet });
  }
  const sessions = new Map((await loadSessions(ctx.db, [...grouped.keys()])).map((session) => [session.id, session]));
  return {
    query,
    results: [...grouped].flatMap(([id, group]): SearchResult[] => {
      const session = sessions.get(id);
      return session ? [{ session, score: group.score, hits: group.hits }] : [];
    }),
  };
}
