package daemonapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/accounts/internal/box"
	"github.com/ikigenba/ikigenba/accounts/internal/cloud"
	"github.com/ikigenba/ikigenba/accounts/internal/hostauth"
	"github.com/ikigenba/ikigenba/accounts/internal/protocol"
	"github.com/ikigenba/ikigenba/accounts/internal/store"
	"github.com/ikigenba/ikigenba/accounts/internal/tools"
	"github.com/ikigenba/ikigenba/accounts/internal/valid"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/services"
)

const (
	listRequest = `{"jsonrpc":"2.0","id":1,"method":"` + protocol.MethodToolsList + `"}`
	callRequest = `{"jsonrpc":"2.0","id":"caller-id","method":"` + protocol.MethodToolsCall + `","params":{"name":"` + box.ToolKeys + `","arguments":{}}}`
)

var unexpectedHandler = http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
	panic("unexpected box handler")
})

func boxPath(uid int64) string {
	return protocol.BoxPath + "?" + protocol.QueryUID + "=" + strconv.FormatInt(uid, 10)
}

func addBoxAccount(t *testing.T, st *store.Store, host, account string) {
	t.Helper()
	_, op, err := st.PlaceAccount(t.Context(), account, host, identity.Caller{UserID: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Finish(t.Context(), op.ID, store.Update{}); err != nil {
		t.Fatal(err)
	}
}

func addBoxUser(t *testing.T, st *store.Store, account, name string) store.User {
	t.Helper()
	u, err := st.AddUser(t.Context(), account, name, boxKey(t, 0), identity.Caller{UserID: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func boxKey(t *testing.T, index int) store.Key {
	t.Helper()
	const kind = "ssh-ed25519"
	var wire [51]byte
	wire[3] = byte(len(kind))
	copy(wire[4:], kind)
	wire[18] = 32
	copy(wire[19:], strconv.Itoa(index))
	body := base64.StdEncoding.EncodeToString(wire[:])
	prefix := kind + " " + body + " "
	key, err := valid.ParseKey(prefix + strings.Repeat("<", valid.MaxKeyBytes-len(prefix)))
	if err != nil {
		t.Fatal(err)
	}
	return store.Key{Type: key.Type, Body: key.Body, Line: key.Line()}
}

// R-SABX-VBC7, R-YDH0-PCK8, R-YEOX-34AX, R-YKSE-ZZ0E, R-YFWT-GW1M.
func TestBoxRouteRequiresConfigurationBothHandlersAndExactPath(t *testing.T) {
	for _, cfg := range []Config{
		{BoxUser: unexpectedHandler, BoxAdmin: unexpectedHandler},
		{Configured: true, BoxUser: unexpectedHandler},
		{Configured: true, BoxAdmin: unexpectedHandler},
	} {
		s := server(t, cfg)
		for _, method := range []string{http.MethodPost, http.MethodGet, http.MethodHead} {
			w := invoke(s, httptest.NewRequestWithContext(t.Context(), method, boxPath(7), neverRead{}))
			expectError(t, w, http.StatusNotFound, NotFound)
		}
	}
	s := server(t, Config{Configured: true, BoxUser: unexpectedHandler, BoxAdmin: unexpectedHandler})
	for _, method := range []string{http.MethodPost, http.MethodGet, http.MethodHead} {
		w := invoke(s, httptest.NewRequestWithContext(t.Context(), method, protocol.BoxPath+"/", neverRead{}))
		expectError(t, w, http.StatusNotFound, NotFound)
	}
	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPut} {
		w := invoke(s, httptest.NewRequestWithContext(t.Context(), method, boxPath(7), neverRead{}))
		expectError(t, w, http.StatusMethodNotAllowed, NotAllowed)
		if got := w.Header().Get("Allow"); got != http.MethodPost {
			t.Fatalf("Allow = %q, want %q", got, http.MethodPost)
		}
	}
	// Only a configured POST at the exact path proceeds to UID validation.
	w := invoke(s, httptest.NewRequestWithContext(t.Context(), http.MethodPost, protocol.BoxPath, neverRead{}))
	expectError(t, w, http.StatusBadRequest, BadRequest)
}

// R-YM0B-DQR3.
func TestInvalidBoxUIDPrecedesAuthentication(t *testing.T) {
	s := server(t, Config{Configured: true, BoxUser: unexpectedHandler, BoxAdmin: unexpectedHandler})
	q := protocol.QueryUID
	for _, query := range []string{"other=7", q + "=", q + "=-1", q + "=%2b1", q + "=4294967296", q + "=x", q + "=3.0", q + "=7&" + q + "=7"} {
		r := httptest.NewRequestWithContext(t.Context(), http.MethodPost, protocol.BoxPath+"?"+query, neverRead{})
		expectError(t, invoke(s, r), http.StatusBadRequest, BadRequest)
	}
}

// R-YN87-RIHS, R-YFWT-GW1M.
func TestBoxAuthenticatesBeforeStoreAndBody(t *testing.T) {
	stamps := new(requests)
	path := boxPath(math.MaxUint32)
	for _, tt := range []struct {
		name   string
		sts    cloud.STS
		header string
		status int
		msg    string
	}{
		{"missing", fixtureSTS{}, "", http.StatusUnauthorized, Refused},
		{"malformed", fixtureSTS{}, "!", http.StatusUnauthorized, Refused},
		{"stale", fixtureSTS{}, stamps.request(t.Context(), http.MethodPost, path, neverRead{}, fixtureNow.Add(-2*hostauth.Freshness), 1200).Header.Get(hostauth.HeaderName), http.StatusUnauthorized, Refused},
		{"rejected", fixtureSTS{err: &cloud.Error{Class: cloud.ClassRefused}}, "signed", http.StatusUnauthorized, Refused},
		{"unavailable", fixtureSTS{err: &cloud.Error{Class: cloud.ClassTransient}}, "signed", http.StatusServiceUnavailable, Busy},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := server(t, Config{Configured: true, Verifier: stamps.verifier(tt.sts), BoxUser: unexpectedHandler, BoxAdmin: unexpectedHandler})
			r := stamps.signed(t.Context(), http.MethodPost, path, neverRead{})
			if tt.header != "signed" {
				r.Header.Set(hostauth.HeaderName, tt.header)
			}
			expectError(t, invoke(s, r), tt.status, tt.msg)
		})
	}
	fixed := new(requests)
	v := fixed.verifier(fixtureSTS{instance: fixtureInstance})
	s := server(t, Config{Configured: true, Verifier: v, BoxUser: unexpectedHandler, BoxAdmin: unexpectedHandler})
	for n := int64(1); n <= hostauth.VerifyBurst; n++ {
		r := fixed.request(t.Context(), http.MethodPost, path, neverRead{}, fixtureNow, n)
		if _, err := v.Verify(r.Context(), r.Header.Get(hostauth.HeaderName)); err != nil {
			t.Fatal(err)
		}
	}
	busy := fixed.request(t.Context(), http.MethodPost, path, neverRead{}, fixtureNow, hostauth.VerifyBurst+1)
	expectError(t, invoke(s, busy), http.StatusServiceUnavailable, Busy)
	// Advancing the clock refills the token bucket, isolating replay rejection.
	fixed.elapsed.Add(1)
	replay := fixed.request(t.Context(), http.MethodPost, path, neverRead{}, fixtureNow, 1)
	expectError(t, invoke(s, replay), http.StatusUnauthorized, Refused)
}

// R-WC9T-P52Z, R-8EXD-EWMO.
func TestBoxOnlyResolvesActiveUsersOfTheVerifiedHost(t *testing.T) {
	d, st, host := fixtureStore(t)
	otherHost := provisionFixtureHost(t, st, "i-87654321")
	addBoxAccount(t, st, host.Name, "acme")
	addBoxAccount(t, st, otherHost.Name, "beta")
	active := addBoxUser(t, st, "acme", "daffy")
	locked := addBoxUser(t, st, "acme", "bugs")
	other := addBoxUser(t, st, "beta", "elmer")
	if _, err := st.Lock(t.Context(), locked.Account, locked.Name); err != nil {
		t.Fatal(err)
	}
	stamps := new(requests)
	validHost := stamps.verifier(fixtureSTS{instance: fixtureInstance})
	cfg := Config{Configured: true, Store: st, Verifier: validHost, BoxUser: unexpectedHandler, BoxAdmin: unexpectedHandler}
	post := func(s *Server, uid int64) *httptest.ResponseRecorder {
		return invoke(s, stamps.signed(t.Context(), http.MethodPost, boxPath(uid), neverRead{}))
	}
	s := server(t, cfg)
	for _, uid := range []int64{0, math.MaxUint32, locked.UID, other.UID} {
		expectError(t, post(s, uid), http.StatusForbidden, NotUser)
	}
	missing := cfg
	missing.Verifier = stamps.verifier(fixtureSTS{instance: "i-11223344"})
	expectError(t, post(server(t, missing), active.UID), http.StatusForbidden, UnknownHost)

	// Host lookup succeeds, then the store fails before user lookup.
	s.store = afterHostLookup{st, func() { d.SetFailing(true) }}
	expectError(t, post(s, active.UID), http.StatusServiceUnavailable, store.Unreachable)
	expectError(t, post(server(t, cfg), active.UID), http.StatusServiceUnavailable, store.Unreachable)
	d.SetFailing(false)

	called := 0
	cfg.BoxUser = http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called++ })
	s = server(t, cfg)
	request := stamps.signed(t.Context(), http.MethodPost, boxPath(active.UID), strings.NewReader(listRequest))
	invoke(s, request)
	if called != 1 {
		t.Fatalf("active user's request reached its handler %d times", called)
	}
}

