package cli_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/scripts"
	"github.com/ikigenba/ikigenba/scripts/internal/runs"
	"github.com/ikigenba/ikigenba/scripts/internal/store"
)

// Each request carries a distinct identity correlation, including polling reads.
type trailClient struct {
	h *runHarness
	n int
}

func (c *trailClient) call(tool string, args any) (map[string]any, string, bool) {
	c.h.t.Helper()
	c.n++
	id := fmt.Sprintf("trail-%d", c.n)
	b, e := json.Marshal(args)
	mustCLI(c.h.t, e)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	r, e := c.h.client.CallTool(ctx, identity.Caller{UserID: "owner", Email: "private@example.test", RequestID: id}, tool, b)
	mustCLI(c.h.t, e)
	b, e = r.MarshalJSON()
	mustCLI(c.h.t, e)
	var v struct {
		Object map[string]any `json:"structuredContent"`
	}
	mustCLI(c.h.t, json.Unmarshal(b, &v))
	return v.Object, id, r.IsError()
}
func (c *trailClient) ok(tool string, args any) (map[string]any, string) {
	c.h.t.Helper()
	v, id, bad := c.call(tool, args)
	if bad {
		c.h.t.Fatalf("refused %s", tool)
	}
	return v, id
}
func (c *trailClient) ended(id any) map[string]any {
	c.h.t.Helper()
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	for {
		v, _ := c.ok("result", map[string]any{"run": id})
		if v["status"] != "running" {
			return v
		}
		select {
		case <-deadline.C:
			c.h.t.Fatal("run did not end")
		default:
		}
	}
}
func domain(e telemetry.Event) bool {
	return strings.HasPrefix(e.Name, "script.") || strings.HasPrefix(e.Name, "run.")
}
func trailWindow(t *testing.T, es []telemetry.Event, id string) []telemetry.Event {
	t.Helper()
	start, end := -1, -1
	for i, e := range es {
		if e.RequestID == id && e.Name == "request.started" {
			if start != -1 {
				t.Fatal("duplicate request start")
			}
			start = i
		}
		if e.RequestID == id && e.Name == "request.finished" {
			end = i
		}
	}
	if start < 0 || end <= start {
		t.Fatalf("missing request window %s", id)
	}
	return es[start : end+1]
}
func trailEvent(t *testing.T, es []telemetry.Event, name string, run any) telemetry.Event {
	t.Helper()
	var found []telemetry.Event
	for _, e := range es {
		if e.Name == name && (run == nil || e.Attrs["run"] == run) {
			found = append(found, e)
		}
	}
	if len(found) != 1 {
		t.Fatalf("%s %v: %d events", name, run, len(found))
	}
	return found[0]
}
func expectAttrs(t *testing.T, e telemetry.Event, want telemetry.Attrs) {
	t.Helper()
	if !reflect.DeepEqual(e.Attrs, want) {
		t.Fatalf("%s attrs %#v want %#v", e.Name, e.Attrs, want)
	}
}
func trailScript(t *testing.T, es []telemetry.Event, id, name string, script any) {
	t.Helper()
	window := trailWindow(t, es, id)
	count := 0
	called := false
	for _, e := range window {
		if e.Name == "tool.called" {
			called = true
		}
		if domain(e) && e.RequestID == id {
			count++
			if called || e.Name != name || e.User != "owner" {
				t.Fatalf("script event order/identity %#v", e)
			}
			expectAttrs(t, e, telemetry.Attrs{"script": script})
		}
	}
	if count != 1 || !called {
		t.Fatalf("domain count %d tool %v", count, called)
	}
}

