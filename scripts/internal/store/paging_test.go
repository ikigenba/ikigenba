package store_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/scripts/internal/store"
)

// R-CJXN-52ZC
func TestReversedWindowMethods(t *testing.T) {
	s := open(t, "")
	var list func(*store.Store, context.Context, string, string, int) ([]store.Script, error)
	var runs func(*store.Store, context.Context, string, string, string, int) ([]store.Run, error)
	list = (*store.Store).ListBefore
	runs = (*store.Store).RunsBefore
	_, err := list(s, ctx, "alice", "", 1)
	must(t, err)
	_, err = runs(s, ctx, "absent", "", "", 1)
	must(t, err)
}

// R-CL5J-IUQ1
func TestListBefore(t *testing.T) {
	s := open(t, "")
	for i, name := range []string{"aa", "a1", "a-b"} {
		sc := create(t, s, "alice", name)
		_, err := s.Subscribe(ctx, sc.ID, "repo.pushed")
		must(t, err)
		add(t, s, run(sc, i+1))
	}
	create(t, s, "bob", "a0")
	all, err := s.List(ctx, "alice", "", 10)
	must(t, err)
	for _, tc := range []struct {
		before string
		n      int
		want   []store.Script
	}{
		{"aa", 5, []store.Script{all[1], all[0]}},
		{"a2", 5, []store.Script{all[1], all[0]}},
		{"a-b", 5, []store.Script{}},
		{"-", 5, []store.Script{}},
		{string([]byte{255}), 5, []store.Script{all[2], all[1], all[0]}},
		{"", 2, []store.Script{all[2], all[1]}},
		{"", 3, []store.Script{all[2], all[1], all[0]}},
		{"aa", 1, []store.Script{all[1]}},
		{"", 0, []store.Script{}},
		{"aa", -1, []store.Script{}},
		{"' OR 1=1 --", 5, []store.Script{}},
	} {
		got, err := s.ListBefore(ctx, "alice", tc.before, tc.n)
		must(t, err)
		equal(t, got, tc.want)
	}
	for _, owner := range []string{"", "absent", "alice' OR 1=1 --"} {
		got, err := s.ListBefore(ctx, owner, "", 5)
		must(t, err)
		equal(t, got, []store.Script{})
	}
}

// R-CMDF-WMGQ
func TestRunsBefore(t *testing.T) {
	s := open(t, "")
	sc := create(t, s, "alice", "alpha")
	other := create(t, s, "bob", "beta")
	base := stamp.UTC().Truncate(time.Second)
	want := []store.Run{}
	for i, entry := range []struct {
		id     int
		offset time.Duration
	}{{30, 0}, {20, 0}, {10, 0}, {99, -time.Second}, {4, -2 * time.Second}} {
		r := run(sc, entry.id)
		r.Started = base.Add(entry.offset)
		if i == 1 {
			r.Status = store.StatusQueued
		}
		if i == 2 {
			r.Status = store.StatusFailed
			r.Reason = store.ReasonGitFailed
			r.Finished = r.Started.Add(time.Minute)
			r.StdoutBytes, r.StderrBytes, r.Truncated = 7, 11, true
		}
		if i == 3 {
			r.Trigger, r.Event = store.TriggerEvent, "event-one"
		}
		want = append(want, add(t, s, r))
	}
	add(t, s, run(other, 200))
	all, err := s.Runs(ctx, sc.ID, "", "", 5)
	must(t, err)
	equal(t, all, want)
	for i, r := range all {
		reversed := []store.Run{}
		for j := i - 1; j >= 0; j-- {
			reversed = append(reversed, all[j])
		}
		got, err := s.RunsBefore(ctx, sc.ID, r.Started.UTC().Format(time.RFC3339), r.ID, 5)
		must(t, err)
		equal(t, got, reversed)
	}
	for _, tc := range []struct {
		started string
		id      string
		n       int
		want    []store.Run
	}{
		{"", "", 2, []store.Run{want[4], want[3]}},
		{"", "", 7, []store.Run{want[4], want[3], want[2], want[1], want[0]}},
		{"2000-01-01T00:00:00Z", "", 2, []store.Run{want[4], want[3]}},
		{base.Format(time.RFC3339), run(sc, 15).ID, 5, []store.Run{want[1], want[0]}},
		{base.Format(time.RFC3339), run(sc, 15).ID, 1, []store.Run{want[1]}},
		{want[4].Started.Format(time.RFC3339), want[4].ID, 2, []store.Run{want[3], want[2]}},
		{"", "", 0, []store.Run{}},
		{base.Format(time.RFC3339), want[2].ID, -1, []store.Run{}},
		{"z' OR 1=1 --", "arbitrary", 5, []store.Run{}},
	} {
		got, err := s.RunsBefore(ctx, sc.ID, tc.started, tc.id, tc.n)
		must(t, err)
		equal(t, got, tc.want)
	}
	for _, script := range []string{"", "absent", sc.ID + "' OR 1=1 --"} {
		got, err := s.RunsBefore(ctx, script, "", "", 5)
		must(t, err)
		equal(t, got, []store.Run{})
	}
}

