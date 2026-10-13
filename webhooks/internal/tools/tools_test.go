package tools_test

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/paging"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/webhooks"
	"github.com/ikigenba/ikigenba/webhooks/internal/ingress"
	"github.com/ikigenba/ikigenba/webhooks/internal/store"
	"github.com/ikigenba/ikigenba/webhooks/internal/tools"
	"github.com/ikigenba/ikigenba/webhooks/internal/trail"
	"github.com/ikigenba/ikigenba/webhooks/internal/urls"
)

const base = "https://webhooks.sbx.example"

type fixture struct {
	t      *testing.T
	ctx    context.Context
	d      *db.DB
	st     *store.Store
	w      *telemetry.Writer
	em     *events.Emitter
	tc     *telemetry.Capture
	ec     *events.Capture
	client *mcp.Client
	caller identity.Caller
	mu     sync.Mutex
	now    time.Time
	serial int
}

func parse(s string) time.Time {
	v, e := time.Parse(time.RFC3339, s)
	if e != nil {
		panic(e)
	}
	return v
}

func pattern(n int) []byte {
	data := make([]byte, n)
	for i := range data {
		data[i] = byte(i/8 + 1)
	}
	return data
}

func setup(t *testing.T) *fixture {
	t.Helper()
	t.Setenv("IKIGENBA_SERVICES", "")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	f := &fixture{t: t, ctx: ctx, now: parse("2026-10-05T09:32:00Z"), tc: &telemetry.Capture{}, ec: &events.Capture{}, caller: identity.Caller{UserID: "owner", Email: "mg@example.com"}}
	now := func() time.Time { f.mu.Lock(); defer f.mu.Unlock(); return f.now }
	d, e := db.Open(ctx, db.Config{Path: filepath.Join(t.TempDir(), "state", "webhooks.db"), Migrations: webhooks.Migrations(), Now: now})
	if e != nil {
		t.Fatal(e)
	}
	f.d = d
	t.Cleanup(func() {
		if err := d.Close(); err != nil {
			t.Error(err)
		}
	})
	f.st = store.New(d, store.Config{Now: now, Rand: bytes.NewReader(pattern(40000))})
	f.w = telemetry.New(telemetry.Config{Service: "webhooks", Sink: f.tc, Now: now})
	f.em = events.New(events.Config{Service: "webhooks", Sink: f.ec, Now: now, Rand: bytes.NewReader(pattern(40000)), Telemetry: f.w, Emits: trail.Emits()})
	srv := mcp.NewServer(mcp.ServerConfig{Name: "webhooks", Version: "test", Telemetry: f.w})
	tools.Register(srv, tools.Config{Store: f.st, Telemetry: f.w, Events: f.em})
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		srv.ServeHTTP(w, r.WithContext(urls.NewContext(r.Context(), base)))
	})
	server := httptest.NewServer(telemetry.Middleware(f.w, events.Middleware(identity.Require(h))))
	t.Cleanup(server.Close)
	f.client = mcp.NewClient(mcp.ClientConfig{Endpoint: server.URL + "/mcp"})
	return f
}

func (f *fixture) as(user, email string) { f.caller = identity.Caller{UserID: user, Email: email} }

func (f *fixture) flush() {
	f.t.Helper()
	if e := f.em.Flush(f.ctx); e != nil {
		f.t.Fatal(e)
	}
	if e := f.w.Flush(f.ctx); e != nil {
		f.t.Fatal(e)
	}
}

func (f *fixture) content() []store.Webhook {
	f.t.Helper()
	xs, e := f.st.List(f.ctx, "", paging.MaxLimit+1)
	if e != nil {
		f.t.Fatal(e)
	}
	return xs
}

func (f *fixture) call(name string, args any) map[string]json.RawMessage {
	f.t.Helper()
	b, e := json.Marshal(args)
	if e != nil {
		f.t.Fatal(e)
	}
	f.serial++
	f.caller.RequestID = fmt.Sprintf("%032x", f.serial)
	r, e := f.client.CallTool(f.ctx, f.caller, name, b)
	if e != nil {
		f.t.Fatal(e)
	}
	raw, e := r.MarshalJSON()
	if e != nil {
		f.t.Fatal(e)
	}
	var obj map[string]json.RawMessage
	if e = json.Unmarshal(raw, &obj); e != nil {
		f.t.Fatal(e)
	}
	return obj
}

