import { readSaveConfig } from "../config/config-store";
import { ulid } from "../shared/ulid";
const HERMES_EXACT_ADOPTION_MS = 30_000;
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
  const cutoff = new Date(
    Date.parse(now) - config.gapMinutes * 60_000,
  ).toISOString();
  if (harness === "hermes") {
    const exactCutoff = new Date(
      Date.parse(now) - HERMES_EXACT_ADOPTION_MS,
    ).toISOString();
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

export async function expireSessions(
  db: D1Database,
  gapMinutes?: number,
  now = new Date().toISOString(),
) {
  const gap = gapMinutes ?? (await readSaveConfig(db)).gapMinutes;
  const cutoff = new Date(Date.parse(now) - gap * 60_000).toISOString();
  await db
    .prepare(
      "UPDATE sessions SET state = 'inactive', inactive_at = COALESCE(inactive_at, ?), ended_at = COALESCE(ended_at, last_active_at) WHERE state = 'active' AND last_active_at < ?",
    )
    .bind(now, cutoff)
    .run();
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
  return (
    !root?.installation_id ||
    !installationID ||
    root.installation_id === installationID
  );
}
