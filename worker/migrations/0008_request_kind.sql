ALTER TABLE exchanges ADD COLUMN request_kind TEXT NOT NULL DEFAULT 'primary' CHECK (request_kind IN ('primary', 'title', 'summary', 'compaction'));
ALTER TABLE exchanges ADD COLUMN intent_candidate TEXT;

CREATE INDEX IF NOT EXISTS exchanges_session_request_kind ON exchanges(session_id, request_kind);
