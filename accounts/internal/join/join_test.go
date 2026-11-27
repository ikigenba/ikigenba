package join_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts"
	"github.com/ikigenba/ikigenba/accounts/internal/join"
	"github.com/ikigenba/ikigenba/accounts/internal/pages"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/accounts/internal/trail"
	"github.com/ikigenba/ikigenba/accounts/internal/valid"
	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
)

const formType = "application/x-www-form-urlencoded"
const requestID = "7d4b1f8a2c6e3095b8a1d4f7c0e3b6a9"

var inviter = identity.Caller{UserID: "space-inviter"}

func signature[T any](value T) T { return value }

type fixture struct {
	store          *store.Store
	db             *db.DB
	pages          *pages.Set
	cfg            join.Config
	h              http.Handler
	clock          time.Time
	storeClockHook func()
	host           store.Host
	acct           store.Account
	events         *telemetry.Capture
	writer         *telemetry.Writer
}

func setup(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{clock: time.Date(2026, 11, 21, 8, 9, 10, 0, time.UTC), events: &telemetry.Capture{}}
	now := func() time.Time { return f.clock }
	var err error
	f.db, err = db.Open(t.Context(), db.Config{Path: filepath.Join(t.TempDir(), "accounts.db"), Migrations: accounts.Migrations(), Now: now})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.db.Close() })
	f.pages, err = pages.Load()
	if err != nil {
		t.Fatal(err)
	}
	var raw []byte
	for i := range uint64(256) {
		raw = binary.BigEndian.AppendUint64(raw, i)
	}
	storeClock := func() time.Time {
		if f.storeClockHook != nil {
			f.storeClockHook()
		}
		return now()
	}
	f.store = store.New(f.db, store.Config{Now: storeClock, Rand: bytes.NewReader(raw)})
	f.writer = telemetry.New(telemetry.Config{Service: pages.ServiceName, Sink: f.events, Now: now, Stderr: io.Discard})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		f.writer.Shutdown(ctx, "test")
	})
	// Positional use checks the documented config shape and field order.
	f.cfg = join.Config{f.pages, f.store, true, "work.test", join.NewLimiter(now), f.writer}
	handler := signature[func(join.Config) http.Handler](join.Handler)
	f.h = handler(f.cfg)
	f.host, _, err = f.store.CreateHost(t.Context(), store.HostSpec{Type: valid.DefaultType, RootGB: valid.DefaultRootGB, DataGB: valid.DefaultDataGB}, inviter)
	if err != nil {
		t.Fatal(err)
	}
	f.acct = f.place(t, "acme")
	return f
}

func (f *fixture) place(t *testing.T, slug string) store.Account {
	t.Helper()
	a, op, err := f.store.PlaceAccount(t.Context(), slug, f.host.Name, inviter)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.store.Finish(t.Context(), op.ID, store.Update{}); err != nil {
		t.Fatal(err)
	}
	return a
}

func (f *fixture) mint(t *testing.T, account string, admin bool) (store.Invite, string) {
	t.Helper()
	i, token, err := f.store.CreateInvite(t.Context(), account, admin, inviter)
	if err != nil {
		t.Fatal(err)
	}
	return i, joinTarget(token)
}

func joinTarget(token string) string {
	return join.Path + "?" + url.Values{join.TokenParam: {token}}.Encode()
}

func (f *fixture) data(outcome, account string, admin bool, user, key string) pages.JoinData {
	return pages.JoinData{Outcome: outcome, Account: account, DNSName: account + "." + f.cfg.Domain, Admin: admin, User: user, Key: key}
}

func (f *fixture) captured(t *testing.T) []telemetry.Event {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := f.writer.Flush(ctx); err != nil {
		t.Fatal(err)
	}
	return f.events.Events()
}

func request(method, path string, body io.Reader) *http.Request {
	r := httptest.NewRequest(method, path, body)
	r.Header.Set("Content-Type", formType)
	r.Header["X-Request-Id"] = []string{requestID, "ignored"}
	return r
}

func submission(path, user, key string) *http.Request {
	return request("POST", path, strings.NewReader(url.Values{"user": {user}, "key": {key}}.Encode()))
}

func serve(h http.Handler, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func securityHeaders(h http.Header) {
	h.Set("Cache-Control", "no-store")
	h.Set("Referrer-Policy", "no-referrer")
	h.Set("X-Frame-Options", "DENY")
	h.Set("X-Robots-Tag", "noindex, nofollow")
}

func expected(set *pages.Set, r *http.Request, status int, data pages.JoinData) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	securityHeaders(w.Header())
	set.Write(w, r, status, "join", data)
	return w
}

