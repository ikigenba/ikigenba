package panel_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/dummy"
	"github.com/ikigenba/ikigenba/dummy/internal/panel"
	"github.com/ikigenba/ikigenba/dummy/internal/tools"
	"github.com/ikigenba/ikigenba/dummy/internal/widget"
)

// panelTemplateData provides the page asset's declared data fields.
type panelTemplateData struct {
	Banner  page.Banner
	Panel   bool
	Message string
	Count   int
	Table   []widget.Widget
	Form    panel.FormView
}

func pageTestRequest(method, path string) *http.Request {
	r := httptest.NewRequest(method, path, nil)
	r.Header.Set("X-User-Id", "caller")
	r.Header.Set("X-User-Email", "reader@example.test")
	r.Host = "dummy.space.test:8443"
	return r
}

func pageTestResponse(h http.Handler, r *http.Request) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func pageTestFormRequest(sub widget.Submission) *http.Request {
	values := url.Values{"name": {sub.Name}, "count": {sub.Count}, "status": {sub.Status}}
	body := values.Encode()
	r := httptest.NewRequest(http.MethodPost, "/widgets", strings.NewReader(body))
	r.Header.Set("X-User-Id", "caller")
	r.Header.Set("X-User-Email", "reader@example.test")
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Host = "dummy.space.test:8443"
	return r
}

// R-RDSK-MCE3 R-RF0H-044S
// R-KNG8-3SG1 R-XN6K-9LLU
// R-XWXR-BRJE R-Y0LG-H2RH
func TestPagePublicDeclarations(t *testing.T) {
	construct := func(f func(*widget.Store, func(page.User) page.Banner, *mcp.Server, *telemetry.Writer) http.Handler) http.Handler {
		t.Setenv(services.Variable, "")
		writer, _, _ := panelTestTelemetry(t, io.Discard)
		return f(panelTestStore(t), pageTestBanner, mcp.NewServer(mcp.ServerConfig{Name: panel.ServiceName, Telemetry: writer}), writer)
	}
	if construct(panel.Handler) == nil {
		t.Fatal("nil handler")
	}
	type logoutURLFunc func(string, string) string
	derive := logoutURLFunc(panel.LogoutURL)
	if derive("localhost", "") != panel.LocalLogoutURL {
		t.Fatal("derivation function")
	}
	const service, method, notFound, notAllowed, unsupported, local = panel.ServiceName, panel.MethodNotAllowedBody, panel.NotFoundMessage, panel.MethodNotAllowedMessage, panel.UnsupportedMediaTypeMessage, panel.LocalLogoutURL
	got := []string{service, method, notFound, notAllowed, unsupported, local}
	want := []string{"dummy", panel.MethodNotAllowedBody, panel.NotFoundMessage, panel.MethodNotAllowedMessage, panel.UnsupportedMediaTypeMessage, "http://localhost:3001/logout"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("constants: %#v", got)
	}
}

// R-0S0R-T71I
func TestPageLogoutURL(t *testing.T) {
	cases := []struct{ host, proto, want string }{
		{"dummy.space.test", "http", "http://auth.space.test/logout"},
		{"dummy.space.test:8443", "https", "https://auth.space.test/logout"},
		{"dummy.dummy.space.test:bad", "http", "http://auth.dummy.space.test/logout"},
		{"dummy.space:part:last", "http", "http://auth.space:part/logout"},
		{"dummy.x:", "http", "http://auth.x/logout"},
		{"dummy.", "http", panel.LocalLogoutURL}, {"dummy.:80", "https", panel.LocalLogoutURL},
		{"127.0.0.1:3000", "https", panel.LocalLogoutURL}, {"Dummy.space", "http", panel.LocalLogoutURL},
		{"[::1]:3000", "http", panel.LocalLogoutURL}, {"", "http", panel.LocalLogoutURL},
	}
	for _, proto := range []string{"", "HTTPS", "https, http", " https", "https ", "javascript:alert(1)"} {
		cases = append(cases, struct{ host, proto, want string }{"dummy.space.test", proto, "https://auth.space.test/logout"})
	}
	for _, tc := range cases {
		if got := panel.LogoutURL(tc.host, tc.proto); got != tc.want {
			t.Errorf("(%q,%q): %q want %q", tc.host, tc.proto, got, tc.want)
		}
	}
}

// R-FPJ9-VVTK R-GBHG-RR62 R-GYNK-1E99
func TestPageProfileURL(t *testing.T) {
	type profileString string
	const local profileString = panel.LocalProfileURL
	if local != profileString("http://localhost:3001/") {
		t.Fatal("local profile URL")
	}
	derive := []func(string, string) string{panel.ProfileURL}[0]
	for _, tc := range []struct{ host, proto, want string }{
		{"dummy.space.test", "", "https://auth.space.test/"},
		{"dummy.space.test:8443", "http", "http://auth.space.test/"},
		{"dummy.space.test:8443", "https", "https://auth.space.test/"},
		{"dummy.space.test", "HTTPS", "https://auth.space.test/"},
		{"dummy.space.test", "http, https", "https://auth.space.test/"},
		{"dummy.space.test", " https", "https://auth.space.test/"},
		{"dummy.a:b:c", "http", "http://auth.a:b/"},
		{"dummy..", "http", "http://auth../"},
		{"dummy.", "https", panel.LocalProfileURL},
		{"dummy.:8443", "http", panel.LocalProfileURL},
		{"Dummy.space.test", "http", panel.LocalProfileURL},
		{"localhost:8080", "http", panel.LocalProfileURL},
		{"", "https", panel.LocalProfileURL},
		{"[::1]:8080", "http", panel.LocalProfileURL},
		{"unrelated.test", "https", panel.LocalProfileURL},
	} {
		if got := derive(tc.host, tc.proto); got != tc.want {
			t.Errorf("ProfileURL(%q, %q) = %q, want %q", tc.host, tc.proto, got, tc.want)
		}
	}
}

// R-ZZW5-QU6U R-1604-1QQ2 R-MFHP-F5AC
func TestPageIdentityBeforeRouting(t *testing.T) {
	for _, path := range []string{"/", "/widgets", "/widgets/table", "/widgets/", "/unknown", "/mcp", "/_appkit/theme.css"} {
		for _, method := range []string{"GET", "HEAD", "POST", "PUT", "OPTIONS"} {
			for _, present := range []bool{false, true} {
				s := panelTestStore(t)
				before := panelStoreAll(t, s)
				r := pageTestRequest(method, path)
				r.Header.Del("X-User-Id")
				if present {
					r.Header["X-User-Id"] = []string{""}
				}
				w := pageTestResponse(coreHandler(t, s, pageTestBanner, io.Discard), r)
				want := identity.MissingBody
				if method == "HEAD" {
					want = ""
				}
				if w.Code != 500 || !reflect.DeepEqual(w.Header().Values("Content-Type"), []string{"text/plain; charset=utf-8"}) || w.Body.String() != want {
					t.Fatalf("missing %s %s: %d %v %q", method, path, w.Code, w.Header(), w.Body.String())
				}
				if !reflect.DeepEqual(before, panelStoreAll(t, s)) {
					t.Fatal("missing identity changed store")
				}
			}
		}
	}
}

