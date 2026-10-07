package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/auth/internal/version"
)

type rejectingSink struct{}

func (rejectingSink) Deliver(context.Context, telemetry.Event) error {
	return fmt.Errorf("refused: %w", telemetry.ErrRejected)
}

type heldSink struct{}

func (heldSink) Deliver(ctx context.Context, _ telemetry.Event) error { <-ctx.Done(); return ctx.Err() }

type zeroRand struct{}

func (zeroRand) Read(b []byte) (int, error) { clear(b); return len(b), nil }

type observedSink struct {
	capture telemetry.Capture
	reject  bool
}

func (s *observedSink) Deliver(ctx context.Context, e telemetry.Event) error {
	_ = s.capture.Deliver(ctx, e)
	if s.reject {
		return rejectingSink{}.Deliver(ctx, e)
	}
	return nil
}

func startTrailRun(t *testing.T, sink telemetry.Sink, issuer string) (Process, context.CancelCauseFunc, <-chan int, string) {
	t.Helper()
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	dir, err := os.MkdirTemp("", "auth-trail-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	path := filepath.Join(dir, "ready.sock")
	ready, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: path, Net: "unixgram"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ready.Close() })
	env := goodEnv()
	env["NOTIFY_SOCKET"] = path
	env["DRAIN_SECONDS"] = "1"
	p := baseProcess(env, testSource(t), ln)
	p.Sink = sink
	if issuer != "" {
		p.OIDCIssuer = issuer
	}
	p.Rand = zeroRand{}
	p.Now = func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 123456789, time.FixedZone("offset", 3600)) }
	ctx, cancel := context.WithCancelCause(t.Context())
	t.Cleanup(func() { cancel(errors.New("cleanup")) })
	done := make(chan int, 1)
	go func() { done <- Run(ctx, p) }()
	if err := ready.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	message := make([]byte, 32)
	n, _, err := ready.ReadFromUnix(message)
	if err != nil || string(message[:n]) != "READY=1" {
		t.Fatalf("readiness=%q err=%v", message[:n], err)
	}
	return p, cancel, done, "http://" + ln.Addr().String()
}

