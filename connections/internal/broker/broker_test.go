package broker_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	connections "github.com/ikigenba/ikigenba/connections"
	"github.com/ikigenba/ikigenba/connections/internal/broker"
	"github.com/ikigenba/ikigenba/connections/internal/providers"
	"github.com/ikigenba/ikigenba/connections/internal/store"
	"github.com/ikigenba/ikigenba/connections/internal/trail"
)

type fixture struct {
	d      *db.DB
	st     *store.Store
	b      *broker.Broker
	cfg    broker.Config
	now    time.Time
	ctx    context.Context
	tc     *telemetry.Capture
	ec     *events.Capture
	forms  []url.Values
	status int
	body   string
	hook   func()
}

func setup(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{now: time.Date(2030, 1, 2, 3, 4, 5, 900, time.UTC), status: 200, body: `{"access_token":"new-access-token-sensitive","refresh_token":"new-refresh-token-sensitive","expires_in":3599,"scope":"openid","id_token":"x.eyJlbWFpbCI6ImFjY291bnRAZXhhbXBsZSIsInN1YiI6InN1YmplY3QifQ.x"}`, tc: &telemetry.Capture{}, ec: &events.Capture{}}
	now := func() time.Time { return f.now }
	d, err := db.Open(context.Background(), db.Config{Path: filepath.Join(t.TempDir(), "state", "connections.db"), Migrations: connections.Migrations(), Now: now})
	if err != nil {
		t.Fatal(err)
	}
	f.d = d
	t.Cleanup(func() { _ = d.Close() })
	f.st = store.New(d, store.Config{Now: now, Rand: bytes.NewReader(bytes.Repeat([]byte{91}, 2048))})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Error(err)
		}
		f.forms = append(f.forms, r.Form)
		if f.hook != nil {
			f.hook()
		}
		w.WriteHeader(f.status)
		_, _ = io.WriteString(w, f.body)
	}))
	t.Cleanup(srv.Close)
	p := providers.Table()[0]
	p.AuthorizeURL = srv.URL + "/authorize"
	p.TokenURL = srv.URL + "/token"
	p.RevokeURL = srv.URL + "/revoke"
	tw := telemetry.New(telemetry.Config{Service: "connections", Sink: f.tc, Now: now, Rand: bytes.NewReader(bytes.Repeat([]byte{17}, 2048))})
	em := events.New(events.Config{Service: "connections", Sink: f.ec, Emits: trail.Emits(), Now: now, Rand: bytes.NewReader(bytes.Repeat([]byte{18}, 2048))})
	t.Cleanup(func() { em.Shutdown(context.Background()); tw.Shutdown(context.Background(), "test") })
	f.cfg = broker.Config{Store: f.st, Clients: []providers.Client{{Provider: p, ID: "client", Secret: "client-secret-sensitive", HTTP: srv.Client(), Now: now}}, Telemetry: tw, Events: em, Now: now, Rand: bytes.NewReader(bytes.Repeat([]byte{90}, 512)), CallbackURL: "https://callback.example"}
	f.b = broker.New(f.cfg)
	f.ctx = identity.NewContext(context.Background(), identity.Caller{UserID: "user", Email: "user@example", RequestID: "request"})
	return f
}
func (f *fixture) flush(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := f.cfg.Telemetry.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	if err := f.cfg.Events.Flush(ctx); err != nil {
		t.Fatal(err)
	}
}
func (f *fixture) seed(t *testing.T, expiry time.Time) store.Connection {
	t.Helper()
	c, _, err := f.st.Upsert(f.ctx, store.Connection{UserID: "user", UserEmail: "user@example", Provider: "google", AccountEmail: "account@example", AccountSubject: "subject", RefreshToken: "old-refresh-token-sensitive", AccessToken: "old-access-token-sensitive", AccessExpires: expiry, Scopes: []string{"openid"}})
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func TestBegin(t *testing.T) {
	// R-G8HF-MKJM R-RKIP-GM3O R-RLQL-UDUD R-RO6E-LXBR R-RPEA-ZP2G
	f := setup(t)
	rnd := &recordReader{data: append(bytes.Repeat([]byte{90}, 16), bytes.Repeat([]byte{91}, 32)...)}
	f.cfg.Rand = rnd
	duplicate := f.cfg.Clients[0]
	duplicate.Provider.AuthorizeURL = "https://second.example/authorize"
	f.cfg.Clients = append(f.cfg.Clients, duplicate)
	f.b = broker.New(broker.Config{
		Clients: f.cfg.Clients, Store: f.cfg.Store,
		Telemetry: f.cfg.Telemetry, Events: f.cfg.Events,
		CallbackURL: f.cfg.CallbackURL, ServicesPath: f.cfg.ServicesPath,
		Now: f.cfg.Now, Rand: f.cfg.Rand,
	})
	loc, err := f.b.Begin(f.ctx, "google")
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(loc)
	if err != nil {
		t.Fatal(err)
	}
	s := broker.StatePrefix + strings.Repeat("5a", 16)
	v := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{91}, 32))
	q := u.Query()
	if !reflect.DeepEqual(rnd.reads, []int{16, 32}) || rnd.offset != 48 || loc != f.cfg.Clients[0].AuthorizeURL(s, v, "https://callback.example/google/callback") {
		t.Fatal("random order/client", rnd.reads, loc)
	}
	if q.Get("state") != s || q.Get("redirect_uri") != "https://callback.example/google/callback" || q.Get("code_challenge") != providers.Challenge(v) {
		t.Fatal(q)
	}
	pd, err := f.st.TakePending(f.ctx, s, "user", "google")
	if err != nil {
		t.Fatal(err)
	}
	want := store.Pending{State: s, Verifier: v, UserID: "user", UserEmail: "user@example", Provider: "google", Expires: f.now.Truncate(time.Second).Add(broker.PendingLifetime)}
	if !reflect.DeepEqual(pd, want) || len(f.forms) != 0 || broker.PendingLifetime != 10*time.Minute || broker.NoCallback == broker.NoRandom || broker.NoCallback == "" || broker.NoRandom == "" || strings.Contains(broker.NoCallback+broker.NoRandom, "\n") {
		t.Fatal(pd)
	}
}
func TestBeginFailures(t *testing.T) {
	// R-RRU3-R8JU
	for _, kind := range []string{"unknown", "callback", "random", "partial-random", "store"} {
		t.Run(kind, func(t *testing.T) {
			f := setup(t)
			p := "google"
			want := broker.ErrRandom
			rnd := &recordReader{}
			f.cfg.Rand = rnd
			switch kind {
			case "unknown":
				p = "nope"
				want = broker.ErrUnknownProvider
			case "callback":
				f.cfg.CallbackURL = ""
				want = broker.ErrNoCallback
			case "partial-random":
				f.cfg.Rand = bytes.NewReader(bytes.Repeat([]byte{90}, 47))
			case "store":
				f.cfg.Rand = bytes.NewReader(bytes.Repeat([]byte{90}, 48))
				f.d.SetFailing(true)
				want = store.ErrUnreachable
			}
			loc, err := broker.New(f.cfg).Begin(f.ctx, p)
			if loc != "" || !errors.Is(err, want) || len(f.forms) != 0 {
				t.Fatal(loc, err)
			}
			if (kind == "unknown" || kind == "callback") && len(rnd.reads) != 0 {
				t.Fatal("read randomness")
			}
			f.d.SetFailing(false)
			_, err = f.st.TakePending(f.ctx, broker.StatePrefix+strings.Repeat("5a", 16), "user", "google")
			if !errors.Is(err, store.ErrNotFound) {
				t.Fatal(err)
			}
			f.flush(t)
			if len(f.tc.Events())+len(f.ec.Events()) != 0 {
				t.Fatal("events")
			}
		})
	}
}
func TestComplete(t *testing.T) {
	// R-QU98-V2YT R-RVHS-WJRX
	f := setup(t)
	loc, err := f.b.Begin(f.ctx, "google")
	if err != nil {
		t.Fatal(err)
	}
	u, _ := url.Parse(loc)
	q := url.Values{"state": {u.Query().Get("state")}, "code": {"authorization-code-sensitive"}}
	c, err := f.b.Complete(f.ctx, "google", q)
	if err != nil {
		t.Fatal(err)
	}
	got, err := f.st.Get(f.ctx, "user", "google")
	if err != nil || !reflect.DeepEqual(c, got) || c.AccountEmail != "account@example" || c.AccountSubject != "subject" || c.UserID != "user" || c.UserEmail != "user@example" || c.Provider != "google" || c.RefreshToken != "new-refresh-token-sensitive" || !c.AccessExpires.Equal(f.now.Truncate(time.Second).Add(3599*time.Second)) || !reflect.DeepEqual(c.Scopes, []string{"openid"}) || c.AccessToken != "new-access-token-sensitive" {
		t.Fatal(c, err)
	}
	if _, replayErr := f.b.Complete(f.ctx, "google", q); !errors.Is(replayErr, broker.ErrUnknown) {
		t.Fatal("replay", replayErr)
	}
	form := f.forms[0]
	if form.Get("redirect_uri") != u.Query().Get("redirect_uri") || providers.Challenge(form.Get("code_verifier")) != u.Query().Get("code_challenge") {
		t.Fatal("PKCE")
	}
	f.assertEvents(t, trail.Created, c)
	if len(f.tc.Events()) != 1 || len(f.ec.Events()) != 1 || f.tc.Events()[0].Name != trail.Created {
		t.Fatal("events")
	}
	if _, err = f.st.MarkBroken(f.ctx, "user", "google"); err != nil {
		t.Fatal(err)
	}
	f.body = `{"access_token":"reconnected-access-sensitive","refresh_token":"reconnected-refresh-sensitive","expires_in":1234,"scope":"extra","id_token":"x.eyJlbWFpbCI6Im5ldy1hY2NvdW50QGV4YW1wbGUiLCJzdWIiOiJuZXctc3ViamVjdCJ9.x"}`
	loc, err = f.b.Begin(f.ctx, "google")
	if err != nil {
		t.Fatal(err)
	}
	u, _ = url.Parse(loc)
	q.Set("state", u.Query().Get("state"))
	c2, err := f.b.Complete(f.ctx, "google", q)
	if err != nil || c.ID != c2.ID || !c.Created.Equal(c2.Created) || c2.Status != store.StatusOK || c2.AccountEmail != "new-account@example" || c2.AccountSubject != "new-subject" || c2.RefreshToken != "reconnected-refresh-sensitive" || c2.AccessToken != "reconnected-access-sensitive" || !c2.AccessExpires.Equal(f.now.Truncate(time.Second).Add(1234*time.Second)) || !reflect.DeepEqual(c2.Scopes, []string{"extra"}) {
		t.Fatal("reconnect", err)
	}
	f.flush(t)
	if len(f.ec.Events()) != 2 || len(f.tc.Events()) != 2 || f.tc.Events()[1].Name != trail.Created || f.tc.Events()[1].Attrs["account"] != c2.AccountEmail || f.tc.Events()[1].Attrs["scopes"] != "extra" {
		t.Fatal("reconnect event")
	}
}
func TestCompleteFailures(t *testing.T) {
	// R-RT20-50AJ
	for _, kind := range []string{"missing", "unknown", "provider", "expired", "foreign", "denied", "code", "refused", "failed", "account", "store"} {
		t.Run(kind, func(t *testing.T) {
			f := setup(t)
			previous := f.seed(t, f.now.Add(time.Hour))
			q := url.Values{"state": {"pending-sensitive-state"}, "code": {"authorization-code-sensitive"}}
			pd := store.Pending{State: q.Get("state"), Verifier: "pending-verifier-sensitive", UserID: "user", Provider: "google", Expires: f.now.Add(time.Hour)}
			want := broker.ErrExchange
			consumed := true
			switch kind {
			case "missing":
				q.Del("state")
				want = broker.ErrMissing
				consumed = false
			case "unknown":
				want = broker.ErrUnknown
				consumed = false
			case "provider":
				pd.Provider = "other"
				want = broker.ErrUnknown
				consumed = false
			case "expired":
				want = broker.ErrExpired
				q.Set("error", "denied")
			case "foreign":
				pd.UserID = "other"
				want = broker.ErrForeign
				q.Set("error", "denied")
				consumed = false
			case "denied":
				q["error"] = []string{""}
				q.Del("code")
				want = broker.ErrDenied
			case "code":
				q.Del("code")
			case "refused":
				f.status = 400
			case "failed":
				f.status = 502
			case "account":
				f.body = `{"access_token":"access","refresh_token":"refresh","expires_in":60}`
				want = broker.ErrAccount
			case "store":
				want = store.ErrUnreachable
				consumed = false
			}
			if kind != "unknown" {
				if err := f.st.PutPending(f.ctx, pd); err != nil {
					t.Fatal(err)
				}
			}
			if kind == "expired" {
				f.now = f.now.Add(2 * time.Hour)
			}
			if kind == "store" {
				f.d.SetFailing(true)
			}
			c, err := f.b.Complete(f.ctx, "google", q)
			if !errors.Is(err, want) || !reflect.DeepEqual(c, store.Connection{}) {
				t.Fatal(c, err)
			}
			f.d.SetFailing(false)
			held, heldErr := f.st.Get(f.ctx, "user", "google")
			if heldErr != nil || !reflect.DeepEqual(held, previous) {
				t.Fatal("existing grant changed", heldErr)
			}
			if consumed {
				_, err = f.st.TakePending(f.ctx, pd.State, pd.UserID, pd.Provider)
				if !errors.Is(err, store.ErrNotFound) {
					t.Fatal("state survived", err)
				}
			}
			if kind == "foreign" || kind == "provider" {
				if _, err = f.st.TakePending(f.ctx, pd.State, pd.UserID, pd.Provider); err != nil {
					t.Fatal("state lost", err)
				}
			}
			if kind != "refused" && kind != "failed" && kind != "account" && len(f.forms) != 0 {
				t.Fatal("provider called")
			}
			f.flush(t)
			if len(f.tc.Events())+len(f.ec.Events()) != 0 {
				t.Fatal("events")
			}
		})
	}
}
func TestErrorsAndJSON(t *testing.T) {
	// R-RMYI-85L2 R-S8WP-40XK R-S7OS-Q96V
	errs := []error{broker.ErrUnknownProvider, broker.ErrNoCallback, broker.ErrRandom, broker.ErrMissing, broker.ErrUnknown, broker.ErrExpired, broker.ErrForeign, broker.ErrDenied, broker.ErrExchange, broker.ErrAccount, broker.ErrReconnect, broker.ErrProvider}
	others := []error{store.ErrNotFound, store.ErrExpired, store.ErrForeign, store.ErrUnreachable, providers.ErrRefused, providers.ErrFailed, providers.ErrAccount}
	for i, e := range errs {
		if e == nil {
			t.Fatal("nil")
		}
		for j, o := range errs {
			if i != j && errors.Is(e, o) {
				t.Fatal("overlap")
			}
		}
		for _, o := range others {
			if errors.Is(e, o) {
				t.Fatal("wrap")
			}
		}
	}
	zero, zeroErr := marshalValue(broker.Token{})
	if zeroErr != nil || string(zero) != `{"access_token":"","expires":"","account":"","scopes":[]}` {
		t.Fatal(string(zero), zeroErr)
	}
	b, err := marshalValue(broker.Token{AccessToken: "a", Expires: "e", Account: "c", Scopes: []string{}})
	if err != nil || string(b) != `{"access_token":"a","expires":"e","account":"c","scopes":[]}` {
		t.Fatal(string(b), err)
	}
	b, err = json.Marshal(broker.Refusal{Error: broker.RefusalReconnect, URL: "u"})
	if err != nil || string(b) != `{"error":"reconnect","url":"u"}` {
		t.Fatal(string(b), err)
	}
	b, _ = json.Marshal(broker.Refusal{Error: broker.RefusalMethod})
	if string(b) != `{"error":"method_not_allowed"}` {
		t.Fatal(string(b))
	}
	if broker.RefusalUnknownProvider != "unknown_provider" || broker.RefusalNotConnected != "not_connected" || broker.RefusalProvider != "provider" || broker.RefusalUnavailable != "unavailable" || broker.RefreshMargin != 2*time.Minute {
		t.Fatal("constants")
	}
}

