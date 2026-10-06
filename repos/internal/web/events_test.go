package web

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/mcp"
)

// R-D5XZ-HY5C R-DZ7K-OFY0
func TestBusRoutesBypassIdentityExactly(t *testing.T) {
	f := newWebFixture(t)
	h := Handler(f.cfg)
	before := f.gitCalls.Load()
	for _, path := range []string{"/events", "/declarations"} {
		baseline := events.DeliveryHandler(nil)
		if path == "/declarations" {
			baseline = events.DeclarationsHandler(f.cfg.Events, nil)
		}
		for _, method := range []string{"GET", "HEAD", "POST", "PUT", "DELETE"} {
			for _, user := range []string{"", "owner"} {
				r := httptest.NewRequest(method, path+"?ignored=yes", strings.NewReader(`{"unexpected":true}`))
				r.Header.Set("X-User-Id", user)
				r.Header.Set("Content-Type", "application/json")
				r.Header.Set("X-Event-Cause", "evt_0123456789abcdef")
				r.Header.Set("X-Event-Depth", "0")
				r.Header.Set("X-Request-Id", "bus-path")
				got, want := httptest.NewRecorder(), httptest.NewRecorder()
				count := len(f.events(t))
				h.ServeHTTP(got, r.Clone(r.Context()))
				baseline.ServeHTTP(want, r.Clone(r.Context()))
				if got.Code != want.Code || !reflect.DeepEqual(got.Header(), want.Header()) || got.Body.String() != want.Body.String() {
					t.Fatalf("%s %s: got %d %v %q want %d %v %q", method, path, got.Code, got.Header(), got.Body.String(), want.Code, want.Header(), want.Body.String())
				}
				trace := f.events(t)[count:]
				if len(trace) != 2 || trace[0].Name != "request.started" || trace[1].Name != "request.finished" {
					t.Fatalf("bus path trace: %+v", trace)
				}
			}
		}
	}
	if f.gitCalls.Load() != before || f.bannerCalls.Load() != 0 || len(f.busEvents(t)) != 0 {
		t.Fatal("bus routes reached domain operation")
	}
	for _, path := range []string{"/events/", "/declarations/"} {
		if got := webRequest(h, "GET", path, "owner", "suffix", nil); got.Code != 404 {
			t.Fatalf("%s: %d", path, got.Code)
		}
		if got := webRequest(h, "GET", path, "", "suffix", nil); got.Code != 500 {
			t.Fatalf("%s missing identity: %d", path, got.Code)
		}
	}
}

func webPushBody(t *testing.T, f *webFixture) ([]byte, string) {
	t.Helper()
	work := filepath.Join(f.dir, "event-work")
	f.git(t, f.dir, "init", "--initial-branch=main", work)
	f.git(t, work, "commit", "--allow-empty", "-m", "fixture")
	sha := strings.TrimSpace(string(f.git(t, work, "rev-parse", "HEAD")))
	pack := f.git(t, work, "pack-objects", "--all", "--stdout")
	command := strings.Repeat("0", 40) + " " + sha + " refs/heads/main\x00report-status\n"
	return append([]byte(fmt.Sprintf("%04x%s0000", len(command)+4, command)), pack...), sha
}

// R-E5B2-LANH R-E7QV-CU4V R-E8YR-QLVK R-3JWT-WM7P
func TestPushCarriesRequestAndValidCause(t *testing.T) {
	f := newWebFixture(t)
	body, _ := webPushBody(t, f)
	h := Handler(f.cfg)
	cases := []struct {
		cause, depth []string
		wantCause    string
		wantDepth    int
	}{
		{nil, nil, "", 0},
		{[]string{"evt_0123456789abcdef"}, []string{"0"}, "evt_0123456789abcdef", 1},
		{[]string{"evt_0123456789abcdef"}, []string{"00042"}, "evt_0123456789abcdef", 43},
		{[]string{"evt_0123456789abcdef"}, []string{"1000000"}, "evt_0123456789abcdef", 1000001},
		{[]string{"evt_0123456789abcdef"}, nil, "", 0},
		{nil, []string{"0"}, "", 0},
		{[]string{"evt_0123456789abcdef"}, []string{"-1"}, "", 0},
		{[]string{"evt_0123456789abcdef"}, []string{"+1"}, "", 0},
		{[]string{"evt_0123456789abcdef"}, []string{"one"}, "", 0},
		{[]string{"evt_0123456789abcdef"}, []string{" 1"}, "", 0},
		{[]string{"evt_0123456789abcdef"}, []string{"1.0"}, "", 0},
		{[]string{"evt_0123456789abcdef"}, []string{"１"}, "", 0},
		{[]string{"evt_0123456789ABCDEF"}, []string{"0"}, "", 0},
		{[]string{"evt_0123456789abcde"}, []string{"0"}, "", 0},
		{[]string{"evt_0123456789abcdef0"}, []string{"0"}, "", 0},
		{[]string{"bad", "evt_0123456789abcdef"}, []string{"0"}, "", 0},
		{[]string{"evt_0123456789abcdef"}, []string{"", "0"}, "", 0},
		{[]string{"evt_0123456789abcdef", "bad"}, []string{"0", "bad"}, "evt_0123456789abcdef", 1},
	}
	for i, tc := range cases {
		name := fmt.Sprintf("push%d", i)
		repo := f.repo(t, "owner", name)
		r := httptest.NewRequest("POST", "/"+name+".git/git-receive-pack", bytes.NewReader(body))
		r.Header.Set("Content-Type", "application/x-git-receive-pack-request")
		r.Header["X-User-Id"] = []string{"owner", "ignored"}
		r.Header["X-Event-Cause"], r.Header["X-Event-Depth"] = tc.cause, tc.depth
		if i%2 == 0 {
			r.Header["X-Request-Id"] = []string{"explicit-id", "ignored"}
		}
		before := len(f.busEvents(t))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 200 || !strings.Contains(w.Body.String(), "ok refs/heads/main") {
			t.Fatalf("push %d: %d %q", i, w.Code, w.Body.String())
		}
		observed := f.busEvents(t)[before:]
		if len(observed) != 1 {
			t.Fatalf("push %d bus events: %+v", i, observed)
		}
		e := observed[0]
		if e.Name != "repo.pushed" || e.User != "owner" || e.Cause != tc.wantCause || e.Depth != tc.wantDepth {
			t.Fatalf("push %d envelope: %+v", i, e)
		}
		found := false
		for _, te := range f.events(t) {
			if te.Name == "repo.pushed" && te.Attrs["repo"] == repo.ID {
				found = true
				if te.RequestID != e.RequestID || te.User != e.User || e.RequestID == "" {
					t.Fatalf("push correlation: %+v %+v", te, e)
				}
				if i%2 == 0 && e.RequestID != "explicit-id" {
					t.Fatalf("first request id lost: %+v", e)
				}
			}
		}
		if !found {
			t.Fatalf("push %d telemetry absent", i)
		}
	}
	if got := f.busStderr.String(); got != "" {
		t.Fatalf("successful bus delivery wrote stderr: %s", got)
	}
}

