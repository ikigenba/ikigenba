package cli_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/appkit/version"
	"github.com/ikigenba/ikigenba/connections"
	"github.com/ikigenba/ikigenba/connections/internal/cli"
	"github.com/ikigenba/ikigenba/connections/internal/pages"
	"github.com/ikigenba/ikigenba/connections/internal/providers"
	"github.com/ikigenba/ikigenba/connections/internal/settings"
)

type writes struct {
	bytes.Buffer
	count int
}

func (w *writes) WriteString(s string) (int, error) { return w.Write([]byte(s)) }
func (w *writes) Write(b []byte) (int, error)       { w.count++; return w.Buffer.Write(b) }

type forbiddenReader struct{ t *testing.T }

func (r forbiddenReader) Read([]byte) (int, error) { r.t.Error("read forbidden"); return 0, io.EOF }

type transport func(*http.Request) (*http.Response, error)

func (f transport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type sink struct{ t *testing.T }

func (s sink) Deliver(context.Context, telemetry.Event) error {
	s.t.Error("deliver forbidden")
	return nil
}

type eventSink struct{ t *testing.T }

func (s eventSink) Deliver(context.Context, events.Event) error {
	s.t.Error("emit forbidden")
	return nil
}

// R-01WG-IL1Y R-034C-WCSN R-1FWC-76GF R-1LZU-415W R-1IC4-YPXT R-1B0Q-O3HN R-1C8N-1V8C R-1DGJ-FMZ1 R-1H48-KY74 R-1JK1-CHOI R-1KRX-Q9F7
func TestCommands(t *testing.T) {
	const usageAlias = cli.Usage
	const manifestAlias = cli.Manifest
	const nginxAlias = cli.NginxConf
	var success uint8 = cli.ExitSuccess
	var failure uint8 = cli.ExitServerFailed
	var usage uint8 = cli.ExitUsage
	if usageAlias != cli.Usage || manifestAlias != cli.Manifest || nginxAlias != cli.NginxConf || success != 0 || failure != 1 || usage != 2 {
		t.Fatal("constant declarations")
	}
	const expectedUsage = "Usage: connections [command]\n\nAccounts at outside providers, connected for the suite's connectors: a\nperson connects one at /<provider>/connect, siblings on the host get\naccess tokens at /token/<provider>, with MCP tools at /mcp and a page of\nconnections at /, on the socket systemd passes in.\nWith no command, serve.\n\nCommands:\n  manifest    print the app manifest\n  db status   print applied and pending migrations\n\nOptions:\n  --help      print this help\n  --version   print the version\n\nExit codes:\n  0  success\n  1  failure\n  2  usage error\n"
	const expectedManifest = "app = \"connections\"\ndescription = \"Accounts at outside providers, connected for the suite's connectors\"\ndefault = false\nmcp = true\nguests = false\nsecrets = [\"GOOGLE_CLIENT_ID\", \"GOOGLE_CLIENT_SECRET\"]\n\n[database]\nengine = \"sqlite\"\npath = \"state/connections.db\"\n\n[resources]\nmemory_max = \"128M\"\n\n[home]\ngroup = \"core\"\n"
	const expectedNginx = "location ^~ /token/ { return 404; }\nlocation = /events { return 404; }\nlocation = /declarations { return 404; }\n"
	if cli.Usage != expectedUsage || cli.Manifest != expectedManifest || cli.NginxConf != expectedNginx {
		t.Fatal("command constants differ")
	}
	for _, tc := range []struct {
		args []string
		out  string
		arg  string
	}{{[]string{"--version"}, "test-display\n", ""}, {[]string{"manifest"}, cli.Manifest, ""}, {[]string{"--help"}, cli.Usage, ""}, {[]string{"bogus"}, "", "bogus"}, {[]string{"-bad"}, "", "-bad"}, {[]string{"--version", "tail"}, "", "tail"}, {[]string{"manifest", "-tail"}, "", "-tail"}, {[]string{"--help", "x"}, "", "x"}, {[]string{"db"}, "", "db"}, {[]string{"db", "wrong", "status"}, "", "wrong"}, {[]string{"db", "status", "-extra", "later"}, "", "-extra"}} {
		var out bytes.Buffer
		var err writes
		dir := t.TempDir()
		bad := func() { t.Error("forbidden process hook") }
		p := cli.Process{Args: tc.args, Version: "test-display", Stdout: &out, Stderr: &err, Dir: dir, LookupEnv: func(string) (string, bool) { bad(); return "", false }, Unsetenv: func(string) error { bad(); return nil }, Inherit: func(uintptr) (net.Listener, error) { bad(); return nil, nil }, Now: func() time.Time { bad(); return time.Time{} }, Sleep: func(context.Context, time.Duration) { bad() }, Rand: forbiddenReader{t}, Banner: func(page.User) page.Banner { bad(); return page.Banner{} }, MCP: func(*telemetry.Writer) *mcp.Server { bad(); return nil }, Sink: sink{t}, EventSink: eventSink{t}, HTTP: &http.Client{Transport: transport(func(*http.Request) (*http.Response, error) { bad(); return nil, errors.New("forbidden") })}}
		code := cli.Run(context.Background(), p)
		if tc.arg == "" {
			if code != 0 || err.Len() != 0 || out.String() != tc.out {
				t.Fatalf("%v: %d %q %q", tc.args, code, out.String(), err.String())
			}
		} else {
			kind := "command"
			if strings.HasPrefix(tc.arg, "-") {
				kind = "option"
			}
			want := "connections: unknown " + kind + " '" + tc.arg + "'\n\nsee 'connections --help' for usage\n"
			if code != 2 || out.Len() != 0 || err.String() != want || err.count != 1 {
				t.Fatalf("%v: %d %q", tc.args, code, err.String())
			}
		}
		entries, e := os.ReadDir(dir)
		if e != nil || len(entries) != 0 {
			t.Fatal(entries, e)
		}
	}
	for _, tc := range []struct{ name, s string }{{"manifest.toml", cli.Manifest}, {"nginx.conf", cli.NginxConf}} {
		b, e := fs.ReadFile(connections.Etc(), tc.name)
		if e != nil || string(b) != tc.s {
			t.Fatal(tc.name, e)
		}
	}
	if cli.ExitSuccess != 0 || cli.ExitServerFailed != 1 || cli.ExitUsage != 2 {
		t.Fatal("exit values")
	}
}

// R-1N7Q-HSWL
func TestDatabaseStatus(t *testing.T) {
	for _, mode := range []string{"absent", "open", "invalid"} {
		dir := t.TempDir()
		path := filepath.Join(dir, "state", "connections.db")
		if mode == "open" {
			d, e := db.Open(context.Background(), db.Config{Path: path, Migrations: connections.Migrations(), Now: func() time.Time { return time.Unix(123, 0) }})
			if e != nil {
				t.Fatal(e)
			}
			_ = d.Close()
		}
		if mode == "invalid" {
			if e := os.WriteFile(filepath.Join(dir, "state"), []byte("fixture"), 0600); e != nil {
				t.Fatal(e)
			}
		}
		var expected, out bytes.Buffer
		var stderr writes
		e := db.Status(context.Background(), db.Config{Path: path, Migrations: connections.Migrations()}, &expected)
		code := cli.Run(context.Background(), cli.Process{Args: []string{"db", "status"}, Dir: dir, Stdout: &out, Stderr: &stderr})
		if out.String() != expected.String() {
			t.Fatal(out.String(), expected.String())
		}
		if e == nil {
			if code != 0 || stderr.Len() != 0 {
				t.Fatal(code, stderr.String())
			}
		} else if code != 1 || stderr.count != 1 || stderr.String() != "connections: "+strings.ReplaceAll(e.Error(), "\n", " ")+"\n" {
			t.Fatal(code, stderr.String())
		}
		if mode == "absent" {
			es, _ := os.ReadDir(dir)
			if len(es) != 0 {
				t.Fatal(es)
			}
		}
	}
}
func env(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) { v, ok := m[k]; return v, ok }
}
func provider() providers.Provider {
	p := providers.Table()[0]
	p.Name = "fake"
	p.AuthorizeURL = "https://provider.invalid/authorize"
	p.TokenURL = "https://provider.invalid/token"
	p.RevokeURL = "https://provider.invalid/revoke"
	p.ClientIDVar = "CLIENT_ID"
	p.ClientSecretVar = "CLIENT_SECRET"
	p.Account = func(string) (providers.Account, error) {
		return providers.Account{Email: "account@example.com", Subject: "subject"}, nil
	}
	return p
}

