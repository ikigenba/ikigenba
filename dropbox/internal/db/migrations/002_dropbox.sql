-- dropbox domain schema (PLAN.md §6). Greenfield; once this migration has
-- shipped it is FROZEN — every later schema change is a new, higher-numbered,
-- additive migration (see the mono-repo CLAUDE.md migration-immutability note).

-- sync_state: a single-row table holding the current Dropbox list_folder cursor.
-- The CHECK(id=1) pins it to exactly one row so the cursor is a singleton.
CREATE TABLE sync_state (
    id         INTEGER PRIMARY KEY CHECK (id = 1),
    cursor     TEXT,
    updated_at TEXT
);

-- files: the per-path mirror index. Drives the created-vs-modified decision,
-- rev/content_hash dedup, deletes, and mirror_bytes (SELECT SUM(size)). `path`
-- stores the current DISPLAY path; `path_lower` is the case-folded form matched
-- for Dropbox's case-insensitive semantics (store display, match folded — §2
-- case-folding). The nullable `error` column holds the last failure for a poison
-- entry the engine advanced past (§2 poison-entry bound); dropbox_health surfaces
-- the count of non-null `error` rows.
CREATE TABLE files (
    path         TEXT    PRIMARY KEY,
    rev          TEXT    NOT NULL,
    content_hash TEXT    NOT NULL,
    size         INTEGER NOT NULL,
    updated_at   TEXT    NOT NULL,
    path_lower   TEXT    NOT NULL,
    error        TEXT
);

CREATE INDEX idx_files_path_lower ON files (path_lower);
