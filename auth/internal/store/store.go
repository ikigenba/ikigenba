// Package store persists users, sessions, login states, and tokens.
package store

import (
	"errors"
	"io"

	"github.com/ikigenba/ikigenba/appkit/db"
)

// ErrNotFound is returned when a row lookup or mutation matches nothing.
var ErrNotFound = errors.New("store: not found")

// Store uses the caller's database handle and random source.
type Store struct {
	db   *db.DB
	rand io.Reader
}

// New builds a store over a handle owned by the caller.
func New(d *db.DB, rand io.Reader) *Store { return &Store{db: d, rand: rand} }