// R-H9W4-F3CV R-HERP-Y6BN R-HFZM-BY2C R-HCBX-6MU9 R-HH7I-PPT1 R-HIFF-3HJQ R-HB40-SV3K
func TestWindowsAndCount(t *testing.T) {
	s := open(t, "")
	for _, name := range []string{"aa", "a1", "a-b"} {
		create(t, s, "alice", name)
	}
	other := create(t, s, "bob", "foreign")
	for _, tc := range []struct {
		after string
		n     int
		names []string
	}{{"", 2, []string{"a-b", "a1"}}, {"-", 9, []string{"a-b", "a1", "aa"}}, {"a1", 9, []string{"aa"}}, {string([]byte{255}), 9, []string{}}, {"", 0, []string{}}, {"", -1, []string{}}} {
		list, err := s.List(ctx, "alice", tc.after, tc.n)
		must(t, err)
		names := []string{}
		for _, sc := range list {
			names = append(names, sc.Name)
		}
		equal(t, names, tc.names)
	}
	sc, err := s.Find(ctx, "alice", "aa")
	must(t, err)
	base := stamp.UTC().Truncate(time.Second)
	want := []store.Run{}
	for _, entry := range []struct {
		id     int
		offset time.Duration
	}{{3, 0}, {2, 0}, {1, 0}, {99, -time.Second}, {4, -2 * time.Second}} {
		r := run(sc, entry.id)
		r.Started = base.Add(entry.offset)
		want = append(want, add(t, s, r))
	}
	add(t, s, run(other, 200))
	all := []store.Run{}
	started, id := "", ""
	for {
		rr, err := s.Runs(ctx, sc.ID, started, id, 2)
		must(t, err)
		if len(rr) == 0 {
			break
		}
		all = append(all, rr...)
		last := rr[len(rr)-1]
		started, id = last.Started.UTC().Format(time.RFC3339), last.ID
	}
	equal(t, all, want)
	rr, err := s.Runs(ctx, sc.ID, "2000-01-01T00:00:00Z", "", 2)
	must(t, err)
	equal(t, rr, want[:2])
	for _, n := range []int{0, -1} {
		rr, err := s.Runs(ctx, sc.ID, "", "", n)
		must(t, err)
		equal(t, rr, []store.Run{})
	}
	for _, missing := range []string{"absent", ""} {
		rr, err := s.Runs(ctx, missing, "", "", 2)
		must(t, err)
		equal(t, rr, []store.Run{})
		count, err := s.RunCount(ctx, missing)
		must(t, err)
		equal(t, count, 0)
	}
	got, err := s.Find(ctx, sc.Owner, sc.Name)
	must(t, err)
	equal(t, *got.Last, want[0])
	must(t, s.DeleteRun(ctx, want[0].ID))
	got, err = s.Find(ctx, sc.Owner, sc.Name)
	must(t, err)
	equal(t, *got.Last, want[1])
	for i := 0; i < 26; i++ {
		r := run(sc, 300+i)
		r.Status = store.StatusFailed
		r.Reason = store.ReasonGitFailed
		r.Finished = r.Started.Add(time.Second)
		if i == 0 {
			r.Status = store.StatusQueued
			r.Reason = ""
			r.Finished = time.Time{}
		}
		add(t, s, r)
	}
	before := content(t, s)
	count, err := s.RunCount(ctx, sc.ID)
	must(t, err)
	equal(t, count, 30)
	rr, err = s.Runs(ctx, sc.ID, "", "", 1)
	must(t, err)
	equal(t, len(rr), 1)
	count, err = s.RunCount(ctx, sc.ID)
	must(t, err)
	equal(t, count, 30)
	equal(t, content(t, s), before)
}

