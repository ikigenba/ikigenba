package cli_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	app "github.com/ikigenba/ikigenba/gmail"
	"github.com/ikigenba/ikigenba/gmail/internal/cli"
	"github.com/ikigenba/ikigenba/gmail/internal/server"
	"github.com/ikigenba/ikigenba/gmail/internal/store"
	"github.com/ikigenba/ikigenba/gmail/internal/tools"
)

type lifecycleClock struct {
	mu sync.Mutex
	at time.Time
}

func (c *lifecycleClock) Now() time.Time   { c.mu.Lock(); defer c.mu.Unlock(); return c.at }
func (c *lifecycleClock) Set(at time.Time) { c.mu.Lock(); defer c.mu.Unlock(); c.at = at }
func lifecycleAt() time.Time               { return time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC) }
func lifecycleStore(t *testing.T, dir string, now func() time.Time) (*db.DB, *store.Store) {
	t.Helper()
	d, e := db.Open(context.Background(), db.Config{Path: filepath.Join(dir, "state", "gmail.db"), Migrations: app.Migrations(), Now: now})
	if e != nil {
		t.Fatal(e)
	}
	return d, store.New(d, store.Config{Now: now, Rand: &runRandom{}})
}
func lifecycleWait[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	select {
	case v := <-ch:
		return v
	case <-time.After(10 * time.Second):
		t.Fatal("lifecycle wait deadline")
		var z T
		return z
	}
}
func lifecyclePage(t *testing.T, f *runFixture, user, path string) int {
	t.Helper()
	r, e := http.NewRequest("GET", f.Base+path, nil)
	if e != nil {
		t.Fatal(e)
	}
	r.Header.Set("X-User-Id", user)
	resp, e := f.HTTP.Do(r)
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
	return resp.StatusCode
}
func lifecycleEqual(t *testing.T, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v want %#v", got, want)
	}
}
func lifecyclePull(t *testing.T, f *runFixture, path string) string {
	t.Helper()
	r := f.call(fixtureCaller, "pull", map[string]any{"message": "message", "attachment": "part", "path": path})
	if r.IsError() {
		t.Fatalf("pull refused: %v", resultJSON(t, r))
	}
	v := resultJSON(t, r)
	id, ok := v["id"].(string)
	if !ok {
		t.Fatalf("pull result %v", v)
	}
	return id
}

// R-XM2U-HKLR R-PXBI-14NN R-XQYG-0NKJ R-XY9U-BA0P
func TestLifecycleRecoveryAndStop(t *testing.T) {
	dir := t.TempDir()
	clock := &lifecycleClock{at: lifecycleAt().Add(-time.Hour)}
	d, s := lifecycleStore(t, dir, clock.Now)
	v, e := s.AddTransfer(context.Background(), store.Transfer{Direction: store.DirectionPull, Status: store.StatusRunning, Message: "message", Attachment: "part", Name: "name", Path: "path", ContentType: "type", UserID: "owner", UserEmail: "email", RequestID: "request"})
	if e != nil {
		t.Fatal(e)
	}
	v, e = s.SetTransferBytes(context.Background(), v.ID, 42)
	if e != nil {
		t.Fatal(e)
	}
	q, e := s.AddTransfer(context.Background(), store.Transfer{Direction: store.DirectionPush, Status: store.StatusQueued, Message: "draft-message", Draft: "draft", Name: "queued-name", Path: "queued-path", ContentType: "queued-type", UserID: "owner"})
	if e != nil {
		t.Fatal(e)
	}
	if e = d.Close(); e != nil {
		t.Fatal(e)
	}
	clock.Set(lifecycleAt())
	f := startRun(t, runOptions{Dir: dir, Now: clock.Now})
	ss := f.reopen()
	for _, want := range []store.Transfer{v, q} {
		got, e := ss.GetTransfer(context.Background(), want.UserID, want.ID)
		if e != nil {
			t.Fatal(e)
		}
		want.Status = store.StatusFailed
		want.Reason = store.ReasonInterrupted
		want.Finished = clock.Now()
		lifecycleEqual(t, got, want)
	}
	if code := lifecyclePage(t, f, "owner", "/transfers/"+v.ID); code != 200 {
		t.Fatal(code)
	}
	if code := f.stop(); code != cli.ExitSuccess {
		t.Fatal(code)
	}
	events := f.Telemetry.Events()
	if len(events) < 3 {
		t.Fatal(events)
	}
	lifecycleEqual(t, events[0].Name, "service.started")
	lifecycleEqual(t, events[0].RequestID, "")
	lifecycleEqual(t, events[0].User, "")
	lifecycleEqual(t, events[0].Attrs, telemetry.Attrs{"version": f.Process.Version})
	requestSeen := false
	for _, e := range events {
		if e.Name == "request.started" {
			requestSeen = true
		}
		if e.Name == "transfer.finished" && requestSeen {
			t.Fatal("settling after request")
		}
	}
	last := events[len(events)-1]
	lifecycleEqual(t, last.Name, "service.stopping")
	lifecycleEqual(t, last.User, "")
	lifecycleEqual(t, last.RequestID, "")
	lifecycleEqual(t, last.Attrs, telemetry.Attrs{"reason": "fixture stop"})
	count := 0
	for _, e := range events {
		if e.Name == "service.stopping" {
			count++
		}
	}
	lifecycleEqual(t, count, 1)
	lifecycleEqual(t, f.Out.String(), "")
	lifecycleEqual(t, f.Err.String(), "")
}

