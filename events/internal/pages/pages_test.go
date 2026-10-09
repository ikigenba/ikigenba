package pages_test

import (
	"bytes"
	"context"
	"html/template"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	appEvents "github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/events"
	"github.com/ikigenba/ikigenba/events/internal/pages"
	"github.com/ikigenba/ikigenba/events/internal/store"
	"github.com/ikigenba/ikigenba/events/internal/web"
)

func fixture(t *testing.T) (*db.DB, *store.Store, time.Time) {
	t.Helper()
	t.Setenv("IKIGENBA_SERVICES", "")
	now := time.Date(2026, 10, 5, 9, 14, 0, 0, time.FixedZone("offset", 3600))
	clock := func() time.Time { return now }
	d, err := db.Open(context.Background(), db.Config{Path: filepath.Join(t.TempDir(), "log.db"), Migrations: events.Migrations(), Now: clock})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := d.Close(); err != nil {
			t.Error(err)
		}
	})
	return d, store.New(d, store.Config{Now: clock, DepthMax: 8}), now
}
func invoke(f func(http.ResponseWriter, *http.Request), method string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "http://events.space.test:443/?q=x", nil)
	r.Header.Set("X-User-Email", "<person>&@example.test")
	w := httptest.NewRecorder()
	w.Header().Add("Existing", "one")
	w.Header().Add("Existing", "two")
	f(w, r)
	return w
}
func expected(t *testing.T, name string, data any) string {
	t.Helper()
	templates, err := page.Templates().ParseFS(events.Assets(), "*.html")
	if err != nil {
		t.Fatal(err)
	}
	var b bytes.Buffer
	if err := templates.ExecuteTemplate(&b, name, data); err != nil {
		t.Fatal(err)
	}
	return b.String()
}
func check(t *testing.T, w *httptest.ResponseRecorder, status int, body string) {
	t.Helper()
	if w.Code != status || w.Body.String() != body || !reflect.DeepEqual(w.Header(), http.Header{"Existing": {"one", "two"}, "Content-Type": {"text/html; charset=utf-8"}}) {
		t.Fatalf("status=%d headers=%v body mismatch=%v", w.Code, w.Header(), w.Body.String() != body)
	}
}
func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// R-SR5N-PCRU R-FWG8-XFGQ R-QJLY-JFSG R-QKTU-X7J5 R-QM1R-AZ9U R-6RN7-LV47 R-6SV3-ZMUW R-QOHK-2IR8 R-QPPG-GAHX R-QQXC-U28M R-FYW1-OYY4 R-6U30-DELL R-R1WG-9ZWV R-R34C-NRNK R-R4C9-1JE9 R-R5K5-FB4Y R-R6S1-T2VN R-R7ZY-6UMC
func TestExactTemplateAnswers(t *testing.T) {
	d, st, now := fixture(t)
	ctx := context.Background()
	must(t, st.Declare(ctx, "repos", store.Declaration{Emits: []appEvents.Emission{{Event: "repo.pushed"}}}))
	for _, service := range []string{"scripts", "sites", "gone"} {
		must(t, st.Declare(ctx, service, store.Declaration{Accepts: []string{"repo.pushed"}}))
	}
	must(t, st.Deliver(ctx, appEvents.Event{ID: "evt_0000000000000001", Time: now, Service: "repos", Name: "repo.pushed", RequestID: "request", User: "user", Attrs: appEvents.Attrs{}}))
	must(t, st.Advance(ctx, "scripts", 1))
	must(t, st.Pause(ctx, "sites", 1, "publish <failed>&"))
	must(t, st.Forget(ctx, "gone"))
	p := pages.New(pages.Config{Banner: page.New(appEvents.ServiceName, "test-build").Banner, Store: st})
	var _ web.Pages = p
	b := page.Banner{Service: appEvents.ServiceName, Version: "test-build", Email: "<person>&@example.test", ProfileURL: "https://auth.space.test/", LogoutURL: "https://auth.space.test/logout"}
	subs, err := st.Subscribers(ctx)
	must(t, err)
	rows := []pages.SubscriberRow{}
	for _, s := range subs {
		var reason *pages.Reason
		if s.Reason != nil {
			reason = &pages.Reason{s.Reason.Name, s.Reason.Seq, s.Reason.Error}
		}
		// Unkeyed construction proves the exact row and reason field shapes.
		rows = append(rows, pages.SubscriberRow{s.Service, string(s.Status), strconv.FormatInt(s.Cursor, 10), strconv.FormatInt(s.Lag, 10), s.Since, reason})
	}
	changed := st.Changed()
	for _, method := range []string{"GET", "HEAD"} {
		body := expected(t, "landing", pages.LandingData{Banner: b, Subscribers: rows})
		if method == "HEAD" {
			body = ""
		}
		check(t, invoke(p.Landing, method), 200, body)
	}
	select {
	case <-changed:
		t.Fatal("Landing changed store")
	default:
	}
	after, err := st.Subscribers(ctx)
	must(t, err)
	if !reflect.DeepEqual(subs, after) {
		t.Fatal(after)
	}
	const description = pages.Description
	type copyText string
	_ = copyText(description)
	notice := pages.NoticeData{Banner: page.Banner{Service: appEvents.ServiceName, Version: "test-build"}}
	for _, failing := range []bool{false, true} {
		d.SetFailing(failing)
		for _, method := range []string{"GET", "HEAD"} {
			body := expected(t, "about", pages.AboutData{Banner: b, Description: pages.Description})
			if method == "HEAD" {
				body = ""
			}
			check(t, invoke(p.About, method), 200, body)
			body = expected(t, "notfound", notice)
			if method == "HEAD" {
				body = ""
			}
			check(t, invoke(p.NotFound, method), 404, body)
			if failing {
				body = expected(t, "unavailable", notice)
				if method == "HEAD" {
					body = ""
				}
				check(t, invoke(p.Landing, method), 503, body)
			}
		}
	}
	nilStore := pages.New(pages.Config{Banner: page.New(appEvents.ServiceName, "test-build").Banner})
	check(t, invoke(nilStore.About, "GET"), 200, expected(t, "about", pages.AboutData{Banner: b, Description: pages.Description}))
	check(t, invoke(nilStore.NotFound, "POST"), 404, expected(t, "notfound", notice))
}

