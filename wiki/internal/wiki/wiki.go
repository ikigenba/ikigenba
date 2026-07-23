// Package wiki wires the service skeleton into the shared appkit chassis.
package wiki

import (
	"context"
	"database/sql"
	"errors"
)

const (
	App   = "wiki"
	Mount = "/srv/wiki/"

	ModelID           = "gpt-5.6-luna"
	WorkerConcurrency = 1
	SearchDefault     = 8
	SearchCap         = 32
)

// VectorCacheEntry is one stored page embedding prepared for an in-memory cache.
type VectorCacheEntry struct {
	SubjectID string
	Title     string
	Vec       []float32
}

// LoadVectorCacheEntries loads stored page embeddings with their page titles.
func LoadVectorCacheEntries(ctx context.Context, db any) ([]VectorCacheEntry, error) {
	c := mustConns(db)
	embeddings, err := NewEmbeddingStore(c).LoadAll(ctx)
	if err != nil {
		return nil, err
	}
	pages := NewPageStore(c.Read)
	entries := make([]VectorCacheEntry, 0, len(embeddings))
	for _, embedding := range embeddings {
		page, err := pages.GetBySubject(ctx, embedding.SubjectID)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		entries = append(entries, VectorCacheEntry{
			SubjectID: embedding.SubjectID,
			Title:     page.Title,
			Vec:       embedding.Vec,
		})
	}
	return entries, nil
}
