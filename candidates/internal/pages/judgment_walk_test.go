package pages

import (
	"context"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/passes"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

func judgePageFixture(ctx context.Context, st *store.Store) error {
	after := ""
	at := time.Date(2026, 10, 5, 9, 0, 7, 0, time.UTC)
	for {
		next, _, err := st.JudgeAll(ctx, after, passes.BatchSize, at)
		if err != nil {
			return err
		}
		if next == "" {
			return nil
		}
		after = next
	}
}
