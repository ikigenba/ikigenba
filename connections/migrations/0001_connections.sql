CREATE TABLE connections (
 id TEXT PRIMARY KEY,
 user_id TEXT NOT NULL, user_email TEXT NOT NULL, provider TEXT NOT NULL,
 account_email TEXT NOT NULL, account_subject TEXT NOT NULL,
 refresh_token TEXT NOT NULL, access_token TEXT NOT NULL, access_expires TEXT NOT NULL,
 scopes TEXT NOT NULL, status TEXT NOT NULL, created TEXT NOT NULL, modified TEXT NOT NULL,
 UNIQUE(user_id, provider)
);
CREATE TABLE pending (
 state TEXT PRIMARY KEY, verifier TEXT NOT NULL,
 user_id TEXT NOT NULL, user_email TEXT NOT NULL, provider TEXT NOT NULL, expires TEXT NOT NULL
);
CREATE INDEX pending_expires ON pending(expires);
