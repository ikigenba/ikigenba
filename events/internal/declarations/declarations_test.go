package declarations_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	contract "github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	root "github.com/ikigenba/ikigenba/events"
	"github.com/ikigenba/ikigenba/events/internal/declarations"
	"github.com/ikigenba/ikigenba/events/internal/store"
)

type harness struct {
	t         *testing.T
	dir, file string
	handle    *db.DB
	store     *store.Store
	writer    *telemetry.Writer
	capture   *telemetry.Capture
	sockets   int
}

func setup(t *testing.T) *harness {
	t.Helper()
	t.Setenv("IKIGENBA_SERVICES", "")
	dir, err := os.MkdirTemp("", "decl-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	now := func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }
	handle, err := db.Open(context.Background(), db.Config{Path: filepath.Join(dir, "log.db"), Migrations: root.Migrations(), Now: now})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = handle.Close() })
	capture := &telemetry.Capture{}
	writer := telemetry.New(telemetry.Config{Service: "events", Version: "test", Sink: capture, Stderr: io.Discard, Now: now, Sleep: func(context.Context, time.Duration) {}, Rand: bytes.NewReader(bytes.Repeat([]byte{1}, 65536))})
	writer.Ready()
	if err := writer.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { writer.Shutdown(context.Background(), "test") })
	return &harness{t: t, dir: dir, file: filepath.Join(dir, "services.json"), handle: handle, store: store.New(handle, store.Config{Now: now, DepthMax: 8}), writer: writer, capture: capture}
}
func (h *harness) service(name string, enabled bool, socket string) map[string]any {
	return map[string]any{"name": name, "enabled": enabled, "socket": socket, "url": "", "description": "", "mcp": false}
}
func (h *harness) list(entries ...map[string]any) {
	h.t.Helper()
	data, err := json.Marshal(map[string]any{"services": entries})
	if err != nil {
		h.t.Fatal(err)
	}
	if err := os.WriteFile(h.file, data, 0600); err != nil {
		h.t.Fatal(err)
	}
}
func (h *harness) server(handler http.HandlerFunc) string {
	h.t.Helper()
	h.sockets++
	socket := filepath.Join(h.dir, fmt.Sprint("socket-", h.sockets))
	listener, err := net.Listen("unix", socket)
	if err != nil {
		h.t.Fatal(err)
	}
	server := &http.Server{Handler: handler, ReadHeaderTimeout: time.Second}
	go func() { _ = server.Serve(listener) }()
	h.t.Cleanup(func() { _ = server.Close() })
	return socket
}
func (h *harness) manager(after func(time.Duration) <-chan time.Time, joined func(string)) *declarations.Declarations {
	return declarations.New(declarations.Config{Store: h.store, Services: h.file, Telemetry: h.writer, AskAfter: after, Joined: joined})
}
func (h *harness) held() map[string]store.Declaration {
	h.t.Helper()
	result, err := h.store.Declarations(context.Background())
	if err != nil {
		h.t.Fatal(err)
	}
	return result
}
func (h *harness) declare(name string, d store.Declaration) {
	h.t.Helper()
	if err := h.store.Declare(context.Background(), name, d); err != nil {
		h.t.Fatal(err)
	}
}
func wait(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal("operation did not finish")
	}
}
func (h *harness) snapshot() []telemetry.Event {
	h.t.Helper()
	if err := h.writer.Flush(context.Background()); err != nil {
		h.t.Fatal(err)
	}
	return h.capture.Events()
}

func (h *harness) recordsSince(before []telemetry.Event) []telemetry.Event {
	h.t.Helper()
	after := h.snapshot()
	if len(after) < len(before) || !reflect.DeepEqual(after[:len(before)], before) {
		h.t.Fatal("telemetry history changed")
	}
	return after[len(before):]
}

func (h *harness) siblingRecords(before []telemetry.Event, expected map[string]int64) {
	h.t.Helper()
	records := h.recordsSince(before)
	if len(records) != len(expected) {
		h.t.Fatalf("telemetry %#v, want %d sibling calls", records, len(expected))
	}
	seen := make(map[string]bool)
	for _, e := range records {
		target, ok := e.Attrs["target"].(string)
		status, exists := expected[target]
		if e.Name != "sibling.called" || !ok || !exists || seen[target] || e.Attrs["method"] != "GET" || e.Attrs["path"] != contract.DeclarationsPath || e.Attrs["status"] != status {
			h.t.Fatalf("unexpected telemetry %#v", e)
		}
		if duration, ok := e.Attrs["duration_us"].(int64); !ok || duration < 0 {
			h.t.Fatalf("duration %#v", e)
		}
		seen[target] = true
	}
}

