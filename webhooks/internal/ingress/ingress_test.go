package ingress_test

import (
	"bufio"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/webhooks"
	"github.com/ikigenba/ikigenba/webhooks/internal/ingress"
	"github.com/ikigenba/ikigenba/webhooks/internal/store"
	"github.com/ikigenba/ikigenba/webhooks/internal/trail"
)

var instant = time.Date(2026, 10, 9, 14, 12, 37, 0, time.UTC)

type lockedRand struct {
	mu sync.Mutex
	r  *rand.ChaCha8
}

func (l *lockedRand) Read(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.r.Read(p)
}

type env struct {
	db     *db.DB
	store  *store.Store
	ts     *telemetry.Capture
	es     *events.Capture
	w      *telemetry.Writer
	em     *events.Emitter
	stderr *bytes.Buffer
	srv    *httptest.Server
	mu     sync.Mutex
}

func setup(t *testing.T) *env {
	t.Helper()
	t.Setenv(services.Variable, "")
	now := func() time.Time { return instant }
	d, err := db.Open(context.Background(), db.Config{Path: filepath.Join(t.TempDir(), "state", "webhooks.db"), Migrations: webhooks.Migrations(), Now: now})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	var seed [32]byte
	seed[0] = 7
	e := &env{db: d, ts: &telemetry.Capture{}, es: &events.Capture{}, stderr: &bytes.Buffer{}}
	e.store = store.New(d, store.Config{Now: now, Rand: &lockedRand{r: rand.NewChaCha8(seed)}})
	sleep := func(context.Context, time.Duration) {}
	stderr := writerFunc(func(p []byte) (int, error) { e.mu.Lock(); defer e.mu.Unlock(); return e.stderr.Write(p) })
	e.w = telemetry.New(telemetry.Config{Service: "webhooks", Sink: e.ts, Stderr: stderr, Now: now, Sleep: sleep, Rand: &lockedRand{r: rand.NewChaCha8([32]byte{1})}})
	e.em = events.New(events.Config{Service: "webhooks", Sink: e.es, Stderr: stderr, Now: now, Sleep: sleep, Rand: &lockedRand{r: rand.NewChaCha8([32]byte{2})}, Emits: trail.Emits()})
	h := ingress.Handler(ingress.Config{Store: e.store, Telemetry: e.w, Events: e.em})
	e.srv = httptest.NewServer(telemetry.Middleware(e.w, identity.Optional(h)))
	t.Cleanup(func() {
		e.srv.Close()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		e.em.Shutdown(ctx)
		e.w.Shutdown(ctx, "test")
	})
	return e
}

type writerFunc func([]byte) (int, error)

func (f writerFunc) Write(p []byte) (int, error) { return f(p) }

func (e *env) flush(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := e.em.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	if err := e.w.Flush(ctx); err != nil {
		t.Fatal(err)
	}
}

