package store_test

import (
	"context"
	"database/sql"
	"fmt"
	"runtime"
	"testing"

	"github.com/ikigenba/ikigenba/candidates/internal/passes"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// R-Q3Z2-0ZJF R-Q6EU-SJ0T R-Q8UN-K2I7
func TestPruneRemovesOldestPageAndPreservesOthers(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	contractSeed(t, f)
	ended := []store.Pass{}
	for i := range 6 {
		p := contractNewPass(t, f, fmt.Sprintf("retained-%d", i))
		var err error
		if i < 4 {
			p, err = f.s.FinishPass(ctx, p.ID, store.Counts{})
		} else {
			p, err = f.s.FailPass(ctx, p.ID, store.ReasonScrapes)
		}
		contractOK(t, err)
		ended = append(ended, p)
	}
	running := contractNewPass(t, f, "running")
	before := contractState(t, f)
	for _, a := range []struct {
		k int64
		n int
	}{{0, 1}, {1, 0}, {-1, 1}, {1, -1}} {
		n, e := f.s.PrunePasses(ctx, a.k, a.n)
		contractError(t, e, store.ErrUnreachable)
		contractEqual(t, n, int64(0))
		contractEqual(t, contractState(t, f), before)
	}
	for i := range 2 {
		n, e := f.s.PrunePasses(ctx, 2, 1)
		contractOK(t, e)
		contractEqual(t, n, int64(1))
		_, e = f.s.GetPass(ctx, ended[i].ID)
		contractError(t, e, store.ErrNotFound)
		f.reopen()
		_, e = f.s.GetPass(ctx, ended[i].ID)
		contractError(t, e, store.ErrNotFound)
		for _, p := range ended[i+1:] {
			got, e := f.s.GetPass(ctx, p.ID)
			contractOK(t, e)
			contractEqual(t, got, p)
		}
	}
	n, e := f.s.PrunePasses(ctx, 2, 1)
	contractOK(t, e)
	contractEqual(t, n, int64(0))
	after := contractState(t, f)
	after.passes = before.passes
	contractEqual(t, after, before)
	_, e = f.s.FailPass(ctx, running.ID, store.ReasonInterrupted)
	contractOK(t, e)
	for _, p := range ended[:2] {
		q, e := f.s.AddPass(ctx, store.Pass{Kind: store.KindIngest, Origin: store.OriginRun, Trigger: p.Trigger, Delivery: p.Delivery})
		contractOK(t, e)
		_, e = f.s.FinishPass(ctx, q.ID, store.Counts{})
		contractOK(t, e)
	}
	for {
		n, e = f.s.PrunePasses(ctx, 1, 1)
		contractOK(t, e)
		if n == 0 {
			break
		}
	}
	left, e := f.s.ListPasses(ctx, "", "", 100)
	contractOK(t, e)
	contractEqual(t, len(left), 2)
}

// R-LL7Q-0WU0
func TestPruneAllocationBound(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	contractOK(t, f.d.Write(ctx, func(tx *sql.Tx) error {
		stmt, e := tx.PrepareContext(ctx, `INSERT INTO passes(id,"trigger",status,reason,started,finished,"rows","new",changed,unchanged,gone,created,linked,rescored,unranged,user_id,user_email,request_id,delivery) VALUES (?,?,'done','','2030-01-01T00:00:00Z','2030-01-01T00:00:00Z',0,0,0,0,0,0,0,0,0,'','','','')`)
		if e != nil {
			return e
		}
		defer func() { _ = stmt.Close() }()
		for i := range 20000 {
			if _, e = stmt.ExecContext(ctx, fmt.Sprintf("pass_%016x", i), fmt.Sprintf("run-%d", i)); e != nil {
				return e
			}
		}
		return nil
	}))
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	n, e := f.s.PrunePasses(ctx, 1, passes.BatchSize)
	runtime.ReadMemStats(&after)
	contractOK(t, e)
	contractEqual(t, n, int64(passes.BatchSize))
	allocated := after.TotalAlloc - before.TotalAlloc
	t.Logf("prune allocated %d", allocated)
	if allocated >= 4194304 {
		t.Fatalf("allocated %d", allocated)
	}
}
