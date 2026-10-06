package cli_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	root "github.com/ikigenba/ikigenba/telemetry"
	"github.com/ikigenba/ikigenba/telemetry/internal/cli"
	"github.com/ikigenba/ikigenba/telemetry/internal/store"
	"github.com/ikigenba/ikigenba/telemetry/internal/web"
)

type checkedOutput struct {
	mu          sync.Mutex
	calls       [][]byte
	active      atomic.Bool
	overlapping atomic.Bool
	ended       bool
	late        bool
}

func (w *checkedOutput) Write(b []byte) (int, error) {
	if w.active.Swap(true) {
		w.overlapping.Store(true)
	}
	defer w.active.Store(false)
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.ended {
		w.late = true
	}
	w.calls = append(w.calls, bytes.Clone(b))
	return len(b), nil
}
func (w *checkedOutput) lines() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	s := make([]string, len(w.calls))
	for i, b := range w.calls {
		s[i] = string(b)
	}
	return s
}
func (w *checkedOutput) end() { w.mu.Lock(); defer w.mu.Unlock(); w.ended = true }
func (w *checkedOutput) assertQuiet(t *testing.T) {
	t.Helper()
	if lines := w.lines(); len(lines) != 0 {
		t.Fatalf("unexpected output %q", lines)
	}
}

type runtimeTest struct {
	p      cli.Process
	ctx    context.Context
	cancel context.CancelCauseFunc
	done   chan int
	ln     net.Listener
	notify *net.UnixConn
	client *http.Client
	output *checkedOutput
	writer chan *telemetry.Writer
}

func runtimeFor(t *testing.T, env map[string]string) *runtimeTest {
	t.Helper()
	t.Setenv("IKIGENBA_SERVICES", "")
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	short, err := os.MkdirTemp("", "notify-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(short); err != nil {
			t.Error(err)
		}
	})
	addr := filepath.Join(short, "ready")
	notification, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: addr, Net: "unixgram"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = notification.Close() })
	envCopy := map[string]string{"LISTEN_PID": "42", "LISTEN_FDS": "1", "NOTIFY_SOCKET": addr}
	for k, v := range env {
		envCopy[k] = v
	}
	ctx, cancel := context.WithCancelCause(context.Background())
	t.Cleanup(func() { cancel(errors.New("test cleanup")) })
	output := new(checkedOutput)
	writers := make(chan *telemetry.Writer, 1)
	fixed := time.Date(2025, 6, 20, 12, 30, 1, 123456789, time.FixedZone("test", 7200))
	r := &runtimeTest{ctx: ctx, cancel: cancel, done: make(chan int, 1), ln: ln, notify: notification, output: output, writer: writers, client: &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: 3 * time.Second}}
	t.Cleanup(func() { r.client.CloseIdleConnections() })
	r.p = cli.Process{Pid: 42, LookupEnv: func(k string) (string, bool) { v, ok := envCopy[k]; return v, ok }, Inherit: func(fd uintptr) (net.Listener, error) {
		if fd != 3 {
			t.Errorf("descriptor %d", fd)
		}
		return ln, nil
	}, Stdout: new(bytes.Buffer), Stderr: output, Now: func() time.Time { return fixed }, Sleep: func(c context.Context, d time.Duration) {
		if d == time.Hour {
			<-c.Done()
		}
	}, Rand: bytes.NewReader(bytes.Repeat([]byte{0x3b}, 4096)), Dir: t.TempDir(), Banner: func(page.User) page.Banner { return page.Banner{} }, MCP: func(w *telemetry.Writer) *mcp.Server {
		writers <- w
		return mcp.NewServer(mcp.ServerConfig{Name: web.ServiceName, Version: cli.Version, Telemetry: w})
	}}
	return r
}
func (r *runtimeTest) start() { go func() { r.done <- cli.Run(r.ctx, r.p) }() }
func (r *runtimeTest) ready(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	stop := context.AfterFunc(ctx, func() { _ = r.notify.Close() })
	defer stop()
	b := make([]byte, 100)
	n, _, err := r.notify.ReadFromUnix(b)
	if err != nil || string(b[:n]) != "READY=1" {
		t.Fatalf("readiness %q %v", b[:n], err)
	}
}
func (r *runtimeTest) finish(t *testing.T, want int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	select {
	case code := <-r.done:
		if code != want {
			t.Fatalf("exit %d output %q", code, r.output.lines())
		}
		r.output.end()
		if r.p.Stdout.(*bytes.Buffer).Len() != 0 {
			t.Fatal("serve wrote to stdout")
		}
	case <-ctx.Done():
		t.Fatal("Run did not return")
	}
}
func (r *runtimeTest) request(t *testing.T, path, id string) (int, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://"+r.ln.Addr().String()+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-User-Id", "test-user")
	if id != "" {
		req.Header.Set("X-Request-Id", id)
	}
	resp, err := r.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, string(body)
}
func records(t *testing.T, source string) []store.Record {
	t.Helper()
	handle := openDatabase(t, source)
	s := store.New(handle)
	defer func() { _ = handle.Close() }()
	result, err := s.Search(context.Background(), store.Filter{}, 500, "")
	if err != nil {
		t.Fatal(err)
	}
	return result.Records
}
func flush(t *testing.T, w *telemetry.Writer) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := w.Flush(ctx); err != nil {
		t.Fatal(err)
	}
}

