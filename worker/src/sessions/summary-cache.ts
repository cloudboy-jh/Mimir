export const SESSION_SUMMARY_VERSION = "reconstructed:v2";

// Lists and search must not expose an old template or a reconstruction predating
// a late save, descendant activity, outcome event, or Git artifact repair.
export function summaryCacheValidSQL(alias = "sessions"): string {
  return `(${alias}.summary_source LIKE '${SESSION_SUMMARY_VERSION}:%' AND ${alias}.summary_updated_at IS NOT NULL
    AND julianday(${alias}.summary_updated_at) > (
      WITH RECURSIVE summary_tree(id) AS (
        SELECT ${alias}.id UNION ALL SELECT child.id FROM sessions child JOIN summary_tree ON child.parent_session_id = summary_tree.id
      )
      SELECT MAX(
        COALESCE((SELECT MAX(MAX(COALESCE(julianday(started_at), 0), COALESCE(julianday(last_active_at), 0), COALESCE(julianday(ended_at), 0), COALESCE(julianday(inactive_at), 0), COALESCE(julianday(outcome_updated_at), 0))) FROM sessions WHERE id IN (SELECT id FROM summary_tree)), 0),
        COALESCE((SELECT MAX(MAX(COALESCE(julianday(saved_at), 0), COALESCE(julianday(failed_at), 0), COALESCE(julianday(accepted_at), 0), COALESCE(julianday(ts), 0))) FROM exchanges WHERE session_id IN (SELECT id FROM summary_tree)), 0),
        COALESCE((SELECT MAX(julianday(created_at)) FROM session_outcome_events WHERE session_id IN (SELECT id FROM summary_tree)), 0),
        COALESCE((SELECT MAX(MAX(COALESCE(julianday(saved_at), 0), COALESCE(julianday(failed_at), 0), COALESCE(julianday(accepted_at), 0), COALESCE(julianday(created_at), 0))) FROM session_git_artifacts WHERE session_id IN (SELECT id FROM summary_tree)), 0)
      )
    ))`;
}

export function guardedSummaryColumns(alias = "sessions"): string {
  const valid = summaryCacheValidSQL(alias);
  return `CASE WHEN ${valid} THEN ${alias}.summary_text ELSE NULL END AS summary_text, CASE WHEN ${valid} THEN ${alias}.summary_status ELSE 'pending' END AS summary_status`;
}
