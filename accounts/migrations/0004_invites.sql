CREATE TABLE user_admins (
    uid INTEGER PRIMARY KEY
);
CREATE TABLE invites (
    id TEXT PRIMARY KEY,
    account TEXT NOT NULL,
    gid INTEGER NOT NULL,
    admin INTEGER NOT NULL,
    created INTEGER NOT NULL,
    expires INTEGER NOT NULL,
    created_by TEXT NOT NULL,
    token_hash BLOB NOT NULL UNIQUE
) WITHOUT ROWID;
CREATE INDEX invites_account_created ON invites(account, gid, created, id);
