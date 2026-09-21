package cli

import (
	"bufio"
	"bytes"
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
	"os/signal"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/auth/internal/idcodec"
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
	// R-OWEZ-LSCC
	stdout, stderr, code = run(t, Process{Args: []string{"--help"}})
	if code != 0 || stderr != "" || stdout != usageText {
		t.Fatalf("--help code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if !strings.HasSuffix(usageText, "\n") {
		t.Fatal("usage text has no trailing newline")
	}

	// R-P2IH-IN1T
	// R-OXMV-ZK31
	stdout, stderr, code = run(t, Process{Args: []string{"manifest"}})
	if code != 0 || stderr != "" || stdout != manifestText {
		t.Fatalf("manifest code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if !strings.HasSuffix(manifestText, "\n") {
		t.Fatal("manifest text has no trailing newline")
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
	// R-OV73-80LN
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
	dir := t.TempDir()
	source := filepath.Join(dir, "missing", "auth.db")

	// R-IGZ2-JTSP
	// R-IFR6-6220: PORT is read and rejected before any Google setting.
	var looked []string
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
	if _, err := os.Stat(source); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("store opened on unset PORT: %v", err)
	}

	// R-II6Y-XLJE
	for _, value := range []string{"0", "65536", "-1", "80a", " 80", "1.5", "0x10"} {
		stdout, stderr, code = run(t, Process{
			Getenv:   mapGetenv(map[string]string{"PORT": value, "GOOGLE_CLIENT_ID": "id"}),
			DBSource: source,
		})
		want := fmt.Sprintf("auth: PORT is '%s', not a port number\n", value)
		if code != 2 || stdout != "" || stderr != want {
			t.Fatalf("PORT %q code=%d stdout=%q stderr=%q", value, code, stdout, stderr)
		}
	}

	// R-IJEV-BDA3
	cases := []struct {
		env  map[string]string
		name string
	}{
		{map[string]string{"PORT": "1"}, "GOOGLE_CLIENT_ID"},
		{map[string]string{"PORT": "65535", "GOOGLE_CLIENT_ID": "id"}, "GOOGLE_CLIENT_SECRET"},
		{map[string]string{"PORT": "80", "GOOGLE_CLIENT_ID": "id", "GOOGLE_CLIENT_SECRET": "secret"}, "WORKSPACE_DOMAIN"},
		{map[string]string{"PORT": "80", "GOOGLE_CLIENT_SECRET": "secret", "WORKSPACE_DOMAIN": "example.test"}, "GOOGLE_CLIENT_ID"},
	}
	for _, tc := range cases {
		looked = nil
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
		if looked[0] != "PORT" {
			t.Fatalf("PORT was not validated first: %v", looked)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "missing")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a config fault created the database parent")
	}
}

func TestOpenFailureDoesNotListen(t *testing.T) {
	// R-IN2K-GOI6
	// R-3XVE-I7OD
	source := filepath.Join(t.TempDir(), "missing", "auth.db")
	held := hold(t)
	port := portOf(t, held)
	stdout, stderr, code := run(t, serveProcess(t, port, source))
	wantPrefix := "auth: cannot open database " + source + ": "
	if code != 1 || stdout != "" || !strings.HasPrefix(stderr, wantPrefix) || strings.Count(stderr, "\n") != 1 {
		t.Fatalf("open failure code=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
	if !stillHolds(t, held, port) {
		t.Fatal("open failure disturbed the process holding the port")
	}
}

func TestStoreOpenBeforeListen(t *testing.T) {
	// R-4KCJ-48RH
	// Ordering is source order, not a race against store.Open: Serve is reached only after Open returns.
	assertOpenCallPrecedesServe(t)

	port := freePort(t)
	source := filepath.Join(t.TempDir(), "missing", "auth.db")
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
	// R-IOAG-UG8V
	// R-KXL6-33Q7
	// R-OYUS-DBTQ
	// R-3XVE-I7OD
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
	// R-IT62-DJ7N
	signal.Reset(syscall.SIGINT, syscall.SIGTERM)
	t.Cleanup(func() { signal.Reset(syscall.SIGINT, syscall.SIGTERM) })
	var codes []int
	var outs []string
	for _, sig := range []os.Signal{syscall.SIGTERM, syscall.SIGINT} {
		source := freshDB(t)
		port := freePort(t)
		p := serveProcess(t, port, source)
		var stdout, stderr bytes.Buffer
		p.Stdout = &stdout
		p.Stderr = &stderr
		accepted := make(chan struct{})
		done := make(chan int, 1)
		go func() {
			done <- Run(p)
		}()
		conn := waitConn(t, "127.0.0.1:"+port)
		close(accepted)
		release := hangRequest(t, conn)
		if err := syscall.Kill(os.Getpid(), sig.(syscall.Signal)); err != nil {
			release()
			t.Fatalf("signal %s: %v", sig, err)
		}
		select {
		case code := <-done:
			release()
			codes = append(codes, code)
			outs = append(outs, stdout.String()+"|"+stderr.String())
			if code != 0 || stdout.Len() != 0 || stderr.Len() != 0 {
				t.Fatalf("%s code=%d stdout=%q stderr=%q", sig, code, stdout.String(), stderr.String())
			}
		case <-time.After(5 * time.Second):
			release()
			t.Fatalf("%s did not return", sig)
		}
		if canDial(t, "127.0.0.1:"+port) {
			t.Fatalf("%s left 127.0.0.1:%s listening", sig, port)
		}
		_ = accepted
	}
	if codes[0] != codes[1] || outs[0] != outs[1] {
		t.Fatalf("signals diverged: codes %v outs %q", codes, outs)
	}
}

func TestRunUsesOnlyProcess(t *testing.T) {
	// R-40B7-9R5R
	signal.Reset(syscall.SIGINT, syscall.SIGTERM)
	t.Cleanup(func() { signal.Reset(syscall.SIGINT, syscall.SIGTERM) })
	t.Setenv("PORT", "9999")
	t.Setenv("GOOGLE_CLIENT_ID", "real-id")
	t.Setenv("GOOGLE_CLIENT_SECRET", "real-secret")
	t.Setenv("WORKSPACE_DOMAIN", "real.example")

	var gotEnv []string
	var nowCalls int
	var randBytes int
	fixed := time.Date(2026, 9, 21, 12, 0, 0, 0, time.UTC)
	source := freshDB(t)
	port := freePort(t)
	p := Process{
		Args: nil,
		Getenv: func(name string) string {
			gotEnv = append(gotEnv, name)
			return map[string]string{
				"PORT":                 port,
				"GOOGLE_CLIENT_ID":     "injected-id",
				"GOOGLE_CLIENT_SECRET": "injected-secret",
				"WORKSPACE_DOMAIN":     "injected.example",
			}[name]
		},
		Stdout:     &bytes.Buffer{},
		Stderr:     &bytes.Buffer{},
		Now:        func() time.Time { nowCalls++; return fixed },
		Rand:       readCounter{&randBytes},
		OIDCIssuer: "http://127.0.0.1:1",
		DBSource:   source,
	}
	done := make(chan int, 1)
	go func() { done <- Run(p) }()
	conn := waitConn(t, "127.0.0.1:"+port)
	_ = conn.Close()
	if nowCalls != 0 {
		t.Fatal("Run read the clock before a request")
	}
	if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
		t.Fatalf("signal: %v", err)
	}
	select {
	case code := <-done:
		if code != 0 {
			t.Fatalf("code = %d stderr=%q", code, p.Stderr.(*bytes.Buffer).String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return")
	}
	if p.Stdout.(*bytes.Buffer).Len() != 0 || p.Stderr.(*bytes.Buffer).Len() != 0 {
		t.Fatalf("streams not empty stdout=%q stderr=%q", p.Stdout, p.Stderr)
	}
	for _, name := range gotEnv {
		if name != "PORT" && name != "GOOGLE_CLIENT_ID" && name != "GOOGLE_CLIENT_SECRET" && name != "WORKSPACE_DOMAIN" {
			t.Fatalf("Run read unexpected env %q", name)
		}
	}
	if !reflect.DeepEqual(gotEnv[:4], []string{"PORT", "GOOGLE_CLIENT_ID", "GOOGLE_CLIENT_SECRET", "WORKSPACE_DOMAIN"}) {
		t.Fatalf("env reads = %v", gotEnv)
	}
	if randBytes != 0 {
		t.Fatalf("Run read %d random bytes before a request", randBytes)
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

	// R-3GST-5FAN
	// R-3VFL-QO6Z
	// R-3WNI-4FXO
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

	// R-3I0P-J71C
	cliSrc := readFile(t, filepath.Join(root, "internal", "cli", "cli.go"))
	if bytes.Contains(cliSrc, []byte("func handle")) || bytes.Contains(cliSrc, []byte("http.NewServeMux")) || bytes.Contains(cliSrc, []byte("CREATE TABLE")) {
		t.Fatal("cli owns a concern that belongs to another package")
	}

	// R-3J8L-WYS1: HTTP handler names live in internal/server, not cli.
	serverFiles := goFiles(t, filepath.Join(root, "internal", "server"))
	if !bytes.Contains(bytes.Join(serverFiles, nil), []byte("http.NewServeMux")) {
		t.Fatal("internal/server does not own the HTTP router")
	}
	if bytes.Contains(cliSrc, []byte("HandleFunc")) {
		t.Fatal("cli owns an HTTP handler")
	}

	// R-3KGI-AQIQ
	storeSrc := readFile(t, filepath.Join(root, "internal", "store", "store.go"))
	if !bytes.Contains(storeSrc, []byte("func Open")) || !bytes.Contains(storeSrc, []byte("CREATE TABLE")) {
		t.Fatal("internal/store does not own persistence")
	}

	// R-3LOE-OI9F
	googleSrc := readFile(t, filepath.Join(root, "internal", "google", "google.go"))
	for _, name := range []string{"func NewClient", "func (c *Client) AuthCodeURL", "func (c *Client) Exchange"} {
		if !bytes.Contains(googleSrc, []byte(name)) {
			t.Fatalf("internal/google missing %s", name)
		}
	}

	// R-3MWB-2A04
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
	// R-YNFB-36HN: HTML, JavaScript, and CSS are served from embedded assets
	// by internal/server. This clause is the file half: a serve opens the
	// SQLite database and no other file. The release build is cgo-free, so
	// the binary depends on no shared library; that is proven by building
	// with CGO_ENABLED=0 in the release gate, not by a second process here.
	signal.Reset(syscall.SIGINT, syscall.SIGTERM)
	t.Cleanup(func() { signal.Reset(syscall.SIGINT, syscall.SIGTERM) })

	dir := t.TempDir()
	source := filepath.Join(dir, "state", "auth.db")
	if err := os.Mkdir(filepath.Dir(source), 0o700); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	before := filesIn(t, dir)
	port := freePort(t)
	p := serveProcess(t, port, source)
	done := make(chan int, 1)
	go func() { done <- Run(p) }()
	conn := waitConn(t, "127.0.0.1:"+port)
	_ = conn.Close()
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

func hangRequest(t *testing.T, conn net.Conn) func() {
	t.Helper()
	if _, err := io.WriteString(conn, "GET / HTTP/1.1\r\nHost: 127.0.0.1\r\n\r\n"); err != nil {
		t.Fatalf("write request: %v", err)
	}
	done := make(chan struct{})
	go func() {
		resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
		if err == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}
		close(done)
	}()
	return func() {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("accepted request did not finish before shutdown returned")
		}
		_ = conn.Close()
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

type readCounter struct{ n *int }

func (r readCounter) Read(p []byte) (int, error) {
	*r.n += len(p)
	for i := range p {
		p[i] = 1
	}
	return len(p), nil
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
