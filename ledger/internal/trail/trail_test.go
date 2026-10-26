package trail_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http/httptest"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/ledger/internal/trail"
)

type rig struct {
	w  *telemetry.Writer
	em *events.Emitter
	wc *telemetry.Capture
	ec *events.Capture
}

func setup(t *testing.T, ws telemetry.Sink, es events.Sink) rig {
	t.Helper()
	t.Setenv("IKIGENBA_SERVICES", "")
	r := rig{wc: &telemetry.Capture{}, ec: &events.Capture{}}
	if ws == nil {
		ws = r.wc
	}
	if es == nil {
		es = r.ec
	}
	now := func() time.Time { return time.Date(2026, 10, 5, 9, 0, 0, 123456789, time.UTC) }
	r.w = telemetry.New(telemetry.Config{Service: "ledger", Sink: ws, Rand: bytes.NewReader(bytes.Repeat([]byte{0x5a}, 20000)), Now: now, Sleep: func(context.Context, time.Duration) {}, Stderr: io.Discard})
	r.em = events.New(events.Config{Service: "ledger", Sink: es, Now: now, Rand: bytes.NewReader(bytes.Repeat([]byte{0x5a}, 20000)), Sleep: func(context.Context, time.Duration) {}, Stderr: io.Discard, Emits: trail.Emits(), Telemetry: r.w})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		r.em.Shutdown(ctx)
		r.w.Shutdown(ctx, "test")
	})
	return r
}
func (r rig) flush(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := r.em.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	if err := r.w.Flush(ctx); err != nil {
		t.Fatal(err)
	}
}

type badEvents struct {
	err      error
	received chan events.Event
}

func (s badEvents) Deliver(_ context.Context, e events.Event) error {
	select {
	case s.received <- e:
	default:
	}
	return s.err
}

type badTelemetry struct{ received chan telemetry.Event }

func (s badTelemetry) Deliver(_ context.Context, e telemetry.Event) error {
	s.received <- e
	return telemetry.ErrRejected
}

type blockingTelemetry struct{ entered, release chan struct{} }

func (s blockingTelemetry) Deliver(context.Context, telemetry.Event) error {
	select {
	case s.entered <- struct{}{}:
	default:
	}
	<-s.release
	return nil
}

type blockingEvents struct{ entered, release chan struct{} }

func (s blockingEvents) Deliver(context.Context, events.Event) error {
	select {
	case s.entered <- struct{}{}:
	default:
	}
	<-s.release
	return nil
}

// R-DYFB-OT9Q R-DZN8-2L0F R-E0V4-GCR4 R-E5QP-ZFPW R-E6YM-D7GL R-EJ5M-6WVJ
func TestDeclarations(t *testing.T) {
	want := []events.Emission{{Event: "transaction.posted", Attrs: []string{"transaction", "date", "flag", "key", "reverses", "postings"}}}
	if trail.EventTransactionPosted != want[0].Event {
		t.Fatal("name")
	}
	got := trail.Emits()
	if !reflect.DeepEqual(got, want) {
		t.Fatal(got)
	}
	got[0].Event = "changed"
	got[0].Attrs[0] = "changed"
	if !reflect.DeepEqual(trail.Emits(), want) {
		t.Fatal("shared storage")
	}
	r := setup(t, nil, nil)
	rec := httptest.NewRecorder()
	events.DeclarationsHandler(r.em, nil).ServeHTTP(rec, httptest.NewRequest("GET", "/declarations", nil))
	if rec.Code != 200 || rec.Header().Get("Content-Type") != "application/json" || rec.Body.String() != `{"emits":[{"event":"transaction.posted","attrs":["transaction","date","flag","key","reverses","postings"]}],"accepts":[]}` {
		t.Fatal(rec)
	}
}

type change struct {
	name  string
	attrs telemetry.Attrs
	call  func(context.Context, *telemetry.Writer)
}