func TestRunTrailDelivery(t *testing.T) {
	// R-BEG2-DPNX R-BGVV-595B R-BKJK-AKDE R-2MRR-GNCF R-B74O-337R R-2J42-BC4C
	for _, reject := range []bool{false, true} {
		t.Run(fmt.Sprint(reject), func(t *testing.T) {
			sink := &observedSink{reject: reject}
			var issuer *httptest.Server
			issuer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/.well-known/openid-configuration" {
					http.NotFound(w, r)
					return
				}
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{"issuer": issuer.URL, "authorization_endpoint": issuer.URL + "/authorize", "token_endpoint": issuer.URL + "/token", "jwks_uri": issuer.URL + "/jwks", "id_token_signing_alg_values_supported": []string{"RS256"}})
			}))
			defer issuer.Close()
			p, cancel, done, base := startTrailRun(t, sink, issuer.URL)
			client := &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
			for i, path := range []string{"/missing", "/login/google", "/login/google"} {
				req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, base+path, nil)
				if err != nil {
					t.Fatal(err)
				}
				resp, err := client.Do(req)
				if err != nil {
					t.Fatal(err)
				}
				_, _ = io.Copy(io.Discard, resp.Body)
				_ = resp.Body.Close()
				want := 404
				if path == "/login/google" {
					want = 302
					if i == 2 {
						want = 500
					}
				}
				if resp.StatusCode != want {
					t.Fatalf("%s status=%d", path, resp.StatusCode)
				}
			}
			cancel(errors.New("test stop"))
			if code := <-done; code != 0 {
				t.Fatalf("code=%d stderr=%s", code, p.Stderr)
			}
			events := sink.capture.Events()
			if len(events) < 6 {
				t.Fatalf("events=%+v", events)
			}
			first, last := events[0], events[len(events)-1]
			if first.Name != "service.started" || first.RequestID != "" || first.User != "" || len(first.Attrs) != 1 || first.Attrs["version"] != version.Version {
				t.Fatalf("start=%+v", first)
			}
			if last.Name != "service.stopping" || last.RequestID != "" || last.User != "" || len(last.Attrs) != 1 || last.Attrs["reason"] != "test stop" {
				t.Fatalf("stop=%+v", last)
			}
			statuses := map[int64]bool{}
			for _, e := range events {
				if e.Service != "auth" || !e.Time.Equal(p.Now().UTC().Truncate(time.Microsecond)) {
					t.Fatalf("envelope=%+v", e)
				}
				if e.Name != "service.started" && e.Name != "service.stopping" && e.RequestID != strings.Repeat("0", 32) {
					t.Fatalf("request ID=%q", e.RequestID)
				}
				if e.Name == "request.finished" {
					status, ok := e.Attrs["status"].(int64)
					if !ok {
						t.Fatalf("status=%T", e.Attrs["status"])
					}
					statuses[status] = true
				}
			}
			if !statuses[404] || !statuses[302] || !statuses[500] {
				t.Fatalf("statuses=%v", statuses)
			}
			output := p.Stderr.(*countWriter)
			if reject {
				var want strings.Builder
				for _, e := range events {
					data, err := e.MarshalJSON()
					if err != nil {
						t.Fatal(err)
					}
					want.WriteString("auth: undelivered event: ")
					want.Write(data)
					want.WriteByte('\n')
				}
				if output.String() != want.String() || output.calls != len(events) {
					t.Fatalf("writes=%d want=%d stderr=%q", output.calls, len(events), output.String())
				}
			} else if output.Len() != 0 {
				t.Fatalf("stderr=%q", output.String())
			}
			if p.Stdout.(*bytes.Buffer).Len() != 0 {
				t.Fatal("stdout written")
			}
		})
	}
}

func TestRunTrailBeforeServe(t *testing.T) {
	// R-8B6Y-ZBZR
	for _, kind := range []string{"command", "status", "usage", "config", "inherit", "database", "notify"} {
		t.Run(kind, func(t *testing.T) {
			env := goodEnv()
			p := baseProcess(env, testSource(t), nil)
			switch kind {
			case "command":
				p.Args = []string{"--version"}
			case "status":
				p.Args = []string{"db", "status"}
			case "usage":
				p.Args = []string{"bogus"}
			case "config":
				delete(env, "GOOGLE_CLIENT_ID")
			case "inherit":
				p.Inherit = func(uintptr) (net.Listener, error) { return nil, errors.New("bad descriptor") }
			case "database", "notify":
				ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = ln.Close() })
				p.Inherit = func(uintptr) (net.Listener, error) { return ln, nil }
				if kind == "database" {
					p.Dir = t.TempDir()
					if err := os.Mkdir(filepath.Join(p.Dir, "state"), 0700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(p.Dir, "state", "auth.db"), []byte("not sqlite"), 0600); err != nil {
						t.Fatal(err)
					}
				} else {
					env["NOTIFY_SOCKET"] = filepath.Join(t.TempDir(), "missing.sock")
				}
			}
			_ = Run(t.Context(), p)
			if len(p.Sink.(*telemetry.Capture).Events()) != 0 {
				t.Fatal("event before Serve")
			}
			if strings.Contains(p.Stderr.(*countWriter).String(), " event: ") {
				t.Fatal("trail output before Serve")
			}
		})
	}
}

