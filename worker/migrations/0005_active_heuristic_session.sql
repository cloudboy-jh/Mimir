CREATE UNIQUE INDEX IF NOT EXISTS sessions_one_active_heuristic
ON sessions(IFNULL(repo, ''), IFNULL(harness, ''))
WHERE boundary = 'heuristic' AND state = 'active';