func same(t *testing.T, got, want *httptest.ResponseRecorder) {
	t.Helper()
	if got.Code != want.Code || !reflect.DeepEqual(got.Header(), want.Header()) || got.Body.String() != want.Body.String() {
		t.Fatalf("answer = %d %v (%d body bytes); want %d %v (%d body bytes), bodies equal: %t",
			got.Code, got.Header(), got.Body.Len(), want.Code, want.Header(), want.Body.Len(), got.Body.String() == want.Body.String())
	}
}

func plain(r *http.Request, status int) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	securityHeaders(w.Header())
	if status == http.StatusServiceUnavailable {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	}
	w.WriteHeader(status)
	if status == http.StatusServiceUnavailable && r.Method != http.MethodHead {
		_, _ = io.WriteString(w, store.Unreachable+"\n")
	}
	return w
}

type countedBody struct{ n int }

func (b *countedBody) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'x'
	}
	b.n += len(p)
	return len(p), nil
}

// R-1FPL-X03I R-1GXI-ARU7 R-I6G5-R6SK R-1PGS-Z612 R-1QOP-CXRR R-IZPQ-XOL8 R-20FW-F3PB
func TestJoinDispatchAndRateLimit(t *testing.T) {
	f := setup(t)
	_, link := f.mint(t, f.acct.Slug, true)
	before := f.records(t)
	body := new(countedBody)
	const addr = "192.0.2.7"
	f.db.SetFailing(true)
	for _, method := range []string{"PUT", "DELETE", "PATCH"} {
		r := request(method, link, body)
		r.Header.Set("X-Real-IP", addr)
		want := plain(r, 405)
		want.Header().Set("Allow", "GET, HEAD, POST")
		same(t, serve(f.h, r), want)
	}
	drain(t, f.cfg.Limiter, addr)
	f.clock = f.clock.Add(250 * time.Millisecond)
	r := request("POST", link, body)
	r.Header["X-Real-Ip"] = []string{"::ffff:" + addr, "192.0.2.8"}
	want := expected(f.pages, r, 429, pages.JoinData{Outcome: pages.JoinLimited})
	want.Header().Set("Retry-After", "6")
	same(t, serve(f.h, r), want)
	if body.n != 0 {
		t.Fatalf("method or rate-limited request read %d body bytes", body.n)
	}
	parsed := request("GET", link, nil).URL
	token := parsed.Query().Get(join.TokenParam)
	invalidLinks := []string{
		join.Path, joinTarget(""), join.Path + "?utm=x", joinTarget("inv_x"),
		link + "&" + parsed.RawQuery, link + "&%74oken=", joinTarget(token + "x"),
		join.Path + "/?" + parsed.RawQuery, join.Path + "/" + token, join.Path + "?token=%zz",
	}
	for _, path := range invalidLinks {
		for _, method := range []string{"GET", "HEAD", "POST"} {
			r := request(method, path, body)
			r.Header.Set("X-Real-IP", "192.0.2.9")
			same(t, serve(f.h, r), expected(f.pages, r, 404, pages.JoinData{Outcome: pages.JoinUnusable}))
		}
	}
	cfg := f.cfg
	cfg.Configured = false
	for _, method := range []string{"GET", "HEAD", "POST"} {
		r := request(method, link, body)
		r.Header.Set("X-Real-IP", "192.0.2.8")
		same(t, serve(join.Handler(cfg), r), expected(f.pages, r, 404, pages.JoinData{Outcome: pages.JoinUnusable}))
	}
	if body.n != 0 {
		t.Fatalf("unusable or unconfigured link read %d body bytes", body.n)
	}
	for range join.Burst - 1 {
		allow(t, f.cfg.Limiter, "192.0.2.8", true, 0)
	}
	allow(t, f.cfg.Limiter, "192.0.2.8", false, join.Every)
	for range join.Burst {
		r := request("POST", join.Path, body)
		same(t, serve(f.h, r), expected(f.pages, r, 404, pages.JoinData{Outcome: pages.JoinUnusable}))
	}
	for _, invalid := range []string{"bogus", "192.0.2.7, 10.0.0.1"} {
		r := request("POST", link, body)
		r.Header.Set("X-Real-IP", invalid)
		want := expected(f.pages, r, 429, pages.JoinData{Outcome: pages.JoinLimited})
		want.Header().Set("Retry-After", "6")
		same(t, serve(f.h, r), want)
	}
	f.db.SetFailing(false)
	for _, path := range []string{link + "&utm=x", join.Path + "?utm=x&" + parsed.RawQuery, "/%6aoin?" + parsed.RawQuery} {
		r = request("GET", path, body)
		r.Header.Set("X-Real-IP", addr)
		same(t, serve(f.h, r), expected(f.pages, r, 200, f.data(pages.JoinForm, f.acct.Slug, true, "", "")))
	}
	if after := f.records(t); !reflect.DeepEqual(after, before) {
		t.Fatal("dispatch or rate refusal changed the record")
	}
	if len(f.captured(t)) != 0 {
		t.Fatal("dispatch or rate refusal emitted events")
	}
}