// R-QTD5-LLQ0 R-QUL1-ZDGP R-QX0U-QWY3 R-FXO5-B77F R-SW19-8FQM
func TestBannerRefreshAndFallback(t *testing.T) {
	_, st, _ := fixture(t)
	path := filepath.Join(t.TempDir(), "services.json")
	t.Setenv("IKIGENBA_SERVICES", path)
	p := pages.New(pages.Config{Banner: page.New(appEvents.ServiceName, "test").Banner, ServicesPath: path, Store: st})
	for _, host := range []string{"events.space.test:443", "EvEnTs.space.test", "space.test"} {
		for _, proto := range []string{"http", "https", "HTTPS", "https, http", ""} {
			r := httptest.NewRequest("GET", "/", nil)
			r.Host = host
			r.Header.Set("X-Forwarded-Proto", proto)
			r.Header.Set("X-User-Email", "email")
			w := httptest.NewRecorder()
			p.About(w, r)
			scheme := proto
			if scheme != "http" && scheme != "https" {
				scheme = "https"
			}
			b := page.Banner{Service: "events", Version: "test", Email: "email", ProfileURL: scheme + "://auth.space.test/", LogoutURL: scheme + "://auth.space.test/logout"}
			if w.Body.String() != expected(t, "about", pages.AboutData{Banner: b, Description: pages.Description}) {
				t.Fatal(host, proto)
			}
		}
	}
	write := func(enabled bool) {
		t.Helper()
		value := `{"services":[{"name":"other","url":"https://other.test","description":"other","socket":"","enabled":` + strconv.FormatBool(enabled) + `,"mcp":false,"icon":"fixture-icon"},{"name":"auth","url":"https://accounts.test","description":"auth","socket":"","enabled":true,"mcp":false},{"name":"events","url":"https://events.test","description":"events","socket":"","enabled":true,"mcp":true,"icon":"fixture-icon"}]}`
		must(t, os.WriteFile(path, []byte(value), 0600))
	}
	for _, enabled := range []bool{true, false} {
		write(enabled)
		b := page.Banner{Service: "events", Version: "test", Email: "<person>&@example.test", ProfileURL: "https://accounts.test/", LogoutURL: "https://accounts.test/logout", Icon: template.HTML("fixture-icon"), Services: []page.Service{{Name: "other", URL: "https://other.test", Icon: template.HTML("fixture-icon"), Enabled: enabled}, {Name: "events", URL: "https://events.test", Icon: template.HTML("fixture-icon"), Enabled: true, Current: true}}}
		check(t, invoke(p.Landing, "GET"), 200, expected(t, "landing", pages.LandingData{Banner: b}))
		check(t, invoke(p.About, "GET"), 200, expected(t, "about", pages.AboutData{Banner: b, Description: pages.Description}))
	}
	must(t, os.WriteFile(path, []byte(`broken`), 0600))
	b := page.Banner{Service: "events", Version: "test", Email: "<person>&@example.test", ProfileURL: "https://auth.space.test/", LogoutURL: "https://auth.space.test/logout"}
	check(t, invoke(p.About, "GET"), 200, expected(t, "about", pages.AboutData{Banner: b, Description: pages.Description}))
}

