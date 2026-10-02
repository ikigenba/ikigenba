package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/auth/internal/google"
	"github.com/ikigenba/ikigenba/auth/internal/idcodec"
	"github.com/ikigenba/ikigenba/auth/internal/store"
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

const wantSocketHint = "\n\nrun it under systemd, with a listening socket passed in\n"

func TestSurface(t *testing.T) {
	// R-4WVH-WX70
	// An unkeyed literal fixes the field set, order, and types at compile time;
	// reading each field back into a variable of its declared type fixes them exactly.
	var (
		args      []string
		lookupEnv func(string) (string, bool)
		unsetenv  func(string) error
		pid       int
		stdout    io.Writer
		stderr    io.Writer
		inherit   func(uintptr) (net.Listener, error)
		now       func() time.Time
		rnd       io.Reader
		issuer    string
		dbSource  string
		banner    func(page.User) page.Banner
	)
	p := Process{args, lookupEnv, unsetenv, pid, stdout, stderr, inherit, now, rnd, issuer, dbSource, banner}
	args, lookupEnv, unsetenv, pid = p.Args, p.LookupEnv, p.Unsetenv, p.Pid
	stdout, stderr, inherit, now = p.Stdout, p.Stderr, p.Inherit, p.Now
	rnd, issuer, dbSource, banner = p.Rand, p.OIDCIssuer, p.DBSource, p.Banner
	_, _, _, _, _, _, _, _, _, _, _, _ = args, lookupEnv, unsetenv, pid, stdout, stderr, inherit, now, rnd, issuer, dbSource, banner
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
	}, Now: func() time.Time { return time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC) }, Rand: bytes.NewReader(bytes.Repeat([]byte{0x42}, 4096)), OIDCIssuer: "http://127.0.0.1:0", DBSource: source, Banner: func(u page.User) page.Banner {
		return page.Banner{Service: "auth", Email: u.Email, ProfileURL: u.ProfileURL, LogoutURL: u.LogoutURL}
	}}
}
func goodEnv() map[string]string {
	return map[string]string{"GOOGLE_CLIENT_ID": "id", "GOOGLE_CLIENT_SECRET": "secret", "WORKSPACE_DOMAIN": "example.test", "LISTEN_PID": "42", "LISTEN_FDS": "1"}
}
func testSource(t *testing.T) string { return filepath.Join(t.TempDir(), "auth.db") }

func TestConfigAndSocketValidation(t *testing.T) {
	// R-GESR-IE7R R-MLKW-P1U5 R-MO0P-GLBJ R-MMST-2TKU R-MQGI-84SX R-GIGG-NPFU R-GJOD-1H6J
	dir, err := os.MkdirTemp("", "auth-validation-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	notifyPath := filepath.Join(dir, "notify.sock")
	notifications, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: notifyPath, Net: "unixgram"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = notifications.Close() }()
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
			env["NOTIFY_SOCKET"] = notifyPath
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
			assertNoNotification(t, notifications)
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
	// R-GM45-T0NX R-MWK0-4ZIE R-MXRW-IR93 R-MYZS-WIZS
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
	slices.Sort(unset)
	if !slices.Equal(unset, []string{"LISTEN_FDNAMES", "LISTEN_FDS", "LISTEN_PID"}) {
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

type trackedListener struct {
	net.Listener
	closed atomic.Bool
}

func (ln *trackedListener) Close() error {
	ln.closed.Store(true)
	return ln.Listener.Close()
}

func TestRunTakesInjectedListener(t *testing.T) {
	// R-GNC2-6SEM R-FO7L-15H1
	for _, network := range []string{"tcp", "unix"} {
		for _, drain := range []string{"unset", "", "1"} {
			t.Run(network+"/"+drain, func(t *testing.T) {
				dir, err := os.MkdirTemp("", "auth-inherit-")
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.RemoveAll(dir) })
				address := "127.0.0.1:0"
				if network == "unix" {
					address = filepath.Join(dir, "serve.sock")
				}
				listener, err := (&net.ListenConfig{}).Listen(t.Context(), network, address)
				if err != nil {
					t.Fatal(err)
				}
				ln := &trackedListener{Listener: listener}
				t.Cleanup(func() { _ = listener.Close() })
				readyPath := filepath.Join(dir, "ready.sock")
				ready, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: readyPath, Net: "unixgram"})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = ready.Close() })
				env := goodEnv()
				env["NOTIFY_SOCKET"] = readyPath
				if drain != "unset" {
					env["DRAIN_SECONDS"] = drain
				}
				p := baseProcess(env, testSource(t), ln)
				var descriptors []uintptr
				p.Inherit = func(fd uintptr) (net.Listener, error) {
					descriptors = append(descriptors, fd)
					return ln, nil
				}
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				done := make(chan int, 1)
				go func() { done <- Run(ctx, p) }()
				if err := ready.SetReadDeadline(time.Now().Add(5 * time.Second)); err != nil {
					t.Fatal(err)
				}
				if _, _, err := ready.ReadFromUnix(make([]byte, 32)); err != nil {
					t.Fatal(err)
				}
				transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					return (&net.Dialer{}).DialContext(ctx, network, listener.Addr().String())
				}}
				defer transport.CloseIdleConnections()
				client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
				req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://auth/unknown", nil)
				if err != nil {
					t.Fatal(err)
				}
				response, err := client.Do(req)
				if err != nil {
					t.Fatalf("inherited listener did not answer: %v", err)
				}
				_ = response.Body.Close()
				cancel()
				<-done
				if !slices.Equal(descriptors, []uintptr{3}) {
					t.Fatalf("inherited descriptors = %v, want [3]", descriptors)
				}
				if !ln.closed.Load() {
					t.Fatal("Run returned without closing its listener")
				}
			})
		}
	}
}

