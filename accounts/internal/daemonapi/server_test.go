package daemonapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/ikigenba/ikigenba/accounts"
	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
	"github.com/ikigenba/ikigenba/accounts/internal/hostauth"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/accounts/internal/trail"
	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
)

const (
	fixtureRegion   = "us-east-2"
	fixtureAccount  = "012345678901"
	fixtureRole     = "hosts-role"
	fixtureInstance = "i-01234567"
)

var fixtureNow = time.Date(2026, 11, 17, 18, 0, 0, 0, time.UTC)

type fixtureSTS struct {
	instance string
	err      error
}

func (s fixtureSTS) CallerOf(context.Context, string, string) (cloud.Caller, error) {
	return cloud.Caller{
		Account: fixtureAccount,
		Arn:     "arn:aws:sts::" + fixtureAccount + ":assumed-role/" + fixtureRole + "/" + s.instance,
		UserID:  "AROAEXAMPLE:" + s.instance,
	}, s.err
}

func (fixtureSTS) Account(context.Context) (string, error) { return fixtureAccount, nil }

type requests struct{ elapsed atomic.Int64 }

func (r *requests) now() time.Time {
	return fixtureNow.Add(time.Duration(r.elapsed.Load()) * time.Second)
}

func (r *requests) verifier(sts cloud.STS) *hostauth.Verifier {
	return hostauth.NewVerifier(hostauth.VerifierConfig{
		Region: fixtureRegion,
		Role:   fixtureRole,
		Binding: func() (string, error) {
			return "accounts.example", nil
		},
		STS: sts,
		Now: r.now,
	})
}

func (r *requests) signed(ctx context.Context, method, path string, body io.Reader) *http.Request {
	n := r.elapsed.Add(1)
	return r.request(ctx, method, path, body, fixtureNow.Add(time.Duration(n)*time.Second), n)
}

func (r *requests) request(ctx context.Context, method, path string, body io.Reader, date time.Time, signature int64) *http.Request {
	req := httptest.NewRequestWithContext(ctx, method, path, body)
	query := url.Values{
		"Action":              {"GetCallerIdentity"},
		"Version":             {"2011-06-15"},
		"X-Amz-Algorithm":     {"AWS4-HMAC-SHA256"},
		"X-Amz-SignedHeaders": {"host;" + strings.ToLower(hostauth.BindingHeader)},
		"X-Amz-Expires":       {"60"},
		"X-Amz-Date":          {date.Format("20060102T150405Z")},
		"X-Amz-Signature":     {fmt.Sprintf("%064x", signature)},
	}
	raw := "https://sts." + fixtureRegion + ".amazonaws.com/?" + query.Encode()
	req.Header.Set(hostauth.HeaderName, base64.RawURLEncoding.EncodeToString([]byte(raw)))
	return req
}

func fixtureStore(t *testing.T) (*db.DB, *store.Store, store.Host) {
	t.Helper()
	d, err := db.Open(t.Context(), db.Config{Path: filepath.Join(t.TempDir(), "db"), Migrations: accounts.Migrations(), Stderr: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := d.Close(); err != nil {
			t.Error(err)
		}
	})
	s := store.New(d, store.Config{
		Now:  func() time.Time { return fixtureNow },
		Rand: strings.NewReader("12345678abcdefghIJKLMNOPqrstuvwxABCDEFGHijklmnop"),
	})
	return d, s, provisionFixtureHost(t, s, fixtureInstance)
}