// R-QQ1K-M93C R-RARV-4CP5 R-RQMK-3DC6 R-Q68U-O4E4 R-RRUG-H52V R-RO6R-BTUS R-S0DR-5J9Q R-RT2C-UWTK R-RVI5-MGAY R-RCXC-23CF
func TestHealthyRun(t *testing.T) {
	for _, services := range []string{"", "missing", "malformed"} {
		t.Run(services, func(t *testing.T) {
			env := map[string]string{}
			if services != "" {
				env["IKIGENBA_SERVICES"] = filepath.Join(t.TempDir(), services)
				if services == "malformed" {
					if err := os.WriteFile(env["IKIGENBA_SERVICES"], []byte("invalid services"), 0600); err != nil {
						t.Fatal(err)
					}
				}
			}
			r := runtimeFor(t, env)
			r.start()
			r.ready(t)
			w := <-r.writer
			for _, path := range []string{"/", "/about", "/nope"} {
				status, body := r.request(t, path, "")
				if body == "" {
					t.Fatal("empty response body")
				}
				want := http.StatusOK
				if path == "/nope" {
					want = http.StatusNotFound
				}
				if status != want {
					t.Fatalf("%s status %d", path, status)
				}
			}
			flush(t, w)
			select {
			case code := <-r.done:
				t.Fatalf("returned before cancellation %d", code)
			default:
			}
			r.cancel(errors.New("stop-cause"))
			r.finish(t, cli.ExitSuccess)
			r.output.assertQuiet(t)
			trail := records(t, databasePath(r.p.Dir))
			if len(trail) != 8 {
				t.Fatalf("records %d", len(trail))
			}
			oldest := trail[len(trail)-1]
			latest := trail[0]
			if oldest.Event != "service.started" || string(oldest.Attrs) != "{\"version\":\""+cli.Version+"\"}" || oldest.RequestID != "" || oldest.User != "" {
				t.Fatalf("started %+v", oldest)
			}
			if latest.Event != "service.stopping" || string(latest.Attrs) != "{\"reason\":\"stop-cause\"}" || latest.RequestID != "" || latest.User != "" {
				t.Fatalf("stopping %+v", latest)
			}
			for _, rec := range trail {
				if rec.Service != web.ServiceName || !rec.Time.Equal(r.p.Now().UTC().Truncate(time.Microsecond)) {
					t.Fatalf("event source %+v", rec)
				}
				switch rec.Event {
				case "service.started", "service.stopping", "request.started", "request.finished", "tool.called":
				default:
					t.Fatalf("nonframework event %+v", rec)
				}
				if strings.HasPrefix(rec.Event, "request.") && (rec.RequestID != hex.EncodeToString(bytes.Repeat([]byte{0x3b}, 16)) || rec.User != "test-user") {
					t.Fatalf("request identity %+v", rec)
				}
			}
		})
	}
}

