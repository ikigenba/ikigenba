CREATE TABLE bookmarks_new (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL COLLATE NOCASE,
    url TEXT NOT NULL,
    icon BLOB,
    icon_type TEXT,
    created_by TEXT NOT NULL,
    created_at TEXT NOT NULL,
    position INTEGER,
    UNIQUE (created_by, name)
);
INSERT INTO bookmarks_new (id, name, url, icon, icon_type, created_by, created_at, position)
SELECT id, name, url, icon, icon_type, created_by, created_at,
       row_number() OVER (PARTITION BY created_by ORDER BY name COLLATE NOCASE, id)
FROM bookmarks;
DROP TABLE bookmarks;
ALTER TABLE bookmarks_new RENAME TO bookmarks;
CREATE TRIGGER bookmarks_append AFTER INSERT ON bookmarks
WHEN NEW.position IS NULL
BEGIN
    UPDATE bookmarks
    SET position = (
        WITH ordered AS MATERIALIZED (
            SELECT id, row_number() OVER (ORDER BY position, id) AS position
            FROM bookmarks
            WHERE created_by = NEW.created_by AND id <> NEW.id
        )
        SELECT position FROM ordered WHERE ordered.id = bookmarks.id
    )
    WHERE created_by = NEW.created_by AND id <> NEW.id
      AND (SELECT max(position) FROM bookmarks WHERE created_by = NEW.created_by) = 9223372036854775807;
    UPDATE bookmarks
    SET position = (SELECT coalesce(max(position), 0) + 1 FROM bookmarks WHERE created_by = NEW.created_by)
    WHERE id = NEW.id;
END;