func marshalValue(v any) ([]byte, error) { return json.Marshal(v) }

func TestCompleteUnavailableAndUnknown(t *testing.T) {
	// R-RWPP-ABIM
	for _, kind := range []string{"read", "write", "unknown"} {
		t.Run(kind, func(t *testing.T) {
			f := setup(t)
			loc, err := f.b.Begin(f.ctx, "google")
			if err != nil {
				t.Fatal(err)
			}
			u, _ := url.Parse(loc)
			q := url.Values{"state": {u.Query().Get("state")}, "code": {"authorization-code-sensitive"}}
			p := "google"
			want := store.ErrUnreachable
			if kind == "write" {
				f.hook = func() { f.d.SetFailing(true) }
			} else {
				f.d.SetFailing(true)
			}
			if kind == "unknown" {
				p = "absent"
				want = broker.ErrUnknownProvider
			}
			c, err := f.b.Complete(f.ctx, p, q)
			if !errors.Is(err, want) || !reflect.DeepEqual(c, store.Connection{}) {
				t.Fatal(c, err)
			}
			f.d.SetFailing(false)
			_, err = f.st.Get(f.ctx, "user", "google")
			if !errors.Is(err, store.ErrNotFound) {
				t.Fatal("connection stored")
			}
			if kind != "write" && len(f.forms) != 0 {
				t.Fatal("provider called")
			}
			f.flush(t)
			if len(f.tc.Events())+len(f.ec.Events()) != 0 {
				t.Fatal("events")
			}
		})
	}
}

type recordReader struct {
	data   []byte
	offset int
	reads  []int
}

func (r *recordReader) Read(p []byte) (int, error) {
	r.reads = append(r.reads, len(p))
	n := copy(p, r.data[r.offset:])
	r.offset += n
	if n == 0 {
		return 0, io.EOF
	}
	return n, nil
}

func (f *fixture) assertEvents(t *testing.T, name string, c store.Connection) {
	t.Helper()
	f.flush(t)
	te := f.tc.Events()
	be := f.ec.Events()
	if len(te) != 1 || len(be) != 1 {
		t.Fatal("event count", len(te), len(be))
	}
	want := telemetry.Attrs{"provider": c.Provider, "account": c.AccountEmail, "scopes": strings.Join(c.Scopes, " ")}
	if te[0].Name != name || te[0].RequestID != "request" || te[0].User != "user" || !reflect.DeepEqual(te[0].Attrs, want) || be[0].Name != name || be[0].RequestID != "request" || be[0].User != "user" || !reflect.DeepEqual(telemetry.Attrs(be[0].Attrs), want) {
		t.Fatal("event contract", te, be)
	}
}
