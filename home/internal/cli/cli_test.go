package cli

import (
	"bytes"
	"context"
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
	"sync/atomic"
	"testing"
	"time"
	"unicode"

	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/home/internal/pages"
	"github.com/ikigenba/ikigenba/home/internal/server"
)

type sinkFunc func(context.Context, telemetry.Event) error

func (f sinkFunc) Deliver(ctx context.Context, e telemetry.Event) error { return f(ctx, e) }

type writes struct {
	mu      sync.Mutex
	calls   []string
	active  atomic.Bool
	overlap atomic.Bool
}

func (w *writes) Write(b []byte) (int, error) {
	if !w.active.CompareAndSwap(false, true) {
		w.overlap.Store(true)
	}
	defer w.active.Store(false)
	w.mu.Lock()
	defer w.mu.Unlock()
	w.calls = append(w.calls, string(b))
	return len(b), nil
}
func (w *writes) text() string { w.mu.Lock(); defer w.mu.Unlock(); return strings.Join(w.calls, "") }
func (w *writes) count() int   { w.mu.Lock(); defer w.mu.Unlock(); return len(w.calls) }
func (w *writes) last() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.calls) == 0 {
		return ""
	}
	return w.calls[len(w.calls)-1]
}

// R-7AW0-B0IB R-5FTJ-9MQ4 R-5I9C-167I R-4SNF-ZZMX
func TestConstants(t *testing.T) {
	const declaredUsage = Usage
	const declaredManifest = Manifest
	const success = ExitSuccess + 0
	const failed = ExitServerFailed + 0
	const usage = ExitUsage + 0
	var narrow uint8 = ExitUsage
	var floating float64 = ExitServerFailed
	if narrow != 2 || floating != 1 {
		t.Fatal(narrow, floating)
	}
	var values = []int{success, failed, usage}
	if !reflect.DeepEqual(values, []int{0, 1, 2}) {
		t.Fatal(values)
	}
	want := "Usage: home [command]\n\nServe the space's front door, a page of every service at /, on the\nsocket systemd passes in. With no command, serve.\n\nCommands:\n  manifest    print the app manifest\n\nOptions:\n  --help      print this help\n  --version   print the version\n\nExit codes:\n  0  success\n  1  failure\n  2  usage error\n"
	if declaredUsage != want {
		t.Fatalf("usage %q", Usage)
	}
	for _, c := range pages.Description {
		if c == '"' || c == '\\' || unicode.IsControl(c) {
			t.Fatalf("invalid description character %q", c)
		}
	}
	if pages.Description == "" {
		t.Fatal("empty description")
	}
	want = "app = \"home\"\ndescription = \"" + pages.Description + "\"\ndefault = true\nmcp = false\nguests = false\nsecrets = []\n\n[resources]\nmemory_max = \"128M\"\n"
	if declaredManifest != want {
		t.Fatalf("manifest %q", Manifest)
	}
}

