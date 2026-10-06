ALTER TABLE session_git_artifacts ADD COLUMN repository_key TEXT NOT NULL DEFAULT '';

-- Backfill common HTTPS, HTTP, SSH, Git and SCP remotes without credentials.
-- A display-only repository label is not an identity. Missing/invalid remotes
-- remain session-scoped so unrelated repositories cannot merge by SHA.
-- Materialized stages prevent SQLite from repeatedly expanding nested URL
-- expressions while preparing the backfill; retain only the next stage's input.
WITH
remote AS (
  SELECT session_id, commit_sha, trim(COALESCE(repository_url, '')) AS raw FROM session_git_artifacts
),
transport AS MATERIALIZED (
  SELECT session_id, commit_sha, CASE
    WHEN lower(raw) LIKE 'https://%' THEN substr(raw, 9)
    WHEN lower(raw) LIKE 'http://%' THEN substr(raw, 8)
    WHEN lower(raw) LIKE 'ssh://%' THEN substr(raw, 7)
    WHEN lower(raw) LIKE 'git://%' THEN substr(raw, 7)
    WHEN instr(raw, '://') = 0 AND instr(raw, '@') > 0 AND instr(raw, ':') > instr(raw, '@')
      THEN substr(raw, instr(raw, '@') + 1, instr(raw, ':') - instr(raw, '@') - 1) || '/' || substr(raw, instr(raw, ':') + 1)
    ELSE '' END AS address FROM remote
),
parts AS MATERIALIZED (
  SELECT session_id, commit_sha, substr(address, 1, instr(address, '/') - 1) AS authority,
    substr(address, instr(address, '/')) AS pathname FROM transport
),
credentials AS MATERIALIZED (
  SELECT session_id, commit_sha, CASE WHEN instr(authority, '@') > 0 THEN substr(authority, instr(authority, '@') + 1) ELSE authority END AS hostport,
    substr(pathname, 1, min(CASE WHEN instr(pathname, '?') > 0 THEN instr(pathname, '?') ELSE length(pathname) + 1 END,
      CASE WHEN instr(pathname, '#') > 0 THEN instr(pathname, '#') ELSE length(pathname) + 1 END) - 1) AS cleanpath FROM parts
),
normalized AS MATERIALIZED (
  SELECT session_id, commit_sha, lower(CASE WHEN instr(hostport, ':') > 0 THEN substr(hostport, 1, instr(hostport, ':') - 1) ELSE hostport END) AS host,
    rtrim(cleanpath, '/') AS tail FROM credentials
),
identity AS (
  SELECT session_id, commit_sha, host, CASE WHEN lower(substr(tail, -4)) = '.git' THEN substr(tail, 1, length(tail) - 4) ELSE tail END AS repo_path FROM normalized
)
UPDATE session_git_artifacts AS artifact SET repository_key = COALESCE((
  SELECT CASE WHEN instr(host, '.') > 0 AND host NOT GLOB '*[ @]*' AND length(repo_path) > 1 AND substr(repo_path, 1, 1) = '/'
    THEN 'https://' || host || CASE WHEN host IN ('github.com', 'bitbucket.org') THEN lower(repo_path) ELSE repo_path END
    ELSE 'session:' || identity.session_id END
  FROM identity WHERE identity.session_id = artifact.session_id AND identity.commit_sha = artifact.commit_sha
), 'session:' || artifact.session_id);

CREATE INDEX session_git_artifacts_repository_commit
ON session_git_artifacts(repository_key, commit_sha, committed_at, created_at);
CREATE INDEX session_git_artifacts_ref_capture_session
ON session_git_artifacts(ref, capture_status, session_id);
