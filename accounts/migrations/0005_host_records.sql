CREATE TABLE host_records (
    host_name TEXT PRIMARY KEY,
    record_change TEXT NOT NULL,
    record_reason TEXT NOT NULL,
    settled INTEGER NOT NULL DEFAULT 0
) WITHOUT ROWID;
