package store_test

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/scripts/internal/store"
)

// R-R7EC-DYXR R-G3V1-AOWK
func TestEventVocabulary(t *testing.T) {
	for i, a := range []error{store.ErrNotFound, store.ErrNameTaken, store.ErrEnded, store.ErrNotSubscribed, store.ErrDelivered} {
		if a == nil {
			t.Fatal("nil sentinel")
		}
		for j, b := range []error{store.ErrNotFound, store.ErrNameTaken, store.ErrEnded, store.ErrNotSubscribed, store.ErrDelivered} {
			if i != j && errors.Is(a, b) {
				t.Fatal("overlapping sentinels")
			}
		}
	}
	for _, s := range []string{"repo.pushed", "crm.contact_added", "a1.b_2c", "repo.git.pushed", "repo.*", "*.*", "*.pushed", "cron.*.fired"} {
		equal(t, store.ValidEvent(s), true)
	}
	for _, s := range []string{"*", "Repo.Pushed", "pushed", ".pushed", "repo.", "repo.2fa", "repo._pushed", "repo.pushed_", "repo.push__ed", "repo..pushed", "repo.pushed.", "re*po.pushed", "repo.push*", "repo.**", "repo-x.pushed", " repo.pushed", "repo.pushed ", "repo.pushed\n", ""} {
		equal(t, store.ValidEvent(s), false)
	}
}

// R-R2IQ-UVYZ R-G2N4-WX5V R-R8M8-RQOG R-G52X-OGN9 R-RH5J-G4VB R-RJLC-7OCP R-G8QM-TRVC R-RPOU-4J26 R-RUKF-NM0Y
func TestSubscriptionsLifecycle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.db")
	now := stamp
	s := configured(t, path, store.Config{Now: func() time.Time { return now }, Rand: &sequence{}})
	a := create(t, s, "alice", "alpha")
	b := create(t, s, "bob", "beta")
	equal(t, a.Subscriptions, []store.Subscription{})
	eventRun := run(a, 1)
	eventRun.Trigger, eventRun.Event = store.TriggerEvent, "existing-event"
	r := add(t, s, eventRun)
	assertDelivered := func() {
		t.Helper()
		delivered, err := s.Delivered(ctx, a.ID, eventRun.Event)
		must(t, err)
		equal(t, delivered, true)
	}
	assertDelivered()
	a, err := s.Subscribe(ctx, a.ID, "repo.pushed")
	must(t, err)
	first := store.Subscription{Event: "repo.pushed", Created: stamp.UTC().Truncate(time.Second)}
	equal(t, a.Subscriptions, []store.Subscription{first})
	assertDelivered()
	equal(t, *a.Last, r)
	now = stamp.Add(time.Hour)
	a, err = s.Subscribe(ctx, a.ID, "repo.pushed")
	must(t, err)
	equal(t, a.Subscriptions, []store.Subscription{first})
	a, err = s.Subscribe(ctx, a.ID, "crm.contact_added")
	must(t, err)
	equal(t, a.Subscriptions[0].Event, "crm.contact_added")
	_, err = s.Subscribe(ctx, b.ID, "repo.pushed")
	must(t, err)
	subs, err := s.Subscribers(ctx, "repo.pushed")
	must(t, err)
	equal(t, len(subs), 2)
	equal(t, subs[0].ID, a.ID)
	equal(t, *subs[0].Last, r)
	equal(t, subs[0].Subscriptions, []store.Subscription{
		{Event: "crm.contact_added", Created: now.UTC().Truncate(time.Second)}, first,
	})
	equal(t, subs[1].Subscriptions, []store.Subscription{
		{Event: "repo.pushed", Created: now.UTC().Truncate(time.Second)},
	})
	for _, name := range []string{"repo.created", "repo.pushed_x", "repo", "repo.*", "Repo.Pushed"} {
		got, err := s.Subscribers(ctx, name)
		must(t, err)
		equal(t, got, []store.Script{})
	}
	expected := a.Subscriptions
	add(t, s, run(a, 2))
	a, err = s.Find(ctx, a.Owner, a.Name)
	must(t, err)
	equal(t, a.Subscriptions, expected)
	a, _, err = s.SetRef(ctx, a.ID, "next")
	must(t, err)
	equal(t, a.Subscriptions, expected)
	_, err = s.FinishRun(ctx, r.ID, store.Ending{Status: store.StatusExited, Finished: stamp})
	must(t, err)
	must(t, s.DeleteRun(ctx, r.ID))
	create(t, s, "alice", "gamma")
	a, err = s.Find(ctx, "alice", "alpha")
	must(t, err)
	equal(t, a.Subscriptions, expected)
	a, err = s.Unsubscribe(ctx, a.ID, "repo.pushed")
	must(t, err)
	equal(t, len(a.Subscriptions), 1)
	assertDelivered()
	a, err = s.Subscribe(ctx, a.ID, "repo.pushed")
	must(t, err)
	equal(t, a.Subscriptions[1].Created, now.UTC().Truncate(time.Second))
	assertDelivered()
	before := content(t, s)
	must(t, closeStore(s))
	s = open(t, path)
	equal(t, content(t, s), before)
	a, err = s.Find(ctx, "alice", "alpha")
	must(t, err)
	equal(t, len(a.Subscriptions), 2)
	must(t, s.Delete(ctx, a.ID))
	got, err := s.Subscribers(ctx, "repo.pushed")
	must(t, err)
	equal(t, len(got), 1)
	equal(t, got[0].ID, b.ID)
}

