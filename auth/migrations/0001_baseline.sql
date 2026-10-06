CREATE TABLE IF NOT EXISTS users (
    id                TEXT PRIMARY KEY,
    issuer            TEXT NOT NULL,
    subject           TEXT NOT NULL,
    email             TEXT NOT NULL,
    last_google_login INTEGER NOT NULL,
    UNIQUE (issuer, subject)
);

CREATE TABLE IF NOT EXISTS sessions (
    id           TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    login_at     INTEGER NOT NULL,
    last_used_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS login_states (
    state      TEXT PRIMARY KEY,
    verifier   TEXT NOT NULL,
    return_url TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS tokens (
    id           TEXT PRIMARY KEY,
    user_id      TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name         TEXT NOT NULL,
    hash         TEXT NOT NULL UNIQUE,
    enabled      INTEGER NOT NULL CHECK (enabled IN (0, 1)),
    created_at   INTEGER NOT NULL,
    expires_at   INTEGER,
    last_used_at INTEGER
);

CREATE INDEX IF NOT EXISTS sessions_user_id_idx ON sessions(user_id);
CREATE INDEX IF NOT EXISTS tokens_user_id_idx ON tokens(user_id);