func provisionFixtureHost(t *testing.T, s *store.Store, instance string) store.Host {
	t.Helper()
	h, op, err := s.CreateHost(t.Context(), store.HostSpec{Type: "t4g.medium", RootGB: 20, DataGB: 50}, identity.Caller{UserID: "test-user"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Finish(t.Context(), op.ID, store.Update{InstanceID: instance}); err != nil {
		t.Fatal(err)
	}
	h, err = s.Host(t.Context(), h.Name)
	if err != nil {
		t.Fatal(err)
	}
	return h
}

func server(t *testing.T, cfg Config) *Server {
	t.Helper()
	s, err := New(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Stop)
	return s
}

func invoke(s *Server, req *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	s.ServeHTTP(w, req)
	return w
}

func expectError(t *testing.T, w *httptest.ResponseRecorder, status int, message string) {
	t.Helper()
	if w.Code != status || w.Body.String() != message+"\n" || w.Header().Get("Content-Type") != "text/plain; charset=utf-8" {
		t.Fatalf("response = %d %v %q, want %d plain text %q", w.Code, w.Header(), w.Body.String(), status, message)
	}
	if status == http.StatusUnauthorized && w.Header().Get("WWW-Authenticate") != "Accountsd-Identity" {
		t.Fatalf("401 challenge = %q", w.Header().Get("WWW-Authenticate"))
	}
}

func writeBinary(t *testing.T, file string, content []byte) {
	t.Helper()
	if err := os.WriteFile(file, content, 0600); err != nil {
		t.Fatal(err)
	}
}

// R-E0OG-WWIF, R-R5X9-LMQP, R-R755-ZEHE, R-YICM-8FJ0, R-YJKI-M79P.
func TestSurfaceAndBinaryStartup(t *testing.T) {
	_ = struct {
		new    func(context.Context, Config) (*Server, error)
		stop   func(*Server)
		digest func(*Server) string
		http   http.Handler
	}{New, (*Server).Stop, (*Server).Digest, (*Server)(nil)}
	_ = struct {
		Store      *store.Store
		Verifier   *hostauth.Verifier
		Binary     string
		Telemetry  *telemetry.Writer
		Now        func() time.Time
		Configured bool
		After      func(time.Duration) <-chan time.Time
		BoxUser    http.Handler
		BoxAdmin   http.Handler
		Domain     string
	}(Config{})
	const _ int8 = MaxBoxServing
	const _ uint16 = MaxBoxServing
	if MaxWaiters != 1000 || CopyBuffer != 32<<10 || MaxBoxServing != 32 {
		t.Fatal("unexpected HTTP limits")
	}
	seen := map[string]bool{store.Unreachable: true}
	for _, copy := range []string{NotFound, NotAllowed, BadRequest, Refused, UnknownHost, TooLarge, Busy, NotUser} {
		if copy == "" || strings.ContainsAny(copy, "\r\n") || seen[copy] {
			t.Fatalf("invalid or duplicate error copy %q", copy)
		}
		seen[copy] = true
	}
	dir := t.TempDir()
	for _, name := range []string{"", dir, filepath.Join(dir, "missing")} {
		if digest := server(t, Config{Binary: name}).Digest(); digest != "" {
			t.Errorf("Digest(%q) = %q, want empty", name, digest)
		}
	}
	file := filepath.Join(dir, "accountsd")
	data := []byte("fixture binary")
	writeBinary(t, file, data)
	want, err := protocol.Digest(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if got := server(t, Config{Binary: file}).Digest(); got != want {
		t.Errorf("digest = %q, want %q", got, want)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := New(ctx, Config{Binary: file}); err == nil {
		t.Error("canceled digest succeeded")
	}
}

// R-YDH0-PCK8, R-YEOX-34AX, R-RC0R-IHG6, R-REGK-A0XK, R-YFWT-GW1M.
func TestRoutingAndMethodRefusals(t *testing.T) {
	unconfigured := server(t, Config{})
	configured := server(t, Config{Configured: true})
	for _, path := range []string{protocol.BinaryPath, protocol.StatePath, protocol.ReportPath, protocol.BoxPath, "/accountsd/", "/accountsd/v1/state/", "/accountsd/v1", "/accountsd/v2/state"} {
		for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost, http.MethodDelete} {
			w := invoke(unconfigured, httptest.NewRequestWithContext(t.Context(), method, path+"?after=bogus", neverRead{}))
			expectError(t, w, http.StatusNotFound, NotFound)
		}
	}
	for _, path := range []string{"/accountsd/", "/accountsd/v1/state/", "/accountsd/v1", "/accountsd/v2/state", protocol.BoxPath + "/"} {
		w := invoke(configured, httptest.NewRequestWithContext(t.Context(), http.MethodPost, path, neverRead{}))
		expectError(t, w, http.StatusNotFound, NotFound)
	}
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		w := invoke(configured, httptest.NewRequestWithContext(t.Context(), method, protocol.BinaryPath, neverRead{}))
		expectError(t, w, http.StatusNotFound, NotFound)
	}
	for _, tt := range []struct{ path, method, allow string }{
		{protocol.BinaryPath, http.MethodPost, "GET, HEAD"},
		{protocol.StatePath, http.MethodHead, "GET"},
		{protocol.ReportPath, http.MethodGet, "POST"},
	} {
		w := invoke(configured, httptest.NewRequestWithContext(t.Context(), tt.method, tt.path, neverRead{}))
		expectError(t, w, http.StatusMethodNotAllowed, NotAllowed)
		if got := w.Header().Get("Allow"); got != tt.allow {
			t.Fatalf("Allow = %q, want %q", got, tt.allow)
		}
	}
}

type neverRead struct{}

func (neverRead) Read([]byte) (int, error) { panic("request body must be unread") }

type routedPacket struct {
	writer *httptest.ResponseRecorder
	sent   chan int
	resume chan struct{}
}

func (w *routedPacket) Header() http.Header    { return w.writer.Header() }
func (w *routedPacket) WriteHeader(status int) { w.writer.WriteHeader(status) }
func (w *routedPacket) Write(p []byte) (int, error) {
	n, err := w.writer.Write(p)
	w.sent <- n
	<-w.resume
	return n, err
}

// R-GIIR-DJLT, R-RD8N-W96V.
func TestIndependentBinaryDownloads(t *testing.T) {
	data := bytes.Repeat([]byte("fixture download"), (1<<20)/len("fixture download"))
	dir := t.TempDir()
	file := filepath.Join(dir, "accountsd")
	writeBinary(t, file, data)
	s := server(t, Config{Configured: true, Binary: file})
	req := func(method string) *http.Request {
		r := httptest.NewRequestWithContext(t.Context(), method, protocol.BinaryPath+"?random=value", nil)
		r.Header.Set("Range", "bytes=1-2")
		r.Header.Set(hostauth.HeaderName, "ignored")
		return r
	}

	replacement := filepath.Join(dir, "replacement")
	writeBinary(t, replacement, []byte("different binary"))
	if err := os.Rename(replacement, file); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(file); err != nil {
		t.Fatal(err)
	}
	packet := func() *routedPacket {
		w := &routedPacket{writer: httptest.NewRecorder(), sent: make(chan int), resume: make(chan struct{})}
		go func() {
			s.ServeHTTP(w, req(http.MethodGet))
			close(w.sent)
		}()
		return w
	}
	left, right := packet(), packet()
	for {
		l, lOpen := <-left.sent
		r, rOpen := <-right.sent
		if lOpen {
			left.resume <- struct{}{}
		}
		if rOpen {
			right.resume <- struct{}{}
		}
		if lOpen != rOpen || l > CopyBuffer || r > CopyBuffer {
			t.Errorf("interleaved writes = %d/%t and %d/%t", l, lOpen, r, rOpen)
		}
		if !lOpen && !rOpen {
			break
		}
	}
	plain := invoke(s, req(http.MethodGet))
	head := invoke(s, req(http.MethodHead))
	for _, w := range []*httptest.ResponseRecorder{left.writer, right.writer, plain, head} {
		want := data
		if w == head {
			want = nil
		}
		h := w.Header()
		if w.Code != http.StatusOK || h.Get("Content-Type") != protocol.ContentBinary ||
			h.Get("Content-Length") != strconv.Itoa(len(data)) || h.Get("Location") != "" || !bytes.Equal(w.Body.Bytes(), want) {
			t.Fatalf("download %d %v (%d bytes), wanted correct headers and %d bytes", w.Code, h, w.Body.Len(), len(want))
		}
	}
}

// R-RGWD-1KEY, R-RI49-FC5N.
func TestBadQueriesAndAuthenticationPrecedeReads(t *testing.T) {
	s := server(t, Config{Configured: true})
	for _, after := range []string{"", "-1", "%2b1", "1.0", "a", "%201", "%xx", "9223372036854775808", "1&after=1"} {
		w := invoke(s, httptest.NewRequestWithContext(t.Context(), http.MethodGet, protocol.StatePath+"?after="+after, nil))
		expectError(t, w, http.StatusBadRequest, BadRequest)
	}
	stamps := new(requests)
	for _, tt := range []struct {
		name string
		sts  cloud.STS
		edit func(*http.Request)
		want int
		copy string
	}{
		{"missing header", fixtureSTS{}, func(r *http.Request) { r.Header.Del(hostauth.HeaderName) }, http.StatusUnauthorized, Refused},
		{"stale", fixtureSTS{}, func(r *http.Request) {
			stale := stamps.request(r.Context(), http.MethodPost, protocol.ReportPath, neverRead{}, fixtureNow.Add(-hostauth.Freshness), 9000)
			r.Header.Set(hostauth.HeaderName, stale.Header.Get(hostauth.HeaderName))
		}, http.StatusUnauthorized, Refused},
		{"refused", fixtureSTS{err: &cloud.Error{Class: cloud.ClassRefused}}, nil, http.StatusUnauthorized, Refused},
		{"unavailable", fixtureSTS{err: &cloud.Error{Class: cloud.ClassTransient}}, nil, http.StatusServiceUnavailable, Busy},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := server(t, Config{Configured: true, Verifier: stamps.verifier(tt.sts)})
			r := stamps.signed(t.Context(), http.MethodPost, protocol.ReportPath, neverRead{})
			if tt.edit != nil {
				tt.edit(r)
			}
			expectError(t, invoke(s, r), tt.want, tt.copy)
		})
	}
	replayed := stamps.verifier(fixtureSTS{instance: fixtureInstance})
	request := stamps.signed(t.Context(), http.MethodPost, protocol.ReportPath, neverRead{})
	if _, err := replayed.Verify(t.Context(), request.Header.Get(hostauth.HeaderName)); err != nil {
		t.Fatal(err)
	}
	s = server(t, Config{Configured: true, Verifier: replayed})
	expectError(t, invoke(s, request), http.StatusUnauthorized, Refused)
}

