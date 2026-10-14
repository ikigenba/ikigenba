package store_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/scrapes"
	"github.com/ikigenba/ikigenba/scrapes/internal/source"
	"github.com/ikigenba/ikigenba/scrapes/internal/store"
)

var ctx = context.Background()

func open(t *testing.T, path string, rnd io.Reader) (*store.Store, *db.DB) {
	t.Helper()
	d, e := db.Open(ctx, db.Config{Path: path, Migrations: scrapes.Migrations(), Now: func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC) }})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if e := d.Close(); e != nil {
			t.Error(e)
		}
	})
	if rnd == nil {
		b := []byte{}
		for n := 1; n < 240; n++ {
			b = append(b, bytes.Repeat([]byte{byte(n)}, 8)...)
		}
		rnd = bytes.NewReader(b)
	}
	return store.New(d, store.Config{Now: func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 999, time.FixedZone("test", 3600)) }, Rand: rnd}), d
}
func fresh(t *testing.T) (*store.Store, *db.DB) {
	t.Helper()
	return open(t, filepath.Join(t.TempDir(), "db"), nil)
}
func add(t *testing.T, s *store.Store, scraper string) store.Run {
	t.Helper()
	r, e := s.AddRun(ctx, store.Run{Scraper: scraper, UserID: "caller", UserEmail: "caller@example.test", RequestID: "request"})
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func finish(t *testing.T, s *store.Store, r store.Run, rows ...source.Row) store.Run {
	t.Helper()
	if e := s.PutRows(ctx, r.ID, rows); e != nil {
		t.Fatal(e)
	}
	r, e := s.FinishRun(ctx, r.ID)
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func is(t *testing.T, e, want error) {
	t.Helper()
	if !errors.Is(e, want) {
		t.Fatalf("%v does not match %v", e, want)
	}
}

// R-ZDDP-WI4Q R-ZFTI-O1M4 R-ZH1F-1TCT R-ZI9B-FL3I R-ZKP4-74KW
func TestVocabulary(t *testing.T) {
	for _, v := range []string{"run_0123456789abcdef", "run_0000000000000000"} {
		if !store.ValidRunID(v) {
			t.Fatal(v)
		}
	}
	for _, v := range []string{"", "run_0123456789ABCDEF", "run_0123456789abcde", "run_0123456789abcdef0", "xfr_0123456789abcdef"} {
		if store.ValidRunID(v) {
			t.Fatal(v)
		}
	}
	if store.RunIDPrefix != "run_" || store.StatusRunning != "running" || store.StatusDone != "done" || store.StatusFailed != "failed" || store.ReasonSource != "source" || store.ReasonInterrupted != "interrupted" || store.ReasonStore != "store" {
		t.Fatal("vocabulary")
	}
	errs := []error{store.ErrNotFound, store.ErrBusy, store.ErrDelivered, store.ErrEnded, store.ErrUnreachable}
	for i, e := range errs {
		if e == nil {
			t.Fatal(i)
		}
		for j, w := range errs {
			if i != j && errors.Is(e, w) {
				t.Fatal(i, j)
			}
		}
	}
	if store.Unreachable == "" || strings.Contains(store.Unreachable, "\n") {
		t.Fatal("copy")
	}
}

// R-7ZYA-R23X R-01RP-JWYM R-05FE-P86P
func TestEmptyAndRunning(t *testing.T) {
	s, _ := fresh(t)
	for _, name := range []string{"", "one"} {
		for _, position := range [][2]string{{"", ""}, {"2000-01-01T00:00:00Z", ""}, {"", "missing"}, {"2999-01-01T00:00:00Z", "missing"}} {
			for _, limit := range []int{1, 2, 100} {
				rs, e := s.ListRunsAfter(ctx, name, position[0], position[1], limit)
				if e != nil || rs == nil || len(rs) != 0 {
					t.Fatal(rs, e)
				}
			}
		}
		var e error
		_, e = s.CurrentRun(ctx, name)
		is(t, e, store.ErrNotFound)
		_, e = s.RunningRun(ctx, name)
		is(t, e, store.ErrNotFound)
	}
	_, e := s.GetRun(ctx, "missing")
	is(t, e, store.ErrNotFound)
	_, e = s.GetRow(ctx, "missing", "x")
	is(t, e, store.ErrNotFound)
	_, e = s.ListRowsAfter(ctx, "missing", "", "", 10)
	is(t, e, store.ErrNotFound)
	rs, e := s.RunningRuns(ctx)
	if e != nil || rs == nil || len(rs) != 0 {
		t.Fatal(rs, e)
	}
	a := add(t, s, "a")
	b := add(t, s, "b")
	rs, e = s.RunningRuns(ctx)
	if e != nil || !reflect.DeepEqual(rs, []store.Run{a, b}) {
		t.Fatal(rs, e)
	}
	if _, e = s.FailRun(ctx, a.ID, store.ReasonInterrupted); e != nil {
		t.Fatal(e)
	}
	rs, e = s.RunningRuns(ctx)
	if e != nil || !reflect.DeepEqual(rs, []store.Run{b}) {
		t.Fatal(rs, e)
	}
}

// R-ZLX0-KWBL R-ZOCT-CFSZ R-ZPKP-Q7JO R-ZQSM-3ZAD R-0HME-IXLN
func TestAddPersistenceAndRefusals(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db")
	s, d := open(t, path, nil)
	input := store.Run{ID: "discard", Scraper: "one", Status: "discard", Reason: "discard", Rows: 99, UserID: "caller", UserEmail: "caller@example.test", RequestID: "req"}
	r, e := s.AddRun(ctx, input)
	if e != nil || !store.ValidRunID(r.ID) || r.Status != store.StatusRunning || r.Reason != "" || !r.Finished.IsZero() || r.Rows != 0 || r.New != 0 || r.Changed != 0 || r.Unchanged != 0 || r.Gone != 0 || r.UserID != input.UserID || r.UserEmail != input.UserEmail || r.RequestID != input.RequestID || r.Started.Location() != time.UTC || r.Started.Nanosecond() != 0 {
		t.Fatal(r, e)
	}
	got, e := s.GetRun(ctx, r.ID)
	if e != nil || got != r {
		t.Fatal(got, e)
	}
	_, e = s.AddRun(ctx, input)
	is(t, e, store.ErrBusy)
	cron, e := s.AddRun(ctx, store.Run{Scraper: "two", Trigger: "job", Delivery: "event"})
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.AddRun(ctx, store.Run{Scraper: "one", Trigger: "job", Delivery: "event"})
	is(t, e, store.ErrDelivered)
	if _, e = s.FailRun(ctx, cron.ID, store.ReasonSource); e != nil {
		t.Fatal(e)
	}
	_, e = s.AddRun(ctx, store.Run{Scraper: "three", Trigger: "job", Delivery: "event"})
	is(t, e, store.ErrDelivered)
	for _, bad := range []store.Run{{}, {Scraper: "bad"}, {Scraper: "bad", UserID: "u", Trigger: "job"}, {Scraper: "bad", UserID: "u", Delivery: "event"}} {
		out, e := s.AddRun(ctx, bad)
		is(t, e, store.ErrUnreachable)
		if out != (store.Run{}) {
			t.Fatal(out)
		}
	}
	if e = d.Close(); e != nil {
		t.Fatal(e)
	}
	s, _ = open(t, path, nil)
	got, e = s.GetRun(ctx, r.ID)
	if e != nil || got != r {
		t.Fatal(got, e)
	}
}

// R-ZS0I-HR12
func TestIDCandidates(t *testing.T) {
	rnd := bytes.NewReader(append(bytes.Repeat([]byte{0x5a}, 16), bytes.Repeat([]byte{0x6b}, 8)...))
	s, _ := open(t, filepath.Join(t.TempDir(), "db"), rnd)
	a := add(t, s, "a")
	b := add(t, s, "b")
	if a.ID != "run_5a5a5a5a5a5a5a5a" || b.ID != "run_6b6b6b6b6b6b6b6b" || rnd.Len() != 0 {
		t.Fatal(a, b, rnd.Len())
	}
	rnd2 := bytes.NewReader(bytes.Repeat([]byte{0x5a}, 72))
	s, _ = open(t, filepath.Join(t.TempDir(), "db"), rnd2)
	add(t, s, "a")
	_, e := s.AddRun(ctx, store.Run{Scraper: "b", UserID: "u"})
	is(t, e, store.ErrUnreachable)
	if rnd2.Len() != 0 {
		t.Fatal(rnd2.Len())
	}
	_, e = s.AddRun(ctx, store.Run{Scraper: "c", UserID: "u"})
	is(t, e, store.ErrUnreachable)
}

// R-ZT8E-VIRR R-ZWW4-0TZU R-ZN4W-YO2A R-82KK-92F2 R-07V7-GRO3 R-0933-UJES R-047I-BGG0
func TestRowsComparisonAndReads(t *testing.T) {
	s, _ := fresh(t)
	base := finish(t, s, add(t, s, "a"), source.Row{SourceID: "2", Digest: "b"}, source.Row{SourceID: "3", Digest: "x"}, source.Row{SourceID: "4", Digest: "d"})
	if base.Rows != 3 || base.New != 3 || base.Changed != 0 || base.Unchanged != 0 || base.Gone != 0 {
		t.Fatal(base)
	}
	r := add(t, s, "a")
	lat, lon := 1.2, 3.4
	row := source.Row{Scraper: "ignored", Run: "ignored", SourceID: "1", Collected: time.Date(2026, 2, 3, 4, 5, 6, 789, time.FixedZone("fixture", -3600)), Digest: "a", Name: "fixture", Names: []string{"Fixture Trucking", "Other"}, Address: source.Address{Street: "test street", City: "test city", State: "MN", Zip: "00000", County: "test county"}, Lat: &lat, Lon: &lon, People: []source.Person{{Name: "fixture person", Role: "fixture role"}}, Facts: []byte(" { \"units\": 5 } "), Raw: []byte(" { \"id\": \"1\" } ")}
	if e := s.PutRows(ctx, r.ID, []source.Row{{SourceID: "1", Digest: "old"}, row, {SourceID: "2", Digest: "b"}, {SourceID: "3", Digest: "c"}}); e != nil {
		t.Fatal(e)
	}
	rs, e := s.ListRowsAfter(ctx, r.ID, "", "", 10)
	if e != nil || rs == nil || len(rs) != 0 {
		t.Fatal(rs, e)
	}
	_, e = s.GetRow(ctx, r.ID, "1")
	is(t, e, store.ErrNotFound)
	calls := 0
	if e = s.EachRow(ctx, r.ID, func(source.Row) error { calls++; return nil }); e != nil || calls != 0 {
		t.Fatal(e, calls)
	}
	r = finish(t, s, r)
	if r.Rows != 3 || r.New != 1 || r.Changed != 1 || r.Unchanged != 1 || r.Gone != 1 || r.Status != store.StatusDone || r.Finished.Location() != time.UTC || r.Finished.Nanosecond() != 0 {
		t.Fatal(r)
	}
	row.Run = r.ID
	row.Scraper = r.Scraper
	row.Collected = row.Collected.UTC().Truncate(time.Second)
	got, e := s.GetRow(ctx, r.ID, "1")
	if e != nil || !reflect.DeepEqual(got, row) {
		t.Fatal(got, row, e)
	}
	for _, name := range []string{"trucking", "TRUCKING", "Trucking"} {
		rs, e = s.ListRowsAfter(ctx, r.ID, name, "", 1)
		if e != nil || len(rs) != 1 || rs[0].SourceID != "1" || rs[0].Raw != nil {
			t.Fatal(rs, e)
		}
	}
	rs, e = s.ListRowsAfter(ctx, r.ID, "", "1", 1)
	if e != nil || len(rs) != 1 || rs[0].SourceID != "2" {
		t.Fatal(rs, e)
	}
	expectedRows := []source.Row{row}
	for _, id := range []string{"2", "3"} {
		expected, err := s.GetRow(ctx, r.ID, id)
		if err != nil {
			t.Fatal(err)
		}
		expectedRows = append(expectedRows, expected)
	}
	deliveredRows := []source.Row{}
	if e = s.EachRow(ctx, r.ID, func(delivered source.Row) error {
		deliveredRows = append(deliveredRows, delivered)
		return nil
	}); e != nil || !reflect.DeepEqual(deliveredRows, expectedRows) {
		t.Fatal(deliveredRows, expectedRows, e)
	}
	sentinel := errors.New("callback")
	calls = 0
	e = s.EachRow(ctx, r.ID, func(source.Row) error { calls++; return sentinel })
	is(t, e, sentinel)
	if calls != 1 {
		t.Fatal(calls)
	}
	_, e = s.GetRow(ctx, r.ID, "missing")
	is(t, e, store.ErrNotFound)
	e = s.EachRow(ctx, "missing", func(source.Row) error { t.Fatal("callback"); return nil })
	is(t, e, store.ErrNotFound)
	current, e := s.CurrentRun(ctx, "a")
	if e != nil || current != r {
		t.Fatal(current, e)
	}
	active := add(t, s, "a")
	running, e := s.RunningRun(ctx, "a")
	if e != nil || running != active {
		t.Fatal(running, e)
	}
	current, e = s.CurrentRun(ctx, "a")
	if e != nil || current != r {
		t.Fatal(current, e)
	}
}

// R-ZUGB-9AIG R-82E3-ILLB R-ZZBW-SDH8 R-7V2P-7Z55 R-0DYP-DMDK
func TestFailureAndInvalidWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db")
	clock := time.Date(2026, 2, 3, 4, 5, 6, 789, time.FixedZone("failure", -3600))
	randomBytes := []byte{}
	for n := byte(1); n < 20; n++ {
		randomBytes = append(randomBytes, bytes.Repeat([]byte{n}, 8)...)
	}
	rnd := bytes.NewReader(randomBytes)
	_, d := open(t, path, rnd)
	cfg := store.Config{Now: func() time.Time { return clock }, Rand: rnd}
	s := store.New(d, cfg)
	r := add(t, s, "a")
	if e := s.PutRows(ctx, r.ID, []source.Row{{SourceID: "rejected"}, {}}); !errors.Is(e, store.ErrUnreachable) {
		t.Fatal(e)
	}
	completed := finish(t, s, r)
	if completed.Rows != 0 {
		t.Fatal(completed)
	}
	if _, err := s.GetRow(ctx, completed.ID, "rejected"); !errors.Is(err, store.ErrNotFound) {
		t.Fatal(err)
	}
	r = add(t, s, "a")
	for _, reason := range []string{store.ReasonSource, store.ReasonInterrupted, store.ReasonStore} {
		if reason != store.ReasonSource {
			r = add(t, s, "a")
		}
		if e := s.PutRows(ctx, r.ID, []source.Row{{SourceID: "x"}}); e != nil {
			t.Fatal(e)
		}
		clock = clock.Add(73*time.Second + 123*time.Nanosecond)
		failed, e := s.FailRun(ctx, r.ID, reason)
		expected := r
		expected.Status, expected.Reason, expected.Finished = store.StatusFailed, reason, clock.UTC().Truncate(time.Second)
		if e != nil || failed != expected {
			t.Fatal(failed, expected, e)
		}
		if e := d.Close(); e != nil {
			t.Fatal(e)
		}
		_, d = open(t, path, rnd)
		s = store.New(d, cfg)
		stored, e := s.GetRun(ctx, r.ID)
		if e != nil || stored != expected {
			t.Fatal(stored, expected, e)
		}
		calls := 0
		if e := s.EachRow(ctx, r.ID, func(source.Row) error { calls++; return nil }); e != nil || calls != 0 {
			t.Fatal(calls, e)
		}
		rows, e := s.ListRowsAfter(ctx, r.ID, "", "", 10)
		if e != nil || rows == nil || len(rows) != 0 {
			t.Fatal(rows, e)
		}
		_, e = s.GetRow(ctx, r.ID, "x")
		is(t, e, store.ErrNotFound)
		_, e = s.FinishRun(ctx, r.ID)
		is(t, e, store.ErrEnded)
		_, e = s.FailRun(ctx, r.ID, reason)
		is(t, e, store.ErrEnded)
		is(t, s.PutRows(ctx, r.ID, nil), store.ErrEnded)
	}
	_, e := s.FailRun(ctx, "missing", "bad")
	is(t, e, store.ErrUnreachable)
	_, e = s.FailRun(ctx, "missing", store.ReasonSource)
	is(t, e, store.ErrNotFound)
	_, e = s.FinishRun(ctx, "missing")
	is(t, e, store.ErrNotFound)
	is(t, s.PutRows(ctx, "missing", nil), store.ErrNotFound)
	is(t, s.PutRows(ctx, "missing", []source.Row{{}}), store.ErrUnreachable)
	for _, limit := range []int{-1, 0} {
		rows, e := s.ListRowsAfter(ctx, "missing", "", "", limit)
		is(t, e, store.ErrUnreachable)
		if rows != nil {
			t.Fatal(rows)
		}
	}
	for _, n := range []int64{0, -1} {
		rs, e := s.PruneRuns(ctx, n)
		is(t, e, store.ErrUnreachable)
		if rs != nil {
			t.Fatal(rs)
		}
	}
}

// R-0BIW-M2W6 R-0XAA-00BB
func TestOrderingAndPrune(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db")
	s, d := open(t, path, nil)
	old := finish(t, s, add(t, s, "a"), source.Row{SourceID: "x"})
	second := finish(t, s, add(t, s, "a"))
	third := finish(t, s, add(t, s, "a"))
	fail1 := add(t, s, "a")
	if _, e := s.FailRun(ctx, fail1.ID, store.ReasonSource); e != nil {
		t.Fatal(e)
	}
	fail2 := add(t, s, "a")
	if _, e := s.FailRun(ctx, fail2.ID, store.ReasonStore); e != nil {
		t.Fatal(e)
	}
	running := add(t, s, "a")
	other := add(t, s, "b")
	all, e := s.ListRunsAfter(ctx, "a", running.Started.UTC().Format(time.RFC3339), running.ID, 2)
	if e != nil || len(all) != 2 || all[0].ID != fail2.ID || all[1].ID != fail1.ID {
		t.Fatal(all, e)
	}
	for _, limit := range []int{-1, 0} {
		rs, e := s.ListRunsAfter(ctx, "", "", "", limit)
		is(t, e, store.ErrUnreachable)
		if rs != nil {
			t.Fatal(rs)
		}
	}
	removed, e := s.PruneRuns(ctx, 2)
	if e != nil || !reflect.DeepEqual(removed, []store.Run{old}) {
		t.Fatal(removed, e)
	}
	for _, r := range []store.Run{second, third, running, other} {
		got, e := s.GetRun(ctx, r.ID)
		if e != nil || got != r {
			t.Fatal(got, e)
		}
	}
	_, e = s.GetRun(ctx, old.ID)
	is(t, e, store.ErrNotFound)
	if e = d.Close(); e != nil {
		t.Fatal(e)
	}
	s, _ = open(t, path, nil)
	_, e = s.GetRun(ctx, old.ID)
	is(t, e, store.ErrNotFound)
}

// R-0F6L-RE49 R-7XIH-ZIMJ
func TestUnavailablePrecedence(t *testing.T) {
	s, d := fresh(t)
	r := add(t, s, "a")
	done, cancel := context.WithCancel(ctx)
	cancel()
	for _, c := range []context.Context{done, ctx} {
		if c == ctx {
			d.SetFailing(true)
		}
		checks := []func() error{func() error { _, e := s.AddRun(c, store.Run{}); return e }, func() error { return s.PutRows(c, "missing", []source.Row{{}}) }, func() error { _, e := s.FinishRun(c, "missing"); return e }, func() error { _, e := s.FailRun(c, "missing", "bad"); return e }, func() error { _, e := s.GetRun(c, "missing"); return e }, func() error { _, e := s.ListRunsAfter(c, "", "", "", 0); return e }, func() error { _, e := s.CurrentRun(c, "a"); return e }, func() error { _, e := s.RunningRun(c, "a"); return e }, func() error { _, e := s.RunningRuns(c); return e }, func() error { _, e := s.GetRow(c, "missing", "x"); return e }, func() error { _, e := s.ListRowsAfter(c, "missing", "", "", 0); return e }, func() error { return s.EachRow(c, r.ID, func(source.Row) error { t.Fatal("callback"); return nil }) }, func() error { _, e := s.PruneRuns(c, 0); return e }}
		for _, call := range checks {
			e := call()
			is(t, e, store.ErrUnreachable)
			if c == done {
				is(t, e, context.Canceled)
			}
			for _, bad := range []error{store.ErrNotFound, store.ErrBusy, store.ErrDelivered, store.ErrEnded} {
				if errors.Is(e, bad) {
					t.Fatal(e)
				}
			}
		}
	}
	d.SetFailing(false)
	got, e := s.GetRun(ctx, r.ID)
	if e != nil || got != r {
		t.Fatal(got, e)
	}
	if e = d.Close(); e != nil {
		t.Fatal(e)
	}
	_, e = s.GetRun(ctx, r.ID)
	is(t, e, store.ErrUnreachable)
}

// R-00JT-657X R-0IUA-WPCC
func TestConcurrentOperations(t *testing.T) {
	s, _ := fresh(t)
	var wg sync.WaitGroup
	results := make(chan error, 20)
	for range 20 {
		wg.Go(func() { _, e := s.AddRun(ctx, store.Run{Scraper: "a", UserID: "u"}); results <- e })
	}
	wg.Wait()
	close(results)
	success := 0
	for e := range results {
		if e == nil {
			success++
		} else {
			is(t, e, store.ErrBusy)
		}
	}
	if success != 1 {
		t.Fatal(success)
	}
	r, e := s.RunningRun(ctx, "a")
	if e != nil {
		t.Fatal(e)
	}
	ends := make(chan error, 2)
	wg.Go(func() { _, e := s.FinishRun(ctx, r.ID); ends <- e })
	wg.Go(func() { _, e := s.FailRun(ctx, r.ID, store.ReasonSource); ends <- e })
	wg.Wait()
	a, b := <-ends, <-ends
	if a != nil && b != nil || a == nil && b == nil {
		t.Fatal(a, b)
	}
	if a != nil {
		is(t, a, store.ErrEnded)
	} else {
		is(t, b, store.ErrEnded)
	}
	for i := range 20 {
		wg.Go(func() {
			r, e := s.AddRun(ctx, store.Run{Scraper: fmt.Sprint(i), Trigger: "trigger", Delivery: "same"})
			if e == nil {
				_, e = s.FinishRun(ctx, r.ID)
			}
			if e != nil && !errors.Is(e, store.ErrDelivered) {
				t.Error(e)
			}
		})
	}
	wg.Wait()
}

// R-ZT8E-VIRR R-ZWW4-0TZU R-82E3-ILLB R-0BIW-M2W6
func TestRowsAndOutcomesPersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db")
	s, d := open(t, path, nil)
	r, e := s.AddRun(ctx, store.Run{Scraper: "a", Trigger: "job", Delivery: "delivery"})
	if e != nil {
		t.Fatal(e)
	}
	raw := source.Row{SourceID: "record", Names: []string{"fixture"}, Facts: []byte(" {} "), Raw: []byte(" {\"key\":1} ")}
	done := finish(t, s, r, raw)
	partial := add(t, s, "a")
	if err := s.PutRows(ctx, partial.ID, []source.Row{{SourceID: "partial", Names: []string{"discarded"}}}); err != nil {
		t.Fatal(err)
	}
	failed, e := s.FailRun(ctx, partial.ID, store.ReasonInterrupted)
	if e != nil {
		t.Fatal(e)
	}
	got, e := s.GetRow(ctx, done.ID, "record")
	if e != nil {
		t.Fatal(e)
	}
	if e = d.Close(); e != nil {
		t.Fatal(e)
	}
	s, d = open(t, path, nil)
	reopened, e := s.GetRow(ctx, done.ID, "record")
	if e != nil || !reflect.DeepEqual(reopened, got) {
		t.Fatal(reopened, got, e)
	}
	for _, want := range []store.Run{done, failed} {
		actual, e := s.GetRun(ctx, want.ID)
		if e != nil || actual != want {
			t.Fatal(actual, want, e)
		}
	}
	rows, e := s.ListRowsAfter(ctx, failed.ID, "discarded", "", 25)
	if e != nil || rows == nil || len(rows) != 0 {
		t.Fatal(rows, e)
	}
	if _, e := s.GetRow(ctx, failed.ID, "partial"); !errors.Is(e, store.ErrNotFound) {
		t.Fatal(e)
	}
	calls := 0
	if e := s.EachRow(ctx, failed.ID, func(source.Row) error { calls++; return nil }); e != nil || calls != 0 {
		t.Fatal(calls, e)
	}
	if _, e = s.AddRun(ctx, store.Run{Scraper: "b", Trigger: "job", Delivery: "delivery"}); !errors.Is(e, store.ErrDelivered) {
		t.Fatal(e)
	}
	finish(t, s, add(t, s, "a"))
	removed, e := s.PruneRuns(ctx, 1)
	if e != nil || len(removed) != 1 || removed[0] != done {
		t.Fatal(removed, e)
	}
	if e = d.Close(); e != nil {
		t.Fatal(e)
	}
	s, _ = open(t, path, bytes.NewReader(bytes.Repeat([]byte{0xee}, 8)))
	if _, e = s.GetRow(ctx, done.ID, "record"); !errors.Is(e, store.ErrNotFound) {
		t.Fatal(e)
	}
	_, e = s.AddRun(ctx, store.Run{Scraper: "b", Trigger: "job", Delivery: "delivery"})
	if e != nil {
		t.Fatal(e)
	}
}

