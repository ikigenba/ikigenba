package web_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/sites/internal/store"
	"github.com/ikigenba/ikigenba/sites/internal/visitor"
	"github.com/ikigenba/ikigenba/sites/internal/web"
)

//	R-AJOV-DIFN R-FA6U-BB22 R-RENJ-N1IL R-ZWY1-7AE7
//
// R-UH9Z-UHBP R-X244-JYKW R-XRGM-7X3F
func TestRequestTrails(t *testing.T) {
	f := fresh(t)
	f.repository(t)
	sha := f.commit(t, "old body")
	s := f.add(t, "blog", store.Public)
	if e := f.cfg.Cache.Unpack(context.Background(), s.ID, s.Repo, sha, ""); e != nil {
		t.Fatal(e)
	}
	if _, e := f.cfg.Store.Publish(context.Background(), s.ID, sha, ""); e != nil {
		t.Fatal(e)
	}
	f.add(t, "private", store.Private)
	f.get(t, "GET", "/blog/?secret=query", "sites", "", map[string]string{"Referer": "https://example.invalid/path?secret=hidden"})
	f.get(t, "GET", "/blog/", "sites", "user", map[string]string{"Cookie": "ikigenba_visitor=vis_0123456789abcdef"})
	for _, q := range []struct{ method, path, host, user string }{{"GET", "/", "sites", "user"}, {"GET", "/about", "sites", "user"}, {"GET", "/_appkit/theme.css", "sites", ""}, {"GET", "/nosuch/", "sites", ""}, {"GET", "/private/", "sites", ""}, {"POST", "/blog/", "sites", ""}, {"GET", "/mcp", "sites", ""}, {"HEAD", "/", "sites", "user"}, {"GET", "/blog/", "ikigenba.dev", ""}} {
		r := f.get(t, q.method, q.path, q.host, q.user, map[string]string{"Cookie": "ikigenba_visitor=vis_0123456789abcdef"})
		if len(r.Header().Values("Set-Cookie")) != 0 {
			t.Fatal("nonview sets cookie", q)
		}
	}
	for _, q := range []struct{ name, args string }{{"create", `{"name":"newsite","repo":"rep_0123456789abcdef"}`}, {"publish", `{"name":"newsite"}`}, {"update", `{"name":"newsite","listed":false,"ref":"preview"}`}, {"apex", `{"name":"newsite"}`}, {"delete", `{"name":"newsite"}`}, {"publish", `{"name":"nope"}`}} {
		f.call(t, q.name, q.args)
	}
	if e := os.RemoveAll(f.cfg.Cache.Dir(s.ID, sha, "")); e != nil {
		t.Fatal(e)
	}
	f.cfg.Limits.Drain()
	f.get(t, "GET", "/blog/", "sites", "", nil)
	events := f.events(t)
	trails := map[string][]telemetry.Event{}
	for _, e := range events {
		trails[e.RequestID] = append(trails[e.RequestID], e)
		if e.RequestID == "" {
			t.Fatal("empty request id")
		}
		if strings.HasPrefix(e.Name, "site.") {
			if e.Attrs["site"] != "" && !store.ValidID(e.Attrs["site"].(string)) {
				t.Fatal("site id")
			}
			if v, ok := e.Attrs["visitor"]; ok && !visitor.ValidID(v.(string)) {
				t.Fatal("visitor id")
			}
			if v, ok := e.Attrs["repo"]; ok && v != "rep_0123456789abcdef" {
				t.Fatal("repo id")
			}
		}
	}
	viewed, mutation := 0, 0
	for _, trail := range trails {
		if trail[0].Name != "request.started" || trail[len(trail)-1].Name != "request.finished" {
			t.Fatalf("request bounds %+v", trail)
		}
		if len(trail[0].Attrs) != 2 || len(trail[len(trail)-1].Attrs) != 4 {
			t.Fatal("request attributes")
		}
		tool := false
		seenUnavailable := false
		seenView := false
		for _, e := range trail {
			if e.RequestID != trail[0].RequestID || e.User != trail[0].User {
				t.Fatal("envelope mixed")
			}
			if e.Name == "tool.called" {
				tool = true
			}
			if strings.HasPrefix(e.Name, "site.") && tool {
				t.Fatal("site event after tool")
			}
			if e.Name == "site.viewed" {
				seenView = true
				viewed++
				if e.Attrs["path"] != "/blog/" {
					t.Fatal("query leaked")
				}
			}
			if e.Name == "site.unavailable" {
				if !seenView {
					t.Fatal("unavailable before view")
				}
				seenUnavailable = true
			}
			if e.Name == "site.created" {
				mutation++
			}
		}
		if seenUnavailable {
			t.Fatal("stopping or refused publish emitted unavailable")
		}
	}
	if viewed != 3 || mutation != 1 {
		t.Fatalf("views=%d creates=%d", viewed, mutation)
	}
	// Catalog refusal remains a nonview and request middleware counts its bytes.
	f.db.SetFailing(true)
	r := f.get(t, "GET", "/blog/", "sites", "", nil)
	if r.Code != 503 || r.Header().Get("Set-Cookie") != "" {
		t.Fatal("catalog cookie")
	}
	es := f.events(t)
	last := es[len(es)-1]
	if last.Name != "request.finished" || last.Attrs["status"] != int64(503) || last.Attrs["request_bytes"] != int64(0) || last.Attrs["response_bytes"] != int64(42) {
		t.Fatalf("catalog accounting %+v", last)
	}
}