// R-G2N4-WX5V R-G52X-OGN9 R-RH5J-G4VB R-RJLC-7OCP R-RUKF-NM0Y
func TestPatternSubscriptionsLifecycle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.db")
	now := stamp
	s := configured(t, path, store.Config{Now: func() time.Time { return now }, Rand: &sequence{}})
	a := create(t, s, "alice", "alpha")
	r := add(t, s, run(a, 1))
	wildcard := store.Subscription{Event: "cron.*.fired", Created: now.UTC().Truncate(time.Second)}
	a, err := s.Subscribe(ctx, a.ID, wildcard.Event)
	must(t, err)
	equal(t, a.Subscriptions, []store.Subscription{wildcard})
	equal(t, *a.Last, r)
	now = stamp.Add(time.Hour)
	a, err = s.Subscribe(ctx, a.ID, wildcard.Event)
	must(t, err)
	equal(t, a.Subscriptions, []store.Subscription{wildcard})
	exact := store.Subscription{Event: "cron.hourly.fired", Created: now.UTC().Truncate(time.Second)}
	a, err = s.Subscribe(ctx, a.ID, exact.Event)
	must(t, err)
	expected := []store.Subscription{wildcard, exact}
	equal(t, a.Subscriptions, expected)
	check := func(s *store.Store) {
		t.Helper()
		found, err := s.Find(ctx, a.Owner, a.Name)
		must(t, err)
		equal(t, found.Subscriptions, expected)
		equal(t, *found.Last, r)
		listed, err := s.List(ctx, a.Owner)
		must(t, err)
		equal(t, listed, []store.Script{found})
		subs, err := s.Subscribers(ctx, "cron.hourly.fired")
		must(t, err)
		if len(expected) == 0 {
			equal(t, subs, []store.Script{})
		} else {
			equal(t, subs, listed)
		}
	}
	check(s)
	a, _, err = s.SetRef(ctx, a.ID, "next")
	must(t, err)
	equal(t, a.Subscriptions, expected)
	must(t, closeStore(s))
	s = configured(t, path, store.Config{Now: func() time.Time { return now }, Rand: &sequence{}})
	check(s)
	a, err = s.Unsubscribe(ctx, a.ID, wildcard.Event)
	must(t, err)
	equal(t, a.Subscriptions, []store.Subscription{exact})
	equal(t, *a.Last, r)
	now = stamp.Add(2 * time.Hour)
	a, err = s.Subscribe(ctx, a.ID, wildcard.Event)
	must(t, err)
	wildcard.Created = now.UTC().Truncate(time.Second)
	equal(t, a.Subscriptions, []store.Subscription{wildcard, exact})
	a, err = s.Unsubscribe(ctx, a.ID, exact.Event)
	must(t, err)
	equal(t, a.Subscriptions, []store.Subscription{wildcard})
	a, err = s.Unsubscribe(ctx, a.ID, wildcard.Event)
	must(t, err)
	equal(t, a.Subscriptions, []store.Subscription{})
	equal(t, *a.Last, r)
	must(t, closeStore(s))
	s = open(t, path)
	expected = []store.Subscription{}
	check(s)
}