// R-RI39-EZ5B R-RPEN-PLLH R-R9JY-QKYG
func TestStartupRuntimeFailures(t *testing.T) {
	t.Run("database", func(t *testing.T) {
		r := runtimeFor(t, nil)
		if err := os.WriteFile(filepath.Join(r.p.Dir, "state"), []byte("file"), 0600); err != nil {
			t.Fatal(err)
		}
		_, openErr := db.Open(context.Background(), db.Config{Path: databasePath(r.p.Dir), Migrations: root.Migrations(), Now: r.p.Now})
		if openErr == nil {
			t.Fatal("fixture opens")
		}
		r.p.MCP = nil
		r.p.Banner = nil
		r.start()
		r.finish(t, cli.ExitServerFailed)
		want := "telemetry: cannot open database state/telemetry.db: " + strings.ReplaceAll(openErr.Error(), "\n", " ") + "\n"
		if got := r.output.lines(); len(got) != 1 || got[0] != want {
			t.Fatalf("database diagnostic %q", got)
		}
	})
	t.Run("notification", func(t *testing.T) {
		r := runtimeFor(t, map[string]string{"NOTIFY_SOCKET": filepath.Join(t.TempDir(), "absent")})
		address, _ := r.p.LookupEnv("NOTIFY_SOCKET")
		conn, notifyErr := net.DialUnix("unixgram", nil, &net.UnixAddr{Name: address, Net: "unixgram"})
		if notifyErr == nil {
			_ = conn.Close()
			t.Fatal("notification fixture accepted")
		}
		r.p.MCP = nil
		r.p.Banner = nil
		r.start()
		r.finish(t, cli.ExitServerFailed)
		if got := r.output.lines(); len(got) != 1 || got[0] != "telemetry: "+notifyErr.Error()+"\n" {
			t.Fatalf("notify diagnostic %q", got)
		}
		if got := records(t, databasePath(r.p.Dir)); len(got) != 0 {
			t.Fatalf("refused start records %+v", got)
		}
	})
}

// R-PHUV-0PK8 R-RKJ2-6IMP R-RLQY-KADE R-RKJ2-6IMP
func TestRetentionSweeps(t *testing.T) {
	for _, days := range []string{"", "1", "999999999999999999999999999999999999999999999999999999999999999999999999999"} {
		t.Run(days, func(t *testing.T) {
			r := runtimeFor(t, map[string]string{"RETENTION_DAYS": days})
			now := r.p.Now().Truncate(time.Microsecond)
			var clockMu sync.Mutex
			r.p.Now = func() time.Time { clockMu.Lock(); defer clockMu.Unlock(); return now }
			entered := make(chan struct{})
			advance := make(chan struct{})
			r.p.Sleep = func(ctx context.Context, d time.Duration) {
				if d != time.Hour {
					return
				}
				select {
				case entered <- struct{}{}:
				case <-ctx.Done():
					return
				}
				select {
				case <-advance:
				case <-ctx.Done():
				}
			}
			window := 15 * 24 * time.Hour
			if days == "1" {
				window = 24 * time.Hour
			}
			if len(days) > 20 {
				window = time.Duration(1<<63 - 1)
				now = now.Add(time.Duration(int64(window) % 1000))
			}
			seedHandle := openDatabase(t, databasePath(r.p.Dir))
			seed := store.New(seedHandle)
			for i, when := range []time.Time{now.Add(-window).Add(-time.Microsecond), now.Add(-window), now.Add(-window + time.Hour)} {
				event := telemetry.Event{Time: when, Service: "sibling", Name: "fixture.event", RequestID: []string{"old", "edge", "young"}[i], Attrs: telemetry.Attrs{}}
				if err := seed.Deliver(context.Background(), event); err != nil {
					t.Fatal(err)
				}
			}
			if err := seedHandle.Close(); err != nil {
				t.Fatal(err)
			}
			r.start()
			r.ready(t)
			<-entered
			w := <-r.writer
			flush(t, w)
			read := func() []store.Record {
				h := openDatabase(t, databasePath(r.p.Dir))
				defer func() { _ = h.Close() }()
				s := store.New(h)
				p, e := s.Search(context.Background(), store.Filter{Services: []string{"sibling"}}, 50, "")
				if e != nil {
					t.Fatal(e)
				}
				return p.Records
			}
			got := read()
			if len(got) != 2 || got[0].RequestID != "young" || got[1].RequestID != "edge" {
				t.Fatalf("initial sweep %+v", got)
			}
			clockMu.Lock()
			now = now.Add(time.Hour)
			clockMu.Unlock()
			advance <- struct{}{}
			<-entered
			got = read()
			if len(got) != 1 || got[0].RequestID != "young" {
				t.Fatalf("hourly sweep %+v", got)
			}
			r.cancel(errors.New("stop"))
			r.finish(t, cli.ExitSuccess)
			r.output.assertQuiet(t)
		})
	}
}