// R-YFWT-GW1M, R-8NGO-3ATJ.
func TestBoxReadsOneBoundedJSONRPCRequest(t *testing.T) {
	_, st, host := fixtureStore(t)
	addBoxAccount(t, st, host.Name, "acme")
	u := addBoxUser(t, st, "acme", "daffy")
	stamps := new(requests)
	calls := 0
	count := http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls++ })
	s := server(t, Config{
		Configured: true, Store: st, Verifier: stamps.verifier(fixtureSTS{instance: fixtureInstance}),
		BoxUser: count, BoxAdmin: count,
	})
	post := func(raw string) *httptest.ResponseRecorder {
		return invoke(s, stamps.signed(t.Context(), http.MethodPost, boxPath(u.UID), strings.NewReader(raw)))
	}
	for _, raw := range []string{
		"", "[" + listRequest + "]", listRequest + " " + listRequest,
		strings.Replace(listRequest, `"jsonrpc":"2.0"`, `"jsonrpc":"1.0"`, 1),
		strings.Replace(listRequest, `"id":1,`, "", 1),
		strings.Replace(listRequest, `"id":1`, `"id":null`, 1),
		strings.Replace(listRequest, `"id":1`, `"id":true`, 1),
		strings.Replace(listRequest, `"id":1`, `"id":1.0`, 1),
		strings.Replace(listRequest, `"id":1`, `"id":1e0`, 1),
		strings.Replace(listRequest, protocol.MethodToolsList, "initialize", 1),
		strings.Replace(listRequest, `"method"`, `"METHOD"`, 1),
		strings.Replace(listRequest, `"method"`, `"metho\u0064"`, 1),
		strings.TrimSuffix(callRequest, "}") + `,"method":"` + protocol.MethodToolsList + `"}`,
		strings.TrimSuffix(callRequest, "}") + `,"metho\u0064":"` + protocol.MethodToolsList + `"}`,
		strings.TrimSuffix(listRequest, "}") + `,"id":2}`,
		strings.TrimSuffix(listRequest, "}") + `,"\u0069d":2}`,
		strings.TrimSuffix(listRequest, "}") + `,"jsonrpc":"2.0"}`,
		strings.TrimSuffix(listRequest, "}") + `,"jsonrp\u0063":"2.0"}`,
	} {
		expectError(t, post(raw), http.StatusBadRequest, BadRequest)
	}
	exact := listRequest + strings.Repeat(" ", protocol.MaxBoxRequest-len(listRequest))
	expectError(t, post(exact+" "), http.StatusRequestEntityTooLarge, TooLarge)
	if calls != 0 {
		t.Fatalf("%d invalid requests reached a handler", calls)
	}
	post(exact)
	post(" \t\r\n" + strings.Replace(listRequest, `"id":1`, `"id":-9007199254740993`, 1) + "\r\n")
	if calls != 2 {
		t.Fatalf("valid requests reached a handler %d times, want 2", calls)
	}
}

