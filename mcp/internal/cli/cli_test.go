package cli_test

import (
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
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	appkitmcp "github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/mcp/internal/cli"
	"github.com/ikigenba/ikigenba/mcp/internal/gateway"
)

// R-V6AK-SXDV R-V7IH-6P4K
func TestVersion(t *testing.T) {
	value := &cli.Version
	if !regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?(\+[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$`).MatchString(*value) {
		t.Fatalf("invalid version %q", *value)
	}
	for _, part := range strings.SplitN(strings.SplitN(strings.TrimPrefix(*value, "v"), "+", 2)[0], "-", 2)[1:] {
		for _, id := range strings.Split(part, ".") {
			if len(id) > 1 && id[0] == '0' && strings.Trim(id, "0123456789") == "" {
				t.Fatal("numeric prerelease leading zero")
			}
		}
	}
}

// R-J0ZA-CPHS R-V9Y9-Y8LY R-X2KY-82WR R-VCE2-PS3C
func TestConstants(t *testing.T) {
	const manifest = cli.Manifest
	const usage = cli.Usage
	const a, b, c = cli.ExitSuccess, cli.ExitServerFailed, cli.ExitUsage
	if a != 0 || b != 1 || c != 2 {
		t.Fatal(a, b, c)
	}
	if manifest != "app = \"mcp\"\ndescription = \"Connect AI assistants to your services\"\ndefault = false\nmcp = false\nguests = true\nsecrets = []\n\n[resources]\nmemory_max = \"128M\"\n" {
		t.Fatal(manifest)
	}
	if usage != "Usage: mcp [command]\n\nServe the MCP gateway at /mcp, and its connect page at /, on the socket\nsystemd passes in. With no command, serve.\n\nCommands:\n  manifest   print the app manifest\n\nOptions:\n  --help      print this help\n  --version   print the version\n\nExit codes:\n  0  success\n  1  the server failed\n  2  usage error\n" {
		t.Fatal(usage)
	}
}

type writes struct{ calls [][]byte }

func (w *writes) Write(p []byte) (int, error) {
	w.calls = append(w.calls, bytes.Clone(p))
	return len(p), nil
}
func (w *writes) String() string { return string(bytes.Join(w.calls, nil)) }

// R-1ESK-Z5EA R-TGFS-0R50 R-X3SU-LUNG R-X68N-DE4U R-X7GJ-R5VJ
// R-X8OG-4XM8 R-X9WC-IPCX R-XB48-WH3M R-XCC5-A8UB R-XDK1-O0L0 R-VDLZ-3JU1
func TestCommands(t *testing.T) {
	for _, tc := range []struct {
		args     []string
		out, arg string
		code     int
	}{
		{[]string{"--version"}, cli.Version + "\n", "", 0}, {[]string{"manifest"}, cli.Manifest, "", 0}, {[]string{"--help"}, cli.Usage, "", 0},
		{[]string{"bogus"}, "", "bogus", 2}, {[]string{"-bad"}, "", "-bad", 2}, {[]string{"bogus", "--help"}, "", "bogus", 2},
		{[]string{"--version", "extra"}, "", "extra", 2}, {[]string{"manifest", "--version"}, "", "--version", 2}, {[]string{"--help", ""}, "", "", 2},
	} {
		t.Run(fmt.Sprint(tc.args), func(t *testing.T) {
			var out bytes.Buffer
			var errout writes
			forbidden := func() { t.Fatal("command touched process environment/socket") }
			p := cli.Process{Args: tc.args, LookupEnv: func(string) (string, bool) { forbidden(); return "", false }, Unsetenv: func(string) error { forbidden(); return nil }, Pid: 42, Stdout: &out, Stderr: &errout, Inherit: func(uintptr) (net.Listener, error) { forbidden(); return nil, nil }, Banner: func(page.User) page.Banner { return page.Banner{} }, MCP: func(*telemetry.Writer) *appkitmcp.Server { forbidden(); return nil }, Sink: sinkFunc(func(context.Context, telemetry.Event) error { forbidden(); return nil })}
			run := cli.Run
			code := run(context.Background(), p)
			if code != tc.code || out.String() != tc.out {
				t.Fatalf("code=%d out=%q", code, out.String())
			}
			if tc.code == 0 {
				if errout.String() != "" {
					t.Fatal(errout.String())
				}
				return
			}
			kind := "command"
			if strings.HasPrefix(tc.arg, "-") {
				kind = "option"
			}
			want := "mcp: unknown " + kind + " '" + tc.arg + "'\n\nsee 'mcp --help' for usage\n"
			if errout.String() != want || len(errout.calls) != 1 {
				t.Fatalf("diagnostic %+v", errout.calls)
			}
		})
	}
}

func refusal(t *testing.T, env map[string]string, want string) {
	t.Helper()
	var out bytes.Buffer
	var errout writes
	p := cli.Process{Pid: 42, Stdout: &out, Stderr: &errout, LookupEnv: func(k string) (string, bool) { v, ok := env[k]; return v, ok }, Unsetenv: func(string) error { t.Fatal("unset on refused start"); return nil }, Inherit: func(uintptr) (net.Listener, error) { t.Fatal("inherit on refused start"); return nil, nil }}
	if code := cli.Run(context.Background(), p); code != cli.ExitUsage || out.Len() != 0 || errout.String() != want || len(errout.calls) != 1 {
		t.Fatalf("code=%d out=%q err=%q", code, out.String(), errout.String())
	}
}

// R-W5NN-W9W0 R-W6VK-A1MP R-W83G-NTDE
func TestBadDrain(t *testing.T) {
	for _, v := range []string{"0", "-1", "2.5", "5s", "05", " 5", "abc", "+1", "1 ", "١", "1\n"} {
		t.Run(strconv.Quote(v), func(t *testing.T) {
			refusal(t, map[string]string{"DRAIN_SECONDS": v}, "mcp: DRAIN_SECONDS is '"+v+"', not a positive whole number of seconds\n")
		})
	}
}

// R-WBR5-T4LH R-WCZ2-6WC6 R-WE6Y-KO2V
func TestSocketValidation(t *testing.T) {
	none := "mcp: no socket was passed in\n\nrun it under systemd, with a listening socket passed in\n"
	for _, env := range []map[string]string{{}, {"LISTEN_PID": "042", "LISTEN_FDS": "1"}, {"LISTEN_PID": "43", "LISTEN_FDS": "1"}, {"LISTEN_FDS": "1"}, {"LISTEN_PID": "42"}, {"LISTEN_PID": "42", "LISTEN_FDS": "0"}, {"LISTEN_PID": "42", "LISTEN_FDS": "+1"}, {"LISTEN_PID": "42", "LISTEN_FDS": " 1"}, {"LISTEN_PID": "42", "LISTEN_FDS": "1s"}, {"LISTEN_PID": "42", "LISTEN_FDS": "١"}, {"DRAIN_SECONDS": "", "LISTEN_PID": "42", "LISTEN_FDS": ""}} {
		refusal(t, env, none)
	}
	for _, v := range []string{"2", "0002", strings.Repeat("9", 100)} {
		refusal(t, map[string]string{"LISTEN_PID": "42", "LISTEN_FDS": v}, "mcp: "+v+" sockets were passed in, expected 1\n\nrun it under systemd, with a listening socket passed in\n")
	}
}

// R-WHUN-PZAY R-TK3H-62D3 R-TMJ9-XLUH
func TestInheritanceFailure(t *testing.T) {
	for _, fds := range []string{"1", "0001"} {
		env := map[string]string{"LISTEN_PID": "42", "LISTEN_FDS": fds, "LISTEN_FDNAMES": "gateway", "untouched": "yes"}
		var out bytes.Buffer
		var errout writes
		var keys []string
		p := cli.Process{Pid: 42, Stdout: &out, Stderr: &errout, LookupEnv: func(k string) (string, bool) { v, ok := env[k]; return v, ok }, Unsetenv: func(k string) error { keys = append(keys, k); delete(env, k); return nil }, Inherit: func(fd uintptr) (net.Listener, error) {
			if fd != 3 {
				t.Fatalf("fd=%d", fd)
			}
			return nil, errors.New("broken descriptor")
		}}
		if code := cli.Run(context.Background(), p); code != cli.ExitServerFailed || out.Len() != 0 || errout.String() != "mcp: broken descriptor\n" {
			t.Fatal(code, errout.String())
		}
		if strings.Join(keys, ",") != "LISTEN_PID,LISTEN_FDS,LISTEN_FDNAMES" || len(env) != 1 || env["untouched"] != "yes" {
			t.Fatal(keys, env)
		}
	}
}

type harness struct {
	writer  *telemetry.Writer
	capture *telemetry.Capture
	p       cli.Process
	env     map[string]string
	ln      net.Listener
	notify  *net.UnixConn
	ctx     context.Context
	cancel  context.CancelFunc
	done    chan int
	out     bytes.Buffer
	errout  bytes.Buffer
	dir     string
}

func setup(t *testing.T) *harness {
	t.Helper()
	t.Setenv(services.Variable, "")
	dir, err := os.MkdirTemp("", "mcp-cli-")
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
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	h := &harness{env: map[string]string{"LISTEN_PID": "42", "LISTEN_FDS": "1", "NOTIFY_SOCKET": filepath.Join(dir, "notify")}, ln: ln, notify: notify, ctx: ctx, cancel: cancel, done: make(chan int, 1), dir: dir}
	h.p = cli.Process{Pid: 42, Stdout: &h.out, Stderr: &h.errout, LookupEnv: func(k string) (string, bool) { v, ok := h.env[k]; return v, ok }, Inherit: func(fd uintptr) (net.Listener, error) {
		if fd != 3 {
			t.Errorf("fd=%d", fd)
		}
		return h.ln, nil
	}, Banner: func(page.User) page.Banner { return page.Banner{} }, MCP: func(w *telemetry.Writer) *appkitmcp.Server { return gateway.NewServer(cli.Version, w) }, Sink: &telemetry.Capture{}}

	h.capture = &telemetry.Capture{}
	h.p.Sink = h.capture
	h.p.MCP = func(w *telemetry.Writer) *appkitmcp.Server { h.writer = w; return gateway.NewServer(cli.Version, w) }
	return h
}
func (h *harness) start(t *testing.T) {
	t.Helper()
	go func() { h.done <- cli.Run(h.ctx, h.p) }()
	if err := h.notify.SetReadDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	b := make([]byte, 128)
	n, _, err := h.notify.ReadFromUnix(b)
	if err != nil || string(b[:n]) != "READY=1" {
		t.Fatalf("ready=%q err=%v", b[:n], err)
	}
}
func (h *harness) finish(t *testing.T, want int) {
	t.Helper()
	select {
	case code := <-h.done:
		if code != want {
			t.Fatalf("code=%d err=%q", code, h.errout.String())
		}
	case <-time.After(8 * time.Second):
		t.Fatal("run failed to return")
	}
}
func (h *harness) get(t *testing.T, path string, id bool) *http.Response {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://"+h.ln.Addr().String()+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if id {
		req.Header.Set("X-User-Id", "person")
	}
	client := &http.Client{Timeout: 2 * time.Second}
	r, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func readBody(t *testing.T, r *http.Response) string {
	t.Helper()
	defer func() { _ = r.Body.Close() }()
	b, err := io.ReadAll(r.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// R-U8EY-8T0H R-G9BZ-FZJE R-TMJ9-XLUH
func TestServeAndCleanStop(t *testing.T) {
	h := setup(t)
	h.start(t)
	r := h.get(t, "/", true)
	if r.StatusCode != 200 {
		t.Fatal(r.StatusCode)
	}
	readBody(t, r)
	select {
	case code := <-h.done:
		t.Fatalf("returned early %d", code)
	default:
	}
	h.cancel()
	h.finish(t, 0)
	if h.out.Len() != 0 || h.errout.Len() != 0 {
		t.Fatal(h.out.String(), h.errout.String())
	}
}

// R-WKAG-HISC
func TestNotifyBeforeAcceptAndAbstract(t *testing.T) {
	for _, abstract := range []bool{false, true} {
		t.Run(fmt.Sprint(abstract), func(t *testing.T) {
			h := setup(t)
			if abstract {
				_ = h.notify.Close()
				address := "@" + filepath.Base(h.dir)
				n, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: address, Net: "unixgram"})
				if err != nil {
					t.Fatal(err)
				}
				h.notify = n
				t.Cleanup(func() { _ = n.Close() })
				h.env["NOTIFY_SOCKET"] = address
			}
			ready := make(chan error, 1)
			h.p.Inherit = func(uintptr) (net.Listener, error) {
				return &readyListener{Listener: h.ln, notify: h.notify, result: ready}, nil
			}
			go func() { h.done <- cli.Run(h.ctx, h.p) }()
			select {
			case err := <-ready:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("never accepted")
			}
			if err := h.notify.SetReadDeadline(time.Now().Add(time.Millisecond)); err != nil {
				t.Fatal(err)
			}
			b := make([]byte, 100)
			if _, _, err := h.notify.ReadFromUnix(b); err == nil {
				t.Fatal("extra datagram")
			}
			h.cancel()
			h.finish(t, 0)
		})
	}
}

type readyListener struct {
	net.Listener
	notify *net.UnixConn
	result chan error
	once   sync.Once
}

func (l *readyListener) Accept() (net.Conn, error) {
	l.once.Do(func() {
		err := l.notify.SetReadDeadline(time.Now().Add(time.Second))
		if err == nil {
			b := make([]byte, 128)
			var n int
			n, _, err = l.notify.ReadFromUnix(b)
			if err == nil && string(b[:n]) != "READY=1" {
				err = fmt.Errorf("datagram %q", b[:n])
			}
		}
		l.result <- err
	})
	return l.Listener.Accept()
}

type observeListener struct {
	net.Listener
	accept chan struct{}
	once   sync.Once
}

func (l *observeListener) Accept() (net.Conn, error) {
	l.once.Do(func() { close(l.accept) })
	return l.Listener.Accept()
}

// R-WLIC-VAJ1
func TestNotifyFailure(t *testing.T) {
	h := setup(t)
	h.env["NOTIFY_SOCKET"] = filepath.Join(h.dir, "absent")
	conn, notifyErr := net.DialUnix("unixgram", nil, &net.UnixAddr{Name: h.env["NOTIFY_SOCKET"], Net: "unixgram"})
	if notifyErr == nil {
		_ = conn.Close()
		t.Fatal("absent notification socket accepted connection")
	}
	var diagnostics writes
	h.p.Stderr = &diagnostics
	accepted := make(chan struct{})
	h.p.Inherit = func(uintptr) (net.Listener, error) { return &observeListener{Listener: h.ln, accept: accepted}, nil }
	code := cli.Run(h.ctx, h.p)
	if code != cli.ExitServerFailed || h.out.Len() != 0 || diagnostics.String() != "mcp: "+notifyErr.Error()+"\n" {
		t.Fatal(code, diagnostics.String())
	}
	select {
	case <-accepted:
		t.Fatal("accepted before failed notification")
	default:
	}
}

type failListener struct {
	net.Listener
	err error
}

func (l failListener) Accept() (net.Conn, error) { return nil, l.err }

// R-G6W6-OG20
func TestAcceptFailure(t *testing.T) {
	h := setup(t)
	delete(h.env, "NOTIFY_SOCKET")
	h.p.Inherit = func(uintptr) (net.Listener, error) { return failListener{h.ln, errors.New("accept failed")}, nil }
	if code := cli.Run(h.ctx, h.p); code != 1 || h.out.Len() != 0 || !strings.HasPrefix(h.errout.String(), "mcp: ") || strings.Count(h.errout.String(), "\n") != 1 {
		t.Fatal(code, h.errout.String())
	}
}

// R-U9MU-MKR6 R-W6VK-A1MP
func TestServicesAndLargeDrainCannotRefuseStart(t *testing.T) {
	for _, v := range []string{"", "1", strings.Repeat("9", 100)} {
		for _, path := range []string{"", "absent", "bad"} {
			t.Run(v+path, func(t *testing.T) {
				h := setup(t)
				h.env["DRAIN_SECONDS"] = v
				if path != "" {
					h.env[services.Variable] = filepath.Join(h.dir, path)
				}
				if path == "bad" {
					if err := os.WriteFile(h.env[services.Variable], []byte("not toml ["), 0600); err != nil {
						t.Fatal(err)
					}
				}
				h.start(t)
				readBody(t, h.get(t, "/", true))
				h.cancel()
				h.finish(t, 0)
				if h.out.Len() != 0 || h.errout.Len() != 0 {
					t.Fatal(h.errout.String())
				}
			})
		}
	}
}

// R-FVX3-8IDR
func TestGatewayConfiguration(t *testing.T) {
	h := setup(t)

	var calls atomic.Int32
	h.p.Banner = func(page.User) page.Banner { calls.Add(1); return page.Banner{} }
	first := filepath.Join(h.dir, "first")
	h.env[services.Variable] = first
	var changed atomic.Bool
	baseLookup := h.p.LookupEnv
	h.p.LookupEnv = func(k string) (string, bool) {
		if k == services.Variable && changed.Load() {
			return filepath.Join(h.dir, "second"), true
		}
		return baseLookup(k)
	}
	if err := os.WriteFile(first, []byte(`{"services":[{"name":"alpha","url":"","description":"First service","socket":"","enabled":true,"mcp":true}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	h.start(t)
	// Changing the seam's later answer cannot change the captured path.
	// The second file is absent; the captured file remains populated.
	changed.Store(true)
	referenceWriter := telemetry.New(telemetry.Config{Service: gateway.ServiceName, Sink: &telemetry.Capture{}})
	defer referenceWriter.Shutdown(context.Background(), "test")
	for _, path := range []string{"/", "/missing", "/mcp/bad,", "/_appkit/no-file"} {
		cfg := gateway.Config{Banner: h.p.Banner, MCP: gateway.NewServer(cli.Version, referenceWriter), ServicesPath: first, Telemetry: referenceWriter}
		reference := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("X-User-Id", "person")
		req.Host = h.ln.Addr().String()
		gateway.Handler(cfg).ServeHTTP(reference, req)
		actual := h.get(t, path, true)
		body := readBody(t, actual)
		if actual.StatusCode != reference.Code || body != reference.Body.String() {
			t.Fatalf("%s differs from configured handler status %d/%d actual=%q reference=%q", path, actual.StatusCode, reference.Code, body, reference.Body.String())
		}
	}

	client := appkitmcp.NewClient(appkitmcp.ClientConfig{Endpoint: "http://" + h.ln.Addr().String() + "/mcp", HTTPClient: &http.Client{Timeout: 2 * time.Second}})
	result, err := client.CallTool(h.ctx, identity.Caller{UserID: "person"}, "services", nil)
	if err != nil {
		t.Fatal(err)
	}
	wire, err := result.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]json.RawMessage
	if err = json.Unmarshal(wire, &decoded); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(decoded["structuredContent"], []byte(`"name":"alpha"`)) {
		t.Fatal(string(wire))
	}

	ref := httptest.NewRecorder()
	missing := httptest.NewRequest(http.MethodGet, "/missing", nil)
	missing.Host = h.ln.Addr().String()
	gateway.Handler(gateway.Config{Banner: h.p.Banner, MCP: gateway.NewServer(cli.Version, referenceWriter), ServicesPath: first, Telemetry: referenceWriter}).ServeHTTP(ref, missing)
	actualMissing := h.get(t, "/missing", false)
	if actualMissing.StatusCode != ref.Code || readBody(t, actualMissing) != ref.Body.String() {
		t.Fatal("missing identity differs")
	}
	h.cancel()
	h.finish(t, 0)

	if calls.Load() != 2 {
		t.Fatalf("banner calls=%d", calls.Load())
	}
}

type temporaryError struct{}

func (temporaryError) Error() string   { return "temporary" }
func (temporaryError) Timeout() bool   { return false }
func (temporaryError) Temporary() bool { return true }

type retryListener struct {
	net.Listener
	once sync.Once
}

func (l *retryListener) Accept() (net.Conn, error) {
	first := false
	l.once.Do(func() { first = true })
	if first {
		return nil, temporaryError{}
	}
	return l.Listener.Accept()
}

// R-WP62-0LR4
func TestServingDoesNotUseDefaultLogger(t *testing.T) {
	h := setup(t)
	var captured bytes.Buffer
	old := log.Writer()
	log.SetOutput(&captured)
	defer log.SetOutput(old)
	h.p.Banner = func(page.User) page.Banner { panic("banner failed") }
	h.p.Inherit = func(uintptr) (net.Listener, error) { return &retryListener{Listener: h.ln}, nil }
	h.start(t)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://"+h.ln.Addr().String()+"/", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-User-Id", "person")
	client := &http.Client{Timeout: 2 * time.Second}
	response, err := client.Do(req)
	if err == nil {
		_ = response.Body.Close()
		t.Fatal("expected panic to close connection")
	}
	h.cancel()
	h.finish(t, 0)
	if captured.Len() != 0 {
		t.Fatal(captured.String())
	}
}

type overlapWriter struct {
	writing atomic.Bool
	overlap atomic.Bool
}

func (w *overlapWriter) Write(p []byte) (int, error) {
	if !w.writing.CompareAndSwap(false, true) {
		w.overlap.Store(true)
	}
	defer w.writing.Store(false)
	return len(p), nil
}

// R-FZKS-DTLU
func TestConcurrentDiagnostics(t *testing.T) {
	h := setup(t)
	var output overlapWriter
	h.p.Stderr = &output
	h.p.Sink = rejectSink{}
	h.start(t)
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() { r := h.get(t, "/missing", false); readBody(t, r) })
	}
	wg.Wait()
	h.cancel()
	h.finish(t, 0)
	if output.overlap.Load() {
		t.Fatal("overlapping stderr writes")
	}
}

// R-GFFH-CU8V R-FYCW-01V5 R-U771-V19S
func TestDrainDeadlineAndNoLaterWrites(t *testing.T) {
	for _, drain := range []string{"1", "unset", ""} {
		t.Run(strconv.Quote(drain), func(t *testing.T) {
			h := setup(t)
			if drain != "unset" {
				h.env["DRAIN_SECONDS"] = drain
			}
			entered := make(chan struct{})
			release := make(chan struct{})
			exited := make(chan struct{})
			h.p.Banner = func(page.User) page.Banner { close(entered); <-release; defer close(exited); return page.Banner{} }
			h.start(t)
			conn, err := net.Dial("tcp", h.ln.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = conn.Close() }()
			if _, err = io.WriteString(conn, "GET / HTTP/1.1\r\nHost: test\r\nX-User-Id: person\r\nConnection: close\r\n\r\n"); err != nil {
				t.Fatal(err)
			}
			select {
			case <-entered:
			case <-time.After(2 * time.Second):
				t.Fatal("handler not entered")
			}
			start := time.Now()
			h.cancel()
			h.finish(t, 1)
			elapsed := time.Since(start)
			deadline := 5 * time.Second
			if drain == "1" {
				deadline = time.Second
			}
			if elapsed < deadline || elapsed >= deadline+time.Second {
				t.Fatalf("elapsed=%s deadline=%s", elapsed, deadline)
			}
			if err = conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
				t.Fatal(err)
			}
			b, err := io.ReadAll(conn)
			if err != nil || len(b) != 0 {
				t.Fatalf("cut connection body=%q err=%v", b, err)
			}
			want := "mcp: stopped with 1 request unfinished\n"
			if !strings.HasSuffix(h.errout.String(), want) || h.out.Len() != 0 {
				t.Fatal(h.errout.String())
			}
			before := h.errout.String()
			close(release)
			<-exited
			if h.errout.String() != before {
				t.Fatal("wrote after return")
			}
		})
	}
}

