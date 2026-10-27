package store_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/candidates"
	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/parks"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// This constant expression requires WriteRetries to remain untyped.
const retryCount uint8 = store.WriteRetries

func holdStoreWriter(t *testing.T, f *contractFixture) func() {
	t.Helper()
	ctx := context.Background()
	d, err := db.Open(ctx, db.Config{Path: f.path, Migrations: candidates.Migrations(), Now: func() time.Time { return f.now }})
	contractOK(t, err)
	locked := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- d.Write(ctx, func(tx *sql.Tx) error {
			if _, e := tx.ExecContext(ctx, `UPDATE candidates SET name=name`); e != nil {
				return e
			}
			close(locked)
			<-release
			return nil
		})
	}()
	<-locked
	released := false
	unlock := func() {
		if released {
			return
		}
		released = true
		close(release)
		contractOK(t, <-done)
		contractOK(t, d.Close())
	}
	t.Cleanup(unlock)
	return unlock
}

type retryAnswer struct {
	Cursor string
	Counts store.Counts
	IDs    []string
	N      int64
}

func retryCalls(f *contractFixture) []struct {
	name string
	call func(context.Context) (retryAnswer, error)
} {
	return []struct {
		name string
		call func(context.Context) (retryAnswer, error)
	}{
		{"Attach", func(ctx context.Context) (retryAnswer, error) {
			o := contractObservation("evidence", "Changed business", f.now)
			o.Digest = "changed"
			counts, ids, e := f.s.Attach(ctx, "next", false, []store.Observation{o})
			return retryAnswer{Counts: counts, IDs: ids}, e
		}},
		{"MarkGone", func(ctx context.Context) (retryAnswer, error) {
			cursor, counts, e := f.s.MarkGone(ctx, "source-z", "new", "", 500)
			return retryAnswer{Cursor: cursor, Counts: counts}, e
		}},
		{"Derive", func(ctx context.Context) (retryAnswer, error) {
			cursor, n, e := f.s.Derive(ctx, enrichers.KindLinkFacts, "", true, 500)
			return retryAnswer{Cursor: cursor, N: n}, e
		}},
		{"JudgeAll", func(ctx context.Context) (retryAnswer, error) {
			cursor, counts, e := f.s.JudgeAll(ctx, "", 500, f.now)
			return retryAnswer{Cursor: cursor, Counts: counts}, e
		}},
		{"FillFacts", func(ctx context.Context) (retryAnswer, error) {
			cursor, e := f.s.FillFacts(ctx, "", 500)
			return retryAnswer{Cursor: cursor}, e
		}},
		{"PrunePasses", func(ctx context.Context) (retryAnswer, error) {
			n, e := f.s.PrunePasses(ctx, 1, 500)
			return retryAnswer{N: n}, e
		}},

		{"ApplyParks", func(ctx context.Context) (retryAnswer, error) {
			p, e := f.s.ApplyParks(ctx, f.parkPassID, "retry-parcel-run", []string{"county"}, parks.Plan{News: []parks.New{{Cluster: parkCluster("retry-pin", "Town", "county", "", 5), Number: 1}}}, store.Counts{ParcelsRead: 1, ParksCreated: 1})
			return retryAnswer{N: p.ParksCreated}, e
		}},
	}
}

func seedRetryStore(t *testing.T) *contractFixture {
	t.Helper()
	f := contractDB(t)
	id := contractSeed(t, f)
	_, e := f.s.AddEnrichments(context.Background(), []store.Enrichment{headcountLinkResult(t, id)})
	contractOK(t, e)
	for i := 0; i < 3; i++ {
		p := contractNewPass(t, f, string(rune('a'+i)))
		_, e = f.s.FinishPass(context.Background(), p.ID, store.Counts{})
		contractOK(t, e)
	}
	p, e := f.s.AddPass(context.Background(), parkPass(store.OriginStart))
	f.parkPassID = p.ID
	contractOK(t, e)
	return f
}

