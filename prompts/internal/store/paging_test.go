package store_test

import (
	"bytes"
	"database/sql"
	"errors"
	"io/fs"
	"math"
	"path/filepath"
	"testing"
	"testing/fstest"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/prompts"
	"github.com/ikigenba/ikigenba/prompts/internal/store"
)

// R-T27H-KF4L R-SNKO-Z689 R-STO6-W0XQ
func TestPromptWindows(t *testing.T) {
	s, _, _ := setup(t)
	want := []store.Prompt{create(t, s, "a-b"), create(t, s, "a1"), create(t, s, "aa")}
	other := draft("a0")
	other.Owner = "bob"
	if _, e := s.Create(ctx, other); e != nil {
		t.Fatal(e)
	}
	for _, tc := range []struct {
		after string
		n     int
		want  []store.Prompt
	}{
		{"", 2, want[:2]}, {"-", 9, want}, {"a1", 9, want[2:]}, {string([]byte{255}), 9, []store.Prompt{}}, {"", 0, []store.Prompt{}}, {"", -1, []store.Prompt{}},
	} {
		got, e := s.List(ctx, "alice", tc.after, tc.n)
		if e != nil {
			t.Fatal(e)
		}
		equal(t, got, tc.want)
	}
	equal(t, content(t, s)[:3], want)
}

// R-WRTI-41G2 R-SZRO-SVN7 R-TAQS-8TBG R-SR8E-4HGC
func TestRunWindowsAndCount(t *testing.T) {
	s, _, _ := setup(t)
	p := create(t, s, "window")
	q := create(t, s, "other-window")
	want := []store.Run{}
	for i, n := range []int{3, 2, 1, 9, 4} {
		status := store.StatusFailed
		if i == 0 {
			status = store.StatusQueued
		}
		if i == 1 {
			status = store.StatusRunning
		}
		r := record(p, n, status)
		if i >= 3 {
			r.Started = instant.Add(-time.Duration(i-2) * time.Second)
		}
		if status == store.StatusFailed {
			r.Finished = r.Started
			r.Reason = store.ReasonStartFailed
		}
		want = append(want, add(t, s, r))
	}
	add(t, s, record(q, 50, store.StatusQueued))
	got := []store.Run{}
	started, id := "2000-01-01T00:00:00Z", ""
	for {
		page, e := s.Runs(ctx, p.ID, started, id, 2)
		if e != nil || page == nil || len(page) > 2 {
			t.Fatal(page, e)
		}
		if len(page) == 0 {
			break
		}
		got = append(got, page...)
		last := page[len(page)-1]
		started, id = last.Started.UTC().Format(time.RFC3339), last.ID
	}
	equal(t, got, want)
	for _, n := range []int{0, -1} {
		got, e := s.Runs(ctx, p.ID, "", "", n)
		if e != nil || got == nil || len(got) != 0 {
			t.Fatal(got, e)
		}
	}
	for i := 10; i < 35; i++ {
		add(t, s, record(p, i, store.StatusQueued))
	}
	count, e := s.RunCount(ctx, p.ID)
	if e != nil || count != 30 {
		t.Fatal(count, e)
	}
	before := content(t, s)
	for _, id := range []string{q.ID, "missing"} {
		n, e := s.RunCount(ctx, id)
		if e != nil {
			t.Fatal(e)
		}
		if id == q.ID {
			equal(t, n, 1)
		} else {
			equal(t, n, 0)
		}
	}
	equal(t, content(t, s), before)
}

// R-SW3Z-NKF4
func TestEmptyWindows(t *testing.T) {
	s, _, _ := setup(t)
	for _, n := range []int{-1, 0, 1, math.MaxInt} {
		for _, after := range []string{"", "unknown"} {
			ps, e := s.List(ctx, "owner", after, n)
			if e != nil || ps == nil || len(ps) != 0 {
				t.Fatal(ps, e)
			}
			rs, e := s.Runs(ctx, "prompt", after, after, n)
			if e != nil || rs == nil || len(rs) != 0 {
				t.Fatal(rs, e)
			}
		}
	}
	n, e := s.RunCount(ctx, "unknown")
	if e != nil || n != 0 {
		t.Fatal(n, e)
	}
}

