CREATE TABLE IF NOT EXISTS harness_loads (
  token_hash TEXT NOT NULL,
  token_label TEXT NOT NULL,
  harness TEXT NOT NULL CHECK (harness IN ('opencode', 'hermes')),
  artifact_sha256 TEXT NOT NULL CHECK (length(artifact_sha256) = 64 AND artifact_sha256 NOT GLOB '*[^0-9a-f]*'),
  bundle_version TEXT,
  cli_version TEXT,
  cli_commit TEXT,
  installation_id TEXT NOT NULL DEFAULT '',
  client_loaded_at TEXT NOT NULL,
  reported_at TEXT NOT NULL,
  PRIMARY KEY (token_hash, harness, installation_id)
);