// R-YA3F-R76B R-1KMW-MZME R-1N2P-EJ3S R-1KMW-MZME R-1PII-62L6
// R-Y8VJ-DFFM R-1UE3-P5JY R-MGPL-SX11
func TestPageRoutes(t *testing.T) {
	cases := []struct {
		method, path   string
		status         int
		allow, message string
	}{
		{"GET", "/", 303, "", ""}, {"HEAD", "/?q=x", 303, "", ""},
		{"POST", "/", 405, "GET, HEAD", panel.MethodNotAllowedMessage}, {"OPTIONS", "/", 405, "GET, HEAD", panel.MethodNotAllowedMessage},
		{"GET", "/widgets", 200, "", ""}, {"GET", "/widgets?q=/unknown", 200, "", ""},
		{"PUT", "/widgets", 405, "GET, HEAD, POST", panel.MethodNotAllowedMessage},
		{"OPTIONS", "/widgets", 405, "GET, HEAD, POST", panel.MethodNotAllowedMessage},
		{"GET", "/unknown", 404, "", panel.NotFoundMessage}, {"POST", "/unknown", 404, "", panel.NotFoundMessage},
		{"HEAD", "/unknown", 404, "", panel.NotFoundMessage}, {"DELETE", "/unknown", 404, "", panel.NotFoundMessage},
		{"GET", "/mcp/", 404, "", panel.NotFoundMessage}, {"POST", "/mcp/more", 404, "", panel.NotFoundMessage},
		{"GET", "/widgets/", 404, "", panel.NotFoundMessage}, {"POST", "/widgets/", 404, "", panel.NotFoundMessage},
		{"GET", "/widgets/table/", 404, "", panel.NotFoundMessage}, {"HEAD", "/widgets/table/", 404, "", panel.NotFoundMessage},
		{"GET", "/about/", 404, "", panel.NotFoundMessage}, {"POST", "/tools/", 404, "", panel.NotFoundMessage},
		{"GET", "//widgets", 404, "", panel.NotFoundMessage}, {"GET", "/Widgets", 404, "", panel.NotFoundMessage},
		{"GET", "/widgets/table", 200, "", ""}, {"POST", "/widgets/table", 405, "GET, HEAD", ""},
	}
	for _, path := range []string{"/unknown", "/widgets/", "/widgets/table/", "/about/", "/tools/", "/mcp/", "/mcp/more"} {
		for _, method := range []string{"GET", "HEAD", "POST", "PUT", "DELETE", "PATCH", "OPTIONS", "TRACE", "custom"} {
			cases = append(cases, struct {
				method, path   string
				status         int
				allow, message string
			}{method, path + "?q=/widgets", 404, "", panel.NotFoundMessage})
		}
	}
	for _, tc := range cases {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			s := panelTestStore(t)
			before := panelStoreAll(t, s)
			r := pageTestRequest(tc.method, tc.path)
			r.Host = "unrelated.test"
			r.Header.Set("Accept", "application/json")
			r.Header.Set("X-Original-URI", "/different")
			w := pageTestResponse(coreHandler(t, s, pageTestBanner, io.Discard), r)
			if w.Code != tc.status || w.Header().Get("Allow") != tc.allow {
				t.Fatalf("route: %d %v", w.Code, w.Header())
			}
			if tc.status == 303 {
				if w.Header().Get("Location") != "/widgets" || w.Body.Len() != 0 {
					t.Fatal("redirect shape")
				}
			} else if w.Header().Get("Location") != "" {
				t.Fatal("unexpected redirect")
			}
			if tc.message != "" {
				pageTestFailure(t, w, r, tc.message)
			}
			if !reflect.DeepEqual(before, panelStoreAll(t, s)) {
				t.Fatal("read or failure changed store")
			}
		})
	}
}

func pageTestFailure(t *testing.T, w *httptest.ResponseRecorder, r *http.Request, message string) {
	t.Helper()
	if w.Header().Get("Content-Type") != "text/html; charset=utf-8" {
		t.Fatal("failure content type")
	}
	if r.Method == "HEAD" {
		if w.Body.Len() != 0 {
			t.Fatal("HEAD failure body")
		}
		return
	}
	var exact bytes.Buffer
	set, err := page.Templates().ParseFS(dummy.Assets(), "*.html")
	if err != nil {
		t.Fatal(err)
	}
	if err = set.ExecuteTemplate(&exact, "page", panelTemplateData{Banner: pageTestBannerData(r), Message: message}); err != nil {
		t.Fatal(err)
	}
	if w.Body.String() != exact.String() {
		t.Fatal("failure differs from template")
	}
}

// R-GVCC-LSLI R-GXS5-DC2W
func TestPageHeadAndOptionalEmail(t *testing.T) {
	for _, path := range []string{"/", "/widgets", "/widgets/table", "/unknown", "/widgets/", "/widgets/table/"} {
		h := coreHandler(t, panelTestStore(t), pageTestBanner, io.Discard)
		get := pageTestRequest("GET", path)
		head := get.Clone(get.Context())
		head.Method = "HEAD"
		a, b := pageTestResponse(h, get), pageTestResponse(h, head)
		if a.Code != b.Code || !reflect.DeepEqual(a.Header(), b.Header()) || b.Body.Len() != 0 {
			t.Fatalf("HEAD %s: %d/%d %v/%v", path, a.Code, b.Code, a.Header(), b.Header())
		}

		h = coreHandler(t, panelTestStore(t), pageTestBanner, io.Discard)
		absent := pageTestRequest("GET", path)
		absent.Header.Del("X-User-Email")
		empty := absent.Clone(absent.Context())
		empty.Header["X-User-Email"] = []string{""}
		a, b = pageTestResponse(h, absent), pageTestResponse(h, empty)
		if a.Code == 500 || a.Code != b.Code || !reflect.DeepEqual(a.Header(), b.Header()) {
			t.Fatalf("email precondition %s", path)
		}
	}
}

