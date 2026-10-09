package scheduler_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"path/filepath"
	"reflect"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	cron "github.com/ikigenba/ikigenba/cron"
	"github.com/ikigenba/ikigenba/cron/internal/scheduler"
	"github.com/ikigenba/ikigenba/cron/internal/store"
	"github.com/ikigenba/ikigenba/cron/internal/trail"
)

type wait struct {
	duration time.Duration
	channel  chan time.Time
}
type clock struct {
	mu    sync.Mutex
	now   time.Time
	waits chan wait
}

func (c *clock) Now() time.Time    { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *clock) set(now time.Time) { c.mu.Lock(); defer c.mu.Unlock(); c.now = now }
func (c *clock) After(d time.Duration) <-chan time.Time {
	ch := make(chan time.Time, 1)
	c.waits <- wait{d, ch}
	return ch
}
func stamp(s string) time.Time {
	t, e := time.Parse(time.RFC3339, s)
	if e != nil {
		panic(e)
	}
	return t
}
func take(t *testing.T, c *clock) wait {
	t.Helper()
	select {
	case w := <-c.waits:
		if w.duration < 0 || w.duration > time.Minute {
			t.Fatalf("duration %v", w.duration)
		}
		return w
	case <-time.After(3 * time.Second):
		t.Fatal("scheduler did not install next wait")
		return wait{}
	}
}