func (e *env) create(t *testing.T, slug, scheme string) (store.Webhook, string) {
	t.Helper()
	h, secret, err := e.store.Create(context.Background(), store.Draft{Slug: slug, Scheme: scheme, OwnerID: "owner", OwnerEmail: "owner@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	return h, secret
}

func sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return ingress.SignaturePrefix + hex.EncodeToString(mac.Sum(nil))
}

type answer struct {
	status int
	header http.Header
	body   []byte
}

func (e *env) post(t *testing.T, method, path string, header http.Header, body []byte) answer {
	t.Helper()
	req, err := http.NewRequest(method, e.srv.URL+path, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	for k, vs := range header {
		req.Header[k] = vs
	}
	res, err := e.srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = res.Body.Close() }()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return answer{res.StatusCode, res.Header, b}
}

// raw sends one request over its own connection and answers the response's
// bytes as read, the Date line removed.
func (e *env) raw(t *testing.T, path string, header http.Header, body []byte) string {
	t.Helper()
	conn, err := net.Dial("tcp", e.srv.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(10 * time.Second))
	var head bytes.Buffer
	fmt.Fprintf(&head, "POST %s HTTP/1.1\r\nHost: webhooks.example.test\r\nContent-Length: %d\r\n", path, len(body))
	for k, vs := range header {
		for _, v := range vs {
			fmt.Fprintf(&head, "%s: %s\r\n", k, v)
		}
	}
	head.WriteString("\r\n")
	go func() {
		_, _ = conn.Write(append(head.Bytes(), body...))
	}()
	res, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	_ = res.Body.Close()
	res.Header.Del("Date")
	var out bytes.Buffer
	fmt.Fprintf(&out, "%d close=%v %v %q", res.StatusCode, res.Close, res.Header, b)
	return out.String()
}

func hdr(kv ...string) http.Header {
	h := http.Header{}
	for i := 0; i+1 < len(kv); i += 2 {
		h.Add(kv[i], kv[i+1])
	}
	return h
}

func (e *env) busEvents(t *testing.T) []events.Event {
	t.Helper()
	e.flush(t)
	return e.es.Events()
}

func (e *env) nonRequestEvents(t *testing.T) []telemetry.Event {
	t.Helper()
	e.flush(t)
	var out []telemetry.Event
	for _, ev := range e.ts.Events() {
		if ev.Name != "request.started" && ev.Name != "request.finished" {
			out = append(out, ev)
		}
	}
	return out
}

func (e *env) unchanged(t *testing.T, slugs ...string) {
	t.Helper()
	for _, s := range slugs {
		h, err := e.store.Get(context.Background(), s)
		if err != nil {
			t.Fatal(err)
		}
		if !h.LastReceived.IsZero() {
			t.Fatalf("%s received a delivery", s)
		}
	}
	if n := len(e.busEvents(t)); n != 0 {
		t.Fatalf("%d bus events", n)
	}
	if evs := e.nonRequestEvents(t); len(evs) != 0 {
		t.Fatalf("trail events %v", evs)
	}
}

func shape(func(ingress.Config) http.Handler, func(store.Webhook, http.Header, []byte) bool, ingress.Config) {
}

// R-Z9QY-6VRD R-ZC6Q-YF8R
func TestAdmitsBearer(t *testing.T) {
	shape(ingress.Handler, ingress.Admits, ingress.Config{Store: (*store.Store)(nil), Telemetry: (*telemetry.Writer)(nil), Events: (*events.Emitter)(nil)})
	admits := ingress.Admits
	if ingress.AuthHeader != "X-Webhook-Secret" || ingress.SignatureHeader != "X-Hub-Signature-256" || ingress.SignaturePrefix != "sha256=" || ingress.GitHubEventHeader != "X-GitHub-Event" || ingress.GitHubDeliveryHeader != "X-GitHub-Delivery" {
		t.Fatal("header constants")
	}
	secret := "whs_" + strings.Repeat("Q", 52)
	h := store.Webhook{Scheme: store.Bearer, SecretSHA256: store.HashSecret(secret)}
	for _, body := range [][]byte{nil, []byte("anything"), bytes.Repeat([]byte{0xff}, 100)} {
		if !admits(h, hdr("X-Webhook-Secret", secret), body) {
			t.Fatal("right secret refused")
		}
	}
	for name, header := range map[string]http.Header{
		"missing": {},
		"wrong":   hdr("X-Webhook-Secret", secret+"x"),
		"empty":   hdr("X-Webhook-Secret", ""),
		"twice":   hdr("X-Webhook-Secret", secret, "X-Webhook-Secret", secret),
		"hash":    hdr("X-Webhook-Secret", h.SecretSHA256),
		"auth":    hdr("Authorization", "Bearer "+secret),
	} {
		if admits(h, header, nil) {
			t.Fatalf("%s admitted", name)
		}
	}
}

// R-ZDEN-C6ZG
func TestAdmitsGitHubHMAC(t *testing.T) {
	secret := "whs_" + strings.Repeat("R", 52)
	h := store.Webhook{Scheme: store.GitHubHMAC, SecretPlain: secret}
	body := []byte(`{"ref":"refs/heads/main"}`)
	sig := sign(secret, body)
	if !ingress.Admits(h, hdr("X-Hub-Signature-256", sig), body) {
		t.Fatal("signed body refused")
	}
	if !ingress.Admits(h, hdr("X-Hub-Signature-256", sig), body) || !ingress.Admits(h, hdr("X-Hub-Signature-256", "sha256="+strings.ToUpper(sig[7:])), body) {
		t.Fatal("uppercase hex refused")
	}
	changed := append([]byte{}, body...)
	changed[3] ^= 1
	for name, c := range map[string]struct {
		header http.Header
		body   []byte
		h      store.Webhook
	}{
		"changed":   {hdr("X-Hub-Signature-256", sig), changed, h},
		"missing":   {http.Header{}, body, h},
		"noprefix":  {hdr("X-Hub-Signature-256", sig[7:]), body, h},
		"sha1":      {hdr("X-Hub-Signature-256", "sha1="+sig[7:]), body, h},
		"short":     {hdr("X-Hub-Signature-256", sig[:len(sig)-2]), body, h},
		"nothex":    {hdr("X-Hub-Signature-256", "sha256="+strings.Repeat("z", 64)), body, h},
		"twice":     {hdr("X-Hub-Signature-256", sig, "X-Hub-Signature-256", sig), body, h},
		"bearer":    {hdr("X-Webhook-Secret", secret), body, h},
		"nosecret":  {hdr("X-Hub-Signature-256", sign("", body)), body, store.Webhook{Scheme: store.GitHubHMAC}},
		"othersecr": {hdr("X-Hub-Signature-256", sign(secret+"x", body)), body, h},
	} {
		if ingress.Admits(c.h, c.header, c.body) {
			t.Fatalf("%s admitted", name)
		}
	}
}

// R-ZEMJ-PYQ5
func TestAdmitsOtherScheme(t *testing.T) {
	secret := "whs_" + strings.Repeat("S", 52)
	body := []byte("x")
	for _, scheme := range []string{"", "basic", "Bearer", "GITHUB-HMAC"} {
		h := store.Webhook{Scheme: scheme, SecretSHA256: store.HashSecret(secret), SecretPlain: secret}
		if ingress.Admits(h, hdr("X-Webhook-Secret", secret, "X-Hub-Signature-256", sign(secret, body)), body) {
			t.Fatalf("%q admitted", scheme)
		}
	}
}

// R-ZFUG-3QGU
func TestMethodNotAllowed(t *testing.T) {
	e := setup(t)
	_, secret := e.create(t, "tick", store.Bearer)
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPut, http.MethodDelete, http.MethodPatch, http.MethodOptions} {
		for _, path := range []string{"/in/tick", "/in/", "/in/nobody", "/in/Bad", "/in/a/b"} {
			a := e.post(t, method, path, hdr("X-Webhook-Secret", secret), []byte("body"))
			if a.status != http.StatusMethodNotAllowed || a.header.Get("Allow") != "POST" || len(a.body) != 0 {
				t.Fatalf("%s %s: %d %v %q", method, path, a.status, a.header, a.body)
			}
		}
	}
	e.unchanged(t, "tick")
}

