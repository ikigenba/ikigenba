package daemonapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
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
		Rand: strings.NewReader("12345678abcdefghIJKLMNOP"),
	})
	h, op, err := s.CreateHost(t.Context(), store.HostSpec{Type: "t4g.medium", RootGB: 20, DataGB: 50}, identity.Caller{UserID: "test-user"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Finish(t.Context(), op.ID, store.Update{InstanceID: fixtureInstance}); err != nil {
		t.Fatal(err)
	}
	h, err = s.Host(t.Context(), h.Name)
	if err != nil {
		t.Fatal(err)
	}
	return d, s, h
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

// R-R4PD-7V00, R-R5X9-LMQP, R-R755-ZEHE.
func TestSurfaceAndBinaryStartup(t *testing.T) {
	_ = struct {
		new    func(context.Context, Config) (*Server, error)
		stop   func(*Server)
		digest func(*Server) string
		http   http.Handler
	}{New, (*Server).Stop, (*Server).Digest, (*Server)(nil)}
	_ = Config{nil, nil, "", nil, nil, false, nil}
	if MaxWaiters != 1000 || CopyBuffer != 32<<10 {
		t.Fatal("unexpected HTTP limits")
	}
	seen := map[string]bool{store.Unreachable: true}
	for _, copy := range []string{NotFound, NotAllowed, BadRequest, Refused, UnknownHost, TooLarge, Busy} {
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

// R-R8D2-D683, R-R9KY-QXYS, R-RC0R-IHG6, R-REGK-A0XK, R-RFOG-NSO9.
func TestRoutingAndMethodRefusals(t *testing.T) {
	unconfigured := server(t, Config{})
	configured := server(t, Config{Configured: true})
	for _, path := range []string{protocol.BinaryPath, protocol.StatePath, protocol.ReportPath, "/accountsd/", "/accountsd/v1/state/", "/accountsd/v1", "/accountsd/v2/state"} {
		for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost, http.MethodDelete} {
			w := invoke(unconfigured, httptest.NewRequestWithContext(t.Context(), method, path+"?after=bogus", neverRead{}))
			expectError(t, w, http.StatusNotFound, NotFound)
		}
	}
	for _, path := range []string{"/accountsd/", "/accountsd/v1/state/", "/accountsd/v1", "/accountsd/v2/state"} {
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

// R-RKK2-6VN1, R-RGWD-1KEY, R-AV2L-YS78.
func TestImmediateState(t *testing.T) {
	_, st, host := fixtureStore(t)
	stamps := new(requests)
	file := filepath.Join(t.TempDir(), "accountsd")
	writeBinary(t, file, []byte("fixture"))
	s := server(t, Config{
		Store: st, Verifier: stamps.verifier(fixtureSTS{instance: fixtureInstance}), Configured: true, Binary: file,
		After: func(time.Duration) <-chan time.Time {
			t.Fatal("immediate state request waited")
			return nil
		},
	})
	want := desired(t, st, host.Name, s.Digest())
	for _, path := range []string{
		protocol.StatePath,
		pollingRequest(host, "previous-digest"),
		protocol.StatePath + "?after=9223372036854775807&digest=" + url.QueryEscape(s.Digest()),
	} {
		expectState(t, invoke(s, stamps.signed(t.Context(), http.MethodGet, path, nil)), want)
	}
	_ = invoke(s, httptest.NewRequestWithContext(t.Context(), http.MethodGet, protocol.BinaryPath, nil))
	if report, err := st.Report(t.Context(), host.Name); report != nil || err != nil {
		t.Fatalf("reads wrote a report: %+v, %v", report, err)
	}
}

func startPoll(t *testing.T, s *Server, req *http.Request, waits <-chan time.Duration) <-chan *httptest.ResponseRecorder {
	t.Helper()
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- invoke(s, req) }()
	select {
	case duration := <-waits:
		if duration != protocol.LongPoll {
			t.Fatalf("wait duration = %v, want %v", duration, protocol.LongPoll)
		}
	case w := <-done:
		t.Fatalf("poll finished before waiting: %d %s", w.Code, w.Body.String())
	}
	select {
	case w := <-done:
		t.Fatalf("poll finished early: %d %s", w.Code, w.Body.String())
	default:
	}
	return done
}

// R-RMZU-YF4F, R-GKYK-5337.
func TestPollEndsOnWriteTimeoutCancelOrStop(t *testing.T) {
	_, st, host := fixtureStore(t)
	stamps := new(requests)
	wait := make(chan time.Duration, 1)
	timer := make(chan time.Time, 1)
	s := server(t, Config{
		Store: st, Configured: true, Verifier: stamps.verifier(fixtureSTS{instance: fixtureInstance}),
		After: func(d time.Duration) <-chan time.Time { wait <- d; return timer },
	})
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

// R-RO7R-C6V4.
func TestDesiredFailureAfterWait(t *testing.T) {
	for _, tt := range []struct {
		name    string
		failure func(*testing.T, *db.DB, *store.Store, store.Host)
		code    int
		copy    string
	}{
		{"deleted", removeFixtureHost, http.StatusForbidden, UnknownHost},
		{"database closed", closeFixtureDatabase, http.StatusServiceUnavailable, store.Unreachable},
	} {
		t.Run(tt.name, func(t *testing.T) {
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
	}
}

func removeFixtureHost(t *testing.T, _ *db.DB, st *store.Store, host store.Host) {
	t.Helper()
	_, op, err := st.StartHostOp(t.Context(), host.Name, store.OpDelete, store.Target{}, identity.Caller{UserID: "test-user"})
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

// R-RPFN-PYLT.
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

// R-RRVG-HI37, R-RXYY-ECSO.
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

func TestReportWriteFailure(t *testing.T) {
	for _, tt := range []struct {
		name    string
		failure func(*testing.T, *db.DB, *store.Store, store.Host)
		code    int
		copy    string
	}{
		{"deleted", removeFixtureHost, http.StatusForbidden, UnknownHost},
		{"database closed", closeFixtureDatabase, http.StatusServiceUnavailable, store.Unreachable},
	} {
		t.Run(tt.name, func(t *testing.T) {
			d, st, host := fixtureStore(t)
			stamps := new(requests)
			s := server(t, Config{Store: st, Configured: true, Verifier: stamps.verifier(fixtureSTS{instance: fixtureInstance})})
			body := readOnce{
				before: func() { tt.failure(t, d, st, host) },
				input:  strings.NewReader(fmt.Sprintf(`{"protocol":%d}`, protocol.Version)),
			}
			w := invoke(s, stamps.signed(t.Context(), http.MethodPost, protocol.ReportPath, &body))
			expectError(t, w, tt.code, tt.copy)
		})
	}
}

type readOnce struct {
	before func()
	input  *strings.Reader
}

func (r *readOnce) Read(p []byte) (int, error) {
	if r.before != nil {
		r.before()
		r.before = nil
	}
	return r.input.Read(p)
}

type eventSink struct {
	mu     sync.Mutex
	events []telemetry.Event
}

func (s *eventSink) Deliver(_ context.Context, event telemetry.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, event)
	return nil
}

// R-ASMT-78PU, R-ATUP-L0GJ, R-AV2L-YS78.
func TestReportEmitsOnlyHealthEdgesWithoutCaller(t *testing.T) {
	_, st, host := fixtureStore(t)
	stamps := new(requests)
	sink := new(eventSink)
	w := telemetry.New(telemetry.Config{Service: "accounts", Sink: sink, Stderr: io.Discard})
	t.Cleanup(func() { w.Shutdown(context.Background(), "test stopped") })
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
	saved, err := st.Report(t.Context(), host.Name)
	if err != nil || !saved.Received.Equal(received.Add(-time.Second)) {
		t.Fatalf("report did not use Config.Now: %+v, %v", saved, err)
	}
	wantState := desired(t, st, host.Name, "")
	expectState(t, invoke(s, stamps.signed(ctx, http.MethodGet, protocol.StatePath, nil)), wantState)
	current, err := st.Report(t.Context(), host.Name)
	if err != nil || !reflect.DeepEqual(current, saved) {
		t.Fatalf("state request changed report: %+v, %v", current, err)
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
	sink.mu.Lock()
	defer sink.mu.Unlock()
	if len(sink.events) != len(want) {
		t.Fatalf("events = %+v; want %d", sink.events, len(want))
	}
	for i, e := range sink.events {
		if e.RequestID != "" || e.User != "" || e.Name != want[i].name || !reflect.DeepEqual(e.Attrs, want[i].attr) {
			t.Errorf("event %d = %+v, want %s %v without caller", i, e, want[i].name, want[i].attr)
		}
	}
}