// R-18KX-WK09 R-19SU-ABQY R-1TB8-ENM2
func TestStartupValidation(t *testing.T) {
	for _, tc := range []struct {
		m         map[string]string
		providers []providers.Provider
		want      string
		calls     []string
	}{{map[string]string{"GOOGLE_CLIENT_SECRET": "yes"}, nil, "connections: GOOGLE_CLIENT_ID is not set\n", []string{"GOOGLE_CLIENT_ID"}}, {map[string]string{"CLIENT_ID": "id", "CLIENT_SECRET": "secret", "DRAIN_SECONDS": "5s"}, []providers.Provider{provider()}, "connections: " + (&settings.Error{Name: "DRAIN_SECONDS", Value: "5s", Unit: "seconds"}).Error() + "\n", []string{"CLIENT_ID", "CLIENT_SECRET", "DRAIN_SECONDS"}}, {map[string]string{"CLIENT_ID": "id", "CLIENT_SECRET": "secret", "IKIGENBA_CALLBACK_URL": ""}, []providers.Provider{provider()}, "connections: " + (&settings.OriginError{Name: "IKIGENBA_CALLBACK_URL"}).Error() + "\n", []string{"CLIENT_ID", "CLIENT_SECRET", "DRAIN_SECONDS", "IKIGENBA_CALLBACK_URL"}}, {map[string]string{"CLIENT_ID": "id", "CLIENT_SECRET": "secret"}, []providers.Provider{provider()}, "connections: no socket was passed in\n\nrun it under systemd, with a listening socket passed in\n", []string{"CLIENT_ID", "CLIENT_SECRET", "DRAIN_SECONDS", "IKIGENBA_CALLBACK_URL", "LISTEN_PID", "LISTEN_FDS"}}, {map[string]string{"CLIENT_ID": "id", "CLIENT_SECRET": "secret", "LISTEN_PID": "23", "LISTEN_FDS": "02"}, []providers.Provider{provider()}, "connections: 02 sockets were passed in, expected 1\n\nrun it under systemd, with a listening socket passed in\n", []string{"CLIENT_ID", "CLIENT_SECRET", "DRAIN_SECONDS", "IKIGENBA_CALLBACK_URL", "LISTEN_PID", "LISTEN_FDS"}}} {
		dir := t.TempDir()
		var out bytes.Buffer
		var stderr writes
		calls := []string{}
		code := cli.Run(context.Background(), cli.Process{Dir: dir, Providers: tc.providers, Pid: 23, Stdout: &out, Stderr: &stderr, LookupEnv: func(k string) (string, bool) { calls = append(calls, k); return env(tc.m)(k) }, Unsetenv: func(string) error { t.Error("unset"); return nil }, Inherit: func(uintptr) (net.Listener, error) { t.Error("inherit"); return nil, nil }})
		if code != 2 || out.Len() != 0 || stderr.count != 1 || stderr.String() != tc.want || !reflect.DeepEqual(calls, tc.calls) {
			t.Fatalf("%d %q %v", code, stderr.String(), calls)
		}
		es, _ := os.ReadDir(dir)
		if len(es) != 0 {
			t.Fatal(es)
		}
	}
}