// R-8R4D-8M1M, R-8US2-DX9P.
func TestBoxTierAndForwardedCaller(t *testing.T) {
	_, st, host := fixtureStore(t)
	addBoxAccount(t, st, host.Name, "acme")
	ordinary := addBoxUser(t, st, "acme", "daffy")
	admin := addBoxUser(t, st, "acme", "bugs")
	if _, _, err := st.SetAdmin(t.Context(), admin.Account, admin.Name, true); err != nil {
		t.Fatal(err)
	}
	original := identity.Caller{UserID: "space-user", Email: "user@example.com", RequestID: "request-id"}
	for _, tt := range []struct {
		name       string
		user       store.User
		body       string
		withCaller bool
		adminTier  bool
	}{
		{"ordinary list", ordinary, listRequest, false, false},
		{"admin list", admin, listRequest, true, true},
		{"ordinary call", ordinary, callRequest, true, true},
		{"admin call", admin, callRequest, false, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			stamps := new(requests)
			ctx := t.Context()
			wantCaller := identity.Caller{UserID: box.ActorPrefix + tt.user.Name}
			if tt.withCaller {
				ctx = identity.NewContext(ctx, original)
				wantCaller.RequestID = original.RequestID
			}
			body := " \r\n" + tt.body + "\t "
			r := stamps.signed(ctx, http.MethodPost, boxPath(tt.user.UID)+"&extra=preserved", strings.NewReader(body))
			r.Header.Set("X-User-Id", "header-user")
			r.Header.Set("X-User-Email", "header@example.com")
			r.Header.Set("Content-Type", protocol.ContentJSON)
			wantHeaders := r.Header.Clone()
			if tt.withCaller {
				r.Header.Set("Origin", "https://foreign.example")
			}
			calls := 0
			handler := http.HandlerFunc(func(_ http.ResponseWriter, forwarded *http.Request) {
				calls++
				if forwarded.Method != r.Method || *forwarded.URL != *r.URL || !reflect.DeepEqual(forwarded.Header, wantHeaders) {
					t.Fatalf("forwarded request = %s %v %v, want %s %v %v", forwarded.Method, forwarded.URL, forwarded.Header, r.Method, r.URL, wantHeaders)
				}
				if got, ok := identity.FromContext(forwarded.Context()); !ok || got != wantCaller {
					t.Fatalf("forwarded caller = %+v, %v; want %+v", got, ok, wantCaller)
				}
				got, err := io.ReadAll(forwarded.Body)
				if err != nil || string(got) != body {
					t.Fatalf("forwarded body = %q, %v; want %q", got, err, body)
				}
			})
			cfg := Config{Configured: true, Store: st, Verifier: stamps.verifier(fixtureSTS{instance: fixtureInstance}),
				BoxUser: unexpectedHandler, BoxAdmin: unexpectedHandler}
			if tt.adminTier {
				cfg.BoxAdmin = handler
			} else {
				cfg.BoxUser = handler
			}
			invoke(server(t, cfg), r)
			if calls != 1 {
				t.Fatalf("expected handler called %d times", calls)
			}
		})
	}
}

