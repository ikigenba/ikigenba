package pages_test

import (
	"bytes"
	"context"
	"encoding/json"
	"html"
	"html/template"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
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
func text(s string) string {
	return strings.Join(strings.Fields(html.UnescapeString(regexp.MustCompile(`<[^>]*>`).ReplaceAllString(s, ""))), " ")
}
func element(t *testing.T, body, tag, attrs string) string {
	t.Helper()
	r := regexp.MustCompile(`(?s)<` + tag + `(?: ` + attrs + `)?>(.*?)</` + tag + `>`)
	m := r.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("missing %s %s", tag, attrs)
	}
	return text(m[1])
}
func contents(t *testing.T, body, tag, attribute string) string {
	t.Helper()
	pattern := `(?s)<` + tag + `\b[^>]*`
	if attribute != "" {
		pattern += regexp.QuoteMeta(attribute) + `[^>]*`
	}
	matches := regexp.MustCompile(pattern+`>(.*?)</`+tag+`>`).FindAllStringSubmatch(body, -1)
	if len(matches) != 1 {
		t.Fatalf("wanted one %s %s, got %d", tag, attribute, len(matches))
	}
	return matches[0][1]
}
func sharedAssets(t *testing.T, body string, launcher bool) {
	t.Helper()
	counts := map[string]int{}
	attributes := regexp.MustCompile(`([a-zA-Z][a-zA-Z0-9_-]*)(?:\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s>]+)))?`)
	for _, tag := range regexp.MustCompile(`<(link|meta|script|img)\b([^>]*)>`).FindAllStringSubmatch(body, -1) {
		attrs := map[string]string{}
		for _, a := range attributes.FindAllStringSubmatch(tag[2], -1) {
			attrs[a[1]] = html.UnescapeString(a[2] + a[3] + a[4])
		}
		switch tag[1] {
		case "link":
			if attrs["rel"] == "stylesheet" {
				counts["stylesheet"]++
				if attrs["href"] != "/_appkit/theme.css" {
					t.Fatal("stylesheet", tag[0])
				}
			}
			if attrs["rel"] == "icon" {
				counts["icon"]++
				if attrs["href"] != "/_appkit/favicon.svg" || attrs["type"] != "image/svg+xml" {
					t.Fatal("favicon", tag[0])
				}
			}
		case "meta":
			if attrs["name"] == "viewport" {
				counts["viewport"]++
				if attrs["content"] != "width=device-width, initial-scale=1" {
					t.Fatal("viewport", tag[0])
				}
			}
		case "script":
			switch attrs["src"] {
			case "/_appkit/feedback.js":
				counts["feedback"]++
				if _, ok := attrs["defer"]; !ok {
					t.Fatal("feedback without defer", tag[0])
				}
			case "/_appkit/launcher.js":
				counts["launcher"]++
			default:
				t.Fatal("unexpected script", tag[0])
			}
		}
		assetAttr := "src"
		if tag[1] == "link" {
			assetAttr = "href"
		}
		if tag[1] != "meta" {
			if value, ok := attrs[assetAttr]; ok && !strings.HasPrefix(value, "/_appkit/") {
				t.Fatal("external asset", tag[0])
			}
		}
	}
	wantLauncher := 0
	if launcher {
		wantLauncher = 1
	}
	for key, want := range map[string]int{"stylesheet": 1, "icon": 1, "viewport": 1, "feedback": 1, "launcher": wantLauncher} {
		if counts[key] != want {
			t.Fatalf("%s count: got %d, want %d", key, counts[key], want)
		}
	}
}

