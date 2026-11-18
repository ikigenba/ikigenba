package tools_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"io"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	accounts "github.com/ikigenba/ikigenba/accounts"
	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
	"github.com/ikigenba/ikigenba/accounts/internal/settings"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/accounts/internal/tools"
	"github.com/ikigenba/ikigenba/accounts/internal/trail"
	"github.com/ikigenba/ikigenba/accounts/internal/valid"
	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
)

type fixedReader struct{}

func (fixedReader) Read(p []byte) (int, error) { clear(p); return len(p), nil }

type fixture struct {
	t       *testing.T
	d       *db.DB
	path    string
	st      *store.Store
	cloud   *dnsCloud
	hosts   settings.Hosts
	wakes   int
	onWake  func()
	w       *telemetry.Writer
	capture *telemetry.Capture
	srv     *mcp.Server
	client  *mcp.Client
	caller  identity.Caller
	now     time.Time
}

func setup(t *testing.T) *fixture {
	t.Helper()
	return setupConfigured(t, true)
}

func setupConfigured(t *testing.T, configured bool) *fixture {
	t.Helper()
	t.Setenv(services.Variable, "")
	f := &fixture{t: t, path: filepath.Join(t.TempDir(), "accounts.db"), now: time.Date(2026, 10, 29, 9, 3, 7, 900000000, time.FixedZone("offset", -5*3600)), caller: identity.Caller{UserID: "first-caller", Email: "private@example.test", RequestID: "tool-request"}}
	var err error
	f.d, err = db.Open(context.Background(), db.Config{Path: f.path, Migrations: accounts.Migrations(), Now: func() time.Time { return f.now }, Service: "accounts", Stderr: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := f.d.Close(); err != nil {
			t.Error(err)
		}
	})
	f.st = store.New(f.d, store.Config{Now: func() time.Time { return f.now }, Rand: rand.NewChaCha8([32]byte{1})})
	f.cloud = &dnsCloud{}
	f.hosts = settings.Hosts{Domain: "metaspot.example", ZoneID: "zone-example"}
	f.capture = &telemetry.Capture{}
	f.w = telemetry.New(telemetry.Config{Service: "accounts", Sink: f.capture, Now: func() time.Time { return f.now }, Rand: fixedReader{}, Sleep: func(context.Context, time.Duration) {}, Stderr: io.Discard})
	t.Cleanup(func() { f.w.Shutdown(context.Background(), "done") })
	f.srv = mcp.NewServer(mcp.ServerConfig{Name: "accounts", Telemetry: f.w})
	tools.Register(f.srv, tools.Config{
		Store: f.st, Cloud: f.cloud, Hosts: f.hosts, Configured: configured, Telemetry: f.w,
		Wake: func() {
			f.wakes++
			if f.onWake != nil {
				f.onWake()
			}
		},
	})
	f.client = mcp.NewClient(mcp.ClientConfig{
		Endpoint:   "http://accounts.example/mcp",
		HTTPClient: &http.Client{Transport: testTransport{identity.Require(f.srv)}},
	})
	return f
}

func (f *fixture) serveLoopback() {
	f.t.Helper()
	srv := httptest.NewServer(identity.Require(f.srv))
	f.t.Cleanup(srv.Close)
	f.client = mcp.NewClient(mcp.ClientConfig{Endpoint: srv.URL + "/mcp", HTTPClient: srv.Client()})
}

type testTransport struct{ handler http.Handler }

func (tr testTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	recorder := httptest.NewRecorder()
	tr.handler.ServeHTTP(recorder, req)
	return recorder.Result(), nil
}

func (f *fixture) call(name string, args any) mcp.Result {
	f.t.Helper()
	return f.callContext(context.Background(), name, args)
}

func (f *fixture) callContext(ctx context.Context, name string, args any) mcp.Result {
	f.t.Helper()
	b, err := json.Marshal(args)
	if err != nil {
		f.t.Fatal(err)
	}
	r, err := f.client.CallTool(ctx, f.caller, name, b)
	if err != nil {
		f.t.Fatal(err)
	}
	return r
}

type envelope struct {
	Content    []struct{ Type, Text string }
	Structured json.RawMessage `json:"structuredContent"`
}