// R-RJC5-T3WC.
func TestHostLookupRefusals(t *testing.T) {
	d, st, _ := fixtureStore(t)
	stamps := new(requests)
	missing := server(t, Config{Configured: true, Store: st, Verifier: stamps.verifier(fixtureSTS{instance: "i-87654321"})})
	expectError(t, invoke(missing, stamps.signed(t.Context(), http.MethodGet, protocol.StatePath, nil)), http.StatusForbidden, UnknownHost)
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	unreachable := server(t, Config{Configured: true, Store: st, Verifier: stamps.verifier(fixtureSTS{instance: fixtureInstance})})
	expectError(t, invoke(unreachable, stamps.signed(t.Context(), http.MethodGet, protocol.StatePath, nil)), http.StatusServiceUnavailable, store.Unreachable)
}

func expectState(t *testing.T, w *httptest.ResponseRecorder, want protocol.DesiredState) {
	t.Helper()
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != protocol.ContentJSON || w.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("state response = %d %v", w.Code, w.Header())
	}
	raw, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(bytes.TrimSpace(w.Body.Bytes()), raw) {
		t.Fatalf("state = %s, want %s", w.Body.Bytes(), raw)
	}
}

func pollingRequest(host store.Host, digest string) string {
	return protocol.StatePath + "?" + protocol.QueryAfter + "=" + strconv.FormatInt(host.Version, 10) + "&" + protocol.QueryDigest + "=" + url.QueryEscape(digest)
}