// R-S2TJ-X2R4 R-Q7GR-1W4T R-Q1D9-51FC R-RUA9-8OK9 R-RZ5U-RRJ1 R-S1LN-JB0F R-RVI5-MGAY
func TestDrain(t *testing.T) {
	for _, cutoff := range []bool{false, true} {
		t.Run(map[bool]string{false: "completed", true: "cutoff"}[cutoff], func(t *testing.T) {
			r := runtimeFor(t, map[string]string{"DRAIN_SECONDS": "1"})
			entered := make(chan struct{})
			release := make(chan struct{})
			completed := make(chan struct{})
			r.p.Banner = func(page.User) page.Banner { close(entered); <-release; defer close(completed); return page.Banner{} }
			r.start()
			r.ready(t)
			w := <-r.writer
			response := make(chan error, 1)
			go func() {
				req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://"+r.ln.Addr().String()+"/", nil)
				if err != nil {
					response <- err
					return
				}
				req.Header.Set("X-User-Id", "drain-user")
				req.Header.Set("X-Request-Id", "drain-request")
				resp, err := r.client.Do(req)
				if err == nil {
					_, err = io.ReadAll(resp.Body)
					_ = resp.Body.Close()
				}
				response <- err
			}()
			<-entered
			flush(t, w)
			start := time.Now()
			r.cancel(errors.New("drain-cause"))
			if !cutoff {
				select {
				case <-r.done:
					t.Fatal("returned while request blocked")
				default:
				}
				close(release)
				if err := <-response; err != nil {
					t.Fatal(err)
				}
				r.finish(t, cli.ExitSuccess)
				if time.Since(start) >= time.Second {
					t.Fatal("clean drain waited past deadline")
				}
				r.output.assertQuiet(t)
				trail := records(t, databasePath(r.p.Dir))
				if len(trail) != 4 || trail[0].Event != "service.stopping" || trail[1].Event != "request.finished" {
					t.Fatalf("drain order %+v", trail)
				}
			} else {
				r.finish(t, cli.ExitServerFailed)
				elapsed := time.Since(start)
				if elapsed < time.Second || elapsed >= 2*time.Second {
					t.Fatalf("drain elapsed %v", elapsed)
				}
				if err := <-response; err == nil {
					t.Fatal("cut off request delivered complete response")
				}
				lines := r.output.lines()
				if len(lines) != 2 || !strings.HasPrefix(lines[0], "telemetry: undelivered event: ") || lines[1] != "telemetry: stopped with 1 request unfinished\n" {
					t.Fatalf("drain lines %q", lines)
				}
				event := decodeEvent(t, lines[0])
				if event.Name != "service.stopping" || event.Attrs["reason"] != "drain-cause" {
					t.Fatalf("undelivered stopping %+v", event)
				}
				trail := records(t, databasePath(r.p.Dir))
				if len(trail) != 2 || trail[0].Event != "request.started" || trail[1].Event != "service.started" {
					t.Fatalf("cutoff trail %+v", trail)
				}
				close(release)
				<-completed
				flush(t, w)
				if r.output.overlapping.Load() {
					t.Fatal("concurrent stream writes")
				}
				r.output.mu.Lock()
				late := r.output.late
				r.output.mu.Unlock()
				if late {
					t.Fatal("output after Run returned")
				}
			}
		})
	}
}

