package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/auth/internal/idcodec"
	"github.com/ikigenba/ikigenba/auth/internal/version"
)

const wantUsage = `Usage: auth [command]

Serve the auth service on the socket systemd passes in. With no command,
serve.

Commands:
  manifest   print the app manifest

Options:
  --help      print this help
  --version   print the version

Exit codes:
  0  success
  1  the server failed
  2  usage error
`
const wantManifest = `app = "auth"
default = false
secrets = ["GOOGLE_CLIENT_ID", "GOOGLE_CLIENT_SECRET"]

[env]
WORKSPACE_DOMAIN = "michaelgreenly.dev"

[database]
engine = "sqlite"
path = "state/auth.db"
`

const wantSocketHint = "\n\nrun it under systemd, or locally with 'systemd-socket-activate -l 127.0.0.1:3001 auth'\n"

func TestSurface(t *testing.T) {
	// R-LTJ7-WBS6
	typ := reflect.TypeOf(Process{})
	want := []struct {
		name string
		typ  reflect.Type
	}{
		{"Args", reflect.TypeFor[[]string]()}, {"LookupEnv", reflect.TypeFor[func(string) (string, bool)]()},
		{"Unsetenv", reflect.TypeFor[func(string) error]()}, {"Pid", reflect.TypeFor[int]()},
		{"Stdout", reflect.TypeFor[io.Writer]()}, {"Stderr", reflect.TypeFor[io.Writer]()},
		{"Inherit", reflect.TypeFor[func(uintptr) (net.Listener, error)]()},
		{"Now", reflect.TypeFor[func() time.Time]()}, {"Rand", reflect.TypeFor[io.Reader]()},
		{"OIDCIssuer", reflect.TypeFor[string]()}, {"DBSource", reflect.TypeFor[string]()},
	}
	if typ.NumField() != len(want) {
		t.Fatalf("Process fields = %d", typ.NumField())
	}
	for i, w := range want {
		f := typ.Field(i)
		if f.Name != w.name || f.Type != w.typ {
			t.Fatalf("field %d = %s %s", i, f.Name, f.Type)
		}
	}
	// R-LUR4-A3IV R-3XVE-I7OD
	runFn := Run
	if code := runFn(t.Context(), Process{Args: []string{"--version"}, Stdout: io.Discard, Stderr: io.Discard}); code != 0 {
		t.Fatal(code)
	}
}

func TestCommands(t *testing.T) {
	tests := []struct {
		args      []string
		out, diag string
		code      int
	}{
		{[]string{"--version"}, version.Version + "\n", "", 0},                                          // R-P02O-R3KF
		{[]string{"--help"}, wantUsage, "", 0},                                                          // R-P1AL-4VB4 R-M5Q7-Q174
		{[]string{"manifest"}, wantManifest, "", 0},                                                     // R-P2IH-IN1T R-M6Y4-3SXT
		{[]string{"bogus"}, "", "auth: unknown command 'bogus'\n\nsee 'auth --help' for usage\n", 2},    // R-P666-NY9W R-P8LZ-FHRA
		{[]string{"--bogus"}, "", "auth: unknown option '--bogus'\n\nsee 'auth --help' for usage\n", 2}, // R-P7E3-1Q0L
		{[]string{"manifest", "extra"}, "", "auth: unknown command 'extra'\n\nsee 'auth --help' for usage\n", 2},
	}
	// R-OV73-80LN R-M860-HKOI R-M9DW-VCF7
	for _, tt := range tests {
		t.Run(strings.Join(tt.args, "_"), func(t *testing.T) {
			out := new(bytes.Buffer)
			errOut := &countWriter{}
			called := false
			p := Process{Args: tt.args, LookupEnv: func(string) (string, bool) { called = true; return "", false }, Unsetenv: func(string) error { called = true; return nil }, Inherit: func(uintptr) (net.Listener, error) { called = true; return nil, errors.New("unexpected") }, Stdout: out, Stderr: errOut}
			code := Run(t.Context(), p)
			if code != tt.code || out.String() != tt.out || errOut.String() != tt.diag || called {
				t.Fatalf("code=%d out=%q diag=%q called=%v", code, out, errOut.String(), called)
			}
			if tt.diag != "" && errOut.calls != 1 {
				t.Fatalf("diagnostic writes=%d", errOut.calls)
			}
		})
	}
	// R-P4YA-A6J7
	b, err := os.ReadFile(filepath.Join("..", "..", "etc", "manifest.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != wantManifest {
		t.Fatalf("manifest file=%q", b)
	}
}

type countWriter struct {
	bytes.Buffer
	calls int
}

type safeRecordWriter struct {
	mu sync.Mutex
	bytes.Buffer
}

func (w *safeRecordWriter) Write(b []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.Buffer.Write(b)
}
func (w *safeRecordWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.Buffer.String()
}

func (w *countWriter) Write(b []byte) (int, error) { w.calls++; return w.Buffer.Write(b) }

func baseProcess(env map[string]string, source string, ln net.Listener) Process {
	return Process{LookupEnv: func(k string) (string, bool) { v, ok := env[k]; return v, ok }, Pid: 42, Stdout: new(bytes.Buffer), Stderr: new(countWriter), Inherit: func(fd uintptr) (net.Listener, error) {
		if fd != 3 {
			return nil, fmt.Errorf("fd %d", fd)
		}
		return ln, nil
	}, Now: func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC) }, Rand: bytes.NewReader(bytes.Repeat([]byte{0x42}, 4096)), OIDCIssuer: "http://127.0.0.1:0", DBSource: source}
}
func goodEnv() map[string]string {
	return map[string]string{"GOOGLE_CLIENT_ID": "id", "GOOGLE_CLIENT_SECRET": "secret", "WORKSPACE_DOMAIN": "example.test", "LISTEN_PID": "42", "LISTEN_FDS": "1"}
}
func testSource(t *testing.T) string { return filepath.Join(t.TempDir(), "auth.db") }