func pageTestDocuments() []*http.Request {
	return []*http.Request{
		pageTestRequest("GET", "/widgets"),
		pageTestRequest("GET", "/unknown"),
		pageTestRequest("POST", "/"),
		pageTestRequest("PUT", "/widgets"),
		pageTestRequest("POST", "/widgets"),
		pageTestFormRequest(widget.Submission{Name: " ", Count: "three", Status: "archived"}),
	}
}

// R-HCEX-YKZ8
func TestPageResponsesIndependentOfWorkingDirectory(t *testing.T) {
	checkout, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	empty := t.TempDir()
	// Construct and serve each handler in each directory, with a fresh equal store.
	secondRequests := pageTestDocuments()
	for i, request := range pageTestDocuments() {
		t.Chdir(checkout)
		first := pageTestResponse(coreHandler(t, panelTestStore(t), pageTestBanner, io.Discard), request.Clone(request.Context()))
		t.Chdir(empty)
		secondRequest := secondRequests[i]
		if request.GetBody != nil {
			secondRequest.Body, err = request.GetBody()
			if err != nil {
				t.Fatal(err)
			}
		}
		second := pageTestResponse(coreHandler(t, panelTestStore(t), pageTestBanner, io.Discard), secondRequest)
		if first.Code != second.Code || !reflect.DeepEqual(first.Header(), second.Header()) || first.Body.String() != second.Body.String() {
			t.Fatalf("directory-dependent response for %s %s", request.Method, request.URL.Path)
		}
	}
}

// R-ZT84-QX2A
func TestPageResponsesNeverSetCookie(t *testing.T) {
	for _, path := range []string{"/", "/widgets", "/widgets/table", "/logout", "/unknown", "/assets/theme.css", "/assets/OFL.txt", "/assets/absent"} {
		for _, method := range []string{"GET", "HEAD", "POST", "PUT", "OPTIONS"} {
			for _, identity := range []string{"", "user"} {
				for _, media := range []string{"application/json", "application/x-www-form-urlencoded"} {
					for _, body := range []string{"name=fresh&count=1&status=active", "name=&count=bad&status=archived"} {
						for _, validator := range []string{"", "*"} {
							r := httptest.NewRequest(method, path, strings.NewReader(body))
							r.Header.Set("X-User-Id", identity)
							r.Header.Set("Content-Type", media)
							r.Header.Set("Cookie", "session=present")
							r.Header.Set("If-None-Match", validator)
							w := pageTestResponse(coreHandler(t, panelTestStore(t), pageTestBanner, io.Discard), r)
							for key := range w.Header() {
								if strings.EqualFold(key, "Set-Cookie") {
									t.Fatalf("cookie in %s %s %d", method, path, w.Code)
								}
							}
						}
					}
				}
			}
		}
	}
}

func pageTestEchoingBanner(services []page.Service) func(page.User) page.Banner {
	return func(u page.User) page.Banner {
		return page.Banner{Service: panel.ServiceName, Email: u.Email, ProfileURL: u.ProfileURL, LogoutURL: u.LogoutURL, Services: services}
	}
}

func pageTestBanner(u page.User) page.Banner { return pageTestEchoingBanner(nil)(u) }

// R-HWWB-NEUV
func TestPageEchoingBannerSource(t *testing.T) {
	for _, services := range [][]page.Service{nil, {}, {{Name: "other", URL: "/other", Enabled: true}}} {
		source := pageTestEchoingBanner(services)
		for _, user := range []page.User{{}, {Email: "reader@example.test", ProfileURL: "/profile", LogoutURL: "/logout"}} {
			want := page.Banner{Service: panel.ServiceName, Email: user.Email, ProfileURL: user.ProfileURL, LogoutURL: user.LogoutURL, Services: services}
			if got := source(user); !reflect.DeepEqual(got, want) {
				t.Fatalf("echoing source: %#v, want %#v", got, want)
			}
		}
	}
}