func noTimer(time.Duration) <-chan time.Time { return make(chan time.Time) }

var old = store.Declaration{Emits: []contract.Emission{{Event: "repo.old", Attrs: []string{}}}, Accepts: []string{"repo.old"}}

const answer = `{"emits":[{"event":"repo.pushed","attrs":["branch","commit"]}],"accepts":["repo.pushed","*"]}`

// R-2Z1S-TVT6 R-EJPH-47NH R-EM59-VR4V R-355A-QQIN R-309P-7NJV R-EZK6-38AI
func TestAskContract(t *testing.T) {
	h := setup(t)
	if declarations.AskTimeout != 2*time.Second {
		t.Fatal("ask timeout constant")
	}
	var calls atomic.Int64
	socket := h.server(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Method != "GET" || r.URL.Path != contract.DeclarationsPath {
			t.Errorf("request %s %s", r.Method, r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		if len(body) != 0 {
			t.Error("request has body")
		}
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, answer)
	})
	h.list(h.service("repos", true, socket))
	h.declare("other", old)
	if err := h.store.Deliver(context.Background(), contract.Event{ID: "evt_0000000000000001", Time: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Service: "other", Name: "repo.old", Attrs: contract.Attrs{}}); err != nil {
		t.Fatal(err)
	}
	before, _ := h.store.Search(context.Background(), store.Filter{}, 100, "")
	head, _ := h.store.Head(context.Background())
	var timerCalls int
	d := h.manager(func(duration time.Duration) <-chan time.Time {
		timerCalls++
		if duration != declarations.AskTimeout {
			t.Errorf("duration %s", duration)
		}
		return noTimer(duration)
	}, nil)
	cfg := store.Config{Ask: d.Ask}
	telemetryBefore := h.snapshot()
	cfg.Ask(context.Background(), "repos")
	expected := store.Declaration{Emits: []contract.Emission{{Event: "repo.pushed", Attrs: []string{"branch", "commit"}}}, Accepts: []string{"repo.pushed", "*"}}
	if !reflect.DeepEqual(h.held(), map[string]store.Declaration{"repos": expected, "other": old}) {
		t.Fatalf("held %#v", h.held())
	}
	after, _ := h.store.Search(context.Background(), store.Filter{}, 100, "")
	afterHead, _ := h.store.Head(context.Background())
	if head != afterHead || !reflect.DeepEqual(before, after) {
		t.Fatal("log changed")
	}
	if calls.Load() != 1 || timerCalls != 1 {
		t.Fatalf("calls %d timers %d", calls.Load(), timerCalls)
	}
	h.siblingRecords(telemetryBefore, map[string]int64{"repos": 200})
	telemetryBefore = h.snapshot()
	d.Refresh(context.Background())
	h.siblingRecords(telemetryBefore, map[string]int64{"repos": 200})
	refreshed, err := h.store.Search(context.Background(), store.Filter{}, 100, "")
	if err != nil || !reflect.DeepEqual(before, refreshed) {
		t.Fatalf("refresh changed log: %v %#v", err, refreshed)
	}
	refreshHead, err := h.store.Head(context.Background())
	if err != nil || refreshHead != head {
		t.Fatalf("refresh head %d %v", refreshHead, err)
	}
}