// R-G8QM-TRVC R-G52X-OGN9
func TestSubscriberPatterns(t *testing.T) {
	s := open(t, "")
	a := create(t, s, "alice", "alpha")
	b := create(t, s, "bob", "beta")
	c := create(t, s, "carol", "gamma")
	d := create(t, s, "alice", "delta")
	for _, sub := range []struct{ id, pattern string }{
		{a.ID, "cron.*.fired"}, {a.ID, "cron.hourly.fired"},
		{b.ID, "*.*"}, {c.ID, "repo.pushed"}, {d.ID, "crm.contact_added"},
	} {
		_, err := s.Subscribe(ctx, sub.id, sub.pattern)
		must(t, err)
	}
	a, err := s.Find(ctx, a.Owner, a.Name)
	must(t, err)
	b, err = s.Find(ctx, b.Owner, b.Name)
	must(t, err)
	c, err = s.Find(ctx, c.Owner, c.Name)
	must(t, err)
	d, err = s.Find(ctx, d.Owner, d.Name)
	must(t, err)
	before := content(t, s)
	for _, tc := range []struct {
		event string
		want  []store.Script
	}{
		{"cron.hourly.fired", []store.Script{a}},
		{"cron.tick.fired", []store.Script{a}},
		{"cron.fired", []store.Script{b}},
		{"cron.a.b.fired", []store.Script{}},
		{"cron.Hourly.fired", []store.Script{}},
		{"repo.pushed", []store.Script{b, c}},
		{"repo.created", []store.Script{b}},
		{"repo.pushed_x", []store.Script{b}},
		{"repo", []store.Script{}},
		{"repo.*", []store.Script{}},
		{"repo.git.pushed", []store.Script{}},
		{"Repo.Pushed", []store.Script{}},
		{"crm.contact_added", []store.Script{b, d}},
		{"crm.contact1added", []store.Script{b}},
		{"crm.contact_added\n", []store.Script{}},
		{"", []store.Script{}},
	} {
		t.Run(tc.event, func(t *testing.T) {
			got, err := s.Subscribers(ctx, tc.event)
			must(t, err)
			equal(t, got, tc.want)
			equal(t, content(t, s), before)
		})
	}
}

// R-G7IQ-G04N
func TestUnsubscribeComparesPatternsExactly(t *testing.T) {
	s := open(t, "")
	a := create(t, s, "alice", "alpha")
	b := create(t, s, "bob", "beta")
	_, err := s.Subscribe(ctx, a.ID, "cron.*.fired")
	must(t, err)
	_, err = s.Subscribe(ctx, b.ID, "cron.hourly.fired")
	must(t, err)
	before := content(t, s)
	for _, tc := range []struct{ id, pattern string }{
		{a.ID, "cron.hourly.fired"}, {b.ID, "cron.*.fired"},
	} {
		got, err := s.Unsubscribe(ctx, tc.id, tc.pattern)
		equal(t, got, store.Script{})
		equal(t, errors.Is(err, store.ErrNotSubscribed), true)
		equal(t, content(t, s), before)
	}
}

