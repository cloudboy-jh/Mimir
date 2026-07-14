CREATE TABLE IF NOT EXISTS sessions (
  id TEXT PRIMARY KEY,
  started_at TEXT NOT NULL,
  ended_at TEXT,
  boundary TEXT NOT NULL CHECK (boundary IN ('header', 'heuristic')),
  outcome TEXT NOT NULL DEFAULT 'unknown' CHECK (outcome IN ('promoted', 'discarded', 'abandoned', 'unknown')),
  outcome_src TEXT,
  repo TEXT,
  source_ref TEXT,
  model_primary TEXT,
  request_count INTEGER NOT NULL DEFAULT 0,
  tokens_in INTEGER NOT NULL DEFAULT 0,
  tokens_out INTEGER NOT NULL DEFAULT 0,
  files TEXT NOT NULL DEFAULT '[]',
  errors TEXT NOT NULL DEFAULT '[]',
  intent TEXT,
  log_refs TEXT NOT NULL DEFAULT '[]'
);

CREATE INDEX IF NOT EXISTS sessions_repo_started_at ON sessions(repo, started_at DESC);
CREATE INDEX IF NOT EXISTS sessions_outcome_started_at ON sessions(outcome, started_at DESC);

CREATE TABLE IF NOT EXISTS exchanges (
  id TEXT PRIMARY KEY,
  session_id TEXT NOT NULL REFERENCES sessions(id),
  ts TEXT NOT NULL,
  endpoint TEXT NOT NULL,
  model TEXT,
  request_excerpt TEXT NOT NULL DEFAULT '',
  response_excerpt TEXT NOT NULL DEFAULT '',
  usage_json TEXT NOT NULL DEFAULT '{}',
  latency_ms INTEGER NOT NULL,
  repo TEXT,
  harness TEXT,
  r2_key TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS exchanges_session_ts ON exchanges(session_id, ts DESC);
CREATE INDEX IF NOT EXISTS exchanges_repo_ts ON exchanges(repo, ts DESC);

CREATE TABLE IF NOT EXISTS config (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