// R-YVRI-FWON.
func TestBoxRelaysHandlerStatusHeadersAndEachWrite(t *testing.T) {
	_, st, host := fixtureStore(t)
	addBoxAccount(t, st, host.Name, "acme")
	u := addBoxUser(t, st, "acme", "daffy")
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	first, proceed := make(chan struct{}), make(chan struct{})
	stamps := new(requests)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Box-Test", "forwarded")
		w.Header().Set("Content-Type", "application/octet-stream")
		if r.URL.Query().Has("status") {
			w.WriteHeader(http.StatusForbidden)
		}
		_, _ = w.Write([]byte{0, 1, 2})
		close(first)
		select {
		case <-proceed:
		case <-r.Context().Done():
			return
		}
		_, _ = w.Write([]byte{3, 4})
	})
	s := server(t, Config{
		Configured: true, Store: st, Verifier: stamps.verifier(fixtureSTS{instance: fixtureInstance}),
		BoxUser: handler, BoxAdmin: unexpectedHandler,
	})
	for _, status := range []int{http.StatusForbidden, http.StatusOK} {
		first, proceed = make(chan struct{}), make(chan struct{})
		path := boxPath(u.UID)
		if status == http.StatusForbidden {
			path += "&status=explicit"
		}
		r := stamps.signed(ctx, http.MethodPost, path, strings.NewReader(listRequest))
		w := httptest.NewRecorder()
		done := make(chan struct{})
		go func() {
			defer close(done)
			s.ServeHTTP(w, r)
		}()
		select {
		case <-first:
		case <-ctx.Done():
			t.Fatal("box handler did not complete its first write")
		}
		gotFirst := w.Body.String()
		close(proceed)
		select {
		case <-done:
		case <-ctx.Done():
			t.Fatal("box response did not finish")
		}
		wantHeaders := http.Header{"Content-Type": {"application/octet-stream"}, "X-Box-Test": {"forwarded"}}
		if gotFirst != "\x00\x01\x02" || w.Body.String() != "\x00\x01\x02\x03\x04" || w.Code != status || !reflect.DeepEqual(w.Header(), wantHeaders) {
			t.Fatalf("relayed response = first %q, full %q, status %d, headers %v", gotFirst, w.Body.String(), w.Code, w.Header())
		}
	}
}

