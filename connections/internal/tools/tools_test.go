package tools

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	connections "github.com/ikigenba/ikigenba/connections"
	"github.com/ikigenba/ikigenba/connections/internal/broker"
	"github.com/ikigenba/ikigenba/connections/internal/providers"
	"github.com/ikigenba/ikigenba/connections/internal/store"
	"github.com/ikigenba/ikigenba/connections/internal/trail"
)

// R-AKQ5-RX4S R-QOHX-7SB9 R-AODU-X8CV R-APLR-B03K R-AQTN-ORU9
func TestCatalogAndStatus(t *testing.T) {
	if StatusConnected != "connected" || StatusBroken != "broken" || StatusNotConnected != "not_connected" {
		t.Fatal("status constants")
	}
	for _, s := range []string{CredentialGuide, ConnectInstructions, UnknownProvider("input-name"), NoConnection("input-name")} {
		if s == "" || strings.Contains(s, "\n") {
			t.Fatal(s)
		}
	}
	if !strings.Contains(UnknownProvider("input-name"), "input-name") || !strings.Contains(NoConnection("input-name"), "input-name") || UnknownProvider("input-name") == NoConnection("input-name") || UnknownProvider("input-name") == store.Unreachable || NoConnection("input-name") == store.Unreachable {
		t.Fatal("copy functions")
	}
	cat := Catalog()
	if len(cat) != 3 {
		t.Fatal(cat)
	}
	seen := map[string]bool{}
	for i, n := range []string{"list", "connect", "disconnect"} {
		e := cat[i]
		first := FirstLine(e.Description)
		if e.Name != n || first == "" || seen[first] || e.Description != first+"\n\n"+CredentialGuide {
			t.Fatal(e)
		}
		seen[first] = true
	}
	for _, s := range []string{"", "first", "first\nsecond", "\nsecond"} {
		want, _, _ := strings.Cut(s, "\n")
		if FirstLine(s) != want {
			t.Fatal(s)
		}
	}
	p := providers.Table()[0]
	scopes := []string{"extra"}
	for _, s := range p.Scopes {
		scopes = append(scopes, p.GrantedName(s))
	}
	for i, j := 0, len(scopes)-1; i < j; i, j = i+1, j-1 {
		scopes[i], scopes[j] = scopes[j], scopes[i]
	}
	c := store.Connection{Status: store.StatusOK, Scopes: scopes}
	if StatusOf(p, c) != StatusConnected {
		t.Fatal(c)
	}
	c.Status = store.StatusBroken
	if StatusOf(p, c) != StatusBroken {
		t.Fatal(c)
	}
	c.Status = store.StatusOK
	for _, missing := range p.Scopes {
		c.Scopes = nil
		for _, s := range scopes {
			if s != p.GrantedName(missing) {
				c.Scopes = append(c.Scopes, s)
			}
		}
		if StatusOf(p, c) != StatusBroken {
			t.Fatal(missing)
		}
	}
}

type fixture struct {
	d          *db.DB
	st         *store.Store
	client     *mcp.Client
	w          *telemetry.Writer
	em         *events.Emitter
	tc         *telemetry.Capture
	ec         *events.Capture
	calls      []string
	status     int
	failDelete bool
	flowRand   *countedRand
	p          providers.Provider
	now        time.Time
}