func TestConfigAndSocketValidation(t *testing.T) {
	//nolint:misspell // R-MROE-LWJM is an opaque requirement ID.
	// R-MKD0-BA3G R-MLKW-P1U5 R-MO0P-GLBJ R-MMST-2TKU R-MQGI-84SX R-MSWA-ZOAB
	cases := []struct {
		name   string
		mutate func(map[string]string)
		want   string
	}{
		{"missing client", func(e map[string]string) { delete(e, "GOOGLE_CLIENT_ID") }, "auth: GOOGLE_CLIENT_ID is not set\n"},
		{"empty secret", func(e map[string]string) { e["GOOGLE_CLIENT_SECRET"] = "" }, "auth: GOOGLE_CLIENT_SECRET is not set\n"},
		{"missing domain", func(e map[string]string) { delete(e, "WORKSPACE_DOMAIN") }, "auth: WORKSPACE_DOMAIN is not set\n"},
		{"drain zero", func(e map[string]string) { e["DRAIN_SECONDS"] = "0" }, "auth: DRAIN_SECONDS is '0', not a positive whole number of seconds\n"},
		{"drain leading zero", func(e map[string]string) { e["DRAIN_SECONDS"] = "05" }, "auth: DRAIN_SECONDS is '05', not a positive whole number of seconds\n"},
		{"drain fraction", func(e map[string]string) { e["DRAIN_SECONDS"] = "2.5" }, "auth: DRAIN_SECONDS is '2.5', not a positive whole number of seconds\n"},
		{"drain negative", func(e map[string]string) { e["DRAIN_SECONDS"] = "-1" }, "auth: DRAIN_SECONDS is '-1', not a positive whole number of seconds\n"},
		{"drain suffix", func(e map[string]string) { e["DRAIN_SECONDS"] = "5s" }, "auth: DRAIN_SECONDS is '5s', not a positive whole number of seconds\n"},
		{"drain whitespace", func(e map[string]string) { e["DRAIN_SECONDS"] = " 5" }, "auth: DRAIN_SECONDS is ' 5', not a positive whole number of seconds\n"},
		{"drain alpha", func(e map[string]string) { e["DRAIN_SECONDS"] = "abc" }, "auth: DRAIN_SECONDS is 'abc', not a positive whole number of seconds\n"},
		{"no pid", func(e map[string]string) { delete(e, "LISTEN_PID") }, "auth: no socket was passed in" + wantSocketHint},
		{"wrong pid", func(e map[string]string) { e["LISTEN_PID"] = "43" }, "auth: no socket was passed in" + wantSocketHint},
		{"no fds", func(e map[string]string) { delete(e, "LISTEN_FDS") }, "auth: no socket was passed in" + wantSocketHint},
		{"bad fds", func(e map[string]string) { e["LISTEN_FDS"] = "1x" }, "auth: no socket was passed in" + wantSocketHint},
		{"zero fds", func(e map[string]string) { e["LISTEN_FDS"] = "000" }, "auth: no socket was passed in" + wantSocketHint},
		{"two fds", func(e map[string]string) { e["LISTEN_FDS"] = "002" }, "auth: 002 sockets were passed in, expected 1" + wantSocketHint},
		{"huge fds", func(e map[string]string) { e["LISTEN_FDS"] = strings.Repeat("9", 100) }, "auth: " + strings.Repeat("9", 100) + " sockets were passed in, expected 1" + wantSocketHint},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			env := goodEnv()
			tt.mutate(env)
			p := baseProcess(env, testSource(t), nil)
			calls := 0
			p.Inherit = func(uintptr) (net.Listener, error) { calls++; return nil, errors.New("unexpected") }
			p.Unsetenv = func(string) error { calls++; return nil }
			code := Run(t.Context(), p)
			if code != 2 || p.Stdout.(*bytes.Buffer).Len() != 0 || p.Stderr.(*countWriter).String() != tt.want || calls != 0 {
				t.Fatalf("code=%d diag=%q calls=%d", code, p.Stderr, calls)
			}
			if _, err := os.Stat(p.DBSource); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("database opened: %v", err)
			}
		})
	}
	// Check precedence even when later inputs are bad.
	env := goodEnv()
	delete(env, "GOOGLE_CLIENT_ID")
	env["DRAIN_SECONDS"] = "0"
	env["LISTEN_FDS"] = "2"
	p := baseProcess(env, testSource(t), nil)
	if code := Run(t.Context(), p); code != 2 || p.Stderr.(*countWriter).String() != "auth: GOOGLE_CLIENT_ID is not set\n" {
		t.Fatal(p.Stderr)
	}
	// A bad drain is decided before either socket-activation lookup.
	env = goodEnv()
	env["DRAIN_SECONDS"] = "0"
	p = baseProcess(env, testSource(t), nil)
	p.LookupEnv = func(k string) (string, bool) {
		if k == "LISTEN_PID" || k == "LISTEN_FDS" {
			t.Fatalf("socket lookup before drain validation: %s", k)
		}
		v, ok := env[k]
		return v, ok
	}
	if code := Run(t.Context(), p); code != 2 || p.Stderr.(*countWriter).String() != "auth: DRAIN_SECONDS is '0', not a positive whole number of seconds\n" {
		t.Fatal(p.Stderr)
	}
}