// R-PW3L-NCWY R-X7G1-WBPF R-XOIN-9435 R-XQYG-0NKJ
func TestLifecyclePrunes(t *testing.T) {
	dir := t.TempDir()
	clock := &lifecycleClock{at: lifecycleAt()}
	d, s := lifecycleStore(t, dir, clock.Now)
	type record struct {
		user, path string
		removed    bool
	}
	records := []record{}
	seedTransfer := func(user string, days int, removed bool) {
		clock.Set(lifecycleAt().Add(-time.Duration(days) * 24 * time.Hour))
		v, e := s.AddTransfer(context.Background(), store.Transfer{Direction: store.DirectionPull, Status: store.StatusRunning, Message: "message", Path: "path", UserID: user})
		if e != nil {
			t.Fatal(e)
		}
		_, e = s.FinishTransfer(context.Background(), v.ID, store.Ending{Status: store.StatusDone})
		if e != nil {
			t.Fatal(e)
		}
		records = append(records, record{user, "/transfers/" + v.ID, removed})
	}
	seedDraft := func(user string, age time.Duration, status string, removed bool) {
		clock.Set(lifecycleAt().Add(-age))
		v, e := s.AddDraft(context.Background(), store.Draft{GmailDraft: "remote", Message: "message", UserID: user})
		if e != nil {
			t.Fatal(e)
		}
		switch status {
		case store.DraftSent:
			_, e = s.MarkDraftSent(context.Background(), v.ID, "sent", "", time.Time{})
		case store.DraftDiscarded:
			_, e = s.MarkDraftDiscarded(context.Background(), v.ID)
		}
		if e != nil {
			t.Fatal(e)
		}
		records = append(records, record{user, "/drafts/" + v.ID, removed})
	}
	seedTransfer("owner", 20, true)
	seedTransfer("owner", 10, false)
	seedTransfer("owner", 5, false)
	seedTransfer("other", 20, false)
	for _, days := range []int{3, 35, 40} {
		seedDraft("owner", time.Duration(days)*24*time.Hour, store.DraftSent, days == 40)
	}
	seedDraft("owner", time.Hour, store.DraftOpen, false)
	seedDraft("owner", 60*24*time.Hour, store.DraftOpen, false)
	for _, days := range []int{1, 2, 3} {
		seedDraft("other", time.Duration(days)*24*time.Hour, store.DraftDiscarded, false)
	}
	clock.Set(lifecycleAt())
	if e := d.Close(); e != nil {
		t.Fatal(e)
	}
	type timer struct {
		duration time.Duration
		ch       chan time.Time
	}
	timers := make(chan timer, 8)
	f := startRun(t, runOptions{Dir: dir, Now: clock.Now, Env: map[string]string{"TRANSFER_KEEP_COUNT": "1", "DRAFT_KEEP_DAYS": "30", "DRAFT_KEEP_COUNT": "2"}, After: func(d time.Duration) <-chan time.Time {
		ch := make(chan time.Time, 1)
		timers <- timer{d, ch}
		return ch
	}})
	tick := lifecycleWait(t, timers)
	lifecycleEqual(t, tick.duration, cli.SweepInterval)
	for _, r := range records {
		want := 200
		if r.removed {
			want = 404
		}
		lifecycleEqual(t, lifecyclePage(t, f, r.user, r.path), want)
	}
	clock.Set(lifecycleAt().Add(40 * 24 * time.Hour))
	tick.ch <- clock.Now()
	next := lifecycleWait(t, timers)
	lifecycleEqual(t, next.duration, cli.SweepInterval)
	for i, r := range records {
		want := 200
		if r.removed || i == 1 || i == 11 {
			want = 404
		}
		lifecycleEqual(t, lifecyclePage(t, f, r.user, r.path), want)
	}
	lifecycleEqual(t, f.stop(), cli.ExitSuccess)
	select {
	case extra := <-timers:
		t.Fatalf("After after stop: %v", extra.duration)
	default:
	}
}