// R-YV6W-GECE
func TestDeclarationAnswers(t *testing.T) {
	cases := []struct {
		body   string
		status int
		valid  bool
	}{
		{answer, 200, true}, {` {"emits":[],"accepts":[],"extra":true} `, 200, true}, {`{"emits":[{"event":"a_b.c_d","attrs":["x_y","x_y"],"extra":1},{"event":"a_b.c_d","attrs":[]}],"accepts":["*","a_b.c_d","a_b.c_d"]}`, 200, true},
		{`{"emits":[{"event":"cron.*.fired","attrs":["schedule","schedule"]},{"event":"cron.hourly.fired","attrs":[]},{"event":"a.b.c.d","attrs":[]},{"event":"*.*","attrs":[]},{"event":"cron.*.fired","attrs":["later"]}],"accepts":["cron.*.fired","cron.hourly.fired","a.b.c.d","*.*","*","cron.*.fired"]}`, 200, true},
		{answer, 404, false}, {answer, 500, false}, {`no`, 200, false}, {`[]`, 200, false}, {`null`, 200, false}, {`{"emits":"repo.pushed","accepts":[]}`, 200, false}, {`{"emits":[],"accepts":null}`, 200, false}, {`{"emits":[]}`, 200, false}, {`{"emits":[{"event":"Repo.Pushed","attrs":[]}],"accepts":[]}`, 200, false}, {`{"emits":[{"event":"repo.pushed","attrs":["Bad"]}],"accepts":[]}`, 200, false}, {`{"emits":[{"event":"repo.pushed"}],"accepts":[]}`, 200, false}, {`{"emits":[],"accepts":["bad"]}`, 200, false}, {answer + ` {}`, 200, false}, {answer + strings.Repeat(" ", contract.MaxEventBytes), 200, false}, {answer + strings.Repeat(" ", contract.MaxEventBytes-len(answer)), 200, true},
		{`{"emits":null,"accepts":[]}`, 200, false}, {`{"accepts":[]}`, 200, false}, {`{"emits":[null],"accepts":[]}`, 200, false}, {`{"emits":["repo.pushed"],"accepts":[]}`, 200, false}, {`{"emits":[{"attrs":[]}],"accepts":[]}`, 200, false}, {`{"emits":[{"event":17,"attrs":[]}],"accepts":[]}`, 200, false}, {`{"emits":[{"event":"repo.pushed","attrs":null}],"accepts":[]}`, 200, false}, {`{"emits":[{"event":"repo.pushed","attrs":[17]}],"accepts":[]}`, 200, false}, {`{"emits":[{"event":"repo.pushed","attrs":[null]}],"accepts":[]}`, 200, false}, {`{"emits":[],"accepts":"*"}`, 200, false}, {`{"emits":[],"accepts":[17]}`, 200, false}, {`{"emits":[],"accepts":[null]}`, 200, false},
	}
	for _, event := range []string{"pushed", "*", "cron.h*.fired", "repo.", "", "a..b", ".repo.pushed", "repo.pushed.", "cron.**.fired"} {
		cases = append(cases, struct {
			body   string
			status int
			valid  bool
		}{fmt.Sprintf(`{"emits":[{"event":%q,"attrs":[]}],"accepts":[]}`, event), 200, false})
	}
	for _, event := range []string{"Repo.Pushed", "pushed", "cron.h*.fired", "repo.", "", "a..b", "cron.**.fired"} {
		cases = append(cases, struct {
			body   string
			status int
			valid  bool
		}{fmt.Sprintf(`{"emits":[],"accepts":[%q]}`, event), 200, false})
	}
	for _, attr := range []string{"", "_branch", "branch_", "branch__name", "branch.name", "2branch", "branch-name"} {
		cases = append(cases, struct {
			body   string
			status int
			valid  bool
		}{fmt.Sprintf(`{"emits":[{"event":"repo.pushed","attrs":[%q]}],"accepts":[]}`, attr), 200, false})
	}
	for index, tc := range cases {
		t.Run(string(rune('a'+index)), func(t *testing.T) {
			h := setup(t)
			socket := h.server(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "text/plain")
				w.WriteHeader(tc.status)
				_, _ = io.WriteString(w, tc.body)
			})
			h.list(h.service("repos", true, socket))
			h.declare("repos", old)
			h.manager(noTimer, nil).Ask(context.Background(), "repos")
			got := h.held()["repos"]
			if tc.valid {
				var expected store.Declaration
				if err := json.Unmarshal([]byte(tc.body), &expected); err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(got, expected) {
					t.Fatalf("got %#v want %#v", got, expected)
				}
			} else if !reflect.DeepEqual(got, old) {
				t.Fatalf("invalid replaced held: %#v", got)
			}
		})
	}
	t.Run("no response", func(t *testing.T) {
		h := setup(t)
		h.list(h.service("repos", true, filepath.Join(h.dir, "absent")))
		h.declare("repos", old)
		h.manager(noTimer, nil).Ask(context.Background(), "repos")
		if got := h.held()["repos"]; !reflect.DeepEqual(got, old) {
			t.Fatalf("absent response replaced held: %#v", got)
		}
	})
}