type temporaryError struct{}

func (temporaryError) Error() string   { return "retry accept" }
func (temporaryError) Timeout() bool   { return false }
func (temporaryError) Temporary() bool { return true }

type firstErrorListener struct {
	net.Listener
	first bool
	err   error
}

func (l *firstErrorListener) Accept() (net.Conn, error) {
	if !l.first {
		l.first = true
		return nil, l.err
	}
	return l.Listener.Accept()
}

// R-Q05C-R9ON R-QG01-QABO
func TestAcceptFailuresAndDiscardedLogger(t *testing.T) {
	t.Run("failure", func(t *testing.T) {
		r := runtimeFor(t, nil)
		r.p.Inherit = func(uintptr) (net.Listener, error) {
			return &firstErrorListener{Listener: r.ln, err: errors.New("accept failed")}, nil
		}
		r.start()
		r.ready(t)
		r.finish(t, cli.ExitServerFailed)
		lines := r.output.lines()
		if len(lines) == 0 || lines[len(lines)-1] != "telemetry: accept failed\n" {
			t.Fatalf("accept diagnostic %q", lines)
		}
	})
	t.Run("retry-and-panic", func(t *testing.T) {
		r := runtimeFor(t, nil)
		var logged bytes.Buffer
		original := log.Writer()
		log.SetOutput(&logged)
		defer log.SetOutput(original)
		r.p.Inherit = func(uintptr) (net.Listener, error) {
			return &firstErrorListener{Listener: r.ln, err: temporaryError{}}, nil
		}
		r.p.Banner = func(page.User) page.Banner { panic("test banner panic") }
		r.start()
		r.ready(t)
		w := <-r.writer
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://"+r.ln.Addr().String()+"/", nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("X-User-Id", "panic-user")
		if resp, err := r.client.Do(req); err == nil {
			_ = resp.Body.Close()
			t.Fatal("panicking handler returned response")
		}
		r.cancel(errors.New("stop"))
		r.finish(t, cli.ExitSuccess)
		flush(t, w)
		if logged.Len() != 0 {
			t.Fatalf("default log %q", logged.String())
		}
		r.output.assertQuiet(t)
	})
}

// R-6ZGC-JRPJ R-RWQ2-081N R-S1LN-JB0F R-RZ5U-RRJ1 R-RXXY-DZSC
func TestUndeliveredRequestEvents(t *testing.T) {
	r := runtimeFor(t, nil)
	r.start()
	r.ready(t)
	w := <-r.writer
	flush(t, w)
	h := openDatabase(t, databasePath(r.p.Dir))
	defer func() { _ = h.Close() }()
	if err := h.Write(context.Background(), func(tx *sql.Tx) error {
		_, err := tx.Exec("CREATE TRIGGER refuse_records BEFORE INSERT ON records BEGIN SELECT RAISE(ABORT, 'refused'); END")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	var requests sync.WaitGroup
	for _, id := range []string{"undelivered-a", "undelivered-b", "undelivered-c"} {
		requests.Add(1)
		go func(id string) {
			defer requests.Done()
			status, _ := r.request(t, "/", id)
			if status != 200 {
				t.Errorf("status %d", status)
			}
		}(id)
	}
	requests.Wait()
	flush(t, w)
	lines := r.output.lines()
	if len(lines) != 6 {
		t.Fatalf("undelivered lines %q", lines)
	}
	counts := map[string][]string{}
	for _, line := range lines {
		if !strings.HasPrefix(line, "telemetry: undelivered event: ") {
			t.Fatalf("line %q", line)
		}
		event := decodeEvent(t, line)
		if event.User != "test-user" {
			t.Fatalf("event user %+v", event)
		}
		canonical, err := event.MarshalJSON()
		if err != nil {
			t.Fatal(err)
		}
		if line != "telemetry: undelivered event: "+string(canonical)+"\n" {
			t.Fatalf("noncanonical output %q", line)
		}
		counts[event.RequestID] = append(counts[event.RequestID], event.Name)
	}
	if len(counts) != 3 {
		t.Fatalf("request ids %v", counts)
	}
	for _, id := range []string{"undelivered-a", "undelivered-b", "undelivered-c"} {
		names := counts[id]
		if len(names) != 2 || names[0] != "request.started" || names[1] != "request.finished" {
			t.Fatalf("%s events %v", id, names)
		}
	}
	if r.output.overlapping.Load() {
		t.Fatal("concurrent stderr calls")
	}
	r.cancel(errors.New("stop"))
	r.finish(t, cli.ExitSuccess)
}

func decodeEvent(t *testing.T, line string) telemetry.Event {
	t.Helper()
	var wire struct {
		Time      time.Time       `json:"time"`
		Service   string          `json:"service"`
		Name      string          `json:"event"`
		RequestID string          `json:"request_id"`
		User      string          `json:"user"`
		Attrs     telemetry.Attrs `json:"attrs"`
	}
	if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "telemetry: undelivered event: ")), &wire); err != nil {
		t.Fatal(err)
	}
	return telemetry.Event{Time: wire.Time, Service: wire.Service, Name: wire.Name, RequestID: wire.RequestID, User: wire.User, Attrs: wire.Attrs}
}