func success(t *testing.T, r map[string]json.RawMessage, out any) {
	t.Helper()
	if _, ok := r["isError"]; ok {
		t.Fatalf("refused: %s", r["content"])
	}
	if e := json.Unmarshal(r["structuredContent"], out); e != nil {
		t.Fatal(e)
	}
}

func refusal(t *testing.T, r map[string]json.RawMessage, want string) {
	t.Helper()
	delete(r, "_meta")
	got, e := json.Marshal(r)
	if e != nil {
		t.Fatal(e)
	}
	b, e := mcp.ErrorResult(want).MarshalJSON()
	if e != nil {
		t.Fatal(e)
	}
	var a, c any
	if e = json.Unmarshal(got, &a); e != nil {
		t.Fatal(e)
	}
	if e = json.Unmarshal(b, &c); e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(a, c) {
		t.Fatalf("refusal got %s want %s", got, b)
	}
}

// unchanged calls a tool, expects the refusal, and checks nothing changed or was emitted.
func (f *fixture) unchanged(name string, args any, want string) {
	f.t.Helper()
	before := f.content()
	f.flush()
	n, m := len(f.ec.Events()), len(f.tc.Events())
	refusal(f.t, f.call(name, args), want)
	f.flush()
	if !reflect.DeepEqual(before, f.content()) {
		f.t.Fatal("refusal changed state")
	}
	if len(f.ec.Events()) != n {
		f.t.Fatal("refusal emitted on the bus")
	}
	for _, e := range f.tc.Events()[m:] {
		if strings.HasPrefix(e.Name, "webhook.") {
			f.t.Fatal("refusal recorded a webhook event")
		}
	}
}

func (f *fixture) create(slug, scheme string) tools.Minted {
	f.t.Helper()
	args := map[string]any{"slug": slug}
	if scheme != "" {
		args["scheme"] = scheme
	}
	var m tools.Minted
	success(f.t, f.call("create", args), &m)
	return m
}

func (f *fixture) receive(slug string, a store.Arrival) store.Delivery {
	f.t.Helper()
	h, e := f.st.Get(f.ctx, slug)
	if e != nil {
		f.t.Fatal(e)
	}
	d, e := f.st.Receive(f.ctx, h.ID, a)
	if e != nil {
		f.t.Fatal(e)
	}
	return d
}

func bearerHeader(secret string) http.Header {
	h := http.Header{}
	h.Set(ingress.AuthHeader, secret)
	return h
}

func signedHeader(secret string, body []byte) http.Header {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write(body)
	h := http.Header{}
	h.Set(ingress.SignatureHeader, ingress.SignaturePrefix+hex.EncodeToString(mac.Sum(nil)))
	return h
}

func lifecycle(t *testing.T, f *fixture, kind string, x tools.Minted, requestID string) {
	t.Helper()
	f.flush()
	name := trail.Name(x.Slug, kind)
	attrs := map[string]any{"hook": x.ID, "scheme": x.Scheme}
	var bus []events.Event
	for _, e := range f.ec.Events() {
		if e.Name == name {
			bus = append(bus, e)
		}
	}
	if len(bus) != 1 || !reflect.DeepEqual(map[string]any(bus[0].Attrs), attrs) || bus[0].User != f.caller.UserID || bus[0].RequestID != requestID || bus[0].Cause != "" || bus[0].Depth != 0 {
		t.Fatalf("bus event %s: %+v", name, bus)
	}
	var rec []telemetry.Event
	for _, e := range f.tc.Events() {
		if e.Name == name {
			rec = append(rec, e)
		}
	}
	if len(rec) != 1 || rec[0].User != f.caller.UserID || rec[0].RequestID != requestID {
		t.Fatalf("trail event %s: %+v", name, rec)
	}
	got, _ := json.Marshal(rec[0].Attrs)
	want, _ := json.Marshal(attrs)
	if string(got) != string(want) {
		t.Fatalf("trail attrs %s want %s", got, want)
	}
}