func TestTrailCatalogAndRunWindows(t *testing.T) {
	// R-LGMM-YPRO R-T6FP-IEX5 R-T7NL-W6NU R-T8VI-9YEJ R-TA3E-NQ58
	// R-TBBB-1HVX R-TDR3-T1DB R-TEZ0-6T40 R-TG6W-KKUP R-TIMP-C4C3
	// R-TJUL-PW2S R-STXI-QYJ3 R-SV5F-4Q9S R-TOQ7-8Z1K R-U253-GG77
	// R-TR60-0IIY R-LHUJ-CHID R-LJ2F-Q992 R-6JBK-EM35 R-XP0H-295S
	h := newHarness(t)
	h.repository("import time\ntime.sleep(3600)\n")
	h.p.Sink = &h.sink.capture
	h.start()
	c := &trailClient{h: h}
	sc, create := c.ok("create", map[string]any{"name": "secret-name", "repo": "rep_0102030405060708"})
	_, update := c.ok("update", map[string]any{"name": "secret-name", "ref": "other-ref"})
	_, same := c.ok("update", map[string]any{"name": "secret-name", "ref": "other-ref"})
	failed, failedID := c.ok("run", map[string]any{"name": "secret-name", "input": map[string]any{"private": "input"}})
	if failed["status"] != "failed" || failed["reason"] != "commit_missing" {
		t.Fatal(failed)
	}
	_, reset := c.ok("update", map[string]any{"name": "secret-name", "ref": "main"})
	shown, show := c.ok("show", map[string]any{"name": "secret-name"})
	if shown["id"] != sc["id"] {
		t.Fatal(shown)
	}
	first, firstID := c.ok("run", map[string]any{"name": "secret-name"})
	if first["status"] != "running" {
		t.Fatal(first)
	}
	_, cancel := c.ok("cancel", map[string]any{"run": first["id"]})
	second, secondID := c.ok("run", map[string]any{"name": "secret-name"})
	if second["status"] != "running" {
		t.Fatal(second)
	}
	_, list := c.ok("list", map[string]any{})
	_, history := c.ok("runs", map[string]any{"name": "secret-name"})
	_, result := c.ok("result", map[string]any{"run": first["id"]})
	_, refused, bad := c.call("run", map[string]any{"name": "missing"})
	if !bad {
		t.Fatal("missing script accepted")
	}
	refusals := []string{refused}
	for _, tc := range []struct {
		tool string
		args map[string]any
	}{{"create", map[string]any{"name": "secret-name", "repo": "rep_0102030405060708"}}, {"update", map[string]any{"name": "missing", "ref": "main"}}, {"delete", map[string]any{"name": "missing"}}, {"cancel", map[string]any{"run": first["id"]}}} {
		_, id, bad := c.call(tc.tool, tc.args)
		if !bad {
			t.Fatalf("unexpected success %s", tc.tool)
		}
		refusals = append(refusals, id)
	}
	deleted, del := c.ok("delete", map[string]any{"name": "secret-name"})
	h.stop()
	es := h.sink.capture.Events()
	trailScript(t, es, create, "script.created", sc["id"])
	trailScript(t, es, update, "script.updated", sc["id"])
	trailScript(t, es, del, "script.deleted", deleted["id"])
	for _, id := range append([]string{same, show, list, history, result}, refusals...) {
		for _, e := range trailWindow(t, es, id) {
			if e.RequestID == id && domain(e) {
				t.Fatalf("unexpected domain event %#v", e)
			}
		}
	}
	for _, r := range []struct {
		v  map[string]any
		id string
	}{{first, firstID}, {second, secondID}} {
		window := trailWindow(t, es, r.id)
		start := trailEvent(t, window, "run.started", r.v["id"])
		expectAttrs(t, start, telemetry.Attrs{"run": r.v["id"], "script": shown["id"], "sha": r.v["sha"], "trigger": "manual"})
		called := trailEvent(t, window, "tool.called", nil)
		if start.RequestID != called.RequestID {
			t.Fatal("start correlation")
		}
		seen := false
		for _, e := range window {
			if e.Name == "run.started" {
				seen = true
			}
			if e.Name == "tool.called" && !seen {
				t.Fatal("tool before run start")
			}
		}
	}
	fail := trailEvent(t, trailWindow(t, es, failedID), "run.finished", failed["id"])
	failSeen := false
	for _, e := range trailWindow(t, es, failedID) {
		if e.Name == "run.finished" {
			failSeen = true
		}
		if e.Name == "tool.called" && !failSeen {
			t.Fatal("failed event after tool.called")
		}
	}
	expectAttrs(t, fail, telemetry.Attrs{"run": failed["id"], "status": "failed", "reason": failed["reason"], "duration_us": int64(0), "truncated": false})
	for _, x := range []struct {
		id  string
		run any
	}{{cancel, first["id"]}, {del, second["id"]}} {
		end := trailEvent(t, trailWindow(t, es, x.id), "run.finished", x.run)
		expectAttrs(t, end, telemetry.Attrs{"run": x.run, "status": "killed", "duration_us": int64(0), "truncated": false})
		for _, e := range trailWindow(t, es, x.id) {
			if e.Name == "tool.called" {
				break
			}
			if e.Name == "run.finished" {
				goto ordered
			}
		}
		t.Fatal("end after tool.called")
	ordered:
	}
	names := map[string]bool{}
	for _, n := range []string{"service.started", "service.stopping", "request.started", "request.finished", "tool.called", "script.created", "script.updated", "script.deleted", "run.started", "run.finished"} {
		names[n] = true
	}
	origins := map[any]string{first["id"]: firstID, second["id"]: secondID, failed["id"]: failedID}
	for run := range origins {
		trailEvent(t, es, "run.finished", run)
		if run != failed["id"] {
			trailEvent(t, es, "run.started", run)
		}
	}
	scriptCalls := map[string]string{create: "script.created", update: "script.updated", reset: "script.updated", del: "script.deleted"}
	finished := map[any]bool{}
	for _, e := range es {
		if e.Name == "script.deleted" {
			for run := range origins {
				if !finished[run] {
					t.Fatal("deleted before finish")
				}
			}
		}
		if !names[e.Name] {
			t.Fatal(e.Name)
		}
		if domain(e) {
			if e.Service != "scripts" || !e.Time.Equal(h.now.UTC().Truncate(time.Microsecond)) {
				t.Fatal(e)
			}
			if strings.HasPrefix(e.Name, "script.") {
				if want, ok := scriptCalls[e.RequestID]; !ok || want != e.Name || e.User != "owner" {
					t.Fatalf("script event has no successful originating call: %#v", e)
				}
				expectAttrs(t, e, telemetry.Attrs{"script": sc["id"]})
			}
			if strings.HasPrefix(e.Name, "run.") {
				origin, answered := origins[e.Attrs["run"]]
				if !answered || origin != e.RequestID || e.User != "owner" {
					t.Fatal(e)
				}
				if e.Name == "run.finished" {
					if e.Attrs["duration_us"] != int64(0) {
						t.Fatal(e)
					}
					finished[e.Attrs["run"]] = true
				} else if finished[e.Attrs["run"]] {
					t.Fatal("event after finish")
				}
			}
		}
		for k, v := range e.Attrs {
			switch k {
			case "script":
				s, ok := v.(string)
				if !ok || !store.ValidScriptID(s) {
					t.Fatal(e)
				}
			case "run":
				s, ok := v.(string)
				if !ok || !store.ValidRunID(s) {
					t.Fatal(e)
				}
			case "sha":
				s, ok := v.(string)
				if !ok || len(s) != 40 || strings.Trim(s, "0123456789abcdef") != "" {
					t.Fatal(e)
				}
			}
		}
		if e.Name == "tool.called" {
			if len(e.Attrs) != 4 {
				t.Fatal(e)
			}
			for _, k := range []string{"tool", "kind", "outcome"} {
				if _, ok := e.Attrs[k].(string); !ok {
					t.Fatal(e)
				}
			}
			if d, ok := e.Attrs["duration_us"].(int64); !ok || d < 0 {
				t.Fatal(e)
			}
		}
	}
	for _, e := range es {
		if e.Name == "run.started" && e.Attrs["run"] == failed["id"] {
			t.Fatal("failed run started")
		}
	}
}