func changes(v string) []change {
	value := func(key string) string {
		if v == "" {
			return ""
		}
		return key + "-" + v
	}
	return []change{
		{trail.EventAccountOpened, telemetry.Attrs{"account": value("account"), "date": value("date")}, func(c context.Context, w *telemetry.Writer) {
			trail.AccountOpened(c, w, value("account"), value("date"))
		}},
		{trail.EventAccountClosed, telemetry.Attrs{"account": value("account"), "date": value("date")}, func(c context.Context, w *telemetry.Writer) {
			trail.AccountClosed(c, w, value("account"), value("date"))
		}},
		{trail.EventTransactionAnnotated, telemetry.Attrs{"transaction": value("transaction"), "version": int64(-2)}, func(c context.Context, w *telemetry.Writer) {
			trail.TransactionAnnotated(c, w, value("transaction"), int64(-2))
		}},
		{trail.EventTransactionReversed, telemetry.Attrs{"transaction": value("transaction"), "reverses": value("reverses"), "date": value("date")}, func(c context.Context, w *telemetry.Writer) {
			trail.TransactionReversed(c, w, value("transaction"), value("reverses"), value("date"))
		}},
		{trail.EventAssertionAdded, telemetry.Attrs{"assertion": value("assertion"), "account": value("account"), "date": value("date"), "amount": value("amount"), "commodity": value("commodity")}, func(c context.Context, w *telemetry.Writer) {
			trail.AssertionAdded(c, w, value("assertion"), value("account"), value("date"), value("amount"), value("commodity"))
		}},
		{trail.EventAssertionRetracted, telemetry.Attrs{"assertion": value("assertion")}, func(c context.Context, w *telemetry.Writer) { trail.AssertionRetracted(c, w, value("assertion")) }},
		{trail.EventNoteAdded, telemetry.Attrs{"note": value("note"), "account": value("account"), "date": value("date")}, func(c context.Context, w *telemetry.Writer) {
			trail.NoteAdded(c, w, value("note"), value("account"), value("date"))
		}},
		{trail.EventNoteRetracted, telemetry.Attrs{"note": value("note")}, func(c context.Context, w *telemetry.Writer) { trail.NoteRetracted(c, w, value("note")) }},
		{trail.EventDocumentAdded, telemetry.Attrs{"document": value("document"), "account": value("account"), "date": value("date"), "transaction": value("transaction"), "artifact": value("artifact")}, func(c context.Context, w *telemetry.Writer) {
			trail.DocumentAdded(c, w, value("document"), value("account"), value("date"), value("transaction"), value("artifact"))
		}},
		{trail.EventDocumentRetracted, telemetry.Attrs{"document": value("document")}, func(c context.Context, w *telemetry.Writer) { trail.DocumentRetracted(c, w, value("document")) }},
		{trail.EventBookChanged, telemetry.Attrs{"lock_date": value("lock_date"), "fiscal_year_start": value("fiscal_year_start")}, func(c context.Context, w *telemetry.Writer) {
			trail.BookChanged(c, w, value("lock_date"), value("fiscal_year_start"))
		}},
	}
}