// R-5IRX-7KAO
func TestSharedPageAssets(t *testing.T) {
	d, st, _ := fixture(t)
	path := filepath.Join(t.TempDir(), "services.json")
	p := pages.New(pages.Config{ServicesPath: path, Store: st, Version: "test"})
	// Launcher icon markup is verbatim and excluded from the page's asset contract.
	icon := `<svg><link rel="icon" href="https://icon.test/favicon.svg"><link rel="stylesheet" href="https://icon.test/style.css"><meta name="viewport" content="icon"><script src="https://icon.test/script.js"></script><img src="https://icon.test/image.svg"></svg>`
	encodedIcon, err := json.Marshal(icon)
	must(t, err)
	for _, services := range []struct {
		name, data string
		launcher   bool
	}{
		{"empty", `{"services":[]}`, false},
		{"no-icons", `{"services":[{"name":"auth","url":"https://auth.test","description":"auth","socket":"","mcp":false,"enabled":true}]}`, false},
		{"icons", `{"services":[{"name":"events","url":"https://events.test","description":"events","socket":"","mcp":true,"enabled":true,"icon":` + string(encodedIcon) + `},{"name":"other","url":"https://other.test","description":"other","socket":"","mcp":false,"enabled":false,"icon":` + string(encodedIcon) + `}]}`, true},
	} {
		t.Run(services.name, func(t *testing.T) {
			must(t, os.WriteFile(path, []byte(services.data), 0600))
			for _, failing := range []bool{false, true} {
				d.SetFailing(failing)
				for _, method := range []string{"GET", "POST", "DELETE"} {
					for _, route := range []struct {
						name    string
						handler func(http.ResponseWriter, *http.Request)
					}{{"landing", p.Landing}, {"about", p.About}, {"notfound", p.NotFound}} {
						t.Run(route.name+"/"+strconv.FormatBool(failing)+"/"+method, func(t *testing.T) {
							out := invoke(route.handler, method)
							body := out.Body.String()
							if services.launcher && out.Code == 200 && !strings.Contains(body, icon) {
								t.Fatal("launcher icon fixture was not inserted")
							}
							sharedAssets(t, strings.ReplaceAll(body, icon, ""), services.launcher && out.Code == 200)
						})
					}
				}
			}
		})
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// R-QH65-RWB2 R-QIE2-5O1R R-QJLY-JFSG R-QKTU-X7J5 R-QM1R-AZ9U R-QN9N-OR0J R-QOHK-2IR8 R-QPPG-GAHX R-QQXC-U28M R-QZGN-IGFH R-R0OJ-W866 R-R1WG-9ZWV R-R34C-NRNK R-R4C9-1JE9 R-R5K5-FB4Y R-R6S1-T2VN R-R7ZY-6UMC
func TestExactTemplateAnswers(t *testing.T) {
	d, st, now := fixture(t)
	ctx := context.Background()
	must(t, st.Declare(ctx, "repos", store.Declaration{Emits: []appEvents.Emission{{Event: "repo.pushed"}}}))
	for _, service := range []string{"scripts", "sites", "gone"} {
		must(t, st.Declare(ctx, service, store.Declaration{Accepts: []string{"repo.pushed"}}))
	}
	must(t, st.Deliver(ctx, appEvents.Event{ID: "evt_0000000000000001", Time: now, Service: "repos", Name: "repo.pushed", RequestID: "request", User: "user", Attrs: appEvents.Attrs{}}))
	must(t, st.Pause(ctx, "sites", 1, "publish <failed>&"))
	must(t, st.Forget(ctx, "gone"))
	p := pages.New(pages.Config{Store: st, Version: "test-build"})
	var _ web.Pages = p
	b := page.Banner{Service: appEvents.ServiceName, Version: "test-build", Email: "<person>&@example.test", ProfileURL: "https://auth.space.test/", LogoutURL: "https://auth.space.test/logout"}
	subs, err := st.Subscribers(ctx)
	must(t, err)
	rows := []pages.SubscriberRow{}
	for _, s := range subs {
		kind := map[store.Status]string{store.StatusOK: "ok", store.StatusPaused: "warn", store.StatusGone: "info"}[s.Status]
		reason := ""
		if s.Reason != nil {
			reason = "at " + s.Reason.Name + " " + strconv.FormatInt(s.Reason.Seq, 10) + ": " + s.Reason.Error
		}
		rows = append(rows, pages.SubscriberRow{Service: s.Service, Status: string(s.Status), Kind: kind, Reason: reason, Cursor: strconv.FormatInt(s.Cursor, 10), Lag: strconv.FormatInt(s.Lag, 10), Since: s.Since})
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
	if pages.Description != "The suite's internal event bus" {
		t.Fatal(pages.Description)
	}
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
	nilStore := pages.New(pages.Config{Version: "test-build"})
	check(t, invoke(nilStore.About, "GET"), 200, expected(t, "about", pages.AboutData{Banner: b, Description: pages.Description}))
	check(t, invoke(nilStore.NotFound, "POST"), 404, expected(t, "notfound", notice))
}

// R-QS59-7TZB R-MZMS-U7R3 R-RCVJ-PXL4 R-MVZ3-OWJ0 R-RGJ8-V8T7 R-RIZ1-MSAL R-RK6Y-0K1A
func TestVisibleHooks(t *testing.T) {
	d, st, _ := fixture(t)
	p := pages.New(pages.Config{Store: st, Version: "test-build"})
	body := invoke(p.Landing, "GET").Body.String()
	for _, tc := range []struct{ tag, attrs, want string }{{"title", "", "events"}, {"h1", "", "events"}, {"p", `id="summary" class="lede"`, "The suite's internal event bus: services emit events here, and each event is delivered, in order, to every service that accepts it."}, {"h2", `id="subscribers-title"`, "Subscribers"}, {"h3", `class="text-md"`, "No subscribers yet"}, {"a", `id="about-link" href="/about"`, "About events"}, {"footer", `class="footer"`, "events test-build"}} {
		if tc.tag == "footer" {
			if text(contents(t, body, "footer", "")) != "events test-build" {
				t.Fatal(body)
			}
			continue
		}
		if got := element(t, body, tc.tag, tc.attrs); got != tc.want {
			t.Fatal(got, tc.want)
		}
	}
	if !strings.Contains(body, `id="no-subscribers"`) || strings.Contains(body, `id="subscriber-list"`) || !strings.Contains(body, "A service that accepts events shows up here once it declares an event it accepts.") {
		t.Fatal(body)
	}
	subscribersSection := contents(t, body, "section", `id="subscribers"`)
	empty := contents(t, subscribersSection, "div", `id="no-subscribers"`)
	if !strings.Contains(text(empty), "No subscribers yet A service that accepts events shows up here once it declares an event it accepts.") || strings.Index(subscribersSection, `</h2>`) > strings.Index(subscribersSection, `id="no-subscribers"`) {
		t.Fatal(subscribersSection)
	}
	if !strings.Contains(text(subscribersSection), "Each service that accepts events, where it is in the log, and how far behind.") {
		t.Fatal(subscribersSection)
	}
	toolsSection := contents(t, body, "section", `aria-labelledby="tools-title"`)
	if element(t, toolsSection, "h2", `id="tools-title"`) != "MCP tools" || !strings.Contains(text(toolsSection), "Agents inspect the bus and unstick subscribers with these tools, through the MCP gateway's call and mutate.") {
		t.Fatal(toolsSection)
	}
	codes := regexp.MustCompile(`<code>(.*?)</code>`).FindAllStringSubmatch(contents(t, toolsSection, "header", ""), -1)
	if len(codes) != 2 || text(codes[0][1]) != "call" || text(codes[1][1]) != "mutate" {
		t.Fatal(codes)
	}
	toolsList := contents(t, toolsSection, "dl", `id="tools"`)
	names := []string{"catalog", "search", "subscribers", "skip", "resume"}
	descriptions := []string{"Every event the suite emits, who emits and accepts it, counts and last seen.", "The retained log, newest first, filtered by service, event, user, request id, cause or attributes.", "Each subscriber's status, reason, cursor and lag.", "Skip the event a paused subscriber is stuck on and resume it.", "Retry the event a paused subscriber is stuck on."}
	matches := regexp.MustCompile(`(?s)<dt data-tool="([^"]+)"><code>([^<]+)</code></dt><dd>(.*?)</dd>`).FindAllStringSubmatch(toolsList, -1)
	if len(matches) != 5 {
		t.Fatal(matches)
	}
	for i, m := range matches {
		if m[1] != names[i] || m[2] != names[i] || text(m[3]) != descriptions[i] {
			t.Fatal(m)
		}
	}
	ctx := context.Background()
	must(t, st.Declare(ctx, "repos", store.Declaration{Emits: []appEvents.Emission{{Event: "repo.pushed"}}}))
	for _, s := range []string{"ok", "paused", "gone"} {
		must(t, st.Declare(ctx, s, store.Declaration{Accepts: []string{"repo.pushed"}}))
	}
	must(t, st.Deliver(ctx, appEvents.Event{ID: "evt_0000000000000001", Time: time.Unix(100, 0), Service: "repos", Name: "repo.pushed", Attrs: appEvents.Attrs{}}))
	must(t, st.Pause(ctx, "paused", 1, "bad <commit>&"))
	must(t, st.Forget(ctx, "gone"))
	body = invoke(p.Landing, "GET").Body.String()
	subs, err := st.Subscribers(ctx)
	must(t, err)
	table := contents(t, contents(t, body, "section", `id="subscribers"`), "table", `id="subscriber-list"`)
	rows := regexp.MustCompile(`(?s)<tr data-subscriber="([^"]+)">(.*?)</tr>`).FindAllStringSubmatch(table, -1)
	if len(rows) != len(subs) || strings.Contains(body, `id="no-subscribers"`) {
		t.Fatal(rows)
	}
	headers := regexp.MustCompile(`<th>(.*?)</th>`).FindAllStringSubmatch(table, -1)
	if len(headers) != 5 {
		t.Fatal(headers)
	}
	for i, s := range []string{"Service", "Status", "Cursor", "Lag", "Since"} {
		if text(headers[i][1]) != s {
			t.Fatal(headers)
		}
	}
	for i, m := range rows {
		s := subs[i]
		if m[1] != s.Service || element(t, m[2], "td", `class="subscriber-service"`) != s.Service || element(t, m[2], "td", `class="subscriber-cursor"`) != strconv.FormatInt(s.Cursor, 10) || element(t, m[2], "td", `class="subscriber-lag"`) != strconv.FormatInt(s.Lag, 10) {
			t.Fatal(m)
		}
		kind := map[store.Status]string{store.StatusOK: "ok", store.StatusPaused: "warn", store.StatusGone: "info"}[s.Status]
		statusCell := contents(t, m[2], "td", `class="subscriber-status"`)
		sinceCell := contents(t, m[2], "td", `class="subscriber-since"`)
		if element(t, statusCell, "span", `class="status" data-status="`+string(s.Status)+`" data-kind="`+kind+`"`) != string(s.Status) {
			t.Fatal(m)
		}
		if element(t, sinceCell, "time", `datetime="`+s.Since.UTC().Format(time.RFC3339)+`"`) != s.Since.UTC().Format("2006-01-02 15:04") {
			t.Fatal(m)
		}
		if s.Reason != nil {
			if element(t, statusCell, "span", `class="muted"`) != "at repo.pushed 1: bad <commit>&" {
				t.Fatal(m)
			}
		} else if strings.Contains(statusCell, `class="muted"`) {
			t.Fatal(m)
		}
	}
	about := invoke(p.About, "GET").Body.String()
	aboutList := contents(t, about, "dl", `id="about"`)
	facts := regexp.MustCompile(`(?s)<(dt|dd)\b[^>]*>(.*?)</(?:dt|dd)>`).FindAllStringSubmatch(aboutList, -1)
	if len(facts) != 6 {
		t.Fatal(facts)
	}
	for i, want := range []string{"Name", "events", "Version", "test-build", "Description", pages.Description} {
		kind := "dt"
		if i%2 == 1 {
			kind = "dd"
		}
		if facts[i][1] != kind || text(facts[i][2]) != want {
			t.Fatal(facts)
		}
	}
	remaining := regexp.MustCompile(`(?s)<(?:dt|dd)\b[^>]*>.*?</(?:dt|dd)>`).ReplaceAllString(aboutList, "")
	if strings.TrimSpace(remaining) != "" {
		t.Fatal("extra about content", remaining)
	}
	if strings.Count(aboutList, "<") != 12 {
		t.Fatal("unexpected about facts", aboutList)
	}
	for _, tc := range []struct{ tag, attrs, want string }{{"title", "", "About events"}, {"h1", "", "About events"}, {"dd", `id="about-name"`, "events"}, {"dd", `id="about-version"`, "test-build"}, {"dd", `id="about-description"`, pages.Description}, {"a", `id="home-link" href="/"`, "Back to events"}} {
		if element(t, about, tc.tag, tc.attrs) != tc.want {
			t.Fatal(tc)
		}
	}
	for _, needle := range []string{`id="subscribers"`, `id="tools"`} {
		if strings.Contains(about, needle) {
			t.Fatal(needle)
		}
	}
	d.SetFailing(true)
	unavailable := invoke(p.Landing, "GET").Body.String()
	missing := invoke(p.NotFound, "DELETE").Body.String()
	for _, tc := range []struct{ body, title, id, words string }{{missing, "Not found", "notfound", "There is nothing at this address."}, {unavailable, "Page unavailable", "unavailable", "This page is not available right now. Try again in a moment."}} {
		if element(t, tc.body, "title", "") != tc.title || element(t, tc.body, "h1", "") != tc.title || element(t, tc.body, "p", `id="`+tc.id+`" class="lede"`) != tc.words {
			t.Fatal(tc)
		}
		for _, attr := range regexp.MustCompile(`\bclass="([^"]*)"`).FindAllStringSubmatch(tc.body, -1) {
			for _, class := range strings.Fields(html.UnescapeString(attr[1])) {
				if class == "mark" {
					t.Fatal("notice contains mark class", attr)
				}
			}
		}
		for _, bad := range []string{"<header", `class="mark"`, "<form", `id="subscribers"`, `id="tools"`} {
			if strings.Contains(tc.body, bad) {
				t.Fatal(bad)
			}
		}
	}
	for _, b := range []string{body, about, missing, unavailable} {
		if text(contents(t, b, "footer", "")) != "events test-build" {
			t.Fatal("footer", b)
		}
		if len(regexp.MustCompile(`<h1\b`).FindAllString(b, -1)) != 1 {
			t.Fatal("h1 count", b)
		}
		sharedAssets(t, b, false)
	}
}

// R-QTD5-LLQ0 R-QUL1-ZDGP R-QX0U-QWY3 R-QY8R-4OOS R-R97U-KMD1
func TestBannerRefreshAndFallback(t *testing.T) {
	_, st, _ := fixture(t)
	path := filepath.Join(t.TempDir(), "services.json")
	p := pages.New(pages.Config{ServicesPath: path, Store: st, Version: "test"})
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
		value := `{"services":[{"name":"other","url":"https://other.test","description":"other","socket":"","enabled":` + strconv.FormatBool(enabled) + `,"mcp":false,"icon":"<svg></svg>"},{"name":"auth","url":"https://accounts.test","description":"auth","socket":"","enabled":true,"mcp":false},{"name":"events","url":"https://events.test","description":"events","socket":"","enabled":true,"mcp":true,"icon":"<svg></svg>"}]}`
		must(t, os.WriteFile(path, []byte(value), 0600))
	}
	write(true)
	before := invoke(p.Landing, "GET").Body.String()
	if !strings.Contains(before, `href="https://other.test"`) || !strings.Contains(before, `href="https://accounts.test/"`) || strings.Count(before, `src="/_appkit/launcher.js"`) != 1 {
		t.Fatal(before)
	}
	sharedAssets(t, before, true)
	write(false)
	after := invoke(p.Landing, "GET").Body.String()
	if strings.Contains(after, `href="https://other.test"`) || !strings.Contains(after, `title="other is unavailable"`) {
		t.Fatal(after)
	}
	b := page.Banner{Service: "events", Version: "test", Email: "<person>&@example.test", ProfileURL: "https://accounts.test/", LogoutURL: "https://accounts.test/logout", Services: []page.Service{{Name: "other", URL: "https://other.test", Icon: template.HTML("<svg></svg>"), Enabled: false}, {Name: "events", URL: "https://events.test", Icon: template.HTML("<svg></svg>"), Enabled: true, Current: true}}}
	check(t, invoke(p.About, "GET"), 200, expected(t, "about", pages.AboutData{Banner: b, Description: pages.Description}))
	must(t, os.WriteFile(path, []byte(`broken`), 0600))
	if strings.Contains(invoke(p.About, "GET").Body.String(), `src="/_appkit/launcher.js"`) {
		t.Fatal("broken services launcher")
	}
}

// R-RAFQ-YE3Q
func TestConcurrentPages(t *testing.T) {
	_, st, _ := fixture(t)
	p := pages.New(pages.Config{Store: st, Version: "test"})
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