// R-GFFH-CU8V
func TestDrainCompletesFullResponse(t *testing.T) {
	h := setup(t)
	h.env["DRAIN_SECONDS"] = "1"
	entered := make(chan struct{})
	release := make(chan struct{})
	h.p.Banner = func(page.User) page.Banner { close(entered); <-release; return page.Banner{} }
	h.start(t)
	result := make(chan string, 1)
	go func() { result <- readBody(t, h.get(t, "/", true)) }()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("not entered")
	}
	h.cancel()
	select {
	case code := <-h.done:
		t.Fatalf("returned while active %d", code)
	default:
	}
	close(release)
	select {
	case body := <-result:
		if !strings.Contains(body, "</html>") {
			t.Fatal("incomplete response")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("response not delivered")
	}
	h.finish(t, 0)
}

// R-U771-V19S R-FYCW-01V5
func TestBackendDrainCannotWriteAfterReturn(t *testing.T) {
	h := setup(t)
	h.env["DRAIN_SECONDS"] = "1"
	backendPath := filepath.Join(h.dir, "backend")
	ln, err := net.Listen("unix", backendPath)
	if err != nil {
		t.Fatal(err)
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	backendExited := make(chan struct{})
	backend := &http.Server{ReadHeaderTimeout: time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(entered)
		<-release
		defer close(backendExited)
		_, _ = io.WriteString(w, `{"jsonrpc":"2.0","id":1,"result":{"tools":[]}}`)
	})}
	go func() { _ = backend.Serve(ln) }()
	t.Cleanup(func() { _ = backend.Close() })
	servicesPath := filepath.Join(h.dir, "services")
	fixture := fmt.Sprintf(`{"services":[{"name":"alpha","url":"","description":"","socket":%q,"enabled":true,"mcp":true}]}`, backendPath)
	if err = os.WriteFile(servicesPath, []byte(fixture), 0600); err != nil {
		t.Fatal(err)
	}
	h.env[services.Variable] = servicesPath
	h.start(t)
	client := appkitmcp.NewClient(appkitmcp.ClientConfig{Endpoint: "http://" + h.ln.Addr().String() + "/mcp", HTTPClient: &http.Client{Timeout: 3 * time.Second}})
	returned := make(chan struct{})
	go func() {
		_, _ = client.CallTool(context.Background(), identity.Caller{UserID: "person", RequestID: "drain"}, "describe", json.RawMessage(`{"service":"alpha"}`))
		close(returned)
	}()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("backend never called")
	}
	h.cancel()
	h.finish(t, cli.ExitServerFailed)
	before := h.errout.String()
	if !strings.HasSuffix(before, "mcp: stopped with 1 request unfinished\n") {
		t.Fatal(before)
	}
	close(release)
	<-backendExited
	select {
	case <-returned:
	case <-time.After(2 * time.Second):
		t.Fatal("client never returned")
	}
	if h.errout.String() != before {
		t.Fatal("late backend diagnostic", h.errout.String())
	}
}

