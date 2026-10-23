CREATE TABLE bookmarks (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL COLLATE NOCASE UNIQUE,
    url TEXT NOT NULL,
    icon BLOB,
    icon_type TEXT,
    created_by TEXT NOT NULL,
    created_at TEXT NOT NULL
);