// R-RARV-4CP5 R-QQ1K-M93C R-PAJG-Q342
func TestRunConstructorInputsAndFrozenServicesPath(t *testing.T) {
	r := runtimeFor(t, map[string]string{"DRAIN_SECONDS": "99999999999999999999999999999999999999999999999999999999999999999999999"})
	servicesPath := filepath.Join(t.TempDir(), "services.json")
	if err := os.WriteFile(servicesPath, []byte(`{"services":[{"name":"auth","url":"https://auth.custom","description":"auth","socket":"/unused","enabled":true,"mcp":false}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	originalLookup := r.p.LookupEnv
	var lookups atomic.Int32
	r.p.LookupEnv = func(key string) (string, bool) {
		if key == "IKIGENBA_SERVICES" {
			if lookups.Add(1) == 1 {
				return servicesPath, true
			}
			return "changed-path", true
		}
		return originalLookup(key)
	}
	var calls atomic.Int32
	originalMCP := r.p.MCP
	r.p.MCP = func(w *telemetry.Writer) *mcp.Server {
		calls.Add(1)
		if w == nil {
			t.Error("nil writer")
		}
		return originalMCP(w)
	}
	var usersMu sync.Mutex
	var users []page.User
	r.p.Banner = func(u page.User) page.Banner {
		usersMu.Lock()
		users = append(users, u)
		usersMu.Unlock()
		return page.Banner{Service: "injected-banner", Version: cli.Version, Email: u.Email, ProfileURL: u.ProfileURL, LogoutURL: u.LogoutURL}
	}
	r.start()
	r.ready(t)
	for range 2 {
		status, body := r.request(t, "/", "known-request")
		if status != 200 || !strings.Contains(body, "injected-banner") {
			t.Fatalf("banner %d %q", status, body)
		}
	}
	r.cancel(errors.New("stop"))
	r.finish(t, cli.ExitSuccess)
	r.output.assertQuiet(t)
	if calls.Load() != 1 || lookups.Load() != 1 {
		t.Fatalf("constructors %d services reads %d", calls.Load(), lookups.Load())
	}
	usersMu.Lock()
	defer usersMu.Unlock()
	if len(users) != 2 {
		t.Fatalf("banner calls %d", len(users))
	}
	for _, u := range users {
		if u.ProfileURL != "https://auth.custom/" || u.LogoutURL != "https://auth.custom/logout" {
			t.Fatalf("services input %+v", u)
		}
	}
}

// R-S2TJ-X2R4 R-Q7GR-1W4T R-Q1D9-51FC R-RUA9-8OK9
func TestDefaultDrainCutsOffMultipleRequests(t *testing.T) {
	r := runtimeFor(t, map[string]string{"DRAIN_SECONDS": ""})
	r.client.Timeout = 7 * time.Second
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	var finished sync.WaitGroup
	finished.Add(2)
	r.p.Banner = func(page.User) page.Banner { entered <- struct{}{}; <-release; finished.Done(); return page.Banner{} }
	r.start()
	r.ready(t)
	w := <-r.writer
	responses := make(chan error, 2)
	for _, id := range []string{"first-blocked", "second-blocked"} {
		go func(id string) {
			req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://"+r.ln.Addr().String()+"/", nil)
			if err != nil {
				responses <- err
				return
			}
			req.Header.Set("X-User-Id", "drain-user")
			req.Header.Set("X-Request-Id", id)
			resp, err := r.client.Do(req)
			if err == nil {
				_, err = io.ReadAll(resp.Body)
				_ = resp.Body.Close()
			}
			responses <- err
		}(id)
	}
	<-entered
	<-entered
	flush(t, w)
	started := time.Now()
	r.cancel(errors.New("default-drain"))
	waitCtx, cancel := context.WithTimeout(context.Background(), 7*time.Second)
	defer cancel()
	select {
	case code := <-r.done:
		if code != cli.ExitServerFailed {
			t.Fatalf("exit %d", code)
		}
	case <-waitCtx.Done():
		t.Fatal("default drain did not finish")
	}
	elapsed := time.Since(started)
	if elapsed < 5*time.Second || elapsed >= 6*time.Second {
		t.Fatalf("default drain %v", elapsed)
	}
	for range 2 {
		if err := <-responses; err == nil {
			t.Fatal("complete response from cut-off connection")
		}
	}
	lines := r.output.lines()
	if len(lines) != 2 || decodeEvent(t, lines[0]).Name != "service.stopping" || lines[1] != "telemetry: stopped with 2 requests unfinished\n" {
		t.Fatalf("multiple cutoff output %q", lines)
	}
	trail := records(t, databasePath(r.p.Dir))
	if len(trail) != 3 {
		t.Fatalf("cutoff records %+v", trail)
	}
	for _, rec := range trail {
		if rec.Event != "request.started" && rec.Event != "service.started" {
			t.Fatalf("delivered after cutoff %+v", rec)
		}
	}
	r.output.end()
	close(release)
	finished.Wait()
	flush(t, w)
	if r.output.overlapping.Load() {
		t.Fatal("overlapping output")
	}
	r.output.mu.Lock()
	late := r.output.late
	r.output.mu.Unlock()
	if late {
		t.Fatal("late output")
	}
}

// R-RKJ2-6IMP
func TestAbstractReadinessSocket(t *testing.T) {
	r := runtimeFor(t, nil)
	address := "@" + filepath.Base(filepath.Dir(r.notify.LocalAddr().String())) + "-abstract"
	if err := r.notify.Close(); err != nil {
		t.Fatal(err)
	}
	notification, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: address, Net: "unixgram"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = notification.Close() })
	r.notify = notification
	originalLookup := r.p.LookupEnv
	r.p.LookupEnv = func(key string) (string, bool) {
		if key == "NOTIFY_SOCKET" {
			return address, true
		}
		return originalLookup(key)
	}
	r.start()
	r.ready(t)
	<-r.writer
	r.cancel(errors.New("abstract-stop"))
	r.finish(t, cli.ExitSuccess)
	r.output.assertQuiet(t)
}

func databasePath(dir string) string { return filepath.Join(dir, "state", "telemetry.db") }
func openDatabase(t *testing.T, path string) *db.DB {
	t.Helper()
	h, err := db.Open(context.Background(), db.Config{Path: path, Migrations: root.Migrations(), Now: func() time.Time { return time.Date(2025, 6, 20, 10, 30, 1, 123456789, time.UTC) }})
	if err != nil {
		t.Fatal(err)
	}
	return h
}