// R-1UCE-I8ZU R-1VKA-W0QJ R-T3H0-ZN3Z R-20FW-F3PB R-243L-KEXE R-25BH-Y6O3
func TestJoinLookupBodyLimitAndFailure(t *testing.T) {
	f := setup(t)
	_, link := f.mint(t, f.acct.Slug, true)
	before := f.records(t)
	get := request("GET", link, nil)
	head := request("HEAD", link, nil)
	data := f.data(pages.JoinForm, f.acct.Slug, true, "", "")
	same(t, serve(f.h, get), expected(f.pages, get, 200, data))
	get.Header.Set("X-User-Id", "signed-in")
	get.Header.Set("X-User-Email", "chosen@example.test")
	same(t, serve(f.h, get), expected(f.pages, get, 200, data))
	gotHead := serve(f.h, head)
	same(t, gotHead, expected(f.pages, head, 200, data))
	drain(t, f.cfg.Limiter, "")
	f.clock = f.clock.Add(time.Minute)
	body := strings.NewReader(strings.Repeat("x", join.MaxBodyBytes))
	r := request("POST", link, body)
	same(t, serve(f.h, r), expected(f.pages, r, 422, f.data(pages.JoinName, f.acct.Slug, true, "", "")))
	f.db.SetFailing(true)
	body = strings.NewReader(strings.Repeat("x", join.MaxBodyBytes+1))
	r = request("POST", link, body)
	same(t, serve(f.h, r), plain(r, 413))
	endless := new(countedBody)
	r = request("POST", link, endless)
	same(t, serve(f.h, r), plain(r, 413))
	if endless.n != join.MaxBodyBytes+1 {
		t.Fatalf("oversized body used %d bytes, want %d", endless.n, join.MaxBodyBytes+1)
	}
	for _, r := range []*http.Request{get, head, submission(link, "bugs", keyLine())} {
		same(t, serve(f.h, r), plain(r, 503))
	}
	f.db.SetFailing(false)
	same(t, serve(f.h, get), expected(f.pages, get, 200, data))
	if after := f.records(t); !reflect.DeepEqual(after, before) {
		t.Fatal("a form or failed request changed the record")
	}
	if len(f.captured(t)) != 0 {
		t.Fatal("a form or failed request emitted events")
	}
}

func keyLine() string {
	wire := binary.BigEndian.AppendUint32(nil, 11)
	wire = append(wire, "ssh-ed25519"...)
	wire = binary.BigEndian.AppendUint32(wire, 32)
	wire = append(wire, bytes.Repeat([]byte{0x4b}, 32)...)
	return "ssh-ed25519 " + base64.StdEncoding.EncodeToString(wire) + " chosen@laptop"
}

func storedKey() store.Key {
	typ, rest, _ := strings.Cut(keyLine(), " ")
	body, _, _ := strings.Cut(rest, " ")
	return store.Key{Type: typ, Body: body, Line: keyLine()}
}

type records struct {
	hosts    []store.Host
	accounts []store.Account
	users    []store.User
	invites  map[string][]store.Invite
}

func (f *fixture) records(t *testing.T) records {
	t.Helper()
	ctx := t.Context()
	const limit = 25
	hosts, err := f.store.ListHosts(ctx, 0, limit)
	if err != nil {
		t.Fatal(err)
	}
	accts, err := f.store.ListAccounts(ctx, "", limit)
	if err != nil {
		t.Fatal(err)
	}
	users := []store.User{}
	invites := make(map[string][]store.Invite)
	for _, a := range accts {
		members, err := f.store.UsersOf(ctx, a.Slug)
		if err != nil {
			t.Fatal(err)
		}
		users = append(users, members...)
		invites[a.Slug], err = f.store.Invites(ctx, a.Slug)
		if err != nil {
			t.Fatal(err)
		}
	}
	return records{hosts, accts, users, invites}
}

