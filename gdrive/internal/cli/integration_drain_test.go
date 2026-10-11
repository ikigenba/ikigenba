package cli_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/gdrive/internal/cli"
	"github.com/ikigenba/ikigenba/gdrive/internal/store"
)

type integrationSequence struct{ n byte }

func (s *integrationSequence) Read(p []byte) (int, error) {
	s.n++
	for i := range p {
		p[i] = s.n
	}
	return len(p), nil
}

type integrationHeldBody struct {
	ctx     context.Context
	release <-chan struct{}
	prefix  *strings.Reader
	once    bool
}

func (b *integrationHeldBody) Read(p []byte) (int, error) {
	if b.prefix.Len() > 0 {
		return b.prefix.Read(p)
	}
	if !b.once {
		select {
		case <-b.ctx.Done():
			return 0, b.ctx.Err()
		case <-b.release:
			b.once = true
			return 0, io.EOF
		}
	}
	return 0, io.EOF
}
func (*integrationHeldBody) Close() error { return nil }

type integrationDrainRig struct {
	run         *integrationRun
	media       chan struct{}
	release     chan struct{}
	head        chan struct{}
	releaseHead chan struct{}
	blockedCall chan struct{}
	releaseCall chan struct{}
	once        sync.Once
}

func integrationDrain(t *testing.T) *integrationDrainRig {
	t.Helper()
	d := integrationSocketDir(t)
	f := &integrationDrainRig{media: make(chan struct{}), release: make(chan struct{}), head: make(chan struct{}), releaseHead: make(chan struct{}), blockedCall: make(chan struct{}), releaseCall: make(chan struct{})}
	cs := integrationUnix(t, d, "connections", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": strings.Repeat("Zg7Q", 10), "expires": "2030-01-01T00:00:00Z", "account": "account", "scopes": []string{}})
	}))
	as := integrationUnix(t, d, "artifacts", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "HEAD" {
			if r.URL.Path == "/files/third" {
				close(f.head)
				<-f.releaseHead
			}
			w.WriteHeader(404)
			return
		}
		b, e := io.ReadAll(r.Body)
		if e != nil {
			return
		}
		w.WriteHeader(201)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "artifact", "path": "first", "content_type": "application/octet-stream", "sha256": "hash", "owner": "chosen-user", "url": "https://artifacts.invalid/file", "bytes": len(b), "created": "2026-01-20T12:00:00Z", "modified": "2026-01-20T12:00:00Z"})
	}))
	path := integrationServices(t, d, integrationEntry("connections", cs, ""), integrationEntry("artifacts", as, ""))
	f.run = integrationNew(t, path, integrationRoundTripper(func(q *http.Request) (*http.Response, error) {
		if strings.HasSuffix(q.URL.Path, "/drives") {
			close(f.blockedCall)
			select {
			case <-f.releaseCall:
			case <-q.Context().Done():
				return nil, q.Context().Err()
			}
			return integrationResponse(q, `{"drives":[]}`), nil
		}
		if q.URL.Query().Get("alt") == "media" {
			f.once.Do(func() { close(f.media) })
			v := integrationResponse(q, "")
			v.Body = &integrationHeldBody{ctx: q.Context(), release: f.release, prefix: strings.NewReader(strings.Repeat("Yh8Rx2Lm9Pw3Ks6T", 4096))}
			return v, nil
		}
		return integrationResponse(q, `{"id":"source","name":"held","mimeType":"application/octet-stream","size":"65536"}`), nil
	}))
	f.run.p.Rand = &integrationSequence{}
	f.run.env["TRANSFER_MAX_ACTIVE"] = "1"
	return f
}
func integrationWait(t *testing.T, c <-chan struct{}) {
	t.Helper()
	select {
	case <-c:
	case <-time.After(5 * time.Second):
		t.Fatal("fake never reached")
	}
}
func integrationID(t *testing.T, v any) string {
	t.Helper()
	var o struct {
		Structured struct {
			ID string `json:"id"`
		} `json:"structuredContent"`
	}
	if e := json.Unmarshal([]byte(integrationJSON(t, v)), &o); e != nil {
		t.Fatal(e)
	}
	if o.Structured.ID == "" {
		t.Fatal("missing transfer", integrationJSON(t, v))
	}
	return o.Structured.ID
}