func TestRunClosesListenerOnLaterFailure(t *testing.T) {
	// R-FO7L-15H1
	for _, failure := range []string{"store", "notify", "serve"} {
		t.Run(failure, func(t *testing.T) {
			env := goodEnv()
			source := testSource(t)
			var listener net.Listener
			if failure == "serve" {
				listener = &brokenListener{err: errors.New("accept failed")}
			} else {
				var err error
				listener, err = (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = listener.Close() })
			}
			if failure == "store" {
				obstruction := filepath.Join(t.TempDir(), "parent-file")
				if err := os.WriteFile(obstruction, []byte("not a directory"), 0o600); err != nil {
					t.Fatal(err)
				}
				source = filepath.Join(obstruction, "auth.db")
			}
			if failure == "notify" {
				env["NOTIFY_SOCKET"] = filepath.Join(t.TempDir(), "missing.sock")
			}
			ln := &trackedListener{Listener: listener}
			p := baseProcess(env, source, ln)
			Run(t.Context(), p)
			if !ln.closed.Load() {
				t.Fatal("Run returned without closing its listener")
			}
		})
	}
}

func TestServeReadinessAndInjectedSeam(t *testing.T) {
	// R-T07Q-JE4P R-NII7-0UUW R-SRCY-E4WP R-N1FL-O2H6 R-N3VE-FLYK R-N6B7-75FY R-T1FM-X5VE R-OYUS-DBTQ
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
			// R-T4RU-LM2C: no request may consult the banner source.
			p.Banner = func(page.User) page.Banner { t.Error("banner called without request"); return page.Banner{} }
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
	// R-SRCY-E4WP R-T07Q-JE4P: serve a request through Run's inherited listener.
	var issuerCalls atomic.Int32
	credentials := make(chan [2]string, 1)
	var issuer *httptest.Server
	issuer = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		issuerCalls.Add(1)
		if r.URL.Path == "/token" {
			if err := r.ParseForm(); err != nil {
				t.Error(err)
			}
			id, secret, ok := r.BasicAuth()
			if !ok {
				id, secret = r.Form.Get("client_id"), r.Form.Get("client_secret")
			}
			credentials <- [2]string{id, secret}
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"access_token":"unused","token_type":"Bearer"}`)
			return
		}
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
	source := testSource(t)
	p := baseProcess(env, source, ln)
	st, err := store.Open(source, &synchronizedRand{})
	if err != nil {
		t.Fatal(err)
	}
	u, err := st.UpsertUserOnLogin("issuer", "subject", "user@example.test", p.Now())
	if err != nil {
		t.Fatal(err)
	}
	session, err := st.CreateSession(u.ID, p.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Close(); err != nil {
		t.Fatal(err)
	}
	bannerCalls := 0
	p.Banner = func(u page.User) page.Banner {
		bannerCalls++
		return page.Banner{Service: "injected-banner", Email: u.Email, ProfileURL: u.ProfileURL, LogoutURL: u.LogoutURL}
	}
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
	if issuerCalls.Load() != 0 {
		t.Fatal("issuer contacted before a request")
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
	if resp.StatusCode != http.StatusFound || loc.Scheme+"://"+loc.Host+loc.Path != issuer.URL+"/authorize" || loc.Query().Get("client_id") != "id" || loc.Query().Get("hd") != "example.test" || loc.Query().Get("state") != idcodec.Encode(bytes.Repeat([]byte{0x42}, 16)) || loc.Query().Get("code_challenge") != base64.RawURLEncoding.EncodeToString(sum[:]) {
		t.Fatalf("status=%d location=%s", resp.StatusCode, loc)
	}
	callback, err := http.NewRequestWithContext(reqCtx, http.MethodGet, "http://"+ln.Addr().String()+"/login/google/callback?code=test&state="+url.QueryEscape(loc.Query().Get("state")), nil)
	if err != nil {
		t.Fatal(err)
	}
	callback.Header.Set("X-Request-Id", "wiring")
	failure, err := client.Do(callback)
	if err != nil {
		t.Fatal(err)
	}
	_ = failure.Body.Close()
	if failure.StatusCode != http.StatusBadGateway {
		t.Fatalf("callback status=%d", failure.StatusCode)
	}
	if got := <-credentials; got != [2]string{"id", "secret"} {
		t.Fatalf("client credentials=%q", got)
	}
	// The client supplies the reason; its wording is not part of Run's contract.
	_, exchangeErr := google.NewClient("id", "secret", "example.test", issuer.URL).Exchange(reqCtx, "test", verifier, loc.Query().Get("redirect_uri"))
	if exchangeErr == nil {
		t.Fatal("fake token response unexpectedly supplied an ID token")
	}
	profile, err := http.NewRequestWithContext(reqCtx, http.MethodGet, "http://"+ln.Addr().String()+"/", nil)
	if err != nil {
		t.Fatal(err)
	}
	profile.AddCookie(&http.Cookie{Name: "ikigenba_session", Value: session.ID, Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode})
	page, err := client.Do(profile)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(page.Body)
	_ = page.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if code := <-done; code != 0 {
		t.Fatalf("Run=%d diagnostic=%q", code, p.Stderr)
	}
	// R-T1FM-X5VE: the server's one diagnostic is forwarded once, unaltered.
	if got := p.Stderr.(*countWriter); got.String() != "auth: request wiring: "+exchangeErr.Error()+"\n" || got.calls != 1 || p.Stdout.(*bytes.Buffer).Len() != 0 {
		t.Fatalf("stdout=%q stderr=%q calls=%d", p.Stdout, got.String(), got.calls)
	}
	if bannerCalls == 0 || !bytes.Contains(body, []byte("injected-banner")) {
		t.Fatalf("banner calls=%d page=%s", bannerCalls, body)
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
	// R-MP8L-UD28
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
	// R-T2NJ-AXM3: concurrent HTTP failures exercise the writer passed by Run.
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

func TestSocketUsageErrorsHaveNoServingSideEffects(t *testing.T) {
	// R-GIGG-NPFU R-GJOD-1H6J
	dir, err := os.MkdirTemp("", "auth-notify-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	addr := filepath.Join(dir, "notify.sock")
	notifications, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: addr, Net: "unixgram"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = notifications.Close() }()
	for _, fds := range []string{"0", "002", strings.Repeat("9", 100)} {
		for _, drain := range []string{"unset", "", "1", "9999999999999999999999999"} {
			t.Run(fds+"/"+drain, func(t *testing.T) {
				env := goodEnv()
				env["LISTEN_FDS"] = fds
				env["NOTIFY_SOCKET"] = addr
				if drain != "unset" {
					env["DRAIN_SECONDS"] = drain
				}
				p := baseProcess(env, testSource(t), nil)
				p.Inherit = func(uintptr) (net.Listener, error) {
					t.Fatal("attempted listener inheritance")
					return nil, errors.New("unexpected inheritance")
				}
				p.Unsetenv = func(string) error { t.Fatal("removed environment variable"); return nil }
				want := "auth: no socket was passed in" + wantSocketHint
				if fds != "0" {
					want = "auth: " + fds + " sockets were passed in, expected 1" + wantSocketHint
				}
				if code := Run(t.Context(), p); code != 2 || p.Stdout.(*bytes.Buffer).Len() != 0 || p.Stderr.(*countWriter).String() != want {
					t.Fatalf("code=%d stdout=%q stderr=%q", code, p.Stdout, p.Stderr)
				}
				if _, err := os.Stat(p.DBSource); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("database opened: %v", err)
				}
				assertNoNotification(t, notifications)
			})
		}
	}
}

func assertNoNotification(t *testing.T, notifications *net.UnixConn) {
	t.Helper()
	// Run has returned synchronously: inspect the datagram queue without waiting.
	raw, err := notifications.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	var receiveErr error
	if err := raw.Read(func(fd uintptr) bool {
		_, _, receiveErr = syscall.Recvfrom(int(fd), make([]byte, 32), syscall.MSG_DONTWAIT)
		return true
	}); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(receiveErr, syscall.EAGAIN) {
		t.Fatalf("notification queue was not empty: %v", receiveErr)
	}
}
