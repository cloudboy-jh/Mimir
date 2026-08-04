ALTER TABLE sessions ADD COLUMN title TEXT;
ALTER TABLE sessions ADD COLUMN title_source TEXT CHECK (title_source IN ('manual', 'harness', 'generated', 'derived'));
ALTER TABLE sessions ADD COLUMN title_updated_at TEXT;

ALTER TABLE exchanges ADD COLUMN title_candidate TEXT;
