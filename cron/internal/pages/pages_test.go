package pages_test

import (
	"bytes"
	"context"
	"fmt"
	"html"
	"html/template"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	cron "github.com/ikigenba/ikigenba/cron"
	"github.com/ikigenba/ikigenba/cron/internal/pages"
	"github.com/ikigenba/ikigenba/cron/internal/scheduler"
	"github.com/ikigenba/ikigenba/cron/internal/store"
	"github.com/ikigenba/ikigenba/cron/internal/trail"
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
	// R-D7V6-ODWX R-CK47-AGUS
	ts, err := page.Templates().ParseFS(cron.Assets(), "*.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"landing", "about", "notfound"} {
		require(t, ts.Lookup(n) != nil, "missing template "+n)
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

func TestPublicDataAndTemplateExecution(t *testing.T) {
	// R-CUGA-GWRA R-CVO6-UOHZ R-CWW3-8G8O R-CY3Z-M7ZD
	equal(t, pages.ServiceName, "cron")
	equal(t, pages.Description, "Triggers that emit events on a schedule")
	load := pages.Load
	s, err := load()
	if err != nil {
		t.Fatal(err)
	}
	write := s.Write
	// R-CZBV-ZZQ2 R-D1RO-RJ7G R-D2ZL-5AY5 R-D47H-J2OU
	row := pages.TriggerRow{"crn_example", "slug", "@hourly", "owner@example.test", "active", true, "stamp", "minute", "next", "nextminute"}
	equal(t, row.ID, "crn_example")
	equal(t, row.Mine, true)
	equal(t, row.NextText, "nextminute")
	b := page.Banner{Service: "cron", Version: "test-display", Email: "a<&@example.test", ProfileURL: "https://auth.example.test/", LogoutURL: "https://auth.example.test/logout"}
	data := []struct {
		name       string
		zero, full any
	}{
		{"landing", pages.LandingData{}, pages.LandingData{b, []pages.TriggerRow{row}}},
		{"about", pages.AboutData{}, pages.AboutData{b, "<description>"}},
		{"notfound", pages.NoticeData{}, pages.NoticeData{b}},
	}
	ts := templates(t)
	// R-CMK0-20C6 R-CNRW-FS2V R-COZS-TJTK R-CQ7P-7BK9
	for _, d := range data {
		for _, v := range []any{d.zero, d.full} {
			for _, method := range []string{"GET", "HEAD", "POST"} {
				for _, status := range []int{200, 404} {
					w := httptest.NewRecorder()
					w.Header()["X-Keep"] = []string{"one", "two"}
					w.Header()["Content-Type"] = []string{"old", "older"}
					write(w, httptest.NewRequest(method, "/", nil), status, d.name, v)
					equal(t, w.Code, status)
					equal(t, w.Header(), http.Header{"X-Keep": []string{"one", "two"}, "Content-Type": []string{"text/html; charset=utf-8"}})
					want := rendered(t, ts, d.name, v)
					if method == "HEAD" {
						want = ""
					}
					equal(t, w.Body.String(), want)
				}
			}
		}
	}
	// R-CLC3-O8LH
	t.Chdir(t.TempDir())
	for range 2 {
		require(t, loaded(t) != nil, "load outside checkout failed")
	}
}

type fixture struct {
	db        *db.DB
	store     *store.Store
	scheduler *scheduler.Scheduler
	cfg       pages.Config
	banner    page.Banner
	path      string
	triggers  []store.Trigger
}

var now = time.Date(2026, 10, 5, 9, 32, 0, 0, time.UTC)

func setup(t *testing.T, populated bool) *fixture {
	t.Helper()
	dir := t.TempDir()
	d, err := db.Open(context.Background(), db.Config{Path: filepath.Join(dir, "state", "cron.db"), Migrations: cron.Migrations(), Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := d.Close(); err != nil {
			t.Error(err)
		}
	})
	random := make([]byte, 1024)
	for i := range random {
		random[i] = byte(i)
	}
	st := store.New(d, store.Config{Now: func() time.Time { return now.Add(-24 * time.Hour) }, Rand: bytes.NewReader(random)})
	f := &fixture{db: d, store: st, banner: page.Banner{Service: "cron", Version: "test-display"}, path: filepath.Join(dir, "services.json")}
	if populated {
		for _, draft := range []store.Draft{
			{Slug: "weekly_digest", When: "@weekly", OwnerID: "u_7f3a9c21", OwnerEmail: "mira@example.test"},
			{Slug: "nightly_backup", When: "0 2 * * *", OwnerID: "u_2b8e1d04", OwnerEmail: "noah@example.test"},
			{Slug: "month_end", When: "0 0 31 * *", OwnerID: "u_2b8e1d04", OwnerEmail: "noah@example.test"},
			{Slug: "hourly", When: "@hourly", OwnerID: "u_7f3a9c21", OwnerEmail: "mira@example.test"},
			{Slug: "never", When: "0 0 31 2 *", OwnerID: "u_2b8e1d04", OwnerEmail: "noah@example.test"},
		} {
			x, e := st.Create(context.Background(), draft)
			if e != nil {
				t.Fatal(e)
			}
			if x.Slug == "weekly_digest" {
				if _, e = st.SetStatus(context.Background(), x.ID, store.Paused); e != nil {
					t.Fatal(e)
				}
			}
			if x.Slug == "hourly" {
				if _, e = st.SetLastFired(context.Background(), x.ID, time.Date(2026, 10, 5, 11, 0, 0, 0, time.FixedZone("east", 7200))); e != nil {
					t.Fatal(e)
				}
			}
		}
	}
	w := telemetry.New(telemetry.Config{Service: "cron", Sink: &telemetry.Capture{}, Stderr: &bytes.Buffer{}, Now: func() time.Time { return now }, Rand: bytes.NewReader(make([]byte, 1024)), Sleep: func(context.Context, time.Duration) {}})
	t.Cleanup(func() { w.Shutdown(context.Background(), "test complete") })
	em := events.New(events.Config{Service: "cron", Sink: &events.Capture{}, Stderr: &bytes.Buffer{}, Now: func() time.Time { return now }, Rand: bytes.NewReader(make([]byte, 1024)), Sleep: func(context.Context, time.Duration) {}, Telemetry: w, Emits: trail.Emits()})
	t.Cleanup(func() { em.Shutdown(context.Background()) })
	sch, err := scheduler.Start(context.Background(), scheduler.Config{Store: st, Events: em, Telemetry: w, Now: func() time.Time { return now }, After: func(time.Duration) <-chan time.Time { return make(chan time.Time) }, Rand: bytes.NewReader(make([]byte, 1024))})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(sch.Stop)
	f.scheduler = sch
	f.triggers, err = st.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// R-D5FD-WUFJ R-D6NA-AM68
	f.cfg = pages.Config{func(page.User) page.Banner { return f.banner }, loaded(t), f.path, st, sch}
	return f
}
func request(method, path, user, email, host, proto string) *http.Request {
	r := httptest.NewRequest(method, "https://cron.example.test"+path, strings.NewReader("arbitrary body"))
	r.Host = host
	if user != "" {
		r.Header.Set("X-User-Id", user)
	}
	r.Header.Set("X-User-Email", email)
	r.Header.Set("X-Forwarded-Proto", proto)
	r.Header.Set("X-Other", "irrelevant")
	return r
}
func answer(cfg pages.Config, r *http.Request) *httptest.ResponseRecorder {
	// R-DDYO-L8ME
	w := httptest.NewRecorder()
	identity.Require(pages.Handler(cfg)).ServeHTTP(w, r)
	return w
}
func expectedRows(f *fixture, user string) []pages.TriggerRow {
	// R-DK26-I3BV R-DLA2-VV2K
	rows := make([]pages.TriggerRow, 0, len(f.triggers))
	for _, x := range f.triggers {
		r := pages.TriggerRow{ID: x.ID, Slug: x.Slug, When: x.When, Owner: x.OwnerEmail, Status: x.Status, Mine: x.OwnerID == user}
		if !x.LastFired.IsZero() {
			r.LastFired = x.LastFired.UTC().Format(time.RFC3339)
			r.LastFiredText = x.LastFired.UTC().Format("2006-01-02 15:04")
		}
		if n, ok := f.scheduler.Next(x.ID); ok {
			r.Next = n.UTC().Format(time.RFC3339)
			r.NextText = n.UTC().Format("2006-01-02 15:04")
		}
		rows = append(rows, r)
	}
	return rows
}
func TestLandingAndAboutExactBodies(t *testing.T) {
	// R-DMHZ-9MT9 R-DNPV-NEJY
	for _, populated := range []bool{false, true} {
		t.Run(fmt.Sprint(populated), func(t *testing.T) {
			f := setup(t, populated)
			ts := templates(t)
			for _, user := range []string{"u_7f3a9c21", "u_2b8e1d04"} {
				for _, path := range []string{"/", "/about"} {
					var seen []page.User
					f.cfg.Banner = func(u page.User) page.Banner { seen = append(seen, u); return f.banner }
					w := answer(f.cfg, request("GET", path+"?arbitrary=query", user, "MiRa+tag@example.test", "cron.sbx.example.test:443", "HTTPS"))
					equal(t, w.Code, 200)
					equal(t, w.Header(), http.Header{"Content-Type": []string{"text/html; charset=utf-8"}})
					want := rendered(t, ts, "about", pages.AboutData{Banner: f.banner, Description: pages.Description})
					if path == "/" {
						want = rendered(t, ts, "landing", pages.LandingData{Banner: f.banner, Triggers: expectedRows(f, user)})
					}
					equal(t, w.Body.String(), want)
					require(t, len(seen) > 0, "banner not called")
					for _, u := range seen {
						equal(t, u, page.User{Email: "MiRa+tag@example.test", ProfileURL: "https://auth.sbx.example.test/", LogoutURL: "https://auth.sbx.example.test/logout"})
					}
				}
			}
		})
	}
}
func writeServices(t *testing.T, path, url string) {
	t.Helper()
	data := fmt.Sprintf(`{"services":[{"name":"auth","url":%q,"description":"auth","socket":"/unused","enabled":true,"mcp":false}]}`, url)
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
}
func TestBannerUserAndFreshServices(t *testing.T) {
	// R-DF6K-Z0D3 R-DGEH-CS3S R-DHMD-QJUH
	f := setup(t, false)
	cases := []struct{ host, proto, base string }{
		{"cron.sbx.example.test:443", "https", "https://auth.sbx.example.test"},
		{"cron.sbx.example.test", "", "https://auth.sbx.example.test"},
		{"cron.sbx.example.test", "HTTPS", "https://auth.sbx.example.test"},
		{"cron.sbx.example.test", "Http", "https://auth.sbx.example.test"},
		{"cron.sbx.example.test", "https, http", "https://auth.sbx.example.test"},
		{"sbx.example.test", "https", "https://auth.sbx.example.test"},
		{"cron.sbx.example.test", "http", "http://auth.sbx.example.test"},
		{"cron.", "http", "http://auth.cron."},
		{"cron.sbx.example.test:abc", "http", "http://auth.sbx.example.test:abc"},
		{"cron.sbx.example.test:", "http", "http://auth.sbx.example.test"},
	}
	var got page.User
	f.cfg.Banner = func(u page.User) page.Banner { got = u; return f.banner }
	for _, servicesMode := range []string{"missing", "empty-path", "malformed", "no-auth", "empty-url", "custom", "rewritten"} {
		f.cfg.ServicesPath = f.path
		switch servicesMode {
		case "empty-path":
			f.cfg.ServicesPath = ""
		case "malformed":
			if e := os.WriteFile(f.path, []byte("bad"), 0600); e != nil {
				t.Fatal(e)
			}
		case "no-auth":
			if e := os.WriteFile(f.path, []byte(`{"services":[]}`), 0600); e != nil {
				t.Fatal(e)
			}
		case "empty-url":
			writeServices(t, f.path, "")
		case "custom":
			writeServices(t, f.path, "http://accounts.example.test/prefix/")
		case "rewritten":
			writeServices(t, f.path, "https://replacement.example.test")
		}
		for _, c := range cases {
			for _, path := range []string{"/", "/about", "/nope"} {
				base := c.base
				if servicesMode == "custom" {
					base = "http://accounts.example.test/prefix/"
				}
				if servicesMode == "rewritten" {
					base = "https://replacement.example.test"
				}
				w := answer(f.cfg, request("GET", path, "caller", "Email<&@example.test", c.host, c.proto))
				require(t, w.Code == 200 || w.Code == 404, "page failed")
				equal(t, got, page.User{Email: "Email<&@example.test", ProfileURL: base + "/", LogoutURL: base + "/logout"})
			}
		}
	}
}
func TestRoutesMethodsFailuresAndReadOnly(t *testing.T) {
	// R-DOXS-16AN R-DQ5O-EY1C R-DRDK-SPS1 R-DSLH-6HIQ R-DTTD-K99F R-CRFL-L3AY R-CSNH-YV1N
	f := setup(t, true)
	ts := templates(t)
	before := append([]store.Trigger(nil), f.triggers...)
	next := map[string]time.Time{}
	has := map[string]bool{}
	for _, x := range before {
		next[x.ID], has[x.ID] = f.scheduler.Next(x.ID)
	}
	for _, path := range []string{"/", "/about", "/nope", "/hourly", "/hourly/", "/about/", "/mcp/", "/_appkit", "//"} {
		for _, method := range []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"} {
			var healthy *httptest.ResponseRecorder
			for _, failing := range []bool{false, true} {
				f.db.SetFailing(failing)
				w := answer(f.cfg, request(method, path+"?anything=yes", "viewer", "viewer@example.test", "cron.example.test", "https"))
				equal(t, w.Header().Values("Set-Cookie"), []string(nil))
				equal(t, w.Header().Values("Location"), []string(nil))
				switch {
				case path != "/" && path != "/about":
					equal(t, w.Code, 404)
					equal(t, w.Header().Values("Allow"), []string(nil))
					equal(t, w.Header().Values("Content-Type"), []string{"text/html; charset=utf-8"})
					want := rendered(t, ts, "notfound", pages.NoticeData{Banner: f.banner})
					if method == "HEAD" {
						want = ""
					}
					equal(t, w.Body.String(), want)
				case method != "GET" && method != "HEAD":
					equal(t, w.Code, 405)
					equal(t, w.Header().Values("Allow"), []string{"GET, HEAD"})
					equal(t, w.Body.String(), "")
				case path == "/" && failing:
					equal(t, w.Code, 503)
					equal(t, w.Header().Values("Content-Type"), []string{"text/plain; charset=utf-8"})
					want := store.Unreachable + "\n"
					if method == "HEAD" {
						want = ""
					}
					equal(t, w.Body.String(), want)
				default:
					equal(t, w.Code, 200)
					equal(t, w.Header().Values("Content-Type"), []string{"text/html; charset=utf-8"})
				}
				if healthy == nil {
					healthy = w
				} else if path != "/" || (method != "GET" && method != "HEAD") {
					equal(t, w.Code, healthy.Code)
					equal(t, w.Header(), healthy.Header())
					equal(t, w.Body.String(), healthy.Body.String())
				}
				if method == "HEAD" {
					get := answer(f.cfg, request("GET", path+"?anything=yes", "viewer", "viewer@example.test", "cron.example.test", "https"))
					equal(t, w.Code, get.Code)
					equal(t, w.Header(), get.Header())
					equal(t, w.Body.String(), "")
				}
			}
		}
	}
	f.db.SetFailing(false)
	after, err := f.store.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	equal(t, after, before)
	for _, x := range before {
		n, ok := f.scheduler.Next(x.ID)
		equal(t, n, next[x.ID])
		equal(t, ok, has[x.ID])
	}
}
func TestConcurrentPages(t *testing.T) {
	// R-DXH2-PKHI
	f := setup(t, true)
	h := identity.Require(pages.Handler(f.cfg))
	var wg sync.WaitGroup
	for i := range 24 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			path := "/"
			if i%2 == 0 {
				path = "/about"
			}
			w := httptest.NewRecorder()
			h.ServeHTTP(w, request("GET", path, "viewer", "viewer@example.test", "cron.example.test", "https"))
			if w.Code != 200 {
				t.Errorf("status %d", w.Code)
			}
		}()
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
		t.Fatal("concurrent page requests did not finish")
	}
}