type repeat byte

func (r repeat) Read(b []byte) (int, error) {
	for i := range b {
		b[i] = byte(r)
	}
	return len(b), nil
}

func packet(c *net.UnixConn) (string, error) {
	raw, e := c.SyscallConn()
	if e != nil {
		return "", e
	}
	var n int
	var readErr error
	b := make([]byte, 100)
	e = raw.Control(func(fd uintptr) { n, _, readErr = syscall.Recvfrom(int(fd), b, syscall.MSG_DONTWAIT) })
	if e != nil {
		return "", e
	}
	if readErr != nil {
		return "", readErr
	}
	return string(b[:n]), nil
}

type noAccept struct {
	net.Listener
	calls atomic.Int32
}

func (l *noAccept) Accept() (net.Conn, error) { l.calls.Add(1); return l.Listener.Accept() }

type readyListener struct {
	net.Listener
	notify *net.UnixConn
	once   sync.Once
	ready  chan error
}

func (l *readyListener) Accept() (net.Conn, error) {
	l.once.Do(func() {
		s, e := packet(l.notify)
		if e == nil && s != "READY=1" {
			e = fmt.Errorf("unexpected notification %q", s)
		}
		l.ready <- e
	})
	return l.Listener.Accept()
}

type running struct {
	endpoint string
	notify   *net.UnixConn
	p        cli.Process
	cancel   context.CancelCauseFunc
	done     chan int
	client   *http.Client
	out, err *bytes.Buffer
	capture  *telemetry.Capture
	bus      *events.Capture
}