const gatePython = "import os\np=os.path.join(os.environ['IKIGENBA_RUN_DIR'],'release')\nwhile not os.path.exists(p):\n pass\n"

func releaseRun(t *testing.T, h *runHarness, sc, id any) {
	t.Helper()
	mustCLI(t, os.WriteFile(filepath.Join(h.p.Dir, "state", "runs", sc.(string), id.(string), "release"), nil, 0600))
}
func TestTrailEndingsAndClock(t *testing.T) {
	// R-11RA-8ZSQ R-ZC8Y-ZZD8 R-LMQ4-VKH5 R-LNY1-9C7U R-LP5X-N3YJ
	// R-LKAC-40ZR R-LLI8-HSQG R-ZH4K-J2C0 R-TZPA-OWPT R-3HRB-50VE
	for _, mode := range []string{"exit", "backwards", "timeout", "truncated", "race"} {
		t.Run(mode, func(t *testing.T) {
			h := newHarness(t)
			script := gatePython + "raise SystemExit(3)\n"
			if mode == "truncated" {
				script = gatePython + "import sys\nsys.stdout.write('s'*2000)\nsys.stderr.write('e'*2000)\n"
				h.set("OUTPUT_MAX_BYTES", "1024")
			}
			h.repository(script)
			var mu sync.Mutex
			now := h.now
			t0 := now
			h.p.Now = func() time.Time { mu.Lock(); defer mu.Unlock(); return now }
			h.p.Sink = &h.sink.capture
			h.start()
			c := &trailClient{h: h}
			sc, _ := c.ok("create", map[string]any{"name": "clock", "repo": "rep_0102030405060708"})
			r, origin := c.ok("run", map[string]any{"name": "clock"})
			if r["status"] != "running" {
				t.Fatal(r)
			}
			mu.Lock()
			if mode == "backwards" {
				now = t0.Add(-1500 * time.Microsecond)
			} else {
				now = t0.Add(1500 * time.Microsecond)
			}
			mu.Unlock()
			switch mode {
			case "timeout":
				select {
				case timer := <-h.timers:
					timer <- t0
				case <-time.After(10 * time.Second):
					t.Fatal("no script timer")
				}
			case "race":
				var wg sync.WaitGroup
				wg.Add(1)
				go func() { defer wg.Done(); _, _, _ = c.call("cancel", map[string]any{"run": r["id"]}) }()
				releaseRun(t, h, sc["id"], r["id"])
				wg.Wait()
			default:
				releaseRun(t, h, sc["id"], r["id"])
			}
			ended := c.ended(r["id"])
			_, _, bad := c.call("cancel", map[string]any{"run": r["id"]})
			if !bad {
				t.Fatal("ended cancel accepted")
			}
			h.stop()
			es := h.sink.capture.Events()
			trailEvent(t, es, "service.stopping", nil)
			start := trailEvent(t, es, "run.started", r["id"])
			end := trailEvent(t, es, "run.finished", r["id"])
			if end.RequestID != origin || end.User != "owner" || start.RequestID != origin {
				t.Fatal(end)
			}
			duration := int64(1500)
			if mode == "backwards" {
				duration = 0
			}
			if end.Attrs["duration_us"] != duration {
				t.Fatal(end)
			}
			if mode == "timeout" {
				if end.Attrs["status"] != "timed_out" {
					t.Fatal(end)
				}
			} else if mode != "race" {
				code := int64(3)
				if mode == "truncated" {
					code = 0
				}
				if end.Attrs["status"] != "exited" || end.Attrs["exit_code"] != code {
					t.Fatal(end)
				}
			}
			if end.Attrs["truncated"] != (mode == "truncated") {
				t.Fatal(end)
			}
			if ended["status"] != end.Attrs["status"] {
				t.Fatal(ended)
			}
			sDB, e := db.Open(context.Background(), db.Config{Path: filepath.Join(h.p.Dir, "state", "scripts.db"), Migrations: scripts.Migrations(), Now: h.p.Now})
			mustCLI(t, e)
			s := store.New(sDB, store.Config{Now: h.p.Now, Rand: &countingRandom{}})
			defer func() { mustCLI(t, sDB.Close()) }()
			record, e := s.RunByID(context.Background(), r["id"].(string))
			mustCLI(t, e)
			expectAttrs(t, end, runs.FinishedAttrs(record, time.Duration(duration)*time.Microsecond))
			state := 0
			called := false
			for _, e := range es {
				if e.RequestID == origin && e.Name == "tool.called" {
					called = true
				}
				if e.Attrs["run"] == r["id"] {
					switch e.Name {
					case "run.started":
						if state != 0 {
							t.Fatal("duplicate/late start")
						}
						state = 1
					case "run.finished":
						if state != 1 || !called {
							t.Fatal("finish order")
						}
						state = 2
					}
				}
				if e.Name == "service.stopping" && state != 2 {
					t.Fatal("stop before end")
				}
			}
			if state != 2 {
				t.Fatal("missing end")
			}
		})
	}
}