func desired(t *testing.T, st *store.Store, name, digest string) protocol.DesiredState {
	t.Helper()
	state, err := st.Desired(t.Context(), name)
	if err != nil {
		t.Fatal(err)
	}
	state.Daemon = digest
	return state
}

type desiredWithDomain struct {
	*store.Store
	domain string
}

func (s desiredWithDomain) Desired(ctx context.Context, name string) (protocol.DesiredState, error) {
	state, err := s.Store.Desired(ctx, name)
	state.Domain = s.domain
	return state, err
}

// R-E1WD-AO94, R-RGWD-1KEY.
func TestImmediateState(t *testing.T) {
	_, st, host := fixtureStore(t)
	stamps := new(requests)
	file := filepath.Join(t.TempDir(), "accountsd")
	writeBinary(t, file, []byte("fixture"))
	const domain = "hosts.example.com"
	s := server(t, Config{
		Store: st, Verifier: stamps.verifier(fixtureSTS{instance: fixtureInstance}), Configured: true, Binary: file, Domain: domain,
		After: func(time.Duration) <-chan time.Time {
			t.Fatal("immediate state request waited")
			return nil
		},
	})
	s.store = desiredWithDomain{st, "unwanted.example.com"}
	want := desired(t, st, host.Name, s.Digest())
	want.Domain = domain
	for _, path := range []string{
		protocol.StatePath,
		pollingRequest(host, "previous-digest"),
		protocol.StatePath + "?after=9223372036854775807&digest=" + url.QueryEscape(s.Digest()),
	} {
		expectState(t, invoke(s, stamps.signed(t.Context(), http.MethodGet, path, nil)), want)
	}
	s = server(t, Config{
		Store: st, Verifier: stamps.verifier(fixtureSTS{instance: fixtureInstance}), Configured: true,
		After: func(time.Duration) <-chan time.Time {
			t.Fatal("an empty daemon digest delayed a mismatched version")
			return nil
		},
	})
	s.store = desiredWithDomain{st, "unwanted.example.com"}
	expectState(t, invoke(s, stamps.signed(t.Context(), http.MethodGet, protocol.StatePath, nil)), desired(t, st, host.Name, ""))
}

func startPoll(t *testing.T, s *Server, req *http.Request, waits <-chan time.Duration) <-chan *httptest.ResponseRecorder {
	t.Helper()
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- invoke(s, req) }()
	waitForPoll(t, waits, done)
	return done
}

func waitForPoll(t *testing.T, waits <-chan time.Duration, done <-chan *httptest.ResponseRecorder) {
	t.Helper()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	select {
	case duration := <-waits:
		if duration != protocol.LongPoll {
			t.Fatalf("wait duration = %v, want %v", duration, protocol.LongPoll)
		}
	case w := <-done:
		t.Fatalf("poll finished before waiting: %d %s", w.Code, w.Body.String())
	case <-deadline.C:
		t.Fatal("poll did not reach its timer")
	}
	select {
	case w := <-done:
		t.Fatalf("poll finished early: %d %s", w.Code, w.Body.String())
	default:
	}
}

// R-E349-OFZT, R-E7ZV-7IYL.
func TestPollEndsOnWriteTimeoutCancelOrStop(t *testing.T) {
	_, st, host := fixtureStore(t)
	stamps := new(requests)
	wait := make(chan time.Duration, 1)
	timer := make(chan time.Time, 1)
	const domain = "hosts.example.com"
	s := server(t, Config{
		Store: st, Configured: true, Verifier: stamps.verifier(fixtureSTS{instance: fixtureInstance}), Domain: domain,
		After: func(d time.Duration) <-chan time.Time { wait <- d; return timer },
	})
	s.store = desiredWithDomain{st, "unwanted.example.com"}
	poll := func(ctx context.Context) <-chan *httptest.ResponseRecorder {
		return startPoll(t, s, stamps.signed(ctx, http.MethodGet, pollingRequest(host, "ignored-with-no-binary"), nil), wait)
	}

	owner := identity.Caller{UserID: "test-user"}
	if _, _, err := st.PlaceAccount(t.Context(), "acme", host.Name, owner); err != nil {
		t.Fatal(err)
	}
	var err error
	host, err = st.Host(t.Context(), host.Name)
	if err != nil {
		t.Fatal(err)
	}
	writing := poll(t.Context())
	u, err := st.AddUser(t.Context(), "acme", "daffy", store.Key{Type: "ssh-ed25519", Body: "fixture-key", Line: "ssh-ed25519 fixture-key"}, owner)
	if err != nil {
		t.Fatal(err)
	}
	w := <-writing
	state := desired(t, st, host.Name, "")
	state.Domain = domain
	expectState(t, w, state)
	if state.Version <= host.Version || len(state.Users) != 1 || state.Users[0].Name != u.Name {
		t.Fatalf("account write did not change desired state: %+v", state)
	}
	host.Version = state.Version

	timed := poll(t.Context())
	timer <- fixtureNow
	expectState(t, <-timed, state)

	ctx, cancel := context.WithCancel(t.Context())
	canceled := poll(ctx)
	cancel()
	if w := <-canceled; w.Body.Len() != 0 {
		t.Fatalf("canceled poll wrote %q", w.Body.String())
	}

	stopping := poll(t.Context())
	var stops sync.WaitGroup
	for range 2 {
		stops.Go(s.Stop)
	}
	stops.Wait()
	expectState(t, <-stopping, state)
	expectError(t, invoke(s, stamps.signed(t.Context(), http.MethodGet, pollingRequest(host, ""), nil)), http.StatusServiceUnavailable, Busy)
	expectState(t, invoke(s, stamps.signed(t.Context(), http.MethodGet, protocol.StatePath, nil)), state)
}

