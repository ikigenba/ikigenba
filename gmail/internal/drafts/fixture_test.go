package drafts_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/connections"
	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	app "github.com/ikigenba/ikigenba/gmail"
	"github.com/ikigenba/ikigenba/gmail/internal/drafts"
	"github.com/ikigenba/ikigenba/gmail/internal/gmail"
	"github.com/ikigenba/ikigenba/gmail/internal/store"
	"github.com/ikigenba/ikigenba/gmail/internal/trail"
	"github.com/ikigenba/ikigenba/gmail/internal/transfers"
)

type request struct {
	Method, Path, Authorization string
	Body                        []byte
}
type draftFixture struct {
	st          *store.Store
	db          *db.DB
	svc         *drafts.Service
	cfg         drafts.Config
	ctx         context.Context
	caller      identity.Caller
	token       connections.Token
	telemetry   *telemetry.Capture
	bus         *events.Capture
	writer      *telemetry.Writer
	emitter     *events.Emitter
	mu          sync.Mutex
	requests    []request
	tokenCalls  int
	tokenStatus int
	tokenBody   string
	handle      func(http.ResponseWriter, *http.Request)
}

func fixture(t *testing.T) *draftFixture {
	t.Helper()
	t.Setenv(services.Variable, "")
	f := &draftFixture{caller: identity.Caller{UserID: "owner", Email: "caller-label", RequestID: "request"}, token: connections.Token{AccessToken: "synthetic-token", Account: "sender@example.invalid"}, tokenStatus: 200}
	f.ctx = identity.NewContext(context.Background(), f.caller)
	now := func() time.Time { return time.Unix(1700000000, 0) }
	var err error
	f.db, err = db.Open(f.ctx, db.Config{Path: filepath.Join(t.TempDir(), "gmail.db"), Migrations: app.Migrations(), Now: now, Service: "gmail", Stderr: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if e := f.db.Close(); e != nil {
			t.Error(e)
		}
	})
	random := make([]byte, 16384)
	for i := range random {
		random[i] = byte(i / 8)
	}
	f.st = store.New(f.db, store.Config{Now: now, Rand: bytes.NewReader(random)})
	f.telemetry, f.bus = &telemetry.Capture{}, &events.Capture{}
	pause := func(context.Context, time.Duration) {}
	f.writer = telemetry.New(telemetry.Config{Service: "gmail", Version: "test", Sink: f.telemetry, Stderr: io.Discard, Now: now, Sleep: pause, Rand: bytes.NewReader(make([]byte, 16384))})
	f.emitter = events.New(events.Config{Service: "gmail", Sink: f.bus, Stderr: io.Discard, Now: now, Sleep: pause, Rand: bytes.NewReader(make([]byte, 16384)), Emits: trail.Emits(), Telemetry: f.writer})
	t.Cleanup(func() { f.emitter.Shutdown(context.Background()); f.writer.Shutdown(context.Background(), "test") })
	f.handle = func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/gmail/v1/users/me/drafts":
			writeJSON(w, 200, map[string]any{"id": "provider-draft", "message": map[string]any{"id": "draft-message", "threadId": "draft-thread"}})
		case r.Method == http.MethodGet:
			writeJSON(w, 200, map[string]any{"id": "provider-draft", "message": map[string]any{"id": "draft-message", "threadId": "draft-thread"}})
		case r.Method == http.MethodPost:
			writeJSON(w, 200, map[string]any{"id": "sent-message", "threadId": "sent-thread"})
		case r.Method == http.MethodDelete:
			w.WriteHeader(204)
		default:
			w.WriteHeader(500)
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, e := io.ReadAll(r.Body)
		if e != nil {
			t.Error(e)
		}
		f.mu.Lock()
		f.requests = append(f.requests, request{r.Method, r.URL.Path, r.Header.Get("Authorization"), body})
		h := f.handle
		f.mu.Unlock()
		if strings.HasSuffix(r.URL.Path, "/labels") {
			writeJSON(w, 200, map[string]any{"labels": []any{}})
			return
		}
		h(w, r)
	}))
	t.Cleanup(server.Close)
	dir, err := os.MkdirTemp("", "gmail-drafts-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if e := os.RemoveAll(dir); e != nil {
			t.Error(e)
		}
	})
	socket := filepath.Join(dir, "connections")
	ln, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	broker := &http.Server{ReadHeaderTimeout: time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		f.mu.Lock()
		f.tokenCalls++
		status, body := f.tokenStatus, f.tokenBody
		f.mu.Unlock()
		if body == "" {
			writeJSON(w, status, map[string]any{"access_token": f.token.AccessToken, "account": f.token.Account, "expires": "2030-01-01T00:00:00Z", "scopes": []string{}})
		} else {
			w.WriteHeader(status)
			_, _ = io.WriteString(w, body)
		}
	})}
	go func() { _ = broker.Serve(ln) }()
	t.Cleanup(func() { _ = broker.Close() })
	f.cfg = drafts.Config{Store: f.st, Gmail: gmail.NewClient(gmail.Config{HTTP: server.Client(), BaseURL: server.URL, Telemetry: f.writer}), Connections: connections.NewClient(connections.ClientConfig{Telemetry: f.writer, Service: services.Entry{Name: "connections", Socket: socket}}), Telemetry: f.writer, Events: f.emitter, ConnectURL: "http://connections.example.invalid/google/connect"}
	f.svc = drafts.New(f.cfg)
	return f
}
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func (f *draftFixture) add(t *testing.T) store.Draft {
	t.Helper()
	d, e := f.st.AddDraft(f.ctx, store.Draft{GmailDraft: "provider-draft", Message: "draft-message", Thread: "draft-thread", UserID: f.caller.UserID, To: []string{"recipient@example.invalid"}})
	if e != nil {
		t.Fatal(e)
	}
	return d
}
func (f *draftFixture) snapshot() []request {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]request(nil), f.requests...)
}
func (f *draftFixture) setHandler(h func(http.ResponseWriter, *http.Request)) {
	f.mu.Lock()
	f.handle = h
	f.mu.Unlock()
}
func (f *draftFixture) flush(t *testing.T) {
	t.Helper()
	if e := f.emitter.Flush(f.ctx); e != nil {
		t.Fatal(e)
	}
	if e := f.writer.Flush(f.ctx); e != nil {
		t.Fatal(e)
	}
}
func (f *draftFixture) events(t *testing.T, name string, d store.Draft) {
	t.Helper()
	f.flush(t)
	a, b := f.telemetry.Events(), f.bus.Events()
	aa := []telemetry.Event{}
	for _, e := range a {
		if e.Name == name {
			aa = append(aa, e)
		}
	}
	if len(aa) != 1 || len(b) != 1 {
		t.Fatalf("events %d %d: %#v %#v", len(aa), len(b), aa, b)
	}
	want := telemetry.Attrs{"draft": d.ID, "message": d.Message, "thread": d.Thread, "attachments": int64(len(d.Attachments))}
	if aa[0].Name != name || b[0].Name != name || aa[0].User != f.caller.UserID || b[0].User != f.caller.UserID || aa[0].RequestID != f.caller.RequestID || b[0].RequestID != f.caller.RequestID || !reflect.DeepEqual(aa[0].Attrs, want) || !reflect.DeepEqual(b[0].Attrs, events.Attrs(want)) {
		t.Fatalf("mismatched events %#v %#v", aa, b)
	}
}
func (f *draftFixture) noEvents(t *testing.T) {
	t.Helper()
	f.flush(t)
	for _, e := range f.telemetry.Events() {
		if e.Name == trail.DraftCreated || e.Name == trail.DraftSent || e.Name == trail.DraftDiscarded {
			t.Fatal(e)
		}
	}
	if len(f.bus.Events()) != 0 {
		t.Fatal(f.bus.Events())
	}
}
func refused(t *testing.T, d store.Draft, err error, word, url string) {
	t.Helper()
	var r *transfers.Refusal
	if !reflect.DeepEqual(d, store.Draft{}) || !errors.As(err, &r) || r.Word != word || r.URL != url {
		t.Fatalf("refusal %#v %v wanted %s %s", d, err, word, url)
	}
}