// R-47BB-A47P R-4IAE-Q1VY R-5CV0-YKN2 R-5E2X-CCDR R-5IYI-VFCJ
func TestEachWriteUnitRetriesBusyTransaction(t *testing.T) {
	for index := range 7 {
		t.Run([]string{"Attach", "MarkGone", "Derive", "JudgeAll", "FillFacts", "PrunePasses", "ApplyParks"}[index], func(t *testing.T) {
			t.Parallel()
			f := seedRetryStore(t)
			control := seedRetryStore(t)
			ctx := context.Background()
			original, e := control.s.ListCandidates(ctx, store.CandidateFilter{}, "", "", 10)
			contractOK(t, e)
			call := retryCalls(f)[index]
			wantAnswer, e := retryCalls(control)[index].call(ctx)
			contractOK(t, e)
			want := contractState(t, control)
			wantParks := parkState(t, control)
			wantFacts, e := control.s.GetFacts(ctx, original[0].ID)
			contractOK(t, e)
			unlock := holdStoreWriter(t, f)
			pauses := []time.Duration{}
			f.s = store.New(f.d, store.Config{Now: func() time.Time { return f.now }, Rand: f.rnd, Sleep: func(got context.Context, d time.Duration) {
				if got != ctx {
					t.Error("different context")
				}
				pauses = append(pauses, d)
				unlock()
			}})
			gotAnswer, e := call.call(ctx)
			contractOK(t, e)
			contractEqual(t, gotAnswer, wantAnswer)
			contractEqual(t, pauses, []time.Duration{store.WriteBackoff})
			contractEqual(t, contractState(t, f), want)
			contractEqual(t, parkState(t, f), wantParks)
			facts, e := f.s.GetFacts(ctx, wantFacts.Candidate)
			contractOK(t, e)
			contractEqual(t, facts, wantFacts)
			pauses = nil
			if index == 6 {
				p, e := f.s.AddPass(ctx, parkPass(store.OriginStart))
				contractOK(t, e)
				f.parkPassID = p.ID
			}
			_, e = call.call(ctx)
			contractOK(t, e)
			if len(pauses) != 0 {
				t.Fatal(pauses)
			}
			f.d.SetFailing(true)
			for _, c := range retryCalls(f) {
				_, err := c.call(ctx)
				contractError(t, err, store.ErrUnreachable)
			}
			f.d.SetFailing(false)
			if len(pauses) != 0 {
				t.Fatal(pauses)
			}
		})
	}
	contractEqual(t, retryCount, uint8(5))
	contractEqual(t, store.WriteBackoff, time.Second)
}

// R-5FAT-Q44G R-5GIQ-3VV5 R-5HQM-HNLU
func TestAttachBusyExhaustionCancellationAndNoSleeper(t *testing.T) {
	for _, mode := range []string{"exhaust", "cancel", "nil"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			f := seedRetryStore(t)
			before := contractState(t, f)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			unlock := holdStoreWriter(t, f)
			pauses := []time.Duration{}
			var sleep func(context.Context, time.Duration)
			if mode != "nil" {
				sleep = func(got context.Context, d time.Duration) {
					contractEqual(t, got, ctx)
					pauses = append(pauses, d)
					if mode == "cancel" {
						cancel()
					}
				}
			}
			f.s = store.New(f.d, store.Config{Now: func() time.Time { return f.now }, Rand: f.rnd, Sleep: sleep})
			answer, err := retryCalls(f)[0].call(ctx)
			contractEqual(t, answer, retryAnswer{})
			contractError(t, err, store.ErrUnreachable)
			if mode == "cancel" && !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
			want := []time.Duration{}
			if mode == "cancel" {
				want = []time.Duration{store.WriteBackoff}
			}
			if mode == "exhaust" {
				for i := range store.WriteRetries {
					want = append(want, store.WriteBackoff<<i)
				}
			}
			contractEqual(t, pauses, want)
			unlock()
			contractEqual(t, contractState(t, f), before)
		})
	}
}
