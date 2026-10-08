package store_test

import (
	"context"
	"database/sql"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/events/internal/store"
)

func TestSweepAgeAndSubscriberBound(t *testing.T) {
	// R-E2MV-RF9R R-PNJD-XS7X R-3617-RH0H
	now := instant
	s, d, _ := openStore(t, store.Config{Now: func() time.Time { return now }})
	declare(t, s, "producer")
	declare(t, s, "reader", "*")
	declare(t, s, "paused", "*")
	declare(t, s, "gone", "*")
	for n := 1; n <= 3; n++ {
		e := emit(n)
		e.Time = instant.Add(365 * 24 * time.Hour)
		must(t, s.Deliver(ctx, e))
	}
	now = now.Add(time.Hour)
	e := emit(4)
	e.Time = instant.Add(-365 * 24 * time.Hour)
	must(t, s.Deliver(ctx, e))
	must(t, s.Advance(ctx, "reader", 3))
	must(t, s.Advance(ctx, "paused", 2))
	must(t, s.Pause(ctx, "paused", 3, "failure"))
	must(t, s.Forget(ctx, "gone"))
	beforeHead, err := s.Head(ctx)
	must(t, err)
	beforeDs, err := s.Declarations(ctx)
	must(t, err)
	beforeSubs := subs(t, s)
	old := all(t, s)
	must(t, s.Sweep(ctx, instant.Add(time.Minute)))
	records := all(t, s)
	if len(records) != 2 || records[0].Seq != 4 || records[1].Seq != 3 || !reflect.DeepEqual(records, old[:2]) {
		t.Fatal(records)
	}
	head, err := s.Head(ctx)
	must(t, err)
	ds, err := s.Declarations(ctx)
	must(t, err)
	if head != beforeHead || !reflect.DeepEqual(ds, beforeDs) || !reflect.DeepEqual(subs(t, s), beforeSubs) {
		t.Fatal("sweep changed metadata")
	}
	must(t, s.Forget(ctx, "paused"))
	must(t, s.Forget(ctx, "reader"))
	must(t, s.Sweep(ctx, now.UTC().Truncate(time.Microsecond)))
	records = all(t, s)
	if len(records) != 1 || records[0].Seq != 4 {
		t.Fatal("cutoff equality or event time used", records)
	}
	must(t, s.Sweep(ctx, now.Add(time.Microsecond)))
	if len(all(t, s)) != 0 {
		t.Fatal("did not sweep everything")
	}
	head, err = s.Head(ctx)
	must(t, err)
	if head != 4 {
		t.Fatal(head)
	}
	must(t, d.Read(ctx, func(tx *sql.Tx) error {
		var count int
		if err := tx.QueryRow("SELECT COUNT(*) FROM attrs").Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			t.Fatal("orphan attrs", count)
		}
		return nil
	}))
	must(t, s.Deliver(ctx, emit(5)))
	if all(t, s)[0].Seq != 5 {
		t.Fatal("sequence reused")
	}
}

func TestSweepBatchesReturningSubscriber(t *testing.T) {
	// R-2F3L-1GVD R-Z65Z-WC0N R-YRJ7-B34B
	var s *store.Store
	calls := []int{}
	returning := false
	s, _, _ = openStore(t, store.Config{Swept: func(n int) {
		calls = append(calls, n)
		if returning {
			returning = false
			v := all(t, s)
			if n != store.SweepBatch || len(v) != 501 || v[len(v)-1].Seq != 501 {
				t.Fatal(n, len(v))
			}
			declare(t, s, "reader", "*")
		}
	}})
	declare(t, s, "producer")
	declare(t, s, "reader", "*")
	for n := 1; n <= 1001; n++ {
		must(t, s.Deliver(ctx, emit(n)))
	}
	must(t, s.Advance(ctx, "reader", 500))
	must(t, s.Forget(ctx, "reader"))
	returning = true
	must(t, s.Sweep(ctx, instant.Add(time.Second)))
	if !reflect.DeepEqual(calls, []int{500, 0}) {
		t.Fatal(calls)
	}
	sub := one(t, s, "reader")
	if sub.Status != store.StatusOK || sub.Cursor != 500 || len(all(t, s)) != 501 {
		t.Fatal(sub)
	}
	must(t, s.Advance(ctx, "reader", 1001))
	calls = nil
	must(t, s.Sweep(ctx, instant.Add(time.Second)))
	if !reflect.DeepEqual(calls, []int{500, 1}) || len(all(t, s)) != 0 {
		t.Fatal(calls)
	}
}

