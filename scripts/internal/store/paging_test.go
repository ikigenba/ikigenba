package store_test

import (
	"bytes"
	"database/sql"
	"encoding/gob"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/scripts/internal/store"
)

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

// R-HDJT-KEKY R-EFD9-FM0F
func assertRecord(t *testing.T, s *store.Store) {
	t.Helper()
	var encoded []byte
	must(t, handle(s).Read(ctx, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, "SELECT value FROM catalog WHERE id=1").Scan(&encoded)
	}))
	var stored struct {
		Scripts map[string]store.Script
		Runs    map[string]store.Run
	}
	must(t, gob.NewDecoder(bytes.NewReader(encoded)).Decode(&stored))
	scripts, runs := 0, 0
	for _, owner := range []string{"alice", "bob"} {
		ss, err := s.List(ctx, owner, "", 10000)
		must(t, err)
		for _, sc := range ss {
			scripts++
			got, ok := stored.Scripts[sc.ID]
			equal(t, ok, true)
			equal(t, []string{got.ID, got.Name, got.Owner, got.Repo, got.Ref}, []string{sc.ID, sc.Name, sc.Owner, sc.Repo, sc.Ref})
			equal(t, got.Created.Equal(sc.Created), true)
			rr, err := s.Runs(ctx, sc.ID, "", "", 10000)
			must(t, err)
			for _, r := range rr {
				runs++
				got, ok := stored.Runs[r.ID]
				equal(t, ok, true)
				equal(t, got.Started.Equal(r.Started), true)
				equal(t, got.Finished.Equal(r.Finished), true)
				got.Started = r.Started
				got.Finished = r.Finished
				equal(t, got, r)
			}
		}
	}
	equal(t, len(stored.Scripts), scripts)
	equal(t, len(stored.Runs), runs)
}

// R-EBPK-AASC R-EE5D-1U9Q R-EFD9-FM0F
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
					var encoded bytes.Buffer
					if err := gob.NewEncoder(&encoded).Encode(struct {
						Scripts map[string]store.Script
						Runs    map[string]store.Run
					}{map[string]store.Script{phantom.ID: phantom}, map[string]store.Run{}}); err != nil {
						return err
					}
					_, err := tx.ExecContext(ctx, "INSERT INTO catalog VALUES(1,?) ON CONFLICT(id) DO UPDATE SET value=excluded.value", encoded.Bytes())
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
			assertRecord(t, s)
			poison()
			_, _, err = s.SetRef(ctx, sc.ID, "other")
			must(t, err)
			assertRecord(t, s)
			poison()
			newRun := add(t, s, run(other, 9))
			assertRecord(t, s)
			poison()
			_, err = s.StartRun(ctx, queued.ID)
			must(t, err)
			assertRecord(t, s)
			poison()
			_, err = s.FinishRun(ctx, running.ID, store.Ending{Status: store.StatusExited, Finished: stamp.Add(time.Minute)})
			must(t, err)
			assertRecord(t, s)
			poison()
			must(t, s.DeleteRun(ctx, newRun.ID))
			assertRecord(t, s)
			poison()
			must(t, s.Delete(ctx, sc.ID))
			assertRecord(t, s)
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