// R-6WJR-DBOE R-6XRN-R3F3 R-6YZK-4V5S R-707G-IMWH R-71FC-WEN6 R-72N9-A6DV R-73V5-NY4K R-7532-1PV9 R-77IU-T9CN R-78QR-713C R-79YN-KSU1 R-7B6J-YKKQ R-7CEG-CCBF R-7DMC-Q424 R-7EU9-3VST R-7G25-HNJI R-7HA1-VFA7
func TestChanges(t *testing.T) {
	names := []string{"account.opened", "account.closed", "transaction.annotated", "transaction.reversed", "assertion.added", "assertion.retracted", "note.added", "note.retracted", "document.added", "document.retracted", "book.changed"}
	for _, v := range []string{"", "value"} {
		for _, caller := range []bool{false, true} {
			for _, done := range []bool{false, true} {
				r := setup(t, nil, nil)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				u, q := "", ""
				if caller {
					u, q = "user", "request"
					ctx = identity.NewContext(ctx, identity.Caller{UserID: u, RequestID: q, Email: "private@example.test"})
				}
				if done {
					ctx = events.NewContext(ctx, events.Cause{ID: "evt_0123456789abcdef", Depth: 1})
				}
				if done {
					cancel()
				}
				all := changes(v)
				for _, c := range all {
					c.call(ctx, r.w)
					r.w.Emit(ctx, "tool.called", nil)
				}
				r.flush(t)
				got := r.wc.Events()
				if len(got) != len(all)*2 {
					t.Fatal(len(got))
				}
				for i, c := range all {
					e := got[i*2]
					if c.name != names[i] || e.Name != names[i] || !reflect.DeepEqual(e.Attrs, c.attrs) || e.RequestID != q || e.User != u || got[i*2+1].Name != "tool.called" {
						t.Fatalf("%+v want %+v", e, c)
					}
				}
			}
		}
	}
}

// R-E230-U4HT R-E3AX-7W8I R-E86I-QZ7A R-10QZ-OJNQ R-Q47D-WBOO R-RI03-NQ98 R-S7NT-E6C7 R-EEA0-NTWR
func TestPosted(t *testing.T) {
	for _, caller := range []bool{false, true} {
		for _, done := range []bool{false, true} {
			for _, cause := range []bool{false, true} {
				for _, ps := range [][]trail.Posting{nil, {}, {{Account: `Assets:Bank:"cash`, Amount: "-1.00", Commodity: "USD"}, {Account: "Expenses:Office", Amount: "1.00", Commodity: "USD"}}} {
					r := setup(t, nil, nil)
					ctx, cancel := context.WithCancel(context.Background())
					defer cancel()
					u, q := "", ""
					if caller {
						u, q = "user", "request"
						ctx = identity.NewContext(ctx, identity.Caller{UserID: u, RequestID: q})
					}
					depth := 0
					causeID := ""
					if cause {
						causeID = "evt_0123456789abcdef"
						depth = 1000001
						ctx = events.NewContext(ctx, events.Cause{ID: causeID, Depth: depth - 1})
					}
					if done {
						cancel()
					}
					trail.TransactionPosted(ctx, r.w, r.em, "tx", "2026-10-26", "pending", "key", "", ps)
					r.w.Emit(ctx, "tool.called", nil)
					trail.ReversalPosted(ctx, r.em, "rev", "2026-10-27", "cleared", "", "tx", ps)
					r.flush(t)
					var encoded bytes.Buffer
					encoded.WriteByte('[')
					for i, p := range ps {
						if i > 0 {
							encoded.WriteByte(',')
						}
						a, _ := json.Marshal(p.Account)
						m, _ := json.Marshal(p.Amount)
						c, _ := json.Marshal(p.Commodity)
						fmt.Fprintf(&encoded, `{"account":%s,"amount":%s,"commodity":%s}`, a, m, c)
					}
					encoded.WriteByte(']')
					want := telemetry.Attrs{"transaction": "tx", "date": "2026-10-26", "flag": "pending", "key": "key", "reverses": "", "postings": encoded.String()}
					we, ee := r.wc.Events(), r.ec.Events()
					if len(we) != 2 || len(ee) != 2 {
						t.Fatalf("counts %d %d", len(we), len(ee))
					}
					if we[0].Name != trail.EventTransactionPosted || !reflect.DeepEqual(we[0].Attrs, want) || we[0].RequestID != q || we[0].User != u || we[1].Name != "tool.called" {
						t.Fatal(we)
					}
					for i, e := range ee {
						if i == 1 {
							want["transaction"] = "rev"
							want["date"] = "2026-10-27"
							want["flag"] = "cleared"
							want["key"] = ""
							want["reverses"] = "tx"
						}
						if e.Name != trail.EventTransactionPosted || !reflect.DeepEqual(telemetry.Attrs(e.Attrs), want) || e.RequestID != q || e.User != u || e.Cause != causeID || e.Depth != depth {
							t.Fatalf("%+v want %+v", e, want)
						}
					}
				}
			}
		}
	}
}

