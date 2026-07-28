ALTER TABLE sessions ADD COLUMN parent_session_id TEXT REFERENCES sessions(id);

CREATE INDEX IF NOT EXISTS sessions_parent_started_at ON sessions(parent_session_id, started_at DESC);