// R-6934-G4IS R-6K27-W271 R-6LA4-9TXQ R-7DBT-2JZP R-5H1F-NEGT R-4YQX-WUCE R-7C3W-OS90 R-5N4X-K96A R-4E0N-EQQL R-46P9-44AF R-4TVC-DRDM
func TestCommands(t *testing.T) {
	for _, version := range []string{"", "caller supplied display"} {
		cases := []struct {
			args      []string
			product   string
			offending string
		}{
			{[]string{"--version"}, version + "\n", ""}, {[]string{"manifest"}, Manifest, ""}, {[]string{"--help"}, Usage, ""},
			{[]string{"bogus"}, "", "bogus"}, {[]string{"--bogus"}, "", "--bogus"}, {[]string{""}, "", ""},
			{[]string{"manifest", "x"}, "", "x"}, {[]string{"--help", "-x"}, "", "-x"}, {[]string{"--version", "manifest"}, "", "manifest"},
			{[]string{"--bogus", "--help"}, "", "--bogus"},
		}
		for _, tc := range cases {
			t.Run(fmt.Sprint(tc.args, version), func(t *testing.T) {
				var stdout bytes.Buffer
				stderr := &writes{}
				touched := false
				touch := func() { touched = true }
				p := Process{Args: tc.args, Version: version, Stdout: &stdout, Stderr: stderr,
					LookupEnv: func(string) (string, bool) { touch(); return "unusable", true }, Unsetenv: func(string) error { touch(); return nil },
					Inherit: func(uintptr) (net.Listener, error) { touch(); return nil, errors.New("forbidden") },
					Banner:  func(page.User) page.Banner { touch(); return page.Banner{} }, Sink: sinkFunc(func(context.Context, telemetry.Event) error { touch(); return nil })}
				code := Run(context.Background(), p)
				if code != ExitSuccess && code != ExitServerFailed && code != ExitUsage {
					t.Fatal(code)
				}
				if touched {
					t.Fatal("command touched service resources")
				}
				if stdout.String() != tc.product {
					t.Fatalf("stdout %q", stdout.String())
				}
				if tc.product != "" {
					if code != ExitSuccess || stderr.count() != 0 {
						t.Fatalf("code=%d stderr=%q", code, stderr.text())
					}
					return
				}
				kind := "command"
				if strings.HasPrefix(tc.offending, "-") {
					kind = "option"
				}
				want := "home: unknown " + kind + " '" + tc.offending + "'\n\nsee 'home --help' for usage\n"
				if code != ExitUsage || stderr.text() != want || stderr.count() != 1 {
					t.Fatalf("code=%d stderr=%q writes=%d", code, stderr.text(), stderr.count())
				}
			})
		}
	}
	// Commands work with all service resource fields left nil.
	for _, arg := range []string{"--version", "manifest", "--help", "bogus"} {
		var out, err bytes.Buffer
		code := Run(context.Background(), Process{Args: []string{arg}, Stdout: &out, Stderr: &err})
		if code == ExitServerFailed {
			t.Fatal(arg)
		}
	}
}

