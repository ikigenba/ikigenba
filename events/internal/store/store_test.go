package store_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	event "github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	root "github.com/ikigenba/ikigenba/events"
	"github.com/ikigenba/ikigenba/events/internal/store"
)

var ctx = context.Background()
var instant = time.Date(2025, 3, 4, 5, 6, 7, 123456789, time.FixedZone("offset", 3600))

func openStore(t *testing.T, cfg store.Config) (*store.Store, *db.DB, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "state", "events.db")
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return instant }
	}
	d, err := db.Open(ctx, db.Config{Path: path, Migrations: root.Migrations(), Now: cfg.Now})
	must(t, err)
	t.Cleanup(func() { must(t, d.Close()) })
	return store.New(d, cfg), d, path
}
func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func emit(n int) event.Event {
	return event.Event{ID: fmt.Sprintf("evt_%016x", n), Time: instant, Service: "producer", Name: "item.created", RequestID: "request", User: "alice", Attrs: event.Attrs{"count": int64(9007199254740993), "flag": true, "text": "value"}}
}
func declare(t *testing.T, s *store.Store, service string, accepts ...string) {
	t.Helper()
	must(t, s.Declare(ctx, service, store.Declaration{Emits: []event.Emission{{Event: "item.created", Attrs: []string{"count", "flag", "text"}}, {Event: "item.changed"}}, Accepts: accepts}))
}
func all(t *testing.T, s *store.Store) []event.Event {
	t.Helper()
	p, err := s.Search(ctx, store.Filter{}, 10000, "")
	must(t, err)
	return p.Records
}
func subs(t *testing.T, s *store.Store) []store.Subscriber {
	t.Helper()
	v, err := s.Subscribers(ctx)
	must(t, err)
	return v
}
func one(t *testing.T, s *store.Store, service string) store.Subscriber {
	t.Helper()
	for _, sub := range subs(t, s) {
		if sub.Service == service {
			return sub
		}
	}
	t.Fatalf("missing %s", service)
	return store.Subscriber{}
}
func assertOpen(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
		t.Fatal("notification unexpectedly closed")
	default:
	}
}
func assertClosed(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	default:
		t.Fatal("missing notification")
	}
}

func TestPublicContract(t *testing.T) {
	// R-TGAG-JISZ R-CIJI-5Z5T R-CJRE-JQWI R-CKZA-XIN7 R-CM77-BADW R-CNF3-P24L R-CON0-2TVA R-CPUW-GLLZ R-CSAP-853D
	s, _, _ := openStore(t, store.Config{func() time.Time { return instant }, 3, nil, nil, nil, nil})
	var sink event.Sink = s
	if !errors.Is(sink.Deliver(ctx, event.Event{}), event.ErrRejected) {
		t.Fatal("sink rejection")
	}
	if store.SweepBatch != 500 {
		t.Fatal(store.SweepBatch)
	}
	_ = store.Declaration{[]event.Emission{}, []string{}}
	r := store.Reason{"id", "item.created", 1, "failure"}
	_ = store.Subscriber{"service", store.StatusOK, 0, 1, instant, &r}
	_ = store.Producer{"service", []string{}}
	_ = store.CatalogEntry{"item.created", []store.Producer{}, []string{}, 0, time.Time{}}
	_ = store.Filter{nil, nil, []string{}, []string{}, nil, nil, nil, event.Attrs{}}
	_ = store.Page{[]event.Event{}, store.Cursor("")}
	if store.StatusOK != "ok" || store.StatusPaused != "paused" || store.StatusGone != "gone" {
		t.Fatal("status constants")
	}
	_ = s.Changed()
	must(t, s.Declare(ctx, "test", store.Declaration{}))
	must(t, s.Forget(ctx, "test"))
	_, err := s.Declarations(ctx)
	must(t, err)
	_, err = s.Subscribers(ctx)
	must(t, err)
	_, err = s.Head(ctx)
	must(t, err)
	_, err = s.Catalog(ctx, "", "")
	must(t, err)
	_, err = s.Search(ctx, store.Filter{}, 1, "")
	must(t, err)
	if !errors.Is(s.Advance(ctx, "missing", 1), store.ErrNoSubscriber) || !errors.Is(s.Pause(ctx, "missing", 1, "failure"), store.ErrNoSubscriber) {
		t.Fatal("missing subscriber")
	}
	must(t, s.Sweep(ctx, instant))
}

