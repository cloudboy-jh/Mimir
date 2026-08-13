CREATE TABLE IF NOT EXISTS machines (
  installation_id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  platform TEXT NOT NULL,
  arch TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  last_seen_at TEXT,
  revoked_at TEXT
);

ALTER TABLE access_tokens ADD COLUMN installation_id TEXT REFERENCES machines(installation_id);
ALTER TABLE sessions ADD COLUMN installation_id TEXT REFERENCES machines(installation_id);
ALTER TABLE hermes_credentials ADD COLUMN installation_id TEXT REFERENCES machines(installation_id);

CREATE INDEX IF NOT EXISTS access_tokens_installation ON access_tokens(installation_id);
CREATE INDEX IF NOT EXISTS sessions_installation_started_at ON sessions(installation_id, started_at DESC);
CREATE INDEX IF NOT EXISTS hermes_credentials_installation ON hermes_credentials(installation_id);

DROP INDEX IF EXISTS sessions_one_active_heuristic;
CREATE UNIQUE INDEX sessions_one_active_heuristic
ON sessions(IFNULL(repo, ''), IFNULL(harness, ''), IFNULL(installation_id, ''))
WHERE boundary = 'heuristic' AND state = 'active';