// R-TJY5-OU12 R-32PH-Z719
func TestUnaskableAndFreshFile(t *testing.T) {
	h := setup(t)
	var calls atomic.Int64
	socket := h.server(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); _, _ = io.WriteString(w, answer) })
	h.list(h.service("repos", false, socket), h.service(contract.ServiceName, true, socket))
	h.declare("held", old)
	var allowTimer atomic.Bool
	var timers atomic.Int64
	d := h.manager(func(time.Duration) <-chan time.Time {
		timers.Add(1)
		if !allowTimer.Load() {
			t.Error("unexpected timer")
		}
		return noTimer(0)
	}, func(string) { t.Error("unexpected join") })
	telemetryBefore := h.snapshot()
	for _, name := range []string{"", "repos", "missing", contract.ServiceName} {
		d.Ask(context.Background(), name)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	h.list(h.service("repos", true, socket))
	d.Ask(ctx, "repos")
	if calls.Load() != 0 || !reflect.DeepEqual(h.held(), map[string]store.Declaration{"held": old}) {
		t.Fatal("unaskable changed state")
	}
	h.siblingRecords(telemetryBefore, nil)
	if timers.Load() != 0 {
		t.Fatal("unaskable armed timer")
	}
	allowTimer.Store(true)
	telemetryBefore = h.snapshot()
	d.Ask(context.Background(), "repos")
	if calls.Load() != 1 || timers.Load() != 1 {
		t.Fatal("file was not reread")
	}
	h.siblingRecords(telemetryBefore, map[string]int64{"repos": 200})
	telemetryBefore = h.snapshot()
	empty := declarations.New(declarations.Config{Store: h.store, Telemetry: h.writer, AskAfter: func(time.Duration) <-chan time.Time { t.Error("empty path asked"); return nil }})
	before := h.held()
	empty.Ask(context.Background(), "repos")
	h.siblingRecords(telemetryBefore, nil)
	if !reflect.DeepEqual(before, h.held()) {
		t.Fatal("empty path changed declarations")
	}
}