// R-8IL2-K7UR.
func TestBoxLimitsConcurrentRequestsAndReleasesCompletedSlot(t *testing.T) {
	_, st, host := fixtureStore(t)
	addBoxAccount(t, st, host.Name, "acme")
	u := addBoxUser(t, st, "acme", "daffy")
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	stamps := new(requests)
	type transfer struct {
		release chan struct{}
		result  chan *httptest.ResponseRecorder
	}
	entered := make(chan chan struct{}, MaxBoxServing+1)
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		release := make(chan struct{})
		entered <- release
		select {
		case <-release:
			w.WriteHeader(http.StatusNoContent)
		case <-r.Context().Done():
		}
	})
	s := server(t, Config{
		Configured: true, Store: st, Verifier: stamps.verifier(fixtureSTS{instance: fixtureInstance}),
		BoxUser: handler, BoxAdmin: unexpectedHandler,
	})
	start := func() transfer {
		r := stamps.signed(ctx, http.MethodPost, boxPath(u.UID), strings.NewReader(listRequest))
		result := make(chan *httptest.ResponseRecorder, 1)
		go func() { result <- invoke(s, r) }()
		select {
		case release := <-entered:
			return transfer{release: release, result: result}
		case response := <-result:
			t.Fatalf("request returned %d instead of reaching its handler", response.Code)
		case <-ctx.Done():
			t.Fatal("box handler was not reached")
		}
		return transfer{}
	}
	finish := func(pending transfer) {
		close(pending.release)
		select {
		case response := <-pending.result:
			if response.Code != http.StatusNoContent {
				t.Errorf("handler response = %d, want 204", response.Code)
			}
		case <-ctx.Done():
			t.Fatal("released box request did not finish")
		}
	}
	pending := make([]transfer, MaxBoxServing)
	for i := range pending {
		pending[i] = start()
	}
	r := stamps.signed(ctx, http.MethodPost, boxPath(u.UID), neverRead{})
	expectError(t, invoke(s, r), http.StatusServiceUnavailable, Busy)
	finish(pending[0])
	finish(start())
	for _, waiting := range pending[1:] {
		finish(waiting)
	}
}