func TestInheritedListenerFailuresAndOpenOrder(t *testing.T) {
	// R-MU47-DG10 R-MVC3-R7RP R-MWK0-4ZIE R-MXRW-IR93 R-MYZS-WIZS
	env := goodEnv()
	source := testSource(t)
	p := baseProcess(env, source, nil)
	var unset []string
	calls := 0
	p.Unsetenv = func(k string) error { unset = append(unset, k); return nil }
	p.Inherit = func(fd uintptr) (net.Listener, error) {
		calls++
		if fd != 3 {
			t.Fatalf("fd %d", fd)
		}
		return nil, errors.New("bad listener")
	}
	if code := Run(t.Context(), p); code != 1 || p.Stderr.(*countWriter).String() != "auth: bad listener\n" || calls != 1 {
		t.Fatalf("code=%d diag=%q", code, p.Stderr)
	}
	if !slices.Equal(unset, []string{"LISTEN_PID", "LISTEN_FDS", "LISTEN_FDNAMES"}) {
		t.Fatal(unset)
	}
	if _, err := os.Stat(source); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("store opened before listener: %v", err)
	}
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	obstruction := filepath.Join(t.TempDir(), "parent-file")
	if err := os.WriteFile(obstruction, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	source = filepath.Join(obstruction, "auth.db")
	p = baseProcess(env, source, ln)
	if code := Run(t.Context(), p); code != 1 || !strings.HasPrefix(p.Stderr.(*countWriter).String(), "auth: cannot open database "+source+": ") {
		t.Fatalf("code=%d diag=%q", code, p.Stderr)
	}
	if p.Stdout.(*bytes.Buffer).Len() != 0 {
		t.Fatal("stdout on failure")
	}
}