// R-WSCX-HSNC R-QOV8-CWVK
func TestServicesURLReloadWithoutSocketCalls(t *testing.T) {
	f := fresh(t)
	s := f.add(t, "blog", store.Public)
	sock := filepath.Join(t.TempDir(), "service.sock")
	ln, e := net.ListenUnix("unix", &net.UnixAddr{Name: sock, Net: "unix"})
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = ln.Close() }()
	path := filepath.Join(f.root, "services.json")
	f.cfg.ServicesPath = path
	// A newly configured server is needed: Handler registers tools once.
	f.cfg.MCP = newServer(f.cfg.Telemetry)
	f.h = web.Handler(f.cfg)
	for _, base := range []string{"https://sites.one.example.invalid", "https://sites.two.example.invalid"} {
		b, _ := json.Marshal(map[string]any{"services": []any{map[string]any{"name": "sites", "url": base, "description": "fixture", "socket": sock, "enabled": true, "mcp": true, "icon": "<svg></svg>"}}})
		if e := os.WriteFile(path, b, 0600); e != nil {
			t.Fatal(e)
		}
		r := f.call(t, "show", `{"name":"blog"}`)
		b, _ = json.Marshal(r)
		if !strings.Contains(string(b), base+"/"+s.Slug+"/") {
			t.Fatalf("URL not reloaded %s", b)
		}
	}
	for _, q := range []struct{ name, args string }{{"list", `{}`}, {"create", `{"name":"x","repo":"missing"}`}, {"publish", `{"name":"missing"}`}, {"update", `{"name":"blog","ref":"preview"}`}, {"apex", `{}`}, {"delete", `{"name":"missing"}`}} {
		f.call(t, q.name, q.args)
	}
	f.get(t, "GET", "/blog/", "sites", "", nil)
	f.get(t, "GET", "/", "example.invalid", "", nil)
	f.get(t, "GET", "/", "domain.example.com", "", nil)
	if e := ln.SetDeadline(time.Now().Add(20 * time.Millisecond)); e != nil {
		t.Fatal(e)
	}
	conn, e := ln.Accept()
	if e == nil {
		_ = conn.Close()
		t.Fatal("handler connected to service socket")
	}
	var ne net.Error
	if !errors.As(e, &ne) || !ne.Timeout() {
		t.Fatal(e)
	}
}

// R-XEB4-DNZU
func TestConcurrentRequestsKeepTheirTrails(t *testing.T) {
	f := fresh(t)
	var wg sync.WaitGroup
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) { defer wg.Done(); f.get(t, "GET", "/about", "sites", fmt.Sprintf("user%d", i), nil) }(i)
	}
	wg.Wait()
	es := f.events(t)
	users := map[string]string{}
	for _, e := range es {
		if prev, ok := users[e.RequestID]; ok && prev != e.User {
			t.Fatal("crossed callers")
		}
		users[e.RequestID] = e.User
	}
	if len(users) != 32 {
		t.Fatal("request ids collided")
	}
}