// R-T118-83ML R-26JE-BYES R-EXM4-WJR5 R-I6G5-R6SK
func TestJoinFormRefusals(t *testing.T) {
	f := setup(t)
	other := f.place(t, "other")
	if _, err := f.store.AddUser(t.Context(), other.Slug, "daffy", storedKey(), inviter); err != nil {
		t.Fatal(err)
	}
	_, link := f.mint(t, f.acct.Slug, true)
	before := f.records(t)
	for _, tc := range []struct {
		user, key, outcome string
	}{
		{"Bugs", keyLine(), pages.JoinName},
		{"root", keyLine(), pages.JoinName},
		{"root", "ssh-rsa AAAA", pages.JoinName},
		{" bugs", keyLine(), pages.JoinName},
		{"daffy", keyLine(), pages.JoinName},
		{other.Slug, keyLine(), pages.JoinName},
		{"bugs", " ssh-rsa AAAA\r\n", pages.JoinKey},
	} {
		r := submission(link, tc.user, tc.key)
		same(t, serve(f.h, r), expected(f.pages, r, 422, f.data(tc.outcome, f.acct.Slug, true, tc.user, tc.key)))
	}
	for _, tc := range []struct{ contentType, body, user, key string }{
		{formType + "; charset=utf-8", "user=root&user=bugs&key=first&key=second", "root", "first"},
		{"text/plain", "user=bugs&key=some", "", ""},
		{formType, "user=bugs&key=%Q2", "", ""},
		{formType, "", "", ""},
	} {
		r := request("POST", link+"&user=bugs&key=query", strings.NewReader(tc.body))
		r.Header.Set("Content-Type", tc.contentType)
		r.Header.Set("X-Real-IP", "192.0.2.8")
		same(t, serve(f.h, r), expected(f.pages, r, 422, f.data(pages.JoinName, f.acct.Slug, true, tc.user, tc.key)))
	}
	if after := f.records(t); !reflect.DeepEqual(after, before) {
		t.Fatal("form refusal changed the record")
	}
	if len(f.captured(t)) != 0 {
		t.Fatal("form refusal emitted events")
	}
}

// R-T294-LVDA R-26JE-BYES
func TestJoinFullHost(t *testing.T) {
	f := setup(t)
	_, link := f.mint(t, f.acct.Slug, false)
	for i := range valid.MaxUsersPerHost {
		if _, err := f.store.AddUser(t.Context(), f.acct.Slug, fmt.Sprintf("existing-%d", i), storedKey(), inviter); err != nil {
			t.Fatal(err)
		}
	}
	before := f.records(t)
	r := submission(link, "bugs", keyLine())
	same(t, serve(f.h, r), expected(f.pages, r, 409, f.data(pages.JoinFull, f.acct.Slug, false, "bugs", keyLine())))
	if after := f.records(t); !reflect.DeepEqual(after, before) {
		t.Fatal("full host refusal changed the record")
	}
	r = request("GET", link, nil)
	same(t, serve(f.h, r), expected(f.pages, r, 200, f.data(pages.JoinForm, f.acct.Slug, false, "", "")))
	if len(f.captured(t)) != 0 {
		t.Fatal("full host refusal emitted events")
	}
}

