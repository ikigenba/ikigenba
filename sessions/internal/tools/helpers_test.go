package tools_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	sessions "github.com/ikigenba/ikigenba/sessions"
	"github.com/ikigenba/ikigenba/sessions/internal/store"
	"github.com/ikigenba/ikigenba/sessions/internal/tools"
	"github.com/ikigenba/ikigenba/sessions/internal/trail"
)

var now = time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
var owner = identity.Caller{UserID: "u", Email: "e", RequestID: "request"}
var other = identity.Caller{UserID: "u2", Email: "e2", RequestID: "second"}

type rig struct {
	d       *db.DB
	s       *store.Store
	w       *telemetry.Writer
	capture *telemetry.Capture
	srv     *mcp.Server
	client  *mcp.Client
}

func database(t *testing.T) (*db.DB, *store.Store) {
	t.Helper()
	d, err := db.Open(context.Background(), db.Config{Path: filepath.Join(t.TempDir(), "sessions.db"), Migrations: sessions.Migrations(), Now: func() time.Time { return now }, Stderr: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := d.Close(); err != nil {
			t.Error(err)
		}
	})
	return d, store.New(d, store.Config{Now: func() time.Time { return now }})
}
func newRig(t *testing.T) *rig {
	t.Helper()
	t.Setenv(services.Variable, "")
	d, s := database(t)
	capture := &telemetry.Capture{}
	w := telemetry.New(telemetry.Config{Service: "sessions", Version: "test", Sink: capture, Stderr: io.Discard, Now: func() time.Time { return now }, Sleep: func(context.Context, time.Duration) {}, Rand: bytes.NewReader(make([]byte, 1<<20))})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		w.Shutdown(ctx, "test")
	})
	srv := mcp.NewServer(mcp.ServerConfig{Name: "sessions", Version: "test", Telemetry: w})
	tools.Register(srv, tools.Config{s, w})
	return &rig{d, s, w, capture, srv, serve(t, srv)}
}
func serve(t *testing.T, srv *mcp.Server) *mcp.Client {
	t.Helper()
	h := httptest.NewServer(identity.Require(srv))
	t.Cleanup(h.Close)
	return mcp.NewClient(mcp.ClientConfig{Endpoint: h.URL, HTTPClient: h.Client(), Name: "tests", Version: "test"})
}
func raw(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}
func call(t *testing.T, r *rig, c identity.Caller, name string, a any) mcp.Result {
	t.Helper()
	v, err := r.client.CallTool(context.Background(), c, name, raw(t, a))
	if err != nil {
		t.Fatal(err)
	}
	return v
}

type resultWire struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Structured json.RawMessage `json:"structuredContent"`
}

func wire(t *testing.T, v mcp.Result) resultWire {
	t.Helper()
	var w resultWire
	if err := json.Unmarshal(raw(t, v), &w); err != nil {
		t.Fatal(err)
	}
	return w
}
func success[T any](t *testing.T, r *rig, c identity.Caller, name string, a any) T {
	t.Helper()
	v := call(t, r, c, name, a)
	if v.IsError() {
		t.Fatalf("%s: %s", name, raw(t, v))
	}
	w := wire(t, v)
	if len(w.Content) != 1 || w.Content[0].Type != "text" || !json.Valid(w.Structured) {
		t.Fatalf("success envelope %s", raw(t, v))
	}
	if w.Content[0].Text != string(w.Structured) {
		t.Fatalf("text and structured result differ: %s", raw(t, v))
	}
	var out T
	if err := json.Unmarshal(w.Structured, &out); err != nil {
		t.Fatal(err)
	}
	return out
}
func refused(t *testing.T, r *rig, name string, a any, want string) {
	t.Helper()
	v := call(t, r, owner, name, a)
	w := wire(t, v)
	if !v.IsError() || len(w.Content) != 1 || w.Content[0].Type != "text" || w.Content[0].Text != want || len(w.Structured) != 0 {
		t.Fatalf("%s got %s want refusal %q", name, raw(t, v), want)
	}
}
func equal(t *testing.T, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v\nwant %#v", got, want)
	}
}
func ptr[T any](v T) *T { return &v }
func flush(t *testing.T, r *rig) []telemetry.Event {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := r.w.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	var out []telemetry.Event
	for _, e := range r.capture.Events() {
		if e.Name == trail.EventChangeFinished {
			out = append(out, e)
		}
	}
	return out
}
func entry(id string, at time.Time) tools.PushSession {
	return tools.PushSession{Tool: "claude", ID: id, Host: "test-host", Directory: "/work", Turns: []tools.PushTurn{{Moment: at.Format(time.RFC3339Nano), Whose: "person"}}}
}
func push(t *testing.T, r *rig, c identity.Caller, x ...tools.PushSession) tools.LatestList {
	t.Helper()
	return success[tools.LatestList](t, r, c, tools.ToolPushTurns, tools.PushTurnsArgs{Sessions: x})
}
