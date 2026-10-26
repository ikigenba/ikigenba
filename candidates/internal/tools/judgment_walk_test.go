package tools_test

import (
	"context"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/passes"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

func walkJudgments(ctx context.Context, st *store.Store, at time.Time) error {
	after := ""
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