func TestServeReadinessAndInjectedSeam(t *testing.T) {
	// R-NKXZ-SECA R-NII7-0UUW R-A6NL-V77Q R-N1FL-O2H6 R-N3VE-FLYK R-N6B7-75FY R-GYUP-OF53 R-OYUS-DBTQ
	for _, preexisting := range []bool{false, true} {
		t.Run(strconv.FormatBool(preexisting), func(t *testing.T) {
			source := testSource(t)
			if preexisting { // make a valid existing database through a first run
				ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
				if err != nil {
					t.Fatal(err)
				}
				env := goodEnv()
				p := baseProcess(env, source, ln)
				ctx, cancel := context.WithCancel(t.Context())
				cancel()
				if code := Run(ctx, p); code != 0 {
					t.Fatalf("initial open: %d %s", code, p.Stderr)
				}
			}
			ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			env := goodEnv()
			dir, err := os.MkdirTemp("", "auth-notify-")
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = os.RemoveAll(dir) }()
			addr := filepath.Join(dir, "notify.sock")
			if preexisting {
				addr = "@" + filepath.Base(dir)
			}
			notifyLn, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: addr, Net: "unixgram"})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = notifyLn.Close() }()
			env["NOTIFY_SOCKET"] = addr
			p := baseProcess(env, source, ln)
			ctx, cancel := context.WithCancel(t.Context())
			done := make(chan int, 1)
			go func() { done <- Run(ctx, p) }()
			if err := notifyLn.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
				t.Fatal(err)
			}
			buf := make([]byte, 32)
			n, _, err := notifyLn.ReadFromUnix(buf)
			if err != nil {
				cancel()
				t.Fatal(err)
			}
			if string(buf[:n]) != "READY=1" {
				t.Fatalf("notify %q", buf[:n])
			}
			if err := notifyLn.SetReadDeadline(time.Now().Add(20 * time.Millisecond)); err != nil {
				t.Fatal(err)
			}
			if n, _, err := notifyLn.ReadFromUnix(buf); err == nil {
				t.Fatalf("extra readiness datagram %q", buf[:n])
			} else {
				var timeout net.Error
				if !errors.As(err, &timeout) || !timeout.Timeout() {
					t.Fatalf("second datagram read: %v", err)
				}
			}
			cancel()
			if code := <-done; code != 0 {
				t.Fatalf("code=%d diag=%q", code, p.Stderr)
			}
			if p.Stdout.(*bytes.Buffer).Len() != 0 || p.Stderr.(*countWriter).Len() != 0 {
				t.Fatalf("streams %q %q", p.Stdout, p.Stderr)
			}
			if _, err := os.Stat(source); err != nil {
				t.Fatalf("store not opened: %v", err)
			}
		})
	}
}