// R-0HNB-YMGD R-ZX71-FO4X R-0MIX-HPF5
func TestDeclarations(t *testing.T) {
	var _ = tools.Config{Store: (*store.Store)(nil), Telemetry: (*telemetry.Writer)(nil), Events: (*events.Emitter)(nil)}
	var register = tools.Register
	_ = register
	entries := tools.Catalog()
	first := tools.Entry{Name: entries[0].Name, Description: entries[0].Description}
	_ = first
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.Description == "" {
			t.Fatal("empty description", e.Name)
		}
		names = append(names, e.Name)
	}
	if !reflect.DeepEqual(names, []string{"create", "list", "show", "rotate", "delete", "delivery"}) {
		t.Fatal("catalog order", names)
	}
	if got := tools.Scheme("").Enum(); !reflect.DeepEqual(got, []string{"bearer", "github-hmac"}) {
		t.Fatal("enum", got)
	}
	if !reflect.DeepEqual(tools.Scheme("").Enum(), []string{store.Bearer, store.GitHubHMAC}) {
		t.Fatal("enum constants")
	}
	for in, want := range map[string]string{"one\ntwo\nthree": "one", "single": "single", "": "", "\nrest": "", "a b\n": "a b"} {
		if got := tools.FirstLine(in); got != want {
			t.Fatalf("FirstLine(%q) = %q", in, got)
		}
	}
	scheme := tools.Scheme(store.GitHubHMAC)
	_ = tools.CreateArgs{Slug: "x", Scheme: &scheme}
	_ = tools.SlugArgs{Slug: "x"}
	_ = tools.DeliveryArgs{ID: "x"}
	limit := 1
	_ = tools.ListArgs{Page: func() *string { v := ""; return &v }(), Limit: &limit}
}

// R-ZZMU-77MB R-0LB1-3XOG
func TestResultShapesAndCopy(t *testing.T) {
	s := "x"
	_ = tools.Webhook{ID: s, Slug: s, Scheme: s, URL: s, Owner: s, Created: s, LastReceived: &s}
	_ = tools.Minted{ID: s, Slug: s, Scheme: s, URL: s, Owner: s, Created: s, LastReceived: &s, Secret: s}
	_ = tools.WebhookList{Webhooks: []tools.ListedWebhook{{ID: s, Slug: s, Scheme: s, URL: s, Owner: s, LastReceived: &s}}}
	_ = tools.Deleted{Deleted: true, ID: s}
	_ = tools.Delivery{ID: s, Hook: s, Received: s, ContentType: s, Headers: []tools.Header{{Name: s, Value: s}}, Body: &s, BodyBase64: &s}
	members := []struct {
		v    any
		want []string
	}{
		{tools.Minted{LastReceived: &s}, []string{"id", "slug", "scheme", "url", "owner", "created", "last_received", "secret"}},
		{tools.ListedWebhook{LastReceived: &s}, []string{"id", "slug", "scheme", "url", "owner", "last_received"}},
		{tools.WebhookList{Next: &s}, []string{"webhooks", "next"}},
		{tools.Deleted{}, []string{"deleted", "id"}},
		{tools.Header{}, []string{"name", "value"}},
		{tools.Delivery{Body: &s, BodyBase64: &s}, []string{"id", "hook", "received", "content_type", "headers", "body", "body_base64"}},
		{tools.Webhook{LastReceived: &s}, []string{"id", "slug", "scheme", "url", "owner", "created", "last_received"}},
		{tools.CreateArgs{Scheme: func() *tools.Scheme { v := tools.Scheme("bearer"); return &v }()}, []string{"slug", "scheme"}},
		{tools.SlugArgs{}, []string{"slug"}},
		{tools.DeliveryArgs{}, []string{"id"}},
	}
	for _, m := range members {
		v, want := m.v, m.want
		b, e := json.Marshal(v)
		if e != nil {
			t.Fatal(e)
		}
		assertKeys(t, b, want)
	}
	for _, fn := range []func(string) string{tools.NoWebhook, tools.InvalidSlug, tools.SlugTaken, tools.NoDelivery} {
		for _, arg := range []string{"gh_push", "whd_feed"} {
			if got := fn(arg); got == "" || !strings.Contains(got, arg) {
				t.Fatalf("copy %q lacks its argument", got)
			}
		}
	}
}

func assertKeys(t *testing.T, raw json.RawMessage, want []string) {
	t.Helper()
	d := json.NewDecoder(bytes.NewReader(raw))
	if token, e := d.Token(); e != nil || token != json.Delim('{') {
		t.Fatal("expected object", e)
	}
	var keys []string
	for d.More() {
		token, e := d.Token()
		if e != nil {
			t.Fatal(e)
		}
		keys = append(keys, token.(string))
		var value json.RawMessage
		if e = d.Decode(&value); e != nil {
			t.Fatal(e)
		}
	}
	if !reflect.DeepEqual(keys, want) {
		t.Fatal("member order", keys, want)
	}
}

