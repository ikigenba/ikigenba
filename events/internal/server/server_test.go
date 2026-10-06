package server_test

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	bus "github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	root "github.com/ikigenba/ikigenba/events"
	"github.com/ikigenba/ikigenba/events/internal/declarations"
	"github.com/ikigenba/ikigenba/events/internal/server"
	"github.com/ikigenba/ikigenba/events/internal/store"
)

type safeBuffer struct {
	mu sync.Mutex
	bytes.Buffer
}

func (b *safeBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.Buffer.Write(p)
}
func (b *safeBuffer) text() string { b.mu.Lock(); defer b.mu.Unlock(); return b.String() }

type loop struct {
	run   func(context.Context)
	drain func(context.Context)
}

func (l loop) Run(c context.Context)   { l.run(c) }
func (l loop) Drain(c context.Context) { l.drain(c) }
func take[T any](t *testing.T, c <-chan T) T {
	t.Helper()
	select {
	case v := <-c:
		return v
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for signal")
		var z T
		return z
	}
}
func shortDir(t *testing.T) string {
	t.Helper()
	d, e := os.MkdirTemp("", "evsrv-")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if e := os.RemoveAll(d); e != nil {
			t.Error(e)
		}
	})
	return d
}

type fixture struct {
	cfg               server.Config
	db                *db.DB
	capture           *telemetry.Capture
	stderr            *safeBuffer
	now               time.Time
	clockMu           sync.Mutex
	run, drain        chan struct{}
	sweeps, refreshes chan chan time.Time
}

func fresh(t *testing.T) *fixture {
	t.Helper()
	t.Setenv(services.Variable, "")
	f := &fixture{capture: &telemetry.Capture{}, stderr: &safeBuffer{}, now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), run: make(chan struct{}, 1), drain: make(chan struct{}, 1), sweeps: make(chan chan time.Time, 20), refreshes: make(chan chan time.Time, 20)}
	now := func() time.Time { f.clockMu.Lock(); defer f.clockMu.Unlock(); return f.now }
	var e error
	f.db, e = db.Open(context.Background(), db.Config{Path: filepath.Join(t.TempDir(), "events.db"), Migrations: root.Migrations(), Now: now})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = f.db.Close() })
	w := telemetry.New(telemetry.Config{Service: "events", Version: "test-version", Sink: f.capture, Stderr: f.stderr, Now: now})
	t.Cleanup(func() { c, cancel := context.WithCancel(context.Background()); cancel(); w.Shutdown(c, "cleanup") })
	s := store.New(f.db, store.Config{Now: now, DepthMax: 10})
	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = listener.Close() })
	f.cfg = server.Config{Listener: listener, Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(201); _, _ = io.WriteString(w, "answer") }), Store: s, Declarations: declarations.New(declarations.Config{Store: s, Telemetry: w}), Telemetry: w, Retention: time.Hour, Refresh: time.Minute, Drain: time.Second, Now: now,
		Delivery: loop{run: func(c context.Context) { f.run <- struct{}{}; <-c.Done() }, drain: func(context.Context) { f.drain <- struct{}{} }},
		SweepAfter: func(d time.Duration) <-chan time.Time {
			if d != time.Hour {
				t.Errorf("sweep duration %v", d)
			}
			c := make(chan time.Time, 1)
			f.sweeps <- c
			return c
		},
		RefreshAfter: func(d time.Duration) <-chan time.Time {
			if d != time.Minute {
				t.Errorf("refresh duration %v", d)
			}
			c := make(chan time.Time, 1)
			f.refreshes <- c
			return c
		}}
	return f
}
func (f *fixture) start() (context.CancelCauseFunc, <-chan error) {
	c, cancel := context.WithCancelCause(context.Background())
	out := make(chan error, 1)
	go func() { out <- server.Run(c, f.cfg) }()
	return cancel, out
}
func notifySocket(t *testing.T, address string) *net.UnixConn {
	t.Helper()
	c, e := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: address, Net: "unixgram"})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}
