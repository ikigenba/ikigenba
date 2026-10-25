package store_test

import (
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
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
	return store.New(d, store.Config{Now: func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 999, time.FixedZone("test", 3600)) }, Rand: rnd, Runs: filepath.Join(filepath.Dir(path), "runs")}), d
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
	c, e := s.Build(ctx, r.ID, nil)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = c.Discard() })
	if e = c.Add(ctx, rows); e != nil {
		t.Fatal(e)
	}
	if e = c.Seal(ctx); e != nil {
		t.Fatal(e)
	}
	counts, e := c.Load(ctx, nil)
	if e != nil {
		t.Fatal(e)
	}
	r, e = s.FinishRun(ctx, r.ID, counts)
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

// R-MZJI-329N R-N0RE-GU0C R-N6UW-DOPT R-ZFTI-O1M4 R-ZH1F-1TCT R-LIAV-UW31 R-ZKP4-74KW
func TestVocabulary(t *testing.T) {
	shape := store.Run{"id", "scraper", "status", "reason", time.Unix(1, 0), time.Unix(2, 0), 3, 4, 5, 6, 7, "user", "email", "trigger", "request", "delivery"}
	if shape.ID != "id" || shape.Scraper != "scraper" || shape.Status != "status" || shape.Reason != "reason" || !shape.Started.Equal(time.Unix(1, 0)) || !shape.Finished.Equal(time.Unix(2, 0)) || shape.Rows != 3 || shape.New != 4 || shape.Changed != 5 || shape.Unchanged != 6 || shape.Gone != 7 || shape.UserID != "user" || shape.UserEmail != "email" || shape.Trigger != "trigger" || shape.RequestID != "request" || shape.Delivery != "delivery" {
		t.Fatal(shape)
	}

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
	errs := []error{store.ErrMissing, store.ErrNotFound, store.ErrBusy, store.ErrDelivered, store.ErrEnded, store.ErrUnreachable, store.ErrLocked}
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
	if store.DefaultChunkBytes != 16777216 || store.DefaultLoadRows != 5000 {
		t.Fatal("defaults")
	}
	if store.Missing == "" || strings.Contains(store.Missing, "\n") {
		t.Fatal("missing copy")
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

// R-R3AI-N5IH R-RJ57-M65I R-RLL0-DPMW R-QSBF-77U8 R-RE9M-336Q R-R4IF-0X96 R-QNFT-O4VG R-047I-BGG0
func TestRowsComparisonAndReads(t *testing.T) {
	s, _ := fresh(t)
	base := finish(t, s, add(t, s, "a"), source.Row{SourceID: "2", Digest: "b"}, source.Row{SourceID: "3", Digest: "x"}, source.Row{SourceID: "4", Digest: "d"})
	if base.Rows != 3 || base.New != 3 || base.Changed != 0 || base.Unchanged != 0 || base.Gone != 0 {
		t.Fatal(base)
	}
	r := add(t, s, "a")
	lat, lon := 1.2, 3.4
	row := source.Row{Scraper: "ignored", Run: "ignored", SourceID: "1", Scraped: time.Date(2026, 2, 3, 4, 5, 6, 789, time.FixedZone("fixture", -3600)), Digest: "a", Name: "fixture", Names: []string{"Fixture Trucking", "Other"}, Address: source.Address{Street: "test street", City: "test city", State: "MN", Zip: "00000", County: "test county"}, Lat: &lat, Lon: &lon, People: []source.Person{{Name: "fixture person", Role: "fixture role"}}, Facts: []byte(" { \"units\": 5 } "), Raw: []byte(" { \"id\": \"1\" } ")}
	col, e := s.Build(ctx, r.ID, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = col.Discard() }()
	if e := col.Add(ctx, []source.Row{{SourceID: "1", Digest: "old"}, row, {SourceID: "2", Digest: "b"}, {SourceID: "3", Digest: "c"}}); e != nil {
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
	if e = col.Seal(ctx); e != nil {
		t.Fatal(e)
	}
	counts, e := col.Load(ctx, nil)
	if e != nil {
		t.Fatal(e)
	}
	r, e = s.FinishRun(ctx, r.ID, counts)
	if e != nil {
		t.Fatal(e)
	}
	if r.Rows != 3 || r.New != 1 || r.Changed != 1 || r.Unchanged != 1 || r.Gone != 1 || r.Status != store.StatusDone || r.Finished.Location() != time.UTC || r.Finished.Nanosecond() != 0 {
		t.Fatal(r)
	}
	row.Run = r.ID
	row.Scraper = r.Scraper
	row.Scraped = row.Scraped.UTC().Truncate(time.Second)
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

// R-OD11-7L2V R-0XAA-00BB
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

// R-O04H-K6IH R-0IUA-WPCC
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
	col, e := s.Build(ctx, r.ID, nil)
	if e != nil {
		t.Fatal(e)
	}
	if e = col.Seal(ctx); e != nil {
		t.Fatal(e)
	}
	counts, e := col.Load(ctx, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = col.Discard() }()
	ends := make(chan error, 2)
	wg.Go(func() { _, e := s.FinishRun(ctx, r.ID, counts); ends <- e })
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
				_, e = s.FailRun(ctx, r.ID, store.ReasonSource)
			}
			if e != nil && !errors.Is(e, store.ErrDelivered) {
				t.Error(e)
			}
		})
	}
	wg.Wait()
}

// R-R3AI-N5IH R-RJ57-M65I R-RLL0-DPMW R-NYWL-6ERS R-OD11-7L2V
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
	col, e := s.Build(ctx, partial.ID, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = col.Discard() }()
	if err := col.Add(ctx, []source.Row{{SourceID: "partial", Names: []string{"discarded"}}}); err != nil {
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

// R-QSBF-77U8 R-047I-BGG0 R-RJ57-M65I R-RLL0-DPMW R-RE9M-336Q
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
	custom := store.New(d, store.Config{Now: func() time.Time { return clock }, Rand: bytes.NewReader(append(bytes.Repeat([]byte{0x11}, 8), bytes.Repeat([]byte{0x22}, 8)...)), Runs: filepath.Join(t.TempDir(), "runs")})
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

// R-ZQSM-3ZAD R-ZS0I-HR12 R-RSWE-OC32
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
	s := store.New(d, store.Config{Now: func() time.Time { return clock }, Rand: rnd, Runs: filepath.Join(t.TempDir(), "runs")})
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

// R-RE9M-336Q
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

// R-R3AI-N5IH
func TestReplacedNamesPersist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "db")
	s, d := open(t, path, nil)
	r := add(t, s, "a")
	col, e := s.Build(ctx, r.ID, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = col.Discard() }()
	if err := col.Add(ctx, []source.Row{{SourceID: "same", Names: []string{"ACME TRUCKING INC"}}, {SourceID: "same", Names: []string{"TEMP HAULING"}}}); err != nil {
		t.Fatal(err)
	}
	if err := col.Add(ctx, []source.Row{{SourceID: "same", Names: []string{"BETA HAULING"}}}); err != nil {
		t.Fatal(err)
	}
	if e = col.Seal(ctx); e != nil {
		t.Fatal(e)
	}
	counts, e := col.Load(ctx, nil)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.FinishRun(ctx, r.ID, counts); e != nil {
		t.Fatal(e)
	}
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

