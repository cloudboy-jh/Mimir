import type { Hono } from "hono";
import type { AppEnv } from "../env";
import { expireSessions } from "../sessions/lifecycle";
import { canonicalOutcome } from "../sessions/outcomes";
import { SESSION_TREE_CTE } from "../sessions/session-queries";
import { sessionTitleSearchClause } from "../sessions/titles";

const SEARCH_TYPES = [
  "title",
  "intent",
  "excerpts",
  "files",
  "errors",
] as const;
type SearchType = (typeof SEARCH_TYPES)[number];

function searchTypes(types: string[] | undefined): (SearchType | null)[] {
  if (!types?.length) return [...SEARCH_TYPES];
  return types.map((type) =>
    (SEARCH_TYPES as readonly string[]).includes(type)
      ? (type as SearchType)
      : null,
  );
}

function clauseNeedles(type: SearchType) {
  return type === "excerpts" || type === "title" ? 2 : 1;
}

export function registerSearchRoutes(app: Hono<AppEnv>) {
  app.post("/search", async (c) => {
    await expireSessions(c.env.DB);
    const body = await c.req.json<{
      query?: string;
      types?: string[];
      budget?: number;
      filters?: { repo?: string; outcome?: string };
    }>();
    const query = body.query?.trim() ?? "";
    const budget = Math.max(1, Math.min(body.budget ?? 4000, 16000));
    const filters = body.filters ?? {};
    const clauses: string[] = [];
    const values: string[] = [];
    const needle = query;
    for (const type of searchTypes(body.types)) {
      if (!type) return c.json({ error: "invalid search type" }, 400);
      if (type === "title") clauses.push(sessionTitleSearchClause("s"));
      if (type === "intent")
        clauses.push("instr(lower(s.intent), lower(?)) > 0");
      if (type === "excerpts")
        clauses.push(
          "(instr(lower(e.request_excerpt), lower(?)) > 0 OR instr(lower(e.response_excerpt), lower(?)) > 0)",
        );
      if (type === "files")
        clauses.push(
          "EXISTS (SELECT 1 FROM session_files sf WHERE sf.session_id = s.id AND instr(lower(sf.file), lower(?)) > 0)",
        );
      if (type === "errors")
        clauses.push(
          "EXISTS (SELECT 1 FROM session_errors se WHERE se.session_id = s.id AND instr(lower(se.signature), lower(?)) > 0)",
        );
      values.push(...Array(clauseNeedles(type)).fill(needle));
    }
    const where = [`(${clauses.join(" OR ")})`];
    if (filters.repo) {
      where.push("root.repo = ?");
      values.push(filters.repo);
    }
    if (filters.outcome) {
      const canonical = canonicalOutcome(filters.outcome);
      if (!canonical) return c.json({ error: "invalid outcome" }, 400);
      where.push("root.work_outcome = ?");
      values.push(canonical);
    }
    where.push("e.capture_status = 'saved'");
    const sql = `${SESSION_TREE_CTE} SELECT root.id AS session_id, root.started_at, root.work_outcome AS outcome, root.repo, root.model_primary, root.title, root.title_source, root.title_updated_at, COALESCE(root.title, root.intent, root.id) AS display_title, e.id AS exchange_id, e.request_excerpt, e.response_excerpt, e.r2_key FROM sessions s JOIN session_tree ON session_tree.id = s.id JOIN sessions root ON root.id = session_tree.root_id JOIN exchanges e ON e.session_id = s.id WHERE ${where.join(" AND ")} ORDER BY root.started_at DESC LIMIT 50`;
    const result = await c.env.DB.prepare(sql)
      .bind(...values)
      .all<Record<string, unknown>>();
    const matches: Record<string, unknown>[] = [];
    let used = 0;
    for (const row of result.results) {
      const cost = JSON.stringify(row).length;
      if (matches.length && used + cost > budget * 4) break;
      matches.push(row);
      used += cost;
    }
    return c.json({ query, budget, matches });
  });
}