// R-T294-LVDA R-EXM4-WJR5
func TestJoinNameClaimedDuringEnrollment(t *testing.T) {
	f := setup(t)
	ctx := t.Context()
	_, link := f.mint(t, f.acct.Slug, true)
	// Give the write transaction a competing claim that the earlier read
	// cannot see. The trigger runs inside the test-owned database only.
	if err := f.db.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `CREATE TRIGGER join_name_claim BEFORE INSERT ON names
			WHEN NEW.name = 'bugs' BEGIN INSERT INTO names (name) VALUES (NEW.name); END`)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if held, err := f.store.NameHeld(ctx, "bugs"); held || err != nil {
		t.Fatalf("name before enrollment = %t, %v; want free", held, err)
	}
	token := request("GET", link, nil).URL.Query().Get(join.TokenParam)
	if _, _, err := f.store.Enroll(ctx, token, "bugs", storedKey()); !errors.Is(err, store.ErrTaken) {
		t.Fatalf("competing claim returns %v, want %v", err, store.ErrTaken)
	}
	before := f.records(t)
	pasted := " " + keyLine() + "\r\n"
	r := submission(link, "bugs", pasted)
	same(t, serve(f.h, r), expected(f.pages, r, 422, f.data(pages.JoinName, f.acct.Slug, true, "bugs", pasted)))
	if after := f.records(t); !reflect.DeepEqual(after, before) {
		t.Fatal("competing claim changed the record")
	}
	if err := f.db.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "DROP TRIGGER join_name_claim")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	r = submission(link, "bugs", pasted)
	same(t, serve(f.h, r), expected(f.pages, r, 200, f.data(pages.JoinDone, f.acct.Slug, true, "bugs", "")))
}

// R-T294-LVDA R-25BH-Y6O3 R-26JE-BYES R-T74Q-4YC2 R-39O3-OM20
func TestJoinEnrollment(t *testing.T) {
	f := setup(t)
	ctx := t.Context()
	f.place(t, "other")
	f.mint(t, "other", false)
	i, link := f.mint(t, f.acct.Slug, true)
	before := f.records(t)
	pasted := "  " + strings.Replace(keyLine(), " ", "\t", 1) + " \r\n"
	pasted = strings.Replace(pasted, " chosen@laptop", "   chosen@laptop", 1)
	r := submission(link, "bugs", pasted)
	r.Header.Set("X-User-Id", "different-space-user")
	r.Header.Set("X-User-Email", "unrelated@example.test")
	same(t, serve(f.h, r), expected(f.pages, r, 200, f.data(pages.JoinDone, f.acct.Slug, true, "bugs", "")))
	u, err := f.store.User(ctx, f.acct.Slug, "bugs")
	if err != nil {
		t.Fatal(err)
	}
	if u.Admin != i.Admin || u.CreatedBy != i.CreatedBy || !u.Pending || !reflect.DeepEqual(u.Keys, []store.Key{storedKey()}) {
		t.Fatalf("created user = %+v", u)
	}
	want := before
	want.hosts[0].Version++
	want.accounts[0].Users++
	want.accounts[0].Invites--
	want.invites[f.acct.Slug] = []store.Invite{}
	want.users = []store.User{u}
	if after := f.records(t); !reflect.DeepEqual(after, want) {
		t.Fatalf("record after enrollment = %+v; want %+v", after, want)
	}
	for _, later := range []*http.Request{request("GET", link, nil), request("HEAD", link, nil), submission(link, "bugs", pasted)} {
		same(t, serve(f.h, later), expected(f.pages, later, 404, pages.JoinData{Outcome: pages.JoinUnusable}))
	}
	if _, err := f.store.RecordReport(ctx, f.host.Name, protocol.Report{Protocol: protocol.Version, Attempted: u.Version, Applied: u.Version}, f.clock); err != nil {
		t.Fatal(err)
	}
	if applied, err := f.store.User(ctx, u.Account, u.Name); err != nil || applied.Pending {
		t.Fatalf("applied user = %+v, %v", applied, err)
	}
	events := f.captured(t)
	wantAttrs := telemetry.Attrs{"invite": i.ID, "account": f.acct.Slug, "host": f.host.Name, "user": "bugs", "admin": true}
	if len(events) != 1 || events[0].Name != trail.EventInviteUsed || events[0].User != "" || events[0].RequestID != requestID || !reflect.DeepEqual(events[0].Attrs, wantAttrs) {
		t.Fatalf("enrollment events = %+v", events)
	}
}

// R-T3H0-ZN3Z R-T74Q-4YC2
func TestJoinAccountReadFailureAfterEnrollment(t *testing.T) {
	f := setup(t)
	i, link := f.mint(t, f.acct.Slug, true)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	observer := store.New(f.db, store.Config{Now: func() time.Time { return f.clock }})
	f.storeClockHook = func() {
		if _, err := observer.User(t.Context(), f.acct.Slug, "bugs"); err == nil {
			// The write is already visible. Cancel the request during the
			// account lookup, whose count of live invites uses the clock.
			cancel()
		}
	}
	r := submission(link, "bugs", keyLine()).WithContext(ctx)
	same(t, serve(f.h, r), expected(f.pages, r, 200, f.data(pages.JoinDone, f.acct.Slug, true, "bugs", "")))
	if ctx.Err() == nil {
		t.Fatal("the request was never canceled during the post-enrollment lookup")
	}
	events := f.captured(t)
	wantAttrs := telemetry.Attrs{"invite": i.ID, "account": f.acct.Slug, "host": "", "user": "bugs", "admin": true}
	if len(events) != 1 || events[0].Name != trail.EventInviteUsed || !reflect.DeepEqual(events[0].Attrs, wantAttrs) {
		t.Fatalf("enrollment events after account lookup failed = %+v", events)
	}
}

