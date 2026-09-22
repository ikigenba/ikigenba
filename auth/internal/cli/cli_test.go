package cli

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"html"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/auth/internal/idcodec"
	"github.com/ikigenba/ikigenba/auth/internal/server/assets"
	"github.com/ikigenba/ikigenba/auth/internal/store"
	"github.com/ikigenba/ikigenba/auth/internal/version"
)

func TestProcessAndRunSignatures(t *testing.T) {
	// R-3QK0-7L87
	got := reflect.TypeOf(Process{})
	want := []struct {
		name string
		typ  reflect.Type
	}{
		{"Args", reflect.TypeFor[[]string]()},
		{"Getenv", reflect.TypeFor[func(string) string]()},
		{"Stdout", reflect.TypeFor[io.Writer]()},
		{"Stderr", reflect.TypeFor[io.Writer]()},
		{"Now", reflect.TypeFor[func() time.Time]()},
		{"Rand", reflect.TypeFor[io.Reader]()},
		{"OIDCIssuer", reflect.TypeFor[string]()},
		{"DBSource", reflect.TypeFor[string]()},
	}
	if got.NumField() != len(want) {
		t.Fatalf("Process has %d fields, want %d", got.NumField(), len(want))
	}
	for i, field := range want {
		f := got.Field(i)
		if f.Name != field.name || f.Type != field.typ {
			t.Fatalf("field %d = %s %s, want %s %s", i, f.Name, f.Type, field.name, field.typ)
		}
	}

	// R-3RRW-LCYW
	code := Run(Process{Args: []string{"--version"}, Stdout: io.Discard, Stderr: io.Discard})
	if code != 0 {
		t.Fatalf("Run(Process) int = %d, want 0", code)
	}
}

