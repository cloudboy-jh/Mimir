-- Git status: a saved commit is "pushed" once any report sees it on a remote.
ALTER TABLE session_git_artifacts ADD COLUMN pushed INTEGER NOT NULL DEFAULT 0;
ALTER TABLE session_git_artifacts ADD COLUMN pushed_at TEXT;

-- Devices know their Tailscale identity and last observed reachability.
ALTER TABLE machines ADD COLUMN tailnet_host_name TEXT;
ALTER TABLE machines ADD COLUMN tailnet_dns_name TEXT;
ALTER TABLE machines ADD COLUMN tailnet_name TEXT;
ALTER TABLE machines ADD COLUMN tailnet_ips TEXT;
ALTER TABLE machines ADD COLUMN tailnet_online INTEGER;
ALTER TABLE machines ADD COLUMN tailnet_observed_at TEXT;
CREATE INDEX machines_tailnet_dns_name ON machines(tailnet_dns_name) WHERE tailnet_dns_name IS NOT NULL;

-- Structured summaries are built when a session ends or expires. Text
-- summaries from earlier releases are rebuilt by the scheduled handler.
ALTER TABLE sessions ADD COLUMN summary_json TEXT;
UPDATE sessions SET summary_status = 'pending' WHERE summary_status <> 'pending';
CREATE INDEX sessions_summary_backlog ON sessions(last_active_at) WHERE parent_session_id IS NULL AND state = 'inactive' AND summary_status = 'pending';

-- Full-text search over the text each saved exchange adds. Existing rows are
-- indexed from R2 by the scheduled backfill in bounded batches.
ALTER TABLE exchanges ADD COLUMN search_indexed INTEGER NOT NULL DEFAULT 0;
CREATE INDEX exchanges_search_backlog ON exchanges(ts) WHERE capture_status = 'saved' AND search_indexed = 0;
CREATE VIRTUAL TABLE exchange_search USING fts5(exchange_id UNINDEXED, body, tokenize = 'porter unicode61');