// R-0NQT-VH5U
func TestInventory(t *testing.T) {
	f := setup(t)
	infos, e := f.client.ListTools(f.ctx, f.caller)
	if e != nil {
		t.Fatal(e)
	}
	catalog := tools.Catalog()
	effects := map[string]mcp.Effect{"create": mcp.Additive, "list": mcp.Read, "show": mcp.Read, "delivery": mcp.Read, "rotate": mcp.Destructive, "delete": mcp.Destructive}
	if len(infos) != 6 || len(catalog) != 6 {
		t.Fatal("six tools", len(infos))
	}
	for i, info := range infos {
		if info.Name != catalog[i].Name || info.Description != catalog[i].Description || info.Effect() != effects[info.Name] {
			t.Fatalf("tool %d: %+v", i, info)
		}
		a := info.Annotations
		want := effects[info.Name]
		if a.ReadOnlyHint == nil || *a.ReadOnlyHint != (want == mcp.Read) || a.DestructiveHint == nil || *a.DestructiveHint != (want == mcp.Destructive) {
			t.Fatalf("annotations %s", info.Name)
		}
	}
}

// R-0OYQ-98WJ
func TestCreate(t *testing.T) {
	f := setup(t)
	for _, c := range []struct{ slug, scheme, want string }{{"tick", "", store.Bearer}, {"gh_push", store.Bearer, store.Bearer}, {"gh_pull", store.GitHubHMAC, store.GitHubHMAC}} {
		f.serial = 0
		m := f.create(c.slug, c.scheme)
		requestID := fmt.Sprintf("%032x", 1)
		x, e := f.st.Get(f.ctx, c.slug)
		if e != nil {
			t.Fatal(e)
		}
		if m.ID != x.ID || !store.ValidID(m.ID) || m.Slug != c.slug || m.Scheme != c.want || x.Scheme != c.want || m.URL != urls.Hook(base, c.slug) || m.Owner != "mg@example.com" || m.Created != "2026-10-05T09:32:00Z" || m.LastReceived != nil {
			t.Fatalf("created %+v", m)
		}
		if x.OwnerID != "owner" || x.OwnerEmail != "mg@example.com" {
			t.Fatal("owner", x)
		}
		if !strings.HasPrefix(m.Secret, store.SecretPrefix) || len(m.Secret) != len(store.SecretPrefix)+52 {
			t.Fatal("secret form", m.Secret)
		}
		body := []byte("payload")
		hdr := bearerHeader(m.Secret)
		if c.want == store.GitHubHMAC {
			hdr = signedHeader(m.Secret, body)
		}
		if !ingress.Admits(x, hdr, body) {
			t.Fatal("minted secret is not the kept one")
		}
		lifecycle(t, f, trail.Created, m, requestID)
	}
}

// R-0Q6M-N0N8
func TestCreateRefusals(t *testing.T) {
	f := setup(t)
	f.create("taken", "")
	for _, slug := range []string{"Bad", "", "a__b", "9am", "gh-push", strings.Repeat("a", 65)} {
		f.unchanged("create", map[string]any{"slug": slug}, tools.InvalidSlug(slug))
	}
	f.unchanged("create", map[string]any{"slug": "taken"}, tools.SlugTaken("taken"))
	f.as("other", "ann@example.com")
	f.unchanged("create", map[string]any{"slug": "taken", "scheme": store.GitHubHMAC}, tools.SlugTaken("taken"))
}