type sinkFunc func(context.Context, telemetry.Event) error

func (s sinkFunc) Deliver(ctx context.Context, e telemetry.Event) error { return s(ctx, e) }

type rejectSink struct{}

func (rejectSink) Deliver(context.Context, telemetry.Event) error {
	return fmt.Errorf("refused: %w", telemetry.ErrRejected)
}

// R-1X32-PPIP R-1ZIV-H903 R-G38H-J4TX
func TestRunWriterLifecycle(t *testing.T) {
	h := setup(t)
	ctx, cancel := context.WithCancelCause(context.Background())
	h.ctx = ctx
	defer cancel(nil)
	var calls int
	h.p.MCP = func(w *telemetry.Writer) *appkitmcp.Server {
		calls++
		if w == nil {
			t.Error("nil writer")
		}
		return gateway.NewServer(cli.Version, w)
	}
	h.start(t)
	readBody(t, h.get(t, "/", true))
	cancel(errors.New("SIGINT"))
	h.finish(t, cli.ExitSuccess)
	events := h.capture.Events()
	if calls != 1 || len(events) != 4 {
		t.Fatalf("calls=%d events=%v", calls, events)
	}
	want := []string{"service.started", "request.started", "request.finished", "service.stopping"}
	for i, e := range events {
		if e.Service != gateway.ServiceName || e.Name != want[i] {
			t.Fatal(events)
		}
	}
	for _, i := range []int{0, 3} {
		if events[i].RequestID != "" || events[i].User != "" {
			t.Fatal(events[i])
		}
	}
	if !reflect.DeepEqual(events[0].Attrs, telemetry.Attrs{"version": cli.Version}) || !reflect.DeepEqual(events[3].Attrs, telemetry.Attrs{"reason": "SIGINT"}) {
		t.Fatal(events)
	}
}

