CREATE TABLE transfers (
 id TEXT PRIMARY KEY,
 direction TEXT NOT NULL, status TEXT NOT NULL, reason TEXT NOT NULL,
 file TEXT NOT NULL, name TEXT NOT NULL, kind TEXT NOT NULL,
 path TEXT NOT NULL, content_type TEXT NOT NULL, format TEXT NOT NULL,
 bytes INTEGER NOT NULL, started TEXT NOT NULL, finished TEXT NOT NULL,
 url TEXT NOT NULL, link TEXT NOT NULL, artifact_id TEXT NOT NULL,
 user_id TEXT NOT NULL, user_email TEXT NOT NULL, request_id TEXT NOT NULL
);
CREATE INDEX transfers_user_started ON transfers(user_id, started);