// R-ZN4W-YO2A R-047I-BGG0 R-ZWW4-0TZU R-82KK-92F2
func TestStartOrderAndBaseline(t *testing.T) {
	s, _ := fresh(t)
	a := finish(t, s, add(t, s, "a"), source.Row{SourceID: "x", Digest: "first"})
	b := finish(t, s, add(t, s, "a"), source.Row{SourceID: "x", Digest: "second", Names: []string{"ACME TRUCK", "TRUCK ING LLC"}})
	c := finish(t, s, add(t, s, "a"), source.Row{SourceID: "x", Digest: "second"})
	if c.Unchanged != 1 || c.New != 0 || c.Changed != 0 {
		t.Fatal(c)
	}
	rows, e := s.ListRowsAfter(ctx, b.ID, "TRUCKING", "", 10)
	if e != nil || len(rows) != 0 {
		t.Fatal(rows, e)
	}
	rows, e = s.ListRowsAfter(ctx, b.ID, "", "z", 1)
	if e != nil || rows == nil || len(rows) != 0 {
		t.Fatal(rows, e)
	}
	all, e := s.ListRunsAfter(ctx, "", "", "", 99)
	if e != nil || !reflect.DeepEqual(all, []store.Run{c, b, a}) {
		t.Fatal(all, e)
	}
	clock := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	dPath := filepath.Join(t.TempDir(), "db")
	d, e := db.Open(ctx, db.Config{Path: dPath, Migrations: scrapes.Migrations(), Now: func() time.Time { return clock }})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if e := d.Close(); e != nil {
			t.Error(e)
		}
	})
	custom := store.New(d, store.Config{Now: func() time.Time { return clock }, Rand: bytes.NewReader(append(bytes.Repeat([]byte{0x11}, 8), bytes.Repeat([]byte{0x22}, 8)...))})
	older := finish(t, custom, add(t, custom, "a"))
	clock = clock.Add(-time.Hour)
	later := finish(t, custom, add(t, custom, "a"))
	all, e = custom.ListRunsAfter(ctx, "a", "", "", 10)
	if e != nil || !reflect.DeepEqual(all, []store.Run{older, later}) {
		t.Fatal(all, e)
	}
	current, e := custom.CurrentRun(ctx, "a")
	if e != nil || current != older {
		t.Fatal(current, e)
	}
}