func ready(t *testing.T, c *net.UnixConn) {
	t.Helper()
	if e := c.SetReadDeadline(time.Now().Add(3 * time.Second)); e != nil {
		t.Fatal(e)
	}
	b := make([]byte, 64)
	n, _, e := c.ReadFromUnix(b)
	if e != nil {
		t.Fatal(e)
	}
	if string(b[:n]) != "READY=1" {
		t.Fatalf("notify %q", b[:n])
	}
}
func (f *fixture) flush(t *testing.T) []telemetry.Event {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if e := f.cfg.Telemetry.Flush(c); e != nil {
		t.Fatal(e)
	}
	return f.capture.Events()
}
func (f *fixture) put(t *testing.T, id string, age time.Duration) {
	t.Helper()
	c := context.Background()
	f.clockMu.Lock()
	f.now = f.now.Add(-age)
	f.clockMu.Unlock()
	defer func() { f.clockMu.Lock(); f.now = f.now.Add(age); f.clockMu.Unlock() }()
	if e := f.cfg.Store.Declare(c, "producer", store.Declaration{Emits: []bus.Emission{{Event: "repo.pushed"}}}); e != nil {
		t.Fatal(e)
	}
	if e := f.cfg.Store.Deliver(c, bus.Event{ID: id, Time: f.cfg.Now().Add(-age), Service: "producer", Name: "repo.pushed", Attrs: bus.Attrs{}}); e != nil {
		t.Fatal(e)
	}
}
func (f *fixture) records(t *testing.T) []bus.Event {
	t.Helper()
	p, e := f.cfg.Store.Search(context.Background(), store.Filter{}, 100, "")
	if e != nil {
		t.Fatal(e)
	}
	return p.Records
}
func TestPublicContractAndNormalLifecycle(t *testing.T) {
	// R-AV6Y-9ADI R-AWEU-N247 R-AXMR-0TUW R-AYUN-ELLL R-B2IC-JWTO
	// R-8LSI-MLET R-8O8B-E4W7 R-BC9J-M2R8 R-BJKX-WP7E R-BM0Q-O8OS R-UR40-RL80
	for _, n := range []int{0, 1, 2, -1} {
		e := &server.DrainError{Unfinished: n}
		want := fmt.Sprintf("stopped with %d requests unfinished", n)
		if n == 1 {
			want = "stopped with 1 request unfinished"
		}
		if e.Error() != want {
			t.Fatal(e.Error())
		}
	}
	f := fresh(t)
	var contract server.Delivery = loop{run: func(context.Context) {}, drain: func(context.Context) {}}
	_ = contract
	f.cfg.Delivery = loop{run: func(c context.Context) {
		ev := f.flush(t)
		if len(ev) != 1 || ev[0].Name != "service.started" || ev[0].RequestID != "" || ev[0].User != "" || !reflect.DeepEqual(ev[0].Attrs, telemetry.Attrs{"version": "test-version"}) {
			t.Errorf("start records %#v", ev)
		}
		if c.Err() != nil {
			t.Error("delivery context already done")
		}
		f.run <- struct{}{}
		<-c.Done()
	}, drain: func(c context.Context) {
		if c.Err() != nil {
			t.Error("drain context already done")
		}
		if _, ok := c.Deadline(); !ok {
			t.Error("no drain deadline")
		}
		f.cfg.Telemetry.Emit(context.Background(), "event.delivered", telemetry.Attrs{})
		f.drain <- struct{}{}
	}}
	cancel, out := f.start()
	take(t, f.run)
	resp, e := http.Get("http://" + f.cfg.Listener.Addr().String() + "/anything")
	if e != nil {
		t.Fatal(e)
	}
	b, e := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if e != nil || resp.StatusCode != 201 || string(b) != "answer" || resp.Proto != "HTTP/1.1" {
		t.Fatalf("response %v %q %v", resp, b, e)
	}
	select {
	case e := <-out:
		t.Fatalf("returned early %v", e)
	default:
	}
	began := time.Now()
	cancel(errors.New("test-stop"))
	if e := take(t, out); e != nil {
		t.Fatal(e)
	}
	take(t, f.drain)
	if time.Since(began) >= f.cfg.Drain {
		t.Error("waited for drain deadline")
	}
	ev := f.capture.Events()
	if len(ev) != 3 || ev[2].Name != "service.stopping" || ev[2].RequestID != "" || ev[2].User != "" || !reflect.DeepEqual(ev[2].Attrs, telemetry.Attrs{"reason": "test-stop"}) {
		t.Fatalf("stop records %#v", ev)
	}
	if f.stderr.text() != "" {
		t.Fatal(f.stderr.text())
	}
	select {
	case <-f.run:
		t.Fatal("delivery started twice")
	default:
	}
	select {
	case <-f.drain:
		t.Fatal("delivery drained twice")
	default:
	}
}
func TestStartSweepAndHourlySchedule(t *testing.T) {
	// R-B3Q8-XOKD R-8PG7-RWMW R-8QO4-5ODL R-OXEZ-P9WH R-OYMW-31N6
	f := fresh(t)
	f.put(t, "evt_0000000000000001", 2*time.Hour)
	f.put(t, "evt_0000000000000002", time.Hour)
	f.put(t, "evt_0000000000000003", 0)
	f.cfg.NotifySocket = filepath.Join(shortDir(t), "n")
	n := notifySocket(t, f.cfg.NotifySocket)
	cancel, out := f.start()
	ready(t, n)
	take(t, f.run)
	records := f.records(t)
	if len(records) != 2 || records[1].ID != "evt_0000000000000002" {
		t.Fatalf("start sweep %#v", records)
	}
	tick := take(t, f.sweeps)
	f.clockMu.Lock()
	f.now = f.now.Add(time.Hour)
	f.clockMu.Unlock()
	tick <- f.cfg.Now()
	next := take(t, f.sweeps)
	if got := f.records(t); len(got) != 1 || got[0].ID != "evt_0000000000000003" {
		t.Fatalf("hourly sweep %#v", got)
	}
	refresh := take(t, f.refreshes)
	cancel(errors.New("stop"))
	if e := take(t, out); e != nil {
		t.Fatal(e)
	}
	next <- f.cfg.Now()
	refresh <- f.cfg.Now()
	select {
	case <-f.sweeps:
		t.Fatal("armed sweep after stop")
	default:
	}
	select {
	case <-f.refreshes:
		t.Fatal("armed refresh after stop")
	default:
	}
	if len(f.records(t)) != 1 {
		t.Fatal("swept after stop")
	}
}
func TestSweepFailureIsSilent(t *testing.T) {
	// R-B4Y5-BGB2
	f := fresh(t)
	f.put(t, "evt_0000000000000008", 2*time.Hour)
	f.db.SetFailing(true)
	f.cfg.NotifySocket = filepath.Join(shortDir(t), "n")
	n := notifySocket(t, f.cfg.NotifySocket)
	cancel, out := f.start()
	ready(t, n)
	take(t, f.run)
	tick := take(t, f.sweeps)
	tick <- f.cfg.Now()
	take(t, f.sweeps)
	resp, e := http.Get("http://" + f.cfg.Listener.Addr().String())
	if e != nil {
		t.Fatal(e)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != 201 {
		t.Fatal(resp.StatusCode)
	}
	cancel(errors.New("stop"))
	if e := take(t, out); e != nil {
		t.Fatal(e)
	}
	if f.stderr.text() != "" {
		t.Fatal(f.stderr.text())
	}
	if f.capture.Events()[0].Name != "service.started" {
		t.Fatal("not started")
	}
}
func TestNotificationFailure(t *testing.T) {
	// R-B8LU-GRJ5
	f := fresh(t)
	f.cfg.NotifySocket = filepath.Join(shortDir(t), "missing")
	_, out := f.start()
	e := take(t, out)
	var drain *server.DrainError
	if e == nil || errors.As(e, &drain) {
		t.Fatalf("error %v", e)
	}
	if len(f.capture.Events()) != 0 {
		t.Fatal("recorded start")
	}
	select {
	case <-f.run:
		t.Fatal("delivery started")
	default:
	}
}
func TestNotificationAddressesAndExactlyOnce(t *testing.T) {
	// R-B7DY-2ZSG
	for _, abstract := range []bool{false, true} {
		t.Run(fmt.Sprint(abstract), func(t *testing.T) {
			f := fresh(t)
			path := filepath.Join(shortDir(t), "n")
			if abstract {
				path = "@" + path
			}
			f.cfg.NotifySocket = path
			n := notifySocket(t, path)
			cancel, out := f.start()
			ready(t, n)
			take(t, f.run)
			cancel(errors.New("stop"))
			if e := take(t, out); e != nil {
				t.Fatal(e)
			}
			_ = n.SetReadDeadline(time.Now())
			b := make([]byte, 64)
			if _, _, e := n.ReadFromUnix(b); e == nil {
				t.Fatal("extra notification")
			}
		})
	}
}

type badListener struct{ net.Listener }

func (b badListener) Accept() (net.Conn, error) { return nil, errors.New("accept failed") }
func TestAcceptFailure(t *testing.T) {
	// R-8XZI-GATR
	f := fresh(t)
	f.cfg.Listener = badListener{f.cfg.Listener}
	_, out := f.start()
	e := take(t, out)
	var d *server.DrainError
	if e == nil || errors.As(e, &d) || !strings.Contains(e.Error(), "accept failed") {
		t.Fatalf("error %v", e)
	}
}
func unixSibling(t *testing.T, h http.Handler) string {
	t.Helper()
	path := filepath.Join(shortDir(t), "s")
	l, e := net.Listen("unix", path)
	if e != nil {
		t.Fatal(e)
	}
	s := httptest.NewUnstartedServer(h)
	s.Listener = l
	s.Start()
	t.Cleanup(s.Close)
	return path
}
func servicesFile(t *testing.T, socket string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "services.json")
	body := fmt.Sprintf(`{"services":[{"name":"producer","url":"","description":"","socket":%q,"enabled":true,"mcp":false}]}`, socket)
	if e := os.WriteFile(p, []byte(body), 0600); e != nil {
		t.Fatal(e)
	}
	return p
}
func TestRefreshStartupDeadlineAndFixedSchedule(t *testing.T) {
	// R-B661-P81R R-8N0F-0D5I R-OSJE-66XP R-OTRA-JYOE
	f := fresh(t)
	asked := make(chan chan struct{}, 10)
	finished := make(chan struct{}, 10)
	socket := unixSibling(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		release := make(chan struct{})
		asked <- release
		select {
		case <-release:
			_, _ = io.WriteString(w, `{"emits":[{"event":"repo.pushed","attrs":[]}],"accepts":[]}`)
		case <-r.Context().Done():
		}
		finished <- struct{}{}
	}))
	deadlines := make(chan chan time.Time, 10)
	f.cfg.Declarations = declarations.New(declarations.Config{Store: f.cfg.Store, Services: servicesFile(t, socket), Telemetry: f.cfg.Telemetry, AskAfter: func(d time.Duration) <-chan time.Time {
		if d != declarations.AskTimeout {
			t.Errorf("ask duration %v", d)
		}
		c := make(chan time.Time, 1)
		deadlines <- c
		return c
	}})
	f.cfg.NotifySocket = filepath.Join(shortDir(t), "n")
	n := notifySocket(t, f.cfg.NotifySocket)
	cancel, out := f.start()
	tick := take(t, f.refreshes)
	release := take(t, asked)
	take(t, deadlines)
	select {
	case <-f.run:
		t.Fatal("ready before startup ask")
	default:
	}
	tick <- f.cfg.Now()
	tick = take(t, f.refreshes)
	select {
	case <-asked:
		t.Fatal("overlapping startup refresh")
	default:
	}
	close(release)
	ready(t, n)
	take(t, f.run)
	take(t, finished)
	held, e := f.cfg.Store.Declarations(context.Background())
	if e != nil || len(held["producer"].Emits) != 1 {
		t.Fatalf("held declarations %#v %v", held, e)
	}
	ev := f.flush(t)
	if len(ev) != 2 || ev[0].Name != "sibling.called" || ev[1].Name != "service.started" {
		t.Fatalf("startup order %#v", ev)
	}
	tick <- f.cfg.Now()
	tick = take(t, f.refreshes)
	release = take(t, asked)
	take(t, deadlines)
	tick <- f.cfg.Now()
	tick = take(t, f.refreshes)
	select {
	case <-asked:
		t.Fatal("overlapping periodic refresh")
	default:
	}
	close(release)
	take(t, finished)
	// Drive fixed ticks until the completed refresh allows another ask.
	tick <- f.cfg.Now()
	limit := time.After(3 * time.Second)
	var nextRelease chan struct{}
	for nextRelease == nil {
		select {
		case nextRelease = <-asked:
		case tick = <-f.refreshes:
			tick <- f.cfg.Now()
		case <-limit:
			t.Fatal("completed refresh never allowed a later ask")
		}
	}
	take(t, deadlines)
	close(nextRelease)
	take(t, finished)
	cancel(errors.New("stop"))
	if e := take(t, out); e != nil {
		t.Fatal(e)
	}
}
func TestStartupAskDeadlineReleasesReadiness(t *testing.T) {
	// R-B661-P81R
	f := fresh(t)
	asked := make(chan struct{}, 1)
	socket := unixSibling(t, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) { asked <- struct{}{}; <-r.Context().Done() }))
	deadlines := make(chan chan time.Time, 1)
	f.cfg.Declarations = declarations.New(declarations.Config{Store: f.cfg.Store, Services: servicesFile(t, socket), Telemetry: f.cfg.Telemetry, AskAfter: func(time.Duration) <-chan time.Time { c := make(chan time.Time, 1); deadlines <- c; return c }})
	f.cfg.NotifySocket = filepath.Join(shortDir(t), "n")
	n := notifySocket(t, f.cfg.NotifySocket)
	cancel, out := f.start()
	take(t, asked)
	deadline := take(t, deadlines)
	select {
	case <-f.run:
		t.Fatal("started before deadline")
	default:
	}
	deadline <- f.cfg.Now()
	ready(t, n)
	take(t, f.run)
	cancel(errors.New("stop"))
	if e := take(t, out); e != nil {
		t.Fatal(e)
	}
}

