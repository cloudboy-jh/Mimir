// Session hierarchy SQL. A root session and its sub-agent descendants form one
// unit of work; most projections aggregate over the whole subtree.

/** `subtree(id)`: the session bound to the first `?` and its descendants. */
export const SUBTREE_CTE =
  "WITH RECURSIVE subtree(id) AS (SELECT ? UNION ALL SELECT sessions.id FROM sessions JOIN subtree ON sessions.parent_session_id = subtree.id)";

/** `session_tree(root_id, id)` over every root, plus `root_activity(root_id, activity_at)`. */
export const ROOT_TREE_CTE =
  "WITH RECURSIVE session_tree(root_id, id) AS (SELECT id, id FROM sessions WHERE parent_session_id IS NULL UNION ALL SELECT session_tree.root_id, sessions.id FROM sessions JOIN session_tree ON sessions.parent_session_id = session_tree.id), root_activity(root_id, activity_at) AS (SELECT session_tree.root_id, MAX(COALESCE(activity.last_active_at, activity.started_at)) FROM session_tree JOIN sessions activity ON activity.id = session_tree.id GROUP BY session_tree.root_id)";

/** `tree(root_id, id)` seeded from a JSON array of session ids bound to `?1`. */
export const SEEDED_TREE_CTE =
  "WITH RECURSIVE tree(root_id, id) AS (SELECT value, value FROM json_each(?1) UNION ALL SELECT tree.root_id, sessions.id FROM sessions JOIN tree ON sessions.parent_session_id = tree.id)";

export async function rootSessionID(db: D1Database, id: string): Promise<string> {
  const root = await db
    .prepare(
      "WITH RECURSIVE ancestors(id, parent_session_id) AS (SELECT id, parent_session_id FROM sessions WHERE id = ? UNION ALL SELECT sessions.id, sessions.parent_session_id FROM sessions JOIN ancestors ON sessions.id = ancestors.parent_session_id) SELECT id FROM ancestors WHERE parent_session_id IS NULL LIMIT 1",
    )
    .bind(id)
    .first<{ id: string }>();
  return root?.id ?? id;
}
