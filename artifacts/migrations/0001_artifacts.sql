CREATE TABLE artifacts (
 id TEXT PRIMARY KEY,
 path TEXT NOT NULL UNIQUE,
 blob TEXT NOT NULL,
 bytes INTEGER NOT NULL,
 content_type TEXT NOT NULL,
 sha256 TEXT NOT NULL,
 owner_id TEXT NOT NULL,
 owner_email TEXT NOT NULL,
 created TEXT NOT NULL,
 modified TEXT NOT NULL,
 expires TEXT
);
CREATE INDEX artifacts_expires ON artifacts(expires);