func start(t *testing.T, dir string, modify func(*cli.Process, map[string]string)) *running {
	t.Helper()
	t.Setenv(services.Variable, "")
	socketDir, e := os.MkdirTemp("", "connections-")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = os.RemoveAll(socketDir) })
	notify, e := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: filepath.Join(socketDir, "ready"), Net: "unixgram"})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = notify.Close() })
	ln, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	guarded := &readyListener{Listener: ln, notify: notify, ready: make(chan error, 1)}
	inheritCalls := 0
	r := &running{done: make(chan int, 1), out: &bytes.Buffer{}, err: &bytes.Buffer{}, capture: &telemetry.Capture{}, bus: &events.Capture{}}
	values := map[string]string{"CLIENT_ID": "client chosen", "CLIENT_SECRET": "secret chosen", "IKIGENBA_CALLBACK_URL": "https://callback.example:7443", "LISTEN_PID": "23", "LISTEN_FDS": "1", "NOTIFY_SOCKET": notify.LocalAddr().String()}
	r.notify = notify
	r.p = cli.Process{Dir: dir, LookupEnv: env(values), Pid: 23, Stdout: r.out, Stderr: r.err, Version: "run-display", Inherit: func(fd uintptr) (net.Listener, error) {
		inheritCalls++
		if fd != 3 || inheritCalls != 1 {
			t.Error(fd, inheritCalls)
		}
		return guarded, nil
	}, Now: func() time.Time { return time.Unix(1700000000, 0) }, Rand: repeat(0x5a), Sink: r.capture, EventSink: r.bus, Providers: []providers.Provider{provider()}, Banner: page.New(pages.ServiceName, version.Identity{}).Banner, HTTP: &http.Client{Transport: transport(func(req *http.Request) (*http.Response, error) {
		t.Errorf("unexpected provider call %s", req.URL)
		return nil, errors.New("unexpected")
	})}}
	if modify != nil {
		modify(&r.p, values)
	}
	ctx, cancel := context.WithCancelCause(context.Background())
	r.cancel = cancel
	go func() { r.done <- cli.Run(ctx, r.p) }()
	select {
	case e := <-guarded.ready:
		if e != nil {
			cancel(errors.New("failed"))
			t.Fatalf("notification must precede accepting: %v", e)
		}
	case <-time.After(5 * time.Second):
		cancel(errors.New("failed"))
		t.Fatal("server did not accept")
	}
	if inheritCalls != 1 {
		t.Fatal(inheritCalls)
	}
	r.client = &http.Client{Transport: &http.Transport{}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }, Timeout: 5 * time.Second}
	r.endpoint = "http://" + ln.Addr().String()
	t.Cleanup(func() { r.client.CloseIdleConnections() })
	return r
}
func (r *running) stop(t *testing.T) {
	t.Helper()
	r.client.CloseIdleConnections()
	r.cancel(errors.New("test-stop"))
	select {
	case code := <-r.done:
		if code != 0 {
			t.Fatalf("exit %d %s", code, r.err.String())
		}
	case <-time.After(7 * time.Second):
		t.Fatal("run did not stop")
	}
	if s, e := packet(r.notify); !errors.Is(e, syscall.EAGAIN) && !errors.Is(e, syscall.EWOULDBLOCK) {
		t.Fatalf("additional notification %q, %v", s, e)
	}
	if r.out.Len() != 0 || r.err.Len() != 0 {
		t.Fatal(r.out.String(), r.err.String())
	}
}
func (r *running) get(t *testing.T, method, path string) *http.Response {
	t.Helper()
	req, e := http.NewRequest(method, r.endpoint+path, nil)
	if e != nil {
		t.Fatal(e)
	}
	req.Header.Set("X-User-Id", "user")
	req.Host = "connections.example"
	req.Header.Set("X-Forwarded-Proto", "https")
	if method == "POST" {
		req.Header.Set("Origin", "https://connections.example")
	}
	res, e := r.client.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = res.Body.Close() })
	return res
}

