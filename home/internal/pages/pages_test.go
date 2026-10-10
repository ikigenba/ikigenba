package pages_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
	"unicode"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/home"
	"github.com/ikigenba/ikigenba/home/internal/pages"
)

func templateSet(t *testing.T) *template.Template {
	t.Helper()
	set, err := page.Templates().ParseFS(home.Assets(), "*.html")
	if err != nil {
		t.Fatal(err)
	}
	return set
}
func execute(t *testing.T, name string, data any) string {
	t.Helper()
	var b bytes.Buffer
	if err := templateSet(t).ExecuteTemplate(&b, name, data); err != nil {
		t.Fatal(err)
	}
	return b.String()
}
func writer(t *testing.T, sink telemetry.Sink, stderr *bytes.Buffer) *telemetry.Writer {
	t.Helper()
	w := telemetry.New(telemetry.Config{Service: pages.ServiceName, Sink: sink, Stderr: stderr, Now: func() time.Time { return time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC) }, Rand: bytes.NewReader(bytes.Repeat([]byte{7}, 16384)), Sleep: func(context.Context, time.Duration) {}})
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		w.Shutdown(ctx, "test end")
	})
	return w
}
func flush(t *testing.T, w *telemetry.Writer) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := w.Flush(ctx); err != nil {
		t.Fatal(err)
	}
}
func request(method, path string) *http.Request {
	r := httptest.NewRequest(method, "https://home.example.test"+path, strings.NewReader("test body"))
	r.Header.Set("X-User-Id", "caller")
	r.Header.Set("X-User-Email", "caller@example.test")
	r.Header.Set("X-Request-Id", "request-value")
	r.Header.Set("X-Forwarded-Proto", "https")
	return r
}
func answer(h http.Handler, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func banner() page.Banner {
	return page.Banner{Service: pages.ServiceName, Release: "test-release", Commit: "test-commit", Email: "fixed@example.test", ProfileURL: "https://profile.example.test/", LogoutURL: "https://profile.example.test/logout", Home: "https://front.example.test/", Tools: true, Trail: []page.Level{{Name: "injected", URL: "/injected"}}}
}

// R-65FF-ATAP R-5Y41-06UJ R-DJLZ-6UD8 R-DKTV-KM3X R-5QSM-PKED R-67V8-2CS3
// R-5VO8-8ND5 R-60JT-RQBX R-6CQT-LFQV R-6DYP-Z7HK R-5ZBX-DYL8
func TestPublicDataAndTemplates(t *testing.T) {
	t.Setenv(services.Variable, "")
	const serviceName = pages.ServiceName
	const description string = pages.Description
	if serviceName != "home" {
		t.Fatal(pages.ServiceName)
	}
	if description == "" || strings.ContainsAny(description, "\"\\") || strings.ContainsFunc(description, unicode.IsControl) {
		t.Fatal("invalid description")
	}
	b := banner()
	cases := []struct {
		name string
		data any
	}{{"landing", pages.LandingData{b, []pages.Tile{{"provided-name", "https://provided.test", template.HTML("provided-icon"), true}}, []pages.Tile{{"another-name", "https://another.test", template.HTML("another-icon"), false}}}}, {"landing", pages.LandingData{}}, {"about", pages.AboutData{b, pages.Description}}, {"about", pages.AboutData{}}, {"notfound", pages.NoticeData{b}}, {"notfound", pages.NoticeData{}}}
	for _, c := range cases {
		if templateSet(t).Lookup(c.name) == nil {
			t.Fatal(c.name)
		}
		_ = execute(t, c.name, c.data)
	}
	handlers := struct {
		makeHandler func(pages.Config) http.Handler
	}{pages.Handler}
	h := handlers.makeHandler(pages.Config{func(page.User) page.Banner { return b }, "", writer(t, &telemetry.Capture{}, new(bytes.Buffer))})
	if answer(h, request("GET", "/")).Code != 200 {
		t.Fatal("handler")
	}
}

// R-DM1R-YDUM R-DOHK-PXC0 R-5S0J-3C52 R-66NB-OL1E R-61RQ-5I2M
// R-5OCT-Y0WZ R-4XJ1-J2LP R-5AXX-QJRC R-4ZYU-AM33
func TestRoutesAndPageData(t *testing.T) {
	t.Setenv(services.Variable, "")
	for _, empty := range []bool{false, true} {
		b := banner()
		if empty {
			b.Release, b.Commit = "", ""
		}
		root := b
		root.Trail = nil
		ab := b
		ab.Trail = []page.Level{{Name: "about", URL: "/about"}}
		h := pages.Handler(pages.Config{Banner: func(page.User) page.Banner { return b }, Telemetry: writer(t, &telemetry.Capture{}, new(bytes.Buffer))})
		for _, path := range []string{"/", "/about", "/nope", "/about/", "/index.html", "/tools", "/mcp", "/events", "/declarations", "/_appkit", "//"} {
			for _, method := range []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE"} {
				r := request(method, path+"?route=/about")
				r.Header.Set("Location", "/about")
				r.Host = "different.example.test"
				got := answer(h, r)
				status, want := 404, execute(t, "notfound", pages.NoticeData{root})
				if path == "/" || path == "/about" {
					if method != "GET" && method != "HEAD" {
						status, want = 405, ""
					} else {
						status = 200
						if path == "/" {
							want = execute(t, "landing", pages.LandingData{Banner: root})
						} else {
							want = execute(t, "about", pages.AboutData{ab, pages.Description})
						}
					}
				}
				if method == "HEAD" {
					want = ""
				}
				if got.Code != status || got.Body.String() != want {
					t.Fatalf("%s %s: status %d want %d, body equality %v", method, path, got.Code, status, got.Body.String() == want)
				}
				if status == 405 {
					if !reflect.DeepEqual(got.Header().Values("Allow"), []string{"GET, HEAD"}) {
						t.Fatal(got.Header())
					}
				} else {
					if !reflect.DeepEqual(got.Header().Values("Content-Type"), []string{"text/html; charset=utf-8"}) {
						t.Fatal(got.Header())
					}
					if status == 404 && len(got.Header().Values("Allow")) != 0 {
						t.Fatal(got.Header())
					}
				}
				if len(got.Header().Values("Set-Cookie")) != 0 || len(got.Header().Values("Location")) != 0 {
					t.Fatal(got.Header())
				}
			}
			get := answer(h, request("GET", path))
			head := answer(h, request("HEAD", path))
			if get.Code != head.Code || !reflect.DeepEqual(get.Header(), head.Header()) || head.Body.Len() != 0 {
				t.Fatal(path)
			}
		}
		for _, path := range []string{"/", "/about"} {
			r := request("GET", path)
			r.Header.Del("X-User-Email")
			a := answer(h, r)
			r.Header.Set("X-User-Email", "")
			c := answer(h, r)
			if a.Code == 500 || a.Code != c.Code || !reflect.DeepEqual(a.Header(), c.Header()) {
				t.Fatal("email is optional")
			}
		}
	}
}