type attribute struct {
	name, value string
	assigned    bool
}
type tag struct {
	name, raw, content string
	start, end         int
	attrs              []attribute
}

var starts = regexp.MustCompile(`(?i)<([a-z][a-z0-9-]*)([^a-z0-9>][^>]*)?>`)
var attributes = regexp.MustCompile(`^[ \t\r\n\f\v]+([^ \t\r\n\f\v"'<>/=]+)(?:="([^"]*)")?`)

func tags(s string) []tag {
	// R-DYOZ-3C87 R-DZWV-H3YW
	result := []tag{}
	for _, m := range starts.FindAllStringSubmatchIndex(s, -1) {
		x := tag{name: strings.ToLower(s[m[2]:m[3]]), raw: s[m[0]:m[1]], start: m[0], end: m[1]}
		rest := s[m[3]:m[1]]
		for {
			a := attributes.FindStringSubmatchIndex(rest)
			if a == nil {
				break
			}
			v := attribute{name: strings.ToLower(rest[a[2]:a[3]]), assigned: a[4] >= 0}
			if v.assigned {
				v.value = html.UnescapeString(rest[a[4]:a[5]])
			}
			x.attrs = append(x.attrs, v)
			rest = rest[a[1]:]
		}
		end := regexp.MustCompile(`(?i)</` + x.name + `(?:[^a-z0-9>][^>]*)?>`).FindStringIndex(s[x.end:])
		if end != nil {
			x.content = s[x.end : x.end+end[0]]
		}
		result = append(result, x)
	}
	return result
}
func (x tag) values(name string) []string {
	out := []string{}
	for _, a := range x.attrs {
		if a.name == name && a.assigned {
			out = append(out, a.value)
		}
	}
	return out
}
func (x tag) bare(name string) bool {
	for _, a := range x.attrs {
		if a.name == name && !a.assigned {
			return true
		}
	}
	return false
}
func (x tag) class(name string) bool {
	for _, v := range x.values("class") {
		for _, c := range strings.FieldsFunc(v, func(r rune) bool { return strings.ContainsRune(" \t\r\n\f\v", r) }) {
			if c == name {
				return true
			}
		}
	}
	return false
}
func named(s, name string) []tag {
	out := []tag{}
	for _, x := range tags(s) {
		if x.name == name {
			out = append(out, x)
		}
	}
	return out
}
func matching(s, attr, value string) []tag {
	out := []tag{}
	for _, x := range tags(s) {
		for _, v := range x.values(attr) {
			if v == value {
				out = append(out, x)
				break
			}
		}
	}
	return out
}
func one(t *testing.T, xs []tag, what string) tag {
	t.Helper()
	if len(xs) != 1 {
		t.Fatalf("%s: got %d tags; want one", what, len(xs))
	}
	return xs[0]
}
func id(t *testing.T, s, key, name string) tag {
	t.Helper()
	x := one(t, matching(s, "id", key), key)
	equal(t, x.name, name)
	return x
}
func normal(s string) string {
	// R-E14R-UVPL
	for {
		i := strings.IndexByte(s, '<')
		if i < 0 {
			break
		}
		j := strings.IndexByte(s[i:], '>')
		if j < 0 {
			s = s[:i]
			break
		}
		s = s[:i] + s[i+j+1:]
	}
	return strings.Join(strings.Fields(html.UnescapeString(s)), " ")
}
func visible(s string) string {
	b := named(s, "body")
	if len(b) == 0 {
		return ""
	}
	return normal(b[0].content)
}
func texts(xs []tag) []string {
	out := []string{}
	for _, x := range xs {
		out = append(out, normal(x.content))
	}
	return out
}
func written(t *testing.T, body string, b page.Banner) string {
	t.Helper()
	// R-E2CO-8NGA R-E60D-DYOD
	banner := rendered(t, page.Templates(), "banner", b)
	footer := rendered(t, page.Templates(), "footer", b)
	require(t, strings.Count(body, banner) == 1, "banner occurrence")
	require(t, strings.Count(body, footer) == 1, "footer occurrence")
	bodyTag := one(t, named(body, "body"), "body")
	bi := strings.Index(body, banner)
	fi := strings.Index(body, footer)
	end := strings.LastIndex(strings.ToLower(body), "</body>")
	require(t, bi >= bodyTag.end && strings.Trim(body[bodyTag.end:bi], " \t\r\n\f\v") == "", "banner must start body")
	require(t, fi >= bi+len(banner) && end >= fi+len(footer) && strings.Trim(body[fi+len(footer):end], " \t\r\n\f\v") == "", "footer must end body")
	return strings.Replace(strings.Replace(body, banner, "", 1), footer, "", 1)
}
func ownMarkup(t *testing.T, s string) {
	t.Helper()
	// R-D3ML-ESPW R-D8I6-XVOO
	equal(t, len(named(s, "style")), 0)
	for _, x := range tags(s) {
		equal(t, len(x.values("style")), 0)
		if x.name == "script" {
			equal(t, x.values("src"), []string{"/_appkit/feedback.js"})
			require(t, !x.bare("src"), "bare src")
			equal(t, len(x.values("href")), 0)
			equal(t, len(x.values("xlink:href")), 0)
		}
		if x.name != "a" {
			for _, key := range []string{"href", "src", "poster", "data", "background", "manifest"} {
				for _, v := range x.values(key) {
					require(t, v == "/" || (len(v) >= 2 && v[0] == '/' && v[1] != '/' && v[1] != '\\'), "nonlocal "+key+" "+v)
				}
			}
		}
	}
}
func plainMarkup(t *testing.T, s string) {
	t.Helper()
	// R-CWB7-469Q
	ownMarkup(t, s)
	for _, x := range tags(s) {
		for _, a := range []string{"srcset", "imagesrcset"} {
			equal(t, len(x.values(a)), 0)
		}
		if x.name == "meta" {
			equal(t, len(x.values("http-equiv")), 0)
		}
	}
}
func head(t *testing.T, s, title string) {
	t.Helper()
	// R-CTVE-CMSC R-E8G6-5I5R R-E9O2-J9WG R-EAVY-X1N5 R-EC3V-ATDU
	// R-D4UH-SKGL R-D62E-6C7A R-D7AA-K3XZ R-F0HU-Y87Q R-F1PR-BZYF
	body := one(t, named(s, "body"), "body")
	ttl := one(t, named(s, "title"), "title")
	equal(t, normal(ttl.content), title)
	require(t, ttl.end < body.start, "title not in head")
	titleEnd := regexp.MustCompile(`(?i)</title(?:[^a-z0-9>][^>]*)?>`).FindStringIndex(s[ttl.end:])
	require(t, titleEnd != nil && ttl.end+titleEnd[1] <= body.start, "title end not in head")
	equal(t, len(regexp.MustCompile(`(?i)</title(?:[^a-z0-9>][^>]*)?>`).FindAllString(s, -1)), 1)
	css := one(t, matching(s, "rel", "stylesheet"), "stylesheet")
	equal(t, css.name, "link")
	equal(t, css.values("href"), []string{"/_appkit/theme.css"})
	require(t, css.end < body.start, "stylesheet not in head")
	viewport := one(t, matching(s, "name", "viewport"), "viewport")
	equal(t, viewport.name, "meta")
	equal(t, viewport.values("content"), []string{"width=device-width, initial-scale=1"})
	require(t, viewport.end < body.start, "viewport not in head")
	script := one(t, matching(s, "src", "/_appkit/feedback.js"), "feedback")
	equal(t, script.name, "script")
	require(t, script.bare("defer") || len(script.values("defer")) > 0, "missing defer")
	require(t, script.end < body.start, "feedback not in head")
	icon := one(t, matching(s, "rel", "icon"), "icon")
	equal(t, icon.name, "link")
	equal(t, icon.values("href"), []string{"/_appkit/favicon.svg"})
	equal(t, icon.values("type"), []string{"image/svg+xml"})
	require(t, icon.end < body.start, "icon not in head")
	preload := one(t, matching(s, "rel", "preload"), "preload")
	equal(t, preload.name, "link")
	equal(t, preload.values("as"), []string{"font"})
	equal(t, preload.values("type"), []string{"font/woff2"})
	equal(t, preload.values("href"), []string{page.PreloadURL()})
	require(t, preload.bare("crossorigin") || reflect.DeepEqual(preload.values("crossorigin"), []string{""}), "crossorigin")
	require(t, preload.end < body.start, "preload not in head")
}
func TestPlainPageHeadsAndChrome(t *testing.T) {
	// R-E4SH-06XO R-CV3A-QEJ1 R-VLVE-ADI5
	f := setup(t, false)
	for _, path := range []string{"/", "/about"} {
		w := answer(f.cfg, request("GET", path, "viewer", "viewer@example.test", "cron.example.test", "https"))
		s := written(t, w.Body.String(), f.banner)
		title := "cron"
		if path == "/about" {
			title = "About cron"
		}
		head(t, s, title)
		plainMarkup(t, s)
		equal(t, texts(named(s, "h1")), []string{title})
		for _, x := range named(s, "nav") {
			require(t, !x.class("crumbs"), "breadcrumb")
		}
	}
}
func TestLauncherPresence(t *testing.T) {
	// R-ZYI0-YPM4
	f := setup(t, false)
	services := []struct {
		name    string
		entries []page.Service
	}{
		{"nil", nil},
		{"empty", []page.Service{}},
		{"one", []page.Service{{Name: "cron", URL: "https://cron.example.test", Icon: template.HTML("icon"), Enabled: true, Current: true}}},
		{"several", []page.Service{{Name: "auth", URL: "https://auth.example.test", Enabled: true}, {Name: "cron", URL: "https://cron.example.test", Icon: template.HTML("&amp; icon"), Current: true}}},
	}
	for _, entries := range services {
		for _, icon := range []template.HTML{"", "icon", "&amp; icon"} {
			for _, path := range []string{"/", "/about"} {
				t.Run(fmt.Sprintf("%s/%s/%s", entries.name, icon, path), func(t *testing.T) {
					f.banner.Services = entries.entries
					f.banner.Icon = icon
					w := answer(f.cfg, request("GET", path, "viewer", "viewer@example.test", "cron.example.test", "https"))
					equal(t, w.Code, http.StatusOK)
					body := w.Body.String()
					buttons := []tag{}
					for _, x := range named(body, "button") {
						if x.class("launcher") {
							buttons = append(buttons, x)
						}
					}
					count := 0
					if len(entries.entries) != 0 {
						count = 1
					}
					equal(t, len(buttons), count)
					launcherScripts := 0
					for _, x := range named(body, "script") {
						v := x.values("src")
						launcher := false
						feedback := false
						for _, src := range v {
							launcher = launcher || src == "/_appkit/launcher.js"
							feedback = feedback || src == "/_appkit/feedback.js"
						}
						switch {
						case count == 1 && launcher:
							launcherScripts++
						case count == 1:
							require(t, feedback, "non-launcher script must load feedback")
						default:
							equal(t, v, []string{"/_appkit/feedback.js"})
							require(t, !x.bare("src"), "other src occurrence without services")
						}
					}
					equal(t, launcherScripts, count)
					if count == 0 {
						equal(t, len(named(body, "input")), 0)
					}
				})
			}
		}
	}
}
func TestLandingEmptyStateAndTools(t *testing.T) {
	// R-EFRK-G4LX R-EGZG-TWCM R-EI7D-7O3B R-EPIR-IAJH R-EQQN-W2A6 R-ERYK-9U0V
	f := setup(t, false)
	body := answer(f.cfg, request("GET", "/", "viewer", "viewer@example.test", "cron.example.test", "https")).Body.String()
	s := written(t, body, f.banner)
	summary := id(t, s, "summary", "p")
	equal(t, normal(summary.content), "Triggers that emit events on the suite's event bus on a schedule.")
	hs := named(s, "h2")
	equal(t, texts(hs), []string{"Triggers", "MCP tools"})
	section := id(t, s, "triggers", "section")
	equal(t, texts(named(section.content, "h2")), []string{"Triggers"})
	empty := id(t, s, "no-triggers", "div")
	require(t, empty.start > hs[0].start && strings.Contains(section.content, empty.raw), "empty state outside triggers")
	equal(t, texts(named(empty.content, "h3")), []string{"No triggers yet"})
	ps := named(empty.content, "p")
	equal(t, texts(ps), []string{"A trigger an agent creates with the create tool shows up here."})
	equal(t, texts(named(ps[0].content, "code")), []string{"create"})
	equal(t, len(matching(s, "id", "trigger-list")), 0)
	equal(t, len(named(s, "table")), 0)
	for _, x := range tags(s) {
		equal(t, len(x.values("data-trigger")), 0)
	}
	checkTools(t, s)
	link := id(t, s, "about-link", "a")
	equal(t, link.values("href"), []string{"/about"})
	equal(t, normal(link.content), "About cron")
	v := visible(s)
	for _, needle := range []string{"cron", "Triggers that emit events on the suite's event bus on a schedule.", "Triggers", "MCP tools", "Agents manage triggers with these tools, through the MCP gateway's call and mutate.", "list", "Every trigger in the space, by slug.", "delete", "Delete a trigger you own.", "About cron"} {
		i := strings.Index(v, needle)
		require(t, i >= 0, "missing ordered text "+needle)
		v = v[i+len(needle):]
	}
	footer := rendered(t, page.Templates(), "footer", f.banner)
	require(t, strings.HasSuffix(visible(body), normal(footer)), "footer visible text not at end")
}
func checkTools(t *testing.T, s string) {
	t.Helper()
	dl := id(t, s, "tools", "dl")
	dts, dds := named(dl.content, "dt"), named(dl.content, "dd")
	equal(t, len(dts), 7)
	equal(t, len(dds), 7)
	names := []string{"list", "show", "create", "update", "pause", "resume", "delete"}
	descs := []string{"Every trigger in the space, by slug.", "One trigger, with its schedule, its owner, and when it last fired and fires next.", "Create a trigger that emits an event on a schedule.", "Change the schedule of a trigger you own.", "Stop a trigger you own from firing.", "Start a paused trigger you own firing again, from its next slot.", "Delete a trigger you own."}
	for i, x := range dts {
		equal(t, x.values("data-tool"), []string{names[i]})
		equal(t, texts(named(x.content, "code")), []string{names[i]})
		equal(t, normal(x.content), names[i])
		equal(t, normal(dds[i].content), descs[i])
		require(t, x.start < dds[i].start, "dt not followed by dd")
		if i+1 < len(dts) {
			require(t, dds[i].start < dts[i+1].start, "dd not before next dt")
		}
	}
}
func TestLandingRowsOwnershipTimesAndPrivateData(t *testing.T) {
	// R-EJF9-LFU0 R-EKN5-Z7KP R-ELV2-CZBE R-EOAV-4ISS R-ET6G-NLRK
	f := setup(t, true)
	for _, user := range []string{"u_7f3a9c21", "u_2b8e1d04", "viewer"} {
		body := answer(f.cfg, request("GET", "/", user, "viewer@example.test", "cron.example.test", "https")).Body.String()
		s := written(t, body, f.banner)
		expected := expectedRows(f, user)
		equal(t, len(matching(s, "id", "no-triggers")), 0)
		section := id(t, s, "triggers", "section")
		table := id(t, s, "trigger-list", "table")
		require(t, strings.Contains(section.content, table.raw), "table outside section")
		equal(t, texts(named(table.content, "th")), []string{"ID", "Slug", "When", "Owner", "Status", "Last fired", "Next"})
		rows := []tag{}
		for _, x := range tags(s) {
			if len(x.values("data-trigger")) > 0 {
				rows = append(rows, x)
			}
		}
		equal(t, len(rows), len(expected))
		mines := 0
		for i, x := range rows {
			r := expected[i]
			equal(t, x.name, "tr")
			equal(t, x.values("data-trigger"), []string{r.Slug})
			require(t, strings.Contains(table.content, x.raw), "row outside table")
			cells := named(x.content, "td")
			equal(t, len(cells), 7)
			for j, c := range []string{"trigger-id", "trigger-slug", "trigger-when", "trigger-owner", "trigger-status", "trigger-last", "trigger-next"} {
				require(t, cells[j].class(c), "wrong td class "+c)
			}
			equal(t, texts(cells[:3]), []string{r.ID, r.Slug, r.When})
			status := one(t, named(cells[4].content, "span"), "status")
			require(t, status.class("status"), "status class")
			equal(t, status.values("data-status"), []string{r.Status})
			equal(t, normal(status.content), r.Status)
			badges := named(cells[3].content, "span")
			owner := r.Owner
			if r.Mine {
				mines++
				badge := one(t, badges, "mine")
				require(t, badge.class("badge"), "badge class")
				equal(t, badge.values("data-kind"), []string{"mine"})
				equal(t, normal(badge.content), "yours")
				owner += " yours"
			} else {
				equal(t, len(badges), 0)
			}
			equal(t, normal(cells[3].content), owner)
			for j, pair := range [][2]string{{r.LastFired, r.LastFiredText}, {r.Next, r.NextText}} {
				cell := cells[5+j]
				times := named(cell.content, "time")
				equal(t, normal(cell.content), pair[1])
				if pair[0] == "" {
					equal(t, len(times), 0)
				} else {
					tm := one(t, times, "time")
					equal(t, tm.values("datetime"), []string{pair[0]})
					equal(t, normal(tm.content), pair[1])
				}
			}
		}
		equal(t, len(matching(s, "data-kind", "mine")), mines)
		for _, x := range f.triggers {
			require(t, !strings.Contains(body, x.OwnerID), "owner id exposed")
			require(t, !strings.Contains(body, x.Created.UTC().Format(time.RFC3339)), "creation stamp exposed")
			require(t, !strings.Contains(body, x.Created.UTC().Format("2006-01-02 15:04")), "creation text exposed")
		}
	}
}
func TestAboutHooksAndEmptyVersion(t *testing.T) {
	// R-EUED-1DI9 R-CYQZ-VPR4
	f := setup(t, false)
	for _, version := range []string{"test-display", ""} {
		f.banner.Version = version
		body := answer(f.cfg, request("GET", "/about", "viewer", "viewer@example.test", "cron.example.test", "https")).Body.String()
		s := written(t, body, f.banner)
		dl := id(t, s, "about", "dl")
		dts, dds := named(dl.content, "dt"), named(dl.content, "dd")
		equal(t, texts(dts), []string{"Name", "Version", "Description"})
		equal(t, texts(dds), []string{f.banner.Service, version, pages.Description})
		for i, key := range []string{"about-name", "about-version", "about-description"} {
			equal(t, dds[i].values("id"), []string{key})
			require(t, dts[i].start < dds[i].start, "about dt/dd order")
			if i < 2 {
				require(t, dds[i].start < dts[i+1].start, "about dd/dt order")
			}
		}
		link := id(t, s, "home-link", "a")
		equal(t, link.values("href"), []string{"/"})
		equal(t, normal(link.content), "Back to cron")
		equal(t, len(named(s, "h2")), 0)
		equal(t, len(named(s, "table")), 0)
		for _, key := range []string{"summary", "triggers", "trigger-list", "no-triggers", "tools", "about-link"} {
			equal(t, len(matching(s, "id", key)), 0)
		}
	}
}
func TestNoticeHooksAndFooter(t *testing.T) {
	// R-CZYW-9HHT R-D16S-N98I R-D2EP-10Z7
	s := loaded(t)
	for _, b := range []page.Banner{{Service: "cron", Version: "test-display"}, {Service: "cron"}} {
		w := httptest.NewRecorder()
		s.Write(w, httptest.NewRequest("GET", "/nope", nil), 404, "notfound", pages.NoticeData{Banner: b})
		body := w.Body.String()
		equal(t, w.Code, 404)
		head(t, body, "Not found")
		ownMarkup(t, body)
		footer := rendered(t, page.Templates(), "footer", b)
		require(t, strings.Count(body, footer) == 1, "notice footer count")
		i := strings.Index(body, footer)
		end := strings.LastIndex(strings.ToLower(body), "</body>")
		require(t, end >= i+len(footer) && strings.Trim(body[i+len(footer):end], " \t\r\n\f\v") == "", "footer not at body end")
		for _, name := range []string{"header", "form", "button"} {
			equal(t, len(named(body, name)), 0)
		}
		for _, x := range named(body, "strong") {
			require(t, !x.class("mark"), "notice mark")
		}
		for _, x := range named(body, "a") {
			require(t, !x.class("profile"), "notice profile")
		}
		equal(t, texts(named(body, "h1")), []string{"Not found"})
		p := id(t, body, "notfound", "p")
		equal(t, normal(p.content), "There is nothing at this address.")
	}
}
