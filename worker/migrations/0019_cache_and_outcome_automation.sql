ALTER TABLE exchanges ADD COLUMN cache_read_tokens INTEGER NOT NULL DEFAULT 0;
ALTER TABLE exchanges ADD COLUMN cache_write_tokens INTEGER NOT NULL DEFAULT 0;
ALTER TABLE sessions ADD COLUMN cache_read_tokens INTEGER NOT NULL DEFAULT 0;
ALTER TABLE sessions ADD COLUMN cache_write_tokens INTEGER NOT NULL DEFAULT 0;

DROP TRIGGER IF EXISTS sessions_legacy_outcome_update;

CREATE TABLE session_outcome_events_next (
  id TEXT PRIMARY KEY,
  session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
  outcome TEXT NOT NULL CHECK (outcome IN ('landed', 'discarded', 'abandoned', 'unresolved')),
  source TEXT NOT NULL CHECK (source IN ('agent', 'user', 'git', 'migration', 'auto')),
  reason TEXT,
  evidence_json TEXT,
  created_at TEXT NOT NULL
);

INSERT INTO session_outcome_events_next
SELECT id, session_id, outcome, source, reason, evidence_json, created_at
FROM session_outcome_events;

DROP TABLE session_outcome_events;
ALTER TABLE session_outcome_events_next RENAME TO session_outcome_events;

CREATE INDEX session_outcome_events_session_created_at
ON session_outcome_events(session_id, created_at DESC);

CREATE TRIGGER sessions_legacy_outcome_update
AFTER UPDATE OF outcome, outcome_src ON sessions
WHEN NEW.outcome_src IN ('explicit', 'git')
  AND NEW.outcome_updated_at IS OLD.outcome_updated_at
  AND (NEW.outcome IS NOT OLD.outcome OR NEW.outcome_src IS NOT OLD.outcome_src)
BEGIN
  UPDATE sessions
  SET work_outcome = CASE NEW.outcome
        WHEN 'promoted' THEN 'landed'
        WHEN 'discarded' THEN 'discarded'
        WHEN 'abandoned' THEN 'abandoned'
        ELSE 'unresolved'
      END,
      outcome_src = CASE NEW.outcome_src WHEN 'git' THEN 'git' ELSE 'agent' END,
      outcome_updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now'),
      outcome_reason = 'Recorded by legacy Worker during deployment'
  WHERE id = NEW.id;

  INSERT INTO session_outcome_events(id, session_id, outcome, source, reason, evidence_json, created_at)
  VALUES (
    'legacy-update:' || lower(hex(randomblob(16))),
    NEW.id,
    CASE NEW.outcome
      WHEN 'promoted' THEN 'landed'
      WHEN 'discarded' THEN 'discarded'
      WHEN 'abandoned' THEN 'abandoned'
      ELSE 'unresolved'
    END,
    CASE NEW.outcome_src WHEN 'git' THEN 'git' ELSE 'agent' END,
    'Recorded by legacy Worker during deployment',
    json_object('legacy_outcome', NEW.outcome, 'legacy_source', NEW.outcome_src),
    strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
  );
END;
