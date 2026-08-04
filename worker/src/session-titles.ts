export const MAX_SESSION_TITLE_CHARS = 200;

export type SessionTitleSource = "manual" | "harness" | "generated" | "derived";

const SOURCE_RANK: Record<SessionTitleSource, number> = {
  derived: 1,
  generated: 2,
  harness: 3,
  manual: 4,
};

export function normalizeSessionTitle(value: unknown): string | null {
  if (typeof value !== "string") return null;
  let title = value.replace(/\s+/g, " ").trim();
  title = title.replace(/^#{1,6}\s+/, "").replace(/^(?:title\s*:\s*)/i, "").trim();
  if ((title.startsWith('"') && title.endsWith('"')) || (title.startsWith("'") && title.endsWith("'")) || (title.startsWith("`") && title.endsWith("`"))) {
    title = title.slice(1, -1).trim();
  }
  return title && title.length <= MAX_SESSION_TITLE_CHARS ? title : null;
}

export function extractGeneratedTitle(response: unknown): string | null {
  const record = objectRecord(response);
  const direct = normalizeSessionTitle(record.content ?? record.output_text ?? record.output);
  if (direct) return direct;

  const choices = Array.isArray(record.choices) ? record.choices : [];
  for (const choice of choices) {
    const choiceRecord = objectRecord(choice);
    const message = objectRecord(choiceRecord.message);
    const candidate = normalizeSessionTitle(message.content ?? choiceRecord.text);
    if (candidate) return candidate;
  }

  const content = Array.isArray(record.content) ? record.content : [];
  const blocks = content.map((block) => objectRecord(block).text).filter((text): text is string => typeof text === "string").join(" ");
  return normalizeSessionTitle(blocks);
}

export function sessionTitleColumns(alias: string): string {
  return `${alias}.title, ${alias}.title_source, ${alias}.title_updated_at, COALESCE(${alias}.title, substr(${alias}.intent, 1, 100), ${alias}.id) AS display_title`;
}

export function sessionTitleSearchClause(alias: string): string {
  return `(instr(lower(COALESCE(${alias}.title, '')), lower(?)) > 0 OR instr(lower(COALESCE(${alias}.intent, '')), lower(?)) > 0)`;
}

export function titleUpdateStatement(db: D1Database, sessionId: string, title: string, source: SessionTitleSource, updatedAt: string) {
  const rank = SOURCE_RANK[source];
  return db.prepare(`UPDATE sessions SET title = ?, title_source = ?, title_updated_at = ? WHERE id = ? AND (
    title IS NULL OR
    CASE title_source WHEN 'manual' THEN 4 WHEN 'harness' THEN 3 WHEN 'generated' THEN 2 WHEN 'derived' THEN 1 ELSE 0 END < ? OR
    (CASE title_source WHEN 'manual' THEN 4 WHEN 'harness' THEN 3 WHEN 'generated' THEN 2 WHEN 'derived' THEN 1 ELSE 0 END = ? AND COALESCE(title_updated_at, '') <= ?)
  )`).bind(title, source, updatedAt, sessionId, rank, rank, updatedAt);
}

export function finalizedExchangeTitleStatements(db: D1Database, sessionId: string, generatedTitle: string | null, activityAt: string) {
  const statements: D1PreparedStatement[] = [];
  if (generatedTitle) statements.push(titleUpdateStatement(db, sessionId, generatedTitle, "generated", activityAt));
  statements.push(db.prepare(`UPDATE sessions SET
    title = substr(COALESCE((SELECT intent_candidate FROM exchanges WHERE session_id = ? AND capture_status = 'saved' AND request_kind = 'primary' AND intent_candidate IS NOT NULL ORDER BY ts, id LIMIT 1), intent), 1, 100),
    title_source = 'derived',
    title_updated_at = COALESCE((SELECT ts FROM exchanges WHERE session_id = ? AND capture_status = 'saved' AND request_kind = 'primary' AND intent_candidate IS NOT NULL ORDER BY ts, id LIMIT 1), ?)
    WHERE id = ? AND COALESCE((SELECT intent_candidate FROM exchanges WHERE session_id = ? AND capture_status = 'saved' AND request_kind = 'primary' AND intent_candidate IS NOT NULL ORDER BY ts, id LIMIT 1), intent) IS NOT NULL
      AND (title IS NULL OR title_source IS NULL OR title_source = 'derived')`)
    .bind(sessionId, sessionId, activityAt, sessionId, sessionId));
  return statements;
}

export function reconcileSessionTitleStatement(db: D1Database, sessionId: string, updatedAt: string) {
  return db.prepare(`UPDATE sessions SET
    title = COALESCE((SELECT title_candidate FROM exchanges WHERE session_id = ? AND capture_status = 'saved' AND request_kind = 'title' AND title_candidate IS NOT NULL ORDER BY ts DESC, id DESC LIMIT 1), substr(intent, 1, 100)),
    title_source = CASE
      WHEN EXISTS (SELECT 1 FROM exchanges WHERE session_id = ? AND capture_status = 'saved' AND request_kind = 'title' AND title_candidate IS NOT NULL) THEN 'generated'
      WHEN intent IS NOT NULL THEN 'derived'
      ELSE NULL
    END,
    title_updated_at = COALESCE(
      (SELECT ts FROM exchanges WHERE session_id = ? AND capture_status = 'saved' AND request_kind = 'title' AND title_candidate IS NOT NULL ORDER BY ts DESC, id DESC LIMIT 1),
      (SELECT ts FROM exchanges WHERE session_id = ? AND capture_status = 'saved' AND request_kind = 'primary' AND intent_candidate IS NOT NULL ORDER BY ts, id LIMIT 1),
      CASE WHEN intent IS NOT NULL THEN ? ELSE NULL END
    )
    WHERE id = ? AND (title_source IS NULL OR title_source IN ('generated', 'derived'))`)
    .bind(sessionId, sessionId, sessionId, sessionId, updatedAt, sessionId);
}

function objectRecord(value: unknown): Record<string, unknown> {
  return value && typeof value === "object" && !Array.isArray(value) ? value as Record<string, unknown> : {};
}