func TestErrorsAndEmptyReads(t *testing.T) {
	// R-CJRE-JQWI R-D21W-AB0X R-DKCE-0V5C R-7HW2-YLGD R-E8QD-O9Z8 R-E072-ZVSD R-DSVO-P9C7
	s, _, _ := openStore(t, store.Config{})
	sentinels := []error{store.ErrUndeclared, store.ErrTooDeep, store.ErrNoSubscriber, store.ErrNotPaused, store.ErrCursor}
	for i, a := range sentinels {
		if a == nil {
			t.Fatal("nil sentinel")
		}
		for j, b := range sentinels {
			if i != j && errors.Is(a, b) {
				t.Fatalf("sentinels overlap %d %d", i, j)
			}
		}
		if errors.Is(a, event.ErrRejected) != (i < 2) {
			t.Fatal("rejection wrapping")
		}
	}
	ds, err := s.Declarations(ctx)
	must(t, err)
	if ds == nil || len(ds) != 0 {
		t.Fatal(ds)
	}
	if v := subs(t, s); v == nil || len(v) != 0 {
		t.Fatal(v)
	}
	catalog, err := s.Catalog(ctx, "", "")
	must(t, err)
	if catalog == nil || len(catalog) != 0 {
		t.Fatal(catalog)
	}
	p, err := s.Search(ctx, store.Filter{}, 1, "")
	must(t, err)
	if p.Records == nil || len(p.Records) != 0 || p.Next != "" {
		t.Fatal(p)
	}
	for _, service := range []string{"", "missing", "never"} {
		if service == "never" {
			declare(t, s, service)
		}
		sub, r, err := s.Skip(ctx, service)
		if !errors.Is(err, store.ErrNoSubscriber) || !reflect.DeepEqual(sub, store.Subscriber{}) || r != (store.Reason{}) {
			t.Fatal(sub, r, err)
		}
		sub, err = s.Resume(ctx, service)
		if !errors.Is(err, store.ErrNoSubscriber) || !reflect.DeepEqual(sub, store.Subscriber{}) {
			t.Fatal(sub, err)
		}
		e, ok, err := s.Next(ctx, service)
		if !errors.Is(err, store.ErrNoSubscriber) || ok || !reflect.DeepEqual(e, event.Event{}) {
			t.Fatal(e, ok, err)
		}
	}
}

func TestIngestStoredFormAndSchemaRows(t *testing.T) {
	// R-7BSL-1QQW R-3794-58R6 R-39OW-WS8K R-3617-RH0H R-6E6I-RU1F R-PNJD-XS7X
	s, d, _ := openStore(t, store.Config{DepthMax: 10})
	declare(t, s, "producer")
	declare(t, s, "consumer", "*")
	h, err := s.Head(ctx)
	must(t, err)
	if h != 0 {
		t.Fatal(h)
	}
	e := emit(1)
	e.Cause = "evt_00000000000000ff"
	e.Depth = 3
	e.Time = time.Date(2000, 1, 1, 0, 0, 0, 999999999, time.UTC)
	must(t, s.Deliver(ctx, e))
	records := all(t, s)
	if len(records) != 1 {
		t.Fatal(records)
	}
	expected := e
	expected.Seq = 1
	expected.Time = e.Time.UTC().Truncate(time.Microsecond)
	expected.Received = instant.UTC().Truncate(time.Microsecond)
	want, err := expected.MarshalJSON()
	must(t, err)
	got, err := records[0].MarshalJSON()
	must(t, err)
	if !bytes.Equal(got, want) {
		t.Fatalf("%s != %s", got, want)
	}
	next, ok, err := s.Next(ctx, "consumer")
	must(t, err)
	if !ok {
		t.Fatal("no next")
	}
	got, err = next.MarshalJSON()
	must(t, err)
	if !bytes.Equal(got, want) {
		t.Fatal(string(got))
	}
	must(t, d.Read(ctx, func(tx *sql.Tx) error {
		var seq, tm, depth, received int64
		var id, service, name, request, user, attrs, cause string
		err := tx.QueryRow("SELECT seq,id,time,service,event,request_id,user,attrs,cause,depth,received FROM events").Scan(&seq, &id, &tm, &service, &name, &request, &user, &attrs, &cause, &depth, &received)
		if err != nil {
			return err
		}
		var raw map[string]json.RawMessage
		must(t, json.Unmarshal(want, &raw))
		if seq != 1 || id != e.ID || service != e.Service || name != e.Name || request != e.RequestID || user != e.User || attrs != string(raw["attrs"]) || cause != e.Cause || depth != 3 || tm != expected.Time.UnixMicro() || received != expected.Received.UnixMicro() {
			t.Fatal("incorrect event row")
		}
		rows, err := tx.Query("SELECT key,value FROM attrs WHERE seq=1 ORDER BY key")
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		values := map[string]string{}
		for rows.Next() {
			var k, v string
			if err = rows.Scan(&k, &v); err != nil {
				return err
			}
			values[k] = v
		}
		if !reflect.DeepEqual(values, map[string]string{"count": "9007199254740993", "flag": "true", "text": `"value"`}) {
			t.Fatal(values)
		}
		return rows.Err()
	}))
}