// R-RIDF-TWM0 R-G7IQ-G04N R-G9YJ-7JM1 R-R9U5-5IF5
func TestSubscriptionRefusals(t *testing.T) {
	s := open(t, "")
	a := create(t, s, "alice", "alpha")
	b := create(t, s, "bob", "beta")
	_, err := s.Subscribe(ctx, b.ID, "repo.pushed")
	must(t, err)
	before := content(t, s)
	for _, id := range []string{a.ID, "absent"} {
		for _, event := range []string{"invalid", "repo.pushed"} {
			got, err := s.Unsubscribe(ctx, id, event)
			equal(t, got, store.Script{})
			switch {
			case event == "invalid":
				catalog(t, err)
			case id == "absent":
				equal(t, errors.Is(err, store.ErrNotFound), true)
			default:
				equal(t, errors.Is(err, store.ErrNotSubscribed), true)
			}
			equal(t, content(t, s), before)
		}
	}
	for _, event := range []string{"invalid", "repo.pushed"} {
		got, err := s.Subscribe(ctx, "absent", event)
		equal(t, got, store.Script{})
		if event == "invalid" {
			catalog(t, err)
		} else {
			equal(t, errors.Is(err, store.ErrNotFound), true)
		}
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	calls := func(c context.Context) []error {
		_, a := s.Subscribe(c, "absent", "invalid")
		_, b := s.Unsubscribe(c, "absent", "invalid")
		_, d := s.Subscribers(c, "repo.pushed")
		_, e := s.Delivered(c, "absent", "event")
		return []error{a, b, d, e}
	}
	for _, err := range calls(cancelled) {
		equal(t, errors.Is(err, context.Canceled), true)
	}
	handle(s).SetFailing(true)
	for _, err := range calls(ctx) {
		catalog(t, err)
	}
	for _, err := range calls(cancelled) {
		equal(t, errors.Is(err, context.Canceled), true)
	}
	handle(s).SetFailing(false)
	equal(t, content(t, s), before)
}

// R-R4YJ-MFGD R-RC9X-X1WJ R-RQWQ-IASV R-RS4M-W2JK R-90R8-PFCQ
func TestEventRunMemory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog.db")
	s := open(t, path)
	a := create(t, s, "alice", "alpha")
	b := create(t, s, "bob", "beta")
	for _, bad := range []store.Run{func() store.Run { r := run(a, 1); r.Event = "event"; return r }(), func() store.Run { r := run(a, 1); r.Trigger = store.TriggerEvent; return r }()} {
		_, err := s.AddRun(ctx, bad)
		catalog(t, err)
	}
	r := run(a, 1)
	r.Trigger = store.TriggerEvent
	r.Event = "event"
	r = add(t, s, r)
	delivered, err := s.Delivered(ctx, a.ID, "event")
	must(t, err)
	equal(t, delivered, true)
	for _, id := range []string{b.ID, "absent"} {
		got, err := s.Delivered(ctx, id, "event")
		must(t, err)
		equal(t, got, false)
	}
	duplicate := run(a, 2)
	duplicate.Trigger = store.TriggerEvent
	duplicate.Event = "event"
	before := content(t, s)
	got, err := s.AddRun(ctx, duplicate)
	equal(t, got, store.Run{})
	equal(t, errors.Is(err, store.ErrDelivered), true)
	equal(t, content(t, s), before)
	ended, err := s.FinishRun(ctx, r.ID, store.Ending{Status: store.StatusExited, Finished: stamp})
	must(t, err)
	equal(t, ended.Event, r.Event)
	equal(t, ended.Trigger, store.TriggerEvent)
	a, err = s.Find(ctx, a.Owner, a.Name)
	must(t, err)
	equal(t, *a.Last, ended)
	must(t, s.DeleteRun(ctx, r.ID))
	must(t, closeStore(s))
	s = open(t, path)
	delivered, err = s.Delivered(ctx, a.ID, "event")
	must(t, err)
	equal(t, delivered, true)
	_, err = s.AddRun(ctx, duplicate)
	equal(t, errors.Is(err, store.ErrDelivered), true)
	must(t, s.Delete(ctx, a.ID))
	delivered, err = s.Delivered(ctx, a.ID, "event")
	must(t, err)
	equal(t, delivered, false)
}

// R-RM14-Z7U3 R-RTCJ-9UA9
func TestConcurrentEventWrites(t *testing.T) {
	s := open(t, "")
	a := create(t, s, "alice", "alpha")
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Go(func() { _, err := s.Subscribe(ctx, a.ID, "repo.pushed"); errs <- err })
	}
	wg.Wait()
	for range 8 {
		must(t, <-errs)
	}
	a, err := s.Find(ctx, a.Owner, a.Name)
	must(t, err)
	equal(t, len(a.Subscriptions), 1)
	for i := range 8 {
		wg.Go(func() {
			r := run(a, i+1)
			r.Trigger = store.TriggerEvent
			r.Event = "event"
			_, err := s.AddRun(ctx, r)
			errs <- err
		})
	}
	wg.Wait()
	successes := 0
	for range 8 {
		err := <-errs
		if err == nil {
			successes++
		} else {
			equal(t, errors.Is(err, store.ErrDelivered), true)
		}
	}
	equal(t, successes, 1)
	rr, err := s.Runs(ctx, a.ID)
	must(t, err)
	equal(t, len(rr), 1)
}