// R-EFHX-1LNG R-7IHY-970W
func TestReturnsWhileSinksBlock(t *testing.T) {
	wt := blockingTelemetry{make(chan struct{}, 1), make(chan struct{})}
	et := blockingEvents{make(chan struct{}, 1), make(chan struct{})}
	r := setup(t, wt, et)
	defer close(wt.release)
	defer close(et.release)
	done := make(chan struct{})
	go func() {
		for _, c := range changes("x") {
			c.call(context.Background(), r.w)
		}
		trail.TransactionPosted(context.Background(), r.w, r.em, "", "", "", "", "", nil)
		trail.ReversalPosted(context.Background(), r.em, "", "", "", "", "", nil)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("recording blocked")
	}
	for _, ch := range []chan struct{}{wt.entered, et.entered} {
		select {
		case <-ch:
		case <-time.After(3 * time.Second):
			t.Fatal("sink not entered")
		}
	}
}

// R-Q6N6-NV62 R-7JPU-MYRL
func TestConcurrent(t *testing.T) {
	r := setup(t, nil, nil)
	var wg sync.WaitGroup
	for i := range 24 {
		wg.Go(func() {
			q := fmt.Sprint(i)
			ctx := identity.NewContext(context.Background(), identity.Caller{UserID: "u" + q, RequestID: q})
			for _, c := range changes(q) {
				c.call(ctx, r.w)
			}
			trail.TransactionPosted(ctx, r.w, r.em, q, q, q, q, q, nil)
			trail.ReversalPosted(ctx, r.em, q, q, q, q, q, nil)
		})
	}
	wg.Wait()
	r.flush(t)
	seen := map[string]bool{}
	for _, e := range r.wc.Events() {
		key := e.RequestID + e.Name
		if seen[key] {
			t.Fatal("duplicate", key)
		}
		seen[key] = true
		if e.User != "u"+e.RequestID {
			t.Fatal(e)
		}
		if e.Name == trail.EventTransactionPosted {
			want := telemetry.Attrs{"transaction": e.RequestID, "date": e.RequestID, "flag": e.RequestID, "key": e.RequestID, "reverses": e.RequestID, "postings": "[]"}
			if !reflect.DeepEqual(e.Attrs, want) {
				t.Fatal(e)
			}
		} else {
			found := false
			for _, c := range changes(e.RequestID) {
				if c.name == e.Name {
					found = true
					if !reflect.DeepEqual(e.Attrs, c.attrs) {
						t.Fatal(e)
					}
				}
			}
			if !found {
				t.Fatal(e)
			}
		}
	}
	if len(seen) != 24*12 {
		t.Fatal(len(seen))
	}
	counts := map[string]int{}
	for _, e := range r.ec.Events() {
		counts[e.RequestID]++
		q := e.RequestID
		want := events.Attrs{"transaction": q, "date": q, "flag": q, "key": q, "reverses": q, "postings": "[]"}
		if e.Name != trail.EventTransactionPosted || e.User != "u"+q || !reflect.DeepEqual(e.Attrs, want) {
			t.Fatal(e)
		}
	}
	if len(counts) != 24 {
		t.Fatal(counts)
	}
	for _, n := range counts {
		if n != 2 {
			t.Fatal(counts)
		}
	}
}