type lifecycleListener struct {
	net.Listener
	accepts atomic.Int64
	before  func()
}

func (l *lifecycleListener) Accept() (net.Conn, error) {
	l.accepts.Add(1)
	if l.before != nil {
		l.before()
	}
	return l.Listener.Accept()
}
func lifecycleBare(t *testing.T, dir string) (cli.Process, *lifecycleListener, *net.UnixConn, *telemetry.Capture, *outputBuffer, *outputBuffer) {
	t.Helper()
	t.Setenv(services.Variable, "")
	socketDir, e := os.MkdirTemp("", "gmail-life-")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = os.RemoveAll(socketDir) })
	notify, e := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: filepath.Join(socketDir, "notify"), Net: "unixgram"})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = notify.Close() })
	ln, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	l := &lifecycleListener{Listener: ln}
	t.Cleanup(func() { _ = l.Close() })
	out, errout := &outputBuffer{}, &outputBuffer{}
	capture := &telemetry.Capture{}
	env := map[string]string{"LISTEN_PID": "123", "LISTEN_FDS": "1", "NOTIFY_SOCKET": notify.LocalAddr().String(), "DRAIN_SECONDS": "1"}
	p := cli.Process{LookupEnv: func(k string) (string, bool) { v, ok := env[k]; return v, ok }, Pid: 123, Stdout: out, Stderr: errout, Dir: dir, Version: "lifecycle", Inherit: func(uintptr) (net.Listener, error) { return l, nil }, Now: lifecycleAt, Rand: &runRandom{}, Sink: capture, EventSink: &lifecycleEventDiscard{}, Sleep: func(c context.Context, _ time.Duration) { <-c.Done() }, After: func(time.Duration) <-chan time.Time { return make(chan time.Time) }}
	return p, l, notify, capture, out, errout
}

type lifecycleEventDiscard struct{}

func (*lifecycleEventDiscard) Deliver(context.Context, events.Event) error { return nil }

// R-XKUY-3SV2
func TestLifecycleUnreadableTransfers(t *testing.T) {
	dir := t.TempDir()
	d, _ := lifecycleStore(t, dir, lifecycleAt)
	if e := d.Write(context.Background(), func(tx *sql.Tx) error { _, e := tx.ExecContext(context.Background(), "DROP TABLE transfers"); return e }); e != nil {
		t.Fatal(e)
	}
	if e := d.Close(); e != nil {
		t.Fatal(e)
	}
	p, l, notify, capture, out, errout := lifecycleBare(t, dir)
	lifecycleEqual(t, cli.Run(context.Background(), p), cli.ExitServerFailed)
	lifecycleEqual(t, l.accepts.Load(), int64(0))
	lifecycleEqual(t, out.String(), "")
	lifecycleEqual(t, errout.String(), "gmail: "+store.Unreachable+"\n")
	lifecycleEqual(t, errout.writes, 1)
	es := capture.Events()
	lifecycleEqual(t, len(es), 1)
	lifecycleEqual(t, es[0].Name, "service.started")
	text, e := lifecycleDatagram(notify)
	if !errors.Is(e, syscall.EAGAIN) {
		t.Fatalf("unexpected notify %q %v", text, e)
	}
}