// R-1UJ4-SFCR R-0SQ8-XJD8 R-0TY5-BB3X R-0V61-P2UM R-2P0M-COW4 R-20MM-PA28 R-21UJ-31SX R-24AB-ULAB
func TestRunWiring(t *testing.T) {
	dir := t.TempDir()
	unset := []string{}
	calls := []string{}
	r := start(t, dir, func(p *cli.Process, m map[string]string) {
		p.Unsetenv = func(k string) error { unset = append(unset, k); delete(m, k); return nil }
		p.LookupEnv = func(k string) (string, bool) { calls = append(calls, k); return env(m)(k) }
	})
	res := r.get(t, "GET", "/fake/connect")
	if res.StatusCode != 302 {
		t.Fatal(res.StatusCode)
	}
	u, e := res.Location()
	if e != nil {
		t.Fatal(e)
	}
	if !strings.HasPrefix(u.String(), provider().AuthorizeURL+"?") || u.Query().Get("state") != "connections."+strings.Repeat("5a", 16) || u.Query().Get("client_id") != "client chosen" || u.Query().Get("redirect_uri") != "https://callback.example:7443/fake/callback" {
		t.Fatal(u)
	}
	f, e := os.Stat(filepath.Join(dir, "state", "connections.db"))
	if e != nil || !f.Mode().IsRegular() {
		t.Fatal(f, e)
	}
	r.stop(t)
	if !reflect.DeepEqual(unset, []string{"LISTEN_PID", "LISTEN_FDS", "LISTEN_FDNAMES"}) {
		t.Fatal(unset)
	}
	want := []string{"CLIENT_ID", "CLIENT_SECRET", "DRAIN_SECONDS", "IKIGENBA_CALLBACK_URL", "LISTEN_PID", "LISTEN_FDS", "IKIGENBA_SERVICES", "NOTIFY_SOCKET"}
	if !reflect.DeepEqual(calls, want) {
		t.Fatal(calls)
	}
	es := r.capture.Events()
	if len(es) < 4 || es[0].Name != "service.started" || es[0].RequestID != "" || es[0].User != "" || !reflect.DeepEqual(es[0].Attrs, telemetry.Attrs{"version": "run-display"}) || es[len(es)-1].Name != "service.stopping" || !reflect.DeepEqual(es[len(es)-1].Attrs, telemetry.Attrs{"reason": "test-stop"}) {
		t.Fatal(es)
	}
	stopping := 0
	for _, e := range es {
		if e.Name == "service.stopping" {
			stopping++
			if e.User != "" || e.RequestID != "" {
				t.Fatal(e)
			}
		}
		if e.Time != time.Unix(1700000000, 0).UTC() {
			t.Fatal(e.Time)
		}
	}
	if stopping != 1 {
		t.Fatal(stopping)
	}
}

// R-1VR1-673G R-1WYX-JYU5
func TestDatabaseRefusal(t *testing.T) {
	dir := t.TempDir()
	if e := os.WriteFile(filepath.Join(dir, "state"), []byte("fixture"), 0600); e != nil {
		t.Fatal(e)
	}
	_, expected := db.Open(context.Background(), db.Config{Path: filepath.Join(dir, "state", "connections.db"), Migrations: connections.Migrations(), Now: func() time.Time { return time.Unix(1, 0) }})
	ln, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = ln.Close() }()
	short, e := os.MkdirTemp("", "refusal-")
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = os.RemoveAll(short) }()
	notify, e := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: filepath.Join(short, "notify"), Net: "unixgram"})
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = notify.Close() }()
	guarded := &noAccept{Listener: ln}
	var out bytes.Buffer
	var stderr writes
	c := &telemetry.Capture{}
	code := cli.Run(context.Background(), cli.Process{Dir: dir, Pid: 23, LookupEnv: env(map[string]string{"CLIENT_ID": "id", "CLIENT_SECRET": "secret", "LISTEN_PID": "23", "LISTEN_FDS": "1", "NOTIFY_SOCKET": notify.LocalAddr().String()}), Providers: []providers.Provider{provider()}, Inherit: func(uintptr) (net.Listener, error) { return guarded, nil }, Stdout: &out, Stderr: &stderr, Sink: c})
	if code != 1 || out.Len() != 0 || stderr.count != 1 || stderr.String() != "connections: cannot open database state/connections.db: "+strings.ReplaceAll(expected.Error(), "\n", " ")+"\n" || len(c.Events()) != 0 {
		t.Fatal(code, stderr.String())
	}
	if guarded.calls.Load() != 0 {
		t.Fatal("accepted during refusal")
	}
	if s, e := packet(notify); !errors.Is(e, syscall.EAGAIN) && !errors.Is(e, syscall.EWOULDBLOCK) {
		t.Fatalf("notified during refusal: %q %v", s, e)
	}
}