// R-KURB-OXFM R-AX5Y-DLEA R-L3AM-DBMH R-L4II-R3D6
func TestIntegrationDrainCutsRunningAndQueue(t *testing.T) {
	f := integrationDrain(t)
	r := f.run
	r.env["DRAIN_SECONDS"] = "1"
	r.start(t)
	first := integrationID(t, r.call(t, "pull", `{"file":"source","path":"first"}`))
	integrationWait(t, f.media)
	second := integrationID(t, r.call(t, "pull", `{"file":"source","path":"second"}`))
	r.waitTransfer(t, second, store.StatusQueued)
	start := time.Now()
	if code := r.stop(t); code != cli.ExitSuccess {
		t.Fatal(code, r.err.String())
	}
	if time.Since(start) > 2*time.Second {
		t.Fatal("drain exceeded deadline plus one second")
	}
	st, closeDB := integrationStore(t, r.dir)
	defer closeDB()
	for _, id := range []string{first, second} {
		v, e := st.Get(context.Background(), integrationCaller.UserID, id)
		if e != nil || v.Status != store.StatusFailed || v.Reason != store.ReasonInterrupted {
			t.Fatalf("%+v %v", v, e)
		}
	}
	started, finished := false, false
	for _, v := range r.bus.Events() {
		if v.Attrs["transfer"] == first {
			started = started || v.Name == "transfer.started"
			finished = finished || v.Name == "transfer.finished"
		}
	}
	if !started || finished {
		t.Fatalf("cutoff event started=%v finished=%v", started, finished)
	}
	for _, output := range []string{integrationJSON(t, r.trace.Events()), integrationJSON(t, r.bus.Events()), r.out.String(), r.err.String()} {
		if strings.Contains(output, strings.Repeat("Zg7Q", 10)) {
			t.Error("token leaked at interruption")
		}
		body := strings.Repeat("Yh8Rx2Lm9Pw3Ks6T", 2)
		for i := 0; i < 16; i++ {
			if strings.Contains(output, body[i:i+16]) {
				t.Error("body leaked at interruption")
				break
			}
		}
	}
	if !strings.Contains(r.err.String(), "lost event") || !strings.Contains(r.err.String(), "transfer.finished") {
		t.Fatal("missing lost event", r.err.String())
	}
}

// R-KZMX-80EE
func TestIntegrationDrainAllowsCompletion(t *testing.T) {
	f := integrationDrain(t)
	r := f.run
	r.env["DRAIN_SECONDS"] = "5"
	r.start(t)
	id := integrationID(t, r.call(t, "pull", `{"file":"source","path":"first"}`))
	integrationWait(t, f.media)
	r.cancel(fmt.Errorf("test-stop"))
	close(f.release)
	if r.stop(t) != cli.ExitSuccess {
		t.Fatal(r.err.String())
	}
	st, closeDB := integrationStore(t, r.dir)
	defer closeDB()
	v, e := st.Get(context.Background(), integrationCaller.UserID, id)
	if e != nil || v.Status != store.StatusDone {
		t.Fatalf("%+v %v", v, e)
	}
	if len(r.bus.Events()) != 2 || r.err.Len() != 0 {
		t.Fatalf("events/stream %v %q", r.bus.Events(), r.err.String())
	}
}

// R-AVY1-ZTNL
func TestIntegrationDrainUnfinishedRequest(t *testing.T) {
	f := integrationDrain(t)
	r := f.run
	r.env["DRAIN_SECONDS"] = "1"
	r.start(t)
	ended := make(chan struct{})
	go func() {
		defer close(ended)
		_, _ = r.client.CallTool(context.Background(), integrationCaller, "drives", nil)
	}()
	integrationWait(t, f.blockedCall)
	if r.stop(t) != cli.ExitServerFailed {
		t.Fatal("unfinished request returned success")
	}
	integrationWait(t, ended)
	if !strings.HasSuffix(r.err.String(), "gdrive: stopped with 1 request unfinished\n") {
		t.Fatal(r.err.String())
	}
}

