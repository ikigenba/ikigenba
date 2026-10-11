package cli_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	connections "github.com/ikigenba/ikigenba/connections"
	"github.com/ikigenba/ikigenba/connections/internal/cli"
	"github.com/ikigenba/ikigenba/connections/internal/providers"
	"github.com/ikigenba/ikigenba/connections/internal/store"
)

type guaranteeValue struct {
	kind, value string
	width       int
}
type guaranteeAnswer struct {
	path, method string
	status       int
	headers      http.Header
	body         []byte
}
type guaranteeCapture struct {
	mu                     sync.Mutex
	values                 []guaranteeValue
	answers                []guaranteeAnswer
	recorded, bus, results [][]byte
}

func (c *guaranteeCapture) addValue(kind, value string, width int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.values = append(c.values, guaranteeValue{kind, value, width})
}
func (c *guaranteeCapture) addJSON(dst *[][]byte, v any) error {
	b, e := json.Marshal(v)
	if e != nil {
		return e
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	*dst = append(*dst, b)
	return nil
}

type guaranteeTelemetry struct{ c *guaranteeCapture }

func (s guaranteeTelemetry) Deliver(_ context.Context, e telemetry.Event) error {
	return s.c.addJSON(&s.c.recorded, e)
}

type guaranteeBus struct{ c *guaranteeCapture }

func (s guaranteeBus) Deliver(_ context.Context, e events.Event) error {
	return s.c.addJSON(&s.c.bus, e)
}

type guaranteeTransport struct {
	c    *guaranteeCapture
	base http.RoundTripper
}

func (s guaranteeTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	res, e := s.base.RoundTrip(r)
	if e != nil {
		return nil, e
	}
	b, e := io.ReadAll(res.Body)
	_ = res.Body.Close()
	if e != nil {
		return nil, e
	}
	res.Body = io.NopCloser(bytes.NewReader(b))
	s.c.mu.Lock()
	s.c.answers = append(s.c.answers, guaranteeAnswer{r.URL.Path, r.Method, res.StatusCode, res.Header.Clone(), b})
	s.c.mu.Unlock()
	return res, nil
}
func guaranteeMarked(t *testing.T, s string) string {
	t.Helper()
	if len(s) < 16 {
		t.Fatal("marked value too short")
	}
	for _, b := range []byte(s) {
		if (b < 'a' || b > 'z') && (b < 'A' || b > 'Z') && (b < '0' || b > '9') {
			t.Fatal("marked value is not ASCII alphanumeric")
		}
	}
	for i := 0; i+8 <= len(s); i++ {
		valid := false
		for _, b := range []byte(s[i : i+8]) {
			if b >= 'g' && b <= 'z' || b >= 'G' && b <= 'Z' {
				valid = true
			}
		}
		if !valid {
			t.Fatal("marked value has an unmarked window")
		}
	}
	return s
}
func guaranteeCheck(t *testing.T, channel string, b []byte, values []guaranteeValue, allow func(guaranteeValue) bool) {
	t.Helper()
	lower := strings.ToLower(string(b))
	for _, v := range values {
		if allow != nil && allow(v) {
			continue
		}
		secret := strings.ToLower(v.value)
		for i := 0; i+v.width <= len(secret); i++ {
			if strings.Contains(lower, secret[i:i+v.width]) {
				t.Errorf("%s leaked a %s window at offset %d", channel, v.kind, i)
				break
			}
		}
	}
}

// R-0Y6K-XN5C: each chosen credential is validated and enters only its designated boundary.
// The supplemental isolation matrix uses deterministic randomness and samples outbound state and verifier values.
func TestRunMarkedCredentialsIsolationMatrix(t *testing.T) {
	t.Setenv(services.Variable, "")
	// The wall-clock deadline is only a deadlock guard, never the application clock.
	watchdog, stopWatchdog := context.WithTimeout(t.Context(), 15*time.Second)
	defer stopWatchdog()
	var entropy []byte
	for i := range 1024 {
		block := sha256.Sum256([]byte(fmt.Sprintf("isolation-run-%d", i)))
		entropy = append(entropy, block[:]...)
	}
	var c guaranteeCapture
	secret := guaranteeMarked(t, "GsecretHsecretIsecretJsecretKsecret")
	c.addValue("client secret", secret, 8)
	var clock atomic.Int64
	clock.Store(time.Date(2034, 3, 4, 5, 6, 7, 0, time.UTC).Unix())
	now := func() time.Time { return time.Unix(clock.Load(), 0).UTC() }
	var modeMu sync.Mutex
	exchangeMode, refreshMode, revokeMode := "ok", "ok", "ok"
	setMode := func(which, mode string) {
		modeMu.Lock()
		defer modeMu.Unlock()
		switch which {
		case "exchange":
			exchangeMode = mode
		case "refresh":
			refreshMode = mode
		case "revoke":
			revokeMode = mode
		}
	}
	var seq atomic.Int64
	token := func(kind string) string {
		pattern := "GaccessHaccessIaccessJaccessKaccess"
		if kind == "refresh token" {
			pattern = "GrefreshHrefreshIrefreshJrefreshKrefresh"
		}
		s := guaranteeMarked(t, fmt.Sprintf("%s%d", pattern, seq.Add(1)))
		c.addValue(kind, s, 8)
		return s
	}
	provider := providers.Table()[0]
	provider.Name = "isolation"
	provider.DisplayName = "Isolation provider"
	provider.ClientIDVar = "ISOLATION_ID"
	provider.ClientSecretVar = "ISOLATION_SECRET"
	grantedScopes := make([]string, len(provider.Scopes))
	for i, scope := range provider.Scopes {
		grantedScopes[i] = provider.GrantedName(scope)
	}
	var seenMu sync.Mutex
	seen := map[string]int{}
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if e := r.ParseForm(); e != nil {
			t.Error(e)
			w.WriteHeader(500)
			return
		}
		modeMu.Lock()
		mode := exchangeMode
		kind := "exchange"
		if r.URL.Path == "/revoke" {
			mode = revokeMode
			kind = "revoke"
		} else if r.Form.Get("grant_type") == "refresh_token" {
			mode = refreshMode
			kind = "refresh"
		}
		modeMu.Unlock()
		seenMu.Lock()
		seen[kind+":"+mode]++
		seenMu.Unlock()
		if kind == "exchange" {
			code := r.Form.Get("code")
			guaranteeMarked(t, code)
			c.addValue("code", code, 8)
			v := r.Form.Get("code_verifier")
			if len(v) < 43 {
				t.Error("missing outbound verifier")
			}
			c.addValue("verifier", v, 16)
		}
		if kind != "revoke" && (r.Form.Get("client_secret") != secret || r.Form.Get("client_id") != "isolation-client") {
			t.Error("provider missing supplied client credentials")
		}
		w.Header().Set("Content-Type", "application/json")
		switch mode {
		case "refuse":
			w.WriteHeader(400)
		case "fail":
			w.WriteHeader(503)
		default:
			if kind == "revoke" {
				_, _ = io.WriteString(w, `{}`)
				return
			}
			account := map[string]string{"sub": "isolation-subject", "email": "outside@example.test"}
			if mode == "no-email" {
				delete(account, "email")
			}
			payload, _ := json.Marshal(account)
			id := "head." + base64.RawURLEncoding.EncodeToString(payload) + ".sig"
			_ = json.NewEncoder(w).Encode(map[string]any{"access_token": token("access token"), "refresh_token": token("refresh token"), "expires_in": 3600, "scope": strings.Join(grantedScopes, " "), "id_token": id})
			return
		}
		// Provider failure detail is not another input boundary for a marked credential.
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "provider_failure", "error_description": "provider declined the operation"})
	}))
	defer fake.Close()
	provider.AuthorizeURL = fake.URL + "/authorize"
	provider.TokenURL = fake.URL + "/token"
	provider.RevokeURL = fake.URL + "/revoke"

	listener, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = listener.Close() }()
	dir := t.TempDir()
	notifyDir, e := os.MkdirTemp("/tmp", "con-ready-")
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = os.RemoveAll(notifyDir) }()
	notifyPath := filepath.Join(notifyDir, "ready.sock")
	notify, e := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: notifyPath, Net: "unixgram"})
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = notify.Close() }()
	deadline, _ := watchdog.Deadline()
	if e = notify.SetReadDeadline(deadline); e != nil {
		t.Fatal(e)
	}
	ready := make(chan error, 1)
	go func() {
		b := make([]byte, 128)
		n, _, err := notify.ReadFromUnix(b)
		if err == nil && string(b[:n]) != "READY=1" {
			err = fmt.Errorf("unexpected readiness message %q", b[:n])
		}
		ready <- err
	}()
	env := map[string]string{"ISOLATION_ID": "isolation-client", "ISOLATION_SECRET": secret, "IKIGENBA_CALLBACK_URL": "http://localhost:8123", "LISTEN_PID": "177", "LISTEN_FDS": "1", "NOTIFY_SOCKET": notifyPath}
	var stderr, stdout bytes.Buffer
	ctx, cancel := context.WithCancel(watchdog)
	defer cancel()
	done := make(chan int, 1)
	p := cli.Process{Version: "test-display", LookupEnv: func(k string) (string, bool) { v, ok := env[k]; return v, ok }, Pid: 177, Inherit: func(uintptr) (net.Listener, error) { return listener, nil }, Dir: dir, Now: now, Rand: bytes.NewReader(entropy), Stdout: &stdout, Stderr: &stderr, Sink: guaranteeTelemetry{&c}, EventSink: guaranteeBus{&c}, Providers: []providers.Provider{provider}, HTTP: fake.Client(), Sleep: func(context.Context, time.Duration) {}, Banner: func(page.User) page.Banner { return page.Banner{} }, MCP: func(w *telemetry.Writer) *mcp.Server {
		return mcp.NewServer(mcp.ServerConfig{Name: "connections", Version: "test-display", Telemetry: w})
	}}
	go func() { done <- cli.Run(ctx, p) }()
	stopped := false
	defer func() {
		if !stopped {
			cancel()
			select {
			case <-done:
			case <-watchdog.Done():
				t.Error("run cleanup exceeded deadline")
			}
		}
	}()
	select {
	case <-watchdog.Done():
		t.Fatal("run readiness exceeded deadline")
	case err := <-ready:
		if err != nil {
			t.Fatal(err)
		}
	case code := <-done:
		stopped = true
		t.Fatalf("run exited before readiness: %d: %s", code, stderr.String())
	}
	base := "http://" + listener.Addr().String()
	client := &http.Client{Timeout: 10 * time.Second, Transport: guaranteeTransport{&c, http.DefaultTransport}, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	request := func(user, method, path string, want int) *http.Response {
		t.Helper()
		r, err := http.NewRequestWithContext(ctx, method, base+path, nil)
		if err != nil {
			t.Fatal(err)
		}
		r.Header.Set("X-User-Id", user)
		r.Header.Set("X-User-Email", user+"@example.test")
		if method == http.MethodPost {
			r.Header.Set("Origin", base)
			r.Header.Set("X-Forwarded-Proto", "http")
		}
		res, err := client.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = io.Copy(io.Discard, res.Body)
		_ = res.Body.Close()
		if res.StatusCode != want {
			t.Fatalf("%s %s (%s): status %d want %d", method, path, user, res.StatusCode, want)
		}
		return res
	}
	connect := func(user, method string) string {
		res := request(user, method, "/isolation/connect", 302)
		u, err := url.Parse(res.Header.Get("Location"))
		if err != nil {
			t.Fatal(err)
		}
		state := u.Query().Get("state")
		suffix := strings.TrimPrefix(state, "connections.")
		if len(suffix) != 32 || suffix == state {
			t.Fatal("invalid outbound state")
		}
		if _, err = hex.DecodeString(suffix); err != nil {
			t.Fatal(err)
		}
		c.addValue("state", suffix, 16)
		return state
	}
	callback := func(user, state, mode string, want int) {
		setMode("exchange", mode)
		code := guaranteeMarked(t, fmt.Sprintf("GcodeHcodeIcodeJcodeKcodeLcode%d", seq.Add(1)))
		request(user, "GET", "/isolation/callback?"+url.Values{"state": {state}, "code": {code}}.Encode(), want)
	}
	mc := mcp.NewClient(mcp.ClientConfig{Endpoint: base + "/mcp", HTTPClient: client})
	call := func(user, name string) {
		args := json.RawMessage(`{}`)
		if name != "list" {
			args = json.RawMessage(`{"provider":"isolation"}`)
		}
		result, err := mc.CallTool(ctx, identity.Caller{UserID: user, Email: user + "@example.test"}, name, args)
		if err != nil {
			t.Fatal(err)
		}
		if result.IsError() {
			t.Fatalf("%s unexpectedly refused", name)
		}
		if err = c.addJSON(&c.results, result); err != nil {
			t.Fatal(err)
		}
	}
	seed := func(user string, broken bool) {
		d, err := db.Open(ctx, db.Config{Path: filepath.Join(dir, "state", "connections.db"), Migrations: connections.Migrations(), Now: now, Stderr: &stderr, Service: "connections"})
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = d.Close() }()
		s := store.New(d, store.Config{Now: now, Rand: bytes.NewReader(bytes.Repeat([]byte{0x75}, 256))})
		_, _, err = s.Upsert(ctx, store.Connection{UserID: user, UserEmail: user + "@example.test", Provider: "isolation", AccountEmail: "seed@example.test", AccountSubject: "seed-subject", AccessToken: token("access token"), RefreshToken: token("refresh token"), AccessExpires: now().Add(-time.Minute), Scopes: grantedScopes})
		if err != nil {
			t.Fatal(err)
		}
		if broken {
			if _, err = s.MarkBroken(ctx, user, "isolation"); err != nil {
				t.Fatal(err)
			}
		}
	}
	providerRequests := func(kind string) int {
		seenMu.Lock()
		defer seenMu.Unlock()
		count := 0
		for key, n := range seen {
			if strings.HasPrefix(key, kind+":") {
				count += n
			}
		}
		return count
	}
	tokenRequest := func(user string, want int, refresh bool) {
		t.Helper()
		before := providerRequests("refresh")
		request(user, "GET", "/token/isolation", want)
		delta := providerRequests("refresh") - before
		expected := 0
		if refresh {
			expected = 1
		}
		if delta != expected {
			t.Fatalf("token outcome %d: provider refresh calls %d want %d", want, delta, expected)
		}
	}
	for _, user := range []string{"one", "two"} {
		other := "two"
		if user == "two" {
			other = "one"
		}
		connect(user, "GET")
		connect(user, "POST")
		request(user, "GET", "/isolation/callback", 400)
		request(user, "GET", "/isolation/callback?state=unknown", 400)
		expired := connect(user, "GET")
		clock.Add(601)
		callback(user, expired, "ok", 400)
		foreign := connect(other, "GET")
		callback(user, foreign, "ok", 400)
		denied := connect(user, "GET")
		request(user, "GET", "/isolation/callback?"+url.Values{"state": {denied}, "error": {"access_denied"}}.Encode(), 400)
		for _, mode := range []string{"refuse", "fail", "no-email", "ok"} {
			want := 502
			if mode == "ok" {
				want = 302
			}
			callback(user, connect(user, "GET"), mode, want)
		}
		tokenRequest(user, 200, false)
		clock.Add(3601)
		setMode("refresh", "ok")
		tokenRequest(user, 200, true)
		clock.Add(3601)
		setMode("refresh", "fail")
		tokenRequest(user, 502, true)
		setMode("refresh", "refuse")
		tokenRequest(user, 409, true)
		seed(user, true)
		tokenRequest(user, 409, false)
		callback(user, connect(user, "GET"), "ok", 302) // reconnect the broken grant
		call(user, "list")
		call(user, "connect")
		for _, path := range []string{"/", "/about", "/tools", "/missing-isolation-page"} {
			want := 200
			if path == "/missing-isolation-page" {
				want = 404
			}
			request(user, "GET", path, want)
		}
		for _, mode := range []string{"refuse", "fail"} {
			setMode("revoke", mode)
			request(user, "POST", "/isolation/disconnect", 302)
			callback(user, connect(user, "GET"), "ok", 302)
		}
		setMode("revoke", "ok")
		call(user, "disconnect")
		call(user, "list")
	}
	cancel()
	var code int
	select {
	case code = <-done:
		stopped = true
	case <-watchdog.Done():
		t.Fatal("run shutdown exceeded deadline")
	}
	if code != 0 {
		t.Fatalf("run stopped with %d: %s", code, stderr.String())
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.recorded) == 0 || len(c.bus) == 0 || len(c.results) == 0 || len(c.answers) == 0 {
		t.Fatal("isolation channels were not exercised")
	}
	guaranteeCheck(t, "stderr", stderr.Bytes(), c.values, nil)
	for _, b := range c.recorded {
		guaranteeCheck(t, "telemetry", b, c.values, nil)
	}
	for _, b := range c.bus {
		guaranteeCheck(t, "bus", b, c.values, nil)
	}
	for _, b := range c.results {
		guaranteeCheck(t, "tool result", b, c.values, nil)
	}
	for _, a := range c.answers {
		allowedAccess := a.method == "GET" && a.path == "/token/isolation" && a.status == 200
		guaranteeCheck(t, "HTTP body", a.body, c.values, func(v guaranteeValue) bool { return allowedAccess && v.kind == "access token" })
		for name, vs := range a.headers {
			guaranteeCheck(t, "HTTP header name", []byte(name), c.values, nil)
			allowedState := strings.EqualFold(name, "Location") && a.path == "/isolation/connect"
			for _, v := range vs {
				guaranteeCheck(t, "HTTP header "+name, []byte(v), c.values, func(s guaranteeValue) bool {
					return allowedAccess && s.kind == "access token" || allowedState && s.kind == "state"
				})
			}
		}
	}
	seenMu.Lock()
	defer seenMu.Unlock()
	for _, key := range []string{"exchange:refuse", "exchange:fail", "exchange:no-email", "exchange:ok", "refresh:ok", "refresh:fail", "refresh:refuse", "revoke:refuse", "revoke:fail", "revoke:ok"} {
		if seen[key] < 2 {
			t.Errorf("provider outcome %s not exercised for both users", key)
		}
	}
}
