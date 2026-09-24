// Package store persists users, sessions, login states, and tokens in SQLite.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	// Register the pure-Go sqlite driver opened by name "sqlite".
	_ "modernc.org/sqlite"
)

// ErrNotFound is returned when a row lookup or mutation matches nothing.
var ErrNotFound = errors.New("store: not found")

// Store is the SQLite database and the randomness used to mint ids and secrets.
type Store struct {
	db   *sql.DB
	rand io.Reader
}

// Open opens source, enables foreign keys, and creates the schema.
// An ordinary filesystem path gets any missing parent directories at mode
// 0700 before the process umask. The empty string, ":memory:", and a
// file: URI are passed to the sqlite driver unchanged.
func Open(source string, rand io.Reader) (*Store, error) {
	if ordinaryDatabasePath(source) {
		if err := os.MkdirAll(filepath.Dir(source), 0o700); err != nil {
			return nil, fmt.Errorf("create database directory: %w", err)
		}
	}

	db, err := sql.Open("sqlite", source)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}

	// SQLite configuration is connection-local. Keeping a single connection
	// also makes :memory: sources behave as one database for the store's life.
	db.SetMaxOpenConns(1)

	if _, err := db.ExecContext(context.Background(), `PRAGMA foreign_keys = ON`); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("configure sqlite database: %w", err)
	}
	if err := createSchema(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if ordinaryDatabasePath(source) {
		// SQLite can silently fall back to a read-only connection for an
		// existing database. Schema creation alone will not detect that when
		// every table already exists. A zero-row update requires write access
		// without changing any persisted rows.
		if _, err := db.ExecContext(context.Background(), `UPDATE users SET id = id WHERE 0`); err != nil {
			_ = db.Close()
			return nil, fmt.Errorf("verify sqlite database is writable: %w", err)
		}
	}

	return &Store{db: db, rand: rand}, nil
}

// Close closes the database.
func (s *Store) Close() error {
	return s.db.Close()
}

// ordinaryDatabasePath reports whether source is a filesystem path whose
// missing parents Open creates. Driver sources keep sqlite's own meaning.
func ordinaryDatabasePath(source string) bool {
	return source != "" && source != ":memory:" && !strings.HasPrefix(source, "file:")
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

	if _, err := db.ExecContext(context.Background(), schema); err != nil {
		return fmt.Errorf("create sqlite schema: %w", err)
	}

	return nil
}