// R-778B-5PA8 R-4HOC-K1YO R-4LC1-PD6R R-714T-8UKR R-5LX1-6HFL R-5JH8-EXY7 R-79O3-X8RM R-4V38-RJ4B R-5DDQ-I38Q R-5ELM-VUZF R-7FRL-U3H3
func TestStartupChecks(t *testing.T) {
	badDrain := []string{"0", "-1", "+5", "2.5", "5s", "05", " 5", "5 ", "abc", "１", "5\n"}
	for _, v := range badDrain {
		for _, socket := range []bool{false, true} {
			env := map[string]string{"DRAIN_SECONDS": v}
			if socket {
				env["LISTEN_PID"] = "23"
				env["LISTEN_FDS"] = "1"
			}
			checkRefusal(t, env, "home: DRAIN_SECONDS is '"+v+"', not a positive whole number of seconds\n")
		}
	}
	for _, env := range []map[string]string{{}, {"LISTEN_PID": "23"}, {"LISTEN_PID": "023", "LISTEN_FDS": "1"}, {"LISTEN_PID": "24", "LISTEN_FDS": "1"}, {"LISTEN_PID": "23", "LISTEN_FDS": "0"}, {"LISTEN_PID": "23", "LISTEN_FDS": "-1"}, {"LISTEN_PID": "23", "LISTEN_FDS": "1 "}, {"LISTEN_PID": "23", "LISTEN_FDS": "+1"}, {"LISTEN_PID": "23", "LISTEN_FDS": ""}} {
		checkRefusal(t, env, "home: no socket was passed in\n\nrun it under systemd, with a listening socket passed in\n")
	}
	for _, v := range []string{"2", "002", "999999999999999999999999999999999999999999999"} {
		checkRefusal(t, map[string]string{"LISTEN_PID": "23", "LISTEN_FDS": v}, "home: "+v+" sockets were passed in, expected 1\n\nrun it under systemd, with a listening socket passed in\n")
	}
	for _, drain := range []string{"", "1", "5", "99999999999999999999999999999999999999999"} {
		for _, fds := range []string{"1", "001"} {
			for _, unset := range []bool{false, true} {
				env := map[string]string{"DRAIN_SECONDS": drain, "LISTEN_PID": "23", "LISTEN_FDS": fds}
				var out bytes.Buffer
				stderr := &writes{}
				keys := []string{}
				fdCalls := 0
				p := Process{Pid: 23, Stdout: &out, Stderr: stderr, LookupEnv: lookup(t, env), Inherit: func(fd uintptr) (net.Listener, error) {
					fdCalls++
					if fd != 3 {
						t.Fatal(fd)
					}
					return nil, errors.New("descriptor unavailable")
				}, Banner: func(page.User) page.Banner { t.Fatal("banner called"); return page.Banner{} }, Sink: sinkFunc(func(context.Context, telemetry.Event) error { t.Fatal("sink called"); return nil })}
				if unset {
					p.Unsetenv = func(key string) error { keys = append(keys, key); return errors.New("ignored removal failure") }
				}
				if code := Run(context.Background(), p); code != ExitServerFailed {
					t.Fatal(code)
				}
				if fdCalls != 1 || out.Len() != 0 || stderr.count() != 1 || stderr.text() != "home: descriptor unavailable\n" {
					t.Fatalf("calls=%d stdout=%q stderr=%q", fdCalls, out.String(), stderr.text())
				}
				if unset {
					seen := make(map[string]bool)
					for _, key := range keys {
						switch key {
						case "LISTEN_PID", "LISTEN_FDS", "LISTEN_FDNAMES":
							seen[key] = true
						default:
							t.Fatalf("unexpected removal %s", key)
						}
					}
					if len(seen) != 3 {
						t.Fatal(keys)
					}
				}
			}
		}
	}
}
func lookup(t *testing.T, env map[string]string) func(string) (string, bool) {
	t.Helper()
	return func(key string) (string, bool) {
		switch key {
		case "DRAIN_SECONDS", "IKIGENBA_SERVICES", "LISTEN_PID", "LISTEN_FDS", "NOTIFY_SOCKET":
		default:
			t.Errorf("unexpected lookup %s", key)
		}
		v, ok := env[key]
		return v, ok
	}
}
func checkRefusal(t *testing.T, env map[string]string, want string) {
	t.Helper()
	var stdout bytes.Buffer
	stderr := &writes{}
	touch := false
	p := Process{Pid: 23, Stdout: &stdout, Stderr: stderr, LookupEnv: func(key string) (string, bool) {
		if key == "NOTIFY_SOCKET" || key == "IKIGENBA_SERVICES" {
			t.Error("late lookup")
		}
		return lookup(t, env)(key)
	}, Unsetenv: func(string) error { touch = true; return nil }, Inherit: func(uintptr) (net.Listener, error) { touch = true; return nil, errors.New("unexpected") }, Banner: func(page.User) page.Banner { touch = true; return page.Banner{} }, Sink: sinkFunc(func(context.Context, telemetry.Event) error { touch = true; return nil })}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if code := Run(ctx, p); code != ExitUsage || stdout.Len() != 0 || stderr.text() != want || stderr.count() != 1 || touch {
		t.Fatalf("code=%d stdout=%q stderr=%q touch=%v", code, stdout.String(), stderr.text(), touch)
	}
}

// R-4MJY-34XG
func TestAlreadyCancelled(t *testing.T) {
	for _, during := range []bool{false, true} {
		ln := &failedListener{err: errors.New("must not accept")}
		var out, err bytes.Buffer
		capture := &telemetry.Capture{}
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		if !during {
			cancel()
		}
		env := map[string]string{"LISTEN_PID": "23", "LISTEN_FDS": "1", "NOTIFY_SOCKET": "/not/a/notify/socket"}
		code := Run(ctx, Process{Pid: 23, LookupEnv: lookup(t, env), Stdout: &out, Stderr: &err, Sink: capture, Inherit: func(uintptr) (net.Listener, error) { cancel(); return ln, nil }})
		if code != ExitSuccess || ln.accepts != 0 || len(capture.Events()) != 0 || out.Len() != 0 || err.Len() != 0 {
			t.Fatalf("code=%d accepts=%d events=%v out=%q err=%q", code, ln.accepts, capture.Events(), out.String(), err.String())
		}
	}
}

type failedListener struct {
	err     error
	accepts int
}

func (l *failedListener) Accept() (net.Conn, error) { l.accepts++; return nil, l.err }
func (*failedListener) Close() error                { return nil }
func (*failedListener) Addr() net.Addr              { return &net.UnixAddr{Name: "unused", Net: "unix"} }