func pageTestBannerData(r *http.Request) page.Banner {
	return pageTestBanner(page.User{Email: r.Header.Get("X-User-Email"), ProfileURL: panel.ProfileURL(r.Host, r.Header.Get("X-Forwarded-Proto")), LogoutURL: panel.LogoutURL(r.Host, r.Header.Get("X-Forwarded-Proto"))})
}

// R-YXIQ-SVS8
func pageTestWrittenBanner(t *testing.T, raw string, data page.Banner) {
	t.Helper()
	for _, name := range []string{"banner", "footer"} {
		var rendered bytes.Buffer
		if err := page.Templates().ExecuteTemplate(&rendered, name, data); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(raw, rendered.String()) {
			t.Fatalf("missing rendered %s", name)
		}
	}
}

// R-Z2EC-BYR0
// R-YV2Y-1CAU R-Z79X-V1PS R-YXIQ-SVS8
func TestPageBannerSourcePerDocument(t *testing.T) {
	var users []page.User
	var drawn page.Banner
	source := func(u page.User) page.Banner {
		users = append(users, u)
		drawn = page.Banner{Service: "supplied", Email: fmt.Sprintf("fresh-%d", len(users)), ProfileURL: "/profile", LogoutURL: "/logout"}
		return drawn
	}
	h := coreHandler(t, panelTestStore(t), source, io.Discard)
	for i, r := range pageTestDocuments() {
		r.Host = "dummy.space.test:8443"
		r.Header.Set("X-User-Email", fmt.Sprintf("reader-%d@example.test", i))
		r.Header.Set("X-Forwarded-Proto", "http")
		before := len(users)
		w := pageTestResponse(h, r)
		if w.Body.Len() == 0 || w.Header().Get("Content-Type") != "text/html; charset=utf-8" || len(users) != before+1 {
			t.Fatal("document must fetch exactly one banner")
		}
		want := page.User{Email: r.Header.Get("X-User-Email"), ProfileURL: "http://auth.space.test/", LogoutURL: "http://auth.space.test/logout"}
		if users[len(users)-1] != want {
			t.Fatalf("banner user: %#v want %#v", users[len(users)-1], want)
		}
		pageTestWrittenBanner(t, w.Body.String(), drawn)
	}
	for _, path := range []string{"/widgets/table", "/_appkit/theme.css", "/_appkit/", "/_appkit/absent", "/widgets", "/unknown"} {
		for _, method := range []string{"GET", "HEAD", "POST", "PUT"} {
			for _, identity := range []bool{false, true} {
				if identity && path != "/widgets/table" && !strings.HasPrefix(path, page.StaticPrefix) {
					continue
				}
				r := pageTestRequest(method, path)
				if !identity {
					r.Header.Del("X-User-Id")
				}
				before := len(users)
				pageTestResponse(h, r)
				if len(users) != before {
					t.Fatalf("banner fetched for %s %s identity=%v", method, path, identity)
				}
			}
		}
	}
}

func coreHandler(t *testing.T, s *widget.Store, banner func(page.User) page.Banner, stderr io.Writer) http.Handler {
	t.Helper()
	t.Setenv(services.Variable, "")
	writer, _, _ := panelTestTelemetry(t, stderr)
	return panel.Handler(s, banner, mcp.NewServer(mcp.ServerConfig{Name: panel.ServiceName, Telemetry: writer}), writer)
}

func coreDraft(sub widget.Submission) widget.Draft { d, _ := widget.ParseSubmission(sub); return d }

// R-UTCV-ZF5P R-RG8D-DVVH R-RHG9-RNM6 R-U955-PAW6
// R-Y5H2-05Q9 R-Y2S1-GKQ5 R-YACN-J8P1
func TestPageTemplateContract(t *testing.T) {
	set, err := page.Templates().ParseFS(dummy.Assets(), "*.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"page", "table", "form", "script", "about", "tools"} {
		if set.Lookup(name) == nil {
			t.Fatalf("missing %s", name)
		}
	}
	store := panelTestStore(t)
	request := pageTestRequest("GET", "/widgets")
	data := panelTemplateData{Banner: pageTestBannerData(request), Panel: true, Count: len(panelStoreAll(t, store)), Table: panelStoreAll(t, store), Form: panel.FormView{Statuses: widget.Statuses()}}
	var body bytes.Buffer
	if err = set.ExecuteTemplate(&body, "page", data); err != nil {
		t.Fatal(err)
	}
	got := pageTestResponse(coreHandler(t, store, pageTestBanner, io.Discard), request)
	if got.Code != 200 || got.Header().Get("Content-Type") != "text/html; charset=utf-8" || got.Body.String() != body.String() {
		t.Fatalf("panel template result: %d %v", got.Code, got.Header())
	}
}