// R-WZYR-VVYZ R-EE5D-1U9Q
func TestCatalogBlobDoesNotControlReadsOrWrites(t *testing.T) {
	for _, replacement := range []bool{false, true} {
		t.Run(fmt.Sprint(replacement), func(t *testing.T) {
			s := open(t, "")
			sc := create(t, s, "alice", "alpha")
			other := create(t, s, "bob", "beta")
			running := add(t, s, run(sc, 1))
			queued := run(sc, 2)
			queued.Status = store.StatusQueued
			queued = add(t, s, queued)
			event := run(sc, 3)
			event.Trigger = store.TriggerEvent
			event.Event = "delivery-one"
			event = add(t, s, event)
			must(t, s.DeleteRun(ctx, event.ID))
			_, err := s.Subscribe(ctx, sc.ID, "cron.*.fired")
			must(t, err)

			ended := run(sc, 4)
			ended.Started = stamp.Add(-time.Hour)
			ended.Status = store.StatusFailed
			ended.Reason = store.ReasonGitFailed
			ended.Finished = ended.Started.Add(time.Minute)
			ended = add(t, s, ended)
			before := content(t, s)
			poison := func() {
				must(t, handle(s).Write(ctx, func(tx *sql.Tx) error {
					if !replacement {
						_, err := tx.ExecContext(ctx, "DELETE FROM catalog")
						return err
					}
					phantom := store.Script{ID: "scr_ffffffffffffffff", Name: "phantom", Owner: "alice", Repo: "repo", Ref: "main", Created: stamp}
					encoded, err := json.Marshal(struct {
						Scripts map[string]store.Script
						Runs    map[string]store.Run
					}{map[string]store.Script{phantom.ID: phantom}, map[string]store.Run{}})
					if err != nil {
						return err
					}
					_, err = tx.ExecContext(ctx, "INSERT INTO catalog VALUES(1,?) ON CONFLICT(id) DO UPDATE SET value=excluded.value", encoded)
					return err
				}))
			}
			poison()
			equal(t, content(t, s), before)
			got, err := s.Find(ctx, sc.Owner, sc.Name)
			must(t, err)
			equal(t, got.ID, sc.ID)
			taken, err := s.Taken(ctx, sc.Name)
			must(t, err)
			equal(t, taken, true)
			taken, err = s.Taken(ctx, "phantom")
			must(t, err)
			equal(t, taken, false)
			count, err := s.RunCount(ctx, sc.ID)
			must(t, err)
			equal(t, count, 3)
			r, err := s.RunByID(ctx, running.ID)
			must(t, err)
			equal(t, r, running)
			r, err = s.FindRun(ctx, sc.Owner, running.ID)
			must(t, err)
			equal(t, r, running)
			_, err = s.Create(ctx, store.Draft{Owner: "bob", Name: sc.Name, Repo: "repo", Ref: "main"})
			equal(t, errors.Is(err, store.ErrNameTaken), true)
			delivered, err := s.Delivered(ctx, sc.ID, event.Event)
			must(t, err)
			equal(t, delivered, true)

			rr, err := s.Running(ctx)
			must(t, err)
			equal(t, rr, []store.Run{running})
			rr, err = s.Queued(ctx)
			must(t, err)
			equal(t, rr, []store.Run{queued})
			rr, err = s.PastKeeping(ctx, stamp.Add(48*time.Hour), 1, 1)
			must(t, err)
			equal(t, rr, []store.Run{ended})
			subscribers, err := s.Subscribers(ctx, "cron.hourly.fired")
			must(t, err)
			equal(t, subscribers, []store.Script{got})
			poison()
			subscribed, err := s.Subscribe(ctx, sc.ID, "repo.pushed")
			must(t, err)
			want := got
			want.Subscriptions = append(append([]store.Subscription{}, got.Subscriptions...), store.Subscription{Event: "repo.pushed", Created: stamp.UTC().Truncate(time.Second)})
			equal(t, subscribed, want)
			subscribers, err = s.Subscribers(ctx, "repo.pushed")
			must(t, err)
			equal(t, subscribers, []store.Script{want})
			poison()
			unsubscribed, err := s.Unsubscribe(ctx, sc.ID, "repo.pushed")
			must(t, err)
			equal(t, unsubscribed, got)
			subscribers, err = s.Subscribers(ctx, "cron.hourly.fired")
			must(t, err)
			equal(t, subscribers, []store.Script{got})
			equal(t, content(t, s), before)
			delivered, err = s.Delivered(ctx, sc.ID, event.Event)
			must(t, err)
			equal(t, delivered, true)
			poison()
			_, err = s.Create(ctx, store.Draft{Owner: "alice", Name: "phantom", Repo: "repo", Ref: "main"})
			must(t, err)
			assertMirror(t, s)
			poison()
			_, _, err = s.SetRef(ctx, sc.ID, "other")
			must(t, err)
			assertMirror(t, s)
			poison()
			newRun := add(t, s, run(other, 9))
			assertMirror(t, s)
			poison()
			_, err = s.StartRun(ctx, queued.ID)
			must(t, err)
			assertMirror(t, s)
			poison()
			_, err = s.FinishRun(ctx, running.ID, store.Ending{Status: store.StatusExited, Finished: stamp.Add(time.Minute)})
			must(t, err)
			assertMirror(t, s)
			poison()
			must(t, s.DeleteRun(ctx, newRun.ID))
			assertMirror(t, s)
			poison()
			must(t, s.Delete(ctx, sc.ID))
			assertMirror(t, s)
			delivered, err = s.Delivered(ctx, sc.ID, event.Event)
			must(t, err)
			equal(t, delivered, false)
			_, err = s.RunByID(ctx, running.ID)
			equal(t, errors.Is(err, store.ErrNotFound), true)
			_, err = s.RunByID(ctx, queued.ID)
			equal(t, errors.Is(err, store.ErrNotFound), true)
		})
	}
}