// R-EPSZ-12CY R-TL62-2LRR R-TMDY-GDIG R-TNLU-U595 R-F0S2-H017 R-355A-QQIN
func TestRefresh(t *testing.T) {
	h := setup(t)
	all := make(chan struct{})
	var asked atomic.Int64
	handler := func(w http.ResponseWriter, _ *http.Request) {
		if asked.Add(1) == 2 {
			close(all)
		}
		<-all
		_, _ = io.WriteString(w, answer)
	}
	first := h.server(handler)
	second := h.server(handler)
	bad := h.server(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(500) })
	h.list(h.service("first", true, first), h.service("first", true, bad), h.service("second", true, second), h.service("bad", true, bad), h.service("disabled", false, bad), h.service(contract.ServiceName, true, bad))
	for _, name := range []string{"bad", "disabled", "removed", contract.ServiceName} {
		h.declare(name, old)
	}
	d := h.manager(noTimer, nil)
	telemetryBefore := h.snapshot()
	done := make(chan struct{})
	go func() { d.Refresh(context.Background()); close(done) }()
	wait(t, done)
	h.siblingRecords(telemetryBefore, map[string]int64{"first": 200, "second": 200, "bad": 500})
	held := h.held()
	if len(held) != 3 || !reflect.DeepEqual(held["bad"], old) || len(held["first"].Emits) != 1 || len(held["second"].Emits) != 1 {
		t.Fatalf("held %#v", held)
	}
	before, err := h.store.Subscribers(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, content := range []string{"broken", `{"services":null}`} {
		if err := os.WriteFile(h.file, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
		telemetryBefore = h.snapshot()
		d.Refresh(context.Background())
		h.siblingRecords(telemetryBefore, nil)
		if !reflect.DeepEqual(held, h.held()) {
			t.Fatal("later missing list changed declarations")
		}
		after, err := h.store.Subscribers(context.Background())
		if err != nil || !reflect.DeepEqual(before, after) {
			t.Fatalf("subscriber changed %v %#v", err, after)
		}
	}
	if err := os.Remove(h.file); err != nil {
		t.Fatal(err)
	}
	telemetryBefore = h.snapshot()
	d.Refresh(context.Background())
	h.siblingRecords(telemetryBefore, nil)
	if !reflect.DeepEqual(held, h.held()) {
		t.Fatal("missing file changed declarations")
	}
	telemetryBefore = h.snapshot()
	h.manager(noTimer, nil).Refresh(context.Background())
	h.siblingRecords(telemetryBefore, nil)
	if len(h.held()) != 0 {
		t.Fatal("start no list did not forget")
	}
	h.list(h.service("bad", true, bad))
	h.handle.SetFailing(true)
	d.Refresh(context.Background())
	h.handle.SetFailing(false)
	var group sync.WaitGroup
	for range 8 {
		group.Go(func() { d.Refresh(context.Background()); d.Ask(context.Background(), "bad") })
	}
	group.Wait()
}

// R-EOL2-NAM9 R-TNLU-U595 R-355A-QQIN
func TestAskDeadlineAndRefreshCancellation(t *testing.T) {
	for _, mode := range []string{"deadline", "cancel", "gone"} {
		t.Run(mode, func(t *testing.T) {
			h := setup(t)
			entered := make(chan struct{})
			socket := filepath.Join(h.dir, "absent")
			if mode != "gone" {
				socket = h.server(func(w http.ResponseWriter, r *http.Request) {
					close(entered)
					w.WriteHeader(200)
					w.(http.Flusher).Flush()
					<-r.Context().Done()
				})
			}
			h.list(h.service("repos", true, socket))
			h.declare("repos", old)
			timer := make(chan time.Time, 1)
			var timers atomic.Int64
			d := h.manager(func(duration time.Duration) <-chan time.Time {
				if duration != declarations.AskTimeout {
					t.Errorf("timeout %s", duration)
				}
				timers.Add(1)
				return timer
			}, nil)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			telemetryBefore := h.snapshot()
			done := make(chan struct{})
			go func() { d.Refresh(ctx); close(done) }()
			if mode != "gone" {
				wait(t, entered)
				if mode == "deadline" {
					timer <- time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
				} else {
					cancel()
				}
			}
			wait(t, done)
			if timers.Load() != 1 || !reflect.DeepEqual(h.held()["repos"], old) {
				t.Fatal("deadline replaced declaration")
			}
			h.siblingRecords(telemetryBefore, map[string]int64{"repos": 0})
		})
	}
}

// R-31HL-LFAK R-33XE-CYRY R-309P-7NJV
func TestJoinedAskOutlivesCallers(t *testing.T) {
	h := setup(t)
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	socket := h.server(func(w http.ResponseWriter, _ *http.Request) {
		entered <- struct{}{}
		<-release
		_, _ = io.WriteString(w, answer)
	})
	h.list(h.service("repos", true, socket))
	joined := make(chan struct{}, 1)
	var timers atomic.Int64
	d := h.manager(func(time.Duration) <-chan time.Time { timers.Add(1); return noTimer(0) }, func(name string) {
		if name != "repos" {
			t.Errorf("join %s", name)
		}
		joined <- struct{}{}
	})
	ctx, cancel := context.WithCancel(context.Background())
	one := make(chan struct{})
	go func() { d.Ask(ctx, "repos"); close(one) }()
	wait(t, entered)
	ctx2, cancel2 := context.WithCancel(context.Background())
	two := make(chan struct{})
	go func() { d.Ask(ctx2, "repos"); close(two) }()
	wait(t, joined)
	cancel()
	cancel2()
	wait(t, one)
	wait(t, two)
	if timers.Load() != 1 {
		t.Fatalf("timers %d", timers.Load())
	}
	select {
	case <-entered:
		t.Fatal("join made another request")
	default:
	}
	// A third waiter observes the held result without issuing a second request.
	three := make(chan struct{})
	go func() { d.Ask(context.Background(), "repos"); close(three) }()
	wait(t, joined)
	close(release)
	wait(t, three)
	if got := h.held()["repos"]; len(got.Emits) != 1 || got.Emits[0].Event != "repo.pushed" {
		t.Fatalf("detached answer %#v", got)
	}
	// A later ask must be issued again rather than rate limited or cached.
	d.Ask(context.Background(), "repos")
	if timers.Load() != 2 {
		t.Fatalf("later timers %d", timers.Load())
	}
}