func TestSweepFailureAndCancellation(t *testing.T) {
	// R-E3US-570G
	cancelled, cancel := context.WithCancel(ctx)
	defer cancel()
	var s *store.Store
	cancelInHook := false
	s, d, _ := openStore(t, store.Config{Swept: func(n int) {
		if cancelInHook && n == 500 {
			cancel()
		}
	}})
	declare(t, s, "producer")
	declare(t, s, "reader", "*")
	for n := 1; n <= 502; n++ {
		must(t, s.Deliver(ctx, emit(n)))
	}
	must(t, s.Advance(ctx, "reader", 501))
	d.SetFailing(true)
	err := s.Sweep(ctx, instant.Add(time.Second))
	if err == nil {
		t.Fatal("failing sweep succeeded")
	}
	d.SetFailing(false)
	if len(all(t, s)) != 502 {
		t.Fatal("failed sweep changed events")
	}
	cancelInHook = true
	err = s.Sweep(cancelled, instant.Add(time.Second))
	if !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	v := all(t, s)
	if len(v) != 2 || v[0].Seq != 502 || v[1].Seq != 501 {
		t.Fatal(v)
	}
	must(t, s.Sweep(ctx, instant.Add(time.Second)))
	v = all(t, s)
	if len(v) != 1 || v[0].Seq != 502 {
		t.Fatal(v)
	}
}

func TestAllMethodsFailureAndRecovery(t *testing.T) {
	// R-EH9O-CO63 R-THIC-XAJO R-E1EZ-DNJ2
	scans := 0
	s, d, _ := openStore(t, store.Config{Scanned: func(string) { scans++ }})
	declare(t, s, "producer")
	declare(t, s, "reader", "*")
	must(t, s.Deliver(ctx, emit(1)))
	must(t, s.Pause(ctx, "reader", 1, "failure"))
	before := subs(t, s)
	records := all(t, s)
	declarations, err := s.Declarations(ctx)
	must(t, err)
	catalog, err := s.Catalog(ctx, "", "")
	must(t, err)
	ch := s.Changed()
	calls := []struct {
		name string
		call func() error
	}{
		{"deliver", func() error { return s.Deliver(ctx, emit(2)) }},
		{"declare", func() error { return s.Declare(ctx, "new", store.Declaration{Accepts: []string{"*"}}) }},
		{"forget", func() error { return s.Forget(ctx, "reader") }},
		{"declarations", func() error { _, err := s.Declarations(ctx); return err }},
		{"subscribers", func() error { _, err := s.Subscribers(ctx); return err }},
		{"head", func() error { _, err := s.Head(ctx); return err }},
		{"next paused", func() error { _, _, err := s.Next(ctx, "reader"); return err }},
		{"next absent", func() error { _, _, err := s.Next(ctx, "absent"); return err }},
		{"advance", func() error { return s.Advance(ctx, "reader", 2) }},
		{"pause", func() error { return s.Pause(ctx, "reader", 1, "different") }},
		{"skip", func() error { _, _, err := s.Skip(ctx, "reader"); return err }},
		{"resume", func() error { _, err := s.Resume(ctx, "reader"); return err }},
		{"sweep", func() error { return s.Sweep(ctx, instant.Add(time.Second)) }},
		{"catalog", func() error { _, err := s.Catalog(ctx, "", ""); return err }},
		{"search", func() error { _, err := s.Search(ctx, store.Filter{}, 1, ""); return err }},
	}
	d.SetFailing(true)
	for _, call := range calls {
		t.Run(call.name, func(t *testing.T) {
			err := call.call()
			if err == nil || sentinel(err) {
				t.Fatal(err)
			}
		})
	}
	assertOpen(t, ch)
	if s.Changed() == nil || scans != 0 {
		t.Fatal("failure invoked scan")
	}
	d.SetFailing(false)
	ds, err := s.Declarations(ctx)
	must(t, err)
	cat, err := s.Catalog(ctx, "", "")
	must(t, err)
	if !reflect.DeepEqual(subs(t, s), before) || !reflect.DeepEqual(all(t, s), records) || !reflect.DeepEqual(ds, declarations) || !reflect.DeepEqual(cat, catalog) {
		t.Fatal("failure mutated state")
	}
	must(t, s.Deliver(ctx, emit(2)))
	must(t, s.Declare(ctx, "new", store.Declaration{Accepts: []string{"*"}}))
	must(t, s.Forget(ctx, "new"))
	_, err = s.Declarations(ctx)
	must(t, err)
	_, err = s.Subscribers(ctx)
	must(t, err)
	_, err = s.Head(ctx)
	must(t, err)
	_, err = s.Catalog(ctx, "", "")
	must(t, err)
	_, err = s.Search(ctx, store.Filter{}, 1, "")
	must(t, err)
	_, err = s.Resume(ctx, "reader")
	must(t, err)
	_, _, err = s.Next(ctx, "reader")
	must(t, err)
	must(t, s.Advance(ctx, "reader", 1))
	must(t, s.Pause(ctx, "reader", 2, "failure"))
	_, _, err = s.Skip(ctx, "reader")
	must(t, err)
	must(t, s.Sweep(ctx, instant.Add(time.Second)))
	if scans != 1 {
		t.Fatal(scans)
	}
}