// R-JSZC-46DW
func TestUnusableLinksAreIdentical(t *testing.T) {
	f := setup(t)
	ctx := t.Context()
	_, expired := f.mint(t, f.acct.Slug, false)
	f.clock = f.clock.Add(store.InviteLife)
	revoked, revokedLink := f.mint(t, f.acct.Slug, false)
	if _, err := f.store.RevokeInvite(ctx, revoked.ID); err != nil {
		t.Fatal(err)
	}
	_, used := f.mint(t, f.acct.Slug, true)
	r := submission(used, "bugs", keyLine())
	same(t, serve(f.h, r), expected(f.pages, r, 200, f.data(pages.JoinDone, f.acct.Slug, true, "bugs", "")))
	_, validLink := f.mint(t, f.acct.Slug, true)
	f.place(t, "departing")
	_, removing := f.mint(t, "departing", false)
	_, op, _, err := f.store.RemoveAccount(ctx, "departing", inviter)
	if err != nil {
		t.Fatal(err)
	}
	cfg := f.cfg
	cfg.Configured = false
	validToken := request("GET", validLink, nil).URL.Query().Get(join.TokenParam)
	unknown := store.TokenPrefix + strings.Repeat("0", len(validToken)-len(store.TokenPrefix))
	if !store.ValidToken(unknown) {
		t.Fatal("unknown token fixture is malformed")
	}
	want := expected(f.pages, request("GET", used, nil), 404, pages.JoinData{Outcome: pages.JoinUnusable})
	for _, link := range []string{used, expired, revokedLink, removing, joinTarget(unknown), validLink + "/", join.Path} {
		same(t, serve(f.h, request("GET", link, nil)), want)
		same(t, serve(f.h, submission(link, "root", "submitted key")), want)
	}
	if err := f.store.Finish(ctx, op.ID, store.Update{}); err != nil {
		t.Fatal(err)
	}
	same(t, serve(f.h, request("GET", removing, nil)), want)
	same(t, serve(f.h, submission(removing, "bugs", keyLine())), want)
	same(t, serve(join.Handler(cfg), request("GET", validLink, nil)), want)
	same(t, serve(join.Handler(cfg), submission(validLink, "bugs", keyLine())), want)
}

// R-28Z7-3HW6 R-39O3-OM20
func TestConcurrentEnrollment(t *testing.T) {
	f := setup(t)
	i, link := f.mint(t, f.acct.Slug, false)
	names := []string{"bugs", "daffy"}
	answers := make([]*httptest.ResponseRecorder, len(names))
	var group sync.WaitGroup
	start := make(chan struct{})
	for idx, name := range names {
		group.Go(func() {
			<-start
			r := submission(link, name, keyLine())
			r.Header.Del("X-Request-Id")
			answers[idx] = serve(f.h, r)
		})
	}
	close(start)
	group.Wait()
	winners := 0
	winnerName := ""
	for idx, got := range answers {
		r := submission(link, names[idx], keyLine())
		if got.Code == 200 {
			winners++
			winnerName = names[idx]
			same(t, got, expected(f.pages, r, 200, f.data(pages.JoinDone, f.acct.Slug, false, names[idx], "")))
		} else {
			same(t, got, expected(f.pages, r, 404, pages.JoinData{Outcome: pages.JoinUnusable}))
		}
	}
	users, err := f.store.UsersOf(t.Context(), f.acct.Slug)
	if err != nil {
		t.Fatal(err)
	}
	events := f.captured(t)
	if winners != 1 || len(users) != 1 || users[0].Name != winnerName || len(events) != 1 || events[0].Name != trail.EventInviteUsed || events[0].RequestID != "" || events[0].Attrs["invite"] != i.ID || events[0].Attrs["user"] != winnerName {
		t.Fatalf("race winners = %d, users = %v, events = %v", winners, users, events)
	}
}