func unpack(t *testing.T, r mcp.Result) envelope {
	t.Helper()
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var e envelope
	if err = json.Unmarshal(b, &e); err != nil {
		t.Fatal(err)
	}
	return e
}
func answer[T any](t *testing.T, r mcp.Result) T {
	t.Helper()
	e := unpack(t, r)
	if r.IsError() || len(e.Content) != 1 || e.Content[0].Type != "text" || !bytes.Equal([]byte(e.Content[0].Text), e.Structured) {
		t.Fatalf("bad success %+v", e)
	}
	var v T
	if err := json.Unmarshal(e.Structured, &v); err != nil {
		t.Fatal(err)
	}
	return v
}
func refusal(t *testing.T, r mcp.Result, want string) {
	t.Helper()
	e := unpack(t, r)
	if !r.IsError() || len(e.Content) != 1 || e.Content[0].Type != "text" || e.Content[0].Text != want || len(e.Structured) != 0 {
		t.Fatalf("refusal %+v want %q", e, want)
	}
}
func (f *fixture) changes() []telemetry.Event {
	f.t.Helper()
	if err := f.w.Flush(context.Background()); err != nil {
		f.t.Fatal(err)
	}
	var out []telemetry.Event
	for _, e := range f.capture.Events() {
		if e.Name == trail.EventChangeFinished {
			out = append(out, e)
		}
	}
	return out
}
func key(n byte) string {
	b := binary.BigEndian.AppendUint32(nil, 11)
	b = append(b, []byte("ssh-ed25519")...)
	b = binary.BigEndian.AppendUint32(b, 32)
	b = append(b, bytes.Repeat([]byte{n}, 32)...)
	return "ssh-ed25519 " + base64.StdEncoding.EncodeToString(b) + " comment with spaces"
}

type dnsLookup struct {
	zone, name string
	deadline   time.Time
}

type dnsCloud struct {
	cloud.Cloud
	lookups []dnsLookup
	find    func(context.Context, string, string) ([]cloud.Record, error)
}

func (c *dnsCloud) FindRecords(ctx context.Context, zone, name string) ([]cloud.Record, error) {
	deadline, _ := ctx.Deadline()
	c.lookups = append(c.lookups, dnsLookup{zone: zone, name: name, deadline: deadline})
	if c.find != nil {
		return c.find(ctx, zone, name)
	}
	return nil, nil
}

func (f *fixture) seedHost(running bool) (store.Host, store.Operation) {
	f.t.Helper()
	ctx := context.Background()
	spec := store.HostSpec{Type: valid.DefaultType, RootGB: valid.DefaultRootGB, DataGB: valid.DefaultDataGB}
	h, op, err := f.st.CreateHost(ctx, spec, f.caller)
	if err != nil {
		f.t.Fatal(err)
	}
	if running {
		f.finish(op.ID, store.Update{InstanceID: "instance-" + h.Name, RootVolume: "root-" + h.Name, DataVolume: "data-" + h.Name, EIP: "192.0.2.10"})
	}
	return h, op
}

func (f *fixture) seedAccount(slug, host string) (store.Account, store.Operation) {
	f.t.Helper()
	a, op, err := f.st.PlaceAccount(context.Background(), slug, host, f.caller)
	if err != nil {
		f.t.Fatal(err)
	}
	return a, op
}

func (f *fixture) finish(id string, update store.Update) {
	f.t.Helper()
	if err := f.st.Finish(context.Background(), id, update); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) fail(id, why string) {
	f.t.Helper()
	if err := f.st.Fail(context.Background(), id, why); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) operation(id string) store.Operation {
	f.t.Helper()
	op, err := f.st.Operation(context.Background(), id)
	if err != nil {
		f.t.Fatal(err)
	}
	return op
}

func (f *fixture) host(name string) store.Host {
	f.t.Helper()
	h, err := f.st.Host(context.Background(), name)
	if err != nil {
		f.t.Fatal(err)
	}
	return h
}

func (f *fixture) user(account, name string) store.User {
	f.t.Helper()
	u, err := f.st.User(context.Background(), account, name)
	if err != nil {
		f.t.Fatal(err)
	}
	return u
}
