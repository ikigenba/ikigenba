package store_test

import (
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	event "github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/events/internal/store"
)

func TestDeclarationReplacementAndRows(t *testing.T) {
	// R-YWES-U633 R-D21W-AB0X R-D39S-O2RM R-D4HP-1UIB R-YGK3-V5G2
	now := instant
	s, d, _ := openStore(t, store.Config{Now: func() time.Time { return now }})
	declare(t, s, "other")
	declare(t, s, "producer")
	must(t, s.Deliver(ctx, emit(1)))
	originalRecords := all(t, s)
	originalDeclarations, err := s.Declarations(ctx)
	must(t, err)
	first := store.Declaration{Emits: []event.Emission{{Event: "item.created", Attrs: []string{"z", "a", "z"}}, {Event: "item.created"}}, Accepts: []string{"item.changed", "*", "item.changed"}}
	must(t, s.Declare(ctx, "test", first))
	ds, err := s.Declarations(ctx)
	must(t, err)
	if !reflect.DeepEqual(all(t, s), originalRecords) || !equalDecl(ds["producer"], originalDeclarations["producer"]) || !equalDecl(ds["other"], originalDeclarations["other"]) {
		t.Fatal("Declare changed other state")
	}
	if !equalDecl(ds["test"], first) {
		t.Fatal(ds)
	}
	now = now.Add(time.Second)
	second := store.Declaration{Emits: []event.Emission{{Event: "item.changed"}}, Accepts: []string{}}
	must(t, s.Declare(ctx, "test", second))
	ds, err = s.Declarations(ctx)
	must(t, err)
	if !equalDecl(ds["test"], second) || len(ds) != 3 || !reflect.DeepEqual(all(t, s), originalRecords) || !equalDecl(ds["producer"], originalDeclarations["producer"]) || !equalDecl(ds["other"], originalDeclarations["other"]) {
		t.Fatal(ds)
	}
	must(t, d.Read(ctx, func(tx *sql.Tx) error {
		var emits, accepts string
		var asked int64
		if err := tx.QueryRow("SELECT emits,accepts,asked FROM declarations WHERE service='test'").Scan(&emits, &accepts, &asked); err != nil {
			return err
		}
		if emits != `[{"event":"item.changed","attrs":[]}]` || accepts != "[]" || asked != now.UTC().Truncate(time.Microsecond).UnixMicro() {
			t.Fatal(emits, accepts, asked)
		}
		var objs []map[string]json.RawMessage
		must(t, json.Unmarshal([]byte(emits), &objs))
		if len(objs[0]) != 2 {
			t.Fatal(objs)
		}
		return nil
	}))
	must(t, s.Forget(ctx, "test"))
	ds, err = s.Declarations(ctx)
	must(t, err)
	if !reflect.DeepEqual(ds, originalDeclarations) || !reflect.DeepEqual(all(t, s), originalRecords) {
		t.Fatal("Forget changed other state")
	}
	beforeSubscribers := subs(t, s)
	beforeHead, err := s.Head(ctx)
	must(t, err)
	ch := s.Changed()
	must(t, s.Forget(ctx, "never"))
	ds, err = s.Declarations(ctx)
	must(t, err)
	afterHead, err := s.Head(ctx)
	must(t, err)
	if !reflect.DeepEqual(ds, originalDeclarations) || !reflect.DeepEqual(all(t, s), originalRecords) || !reflect.DeepEqual(subs(t, s), beforeSubscribers) || afterHead != beforeHead {
		t.Fatal("Forget absent changed state")
	}
	assertOpen(t, ch)
}
func equalDecl(a, b store.Declaration) bool {
	if len(a.Emits) != len(b.Emits) || len(a.Accepts) != len(b.Accepts) {
		return false
	}
	for i, e := range a.Emits {
		if e.Event != b.Emits[i].Event || len(e.Attrs) != len(b.Emits[i].Attrs) {
			return false
		}
		for j, k := range e.Attrs {
			if k != b.Emits[i].Attrs[j] {
				return false
			}
		}
	}
	for i, x := range a.Accepts {
		if x != b.Accepts[i] {
			return false
		}
	}
	return true
}