type watchedListener struct {
	net.Listener
	closed  chan struct{}
	once    sync.Once
	onClose func()
}

func (l *watchedListener) Close() error {
	l.once.Do(func() { close(l.closed) })
	return l.Listener.Close()
}
func (l *watchedListener) Accept() (net.Conn, error) {
	c, e := l.Listener.Accept()
	if e != nil {
		return c, e
	}
	if l.onClose != nil {
		return &watchedConn{Conn: c, close: l.onClose}, nil
	}
	return c, nil
}

type watchedConn struct {
	net.Conn
	close func()
}

func (c watchedConn) Close() error { c.close(); return c.Conn.Close() }

func TestRequestFinishesDuringDrainAndIdleIsClosed(t *testing.T) {
	// R-BGZT-F43Y R-OW73-BI5S R-BID1-IXGP R-BKSU-AGY3 R-BM0Q-O8OS
	f := fresh(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	f.cfg.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/hold" {
			close(entered)
			<-release
		}
		_, _ = io.WriteString(w, "complete")
		f.cfg.Telemetry.Emit(context.Background(), "request.finished", telemetry.Attrs{})
	})
	listener := &watchedListener{Listener: f.cfg.Listener, closed: make(chan struct{})}
	f.cfg.Listener = listener
	cancel, out := f.start()
	take(t, f.run)
	idle, e := net.Dial("tcp", listener.Addr().String())
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = idle.Close() }()
	_, e = io.WriteString(idle, "GET /idle HTTP/1.1\r\nHost: events\r\n\r\n")
	if e != nil {
		t.Fatal(e)
	}
	response, e := http.ReadResponse(bufio.NewReader(idle), nil)
	if e != nil {
		t.Fatal(e)
	}
	_, e = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
	if e != nil {
		t.Fatal(e)
	}
	answers := make(chan string, 1)
	go func() {
		r, e := http.Get("http://" + listener.Addr().String() + "/hold")
		if e != nil {
			answers <- e.Error()
			return
		}
		b, e := io.ReadAll(r.Body)
		_ = r.Body.Close()
		if e != nil {
			answers <- e.Error()
			return
		}
		answers <- string(b)
	}()
	take(t, entered)
	cancel(errors.New("stop"))
	take(t, listener.closed)
	take(t, f.drain)
	_ = idle.SetReadDeadline(time.Now().Add(time.Second))
	var b [1]byte
	if _, e = idle.Read(b[:]); e == nil {
		t.Fatal("idle connection stayed open")
	}
	select {
	case <-out:
		t.Fatal("returned with handler held")
	default:
	}
	close(release)
	if got := take(t, answers); got != "complete" {
		t.Fatal(got)
	}
	if e := take(t, out); e != nil {
		t.Fatal(e)
	}
	ev := f.capture.Events()
	if len(ev) != 4 || ev[2].Name != "request.finished" || ev[3].Name != "service.stopping" {
		t.Fatalf("ordering %#v", ev)
	}
}