// R-AJOV-DIFN
func TestNonviewsHaveOnlyRequestEvents(t *testing.T) {
	f := fresh(t)
	configureServices(t, f)
	public := f.add(t, "blog", store.Public)
	private := f.add(t, "private", store.Private)
	domain := "blog.example.com"
	if _, _, e := f.cfg.Store.Update(context.Background(), public.ID, store.Change{Domain: &domain}); e != nil {
		t.Fatal(e)
	}
	// A restored private row with a domain must never become a view.
	if e := f.db.Write(context.Background(), func(tx *sql.Tx) error {
		_, e := tx.ExecContext(context.Background(), "UPDATE sites SET domain=? WHERE id=?", "private.example.com", private.ID)
		return e
	}); e != nil {
		t.Fatal(e)
	}
	if _, e := f.cfg.Store.SetApex(context.Background(), public.ID); e != nil {
		t.Fatal(e)
	}
	for _, cookie := range []string{"", "ikigenba_visitor=vis_0123456789abcdef", "ikigenba_visitor=invalid"} {
		for _, q := range []struct{ method, path, host, user string }{
			{"GET", "/", "sites", "user"}, {"GET", "/about", "sites", "user"}, {"GET", "/_appkit/theme.css", "sites", ""},
			{"GET", "/nosuch/", "sites", ""}, {"GET", "/private/", "sites", ""}, {"POST", "/blog/", "sites", ""}, {"GET", "/mcp", "sites", ""},
			{"GET", "/blog/", "ikigenba.dev", ""}, {"POST", "/mcp", "ikigenba.dev", ""},
			{"GET", "/", "absent.example.com", ""}, {"GET", "/", "private.example.com", ""}, {"GET", "/", "private.example.com", "user"},
			{"POST", "/", "blog.example.com", ""}, {"POST", "/mcp", "blog.example.com", ""},
		} {
			r := f.get(t, q.method, q.path, q.host, q.user, map[string]string{"Cookie": cookie})
			if len(r.Header().Values("Set-Cookie")) != 0 {
				t.Fatal("nonview cookie", q)
			}
		}
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Host = "sites"
		f.h.ServeHTTP(w, r)
		if len(w.Header().Values("Set-Cookie")) != 0 {
			t.Error("MCP nonview set cookie")
		}
	}))
	defer server.Close()
	client := mcp.NewClient(mcp.ClientConfig{Endpoint: server.URL + "/mcp", HTTPClient: server.Client()})
	for _, q := range []struct{ name, args string }{{"list", `{}`}, {"show", `{"name":"blog"}`}, {"apex", `{}`}, {"create", `{"name":"invalid space","repo":"missing"}`}, {"publish", `{"name":"missing"}`}, {"update", `{"name":"missing","listed":false}`}, {"delete", `{"name":"missing"}`}} {
		if _, e := client.CallTool(context.Background(), identity.Caller{UserID: "user", RequestID: "nonview-" + q.name}, q.name, json.RawMessage(q.args)); e != nil {
			t.Fatal(e)
		}
	}
	f.db.SetFailing(true)
	for _, host := range []string{"sites", "blog.example.com", "absent.example.com"} {
		for _, cookie := range []string{"", "ikigenba_visitor=vis_0123456789abcdef", "ikigenba_visitor=invalid"} {
			r := f.get(t, "GET", "/blog/", host, "", map[string]string{"Cookie": cookie})
			if r.Code != 503 || len(r.Header().Values("Set-Cookie")) != 0 {
				t.Fatal("catalog refusal", host, r.Code)
			}
		}
	}
	for _, e := range f.events(t) {
		if e.Name != "request.started" && e.Name != "request.finished" && e.Name != "tool.called" {
			t.Fatalf("nonview event %+v", e)
		}
	}
}

// R-XPM2-VTHM R-FA6U-BB22
func TestIdentityAndRequestIDHeaders(t *testing.T) {
	f := fresh(t)
	f.add(t, "blog", store.Public)
	for _, q := range []struct {
		user, request         []string
		wantUser, wantRequest string
	}{{[]string{"user", "other"}, []string{"provided", "ignored"}, "user", "provided"}, {[]string{"", "other"}, []string{"", "ignored"}, "", ""}, {nil, nil, "", ""}} {
		r := httptest.NewRequest("GET", "/blog/", nil)
		r.Host = "sites"
		r.Header["X-User-Id"] = q.user
		r.Header["X-Request-Id"] = q.request
		r.Header.Set("X-User-Email", "ignored@example.invalid")
		r.Header.Set("Cookie", "ikigenba_visitor=vis_0123456789abcdef")
		w := httptest.NewRecorder()
		f.h.ServeHTTP(w, r)
		es := f.events(t)
		trail := es[len(es)-3:]
		for _, e := range trail {
			if e.User != q.wantUser || e.RequestID == "" || (q.wantRequest != "" && e.RequestID != q.wantRequest) {
				t.Fatalf("identity %+v", e)
			}
		}
	}
}

// R-RENJ-N1IL R-ZWY1-7AE7
func TestUnavailableFollowsItsView(t *testing.T) {
	f := fresh(t)
	s := f.add(t, "blog", store.Public)
	if _, e := f.cfg.Store.Publish(context.Background(), s.ID, "0123456789abcdef0123456789abcdef01234567", ""); e != nil {
		t.Fatal(e)
	}
	r := f.get(t, "GET", "/blog/", "sites", "", nil)
	if r.Code != 503 {
		t.Fatal(r.Code)
	}
	es := f.events(t)
	if len(es) != 4 || es[0].Name != "request.started" || es[1].Name != "site.viewed" || es[2].Name != "site.unavailable" || es[3].Name != "request.finished" {
		t.Fatalf("unavailable trail %+v", es)
	}
}