func setup(t *testing.T, base string) *fixture {
	t.Helper()
	t.Setenv(services.Variable, "")
	f := &fixture{tc: &telemetry.Capture{}, ec: &events.Capture{}, status: 200, now: time.Date(2025, 3, 4, 5, 6, 7, 0, time.UTC)}
	f.flowRand = &countedRand{reader: bytes.NewReader(bytes.Repeat([]byte("b"), 256))}
	now := func() time.Time { return f.now }
	var err error
	f.d, err = db.Open(context.Background(), db.Config{Path: filepath.Join(t.TempDir(), "connections.db"), Migrations: connections.Migrations(), Now: now, Service: "connections", Stderr: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.d.Close() })
	f.st = store.New(f.d, store.Config{Now: now, Rand: bytes.NewReader([]byte("abcdefghABCDEFGHijklmnopIJKLMNOPqrstuvwxQRSTUVWX"))})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		f.calls = append(f.calls, r.Form.Get("token"))
		if f.failDelete {
			f.d.SetFailing(true)
		}
		w.WriteHeader(f.status)
	}))
	t.Cleanup(upstream.Close)
	f.p = providers.Provider{Name: "google", DisplayName: "provider-display", RevokeURL: upstream.URL, Scopes: []string{"scope-one"}}
	f.w = telemetry.New(telemetry.Config{Service: "connections", Sink: f.tc, Now: now, Stderr: io.Discard, Rand: bytes.NewReader(make([]byte, 4096))})
	f.em = events.New(events.Config{Service: "connections", Sink: f.ec, Emits: trail.Emits(), Now: now, Stderr: io.Discard, Rand: bytes.NewReader(make([]byte, 4096))})
	t.Cleanup(func() { f.em.Shutdown(context.Background()); f.w.Shutdown(context.Background(), "") })
	b := broker.New(broker.Config{Clients: []providers.Client{{Provider: f.p, HTTP: &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		if f.status == 0 {
			if err := r.ParseForm(); err != nil {
				return nil, err
			}
			f.calls = append(f.calls, r.Form.Get("token"))
			if f.failDelete {
				f.d.SetFailing(true)
			}
			return nil, errors.New("injected provider transport failure")
		}
		return http.DefaultTransport.RoundTrip(r)
	})}, Now: now}}, Store: f.st, Telemetry: f.w, Events: f.em, Now: now, CallbackURL: "https://connections.example.test", Rand: f.flowRand})
	srv := mcp.NewServer(mcp.ServerConfig{Name: "connections", Telemetry: f.w})
	Register(srv, Config{Base: base, Providers: []providers.Provider{f.p, {Name: "second", DisplayName: "second-display"}}, Store: f.st, Broker: b})
	server := httptest.NewServer(events.Middleware(identity.Require(srv)))
	t.Cleanup(server.Close)
	f.client = mcp.NewClient(mcp.ClientConfig{Endpoint: server.URL, HTTPClient: &http.Client{Transport: transportFunc(func(r *http.Request) (*http.Response, error) {
		events.Forward(r.Context(), r)
		return http.DefaultTransport.RoundTrip(r)
	})}})
	return f
}
func (f *fixture) put(t *testing.T, user string) store.Connection {
	t.Helper()
	c, _, err := f.st.Upsert(context.Background(), store.Connection{UserID: user, UserEmail: user + "@example.test", Provider: f.p.Name, AccountEmail: user + "-account@example.test", RefreshToken: user + "-refresh-secret", AccessToken: user + "-access-secret", Scopes: []string{"scope-one", "extra"}})
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func (f *fixture) call(t *testing.T, user, name string, args any) mcp.Result {
	t.Helper()
	raw, err := json.Marshal(args)
	if err != nil {
		t.Fatal(err)
	}
	r, err := f.client.CallTool(events.NewContext(context.Background(), events.Cause{ID: "evt_0123456789abcdef", Depth: 2}), identity.Caller{UserID: user, Email: user + "@example.test", RequestID: "req-test"}, name, raw)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func decode(t *testing.T, r mcp.Result, out any) {
	t.Helper()
	if r.IsError() {
		t.Fatal(r)
	}
	raw, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var obj map[string]json.RawMessage
	if err = json.Unmarshal(raw, &obj); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(obj["structuredContent"], out); err != nil {
		t.Fatal(err)
	}
}
func refused(t *testing.T, r mcp.Result, want string) {
	t.Helper()
	if !r.IsError() {
		t.Fatal("success")
	}
	raw, _ := json.Marshal(r)
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		t.Fatal(err)
	}
	if _, ok := obj["structuredContent"]; ok {
		t.Fatal(string(raw))
	}
	var content []struct{ Type, Text string }
	if err := json.Unmarshal(obj["content"], &content); err != nil {
		t.Fatal(err)
	}
	if len(content) != 1 || content[0].Type != "text" || content[0].Text != want {
		t.Fatal(string(raw))
	}
}
func (f *fixture) flush(t *testing.T) {
	t.Helper()
	if err := f.w.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := f.em.Flush(context.Background()); err != nil {
		t.Fatal(err)
	}
}

// R-AH2G-MLWP R-AIAD-0DNE R-O3OT-KL5R R-AS1K-2JKY R-O64M-C4N5 R-AUHC-U32C R-AY51-ZEAF R-AN5Y-JGM6 R-B0KU-QXRT R-B30N-IH97
func TestToolsThroughClient(t *testing.T) {
	for _, base := range []string{"", "https://connections.example.test"} {
		t.Run(base, func(t *testing.T) {
			f := setup(t, base)
			other := f.put(t, "other")
			c := f.put(t, "caller")
			pending := store.Pending{State: "existing-pending", Verifier: "existing-verifier", UserID: "caller", Provider: "google", Expires: f.now.Add(time.Hour)}
			if err := f.st.PutPending(context.Background(), pending); err != nil {
				t.Fatal(err)
			}
			infos, err := f.client.ListTools(context.Background(), identity.Caller{UserID: "caller"})
			if err != nil {
				t.Fatal(err)
			}
			cat := Catalog()
			if len(infos) != len(cat) {
				t.Fatal(infos)
			}
			for i, info := range infos {
				effect := mcp.Read
				if i == 2 {
					effect = mcp.Destructive
				}
				if info.Name != cat[i].Name || info.Description != cat[i].Description || info.Effect() != effect {
					t.Fatal(info)
				}
				if i > 0 {
					var schema struct{ Required []string }
					if err = json.Unmarshal(info.InputSchema, &schema); err != nil {
						t.Fatal(err)
					}
					if !reflect.DeepEqual(schema.Required, []string{"provider"}) {
						t.Fatal(schema)
					}
				}
			}
			var list ConnectionList
			result := f.call(t, "caller", "list", ListArgs{})
			decode(t, result, &list)
			connected := c.Created.UTC().Format(time.RFC3339)
			account := c.AccountEmail
			want := ConnectionList{Connections: []Listed{{Provider: f.p.Name, DisplayName: f.p.DisplayName, Status: StatusConnected, Account: &account, Scopes: c.Scopes, Connected: &connected}, {Provider: "second", DisplayName: "second-display", Status: StatusNotConnected, Scopes: []string{}}}}
			if !reflect.DeepEqual(list, want) {
				t.Fatalf("%#v", list)
			}
			var raw map[string]json.RawMessage
			decode(t, result, &raw)
			var rows []map[string]json.RawMessage
			if err = json.Unmarshal(raw["connections"], &rows); err != nil {
				t.Fatal(err)
			}
			if _, ok := rows[1]["account"]; ok {
				t.Fatal(rows)
			}
			if _, ok := rows[1]["connected"]; ok {
				t.Fatal(rows)
			}
			if string(rows[1]["scopes"]) != "[]" {
				t.Fatal(rows)
			}
			for _, user := range []string{"caller", "absent"} {
				var link Link
				decode(t, f.call(t, user, "connect", ProviderArgs{Provider: "google"}), &link)
				if link != (Link{URL: base + "/google/connect", Instructions: ConnectInstructions}) {
					t.Fatal(link)
				}
			}
			refused(t, f.call(t, "caller", "connect", ProviderArgs{Provider: "nope"}), UnknownProvider("nope"))
			refused(t, f.call(t, "caller", "disconnect", ProviderArgs{Provider: "nope"}), UnknownProvider("nope"))
			refused(t, f.call(t, "absent", "disconnect", ProviderArgs{Provider: "google"}), NoConnection("google"))
			var absent ConnectionList
			decode(t, f.call(t, "absent", "list", ListArgs{}), &absent)
			if absent.Connections[0].Status != StatusNotConnected {
				t.Fatal(absent)
			}
			check, err := f.st.Get(context.Background(), "other", "google")
			if err != nil || !reflect.DeepEqual(check, other) {
				t.Fatal(check, err)
			}
			check, err = f.st.Get(context.Background(), "caller", "google")
			if err != nil || !reflect.DeepEqual(check, c) {
				t.Fatal(check, err)
			}
			if f.flowRand.n != 0 {
				t.Fatal("connect used randomness", f.flowRand.n)
			}
			pd, err := f.st.TakePending(context.Background(), pending.State, "caller", "google")
			if err != nil || !reflect.DeepEqual(pd, pending) {
				t.Fatal(pd, err)
			}
			if _, err = f.st.TakePending(context.Background(), broker.StatePrefix+hex.EncodeToString(bytes.Repeat([]byte("b"), 16)), "caller", "google"); !errors.Is(err, store.ErrNotFound) {
				t.Fatal(err)
			}
			f.flush(t)
			assertNoChanges(t, f)
			if len(f.calls) != 0 || len(f.ec.Events()) != 0 {
				t.Fatal("side effects")
			}
		})
	}
}

// R-WJ0G-MHYK R-WMO5-RT6N R-WNW2-5KXC
func TestDisconnectFailuresAndSuccess(t *testing.T) {
	for _, status := range []int{200, 400, 0} {
		for _, fail := range []bool{false, true} {
			t.Run(strconv.Itoa(status)+map[bool]string{true: "fail", false: "ok"}[fail], func(t *testing.T) {
				f := setup(t, "")
				c := f.put(t, "caller")
				f.status = status
				f.failDelete = fail
				r := f.call(t, "caller", "disconnect", ProviderArgs{Provider: "google"})
				if fail {
					refused(t, r, store.Unreachable)
				} else {
					var deleted Deleted
					decode(t, r, &deleted)
					if deleted != (Deleted{Deleted: true, ID: c.ID}) {
						t.Fatal(deleted)
					}
					var list ConnectionList
					decode(t, f.call(t, "caller", "list", ListArgs{}), &list)
					if list.Connections[0].Status != StatusNotConnected {
						t.Fatal(list)
					}
				}
				f.flush(t)
				if !reflect.DeepEqual(f.calls, []string{c.RefreshToken}) {
					t.Fatal(f.calls)
				}
				deleted, revoke := 0, 0
				for _, e := range f.tc.Events() {
					switch e.Name {
					case trail.Deleted:
						deleted++
						if !reflect.DeepEqual(e.Attrs, telemetry.Attrs{"provider": c.Provider, "account": c.AccountEmail, "scopes": strings.Join(c.Scopes, " ")}) || e.User != "caller" || e.RequestID != "req-test" {
							t.Fatal(e)
						}
					case trail.RevokeFailed:
						revoke++
						if !reflect.DeepEqual(e.Attrs, telemetry.Attrs{"provider": c.Provider, "account": c.AccountEmail, "status": int64(status)}) || e.User != "caller" || e.RequestID != "req-test" {
							t.Fatal(e)
						}
					}
				}
				wantDeleted := 1
				if fail {
					wantDeleted = 0
				}
				wantRevoke := 0
				if status != 200 {
					wantRevoke = 1
				}
				if deleted != wantDeleted || revoke != wantRevoke || len(f.ec.Events()) != wantDeleted {
					t.Fatal(deleted, revoke, f.ec.Events())
				}
				if !fail {
					e := f.ec.Events()[0]
					if e.Name != trail.Deleted || !reflect.DeepEqual(e.Attrs, events.Attrs{"provider": c.Provider, "account": c.AccountEmail, "scopes": strings.Join(c.Scopes, " ")}) || e.User != "caller" || e.RequestID != "req-test" || e.Cause != "evt_0123456789abcdef" || e.Depth != 3 {
						t.Fatal(e)
					}
				}
				f.d.SetFailing(false)
				_, err := f.st.Get(context.Background(), "caller", "google")
				if fail && err != nil || !fail && !errors.Is(err, store.ErrNotFound) {
					t.Fatal(err)
				}
			})
		}
	}
	f := setup(t, "")
	c := f.put(t, "caller")
	f.d.SetFailing(true)
	refused(t, f.call(t, "caller", "list", ListArgs{}), store.Unreachable)
	refused(t, f.call(t, "caller", "disconnect", ProviderArgs{Provider: "google"}), store.Unreachable)
	f.flush(t)
	assertNoChanges(t, f)
	if len(f.calls) != 0 || len(f.ec.Events()) != 0 {
		t.Fatal("side effects")
	}
	f.d.SetFailing(false)
	got, err := f.st.Get(context.Background(), "caller", "google")
	if err != nil || !reflect.DeepEqual(got, c) {
		t.Fatal(got, err)
	}
}

func assertNoChanges(t *testing.T, f *fixture) {
	t.Helper()
	for _, e := range f.tc.Events() {
		if strings.HasPrefix(e.Name, "connection.") {
			t.Fatal(e)
		}
	}
}
func TestListBrokenStatus(t *testing.T) {
	f := setup(t, "")
	c := f.put(t, "caller")
	for _, broken := range []bool{false, true} {
		if broken {
			var err error
			c, err = f.st.MarkBroken(context.Background(), "caller", "google")
			if err != nil {
				t.Fatal(err)
			}
		} else {
			var err error
			c, err = f.st.SetAccess(context.Background(), "caller", "google", store.Access{Token: "supplied-token", Expires: f.now.Add(time.Hour), Scopes: []string{"extra"}})
			if err != nil {
				t.Fatal(err)
			}
		}
		var list ConnectionList
		decode(t, f.call(t, "caller", "list", ListArgs{}), &list)
		if list.Connections[0].Status != StatusOf(f.p, c) || list.Connections[0].Status != StatusBroken || !reflect.DeepEqual(list.Connections[0].Scopes, c.Scopes) {
			t.Fatal(list)
		}
	}
}

type transportFunc func(*http.Request) (*http.Response, error)

func (f transportFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

type countedRand struct {
	n      int
	reader io.Reader
}

func (r *countedRand) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.n += n
	return n, err
}