// R-XM2U-HKLR
func TestLifecycleReadyProtocol(t *testing.T) {
	for _, mode := range []string{"set", "empty", "unset"} {
		t.Run(mode, func(t *testing.T) {
			p, l, notify, _, out, errout := lifecycleBare(t, t.TempDir())
			lookup := p.LookupEnv
			p.LookupEnv = func(k string) (string, bool) {
				if k == "NOTIFY_SOCKET" {
					if mode == "empty" {
						return "", true
					}
					if mode == "unset" {
						return "", false
					}
				}
				return lookup(k)
			}
			type notification struct {
				text string
				err  error
			}
			checked := make(chan notification, 1)
			var once sync.Once
			l.before = func() {
				once.Do(func() {
					if mode == "set" {
						text, e := lifecycleDatagram(notify)
						checked <- notification{text, e}
					} else {
						checked <- notification{}
					}
				})
			}
			ctx, cancel := context.WithCancelCause(context.Background())
			done := make(chan int, 1)
			go func() { done <- cli.Run(ctx, p) }()
			n := lifecycleWait(t, checked)
			if mode == "set" {
				if n.err != nil {
					cancel(fmt.Errorf("failed notification"))
					t.Fatalf("Accept before READY: %v", n.err)
				}
				lifecycleEqual(t, n.text, "READY=1")
			}
			cancel(fmt.Errorf("stop"))
			lifecycleEqual(t, lifecycleWait(t, done), cli.ExitSuccess)
			text, e := lifecycleDatagram(notify)
			if !errors.Is(e, syscall.EAGAIN) {
				t.Fatalf("extra notify %q %v", text, e)
			}
			lifecycleEqual(t, out.String(), "")
			lifecycleEqual(t, errout.String(), "")
		})
	}
}

type lifecycleBody struct {
	ctx     context.Context
	release <-chan struct{}
	entered chan struct{}
	once    sync.Once
	r       *bytes.Reader
}

func (b *lifecycleBody) Read(p []byte) (int, error) {
	b.once.Do(func() { close(b.entered) })
	select {
	case <-b.release:
		return b.r.Read(p)
	case <-b.ctx.Done():
		return 0, b.ctx.Err()
	}
}
func (*lifecycleBody) Close() error { return nil }
func lifecycleHeldTransport(release <-chan struct{}, entered chan struct{}) http.RoundTripper {
	g := &fakeMail{drafts: map[string]string{}}
	return roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/attachments/blob") {
			return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"application/json"}}, Body: &lifecycleBody{ctx: r.Context(), release: release, entered: entered, r: bytes.NewReader([]byte(`{"size":4,"data":"bWFpbA"}`))}, Request: r}, nil
		}
		return g.RoundTrip(r)
	})
}

