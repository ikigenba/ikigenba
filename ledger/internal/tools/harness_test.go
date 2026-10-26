package tools_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/artifacts"
	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	ledger "github.com/ikigenba/ikigenba/ledger"
	"github.com/ikigenba/ikigenba/ledger/internal/store"
	"github.com/ikigenba/ikigenba/ledger/internal/tools"
	"github.com/ikigenba/ikigenba/ledger/internal/trail"
)

type testRand struct {
	counter uint64
	pending []byte
}

func (r *testRand) Read(p []byte) (int, error) {
	n := len(p)
	for len(p) > 0 {
		if len(r.pending) == 0 {
			var seed [8]byte
			binary.LittleEndian.PutUint64(seed[:], r.counter)
			r.counter++
			sum := sha256.Sum256(seed[:])
			r.pending = sum[:]
		}
		copied := copy(p, r.pending)
		p = p[copied:]
		r.pending = r.pending[copied:]
	}
	return n, nil
}
func randomness(seed uint64) io.Reader { return &testRand{counter: seed} }

type harness struct {
	t       *testing.T
	db      *db.DB
	store   *store.Store
	writer  *telemetry.Writer
	emitter *events.Emitter
	trail   *telemetry.Capture
	bus     *events.Capture
	server  *mcp.Server
	client  *mcp.Client
	caller  identity.Caller
	now     time.Time
}

// R-GYWV-WOUO R-H04S-AGLD R-H50D-TJK5: every behavioral test uses this complete seam.
func newHarness(t *testing.T, factories ...func(*telemetry.Writer) *artifacts.Client) *harness {
	t.Helper()
	t.Setenv(services.Variable, "")
	h := &harness{t: t, now: time.Date(2026, 4, 8, 14, 15, 16, 0, time.FixedZone("east", 3600)), caller: identity.Caller{UserID: "keeper", Email: "keeper@example.test", RequestID: "request-one"}, trail: &telemetry.Capture{}, bus: &events.Capture{}}
	now := func() time.Time { return h.now }
	d, err := db.Open(t.Context(), db.Config{Path: filepath.Join(t.TempDir(), "state", "ledger.db"), Migrations: ledger.Migrations(), Now: now, Service: "ledger", Stderr: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	h.db = d
	h.store = store.New(d, store.Config{Now: now, Rand: randomness(1)})
	pause := func(context.Context, time.Duration) {}
	h.writer = telemetry.New(telemetry.Config{Service: "ledger", Sink: h.trail, Now: now, Sleep: pause, Rand: randomness(2), Stderr: io.Discard})
	h.emitter = events.New(events.Config{Service: "ledger", Sink: h.bus, Telemetry: h.writer, Emits: trail.Emits(), Now: now, Sleep: pause, Rand: randomness(3), Stderr: io.Discard})
	h.server = mcp.NewServer(mcp.ServerConfig{Name: "ledger", Telemetry: h.writer})
	var art *artifacts.Client
	if len(factories) > 0 {
		art = factories[0](h.writer)
	}
	tools.Register(h.server, tools.Config{Store: h.store, Telemetry: h.writer, Events: h.emitter, Artifacts: art})
	srv := httptest.NewServer(events.Middleware(identity.Require(h.server)))
	client := srv.Client()
	transport := client.Transport
	client.Transport = roundTripFunc(func(r *http.Request) (*http.Response, error) {
		r = r.Clone(r.Context())
		r.Header.Set(events.CauseHeader, "evt_0123456789abcdef")
		r.Header.Set(events.DepthHeader, "3")
		return transport.RoundTrip(r)
	})
	h.client = mcp.NewClient(mcp.ClientConfig{Endpoint: srv.URL, HTTPClient: client})
	t.Cleanup(func() {
		srv.Close()
		h.emitter.Shutdown(context.Background())
		h.writer.Shutdown(context.Background(), "test done")
		_ = d.Close()
	})
	return h
}
func (h *harness) call(name string, args any) mcp.Result {
	h.t.Helper()
	var raw json.RawMessage
	if v, ok := args.(json.RawMessage); ok {
		raw = v
	} else {
		b, err := json.Marshal(args)
		if err != nil {
			h.t.Fatal(err)
		}
		var members map[string]json.RawMessage
		if err = json.Unmarshal(b, &members); err != nil {
			h.t.Fatal(err)
		}
		for key, value := range members {
			if string(value) == "null" {
				delete(members, key)
			}
		}
		raw, err = json.Marshal(members)
		if err != nil {
			h.t.Fatal(err)
		}
	}
	result, err := h.client.CallTool(h.t.Context(), h.caller, name, raw)
	if err != nil {
		h.t.Fatalf("%s: %v", name, err)
	}
	return result
}

type resultBody struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Structured json.RawMessage `json:"structuredContent"`
}

func unpack(t *testing.T, r mcp.Result) resultBody {
	t.Helper()
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var body resultBody
	if err = json.Unmarshal(b, &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Content) != 1 || body.Content[0].Type != "text" {
		t.Fatalf("invalid result %s", b)
	}
	return body
}

// R-J7E9-5JSI: inspect only appkit's returned Result.
func success[T any](h *harness, name string, args any) T {
	h.t.Helper()
	r := h.call(name, args)
	b := unpack(h.t, r)
	if r.IsError() {
		h.t.Fatalf("%s: %s", name, b.Content[0].Text)
	}
	if len(b.Structured) == 0 || b.Structured[0] != '{' || !bytes.Equal([]byte(b.Content[0].Text), b.Structured) {
		h.t.Fatalf("success content differs: %+v", b)
	}
	var v T
	if err := json.Unmarshal(b.Structured, &v); err != nil {
		h.t.Fatal(err)
	}
	return v
}
func (h *harness) refuse(name string, args any, text string) {
	h.t.Helper()
	h.flush()
	beforeTrail, beforeBus := len(mutations(h)), len(h.bus.Events())
	before, beforeErr := h.store.Status(h.t.Context())
	r := h.call(name, args)
	b := unpack(h.t, r)
	if !r.IsError() || b.Content[0].Text != text || len(b.Structured) != 0 {
		h.t.Fatalf("%s refusal = %+v, want %q", name, b, text)
	}
	h.flush()
	if len(mutations(h)) != beforeTrail || len(h.bus.Events()) != beforeBus {
		h.t.Fatalf("%s refusal emitted a change", name)
	}
	after, afterErr := h.store.Status(h.t.Context())
	if beforeErr == nil && afterErr == nil {
		equal(h.t, after, before)
	}
}
func (h *harness) flush() {
	h.t.Helper()
	ctx, cancel := context.WithTimeout(h.t.Context(), 5*time.Second)
	defer cancel()
	if err := h.emitter.Flush(ctx); err != nil {
		h.t.Fatal(err)
	}
	if err := h.writer.Flush(ctx); err != nil {
		h.t.Fatal(err)
	}
}
func ptr[T any](v T) *T { return &v }
func (h *harness) open(name string) tools.AccountChange {
	return success[tools.AccountChange](h, "open_account", tools.AccountDateArgs{Name: name, Date: "2026-01-01"})
}
func (h *harness) post(key string) tools.TransactionChange {
	return success[tools.TransactionChange](h, "post", tools.PostArgs{Date: "2026-04-01", Key: ptr(key), Postings: []tools.PostingArgs{{Account: "Assets:Bank", Amount: ptr("42.00")}, {Account: "Income:Sales"}}})
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }
