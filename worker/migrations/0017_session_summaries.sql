ALTER TABLE sessions ADD COLUMN summary_text TEXT;
ALTER TABLE sessions ADD COLUMN summary_status TEXT NOT NULL DEFAULT 'pending' CHECK (summary_status IN ('pending', 'ready', 'unavailable'));
ALTER TABLE sessions ADD COLUMN summary_source TEXT;
ALTER TABLE sessions ADD COLUMN summary_updated_at TEXT;