// R-MOY9-S5I9 R-KOO4-HK6Q
func TestPanelMCPDelegation(t *testing.T) {
	t.Setenv(services.Variable, "")
	store := panelTestStore(t)
	writer, _, _ := panelTestTelemetry(t, io.Discard)
	makeServer := func() *mcp.Server {
		return mcp.NewServer(mcp.ServerConfig{Name: panel.ServiceName, Telemetry: writer, Instructions: func(ctx context.Context) string {
			c, _ := identity.FromContext(ctx)
			return c.UserID + "|" + c.Email + "|" + c.RequestID
		}})
	}
	actual, wantServer := makeServer(), makeServer()
	h := panel.Handler(store, pageTestBanner, actual, writer)
	tools.Register(wantServer, store, writer)
	for _, tc := range []struct{ method, media, body string }{{"GET", "", ""}, {"HEAD", "", ""}, {"POST", "application/json", `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`}, {"POST", "application/json", `{broken`}, {"POST", "text/plain", ""}} {
		r := httptest.NewRequest(tc.method, "/mcp?q=x", strings.NewReader(tc.body))
		r.Header["X-User-Id"] = []string{"caller", "ignored"}
		r.Header["X-User-Email"] = []string{"email", "ignored"}
		r.Header["X-Request-Id"] = []string{"trace", "ignored"}
		r.Header.Set("Content-Type", tc.media)
		expected := r.Clone(identity.NewContext(r.Context(), identity.Caller{UserID: "caller", Email: "email", RequestID: "trace"}))
		expected.Body = io.NopCloser(strings.NewReader(tc.body))
		a, b := pageTestResponse(h, r), pageTestResponse(wantServer, expected)
		if a.Code != b.Code || !reflect.DeepEqual(a.Header(), b.Header()) || a.Body.String() != b.Body.String() {
			t.Fatalf("MCP delegation %s: %d/%d", tc.method, a.Code, b.Code)
		}
	}
	aServer := httptest.NewServer(h)
	defer aServer.Close()
	bServer := httptest.NewServer(identity.Require(wantServer))
	defer bServer.Close()
	caller := identity.Caller{UserID: "caller"}
	a, err := mcp.NewClient(mcp.ClientConfig{Endpoint: aServer.URL + "/mcp"}).ListTools(context.Background(), caller)
	if err != nil {
		t.Fatal(err)
	}
	b, err := mcp.NewClient(mcp.ClientConfig{Endpoint: bServer.URL + "/mcp"}).ListTools(context.Background(), caller)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatal("Handler registered different tools")
	}
}

func renderPanelTemplate(t *testing.T, name string, data any) string {
	t.Helper()
	set, err := page.Templates().ParseFS(dummy.Assets(), "*.html")
	if err != nil {
		t.Fatal(err)
	}
	var body bytes.Buffer
	if err = set.ExecuteTemplate(&body, name, data); err != nil {
		t.Fatal(err)
	}
	return body.String()
}

func assertFormPage(t *testing.T, response *httptest.ResponseRecorder, request *http.Request, widgets []widget.Widget, sub widget.Submission, errs widget.FieldErrors) {
	t.Helper()
	view := panel.FormView{Statuses: widget.Statuses()}
	if request.Method == http.MethodPost {
		draft, _ := widget.ParseSubmission(sub)
		view.Submission, view.Errors, view.Selected = sub, errs, draft.Status
	}
	data := panelTemplateData{Banner: pageTestBannerData(request), Panel: true, Count: len(widgets), Table: widgets, Form: view}
	if response.Header().Get("Content-Type") != "text/html; charset=utf-8" || response.Body.Len() == 0 || response.Body.String() != renderPanelTemplate(t, "page", data) {
		t.Fatal("response differs from page template with declared form view")
	}
}