// R-HNB0-MKII R-HCBX-6MU9
func TestRetentionEqualStartedBoundary(t *testing.T) {
	s := open(t, "")
	sc := create(t, s, "alice", "alpha")
	ended := []store.Run{}
	for _, id := range []int{1, 2} {
		r := add(t, s, run(sc, id))
		r, err := s.FinishRun(ctx, r.ID, store.Ending{Status: store.StatusExited, Finished: stamp})
		must(t, err)
		ended = append(ended, r)
	}
	started := ended[0].Started
	before := content(t, s)
	got, err := s.PastKeeping(ctx, started.Add(24*time.Hour), 1, 1)
	must(t, err)
	equal(t, got, []store.Run{})
	got, err = s.PastKeeping(ctx, started.Add(24*time.Hour+time.Nanosecond), 1, 1)
	must(t, err)
	equal(t, got, []store.Run{ended[0]})
	equal(t, content(t, s), before)
}

// R-HNB0-MKII
func TestRetentionCutoffRoundsBeyondFourDigitYear(t *testing.T) {
	s := open(t, "")
	sc := create(t, s, "alice", "alpha")
	ended := []store.Run{}
	for _, id := range []int{1, 2} {
		r := run(sc, id)
		r.Started = time.Date(9999, 12, 30, 12, 0, 0, 0, time.UTC)
		r.Status = store.StatusFailed
		r.Reason = store.ReasonGitFailed
		r.Finished = r.Started.Add(time.Minute)
		ended = append(ended, add(t, s, r))
	}
	before := content(t, s)
	now := time.Date(10000, 1, 1, 23, 59, 59, 1, time.UTC)
	got, err := s.PastKeeping(ctx, now, 1, 1)
	must(t, err)
	equal(t, got, []store.Run{ended[0]})
	equal(t, content(t, s), before)
}