func TestCutoffCountsRequestsAndLogsBeforeClosing(t *testing.T) {
	// R-BOGJ-FS66 R-8T3W-X7UZ R-8VJP-ORCD R-OZUS-GTDV R-8WRM-2J32
	f := fresh(t)
	f.cfg.Drain = 80 * time.Millisecond
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	defer close(release)
	f.cfg.Handler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		entered <- struct{}{}
		<-release
		_, _ = io.WriteString(w, "too late")
	})
	closed := make(chan string, 10)
	f.cfg.Listener = &watchedListener{Listener: f.cfg.Listener, closed: make(chan struct{}), onClose: func() { closed <- f.stderr.text() }}
	cancel, out := f.start()
	take(t, f.run)
	responses := make(chan error, 2)
	for range 2 {
		go func() {
			r, e := http.Get("http://" + f.cfg.Listener.Addr().String() + "/held")
			if e == nil {
				_, e = io.ReadAll(r.Body)
				_ = r.Body.Close()
			}
			responses <- e
		}()
	}
	take(t, entered)
	take(t, entered)
	began := time.Now()
	cancel(errors.New("cutoff"))
	e := take(t, out)
	var d *server.DrainError
	if !errors.As(e, &d) || d.Unfinished != 2 {
		t.Fatalf("error %v", e)
	}
	if time.Since(began) > f.cfg.Drain+time.Second {
		t.Fatal("late cutoff")
	}
	for range 2 {
		if e := take(t, responses); e == nil {
			t.Error("unfinished response delivered")
		}
		if line := take(t, closed); !strings.Contains(line, `"event":"service.stopping"`) {
			t.Errorf("closed before stop line: %q", line)
		}
	}
	ev := f.capture.Events()
	for _, e := range ev {
		if e.Name == "service.stopping" {
			t.Fatal("stopping reached sink")
		}
	}
	if !strings.Contains(f.stderr.text(), "events: undelivered event: ") {
		t.Fatal(f.stderr.text())
	}
}