// R-Q5FA-A3FD
func TestIndependentHalves(t *testing.T) {
	for _, err := range []error{errors.New("failed"), fmt.Errorf("refused: %w", events.ErrRejected)} {
		r := setup(t, nil, badEvents{err, make(chan events.Event, 100)})
		trail.TransactionPosted(context.Background(), r.w, r.em, "tx", "", "", "", "", nil)
		if err := r.w.Flush(context.Background()); err != nil {
			t.Fatal(err)
		}
		we := r.wc.Events()
		if len(we) < 1 || we[0].Name != trail.EventTransactionPosted || we[0].Attrs["transaction"] != "tx" {
			t.Fatal(we)
		}
		stop, cancel := context.WithCancel(context.Background())
		cancel()
		r.em.Shutdown(stop)
	}
	r := setup(t, nil, nil)
	r.em.Shutdown(context.Background())
	trail.TransactionPosted(context.Background(), r.w, r.em, "tx", "", "", "", "", nil)
	trail.ReversalPosted(context.Background(), r.em, "rev", "", "", "", "tx", nil)
	r.flush(t)
	found := false
	for _, e := range r.wc.Events() {
		if e.Name == trail.EventTransactionPosted {
			found = e.Attrs["transaction"] == "tx"
		}
	}
	if !found || len(r.ec.Events()) != 0 {
		t.Fatal("shutdown lost trail or delivered bus")
	}
	r = setup(t, badTelemetry{make(chan telemetry.Event, 20)}, nil)
	trail.TransactionPosted(context.Background(), r.w, r.em, "tx", "", "", "", "", nil)
	r.flush(t)
	ee := r.ec.Events()
	if len(ee) != 1 || ee[0].Name != trail.EventTransactionPosted || ee[0].Attrs["transaction"] != "tx" {
		t.Fatal(ee)
	}
}

// R-10QZ-OJNQ R-RI03-NQ98 R-S7NT-E6C7
func TestEmptyPostedArguments(t *testing.T) {
	r := setup(t, nil, nil)
	trail.TransactionPosted(context.Background(), r.w, r.em, "", "", "", "", "", nil)
	trail.ReversalPosted(context.Background(), r.em, "", "", "", "", "", nil)
	r.flush(t)
	want := telemetry.Attrs{"transaction": "", "date": "", "flag": "", "key": "", "reverses": "", "postings": "[]"}
	we, ee := r.wc.Events(), r.ec.Events()
	if len(we) != 1 || len(ee) != 2 || we[0].Name != trail.EventTransactionPosted || !reflect.DeepEqual(we[0].Attrs, want) || we[0].RequestID != "" || we[0].User != "" {
		t.Fatal(we, ee)
	}
	for _, e := range ee {
		if e.Name != trail.EventTransactionPosted || !reflect.DeepEqual(telemetry.Attrs(e.Attrs), want) || e.RequestID != "" || e.User != "" || e.Cause != "" || e.Depth != 0 {
			t.Fatal(e)
		}
	}
}

// R-707G-IMWH R-7CEG-CCBF
func TestAnnotationVersions(t *testing.T) {
	for _, v := range []int64{-9, 0, 2} {
		r := setup(t, nil, nil)
		trail.TransactionAnnotated(context.Background(), r.w, "chosen-transaction", v)
		r.flush(t)
		es := r.wc.Events()
		if len(es) != 1 || es[0].Name != trail.EventTransactionAnnotated || !reflect.DeepEqual(es[0].Attrs, telemetry.Attrs{"transaction": "chosen-transaction", "version": v}) {
			t.Fatal(es)
		}
	}
}

// R-DZN8-2L0F
func TestPostingShape(t *testing.T) {
	var p trail.Posting = struct{ Account, Amount, Commodity string }{"chosen-account", "chosen-amount", "chosen-commodity"}
	r := setup(t, nil, nil)
	trail.TransactionPosted(context.Background(), r.w, r.em, "", "", "", "", "", []trail.Posting{p})
	r.flush(t)
	e := r.ec.Events()
	if len(e) != 1 || e[0].Attrs["postings"] != `[{"account":"chosen-account","amount":"chosen-amount","commodity":"chosen-commodity"}]` {
		t.Fatal(e)
	}
}