// R-WB1X-BDCA.
func TestBoxAnswersAtTheRecordAndRequestLimits(t *testing.T) {
	t.Setenv(services.Variable, "")
	_, st, host := fixtureStore(t)
	addBoxAccount(t, st, host.Name, "acme")
	ctx := t.Context()
	creator := identity.Caller{UserID: strings.Repeat("<", valid.MaxUserIDBytes)}
	keys := make([]store.Key, valid.MaxKeysPerUser)
	for i := range keys {
		keys[i] = boxKey(t, i)
	}
	var admin store.User
	for i := range valid.MaxUsersPerHost {
		name := fmt.Sprintf("u%031d", i)
		u, err := st.AddUser(ctx, "acme", name, keys[0], creator)
		if err != nil {
			t.Fatal(err)
		}
		for _, key := range keys[1:] {
			if _, err := st.AddKey(ctx, u.Account, u.Name, key); err != nil {
				t.Fatal(err)
			}
		}
		admin = u
	}
	if _, _, err := st.SetAdmin(ctx, admin.Account, admin.Name, true); err != nil {
		t.Fatal(err)
	}
	writer, _ := recorder(t)
	mcpCfg := mcp.ServerConfig{Name: "accounts", Version: "test-version", Telemetry: writer}
	bu, ba := mcp.NewServer(mcpCfg), mcp.NewServer(mcpCfg)
	boxCfg := box.Config{Store: st, Telemetry: writer}
	box.RegisterUser(bu, boxCfg)
	box.RegisterAdmin(ba, boxCfg)
	stamps := new(requests)
	s := server(t, Config{
		Configured: true, Store: st, Verifier: stamps.verifier(fixtureSTS{instance: fixtureInstance}),
		BoxUser: bu, BoxAdmin: ba, Telemetry: writer,
	})
	for _, tt := range []struct {
		name  string
		field string
		count int
	}{
		{tools.ToolUsers, "users", valid.MaxUsersPerHost},
		{box.ToolKeys, "keys", valid.MaxKeysPerUser},
	} {
		emptyID := fmt.Sprintf(`{"jsonrpc":"2.0","id":"","method":%q,"params":{"name":%q,"arguments":{}}}`, protocol.MethodToolsCall, tt.name)
		id := strings.Repeat("<", protocol.MaxBoxRequest-len(emptyID))
		body := strings.Replace(emptyID, `"id":""`, `"id":"`+id+`"`, 1)
		r := stamps.signed(ctx, http.MethodPost, boxPath(admin.UID), strings.NewReader(body))
		r.Header.Set("Content-Type", protocol.ContentJSON)
		w := invoke(s, r)
		if w.Code != http.StatusOK || w.Header().Get("Content-Type") != protocol.ContentJSON || w.Body.Len() > protocol.MaxBoxAnswer {
			t.Fatalf("%s response = status %d, content type %q, %d bytes", tt.name, w.Code, w.Header().Get("Content-Type"), w.Body.Len())
		}
		var response struct {
			ID     string `json:"id"`
			Result struct {
				IsError bool                       `json:"isError"`
				Value   map[string]json.RawMessage `json:"structuredContent"`
			} `json:"result"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
			t.Fatal(err)
		}
		var entries []json.RawMessage
		if err := json.Unmarshal(response.Result.Value[tt.field], &entries); err != nil {
			t.Fatalf("%s response has invalid %s: %v", tt.name, tt.field, err)
		}
		if response.ID != id || response.Result.IsError || len(entries) != tt.count {
			t.Fatalf("%s response has id of %d bytes, %d rows, isError = %t", tt.name, len(response.ID), len(entries), response.Result.IsError)
		}
	}
}