// R-ZH2C-HI7J
func TestRefusalIsIdentical(t *testing.T) {
	e := setup(t)
	_, bearer := e.create(t, "tick", store.Bearer)
	_, hmacSecret := e.create(t, "gh_push", store.GitHubHMAC)
	body := []byte(`{"hello":"world"}`)
	big := bytes.Repeat([]byte("a"), 600*1024)
	cases := []struct {
		name, path string
		header     http.Header
		body       []byte
	}{
		{"invalid slug", "/in/Bad", hdr("X-Webhook-Secret", bearer), body},
		{"empty slug", "/in/", nil, body},
		{"nested", "/in/tick/more", hdr("X-Webhook-Secret", bearer), body},
		{"unknown", "/in/nobody", hdr("X-Webhook-Secret", bearer), body},
		{"unknown big", "/in/nobody", nil, big},
		{"bearer missing", "/in/tick", nil, body},
		{"bearer wrong", "/in/tick", hdr("X-Webhook-Secret", bearer+"x"), body},
		{"bearer twice", "/in/tick", hdr("X-Webhook-Secret", bearer, "X-Webhook-Secret", bearer), body},
		{"bearer other secret", "/in/tick", hdr("X-Webhook-Secret", hmacSecret), big},
		{"bearer in authorization", "/in/tick", hdr("Authorization", "Bearer "+bearer), body},
		{"hmac missing", "/in/gh_push", nil, body},
		{"hmac wrong", "/in/gh_push", hdr("X-Hub-Signature-256", sign(hmacSecret, []byte("other"))), body},
		{"hmac wrong big", "/in/gh_push", hdr("X-Hub-Signature-256", sign(hmacSecret, []byte("other"))), big},
		{"hmac bearer header", "/in/gh_push", hdr("X-Webhook-Secret", hmacSecret), body},
	}
	var first string
	for i, c := range cases {
		got := e.raw(t, c.path, c.header, c.body)
		if !strings.HasPrefix(got, "404 ") {
			t.Fatalf("%s: %s", c.name, got)
		}
		if i == 0 {
			first = got
			if !strings.Contains(got, "close=true") || !strings.HasSuffix(got, `""`) || strings.Contains(got, "Content-Type") {
				t.Fatalf("refusal shape: %s", got)
			}
			continue
		}
		if got != first {
			t.Fatalf("%s: %s differs from %s", c.name, got, first)
		}
	}
	e.unchanged(t, "tick", "gh_push")
}