func TestDeliveryDrainDeadlineIsNotRequestFailure(t *testing.T) {
	// R-BJKX-WP7E R-UR40-RL80 R-OZUS-GTDV R-8WRM-2J32
	f := fresh(t)
	f.cfg.Drain = 50 * time.Millisecond
	f.cfg.Delivery = loop{run: func(c context.Context) { f.run <- struct{}{}; <-c.Done() }, drain: func(c context.Context) { f.drain <- struct{}{}; <-c.Done() }}
	cancel, out := f.start()
	take(t, f.run)
	began := time.Now()
	cancel(errors.New("stop"))
	if e := take(t, out); e != nil {
		t.Fatal(e)
	}
	if time.Since(began) > f.cfg.Drain+time.Second {
		t.Fatal("late return")
	}
	for _, e := range f.capture.Events() {
		if e.Name == "service.stopping" {
			t.Fatal("stopping reached sink")
		}
	}
	if !strings.Contains(f.stderr.text(), `"event":"service.stopping"`) {
		t.Fatal(f.stderr.text())
	}
}

type temporaryError struct{}

func (temporaryError) Error() string   { return "temporary accept" }
func (temporaryError) Temporary() bool { return true }
func (temporaryError) Timeout() bool   { return false }

type flakyListener struct {
	net.Listener
	once sync.Once
}