// R-G20L-5D38
func TestUndeliveredEventEnvelope(t *testing.T) {
	h := setup(t)
	var output writes
	h.p.Stderr = &output
	var rejected []telemetry.Event
	h.p.Sink = sinkFunc(func(_ context.Context, e telemetry.Event) error {
		rejected = append(rejected, e)
		return fmt.Errorf("no: %w", telemetry.ErrRejected)
	})
	h.start(t)
	readBody(t, h.get(t, "/", true))
	h.cancel()
	h.finish(t, 0)
	if len(output.calls) != len(rejected) {
		t.Fatal(len(output.calls), len(rejected))
	}
	for i, e := range rejected {
		raw, err := e.MarshalJSON()
		if err != nil {
			t.Fatal(err)
		}
		want := "mcp: undelivered event: " + string(raw) + "\n"
		if string(output.calls[i]) != want {
			t.Fatalf("%q want %q", output.calls[i], want)
		}
	}
}

// R-20QR-V0QS
func TestRefusedStartDoesNotDeliver(t *testing.T) {
	for _, mode := range []string{"command", "drain", "socket", "many", "inherit", "notify"} {
		t.Run(mode, func(t *testing.T) {
			h := setup(t)
			h.p.Sink = sinkFunc(func(context.Context, telemetry.Event) error { t.Error("refused start delivered"); return nil })
			if mode == "command" {
				h.p.MCP = func(*telemetry.Writer) *appkitmcp.Server { t.Error("command constructed server"); return nil }
			}
			switch mode {
			case "command":
				h.p.Args = []string{"--help"}
			case "drain":
				h.env["DRAIN_SECONDS"] = "0"
			case "socket":
				delete(h.env, "LISTEN_FDS")
			case "many":
				h.env["LISTEN_FDS"] = "2"
			case "inherit":
				h.p.Inherit = func(uintptr) (net.Listener, error) { return nil, errors.New("inherit failed") }
			case "notify":
				h.env["NOTIFY_SOCKET"] = filepath.Join(h.dir, "absent")
			}
			_ = cli.Run(h.ctx, h.p)
		})
	}
}

