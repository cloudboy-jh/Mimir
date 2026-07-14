CREATE TABLE IF NOT EXISTS session_files (
  session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
  file TEXT NOT NULL,
  PRIMARY KEY (session_id, file)
);

CREATE INDEX IF NOT EXISTS session_files_file ON session_files(file, session_id);

CREATE TABLE IF NOT EXISTS session_errors (
  session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
  signature TEXT NOT NULL,
  PRIMARY KEY (session_id, signature)
);

CREATE INDEX IF NOT EXISTS session_errors_signature ON session_errors(signature, session_id);

INSERT OR IGNORE INTO session_files(session_id, file)
SELECT sessions.id, json_each.value FROM sessions, json_each(sessions.files)
WHERE json_valid(sessions.files);

INSERT OR IGNORE INTO session_errors(session_id, signature)
SELECT sessions.id, json_each.value FROM sessions, json_each(sessions.errors)
WHERE json_valid(sessions.errors);