// R-03AJ-CIUE R-0J58-BJHF
func TestListAndShow(t *testing.T) {
	f := setup(t)
	var empty tools.WebhookList
	success(t, f.call("list", map[string]any{}), &empty)
	if empty.Webhooks == nil || len(empty.Webhooks) != 0 {
		t.Fatal("empty list", empty)
	}
	secrets := []string{f.create("zeta", "").Secret}
	f.as("other", "ann@example.com")
	secrets = append(secrets, f.create("alpha", store.GitHubHMAC).Secret)
	f.mu.Lock()
	f.now = parse("2026-10-06T10:11:12Z")
	f.mu.Unlock()
	f.receive("alpha", store.Arrival{Body: []byte("x")})
	r := f.call("list", map[string]any{})
	var l tools.WebhookList
	success(t, r, &l)
	if len(l.Webhooks) != 2 || l.Webhooks[0].Slug != "alpha" || l.Webhooks[1].Slug != "zeta" {
		t.Fatal("list order", l)
	}
	for i, slug := range []string{"alpha", "zeta"} {
		var w tools.Webhook
		sr := f.call("show", map[string]any{"slug": slug})
		success(t, sr, &w)
		lw := l.Webhooks[i]
		if lw.ID != w.ID || lw.Slug != w.Slug || lw.Scheme != w.Scheme || lw.URL != w.URL || lw.Owner != w.Owner || !reflect.DeepEqual(lw.LastReceived, w.LastReceived) {
			t.Fatal("list differs from show", lw, w)
		}
		for _, s := range secrets {
			x, _ := f.st.Get(f.ctx, slug)
			for _, raw := range []json.RawMessage{r["structuredContent"], sr["structuredContent"], r["content"], sr["content"]} {
				if strings.Contains(string(raw), s) || (x.SecretSHA256 != "" && strings.Contains(string(raw), x.SecretSHA256)) {
					t.Fatal("secret shown")
				}
			}
		}
	}
	var w tools.Webhook
	success(t, f.call("show", map[string]any{"slug": "alpha"}), &w)
	if w.LastReceived == nil || *w.LastReceived != "2026-10-06T10:11:12Z" || w.Owner != "ann@example.com" || w.Scheme != store.GitHubHMAC || w.Created != "2026-10-05T09:32:00Z" {
		t.Fatal("show alpha", w)
	}
	f.as("owner", "mg@example.com")
	success(t, f.call("show", map[string]any{"slug": "alpha"}), &w)
	for _, slug := range []string{"missing", "Bad", ""} {
		f.unchanged("show", map[string]any{"slug": slug}, tools.NoWebhook(slug))
	}
}

// R-0SMF-EK4M
func TestRotate(t *testing.T) {
	f := setup(t)
	for _, scheme := range []string{store.Bearer, store.GitHubHMAC} {
		slug := "rot_" + strings.ReplaceAll(scheme, "-", "_")
		old := f.create(slug, scheme)
		f.serial = 0
		var m tools.Minted
		success(t, f.call("rotate", map[string]any{"slug": slug}), &m)
		if m.ID != old.ID || m.URL != old.URL || m.Scheme != old.Scheme || m.Slug != slug || m.Secret == old.Secret || !strings.HasPrefix(m.Secret, store.SecretPrefix) {
			t.Fatal("rotated", m, old)
		}
		x, e := f.st.Get(f.ctx, slug)
		if e != nil {
			t.Fatal(e)
		}
		body := []byte("payload")
		prove := func(secret string) http.Header {
			if scheme == store.GitHubHMAC {
				return signedHeader(secret, body)
			}
			return bearerHeader(secret)
		}
		if !ingress.Admits(x, prove(m.Secret), body) || ingress.Admits(x, prove(old.Secret), body) {
			t.Fatal("rotation did not replace the secret")
		}
		lifecycle(t, f, trail.Rotated, m, fmt.Sprintf("%032x", 1))
	}
}

// R-0TUB-SBVB
func TestDelete(t *testing.T) {
	f := setup(t)
	m := f.create("gone", "")
	keep := f.create("kept", "")
	d := f.receive("gone", store.Arrival{Body: []byte("a")})
	k := f.receive("kept", store.Arrival{Body: []byte("b")})
	f.serial = 0
	r := f.call("delete", map[string]any{"slug": "gone"})
	var del tools.Deleted
	success(t, r, &del)
	assertKeys(t, r["structuredContent"], []string{"deleted", "id"})
	if !del.Deleted || del.ID != m.ID {
		t.Fatal("deleted", del)
	}
	lifecycle(t, f, trail.Deleted, m, fmt.Sprintf("%032x", 1))
	f.unchanged("show", map[string]any{"slug": "gone"}, tools.NoWebhook("gone"))
	f.unchanged("delivery", map[string]any{"id": d.ID}, tools.NoDelivery(d.ID))
	var kd tools.Delivery
	success(t, f.call("delivery", map[string]any{"id": k.ID}), &kd)
	if kd.Hook != keep.Slug {
		t.Fatal("other delivery affected")
	}
}

// R-0V28-63M0
func TestOwnerOnly(t *testing.T) {
	f := setup(t)
	f.create("mine", "")
	f.as("other", "ann@example.com")
	for _, tool := range []string{"rotate", "delete"} {
		for _, slug := range []string{"mine", "missing", "Bad_", ""} {
			f.unchanged(tool, map[string]any{"slug": slug}, tools.NoWebhook(slug))
		}
	}
}