// R-DN9O-C5LB R-DOHK-PXC0 R-DM1R-YDUM R-647I-X1K0
func TestLandingTilesFromFreshServices(t *testing.T) {
	t.Setenv(services.Variable, "")
	path := filepath.Join(t.TempDir(), "services.json")
	b := banner()
	b.Service = "different-service"
	b.Icon = template.HTML("provided-banner-icon")
	var gotUser page.User
	h := pages.Handler(pages.Config{Banner: func(u page.User) page.Banner { gotUser = u; return b }, ServicesPath: path, Telemetry: writer(t, new(telemetry.Capture), new(bytes.Buffer))})
	entryIndex := 0
	entry := func(name, group string, enabled, icon bool) map[string]any {
		entryIndex++
		e := map[string]any{"name": name, "url": "https://provided-service.test/?first=" + url.QueryEscape(name) + fmt.Sprintf("&second=%d", entryIndex), "description": "supplied-description", "socket": "/supplied/socket", "enabled": enabled, "mcp": false}
		if group != "" {
			e["group"] = group
		}
		if icon {
			e["icon"] = "supplied-icon-&" + name
		}
		return e
	}
	check := func(h http.Handler, list services.List) {
		t.Helper()
		root := b
		root.Trail = nil
		want := pages.LandingData{Banner: root}
		for _, e := range list {
			if !e.HasIcon || e.Name == pages.ServiceName {
				continue
			}
			tile := pages.Tile{e.Name, e.URL, e.Icon, e.Enabled}
			if e.Group == "core" {
				want.Core = append(want.Core, tile)
			} else {
				want.Application = append(want.Application, tile)
			}
		}
		r := request("GET", "/?extra=/about")
		r.Header.Set("X-User-Email", "supplied-landing@example.test")
		r.Header.Set("Location", "/about")
		got := answer(h, r)
		if got.Code != 200 || !reflect.DeepEqual(got.Header().Values("Content-Type"), []string{"text/html; charset=utf-8"}) || got.Body.String() != execute(t, "landing", want) {
			t.Fatalf("landing status %d, headers %+v, body equality %v", got.Code, got.Header(), got.Body.String() == execute(t, "landing", want))
		}
		base := "https://auth.example.test"
		if auth, ok := list.Find("auth"); ok && auth.URL != "" {
			base = auth.URL
		}
		if gotUser != (page.User{Email: "supplied-landing@example.test", ProfileURL: base + "/", LogoutURL: base + "/logout"}) {
			t.Fatal(gotUser)
		}
	}
	for _, entries := range [][]map[string]any{
		{entry("z-core<>&\"", "core", true, true), entry("z-app<>&\"", "application", true, true), entry("home", "core", true, true), entry("a-core<>&\"", "core", false, true), entry("no-icon", "core", true, false), entry("auth", "core", true, true), entry("a-app", "", true, true), entry("different-service", "unexpected", true, true), entry("z-app<>&\"", "application", false, true)},
		{entry("changed-core", "core", true, true)},
		{entry("changed-app", "", false, true)},
		{},
	} {
		data, err := json.Marshal(map[string]any{"services": entries})
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
		list, err := services.Read(path)
		if err != nil || len(list) != len(entries) {
			t.Fatalf("fixture read: %d entries, %v", len(list), err)
		}
		check(h, list)
	}
	if err := os.WriteFile(path, []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	check(h, nil)
	for _, p := range []string{"", filepath.Join(t.TempDir(), "missing.json"), t.TempDir()} {
		other := pages.Handler(pages.Config{Banner: func(u page.User) page.Banner { gotUser = u; return b }, ServicesPath: p, Telemetry: writer(t, new(telemetry.Capture), new(bytes.Buffer))})
		check(other, nil)
	}
}

// R-5AXX-QJRC
func TestDecodedPathChoosesRoute(t *testing.T) {
	t.Setenv(services.Variable, "")
	b := banner()
	h := pages.Handler(pages.Config{Banner: func(page.User) page.Banner { return b }, Telemetry: writer(t, new(telemetry.Capture), new(bytes.Buffer))})
	for _, path := range []string{"/%61bout", "/%2F", "/_appkit/%74heme.css"} {
		r := request("GET", path+"?path=/about")
		decoded := r.Clone(r.Context())
		decoded.URL.RawPath = ""
		a, reference := answer(h, r), answer(h, decoded)
		if a.Code != reference.Code || !reflect.DeepEqual(a.Header(), reference.Header()) || a.Body.String() != reference.Body.String() {
			t.Fatalf("encoded path %s differs from URL.Path %s", path, r.URL.Path)
		}
	}
}

func writeServices(t *testing.T, path, url, socket string) {
	t.Helper()
	document := map[string]any{"services": []map[string]any{{"name": "auth", "url": url, "socket": socket, "description": "test description", "enabled": true, "mcp": false}}}
	data, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}

// R-647I-X1K0 R-5UGB-UVMG R-6F6M-CZ89
func TestAuthLinksFreshAndFallback(t *testing.T) {
	t.Setenv(services.Variable, "")
	path := filepath.Join(t.TempDir(), "services.json")
	var got page.User
	h := pages.Handler(pages.Config{Banner: func(u page.User) page.Banner { got = u; return banner() }, ServicesPath: path, Telemetry: writer(t, &telemetry.Capture{}, new(bytes.Buffer))})
	for _, c := range []struct{ host, proto, base string }{{"home.sbx.ikigenba.dev:443", "https", "https://auth.sbx.ikigenba.dev"}, {"home.sbx.ikigenba.dev", "", "https://auth.sbx.ikigenba.dev"}, {"sbx.ikigenba.dev", "https", "https://auth.sbx.ikigenba.dev"}, {"home.sbx.ikigenba.dev", "http", "http://auth.sbx.ikigenba.dev"}, {"home.sbx.ikigenba.dev", "HTTPS", "https://auth.sbx.ikigenba.dev"}, {"home.sbx.ikigenba.dev", "Http", "https://auth.sbx.ikigenba.dev"}, {"home.sbx.ikigenba.dev", "https, http", "https://auth.sbx.ikigenba.dev"}, {"home.", "https", "https://auth.home."}, {"Home.space:abc", "https", "https://auth.Home.space:abc"}, {"home.space:", "https", "https://auth.space"}} {
		r := request("GET", "/")
		r.Host = c.host
		r.Header.Set("X-Forwarded-Proto", c.proto)
		r.Header.Set("X-User-Email", "  exact@example.test  ")
		answer(h, r)
		want := page.User{Email: "  exact@example.test  ", ProfileURL: c.base + "/", LogoutURL: c.base + "/logout"}
		if got != want {
			t.Fatalf("%+v: got %+v want %+v", c, got, want)
		}
	}
	for _, url := range []string{"https://first.test", "https://second.test/", ""} {
		writeServices(t, path, url, "")
		answer(h, request("GET", "/about"))
		base := url
		if base == "" {
			base = "https://auth.example.test"
		}
		if got.ProfileURL != base+"/" || got.LogoutURL != base+"/logout" {
			t.Fatal(got)
		}
	}
	if err := os.WriteFile(path, []byte(`{"services":[{"name":"other","url":"https://other.example.test","description":"fixture","socket":"/fixture/other.sock","enabled":true,"mcp":false}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	answer(h, request("GET", "/about"))
	if got.ProfileURL != "https://auth.example.test/" || got.LogoutURL != "https://auth.example.test/logout" {
		t.Fatal(got)
	}
	if err := os.WriteFile(path, []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	answer(h, request("GET", "/"))
	if got.ProfileURL != "https://auth.example.test/" {
		t.Fatal(got)
	}
	for _, p := range []string{"", t.TempDir()} {
		h := pages.Handler(pages.Config{Banner: func(u page.User) page.Banner { got = u; return banner() }, ServicesPath: p, Telemetry: writer(t, &telemetry.Capture{}, new(bytes.Buffer))})
		answer(h, request("GET", "/"))
		if got.ProfileURL != "https://auth.example.test/" {
			t.Fatal(got)
		}
	}
}

// R-5T8F-H3VR
func TestStaticDelegation(t *testing.T) {
	t.Setenv(services.Variable, "")
	h := pages.Handler(pages.Config{Banner: func(page.User) page.Banner { return page.Banner{} }, Telemetry: writer(t, &telemetry.Capture{}, new(bytes.Buffer))})
	for _, path := range []string{"/_appkit/theme.css", "/_appkit/nope.css", "/_appkit/../theme.css"} {
		for _, method := range []string{"GET", "HEAD", "POST"} {
			r := request(method, path+"?extra=one")
			a := answer(h, r)
			b := answer(page.Static(), r.Clone(r.Context()))
			if a.Code != b.Code || !reflect.DeepEqual(a.Header(), b.Header()) || a.Body.String() != b.Body.String() {
				t.Fatalf("%s %s differs", method, path)
			}
			if a.Header().Get("Set-Cookie") != "" {
				t.Fatal("cookie")
			}
		}
	}
}

// R-53MJ-FXB6
func TestIdentityBeforeAllRoutes(t *testing.T) {
	t.Setenv(services.Variable, "")
	h := pages.Handler(pages.Config{Banner: func(page.User) page.Banner { t.Fatal("unauthenticated banner"); return page.Banner{} }, Telemetry: writer(t, &telemetry.Capture{}, new(bytes.Buffer))})
	ref := identity.Require(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("identity accepted") }))
	for _, path := range []string{"/", "/about", "/nope", "/_appkit/theme.css"} {
		for _, method := range []string{"GET", "HEAD", "POST"} {
			for _, empty := range []bool{false, true} {
				r := request(method, path)
				r.Header.Del("X-User-Id")
				if empty {
					r.Header["X-User-Id"] = []string{"", "later"}
				}
				a := answer(h, r)
				b := answer(ref, r)
				if a.Code != 500 || a.Code != b.Code || !reflect.DeepEqual(a.Header(), b.Header()) || a.Body.String() != b.Body.String() {
					t.Fatal(path, method)
				}
			}
		}
	}
}

// R-59Q1-CS0N R-52EN-25KH R-516Q-ODTS R-54UF-TP1V R-58I4-Z09Y
func TestRequestEvents(t *testing.T) {
	t.Setenv(services.Variable, "")
	for _, c := range []struct{ method, path, user, id string }{{"GET", "/", "caller", "specified"}, {"POST", "/about", "caller", ""}, {"GET", "/nope", "caller", ""}, {"HEAD", "/_appkit/nope.css", "caller", "specified"}, {"GET", "/", "", ""}} {
		capture := new(telemetry.Capture)
		stderr := new(bytes.Buffer)
		w := writer(t, capture, stderr)
		h := pages.Handler(pages.Config{Banner: func(page.User) page.Banner { return banner() }, Telemetry: w})
		r := request(c.method, c.path)
		r.Header["X-User-Id"] = []string{c.user, "ignored"}
		r.Header["X-Request-Id"] = []string{c.id, "ignored"}
		got := answer(h, r)
		flush(t, w)
		events := capture.Events()
		if len(events) != 2 {
			t.Fatalf("events %+v", events)
		}
		if events[0].Name != "request.started" || events[1].Name != "request.finished" {
			t.Fatal(events)
		}
		if !reflect.DeepEqual(events[0].Attrs, telemetry.Attrs{"method": c.method, "path": c.path}) {
			t.Fatal(events[0])
		}
		want := telemetry.Attrs{"status": int64(got.Code), "duration_us": int64(0), "request_bytes": int64(0), "response_bytes": int64(got.Body.Len())}
		if !reflect.DeepEqual(events[1].Attrs, want) {
			t.Fatalf("attrs %+v want %+v", events[1].Attrs, want)
		}
		id := events[0].RequestID
		if c.id != "" {
			if id != c.id {
				t.Fatal(id)
			}
		} else {
			if len(id) != 32 || strings.ContainsFunc(id, func(r rune) bool { return (r < '0' || r > '9') && (r < 'a' || r > 'f') }) {
				t.Fatal(id)
			}
		}
		for _, e := range events {
			if e.RequestID != id || e.User != c.user {
				t.Fatal(e)
			}
		}
		if stderr.Len() != 0 {
			t.Fatal(stderr.String())
		}
	}
}

type sinkFunc func(context.Context, telemetry.Event) error

func (f sinkFunc) Deliver(ctx context.Context, e telemetry.Event) error { return f(ctx, e) }

// R-5C5U-4BI1
func TestDeliveryCannotDelayAnswers(t *testing.T) {
	t.Setenv(services.Variable, "")
	release := make(chan struct{})
	defer close(release)
	sinks := []telemetry.Sink{new(telemetry.Capture), sinkFunc(func(context.Context, telemetry.Event) error { return fmt.Errorf("refused: %w", telemetry.ErrRejected) }), sinkFunc(func(ctx context.Context, _ telemetry.Event) error {
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})}
	var baseline *httptest.ResponseRecorder
	for _, sink := range sinks {
		w := writer(t, sink, new(bytes.Buffer))
		h := pages.Handler(pages.Config{Banner: func(page.User) page.Banner { return banner() }, Telemetry: w})
		done := make(chan *httptest.ResponseRecorder, 1)
		go func() { done <- answer(h, request("GET", "/")) }()
		var got *httptest.ResponseRecorder
		select {
		case got = <-done:
		case <-time.After(time.Second):
			t.Fatal("answer waited for delivery")
		}
		if baseline == nil {
			baseline = got
		} else if baseline.Code != got.Code || !reflect.DeepEqual(baseline.Header(), got.Header()) || baseline.Body.String() != got.Body.String() {
			t.Fatal("delivery changed answer")
		}
	}
}

// R-4WB5-5AV0
func TestConcurrentRequests(t *testing.T) {
	t.Setenv(services.Variable, "")
	capture := new(telemetry.Capture)
	w := writer(t, capture, new(bytes.Buffer))
	h := pages.Handler(pages.Config{Banner: func(u page.User) page.Banner { b := banner(); b.Email = u.Email; return b }, Telemetry: w})
	var wg sync.WaitGroup
	for i := range 24 {
		wg.Go(func() {
			r := request("GET", "/about")
			id := fmt.Sprintf("request-%d", i)
			user := fmt.Sprintf("user-%d", i)
			r.Header.Set("X-Request-Id", id)
			r.Header.Set("X-User-Id", user)
			r.Header.Set("X-User-Email", user+"@example.test")
			got := answer(h, r)
			b := banner()
			b.Email = user + "@example.test"
			b.Trail = []page.Level{{Name: "about", URL: "/about"}}
			if got.Code != 200 || got.Body.String() != execute(t, "about", pages.AboutData{b, pages.Description}) {
				t.Error("concurrent answer")
			}
		})
	}
	wg.Wait()
	flush(t, w)
	events := capture.Events()
	byID := map[string][]telemetry.Event{}
	for _, e := range events {
		byID[e.RequestID] = append(byID[e.RequestID], e)
	}
	if len(byID) != 24 || len(events) != 48 {
		t.Fatal(len(byID), len(events))
	}
	for i := range 24 {
		pair := byID[fmt.Sprintf("request-%d", i)]
		if len(pair) != 2 {
			t.Fatal(pair)
		}
		if pair[0].Name != "request.started" || pair[1].Name != "request.finished" {
			t.Fatal(pair)
		}
		for _, e := range pair {
			if e.User != fmt.Sprintf("user-%d", i) {
				t.Fatal(e)
			}
		}
	}
}

// R-ZH4K-65HP
func TestNoSiblingConnections(t *testing.T) {
	t.Setenv(services.Variable, "")
	tcp, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = tcp.Close() }()
	dir, err := os.MkdirTemp("", "home-socket-")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = os.RemoveAll(dir) }()
	socket := filepath.Join(dir, "s")
	unix, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = unix.Close() }()
	path := filepath.Join(t.TempDir(), "services.json")
	writeServices(t, path, "http://"+tcp.Addr().String(), socket)
	h := pages.Handler(pages.Config{Banner: func(page.User) page.Banner { return banner() }, ServicesPath: path, Telemetry: writer(t, new(telemetry.Capture), new(bytes.Buffer))})
	for _, path := range []string{"/", "/about", "/nope", "/_appkit/theme.css"} {
		for _, method := range []string{"GET", "HEAD", "POST"} {
			answer(h, request(method, path))
		}
	}
	for _, listener := range []net.Listener{tcp, unix} {
		conn, ok := listener.(syscall.Conn)
		if !ok {
			t.Fatal("listener syscall interface")
		}
		raw, err := conn.SyscallConn()
		if err != nil {
			t.Fatal(err)
		}
		var acceptErr error
		if err = raw.Control(func(fd uintptr) {
			accepted, _, e := syscall.Accept4(int(fd), syscall.SOCK_NONBLOCK)
			acceptErr = e
			if e == nil {
				_ = syscall.Close(accepted)
			}
		}); err != nil {
			t.Fatal(err)
		}
		if !errors.Is(acceptErr, syscall.EAGAIN) && !errors.Is(acceptErr, syscall.EWOULDBLOCK) {
			t.Fatalf("connection pending: %v", acceptErr)
		}
	}
}

type observingResponse struct {
	*httptest.ResponseRecorder
	before func()
}

func (w *observingResponse) WriteHeader(status int) {
	w.before()
	w.ResponseRecorder.WriteHeader(status)
}
func (w *observingResponse) Write(b []byte) (int, error) {
	w.before()
	return w.ResponseRecorder.Write(b)
}

// R-52EN-25KH
func TestStartedBeforeResponseAndFinishedAfter(t *testing.T) {
	t.Setenv(services.Variable, "")
	capture := new(telemetry.Capture)
	w := writer(t, capture, new(bytes.Buffer))
	h := pages.Handler(pages.Config{Banner: func(page.User) page.Banner { return banner() }, Telemetry: w})
	out := &observingResponse{ResponseRecorder: httptest.NewRecorder(), before: func() {
		flush(t, w)
		events := capture.Events()
		if len(events) != 1 || events[0].Name != "request.started" {
			t.Fatalf("events before answer %+v", events)
		}
	}}
	h.ServeHTTP(out, request("GET", "/"))
	flush(t, w)
	events := capture.Events()
	if len(events) != 2 || events[1].Name != "request.finished" {
		t.Fatal(events)
	}
}

// R-DOHK-PXC0 R-5S0J-3C52 R-66NB-OL1E R-6F6M-CZ89
func TestEachPageUsesItsRequestBanner(t *testing.T) {
	t.Setenv(services.Variable, "")
	type returned struct {
		user   page.User
		banner page.Banner
	}
	var calls []returned
	serial := 0
	h := pages.Handler(pages.Config{Banner: func(u page.User) page.Banner {
		serial++
		b := banner()
		b.Release = fmt.Sprintf("supplied-release-%d", serial)
		b.Commit = fmt.Sprintf("supplied-commit-%d", serial)
		b.Service = fmt.Sprintf("supplied-service-%d", serial)
		b.Email, b.ProfileURL, b.LogoutURL = u.Email, u.ProfileURL, u.LogoutURL
		calls = append(calls, returned{u, b})
		return b
	}, Telemetry: writer(t, new(telemetry.Capture), new(bytes.Buffer))})
	for i, path := range []string{"/", "/about", "/nope", "/", "/about", "/another"} {
		calls = nil
		r := request("GET", path)
		r.Host = fmt.Sprintf("home.space-%d.test:443", i)
		r.Header.Set("X-Forwarded-Proto", "http")
		r.Header.Set("X-User-Email", fmt.Sprintf("supplied-%d@example.test", i))
		user := page.User{Email: r.Header.Get("X-User-Email"), ProfileURL: fmt.Sprintf("http://auth.space-%d.test/", i), LogoutURL: fmt.Sprintf("http://auth.space-%d.test/logout", i)}
		got := answer(h, r)
		matches := false
		for _, call := range calls {
			if call.user != user {
				continue
			}
			b := call.banner
			b.Trail = nil
			name := "notfound"
			var data any = pages.NoticeData{Banner: b}
			switch path {
			case "/":
				name = "landing"
				data = pages.LandingData{Banner: b}
			case "/about":
				name = "about"
				b.Trail = []page.Level{{Name: "about", URL: "/about"}}
				data = pages.AboutData{Banner: b, Description: pages.Description}
			}
			if got.Body.String() == execute(t, name, data) {
				matches = true
			}
		}
		if !matches {
			t.Fatalf("request %d to %s did not render a banner returned for its user; calls %+v", i, path, calls)
		}
	}
}
