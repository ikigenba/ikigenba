package crm

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
)

// Search is the cross-entity, filtered, recency-ordered read verb (PLAN.md §2).
// With a Type it scopes to one entity; without, it fans out across all entities
// and merges by updated_at DESC. Substring (LIKE) match; true relevance ranking
// is a documented FTS5 escape hatch, not v1.
//
// The per-entity matching lives in each entity's Search hook; this orchestrates
// the fan-out, merge, and limit. (Fleshed out in Phase 2 — entity hooks are
// stubs until Phase 1.)
func (s *Service) Search(ctx context.Context, p SearchParams) ([]Summary, error) {
	tx, err := s.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback()

	var stores []entity
	if p.Type != "" {
		e, ok := s.byType(p.Type)
		if !ok {
			return nil, invalid("type", "unknown type "+p.Type)
		}
		stores = []entity{e}
	} else {
		for _, te := range s.entities() {
			stores = append(stores, te.e)
		}
	}

	var all []Summary
	for _, e := range stores {
		got, err := e.Search(tx, p)
		if err != nil {
			return nil, err
		}
		all = append(all, got...)
	}
	// Recency order across the merged set, then apply the limit.
	sort.SliceStable(all, func(i, j int) bool { return all[i].sortKey.After(all[j].sortKey) })
	if n := p.limit(); len(all) > n {
		all = all[:n]
	}
	return all, nil
}