type blockedPollWriter struct {
	*httptest.ResponseRecorder
	entered chan struct{}
	release chan struct{}
	first   sync.Once
}

func (w *blockedPollWriter) Write(p []byte) (int, error) {
	w.first.Do(func() {
		close(w.entered)
		<-w.release
	})
	return w.ResponseRecorder.Write(p)
}

// R-E349-OFZT, R-E7ZV-7IYL.
func TestMatchingDigestPollStopsWithoutWaitingForWriter(t *testing.T) {
	_, st, host := fixtureStore(t)
	stamps := new(requests)
	file := filepath.Join(t.TempDir(), "accountsd")
	writeBinary(t, file, []byte("fixture"))
	wait := make(chan time.Duration, 1)
	s := server(t, Config{
		Store: st, Verifier: stamps.verifier(fixtureSTS{instance: fixtureInstance}), Configured: true, Binary: file,
		After: func(d time.Duration) <-chan time.Time { wait <- d; return nil },
	})
	digest := s.Digest()
	if digest == "" {
		t.Fatal("non-empty fixture binary produced no digest")
	}
	req := stamps.signed(t.Context(), http.MethodGet, pollingRequest(host, digest), nil)
	w := &blockedPollWriter{ResponseRecorder: httptest.NewRecorder(), entered: make(chan struct{}), release: make(chan struct{})}
	release := sync.OnceFunc(func() { close(w.release) })
	defer release()
	result := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		s.ServeHTTP(w, req)
		result <- w.ResponseRecorder
	}()
	waitForPoll(t, wait, result)
	select {
	case <-w.entered:
		t.Fatal("matching state was written before Stop")
	default:
	}
	stopped := make(chan struct{})
	go func() {
		s.Stop()
		close(stopped)
	}()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	select {
	case <-w.entered:
	case <-deadline.C:
		t.Fatal("Stop did not wake the matching poll")
	}
	select {
	case <-stopped:
	case <-deadline.C:
		t.Fatal("Stop waited for the blocked response write")
	}
	release()
	select {
	case w := <-result:
		expectState(t, w, desired(t, st, host.Name, digest))
	case <-deadline.C:
		t.Fatal("poll did not finish after releasing the response write")
	}
}

type afterHostLookup struct {
	*store.Store
	after func()
}

func (s afterHostLookup) HostByInstance(ctx context.Context, instance string) (store.Host, error) {
	host, err := s.Store.HostByInstance(ctx, instance)
	if err == nil {
		s.after()
	}
	return host, err
}

// R-E349-OFZT.
func TestPollSeesWriteBetweenLookupAndWatch(t *testing.T) {
	_, st, host := fixtureStore(t)
	stamps := new(requests)
	s := server(t, Config{
		Store: st, Configured: true, Verifier: stamps.verifier(fixtureSTS{instance: fixtureInstance}),
		After: func(time.Duration) <-chan time.Time {
			t.Fatal("poll waited after the host version changed")
			return nil
		},
	})
	s.store = afterHostLookup{st, func() {
		if _, err := st.Allow(t.Context(), host.Name, "192.0.2.0/24"); err != nil {
			t.Fatal(err)
		}
	}}
	w := invoke(s, stamps.signed(t.Context(), http.MethodGet, pollingRequest(host, ""), nil))
	want := desired(t, st, host.Name, "")
	if want.Version != host.Version+1 || !slices.Equal(want.Allowlist, []string{"192.0.2.0/24"}) {
		t.Fatalf("allowlist write did not change desired state: %+v", want)
	}
	expectState(t, w, want)
}

// R-E349-OFZT, R-X8LV-14EK.
func TestDefaultLongPollTimer(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		_, st, host := fixtureStore(t)
		stamps := new(requests)
		s := server(t, Config{
			Store: st, Configured: true, Verifier: stamps.verifier(fixtureSTS{instance: fixtureInstance}),
		})
		ctx, cancel := context.WithTimeout(t.Context(), protocol.LongPoll+time.Second)
		defer cancel()
		req := stamps.signed(ctx, http.MethodGet, pollingRequest(host, ""), nil)
		started := time.Now()
		w := invoke(s, req)
		if elapsed := time.Since(started); elapsed != protocol.LongPoll {
			t.Fatalf("default long poll took %v, want %v", elapsed, protocol.LongPoll)
		}
		expectState(t, w, desired(t, st, host.Name, ""))
	})
}

