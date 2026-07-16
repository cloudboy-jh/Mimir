ALTER TABLE sessions ADD COLUMN work_outcome TEXT NOT NULL DEFAULT 'unresolved' CHECK (work_outcome IN ('landed', 'discarded', 'abandoned', 'unresolved'));
ALTER TABLE sessions ADD COLUMN outcome_updated_at TEXT;
ALTER TABLE sessions ADD COLUMN outcome_reason TEXT;

UPDATE sessions
SET work_outcome = CASE outcome
      WHEN 'promoted' THEN 'landed'
      WHEN 'discarded' THEN 'discarded'
      WHEN 'abandoned' THEN 'abandoned'
      ELSE 'unresolved'
    END,
    outcome_src = 'migration',
    outcome_updated_at = COALESCE(ended_at, started_at);

CREATE INDEX IF NOT EXISTS sessions_work_outcome_started_at ON sessions(work_outcome, started_at DESC);

CREATE TABLE IF NOT EXISTS session_outcome_events (
  id TEXT PRIMARY KEY,
  session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
  outcome TEXT NOT NULL CHECK (outcome IN ('landed', 'discarded', 'abandoned', 'unresolved')),
  source TEXT NOT NULL CHECK (source IN ('agent', 'user', 'git', 'migration')),
  reason TEXT,
  evidence_json TEXT,
  created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS session_outcome_events_session_created_at ON session_outcome_events(session_id, created_at DESC);

INSERT INTO session_outcome_events(id, session_id, outcome, source, reason, evidence_json, created_at)
SELECT 'migration:' || id, id, work_outcome, 'migration', 'Backfilled from legacy sessions.outcome', json_object('legacy_outcome', outcome), outcome_updated_at
FROM sessions;

-- Keep the canonical projection and audit trail coherent if the previous
-- Worker records an outcome during the migration-first deployment window.
CREATE TRIGGER IF NOT EXISTS sessions_legacy_outcome_update
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

-- Legacy-safe defaults cover requests served by the previous Worker between
-- this migration and the new deployment. The v1 writer sets these explicitly.
ALTER TABLE exchanges ADD COLUMN capture_status TEXT NOT NULL DEFAULT 'saved' CHECK (capture_status IN ('accepted', 'saved', 'failed'));
ALTER TABLE exchanges ADD COLUMN capture_reason TEXT;
ALTER TABLE exchanges ADD COLUMN accepted_at TEXT;
ALTER TABLE exchanges ADD COLUMN saved_at TEXT;
ALTER TABLE exchanges ADD COLUMN failed_at TEXT;
ALTER TABLE exchanges ADD COLUMN failure_code TEXT;
ALTER TABLE exchanges ADD COLUMN schema_version INTEGER NOT NULL DEFAULT 0;
ALTER TABLE exchanges ADD COLUMN r2_bytes INTEGER;

UPDATE exchanges
SET capture_status = 'saved',
    capture_reason = 'legacy_capture',
    accepted_at = ts,
    saved_at = ts,
    schema_version = 0;

CREATE TRIGGER IF NOT EXISTS exchanges_legacy_capture_defaults
AFTER INSERT ON exchanges
WHEN NEW.capture_status = 'saved' AND (NEW.accepted_at IS NULL OR NEW.saved_at IS NULL)
BEGIN
  UPDATE exchanges
  SET capture_reason = COALESCE(capture_reason, 'legacy_capture'),
      accepted_at = COALESCE(accepted_at, ts),
      saved_at = COALESCE(saved_at, ts)
  WHERE id = NEW.id;
END;

CREATE INDEX IF NOT EXISTS exchanges_capture_status_accepted_at ON exchanges(capture_status, accepted_at);
CREATE INDEX IF NOT EXISTS exchanges_session_capture_status ON exchanges(session_id, capture_status);

CREATE TABLE IF NOT EXISTS exchange_files (
  exchange_id TEXT NOT NULL REFERENCES exchanges(id) ON DELETE CASCADE,
  session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
  file TEXT NOT NULL,
  PRIMARY KEY (exchange_id, file)
);

CREATE INDEX IF NOT EXISTS exchange_files_session_file ON exchange_files(session_id, file);

CREATE TABLE IF NOT EXISTS exchange_errors (
  exchange_id TEXT NOT NULL REFERENCES exchanges(id) ON DELETE CASCADE,
  session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
  signature TEXT NOT NULL,
  PRIMARY KEY (exchange_id, signature)
);

CREATE INDEX IF NOT EXISTS exchange_errors_session_signature ON exchange_errors(session_id, signature);
