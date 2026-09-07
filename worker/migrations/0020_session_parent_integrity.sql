-- Retain exact parent intent when lifecycle events arrive before their parent.
-- Only parent_session_id is a visible edge; unresolved intent is not a session.
ALTER TABLE sessions ADD COLUMN requested_parent_session_id TEXT;
CREATE INDEX sessions_requested_parent ON sessions(requested_parent_session_id) WHERE parent_session_id IS NULL;

CREATE TRIGGER sessions_parent_guard
BEFORE UPDATE OF parent_session_id, requested_parent_session_id, installation_id ON sessions
BEGIN
  SELECT RAISE(ABORT, 'session parent conflict')
  WHERE (OLD.parent_session_id IS NOT NULL AND NEW.parent_session_id IS NOT OLD.parent_session_id)
     OR (OLD.requested_parent_session_id IS NOT NULL AND NEW.requested_parent_session_id IS NOT OLD.requested_parent_session_id)
     OR (NEW.parent_session_id IS NOT NULL AND NEW.requested_parent_session_id IS NOT NULL AND NEW.parent_session_id <> NEW.requested_parent_session_id);

  SELECT RAISE(ABORT, 'session parent cycle')
  WHERE EXISTS (
    WITH RECURSIVE ancestors(id) AS (
      SELECT COALESCE(NEW.parent_session_id, NEW.requested_parent_session_id)
      UNION
      SELECT COALESCE(s.parent_session_id, s.requested_parent_session_id)
      FROM sessions s JOIN ancestors a ON s.id = a.id
      WHERE COALESCE(s.parent_session_id, s.requested_parent_session_id) IS NOT NULL
    )
    SELECT 1 FROM ancestors WHERE id = NEW.id
  );

  SELECT RAISE(ABORT, 'session parent installation conflict')
  WHERE (
    WITH RECURSIVE ancestors(id) AS (
      SELECT COALESCE(NEW.parent_session_id, NEW.requested_parent_session_id)
      UNION
      SELECT COALESCE(s.parent_session_id, s.requested_parent_session_id)
      FROM sessions s JOIN ancestors a ON s.id = a.id
      WHERE COALESCE(s.parent_session_id, s.requested_parent_session_id) IS NOT NULL
    ), descendants(id) AS (
      SELECT NEW.id
      UNION
      SELECT s.id FROM sessions s JOIN descendants d ON s.parent_session_id = d.id
    ), owners(installation_id) AS (
      SELECT NEW.installation_id
      UNION ALL
      SELECT s.installation_id FROM sessions s JOIN ancestors a ON a.id = s.id
      UNION ALL
      SELECT s.installation_id FROM sessions s JOIN descendants d ON d.id = s.id WHERE s.id <> NEW.id
    )
    SELECT COUNT(DISTINCT installation_id) FROM owners
  ) > 1;
END;

CREATE TRIGGER sessions_parent_resolve
AFTER UPDATE OF parent_session_id, requested_parent_session_id, installation_id ON sessions
BEGIN
  UPDATE sessions SET parent_session_id = requested_parent_session_id
  WHERE id = NEW.id AND parent_session_id IS NULL AND requested_parent_session_id IS NOT NULL
    AND EXISTS (SELECT 1 FROM sessions parent WHERE parent.id = sessions.requested_parent_session_id);

  -- A different installation arriving with a reserved ID must not acquire the
  -- waiting child or fail its own capture. Leave that intent unresolved.
  UPDATE sessions SET parent_session_id = NEW.id
  WHERE parent_session_id IS NULL AND requested_parent_session_id = NEW.id
    AND (
      WITH RECURSIVE ancestors(id) AS (
        SELECT NEW.id
        UNION
        SELECT COALESCE(s.parent_session_id, s.requested_parent_session_id)
        FROM sessions s JOIN ancestors a ON s.id = a.id
        WHERE COALESCE(s.parent_session_id, s.requested_parent_session_id) IS NOT NULL
      ), descendants(id) AS (
        SELECT sessions.id
        UNION
        SELECT s.id FROM sessions s JOIN descendants d ON s.parent_session_id = d.id
      ), owners(installation_id) AS (
        SELECT s.installation_id FROM sessions s JOIN ancestors a ON a.id = s.id
        UNION ALL
        SELECT s.installation_id FROM sessions s JOIN descendants d ON d.id = s.id
      )
      SELECT COUNT(DISTINCT installation_id) FROM owners
    ) <= 1;
END;

-- Reuse the same guard for inserts, including direct imports. Any violation
-- aborts the original statement; D1 serializes the check with the mutation.
CREATE TRIGGER sessions_parent_insert
AFTER INSERT ON sessions
BEGIN
  UPDATE sessions SET requested_parent_session_id = requested_parent_session_id WHERE id = NEW.id;
END;
