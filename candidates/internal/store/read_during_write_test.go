package store_test

import (
	"context"
	"database/sql"
	"runtime"
	"testing"

	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// R-DA8Q-FI3G
func TestContractReadsDuringQueuedWrites(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	id := contractSeed(t, f)
	pass := contractNewPass(t, f, "read-while-write")
	cohort, err := f.s.CreateCohort(ctx, store.Cohort{Name: "read", Size: 100, CreatedUserID: "owner"})
	contractOK(t, err)
	reads := []struct {
		name string
		call func() (any, error)
	}{
		{"GetPass", func() (any, error) { return f.s.GetPass(ctx, pass.ID) }},
		{"FindDelivered", func() (any, error) { return f.s.FindDelivered(ctx, pass.Delivery, pass.Trigger) }},
		{"ListPasses", func() (any, error) { return f.s.ListPasses(ctx, "", "", 100) }},
		{"RunningPasses", func() (any, error) { return f.s.RunningPasses(ctx) }},
		{"GetCandidate", func() (any, error) { return f.s.GetCandidate(ctx, id) }},
		{"Links", func() (any, error) { return f.s.Links(ctx, id) }},
		{"Estimates", func() (any, error) { return f.s.Estimates(ctx, id) }},
		{"GetAssessment", func() (any, error) { return f.s.GetAssessment(ctx, id) }},
		{"Rulings", func() (any, error) { return f.s.Rulings(ctx, id) }},
		{"ListCandidates", func() (any, error) { return f.s.ListCandidates(ctx, store.CandidateFilter{}, "", "", 100) }},
		{"ReadCohort", func() (any, error) { return f.s.ReadCohort(ctx, cohort.ID) }},
		{"GetCohort", func() (any, error) { return f.s.GetCohort(ctx, cohort.ID) }},
		{"ListCohorts", func() (any, error) { return f.s.ListCohorts(ctx, "", "", 100) }},
		{"ListCohortsReversed", func() (any, error) { return f.s.ListCohortsReversed(ctx, "", "", 100) }},
		{"Rejects", func() (any, error) { return f.s.Rejects(ctx, id) }},
		{"ZipMinutes", func() (any, error) { return f.s.ZipMinutes(ctx, "55072") }},
	}
	before := make([]any, len(reads))
	for i, read := range reads {
		value, err := read.call()
		contractOK(t, err)
		before[i] = value
	}
	opened, release := make(chan struct{}), make(chan struct{})
	held := make(chan error, 1)
	ended := make(chan struct{})
	go func() {
		defer close(ended)
		held <- f.d.Write(ctx, func(tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, "UPDATE candidates SET name=? WHERE id=?", "Uncommitted display", id); err != nil {
				return err
			}
			close(opened)
			<-release
			return nil
		})
	}()
	<-opened
	var releaseOnce bool
	unblock := func() {
		if !releaseOnce {
			close(release)
			releaseOnce = true
		}
	}
	defer unblock()
	writes := []func() error{
		func() error {
			obs := contractObservation("evidence", "Changed queued display", f.now)
			obs.Digest = "queued-digest"
			_, _, err := f.s.Attach(ctx, "queued-run", false, []store.Observation{obs})
			return err
		},
		func() error {
			_, _, err := f.s.Decide(ctx, store.Ruling{Kind: store.KindExclude, Candidate: id, Why: "queued ruling", UserID: "owner"})
			return err
		},
		func() error { _, err := f.s.Judge(ctx, []string{id}); return err },
	}
	results := make(chan error, len(writes))
	begun := make(chan struct{}, len(writes))
	for _, write := range writes {
		go func() { begun <- struct{}{}; results <- write() }()
	}
	defer func() {
		unblock()
		contractOK(t, <-held)
		for range writes {
			contractOK(t, <-results)
		}
	}()
	for range writes {
		<-begun
	}
	// Yielding makes it likely that the waiting writers have entered their calls;
	// the contract deliberately provides no hook for that point.
	for range 32 {
		runtime.Gosched()
		for i, read := range reads {
			value, err := read.call()
			contractOK(t, err)
			contractEqual(t, value, before[i])
			select {
			case <-ended:
				t.Fatalf("transaction ended before %s", read.name)
			default:
			}
			runtime.Gosched()
		}
	}
}