// R-ZIA8-V9Y8
func TestTooLarge(t *testing.T) {
	e := setup(t)
	_, bearer := e.create(t, "tick", store.Bearer)
	_, hmacSecret := e.create(t, "gh_push", store.GitHubHMAC)
	over := bytes.Repeat([]byte("b"), store.MaxBody+1)
	for _, c := range []struct {
		path   string
		header http.Header
	}{
		{"/in/tick", hdr("X-Webhook-Secret", bearer)},
		{"/in/gh_push", hdr("X-Hub-Signature-256", sign(hmacSecret, over))},
		{"/in/gh_push", nil},
	} {
		a := e.post(t, http.MethodPost, c.path, c.header, over)
		if a.status != http.StatusRequestEntityTooLarge || len(a.body) != 0 {
			t.Fatalf("%s: %d %q", c.path, a.status, a.body)
		}
	}
	e.unchanged(t, "tick", "gh_push")
}

// R-ZJI5-91OX
func TestUnreachable(t *testing.T) {
	e := setup(t)
	_, bearer := e.create(t, "tick", store.Bearer)
	e.db.SetFailing(true)
	for _, path := range []string{"/in/tick", "/in/nobody"} {
		a := e.post(t, http.MethodPost, path, hdr("X-Webhook-Secret", bearer), []byte("payload"))
		if a.status != http.StatusServiceUnavailable || a.header.Get("Content-Type") != "text/plain; charset=utf-8" || string(a.body) != store.Unreachable+"\n" {
			t.Fatalf("%s: %d %v %q", path, a.status, a.header, a.body)
		}
	}
	e.db.SetFailing(false)
	e.unchanged(t, "tick")
}

func (e *env) received(t *testing.T) []events.Event {
	t.Helper()
	var out []events.Event
	for _, ev := range e.busEvents(t) {
		if strings.HasSuffix(ev.Name, ".received") {
			out = append(out, ev)
		}
	}
	return out
}