type runFixture struct {
	cancel  context.CancelCauseFunc
	result  chan int
	address string
	stderr  *writes
	stdout  *writes
	capture *telemetry.Capture
}

func startRun(t *testing.T, modify func(*Process)) *runFixture {
	t.Helper()
	dir, err := os.MkdirTemp("", "home-cli-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Error(err)
		}
	})
	notify, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: filepath.Join(dir, "notify"), Net: "unixgram"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = notify.Close() })
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	f := &runFixture{result: make(chan int, 1), address: ln.Addr().String(), stderr: &writes{}, stdout: &writes{}, capture: &telemetry.Capture{}}
	env := map[string]string{"LISTEN_PID": "23", "LISTEN_FDS": "1", "NOTIFY_SOCKET": notify.LocalAddr().String(), "DRAIN_SECONDS": "60"}
	p := Process{Pid: 23, LookupEnv: lookup(t, env), Stdout: f.stdout, Stderr: f.stderr, Version: "injected display", Sink: f.capture, Inherit: func(fd uintptr) (net.Listener, error) {
		if fd != 3 {
			t.Errorf("fd %d", fd)
		}
		return ln, nil
	}, Banner: func(u page.User) page.Banner {
		return page.Banner{Service: pages.ServiceName, Version: "banner display", Email: u.Email, ProfileURL: u.ProfileURL, LogoutURL: u.LogoutURL}
	}}
	if modify != nil {
		modify(&p)
	}
	ctx, cancel := context.WithCancelCause(context.Background())
	f.cancel = cancel
	t.Cleanup(func() { cancel(errors.New("test cleanup")) })
	go func() { f.result <- Run(ctx, p) }()
	if err = notify.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	b := make([]byte, 64)
	n, _, err := notify.ReadFromUnix(b)
	if err != nil {
		t.Fatal(err)
	}
	if string(b[:n]) != "READY=1" {
		t.Fatalf("notify %q", b[:n])
	}
	return f
}
func (f *runFixture) stop(t *testing.T, reason string) int {
	t.Helper()
	f.cancel(errors.New(reason))
	select {
	case code := <-f.result:
		return code
	case <-time.After(3 * time.Second):
		t.Fatal("Run did not return")
		return -1
	}
}
func request(t *testing.T, address, path string) (int, http.Header, string) {
	t.Helper()
	client := &http.Client{Timeout: 3 * time.Second}
	req, err := http.NewRequest(http.MethodGet, "http://"+address+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Host = "home.test.invalid"
	req.Header.Set("X-User-Id", "caller-id")
	req.Header.Set("X-User-Email", "caller@example.invalid")
	req.Header.Set("X-Request-Id", "injected-request")
	req.Header.Set("X-Forwarded-Proto", "https")
	response, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = response.Body.Close() }()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	client.CloseIdleConnections()
	return response.StatusCode, response.Header, string(body)
}