func TestTrailRecoveryAndReadRequests(t *testing.T) {
	// R-ZB12-M7MJ R-SWDB-II0H
	h := newHarness(t)
	h.p.Sink = &h.sink.capture
	sDB, e := db.Open(context.Background(), db.Config{Path: filepath.Join(h.p.Dir, "state", "scripts.db"), Migrations: scripts.Migrations(), Now: func() time.Time { return h.now }})
	mustCLI(t, e)
	s := store.New(sDB, store.Config{Now: func() time.Time { return h.now }, Rand: &countingRandom{}})
	sc, e := s.Create(context.Background(), store.Draft{Owner: "owner", Name: "recovered", Repo: "rep_0102030405060708", Ref: "main"})
	mustCLI(t, e)
	r, e := s.AddRun(context.Background(), store.Run{ID: "run_1122334455667788", Script: sc.ID, SHA: strings.Repeat("a", 40), Ref: "main", User: "past-owner", RequestID: "past-request", Trigger: "manual", Status: "running", Started: h.now.Add(-2 * time.Second)})
	mustCLI(t, e)
	future, e := s.AddRun(context.Background(), store.Run{ID: "run_8899aabbccddeeff", Script: sc.ID, SHA: strings.Repeat("b", 40), Ref: "main", User: "future-owner", RequestID: "future-request", Trigger: "manual", Status: "running", Started: h.now.Add(2 * time.Second)})
	mustCLI(t, e)
	mustCLI(t, sDB.Close())
	h.start()
	paths := []string{"/", "/recovered/", "/recovered/runs/" + r.ID + "/", "/recovered/runs/" + r.ID + "/stdout", "/about/", "/_appkit/theme.css"}
	for i, p := range paths {
		req, e := http.NewRequest(http.MethodGet, "http://"+h.listener.Addr().String()+p, nil)
		mustCLI(t, e)
		req.Header.Set("X-User-Id", "owner")
		req.Header.Set("X-Request-Id", fmt.Sprintf("page-%d", i))
		res, e := h.http.Do(req)
		mustCLI(t, e)
		_, e = io.Copy(io.Discard, res.Body)
		mustCLI(t, e)
		mustCLI(t, res.Body.Close())
	}
	h.stop()
	es := h.sink.capture.Events()
	end := trailEvent(t, es, "run.finished", r.ID)
	expectAttrs(t, end, telemetry.Attrs{"run": r.ID, "status": "killed", "duration_us": h.now.Sub(r.Started.UTC().Truncate(time.Second)).Microseconds(), "truncated": false})
	if end.RequestID != r.RequestID || end.User != r.User {
		t.Fatal(end)
	}
	futureEnd := trailEvent(t, es, "run.finished", future.ID)
	expectAttrs(t, futureEnd, telemetry.Attrs{"run": future.ID, "status": "killed", "duration_us": int64(0), "truncated": false})
	if futureEnd.RequestID != future.RequestID || futureEnd.User != future.User {
		t.Fatal(futureEnd)
	}
	seen := 0
	for _, e := range es {
		if e.Name == "run.finished" {
			seen++
		}
		if e.Name == "service.started" && seen != 2 {
			t.Fatal("recovery after started")
		}
	}
	for i := range paths {
		window := trailWindow(t, es, fmt.Sprintf("page-%d", i))
		count := 0
		for _, e := range window {
			if e.RequestID == fmt.Sprintf("page-%d", i) {
				count++
			}
		}
		if count != 2 {
			t.Fatal(window)
		}
	}
}

