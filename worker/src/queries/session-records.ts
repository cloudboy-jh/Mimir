import { captureSummaryValues, type CaptureSummary } from "../sessions/capture-status";
import { SEEDED_TREE_CTE } from "../sessions/session-tree";

export type GitStatus = "none" | "committed" | "pushed";
export type DeviceIdentity = { id: string; name: string; platform: string; arch: string };
export type SessionModel = { name: string; request_count: number; first_seen_at: string | null; last_seen_at: string | null };

export type Session = {
  id: string;
  parent_session_id: string | null;
  started_at: string;
  ended_at: string | null;
  activity_at: string;
  state: "active" | "inactive";
  harness: string | null;
  repo: string | null;
  source_ref: string | null;
  model_primary: string | null;
  models: SessionModel[];
  title: string | null;
  title_source: string | null;
  display_title: string;
  intent: string | null;
  request_count: number;
  tokens_in: number;
  tokens_out: number;
  cache_read_tokens: number;
  cache_write_tokens: number;
  child_session_count: number;
  device: DeviceIdentity | null;
  capture: CaptureSummary;
  git_status: GitStatus;
  commit_count: number;
  goal: string | null;
};

type BaseRow = {
  id: string;
  parent_session_id: string | null;
  started_at: string;
  ended_at: string | null;
  state: "active" | "inactive";
  harness: string | null;
  repo: string | null;
  source_ref: string | null;
  model_primary: string | null;
  title: string | null;
  title_source: string | null;
  display_title: string;
  intent: string | null;
  summary_goal: string | null;
  child_session_count: number;
  device_id: string | null;
  device_name: string | null;
  device_platform: string | null;
  device_arch: string | null;
};
type TotalsRow = { root_id: string; activity_at: string; request_count: number; tokens_in: number; tokens_out: number; cache_read_tokens: number; cache_write_tokens: number };
type CaptureRow = { root_id: string; saved: number; failed: number; pending: number; last_saved_at: string | null };
type ModelRow = SessionModel & { root_id: string };
type GitRow = { root_id: string; commit_count: number; pushed: number };

export const DISPLAY_TITLE_SQL = (alias: string) => `COALESCE(${alias}.title, substr(${alias}.intent, 1, 100), ${alias}.id)`;

function byRoot<T extends { root_id: string }>(rows: T[]) {
  return new Map(rows.map((row) => [row.root_id, row]));
}