// R-FUP6-UQN2 R-G5OA-AOBB R-8XD8-XO8M R-G9BZ-FZJE
func TestBlockedTrailDoesNotBlockReadinessOrAnswers(t *testing.T) {
	h := setup(t)
	h.env["DRAIN_SECONDS"] = "1"
	entered := make(chan struct{})
	release := make(chan struct{})
	returned := make(chan struct{})
	var deliveries atomic.Int32
	h.p.Sink = sinkFunc(func(context.Context, telemetry.Event) error {
		deliveries.Add(1)
		close(entered)
		<-release
		close(returned)
		return nil
	})
	h.start(t)
	<-entered
	readBody(t, h.get(t, "/", true))
	start := time.Now()
	h.cancel()
	h.finish(t, 0)
	if elapsed := time.Since(start); elapsed < time.Second || elapsed >= 2*time.Second {
		t.Fatal(elapsed)
	}
	lines := strings.Split(strings.TrimSuffix(h.errout.String(), "\n"), "\n")
	names := []string{}
	for _, line := range lines {
		if !strings.HasPrefix(line, "mcp: undelivered event: ") {
			t.Fatal(line)
		}
		var e struct {
			Name string `json:"event"`
		}
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "mcp: undelivered event: ")), &e); err != nil {
			t.Fatal(err)
		}
		names = append(names, e.Name)
	}
	if !reflect.DeepEqual(names, []string{"service.started", "request.started", "request.finished", "service.stopping"}) {
		t.Fatal(names)
	}
	if h.out.Len() != 0 {
		t.Fatal(h.out.String())
	}
	close(release)
	<-returned
	if deliveries.Load() != 1 {
		t.Fatal(deliveries.Load())
	}
}