type trailBlockingSink struct {
	capture telemetry.Capture
	mu      sync.Mutex
	stopped bool
}

func (s *trailBlockingSink) Deliver(ctx context.Context, e telemetry.Event) error {
	s.mu.Lock()
	stopped := s.stopped
	s.mu.Unlock()
	if stopped {
		<-ctx.Done()
		return ctx.Err()
	}
	return s.capture.Deliver(ctx, e)
}
func TestTrailDrainDeadlineUndeliveredIdentity(t *testing.T) {
	// R-LRLQ-ENFX
	h := newHarness(t)
	h.repository("while True: pass\n")
	sink := &trailBlockingSink{}
	h.p.Sink = sink
	h.start()
	c := &trailClient{h: h}
	_, _ = c.ok("create", map[string]any{"name": "drain", "repo": "rep_0102030405060708"})
	r, origin := c.ok("run", map[string]any{"name": "drain"})
	sink.mu.Lock()
	sink.stopped = true
	h.cancel(context.Canceled)
	sink.mu.Unlock()
	if h.finish() != 0 {
		t.Fatal(h.stderr.String())
	}
	var events []telemetry.Event
	for _, line := range h.stderr.snapshot() {
		if strings.HasPrefix(string(line), "scripts: undelivered event: ") {
			var e struct {
				Name      string          `json:"event"`
				RequestID string          `json:"request_id"`
				User      string          `json:"user"`
				Attrs     telemetry.Attrs `json:"attrs"`
			}
			mustCLI(t, json.Unmarshal([]byte(strings.TrimPrefix(strings.TrimSpace(string(line)), "scripts: undelivered event: ")), &e))
			events = append(events, telemetry.Event{Name: e.Name, RequestID: e.RequestID, User: e.User, Attrs: e.Attrs})
		}
	}
	end := trailEvent(t, events, "run.finished", r["id"])
	if end.RequestID != origin || end.User != "owner" || end.Attrs["status"] != "killed" {
		t.Fatal(end)
	}
	if _, ok := end.Attrs["exit_code"]; ok {
		t.Fatal(end)
	}
	seen := false
	for _, e := range events {
		if e.Name == "run.finished" {
			seen = true
		}
		if e.Name == "service.stopping" && !seen {
			t.Fatal("stop diagnostic before ending")
		}
	}
	trailEvent(t, events, "service.stopping", nil)
}
func TestTrailClientCancellationCutsOffGit(t *testing.T) {
	// R-2I6Y-P4OL
	h := newHarness(t)
	h.repository("pass\n")
	h.p.Sink = &h.sink.capture
	h.start()
	c := &trailClient{h: h}
	_, _ = c.ok("create", map[string]any{"name": "cutoff", "repo": "rep_0102030405060708"})
	for len(h.gitTimers) > 0 {
		<-h.gitTimers
	}
	fifo := filepath.Join(h.root, "cancel-trace")
	mustCLI(t, syscall.Mkfifo(fifo, 0600))
	h.set("GIT_TRACE", fifo)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, e := h.client.CallTool(ctx, identity.Caller{UserID: "owner", RequestID: "client-cutoff"}, "run", json.RawMessage(`{"name":"cutoff"}`))
		done <- e
	}()
	select {
	case <-h.gitTimers:
	case <-time.After(10 * time.Second):
		t.Fatal("git did not start")
	}
	cancel()
	select {
	case e := <-done:
		if e == nil {
			t.Fatal("cancelled call answered")
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled call did not return")
	}
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	finished := false
	for !finished {
		for _, e := range h.sink.capture.Events() {
			if e.RequestID == "client-cutoff" && e.Name == "request.finished" {
				finished = true
			}
		}
		select {
		case <-deadline.C:
			t.Fatal("server did not finish cancelled request within one second")
		default:
		}
	}
	assertNoProcess(t, "GIT_TRACE="+fifo)
	// A completed independent request establishes that serving continues after cancellation.
	_, _ = c.ok("list", map[string]any{})
	h.cancel(context.Canceled)
	if h.finish() != 0 {
		t.Fatal(h.stderr.String())
	}
	assertNoProcess(t, "GIT_TRACE="+fifo)
	es := h.sink.capture.Events()
	trailEvent(t, es, "service.stopping", nil)
	for _, e := range es {
		if e.RequestID == "client-cutoff" && domain(e) {
			t.Fatal(e)
		}
	}
	if strings.Contains(h.stderr.String(), "stopped with") {
		t.Fatal(h.stderr.String())
	}
	for _, line := range h.stderr.snapshot() {
		if strings.Contains(string(line), `"request_id":"client-cutoff"`) && (strings.Contains(string(line), `"event":"run.`) || strings.Contains(string(line), `"event":"script.`)) {
			t.Fatal(string(line))
		}
	}
}