// loadSessions projects session ids into API sessions, in the given order.
// Counts, capture, models and git status cover each session's whole subtree.
export async function loadSessions(db: D1Database, ids: string[]): Promise<Session[]> {
  if (!ids.length) return [];
  const seed = JSON.stringify(ids);
  const [base, totals, capture, models, git] = await db.batch<Record<string, unknown>>([
    db
      .prepare(
        `SELECT s.id, s.parent_session_id, s.started_at, s.ended_at, s.state, s.harness, s.repo, s.source_ref, s.model_primary, s.title, s.title_source, ${DISPLAY_TITLE_SQL("s")} AS display_title, s.intent, CASE WHEN s.summary_status = 'ready' THEN json_extract(s.summary_json, '$.goal') END AS summary_goal, (SELECT COUNT(*) FROM sessions child WHERE child.parent_session_id = s.id) AS child_session_count, m.installation_id AS device_id, m.name AS device_name, m.platform AS device_platform, m.arch AS device_arch FROM sessions s LEFT JOIN machines m ON m.installation_id = s.installation_id WHERE s.id IN (SELECT value FROM json_each(?1))`,
      )
      .bind(seed),
    db
      .prepare(
        `${SEEDED_TREE_CTE} SELECT tree.root_id, MAX(COALESCE(s.last_active_at, s.started_at)) AS activity_at, SUM(s.request_count) AS request_count, SUM(s.tokens_in) AS tokens_in, SUM(s.tokens_out) AS tokens_out, SUM(s.cache_read_tokens) AS cache_read_tokens, SUM(s.cache_write_tokens) AS cache_write_tokens FROM tree JOIN sessions s ON s.id = tree.id GROUP BY tree.root_id`,
      )
      .bind(seed),
    db
      .prepare(
        `${SEEDED_TREE_CTE} SELECT tree.root_id, SUM(e.capture_status = 'saved') AS saved, SUM(e.capture_status = 'failed') AS failed, SUM(e.capture_status = 'accepted') AS pending, MAX(CASE WHEN e.capture_status = 'saved' THEN e.saved_at END) AS last_saved_at FROM tree JOIN exchanges e ON e.session_id = tree.id GROUP BY tree.root_id`,
      )
      .bind(seed),
    db
      .prepare(
        `${SEEDED_TREE_CTE} SELECT tree.root_id, e.model AS name, COUNT(*) AS request_count, MIN(e.ts) AS first_seen_at, MAX(e.ts) AS last_seen_at FROM tree JOIN exchanges e ON e.session_id = tree.id WHERE e.capture_status = 'saved' AND e.model IS NOT NULL AND e.model <> '' GROUP BY tree.root_id, e.model ORDER BY first_seen_at, name`,
      )
      .bind(seed),
    db
      .prepare(
        `${SEEDED_TREE_CTE} SELECT tree.root_id, COUNT(DISTINCT a.commit_sha) AS commit_count, MAX(a.pushed) AS pushed FROM tree JOIN session_git_artifacts a ON a.session_id = tree.id WHERE a.capture_status = 'saved' GROUP BY tree.root_id`,
      )
      .bind(seed),
  ]);
  const rows = new Map((base!.results as BaseRow[]).map((row) => [row.id, row]));
  const totalsByRoot = byRoot(totals!.results as TotalsRow[]);
  const captureByRoot = byRoot(capture!.results as CaptureRow[]);
  const gitByRoot = byRoot(git!.results as GitRow[]);
  const modelsByRoot = new Map<string, SessionModel[]>();
  for (const { root_id, ...model } of models!.results as ModelRow[]) {
    const list = modelsByRoot.get(root_id) ?? [];
    list.push(model);
    modelsByRoot.set(root_id, list);
  }
  return ids.flatMap((id) => {
    const row = rows.get(id);
    if (!row) return [];
    const total = totalsByRoot.get(id);
    const captured = captureByRoot.get(id);
    const commits = gitByRoot.get(id);
    const sessionModels = modelsByRoot.get(id) ?? [];
    const primary = sessionModels.findIndex((model) => model.name === row.model_primary);
    if (primary > 0) sessionModels.unshift(...sessionModels.splice(primary, 1));
    else if (primary < 0 && row.model_primary)
      sessionModels.unshift({ name: row.model_primary, request_count: 0, first_seen_at: null, last_seen_at: null });
    const session: Session = {
      id: row.id,
      parent_session_id: row.parent_session_id,
      started_at: row.started_at,
      ended_at: row.state === "inactive" ? row.ended_at : null,
      activity_at: total?.activity_at ?? row.started_at,
      state: row.state,
      harness: row.harness,
      repo: row.repo,
      source_ref: row.source_ref,
      model_primary: row.model_primary,
      models: sessionModels,
      title: row.title,
      title_source: row.title_source,
      display_title: row.display_title,
      intent: row.intent,
      request_count: Number(total?.request_count ?? 0),
      tokens_in: Number(total?.tokens_in ?? 0),
      tokens_out: Number(total?.tokens_out ?? 0),
      cache_read_tokens: Number(total?.cache_read_tokens ?? 0),
      cache_write_tokens: Number(total?.cache_write_tokens ?? 0),
      child_session_count: Number(row.child_session_count),
      device: row.device_id
        ? { id: row.device_id, name: row.device_name ?? row.device_id, platform: row.device_platform ?? "", arch: row.device_arch ?? "" }
        : null,
      capture: captureSummaryValues(Number(captured?.saved ?? 0), Number(captured?.failed ?? 0), Number(captured?.pending ?? 0), captured?.last_saved_at ?? null),
      git_status: commits ? (commits.pushed ? "pushed" : "committed") : "none",
      commit_count: Number(commits?.commit_count ?? 0),
      goal: row.summary_goal,
    };
    return [session];
  });
}