// R-ZKQ1-MTFM
func TestAccepted(t *testing.T) {
	e := setup(t)
	tick, bearer := e.create(t, "tick", store.Bearer)
	gh, hmacSecret := e.create(t, "gh_push", store.GitHubHMAC)
	binary := []byte{0x00, 0xff, 0xfe, 'x', 0x80}
	exact := bytes.Repeat([]byte("c"), store.MaxBody)
	cases := []struct {
		hook   store.Webhook
		header http.Header
		body   []byte
		ct, ge string
		gd     string
	}{
		{tick, hdr("X-Webhook-Secret", bearer, "Content-Type", "application/json", "X-User-Id", "intruder", "X-User-Email", "intruder@example.com"), []byte(`{"a":1}`), "application/json", "", ""},
		{tick, hdr("X-Webhook-Secret", bearer), binary, "", "", ""},
		{tick, hdr("X-Webhook-Secret", bearer, "Content-Type", "text/odd; x=y", "X-GitHub-Event", "ping", "X-GitHub-Delivery", "guid-one"), nil, "text/odd; x=y", "ping", "guid-one"},
		{tick, hdr("X-Webhook-Secret", bearer, "Content-Type", "application/octet-stream"), exact, "application/octet-stream", "", ""},
		{gh, hdr("X-Hub-Signature-256", sign(hmacSecret, []byte(`{"ref":"main"}`)), "Content-Type", "application/json", "X-GitHub-Event", "push", "X-GitHub-Delivery", "guid-two"), []byte(`{"ref":"main"}`), "application/json", "push", "guid-two"},
	}
	for i, c := range cases {
		a := e.post(t, http.MethodPost, "/in/"+c.hook.Slug, c.header, c.body)
		if a.status != http.StatusAccepted || len(a.body) != 0 {
			t.Fatalf("case %d: %d %q", i, a.status, a.body)
		}
		evs := e.received(t)
		if len(evs) != i+1 {
			t.Fatalf("case %d: %d received events", i, len(evs))
		}
		id, _ := evs[i].Attrs["delivery"].(string)
		d, h, err := e.store.Delivery(context.Background(), id)
		if err != nil {
			t.Fatalf("case %d: %v", i, err)
		}
		if h.ID != c.hook.ID || d.HookID != c.hook.ID || !bytes.Equal(d.Body, c.body) || d.ContentType != c.ct || d.GitHubEvent != c.ge || d.GitHubDelivery != c.gd {
			t.Fatalf("case %d: %+v", i, d)
		}
		after, err := e.store.Get(context.Background(), c.hook.Slug)
		if err != nil || !after.LastReceived.Equal(d.Received) || after.LastReceived.IsZero() {
			t.Fatalf("case %d: %+v %v", i, after, err)
		}
	}
}

// R-ZLXY-0L6B
func TestReceivedEvent(t *testing.T) {
	e := setup(t)
	tick, bearer := e.create(t, "tick", store.Bearer)
	gh, hmacSecret := e.create(t, "gh_push", store.GitHubHMAC)
	body := []byte(`{"n":1}`)
	a := e.post(t, http.MethodPost, "/in/tick", hdr("X-Webhook-Secret", bearer, "X-Request-Id", "req-one", "X-User-Id", "intruder", "X-User-Email", "intruder@example.com", "X-Event-Cause", "evt_0123456789abcdef", "X-Event-Depth", "2", "Content-Type", "text/plain", "X-GitHub-Event", "push"), body)
	if a.status != http.StatusAccepted {
		t.Fatal(a.status)
	}
	a = e.post(t, http.MethodPost, "/in/gh_push", hdr("X-Hub-Signature-256", sign(hmacSecret, body), "X-GitHub-Event", "push", "Content-Type", "application/json"), body)
	if a.status != http.StatusAccepted {
		t.Fatal(a.status)
	}
	bus := e.received(t)
	if len(bus) != 2 {
		t.Fatalf("%v", bus)
	}
	var trailed []telemetry.Event
	var started []telemetry.Event
	for _, ev := range e.ts.Events() {
		switch {
		case strings.HasSuffix(ev.Name, ".received"):
			trailed = append(trailed, ev)
		case ev.Name == "request.started":
			started = append(started, ev)
		}
	}
	if len(trailed) != 2 || len(started) != 2 {
		t.Fatalf("trail %v", e.ts.Events())
	}
	want := []struct {
		hook    store.Webhook
		kind    string
		ct      string
		request string
	}{{tick, "", "text/plain", "req-one"}, {gh, "push", "application/json", started[1].RequestID}}
	for i, w := range want {
		for _, ev := range []struct {
			name, request, user string
			attrs               map[string]any
		}{{bus[i].Name, bus[i].RequestID, bus[i].User, bus[i].Attrs}, {trailed[i].Name, trailed[i].RequestID, trailed[i].User, trailed[i].Attrs}} {
			if ev.name != "webhook."+w.hook.Slug+".received" || ev.request != w.request || ev.user != "owner" || ev.attrs["hook"] != w.hook.ID || ev.attrs["type"] != w.kind || ev.attrs["content_type"] != w.ct || fmt.Sprint(ev.attrs["bytes"]) != strconv.Itoa(len(body)) {
				t.Fatalf("event %d: %+v", i, ev)
			}
		}
		if bus[i].Cause != "" || bus[i].Depth != 0 {
			t.Fatalf("cause %q depth %d", bus[i].Cause, bus[i].Depth)
		}
	}
	if w := want[1]; w.request == "" || len(w.request) != 32 {
		t.Fatalf("minted request id %q", w.request)
	}
	// Nothing is emitted for a request not answered 202.
	before := len(e.busEvents(t))
	trailBefore := len(e.nonRequestEvents(t))
	e.post(t, http.MethodPost, "/in/tick", hdr("X-Webhook-Secret", "wrong"), body)
	e.post(t, http.MethodGet, "/in/tick", nil, nil)
	e.post(t, http.MethodPost, "/in/nobody", nil, body)
	e.post(t, http.MethodPost, "/in/tick", hdr("X-Webhook-Secret", bearer), bytes.Repeat([]byte("d"), store.MaxBody+1))
	if len(e.busEvents(t)) != before || len(e.nonRequestEvents(t)) != trailBefore {
		t.Fatal("refused requests emitted events")
	}
}