func (l *flakyListener) Accept() (net.Conn, error) {
	fail := false
	l.once.Do(func() { fail = true })
	if fail {
		return nil, temporaryError{}
	}
	return l.Listener.Accept()
}
func TestServerLogsAreDiscarded(t *testing.T) {
	// R-BS48-L3E9
	var logs safeBuffer
	old := log.Writer()
	log.SetOutput(&logs)
	defer log.SetOutput(old)
	f := fresh(t)
	f.cfg.Listener = &flakyListener{Listener: f.cfg.Listener}
	entered := make(chan struct{})
	f.cfg.Handler = http.HandlerFunc(func(http.ResponseWriter, *http.Request) { close(entered); panic("handler panic") })
	cancel, out := f.start()
	take(t, f.run)
	r, e := http.Get("http://" + f.cfg.Listener.Addr().String())
	if e == nil {
		_ = r.Body.Close()
		t.Error("panic request succeeded")
	}
	take(t, entered)
	cancel(errors.New("stop"))
	if e := take(t, out); e != nil {
		t.Fatal(e)
	}
	if logs.text() != "" {
		t.Fatal(logs.text())
	}
	if f.stderr.text() != "" {
		t.Fatal(f.stderr.text())
	}
}

func TestDeliveryRunReturnsBeforeDrainStarts(t *testing.T) {
	// R-BJKX-WP7E
	f := fresh(t)
	cancelled := make(chan struct{})
	release := make(chan struct{})
	returned := make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	f.cfg.Delivery = loop{
		run: func(c context.Context) {
			defer close(returned)
			f.run <- struct{}{}
			<-c.Done()
			close(cancelled)
			<-release
		},
		drain: func(c context.Context) {
			select {
			case <-returned:
			default:
				t.Error("Drain began before Run returned")
			}
			if c.Err() != nil {
				t.Error("drain context already done")
			}
			f.drain <- struct{}{}
		},
	}
	listener := &watchedListener{Listener: f.cfg.Listener, closed: make(chan struct{})}
	f.cfg.Listener = listener
	cancel, out := f.start()
	take(t, f.run)
	select {
	case <-f.drain:
		t.Fatal("Drain entered before cancellation")
	default:
	}
	cancel(errors.New("stop"))
	take(t, cancelled)
	take(t, listener.closed)
	select {
	case <-f.drain:
		t.Fatal("Drain entered while Run return was held")
	default:
	}
	select {
	case <-out:
		t.Fatal("server returned while Run return was held")
	default:
	}
	releaseOnce.Do(func() { close(release) })
	take(t, f.drain)
	if e := take(t, out); e != nil {
		t.Fatal(e)
	}
}