// R-FW2U-Q6N5 R-FR79-73OD
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
	s = store.New(d, store.Config{Now: func() time.Time { return clock }, Rand: rnd, Runs: filepath.Join(t.TempDir(), "runs")})
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

// R-RGPE-UMO4
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
	col, e := s.Build(ctx, running.ID, nil)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = col.Discard() }()
	if err := col.Add(ctx, []source.Row{{SourceID: "partial", Names: []string{"anything"}}}); err != nil {
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

func configured(t *testing.T, chunk int64, load int) (*store.Store, *db.DB, string) {
	t.Helper()
	dir := t.TempDir()
	_, d := open(t, filepath.Join(dir, "catalog"), nil)
	rnd := []byte{}
	for n := 1; n < 240; n++ {
		rnd = append(rnd, bytes.Repeat([]byte{byte(n)}, 8)...)
	}
	runs := filepath.Join(dir, "nested", "runs")
	cfg := store.Config{func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 999, time.UTC) }, bytes.NewReader(rnd), runs, chunk, load}
	return store.New(d, cfg), d, runs
}
func build(t *testing.T, s *store.Store, r store.Run, step store.Step) *store.Build {
	t.Helper()
	c, e := s.Build(ctx, r.ID, step)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = c.Discard() })
	return c
}
func load(t *testing.T, c *store.Build, rows []source.Row) store.Counts {
	t.Helper()
	if e := c.Add(ctx, rows); e != nil {
		t.Fatal(e)
	}
	if e := c.Seal(ctx); e != nil {
		t.Fatal(e)
	}
	n, e := c.Load(ctx, nil)
	if e != nil {
		t.Fatal(e)
	}
	return n
}
func snapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	entries, e := os.ReadDir(dir)
	if os.IsNotExist(e) {
		return out
	}
	if e != nil {
		t.Fatal(e)
	}
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		info, e := os.Lstat(path)
		if e != nil {
			t.Fatal(e)
		}
		switch {
		case info.Mode().IsRegular():
			b, e := os.ReadFile(filepath.Clean(path))
			if e != nil {
				t.Fatal(e)
			}
			out[entry.Name()] = string(b)
		case info.Mode()&os.ModeSymlink != 0:
			target, e := os.Readlink(path)
			if e != nil {
				t.Fatal(e)
			}
			out[entry.Name()] = "link:" + target
		default:
			out[entry.Name()] = "directory"
		}
	}
	return out
}