func TestVersionHelpAndManifest(t *testing.T) {
	// R-P02O-R3KF
	stdout, stderr, code := run(t, Process{Args: []string{"--version"}})
	if code != 0 || stderr != "" || stdout != version.Version+"\n" {
		t.Fatalf("--version code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}

	// R-P1AL-4VB4
	stdout, stderr, code = run(t, Process{Args: []string{"--help"}})
	if code != 0 || stderr != "" || stdout != usageText {
		t.Fatalf("--help code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	// R-OWEZ-LSCC: the bytes are the design's usage text, not the writer constant.
	if stdout != designUsageText || !strings.HasSuffix(designUsageText, "\n") {
		t.Fatalf("--help stdout is not the design usage text:\n%s", stdout)
	}

	// R-P2IH-IN1T
	stdout, stderr, code = run(t, Process{Args: []string{"manifest"}})
	if code != 0 || stderr != "" || stdout != manifestText {
		t.Fatalf("manifest code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	// R-OXMV-ZK31: the bytes are the design's manifest, not the writer constant.
	if stdout != designManifestText || !strings.HasSuffix(designManifestText, "\n") {
		t.Fatalf("manifest stdout is not the design manifest:\n%s", stdout)
	}

	// R-P4YA-A6J7
	manifestPath := filepath.Join(moduleRoot(t), "etc", "manifest.toml")
	file, err := os.ReadFile(filepath.Clean(manifestPath))
	if err != nil {
		t.Fatalf("read etc/manifest.toml: %v", err)
	}
	if string(file) != stdout {
		t.Fatalf("etc/manifest.toml is not byte-identical to auth manifest\nfile=%q\nstdout=%q", file, stdout)
	}
}

func TestUnknownCommandAndOption(t *testing.T) {
	// R-P666-NY9W
	// R-P8LZ-FHRA
	stdout, stderr, code := run(t, Process{Args: []string{"bogus"}})
	want := "auth: unknown command 'bogus'\n\nsee 'auth --help' for usage\n"
	if code != 2 || stdout != "" || stderr != want {
		t.Fatalf("bogus code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if strings.Contains(stderr, "Usage:") || strings.Contains(stdout, "Usage:") {
		t.Fatal("usage error wrote the usage text")
	}

	// R-P7E3-1Q0L
	stdout, stderr, code = run(t, Process{Args: []string{"--bogus"}})
	want = "auth: unknown option '--bogus'\n\nsee 'auth --help' for usage\n"
	if code != 2 || stdout != "" || stderr != want {
		t.Fatalf("--bogus code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}

	for _, args := range [][]string{{"manifest", "extra"}, {"--help", "extra"}, {"--version", "--help"}, {"help"}} {
		stdout, stderr, code = run(t, Process{Args: args})
		if code != 2 || stdout != "" || !strings.HasPrefix(stderr, "auth: ") || strings.Contains(stderr, "Usage:") {
			t.Fatalf("args %v code=%d stdout=%q stderr=%q", args, code, stdout, stderr)
		}
	}
}

func TestConfigFaultsOpenNothing(t *testing.T) {
	// R-IGZ2-JTSP
	dir := t.TempDir()
	source := filepath.Join(dir, "auth.db")

	var looked []string
	before := listenSockets(t)
	stdout, stderr, code := run(t, Process{
		Getenv: func(name string) string {
			looked = append(looked, name)
			return ""
		},
		DBSource: source,
		Rand:     bytes.NewReader(nil),
	})
	if code != 2 || stdout != "" || stderr != "auth: PORT is not set\n" {
		t.Fatalf("unset PORT code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if !reflect.DeepEqual(looked, []string{"PORT"}) {
		t.Fatalf("Getenv calls = %v, want only PORT", looked)
	}
	assertDidNotOpenOrListen(t, before, source)

	// An empty PORT is the same Getenv result as an unset one, and it still
	// stops before the Google settings.
	looked = nil
	before = listenSockets(t)
	stdout, stderr, code = run(t, Process{
		Getenv: func(name string) string {
			looked = append(looked, name)
			if name == "PORT" {
				return ""
			}
			return "set"
		},
		DBSource: source,
	})
	if code != 2 || stdout != "" || stderr != "auth: PORT is not set\n" || !reflect.DeepEqual(looked, []string{"PORT"}) {
		t.Fatalf("empty PORT code=%d stdout=%q stderr=%q looked=%v", code, stdout, stderr, looked)
	}
	assertDidNotOpenOrListen(t, before, source)

	// R-II6Y-XLJE
	for _, value := range []string{"0", "65536", "-1", "80a", " 80", "1.5", "0x10"} {
		before = listenSockets(t)
		stdout, stderr, code = run(t, Process{
			Getenv:   mapGetenv(map[string]string{"PORT": value, "GOOGLE_CLIENT_ID": "id"}),
			DBSource: source,
		})
		want := fmt.Sprintf("auth: PORT is '%s', not a port number\n", value)
		if code != 2 || stdout != "" || stderr != want {
			t.Fatalf("PORT %q code=%d stdout=%q stderr=%q", value, code, stdout, stderr)
		}
		assertDidNotOpenOrListen(t, before, source)
	}

	// R-IJEV-BDA3
	cases := []struct {
		env    map[string]string
		name   string
		looked []string
	}{
		{map[string]string{"PORT": "1"}, "GOOGLE_CLIENT_ID", []string{"PORT", "GOOGLE_CLIENT_ID"}},
		{map[string]string{"PORT": "65535", "GOOGLE_CLIENT_ID": "id"}, "GOOGLE_CLIENT_SECRET", []string{"PORT", "GOOGLE_CLIENT_ID", "GOOGLE_CLIENT_SECRET"}},
		{map[string]string{"PORT": "80", "GOOGLE_CLIENT_ID": "id", "GOOGLE_CLIENT_SECRET": "secret"}, "WORKSPACE_DOMAIN", []string{"PORT", "GOOGLE_CLIENT_ID", "GOOGLE_CLIENT_SECRET", "WORKSPACE_DOMAIN"}},
		{map[string]string{"PORT": "80", "GOOGLE_CLIENT_SECRET": "secret", "WORKSPACE_DOMAIN": "example.test"}, "GOOGLE_CLIENT_ID", []string{"PORT", "GOOGLE_CLIENT_ID"}},
		{map[string]string{"PORT": "80", "GOOGLE_CLIENT_ID": "", "GOOGLE_CLIENT_SECRET": "secret", "WORKSPACE_DOMAIN": "example.test"}, "GOOGLE_CLIENT_ID", []string{"PORT", "GOOGLE_CLIENT_ID"}},
	}
	for _, tc := range cases {
		looked = nil
		before = listenSockets(t)
		stdout, stderr, code = run(t, Process{
			Getenv: func(name string) string {
				looked = append(looked, name)
				return tc.env[name]
			},
			DBSource: source,
		})
		want := "auth: " + tc.name + " is not set\n"
		if code != 2 || stdout != "" || stderr != want {
			t.Fatalf("missing %s code=%d stdout=%q stderr=%q", tc.name, code, stdout, stderr)
		}
		if !reflect.DeepEqual(looked, tc.looked) {
			t.Fatalf("missing %s Getenv calls = %v, want %v", tc.name, looked, tc.looked)
		}
		assertDidNotOpenOrListen(t, before, source)
	}
}

func TestOpenFailureDoesNotListen(t *testing.T) {
	// R-IN2K-GOI6
	source := rejectedDB(t)
	_, openErr := store.Open(source, bytes.NewReader(nil))
	if openErr == nil {
		t.Fatal("store.Open succeeded on a non-database file")
	}
	held := hold(t)
	port := portOf(t, held)
	before := listenSockets(t)
	stdout, stderr, code := run(t, serveProcess(t, port, source))
	want := fmt.Sprintf("auth: cannot open database %s: %s\n", source, openErr.Error())
	if code != 1 || stdout != "" || stderr != want {
		t.Fatalf("open failure code=%d stdout=%q stderr=%q want stderr %q", code, stdout, stderr, want)
	}
	if !stillHolds(t, held, port) {
		t.Fatal("open failure disturbed the process holding the port")
	}
	if added := addedListens(before, listenSockets(t)); len(added) != 0 {
		t.Fatalf("open failure listened on %v", added)
	}
}

func TestStoreOpenBeforeListen(t *testing.T) {
	// R-4KCJ-48RH
	// Ordering is source order, not a race against store.Open: Serve is reached only after Open returns.
	assertOpenCallPrecedesServe(t)

	port := freePort(t)
	source := rejectedDB(t)
	addr := net.JoinHostPort("127.0.0.1", port)
	var accepts atomic.Int32
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			conn, err := (&net.Dialer{}).DialContext(t.Context(), "tcp", addr)
			if err != nil {
				continue
			}
			accepts.Add(1)
			_ = conn.Close()
		}
	}()
	defer func() {
		select {
		case <-stop:
		default:
			close(stop)
		}
		wg.Wait()
	}()

	stdout, stderr, code := run(t, serveProcess(t, port, source))
	close(stop)
	wg.Wait()
	wantPrefix := "auth: cannot open database " + source + ": "
	if code != 1 || stdout != "" || !strings.HasPrefix(stderr, wantPrefix) || strings.Count(stderr, "\n") != 1 {
		t.Fatalf("rejected source code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if accepts.Load() != 0 || canDial(t, addr) {
		t.Fatalf("rejected source accepted %d connections on %s", accepts.Load(), addr)
	}

	got := serveLogin(t)
	if !got.dbReadyAtAccept {
		t.Fatal("listener accepted a connection before store.Open finished")
	}
	if got.status != http.StatusFound || got.location == nil {
		t.Fatalf("opened store was not served: status %d", got.status)
	}
	state := got.location.Query().Get("state")
	verifier, ok := verifierFromRand(got.consumed, state, got.location.Query().Get("code_challenge"))
	if !ok {
		t.Fatalf("served login is not derived from Process.Rand (consumed %d bytes, query %s)", len(got.consumed), got.location.RawQuery)
	}
	opened, err := store.Open(got.source, bytes.NewReader(nil))
	if err != nil {
		t.Fatalf("reopen %s: %v", got.source, err)
	}
	t.Cleanup(func() { _ = opened.Close() })
	recorded, err := opened.ConsumeLoginState(state)
	if err != nil || recorded.State != state || recorded.Verifier != verifier {
		t.Fatalf("opened store login state = %#v, %v; want state %s verifier %s", recorded, err, state, verifier)
	}
}

func TestServedLoginUsesInjectedIssuerAndRand(t *testing.T) {
	// R-4O08-9JZK
	got := serveLogin(t)
	if got.hitsBefore != 0 {
		t.Fatalf("discovery hits before listen = %d, want 0", got.hitsBefore)
	}
	if got.status != http.StatusFound || got.location == nil {
		t.Fatalf("GET /login/google = %d, want 302", got.status)
	}
	endpoint, err := url.Parse(got.authEndpoint)
	if err != nil {
		t.Fatalf("authorization endpoint: %v", err)
	}
	if got.location.Scheme != endpoint.Scheme || got.location.Host != endpoint.Host || got.location.Path != endpoint.Path {
		t.Fatalf("Location = %s, want authorization endpoint %s", got.location, got.authEndpoint)
	}
	query := got.location.Query()
	if _, ok := verifierFromRand(got.consumed, query.Get("state"), query.Get("code_challenge")); !ok || query.Get("code_challenge_method") != "S256" {
		t.Fatalf("Location query = %s, not derived from Process.Rand (%d bytes)", got.location.RawQuery, len(got.consumed))
	}
	if query.Get("client_id") != got.clientID || query.Get("hd") != got.workspace {
		t.Fatalf("Location client_id=%q hd=%q, want %q %q", query.Get("client_id"), query.Get("hd"), got.clientID, got.workspace)
	}
	if got.hitsAfter < 1 {
		t.Fatal("GET /login/google did not discover Process.OIDCIssuer")
	}
	if got.exitCode != 0 || got.stdout != "" || got.stderr != "" || got.stillListening {
		t.Fatalf("shutdown code=%d stdout=%q stderr=%q listening=%v", got.exitCode, got.stdout, got.stderr, got.stillListening)
	}
}

func TestPortInUse(t *testing.T) {
	// R-IRY5-ZRGY
	held := hold(t)
	port := portOf(t, held)
	source := freshDB(t)
	stdout, stderr, code := run(t, serveProcess(t, port, source))
	want := fmt.Sprintf("auth: listen tcp 127.0.0.1:%s: bind: address already in use\n", port)
	if code != 1 || stdout != "" || stderr != want {
		t.Fatalf("in use code=%d stdout=%q stderr=%q want stderr %q", code, stdout, stderr, want)
	}
	if !stillHolds(t, held, port) {
		t.Fatal("the process holding the port was disturbed")
	}
}

func TestServeExistingAndAbsentThenShutdown(t *testing.T) {
	signal.Reset(syscall.SIGINT, syscall.SIGTERM)
	t.Cleanup(func() { signal.Reset(syscall.SIGINT, syscall.SIGTERM) })
	// R-ILUO-2WRH
	// R-KXL6-33Q7
	// R-OYUS-DBTQ
	absent := filepath.Join(t.TempDir(), "state", "auth.db")
	if err := os.Mkdir(filepath.Dir(absent), 0o700); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	assertServesUntilSignal(t, absent, false)

	existing := freshDB(t)
	if err := os.WriteFile(existing, nil, 0o600); err != nil {
		t.Fatalf("seed existing database: %v", err)
	}
	assertServesUntilSignal(t, existing, true)
}

func TestSignalsAreIdentical(t *testing.T) {
	// R-3Z3A-VZF2
	// R-IT62-DJ7N: an in-flight request, blocked inside the handler, finishes
	// with its full response after the signal. Closing the connection instead
	// of Shutdown drops that response.
	signal.Reset(syscall.SIGINT, syscall.SIGTERM)
	t.Cleanup(func() { signal.Reset(syscall.SIGINT, syscall.SIGTERM) })
	issuer, arrived, release := gatedIssuer(t)
	var codes []int
	var outs []string
	var responses []string
	for _, sig := range []os.Signal{syscall.SIGTERM, syscall.SIGINT} {
		source := freshDB(t)
		port := freePort(t)
		p := serveProcess(t, port, source)
		p.OIDCIssuer = issuer
		var stdout, stderr bytes.Buffer
		p.Stdout = &stdout
		p.Stderr = &stderr
		done := make(chan int, 1)
		go func() {
			done <- Run(p)
		}()
		mustClose(t, waitConn(t, "127.0.0.1:"+port))

		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:"+port+"/login/google", nil)
		if err != nil {
			cancel()
			t.Fatalf("request: %v", err)
		}
		client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		type gotResp struct {
			resp *http.Response
			body []byte
			err  error
		}
		respCh := make(chan gotResp, 1)
		go func() {
			resp, err := client.Do(req)
			if err != nil {
				if resp != nil && resp.Body != nil {
					if closeErr := resp.Body.Close(); closeErr != nil {
						err = errors.Join(err, closeErr)
					}
				}
				respCh <- gotResp{err: err}
				return
			}
			body, readErr := io.ReadAll(resp.Body)
			if closeErr := resp.Body.Close(); readErr == nil {
				readErr = closeErr
			}
			respCh <- gotResp{resp: resp, body: body, err: readErr}
		}()
		select {
		case <-arrived:
		case <-time.After(5 * time.Second):
			cancel()
			release()
			t.Fatalf("%s: accepted request never reached the handler", sig)
		}
		if err := syscall.Kill(os.Getpid(), sig.(syscall.Signal)); err != nil {
			cancel()
			release()
			t.Fatalf("signal %s: %v", sig, err)
		}
		release()
		var got gotResp
		select {
		case got = <-respCh:
		case <-time.After(5 * time.Second):
			cancel()
			t.Fatalf("%s: in-flight request did not receive a full response", sig)
		}
		cancel()
		if got.err != nil {
			t.Fatalf("%s response: %v", sig, got.err)
		}
		body := got.body
		loc := got.resp.Header.Get("Location")
		parsed, err := url.Parse(loc)
		if err != nil {
			t.Fatalf("%s location: %v", sig, err)
		}
		issuerURL, err := url.Parse(issuer)
		if err != nil {
			t.Fatalf("issuer: %v", err)
		}
		query := parsed.Query()
		if got.resp.StatusCode != http.StatusFound || parsed.Host != issuerURL.Host || parsed.Path != "/authorize" || query.Get("state") == "" || query.Get("code_challenge") == "" || !strings.Contains(string(body), html.EscapeString(loc)) {
			t.Fatalf("%s response status=%d location=%q body=%q", sig, got.resp.StatusCode, loc, body)
		}
		responses = append(responses, fmt.Sprintf("%d %s %s %s %s %s", got.resp.StatusCode, got.resp.Header.Get("Content-Type"), parsed.Host, parsed.Path, query.Get("state"), query.Get("code_challenge")))

		select {
		case code := <-done:
			codes = append(codes, code)
			outs = append(outs, stdout.String()+"|"+stderr.String())
			if code != 0 || stdout.Len() != 0 || stderr.Len() != 0 {
				t.Fatalf("%s code=%d stdout=%q stderr=%q", sig, code, stdout.String(), stderr.String())
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("%s did not return", sig)
		}
		if canDial(t, "127.0.0.1:"+port) {
			t.Fatalf("%s left 127.0.0.1:%s listening", sig, port)
		}
	}
	if codes[0] != codes[1] || outs[0] != outs[1] || responses[0] != responses[1] {
		t.Fatalf("signals diverged: codes %v outs %q responses %q", codes, outs, responses)
	}
}

func TestRunUsesOnlyProcess(t *testing.T) {
	// R-40B7-9R5R
	// R-IFR6-6220: a whole configuration reads exactly the four names, PORT first.
	signal.Reset(syscall.SIGINT, syscall.SIGTERM)
	t.Cleanup(func() { signal.Reset(syscall.SIGINT, syscall.SIGTERM) })
	streams := divertStandardStreams(t)
	origArgs := os.Args
	os.Args = []string{"auth", "bogus", "--help"}
	t.Cleanup(func() { os.Args = origArgs })
	t.Setenv("PORT", "9999")
	t.Setenv("GOOGLE_CLIENT_ID", "real-id")
	t.Setenv("GOOGLE_CLIENT_SECRET", "real-secret")
	t.Setenv("WORKSPACE_DOMAIN", "real.example")

	var usageOut, usageErr bytes.Buffer
	usageCode := Run(Process{
		Args:   []string{"bogus"},
		Stdout: &usageOut,
		Stderr: &usageErr,
		Getenv: func(string) string { return "should-not-be-read" },
	})
	if usageCode != 2 || usageOut.Len() != 0 || usageErr.String() != "auth: unknown command 'bogus'\n\nsee 'auth --help' for usage\n" {
		t.Fatalf("usage via Process code=%d stdout=%q stderr=%q", usageCode, usageOut.String(), usageErr.String())
	}

	fixed := time.Date(1999, 1, 2, 3, 4, 5, 0, time.UTC)
	source := freshDB(t)
	seed, err := store.Open(source, bytes.NewReader(bytes.Repeat([]byte{9}, 64)))
	if err != nil {
		t.Fatalf("seed open: %v", err)
	}
	user, err := seed.UpsertUserOnLogin("https://accounts.google.com", "subject-1", "user@injected.example", fixed.Add(-time.Minute))
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	session, err := seed.CreateSession(user.ID, fixed.Add(-time.Minute))
	if err != nil {
		t.Fatalf("seed session: %v", err)
	}
	if err := seed.Close(); err != nil {
		t.Fatalf("seed close: %v", err)
	}

	issuer, authEndpoint, hits := loopbackIssuer(t)
	raw := make([]byte, 64)
	for i := range raw {
		raw[i] = byte(i + 1)
	}
	reader := &recordingReader{data: raw}
	var gotEnv []string
	var nowCalls int
	port := freePort(t)
	const clientID = "injected-id"
	const workspace = "injected.example"
	var stdout, stderr bytes.Buffer
	p := Process{
		Args: nil,
		Getenv: func(name string) string {
			gotEnv = append(gotEnv, name)
			return map[string]string{
				"PORT":                 port,
				"GOOGLE_CLIENT_ID":     clientID,
				"GOOGLE_CLIENT_SECRET": "injected-secret",
				"WORKSPACE_DOMAIN":     workspace,
			}[name]
		},
		Stdout:     &stdout,
		Stderr:     &stderr,
		Now:        func() time.Time { nowCalls++; return fixed },
		Rand:       reader,
		OIDCIssuer: issuer,
		DBSource:   source,
	}
	done := make(chan int, 1)
	go func() { done <- Run(p) }()
	mustClose(t, waitConn(t, "127.0.0.1:"+port))
	if nowCalls != 0 {
		t.Fatal("Run read the clock before a request")
	}
	if reader.n != 0 {
		t.Fatalf("Run read %d random bytes before a request", reader.n)
	}
	if hits.Load() != 0 {
		t.Fatal("Run contacted the issuer before a request")
	}

	login := getNoRedirect(t, "http://127.0.0.1:"+port+"/login/google")
	if login.status != http.StatusFound || login.location == nil {
		t.Fatalf("GET /login/google = %d", login.status)
	}
	endpoint, err := url.Parse(authEndpoint)
	if err != nil {
		t.Fatalf("authorization endpoint: %v", err)
	}
	if login.location.Scheme != endpoint.Scheme || login.location.Host != endpoint.Host || login.location.Path != endpoint.Path {
		t.Fatalf("Location = %s, want %s", login.location, authEndpoint)
	}
	query := login.location.Query()
	if query.Get("client_id") != clientID || query.Get("hd") != workspace || query.Get("client_id") == "real-id" || query.Get("hd") == "real.example" {
		t.Fatalf("Location client_id=%q hd=%q", query.Get("client_id"), query.Get("hd"))
	}
	if _, ok := verifierFromRand(reader.consumed(), query.Get("state"), query.Get("code_challenge")); !ok {
		t.Fatalf("login state was not minted from Process.Rand (%d bytes, query %s)", len(reader.consumed()), login.location.RawQuery)
	}
	if hits.Load() < 1 {
		t.Fatal("login did not discover Process.OIDCIssuer")
	}
	callsAfterLogin := nowCalls

	check := getNoRedirect(t, "http://127.0.0.1:"+port+"/check", "ikigenba_session="+session.ID)
	if check.status != http.StatusOK || check.header.Get("X-User-Id") != user.ID || check.header.Get("X-User-Email") != user.Email {
		t.Fatalf("GET /check = %d id=%q email=%q body=%q", check.status, check.header.Get("X-User-Id"), check.header.Get("X-User-Email"), check.body)
	}
	if nowCalls <= callsAfterLogin {
		t.Fatal("GET /check did not read Process.Now")
	}

	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatalf("signal: %v", err)
	}
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("code = %d stderr=%q", code, stderr.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return")
	}
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("streams not empty stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	if leaked := streams.stop(); leaked != "" {
		t.Fatalf("real standard streams received %q", leaked)
	}
	for _, name := range gotEnv {
		if name != "PORT" && name != "GOOGLE_CLIENT_ID" && name != "GOOGLE_CLIENT_SECRET" && name != "WORKSPACE_DOMAIN" {
			t.Fatalf("Run read unexpected env %q", name)
		}
	}
	if !reflect.DeepEqual(gotEnv[:4], []string{"PORT", "GOOGLE_CLIENT_ID", "GOOGLE_CLIENT_SECRET", "WORKSPACE_DOMAIN"}) {
		t.Fatalf("env reads = %v", gotEnv)
	}

	opened, err := store.Open(source, bytes.NewReader(nil))
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	t.Cleanup(func() { _ = opened.Close() })
	recorded, err := opened.ConsumeLoginState(query.Get("state"))
	if err != nil || recorded.State != query.Get("state") {
		t.Fatalf("login state is not in Process.DBSource %s: %#v %v", source, recorded, err)
	}
	if err := opened.Close(); err != nil {
		t.Fatalf("close reopened store: %v", err)
	}
	db, err := sql.Open("sqlite", source)
	if err != nil {
		t.Fatalf("sql open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	var lastUsed int64
	if err := db.QueryRowContext(t.Context(), `SELECT last_used_at FROM sessions WHERE id = ?`, session.ID).Scan(&lastUsed); err != nil {
		t.Fatalf("last_used_at: %v", err)
	}
	if lastUsed != fixed.UnixNano() {
		t.Fatalf("session last_used_at = %d, want Process.Now %d (wall clock is %d)", lastUsed, fixed.UnixNano(), time.Now().UnixNano())
	}
}

func TestNoCommandDoesNotPrintProduct(t *testing.T) {
	// R-OYUS-DBTQ
	signal.Reset(syscall.SIGINT, syscall.SIGTERM)
	t.Cleanup(func() { signal.Reset(syscall.SIGINT, syscall.SIGTERM) })
	source := freshDB(t)
	port := freePort(t)
	var stdout, stderr bytes.Buffer
	p := serveProcess(t, port, source)
	p.Stdout = &stdout
	p.Stderr = &stderr
	done := make(chan int, 1)
	go func() { done <- Run(p) }()
	conn := waitConn(t, "127.0.0.1:"+port)
	buf := make([]byte, 64)
	_ = conn.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
	_, _ = conn.Read(buf)
	_ = conn.Close()
	if stdout.Len() != 0 || stderr.Len() != 0 || bytes.Contains(stdout.Bytes(), []byte("Usage:")) || bytes.Contains(stdout.Bytes(), []byte("app =")) {
		t.Fatalf("bare invocation wrote stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	if err := syscall.Kill(os.Getpid(), syscall.SIGINT); err != nil {
		t.Fatalf("signal: %v", err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return")
	}
}

func TestLayoutOwnership(t *testing.T) {
	root := moduleRoot(t)

	// R-3FKW-RNJY
	mod, err := os.ReadFile(filepath.Clean(filepath.Join(root, "go.mod")))
	if err != nil {
		t.Fatalf("read go.mod: %v", err)
	}
	if !bytes.HasPrefix(mod, []byte("module github.com/ikigenba/ikigenba/auth\n")) {
		t.Fatalf("module path = %q", firstLine(mod))
	}

	mainSrc := readFile(t, filepath.Join(root, "cmd", "auth", "main.go"))
	if !bytes.Contains(mainSrc, []byte("package main")) || !bytes.Contains(mainSrc, []byte("func main()")) {
		t.Fatal("cmd/auth/main.go does not own func main")
	}
	if !bytes.Contains(mainSrc, []byte("os.Exit(cli.Run(")) {
		t.Fatal("main does not terminate with cli.Run's integer")
	}
	for _, needle := range []string{
		"os.Args[1:]",
		"os.Getenv",
		"os.Stdout",
		"os.Stderr",
		"time.Now",
		"rand.Reader",
		"https://accounts.google.com",
		"state/auth.db",
	} {
		if !bytes.Contains(mainSrc, []byte(needle)) {
			t.Fatalf("main wiring missing %s", needle)
		}
	}
	if bytes.Contains(mainSrc, []byte("http.")) || bytes.Contains(mainSrc, []byte("sql.")) {
		t.Fatal("main contains logic beyond wiring")
	}

	cliSrc := readFile(t, filepath.Join(root, "internal", "cli", "cli.go"))
	if bytes.Contains(cliSrc, []byte("func handle")) || bytes.Contains(cliSrc, []byte("http.NewServeMux")) || bytes.Contains(cliSrc, []byte("CREATE TABLE")) {
		t.Fatal("cli owns a concern that belongs to another package")
	}

	serverFiles := goFiles(t, filepath.Join(root, "internal", "server"))
	if !bytes.Contains(bytes.Join(serverFiles, nil), []byte("http.NewServeMux")) {
		t.Fatal("internal/server does not own the HTTP router")
	}
	if bytes.Contains(cliSrc, []byte("HandleFunc")) {
		t.Fatal("cli owns an HTTP handler")
	}

	storeSrc := readFile(t, filepath.Join(root, "internal", "store", "store.go"))
	if !bytes.Contains(storeSrc, []byte("func Open")) || !bytes.Contains(storeSrc, []byte("CREATE TABLE")) {
		t.Fatal("internal/store does not own persistence")
	}

	googleSrc := readFile(t, filepath.Join(root, "internal", "google", "google.go"))
	for _, name := range []string{"func NewClient", "func (c *Client) AuthCodeURL", "func (c *Client) Exchange"} {
		if !bytes.Contains(googleSrc, []byte(name)) {
			t.Fatalf("internal/google missing %s", name)
		}
	}

	idSrc := readFile(t, filepath.Join(root, "internal", "idcodec", "idcodec.go"))
	for _, name := range []string{"func Encode", "func NewID", "func NewSecret", "func HashSecret"} {
		if !bytes.Contains(idSrc, []byte(name)) {
			t.Fatalf("internal/idcodec missing %s", name)
		}
	}

	// R-3U7P-CWGA
	assertImportGraph(t, root)
}

func TestSelfContainedServeOpensOnlyTheDatabase(t *testing.T) {
	// R-YNFB-36HN: from a working directory that holds no HTML, JavaScript, or
	// CSS, the running server still serves the bytes embedded in the binary,
	// and the only file it creates is the database.
	signal.Reset(syscall.SIGINT, syscall.SIGTERM)
	t.Cleanup(func() { signal.Reset(syscall.SIGINT, syscall.SIGTERM) })

	dir := t.TempDir()
	source := filepath.Join(dir, "state", "auth.db")
	if err := os.Mkdir(filepath.Dir(source), 0o700); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	t.Chdir(dir)
	before := filesIn(t, dir)
	port := freePort(t)
	p := serveProcess(t, port, source)
	done := make(chan int, 1)
	go func() { done <- Run(p) }()
	conn := waitConn(t, "127.0.0.1:"+port)
	_ = conn.Close()
	assertEmbeddedAssets(t, "http://127.0.0.1:"+port)
	after := filesIn(t, dir)
	var created []string
	for name := range after {
		if !before[name] {
			created = append(created, name)
		}
	}
	if len(created) != 1 || created[0] != source {
		t.Fatalf("files created = %v, want only %s", created, source)
	}
	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatalf("signal: %v", err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return")
	}
}

func filesIn(t *testing.T, root string) map[string]bool {
	t.Helper()
	found := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !entry.IsDir() {
			found[path] = true
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	return found
}

func assertServesUntilSignal(t *testing.T, source string, preexisted bool) {
	t.Helper()
	if _, err := os.Stat(source); preexisted != (err == nil) {
		t.Fatalf("preexisted=%v stat=%v", preexisted, err)
	}
	port := freePort(t)
	p := serveProcess(t, port, source)
	var stdout, stderr bytes.Buffer
	p.Stdout = &stdout
	p.Stderr = &stderr
	done := make(chan int, 1)
	go func() { done <- Run(p) }()
	conn := waitConn(t, "127.0.0.1:"+port)
	if stdout.Len() != 0 || stderr.Len() != 0 {
		_ = conn.Close()
		t.Fatalf("serving wrote stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("second listen: %v", err)
	}
	second := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatalf("close second: %v", err)
	}
	if canDial(t, second) {
		t.Fatalf("serving on %s as well as 127.0.0.1:%s", second, port)
	}
	if canDial(t, "127.0.0.1:1") {
		t.Fatal("serving on a port other than PORT")
	}
	_ = conn.Close()
	select {
	case code := <-done:
		t.Fatalf("Run returned %d before a signal", code)
	default:
	}
	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatalf("signal: %v", err)
	}
	select {
	case code := <-done:
		if code != 0 || stdout.Len() != 0 || stderr.Len() != 0 {
			t.Fatalf("shutdown code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after SIGTERM")
	}
	if canDial(t, "127.0.0.1:"+port) {
		t.Fatalf("127.0.0.1:%s still listening", port)
	}
}

func serveProcess(t *testing.T, port, source string) Process {
	t.Helper()
	env := map[string]string{
		"PORT":                 port,
		"GOOGLE_CLIENT_ID":     "client-id",
		"GOOGLE_CLIENT_SECRET": "client-secret",
		"WORKSPACE_DOMAIN":     "example.test",
	}
	return Process{
		Args:       nil,
		Getenv:     mapGetenv(env),
		Stdout:     &bytes.Buffer{},
		Stderr:     &bytes.Buffer{},
		Now:        func() time.Time { return time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC) },
		Rand:       bytes.NewReader(bytes.Repeat([]byte{1}, 256)),
		OIDCIssuer: "http://127.0.0.1:1",
		DBSource:   source,
	}
}

func run(t *testing.T, p Process) (string, string, int) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	if p.Stdout == nil {
		p.Stdout = &stdout
	} else if buf, ok := p.Stdout.(*bytes.Buffer); ok {
		stdout = *buf
		p.Stdout = &stdout
	}
	if p.Stderr == nil {
		p.Stderr = &stderr
	} else if buf, ok := p.Stderr.(*bytes.Buffer); ok {
		stderr = *buf
		p.Stderr = &stderr
	}
	code := Run(p)
	return stdout.String(), stderr.String(), code
}

func mapGetenv(env map[string]string) func(string) string {
	return func(name string) string { return env[name] }
}

func freshDB(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "auth.db")
}

func rejectedDB(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "auth.db")
	if err := os.WriteFile(path, []byte("this is not sqlite"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

func freePort(t *testing.T) string {
	t.Helper()
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	port := portOf(t, ln)
	if err := ln.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	return port
}

func hold(t *testing.T) net.Listener {
	t.Helper()
	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("hold: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	return ln
}

func portOf(t *testing.T, ln net.Listener) string {
	t.Helper()
	_, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("split: %v", err)
	}
	return port
}

func stillHolds(t *testing.T, ln net.Listener, port string) bool {
	t.Helper()
	conn, err := (&net.Dialer{}).DialContext(t.Context(), "tcp", "127.0.0.1:"+port)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return ln.Addr().String() == "127.0.0.1:"+port
}

func waitConn(t *testing.T, addr string) net.Conn {
	t.Helper()
	for attempt := 0; attempt < 10000; attempt++ {
		conn, err := (&net.Dialer{}).DialContext(t.Context(), "tcp", addr)
		if err == nil {
			return conn
		}
	}
	t.Fatalf("did not listen on %s", addr)
	return nil
}

func mustClose(t *testing.T, conn net.Conn) {
	t.Helper()
	if err := conn.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func canDial(t *testing.T, addr string) bool {
	t.Helper()
	dialer := net.Dialer{Timeout: 20 * time.Millisecond}
	conn, err := dialer.DialContext(t.Context(), "tcp", addr)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

type serveLoginResult struct {
	hitsBefore      int32
	hitsAfter       int32
	status          int
	location        *url.URL
	consumed        []byte
	clientID        string
	workspace       string
	authEndpoint    string
	source          string
	exitCode        int
	stdout          string
	stderr          string
	stillListening  bool
	dbReadyAtAccept bool
}

func serveLogin(t *testing.T) serveLoginResult {
	t.Helper()
	signal.Reset(syscall.SIGINT, syscall.SIGTERM)
	t.Cleanup(func() { signal.Reset(syscall.SIGINT, syscall.SIGTERM) })

	issuer, authEndpoint, hits := loopbackIssuer(t)
	raw := make([]byte, 64)
	for i := range raw {
		raw[i] = byte(i + 1)
	}
	reader := &recordingReader{data: raw}
	source := freshDB(t)
	port := freePort(t)
	const clientID = "client-id"
	const workspace = "example.test"
	var stdout, stderr bytes.Buffer
	fixed := time.Date(2026, 9, 21, 15, 4, 5, 0, time.UTC)
	p := Process{
		Getenv: mapGetenv(map[string]string{
			"PORT":                 port,
			"GOOGLE_CLIENT_ID":     clientID,
			"GOOGLE_CLIENT_SECRET": "client-secret",
			"WORKSPACE_DOMAIN":     workspace,
		}),
		Stdout:     &stdout,
		Stderr:     &stderr,
		Now:        func() time.Time { return fixed },
		Rand:       reader,
		OIDCIssuer: issuer,
		DBSource:   source,
	}
	done := make(chan int, 1)
	go func() { done <- Run(p) }()
	conn := waitConn(t, net.JoinHostPort("127.0.0.1", port))
	_ = conn.Close()
	_, statErr := os.Stat(source)
	hitsBefore := hits.Load()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://"+net.JoinHostPort("127.0.0.1", port)+"/login/google", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	httpClient := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := httpClient.Do(req)
	if err != nil {
		t.Fatalf("GET /login/google: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, resp.Body)
	got := serveLoginResult{
		hitsBefore:      hitsBefore,
		hitsAfter:       hits.Load(),
		status:          resp.StatusCode,
		consumed:        append([]byte(nil), reader.consumed()...),
		clientID:        clientID,
		workspace:       workspace,
		authEndpoint:    authEndpoint,
		source:          source,
		dbReadyAtAccept: statErr == nil,
	}
	if loc, err := resp.Location(); err == nil {
		got.location = loc
	}
	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatalf("signal: %v", err)
	}
	select {
	case got.exitCode = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return after SIGTERM")
	}
	got.stdout = stdout.String()
	got.stderr = stderr.String()
	got.stillListening = canDial(t, net.JoinHostPort("127.0.0.1", port))
	return got
}

func loopbackIssuer(t *testing.T) (issuerURL, authorizationEndpoint string, hits *atomic.Int32) {
	t.Helper()
	var counted atomic.Int32
	var srv *httptest.Server
	srv = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/openid-configuration" {
			http.NotFound(w, r)
			return
		}
		counted.Add(1)
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                srv.URL,
			"authorization_endpoint":                srv.URL + "/authorize",
			"token_endpoint":                        srv.URL + "/token",
			"jwks_uri":                              srv.URL + "/jwks",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		}); err != nil {
			t.Errorf("discovery document: %v", err)
		}
	}))
	srv.Start()
	t.Cleanup(srv.Close)
	return srv.URL, srv.URL + "/authorize", &counted
}

func verifierFromRand(consumed []byte, state, challenge string) (string, bool) {
	for i := 0; i+32 <= len(consumed); i++ {
		verifier := idcodec.Encode(consumed[i : i+32])
		sum := sha256.Sum256([]byte(verifier))
		if base64.RawURLEncoding.EncodeToString(sum[:]) != challenge {
			continue
		}
		for j := 0; j+16 <= len(consumed); j++ {
			if idcodec.Encode(consumed[j:j+16]) == state {
				return verifier, true
			}
		}
	}
	return "", false
}

func assertOpenCallPrecedesServe(t *testing.T) {
	t.Helper()
	path := filepath.Join(moduleRoot(t), "internal", "cli", "cli.go")
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parse cli.go: %v", err)
	}
	var fn *ast.FuncDecl
	for _, decl := range file.Decls {
		f, ok := decl.(*ast.FuncDecl)
		if ok && f.Name.Name == "serve" && f.Body != nil {
			fn = f
			break
		}
	}
	if fn == nil {
		t.Fatal("func serve is missing")
	}
	var openAt token.Pos
	var openArgs []ast.Expr
	ast.Inspect(fn.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		name := sel.Sel.Name
		if name == "Serve" || name == "Listen" || name == "ListenAndServe" {
			if openAt == 0 || call.Pos() < openAt {
				t.Fatal("loopback listener starts before store.Open returns")
			}
		}
		pkg, ok := sel.X.(*ast.Ident)
		if ok && pkg.Name == "store" && name == "Open" {
			if openAt != 0 {
				t.Fatal("serve calls store.Open more than once")
			}
			openAt = call.Pos()
			openArgs = call.Args
		}
		return true
	})
	if openAt == 0 {
		t.Fatal("serve does not call store.Open")
	}
	if len(openArgs) != 2 || !sameProcessFields(openArgs[0], openArgs[1], "DBSource", "Rand") {
		t.Fatal("store.Open must be called with Process.DBSource and Process.Rand")
	}
}

func sameProcessFields(source, rand ast.Expr, sourceField, randField string) bool {
	src, srcOK := selectorRecv(source)
	rnd, rndOK := selectorRecv(rand)
	return srcOK && rndOK && src.recv == rnd.recv && src.field == sourceField && rnd.field == randField
}

type selectorRef struct{ recv, field string }

func selectorRecv(e ast.Expr) (selectorRef, bool) {
	sel, ok := e.(*ast.SelectorExpr)
	if !ok {
		return selectorRef{}, false
	}
	id, ok := sel.X.(*ast.Ident)
	if !ok {
		return selectorRef{}, false
	}
	return selectorRef{recv: id.Name, field: sel.Sel.Name}, true
}

type recordingReader struct {
	data []byte
	n    int
}

func (r *recordingReader) Read(p []byte) (int, error) {
	if r.n >= len(r.data) {
		return 0, io.EOF
	}
	n := copy(p, r.data[r.n:])
	r.n += n
	return n, nil
}

func (r *recordingReader) consumed() []byte {
	return r.data[:r.n]
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return b
}

func goFiles(t *testing.T, dir string) [][]byte {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir %s: %v", dir, err)
	}
	var out [][]byte
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		out = append(out, readFile(t, filepath.Join(dir, entry.Name())))
	}
	return out
}

func firstLine(b []byte) string {
	line, _, _ := bytes.Cut(b, []byte("\n"))
	return string(line)
}

func assertImportGraph(t *testing.T, root string) {
	t.Helper()
	imports := map[string][]string{}
	for _, pkg := range []string{
		"cmd/auth",
		"internal/cli",
		"internal/server",
		"internal/store",
		"internal/google",
		"internal/idcodec",
		"internal/version",
	} {
		imports[pkg] = packageImports(t, filepath.Join(root, pkg))
	}
	prefix := "github.com/ikigenba/ikigenba/auth/"
	internalOf := func(path string) string {
		rest, ok := strings.CutPrefix(path, prefix)
		if !ok || !strings.HasPrefix(rest, "internal/") && rest != "cmd/auth" {
			return ""
		}
		return rest
	}
	allowed := map[string]map[string]bool{
		"cmd/auth":         {"internal/cli": true},
		"internal/cli":     {"internal/server": true, "internal/store": true, "internal/google": true, "internal/idcodec": true, "internal/version": true},
		"internal/server":  {"internal/store": true, "internal/google": true, "internal/idcodec": true, "internal/version": true},
		"internal/store":   {"internal/idcodec": true, "internal/version": true},
		"internal/google":  {"internal/idcodec": true, "internal/version": true},
		"internal/idcodec": {},
		"internal/version": {},
	}
	for pkg, paths := range imports {
		for _, path := range paths {
			internal := internalOf(path)
			if internal == "" {
				continue
			}
			if strings.HasPrefix(internal, "internal/server/") {
				continue
			}
			if _, ok := allowed[pkg][internal]; !ok {
				t.Fatalf("%s imports %s, outside the one-way graph", pkg, path)
			}
		}
	}
	for pkg, paths := range imports {
		if pkg == "cmd/auth" {
			continue
		}
		for _, path := range paths {
			if strings.Contains(path, "internal/cli") {
				t.Fatalf("%s imports internal/cli", pkg)
			}
		}
	}
	if !containsImport(imports["cmd/auth"], prefix+"internal/cli") {
		t.Fatal("cmd/auth does not import internal/cli")
	}
}

func containsImport(paths []string, want string) bool {
	for _, path := range paths {
		if path == want {
			return true
		}
	}
	return false
}

const designUsageText = `Usage: auth [command]

Serve the auth service at 127.0.0.1:$PORT. With no command, serve.

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

const designManifestText = `app = "auth"
port = 3001
default = false
secrets = ["GOOGLE_CLIENT_ID", "GOOGLE_CLIENT_SECRET"]

[env]
WORKSPACE_DOMAIN = "michaelgreenly.dev"

[database]
engine = "sqlite"
path = "state/auth.db"
`

func TestCommandGrammar(t *testing.T) {
	// R-OV73-80LN: the four recognized forms are accepted, and no other form is.
	signal.Reset(syscall.SIGINT, syscall.SIGTERM)
	t.Cleanup(func() { signal.Reset(syscall.SIGINT, syscall.SIGTERM) })

	stdout, stderr, code := run(t, Process{Args: []string{"--version"}})
	if code != 0 || stderr != "" || stdout != version.Version+"\n" {
		t.Fatalf("--version code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	stdout, stderr, code = run(t, Process{Args: []string{"--help"}})
	if code != 0 || stderr != "" || stdout != designUsageText {
		t.Fatalf("--help code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	stdout, stderr, code = run(t, Process{Args: []string{"manifest"}})
	if code != 0 || stderr != "" || stdout != designManifestText {
		t.Fatalf("manifest code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	assertServesUntilSignal(t, freshDB(t), false)

	for _, args := range [][]string{{"help"}, {"--bogus"}, {"manifest", "extra"}, {"--version", "--help"}} {
		stdout, stderr, code = run(t, Process{Args: args})
		if code != 2 || stdout != "" || !strings.HasPrefix(stderr, "auth: ") {
			t.Fatalf("args %v code=%d stdout=%q stderr=%q", args, code, stdout, stderr)
		}
	}
}

func TestRunExitClasses(t *testing.T) {
	// R-3XVE-I7OD
	signal.Reset(syscall.SIGINT, syscall.SIGTERM)
	t.Cleanup(func() { signal.Reset(syscall.SIGINT, syscall.SIGTERM) })

	stdout, stderr, code := run(t, Process{Args: []string{"--version"}})
	if code != 0 || stderr != "" || stdout != version.Version+"\n" {
		t.Fatalf("success code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}

	stdout, stderr, code = run(t, Process{Args: []string{"bogus"}})
	if code != 2 || stdout != "" || stderr != "auth: unknown command 'bogus'\n\nsee 'auth --help' for usage\n" {
		t.Fatalf("usage code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	stdout, stderr, code = run(t, Process{Getenv: func(string) string { return "" }, DBSource: filepath.Join(t.TempDir(), "auth.db")})
	if code != 2 || stdout != "" || stderr != "auth: PORT is not set\n" {
		t.Fatalf("config usage code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}

	source := rejectedDB(t)
	stdout, stderr, code = run(t, serveProcess(t, freePort(t), source))
	if code != 1 || stdout != "" || !strings.HasPrefix(stderr, "auth: cannot open database "+source+": ") {
		t.Fatalf("open failure code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	held := hold(t)
	port := portOf(t, held)
	stdout, stderr, code = run(t, serveProcess(t, port, freshDB(t)))
	if code != 1 || stdout != "" || !strings.Contains(stderr, "address already in use") {
		t.Fatalf("bind failure code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}

	assertServesUntilSignal(t, freshDB(t), false)
}

func TestHealthyListenerIsLoopbackHTTP(t *testing.T) {
	// R-IOAG-UG8V: the only new listener is 127.0.0.1:PORT, and it speaks the auth HTTP routes.
	signal.Reset(syscall.SIGINT, syscall.SIGTERM)
	t.Cleanup(func() { signal.Reset(syscall.SIGINT, syscall.SIGTERM) })
	source := freshDB(t)
	port := freePort(t)
	before := listenSockets(t)
	p := serveProcess(t, port, source)
	var stdout, stderr bytes.Buffer
	p.Stdout = &stdout
	p.Stderr = &stderr
	done := make(chan int, 1)
	go func() { done <- Run(p) }()
	mustClose(t, waitConn(t, "127.0.0.1:"+port))

	added := addedListens(before, listenSockets(t))
	want := []string{net.JoinHostPort("127.0.0.1", port)}
	if !reflect.DeepEqual(added, want) {
		t.Fatalf("listeners added = %v, want %v", added, want)
	}
	root := getNoRedirect(t, "http://127.0.0.1:"+port+"/")
	if root.status != http.StatusOK || root.header.Get("Content-Type") != "text/html; charset=utf-8" || root.body != `<!doctype html><html><body><a href="/login/google">Sign in with Google</a></body></html>` {
		t.Fatalf("GET / = %d %q %q", root.status, root.header.Get("Content-Type"), root.body)
	}
	missing := getNoRedirect(t, "http://127.0.0.1:"+port+"/not-a-route")
	if missing.status != http.StatusNotFound {
		t.Fatalf("GET /not-a-route = %d", missing.status)
	}
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("streams stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	select {
	case code := <-done:
		t.Fatalf("Run returned %d before a signal", code)
	default:
	}
	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatalf("signal: %v", err)
	}
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("shutdown code=%d", code)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return")
	}
	if canDial(t, "127.0.0.1:"+port) {
		t.Fatalf("127.0.0.1:%s still listening", port)
	}
}

func TestMainIsOnlyWiring(t *testing.T) {
	// R-3GST-5FAN
	// R-3VFL-QO6Z: the Process literal is the process arguments, environment,
	// streams, wall clock, crypto reader, production issuer, and state/auth.db.
	path := filepath.Join(moduleRoot(t), "cmd", "auth", "main.go")
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parse main.go: %v", err)
	}
	if file.Name.Name != "main" {
		t.Fatalf("package %s, want main", file.Name.Name)
	}
	imports := importPaths(t, file)
	var mainFn *ast.FuncDecl
	for _, decl := range file.Decls {
		switch decl := decl.(type) {
		case *ast.GenDecl:
			if decl.Tok != token.IMPORT {
				t.Fatalf("main.go declares %s besides imports", decl.Tok)
			}
		case *ast.FuncDecl:
			if mainFn != nil || decl.Name.Name != "main" {
				t.Fatalf("main.go declares %s as well as main", decl.Name.Name)
			}
			mainFn = decl
		default:
			t.Fatalf("main.go has a non-wiring declaration %T", decl)
		}
	}
	if mainFn == nil || mainFn.Body == nil || len(mainFn.Body.List) != 1 {
		t.Fatalf("main body is not a single statement")
	}
	exit, ok := mainFn.Body.List[0].(*ast.ExprStmt)
	if !ok {
		t.Fatal("main's statement is not os.Exit")
	}
	exitCall, ok := exit.X.(*ast.CallExpr)
	if !ok || !selectorIs(exitCall.Fun, "os", "Exit") || len(exitCall.Args) != 1 {
		t.Fatal("main does not os.Exit with one argument")
	}
	runCall, ok := exitCall.Args[0].(*ast.CallExpr)
	if !ok || !selectorIs(runCall.Fun, "cli", "Run") || len(runCall.Args) != 1 {
		t.Fatal("os.Exit's argument is not cli.Run")
	}
	if imports["os"] != "os" || imports["cli"] != "github.com/ikigenba/ikigenba/auth/internal/cli" {
		t.Fatalf("imports = %v", imports)
	}
	lit, ok := runCall.Args[0].(*ast.CompositeLit)
	if !ok || !selectorIs(lit.Type, "cli", "Process") {
		t.Fatal("cli.Run's argument is not a cli.Process literal")
	}
	got := map[string]ast.Expr{}
	for _, elt := range lit.Elts {
		kv, ok := elt.(*ast.KeyValueExpr)
		if !ok {
			t.Fatal("Process literal has a positional element")
		}
		key, ok := kv.Key.(*ast.Ident)
		if !ok {
			t.Fatal("Process literal key is not an identifier")
		}
		got[key.Name] = kv.Value
	}
	want := []string{"Args", "Getenv", "Stdout", "Stderr", "Now", "Rand", "OIDCIssuer", "DBSource"}
	if len(got) != len(want) {
		t.Fatalf("Process literal fields = %v", fieldNames(got))
	}
	slice, ok := got["Args"].(*ast.SliceExpr)
	if !ok || slice.High != nil || slice.Max != nil || !selectorIs(slice.X, "os", "Args") || !basicInt(slice.Low, 1) {
		t.Fatal("Args is not os.Args[1:]")
	}
	if !selectorIs(got["Getenv"], "os", "Getenv") || !selectorIs(got["Stdout"], "os", "Stdout") || !selectorIs(got["Stderr"], "os", "Stderr") {
		t.Fatal("Getenv, Stdout, or Stderr is not the process value")
	}
	if !selectorIs(got["Now"], "time", "Now") || imports["time"] != "time" {
		t.Fatal("Now is not the system wall clock time.Now")
	}
	if !selectorIs(got["Rand"], "rand", "Reader") || imports["rand"] != "crypto/rand" {
		t.Fatalf("Rand is not crypto/rand.Reader (import %q)", imports["rand"])
	}
	if !basicString(t, got["OIDCIssuer"], "https://accounts.google.com") {
		t.Fatal("OIDCIssuer is not the production Google issuer")
	}
	if !basicString(t, got["DBSource"], "state/auth.db") {
		t.Fatal("DBSource is not state/auth.db")
	}
}

func TestPackageOwnership(t *testing.T) {
	// R-3I0P-J71C
	// R-3J8L-WYS1
	// R-3KGI-AQIQ
	// R-3LOE-OI9F
	// R-3MWB-2A04
	files := parseProductionFiles(t, moduleRoot(t))
	cliExported := map[string]bool{}
	handlers := map[string][]string{}
	handleFuncs := map[string][]string{}
	sqlIn := map[string][]string{}
	funcNames := map[string][]string{}
	typeNames := map[string][]string{}
	for _, file := range files {
		for _, decl := range file.file.Decls {
			switch decl := decl.(type) {
			case *ast.FuncDecl:
				funcNames[decl.Name.Name] = append(funcNames[decl.Name.Name], file.pkg)
				if decl.Name.IsExported() && file.pkg == "internal/cli" {
					cliExported[decl.Name.Name] = true
				}
				if isHTTPHandler(decl.Type) {
					handlers[file.pkg] = append(handlers[file.pkg], decl.Name.Name)
				}
				if recv := receiverType(decl); recv == "Store" || recv == "*Store" {
					if file.pkg != "internal/store" {
						t.Fatalf("%s declares a Store method %s", file.pkg, decl.Name.Name)
					}
				}
			case *ast.GenDecl:
				for _, spec := range decl.Specs {
					switch spec := spec.(type) {
					case *ast.TypeSpec:
						typeNames[spec.Name.Name] = append(typeNames[spec.Name.Name], file.pkg)
						if spec.Name.IsExported() && file.pkg == "internal/cli" {
							cliExported[spec.Name.Name] = true
						}
					case *ast.ValueSpec:
						for _, name := range spec.Names {
							if name.IsExported() && file.pkg == "internal/cli" {
								cliExported[name.Name] = true
							}
							if name.IsExported() {
								funcNames[name.Name] = append(funcNames[name.Name], file.pkg)
							}
						}
					}
				}
			}
		}
		ast.Inspect(file.file, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if ok && lit.Kind == token.STRING {
				value, err := strconv.Unquote(lit.Value)
				if err == nil && isSQL(value) && file.pkg != "internal/store" {
					sqlIn[file.pkg] = append(sqlIn[file.pkg], value)
				}
			}
			if fn, ok := n.(*ast.FuncLit); ok && isHTTPHandler(fn.Type) {
				handlers[file.pkg] = append(handlers[file.pkg], "funcLit")
			}
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if sel.Sel.Name == "HandleFunc" || sel.Sel.Name == "Handle" || sel.Sel.Name == "NewServeMux" {
				handleFuncs[file.pkg] = append(handleFuncs[file.pkg], callPattern(call))
			}
			return true
		})
	}

	if !cliExported["Process"] || !cliExported["Run"] || len(cliExported) != 2 {
		t.Fatalf("internal/cli exports %v, want only Process and Run", cliExported)
	}
	for pkg, names := range handlers {
		if pkg != "internal/server" {
			t.Fatalf("HTTP handlers live in %s: %v", pkg, names)
		}
	}
	if len(handlers["internal/server"]) == 0 {
		t.Fatal("internal/server declares no HTTP handler")
	}
	for pkg, patterns := range handleFuncs {
		if pkg != "internal/server" {
			t.Fatalf("HTTP router registration lives in %s: %v", pkg, patterns)
		}
	}
	gotPatterns := map[string]bool{}
	for _, pattern := range handleFuncs["internal/server"] {
		gotPatterns[pattern] = true
	}
	for _, pattern := range []string{
		"GET /{$}",
		"GET /login/google",
		"GET /login/google/callback",
		"POST /logout",
		"GET /check",
		"GET /me",
		"POST /tokens",
		"POST /tokens/{id}/{action}",
	} {
		if !gotPatterns[pattern] {
			t.Fatalf("internal/server router is missing %s (has %v)", pattern, handleFuncs["internal/server"])
		}
	}
	for _, name := range []string{"Server", "New", "SessionCookieName", "HeaderUserID", "HeaderUserEmail"} {
		pkgs := typeOrValuePkgs(typeNames, funcNames, name)
		if len(pkgs) != 1 || pkgs[0] != "internal/server" {
			t.Fatalf("exported HTTP name %s lives in %v, want internal/server", name, pkgs)
		}
	}
	for _, name := range []string{"Serve", "Shutdown"} {
		if pkgs := funcNames[name]; len(pkgs) != 1 || pkgs[0] != "internal/server" {
			t.Fatalf("%s lives in %v, want internal/server", name, pkgs)
		}
	}
	if len(sqlIn) != 0 {
		t.Fatalf("SQL outside internal/store: %v", sqlIn)
	}
	for _, name := range []string{"User", "Session", "LoginState", "Token", "Identity", "Store", "Expiry"} {
		if pkgs := typeNames[name]; len(pkgs) != 1 || pkgs[0] != "internal/store" {
			t.Fatalf("domain type %s lives in %v, want internal/store", name, pkgs)
		}
	}
	for _, name := range []string{"NewClient", "AuthCodeURL", "Exchange"} {
		if pkgs := funcNames[name]; len(pkgs) != 1 || pkgs[0] != "internal/google" {
			t.Fatalf("Google name %s lives in %v, want internal/google", name, pkgs)
		}
	}
	for name, pkgs := range funcNames {
		if !ast.IsExported(name) {
			continue
		}
		if strings.Contains(name, "Verify") || strings.Contains(name, "IDToken") {
			for _, pkg := range pkgs {
				if pkg != "internal/google" {
					t.Fatalf("exported verify name %s lives in %s", name, pkg)
				}
			}
		}
	}
	for _, name := range []string{"Encode", "NewID", "NewSecret", "HashSecret"} {
		if pkgs := funcNames[name]; len(pkgs) != 1 || pkgs[0] != "internal/idcodec" {
			t.Fatalf("%s lives in %v, want internal/idcodec", name, pkgs)
		}
	}

	const crockford = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"
	if idcodec.Alphabet != crockford || strings.ContainsAny(idcodec.Alphabet, "ILOU") {
		t.Fatalf("Alphabet = %q, want Crockford base32", idcodec.Alphabet)
	}
	enc := base32.NewEncoding(crockford).WithPadding(base32.NoPadding)
	raw := []byte{0x01, 0x23, 0x45, 0x67, 0x89, 0xab, 0xcd, 0xef}
	if idcodec.Encode(raw) != enc.EncodeToString(raw) {
		t.Fatalf("Encode = %s, want Crockford %s", idcodec.Encode(raw), enc.EncodeToString(raw))
	}
	idRaw := bytes.Repeat([]byte{0x11}, 16)
	id, err := idcodec.NewID(bytes.NewReader(idRaw))
	if err != nil || id != enc.EncodeToString(idRaw) {
		t.Fatalf("NewID = %q %v, want %s", id, err, enc.EncodeToString(idRaw))
	}
	secretRaw := bytes.Repeat([]byte{0x22}, 32)
	secret, err := idcodec.NewSecret(bytes.NewReader(secretRaw))
	if err != nil || secret != "ikp_"+enc.EncodeToString(secretRaw) {
		t.Fatalf("NewSecret = %q %v", secret, err)
	}
	sum := sha256.Sum256([]byte(secret))
	if idcodec.HashSecret(secret) != hex.EncodeToString(sum[:]) {
		t.Fatalf("HashSecret = %s", idcodec.HashSecret(secret))
	}
}

func typeOrValuePkgs(types, fns map[string][]string, name string) []string {
	out := append([]string{}, types[name]...)
	out = append(out, fns[name]...)
	return out
}

type parsedFile struct {
	pkg  string
	file *ast.File
}

func parseProductionFiles(t *testing.T, root string) []parsedFile {
	t.Helper()
	fset := token.NewFileSet()
	var out []parsedFile
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			base := entry.Name()
			if base == ".git" || base == "specs" || base == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, filepath.Dir(path))
		if err != nil {
			return err
		}
		out = append(out, parsedFile{pkg: filepath.ToSlash(rel), file: file})
		return nil
	})
	if err != nil {
		t.Fatalf("parse module: %v", err)
	}
	return out
}

func isHTTPHandler(fn *ast.FuncType) bool {
	if fn == nil || fn.Params == nil {
		return false
	}
	sawWriter := false
	sawRequest := false
	for _, field := range fn.Params.List {
		name := typeName(field.Type)
		if name == "http.ResponseWriter" || name == "ResponseWriter" {
			sawWriter = true
		}
		if name == "*http.Request" || name == "*Request" {
			sawRequest = true
		}
	}
	return sawWriter && sawRequest
}

func typeName(expr ast.Expr) string {
	switch expr := expr.(type) {
	case *ast.Ident:
		return expr.Name
	case *ast.SelectorExpr:
		if id, ok := expr.X.(*ast.Ident); ok {
			return id.Name + "." + expr.Sel.Name
		}
	case *ast.StarExpr:
		return "*" + typeName(expr.X)
	}
	return ""
}

func receiverType(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) != 1 {
		return ""
	}
	return typeName(fn.Recv.List[0].Type)
}

func isSQL(value string) bool {
	for _, marker := range []string{"CREATE TABLE", "INSERT INTO", "UPDATE ", "DELETE FROM", "SELECT ", "PRAGMA "} {
		if strings.Contains(value, marker) {
			return true
		}
	}
	return false
}

func callPattern(call *ast.CallExpr) string {
	if len(call.Args) == 0 {
		return ""
	}
	lit, ok := call.Args[0].(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return ""
	}
	value, err := strconv.Unquote(lit.Value)
	if err != nil {
		return ""
	}
	return value
}

func importPaths(t *testing.T, file *ast.File) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			t.Fatalf("unquote: %v", err)
		}
		local := path
		if slash := strings.LastIndex(path, "/"); slash >= 0 {
			local = path[slash+1:]
		}
		if spec.Name != nil {
			local = spec.Name.Name
		}
		out[local] = path
	}
	return out
}

func selectorIs(expr ast.Expr, pkg, name string) bool {
	sel, ok := expr.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	id, ok := sel.X.(*ast.Ident)
	return ok && id.Name == pkg && sel.Sel.Name == name
}

func basicInt(expr ast.Expr, want int) bool {
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.INT {
		return false
	}
	return lit.Value == strconv.Itoa(want)
}

func basicString(t *testing.T, expr ast.Expr, want string) bool {
	t.Helper()
	lit, ok := expr.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return false
	}
	value, err := strconv.Unquote(lit.Value)
	if err != nil {
		t.Fatalf("unquote: %v", err)
	}
	return value == want
}

func fieldNames(fields map[string]ast.Expr) []string {
	out := make([]string, 0, len(fields))
	for name := range fields {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func packageImports(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("readdir %s: %v", dir, err)
	}
	fset := token.NewFileSet()
	var out []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(dir, entry.Name()), nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parse %s: %v", entry.Name(), err)
		}
		for _, spec := range file.Imports {
			path, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				t.Fatalf("unquote: %v", err)
			}
			out = append(out, path)
		}
	}
	return out
}

type streamDivert struct {
	buf      *bytes.Buffer
	once     sync.Once
	done     chan struct{}
	write    *os.File
	origOut  *os.File
	origErr  *os.File
	captured string
}

func divertStandardStreams(t *testing.T) *streamDivert {
	t.Helper()
	read, write, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	divert := &streamDivert{
		buf:     &bytes.Buffer{},
		done:    make(chan struct{}),
		write:   write,
		origOut: os.Stdout,
		origErr: os.Stderr,
	}
	os.Stdout = write
	os.Stderr = write
	go func() {
		_, _ = io.Copy(divert.buf, read)
		close(divert.done)
	}()
	t.Cleanup(func() { _ = divert.stop() })
	return divert
}

func (d *streamDivert) stop() string {
	d.once.Do(func() {
		os.Stdout = d.origOut
		os.Stderr = d.origErr
		_ = d.write.Close()
		<-d.done
		d.captured = d.buf.String()
	})
	return d.captured
}

func gatedIssuer(t *testing.T) (string, <-chan struct{}, func()) {
	t.Helper()
	hit := make(chan struct{}, 1)
	var mu sync.Mutex
	var gate chan struct{}
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/.well-known/openid-configuration" {
			http.NotFound(w, r)
			return
		}
		next := make(chan struct{})
		mu.Lock()
		gate = next
		mu.Unlock()
		hit <- struct{}{}
		<-next
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                srv.URL,
			"authorization_endpoint":                srv.URL + "/authorize",
			"token_endpoint":                        srv.URL + "/token",
			"jwks_uri":                              srv.URL + "/jwks",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		}); err != nil {
			t.Errorf("discovery document: %v", err)
		}
	}))
	release := func() {
		mu.Lock()
		current := gate
		gate = nil
		mu.Unlock()
		if current != nil {
			close(current)
		}
	}
	t.Cleanup(func() {
		release()
		srv.Close()
	})
	return srv.URL, hit, release
}

func assertDidNotOpenOrListen(t *testing.T, before map[string]struct{}, source string) {
	t.Helper()
	if _, err := os.Stat(source); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("database created at %s: %v", source, err)
	}
	if added := addedListens(before, listenSockets(t)); len(added) != 0 {
		t.Fatalf("listened on %v", added)
	}
}

// listenSockets reports TCP listeners owned by this process. cli.Run is
// in-process, so the process under test is os.Getpid(). /proc/net/tcp is the
// network namespace: other packages under go test ./... also bind
// 127.0.0.1:0, and those listeners are not this command's.
func listenSockets(t *testing.T) map[string]struct{} {
	t.Helper()
	owned := processSocketInodes(t, os.Getpid())
	out := map[string]struct{}{}
	for _, path := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		text, err := os.ReadFile(filepath.Clean(path))
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		lines := strings.Split(string(text), "\n")
		for _, line := range lines[1:] {
			fields := strings.Fields(line)
			if len(fields) < 10 || fields[3] != "0A" {
				continue
			}
			if _, ok := owned[fields[9]]; !ok {
				continue
			}
			ipHex, portHex, ok := strings.Cut(fields[1], ":")
			if !ok {
				t.Fatalf("local address %q", fields[1])
			}
			ip := parseProcIP(t, ipHex)
			port, err := strconv.ParseUint(portHex, 16, 16)
			if err != nil {
				t.Fatalf("port %q: %v", portHex, err)
			}
			out[net.JoinHostPort(ip.String(), strconv.FormatUint(port, 10))] = struct{}{}
		}
	}
	return out
}

func processSocketInodes(t *testing.T, pid int) map[string]struct{} {
	t.Helper()
	dir := filepath.Join("/proc", strconv.Itoa(pid), "fd")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	inodes := map[string]struct{}{}
	for _, entry := range entries {
		target, err := os.Readlink(filepath.Join(dir, entry.Name()))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			t.Fatalf("readlink %s/%s: %v", dir, entry.Name(), err)
		}
		inode, ok := strings.CutPrefix(target, "socket:[")
		if !ok || !strings.HasSuffix(inode, "]") {
			continue
		}
		inodes[strings.TrimSuffix(inode, "]")] = struct{}{}
	}
	return inodes
}

func parseProcIP(t *testing.T, hexIP string) net.IP {
	t.Helper()
	if len(hexIP) == 8 {
		raw, err := hex.DecodeString(hexIP)
		if err != nil || len(raw) != 4 {
			t.Fatalf("ipv4 %q: %v", hexIP, err)
		}
		// /proc/net/tcp stores IPv4 little-endian.
		return net.IPv4(raw[3], raw[2], raw[1], raw[0]).To4()
	}
	raw, err := hex.DecodeString(hexIP)
	if err != nil || len(raw) != 16 {
		t.Fatalf("ipv6 %q: %v", hexIP, err)
	}
	for i := 0; i < 16; i += 4 {
		raw[i], raw[i+3] = raw[i+3], raw[i]
		raw[i+1], raw[i+2] = raw[i+2], raw[i+1]
	}
	return net.IP(raw)
}

func addedListens(before, after map[string]struct{}) []string {
	var added []string
	for addr := range after {
		if _, ok := before[addr]; !ok {
			added = append(added, addr)
		}
	}
	sort.Strings(added)
	return added
}

type httpGot struct {
	status   int
	header   http.Header
	body     string
	location *url.URL
}

func getNoRedirect(t *testing.T, rawURL string, cookie ...string) httpGot {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	if len(cookie) == 1 {
		req.Header.Set("Cookie", cookie[0])
	}
	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", rawURL, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s: %v", rawURL, err)
	}
	got := httpGot{status: resp.StatusCode, header: resp.Header.Clone(), body: string(body)}
	if loc, err := resp.Location(); err == nil {
		got.location = loc
	}
	return got
}

func assertEmbeddedAssets(t *testing.T, base string) {
	t.Helper()
	wantType := map[string]string{
		"index.html": "text/html; charset=utf-8",
		"app.js":     "text/javascript; charset=utf-8",
		"style.css":  "text/css; charset=utf-8",
	}
	client := &http.Client{}
	for _, name := range []string{"index.html", "app.js", "style.css"} {
		embedded, err := assets.Files.ReadFile(name)
		if err != nil {
			t.Fatalf("embedded %s: %v", name, err)
		}
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/assets/"+name, nil)
		if err != nil {
			cancel()
			t.Fatalf("request: %v", err)
		}
		resp, err := client.Do(req)
		if err != nil {
			cancel()
			t.Fatalf("GET %s: %v", name, err)
		}
		body, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		cancel()
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if resp.StatusCode != http.StatusOK || resp.Header.Get("Content-Type") != wantType[name] || !bytes.Equal(body, embedded) {
			t.Fatalf("served %s status=%d type=%q (%d bytes), want embedded %s", name, resp.StatusCode, resp.Header.Get("Content-Type"), len(body), wantType[name])
		}
		if _, err := os.Stat(name); err == nil {
			t.Fatalf("working directory contains %s; a disk read could satisfy the response", name)
		}
	}
}