// R-T5V6-PQCO R-TGUA-5O0X R-TEEH-E4JJ
func TestLastTieRetentionAndDeletion(t *testing.T) {
	s, d, path := setup(t)
	p := create(t, s, "last-tie")
	low := record(p, 1, store.StatusFailed)
	low.Finished = low.Started
	low.Reason = store.ReasonStartFailed
	low = add(t, s, low)
	high := low
	high.ID = record(p, 2, store.StatusQueued).ID
	high = add(t, s, high)
	check := func(p store.Prompt, e error) {
		t.Helper()
		if e != nil {
			t.Fatal(e)
		}
		equal(t, *p.Last, high)
	}
	check(s.Find(ctx, p.Owner, p.Name))
	ps, e := s.List(ctx, p.Owner, "", 2)
	if e != nil {
		t.Fatal(e)
	}
	check(ps[0], nil)
	updated, _, e := s.Update(ctx, p.ID, store.Change{})
	check(updated, e)
	check(s.Subscribe(ctx, p.ID, "cron.*.fired"))
	ps, e = s.Subscribers(ctx, "cron.hourly.fired")
	if e != nil {
		t.Fatal(e)
	}
	check(ps[0], nil)
	check(s.Unsubscribe(ctx, p.ID, "cron.*.fired"))
	rs, e := s.PastKeeping(ctx, normalized(instant).Add(3*24*time.Hour), 1, 1)
	if e != nil {
		t.Fatal(e)
	}
	equal(t, rs, []store.Run{low})
	if e = s.DeleteRun(ctx, high.ID); e != nil {
		t.Fatal(e)
	}
	after, e := s.Find(ctx, p.Owner, p.Name)
	if e != nil {
		t.Fatal(e)
	}
	equal(t, *after.Last, low)
	if e = d.Close(); e != nil {
		t.Fatal(e)
	}
	reopened, _ := open(t, path, bytes.NewReader(nil))
	after, e = reopened.Find(ctx, p.Owner, p.Name)
	if e != nil {
		t.Fatal(e)
	}
	equal(t, *after.Last, low)
	if e = reopened.DeleteRun(ctx, low.ID); e != nil {
		t.Fatal(e)
	}
	after, e = reopened.Find(ctx, p.Owner, p.Name)
	if e != nil || after.Last != nil {
		t.Fatal(after, e)
	}
}

// R-TJA2-X7IB
func TestListIndexes(t *testing.T) {
	_, d, _ := setup(t)
	e := d.Read(ctx, func(tx *sql.Tx) error {
		for table, want := range map[string]struct {
			name    string
			columns []string
		}{"prompts": {"prompts_owner_name", []string{"owner_id", "name"}}, "runs": {"runs_prompt_started_id", []string{"prompt", "started", "id"}}} {
			rows, e := tx.Query("PRAGMA index_list(" + table + ")")
			if e != nil {
				return e
			}
			found := false
			for rows.Next() {
				var seq, unique, partial int
				var name, origin string
				if e = rows.Scan(&seq, &name, &unique, &origin, &partial); e != nil {
					break
				}
				if name == want.name {
					found = true
					equal(t, unique, 0)
					equal(t, origin, "c")
				}
			}
			if e = errors.Join(e, rows.Err(), rows.Close()); e != nil {
				return e
			}
			if !found {
				t.Fatal(want.name)
			}
			rows, e = tx.Query("PRAGMA index_info(" + want.name + ")")
			if e != nil {
				return e
			}
			got := []string{}
			for rows.Next() {
				var seq, cid int
				var name string
				if e = rows.Scan(&seq, &cid, &name); e != nil {
					break
				}
				got = append(got, name)
			}
			if e = errors.Join(e, rows.Err(), rows.Close()); e != nil {
				return e
			}
			equal(t, got, want.columns)
		}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
}

// R-TMXS-2IQE
func TestExpandCatalogPreservesContent(t *testing.T) {
	first, e := fs.ReadFile(prompts.Migrations(), "0001_catalog.sql")
	if e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(t.TempDir(), "old.db")
	d, e := db.Open(ctx, db.Config{Path: path, Migrations: fstest.MapFS{"0001_catalog.sql": &fstest.MapFile{Data: first}}, Now: clock})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if e := d.Close(); e != nil {
			t.Error(e)
		}
	})
	s := store.New(d, store.Config{Now: clock, Rand: bytes.NewReader(make([]byte, 8))})
	p := create(t, s, "old-prompt")
	if _, e = s.Subscribe(ctx, p.ID, "cron.*.fired"); e != nil {
		t.Fatal(e)
	}
	r := record(p, 1, store.StatusQueued)
	r.Trigger = store.TriggerEvent
	r.Event = "delivery-one"
	add(t, s, r)
	if _, e = s.StartRun(ctx, r.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = s.FinishRun(ctx, r.ID, store.Ending{Status: store.StatusExited, Finished: instant, Usage: store.Usage{Calls: 2, CostNanos: 123}}); e != nil {
		t.Fatal(e)
	}
	before := content(t, s)
	runs, e := s.Runs(ctx, p.ID, "", "", math.MaxInt)
	if e != nil {
		t.Fatal(e)
	}
	if e = d.Close(); e != nil {
		t.Fatal(e)
	}
	next, _ := open(t, path, bytes.NewReader(nil))
	equal(t, content(t, next), before)
	got, e := next.Runs(ctx, p.ID, "", "", math.MaxInt)
	if e != nil {
		t.Fatal(e)
	}
	equal(t, got, runs)
	yes, e := next.Delivered(ctx, p.ID, r.Event)
	if e != nil || !yes {
		t.Fatal(yes, e)
	}
	no, e := next.Delivered(ctx, p.ID, "other-delivery")
	if e != nil || no {
		t.Fatal(no, e)
	}
}