// R-XUM5-5YSM R-XS6C-EFB8 R-XY9U-BA0P
func TestLifecycleInterruptedTransferDrain(t *testing.T) {
	release := make(chan struct{})
	entered := make(chan struct{})
	observed := &lifecycleDeliveries{}
	f := startRun(t, runOptions{Sink: lifecycleTelemetrySink{observed}, EventSink: lifecycleBusSink{observed}, Env: map[string]string{"DRAIN_SECONDS": "1", "TRANSFER_MAX_ACTIVE": "1"}, Transport: lifecycleHeldTransport(release, entered)})
	running := lifecyclePull(t, f, "running.bin")
	lifecycleWait(t, entered)
	queued := lifecyclePull(t, f, "queued.bin")
	cancelAt := time.Now()
	f.Cancel(fmt.Errorf("drain"))
	select {
	case code := <-f.Done:
		f.stopped = true
		lifecycleEqual(t, code, cli.ExitSuccess)
	case <-time.After(2 * time.Second):
		t.Fatal("return exceeded drain plus one second")
	}
	ss := f.reopen()
	for _, id := range []string{running, queued} {
		v, e := ss.GetTransfer(context.Background(), fixtureCaller.UserID, id)
		if e != nil {
			t.Fatal(e)
		}
		lifecycleEqual(t, v.Status, store.StatusFailed)
		lifecycleEqual(t, v.Reason, store.ReasonInterrupted)
	}
	es := observed.events.Events()
	started, finished := 0, 0
	for _, e := range es {
		if e.Name == "transfer.started" {
			started++
			lifecycleEqual(t, e.Attrs["transfer"], running)
		}
		if e.Name == "transfer.finished" {
			finished++
			lifecycleEqual(t, e.Attrs["transfer"], queued)
		}
	}
	lifecycleEqual(t, started, 1)
	lifecycleEqual(t, finished, 1)
	observed.before(t, cancelAt.Add(time.Second))
	var lost events.Event
	for _, line := range strings.Split(f.Err.String(), "\n") {
		if strings.HasPrefix(line, "gmail: lost event: ") {
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "gmail: lost event: ")), &lost); err != nil {
				t.Fatal(err)
			}
		}
	}
	expected := events.Event{ID: lost.ID, Time: f.Process.Now(), Service: "gmail", Name: "transfer.finished", RequestID: fixtureCaller.RequestID, User: fixtureCaller.UserID, Attrs: events.Attrs{"transfer": running, "direction": "pull", "message": "message", "attachment": "part", "draft": "", "path": "running.bin", "status": store.StatusFailed, "reason": store.ReasonInterrupted, "bytes": int64(0)}}
	lifecycleEqual(t, lost, expected)
	lifecycleDiagnostics(t, f.Err.String(), []string{
		lifecycleDiagnostic(t, "lost event", expected),
		lifecycleDiagnostic(t, "undelivered event", telemetry.Event{Time: expected.Time, Service: expected.Service, Name: expected.Name, RequestID: expected.RequestID, User: expected.User, Attrs: telemetry.Attrs(expected.Attrs)}),
		lifecycleDiagnostic(t, "undelivered event", telemetry.Event{Time: expected.Time, Service: "gmail", Name: "event.lost", RequestID: fixtureCaller.RequestID, User: fixtureCaller.UserID, Attrs: telemetry.Attrs{"event": lost.ID, "cause": ""}}),
		lifecycleDiagnostic(t, "undelivered event", lifecycleStopping(f)),
	})
	lifecycleEqual(t, f.Out.String(), "")
}

// R-XVU1-JQJB R-XS6C-EFB8 R-XY9U-BA0P
func TestLifecycleCompletedTransferDrain(t *testing.T) {
	release := make(chan struct{})
	entered := make(chan struct{})
	observed := &lifecycleDeliveries{}
	f := startRun(t, runOptions{Sink: lifecycleTelemetrySink{observed}, EventSink: lifecycleBusSink{observed}, Env: map[string]string{"DRAIN_SECONDS": "5"}, Transport: lifecycleHeldTransport(release, entered)})
	id := lifecyclePull(t, f, "complete.bin")
	lifecycleWait(t, entered)
	cancelAt := time.Now()
	f.Cancel(fmt.Errorf("drain"))
	close(release)
	lifecycleEqual(t, lifecycleWait(t, f.Done), cli.ExitSuccess)
	f.stopped = true
	v, e := f.reopen().GetTransfer(context.Background(), fixtureCaller.UserID, id)
	if e != nil {
		t.Fatal(e)
	}
	lifecycleEqual(t, v.Status, store.StatusDone)
	es := observed.events.Events()
	lifecycleEqual(t, len(es), 2)
	lifecycleEqual(t, es[0].Name, "transfer.started")
	lifecycleEqual(t, es[1].Name, "transfer.finished")
	lifecycleEqual(t, es[0].Attrs["transfer"], id)
	lifecycleEqual(t, es[1].Attrs["transfer"], id)
	observed.before(t, cancelAt.Add(5*time.Second))
	counts := map[string]int{}
	for _, event := range observed.telemetry.Events() {
		counts[event.Name]++
	}
	lifecycleEqual(t, counts, map[string]int{"service.started": 1, "service.stopping": 1, "request.started": 1, "request.finished": 1, "tool.called": 1, "gmail.called": 4, "sibling.called": 4, "transfer.started": 1, "transfer.finished": 1})
	lifecycleEqual(t, f.Out.String(), "")
	lifecycleEqual(t, f.Err.String(), "")
}