// R-0WA4-JVCP
func TestDelivery(t *testing.T) {
	f := setup(t)
	f.create("hook", store.GitHubHMAC)
	f.mu.Lock()
	f.now = parse("2026-10-07T01:02:03Z")
	f.mu.Unlock()
	cases := []struct {
		a       store.Arrival
		headers []tools.Header
	}{
		{store.Arrival{ContentType: "application/json", GitHubEvent: "push", GitHubDelivery: "gd-one", Body: []byte(`{"k":"v"}`)}, []tools.Header{{Name: "Content-Type", Value: "application/json"}, {Name: "X-GitHub-Event", Value: "push"}, {Name: "X-GitHub-Delivery", Value: "gd-one"}}},
		{store.Arrival{GitHubEvent: "ping", Body: []byte("plain text é")}, []tools.Header{{Name: "X-GitHub-Event", Value: "ping"}}},
		{store.Arrival{ContentType: "application/octet-stream", Body: []byte{0xff, 0xfe, 0x00, 0x01}}, []tools.Header{{Name: "Content-Type", Value: "application/octet-stream"}}},
		{store.Arrival{}, []tools.Header{}},
	}
	for _, c := range cases {
		d := f.receive("hook", c.a)
		r := f.call("delivery", map[string]any{"id": d.ID})
		var got tools.Delivery
		success(t, r, &got)
		if got.ID != d.ID || got.Hook != "hook" || got.Received != "2026-10-07T01:02:03Z" || got.ContentType != c.a.ContentType || !reflect.DeepEqual(got.Headers, c.headers) {
			t.Fatalf("delivery %+v", got)
		}
		body := c.a.Body
		if body == nil {
			body = []byte{}
		}
		if bytes.Equal(body, []byte{0xff, 0xfe, 0x00, 0x01}) {
			if got.Body != nil || got.BodyBase64 == nil || *got.BodyBase64 != base64.StdEncoding.EncodeToString(body) {
				t.Fatal("binary body", got)
			}
		} else if got.BodyBase64 != nil || got.Body == nil || *got.Body != string(body) {
			t.Fatal("text body", got)
		}
	}
}

// R-0XI0-XN3E
func TestDeliveryRefusals(t *testing.T) {
	f := setup(t)
	f.create("hook", "")
	d := f.receive("hook", store.Arrival{Body: []byte("private")})
	for _, id := range []string{"", "bogus", "whd_short", strings.ToUpper(d.ID), "whd_0000000000000000"} {
		f.unchanged("delivery", map[string]any{"id": id}, tools.NoDelivery(id))
	}
	f.as("other", "ann@example.com")
	r := f.call("delivery", map[string]any{"id": d.ID})
	refusal(t, r, tools.NoDelivery(d.ID))
	if strings.Contains(fmt.Sprint(r), "private") {
		t.Fatal("body leaked")
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, errors.New("no entropy") }

// R-0YPX-BEU3
func TestUnreachable(t *testing.T) {
	f := setup(t)
	m := f.create("hook", "")
	d := f.receive("hook", store.Arrival{Body: []byte("x")})
	f.flush()
	n := len(f.ec.Events())
	f.d.SetFailing(true)
	for _, c := range []struct {
		tool string
		args map[string]any
	}{
		{"create", map[string]any{"slug": "fresh"}},
		{"list", map[string]any{}},
		{"show", map[string]any{"slug": "hook"}},
		{"rotate", map[string]any{"slug": "hook"}},
		{"delete", map[string]any{"slug": "hook"}},
		{"delivery", map[string]any{"id": d.ID}},
	} {
		refusal(t, f.call(c.tool, c.args), store.Unreachable)
	}
	f.d.SetFailing(false)
	f.flush()
	if len(f.ec.Events()) != n {
		t.Fatal("emitted while unreachable")
	}
	xs := f.content()
	if len(xs) != 1 || xs[0].ID != m.ID {
		t.Fatal("changed while unreachable", xs)
	}
	x, _ := f.st.Get(f.ctx, "hook")
	if !ingress.Admits(x, bearerHeader(m.Secret), nil) {
		t.Fatal("secret changed while unreachable")
	}
	// A random source that fails is the store's unreachable answer too.
	g := setup(t)
	g.st = store.New(g.d, store.Config{Rand: failingReader{}})
	srv := mcp.NewServer(mcp.ServerConfig{Name: "webhooks", Version: "test", Telemetry: g.w})
	tools.Register(srv, tools.Config{Store: g.st, Telemetry: g.w, Events: g.em})
	server := httptest.NewServer(identity.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		srv.ServeHTTP(w, r.WithContext(urls.NewContext(r.Context(), base)))
	})))
	t.Cleanup(server.Close)
	g.client = mcp.NewClient(mcp.ClientConfig{Endpoint: server.URL + "/mcp"})
	g.unchanged("create", map[string]any{"slug": "fresh"}, store.Unreachable)
}