// R-ZQSM-3ZAD R-ZS0I-HR12 R-7XIH-ZIMJ
func TestRandomPrecedenceAndReadCounts(t *testing.T) {
	rnd := bytes.NewReader(bytes.Repeat([]byte{0x5a}, 16))
	s, _ := open(t, filepath.Join(t.TempDir(), "db"), rnd)
	_, e := s.AddRun(ctx, store.Run{Scraper: "bad"})
	is(t, e, store.ErrUnreachable)
	if rnd.Len() != 16 {
		t.Fatal(rnd.Len())
	}
	add(t, s, "a")
	if rnd.Len() != 8 {
		t.Fatal(rnd.Len())
	}
	_, e = s.AddRun(ctx, store.Run{Scraper: "a", UserID: "u"})
	is(t, e, store.ErrBusy)
	if rnd.Len() != 8 {
		t.Fatal(rnd.Len())
	}
	out, e := s.AddRun(ctx, store.Run{Scraper: "b", UserID: "u"})
	is(t, e, store.ErrUnreachable)
	if out != (store.Run{}) || rnd.Len() != 0 {
		t.Fatal(out, rnd.Len())
	}
}

// R-0XAA-00BB
func TestRunsAfterWindows(t *testing.T) {
	_, d := fresh(t)
	clock := time.Date(2026, 2, 3, 4, 5, 6, 987, time.FixedZone("fixture", -3600))
	rnd := bytes.NewReader(append(append(append(bytes.Repeat([]byte{0x40}, 8), bytes.Repeat([]byte{0x20}, 8)...), bytes.Repeat([]byte{0x30}, 8)...), bytes.Repeat([]byte{0x10}, 8)...))
	s := store.New(d, store.Config{Now: func() time.Time { return clock }, Rand: rnd})
	old := finish(t, s, add(t, s, "a"))
	clock = clock.Add(time.Second)
	tieA := finish(t, s, add(t, s, "a"))
	tieB, err := s.FailRun(ctx, add(t, s, "b").ID, store.ReasonSource)
	if err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(time.Second)
	newest := add(t, s, "b")
	for _, tc := range []struct {
		scraper string
		want    []store.Run
	}{{"", []store.Run{newest, tieB, tieA, old}}, {"a", []store.Run{tieA, old}}, {"b", []store.Run{newest, tieB}}, {"missing", []store.Run{}}} {
		for _, limit := range []int{1, 2, 3, 4, 5} {
			var got []store.Run
			started, id := "2000-01-01T00:00:00Z", ""
			for page := 0; page <= len(tc.want); page++ {
				window, err := s.ListRunsAfter(ctx, tc.scraper, started, id, limit)
				if err != nil || window == nil {
					t.Fatal(window, err)
				}
				remaining := len(tc.want) - len(got)
				count := min(remaining, limit)
				if len(window) != count || !reflect.DeepEqual(window, tc.want[len(got):len(got)+count]) {
					t.Fatalf("scraper %q, limit %d, page %d: got %#v", tc.scraper, limit, page, window)
				}
				got = append(got, window...)
				if len(window) == 0 {
					break
				}
				last := window[len(window)-1]
				started, id = last.Started.UTC().Format(time.RFC3339), last.ID
			}
			if len(got) != len(tc.want) {
				t.Fatal(got, tc.want)
			}
		}
	}
	for _, tc := range []struct {
		started, id string
		want        []store.Run
	}{{tieA.Started.Format(time.RFC3339), "run_2525252525252525", []store.Run{tieA, old}}, {"1999-01-01T00:00:00Z", "missing", []store.Run{}}, {"2999-01-01T00:00:00Z", "missing", []store.Run{newest, tieB, tieA, old}}} {
		got, err := s.ListRunsAfter(ctx, "", tc.started, tc.id, 10)
		if err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Fatal(got, tc.want, err)
		}
	}
	for _, limit := range []int{-1, 0} {
		for _, scraper := range []string{"", "a", "missing"} {
			got, err := s.ListRunsAfter(ctx, scraper, "", "", limit)
			if got != nil || !errors.Is(err, store.ErrUnreachable) {
				t.Fatal(got, err)
			}
		}
	}
}