// R-E5K2-FZH7.
func TestStateReadFailures(t *testing.T) {
	for _, tt := range []struct {
		name    string
		failure func(*testing.T, *db.DB, *store.Store, store.Host)
		code    int
		copy    string
	}{
		{"deleted", removeFixtureHost, http.StatusForbidden, UnknownHost},
		{"database closed", closeFixtureDatabase, http.StatusServiceUnavailable, store.Unreachable},
	} {
		t.Run(tt.name+" during Desired after wait", func(t *testing.T) {
			d, st, host := fixtureStore(t)
			stamps := new(requests)
			wait := make(chan time.Duration, 1)
			timer := make(chan time.Time, 1)
			s := server(t, Config{
				Store: st, Configured: true, Verifier: stamps.verifier(fixtureSTS{instance: fixtureInstance}),
				After: func(d time.Duration) <-chan time.Time { wait <- d; return timer },
			})
			result := startPoll(t, s, stamps.signed(t.Context(), http.MethodGet, pollingRequest(host, ""), nil), wait)
			tt.failure(t, d, st, host)
			timer <- fixtureNow
			expectError(t, <-result, tt.code, tt.copy)
		})
		for _, sameVersion := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s after lookup (same version: %t)", tt.name, sameVersion), func(t *testing.T) {
				d, st, host := fixtureStore(t)
				stamps := new(requests)
				s := server(t, Config{
					Store: st, Configured: true, Verifier: stamps.verifier(fixtureSTS{instance: fixtureInstance}),
					After: func(time.Duration) <-chan time.Time {
						t.Fatal("poll waited after a store failure")
						return nil
					},
				})
				s.store = afterHostLookup{st, func() { tt.failure(t, d, st, host) }}
				path := protocol.StatePath
				if sameVersion {
					path = pollingRequest(host, "")
				}
				expectError(t, invoke(s, stamps.signed(t.Context(), http.MethodGet, path, nil)), tt.code, tt.copy)
			})
		}
	}
}