type sinkFunc func(context.Context, telemetry.Event) error

func (f sinkFunc) Deliver(c context.Context, e telemetry.Event) error { return f(c, e) }

func TestUndeliveredQueueIsOrderedAndDeadlineDoesNotWaitForSink(t *testing.T) {
	// R-8T3W-X7UZ R-8WRM-2J32
	f := fresh(t)
	f.cfg.Drain = 80 * time.Millisecond
	entered := make(chan struct{})
	release := make(chan struct{})
	returned := make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	stopped, cancelStopped := context.WithCancel(context.Background())
	cancelStopped()
	f.cfg.Telemetry.Shutdown(stopped, "replace test writer")
	f.capture = &telemetry.Capture{}
	f.stderr = &safeBuffer{}
	sink := sinkFunc(func(c context.Context, e telemetry.Event) error {
		if e.Name == "service.started" {
			return f.capture.Deliver(c, e)
		}
		close(entered)
		// Keep the sender inside the sink after its context ends. Shutdown must
		// write the buffered records and return without waiting for this call.
		<-release
		close(returned)
		return c.Err()
	})
	writer := telemetry.New(telemetry.Config{Service: "events", Version: "test-version", Sink: sink, Stderr: f.stderr, Now: f.cfg.Now})
	t.Cleanup(func() { writer.Shutdown(stopped, "cleanup") })
	f.cfg.Telemetry = writer
	f.cfg.Declarations = declarations.New(declarations.Config{Store: f.cfg.Store, Telemetry: writer})
	cancel, out := f.start()
	take(t, f.run)
	f.flush(t)
	for _, name := range []string{"queue.first", "queue.second", "queue.third"} {
		writer.Emit(context.Background(), name, telemetry.Attrs{})
	}
	take(t, entered)
	began := time.Now()
	cancel(errors.New("queue cutoff"))
	if e := take(t, out); e != nil {
		t.Fatal(e)
	}
	if elapsed := time.Since(began); elapsed > f.cfg.Drain+time.Second {
		t.Fatalf("waited after drain deadline: %v", elapsed)
	}
	select {
	case <-returned:
		t.Fatal("sink unexpectedly returned before release")
	default:
	}
	lines := strings.Split(strings.TrimSuffix(f.stderr.text(), "\n"), "\n")
	want := []string{"queue.first", "queue.second", "queue.third", "service.stopping"}
	if len(lines) != len(want) {
		t.Fatalf("undelivered lines %q", lines)
	}
	for i, line := range lines {
		const prefix = "events: undelivered event: "
		if !strings.HasPrefix(line, prefix) {
			t.Fatalf("diagnostic %q", line)
		}
		var record struct {
			Event string         `json:"event"`
			Attrs map[string]any `json:"attrs"`
		}
		if e := json.Unmarshal([]byte(strings.TrimPrefix(line, prefix)), &record); e != nil {
			t.Fatal(e)
		}
		if record.Event != want[i] {
			t.Fatalf("record %d = %q want %q", i, record.Event, want[i])
		}
		if record.Event == "service.stopping" && !reflect.DeepEqual(record.Attrs, map[string]any{"reason": "queue cutoff"}) {
			t.Fatalf("stop attributes %#v", record.Attrs)
		}
	}
	ev := f.capture.Events()
	if len(ev) != 1 || ev[0].Name != "service.started" {
		t.Fatalf("delivered telemetry %#v", ev)
	}
	releaseOnce.Do(func() { close(release) })
	take(t, returned)
	if got := f.capture.Events(); !reflect.DeepEqual(got, ev) {
		t.Fatalf("late delivered telemetry %#v", got)
	}
}
