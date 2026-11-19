import { readSaveConfig } from "../config/config-store";
import { ulid } from "../shared/ulid";

const HERMES_EXACT_ADOPTION_MS = 30_000;

/** A heuristic session may become active only while no other heuristic session owns its key. */
export const HEURISTIC_REACTIVATION_GUARD =
  "(boundary = 'header' OR NOT EXISTS (SELECT 1 FROM sessions active WHERE active.id <> sessions.id AND active.boundary = 'heuristic' AND active.state = 'active' AND active.repo IS sessions.repo AND active.harness IS sessions.harness AND active.installation_id IS sessions.installation_id))";

export async function resolveSession(
  db: D1Database,
  declared: string | null,
  repo: string | null,
  harness: string | null,
  sourceRef: string | null,
  model: string,
  now: string,
  installationID: string | null = null,
) {
  if (declared) {
    await db
      .prepare(
        "INSERT OR IGNORE INTO sessions(id, installation_id, started_at, last_active_at, harness, boundary, repo, source_ref, model_primary) VALUES (?, ?, ?, ?, ?, 'header', ?, ?, ?)",
      )
      .bind(declared, installationID, now, now, harness, repo, sourceRef, model)
      .run();
    if (installationID) {
      const associated = await db
        .prepare(
          "UPDATE sessions SET installation_id = COALESCE(installation_id, ?) WHERE id = ? AND (installation_id IS NULL OR installation_id = ?)",
        )
        .bind(installationID, declared, installationID)
        .run();
      if (associated.meta.changes === 0)
        throw new Error("session belongs to another installation");
    }
    await db
      .prepare(
        "UPDATE sessions SET harness = COALESCE(harness, ?), repo = COALESCE(repo, ?), source_ref = COALESCE(source_ref, ?), model_primary = COALESCE(model_primary, ?) WHERE id = ?",
      )
      .bind(harness, repo, sourceRef, model, declared)
      .run();
    return { id: declared };
  }
  const config = await readSaveConfig(db);
  const cutoff = new Date(Date.parse(now) - config.gapMinutes * 60_000).toISOString();
  if (harness === "hermes") {
    const exactCutoff = new Date(Date.parse(now) - HERMES_EXACT_ADOPTION_MS).toISOString();
    const exact = await db
      .prepare(
        "SELECT id FROM sessions WHERE boundary = 'header' AND state = 'active' AND repo IS ? AND harness = 'hermes' AND installation_id IS ? AND last_active_at >= ? ORDER BY last_active_at DESC LIMIT 1",
      )
      .bind(repo, installationID, exactCutoff)
      .first<{ id: string }>();
    if (exact) return exact;
  }
  const prior = await db
    .prepare(
      "SELECT id FROM sessions WHERE boundary = 'heuristic' AND state = 'active' AND repo IS ? AND harness IS ? AND installation_id IS ? AND last_active_at >= ? ORDER BY last_active_at DESC LIMIT 1",
    )
    .bind(repo, harness, installationID, cutoff)
    .first<{ id: string }>();
  if (prior) return prior;
  // A silent heuristic session for this key has ended; release its slot so
  // the new boundary can start without waiting for scheduled expiry.
  await db
    .prepare(
      "UPDATE sessions SET state = 'inactive', inactive_at = COALESCE(inactive_at, ?), ended_at = COALESCE(ended_at, last_active_at) WHERE boundary = 'heuristic' AND state = 'active' AND repo IS ? AND harness IS ? AND installation_id IS ? AND last_active_at < ?",
    )
    .bind(now, repo, harness, installationID, cutoff)
    .run();
  const id = ulid();
  await db
    .prepare(
      "INSERT OR IGNORE INTO sessions(id, installation_id, started_at, last_active_at, harness, boundary, repo, model_primary) VALUES (?, ?, ?, ?, ?, 'heuristic', ?, ?)",
    )
    .bind(id, installationID, now, now, harness, repo, model)
    .run();
  const active = await db
    .prepare(
      "SELECT id FROM sessions WHERE boundary = 'heuristic' AND state = 'active' AND repo IS ? AND harness IS ? AND installation_id IS ? ORDER BY last_active_at DESC LIMIT 1",
    )
    .bind(repo, harness, installationID)
    .first<{ id: string }>();
  if (!active) throw new Error("could not resolve heuristic session");
  return active;
}

/** Marks sessions silent for longer than the configured gap inactive; returns their ids. */
export async function expireSessions(db: D1Database, now = new Date().toISOString()) {
  const { gapMinutes } = await readSaveConfig(db);
  const cutoff = new Date(Date.parse(now) - gapMinutes * 60_000).toISOString();
  const expired = await db
    .prepare(
      "UPDATE sessions SET state = 'inactive', inactive_at = COALESCE(inactive_at, ?), ended_at = COALESCE(ended_at, last_active_at) WHERE state = 'active' AND COALESCE(last_active_at, started_at) < ? RETURNING id",
    )
    .bind(now, cutoff)
    .all<{ id: string }>();
  return expired.results.map((row) => row.id);
}

/** Ends an active session. Returns false when the session does not exist. Idempotent. */
export async function endSession(db: D1Database, id: string, at: string) {
  const [, session] = await db.batch<{ id: string }>([
    db
      .prepare(
        "UPDATE sessions SET state = 'inactive', ended_at = ?, inactive_at = ?, last_active_at = CASE WHEN last_active_at IS NULL OR last_active_at < ? THEN ? ELSE last_active_at END WHERE id = ? AND state = 'active'",
      )
      .bind(at, at, at, at, id),
    db.prepare("SELECT id FROM sessions WHERE id = ?").bind(id),
  ]);
  return session!.results.length > 0;
}

export async function canMutateSession(
  db: D1Database,
  id: string,
  installationID: string | null,
): Promise<boolean> {
  const root = await db
    .prepare(
      "WITH RECURSIVE ancestors(id, parent_session_id, installation_id) AS (SELECT id, parent_session_id, installation_id FROM sessions WHERE id = ? UNION ALL SELECT sessions.id, sessions.parent_session_id, sessions.installation_id FROM sessions JOIN ancestors ON sessions.id = ancestors.parent_session_id) SELECT installation_id FROM ancestors WHERE parent_session_id IS NULL LIMIT 1",
    )
    .bind(id)
    .first<{ installation_id: string | null }>();
  return !root?.installation_id || !installationID || root.installation_id === installationID;
}

export type ParentRejection = { status: 403 | 404 | 409; error: string };

// assignParent records parent intent; the D1 triggers resolve it once the
// parent exists and reject conflicts, cycles and cross-installation trees.
export async function assignParent(
  db: D1Database,
  sessionID: string,
  parentID: string,
  installationID: string | null,
  allowMissing: boolean,
): Promise<ParentRejection | null> {
  const parent = await db.prepare("SELECT id FROM sessions WHERE id = ?").bind(parentID).first();
  if (!parent && !allowMissing) return { status: 404, error: "parent session not found" };
  if (
    !(await canMutateSession(db, sessionID, installationID)) ||
    (parent && !(await canMutateSession(db, parentID, installationID)))
  )
    return { status: 403, error: "session belongs to another installation" };
  try {
    await db
      .prepare("UPDATE sessions SET requested_parent_session_id = ? WHERE id = ?")
      .bind(parentID, sessionID)
      .run();
  } catch (error) {
    if (isParentConflict(error)) return { status: 409, error: "invalid session parent relationship" };
    throw error;
  }
  return null;
}

export function isParentConflict(error: unknown) {
  return error instanceof Error && error.message.includes("session parent");
}