// R-MZJI-329N R-QUR7-YRBM R-N377-8DHQ R-QJS4-ITND R-QL00-WLE2 R-QR3I-TG3J R-QTJB-KZKX
func TestBuildAPI(t *testing.T) {
	s, _, runs := configured(t, 1, 2)
	var methods interface {
		AddRun(context.Context, store.Run) (store.Run, error)
		Build(context.Context, string, store.Step) (*store.Build, error)
		FinishRun(context.Context, string, store.Counts) (store.Run, error)
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
		TidyFiles(context.Context) error
	} = s
	c, e := methods.Build(ctx, "missing", nil)
	if c != nil {
		t.Fatal(c)
	}
	is(t, e, store.ErrNotFound)
	r, e := methods.AddRun(ctx, store.Run{Scraper: "a", UserID: "u"})
	if e != nil {
		t.Fatal(e)
	}
	before, _ := methods.GetRun(ctx, r.ID)
	step := store.Step(func(context.Context, int64, time.Duration) error { return nil })
	c, e = methods.Build(ctx, r.ID, step)
	if e != nil || c == nil {
		t.Fatal(c, e)
	}
	after, _ := methods.GetRun(ctx, r.ID)
	if before != after {
		t.Fatal(before, after)
	}
	if _, e = os.Lstat(runs); !os.IsNotExist(e) {
		t.Fatal(e)
	}
	var builder interface {
		Add(context.Context, []source.Row) error
		Seal(context.Context) error
		Load(context.Context, store.Step) (store.Counts, error)
		Discard() error
	} = c
	if e = builder.Add(ctx, []source.Row{{SourceID: "row"}}); e != nil {
		t.Fatal(e)
	}
	if e = builder.Seal(ctx); e != nil {
		t.Fatal(e)
	}
	counts, e := builder.Load(ctx, step)
	if e != nil || counts != (store.Counts{1, 1, 0, 0, 0}) {
		t.Fatal(counts, e)
	}
	if _, e = methods.FinishRun(ctx, r.ID, counts); e != nil {
		t.Fatal(e)
	}
	if e = builder.Discard(); e != nil {
		t.Fatal(e)
	}
	c, e = methods.Build(ctx, r.ID, nil)
	if c != nil {
		t.Fatal(c)
	}
	is(t, e, store.ErrEnded)
	for _, call := range []func() error{
		func() error { _, e := methods.GetRun(ctx, r.ID); return e }, func() error { _, e := methods.CurrentRun(ctx, "a"); return e }, func() error { _, e := methods.ListRunsAfter(ctx, "a", "", "", 1); return e }, func() error { _, e := methods.ListRowsAfter(ctx, r.ID, "", "", 1); return e }, func() error { _, e := methods.GetRow(ctx, r.ID, "row"); return e }, func() error { return methods.EachRow(ctx, r.ID, func(source.Row) error { return nil }) }, func() error { _, e := methods.PruneRuns(ctx, 1); return e }, func() error { return methods.TidyFiles(ctx) },
	} {
		if e = call(); e != nil {
			t.Fatal(e)
		}
	}
	r = add(t, s, "a")
	if _, e = methods.RunningRun(ctx, "a"); e != nil {
		t.Fatal(e)
	}
	if _, e = methods.RunningRuns(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e = methods.FailRun(ctx, r.ID, store.ReasonSource); e != nil {
		t.Fatal(e)
	}
}

func orderedJSONTokens(t *testing.T, data []byte) string {
	t.Helper()
	d := json.NewDecoder(bytes.NewReader(data))
	var tokens []any
	for {
		token, e := d.Token()
		if errors.Is(e, io.EOF) {
			break
		}
		if e != nil {
			t.Fatal(e)
		}
		if delimiter, ok := token.(json.Delim); ok {
			token = struct{ Delimiter string }{string(delimiter)}
		}
		tokens = append(tokens, token)
	}
	b, e := json.Marshal(tokens)
	if e != nil {
		t.Fatal(e)
	}
	return string(b)
}

// R-PSYC-3VC3
func TestGzipJSONLineChunks(t *testing.T) {
	for _, chunk := range []int64{1, store.DefaultChunkBytes} {
		t.Run(fmt.Sprint(chunk), func(t *testing.T) {
			s, _, runs := configured(t, chunk, 2)
			r := add(t, s, "a")
			lat, lon := 44.25, -93.5
			rows := []source.Row{
				{Scraper: "ignored", Run: "ignored", SourceID: "2", Scraped: time.Date(2026, 2, 3, 4, 5, 6, 789, time.FixedZone("source", -6*3600)), Digest: "digest", Name: "<name>", Names: []string{"one", "two"}, Address: source.Address{Street: "street", City: "city", State: "state", Zip: "zip", County: "county"}, Lat: &lat, Lon: &lon, People: []source.Person{{Name: "person", Role: "role"}}, Facts: []byte(" { \"html\": \"<&>\" } "), Raw: []byte(" \n{ \"v\": 1 } \t")},
				{SourceID: "1", Digest: "first", Facts: []byte(`{"v":1}`), Raw: []byte(`{"v":1}`)},
				{SourceID: "1", Digest: "second", Facts: []byte(" { \"v\": 2 } ")},
				{SourceID: "1", Digest: "third", Names: []string{}, People: []source.Person{}, Raw: []byte{}},
			}
			expected := make([]string, len(rows))
			for i, row := range rows {
				row.Run, row.Scraper = r.ID, r.Scraper
				row.Scraped = row.Scraped.UTC().Truncate(time.Second)
				var e error
				row.Facts, e = json.Marshal(string(row.Facts))
				if e != nil {
					t.Fatal(e)
				}
				if row.Raw != nil {
					row.Raw, e = json.Marshal(string(row.Raw))
					if e != nil {
						t.Fatal(e)
					}
				}
				encoded, e := json.Marshal(row)
				if e != nil {
					t.Fatal(e)
				}
				expected[i] = orderedJSONTokens(t, encoded)
			}
			seenFiles := map[string]bool{}
			c := build(t, s, r, func(_ context.Context, n int64, _ time.Duration) error {
				newFiles := 0
				for name, data := range snapshot(t, runs) {
					if seenFiles[name] {
						continue
					}
					seenFiles[name] = true
					newFiles++
					if n != int64(len(data)) {
						t.Fatalf("step bytes %d, gzip file bytes %d", n, len(data))
					}
				}
				if newFiles != 1 {
					t.Fatal(newFiles)
				}
				return nil
			})
			if e := c.Add(ctx, rows[:2]); e != nil {
				t.Fatal(e)
			}
			if e := c.Add(ctx, rows[2:]); e != nil {
				t.Fatal(e)
			}
			if e := c.Seal(ctx); e != nil {
				t.Fatal(e)
			}
			files := snapshot(t, runs)
			wantFiles := 1
			if chunk == 1 {
				wantFiles = len(rows)
			}
			if len(files) != wantFiles || len(seenFiles) != wantFiles {
				t.Fatal(len(files), len(seenFiles), wantFiles)
			}
			remaining := map[string]int{}
			for _, line := range expected {
				remaining[line]++
			}
			for name, data := range files {
				if !strings.HasPrefix(name, r.ID+".") {
					t.Fatal(name)
				}
				z, e := gzip.NewReader(strings.NewReader(data))
				if e != nil {
					t.Fatal(e)
				}
				plain, e := io.ReadAll(z)
				if e != nil {
					t.Fatal(e)
				}
				if e = z.Close(); e != nil {
					t.Fatal(e)
				}
				if len(plain) == 0 || plain[len(plain)-1] != '\n' {
					t.Fatalf("missing line terminator: %q", plain)
				}
				lines := bytes.Split(plain[:len(plain)-1], []byte{'\n'})
				if chunk == 1 && len(lines) != 1 {
					t.Fatal(len(lines))
				}
				objects := make([]string, len(lines))
				for i, line := range lines {
					objects[i] = orderedJSONTokens(t, line)
				}
				if chunk != 1 {
					want := []string{expected[1], expected[2], expected[3], expected[0]}
					if !reflect.DeepEqual(objects, want) {
						t.Fatalf("chunk objects %q, want %q", objects, want)
					}
				}
				for _, object := range objects {
					if remaining[object] == 0 {
						t.Fatalf("unexpected row object tokens: %s", object)
					}
					remaining[object]--
				}
			}
			for line, n := range remaining {
				if n != 0 {
					t.Fatalf("missing %d copies of %s", n, line)
				}
			}
		})
	}
}

// R-QVZ4-CJ2B R-QX70-QAT0 R-R0UP-VM13 R-RO0T-594A R-R3AI-N5IH R-QM7X-AD4R
func TestChunkAndLoadSteps(t *testing.T) {
	for _, chunk := range []int64{1, store.DefaultChunkBytes} {
		t.Run(fmt.Sprint(chunk), func(t *testing.T) {
			s, _, runs := configured(t, chunk, 2)
			base := finish(t, s, add(t, s, "a"), source.Row{SourceID: "old"})
			baseBytes := snapshot(t, runs)[base.ID+".db"]
			r := add(t, s, "a")
			calls := 0
			var written int64
			step := func(x context.Context, n int64, took time.Duration) error {
				calls++
				written += n
				if took != 0 {
					t.Fatal(took)
				}
				files := snapshot(t, runs)
				if _, ok := files[r.ID+".db"]; ok {
					t.Fatal("premature publication")
				}
				for name := range files {
					if name != base.ID+".db" && !strings.HasPrefix(name, r.ID+".") {
						t.Fatal(name)
					}
					info, e := os.Lstat(filepath.Join(runs, name))
					if e != nil || !info.Mode().IsRegular() {
						t.Fatal(info, e)
					}
				}
				return x.Err()
			}
			c := build(t, s, r, step)
			rows := []source.Row{{SourceID: "3", Digest: "a"}, {SourceID: "1", Digest: "old"}, {SourceID: "2", Digest: "b"}}
			if e := c.Add(ctx, rows); e != nil {
				t.Fatal(e)
			}
			if chunk == 1 && calls != 3 || chunk != 1 && calls != 0 {
				t.Fatal(calls)
			}
			row := source.Row{SourceID: "1", Digest: "latest", Names: []string{"new"}, Facts: []byte(" {} "), Raw: []byte(" { \"v\": 1 } ")}
			if e := c.Add(ctx, []source.Row{row, {SourceID: "1", Digest: "last", Names: []string{"last"}, Facts: row.Facts, Raw: row.Raw}}); e != nil {
				t.Fatal(e)
			}
			row.Facts[1] = 'x'
			row.Raw[1] = 'x'
			rows[0].Digest = "mutated"
			if e := c.Seal(ctx); e != nil {
				t.Fatal(e)
			}
			var total int64
			for name := range snapshot(t, runs) {
				if strings.HasPrefix(name, r.ID+".") {
					info, e := os.Stat(filepath.Join(runs, name))
					if e != nil {
						t.Fatal(e)
					}
					total += info.Size()
				}
			}
			if written != total {
				t.Fatal(written, total)
			}
			if chunk != 1 && calls != 1 {
				t.Fatal(calls)
			}
			loads := 0
			var growth int64
			counts, e := c.Load(ctx, func(x context.Context, n int64, took time.Duration) error {
				loads++
				growth += n
				return step(x, n, took)
			})
			if e != nil {
				t.Fatal(e)
			}
			if loads < 2 || loads > 3 {
				t.Fatal(loads)
			}
			info, e := os.Stat(filepath.Join(runs, r.ID+".db"))
			if e != nil || growth != info.Size() {
				t.Fatal(growth, info, e)
			}
			files := snapshot(t, runs)
			if len(files) != 2 || files[base.ID+".db"] != baseBytes {
				t.Fatal("unexpected files")
			}
			unchanged, e := s.GetRun(ctx, r.ID)
			if e != nil || unchanged != r {
				t.Fatal(unchanged, r, e)
			}
			if _, e = s.FinishRun(ctx, r.ID, counts); e != nil {
				t.Fatal(e)
			}
			got, e := s.GetRow(ctx, r.ID, "1")
			if e != nil || got.Digest != "last" || string(got.Facts) != " {} " || string(got.Raw) != " { \"v\": 1 } " {
				t.Fatal(got, e)
			}
			got, e = s.GetRow(ctx, r.ID, "3")
			if e != nil || got.Digest != "a" {
				t.Fatal(got, e)
			}
		})
	}
}

// R-RKD3-ZXW7 R-N0RE-GU0C R-QX70-QAT0
func TestDefaultsAndTiming(t *testing.T) {
	_, d, runs := configured(t, 0, 0)
	var tick int64
	s := store.New(d, store.Config{Now: func() time.Time { tick++; return time.Unix(tick, 0) }, Rand: bytes.NewReader(append(bytes.Repeat([]byte{9}, 8), bytes.Repeat([]byte{10}, 8)...)), Runs: runs})
	r := add(t, s, "a")
	chunks := 0
	step := func(_ context.Context, _ int64, took time.Duration) error {
		if took <= 0 || took%time.Second != 0 {
			t.Fatal(took)
		}
		chunks++
		return nil
	}
	c := build(t, s, r, step)
	for n := 0; n < 3; n++ {
		if e := c.Add(ctx, []source.Row{{SourceID: fmt.Sprint(n)}}); e != nil {
			t.Fatal(e)
		}
	}
	if chunks != 0 {
		t.Fatal(chunks)
	}
	rows := []source.Row{}
	for n := 3; n < 5001; n++ {
		rows = append(rows, source.Row{SourceID: fmt.Sprintf("%05d", n)})
	}
	if e := c.Add(ctx, rows); e != nil {
		t.Fatal(e)
	}
	if e := c.Seal(ctx); e != nil {
		t.Fatal(e)
	}
	loads := 0
	var growth int64
	counts, e := c.Load(ctx, func(_ context.Context, n int64, took time.Duration) error {
		loads++
		growth += n
		if took <= 0 || took%time.Second != 0 {
			t.Fatal(took)
		}
		return nil
	})
	if e != nil || counts.Rows != 5001 || loads < 2 || loads > 3 {
		t.Fatal(counts, loads, e)
	}
	info, e := os.Stat(filepath.Join(runs, r.ID+".db"))
	if e != nil || growth != info.Size() {
		t.Fatal(growth, info, e)
	}
	empty := build(t, s, add(t, s, "b"), step)
	before := chunks
	if e = empty.Seal(ctx); e != nil || before != chunks {
		t.Fatal(e, chunks)
	}
	loads = 0
	counts, e = empty.Load(ctx, func(context.Context, int64, time.Duration) error { loads++; return nil })
	if e != nil || counts != (store.Counts{}) || loads > 1 {
		t.Fatal(counts, loads, e)
	}
}

// R-RD1P-PBG1 R-QZMT-HUAE R-QYEX-42JP R-RFHI-GUXF R-R9E0-K07Y
func TestBuildRefusalsAndDiscard(t *testing.T) {
	for _, chunk := range []int64{1, store.DefaultChunkBytes} {
		t.Run(fmt.Sprint(chunk), func(t *testing.T) {
			s, _, runs := configured(t, chunk, 2)
			r := add(t, s, "a")
			calls := 0
			c := build(t, s, r, func(context.Context, int64, time.Duration) error { calls++; return nil })
			is(t, func() error { _, e := c.Load(ctx, nil); return e }(), store.ErrUnreachable)
			if e := c.Add(ctx, []source.Row{{SourceID: "1"}}); e != nil {
				t.Fatal(e)
			}
			before := snapshot(t, runs)
			prior := calls
			is(t, c.Add(ctx, []source.Row{{SourceID: "2"}, {}}), store.ErrUnreachable)
			if calls != prior || !reflect.DeepEqual(snapshot(t, runs), before) {
				t.Fatal("invalid call wrote")
			}
			if e := c.Seal(ctx); e != nil {
				t.Fatal(e)
			}
			sealed := snapshot(t, runs)
			is(t, c.Add(ctx, nil), store.ErrUnreachable)
			is(t, c.Seal(ctx), store.ErrUnreachable)
			if !reflect.DeepEqual(snapshot(t, runs), sealed) {
				t.Fatal("sealed call wrote")
			}
			counts, e := c.Load(ctx, nil)
			if e != nil || counts.Rows != 1 {
				t.Fatal(counts, e)
			}
			loaded := snapshot(t, runs)
			is(t, func() error { _, e := c.Load(ctx, nil); return e }(), store.ErrUnreachable)
			if !reflect.DeepEqual(loaded, snapshot(t, runs)) {
				t.Fatal("repeated load changed file")
			}
			if _, e = s.FinishRun(ctx, r.ID, counts); e != nil {
				t.Fatal(e)
			}
			_, e = s.GetRow(ctx, r.ID, "2")
			is(t, e, store.ErrNotFound)
			if e = c.Discard(); e != nil {
				t.Fatal(e)
			}
			if e = c.Discard(); e != nil {
				t.Fatal(e)
			}
			is(t, c.Add(ctx, nil), store.ErrUnreachable)
			is(t, c.Seal(ctx), store.ErrUnreachable)
			is(t, func() error { _, e := c.Load(ctx, nil); return e }(), store.ErrUnreachable)
			if !reflect.DeepEqual(loaded, snapshot(t, runs)) {
				t.Fatal("discard changed published file")
			}
		})
	}
	for _, phase := range []string{"add", "seal", "load"} {
		t.Run(phase, func(t *testing.T) {
			chunk := int64(1)
			if phase == "seal" {
				chunk = store.DefaultChunkBytes
			}
			s, _, runs := configured(t, chunk, 1)
			r := add(t, s, "a")
			sentinel := errors.New("step refused")
			calls := 0
			fail := func(context.Context, int64, time.Duration) error { calls++; return sentinel }
			var step store.Step
			if phase != "load" {
				step = fail
			}
			c := build(t, s, r, step)
			var e error
			if phase == "add" {
				e = c.Add(ctx, []source.Row{{SourceID: "1"}, {SourceID: "2"}})
			} else {
				if e = c.Add(ctx, []source.Row{{SourceID: "1"}, {SourceID: "2"}}); e != nil {
					t.Fatal(e)
				}
				if phase == "seal" {
					e = c.Seal(ctx)
				} else {
					if e = c.Seal(ctx); e != nil {
						t.Fatal(e)
					}
					_, e = c.Load(ctx, fail)
				}
			}
			is(t, e, sentinel)
			is(t, e, store.ErrUnreachable)
			if calls != 1 {
				t.Fatal(calls)
			}
			before := snapshot(t, runs)
			is(t, c.Add(ctx, nil), store.ErrUnreachable)
			is(t, c.Seal(ctx), store.ErrUnreachable)
			_, e = c.Load(ctx, fail)
			is(t, e, store.ErrUnreachable)
			if calls != 1 || !reflect.DeepEqual(before, snapshot(t, runs)) {
				t.Fatal("poisoned build wrote")
			}
			if _, e = os.Lstat(filepath.Join(runs, r.ID+".db")); !os.IsNotExist(e) {
				t.Fatal(e)
			}
			if e = c.Discard(); e != nil || len(snapshot(t, runs)) != 0 {
				t.Fatal(e)
			}
		})
	}
}

// R-M1V7-YLAY R-PLMX-T8VX R-R9E0-K07Y
func TestDirectoryCreationAndBuildOwnership(t *testing.T) {
	for _, chunk := range []int64{1, store.DefaultChunkBytes} {
		t.Run(fmt.Sprint(chunk), func(t *testing.T) {
			s, _, runs := configured(t, chunk, 2)
			r := add(t, s, "a")
			c := build(t, s, r, nil)
			if _, e := os.Lstat(runs); !os.IsNotExist(e) {
				t.Fatal(e)
			}
			if e := c.Add(ctx, []source.Row{{SourceID: "1"}}); e != nil {
				t.Fatal(e)
			}
			if chunk != 1 {
				if _, e := os.Lstat(runs); !os.IsNotExist(e) {
					t.Fatal(e)
				}
			}
			if e := c.Seal(ctx); e != nil {
				t.Fatal(e)
			}
			for _, dir := range []string{runs, filepath.Dir(runs)} {
				info, e := os.Stat(dir)
				if e != nil || info.Mode().Perm() != 0700 {
					t.Fatal(info, e)
				}
			}
			second := build(t, s, r, nil)
			if e := second.Add(ctx, []source.Row{{SourceID: "2"}}); e != nil {
				t.Fatal(e)
			}
			if e := second.Seal(ctx); e != nil {
				t.Fatal(e)
			}
			before := snapshot(t, runs)
			if e := c.Discard(); e != nil {
				t.Fatal(e)
			}
			remaining := snapshot(t, runs)
			if len(remaining) != 1 || len(before) != 2 {
				t.Fatal(len(before), len(remaining))
			}
			target := filepath.Join(runs, r.ID+".db")
			if e := os.Mkdir(target, 0700); e != nil {
				t.Fatal(e)
			}
			if e := os.WriteFile(filepath.Join(target, "keep"), []byte("kept"), 0600); e != nil {
				t.Fatal(e)
			}
			_, e := second.Load(ctx, nil)
			is(t, e, store.ErrUnreachable)
			if e = second.Discard(); e != nil {
				t.Fatal(e)
			}
			b, e := os.ReadFile(filepath.Clean(filepath.Join(target, "keep")))
			if e != nil || string(b) != "kept" {
				t.Fatal(string(b), e)
			}
			if len(snapshot(t, runs)) != 1 {
				t.Fatal("temporary files remain")
			}
		})
	}
	for _, chunk := range []int64{1, store.DefaultChunkBytes} {
		s, _, runs := configured(t, chunk, 2)
		r := add(t, s, "a")
		if e := os.MkdirAll(filepath.Dir(runs), 0700); e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(runs, []byte("sentinel"), 0600); e != nil {
			t.Fatal(e)
		}
		c := build(t, s, r, nil)
		e := c.Add(ctx, []source.Row{{SourceID: "1"}})
		if chunk != 1 {
			if e != nil {
				t.Fatal(e)
			}
			e = c.Seal(ctx)
		}
		is(t, e, store.ErrUnreachable)
		b, e := os.ReadFile(filepath.Clean(runs))
		if e != nil || string(b) != "sentinel" {
			t.Fatal(string(b), e)
		}
	}
}

// R-R5QB-EOZV R-R4IF-0X96 R-NYWL-6ERS R-RQGL-WSLO R-0DYP-DMDK
func TestEndValidationAndLoadAfterEnd(t *testing.T) {
	s, _, runs := configured(t, 1, 1)
	r := add(t, s, "a")
	c := build(t, s, r, nil)
	if e := c.Add(ctx, []source.Row{{SourceID: "1"}}); e != nil {
		t.Fatal(e)
	}
	if e := c.Seal(ctx); e != nil {
		t.Fatal(e)
	}
	before := snapshot(t, runs)
	for _, n := range []store.Counts{{Rows: -1}, {New: -1}, {Changed: -1}, {Unchanged: -1}, {Gone: -1}, {Rows: 1}, {Rows: 0, New: 1}} {
		for _, id := range []string{r.ID, "missing"} {
			got, e := s.FinishRun(ctx, id, n)
			is(t, e, store.ErrUnreachable)
			if got != (store.Run{}) {
				t.Fatal(got)
			}
		}
	}
	for _, reason := range []string{"", "unknown"} {
		got, e := s.FailRun(ctx, "missing", reason)
		is(t, e, store.ErrUnreachable)
		if got != (store.Run{}) {
			t.Fatal(got)
		}
	}
	got, e := s.FinishRun(ctx, r.ID, store.Counts{})
	is(t, e, store.ErrMissing)
	if got != (store.Run{}) {
		t.Fatal(got)
	}
	if !reflect.DeepEqual(before, snapshot(t, runs)) {
		t.Fatal("end refusal changed files")
	}
	for _, reason := range []string{store.ReasonSource, store.ReasonStore, store.ReasonInterrupted} {
		if reason != store.ReasonSource {
			r = add(t, s, "a")
		}
		expected := r
		expected.Status = store.StatusFailed
		expected.Reason = reason
		expected.Finished = r.Started
		failed, e := s.FailRun(ctx, r.ID, reason)
		if e != nil || failed != expected {
			t.Fatal(failed, expected, e)
		}
		_, e = s.FinishRun(ctx, r.ID, store.Counts{})
		is(t, e, store.ErrEnded)
		_, e = s.FailRun(ctx, r.ID, reason)
		is(t, e, store.ErrEnded)
		got, e = s.GetRun(ctx, r.ID)
		if e != nil || got != expected {
			t.Fatal(got, e)
		}
	}
	if !reflect.DeepEqual(before, snapshot(t, runs)) {
		t.Fatal("failure changed files")
	}
	_, e = c.Load(ctx, nil)
	is(t, e, store.ErrEnded)
	// A fresh sealed build's run is pruned before its first Load.
	r = add(t, s, "a")
	removed := build(t, s, r, nil)
	if e = removed.Seal(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e = s.FailRun(ctx, r.ID, store.ReasonSource); e != nil {
		t.Fatal(e)
	}
	finish(t, s, add(t, s, "a"))
	if _, e = s.FailRun(ctx, add(t, s, "a").ID, store.ReasonSource); e != nil {
		t.Fatal(e)
	}
	if _, e = s.PruneRuns(ctx, 1); e != nil {
		t.Fatal(e)
	}
	_, e = removed.Load(ctx, nil)
	is(t, e, store.ErrNotFound)
	for _, id := range []string{"absent"} {
		_, e = s.FinishRun(ctx, id, store.Counts{})
		is(t, e, store.ErrNotFound)
		_, e = s.FailRun(ctx, id, store.ReasonSource)
		is(t, e, store.ErrNotFound)
	}
	for _, keep := range []int64{0, -1} {
		got, e := s.PruneRuns(ctx, keep)
		if got != nil {
			t.Fatal(got)
		}
		is(t, e, store.ErrUnreachable)
	}
}

// R-QNFT-O4VG R-QPVM-FOCU R-QSBF-77U8 R-RO0T-594A
func TestComparisonFallback(t *testing.T) {
	for _, kind := range []string{"gone", "garbage", "header", "other database", "directory", "symlink", "corrupt pages", "missing names"} {
		t.Run(kind, func(t *testing.T) {
			s, _, runs := configured(t, 4096, 2)
			base := finish(t, s, add(t, s, "a"), source.Row{SourceID: "2", Digest: "b"}, source.Row{SourceID: "3", Digest: "x"}, source.Row{SourceID: "4", Digest: "d"})
			newestRows := []source.Row{{SourceID: "bogus", Digest: "bad"}}
			if kind == "corrupt pages" {
				newestRows = nil
				for n := 0; n < 200; n++ {
					newestRows = append(newestRows, source.Row{SourceID: fmt.Sprintf("%04d", n), Digest: "bad", Raw: []byte(strings.Repeat("x", 4000))})
				}
			}
			newest := finish(t, s, add(t, s, "a"), newestRows...)
			path := filepath.Join(runs, newest.ID+".db")
			switch kind {
			case "gone":
				if e := os.Remove(path); e != nil {
					t.Fatal(e)
				}
			case "garbage":
				if e := os.WriteFile(path, []byte("not sqlite"), 0600); e != nil {
					t.Fatal(e)
				}
			case "header":
				if e := os.WriteFile(path, append([]byte("SQLite format 3\x00"), []byte("bad")...), 0600); e != nil {
					t.Fatal(e)
				}
			case "other database":
				otherPath := filepath.Join(t.TempDir(), "other.db")
				other, e := db.Open(ctx, db.Config{Path: otherPath, Migrations: fstest.MapFS{}, Now: func() time.Time { return time.Unix(0, 0) }})
				if e != nil {
					t.Fatal(e)
				}
				if e = other.Write(ctx, func(tx *sql.Tx) error { _, e := tx.ExecContext(ctx, "CREATE TABLE other(n TEXT)"); return e }); e != nil {
					t.Fatal(e)
				}
				if e = other.Close(); e != nil {
					t.Fatal(e)
				}
				b, e := os.ReadFile(filepath.Clean(otherPath))
				if e != nil {
					t.Fatal(e)
				}
				if e = writeRunFile(runs, newest.ID, b); e != nil {
					t.Fatal(e)
				}
			case "directory":
				if e := os.Remove(path); e != nil {
					t.Fatal(e)
				}
				if e := os.Mkdir(path, 0700); e != nil {
					t.Fatal(e)
				}
			case "symlink":
				if e := os.Remove(path); e != nil {
					t.Fatal(e)
				}
				if e := os.Symlink(filepath.Join(runs, base.ID+".db"), path); e != nil {
					t.Fatal(e)
				}
			case "corrupt pages":
				b, e := os.ReadFile(filepath.Clean(path))
				if e != nil {
					t.Fatal(e)
				}
				for n := 4096; n < len(b); n++ {
					b[n] = 0xa5
				}
				if e = writeRunFile(runs, newest.ID, b); e != nil {
					t.Fatal(e)
				}
			case "missing names":
				other, e := db.Open(ctx, db.Config{Path: path, Migrations: fstest.MapFS{}, Now: func() time.Time { return time.Unix(0, 0) }})
				if e != nil {
					t.Fatal(e)
				}
				if e = other.Write(ctx, func(tx *sql.Tx) error { _, e := tx.ExecContext(ctx, "DROP TABLE names"); return e }); e != nil {
					t.Fatal(e)
				}
				if e = other.Close(); e != nil {
					t.Fatal(e)
				}
			}
			before := snapshot(t, runs)
			r := add(t, s, "a")
			c := build(t, s, r, nil)
			counts := load(t, c, []source.Row{{SourceID: "1", Digest: "a"}, {SourceID: "2", Digest: "b"}, {SourceID: "3", Digest: "c"}})
			if counts != (store.Counts{3, 1, 1, 1, 1}) {
				t.Fatal(counts)
			}
			after := snapshot(t, runs)
			for name, b := range before {
				if after[name] != b {
					t.Fatal("comparison changed", name)
				}
			}
			if _, e := s.FinishRun(ctx, r.ID, counts); e != nil {
				t.Fatal(e)
			}
			if e := os.Remove(filepath.Join(runs, r.ID+".db")); e != nil {
				t.Fatal(e)
			}
			if e := os.Remove(filepath.Join(runs, base.ID+".db")); e != nil {
				t.Fatal(e)
			}
			finish(t, s, add(t, s, "other"), source.Row{SourceID: "1", Digest: "a"})
			c = build(t, s, add(t, s, "a"), nil)
			counts = load(t, c, []source.Row{{SourceID: "1", Digest: "a"}})
			if counts != (store.Counts{1, 1, 0, 0, 0}) {
				t.Fatal(counts)
			}
		})
	}
}

// R-RP8P-J0UZ R-OCBH-DVXF R-OERA-5FET R-RMSW-RHDL
func TestFileReadFailuresAndStreamDeletion(t *testing.T) {
	for _, kind := range []string{"gone", "parent file", "garbage", "directory", "intact"} {
		t.Run(kind, func(t *testing.T) {
			s, _, runs := configured(t, 1, 2)
			r := finish(t, s, add(t, s, "a"), source.Row{SourceID: "1", Raw: []byte("{}")})
			path := filepath.Join(runs, r.ID+".db")
			want := store.ErrUnreachable
			switch kind {
			case "gone":
				if e := os.Remove(path); e != nil {
					t.Fatal(e)
				}
				want = store.ErrMissing
			case "parent file":
				if e := os.RemoveAll(runs); e != nil {
					t.Fatal(e)
				}
				if e := os.WriteFile(runs, []byte("sentinel"), 0600); e != nil {
					t.Fatal(e)
				}
				want = store.ErrMissing
			case "garbage":
				if e := os.WriteFile(path, []byte("garbage"), 0600); e != nil {
					t.Fatal(e)
				}
			case "directory":
				if e := os.Remove(path); e != nil {
					t.Fatal(e)
				}
				if e := os.Mkdir(path, 0700); e != nil {
					t.Fatal(e)
				}
			case "intact":
				want = nil
			}
			var before map[string]string
			if kind != "parent file" {
				before = snapshot(t, runs)
			}
			for _, call := range []func() error{
				func() error {
					out, e := s.ListRowsAfter(ctx, r.ID, "", "", 1)
					if want != nil && out != nil {
						t.Fatal(out)
					}
					return e
				}, func() error {
					out, e := s.ListRowsBefore(ctx, r.ID, "", "", 1)
					if want != nil && out != nil {
						t.Fatal(out)
					}
					return e
				}, func() error {
					out, e := s.GetRow(ctx, r.ID, "1")
					if want != nil && !reflect.DeepEqual(out, source.Row{}) {
						t.Fatal(out)
					}
					return e
				}, func() error {
					return s.EachRow(ctx, r.ID, func(source.Row) error {
						if want != nil {
							t.Fatal("callback")
						}
						return nil
					})
				},
			} {
				e := call()
				if want == nil {
					if e != nil {
						t.Fatal(e)
					}
				} else {
					is(t, e, want)
					for _, other := range []error{store.ErrMissing, store.ErrNotFound, store.ErrUnreachable} {
						if !errors.Is(want, other) && errors.Is(e, other) {
							t.Fatal(e)
						}
					}
				}
			}
			if kind != "parent file" {
				if !reflect.DeepEqual(before, snapshot(t, runs)) {
					t.Fatal("read changed files")
				}
			} else {
				b, e := os.ReadFile(filepath.Clean(runs))
				if e != nil || string(b) != "sentinel" {
					t.Fatal(string(b), e)
				}
			}
		})
	}
	for _, prune := range []bool{false, true} {
		s, _, runs := configured(t, 1, 1)
		r := finish(t, s, add(t, s, "a"), source.Row{SourceID: "1"}, source.Row{SourceID: "2"}, source.Row{SourceID: "3"})
		if prune {
			finish(t, s, add(t, s, "a"))
		}
		ids := []string{}
		e := s.EachRow(ctx, r.ID, func(row source.Row) error {
			ids = append(ids, row.SourceID)
			if len(ids) == 1 {
				if prune {
					_, e := s.PruneRuns(ctx, 1)
					return e
				}
				return os.Remove(filepath.Join(runs, r.ID+".db"))
			}
			return nil
		})
		if e != nil || !reflect.DeepEqual(ids, []string{"1", "2", "3"}) {
			t.Fatal(ids, e)
		}
	}
}

func writeRunFile(runs, id string, b []byte) error {
	root, e := os.OpenRoot(runs)
	if e != nil {
		return e
	}
	defer func() { _ = root.Close() }()
	return root.WriteFile(id+".db", b, 0600)
}

// R-RROI-AKCD R-RSWE-OC32 R-OH72-WYW7
func TestOutageAndContextPrecedence(t *testing.T) {
	s, d, runs := configured(t, 1, 2)
	r := add(t, s, "a")
	finish(t, s, add(t, s, "eligible"), source.Row{SourceID: "old"})
	finish(t, s, add(t, s, "eligible"), source.Row{SourceID: "new"})
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	for _, mode := range []string{"cancelled", "failing", "closed"} {
		t.Run(mode, func(t *testing.T) {
			c := build(t, s, r, nil)
			if e := c.Add(ctx, []source.Row{{SourceID: "1"}}); e != nil {
				t.Fatal(e)
			}
			if e := c.Seal(ctx); e != nil {
				t.Fatal(e)
			}
			before := snapshot(t, runs)
			x := ctx
			switch mode {
			case "cancelled":
				x = cancelled
			case "failing":
				d.SetFailing(true)
			default:
				if e := d.Close(); e != nil {
					t.Fatal(e)
				}
			}
			checks := []func() error{
				func() error { _, e := s.AddRun(x, store.Run{}); return e }, func() error { _, e := s.Build(x, "missing", nil); return e }, func() error { _, e := s.FinishRun(x, "missing", store.Counts{Rows: -1}); return e }, func() error { _, e := s.FailRun(x, "missing", "bad"); return e }, func() error { _, e := s.GetRun(x, "missing"); return e }, func() error { _, e := s.ListRunsAfter(x, "", "", "", 0); return e }, func() error { _, e := s.ListRunsBefore(x, "", "", "", 0); return e }, func() error { _, e := s.CurrentRun(x, "missing"); return e }, func() error { _, e := s.RunningRun(x, "missing"); return e }, func() error { _, e := s.RunningRuns(x); return e }, func() error { _, e := s.ListRowsAfter(x, "missing", "", "", 0); return e }, func() error { _, e := s.ListRowsBefore(x, "missing", "", "", 0); return e }, func() error { _, e := s.GetRow(x, "missing", "missing"); return e }, func() error { return s.EachRow(x, r.ID, func(source.Row) error { t.Fatal("callback"); return nil }) }, func() error { _, e := s.PruneRuns(x, 0); return e }, func() error { _, e := s.PruneRuns(x, 1); return e }, func() error { return s.TidyFiles(x) }, func() error {
					_, e := c.Load(x, func(context.Context, int64, time.Duration) error { t.Fatal("step"); return nil })
					return e
				},
			}
			if mode == "cancelled" {
				checks = append(checks, func() error { return c.Add(x, []source.Row{{}}) }, func() error { return c.Seal(x) })
			}
			for _, check := range checks {
				e := check()
				is(t, e, store.ErrUnreachable)
				if mode == "cancelled" {
					is(t, e, x.Err())
				}
				for _, wrong := range []error{store.ErrNotFound, store.ErrBusy, store.ErrDelivered, store.ErrEnded, store.ErrMissing, store.ErrLocked} {
					if errors.Is(e, wrong) {
						t.Fatal(e)
					}
				}
			}
			if !reflect.DeepEqual(before, snapshot(t, runs)) {
				t.Fatal("outage changed files")
			}
			if mode == "failing" {
				d.SetFailing(false)
			}
		})
	}
}

// R-RBTT-BJPC
func TestLockedCatalogFinish(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "catalog")
	s, d := open(t, path, nil)
	r := add(t, s, "a")
	c := build(t, s, r, nil)
	counts := load(t, c, []source.Row{{SourceID: "1"}})
	_, locker := open(t, path, nil)
	held := make(chan error, 1)
	release := make(chan struct{})
	ended := make(chan error, 1)
	rollback := errors.New("release")
	go func() {
		ended <- locker.Write(ctx, func(tx *sql.Tx) error {
			_, e := tx.ExecContext(ctx, `UPDATE runs SET "rows"="rows" WHERE id=?`, r.ID)
			held <- e
			if e != nil {
				return e
			}
			<-release
			return rollback
		})
	}()
	unlock := sync.OnceFunc(func() {
		close(release)
		if e := <-ended; !errors.Is(e, rollback) {
			t.Error(e)
		}
	})
	t.Cleanup(unlock)
	if e := <-held; e != nil {
		t.Fatal(e)
	}
	before := snapshot(t, filepath.Join(dir, "runs"))
	_, e := s.FinishRun(ctx, r.ID, counts)
	is(t, e, store.ErrLocked)
	is(t, e, store.ErrUnreachable)
	unlock()
	got, e := s.GetRun(ctx, r.ID)
	if e != nil || got != r {
		t.Fatal(got, e)
	}
	if !reflect.DeepEqual(before, snapshot(t, filepath.Join(dir, "runs"))) {
		t.Fatal("locked write changed files")
	}
	for _, mode := range []string{"missing", "cancelled", "failing"} {
		var e error
		switch mode {
		case "missing":
			_, e = s.FinishRun(ctx, "missing", counts)
			is(t, e, store.ErrNotFound)
		case "cancelled":
			x, cancel := context.WithCancel(ctx)
			cancel()
			_, e = s.FinishRun(x, r.ID, counts)
			is(t, e, store.ErrUnreachable)
		case "failing":
			d.SetFailing(true)
			_, e = s.FinishRun(ctx, r.ID, counts)
			is(t, e, store.ErrUnreachable)
			d.SetFailing(false)
		}
		if errors.Is(e, store.ErrLocked) {
			t.Fatal(e)
		}
	}
}

// R-R864-68H9
func TestConcurrentBuilds(t *testing.T) {
	s, _, _ := configured(t, 1, 2)
	a := add(t, s, "a")
	b := add(t, s, "b")
	var wg sync.WaitGroup
	for _, r := range []store.Run{a, b} {
		wg.Go(func() {
			c, e := s.Build(ctx, r.ID, nil)
			if e != nil {
				t.Error(e)
				return
			}
			defer func() { _ = c.Discard() }()
			for n := 0; n < 20; n++ {
				if e = c.Add(ctx, []source.Row{{SourceID: fmt.Sprint(n), Digest: r.Scraper}}); e != nil {
					t.Error(e)
					return
				}
				if _, e = s.GetRun(ctx, r.ID); e != nil {
					t.Error(e)
					return
				}
			}
			if e = c.Seal(ctx); e != nil {
				t.Error(e)
				return
			}
			counts, e := c.Load(ctx, nil)
			if e != nil || counts != (store.Counts{20, 20, 0, 0, 0}) {
				t.Error(counts, e)
				return
			}
			if _, e = s.FinishRun(ctx, r.ID, counts); e != nil {
				t.Error(e)
				return
			}
		})
	}
	wg.Wait()
	for _, r := range []store.Run{a, b} {
		rows, e := s.ListRowsAfter(ctx, r.ID, "", "", 30)
		if e != nil || len(rows) != 20 {
			t.Fatal(rows, e)
		}
		for _, row := range rows {
			if row.Scraper != r.Scraper || row.Run != r.ID || row.Digest != r.Scraper {
				t.Fatal(row)
			}
		}
	}
}

// R-OIEZ-AQMW R-OD11-7L2V R-OH72-WYW7 R-RU4B-23TR
func TestLegacyTablesPruneAndTidy(t *testing.T) {
	s, d, runs := configured(t, 1, 1)
	// Seed legacy rows for the identifiers the deterministic random source will mint.
	if e := d.Write(ctx, func(tx *sql.Tx) error {
		for _, id := range []string{"run_0101010101010101", "run_0202020202020202"} {
			if _, e := tx.ExecContext(ctx, `INSERT INTO rows("run",source_id,scraper,collected,digest,name,names,names_lower,street,city,state,zip,county,people,facts,raw) VALUES(?, 'legacy','a','','bad','','["legacy"]','legacy','','','','','','[]','{}','{}')`, id); e != nil {
				return e
			}
			if _, e := tx.ExecContext(ctx, `INSERT INTO row_names VALUES(?,'legacy','legacy','legacy')`, id); e != nil {
				return e
			}
		}
		for _, q := range []string{`CREATE TRIGGER guard_rows_INSERT BEFORE INSERT ON rows BEGIN SELECT RAISE(ABORT,'legacy write'); END`, `CREATE TRIGGER guard_rows_UPDATE BEFORE UPDATE ON rows BEGIN SELECT RAISE(ABORT,'legacy write'); END`, `CREATE TRIGGER guard_rows_DELETE BEFORE DELETE ON rows BEGIN SELECT RAISE(ABORT,'legacy write'); END`, `CREATE TRIGGER guard_row_names_INSERT BEFORE INSERT ON row_names BEGIN SELECT RAISE(ABORT,'legacy write'); END`, `CREATE TRIGGER guard_row_names_UPDATE BEFORE UPDATE ON row_names BEGIN SELECT RAISE(ABORT,'legacy write'); END`, `CREATE TRIGGER guard_row_names_DELETE BEFORE DELETE ON row_names BEGIN SELECT RAISE(ABORT,'legacy write'); END`} {
			if _, e := tx.ExecContext(ctx, q); e != nil {
				return e
			}
		}
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	done := finish(t, s, add(t, s, "a"), source.Row{SourceID: "file", Names: []string{"file"}})
	failed, e := s.FailRun(ctx, add(t, s, "b").ID, store.ReasonSource)
	if e != nil {
		t.Fatal(e)
	}
	for _, call := range []func() error{
		func() error {
			rows, e := s.ListRowsAfter(ctx, done.ID, "", "", 10)
			if len(rows) != 1 || rows[0].SourceID != "file" {
				t.Fatal(rows)
			}
			return e
		}, func() error {
			rows, e := s.ListRowsBefore(ctx, done.ID, "", "", 10)
			if len(rows) != 1 || rows[0].SourceID != "file" {
				t.Fatal(rows)
			}
			return e
		}, func() error { _, e := s.GetRow(ctx, done.ID, "file"); return e }, func() error {
			return s.EachRow(ctx, done.ID, func(r source.Row) error {
				if r.SourceID != "file" {
					t.Fatal(r)
				}
				return nil
			})
		},
	} {
		if e = call(); e != nil {
			t.Fatal(e)
		}
	}
	if e = os.Remove(filepath.Join(runs, done.ID+".db")); e != nil {
		t.Fatal(e)
	}
	_, e = s.GetRow(ctx, done.ID, "legacy")
	is(t, e, store.ErrMissing)
	// A directory at a pruned file is deleted, too.
	if e = os.Mkdir(filepath.Join(runs, done.ID+".db"), 0700); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(runs, done.ID+".db", "keep"), []byte("x"), 0600); e != nil {
		t.Fatal(e)
	}
	newest := finish(t, s, add(t, s, "a"), source.Row{SourceID: "newest"})
	if _, e = s.FailRun(ctx, add(t, s, "b").ID, store.ReasonSource); e != nil {
		t.Fatal(e)
	}
	if e = d.Write(ctx, func(tx *sql.Tx) error {
		_, e := tx.ExecContext(ctx, "CREATE TRIGGER refuse_prune BEFORE DELETE ON runs BEGIN SELECT RAISE(ABORT,'refuse'); END")
		return e
	}); e != nil {
		t.Fatal(e)
	}
	before := snapshot(t, runs)
	_, e = s.PruneRuns(ctx, 1)
	is(t, e, store.ErrUnreachable)
	if !reflect.DeepEqual(before, snapshot(t, runs)) {
		t.Fatal("failed prune changed files")
	}
	if e = d.Write(ctx, func(tx *sql.Tx) error { _, e := tx.ExecContext(ctx, "DROP TRIGGER refuse_prune"); return e }); e != nil {
		t.Fatal(e)
	}
	removed, e := s.PruneRuns(ctx, 1)
	if e != nil || !reflect.DeepEqual(removed, []store.Run{done, failed}) {
		t.Fatal(removed, e)
	}
	if _, e = os.Lstat(filepath.Join(runs, done.ID+".db")); !os.IsNotExist(e) {
		t.Fatal(e)
	}
	// The write-aborting legacy-table triggers remain installed throughout the
	// successful build, reads, failure and prune above.
	running := add(t, s, "a")
	for _, name := range []string{running.ID + ".db", failed.ID + ".db", newest.ID + ".chunk", "unknown.db", "notes.txt"} {
		if e = os.WriteFile(filepath.Join(runs, name), []byte("trash"), 0600); e != nil {
			t.Fatal(e)
		}
	}
	if e = os.Mkdir(filepath.Join(runs, "junk"), 0700); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(runs, "junk", "child"), []byte("trash"), 0600); e != nil {
		t.Fatal(e)
	}
	kept := snapshot(t, runs)[newest.ID+".db"]
	if e = s.TidyFiles(ctx); e != nil {
		t.Fatal(e)
	}
	if got := snapshot(t, runs); len(got) != 1 || got[newest.ID+".db"] != kept {
		t.Fatal("tidy contents", len(got))
	}
	records, e := s.ListRunsAfter(ctx, "", "", "", 30)
	if e != nil || len(records) != 3 {
		t.Fatal(records, e)
	}
	if e = os.RemoveAll(runs); e != nil {
		t.Fatal(e)
	}
	if e = s.TidyFiles(ctx); e != nil {
		t.Fatal(e)
	}
	if _, e = os.Lstat(runs); !os.IsNotExist(e) {
		t.Fatal(e)
	}
	if e = os.WriteFile(runs, []byte("sentinel"), 0600); e != nil {
		t.Fatal(e)
	}
	if e = s.TidyFiles(ctx); e != nil {
		t.Fatal(e)
	}
	b, e := os.ReadFile(filepath.Clean(runs))
	if e != nil || string(b) != "sentinel" {
		t.Fatal(string(b), e)
	}
}