// R-XTE8-S71X R-XY9U-BA0P
func TestLifecycleUnfinishedRequestDrain(t *testing.T) {
	release := make(chan struct{})
	entered := make(chan struct{})
	var once sync.Once
	g := &fakeMail{drafts: map[string]string{}}
	tr := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/labels") {
			once.Do(func() { close(entered) })
			<-release
		}
		return g.RoundTrip(r)
	})
	f := startRun(t, runOptions{Env: map[string]string{"DRAIN_SECONDS": "1"}, Transport: tr})
	done := make(chan struct{})
	go func() {
		_, _ = f.Client.CallTool(context.Background(), fixtureCaller, "labels", []byte(`{}`))
		close(done)
	}()
	lifecycleWait(t, entered)
	f.Cancel(fmt.Errorf("drain"))
	lifecycleEqual(t, lifecycleWait(t, f.Done), cli.ExitServerFailed)
	f.stopped = true
	e := &server.DrainError{Unfinished: 1}
	lifecycleEqual(t, f.Err.String(), lifecycleDiagnostic(t, "undelivered event", lifecycleStopping(f))+"\n"+"gmail: "+e.Error()+"\n")
	close(release)
	lifecycleWait(t, done)
	lifecycleEqual(t, f.Out.String(), "")
}

// R-XX1X-XIA0 R-XY9U-BA0P
func TestLifecycleChecksEndingAfterStop(t *testing.T) {
	releaseBody := make(chan struct{})
	bodyEntered := make(chan struct{})
	thirdEntered := make(chan struct{})
	releaseThird := make(chan struct{})
	g := &fakeMail{drafts: map[string]string{}}
	held := lifecycleHeldTransport(releaseBody, bodyEntered)
	tr := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/messages/third") {
			close(thirdEntered)
			<-releaseThird
			return g.RoundTrip(r)
		}
		return held.RoundTrip(r)
	})
	f := startRun(t, runOptions{Env: map[string]string{"DRAIN_SECONDS": "5", "TRANSFER_MAX_ACTIVE": "1"}, Transport: tr})
	running := lifecyclePull(t, f, "running.bin")
	lifecycleWait(t, bodyEntered)
	queued := lifecyclePull(t, f, "queued.bin")
	type answer struct {
		result mcp.Result
		err    error
	}
	third := make(chan answer, 1)
	go func() {
		r, e := f.Client.CallTool(context.Background(), fixtureCaller, "pull", []byte(`{"message":"third","attachment":"part","path":"third.bin"}`))
		third <- answer{r, e}
	}()
	lifecycleWait(t, thirdEntered)
	f.Cancel(fmt.Errorf("stop checks"))
	ss := f.reopen()
	deadline, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for {
		v, e := ss.GetTransfer(context.Background(), fixtureCaller.UserID, queued)
		if e != nil {
			t.Fatal(e)
		}
		if v.Status == store.StatusFailed {
			lifecycleEqual(t, v.Reason, store.ReasonInterrupted)
			break
		}
		select {
		case <-deadline.Done():
			t.Fatal("queue not settled")
		default:
		}
	}
	close(releaseThird)
	a := lifecycleWait(t, third)
	if a.err != nil {
		t.Fatal(a.err)
	}
	assertRefusal(t, a.result, "stopping: "+tools.RefuseStopping)
	close(releaseBody)
	lifecycleEqual(t, lifecycleWait(t, f.Done), cli.ExitSuccess)
	f.stopped = true
	all, e := f.reopen().ListTransfers(context.Background(), fixtureCaller.UserID)
	if e != nil {
		t.Fatal(e)
	}
	lifecycleEqual(t, len(all), 2)
	seen := map[string]bool{}
	for _, v := range all {
		seen[v.ID] = true
	}
	if !seen[running] || !seen[queued] {
		t.Fatal(all)
	}
	lifecycleEqual(t, f.Out.String(), "")
	lifecycleEqual(t, f.Err.String(), "")
}