func removeFixtureHost(t *testing.T, _ *db.DB, st *store.Store, host store.Host) {
	t.Helper()
	_, op, err := st.DeleteHost(t.Context(), host.Name, true, identity.Caller{UserID: "test-user"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Finish(t.Context(), op.ID, store.Update{}); err != nil {
		t.Fatal(err)
	}
}

func closeFixtureDatabase(t *testing.T, d *db.DB, _ *store.Store, _ store.Host) {
	t.Helper()
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
}

// R-E6RY-TR7W, R-X8LV-14EK.
func TestWaiterLimitDoesNotThrottleImmediateReads(t *testing.T) {
	_, st, host := fixtureStore(t)
	stamps := new(requests)
	wait := make(chan time.Duration, 1)
	s := server(t, Config{
		Store: st, Configured: true, Verifier: stamps.verifier(fixtureSTS{instance: fixtureInstance}),
		After: func(d time.Duration) <-chan time.Time { wait <- d; return nil },
	})
	pending := make([]<-chan *httptest.ResponseRecorder, 0, MaxWaiters)
	for range MaxWaiters {
		pending = append(pending, startPoll(t, s, stamps.signed(t.Context(), http.MethodGet, pollingRequest(host, ""), nil), wait))
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	expectError(t, invoke(s, stamps.signed(ctx, http.MethodGet, pollingRequest(host, ""), nil)), http.StatusServiceUnavailable, Busy)
	want := desired(t, st, host.Name, "")
	expectState(t, invoke(s, stamps.signed(ctx, http.MethodGet, protocol.StatePath, nil)), want)
	s.Stop()
	for _, result := range pending {
		expectState(t, <-result, want)
	}
}

// R-RRVG-HI37, R-X8LV-14EK.
func TestReportValidationAndRecording(t *testing.T) {
	_, st, host := fixtureStore(t)
	stamps := new(requests)
	s := server(t, Config{Store: st, Configured: true, Verifier: stamps.verifier(fixtureSTS{instance: fixtureInstance})})
	post := func(body io.Reader) *httptest.ResponseRecorder {
		return invoke(s, stamps.signed(t.Context(), http.MethodPost, protocol.ReportPath, body))
	}
	for _, tt := range []struct {
		body string
		code int
		copy string
	}{
		{strings.Repeat("x", protocol.MaxReportBytes+1), http.StatusRequestEntityTooLarge, TooLarge},
		{`{"protocol":2}`, http.StatusBadRequest, BadRequest},
		{`{"protocol":1} {}`, http.StatusBadRequest, BadRequest},
	} {
		expectError(t, post(strings.NewReader(tt.body)), tt.code, tt.copy)
	}
	if r, err := st.Report(t.Context(), host.Name); r != nil || err != nil {
		t.Fatalf("invalid report changed the store: %+v, %v", r, err)
	}
	raw := fmt.Appendf(nil, `{"protocol":%d,"attempted":%d,"applied":%d,"failed_units":["z","a"]}`, protocol.Version, host.Version, host.Version)
	first := time.Now().Truncate(time.Second)
	if w := post(bytes.NewReader(raw)); w.Code != http.StatusNoContent || w.Body.Len() != 0 {
		t.Fatalf("report = %d %q", w.Code, w.Body.String())
	}
	last := time.Now().Truncate(time.Second)
	saved, err := st.Report(t.Context(), host.Name)
	want, decodeErr := protocol.DecodeReport(bytes.NewReader(raw))
	if decodeErr != nil || err != nil || !reflect.DeepEqual(saved.Body, want) || saved.Received.Before(first) || saved.Received.After(last) {
		t.Fatalf("record = %+v, %v; want %+v between %v and %v", saved, err, want, first, last)
	}
}

// R-KGVR-VVMY.
func TestReportStoresDecodedBodyAndReceivedTime(t *testing.T) {
	_, st, host := fixtureStore(t)
	ctx := t.Context()
	owner := identity.Caller{UserID: "test-user"}
	for _, account := range []string{"acme", "zebra"} {
		if _, _, err := st.PlaceAccount(ctx, account, host.Name, owner); err != nil {
			t.Fatal(err)
		}
	}
	key := store.Key{Type: "ssh-ed25519", Body: "fixture-key", Line: "ssh-ed25519 fixture-key"}
	for _, user := range []string{"alice", "zack"} {
		if _, err := st.AddUser(ctx, "acme", user, key, owner); err != nil {
			t.Fatal(err)
		}
	}
	now := fixtureNow.Add(time.Minute + 500*time.Millisecond)
	input := protocol.Report{
		Protocol: protocol.Version, Attempted: host.Version + 1, Applied: host.Version,
		InstanceID: "reported-instance", Type: "reported-type", DaemonVersion: "test-daemon",
		Error: "fixture apply failure", FailedUnits: []string{"z.service", "a.service"},
		SignIns: []protocol.SignIn{{User: "zack", At: now.Add(time.Hour)}, {User: "nobody", At: now}, {User: "alice", At: fixtureNow}},
		Slices:  []protocol.Slice{{Account: "zebra", MemoryBytes: 12}, {Account: "other"}, {Account: "acme", MemoryBytes: 10}},
	}
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := protocol.DecodeReport(bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	filtered := decoded
	filtered.SignIns = []protocol.SignIn{decoded.SignIns[0], decoded.SignIns[2]}
	filtered.Slices = []protocol.Slice{decoded.Slices[0], decoded.Slices[2]}
	stamps := new(requests)
	w, _ := recorder(t)
	s := server(t, Config{
		Store: st, Verifier: stamps.verifier(fixtureSTS{instance: fixtureInstance}),
		Configured: true, Telemetry: w, Now: func() time.Time { return now },
	})
	recording := &recordingReportStore{Store: st}
	s.store = recording
	response := invoke(s, stamps.signed(ctx, http.MethodPost, protocol.ReportPath, bytes.NewReader(raw)))
	if response.Code != http.StatusNoContent || response.Body.Len() != 0 {
		t.Fatalf("report = %d %q", response.Code, response.Body.String())
	}
	want := &store.Report{Host: host.Name, Received: now.Truncate(time.Second), Body: filtered}
	if saved, err := st.Report(ctx, host.Name); err != nil || !reflect.DeepEqual(saved, want) {
		t.Fatalf("saved report = %+v, %v; want %+v", saved, err, want)
	}
	recording.expectCall(t, reportCall{host: host.Name, body: decoded, now: now})
}

// R-KGVR-VVMY.
func TestReportWriteFailure(t *testing.T) {
	for _, tt := range []struct {
		name    string
		failure func(*testing.T, *db.DB, *store.Store, store.Host)
		code    int
		copy    string
	}{
		{"deleted", removeFixtureHost, http.StatusForbidden, UnknownHost},
		{"database failing", func(_ *testing.T, d *db.DB, _ *store.Store, _ store.Host) {
			d.SetFailing(true)
		}, http.StatusServiceUnavailable, store.Unreachable},
	} {
		t.Run(tt.name, func(t *testing.T) {
			d, st, host := fixtureStore(t)
			stamps := new(requests)
			s := server(t, Config{
				Store: st, Configured: true, Verifier: stamps.verifier(fixtureSTS{instance: fixtureInstance}),
				Now: func() time.Time { return fixtureNow },
			})
			recording := &recordingReportStore{Store: st}
			s.store = recording
			body := readOnce{
				before: func() { tt.failure(t, d, st, host) },
				input:  strings.NewReader(fmt.Sprintf(`{"protocol":%d}`, protocol.Version)),
			}
			w := invoke(s, stamps.signed(t.Context(), http.MethodPost, protocol.ReportPath, &body))
			expectError(t, w, tt.code, tt.copy)
			recording.expectCall(t, reportCall{host: host.Name, body: protocol.Report{Protocol: protocol.Version}, now: fixtureNow})
		})
	}
}

type readOnce struct {
	before func()
	input  *strings.Reader
}

type reportCall struct {
	host string
	body protocol.Report
	now  time.Time
}

type recordingReportStore struct {
	*store.Store
	calls []reportCall
}

func (s *recordingReportStore) RecordReport(ctx context.Context, host string, body protocol.Report, now time.Time) (store.Transition, error) {
	s.calls = append(s.calls, reportCall{host: host, body: body, now: now})
	return s.Store.RecordReport(ctx, host, body, now)
}

func (s *recordingReportStore) expectCall(t *testing.T, want reportCall) {
	t.Helper()
	if !reflect.DeepEqual(s.calls, []reportCall{want}) {
		t.Fatalf("RecordReport calls = %+v, want one call with %+v", s.calls, want)
	}
}

func (r *readOnce) Read(p []byte) (int, error) {
	if r.before != nil {
		r.before()
		r.before = nil
	}
	return r.input.Read(p)
}

func recorder(t *testing.T) (*telemetry.Writer, *telemetry.Capture) {
	t.Helper()
	capture := new(telemetry.Capture)
	w := telemetry.New(telemetry.Config{
		Service: "accounts", Sink: capture, Stderr: io.Discard,
		Now: func() time.Time { return fixtureNow }, Rand: rand.NewChaCha8([32]byte{1}),
	})
	t.Cleanup(func() { w.Shutdown(context.Background(), "test stopped") })
	return w, capture
}

// R-ASMT-78PU, R-ATUP-L0GJ, R-YH4P-UNSB.
func TestReportEmitsOnlyHealthEdgesWithoutCaller(t *testing.T) {
	_, st, host := fixtureStore(t)
	stamps := new(requests)
	w, sink := recorder(t)
	received := fixtureNow.Add(store.SilentAfter + time.Second)
	s := server(t, Config{
		Store: st, Configured: true, Verifier: stamps.verifier(fixtureSTS{instance: fixtureInstance}),
		Now: func() time.Time { return received }, Telemetry: w,
	})
	silent, err := st.MarkSilent(t.Context(), received, "", 1)
	if err != nil || len(silent) != 1 {
		t.Fatalf("mark silent = %+v, %v", silent, err)
	}
	ctx := identity.NewContext(t.Context(), identity.Caller{UserID: "test-user", RequestID: "test-request"})
	for i, message := range []string{"", "", "apply failed", "apply failed again", "", "failed after recovery"} {
		r := protocol.Report{Protocol: protocol.Version, Attempted: int64(i) + host.Version, Applied: host.Version, Error: message}
		raw, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		res := invoke(s, stamps.signed(ctx, http.MethodPost, protocol.ReportPath, bytes.NewReader(raw)))
		if res.Code != http.StatusNoContent || res.Body.Len() != 0 {
			t.Fatalf("report %d = %d %q", i, res.Code, res.Body.String())
		}
		received = received.Add(time.Second)
	}
	if err := w.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	want := []struct {
		name string
		attr telemetry.Attrs
	}{
		{trail.EventHostSynced, telemetry.Attrs{"host": host.Name, "applied": host.Version}},
		{trail.EventHostApplyFailed, telemetry.Attrs{"host": host.Name, "attempted": host.Version + 2}},
		{trail.EventHostSynced, telemetry.Attrs{"host": host.Name, "applied": host.Version}},
		{trail.EventHostApplyFailed, telemetry.Attrs{"host": host.Name, "attempted": host.Version + 5}},
	}
	events := sink.Events()
	if len(events) != len(want) {
		t.Fatalf("events = %+v; want %d", events, len(want))
	}
	for i, e := range events {
		if e.RequestID != "" || e.User != "" || e.Name != want[i].name || !reflect.DeepEqual(e.Attrs, want[i].attr) {
			t.Errorf("event %d = %+v, want %s %v without caller", i, e, want[i].name, want[i].attr)
		}
	}
}

// R-YH4P-UNSB.
func TestBinaryStateAndBoxRequestsLeaveReportsUnchanged(t *testing.T) {
	_, st, host := fixtureStore(t)
	other := provisionFixtureHost(t, st, "i-87654321")
	ctx := t.Context()
	addBoxAccount(t, st, host.Name, "acme")
	user := addBoxUser(t, st, "acme", "daffy")
	boxReached := false
	noop := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { boxReached = true })
	body := protocol.Report{Protocol: protocol.Version, Attempted: host.Version, Applied: host.Version, OS: "fixture"}
	if _, err := st.RecordReport(ctx, host.Name, body, fixtureNow); err != nil {
		t.Fatal(err)
	}
	stamps := new(requests)
	w, capture := recorder(t)
	file := filepath.Join(t.TempDir(), "accountsd")
	writeBinary(t, file, []byte("fixture"))
	timer := make(chan time.Time, 1)
	timer <- fixtureNow
	s := server(t, Config{
		Store: st, Configured: true, Verifier: stamps.verifier(fixtureSTS{instance: fixtureInstance}),
		Binary: file, Telemetry: w, Now: func() time.Time { return fixtureNow.Add(time.Minute) },
		After: func(time.Duration) <-chan time.Time { return timer }, BoxUser: noop, BoxAdmin: noop,
	})
	recording := &recordingReportStore{Store: st}
	s.store = recording
	for _, method := range []string{http.MethodGet, http.MethodHead} {
		if response := invoke(s, httptest.NewRequestWithContext(ctx, method, protocol.BinaryPath, nil)); response.Code != http.StatusOK {
			t.Fatalf("%s binary = %d", method, response.Code)
		}
	}
	wantState := desired(t, st, host.Name, s.Digest())
	for _, path := range []string{protocol.StatePath, pollingRequest(host, s.Digest())} {
		expectState(t, invoke(s, stamps.signed(ctx, http.MethodGet, path, nil)), wantState)
	}
	invoke(s, stamps.signed(ctx, http.MethodPost, boxPath(user.UID), strings.NewReader(listRequest)))
	if !boxReached {
		t.Fatal("box request did not reach its handler")
	}
	want := &store.Report{Host: host.Name, Received: fixtureNow, Body: body}
	if got, err := st.Report(ctx, host.Name); err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("binary, state or box requests changed report: %+v, %v; want %+v", got, err, want)
	}
	if got, err := st.Report(ctx, other.Name); err != nil || got != nil {
		t.Fatalf("binary, state or box requests created another host's report: %+v, %v", got, err)
	}
	if len(recording.calls) != 0 {
		t.Fatalf("binary, state or box requests called RecordReport: %+v", recording.calls)
	}
	if err := w.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	if events := capture.Events(); len(events) != 0 {
		t.Fatalf("binary, state or box requests emitted events: %+v", events)
	}
}