// R-05QC-42BS R-0864-VLT6
func TestPagingSchemas(t *testing.T) {
	f := setup(t)
	infos, err := f.client.ListTools(f.ctx, f.caller)
	if err != nil {
		t.Fatal(err)
	}
	for _, info := range infos {
		if info.Name != "list" {
			continue
		}
		want := map[string]any{"type": "object", "properties": map[string]any{
			"page":  map[string]any{"type": "string", "description": paging.PageDescription},
			"limit": map[string]any{"type": "integer", "description": paging.LimitDescription},
		}, "additionalProperties": false}
		var got any
		if err = json.Unmarshal(info.InputSchema, &got); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("input schema: %s", info.InputSchema)
		}
		var out struct {
			Properties json.RawMessage
			Required   []string
		}
		if err = json.Unmarshal(info.OutputSchema, &out); err != nil {
			t.Fatal(err)
		}
		assertKeys(t, out.Properties, []string{"webhooks", "next"})
		var props map[string]any
		if err = json.Unmarshal(out.Properties, &props); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(props["next"], map[string]any{"type": "string", "description": paging.NextDescription}) {
			t.Fatal("next schema", props)
		}
		for _, field := range out.Required {
			if field == "next" {
				t.Fatal("next required")
			}
		}
		return
	}
	t.Fatal("list absent")
}

// R-0D1Q-EORY R-0GPF-K001 R-41DF-7ZBJ
func TestPagingRefusalsAndTrail(t *testing.T) {
	f := setup(t)
	srv := mcp.NewServer(mcp.ServerConfig{Name: "webhooks", Version: "test", Telemetry: f.w})
	tools.Register(srv, tools.Config{Store: f.st, Telemetry: f.w, Events: f.em})
	server := httptest.NewServer(events.Middleware(identity.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		srv.ServeHTTP(w, r.WithContext(urls.NewContext(r.Context(), base)))
	}))))
	t.Cleanup(server.Close)
	f.client = mcp.NewClient(mcp.ClientConfig{Endpoint: server.URL + "/mcp"})
	f.create("kept", "")
	token := paging.Token{Tool: "list"}
	invalid := []string{"mistyped", (paging.Token{Tool: "list_widgets"}).Encode("a"), token.Encode(int64(1))}
	check := func(args any, want string) {
		t.Helper()
		f.flush()
		before, bus := len(f.tc.Events()), len(f.ec.Events())
		result := f.call("list", args)
		if want != "" {
			refusal(t, result, want)
		} else {
			var out tools.WebhookList
			success(t, result, &out)
		}
		f.flush()
		if len(f.ec.Events()) != bus {
			t.Fatal("list emitted bus event")
		}
		recorded := f.tc.Events()[before:]
		if len(recorded) != 1 || recorded[0].Name != "tool.called" {
			t.Fatalf("list events: %+v", recorded)
		}
	}
	for _, failing := range []bool{false, true} {
		f.d.SetFailing(failing)
		for _, limit := range []int{-1, 0, paging.MaxLimit + 1} {
			_, err := paging.Window(&limit)
			for _, page := range append([]string{""}, invalid...) {
				check(map[string]any{"page": page, "limit": limit}, err.Error())
			}
		}
		for _, page := range invalid {
			for _, limit := range []any{nil, 1, paging.MaxLimit} {
				check(map[string]any{"page": page, "limit": limit}, paging.BadPage)
			}
			check(map[string]any{"page": page}, paging.BadPage)
		}
		want := ""
		if failing {
			want = store.Unreachable
		}
		check(map[string]any{}, want)
		check(map[string]any{"page": nil, "limit": nil}, want)
		check(map[string]any{"page": "", "limit": 1}, want)
	}
}

