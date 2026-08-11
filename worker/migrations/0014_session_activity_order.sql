UPDATE sessions
SET last_active_at = COALESCE(ended_at, started_at)
WHERE last_active_at IS NULL;

CREATE INDEX IF NOT EXISTS sessions_parent_last_active
ON sessions(parent_session_id, last_active_at DESC, id DESC);