// R-RAFQ-YE3Q
func TestConcurrentPages(t *testing.T) {
	_, st, _ := fixture(t)
	p := pages.New(pages.Config{Banner: page.New(appEvents.ServiceName, "test").Banner, Store: st})
	banner := page.Banner{Service: "events", Version: "test", Email: "<person>&@example.test", ProfileURL: "https://auth.space.test/", LogoutURL: "https://auth.space.test/logout"}
	cases := []struct {
		handler func(http.ResponseWriter, *http.Request)
		status  int
		body    string
	}{
		{p.Landing, 200, expected(t, "landing", pages.LandingData{Banner: banner})},
		{p.About, 200, expected(t, "about", pages.AboutData{Banner: banner, Description: pages.Description})},
		{p.NotFound, 404, expected(t, "notfound", pages.NoticeData{Banner: page.Banner{Service: "events", Version: "test"}})},
	}
	var wg sync.WaitGroup
	for range 20 {
		for _, tc := range cases {
			wg.Go(func() {
				out := invoke(tc.handler, "GET")
				if out.Code != tc.status || out.Body.String() != tc.body || !reflect.DeepEqual(out.Header(), http.Header{"Existing": {"one", "two"}, "Content-Type": {"text/html; charset=utf-8"}}) {
					t.Error("concurrent answer mismatch", out.Code, tc.status)
				}
			})
		}
	}
	wg.Wait()
}

// R-FWG8-XFGQ R-FYW1-OYY4 R-FXO5-B77F R-SW19-8FQM
func TestBannerSourcePerAnswer(t *testing.T) {
	d, st, _ := fixture(t)
	path := filepath.Join(t.TempDir(), "services.json")
	must(t, os.WriteFile(path, []byte(`{"services":[{"name":"auth","url":"https://accounts.test","description":"auth","socket":"","enabled":true,"mcp":false},{"name":"path-only","url":"https://path.test","description":"path","socket":"","enabled":true,"mcp":false,"icon":"fixture-icon"}]}`), 0600))
	var users []page.User
	returned := page.Banner{
		Service: "supplied-service", Version: "construction",
		Email: "supplied-email", ProfileURL: "https://supplied.test/profile", LogoutURL: "https://supplied.test/out",
		Icon:     template.HTML("own-icon"),
		Services: []page.Service{{Name: "source-only", URL: "https://source.test", Icon: template.HTML("launcher-icon"), Enabled: true}},
	}
	source := func(u page.User) page.Banner {
		users = append(users, u)
		return returned
	}
	// An unkeyed construction uses the declared field order and types.
	p := pages.New(pages.Config{source, path, st})
	answer := 0
	for _, failing := range []bool{false, true} {
		d.SetFailing(failing)
		for _, method := range []string{"GET", "HEAD"} {
			for _, route := range []struct {
				handler func(http.ResponseWriter, *http.Request)
				name    string
				status  int
			}{{p.Landing, "landing", 200}, {p.About, "about", 200}, {p.NotFound, "notfound", 404}} {
				name, status := route.name, route.status
				if failing && name == "landing" {
					name, status = "unavailable", 503
				}
				answer++
				returned.Version = "answer-" + strconv.Itoa(answer)
				before := len(users)
				out := invoke(route.handler, method)
				wantUser := page.User{}
				var data any = pages.NoticeData{Banner: returned}
				if name == "landing" || name == "about" {
					wantUser = page.User{Email: "<person>&@example.test", ProfileURL: "https://accounts.test/", LogoutURL: "https://accounts.test/logout"}
					if name == "landing" {
						data = pages.LandingData{Banner: returned}
					} else {
						data = pages.AboutData{Banner: returned, Description: pages.Description}
					}
				}
				found := false
				for _, u := range users[before:] {
					if u == wantUser {
						found = true
					}
				}
				if !found {
					t.Fatalf("%s did not request banner for %#v; calls: %#v", name, wantUser, users[before:])
				}
				want := expected(t, name, data)
				if method == "HEAD" {
					want = ""
				}
				check(t, out, status, want)
			}
		}
	}
}
