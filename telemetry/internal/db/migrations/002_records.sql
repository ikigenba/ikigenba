CREATE TABLE records (
    id             TEXT PRIMARY KEY,
    time           TEXT NOT NULL,
    correlation_id TEXT NOT NULL DEFAULT '',
    service        TEXT NOT NULL,
    kind           TEXT NOT NULL,
    owner_email    TEXT NOT NULL DEFAULT '',
    client_id      TEXT NOT NULL DEFAULT '',
    op             TEXT NOT NULL,
    status         TEXT NOT NULL DEFAULT '',
    error          TEXT NOT NULL DEFAULT '',
    duration_ms    INTEGER,
    bytes          INTEGER,
    sha256         TEXT NOT NULL DEFAULT '',
    params         TEXT,
    detail         TEXT,
    received_at    TEXT NOT NULL
);

CREATE INDEX idx_records_time    ON records(time, id);
CREATE INDEX idx_records_chain   ON records(correlation_id, time, id);
CREATE INDEX idx_records_service ON records(service, time, id);
CREATE INDEX idx_records_kind    ON records(kind, time, id);
CREATE INDEX idx_records_owner   ON records(owner_email, time, id);
CREATE INDEX idx_records_client  ON records(client_id, time, id);
CREATE INDEX idx_records_sha     ON records(sha256, time, id);

CREATE TABLE ingest_drops (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    time        TEXT NOT NULL,
    service     TEXT NOT NULL,
    dropped     INTEGER NOT NULL CHECK (dropped > 0)
);

CREATE INDEX idx_ingest_drops_time ON ingest_drops(time);