// R-LZ0S-WAZX R-D0A1-57L0 R-04C9-A4JC
func TestPersistenceAndPurge(t *testing.T) {
	dir := t.TempDir()
	r := start(t, dir, nil)
	u, e := r.get(t, "GET", "/fake/connect").Location()
	if e != nil {
		t.Fatal(e)
	}
	state := u.Query().Get("state")
	r.stop(t)
	for _, tc := range []struct {
		dir     string
		advance time.Duration
		failure string
	}{{dir, 0, pages.FailureDenied}, {t.TempDir(), 0, pages.FailureUnknown}} {
		r = start(t, tc.dir, func(p *cli.Process, _ map[string]string) {
			p.Now = func() time.Time { return time.Unix(1700000000, 0).Add(tc.advance) }
		})
		res := r.get(t, "GET", "/fake/callback?error=access_denied&state="+state)
		checkNotice(t, res, tc.failure)
		r.stop(t)
	}
	r = start(t, dir, nil)
	u, e = r.get(t, "GET", "/fake/connect").Location()
	if e != nil {
		t.Fatal(e)
	}
	state = u.Query().Get("state")
	r.stop(t)
	r = start(t, dir, func(p *cli.Process, _ map[string]string) {
		p.Now = func() time.Time { return time.Unix(1700000000, 0).Add(11 * time.Minute) }
	})
	checkNotice(t, r.get(t, "GET", "/fake/callback?error=access_denied&state="+state), pages.FailureUnknown)
	r.stop(t)
	empty := t.TempDir()
	t.Chdir(empty)
	r = start(t, "", nil)
	if _, e := os.Stat(filepath.Join(empty, "state", "connections.db")); e != nil {
		t.Fatal(e)
	}
	r.stop(t)
}
func checkNotice(t *testing.T, res *http.Response, failure string) {
	t.Helper()
	b, e := io.ReadAll(res.Body)
	if e != nil {
		t.Fatal(e)
	}
	set, e := page.Templates().ParseFS(connections.Assets(), "*.html")
	if e != nil {
		t.Fatal(e)
	}
	banner := page.New(pages.ServiceName, version.Identity{}).Banner(page.User{ProfileURL: "https://auth.example/", LogoutURL: "https://auth.example/logout"})
	var expected bytes.Buffer
	if e := set.ExecuteTemplate(&expected, "notice", pages.NoticeData{Banner: banner, DisplayName: provider().DisplayName, Failure: failure}); e != nil {
		t.Fatal(e)
	}
	if res.StatusCode != 400 || !bytes.Equal(b, expected.Bytes()) {
		t.Fatalf("notice %s status %d\ngot: %s\nwant: %s", failure, res.StatusCode, b, expected.Bytes())
	}
}