func leaks(haystack, needle string) bool {
	h, n := strings.ToLower(haystack), strings.ToLower(needle)
	for i := 0; i+8 <= len(n); i++ {
		if strings.Contains(h, n[i:i+8]) {
			return true
		}
	}
	return false
}

// R-ZN5U-ECX0
func TestNothingLeaks(t *testing.T) {
	e := setup(t)
	_, bearer := e.create(t, "tick", store.Bearer)
	_, hmacSecret := e.create(t, "gh_push", store.GitHubHMAC)
	markedBody := "QuxWvTzRmPnLkJhGyVsQpOnMlKjIhGgFwZyXvU"
	markedWrong := "WqzXvGtKpLmNyRsJhVuZtXwQpLmKjHg"
	markedHeader := "UzkpqHrxVzWyXqRtSuPoMnLkJgTwQv"
	markedAuth := "AqzrhXtkZqXwYvUtSrPoLmNjKhGwQz"
	markedDelivery := "KzptVlRyGqzdZwYxVuTsRqPoNmLkJh"
	body := []byte(markedBody)
	sig := sign(hmacSecret, body)
	common := func(kv ...string) http.Header {
		return hdr(append([]string{"X-Custom-Thing", markedHeader, "Authorization", "Bearer " + markedAuth, "X-GitHub-Delivery", markedDelivery, "Content-Type", "text/plain"}, kv...)...)
	}
	e.post(t, http.MethodPost, "/in/tick", common("X-Webhook-Secret", bearer), body)
	e.post(t, http.MethodPost, "/in/tick", common("X-Webhook-Secret", markedWrong), body)
	e.post(t, http.MethodPost, "/in/tick", common(), body)
	e.post(t, http.MethodPost, "/in/gh_push", common("X-Hub-Signature-256", sig), body)
	e.post(t, http.MethodPost, "/in/gh_push", common("X-Hub-Signature-256", sign(markedWrong, body)), body)
	e.post(t, http.MethodPost, "/in/nobody", common("X-Webhook-Secret", markedWrong), body)
	e.post(t, http.MethodGet, "/in/tick", common("X-Webhook-Secret", bearer), body)
	if len(e.received(t)) != 2 {
		t.Fatal("expected two accepted deliveries")
	}
	var all []string
	for _, ev := range e.ts.Events() {
		all = append(all, fmt.Sprintf("%+v", ev))
	}
	for _, ev := range e.es.Events() {
		all = append(all, fmt.Sprintf("%+v", ev))
	}
	e.mu.Lock()
	all = append(all, e.stderr.String())
	e.mu.Unlock()
	text := strings.Join(all, "\n")
	for name, v := range map[string]string{"bearer": bearer, "hmac secret": hmacSecret, "body": markedBody, "wrong": markedWrong, "header": markedHeader, "authorization": markedAuth, "kept delivery": markedDelivery} {
		if leaks(text, v) {
			t.Fatalf("%s leaked", name)
		}
	}
	for _, s := range []string{sig[len(ingress.SignaturePrefix):], sign(markedWrong, body)[len(ingress.SignaturePrefix):]} {
		if strings.Contains(strings.ToLower(text), s[:16]) {
			t.Fatal("signature leaked")
		}
	}
}