func TestRunFailedServeTrail(t *testing.T) {
	// R-BO79-FVLH
	p := baseProcess(goodEnv(), testSource(t), &brokenListener{err: errors.New("broken accept")})
	sink := &observedSink{reject: true}
	p.Sink = sink
	if code := Run(t.Context(), p); code != 1 {
		t.Fatalf("code=%d", code)
	}
	events := sink.capture.Events()
	if len(events) != 2 || events[0].Name != "service.started" || events[1].Name != "service.stopping" || events[1].Attrs["reason"] != "failed" {
		t.Fatalf("events=%+v", events)
	}
	var want strings.Builder
	for _, e := range events {
		data, _ := e.MarshalJSON()
		want.WriteString("auth: undelivered event: ")
		want.Write(data)
		want.WriteByte('\n')
	}
	want.WriteString("auth: broken accept\n")
	if got := p.Stderr.(*countWriter).String(); got != want.String() {
		t.Fatalf("stderr=%q", got)
	}
}

func TestRunTrailDrainWindow(t *testing.T) {
	// R-2P7K-86TT
	p, cancel, done, _ := startTrailRun(t, heldSink{}, "")
	began := time.Now()
	cancel(errors.New("held stop"))
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("code=%d", code)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("trail exceeded drain window")
	}
	elapsed := time.Since(began)
	if elapsed < 900*time.Millisecond || elapsed > 2*time.Second {
		t.Fatalf("elapsed=%s", elapsed)
	}
	events := undeliveredEvents(t, p.Stderr.(*countWriter).String())
	if len(events) != 2 || events[0]["event"] != "service.started" || events[1]["event"] != "service.stopping" {
		t.Fatalf("events=%v", events)
	}
}

func undeliveredEvents(t *testing.T, out string) []map[string]any {
	t.Helper()
	var events []map[string]any
	for _, line := range strings.Split(strings.TrimSuffix(out, "\n"), "\n") {
		data, ok := strings.CutPrefix(line, "auth: undelivered event: ")
		if !ok {
			t.Fatalf("diagnostic=%q", line)
		}
		var e map[string]any
		if err := json.Unmarshal([]byte(data), &e); err != nil {
			t.Fatal(err)
		}
		events = append(events, e)
	}
	return events
}

func TestAppkitOverflowAndLateEvents(t *testing.T) {
	// R-W9YY-0F39 R-WDMN-5QBC: auth consumes these fallbacks through the published writer.
	entered := make(chan struct{})
	release := make(chan struct{})
	sink := &queueSink{entered: entered, release: release}
	var output bytes.Buffer
	w := telemetry.New(telemetry.Config{Service: "auth", Version: version.Version, Sink: sink, Stderr: &output, Now: func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }, Rand: zeroRand{}})
	w.Ready()
	<-entered
	for range telemetry.QueueCapacity {
		w.Emit(t.Context(), "test.queued", telemetry.Attrs{"value": "queued"})
	}
	w.Emit(t.Context(), "test.overflow", telemetry.Attrs{"value": "overflow"})
	overflow := undeliveredEvents(t, output.String())
	if len(overflow) != 1 || overflow[0]["event"] != "test.overflow" {
		t.Fatalf("overflow=%v", overflow)
	}
	close(release)
	w.Shutdown(t.Context(), "stop")
	w.Emit(t.Context(), "test.late", telemetry.Attrs{"value": "late"})
	events := undeliveredEvents(t, output.String())
	if len(events) != 2 || events[1]["event"] != "test.late" {
		t.Fatalf("fallbacks=%v", events)
	}
}

type queueSink struct {
	entered chan struct{}
	release chan struct{}
	started bool
}

func (s *queueSink) Deliver(_ context.Context, _ telemetry.Event) error {
	if !s.started {
		s.started = true
		close(s.entered)
		<-s.release
	}
	return nil
}

type doneAwareSink struct{ capture telemetry.Capture }

func (s *doneAwareSink) Deliver(ctx context.Context, e telemetry.Event) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.capture.Deliver(ctx, e)
}

type blockingRejectSink struct {
	entered, release chan struct{}
	started          bool
}

func (s *blockingRejectSink) Deliver(ctx context.Context, e telemetry.Event) error {
	if !s.started {
		s.started = true
		close(s.entered)
		select {
		case <-s.release:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return rejectingSink{}.Deliver(ctx, e)
}
