package pages_test

import (
	"bytes"
	"context"
	"fmt"
	"html/template"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/paging"
	"github.com/ikigenba/ikigenba/webhooks"
	"github.com/ikigenba/ikigenba/webhooks/internal/pages"
	"github.com/ikigenba/ikigenba/webhooks/internal/store"
	"github.com/ikigenba/ikigenba/webhooks/internal/tools"
)

func equal(t *testing.T, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v; want %#v", got, want)
	}
}

func require(t *testing.T, ok bool, why string) {
	t.Helper()
	if !ok {
		t.Fatal(why)
	}
}

func templates(t *testing.T) *template.Template {
	t.Helper()
	ts, err := page.Templates().ParseFS(webhooks.Assets(), "*.html")
	if err != nil {
		t.Fatal(err)
	}
	return ts
}

func rendered(t *testing.T, ts *template.Template, name string, data any) string {
	t.Helper()
	var b bytes.Buffer
	if err := ts.ExecuteTemplate(&b, name, data); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

func loaded(t *testing.T) *pages.Set {
	t.Helper()
	s, err := pages.Load()
	if err != nil {
		t.Fatal(err)
	}
	require(t, s != nil, "nil set")
	return s
}

func TestPublicNamesAndTemplateExecution(t *testing.T) {
	// R-ZODQ-S4NP
	equal(t, pages.ServiceName, "webhooks")
	const description string = pages.Description
	require(t, description != "", "empty description")
	equal(t, pages.ToolsLevel, page.Level{Name: "tools", URL: "/tools"})
	equal(t, pages.AboutLevel, page.Level{Name: "about", URL: "/about"})
	// R-ZPLN-5WEE R-P3QS-6TI1
	s, err := pages.Load()
	if err != nil {
		t.Fatal(err)
	}
	write := s.Write
	_ = pages.Config{Banner: func(page.User) page.Banner { return page.Banner{} }, Pages: s, ServicesPath: "", Store: (*store.Store)(nil)}
	row := pages.WebhookRow{ID: "whk_example", Slug: "slug", Scheme: "bearer", URL: "https://x.test/in/slug", Owner: "o@example.test", Mine: true, LastReceived: "stamp", LastReceivedText: "minute"}
	b := page.Banner{Service: "webhooks", Release: "test-release", Commit: "test-commit", Email: "a<&@example.test", ProfileURL: "https://auth.example.test/", LogoutURL: "https://auth.example.test/logout"}
	data := []struct {
		name       string
		zero, full any
	}{
		{"landing", pages.LandingData{}, pages.LandingData{Banner: b, Webhooks: []pages.WebhookRow{row}, Page: "current-token", Next: "sample-token"}},
		{"tools", pages.ToolsData{}, pages.ToolsData{Banner: b, Tools: []pages.ToolRow{{Name: "n", Description: "<d>"}}}},
		{"about", pages.AboutData{}, pages.AboutData{Banner: b, Description: "<description>"}},
		{"notfound", pages.NoticeData{}, pages.NoticeData{Banner: b}},
	}
	ts := templates(t)
	// R-ZS1F-XFVS R-ZT9C-B7MH
	for _, d := range data {
		for _, v := range []any{d.zero, d.full} {
			for _, method := range []string{"GET", "HEAD"} {
				for _, status := range []int{200, 404} {
					w := httptest.NewRecorder()
					w.Header()["X-Keep"] = []string{"one"}
					write(w, httptest.NewRequest(method, "/", nil), status, d.name, v)
					equal(t, w.Code, status)
					equal(t, w.Header(), http.Header{"X-Keep": []string{"one"}, "Content-Type": []string{"text/html; charset=utf-8"}})
					want := rendered(t, ts, d.name, v)
					if method == "HEAD" {
						want = ""
					}
					equal(t, w.Body.String(), want)
				}
			}
		}
	}
	t.Chdir(t.TempDir())
	require(t, loaded(t) != nil, "load outside checkout failed")
}

var now = time.Date(2026, 10, 9, 14, 12, 37, 0, time.UTC)

type fixture struct {
	db     *db.DB
	store  *store.Store
	cfg    pages.Config
	banner page.Banner
	path   string
	seen   []page.User
}

func setup(t *testing.T, populated bool) *fixture {
	t.Helper()
	dir := t.TempDir()
	d, err := db.Open(context.Background(), db.Config{Path: filepath.Join(dir, "state", "webhooks.db"), Migrations: webhooks.Migrations(), Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	random := make([]byte, 4096)
	for i := range random {
		random[i] = byte(i*7 + i/256)
	}
	st := store.New(d, store.Config{Now: func() time.Time { return now }, Rand: bytes.NewReader(random)})
	f := &fixture{db: d, store: st, banner: page.Banner{Service: "webhooks", Release: "test-release", Commit: "test-commit"}, path: filepath.Join(dir, "services.json")}
	if populated {
		for _, draft := range []store.Draft{
			{Slug: "n8n_invoice", Scheme: store.Bearer, OwnerID: "u_grace", OwnerEmail: "grace@example.test"},
			{Slug: "gh_push", Scheme: store.GitHubHMAC, OwnerID: "u_ada", OwnerEmail: "ada@example.test"},
		} {
			x, _, e := st.Create(context.Background(), draft)
			if e != nil {
				t.Fatal(e)
			}
			if x.Slug == "gh_push" {
				if _, e = st.Receive(context.Background(), x.ID, store.Arrival{ContentType: "application/json", Body: []byte("{}")}); e != nil {
					t.Fatal(e)
				}
			}
		}
	}
	f.cfg = pages.Config{Banner: func(u page.User) page.Banner { f.seen = append(f.seen, u); return f.banner }, Pages: loaded(t), ServicesPath: f.path, Store: st}
	return f
}

func request(method, target, user, email, host, proto string) *http.Request {
	r := httptest.NewRequest(method, "https://webhooks.example.test"+target, strings.NewReader("arbitrary body"))
	r.Host = host
	if user != "" {
		r.Header.Set("X-User-Id", user)
	}
	r.Header.Set("X-User-Email", email)
	r.Header.Set("X-Forwarded-Proto", proto)
	return r
}

func answer(cfg pages.Config, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	identity.Optional(pages.Handler(cfg)).ServeHTTP(w, r)
	return w
}

func expectedRows(t *testing.T, f *fixture, user, base string) []pages.WebhookRow {
	t.Helper()
	xs, err := f.store.List(context.Background(), "", paging.DefaultLimit)
	if err != nil {
		t.Fatal(err)
	}
	rows := make([]pages.WebhookRow, 0, len(xs))
	for _, x := range xs {
		r := pages.WebhookRow{ID: x.ID, Slug: x.Slug, Scheme: x.Scheme, URL: base + "/in/" + x.Slug, Owner: x.OwnerEmail, Mine: x.OwnerID == user}
		if !x.LastReceived.IsZero() {
			r.LastReceived = x.LastReceived.UTC().Format(time.RFC3339)
			r.LastReceivedText = x.LastReceived.UTC().Format("2006-01-02 15:04")
		}
		rows = append(rows, r)
	}
	return rows
}

func writeServices(t *testing.T, path string, entries map[string]string) {
	t.Helper()
	var parts []string
	for name, u := range entries {
		parts = append(parts, fmt.Sprintf(`{"name":%q,"url":%q,"description":"d","socket":"/unused","enabled":true,"mcp":false}`, name, u))
	}
	if err := os.WriteFile(path, []byte(`{"services":[`+strings.Join(parts, ",")+`]}`), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestSignedInPagesExactBodies(t *testing.T) {
	// R-P66K-YCZF R-ZWX1-GIUK R-ZY4X-UAL9 R-00KQ-LU2N R-01SM-ZLTC
	for _, populated := range []bool{false, true} {
		t.Run(fmt.Sprint(populated), func(t *testing.T) {
			f := setup(t, populated)
			ts := templates(t)
			for _, user := range []string{"u_ada", "u_grace", "u_other"} {
				f.seen = nil
				w := answer(f.cfg, request("GET", "/?q=1", user, "Ada+tag@example.test", "webhooks.sbx.example.test:443", "HTTPS"))
				equal(t, w.Code, 200)
				equal(t, w.Body.String(), rendered(t, ts, "landing", pages.LandingData{Banner: f.banner, Webhooks: expectedRows(t, f, user, "https://webhooks.sbx.example.test:443")}))

				tb := f.banner
				tb.Trail = []page.Level{pages.ToolsLevel}
				var rows []pages.ToolRow
				for _, e := range tools.Catalog() {
					rows = append(rows, pages.ToolRow{Name: e.Name, Description: tools.FirstLine(e.Description)})
				}
				w = answer(f.cfg, request("GET", "/tools", user, "Ada+tag@example.test", "webhooks.sbx.example.test", "https"))
				equal(t, w.Code, 200)
				equal(t, w.Body.String(), rendered(t, ts, "tools", pages.ToolsData{Banner: tb, Tools: rows}))

				ab := f.banner
				ab.Trail = []page.Level{pages.AboutLevel}
				w = answer(f.cfg, request("GET", "/about", user, "Ada+tag@example.test", "webhooks.sbx.example.test", "https"))
				equal(t, w.Code, 200)
				equal(t, w.Body.String(), rendered(t, ts, "about", pages.AboutData{Banner: ab, Description: pages.Description}))
				require(t, len(f.seen) == 3, "banner not called once per page")
				for _, u := range f.seen {
					equal(t, u, page.User{Email: "Ada+tag@example.test", ProfileURL: "https://auth.sbx.example.test/", LogoutURL: "https://auth.sbx.example.test/logout"})
				}
			}
		})
	}
}

func TestServicesAddresses(t *testing.T) {
	// R-00KQ-LU2N R-01SM-ZLTC
	f := setup(t, true)
	ts := templates(t)
	writeServices(t, f.path, map[string]string{"auth": "https://accounts.example.test/", "webhooks": "https://hooks.example.test/"})
	f.seen = nil
	w := answer(f.cfg, request("GET", "/", "u_ada", "a@example.test", "backend", "http"))
	equal(t, w.Code, 200)
	equal(t, w.Body.String(), rendered(t, ts, "landing", pages.LandingData{Banner: f.banner, Webhooks: expectedRows(t, f, "u_ada", "https://hooks.example.test")}))
	equal(t, f.seen, []page.User{{Email: "a@example.test", ProfileURL: "https://accounts.example.test/", LogoutURL: "https://accounts.example.test/logout"}})
	// No services file, http forwarded, no webhooks. prefix.
	if err := os.Remove(f.path); err != nil {
		t.Fatal(err)
	}
	f.seen = nil
	w = answer(f.cfg, request("GET", "/", "u_ada", "a@example.test", "sbx.example.test:8080", "http"))
	equal(t, w.Body.String(), rendered(t, ts, "landing", pages.LandingData{Banner: f.banner, Webhooks: expectedRows(t, f, "u_ada", "http://sbx.example.test:8080")}))
	equal(t, f.seen, []page.User{{Email: "a@example.test", ProfileURL: "http://auth.sbx.example.test/", LogoutURL: "http://auth.sbx.example.test/logout"}})
}

func TestNotFoundMethodsGuestsAndFailures(t *testing.T) {
	// R-ZZCU-82BY R-030J-DDK1 R-048F-R5AQ R-P8MD-PWGT
	f := setup(t, true)
	ts := templates(t)
	notfound := rendered(t, ts, "notfound", pages.NoticeData{Banner: f.banner})
	for _, failing := range []bool{false, true} {
		f.db.SetFailing(failing)
		for _, user := range []string{"", "u_ada"} {
			for _, path := range []string{"/", "/tools", "/about", "/nope", "/in", "/about/", "//"} {
				for _, method := range []string{"GET", "HEAD", "POST", "DELETE"} {
					w := answer(f.cfg, request(method, path+"?x=a%20b", user, "a@example.test", "webhooks.sbx.example.test", "https"))
					isPage := path == "/" || path == "/tools" || path == "/about"
					switch {
					case !isPage:
						equal(t, w.Code, 404)
						want := notfound
						if method == "HEAD" {
							want = ""
						}
						equal(t, w.Body.String(), want)
					case method != "GET" && method != "HEAD":
						equal(t, w.Code, 405)
						equal(t, w.Header().Values("Allow"), []string{"GET, HEAD"})
						equal(t, w.Body.String(), "")
					case user == "":
						equal(t, w.Code, 302)
						equal(t, w.Body.String(), "")
						want := "https://auth.sbx.example.test/?return=" + url.QueryEscape("https://webhooks.sbx.example.test"+path+"?x=a%20b")
						equal(t, w.Header().Get("Location"), want)
					case path == "/" && failing:
						equal(t, w.Code, 503)
						equal(t, w.Header().Get("Content-Type"), "text/plain; charset=utf-8")
						want := store.Unreachable + "\n"
						if method == "HEAD" {
							want = ""
						}
						equal(t, w.Body.String(), want)
					default:
						equal(t, w.Code, 200)
					}
				}
			}
		}
	}
}

// R-P1AZ-FA0N R-P2IV-T1RC R-P66K-YCZF
func TestLandingWindows(t *testing.T) {
	for _, count := range []int{0, paging.DefaultLimit - 1, paging.DefaultLimit, paging.DefaultLimit + 1, 2 * paging.DefaultLimit, 2*paging.DefaultLimit + 1} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			f := setup(t, false)
			var hooks []store.Webhook
			// Insert in reverse order so the expected order is not insertion order.
			for i := count - 1; i >= 0; i-- {
				slug := fmt.Sprintf("hook%03d", i)
				if i < 3 {
					slug = []string{"a1", "a_b", "aa"}[i]
				}
				scheme := store.Bearer
				if i%2 == 0 {
					scheme = store.GitHubHMAC
				}
				x, _, err := f.store.Create(context.Background(), store.Draft{Slug: slug, Scheme: scheme, OwnerID: fmt.Sprintf("user%d", i%2), OwnerEmail: fmt.Sprintf("user%d@example.test", i%2)})
				if err != nil {
					t.Fatal(err)
				}
				if i%2 == 0 {
					if _, err = f.store.Receive(context.Background(), x.ID, store.Arrival{}); err != nil {
						t.Fatal(err)
					}
					x.LastReceived = now
				}
				hooks = append(hooks, x)
			}
			sort.Slice(hooks, func(i, j int) bool { return hooks[i].Slug < hooks[j].Slug })
			ts := templates(t)
			token := paging.Token{Tool: "list"}
			// Check positions that need not be existing or valid webhook slugs.
			for _, position := range []string{"", "B", "hook012x", "zzzz", string([]byte{0xff})} {
				pageValue := ""
				if position != "" {
					pageValue = token.Encode(position)
				}
				assertLandingWindow(t, f, ts, hooks, pageValue, position)
			}
			assertLandingWindow(t, f, ts, hooks, token.Encode(""), "")
			// Traverse every window through its next token.
			var pageValue, position string
			for {
				next := assertLandingWindow(t, f, ts, hooks, pageValue, position)
				if next == "" {
					break
				}
				if err := token.Decode(next, &position); err != nil {
					t.Fatal(err)
				}
				pageValue = next
			}
		})
	}
}

func assertLandingWindow(t *testing.T, f *fixture, ts *template.Template, hooks []store.Webhook, pageValue, position string) string {
	t.Helper()
	var remainder []store.Webhook
	for _, x := range hooks {
		if pageValue == "" || x.Slug > position {
			remainder = append(remainder, x)
		}
	}
	rows := make([]pages.WebhookRow, 0, min(len(remainder), paging.DefaultLimit))
	for _, x := range remainder[:min(len(remainder), paging.DefaultLimit)] {
		row := pages.WebhookRow{ID: x.ID, Slug: x.Slug, Scheme: x.Scheme, URL: "https://hooks.example.test/in/" + x.Slug, Owner: x.OwnerEmail, Mine: x.OwnerID == "user0"}
		if !x.LastReceived.IsZero() {
			row.LastReceived = now.Format(time.RFC3339)
			row.LastReceivedText = now.Format("2006-01-02 15:04")
		}
		rows = append(rows, row)
	}
	var next string
	if len(remainder) > paging.DefaultLimit {
		next = (paging.Token{Tool: "list"}).Encode(remainder[paging.DefaultLimit-1].Slug)
	}
	w := answer(f.cfg, request("GET", "/?page="+url.QueryEscape(pageValue), "user0", "user0@example.test", "hooks.example.test", "https"))
	equal(t, w.Code, 200)
	equal(t, w.Header().Get("Content-Type"), "text/html; charset=utf-8")
	equal(t, w.Body.String(), rendered(t, ts, "landing", pages.LandingData{Banner: f.banner, Webhooks: rows, Page: pageValue, Next: next}))
	return next
}

// R-P1AZ-FA0N R-P7EH-C4Q4 R-P8MD-PWGT R-030J-DDK1 R-048F-R5AQ
func TestLandingTokenRefusalsAndPrecedence(t *testing.T) {
	f := setup(t, true)
	ts := templates(t)
	token := paging.Token{Tool: "list"}
	badPages := []string{"bogus", token.Encode("gh_push") + "!", (paging.Token{Tool: "list_widgets"}).Encode("gh_push"), token.Encode(int64(7))}
	for _, failing := range []bool{false, true} {
		f.db.SetFailing(failing)
		for _, value := range badPages {
			var key string
			require(t, token.Decode(value, &key) != nil, "invalid token fixture decoded")
			for _, method := range []string{"GET", "HEAD", "POST"} {
				for _, user := range []string{"", "u_ada"} {
					target := "/?page=" + url.QueryEscape(value)
					w := answer(f.cfg, request(method, target, user, "a@example.test", "webhooks.example.test", "https"))
					switch {
					case method == "POST":
						equal(t, w.Code, 405)
						equal(t, w.Header().Get("Allow"), "GET, HEAD")
						equal(t, w.Body.String(), "")
					case user == "":
						equal(t, w.Code, 302)
						equal(t, w.Header().Get("Location"), "https://auth.example.test/?return="+url.QueryEscape("https://webhooks.example.test"+target))
						equal(t, w.Body.String(), "")
					default:
						equal(t, w.Code, 404)
						equal(t, w.Header().Get("Content-Type"), "text/html; charset=utf-8")
						body := rendered(t, ts, "notfound", pages.NoticeData{Banner: f.banner})
						if method == "HEAD" {
							body = ""
						}
						equal(t, w.Body.String(), body)
					}
				}
			}
		}
	}
	f.db.SetFailing(true)
	for _, value := range []string{"", token.Encode("gh_push"), token.Encode("zzzz")} {
		w := answer(f.cfg, request("GET", "/?page="+url.QueryEscape(value), "u_ada", "a@example.test", "webhooks.example.test", "https"))
		equal(t, w.Code, 503)
		equal(t, w.Header().Get("Content-Type"), "text/plain; charset=utf-8")
		equal(t, w.Body.String(), store.Unreachable+"\n")
	}
}

// R-P1AZ-FA0N R-P9UA-3O7I
func TestOnlyLandingPageValueAffectsQuery(t *testing.T) {
	f := setup(t, true)
	token := (paging.Token{Tool: "list"}).Encode("gh_push")
	for _, services := range []bool{false, true} {
		if services {
			writeServices(t, f.path, map[string]string{"auth": "https://accounts.example.test", "webhooks": "https://hooks.example.test"})
		}
		for _, failing := range []bool{false, true} {
			f.db.SetFailing(failing)
			for _, method := range []string{"GET", "HEAD", "POST", "DELETE"} {
				for _, path := range []string{"/", "/tools", "/about", "/nope"} {
					queries := [][2]string{
						{"", "?limit=1&ignored=value"},
						{"?page=", "?page=&page=bogus&limit=500"},
						{"?page=" + url.QueryEscape(token), "?limit=1&page=" + url.QueryEscape(token) + "&page=bogus&x=y"},
						{"?page=bogus", "?page=bogus&page=" + url.QueryEscape(token) + "&limit=bad"},
					}
					if path != "/" || (method != "GET" && method != "HEAD") {
						queries = append(queries, [2]string{"?page=bogus", "?page=" + url.QueryEscape(token)})
					}
					for _, pair := range queries {
						responses := make([]*httptest.ResponseRecorder, 2)
						for i, query := range pair {
							responses[i] = answer(f.cfg, request(method, path+query, "u_ada", "a@example.test", "webhooks.example.test", "https"))
						}
						equal(t, responses[0].Code, responses[1].Code)
						equal(t, responses[0].Header(), responses[1].Header())
						equal(t, responses[0].Body.String(), responses[1].Body.String())
					}
				}
			}
		}
	}
}