type fixture struct {
	c       *clock
	d       *db.DB
	st      *store.Store
	sch     *scheduler.Scheduler
	tc      *telemetry.Capture
	ec      *events.Capture
	writer  *telemetry.Writer
	emitter *events.Emitter
	random  *bytes.Reader
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	t.Setenv("IKIGENBA_SERVICES", "")
	c := &clock{now: stamp("2026-10-05T09:32:00Z"), waits: make(chan wait, 4096)}
	d, err := db.Open(context.Background(), db.Config{Path: filepath.Join(t.TempDir(), "cron.db"), Migrations: cron.Migrations(), Now: c.Now, Service: "cron", Stderr: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	data := make([]byte, 65536)
	for i := range data {
		data[i] = byte(i/8 + i)
	}
	tc, ec := &telemetry.Capture{}, &events.Capture{}
	w := telemetry.New(telemetry.Config{Service: "cron", Sink: tc, Now: c.Now, Rand: bytes.NewReader(data), Stderr: io.Discard})
	em := events.New(events.Config{Service: "cron", Sink: ec, Now: c.Now, Rand: bytes.NewReader(data), Stderr: io.Discard, Emits: trail.Emits()})
	f := &fixture{c: c, d: d, st: store.New(d, store.Config{Now: c.Now, Rand: bytes.NewReader(data)}), tc: tc, ec: ec, writer: w, emitter: em, random: bytes.NewReader(func() []byte {
		b := make([]byte, 65536)
		for i := range b {
			b[i] = byte(i)
		}
		return b
	}())}
	t.Cleanup(func() {
		if f.sch != nil {
			f.sch.Stop()
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		em.Shutdown(ctx)
		w.Shutdown(ctx, "test")
		if err := d.Close(); err != nil {
			t.Error(err)
		}
	})
	return f
}
func (f *fixture) start(t *testing.T) wait {
	t.Helper()
	s, e := scheduler.Start(context.Background(), scheduler.Config{Store: f.st, Events: f.emitter, Telemetry: f.writer, Now: f.c.Now, After: f.c.After, Rand: f.random})
	if e != nil {
		t.Fatal(e)
	}
	f.sch = s
	return take(t, f.c)
}
func (f *fixture) create(t *testing.T, slug, when string) store.Trigger {
	t.Helper()
	x, e := f.st.Create(context.Background(), store.Draft{Slug: slug, When: when, OwnerID: "owner", OwnerEmail: "owner@example.com"})
	if e != nil {
		t.Fatal(e)
	}
	return x
}
func (f *fixture) flush(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if e := f.writer.Flush(ctx); e != nil {
		t.Fatal(e)
	}
	if e := f.emitter.Flush(ctx); e != nil {
		t.Fatal(e)
	}
}
func next(t *testing.T, s *scheduler.Scheduler, id, want string) {
	t.Helper()
	n, ok := s.Next(id)
	if want == "" {
		if ok || !n.IsZero() {
			t.Fatalf("unexpected next %v", n)
		}
		return
	}
	if !ok || !n.Equal(stamp(want)) || n.Location() != time.UTC || n.Second() != 0 || n.Nanosecond() != 0 {
		t.Fatalf("next %v %v; want %s", n, ok, want)
	}
}
func caller() context.Context {
	return identity.NewContext(context.Background(), identity.Caller{UserID: "owner", Email: "caller@example.com", RequestID: "caller-request"})
}

// R-GJOA-GNQ5 R-GNBZ-LYY8
func TestNextSlot(t *testing.T) {
	for _, tc := range []struct{ when, want string }{{"@hourly", "2026-10-05T10:00:00Z"}, {"30 2 * * *", "2026-10-06T02:30:00Z"}, {"@monthly", "2026-11-01T00:00:00Z"}, {"0 8 * * 1", "2026-10-12T08:00:00Z"}, {"0 9 * * *", "2026-10-06T09:00:00Z"}, {"0 0 30 2 *", ""}, {"bogus", ""}, {"@every 1m", ""}} {
		for _, loc := range []*time.Location{time.UTC, time.FixedZone("west", -7*3600)} {
			n, ok := scheduler.NextSlot(tc.when, stamp("2026-10-05T09:32:00Z").In(loc))
			if tc.want == "" {
				if ok || !n.IsZero() {
					t.Fatal(n)
				}
			} else if !ok || !n.Equal(stamp(tc.want)) || n.Location() != time.UTC {
				t.Fatalf("%s = %v", tc.when, n)
			}
		}
	}
	n, ok := scheduler.NextSlot("*/15 * * * *", stamp("2026-10-05T09:45:00Z"))
	if !ok || !n.Equal(stamp("2026-10-05T10:00:00Z")) {
		t.Fatal(n)
	}
}

// R-GDKS-JT0O R-GESO-XKRD R-GH8H-P48R R-GOJV-ZQOX R-43SR-3CUC R-GS7L-51X0 R-GTFH-ITNP R-450N-H4L1
func TestStartAndFreshSlots(t *testing.T) {
	f := newFixture(t)
	h := f.create(t, "hourly", "@hourly")
	m := f.create(t, "month_end", "@monthly")
	n := f.create(t, "nightly_backup", "30 2 * * *")
	p := f.create(t, "weekly_digest", "0 8 * * 1")
	_, e := f.st.SetStatus(context.Background(), p.ID, store.Paused)
	if e != nil {
		t.Fatal(e)
	}
	before, _ := f.st.List(context.Background())
	w := f.start(t)
	if w.duration != time.Minute {
		t.Fatal(w.duration)
	}
	next(t, f.sch, h.ID, "2026-10-05T10:00:00Z")
	next(t, f.sch, m.ID, "2026-11-01T00:00:00Z")
	next(t, f.sch, n.ID, "2026-10-06T02:30:00Z")
	next(t, f.sch, p.ID, "")
	next(t, f.sch, "missing", "")
	after, _ := f.st.List(context.Background())
	if !reflect.DeepEqual(before, after) {
		t.Fatal("start changed store")
	}
	f.flush(t)
	if len(f.tc.Events())+len(f.ec.Events()) != 0 || f.random.Len() != 65536 {
		t.Fatal("start emitted/read random")
	}
	f.sch.Stop()
	select {
	case <-f.c.waits:
		t.Fatal("unsolicited timer")
	default:
	}
	for _, tc := range []struct{ now, last, want string }{{"2026-10-05T12:10:00Z", "2026-10-05T09:00:00Z", "2026-10-05T13:00:00Z"}, {"2026-10-05T09:30:00Z", "2026-10-05T10:00:00Z", "2026-10-05T11:00:00Z"}} {
		f.c.set(stamp(tc.now))
		_, e = f.st.SetLastFired(context.Background(), h.ID, stamp(tc.last))
		if e != nil {
			t.Fatal(e)
		}
		f.start(t)
		next(t, f.sch, h.ID, tc.want)
		f.sch.Stop()
	}
}

// R-GQZO-RA6B
func TestStartFailures(t *testing.T) {
	for _, cancelled := range []bool{false, true} {
		f := newFixture(t)
		ctx, cancel := context.WithCancel(context.Background())
		if cancelled {
			cancel()
		} else {
			f.d.SetFailing(true)
		}
		s, e := scheduler.Start(ctx, scheduler.Config{Store: f.st, Events: f.emitter, Telemetry: f.writer, Now: f.c.Now, After: f.c.After, Rand: f.random})
		cancel()
		if s != nil || !errors.Is(e, store.ErrUnreachable) {
			t.Fatal(s, e)
		}
		select {
		case <-f.c.waits:
			t.Fatal("failed start set timer")
		default:
		}
		f.flush(t)
		if len(f.tc.Events())+len(f.ec.Events()) != 0 || f.random.Len() != 65536 {
			t.Fatal("failed start side effect")
		}
	}
}

// R-GX36-O4VS R-HJ1D-K08A R-H0QV-TG3V R-N3CE-1FR2 R-HK99-XRYZ R-VKNH-WLRG
func TestWakesAndFireIdentity(t *testing.T) {
	for _, wake := range []string{"2026-10-05T10:00:00Z", "2026-10-05T10:20:00Z", "2026-10-05T12:10:00Z"} {
		t.Run(wake, func(t *testing.T) {
			f := newFixture(t)
			x := f.create(t, "hourly", "@hourly")
			w := f.start(t)
			w.channel <- f.c.Now()
			w = take(t, f.c)
			f.flush(t)
			if f.random.Len() != 65536 || len(f.ec.Events()) != 0 {
				t.Fatal("early wake fired")
			}
			f.c.set(stamp(wake))
			w.channel <- f.c.Now()
			w = take(t, f.c)
			if w.duration != time.Minute {
				t.Fatal(w.duration)
			}
			expected := "2026-10-05T10:00:00Z"
			wantNext := "2026-10-05T11:00:00Z"
			if wake == "2026-10-05T12:10:00Z" {
				expected = "2026-10-05T12:00:00Z"
				wantNext = "2026-10-05T13:00:00Z"
			}
			got, e := f.st.Get(context.Background(), x.Slug)
			if e != nil {
				t.Fatal(e)
			}
			x.LastFired = stamp(expected)
			if !reflect.DeepEqual(got, x) {
				t.Fatal(got, x)
			}
			next(t, f.sch, x.ID, wantNext)
			f.flush(t)
			bus, tr := f.ec.Events(), f.tc.Events()
			if len(bus) != 1 || len(tr) != 1 {
				t.Fatal(bus, tr)
			}
			b := bus[0]
			if b.Name != "cron.hourly.fired" || b.User != "owner" || b.RequestID != "000102030405060708090a0b0c0d0e0f" || b.Cause != "" || b.Depth != 0 || !reflect.DeepEqual(b.Attrs, events.Attrs{"trigger": x.ID, "when": x.When, "scheduled": expected}) || tr[0].Name != b.Name || tr[0].RequestID != b.RequestID || tr[0].User != b.User || !reflect.DeepEqual(tr[0].Attrs, telemetry.Attrs(b.Attrs)) {
				t.Fatal(bus, tr)
			}
			if f.random.Len() != 65520 {
				t.Fatal("not exactly sixteen random bytes")
			}
			w.channel <- f.c.Now()
			take(t, f.c)
			f.flush(t)
			if len(f.ec.Events()) != 1 {
				t.Fatal("duplicate fire")
			}
		})
	}
	f := newFixture(t)
	w := f.start(t)
	w.channel <- f.c.Now()
	if take(t, f.c).duration != time.Minute {
		t.Fatal("empty wake")
	}
}

// R-H1YS-77UK R-F1U3-X8ZD
func TestFailedFireRecovery(t *testing.T) {
	f := newFixture(t)
	x := f.create(t, "hourly", "@hourly")
	w := f.start(t)
	f.d.SetFailing(true)
	f.c.set(stamp("2026-10-05T10:00:00Z"))
	w.channel <- f.c.Now()
	w = take(t, f.c)
	if w.duration != time.Minute {
		t.Fatal(w.duration)
	}
	next(t, f.sch, x.ID, "2026-10-05T10:00:00Z")
	f.flush(t)
	if len(f.ec.Events())+len(f.tc.Events()) != 0 {
		t.Fatal("failed fire emitted")
	}
	f.d.SetFailing(false)
	got, _ := f.st.Get(context.Background(), x.Slug)
	if !reflect.DeepEqual(got, x) {
		t.Fatal("failed fire changed store")
	}
	f.c.set(stamp("2026-10-05T12:10:00Z"))
	w.channel <- f.c.Now()
	take(t, f.c)
	got, _ = f.st.Get(context.Background(), x.Slug)
	if !got.LastFired.Equal(stamp("2026-10-05T12:00:00Z")) {
		t.Fatal(got)
	}
	next(t, f.sch, x.ID, "2026-10-05T13:00:00Z")
	f.flush(t)
	if len(f.ec.Events()) != 1 {
		t.Fatal(f.ec.Events())
	}
}

// R-H36O-KZL9 R-GIGE-2VZG R-H5MH-CJ2N R-H6UD-QATC R-H82A-42K1 R-H9A6-HUAQ R-HAI2-VM1F
func TestChanges(t *testing.T) {
	f := newFixture(t)
	f.start(t)
	ctx := events.NewContext(caller(), events.Cause{ID: "evt_0123456789abcdef", Depth: 3})
	f.c.set(stamp("2026-10-05T09:44:58Z"))
	x, e := f.sch.Create(ctx, store.Draft{Slug: "crm_sync", When: "*/15 * * * *", OwnerID: "owner"})
	if e != nil {
		t.Fatal(e)
	}
	stored(t, f.st, x)
	if take(t, f.c).duration != 2*time.Second {
		t.Fatal("create wait")
	}
	next(t, f.sch, x.ID, "2026-10-05T09:45:00Z")
	y, e := f.sch.Update(ctx, "owner", x.Slug, x.When)
	if e != nil || !reflect.DeepEqual(x, y) {
		t.Fatal(y, e)
	}
	stored(t, f.st, y)
	take(t, f.c)
	next(t, f.sch, x.ID, "2026-10-05T09:45:00Z")
	y, e = f.sch.Update(ctx, "owner", x.Slug, "@hourly")
	if e != nil {
		t.Fatal(e)
	}
	stored(t, f.st, y)
	take(t, f.c)
	next(t, f.sch, x.ID, "2026-10-05T10:00:00Z")
	x.When = "@hourly"
	if !reflect.DeepEqual(x, y) {
		t.Fatal(y)
	}
	y, e = f.sch.Pause(ctx, "owner", x.Slug)
	if e != nil {
		t.Fatal(e)
	}
	stored(t, f.st, y)
	take(t, f.c)
	next(t, f.sch, x.ID, "")
	x.Status = store.Paused
	if !reflect.DeepEqual(x, y) {
		t.Fatal(y)
	}
	y, e = f.sch.Pause(ctx, "owner", x.Slug)
	if e != nil || !reflect.DeepEqual(x, y) {
		t.Fatal(y, e)
	}
	take(t, f.c)
	y, e = f.sch.Update(ctx, "owner", x.Slug, "0 8 * * 1")
	if e != nil {
		t.Fatal(e)
	}
	stored(t, f.st, y)
	take(t, f.c)
	next(t, f.sch, x.ID, "")
	x.When = "0 8 * * 1"
	f.c.set(stamp("2026-10-05T12:10:00Z"))
	y, e = f.sch.Resume(ctx, "owner", x.Slug)
	if e != nil {
		t.Fatal(e)
	}
	stored(t, f.st, y)
	take(t, f.c)
	next(t, f.sch, x.ID, "2026-10-12T08:00:00Z")
	x.Status = store.Active
	if !reflect.DeepEqual(x, y) {
		t.Fatal(y)
	}
	y, e = f.sch.Resume(ctx, "owner", x.Slug)
	if e != nil || !reflect.DeepEqual(x, y) {
		t.Fatal(y, e)
	}
	stored(t, f.st, y)
	w := take(t, f.c)
	next(t, f.sch, x.ID, "2026-10-12T08:00:00Z")
	y, e = f.sch.Delete(ctx, "owner", x.Slug)
	if e != nil || !reflect.DeepEqual(x, y) {
		t.Fatal(y, e)
	}
	w = take(t, f.c)
	next(t, f.sch, x.ID, "")
	if _, e = f.st.Get(context.Background(), x.Slug); !errors.Is(e, store.ErrNotFound) {
		t.Fatal(e)
	}
	f.c.set(stamp("2026-10-12T08:00:00Z"))
	w.channel <- f.c.Now()
	take(t, f.c)
	f.flush(t)
	bus, tr := f.ec.Events(), f.tc.Events()
	want := []string{"cron.crm_sync.created", "cron.crm_sync.paused", "cron.crm_sync.resumed", "cron.crm_sync.deleted"}
	if len(bus) != len(want) || len(tr) != len(want) {
		t.Fatal(bus, tr)
	}
	for i, name := range want {
		when := []string{"*/15 * * * *", "@hourly", "0 8 * * 1", "0 8 * * 1"}[i]
		attrs := events.Attrs{"trigger": x.ID, "when": when}
		if bus[i].Name != name || tr[i].Name != name || bus[i].RequestID != "caller-request" || bus[i].User != "owner" || tr[i].RequestID != "caller-request" || tr[i].User != "owner" || bus[i].Cause != "evt_0123456789abcdef" || bus[i].Depth != 4 || !reflect.DeepEqual(bus[i].Attrs, attrs) || !reflect.DeepEqual(tr[i].Attrs, telemetry.Attrs(attrs)) {
			t.Fatal(bus, tr)
		}
	}
	f.c.set(stamp("2026-10-05T09:45:00Z"))
	z, e := f.sch.Create(ctx, store.Draft{Slug: "exact", When: "*/15 * * * *", OwnerID: "owner"})
	if e != nil {
		t.Fatal(e)
	}
	stored(t, f.st, z)
	take(t, f.c)
	next(t, f.sch, z.ID, "2026-10-05T10:00:00Z")
	z, e = f.sch.Create(ctx, store.Draft{Slug: "impossible", When: "0 0 30 2 *", OwnerID: "owner"})
	if e != nil {
		t.Fatal(e)
	}
	stored(t, f.st, z)
	take(t, f.c)
	next(t, f.sch, z.ID, "")
}

// R-HBPZ-9DS4 R-HCXV-N5IT R-HE5S-0X9I
func TestChangeFailures(t *testing.T) {
	f := newFixture(t)
	x := f.create(t, "hourly", "@hourly")
	f.start(t)
	methods := []func(context.Context, string, string) (store.Trigger, error){func(c context.Context, o, s string) (store.Trigger, error) { return f.sch.Update(c, o, s, "bogus") }, f.sch.Pause, f.sch.Resume, f.sch.Delete}
	for _, method := range methods {
		for _, tc := range []struct {
			owner, slug        string
			failing, cancelled bool
			want               error
		}{{"other", x.Slug, false, false, store.ErrNotFound}, {"owner", "unknown", false, false, store.ErrNotFound}, {"other", x.Slug, true, false, store.ErrUnreachable}, {"other", x.Slug, false, true, store.ErrUnreachable}} {
			f.d.SetFailing(tc.failing)
			ctx, cancel := context.WithCancel(caller())
			if tc.cancelled {
				cancel()
			}
			got, e := method(ctx, tc.owner, tc.slug)
			cancel()
			if got != (store.Trigger{}) || !errors.Is(e, tc.want) {
				t.Fatal(got, e)
			}
			exactError(t, e, tc.want)
			f.d.SetFailing(false)
		}
	}
	if got, e := f.sch.Update(caller(), "owner", x.Slug, "bogus"); got != (store.Trigger{}) || !errors.Is(e, store.ErrInvalid) {
		t.Fatal(got, e)
	} else {
		exactError(t, e, store.ErrInvalid)
	}
	for _, draft := range []store.Draft{{Slug: "hourly", When: "@hourly", OwnerID: "owner"}, {Slug: "bad", When: "bogus", OwnerID: "owner"}} {
		_, se := f.st.Create(caller(), draft)
		got, e := f.sch.Create(caller(), draft)
		exactError(t, e, se)
		if got != (store.Trigger{}) || !errors.Is(e, se) {
			t.Fatal(got, e, se)
		}
	}
	got, _ := f.st.Get(context.Background(), x.Slug)
	if !reflect.DeepEqual(got, x) {
		t.Fatal(got)
	}
	next(t, f.sch, x.ID, "2026-10-05T10:00:00Z")
	f.flush(t)
	if len(f.ec.Events())+len(f.tc.Events()) != 0 {
		t.Fatal("failure emitted")
	}
	select {
	case <-f.c.waits:
		t.Fatal("failure reset timer")
	default:
	}
}

// R-GG0L-BCI2 R-HGLK-SGQW R-HK99-XRYZ
func TestStopAndChangesAfterStop(t *testing.T) {
	f := newFixture(t)
	x := f.create(t, "hourly", "@hourly")
	w := f.start(t)
	f.c.set(stamp("2026-10-05T10:00:00Z"))
	f.sch.Stop()
	f.sch.Stop()
	w.channel <- f.c.Now()
	x2, e := f.sch.Create(caller(), store.Draft{Slug: "new", When: "* * * * *", OwnerID: "owner"})
	if e != nil {
		t.Fatal(e)
	}
	next(t, f.sch, x2.ID, "2026-10-05T10:01:00Z")
	if _, e = f.sch.Update(caller(), "owner", x2.Slug, "@hourly"); e != nil {
		t.Fatal(e)
	}
	if _, e = f.sch.Pause(caller(), "owner", x2.Slug); e != nil {
		t.Fatal(e)
	}
	if _, e = f.sch.Resume(caller(), "owner", x2.Slug); e != nil {
		t.Fatal(e)
	}
	if _, e = f.sch.Delete(caller(), "owner", x2.Slug); e != nil {
		t.Fatal(e)
	}
	got, _ := f.st.Get(context.Background(), x.Slug)
	if !reflect.DeepEqual(got, x) {
		t.Fatal(got)
	}
	f.flush(t)
	for _, e := range f.ec.Events() {
		if e.Name == "cron.hourly.fired" {
			t.Fatal(e)
		}
	}
	select {
	case <-f.c.waits:
		t.Fatal("timer after stop")
	default:
	}
}

// R-HFDO-EP07 R-HHTH-68HL
func TestConcurrentChangesAndWake(t *testing.T) {
	for _, remove := range []bool{false, true} {
		f := newFixture(t)
		x := f.create(t, "hourly", "@hourly")
		w := f.start(t)
		f.c.set(stamp("2026-10-05T10:00:00Z"))
		var wg sync.WaitGroup
		wg.Add(3)
		go func() { defer wg.Done(); w.channel <- f.c.Now() }()
		go func() {
			defer wg.Done()
			var e error
			if remove {
				_, e = f.sch.Delete(caller(), "owner", x.Slug)
			} else {
				_, e = f.sch.Pause(caller(), "owner", x.Slug)
			}
			if e != nil {
				t.Error(e)
			}
		}()
		go func() {
			defer wg.Done()
			for range 30 {
				f.sch.Next(x.ID)
			}
		}()
		wg.Wait()
		f.sch.Stop()
		f.flush(t)
		closed := false
		for _, e := range f.ec.Events() {
			if e.Name == "cron.hourly.paused" || e.Name == "cron.hourly.deleted" {
				closed = true
			}
			if closed && e.Name == "cron.hourly.fired" {
				t.Fatal("fire after change")
			}
		}
		closed = false
		for _, e := range f.tc.Events() {
			if e.Name == "cron.hourly.paused" || e.Name == "cron.hourly.deleted" {
				closed = true
			}
			if closed && e.Name == "cron.hourly.fired" {
				t.Fatal("trail fire after change")
			}
		}
	}
}

// R-H0QV-TG3V R-GX36-O4VS
func TestTwoDueTriggers(t *testing.T) {
	f := newFixture(t)
	a := f.create(t, "hourly", "@hourly")
	b := f.create(t, "month_end", "@monthly")
	w := f.start(t)
	f.c.set(stamp("2026-11-01T00:00:00Z"))
	w.channel <- f.c.Now()
	take(t, f.c)
	next(t, f.sch, a.ID, "2026-11-01T01:00:00Z")
	next(t, f.sch, b.ID, "2026-12-01T00:00:00Z")
	f.flush(t)
	bus := f.ec.Events()
	if len(bus) != 2 || f.random.Len() != 65504 {
		t.Fatal(bus)
	}
	ids := []string{bus[0].RequestID, bus[1].RequestID}
	sort.Strings(ids)
	if !reflect.DeepEqual(ids, []string{"000102030405060708090a0b0c0d0e0f", "101112131415161718191a1b1c1d1e1f"}) {
		t.Fatal(ids)
	}
}

type firstFailure struct {
	calls     int
	remaining *bytes.Reader
}

func (r *firstFailure) Read(p []byte) (int, error) {
	r.calls++
	if r.calls == 1 {
		return 0, errors.New("random unavailable")
	}
	return r.remaining.Read(p)
}

// R-H1YS-77UK R-F1U3-X8ZD R-H0QV-TG3V
func TestRandomFailureDoesNotStopOtherFire(t *testing.T) {
	f := newFixture(t)
	a := f.create(t, "first", "@hourly")
	b := f.create(t, "second", "@hourly")
	r := &firstFailure{remaining: bytes.NewReader(bytes.Repeat([]byte{23}, 32))}
	s, e := scheduler.Start(context.Background(), scheduler.Config{Store: f.st, Events: f.emitter, Telemetry: f.writer, Now: f.c.Now, After: f.c.After, Rand: r})
	if e != nil {
		t.Fatal(e)
	}
	f.sch = s
	w := take(t, f.c)
	f.c.set(stamp("2026-10-05T10:00:00Z"))
	w.channel <- f.c.Now()
	w = take(t, f.c)
	if w.duration != time.Minute {
		t.Fatal(w.duration)
	}
	ag, _ := f.st.Get(context.Background(), a.Slug)
	bg, _ := f.st.Get(context.Background(), b.Slug)
	if !ag.LastFired.IsZero() || !bg.LastFired.Equal(f.c.Now()) {
		t.Fatal(ag, bg)
	}
	next(t, s, a.ID, "2026-10-05T10:00:00Z")
	next(t, s, b.ID, "2026-10-05T11:00:00Z")
	f.flush(t)
	if got := f.ec.Events(); len(got) != 1 || got[0].Name != "cron.second.fired" {
		t.Fatal(got)
	}
	f.c.set(stamp("2026-10-05T10:01:00Z"))
	w.channel <- f.c.Now()
	take(t, f.c)
	f.flush(t)
	if len(f.ec.Events()) != 2 {
		t.Fatal(f.ec.Events())
	}
	next(t, s, a.ID, "2026-10-05T11:00:00Z")
}

type blockingReader struct {
	entered chan struct{}
	release chan struct{}
}

func (r *blockingReader) Read(p []byte) (int, error) {
	close(r.entered)
	<-r.release
	for i := range p {
		p[i] = byte(i)
	}
	return len(p), nil
}

// R-HGLK-SGQW R-N3CE-1FR2
func TestStopFinishesAnOngoingFire(t *testing.T) {
	f := newFixture(t)
	x := f.create(t, "hourly", "@hourly")
	r := &blockingReader{entered: make(chan struct{}), release: make(chan struct{})}
	s, e := scheduler.Start(context.Background(), scheduler.Config{Store: f.st, Events: f.emitter, Telemetry: f.writer, Now: f.c.Now, After: f.c.After, Rand: r})
	if e != nil {
		t.Fatal(e)
	}
	f.sch = s
	w := take(t, f.c)
	f.c.set(stamp("2026-10-05T10:00:00Z"))
	w.channel <- f.c.Now()
	select {
	case <-r.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("fire did not read random")
	}
	stopped := make(chan struct{})
	started := make(chan struct{})
	go func() { close(started); s.Stop(); close(stopped) }()
	<-started
	select {
	case <-stopped:
		t.Fatal("stop abandoned ongoing fire")
	default:
	}
	close(r.release)
	select {
	case <-stopped:
	case <-time.After(3 * time.Second):
		t.Fatal("stop did not finish")
	}
	got, e := f.st.Get(context.Background(), x.Slug)
	if e != nil || !got.LastFired.Equal(f.c.Now()) {
		t.Fatal(got, e)
	}
	f.flush(t)
	if len(f.ec.Events()) != 1 || len(f.tc.Events()) != 1 {
		t.Fatal("stop returned without events")
	}
}

// R-H6UD-QATC R-HHTH-68HL
func TestUpdatedScheduleFiresAndConcurrentMethods(t *testing.T) {
	f := newFixture(t)
	f.create(t, "hourly", "@hourly")
	f.start(t)
	x, e := f.sch.Update(caller(), "owner", "hourly", "45 * * * *")
	if e != nil {
		t.Fatal(e)
	}
	w := take(t, f.c)
	f.c.set(stamp("2026-10-05T09:45:00Z"))
	w.channel <- f.c.Now()
	take(t, f.c)
	f.flush(t)
	if got := f.ec.Events(); len(got) != 1 || got[0].Attrs["when"] != "45 * * * *" {
		t.Fatal(got)
	}
	next(t, f.sch, x.ID, "2026-10-05T10:45:00Z")
	var wg sync.WaitGroup
	for _, slug := range []string{"alpha", "beta", "gamma", "delta"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			x, e := f.sch.Create(caller(), store.Draft{Slug: slug, When: "@hourly", OwnerID: "owner"})
			if e != nil {
				t.Error(e)
				return
			}
			if _, e = f.sch.Update(caller(), "owner", slug, "@daily"); e != nil {
				t.Error(e)
			}
			if _, e = f.sch.Pause(caller(), "owner", slug); e != nil {
				t.Error(e)
			}
			f.sch.Next(x.ID)
			if _, e = f.sch.Resume(caller(), "owner", slug); e != nil {
				t.Error(e)
			}
			if _, e = f.sch.Delete(caller(), "owner", slug); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	f.sch.Stop()
	f.flush(t)
	got, e := f.st.List(context.Background())
	if e != nil || len(got) != 1 || got[0].Slug != "hourly" {
		t.Fatal(got, e)
	}
}

func stored(t *testing.T, st *store.Store, want store.Trigger) {
	t.Helper()
	got, err := st.Get(context.Background(), want.Slug)
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("stored row %v, %v; want %v", got, err, want)
	}
}
func exactError(t *testing.T, err, want error) {
	t.Helper()
	for _, sentinel := range []error{store.ErrNotFound, store.ErrSlugTaken, store.ErrInvalid, store.ErrUnreachable} {
		if errors.Is(err, sentinel) != errors.Is(want, sentinel) {
			t.Fatalf("error %v must match exactly the sentinel of %v", err, want)
		}
	}
}