func TestIngestChecksAndAsk(t *testing.T) {
	// R-2J73-UV65 R-2KF0-8MWU R-7D0H-FIHL R-DD0Z-Q8P6 R-LR8H-603R R-E1EZ-DNJ2
	asks := 0
	var s *store.Store
	s, d, _ := openStore(t, store.Config{DepthMax: 1, Ask: func(_ context.Context, service string) {
		asks++
		if service == "producer" {
			declare(t, s, "producer")
		}
	}})
	ch := s.Changed()
	must(t, s.Deliver(ctx, emit(1)))
	assertClosed(t, ch)
	if asks != 1 {
		t.Fatal(asks)
	}
	must(t, s.Forget(ctx, "producer"))
	ch = s.Changed()
	original, err := all(t, s)[0].MarshalJSON()
	must(t, err)
	duplicate := emit(1)
	duplicate.Service = "other"
	duplicate.Cause = "evt_000000000000ffff"
	duplicate.Depth = 99
	must(t, s.Deliver(ctx, duplicate))
	assertOpen(t, ch)
	if asks != 1 || len(all(t, s)) != 1 {
		t.Fatal("duplicate changed state")
	}
	afterDuplicate, err := all(t, s)[0].MarshalJSON()
	must(t, err)
	if !bytes.Equal(original, afterDuplicate) {
		t.Fatal("duplicate replaced stored record")
	}
	bad := emit(1)
	bad.Attrs = event.Attrs{"nested": []string{"bad"}}
	d.SetFailing(true)
	err = s.Deliver(ctx, bad)
	if !errors.Is(err, event.ErrRejected) || asks != 1 {
		t.Fatal(err, asks)
	}
	d.SetFailing(false)
	unknown := emit(2)
	unknown.Service = "unknown"
	err = s.Deliver(ctx, unknown)
	if !errors.Is(err, store.ErrUndeclared) || asks != 2 {
		t.Fatal(err, asks)
	}
	deep := emit(2)
	deep.Cause = "evt_000000000000ffff"
	deep.Depth = 2
	err = s.Deliver(ctx, deep)
	if !errors.Is(err, store.ErrTooDeep) || asks != 3 {
		t.Fatal(err, asks)
	}
	h, err := s.Head(ctx)
	must(t, err)
	if h != 1 {
		t.Fatal(h)
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	err = s.Deliver(cancelled, emit(3))
	if err == nil || errors.Is(err, event.ErrRejected) {
		t.Fatal(err)
	}
	d.SetFailing(true)
	err = s.Deliver(ctx, emit(3))
	if err == nil || errors.Is(err, event.ErrRejected) {
		t.Fatal(err)
	}
	d.SetFailing(false)
	must(t, s.Deliver(ctx, emit(3)))
	h, err = s.Head(ctx)
	must(t, err)
	if h != 2 || asks != 3 {
		t.Fatal(h, asks)
	}
	retained := all(t, s)
	firstRecord, err := retained[len(retained)-1].MarshalJSON()
	must(t, err)
	if !bytes.Equal(firstRecord, original) {
		t.Fatal("new delivery changed prior record")
	}
}

func writer(t *testing.T, now func() time.Time) (*telemetry.Writer, *telemetry.Capture) {
	t.Helper()
	capture := &telemetry.Capture{}
	w := telemetry.New(telemetry.Config{Service: "events", Version: "test", Sink: capture, Stderr: io.Discard, Now: now, Rand: bytes.NewReader(make([]byte, 4096)), Sleep: func(context.Context, time.Duration) {}})
	t.Cleanup(func() { w.Shutdown(ctx, "done") })
	return w, capture
}
func TestTelemetryBeforeVisibility(t *testing.T) {
	// R-69AX-8R2N R-6BQQ-0AK1 R-RJAX-A1WF
	var s *store.Store
	var second *store.Store
	inside := false
	clockCalls := 0
	w, capture := writer(t, func() time.Time {
		clockCalls++
		if inside {
			p, err := second.Search(ctx, store.Filter{}, 10, "")
			must(t, err)
			h, err := second.Head(ctx)
			must(t, err)
			c, err := second.Catalog(ctx, "", "")
			must(t, err)
			if len(p.Records) != 0 || h != 0 || len(c) != 1 || c[0].Count != 0 {
				t.Fatal("event visible before accepted record")
			}
			e, ok, err := second.Next(ctx, "absent")
			if !errors.Is(err, store.ErrNoSubscriber) || ok || !reflect.DeepEqual(e, event.Event{}) {
				t.Fatal(e, ok, err)
			}
		}
		return instant
	})
	s, d, _ := openStore(t, store.Config{DepthMax: 2, Telemetry: w})
	second = store.New(d, store.Config{Now: func() time.Time { return instant }})
	must(t, s.Declare(ctx, "producer", store.Declaration{Emits: []event.Emission{{Event: "item.created"}}}))
	inside = true
	must(t, s.Deliver(ctx, emit(1)))
	inside = false
	if clockCalls != 1 {
		t.Fatal(clockCalls)
	}
	w.Emit(ctx, "after.read", telemetry.Attrs{})
	must(t, w.Flush(ctx))
	v := capture.Events()
	if len(v) != 2 || v[0].Name != "event.accepted" || v[1].Name != "after.read" || !reflect.DeepEqual(v[0].Attrs, telemetry.Attrs{"event": emit(1).ID, "cause": ""}) {
		t.Fatal(v)
	}
	must(t, s.Deliver(ctx, emit(1)))
	bad := emit(2)
	bad.ID = "bad"
	if s.Deliver(ctx, bad) == nil {
		t.Fatal("malformed accepted")
	}
	unknown := emit(2)
	unknown.Service = "unknown"
	if s.Deliver(ctx, unknown) == nil {
		t.Fatal("undeclared accepted")
	}
	deep := emit(2)
	deep.Depth = 3
	deep.Cause = "evt_000000000000ffff"
	if s.Deliver(ctx, deep) == nil {
		t.Fatal("depth accepted")
	}
	d.SetFailing(true)
	if s.Deliver(ctx, emit(2)) == nil {
		t.Fatal("failure accepted")
	}
	d.SetFailing(false)
	must(t, w.Flush(ctx))
	if len(capture.Events()) != 2 {
		t.Fatal(capture.Events())
	}
	caused := emit(3)
	caused.Cause = "evt_00000000000000ff"
	caused.Depth = 1
	must(t, s.Deliver(ctx, caused))
	must(t, w.Flush(ctx))
	v = capture.Events()
	if len(v) != 3 || v[2].Name != "event.accepted" || !reflect.DeepEqual(v[2].Attrs, telemetry.Attrs{"event": caused.ID, "cause": caused.Cause}) {
		t.Fatal(v)
	}
}

func TestRestartAndRestore(t *testing.T) {
	// R-LOSO-EGMD R-PNJD-XS7X R-7AKO-NZ07 R-3H0B-7EOQ
	s, d, path := openStore(t, store.Config{DepthMax: 5})
	declare(t, s, "producer")
	declare(t, s, "reader", "*")
	must(t, s.Deliver(ctx, emit(1)))
	must(t, s.Pause(ctx, "reader", 1, "broken"))
	declare(t, s, "forgotten", "*")
	must(t, s.Forget(ctx, "forgotten"))
	oldHead, err := s.Head(ctx)
	must(t, err)
	oldDs, err := s.Declarations(ctx)
	must(t, err)
	oldSubs := subs(t, s)
	oldCat, err := s.Catalog(ctx, "", "")
	must(t, err)
	oldPage := all(t, s)
	must(t, d.Close())
	directory, err := os.OpenRoot(filepath.Dir(path))
	must(t, err)
	t.Cleanup(func() { must(t, directory.Close()) })
	copyBytes, err := directory.ReadFile(filepath.Base(path))
	must(t, err)
	reopen := func() (*store.Store, *db.DB) {
		h, err := db.Open(ctx, db.Config{Path: path, Migrations: root.Migrations(), Now: func() time.Time { return instant }})
		must(t, err)
		return store.New(h, store.Config{Now: func() time.Time { return instant }, DepthMax: 5}), h
	}
	s, d = reopen()
	h, err := s.Head(ctx)
	must(t, err)
	ds, err := s.Declarations(ctx)
	must(t, err)
	cat, err := s.Catalog(ctx, "", "")
	must(t, err)
	if h != oldHead || !reflect.DeepEqual(ds, oldDs) || !reflect.DeepEqual(subs(t, s), oldSubs) || !reflect.DeepEqual(cat, oldCat) || !reflect.DeepEqual(all(t, s), oldPage) {
		t.Fatal("restart changed answers")
	}
	must(t, s.Deliver(ctx, emit(2)))
	must(t, s.Forget(ctx, "reader"))
	must(t, s.Forget(ctx, "producer"))
	must(t, d.Close())
	must(t, directory.WriteFile(filepath.Base(path), copyBytes, 0600))
	s, d = reopen()
	t.Cleanup(func() { must(t, d.Close()) })
	h, err = s.Head(ctx)
	must(t, err)
	if h != 1 || !reflect.DeepEqual(subs(t, s), oldSubs) {
		t.Fatal(h, subs(t, s))
	}
	ds, err = s.Declarations(ctx)
	must(t, err)
	if !reflect.DeepEqual(ds, oldDs) || !reflect.DeepEqual(all(t, s), oldPage) {
		t.Fatal("restore changed saved state")
	}
	must(t, s.Deliver(ctx, emit(3)))
	h, err = s.Head(ctx)
	must(t, err)
	if h != 2 {
		t.Fatal(h)
	}
}

func TestConcurrentIngest(t *testing.T) {
	// R-6GMB-JDIT R-6HU7-X59I R-6J24-AX07 R-RJAX-A1WF
	w, capture := writer(t, func() time.Time { return instant })
	s, _, _ := openStore(t, store.Config{Telemetry: w})
	declare(t, s, "producer")
	var wg sync.WaitGroup
	errs := make(chan error, 40)
	for i := 0; i < 20; i++ {
		wg.Go(func() { errs <- s.Deliver(ctx, emit(1)) })
	}
	wg.Wait()
	for i := 0; i < 20; i++ {
		n := i + 2
		wg.Go(func() { errs <- s.Deliver(ctx, emit(n)) })
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		must(t, err)
	}
	v := all(t, s)
	if len(v) != 21 {
		t.Fatal(len(v))
	}
	seen := map[int64]bool{}
	ids := map[string]bool{}
	for _, e := range v {
		if seen[e.Seq] || ids[e.ID] {
			t.Fatal("duplicate")
		}
		seen[e.Seq] = true
		ids[e.ID] = true
	}
	must(t, w.Flush(ctx))
	if len(capture.Events()) != 21 {
		t.Fatal(len(capture.Events()))
	}
}
