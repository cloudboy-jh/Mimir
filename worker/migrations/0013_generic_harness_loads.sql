CREATE TABLE harness_loads_next (
  token_hash TEXT NOT NULL,
  token_label TEXT NOT NULL,
  harness TEXT NOT NULL CHECK (
    length(harness) BETWEEN 1 AND 64
    AND substr(harness, 1, 1) GLOB '[a-z0-9]'
    AND harness NOT GLOB '*[^a-z0-9._-]*'
  ),
  artifact_sha256 TEXT NOT NULL CHECK (length(artifact_sha256) = 64 AND artifact_sha256 NOT GLOB '*[^0-9a-f]*'),
  bundle_version TEXT,
  cli_version TEXT,
  cli_commit TEXT,
  installation_id TEXT NOT NULL DEFAULT '',
  client_loaded_at TEXT NOT NULL,
  reported_at TEXT NOT NULL,
  PRIMARY KEY (token_hash, harness, installation_id)
);

INSERT INTO harness_loads_next SELECT * FROM harness_loads;
DROP TABLE harness_loads;
ALTER TABLE harness_loads_next RENAME TO harness_loads;
