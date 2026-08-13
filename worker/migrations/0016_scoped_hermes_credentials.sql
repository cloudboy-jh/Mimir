CREATE TABLE hermes_credentials_scoped (
  token_hash TEXT NOT NULL,
  installation_id TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  authorized_by TEXT,
  PRIMARY KEY (token_hash, installation_id)
);

INSERT INTO hermes_credentials_scoped(token_hash, installation_id, created_at, authorized_by)
SELECT token_hash, installation_id, created_at, authorized_by
FROM hermes_credentials
WHERE installation_id IS NOT NULL;

DROP TABLE hermes_credentials;
ALTER TABLE hermes_credentials_scoped RENAME TO hermes_credentials;

CREATE INDEX hermes_credentials_installation ON hermes_credentials(installation_id);