type controlledFailure struct {
	net.Listener
	failure   chan struct{}
	accepted  chan struct{}
	closed    chan struct{}
	closeOnce sync.Once
	once      sync.Once
}

func (l *controlledFailure) Accept() (net.Conn, error) {
	first := false
	l.once.Do(func() { first = true })
	if first {
		c, e := l.Listener.Accept()
		close(l.accepted)
		return c, e
	}
	<-l.failure
	return nil, errors.New("accept stopped")
}

func (l *controlledFailure) Close() error {
	if l.closed != nil {
		l.closeOnce.Do(func() { close(l.closed) })
	}
	return l.Listener.Close()
}

// R-GBRS-7J0S R-GCZO-LARH R-GGND-QLZK R-G6W6-OG20
func TestAcceptFailureDrainsCompleteResponse(t *testing.T) {
	h := setup(t)
	h.env["DRAIN_SECONDS"] = "1"
	fail := make(chan struct{})
	accepted := make(chan struct{})
	listenerClosed := make(chan struct{})
	h.p.Inherit = func(uintptr) (net.Listener, error) {
		return &controlledFailure{Listener: h.ln, failure: fail, accepted: accepted, closed: listenerClosed}, nil
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	h.p.Banner = func(page.User) page.Banner { close(entered); <-release; return page.Banner{} }
	h.start(t)
	answer := make(chan string, 1)
	go func() { answer <- readBody(t, h.get(t, "/", true)) }()
	<-accepted
	<-entered
	close(fail)
	<-listenerClosed
	h.cancel()
	// Listener closure proves that the failure was classified before the later signal.
	select {
	case code := <-h.done:
		t.Fatalf("returned with active request: %d", code)
	default:
	}
	close(release)
	if body := <-answer; !strings.Contains(body, "</html>") {
		t.Fatal("incomplete response")
	}
	h.finish(t, 1)
	h.cancel()
	if h.errout.String() != "mcp: accept stopped\n" {
		t.Fatal(h.errout.String())
	}
	events := h.capture.Events()
	if len(events) != 4 || events[2].Name != "request.finished" || events[3].Name != "service.stopping" || !reflect.DeepEqual(events[3].Attrs, telemetry.Attrs{"reason": "failed"}) {
		t.Fatal(events)
	}
}

// R-GE7K-Z2I6 R-GBRS-7J0S R-8XD8-XO8M R-G6W6-OG20
func TestAcceptFailureBoundsBlockedTrail(t *testing.T) {
	h := setup(t)
	h.env["DRAIN_SECONDS"] = "1"
	delete(h.env, "NOTIFY_SOCKET")
	h.p.Inherit = func(uintptr) (net.Listener, error) { return failListener{h.ln, errors.New("accept failed")}, nil }
	release := make(chan struct{})
	returned := make(chan struct{})
	var calls atomic.Int32
	h.p.Sink = sinkFunc(func(context.Context, telemetry.Event) error { calls.Add(1); <-release; close(returned); return nil })
	start := time.Now()
	code := cli.Run(h.ctx, h.p)
	if code != 1 || time.Since(start) < time.Second || time.Since(start) >= 2*time.Second {
		t.Fatal(code, time.Since(start))
	}
	lines := strings.Split(strings.TrimSuffix(h.errout.String(), "\n"), "\n")
	if len(lines) != 3 || lines[2] != "mcp: accept failed" {
		t.Fatal(lines)
	}
	for i, name := range []string{"service.started", "service.stopping"} {
		var e struct {
			Name string `json:"event"`
		}
		if err := json.Unmarshal([]byte(strings.TrimPrefix(lines[i], "mcp: undelivered event: ")), &e); err != nil {
			t.Fatal(err)
		}
		if e.Name != name {
			t.Fatal(e)
		}
	}
	close(release)
	if calls.Load() > 0 {
		<-returned
	}
	if calls.Load() > 1 {
		t.Fatal(calls.Load())
	}
}

// R-GBRS-7J0S R-GE7K-Z2I6 R-8XD8-XO8M R-U771-V19S
func TestAcceptFailureCutsOffActiveRequest(t *testing.T) {
	h := setup(t)
	h.env["DRAIN_SECONDS"] = "1"
	fail := make(chan struct{})
	accepted := make(chan struct{})
	closed := make(chan struct{})
	h.p.Inherit = func(uintptr) (net.Listener, error) {
		return &controlledFailure{Listener: h.ln, failure: fail, accepted: accepted, closed: closed}, nil
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	exited := make(chan struct{})
	h.p.Banner = func(page.User) page.Banner { close(entered); <-release; defer close(exited); return page.Banner{} }
	h.start(t)
	conn, err := net.Dial("tcp", h.ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	if _, err = io.WriteString(conn, "GET / HTTP/1.1\r\nHost: test\r\nX-User-Id: person\r\nConnection: close\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	<-accepted
	<-entered
	start := time.Now()
	close(fail)
	<-closed
	h.cancel() // An accept failure owns the stop even if the signal follows it.
	h.finish(t, cli.ExitServerFailed)
	if elapsed := time.Since(start); elapsed < time.Second || elapsed >= 2*time.Second {
		t.Fatal(elapsed)
	}
	if err = conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(conn)
	if err != nil || len(body) != 0 {
		t.Fatalf("body=%q error=%v", body, err)
	}
	before := h.errout.String()
	if !strings.HasSuffix(before, "mcp: accept stopped\n") {
		t.Fatal(before)
	}
	events := h.capture.Events()
	for _, e := range events {
		if e.Name == "service.stopping" {
			t.Fatal("delivered stopping past deadline")
		}
	}
	close(release)
	<-exited
	if h.errout.String() != before {
		t.Fatal("late stderr write")
	}
	if !reflect.DeepEqual(h.capture.Events(), events) {
		t.Fatal("late delivery")
	}
}

type wrappedRetryListener struct {
	errorToReturn error
	net.Listener
	once sync.Once
}

func (l *wrappedRetryListener) Accept() (net.Conn, error) {
	first := false
	l.once.Do(func() { first = true })
	if first {
		return nil, l.errorToReturn
	}
	return l.Listener.Accept()
}

// R-G6W6-OG20
func TestWrappedTemporaryAcceptIsRetried(t *testing.T) {
	for _, err := range []error{
		fmt.Errorf("wrapped: %w", temporaryError{}),
		nonTemporaryWrapper{temporaryError{}},
		errors.Join(nonTemporaryWrapper{errors.New("fatal branch")}, nonTemporaryWrapper{temporaryError{}}),
	} {
		t.Run(err.Error(), func(t *testing.T) {
			h := setup(t)
			h.p.Inherit = func(uintptr) (net.Listener, error) {
				return &wrappedRetryListener{Listener: h.ln, errorToReturn: err}, nil
			}
			h.start(t)
			readBody(t, h.get(t, "/", true))
			h.cancel()
			h.finish(t, 0)
			if h.errout.Len() != 0 {
				t.Fatal(h.errout.String())
			}
		})
	}
}

type nonTemporaryWrapper struct{ err error }

func (e nonTemporaryWrapper) Error() string   { return "outer non-temporary: " + e.err.Error() }
func (e nonTemporaryWrapper) Temporary() bool { return false }
func (e nonTemporaryWrapper) Unwrap() error   { return e.err }

type finalOverlapWriter struct {
	writing atomic.Bool
	overlap atomic.Bool
	entered chan struct{}
	release chan struct{}
}

func (w *finalOverlapWriter) Write(p []byte) (int, error) {
	if !w.writing.CompareAndSwap(false, true) {
		w.overlap.Store(true)
	}
	defer w.writing.Store(false)
	if strings.HasPrefix(string(p), "mcp: stopped with ") {
		close(w.entered)
		<-w.release
	}
	return len(p), nil
}

// R-FZKS-DTLU
func TestFinalDiagnosticAndLateEventsNeverOverlap(t *testing.T) {
	h := setup(t)
	h.env["DRAIN_SECONDS"] = "1"
	output := &finalOverlapWriter{entered: make(chan struct{}), release: make(chan struct{})}
	h.p.Stderr = output
	requestEntered := make(chan struct{})
	requestRelease := make(chan struct{})
	h.p.Banner = func(page.User) page.Banner { close(requestEntered); <-requestRelease; return page.Banner{} }
	h.start(t)
	conn, err := net.Dial("tcp", h.ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	if _, err = io.WriteString(conn, "GET / HTTP/1.1\r\nHost: test\r\nX-User-Id: person\r\nConnection: close\r\n\r\n"); err != nil {
		t.Fatal(err)
	}
	<-requestEntered
	h.cancel()
	select {
	case <-output.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("final diagnostic not reached")
	}
	// Shutdown has completed. Late request events now contend with Run's own diagnostic.
	start := make(chan struct{})
	var begun, finished sync.WaitGroup
	for range 64 {
		begun.Add(1)
		finished.Add(1)
		go func() {
			defer finished.Done()
			<-start
			begun.Done()
			h.writer.Emit(context.Background(), "request.finished", telemetry.Attrs{"status": 200, "duration_us": 0})
		}()
	}
	close(start)
	begun.Wait()
	// Give runnable emitters their turn while the injected diagnostic remains in progress.
	for range 64 {
		runtime.Gosched()
	}
	close(output.release)
	h.finish(t, cli.ExitServerFailed)
	close(requestRelease)
	finished.Wait()
	if output.overlap.Load() {
		t.Fatal("an event write overlapped Run's final diagnostic")
	}
}