func lifecycleDatagram(conn *net.UnixConn) (string, error) {
	raw, e := conn.SyscallConn()
	if e != nil {
		return "", e
	}
	buf := make([]byte, 64)
	n := 0
	var readErr error
	e = raw.Read(func(fd uintptr) bool {
		n, _, readErr = syscall.Recvfrom(int(fd), buf, syscall.MSG_DONTWAIT)
		return true
	})
	if e != nil {
		return "", e
	}
	if readErr != nil {
		return "", readErr
	}
	return string(buf[:n]), nil
}

type lifecycleDeliveries struct {
	mu        sync.Mutex
	telemetry telemetry.Capture
	events    events.Capture
	times     []time.Time
}
type lifecycleTelemetrySink struct{ observed *lifecycleDeliveries }

func (s lifecycleTelemetrySink) Deliver(ctx context.Context, e telemetry.Event) error {
	s.observed.mu.Lock()
	s.observed.times = append(s.observed.times, time.Now())
	s.observed.mu.Unlock()
	return s.observed.telemetry.Deliver(ctx, e)
}

type lifecycleBusSink struct{ observed *lifecycleDeliveries }

func (s lifecycleBusSink) Deliver(ctx context.Context, e events.Event) error {
	s.observed.mu.Lock()
	s.observed.times = append(s.observed.times, time.Now())
	s.observed.mu.Unlock()
	return s.observed.events.Deliver(ctx, e)
}
func (s *lifecycleDeliveries) before(t *testing.T, deadline time.Time) {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.times) == 0 {
		t.Fatal("no observed deliveries")
	}
	for _, at := range s.times {
		if !at.Before(deadline) {
			t.Fatalf("delivery began %v after shared drain deadline %v", at, deadline)
		}
	}
}
func lifecycleDiagnostic(t *testing.T, prefix string, e any) string {
	t.Helper()
	b, err := json.Marshal(e)
	if err != nil {
		t.Fatal(err)
	}
	return "gmail: " + prefix + ": " + string(b)
}
func lifecycleDiagnostics(t *testing.T, got string, want []string) {
	t.Helper()
	if !strings.HasSuffix(got, "\n") {
		t.Fatalf("unterminated stderr: %q", got)
	}
	lines := strings.Split(strings.TrimSuffix(got, "\n"), "\n")
	sort.Strings(lines)
	sort.Strings(want)
	lifecycleEqual(t, lines, want)
}
func lifecycleStopping(f *runFixture) telemetry.Event {
	return telemetry.Event{Time: f.Process.Now(), Service: "gmail", Name: "service.stopping", Attrs: telemetry.Attrs{"reason": "drain"}}
}

// R-XY9U-BA0P
func TestLifecycleFailedTransferIsSilent(t *testing.T) {
	g := &fakeMail{drafts: map[string]string{}}
	f := startRun(t, runOptions{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "/attachments/blob") {
			return jsonResponse(r, 404, map[string]any{}), nil
		}
		return g.RoundTrip(r)
	})})
	id := lifecyclePull(t, f, "failed.bin")
	ended := f.awaitTransfer(fixtureCaller, id)
	lifecycleEqual(t, ended["status"], store.StatusFailed)
	lifecycleEqual(t, f.stop(), cli.ExitSuccess)
	lifecycleEqual(t, f.Out.String(), "")
	lifecycleEqual(t, f.Err.String(), "")
}
