-- gmail domain schema (gmail-connector-plan.md P1). Greenfield; once this
-- migration has shipped it is FROZEN — every later schema change is a new,
-- higher-numbered, additive migration (see the mono-repo CLAUDE.md
-- migration-immutability note).

-- sync_state: a single-row table holding the Gmail History API sync cursor.
-- The service polls users.history.list(startHistoryId=history_id) and advances
-- this cursor atomically with the derived outbox events (the "emitted ==
-- recorded as emitted" pattern). The CHECK(id=1) pins it to exactly one row so
-- the cursor is a singleton. history_id is nullable: a fresh boot (no stored
-- cursor) seeds it from users.getProfile().historyId and emits nothing for
-- pre-existing mail (decisions §"Cursor lifecycle").
CREATE TABLE sync_state (
    id         INTEGER PRIMARY KEY CHECK (id = 1),
    history_id TEXT,
    updated_at TEXT
);