func TestRunWiresIssuerAndRandomness(t *testing.T) {
	// R-A6NL-V77Q R-NKXZ-SECA: serve a request through Run's inherited listener.
	var issuer *httptest.Server
	issuer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/openid-configuration" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer": issuer.URL, "authorization_endpoint": issuer.URL + "/authorize",
			"token_endpoint": issuer.URL + "/token", "jwks_uri": issuer.URL + "/jwks",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	}))
	defer issuer.Close()
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	dir, err := os.MkdirTemp("", "auth-notify-")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	addr := filepath.Join(dir, "notify.sock")
	ready, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: addr, Net: "unixgram"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ready.Close() }()
	env := goodEnv()
	env["NOTIFY_SOCKET"] = addr
	p := baseProcess(env, testSource(t), ln)
	p.OIDCIssuer = issuer.URL
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan int, 1)
	go func() { done <- Run(ctx, p) }()
	if err := ready.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 32)
	if _, _, err := ready.ReadFromUnix(buf); err != nil {
		t.Fatal(err)
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	reqCtx, reqCancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer reqCancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, "http://"+ln.Addr().String()+"/login/google", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	loc, err := url.Parse(resp.Header.Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	verifier := idcodec.Encode(bytes.Repeat([]byte{0x42}, 32))
	sum := sha256.Sum256([]byte(verifier))
	if resp.StatusCode != http.StatusFound || loc.Scheme+"://"+loc.Host+loc.Path != issuer.URL+"/authorize" || loc.Query().Get("state") != idcodec.Encode(bytes.Repeat([]byte{0x42}, 16)) || loc.Query().Get("code_challenge") != base64.RawURLEncoding.EncodeToString(sum[:]) {
		t.Fatalf("status=%d location=%s", resp.StatusCode, loc)
	}
	cancel()
	if code := <-done; code != 0 {
		t.Fatalf("Run=%d diagnostic=%q", code, p.Stderr)
	}
}

func TestNotificationFailureAndServeFailure(t *testing.T) {
	// R-N53A-TDP9 R-N8QZ-YOXC
	env := goodEnv()
	env["NOTIFY_SOCKET"] = filepath.Join(t.TempDir(), "absent.sock")
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := baseProcess(env, testSource(t), ln)
	_, dialErr := net.DialUnix("unixgram", nil, &net.UnixAddr{Name: env["NOTIFY_SOCKET"], Net: "unixgram"})
	if dialErr == nil {
		t.Fatal("missing notification socket unexpectedly accepted a datagram")
	}
	if code := Run(t.Context(), p); code != 1 || p.Stderr.(*countWriter).String() != "auth: "+dialErr.Error()+"\n" || p.Stderr.(*countWriter).calls != 1 {
		t.Fatalf("code=%d diag=%q", code, p.Stderr)
	}
	// A listener with a failing Accept makes Serve report its error.
	delete(env, "NOTIFY_SOCKET")
	p = baseProcess(env, testSource(t), &brokenListener{err: errors.New("accept failed")})
	if code := Run(t.Context(), p); code != 1 || p.Stderr.(*countWriter).String() != "auth: accept failed\n" {
		t.Fatalf("code=%d diag=%q", code, p.Stderr)
	}
}

func TestDrainDuration(t *testing.T) {
	// R-MP8L-UD28: Run uses this conversion after the validation exercised above.
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	source, err := os.ReadFile(filepath.Join(filepath.Dir(thisFile), "cli.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{"drain := 5 * time.Second", "drain = drainDuration(v)", "server.Serve(ctx, ln, h, drain)"} {
		if !bytes.Contains(source, []byte(fragment)) {
			t.Fatalf("Run does not use %q", fragment)
		}
	}
	if got := drainDuration("1"); got != time.Second {
		t.Fatal(got)
	}
	maxSeconds := int64(^uint64(0)>>1) / int64(time.Second)
	if got := drainDuration(strconv.FormatInt(maxSeconds, 10)); got != time.Duration(maxSeconds)*time.Second {
		t.Fatal(got)
	}
	for _, value := range []string{strconv.FormatInt(maxSeconds+1, 10), strings.Repeat("9", 1000)} {
		if got := drainDuration(value); got != time.Duration(1<<63-1) {
			t.Fatalf("%q = %v", value, got)
		}
	}
}

func TestRunUsesConfiguredDrainDeadline(t *testing.T) {
	// R-MP8L-UD28: hold a callback inside Google so Run must use Serve's drain.
	for _, tc := range []struct {
		name, setting string
		want          time.Duration
	}{
		{"configured", "1", time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			arrived := make(chan struct{}, 1)
			release := make(chan struct{})
			var issuer *httptest.Server
			issuer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/.well-known/openid-configuration":
					w.Header().Set("Content-Type", "application/json")
					_ = json.NewEncoder(w).Encode(map[string]any{"issuer": issuer.URL, "authorization_endpoint": issuer.URL + "/authorize", "token_endpoint": issuer.URL + "/token", "jwks_uri": issuer.URL + "/jwks", "id_token_signing_alg_values_supported": []string{"RS256"}})
				case "/token":
					arrived <- struct{}{}
					<-release
				default:
					http.NotFound(w, r)
				}
			}))
			defer issuer.Close()
			defer close(release)
			ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			dir, err := os.MkdirTemp("", "auth-notify-")
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = os.RemoveAll(dir) }()
			addr := filepath.Join(dir, "notify.sock")
			ready, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: addr, Net: "unixgram"})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = ready.Close() }()
			env := goodEnv()
			env["NOTIFY_SOCKET"] = addr
			if tc.setting != "" {
				env["DRAIN_SECONDS"] = tc.setting
			}
			p := baseProcess(env, testSource(t), ln)
			output := new(safeRecordWriter)
			p.Stderr = output
			p.OIDCIssuer = issuer.URL
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			done := make(chan int, 1)
			go func() { done <- Run(ctx, p) }()
			if err := ready.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
				t.Fatal(err)
			}
			buf := make([]byte, 32)
			if _, _, err := ready.ReadFromUnix(buf); err != nil {
				t.Fatal(err)
			}
			client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
			loginCtx, loginCancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer loginCancel()
			login, err := http.NewRequestWithContext(loginCtx, http.MethodGet, "http://"+ln.Addr().String()+"/login/google", nil)
			if err != nil {
				t.Fatal(err)
			}
			response, err := client.Do(login)
			if err != nil {
				t.Fatal(err)
			}
			_ = response.Body.Close()
			if response.StatusCode != http.StatusFound {
				t.Fatalf("login status=%d", response.StatusCode)
			}
			loc, err := url.Parse(response.Header.Get("Location"))
			if err != nil {
				t.Fatal(err)
			}
			callbackCtx, callbackCancel := context.WithTimeout(t.Context(), tc.want+5*time.Second)
			defer callbackCancel()
			callback, err := http.NewRequestWithContext(callbackCtx, http.MethodGet, "http://"+ln.Addr().String()+"/login/google/callback?state="+url.QueryEscape(loc.Query().Get("state"))+"&code=held", nil)
			if err != nil {
				t.Fatal(err)
			}
			callbackDone := make(chan struct{})
			go func() {
				resp, err := client.Do(callback)
				if err == nil {
					_ = resp.Body.Close()
				}
				close(callbackDone)
			}()
			select {
			case <-arrived:
			case <-time.After(5 * time.Second):
				t.Fatal("callback did not reach token endpoint")
			}
			started := time.Now()
			cancel()
			select {
			case code := <-done:
				t.Fatalf("Run returned early: %d", code)
			case <-time.After(tc.want - 100*time.Millisecond):
			}
			select {
			case code := <-done:
				if code != 1 {
					t.Fatalf("Run=%d", code)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("Run missed drain deadline")
			}
			if elapsed := time.Since(started); elapsed < tc.want-100*time.Millisecond || elapsed > tc.want+2*time.Second {
				t.Fatalf("drain elapsed %s, want %s", elapsed, tc.want)
			}
			if got := output.String(); !strings.HasPrefix(got, "auth: stopped with 1 request unfinished\n") {
				t.Fatalf("diagnostic %q", got)
			}
			// The held issuer is released by the deferred close after Run has returned.
			_ = callbackDone
		})
	}
}