// R-PFHM-CCZV R-PGPI-Q4QK
func TestProviderHTTPAndSinks(t *testing.T) {
	var now atomic.Int64
	now.Store(1700000000)
	var mu sync.Mutex
	tokens, revoke := 0, 0
	r := start(t, t.TempDir(), func(p *cli.Process, _ map[string]string) {
		p.Now = func() time.Time { return time.Unix(now.Load(), 0) }
		p.HTTP = &http.Client{Transport: transport(func(req *http.Request) (*http.Response, error) {
			if req.URL.Host != "provider.invalid" {
				t.Error(req.URL)
			}
			if e := req.ParseForm(); e != nil {
				t.Error(e)
			}
			mu.Lock()
			defer mu.Unlock()
			body := "{}"
			switch req.URL.Path {
			case "/token":
				tokens++
				if req.Form.Get("client_id") != "client chosen" || req.Form.Get("client_secret") != "secret chosen" {
					t.Error(req.Form)
				}
				body = fmt.Sprintf(`{"access_token":"access-%d","refresh_token":"refresh","expires_in":60,"id_token":"ignored"}`, tokens)
			case "/revoke":
				revoke++
			default:
				t.Error(req.URL)
			}
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
		})}
	})
	u, e := r.get(t, "GET", "/fake/connect").Location()
	if e != nil {
		t.Fatal(e)
	}
	res := r.get(t, "GET", "/fake/callback?state="+u.Query().Get("state")+"&code=chosen")
	if res.StatusCode != 302 {
		t.Fatal(res.StatusCode)
	}
	now.Add(120)
	res = r.get(t, "GET", "/token/fake")
	b, e := io.ReadAll(res.Body)
	if e != nil || res.StatusCode != 200 || !strings.Contains(string(b), "access-2") {
		t.Fatal(res.StatusCode, string(b), e)
	}
	res = r.get(t, "POST", "/fake/disconnect")
	if res.StatusCode != 302 {
		t.Fatal(res.StatusCode)
	}
	r.stop(t)
	mu.Lock()
	defer mu.Unlock()
	if tokens != 2 || revoke != 1 {
		t.Fatal(tokens, revoke)
	}
	es := r.bus.Events()
	if len(es) != 2 || es[0].Name != "connection.created" || es[1].Name != "connection.deleted" {
		t.Fatal(es)
	}
}

type guardedRandom struct {
	active atomic.Int32
	calls  atomic.Int64
}

func (g *guardedRandom) Read(b []byte) (int, error) {
	if !g.active.CompareAndSwap(0, 1) {
		panic("concurrent random read")
	}
	defer g.active.Store(0)
	n := g.calls.Add(1)
	for i := range b {
		b[i] = byte(n & 255)
	}
	return len(b), nil
}

// R-05K5-NWA1
func TestSharedRandomConcurrent(t *testing.T) {
	random := &guardedRandom{}
	r := start(t, t.TempDir(), func(p *cli.Process, _ map[string]string) { p.Rand = random })
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			res := r.get(t, "GET", "/fake/connect")
			if res.StatusCode != 302 {
				t.Error(res.StatusCode)
			}
		})
	}
	wg.Wait()
	r.stop(t)
	if random.calls.Load() < 40 {
		t.Fatal(random.calls.Load())
	}
}

// R-232F-GTJM
func TestRunDrainDeadline(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	exited := make(chan struct{})
	r := start(t, t.TempDir(), func(p *cli.Process, m map[string]string) {
		m["DRAIN_SECONDS"] = "1"
		p.Banner = func(page.User) page.Banner { close(entered); <-release; close(exited); return page.Banner{} }
	})
	response := make(chan struct{})
	go func() {
		req, _ := http.NewRequest("GET", r.endpoint+"/about", nil)
		req.Header.Set("X-User-Id", "user")
		res, _ := r.client.Do(req)
		if res != nil {
			_ = res.Body.Close()
		}
		close(response)
	}()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("no handler")
	}
	r.cancel(errors.New("deadline-stop"))
	select {
	case code := <-r.done:
		if code != cli.ExitServerFailed || !strings.HasSuffix(r.err.String(), "connections: stopped with 1 request unfinished\n") || r.out.Len() != 0 {
			t.Fatal(code, r.err.String())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("deadline ignored")
	}
	close(release)
	select {
	case <-exited:
	case <-time.After(3 * time.Second):
		t.Fatal("handler did not exit")
	}
	select {
	case <-response:
	case <-time.After(3 * time.Second):
		t.Fatal("response did not finish")
	}
}

// R-D0A1-57L0
func TestFailedStartupPurgeStillServes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "state", "connections.db")
	d, e := db.Open(context.Background(), db.Config{Path: path, Migrations: connections.Migrations(), Now: func() time.Time { return time.Unix(1700000000, 0) }})
	if e != nil {
		t.Fatal(e)
	}
	e = d.Write(context.Background(), func(tx *sql.Tx) error { _, e := tx.Exec("DROP TABLE pending"); return e })
	if e != nil {
		t.Fatal(e)
	}
	_ = d.Close()
	r := start(t, dir, nil)
	res := r.get(t, "GET", "/about")
	if res.StatusCode != 200 {
		t.Fatal(res.StatusCode)
	}
	r.stop(t)
}

