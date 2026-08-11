// Package wiki wires the service skeleton into the shared appkit chassis.
package wiki

import (
	"context"
)

const (
	App   = "wiki"
	Mount = "/srv/wiki/"

	ModelID            = "gpt-5.6-luna"
	WorkerConcurrency  = 1
	SearchDefault      = 8
	SearchCap          = 32
	AskBodyBudget      = 98304
	AskCacheCapDefault = 500
)

// VectorCacheEntry is one stored page embedding prepared for an in-memory cache.
type VectorCacheEntry struct {
	Scope     string
	SubjectID string
	Title     string
	Vec       []float32
}

// LoadVectorCacheEntries loads stored page embeddings with their page titles.
func LoadVectorCacheEntries(ctx context.Context, db any) ([]VectorCacheEntry, error) {
	c := mustConns(db)
	rows, err := c.Read.QueryContext(ctx, `
		SELECT e.subject_id, s.scope, p.title, e.vec
		FROM page_embeddings e
		JOIN subjects s ON s.id = e.subject_id
		JOIN pages p ON p.subject_id = e.subject_id
		ORDER BY e.subject_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var entries []VectorCacheEntry
	for rows.Next() {
		var entry VectorCacheEntry
		var blob []byte
		if err := rows.Scan(&entry.SubjectID, &entry.Scope, &entry.Title, &blob); err != nil {
			return nil, err
		}
		entry.Vec, err = decodeVec(blob)
		if err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	return entries, rows.Err()
}