func TestEmptyNotificationAndHugeDrainServe(t *testing.T) {
	// R-N3VE-FLYK R-MMST-2TKU R-MP8L-UD28
	for _, drain := range []string{"1", strings.Repeat("9", 100)} {
		t.Run(drain[:1], func(t *testing.T) {
			env := goodEnv()
			env["NOTIFY_SOCKET"] = ""
			env["DRAIN_SECONDS"] = drain
			ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			p := baseProcess(env, testSource(t), ln)
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			if code := Run(ctx, p); code != 0 || p.Stderr.(*countWriter).Len() != 0 {
				t.Fatalf("code=%d diagnostic=%q", code, p.Stderr)
			}
		})
	}
}

type overlapWriter struct {
	active     atomic.Int32
	calls      atomic.Int32
	overlapped atomic.Bool
}

type synchronizedRand struct {
	mu   sync.Mutex
	next byte
}

func (r *synchronizedRand) Read(b []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.next++
	for i := range b {
		b[i] = r.next
	}
	return len(b), nil
}

func (w *overlapWriter) Write(b []byte) (int, error) {
	if w.active.Add(1) != 1 {
		w.overlapped.Store(true)
	}
	w.calls.Add(1)
	for range 100 {
		runtime.Gosched()
	}
	w.active.Add(-1)
	return len(b), nil
}

