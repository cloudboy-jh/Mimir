CREATE TABLE IF NOT EXISTS hermes_credentials (
  token_hash TEXT PRIMARY KEY,
  created_at TEXT NOT NULL,
  authorized_by TEXT
);