type webEventSinkFunc func(context.Context, events.Event) error

func (s webEventSinkFunc) Deliver(ctx context.Context, e events.Event) error { return s(ctx, e) }

// R-EA6O-4DM9
func TestPushDoesNotWaitForBus(t *testing.T) {
	f := newWebFixture(t)
	repo := f.repo(t, "owner", "notes")
	body, sha := webPushBody(t, f)
	var baseline *httptest.ResponseRecorder
	paths := []string{"/", "/about", "/nope", "/_appkit/theme.css", "/mcp", "/events", "/declarations", "/notes.git/info/refs?service=git-upload-pack"}
	var baselineRoutes []*httptest.ResponseRecorder
	for _, mode := range []string{"success", "error", "blocked"} {
		release, started := make(chan struct{}), make(chan struct{})
		var once sync.Once
		sink := webEventSinkFunc(func(ctx context.Context, _ events.Event) error {
			once.Do(func() { close(started) })
			if mode == "error" {
				return events.ErrRejected
			}
			if mode == "blocked" {
				select {
				case <-release:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			return nil
		})
		f.setEmitter(t, sink)
		cfg := f.cfg
		cfg.MCP = mcp.NewServer(mcp.ServerConfig{Name: ServiceName, Version: "fixture-version", Telemetry: cfg.Telemetry})
		h := Handler(cfg)
		for i, path := range paths {
			got := webRequest(h, "GET", path, "owner", "route-id", nil)
			if mode == "success" {
				baselineRoutes = append(baselineRoutes, got)
			} else {
				want := baselineRoutes[i]
				if got.Code != want.Code || !reflect.DeepEqual(got.Header(), want.Header()) || got.Body.String() != want.Body.String() {
					t.Fatalf("bus %s changed route %s", mode, path)
				}
			}
		}
		answered := make(chan *httptest.ResponseRecorder, 1)
		go func() {
			r := httptest.NewRequest("POST", "/notes.git/git-receive-pack", bytes.NewReader(body))
			r.Header.Set("X-User-Id", "owner")
			r.Header.Set("X-Request-Id", "same-id")
			r.Header.Set("Content-Type", "application/x-git-receive-pack-request")
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			answered <- w
		}()
		ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
		var got *httptest.ResponseRecorder
		select {
		case got = <-answered:
		case <-ctx.Done():
			close(release)
			cancel()
			t.Fatal("push waited for bus")
		}
		if got.Code != http.StatusOK || !strings.Contains(got.Body.String(), "ok refs/heads/main") {
			close(release)
			cancel()
			t.Fatalf("push %s: %d %q", mode, got.Code, got.Body.String())
		}
		if mode == "blocked" {
			select {
			case <-started:
			case <-ctx.Done():
				close(release)
				cancel()
				t.Fatal("sink did not enter")
			}
		}
		if baseline == nil {
			baseline = got
		} else if got.Code != baseline.Code || !reflect.DeepEqual(got.Header(), baseline.Header()) || got.Body.String() != baseline.Body.String() {
			close(release)
			cancel()
			t.Fatalf("bus %s altered push response", mode)
		}
		gotSHA := strings.TrimSpace(string(f.git(t, f.dir, "--git-dir="+f.cfg.Store.Dir(repo.ID), "rev-parse", "refs/heads/main")))
		if gotSHA != sha {
			close(release)
			cancel()
			t.Fatalf("push %s ref did not move: %s", mode, gotSHA)
		}
		close(release)
		cancel()
		f.git(t, f.dir, "--git-dir="+f.cfg.Store.Dir(repo.ID), "update-ref", "-d", "refs/heads/main")
	}
}