func TestAllMethodsConcurrent(t *testing.T) {
	// R-6FEF-5LS4
	s, _, _ := openStore(t, store.Config{})
	declare(t, s, "producer")
	declare(t, s, "reader", "*")
	must(t, s.Deliver(ctx, emit(1)))
	calls := []func() error{
		func() error { return s.Deliver(ctx, emit(2)) }, func() error { return s.Declare(ctx, "dynamic", store.Declaration{Accepts: []string{"*"}}) }, func() error { return s.Forget(ctx, "dynamic") },
		func() error { _, err := s.Declarations(ctx); return err }, func() error { _, err := s.Subscribers(ctx); return err }, func() error { _, err := s.Head(ctx); return err }, func() error { _, _, err := s.Next(ctx, "reader"); return err },
		func() error { return s.Advance(ctx, "reader", 1) }, func() error { return s.Pause(ctx, "reader", 1, "failed") }, func() error { _, _, err := s.Skip(ctx, "reader"); return err }, func() error { _, err := s.Resume(ctx, "reader"); return err },
		func() error {
			if s.Changed() == nil {
				return errors.New("nil channel")
			}
			return nil
		}, func() error { return s.Sweep(ctx, instant.Add(-time.Second)) }, func() error { _, err := s.Catalog(ctx, "", ""); return err }, func() error { _, err := s.Search(ctx, store.Filter{}, 1, ""); return err },
	}
	var wg sync.WaitGroup
	errs := make(chan error, len(calls)*10)
	for _, call := range calls {
		wg.Go(func() {
			for i := 0; i < 10; i++ {
				errs <- call()
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil && !errors.Is(err, store.ErrNotPaused) {
			t.Fatal(err)
		}
	}
	if len(all(t, s)) != 2 {
		t.Fatal("concurrent duplicate ingest")
	}
}

func TestSweepSubmicrosecondCutoff(t *testing.T) {
	// R-E2MV-RF9R
	s, _, _ := openStore(t, store.Config{})
	declare(t, s, "producer")
	must(t, s.Deliver(ctx, emit(1)))
	received := all(t, s)[0].Received
	must(t, s.Sweep(ctx, received))
	if len(all(t, s)) != 1 {
		t.Fatal("event at cutoff removed")
	}
	must(t, s.Sweep(ctx, received.Add(time.Nanosecond)))
	if len(all(t, s)) != 0 {
		t.Fatal("event before cutoff retained")
	}
}

func TestSweepExtremeTimeBounds(t *testing.T) {
	// R-E2MV-RF9R
	s, _, _ := openStore(t, store.Config{})
	declare(t, s, "producer")
	must(t, s.Deliver(ctx, emit(1)))
	past := time.Date(-300000, 1, 1, 0, 0, 0, 0, time.UTC)
	future := time.Date(300000, 1, 1, 0, 0, 0, 0, time.UTC)
	must(t, s.Sweep(ctx, past))
	if len(all(t, s)) != 1 {
		t.Fatal("far-past cutoff removed event")
	}
	must(t, s.Sweep(ctx, future))
	if len(all(t, s)) != 0 {
		t.Fatal("far-future cutoff retained event")
	}
}