// R-7Q73-OW6D
func TestStoreMethodSignatures(t *testing.T) {
	s, _ := fresh(t)
	var methods interface {
		AddRun(context.Context, store.Run) (store.Run, error)
		PutRows(context.Context, string, []source.Row) error
		FinishRun(context.Context, string) (store.Run, error)
		FailRun(context.Context, string, string) (store.Run, error)
		GetRun(context.Context, string) (store.Run, error)
		ListRunsAfter(context.Context, string, string, string, int) ([]store.Run, error)
		CurrentRun(context.Context, string) (store.Run, error)
		RunningRun(context.Context, string) (store.Run, error)
		RunningRuns(context.Context) ([]store.Run, error)
		ListRowsAfter(context.Context, string, string, string, int) ([]source.Row, error)
		GetRow(context.Context, string, string) (source.Row, error)
		EachRow(context.Context, string, func(source.Row) error) error
		PruneRuns(context.Context, int64) ([]store.Run, error)
	} = s
	r, err := methods.AddRun(ctx, store.Run{Scraper: "a", UserID: "caller"})
	if err != nil {
		t.Fatal(err)
	}
	if err := methods.PutRows(ctx, r.ID, []source.Row{{SourceID: "row"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := methods.RunningRun(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	if _, err := methods.RunningRuns(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := methods.FinishRun(ctx, r.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := methods.GetRun(ctx, r.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := methods.CurrentRun(ctx, "a"); err != nil {
		t.Fatal(err)
	}
	if _, err := methods.ListRunsAfter(ctx, "a", "", "", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := methods.ListRowsAfter(ctx, r.ID, "", "", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := methods.GetRow(ctx, r.ID, "row"); err != nil {
		t.Fatal(err)
	}
	if err := methods.EachRow(ctx, r.ID, func(source.Row) error { return nil }); err != nil {
		t.Fatal(err)
	}
	r, err = methods.AddRun(ctx, store.Run{Scraper: "a", UserID: "caller"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := methods.FailRun(ctx, r.ID, store.ReasonSource); err != nil {
		t.Fatal(err)
	}
	if _, err := methods.PruneRuns(ctx, 1); err != nil {
		t.Fatal(err)
	}
}

// R-82KK-92F2
func TestRowsAfterNamesAndWindows(t *testing.T) {
	s, _ := fresh(t)
	rows := []source.Row{
		{SourceID: "a", Names: []string{"ACME TRUCKING INC", "ACME TRUCKING"}},
		{SourceID: "b", Names: []string{"ACME TRUCK", "TRUCK ING LLC"}},
		{SourceID: "c", Names: []string{"CAFÉ CARRIERS"}},
		{SourceID: "d", Names: []string{"A_B FREIGHT"}},
		{SourceID: "e", Names: []string{"AXB FREIGHT"}},
		{SourceID: "f", Names: []string{"50% OFF MOVERS"}},
		{SourceID: "g", Names: []string{"500 OFF MOVERS"}},
		{SourceID: "h", Names: []string{`BACK\SLASH LLC`}},
		{SourceID: "i", Names: []string{"FIRST", "SECOND"}},
		{SourceID: "j", Names: []string{"O'BRIEN CARRIERS"}},
	}
	r := finish(t, s, add(t, s, "names"), rows...)
	for _, tc := range []struct {
		name string
		ids  []string
	}{
		{"TRUCKING", []string{"a"}}, {"trucking", []string{"a"}}, {"Trucking", []string{"a"}},
		{"É", []string{"c"}}, {"A_B", []string{"d"}}, {"50%", []string{"f"}}, {`\`, []string{"h"}},
		{"FIRST\nSECOND", []string{}}, {strings.Repeat("x", 60000), []string{}},
		{"O'BRIEN", []string{"j"}}, {"' OR 1=1 --", []string{}},
	} {
		got, err := s.ListRowsAfter(ctx, r.ID, tc.name, "", 25)
		if err != nil || got == nil || len(got) != len(tc.ids) {
			t.Fatal(tc.name, got, err)
		}
		for i, row := range got {
			expected, err := s.GetRow(ctx, r.ID, tc.ids[i])
			if err != nil {
				t.Fatal(err)
			}
			expected.Raw = nil
			if !reflect.DeepEqual(row, expected) {
				t.Fatal(row, expected)
			}
		}
	}
	rows = nil
	for i := 29; i >= 0; i-- {
		rows = append(rows, source.Row{SourceID: fmt.Sprintf("%02d", i), Names: []string{"matched", "MATCHED"}, Raw: []byte(`{"fixture":true}`)})
	}
	rows = append(rows, source.Row{SourceID: "00a", Names: []string{"other"}})
	r = finish(t, s, add(t, s, "windows"), rows...)
	after := ""
	index := 0
	for _, count := range []int{25, 5, 0} {
		got, err := s.ListRowsAfter(ctx, r.ID, "matched", after, 25)
		if err != nil || got == nil || len(got) != count {
			t.Fatal(got, err)
		}
		for _, row := range got {
			if row.SourceID <= after || row.SourceID != fmt.Sprintf("%02d", index) || row.Raw != nil {
				t.Fatal(row, after)
			}
			expected, err := s.GetRow(ctx, r.ID, row.SourceID)
			if err != nil {
				t.Fatal(err)
			}
			expected.Raw = nil
			if !reflect.DeepEqual(row, expected) {
				t.Fatal(row, expected)
			}
			after = row.SourceID
			index++
		}
	}
	got, err := s.ListRowsAfter(ctx, r.ID, "", "00", 2)
	if err != nil || len(got) != 2 || got[0].SourceID != "00a" || got[1].SourceID != "01" {
		t.Fatal(got, err)
	}
}

// R-9IJW-5V8T
func TestReplacedNamesPersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db")
	s, d := open(t, path, nil)
	r := add(t, s, "a")
	if err := s.PutRows(ctx, r.ID, []source.Row{{SourceID: "same", Names: []string{"ACME TRUCKING INC"}}, {SourceID: "same", Names: []string{"TEMP HAULING"}}}); err != nil {
		t.Fatal(err)
	}
	if err := s.PutRows(ctx, r.ID, []source.Row{{SourceID: "same", Names: []string{"BETA HAULING"}}}); err != nil {
		t.Fatal(err)
	}
	finish(t, s, r)
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	s, _ = open(t, path, nil)
	for _, tc := range []struct {
		name  string
		count int
	}{{"hauling", 1}, {"trucking", 0}, {"temp", 0}} {
		got, err := s.ListRowsAfter(ctx, r.ID, tc.name, "", 25)
		if err != nil || got == nil || len(got) != tc.count {
			t.Fatal(got, err)
		}
		if tc.count == 1 && got[0].SourceID != "same" {
			t.Fatal(got)
		}
	}
}

// R-7V2P-7Z55
func TestRowsAfterRunState(t *testing.T) {
	s, _ := fresh(t)
	running := add(t, s, "a")
	failed, err := s.FailRun(ctx, add(t, s, "b").ID, store.ReasonSource)
	if err != nil {
		t.Fatal(err)
	}
	for _, run := range []string{"missing", running.ID, failed.ID} {
		for _, limit := range []int{-1, 0} {
			got, err := s.ListRowsAfter(ctx, run, "anything", "position", limit)
			if got != nil || !errors.Is(err, store.ErrUnreachable) {
				t.Fatal(got, err)
			}
		}
		got, err := s.ListRowsAfter(ctx, run, "anything", "position", 1)
		if run == "missing" {
			if got != nil || !errors.Is(err, store.ErrNotFound) {
				t.Fatal(got, err)
			}
		} else if err != nil || got == nil || len(got) != 0 {
			t.Fatal(got, err)
		}
	}
}

func TestRowsNamesBackfillOnUpgrade(t *testing.T) {
	migrations := fstest.MapFS{}
	for _, name := range []string{"0001_runs.sql", "0002_rows.sql", "0003_list_indexes.sql"} {
		data, err := fs.ReadFile(scrapes.Migrations(), name)
		if err != nil {
			t.Fatal(err)
		}
		migrations[name] = &fstest.MapFile{Data: data}
	}
	path := filepath.Join(t.TempDir(), "db")
	clock := func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC) }
	d, err := db.Open(ctx, db.Config{Path: path, Migrations: migrations, Now: clock})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := d.Close(); err != nil {
			t.Error(err)
		}
	})
	s := store.New(d, store.Config{Now: clock, Rand: bytes.NewReader(bytes.Repeat([]byte{1}, 8))})
	r := add(t, s, "legacy")
	err = d.Write(ctx, func(tx *sql.Tx) error {
		for _, fixture := range []struct{ id, names string }{{"a", `["ACME TRUCKING","ACME TRUCKING INC"]`}, {"b", `[]`}, {"c", `["CAFÉ CARRIERS"]`}} {
			_, err := tx.ExecContext(ctx, `INSERT INTO "rows" ("run",source_id,scraper,collected,digest,name,names,names_lower,street,city,state,zip,county,people) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, r.ID, fixture.id, r.Scraper, clock().Format(time.RFC3339), "fixture", "fixture", fixture.names, "unused", "", "", "", "", "", "[]")
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	finish(t, s, r)
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	s, _ = open(t, path, nil)
	for _, tc := range []struct {
		name string
		ids  []string
	}{{"trucking", []string{"a"}}, {"caf", []string{"c"}}, {"É", []string{}}, {"", []string{"a", "b", "c"}}} {
		got, err := s.ListRowsAfter(ctx, r.ID, tc.name, "", 25)
		if err != nil || got == nil || len(got) != len(tc.ids) {
			t.Fatal(got, err)
		}
		for i, row := range got {
			if row.SourceID != tc.ids[i] {
				t.Fatal(got, tc.ids)
			}
		}
	}
}

// R-FO8F-PVZS
func TestBeforeMethodSignatures(t *testing.T) {
	s, _ := fresh(t)
	var methods interface {
		ListRunsBefore(context.Context, string, string, string, int) ([]store.Run, error)
		ListRowsBefore(context.Context, string, string, string, int) ([]source.Row, error)
	} = s
	r := finish(t, s, add(t, s, "a"), source.Row{SourceID: "row"})
	if _, err := methods.ListRunsBefore(ctx, "a", "", "", 1); err != nil {
		t.Fatal(err)
	}
	if _, err := methods.ListRowsBefore(ctx, r.ID, "", "", 1); err != nil {
		t.Fatal(err)
	}
}

// R-FPGC-3NQH
func TestRunsBeforeWindows(t *testing.T) {
	s, d := fresh(t)
	for _, scraper := range []string{"", "a", "missing"} {
		got, err := s.ListRunsBefore(ctx, scraper, "2000-01-01T00:00:00Z", "missing", 2)
		if err != nil || got == nil || len(got) != 0 {
			t.Fatal(got, err)
		}
	}
	clock := time.Date(2026, 2, 3, 4, 5, 6, 987, time.FixedZone("fixture", -3600))
	rnd := bytes.NewReader(append(append(append(bytes.Repeat([]byte{0x40}, 8), bytes.Repeat([]byte{0x20}, 8)...), bytes.Repeat([]byte{0x30}, 8)...), bytes.Repeat([]byte{0x10}, 8)...))
	s = store.New(d, store.Config{Now: func() time.Time { return clock }, Rand: rnd})
	old := finish(t, s, add(t, s, "a"))
	clock = clock.Add(time.Second)
	tieA := finish(t, s, add(t, s, "a"))
	tieB, err := s.FailRun(ctx, add(t, s, "b").ID, store.ReasonSource)
	if err != nil {
		t.Fatal(err)
	}
	clock = clock.Add(time.Second)
	newest := add(t, s, "b")
	for _, tc := range []struct {
		scraper string
		want    []store.Run
	}{{"", []store.Run{old, tieA, tieB, newest}}, {"a", []store.Run{old, tieA}}, {"b", []store.Run{tieB, newest}}, {"missing", []store.Run{}}} {
		for _, limit := range []int{1, 2, 3, 4, 5} {
			index := 0
			started, id := "2000-01-01T00:00:00Z", ""
			for page := 0; page <= len(tc.want); page++ {
				window, err := s.ListRunsBefore(ctx, tc.scraper, started, id, limit)
				count := min(len(tc.want)-index, limit)
				if err != nil || window == nil || !reflect.DeepEqual(window, tc.want[index:index+count]) {
					t.Fatalf("scraper %q limit %d page %d: %#v, %v", tc.scraper, limit, page, window, err)
				}
				index += len(window)
				if len(window) == 0 {
					break
				}
				last := window[len(window)-1]
				started, id = last.Started.UTC().Format(time.RFC3339), last.ID
			}
			if index != len(tc.want) {
				t.Fatal(index, tc.want)
			}
		}
	}
	for _, tc := range []struct {
		scraper, started, id string
		want                 []store.Run
	}{
		{"", old.Started.Format(time.RFC3339), old.ID, []store.Run{tieA, tieB, newest}},
		{"", newest.Started.Format(time.RFC3339), newest.ID, []store.Run{}},
		{"", tieA.Started.Format(time.RFC3339), "run_2525252525252525", []store.Run{tieB, newest}},
		{"a", tieA.Started.Format(time.RFC3339), "run_1515151515151515", []store.Run{tieA}},
		{"b", old.Started.Format(time.RFC3339), old.ID, []store.Run{tieB, newest}},
		{"", "2999-01-01T00:00:00Z", "missing", []store.Run{}},
		{"", "1999-01-01T00:00:00Z", "missing", []store.Run{old, tieA, tieB, newest}},
	} {
		got, err := s.ListRunsBefore(ctx, tc.scraper, tc.started, tc.id, 5)
		if err != nil || !reflect.DeepEqual(got, tc.want) {
			t.Fatal(got, tc.want, err)
		}
	}
	for _, limit := range []int{-1, 0} {
		for _, scraper := range []string{"", "a", "missing"} {
			got, err := s.ListRunsBefore(ctx, scraper, "", "", limit)
			if got != nil || !errors.Is(err, store.ErrUnreachable) {
				t.Fatal(got, err)
			}
		}
	}
}

// R-FQO8-HFH6
func TestRowsBeforeNamesAndWindows(t *testing.T) {
	s, _ := fresh(t)
	rows := []source.Row{
		{SourceID: "a", Names: []string{"ACME TRUCKING INC", "ACME TRUCKING"}},
		{SourceID: "b", Names: []string{"ACME TRUCK", "TRUCK ING LLC"}},
		{SourceID: "c", Names: []string{"CAFÉ CARRIERS"}},
		{SourceID: "d", Names: []string{"A_B FREIGHT"}},
		{SourceID: "e", Names: []string{"AXB FREIGHT"}},
		{SourceID: "f", Names: []string{"50% OFF MOVERS"}},
		{SourceID: "g", Names: []string{"500 OFF MOVERS"}},
		{SourceID: "h", Names: []string{`BACK\SLASH LLC`}},
		{SourceID: "i", Names: []string{"FIRST", "SECOND"}},
		{SourceID: "j", Names: []string{"O'BRIEN CARRIERS"}},
	}
	for i := range rows {
		rows[i].Raw = []byte(`{"fixture":true}`)
	}
	r := finish(t, s, add(t, s, "names"), rows...)
	for _, tc := range []struct {
		name string
		ids  []string
	}{
		{"TRUCKING", []string{"a"}}, {"trucking", []string{"a"}}, {"Trucking", []string{"a"}},
		{"É", []string{"c"}}, {"A_B", []string{"d"}}, {"50%", []string{"f"}}, {`\`, []string{"h"}},
		{"FIRST\nSECOND", []string{}}, {strings.Repeat("x", 60000), []string{}},
		{"O'BRIEN", []string{"j"}}, {"' OR 1=1 --", []string{}}, {"", []string{"j", "i", "h", "g", "f", "e", "d", "c", "b", "a"}},
	} {
		for _, before := range []string{"", "z", "j"} {
			want := []string{}
			for _, id := range tc.ids {
				if before == "" || id < before {
					want = append(want, id)
				}
			}
			got, err := s.ListRowsBefore(ctx, r.ID, tc.name, before, 25)
			if err != nil || got == nil || len(got) != len(want) {
				t.Fatal(tc.name, before, got, err)
			}
			for i, row := range got {
				expected, err := s.GetRow(ctx, r.ID, want[i])
				if err != nil {
					t.Fatal(err)
				}
				expected.Raw = nil
				if !reflect.DeepEqual(row, expected) {
					t.Fatal(row, expected)
				}
			}
		}
	}
	rows = nil
	for i := 0; i < 30; i++ {
		rows = append(rows, source.Row{SourceID: fmt.Sprintf("%02d", i), Names: []string{"matched", "MATCHED"}, Raw: []byte(`{"fixture":true}`)})
	}
	rows = append(rows, source.Row{SourceID: "00a", Names: []string{"other"}})
	r = finish(t, s, add(t, s, "windows"), rows...)
	before := ""
	index := 29
	for _, count := range []int{25, 5, 0} {
		got, err := s.ListRowsBefore(ctx, r.ID, "matched", before, 25)
		if err != nil || got == nil || len(got) != count {
			t.Fatal(got, err)
		}
		for _, row := range got {
			if row.SourceID != fmt.Sprintf("%02d", index) || row.Raw != nil {
				t.Fatal(row, before)
			}
			expected, err := s.GetRow(ctx, r.ID, row.SourceID)
			if err != nil {
				t.Fatal(err)
			}
			expected.Raw = nil
			if !reflect.DeepEqual(row, expected) {
				t.Fatal(row, expected)
			}
			before = row.SourceID
			index--
		}
	}
	for _, tc := range []struct {
		name, before string
		ids          []string
	}{{"", "01a", []string{"01", "00a"}}, {"matched", "01a", []string{"01", "00"}}} {
		got, err := s.ListRowsBefore(ctx, r.ID, tc.name, tc.before, 2)
		if err != nil || len(got) != len(tc.ids) {
			t.Fatal(got, err)
		}
		for i, row := range got {
			if row.SourceID != tc.ids[i] {
				t.Fatal(got, tc.ids)
			}
		}
	}
}

// R-FRW4-V77V
func TestRowsBeforeRunState(t *testing.T) {
	s, _ := fresh(t)
	running := add(t, s, "a")
	if err := s.PutRows(ctx, running.ID, []source.Row{{SourceID: "partial", Names: []string{"anything"}}}); err != nil {
		t.Fatal(err)
	}
	failed, err := s.FailRun(ctx, add(t, s, "b").ID, store.ReasonSource)
	if err != nil {
		t.Fatal(err)
	}
	done := finish(t, s, add(t, s, "c"))
	for _, run := range []string{"missing", running.ID, failed.ID, done.ID} {
		for _, limit := range []int{-1, 0} {
			got, err := s.ListRowsBefore(ctx, run, "anything", "position", limit)
			if got != nil || !errors.Is(err, store.ErrUnreachable) {
				t.Fatal(got, err)
			}
		}
	}
	for _, run := range []string{"missing", running.ID, failed.ID} {
		for _, name := range []string{"", "anything"} {
			for _, before := range []string{"", "z", "position"} {
				got, err := s.ListRowsBefore(ctx, run, name, before, 1)
				if run == "missing" {
					if got != nil || !errors.Is(err, store.ErrNotFound) {
						t.Fatal(got, err)
					}
				} else if err != nil || got == nil || len(got) != 0 {
					t.Fatal(got, err)
				}
			}
		}
	}
}