type integrationQueueSink struct {
	events.Capture
	settled chan struct{}
	once    sync.Once
}

func (s *integrationQueueSink) Deliver(ctx context.Context, v events.Event) error {
	e := s.Capture.Deliver(ctx, v)
	if v.Name == "transfer.finished" && v.Attrs["path"] == "second" {
		s.once.Do(func() { close(s.settled) })
	}
	return e
}

// R-L0UT-LS53
func TestIntegrationLateValidationRefusedWhileDraining(t *testing.T) {
	f := integrationDrain(t)
	r := f.run
	r.env["DRAIN_SECONDS"] = "5"
	sink := &integrationQueueSink{settled: make(chan struct{})}
	r.p.EventSink = sink
	r.start(t)
	_ = integrationID(t, r.call(t, "pull", `{"file":"source","path":"first"}`))
	integrationWait(t, f.media)
	_ = integrationID(t, r.call(t, "pull", `{"file":"source","path":"second"}`))
	answer := make(chan string, 1)
	go func() {
		v, e := r.client.CallTool(context.Background(), integrationCaller, "pull", json.RawMessage(`{"file":"source","path":"third"}`))
		if e != nil {
			answer <- e.Error()
			return
		}
		answer <- integrationJSON(t, v)
	}()
	integrationWait(t, f.head)
	r.cancel(fmt.Errorf("test-stop"))
	integrationWait(t, sink.settled)
	close(f.releaseHead)
	select {
	case s := <-answer:
		if !strings.Contains(s, "stopping") {
			t.Fatal(s)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("third pull not answered")
	}
	close(f.release)
	if r.stop(t) != cli.ExitSuccess {
		t.Fatal(r.err.String())
	}
	st, closeDB := integrationStore(t, r.dir)
	defer closeDB()
	list, e := st.List(context.Background(), integrationCaller.UserID)
	if e != nil || len(list) != 2 {
		t.Fatal(list, e)
	}
}

// R-L5QF-4V3V
func TestIntegrationQueuedAndRecoveredTransferIdentity(t *testing.T) {
	f := integrationDrain(t)
	r := f.run
	r.start(t)
	first := integrationID(t, r.call(t, "pull", `{"file":"source","path":"first"}`))
	integrationWait(t, f.media)
	secondCaller := integrationCaller
	secondCaller.RequestID = "second-request"
	v, e := r.client.CallTool(context.Background(), secondCaller, "pull", json.RawMessage(`{"file":"source","path":"second"}`))
	if e != nil {
		t.Fatal(e)
	}
	second := integrationID(t, v)
	r.waitTransfer(t, second, store.StatusQueued)
	close(f.release)
	r.waitTransfer(t, second, store.StatusDone)
	if r.stop(t) != cli.ExitSuccess {
		t.Fatal(r.err.String())
	}
	integrationCheckTransferEvents(t, r, first, integrationCaller, true)
	integrationCheckTransferEvents(t, r, second, secondCaller, true)
	st, closeDB := integrationStore(t, r.dir)
	recovered, e := st.Add(context.Background(), store.Transfer{Direction: store.DirectionPull, Status: store.StatusRunning, Path: "restart", File: "source", UserID: integrationCaller.UserID, UserEmail: integrationCaller.Email, RequestID: "restart-request"})
	if e != nil {
		t.Fatal(e)
	}
	closeDB()
	restart := integrationNew(t, r.env["IKIGENBA_SERVICES"], r.p.HTTP.Transport)
	restart.dir = r.dir
	restart.p.Dir = r.dir
	restart.start(t)
	if restart.stop(t) != cli.ExitSuccess {
		t.Fatal(restart.err.String())
	}
	caller := integrationCaller
	caller.RequestID = "restart-request"
	integrationCheckTransferEvents(t, restart, recovered.ID, caller, false)
}