// R-0BTU-0X19 R-0J58-BJHF R-0LL1-32YT
func TestPagingWindows(t *testing.T) {
	f := setup(t)
	slugs := []string{"a1", "a_b", "aa"}
	for i := 0; i < paging.DefaultLimit+2; i++ {
		slugs = append(slugs, fmt.Sprintf("z%03d", i))
	}
	for i := len(slugs) - 1; i >= 0; i-- {
		f.as(fmt.Sprintf("user%d", i%2), fmt.Sprintf("user%d@example.test", i%2))
		f.create(slugs[i], "")
	}
	token := paging.Token{Tool: "list"}
	fetch := func(args any, want []string, next bool) tools.WebhookList {
		t.Helper()
		raw := f.call("list", args)
		var out tools.WebhookList
		success(t, raw, &out)
		got := make([]string, 0, len(out.Webhooks))
		for _, w := range out.Webhooks {
			got = append(got, w.Slug)
			var show tools.Webhook
			success(t, f.call("show", tools.SlugArgs{Slug: w.Slug}), &show)
			if !reflect.DeepEqual(w, tools.ListedWebhook{ID: show.ID, Slug: show.Slug, Scheme: show.Scheme, URL: show.URL, Owner: show.Owner, LastReceived: show.LastReceived}) {
				t.Fatal("listed fields", w, show)
			}
		}
		if out.Webhooks == nil || !reflect.DeepEqual(got, want) {
			t.Fatal("window", got, want)
		}
		var members map[string]json.RawMessage
		if err := json.Unmarshal(raw["structuredContent"], &members); err != nil {
			t.Fatal(err)
		}
		_, present := members["next"]
		if present != next || (out.Next != nil) != next {
			t.Fatal("next presence", out)
		}
		if next && *out.Next != token.Encode(want[len(want)-1]) {
			t.Fatal("next token", out.Next)
		}
		return out
	}
	for _, args := range []any{tools.ListArgs{}, map[string]any{"page": nil, "limit": nil}, map[string]any{"page": ""}} {
		fetch(args, slugs[:paging.DefaultLimit], true)
	}
	fetch(map[string]any{"limit": paging.MaxLimit}, slugs, false)
	fetch(map[string]any{"page": token.Encode("B"), "limit": 2}, slugs[:2], true)
	fetch(map[string]any{"page": token.Encode(string([]byte{0xff})), "limit": 2}, []string{}, false)
	fetch(map[string]any{"page": token.Encode("a2"), "limit": 2}, slugs[1:3], true)
	for _, limit := range []int{1, 2, 3, len(slugs)} {
		var page *string
		for offset := 0; offset < len(slugs); {
			end := min(offset+limit, len(slugs))
			out := fetch(tools.ListArgs{Page: page, Limit: &limit}, slugs[offset:end], end < len(slugs))
			page = out.Next
			offset = end
		}
	}
}

// R-ZX71-FO4X
func TestArgumentSchemaProperties(t *testing.T) {
	f := setup(t)
	infos, err := f.client.ListTools(f.ctx, f.caller)
	if err != nil {
		t.Fatal(err)
	}
	expected := map[string]struct {
		properties []string
		required   []string
	}{
		"create":   {properties: []string{"slug", "scheme"}, required: []string{"slug"}},
		"list":     {properties: []string{"page", "limit"}},
		"show":     {properties: []string{"slug"}, required: []string{"slug"}},
		"rotate":   {properties: []string{"slug"}, required: []string{"slug"}},
		"delete":   {properties: []string{"slug"}, required: []string{"slug"}},
		"delivery": {properties: []string{"id"}, required: []string{"id"}},
	}
	for _, info := range infos {
		want, ok := expected[info.Name]
		if !ok {
			continue
		}
		var schema struct {
			Properties map[string]json.RawMessage
			Required   []string
		}
		if err = json.Unmarshal(info.InputSchema, &schema); err != nil {
			t.Fatal(err)
		}
		if len(schema.Properties) != len(want.properties) {
			t.Fatalf("%s properties: %v", info.Name, schema.Properties)
		}
		for _, property := range want.properties {
			if _, ok := schema.Properties[property]; !ok {
				t.Fatalf("%s missing property %s", info.Name, property)
			}
		}
		if len(schema.Required) != len(want.required) {
			t.Fatalf("%s required: %v", info.Name, schema.Required)
		}
		for _, required := range want.required {
			found := false
			for _, got := range schema.Required {
				if got == required {
					found = true
				}
			}
			if !found {
				t.Fatalf("%s missing required property %s", info.Name, required)
			}
		}
		delete(expected, info.Name)
	}
	if len(expected) != 0 {
		t.Fatal("missing tool schemas", expected)
	}
}