func TestSubscriberLifecycle(t *testing.T) {
	// R-YNVI-5RW8 R-2SYA-X13P R-DKCE-0V5C R-YQBA-XBDM R-YSR3-OUV0 R-YTZ0-2MLP R-2VE3-OKL3 R-DVBH-GSTL R-DWJD-UKKA R-DXRA-8CAZ R-DYZ6-M41O R-E072-ZVSD R-DSVO-P9C7 R-E1EZ-DNJ2
	now := instant
	s, _, _ := openStore(t, store.Config{Now: func() time.Time { return now }})
	declare(t, s, "producer")
	must(t, s.Deliver(ctx, emit(1)))
	ch := s.Changed()
	declare(t, s, "reader", "*")
	assertClosed(t, ch)
	first := one(t, s, "reader")
	if first.Status != store.StatusOK || first.Cursor != 1 || first.Lag != 0 || first.Since != now.UTC().Truncate(time.Microsecond) || first.Reason != nil {
		t.Fatal(first)
	}
	e, ok, err := s.Next(ctx, "reader")
	must(t, err)
	if ok || !reflect.DeepEqual(e, event.Event{}) {
		t.Fatal(e)
	}
	now = now.Add(time.Second)
	ch = s.Changed()
	declare(t, s, "reader", "item.changed")
	assertOpen(t, ch)
	if !reflect.DeepEqual(first, one(t, s, "reader")) {
		t.Fatal("active refresh changed subscriber")
	}
	must(t, s.Deliver(ctx, emit(2)))
	ch = s.Changed()
	must(t, s.Advance(ctx, "reader", 0))
	assertOpen(t, ch)
	ch = s.Changed()
	must(t, s.Pause(ctx, "reader", 2, "failure"))
	assertClosed(t, ch)
	paused := one(t, s, "reader")
	if paused.Status != store.StatusPaused || paused.Cursor != 1 || paused.Lag != 1 || paused.Since != now.UTC().Truncate(time.Microsecond) || *paused.Reason != (store.Reason{emit(2).ID, "item.created", 2, "failure"}) {
		t.Fatal(paused)
	}
	now = now.Add(time.Second)
	ch = s.Changed()
	declare(t, s, "reader", "*")
	must(t, s.Advance(ctx, "reader", 3))
	must(t, s.Pause(ctx, "reader", 1, "ignored"))
	e, ok, err = s.Next(ctx, "reader")
	must(t, err)
	if ok || !reflect.DeepEqual(e, event.Event{}) {
		t.Fatal(e)
	}
	assertOpen(t, ch)
	if !reflect.DeepEqual(paused, one(t, s, "reader")) {
		t.Fatal("pause changed")
	}
	ch = s.Changed()
	resumed, err := s.Resume(ctx, "reader")
	must(t, err)
	assertClosed(t, ch)
	if resumed.Status != store.StatusOK || resumed.Cursor != 1 || resumed.Reason != nil || resumed.Since != now.UTC().Truncate(time.Microsecond) || !reflect.DeepEqual(resumed, one(t, s, "reader")) {
		t.Fatal(resumed)
	}
	err = s.Pause(ctx, "reader", 999, "missing")
	if err == nil || sentinel(err) {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(resumed, one(t, s, "reader")) {
		t.Fatal("missing pause changed")
	}
	must(t, s.Pause(ctx, "reader", 2, "again"))
	now = now.Add(time.Second)
	ch = s.Changed()
	skipped, reason, err := s.Skip(ctx, "reader")
	must(t, err)
	assertClosed(t, ch)
	if reason != (store.Reason{emit(2).ID, "item.created", 2, "again"}) || skipped.Status != store.StatusOK || skipped.Cursor != 2 || skipped.Reason != nil || skipped.Since != now.UTC().Truncate(time.Microsecond) || !reflect.DeepEqual(skipped, one(t, s, "reader")) {
		t.Fatal(skipped, reason)
	}
	_, _, err = s.Skip(ctx, "reader")
	if !errors.Is(err, store.ErrNotPaused) {
		t.Fatal(err)
	}
	_, err = s.Resume(ctx, "reader")
	if !errors.Is(err, store.ErrNotPaused) {
		t.Fatal(err)
	}
	must(t, s.Advance(ctx, "reader", 3))
	advanced := one(t, s, "reader")
	if advanced.Cursor != 3 || advanced.Lag != 0 || advanced.Since != skipped.Since {
		t.Fatal(advanced)
	}
	now = now.Add(time.Second)
	ch = s.Changed()
	declare(t, s, "reader")
	assertClosed(t, ch)
	gone := one(t, s, "reader")
	if gone.Status != store.StatusGone || gone.Cursor != 3 || gone.Since != now.UTC().Truncate(time.Microsecond) || gone.Reason != nil {
		t.Fatal(gone)
	}
	now = now.Add(time.Second)
	ch = s.Changed()
	declare(t, s, "reader")
	must(t, s.Forget(ctx, "reader"))
	must(t, s.Advance(ctx, "reader", 5))
	must(t, s.Pause(ctx, "reader", 1, "ignored"))
	e, ok, err = s.Next(ctx, "reader")
	must(t, err)
	if ok || !reflect.DeepEqual(e, event.Event{}) {
		t.Fatal(e)
	}
	assertOpen(t, ch)
	if !reflect.DeepEqual(gone, one(t, s, "reader")) {
		t.Fatal("gone changed")
	}
	_, _, err = s.Skip(ctx, "reader")
	if !errors.Is(err, store.ErrNotPaused) {
		t.Fatal(err)
	}
	_, err = s.Resume(ctx, "reader")
	if !errors.Is(err, store.ErrNotPaused) {
		t.Fatal(err)
	}
	for _, fn := range []func() error{func() error { return s.Advance(ctx, "missing", 1) }, func() error { return s.Pause(ctx, "missing", 1, "failure") }} {
		if !errors.Is(fn(), store.ErrNoSubscriber) {
			t.Fatal("missing subscriber")
		}
	}
	declare(t, s, "z", "*")
	declare(t, s, "a", "*")
	_ = one(t, s, "z")
	v := subs(t, s)
	if len(v) != 3 || v[0].Service != "a" || v[1].Service != "reader" || v[2].Service != "z" {
		t.Fatal(v)
	}
}
func sentinel(err error) bool {
	for _, e := range []error{store.ErrUndeclared, store.ErrTooDeep, store.ErrNoSubscriber, store.ErrNotPaused, store.ErrCursor, event.ErrRejected} {
		if errors.Is(err, e) {
			return true
		}
	}
	return false
}

func TestNextSnapshotAndPassingUnaccepted(t *testing.T) {
	// R-65N8-3FUK R-66V4-H7L9 R-THIC-XAJO R-TIQ9-B2AD
	scans := 0
	insert := false
	var s *store.Store
	s, _, _ = openStore(t, store.Config{Scanned: func(service string) {
		scans++
		if service != "reader" {
			t.Fatal(service)
		}
		if insert {
			insert = false
			must(t, s.Deliver(ctx, emit(4)))
		}
	}})
	declare(t, s, "producer")
	declare(t, s, "reader", "item.created")
	a := emit(1)
	a.Name = "item.changed"
	must(t, s.Deliver(ctx, a))
	b := emit(2)
	b.Name = "item.changed"
	must(t, s.Deliver(ctx, b))
	must(t, s.Deliver(ctx, emit(3)))
	e, ok, err := s.Next(ctx, "reader")
	must(t, err)
	if !ok || e.Seq != 3 || one(t, s, "reader").Cursor != 2 || scans != 1 {
		t.Fatal(e, scans)
	}
	must(t, s.Advance(ctx, "reader", 3))
	insert = true
	e, ok, err = s.Next(ctx, "reader")
	must(t, err)
	if ok || !reflect.DeepEqual(e, event.Event{}) || one(t, s, "reader").Cursor != 3 || scans != 2 {
		t.Fatal(e, scans)
	}
	e, ok, err = s.Next(ctx, "reader")
	must(t, err)
	if !ok || e.Seq != 4 || scans != 3 {
		t.Fatal(e, scans)
	}
	must(t, s.Advance(ctx, "reader", 4))
	e, ok, err = s.Next(ctx, "reader")
	must(t, err)
	if ok || one(t, s, "reader").Cursor != 4 {
		t.Fatal(e)
	}
	must(t, s.Pause(ctx, "reader", 4, "failure"))
	_, _, err = s.Next(ctx, "reader")
	must(t, err)
	if scans != 4 {
		t.Fatal(scans)
	}
	must(t, s.Forget(ctx, "reader"))
	_, _, err = s.Next(ctx, "reader")
	must(t, err)
	if scans != 4 {
		t.Fatal(scans)
	}
}

func TestReturningSubscriberRetainsOrResetsCursor(t *testing.T) {
	// R-YRJ7-B34B R-Z65Z-WC0N
	now := instant
	s, _, _ := openStore(t, store.Config{Now: func() time.Time { return now }})
	declare(t, s, "producer")
	declare(t, s, "reader", "*")
	must(t, s.Deliver(ctx, emit(1)))
	must(t, s.Deliver(ctx, emit(2)))
	must(t, s.Advance(ctx, "reader", 1))
	must(t, s.Forget(ctx, "reader"))
	now = now.Add(time.Second)
	declare(t, s, "reader", "*")
	sub := one(t, s, "reader")
	if sub.Status != store.StatusOK || sub.Cursor != 1 || sub.Since != now.UTC().Truncate(time.Microsecond) {
		t.Fatal(sub)
	}
	must(t, s.Forget(ctx, "reader"))
	must(t, s.Sweep(ctx, now))
	declare(t, s, "reader", "*")
	sub = one(t, s, "reader")
	if sub.Cursor != 2 || sub.Status != store.StatusOK {
		t.Fatal(sub)
	}
}

func TestReturningSubscriberCursorBeyondHead(t *testing.T) {
	// R-YRJ7-B34B
	s, _, _ := openStore(t, store.Config{})
	declare(t, s, "reader", "*")
	must(t, s.Advance(ctx, "reader", 7))
	must(t, s.Forget(ctx, "reader"))
	declare(t, s, "reader", "*")
	sub := one(t, s, "reader")
	if sub.Cursor != 7 || sub.Status != store.StatusOK || sub.Lag != 0 {
		t.Fatal("empty retained interval moved cursor", sub)
	}
}

func TestNotificationsPausedGoneAndRefusalZeros(t *testing.T) {
	// R-E1EZ-DNJ2 R-YTZ0-2MLP R-E072-ZVSD
	now := instant
	s, _, _ := openStore(t, store.Config{Now: func() time.Time { return now }})
	declare(t, s, "producer")
	declare(t, s, "reader", "*")
	must(t, s.Deliver(ctx, emit(1)))
	refuse := func() {
		t.Helper()
		sub, reason, err := s.Skip(ctx, "reader")
		if !errors.Is(err, store.ErrNotPaused) || !reflect.DeepEqual(sub, store.Subscriber{}) || reason != (store.Reason{}) {
			t.Fatal(sub, reason, err)
		}
		sub, err = s.Resume(ctx, "reader")
		if !errors.Is(err, store.ErrNotPaused) || !reflect.DeepEqual(sub, store.Subscriber{}) {
			t.Fatal(sub, err)
		}
	}
	refuse()
	ch := s.Changed()
	must(t, s.Advance(ctx, "reader", 1))
	assertClosed(t, ch)
	ch = s.Changed()
	must(t, s.Forget(ctx, "reader"))
	assertClosed(t, ch)
	refuse()
	for _, forget := range []bool{false, true} {
		declare(t, s, "reader", "*")
		must(t, s.Pause(ctx, "reader", 1, "failure"))
		paused := one(t, s, "reader")
		now = now.Add(time.Second)
		ch = s.Changed()
		if forget {
			must(t, s.Forget(ctx, "reader"))
		} else {
			declare(t, s, "reader")
		}
		assertClosed(t, ch)
		gone := one(t, s, "reader")
		if gone.Status != store.StatusGone || gone.Cursor != paused.Cursor || gone.Reason != nil || gone.Since != now.UTC().Truncate(time.Microsecond) {
			t.Fatal("paused-to-gone state", gone)
		}
		refuse()
	}
}
