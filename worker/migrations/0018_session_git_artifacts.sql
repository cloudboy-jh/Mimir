CREATE TABLE IF NOT EXISTS session_git_artifacts (
  session_id TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
  commit_sha TEXT NOT NULL CHECK (length(commit_sha) = 40 AND commit_sha NOT GLOB '*[^0-9a-f]*'),
  parent_commit_sha TEXT CHECK (parent_commit_sha IS NULL OR length(parent_commit_sha) = 40 AND parent_commit_sha NOT GLOB '*[^0-9a-f]*'),
  committed_at TEXT,
  subject TEXT,
  repository_url TEXT,
  ref TEXT,
  provenance TEXT NOT NULL,
  patch_r2_key TEXT NOT NULL,
  patch_sha256 TEXT NOT NULL CHECK (length(patch_sha256) = 64 AND patch_sha256 NOT GLOB '*[^0-9a-f]*'),
  patch_bytes INTEGER NOT NULL,
  patch_files INTEGER NOT NULL,
  patch_additions INTEGER NOT NULL,
  patch_deletions INTEGER NOT NULL,
  capture_status TEXT NOT NULL DEFAULT 'accepted' CHECK (capture_status IN ('accepted', 'saved', 'failed')),
  accepted_at TEXT NOT NULL,
  saved_at TEXT,
  failed_at TEXT,
  failure_code TEXT,
  created_at TEXT NOT NULL,
  PRIMARY KEY (session_id, commit_sha)
);

CREATE INDEX IF NOT EXISTS session_git_artifacts_session_committed_at
ON session_git_artifacts(session_id, committed_at, created_at);

CREATE INDEX IF NOT EXISTS session_git_artifacts_capture_status_accepted_at
ON session_git_artifacts(capture_status, accepted_at);
