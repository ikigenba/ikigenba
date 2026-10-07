ALTER TABLE tokens ADD COLUMN kind TEXT NOT NULL DEFAULT 'personal';
ALTER TABLE tokens ADD COLUMN host TEXT NOT NULL DEFAULT '';
CREATE TABLE clients (
 id TEXT PRIMARY KEY,
 name TEXT NOT NULL,
 redirect_uris TEXT NOT NULL,
 created_at INTEGER NOT NULL,
 received_token INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE auth_codes (
 code TEXT PRIMARY KEY,
 client_id TEXT NOT NULL,
 user_id TEXT NOT NULL,
 redirect_uri TEXT NOT NULL,
 challenge TEXT NOT NULL,
 resource TEXT NOT NULL,
 issued_at INTEGER NOT NULL
);