func TestDiagnosticWriterSerializesCalls(t *testing.T) {
	// R-H2IE-TQD6: concurrent HTTP failures exercise the writer passed by Run.
	underlying := new(overlapWriter)
	issuer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusServiceUnavailable) }))
	defer issuer.Close()
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()
	dir, err := os.MkdirTemp("", "auth-notify-")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	addr := filepath.Join(dir, "notify.sock")
	ready, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: addr, Net: "unixgram"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ready.Close() }()
	env := goodEnv()
	env["NOTIFY_SOCKET"] = addr
	p := baseProcess(env, testSource(t), ln)
	p.OIDCIssuer = issuer.URL
	p.Stderr = underlying
	p.Rand = &synchronizedRand{}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan int, 1)
	go func() { done <- Run(ctx, p) }()
	if err := ready.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 32)
	if _, _, err := ready.ReadFromUnix(buf); err != nil {
		t.Fatal(err)
	}
	const count = 30
	var wg sync.WaitGroup
	wg.Add(count)
	problems := make(chan error, count)
	for range count {
		go func() {
			defer wg.Done()
			reqCtx, reqCancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer reqCancel()
			req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, "http://"+ln.Addr().String()+"/login/google", nil)
			if err != nil {
				problems <- err
				return
			}
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				problems <- err
				return
			}
			_ = resp.Body.Close()
			if resp.StatusCode != http.StatusBadGateway {
				problems <- fmt.Errorf("status %d", resp.StatusCode)
			}
		}()
	}
	wg.Wait()
	close(problems)
	for err := range problems {
		t.Error(err)
	}
	cancel()
	if code := <-done; code != 0 {
		t.Fatalf("Run=%d", code)
	}
	if underlying.calls.Load() != count {
		t.Fatalf("writes=%d", underlying.calls.Load())
	}
	if underlying.overlapped.Load() {
		t.Fatal("concurrent underlying writes")
	}
}

type brokenListener struct{ err error }

func (b *brokenListener) Accept() (net.Conn, error) { return nil, b.err }
func (b *brokenListener) Close() error              { return nil }
func (b *brokenListener) Addr() net.Addr            { return &net.TCPAddr{} }

func TestSourceRestrictions(t *testing.T) {
	// R-LZMP-T6HN R-M0UM-6Y8C R-3I0P-J71C R-3J8L-WYS1 R-3KGI-AQIQ R-3LOE-OI9F R-3MWB-2A04 R-3U7P-CWGA
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	// R-3FKW-RNJY
	module, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(module), "module github.com/ikigenba/ikigenba/auth\n") {
		t.Fatalf("module declaration: %s", module)
	}
	prohibited := map[string]bool{"os.Args": true, "os.Environ": true, "os.Getenv": true, "os.LookupEnv": true, "os.Setenv": true, "os.Unsetenv": true, "os.Clearenv": true, "os.Getpid": true, "os.Stdin": true, "os.Stdout": true, "os.Stderr": true, "os.Exit": true, "net.Listen": true, "net.ListenTCP": true, "net.ListenUnix": true, "net.ListenUDP": true, "net.ListenUnixgram": true, "net.ListenIP": true, "net.ListenMulticastUDP": true, "net.ListenPacket": true, "http.ListenAndServe": true, "http.ListenAndServeTLS": true, "syscall.Socket": true, "syscall.Bind": true, "syscall.Listen": true}
	checkFile := func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		underInternal := strings.HasPrefix(path, filepath.Join(root, "internal")+string(filepath.Separator))
		for _, imp := range f.Imports {
			if underInternal && imp.Path.Value == `"os/signal"` {
				t.Errorf("%s imports os/signal", path)
			}
		}
		ast.Inspect(f, func(n ast.Node) bool {
			if call, ok := n.(*ast.CallExpr); ok {
				method, ok := call.Fun.(*ast.SelectorExpr)
				if ok && (method.Sel.Name == "Listen" || method.Sel.Name == "ListenPacket" || method.Sel.Name == "ListenAndServe" || method.Sel.Name == "ListenAndServeTLS") {
					t.Errorf("forbidden listener method %s in %s", method.Sel.Name, path)
				}
			}
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			id, ok := sel.X.(*ast.Ident)
			if ok && prohibited[id.Name+"."+sel.Sel.Name] && (underInternal || id.Name != "os") {
				t.Errorf("forbidden %s.%s in %s", id.Name, sel.Sel.Name, path)
			}
			return true
		})
		return nil
	}
	for _, dir := range []string{"internal", "cmd"} {
		if err := filepath.WalkDir(filepath.Join(root, dir), checkFile); err != nil {
			t.Fatal(err)
		}
	}
}