// R-7EJP-GBQE R-73KM-0E25 R-6TTE-Y84L R-6YP0-HB3D R-45HC-QCJQ R-760E-RXJJ R-6GEI-QQYY
func TestServeAndStop(t *testing.T) {
	valid := filepath.Join(t.TempDir(), "services.json")
	if err := os.WriteFile(valid, []byte(`{"services":[{"name":"auth","url":"https://fixture-auth.example.invalid","description":"fixture","socket":"/fixture/auth.sock","enabled":true,"mcp":false}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{valid, "", filepath.Join(t.TempDir(), "absent"), func() string {
		p := filepath.Join(t.TempDir(), "invalid")
		if err := os.WriteFile(p, []byte("not services"), 0600); err != nil {
			t.Fatal(err)
		}
		return p
	}()} {
		for _, present := range []bool{false, true} {
			for _, version := range []string{"", "caller display"} {
				lookupCount := 0
				f := startRun(t, func(p *Process) {
					p.Version = version
					old := p.LookupEnv
					p.LookupEnv = func(key string) (string, bool) {
						if key == "IKIGENBA_SERVICES" {
							lookupCount++
							if lookupCount > 1 {
								return "changed later", true
							}
							return path, present
						}
						return old(key)
					}
				})
				// The same handler configured directly must produce exactly the same answer.
				reference := telemetry.New(telemetry.Config{Service: pages.ServiceName, Sink: &telemetry.Capture{}, Stderr: io.Discard})
				h := pages.Handler(pages.Config{Banner: func(u page.User) page.Banner {
					return page.Banner{Service: pages.ServiceName, Version: "banner display", Email: u.Email, ProfileURL: u.ProfileURL, LogoutURL: u.LogoutURL}
				}, ServicesPath: func() string {
					if present {
						return path
					}
					return ""
				}(), Telemetry: reference})
				for _, route := range []string{"/", "/about", "/unknown"} {
					status, headers, body := request(t, f.address, route)
					req := referenceRequest(t, route)
					rec := httptest.NewRecorder()
					h.ServeHTTP(rec, req)
					if status != rec.Code || body != rec.Body.String() || headers.Get("Content-Type") != rec.Header().Get("Content-Type") {
						t.Fatalf("handler mismatch %s", route)
					}
				}
				reference.Shutdown(context.Background(), "reference ended")
				f.cancel(errors.New("SIGINT"))
				select {
				case code := <-f.result:
					if code != ExitSuccess {
						t.Fatalf("code %d stderr %s", code, f.stderr.text())
					}
				case <-time.After(time.Second):
					t.Fatal("clean stop did not return within one second")
				}
				if lookupCount != 1 || f.stdout.count() != 0 || f.stderr.count() != 0 {
					t.Fatalf("lookup=%d stdout=%q stderr=%q", lookupCount, f.stdout.text(), f.stderr.text())
				}
				events := f.capture.Events()
				if len(events) != 8 {
					t.Fatalf("events %v", events)
				}
				first, last := events[0], events[len(events)-1]
				if first.Name != "service.started" || first.RequestID != "" || first.User != "" || !reflect.DeepEqual(first.Attrs, telemetry.Attrs{"version": version}) {
					t.Fatalf("first %v", first)
				}
				if last.Name != "service.stopping" || last.RequestID != "" || last.User != "" || !reflect.DeepEqual(last.Attrs, telemetry.Attrs{"reason": "SIGINT"}) {
					t.Fatalf("last %v", last)
				}
				for _, e := range events {
					if e.Service != pages.ServiceName {
						t.Fatal(e)
					}
					switch e.Name {
					case "service.started", "service.stopping", "request.started", "request.finished":
					default:
						t.Fatal(e)
					}
				}
			}
		}

	}
}

func referenceRequest(t *testing.T, path string) *http.Request {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, "http://home.test.invalid"+path, nil)
	r.Host = "home.test.invalid"
	r.Header.Set("X-User-Id", "caller-id")
	r.Header.Set("X-User-Email", "caller@example.invalid")
	r.Header.Set("X-Request-Id", "injected-request")
	r.Header.Set("X-Forwarded-Proto", "https")
	return r
}

// R-6HMF-4IPN R-6IUB-IAGC R-3UI9-AEVH
func TestReadinessAndAcceptFailure(t *testing.T) {
	for _, kind := range []string{"filesystem", "abstract", "unset", "empty", "broken"} {
		t.Run(kind, func(t *testing.T) {
			dir, err := os.MkdirTemp("", "home-notify-")
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = os.RemoveAll(dir) }()
			address := filepath.Join(dir, "notify")
			if kind == "abstract" {
				address = "@" + filepath.Base(dir)
			}
			var notify *net.UnixConn
			if kind == "filesystem" || kind == "abstract" {
				notify, err = net.ListenUnixgram("unixgram", &net.UnixAddr{Name: address, Net: "unixgram"})
				if err != nil {
					t.Fatal(err)
				}
				defer func() { _ = notify.Close() }()
			}
			env := map[string]string{"LISTEN_PID": "23", "LISTEN_FDS": "1"}
			if kind != "unset" {
				env["NOTIFY_SOCKET"] = address
			}
			if kind == "empty" {
				env["NOTIFY_SOCKET"] = ""
			}
			var notifyFailure error
			if kind == "broken" {
				probe, probeErr := net.DialUnix("unixgram", nil, &net.UnixAddr{Name: address, Net: "unixgram"})
				if probeErr == nil {
					_ = probe.Close()
					t.Fatal("broken notification socket unexpectedly available")
				}
				notifyFailure = probeErr
			}
			ln := &callbackListener{accept: func() (net.Conn, error) {
				if notify != nil {
					if err := notify.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
						t.Error(err)
					}
					b := make([]byte, 64)
					n, _, err := notify.ReadFromUnix(b)
					if err != nil || string(b[:n]) != "READY=1" {
						t.Errorf("ready=%q err=%v", b[:n], err)
					}
				}
				return nil, errors.New("accept exploded")
			}}
			capture := &telemetry.Capture{}
			var out bytes.Buffer
			stderr := &writes{}
			code := Run(context.Background(), Process{Pid: 23, LookupEnv: lookup(t, env), Stdout: &out, Stderr: stderr, Inherit: func(uintptr) (net.Listener, error) { return ln, nil }, Sink: capture, Banner: func(page.User) page.Banner { t.Fatal("banner called"); return page.Banner{} }})
			if code != ExitServerFailed || out.Len() != 0 {
				t.Fatalf("code %d stdout=%q", code, out.String())
			}
			if kind == "broken" {
				if ln.accepts != 0 || len(capture.Events()) != 0 || stderr.count() != 1 || stderr.text() != "home: "+notifyFailure.Error()+"\n" {
					t.Fatalf("accepts=%d events=%v stderr=%q", ln.accepts, capture.Events(), stderr.text())
				}
			} else {
				if ln.accepts != 1 || stderr.text() != "home: accept exploded\n" || stderr.count() != 1 {
					t.Fatalf("accepts=%d stderr=%q", ln.accepts, stderr.text())
				}
				events := capture.Events()
				if len(events) < 1 || events[0].Name != "service.started" {
					t.Fatal(events)
				}
			}
		})
	}
}

type callbackListener struct {
	accept  func() (net.Conn, error)
	accepts int
}

func (l *callbackListener) Accept() (net.Conn, error) { l.accepts++; return l.accept() }
func (*callbackListener) Close() error                { return nil }
func (*callbackListener) Addr() net.Addr              { return &net.UnixAddr{Name: "test", Net: "unix"} }

// R-78G7-JH0X R-74SI-E5SU
func TestRejectedEvents(t *testing.T) {
	capture := &telemetry.Capture{}
	f := startRun(t, func(p *Process) {
		p.Sink = sinkFunc(func(ctx context.Context, e telemetry.Event) error {
			_ = capture.Deliver(ctx, e)
			return fmt.Errorf("unavailable: %w", telemetry.ErrRejected)
		})
	})
	var group sync.WaitGroup
	for i := 0; i < 12; i++ {
		group.Add(1)
		go func() { defer group.Done(); request(t, f.address, "/") }()
	}
	group.Wait()
	if code := f.stop(t, "SIGTERM"); code != ExitSuccess {
		t.Fatal(code)
	}
	events := capture.Events()
	if len(events) != 26 {
		t.Fatalf("events %d", len(events))
	}
	var want strings.Builder
	for _, e := range events {
		b, err := e.MarshalJSON()
		if err != nil {
			t.Fatal(err)
		}
		want.WriteString("home: undelivered event: ")
		want.Write(b)
		want.WriteByte('\n')
	}
	if f.stderr.text() != want.String() || f.stderr.count() != len(events) || f.stdout.count() != 0 {
		t.Fatalf("stderr writes=%d want=%d text=%q", f.stderr.count(), len(events), f.stderr.text())
	}
	if f.stderr.overlap.Load() {
		t.Fatal("overlapping stderr writes")
	}
	before := f.stderr.count()
	if before != f.stderr.count() {
		t.Fatal("late writes")
	}
}

// R-TWPU-3772 R-4BKU-N797 R-760E-RXJJ R-4CSR-0YZW
func TestDrainCutoff(t *testing.T) {
	for _, drain := range []struct {
		value    string
		present  bool
		duration time.Duration
	}{{"1", true, time.Second}, {"2", true, 2 * time.Second}, {"", false, 5 * time.Second}, {"", true, 5 * time.Second}} {
		t.Run(fmt.Sprintf("%q-present-%t", drain.value, drain.present), func(t *testing.T) {

			entered := make(chan struct{})
			release := make(chan struct{})
			returned := make(chan struct{})
			var liveCutoffDelivery atomic.Bool
			f := startRun(t, func(p *Process) {
				sink := p.Sink
				p.Sink = sinkFunc(func(ctx context.Context, event telemetry.Event) error {
					if (event.Name == "service.stopping" || event.Name == "request.finished") && ctx.Err() == nil {
						liveCutoffDelivery.Store(true)
					}
					return sink.Deliver(ctx, event)
				})
				old := p.LookupEnv
				p.LookupEnv = func(key string) (string, bool) {
					if key == "DRAIN_SECONDS" {
						return drain.value, drain.present
					}
					return old(key)
				}
				p.Banner = func(page.User) page.Banner {
					close(entered)
					<-release
					close(returned)
					return page.Banner{Service: pages.ServiceName}
				}
			})
			response := make(chan error, 1)
			go func() {
				client := &http.Client{Timeout: drain.duration + 3*time.Second}
				req, _ := http.NewRequest(http.MethodGet, "http://"+f.address+"/", nil)
				req.Header.Set("X-User-Id", "drain-user")
				r, err := client.Do(req)
				if err == nil {
					_ = r.Body.Close()
				}
				response <- err
				client.CloseIdleConnections()
			}()
			select {
			case <-entered:
			case <-time.After(3 * time.Second):
				t.Fatal("handler did not begin")
			}
			started := time.Now()
			f.cancel(errors.New("SIGTERM"))
			select {
			case err := <-response:
				if err == nil {
					t.Fatal("cut off response completed")
				}
				if elapsed := time.Since(started); elapsed < drain.duration || elapsed >= drain.duration+time.Second {
					t.Fatalf("elapsed %s", elapsed)
				}
				if !strings.Contains(f.stderr.text(), "service.stopping") {
					t.Fatal("connection closed before stop event was written")
				}
			case <-time.After(drain.duration + 2*time.Second):
				t.Fatal("connection not cut off")
			}
			select {
			case code := <-f.result:
				if code != ExitServerFailed || time.Since(started) >= drain.duration+time.Second {
					t.Fatalf("code=%d elapsed=%s", code, time.Since(started))
				}
			case <-time.After(time.Second):
				t.Fatal("Run did not return")
			}
			if f.stderr.last() != "home: "+(&server.DrainError{Unfinished: 1}).Error()+"\n" {
				t.Fatal(f.stderr.text())
			}
			before := f.stderr.count()
			close(release)
			select {
			case <-returned:
			case <-time.After(time.Second):
				t.Fatal("handler did not return")
			}
			if f.stderr.count() != before {
				t.Fatal("writes after Run return")
			}
			if liveCutoffDelivery.Load() {
				t.Fatal("cutoff event delivered with a live context")
			}
		})
	}

}

// R-TWPU-3772 R-760E-RXJJ
func TestDrainFinishes(t *testing.T) {
	for _, drain := range []struct {
		value   string
		present bool
	}{{"", false}, {"", true}, {"2", true}} {
		entered := make(chan struct{})
		release := make(chan struct{})
		f := startRun(t, func(p *Process) {
			old := p.LookupEnv
			p.LookupEnv = func(key string) (string, bool) {
				if key == "DRAIN_SECONDS" {
					return drain.value, drain.present
				}
				return old(key)
			}
			p.Banner = func(page.User) page.Banner {
				close(entered)
				<-release
				return page.Banner{Service: pages.ServiceName, Version: "full answer marker"}
			}
		})
		body := make(chan string, 1)
		go func() { _, _, b := request(t, f.address, "/"); body <- b }()
		select {
		case <-entered:
		case <-time.After(3 * time.Second):
			t.Fatal("handler did not begin")
		}
		f.cancel(errors.New("finished"))
		// Keep the request active past a short drain, then finish inside the
		// configured window.
		select {
		case code := <-f.result:
			close(release)
			t.Fatalf("drain ended before the request was released: %d", code)
		case <-time.After(1200 * time.Millisecond):
		}
		close(release)
		select {
		case b := <-body:
			if !strings.Contains(b, "full answer marker") {
				t.Fatal("incomplete response")
			}
		case <-time.After(3 * time.Second):
			t.Fatal("response did not arrive")
		}
		select {
		case code := <-f.result:
			if code != ExitSuccess {
				t.Fatalf("code %d", code)
			}
		case <-time.After(3 * time.Second):
			t.Fatal("Run did not finish")
		}
		events := f.capture.Events()
		if len(events) != 4 || events[2].Name != "request.finished" || events[3].Name != "service.stopping" {
			t.Fatal(events)
		}
	}
}

type temporaryError struct{}

func (temporaryError) Error() string   { return "try again" }
func (temporaryError) Timeout() bool   { return false }
func (temporaryError) Temporary() bool { return true }

type retryListener struct {
	net.Listener
	once sync.Once
}

func (l *retryListener) Accept() (net.Conn, error) {
	retry := false
	l.once.Do(func() { retry = true })
	if retry {
		return nil, temporaryError{}
	}
	return l.Listener.Accept()
}

// R-5KP4-SPOW
func TestNoDefaultLogging(t *testing.T) {
	var logged bytes.Buffer
	old := log.Writer()
	log.SetOutput(&logged)
	defer log.SetOutput(old)
	f := startRun(t, func(p *Process) {
		old := p.Inherit
		p.Inherit = func(fd uintptr) (net.Listener, error) { ln, err := old(fd); return &retryListener{Listener: ln}, err }
		p.Banner = func(page.User) page.Banner { panic("test banner panic") }
	})
	client := &http.Client{Timeout: 3 * time.Second}
	req, _ := http.NewRequest(http.MethodGet, "http://"+f.address+"/", nil)
	req.Header.Set("X-User-Id", "panic-user")
	r, err := client.Do(req)
	if err == nil {
		_ = r.Body.Close()
		t.Fatal("panic request succeeded")
	}
	client.CloseIdleConnections()
	if code := f.stop(t, "SIGINT"); code != ExitSuccess {
		t.Fatal(code)
	}
	if logged.Len() != 0 {
		t.Fatal(logged.String())
	}
}

// R-760E-RXJJ R-78G7-JH0X R-74SI-E5SU
func TestBlockedDeliveryStopsAtDeadline(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan struct{})
	f := startRun(t, func(p *Process) {
		old := p.LookupEnv
		p.LookupEnv = func(key string) (string, bool) {
			if key == "DRAIN_SECONDS" {
				return "1", true
			}
			return old(key)
		}
		p.Sink = sinkFunc(func(context.Context, telemetry.Event) error { close(entered); <-release; close(finished); return nil })
	})
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("delivery did not start")
	}
	status, _, _ := request(t, f.address, "/")
	if status != http.StatusOK {
		t.Fatal(status)
	}
	start := time.Now()
	code := f.stop(t, "SIGINT")
	elapsed := time.Since(start)
	if code != ExitSuccess || elapsed < time.Second || elapsed >= 2*time.Second {
		t.Fatalf("code %d elapsed %s", code, elapsed)
	}
	text := f.stderr.text()
	lines := strings.Split(strings.TrimSuffix(text, "\n"), "\n")
	if len(lines) != 4 || f.stderr.count() != 4 {
		t.Fatalf("lines %q", text)
	}
	for i, name := range []string{"service.started", "request.started", "request.finished", "service.stopping"} {
		if !strings.HasPrefix(lines[i], "home: undelivered event: ") || !strings.Contains(lines[i], `"event":"`+name+`"`) {
			t.Fatalf("line %d %q", i, lines[i])
		}
	}
	close(release)
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("delivery did not release")
	}
	if f.stderr.text() != text || f.stderr.overlap.Load() {
		t.Fatal("stderr changed after return or overlapped")
	}
}

// R-7FRL-U3H3
func TestDefaultSocketSink(t *testing.T) {
	t.Setenv(services.Variable, "")
	f := startRun(t, func(p *Process) { p.Sink = nil })
	status, _, _ := request(t, f.address, "/")
	if status != http.StatusOK {
		t.Fatal(status)
	}
	if code := f.stop(t, "default sink test ended"); code != ExitSuccess {
		t.Fatal(code)
	}
	if f.stderr.count() != 4 || strings.Count(f.stderr.text(), "home: undelivered event: ") != 4 {
		t.Fatalf("fallback %q", f.stderr.text())
	}
}
