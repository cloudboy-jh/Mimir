import { sessionTitleColumns } from "./titles";
export const SESSION_COLUMNS = `id, parent_session_id, installation_id, started_at, ended_at, state, last_active_at, COALESCE(last_active_at, started_at) AS activity_at, inactive_at, harness, boundary, work_outcome AS outcome, outcome_src, outcome_updated_at, outcome_reason, repo, source_ref, model_primary, request_count, tokens_in, tokens_out, intent, summary_text, summary_status, summary_source, summary_updated_at, ${sessionTitleColumns("sessions")}, (SELECT COUNT(*) FROM sessions child WHERE child.parent_session_id = sessions.id) AS child_session_count`;

export const SESSION_TREE_CTE =
  "WITH RECURSIVE session_tree(root_id, id) AS (SELECT id, id FROM sessions WHERE parent_session_id IS NULL UNION ALL SELECT session_tree.root_id, sessions.id FROM sessions JOIN session_tree ON sessions.parent_session_id = session_tree.id), root_activity(root_id, activity_at) AS (SELECT session_tree.root_id, MAX(COALESCE(activity.last_active_at, activity.started_at)) FROM session_tree JOIN sessions activity ON activity.id = session_tree.id GROUP BY session_tree.root_id)";

export const ROOT_SESSION_ACTIVITY_AT =
  "(SELECT root_activity.activity_at FROM root_activity WHERE root_activity.root_id = sessions.id)";

export const ROOT_SESSION_COLUMNS = `sessions.id, sessions.parent_session_id, sessions.installation_id, sessions.started_at, sessions.ended_at, sessions.state, sessions.last_active_at, ${ROOT_SESSION_ACTIVITY_AT} AS activity_at, sessions.inactive_at, sessions.harness, sessions.boundary, sessions.work_outcome AS outcome, sessions.outcome_src, sessions.outcome_updated_at, sessions.outcome_reason, sessions.repo, sessions.source_ref, sessions.model_primary, (SELECT COALESCE(SUM(child.request_count), 0) FROM sessions child JOIN session_tree ON session_tree.id = child.id WHERE session_tree.root_id = sessions.id) AS request_count, (SELECT COALESCE(SUM(child.tokens_in), 0) FROM sessions child JOIN session_tree ON session_tree.id = child.id WHERE session_tree.root_id = sessions.id) AS tokens_in, (SELECT COALESCE(SUM(child.tokens_out), 0) FROM sessions child JOIN session_tree ON session_tree.id = child.id WHERE session_tree.root_id = sessions.id) AS tokens_out, sessions.intent, sessions.summary_text, sessions.summary_status, sessions.summary_source, sessions.summary_updated_at, ${sessionTitleColumns("sessions")}, (SELECT COUNT(*) FROM sessions child WHERE child.parent_session_id = sessions.id) AS child_session_count`;

export const SESSION_SUBTREE_CTE =
  "WITH RECURSIVE subtree(id) AS (SELECT ? UNION ALL SELECT sessions.id FROM sessions JOIN subtree ON sessions.parent_session_id = subtree.id)";
export async function loadSessionRecord(db: D1Database, id: string) {
  const requested = await db
    .prepare("SELECT parent_session_id FROM sessions WHERE id = ?")
    .bind(id)
    .first<{ parent_session_id: string | null }>();
  if (!requested) return null;
  // Sessions render their own activity and counts. Sub-agent work is reached
  // through the supporting-session tree, not merged into the parent record.
  return db
    .prepare(`SELECT ${SESSION_COLUMNS} FROM sessions WHERE id = ?`)
    .bind(id)
    .first<Record<string, unknown>>();
}

export function loadSessionStatus(db: D1Database, id: string) {
  return db
    .prepare(
      `SELECT state, ended_at, inactive_at, work_outcome AS outcome, outcome_src, outcome_updated_at, outcome_reason, ${sessionTitleColumns("sessions")} FROM sessions WHERE id = ?`,
    )
    .bind(id)
    .first<Record<string, unknown>>();
}

export async function loadSessionFiles(db: D1Database, id: string) {
  const rows = await db
    .prepare(
      `${SESSION_SUBTREE_CTE} SELECT DISTINCT file FROM session_files WHERE session_id IN (SELECT id FROM subtree) ORDER BY file`,
    )
    .bind(id)
    .all<{ file: string }>();
  return rows.results.map((row) => row.file);
}

export async function loadSessionErrorSignatures(db: D1Database, id: string) {
  const rows = await db
    .prepare(
      `${SESSION_SUBTREE_CTE} SELECT DISTINCT signature FROM session_errors WHERE session_id IN (SELECT id FROM subtree) ORDER BY signature`,
    )
    .bind(id)
    .all<{ signature: string }>();
  return rows.results.map((row) => row.signature);
}

export async function loadSupportingSessions(db: D1Database, id: string) {
  const rows = await db
    .prepare(
      `${SESSION_SUBTREE_CTE} SELECT ${SESSION_COLUMNS} FROM sessions WHERE id IN (SELECT id FROM subtree) AND id <> ? ORDER BY started_at`,
    )
    .bind(id, id)
    .all<Record<string, unknown>>();
  return rows.results;
}

export async function loadSessionOutcomeEvents(db: D1Database, id: string) {
  const rows = await db
    .prepare(
      "SELECT id, outcome, source, reason, evidence_json, created_at FROM session_outcome_events WHERE session_id = ? ORDER BY created_at DESC",
    )
    .bind(id)
    .all<Record<string, unknown>>();
  return rows.results;
}

export type SessionDiffEvidence =
  | { kind: "stream"; body: ReadableStream }
  | { kind: "text"; body: string }
  | { kind: "missing-artifact" }
  | { kind: "unavailable" }
  | { kind: "session-not-found" };

export async function loadSessionDiffEvidence(
  db: D1Database,
  bucket: R2Bucket,
  id: string,
): Promise<SessionDiffEvidence> {
  if (
    !(await db.prepare("SELECT 1 FROM sessions WHERE id = ?").bind(id).first())
  )
    return { kind: "session-not-found" };
  const root = await rootSessionID(db, id);
  const events = await db
    .prepare(
      "SELECT evidence_json FROM session_outcome_events WHERE session_id = ? AND evidence_json IS NOT NULL ORDER BY created_at DESC, rowid DESC",
    )
    .bind(root)
    .all<{ evidence_json: string }>();
  for (const event of events.results) {
    let evidence: Record<string, unknown>;
    try {
      evidence = JSON.parse(event.evidence_json) as Record<string, unknown>;
    } catch {
      continue;
    }
    if (typeof evidence.patch_r2_key === "string") {
      if (!evidence.patch_r2_key.startsWith(`sessions/${root}/diffs/`))
        continue;
      const object = await bucket.get(evidence.patch_r2_key);
      return object
        ? { kind: "stream", body: object.body }
        : { kind: "missing-artifact" };
    }
    if (typeof evidence.patch === "string" && evidence.patch)
      return { kind: "text", body: evidence.patch };
  }
  return { kind: "unavailable" };
}
export async function rootSessionID(
  db: D1Database,
  id: string,
): Promise<string> {
  const root = await db
    .prepare(
      "WITH RECURSIVE ancestors(id, parent_session_id) AS (SELECT id, parent_session_id FROM sessions WHERE id = ? UNION ALL SELECT sessions.id, sessions.parent_session_id FROM sessions JOIN ancestors ON sessions.id = ancestors.parent_session_id) SELECT id FROM ancestors WHERE parent_session_id IS NULL LIMIT 1",
    )
    .bind(id)
    .first<{ id: string }>();
  return root?.id ?? id;
}