// R-04C9-A4JC R-PGPI-Q4QK R-24AB-ULAB
func TestSocketEventSink(t *testing.T) {
	short, e := os.MkdirTemp("", "event-sink-")
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = os.RemoveAll(short) }()
	ln, e := net.Listen("unix", filepath.Join(short, "events"))
	if e != nil {
		t.Fatal(e)
	}
	delivered := make(chan string, 2)
	hs := &http.Server{ReadHeaderTimeout: time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var v struct {
			Name string `json:"event"`
		}
		if r.Method != "POST" || r.URL.Path != events.EmitPath {
			t.Error(r.Method, r.URL.Path)
		}
		if e := json.NewDecoder(r.Body).Decode(&v); e != nil {
			t.Error(e)
		}
		delivered <- v.Name
		w.WriteHeader(204)
	})}
	done := make(chan error, 1)
	go func() { done <- hs.Serve(ln) }()
	defer func() {
		_ = hs.Close()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("sink did not stop")
		}
	}()
	file := filepath.Join(short, "services.json")
	document := map[string]any{"services": []map[string]any{{"name": "events", "url": "https://events.example", "description": "test", "socket": ln.Addr().String(), "enabled": true, "mcp": false}}}
	b, e := json.Marshal(document)
	if e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(file, b, 0600); e != nil {
		t.Fatal(e)
	}
	r := start(t, t.TempDir(), func(p *cli.Process, _ map[string]string) {
		t.Setenv(services.Variable, file)
		p.EventSink = nil
		p.HTTP = &http.Client{Transport: transport(func(req *http.Request) (*http.Response, error) {
			if req.URL.Host != "provider.invalid" {
				t.Error(req.URL)
			}
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"access_token":"access","refresh_token":"refresh","expires_in":60,"id_token":"ignored"}`))}, nil
		})}
	})
	u, e := r.get(t, "GET", "/fake/connect").Location()
	if e != nil {
		t.Fatal(e)
	}
	res := r.get(t, "GET", "/fake/callback?state="+u.Query().Get("state")+"&code=chosen")
	if res.StatusCode != 302 {
		t.Fatal(res.StatusCode)
	}
	r.stop(t)
	select {
	case name := <-delivered:
		if name != "connection.created" {
			t.Fatal(name)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no bus delivery")
	}
	select {
	case name := <-delivered:
		t.Fatal("additional bus event", name)
	default:
	}
	es := r.capture.Events()
	if es[0].Name != "service.started" || es[len(es)-1].Name != "service.stopping" {
		t.Fatal(es)
	}
}

// R-24AB-ULAB
func TestProviderFailuresStayQuiet(t *testing.T) {
	var mode atomic.Int32
	r := start(t, t.TempDir(), func(p *cli.Process, _ map[string]string) {
		p.HTTP = &http.Client{Transport: transport(func(_ *http.Request) (*http.Response, error) {
			status := 200
			body := `{"access_token":"access","refresh_token":"refresh","expires_in":60,"id_token":"ignored"}`
			switch mode.Load() {
			case 0:
				status = 400
			case 2, 3:
				status = 500
			}
			return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
		})}
	})
	u, e := r.get(t, "GET", "/fake/connect").Location()
	if e != nil {
		t.Fatal(e)
	}
	res := r.get(t, "GET", "/fake/callback?state="+u.Query().Get("state")+"&code=bad")
	if res.StatusCode != 502 {
		t.Fatal(res.StatusCode)
	}
	mode.Store(1)
	u, e = r.get(t, "GET", "/fake/connect").Location()
	if e != nil {
		t.Fatal(e)
	}
	res = r.get(t, "GET", "/fake/callback?state="+u.Query().Get("state")+"&code=good")
	if res.StatusCode != 302 {
		t.Fatal(res.StatusCode)
	}
	mode.Store(2)
	res = r.get(t, "GET", "/token/fake")
	if res.StatusCode != 502 {
		t.Fatal(res.StatusCode)
	}
	mode.Store(3)
	res = r.get(t, "POST", "/fake/disconnect")
	if res.StatusCode != 302 {
		t.Fatal(res.StatusCode)
	}
	r.stop(t)
}
