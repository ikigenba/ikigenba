// Package repos owns repository custody and persistence.
package repos

import "database/sql"

// Store is the domain's persistence handle. Repository operations are added by
// the v2 phases; the teardown keeps only the composition seam.
type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store { return &Store{db: db} }
