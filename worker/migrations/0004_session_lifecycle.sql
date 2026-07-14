ALTER TABLE sessions ADD COLUMN state TEXT NOT NULL DEFAULT 'active' CHECK (state IN ('active', 'inactive'));
ALTER TABLE sessions ADD COLUMN last_active_at TEXT;
ALTER TABLE sessions ADD COLUMN inactive_at TEXT;
ALTER TABLE sessions ADD COLUMN harness TEXT;

UPDATE sessions
SET last_active_at = COALESCE(ended_at, started_at),
    state = 'inactive',
    inactive_at = COALESCE(ended_at, started_at);

CREATE INDEX IF NOT EXISTS sessions_state_last_active ON sessions(state, last_active_at DESC);
