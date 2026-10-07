package store_test

import (
	"context"
	"errors"
	"math"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/scripts/internal/store"
)

func TestQueueTransitionsAndPersistence(t *testing.T) {
	// R-9CY8-J4RO R-9E64-WWID R-9FE1-AO92 R-9HTU-27QG R-RNTQ-MS1Z R-982N-01SW R-8X3J-K44N R-8YBF-XVVC R-9371-GYU4
	equal(t, store.StatusQueued, "queued")
	equal(t, store.ReasonQueueAbandoned, "queue_abandoned")
	path := filepath.Join(t.TempDir(), "catalog.db")
	s := open(t, path)
	a := create(t, s, "alice", "alpha")
	b := create(t, s, "bob", "beta")
	empty, err := s.Queued(ctx)
	must(t, err)
	equal(t, empty, []store.Run{})
	q := run(a, 2)
	q.Status = store.StatusQueued
	q = add(t, s, q)
	assertReturnedRun(t, q)
	sc, err := s.Find(ctx, a.Owner, a.Name)
	must(t, err)
	assertReturnedScript(t, sc)
	equal(t, *sc.Last, q)
	other := run(b, 1)
	other.Status = store.StatusQueued
	other = add(t, s, other)
	list, err := s.Queued(ctx)
	must(t, err)
	equal(t, list, []store.Run{other, q})
	r, err := s.StartRun(ctx, q.ID)
	must(t, err)
	q.Status = store.StatusRunning
	equal(t, r, q)
	_, err = s.StartRun(ctx, q.ID)
	equal(t, errors.Is(err, store.ErrEnded), true)
	_, err = s.StartRun(ctx, "absent")
	equal(t, errors.Is(err, store.ErrNotFound), true)
	must(t, closeStore(s))
	s = open(t, path)
	got, err := s.RunByID(ctx, q.ID)
	must(t, err)
	equal(t, got, q)
	got, err = s.FindRun(ctx, a.Owner, q.ID)
	must(t, err)
	equal(t, got, q)
	runs, err := s.Runs(ctx, a.ID)
	must(t, err)
	equal(t, runs, []store.Run{q})
	e := store.Ending{Status: store.StatusFailed, Reason: store.ReasonQueueAbandoned, Finished: stamp.Add(time.Second)}
	got, err = s.FinishRun(ctx, other.ID, e)
	must(t, err)
	other.Status = e.Status
	other.Reason = e.Reason
	other.Finished = e.Finished.UTC().Truncate(time.Second)
	equal(t, got, other)
	list, err = s.Queued(ctx)
	must(t, err)
	equal(t, list, []store.Run{})
	assertReturnedRun(t, other)
	must(t, closeStore(s))
	s = open(t, path)
	got, err = s.RunByID(ctx, other.ID)
	must(t, err)
	equal(t, got, other)
}

func TestConcurrentQueueStart(t *testing.T) {
	// R-9GLX-OFZR
	s := open(t, "")
	sc := create(t, s, "alice", "alpha")
	q := run(sc, 1)
	q.Status = store.StatusQueued
	add(t, s, q)
	results := make(chan error, 16)
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() { _, err := s.StartRun(ctx, q.ID); results <- err })
	}
	wg.Wait()
	close(results)
	successes := 0
	for err := range results {
		if err == nil {
			successes++
		} else {
			equal(t, errors.Is(err, store.ErrEnded), true)
		}
	}
	equal(t, successes, 1)
}

func TestQueueRecordAndEndingRules(t *testing.T) {
	// R-90R8-PFCQ R-99AJ-DTJL R-9BQC-5D0Z
	s := open(t, "")
	sc := create(t, s, "alice", "alpha")
	for i, mutate := range []func(*store.Run){func(r *store.Run) { r.SHA = "" }, func(r *store.Run) { r.Finished = stamp }, func(r *store.Run) { r.StdoutBytes = 1 }, func(r *store.Run) { r.StderrBytes = 1 }, func(r *store.Run) { r.Truncated = true }, func(r *store.Run) { r.Reason = store.ReasonStartFailed }, func(r *store.Run) { r.ExitCode = 1 }} {
		q := run(sc, i+1)
		q.Status = store.StatusQueued
		mutate(&q)
		before := content(t, s)
		_, err := s.AddRun(ctx, q)
		catalog(t, err)
		equal(t, content(t, s), before)
	}
	q := run(sc, 20)
	q.Status = store.StatusQueued
	add(t, s, q)
	for _, e := range []store.Ending{{Status: store.StatusExited, Finished: stamp}, {Status: store.StatusTimedOut, Finished: stamp}, {Status: store.StatusKilled, Finished: stamp, StdoutBytes: 1}, {Status: store.StatusKilled, Finished: stamp, StderrBytes: 1}, {Status: store.StatusKilled, Finished: stamp, Truncated: true}, {Status: store.StatusFailed, Reason: store.ReasonGitFailed, Finished: stamp}, {Status: store.StatusFailed, Reason: store.ReasonStartFailed, Finished: stamp.Add(-time.Second)}} {
		before := content(t, s)
		_, err := s.FinishRun(ctx, q.ID, e)
		catalog(t, err)
		equal(t, content(t, s), before)
	}
	_, err := s.FinishRun(ctx, "absent", store.Ending{Status: store.StatusQueued, Finished: stamp})
	catalog(t, err)
	for i, status := range []string{store.StatusKilled, store.StatusFailed, store.StatusFailed} {
		q := run(sc, 30+i)
		q.Status = store.StatusQueued
		add(t, s, q)
		reason := ""
		if i == 1 {
			reason = store.ReasonStartFailed
		}
		if i == 2 {
			reason = store.ReasonQueueAbandoned
		}
		got, err := s.FinishRun(ctx, q.ID, store.Ending{Status: status, Reason: reason, Finished: stamp})
		must(t, err)
		equal(t, got.Status, status)
		equal(t, got.Reason, reason)
		_, err = s.StartRun(ctx, q.ID)
		equal(t, errors.Is(err, store.ErrEnded), true)
	}
	handle(s).SetFailing(true)
	_, err = s.StartRun(ctx, "absent")
	catalog(t, err)
	_, err = s.Queued(ctx)
	catalog(t, err)
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	_, err = s.StartRun(cancelled, "absent")
	equal(t, errors.Is(err, context.Canceled), true)
	handle(s).SetFailing(false)
}

func TestQueuedRetentionRanks(t *testing.T) {
	// R-9AIF-RLAA
	s := open(t, "")
	sc := create(t, s, "alice", "alpha")
	oldest := run(sc, 1)
	add(t, s, oldest)
	_, err := s.FinishRun(ctx, oldest.ID, store.Ending{Status: store.StatusExited, Finished: stamp})
	must(t, err)
	q := run(sc, 2)
	q.Status = store.StatusQueued
	add(t, s, q)
	rr, err := s.PastKeeping(ctx, stamp.Add(48*time.Hour), 1, 1)
	must(t, err)
	equal(t, len(rr), 1)
	equal(t, rr[0].ID, oldest.ID)
	rr, err = s.PastKeeping(ctx, stamp.Add(48*time.Hour), math.MaxInt64, 1)
	must(t, err)
	equal(t, rr, []store.Run{})
}
