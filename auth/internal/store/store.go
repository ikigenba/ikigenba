package store

import (
	"database/sql"
	"errors"
	"fmt"
	"io"

	_ "modernc.org/sqlite"
)

var ErrNotFound = errors.New("store: not found")

type Store struct {
	db   *sql.DB
	rand io.Reader
}

func Open(source string, rand io.Reader) (*Store, error) {
	db, err := sql.Open("sqlite", source)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}

	// SQLite configuration is connection-local. Keeping a single connection
	// also makes :memory: sources behave as one database for the store's life.
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(`PRAGMA foreign_keys = ON`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("configure sqlite database: %w", err)
	}
	if err := createSchema(db); err != nil {
		_ = db.Close()
		return nil, err
	}

	return &Store{db: db, rand: rand}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func createSchema(db *sql.DB) error {
	const schema = `
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
`

	if _, err := db.Exec(schema); err != nil {
		return fmt.Errorf("create sqlite schema: %w", err)
	}

	return nil
}
