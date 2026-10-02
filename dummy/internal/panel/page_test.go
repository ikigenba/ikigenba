package panel_test

import (
	"bytes"
	"context"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"regexp"
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

func pageTestTags(s, name string, end bool) [][2]int {
	prefix := "<"
	if end {
		prefix += "/"
	}
	re := regexp.MustCompile(`(?i:` + regexp.QuoteMeta(prefix+name) + `)(?:[^a-zA-Z0-9>][^>]*>|>)`)
	matches := re.FindAllStringIndex(s, -1)
	spans := make([][2]int, 0, len(matches))
	for _, m := range matches {
		spans = append(spans, [2]int{m[0], m[1]})
	}
	return spans
}

type coreAttribute struct {
	name, value string
	valued      bool
}

func coreAttributes(tag string) []coreAttribute {
	i := 1
	for i < len(tag) && ((tag[i] >= 'a' && tag[i] <= 'z') || (tag[i] >= 'A' && tag[i] <= 'Z') || (tag[i] >= '0' && tag[i] <= '9') || tag[i] == '-') {
		i++
	}
	var attrs []coreAttribute
	for i < len(tag) {
		start := i
		for i < len(tag) && strings.ContainsRune(" \t\n\v\f\r", rune(tag[i])) {
			i++
		}
		if i == start {
			break
		}
		start = i
		for i < len(tag) && !strings.ContainsRune(" \t\n\v\f\r\"'<>/=", rune(tag[i])) {
			i++
		}
		if i == start {
			break
		}
		attr := coreAttribute{name: tag[start:i]}
		if i < len(tag) && tag[i] == '=' {
			if i+1 >= len(tag) || tag[i+1] != '"' {
				break
			}
			i += 2
			start = i
			for i < len(tag) && tag[i] != '"' {
				i++
			}
			if i >= len(tag) {
				break
			}
			attr.value = html.UnescapeString(tag[start:i])
			attr.valued = true
			i++
		}
		attrs = append(attrs, attr)
	}
	return attrs
}

func pageTestAttribute(tag, name string) (string, bool) {
	for _, attr := range coreAttributes(tag) {
		if attr.valued && strings.EqualFold(attr.name, name) {
			return attr.value, true
		}
	}
	return "", false
}

func pageTestStrip(s string) string {
	var result strings.Builder
	for {
		start, end, name := -1, -1, ""
		for _, candidate := range []string{"script", "style"} {
			spans := pageTestTags(s, candidate, false)
			if len(spans) > 0 && (start < 0 || spans[0][0] < start) {
				start, end, name = spans[0][0], spans[0][1], candidate
			}
		}
		if start < 0 {
			result.WriteString(s)
			return result.String()
		}
		closing := pageTestTags(s[end:], name, true)
		result.WriteString(s[:start])
		if len(closing) == 0 {
			return result.String()
		}
		s = s[end+closing[0][1]:]
	}
}

func pageTestNormalize(s string) string {
	s = pageTestStrip(s)
	for {
		start := strings.IndexByte(s, '<')
		if start < 0 {
			break
		}
		end := strings.IndexByte(s[start:], '>')
		if end < 0 {
			s = s[:start]
			break
		}
		s = s[:start] + s[start+end+1:]
	}
	return strings.Join(strings.Fields(html.UnescapeString(s)), " ")
}

func pageTestVisible(s string) string {
	s = pageTestStrip(s)
	start := pageTestTags(s, "body", false)
	if len(start) == 0 {
		return ""
	}
	end := pageTestTags(s[start[0][1]:], "body", true)
	if len(end) == 0 {
		return ""
	}
	return pageTestNormalize(s[start[0][1] : start[0][1]+end[0][0]])
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

// R-LPDH-LA2H R-MBBO-H5EZ R-MW1Y-Z90S R-NGS9-HCML
// R-O2QG-D7Z3 R-07AH-B3FP
func TestPageTextProcedures(t *testing.T) {
	if got := pageTestTags("<tr><th>x</th></tr>", "tr", false); len(got) != 1 || got[0] != [2]int{0, 4} {
		t.Fatalf("adjacent bare tag extent: %v", got)
	}
	if got := pageTestTags("<aK>", "a", false); len(got) != 1 {
		t.Fatalf("non-ASCII delimiter: %v", got)
	}
	if got := pageTestStrip("<sty<script>x</script>le>"); got != "<style>" {
		t.Fatalf("strip must scan left to right without re-reading earlier output: %q", got)
	}
	tags := `<abbr>a</abbr><A href="x">b</A><a1>c</a1><a-x>d</a-x><a`
	if got := pageTestTags(tags, "a", false); len(got) != 2 || tags[got[0][0]:got[0][1]] != `<A href="x">` || tags[got[1][0]:got[1][1]] != "<a-x>" {
		t.Fatalf("start tag spans: %v", got)
	}
	if got := pageTestTags(tags, "a", true); len(got) != 2 {
		t.Fatalf("end tags: %v", got)
	}
	if got, ok := pageTestAttribute(`<input data-name="wrong" NAME=" &amp;&#34;&lt; &#61; ">`, "name"); !ok || got != " &\"< = " {
		t.Fatalf("attribute read: %q %v", got, ok)
	}
	if _, ok := pageTestAttribute(`<input data-name="wrong" name='single'>`, "name"); ok {
		t.Fatal("non-occurrence accepted")
	}
	raw := `<body> one<script>if (a < b) { "<table>" }</script><style>x</style><b> two &amp; &lt;x&gt; </b>` + "\t\n\u2003" + `three</body>`
	if got := pageTestStrip(raw); strings.Contains(got, "script") || strings.Contains(got, "style") || strings.Contains(got, "table") {
		t.Fatalf("script strip: %q", got)
	}
	if got := pageTestNormalize(raw); got != "one two & <x> three" {
		t.Fatalf("normalise: %q", got)
	}
	if got := pageTestVisible("ignored" + raw + "ignored"); got != "one two & <x> three" {
		t.Fatalf("visible: %q", got)
	}
	for _, s := range []string{"no body", "<body>unclosed", "</body>only"} {
		if pageTestVisible(s) != "" {
			t.Fatalf("visible without body pair: %q", s)
		}
	}
	if got := pageTestStrip(`before<SCRIPT>x`); got != "before" {
		t.Fatalf("unclosed strip: %q", got)
	}
	if got := pageTestNormalize("before<unclosed"); got != "before" {
		t.Fatalf("unclosed normalisation: %q", got)
	}
	if got := pageTestStrip(`<scriptx>keep</scriptx><style>drop</style>`); got != `<scriptx>keep</scriptx>` {
		t.Fatalf("delimiter strip: %q", got)
	}
	// Explicit raw/stripped frames differ for scripts and table-looking script source.
	if len(pageTestTags(raw, "script", false)) != 1 || len(pageTestTags(pageTestStrip(raw), "script", false)) != 0 || len(pageTestTags(pageTestStrip(raw), "table", false)) != 0 {
		t.Fatal("count frame conflated")
	}
}

// R-KNG8-3SG1 R-XN6K-9LLU R-XQU9-EWTX R-XTA2-6GBB
// R-XWXR-BRJE R-Y0LG-H2RH R-Y319-8M8V
func TestPagePublicDeclarations(t *testing.T) {
	construct := func(f func(*widget.Store, func(page.User) page.Banner, *mcp.Server, *telemetry.Writer) http.Handler) http.Handler {
		t.Setenv(services.Variable, "")
		writer, _, _ := panelTestTelemetry(t, io.Discard)
		return f(panelTestStore(), pageTestBanner, mcp.NewServer(mcp.ServerConfig{Name: panel.ServiceName, Telemetry: writer}), writer)
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
	want := []string{"dummy", "method not allowed\n", "That page was not found.", "That method is not allowed here.", "That media type is not supported.", "http://localhost:3001/logout"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("constants: %#v", got)
	}
	for _, body := range []string{panel.MethodNotAllowedBody} {
		if strings.Count(body, "\n") != 1 || !strings.HasSuffix(body, "\n") {
			t.Fatalf("plain line: %q", body)
		}
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
				s := panelTestStore()
				before := s.All()
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
				if !reflect.DeepEqual(before, s.All()) {
					t.Fatal("missing identity changed store")
				}
			}
		}
	}
}

// R-1I73-VG50 R-1KMW-MZME R-1N2P-EJ3S R-1KMW-MZME R-1PII-62L6
// R-1RYA-XM2K R-1UE3-P5JY R-MGPL-SX11
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
		{"GET", "//widgets", 404, "", panel.NotFoundMessage}, {"GET", "/Widgets", 404, "", panel.NotFoundMessage},
		{"GET", "/widgets/table", 200, "", ""}, {"POST", "/widgets/table", 405, "GET, HEAD", ""},
	}
	for _, tc := range cases {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			s := panelTestStore()
			before := s.All()
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
			if !reflect.DeepEqual(before, s.All()) {
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
	written := pageTestWritten(t, w.Body.String(), r)
	if !strings.Contains(pageTestVisible(written), message) {
		t.Fatalf("failure message absent: %q", w.Body.String())
	}
	found := false
	for _, span := range pageTestTags(pageTestStrip(written), "a", false) {
		if href, ok := pageTestAttribute(pageTestStrip(written)[span[0]:span[1]], "href"); ok && href == "/widgets" {
			found = true
		}
	}
	if !found {
		t.Fatal("failure missing backlink")
	}
}

// R-18FW-TA7G R-I0K0-SQ2Y
func TestPageHeadAndOptionalEmail(t *testing.T) {
	for _, path := range []string{"/", "/widgets", "/widgets/table", "/unknown", "/widgets/", "/widgets/table/"} {
		h := coreHandler(t, panelTestStore(), pageTestBanner, io.Discard)
		get := pageTestRequest("GET", path)
		head := get.Clone(get.Context())
		head.Method = "HEAD"
		a, b := pageTestResponse(h, get), pageTestResponse(h, head)
		if a.Code != b.Code || !reflect.DeepEqual(a.Header(), b.Header()) || b.Body.Len() != 0 {
			t.Fatalf("HEAD %s: %d/%d %v/%v", path, a.Code, b.Code, a.Header(), b.Header())
		}

		h = coreHandler(t, panelTestStore(), pageTestBanner, io.Discard)
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

// R-YIVY-7MVW R-0ZWM-4W0L
func TestPageDocumentChromeAndAttributes(t *testing.T) {
	for _, r := range pageTestDocuments() {
		w := pageTestResponse(coreHandler(t, panelTestStore(), pageTestBanner, io.Discard), r)
		body := w.Body.String()
		if w.Header().Get("Content-Type") != "text/html; charset=utf-8" || body == "" {
			t.Fatal("expected HTML document")
		}
		stripped := pageTestStrip(pageTestWritten(t, body, r))
		if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(stripped)), "<!doctype html>") || len(pageTestTags(stripped, "body", false)) != 1 || len(pageTestTags(stripped, "body", true)) != 1 {
			t.Fatal("HTML document frame")
		}
	}
}

func pageTestAttributeInvariants(t *testing.T, body string) {
	t.Helper()
	tags := regexp.MustCompile(`<[^>]*>`).FindAllString(pageTestStrip(body), -1)
	for _, span := range pageTestTags(body, "script", false) {
		tags = append(tags, body[span[0]:span[1]])
	}
	for _, tag := range tags {
		if reason := pageTestNamedAttributeError(tag); reason != "" {
			t.Fatalf("%s: %s", reason, tag)
		}
	}
}

func pageTestNamedAttributeError(tag string) string {
	if len(tag) < 3 || tag[0] != '<' || tag[1] == '/' || tag[1] == '!' {
		return ""
	}
	named := map[string]bool{"href": true, "xlink:href": true, "src": true, "poster": true, "data": true, "background": true, "manifest": true, "srcset": true, "imagesrcset": true, "class": true, "rel": true, "content": true, "id": true, "method": true, "action": true, "enctype": true, "name": true, "type": true, "value": true, "selected": true, "aria-describedby": true, "formaction": true, "formmethod": true, "formenctype": true, "data-service": true, "aria-hidden": true, "aria-label": true, "placeholder": true, "aria-disabled": true, "aria-current": true, "title": true, "data-status": true, "style": true, "ping": true, "srcdoc": true, "http-equiv": true, "for": true, "popover": true, "hidden": true}
	seen := make(map[string]bool)
	i := 1
	for i < len(tag) && !strings.ContainsRune(" \t\n\v\f\r/>", rune(tag[i])) {
		i++
	}
	for i < len(tag) {
		for i < len(tag) && strings.ContainsRune(" \t\n\v\f\r/", rune(tag[i])) {
			i++
		}
		if i >= len(tag) || tag[i] == '>' {
			break
		}
		start := i
		for i < len(tag) && !strings.ContainsRune(" \t\n\v\f\r/=><", rune(tag[i])) {
			i++
		}
		if i == start {
			i++
			continue
		}
		name := strings.ToLower(tag[start:i])
		if named[name] {
			if seen[name] {
				return "duplicate " + name
			}
			seen[name] = true
			if i+1 >= len(tag) || tag[i] != '=' || tag[i+1] != '"' {
				return "unquoted or bare " + name
			}
		}
		for i < len(tag) && strings.ContainsRune(" \t\n\v\f\r", rune(tag[i])) {
			i++
		}
		if i < len(tag) && tag[i] == '=' {
			i++
			for i < len(tag) && strings.ContainsRune(" \t\n\v\f\r", rune(tag[i])) {
				i++
			}
			if i < len(tag) && (tag[i] == '"' || tag[i] == '\'') {
				quote := tag[i]
				i++
				end := strings.IndexByte(tag[i:], quote)
				if end < 0 {
					return "unterminated attribute " + name
				}
				i += end + 1
			} else {
				for i < len(tag) && !strings.ContainsRune(" \t\n\v\f\r>", rune(tag[i])) {
					i++
				}
			}
		}
	}
	return ""
}

// R-YLBQ-Z6DA
func TestPageNamedAttributesHaveOneQuotedOccurrence(t *testing.T) {
	for _, name := range []string{"class", "rel", "content", "data-status", "aria-label", "xlink:href", "srcset", "for"} {
		if pageTestNamedAttributeError(fmt.Sprintf(`<span %s="first" %s="second">`, name, name)) == "" {
			t.Fatalf("duplicate %s accepted", name)
		}
	}
	for _, malformed := range []string{`<option selected>`, `<option selected='selected'>`, `<input value ="x">`, `<input value="x" value="y">`} {
		if pageTestNamedAttributeError(malformed) == "" {
			t.Fatalf("attribute checker accepted %s", malformed)
		}
	}
	if reason := pageTestNamedAttributeError(`<input value="inside selected bare and name=words">`); reason != "" {
		t.Fatalf("attribute checker misread quoted text: %s", reason)
	}
	attack := "x\" value=\"second\"\tname=\"other\"\nid=\"name-error\" > <script>"
	for _, request := range []*http.Request{
		pageTestRequest(http.MethodGet, "/widgets"),
		pageTestRequest(http.MethodGet, "/missing"),
		pageTestFormRequest(widget.Submission{Name: attack, Count: attack, Status: "archived"}),
	} {
		request.Header.Set("X-User-Email", attack)
		request.Host = "dummy." + attack
		request.Header.Set("X-Forwarded-Proto", attack)
		response := pageTestResponse(coreHandler(t, panelTestStore(), pageTestBanner, io.Discard), request)
		if response.Body.Len() == 0 || response.Header().Get("Content-Type") != "text/html; charset=utf-8" {
			t.Fatalf("expected HTML document: %d", response.Code)
		}
		pageTestAttributeInvariants(t, pageTestWritten(t, response.Body.String(), request))
		if response.Code == http.StatusUnprocessableEntity {
			controls := formControls(t, formSpan(t, response.Body.String()))
			for _, field := range []string{"name", "count"} {
				value, ok := pageTestAttribute(controls[field], "value")
				if !ok || value != attack {
					t.Errorf("%s value = %q, present=%v", field, value, ok)
				}
			}
		}
	}
	fragment := pageTestResponse(coreHandler(t, panelTestStore(), pageTestBanner, io.Discard), pageTestRequest(http.MethodGet, "/widgets/table"))
	if fragment.Code != http.StatusOK || fragment.Body.Len() == 0 {
		t.Fatalf("expected table fragment: %d", fragment.Code)
	}
	pageTestAttributeInvariants(t, fragment.Body.String())
}

// R-HZC4-EYC9
func TestPageRequestValuesPreserveDocumentStructure(t *testing.T) {
	base := widget.Submission{Name: strings.Repeat("n", widget.MaxNameRunes+1), Count: "bad", Status: "archived"}
	attack := `"><form><script>bad</script></form>`
	for _, field := range []string{"email", "host", "proto", "name", "count", "status"} {
		variant := base
		first, second := pageTestFormRequest(base), pageTestFormRequest(variant)
		switch field {
		case "email":
			second.Header.Set("X-User-Email", attack)
		case "host":
			second.Host = "dummy." + attack
		case "proto":
			second.Header.Set("X-Forwarded-Proto", attack)
		case "name":
			variant.Name = attack + base.Name
			second = pageTestFormRequest(variant)
		case "count":
			variant.Count = attack + base.Count
			second = pageTestFormRequest(variant)
		case "status":
			variant.Status = attack + base.Status
			second = pageTestFormRequest(variant)
		}
		oracle := panelTestStore()
		firstErrors := coreSubmissionErrors(oracle, base)
		secondErrors := coreSubmissionErrors(oracle, variant)
		if firstErrors != secondErrors {
			t.Fatalf("%s: unequal validation errors", field)
		}
		store := panelTestStore()
		before := store.All()
		h := coreHandler(t, store, pageTestBanner, io.Discard)
		a, b := pageTestResponse(h, first), pageTestResponse(h, second)
		if a.Code != b.Code || a.Code != http.StatusUnprocessableEntity || !reflect.DeepEqual(before, store.All()) {
			t.Fatalf("%s: comparison preconditions failed", field)
		}
		firstWritten, secondWritten := pageTestWritten(t, a.Body.String(), first), pageTestWritten(t, b.Body.String(), second)
		if !reflect.DeepEqual(pageTestTagSequence(firstWritten), pageTestTagSequence(secondWritten)) {
			t.Errorf("%s changed tag-name sequence", field)
		}
		if strings.Count(firstWritten, ">") != strings.Count(secondWritten, ">") {
			t.Errorf("%s changed greater-than count", field)
		}
	}
}

// R-HZC4-EYC9
func TestPageHeaderValuesPreserveEveryDocumentShape(t *testing.T) {
	services := []page.Service{{Name: "other", URL: "/other", Enabled: true}}
	for i, request := range pageTestDocuments() {
		for _, field := range []string{"email", "host", "proto"} {
			for _, attack := range []string{`"><form><script>bad</script></form>`, `</body><body>`, `&lt;div&gt;`} {
				second := pageTestDocuments()[i]
				switch field {
				case "email":
					second.Header.Set("X-User-Email", attack)
				case "host":
					second.Host = "dummy." + attack
				case "proto":
					second.Header.Set("X-Forwarded-Proto", attack)
				}
				s := panelTestStore()
				before := s.All()
				h := coreHandler(t, s, pageTestEchoingBanner(services), io.Discard)
				first := pageTestDocuments()[i]
				a, b := pageTestResponse(h, first), pageTestResponse(h, second)
				if a.Code != b.Code || !reflect.DeepEqual(before, s.All()) {
					t.Fatal("comparison preconditions failed")
				}
				firstData, secondData := pageTestBannerData(first), pageTestBannerData(second)
				firstData.Services, secondData.Services = services, services
				firstWritten, secondWritten := frameTestWritten(t, a.Body.String(), firstData), frameTestWritten(t, b.Body.String(), secondData)
				if !reflect.DeepEqual(pageTestTagSequence(firstWritten), pageTestTagSequence(secondWritten)) || strings.Count(firstWritten, ">") != strings.Count(secondWritten, ">") {
					t.Fatalf("%s %s %s changed document structure", request.Method, request.URL.Path, field)
				}
			}
		}
	}
}

// R-YOZG-4HLD R-YSN5-9STG R-0F6B-MSES R-2L7W-43V8
func TestPageScriptAndDocumentOrder(t *testing.T) {
	requests := append(pageTestDocuments(), pageTestRequest("GET", "/widgets/table"), pageTestRequest("POST", "/widgets/table"), pageTestRequest("GET", "/"))
	missing := pageTestRequest("GET", "/widgets")
	missing.Header.Del("X-User-Id")
	requests = append(requests, missing)
	for _, r := range requests {
		w := pageTestResponse(coreHandler(t, panelTestStore(), pageTestBanner, io.Discard), r)
		body := pageTestWritten(t, w.Body.String(), r)
		stripped := pageTestStrip(body)
		if len(pageTestTags(body, "style", false)) != 0 || len(pageTestTags(stripped, "script", false)) != 0 || len(pageTestTags(stripped, "style", false)) != 0 || pageTestStrip(stripped) != stripped {
			t.Fatal("raw style or non-idempotent stripping")
		}
		starts, ends := pageTestTags(body, "script", false), pageTestTags(body, "script", true)
		for i, start := range starts {
			if i >= len(ends) || ends[i][0] < start[1] {
				t.Fatal("unpaired script")
			}
			if strings.Contains(strings.ToLower(body[start[1]:ends[i][0]]), "</script") {
				t.Fatal("script closing sequence in source")
			}
		}
		if r.URL.Path != "/widgets" || (w.Code != 200 && w.Code != 422) {
			continue
		}
		if len(starts) != 1 || len(ends) != 1 {
			t.Fatal("panel requires exactly one inline script")
		}
		script := body[starts[0][1]:ends[0][0]]
		if _, has := pageTestAttribute(body[starts[0][0]:starts[0][1]], "src"); has {
			t.Fatal("external script")
		}
		for _, literal := range []string{"/widgets/table", "widgets-table", "5000"} {
			if !strings.Contains(script, literal) {
				t.Fatalf("script lacks %s", literal)
			}
		}
		tables := pageTestTags(body, "table", false)
		tableEnds := pageTestTags(body, "table", true)
		if len(tables) != 1 || len(tableEnds) != 1 || (starts[0][0] > tables[0][0] && starts[0][0] < tableEnds[0][1]) {
			t.Fatal("script inside table")
		}
		content := pageTestContent(t, stripped)
		forms := pageTestTags(content, "form", false)
		strippedEnds := pageTestTags(content, "table", true)
		if len(forms) != 1 || len(strippedEnds) != 1 || forms[0][0] < strippedEnds[0][1] {
			t.Fatal("form not below table")
		}
	}
}

func pageTestTagSequence(s string) []string {
	var result []string
	for i := 0; i < len(s); i++ {
		if s[i] != '<' {
			continue
		}
		start := i + 1
		if start < len(s) && s[start] == '/' {
			start++
		}
		end := start
		for end < len(s) && ((s[end] >= 'a' && s[end] <= 'z') || (s[end] >= 'A' && s[end] <= 'Z') || (s[end] >= '0' && s[end] <= '9')) {
			end++
		}
		result = append(result, s[start:end])
	}
	return result
}

func TestPageCallerBytesCannotChangeMarkup(t *testing.T) {
	attacks := []string{`<table id="evil"></table><body><form>`, `</script><script>alert(1)</script>`, `><img src=x>`, `&lt;script&gt;`, `x" value="zzz`, "x\"\tname=\"zzz", "x\"\nid=\"name-error", "x\"\rtype=\"image", "x\"\faria-describedby=\"count-error"}
	for _, field := range []string{"email", "host", "proto", "name", "count", "status"} {
		for _, attack := range attacks {
			base := widget.Submission{Name: strings.Repeat("n", widget.MaxNameRunes+1), Count: "not-a-number", Status: "archived"}
			variant := base
			if field == "name" {
				variant.Name = attack + strings.Repeat("n", widget.MaxNameRunes+1)
			}
			if field == "count" {
				variant.Count = attack + "not-a-number"
			}
			if field == "status" {
				variant.Status = attack + "archived"
			}
			s := panelTestStore()
			baseErr := coreSubmissionErrors(s, base)
			variantErr := coreSubmissionErrors(s, variant)
			if baseErr != variantErr {
				t.Fatal("test must preserve FieldErrors")
			}
			a, b := pageTestFormRequest(base), pageTestFormRequest(variant)
			switch field {
			case "email":
				b.Header.Set("X-User-Email", attack)
			case "host":
				b.Host = "dummy." + attack
			case "proto":
				b.Header.Set("X-Forwarded-Proto", attack)
			}
			h := coreHandler(t, s, pageTestBanner, io.Discard)
			first := pageTestResponse(h, a)
			second := pageTestResponse(h, b)
			if first.Code != 422 || second.Code != 422 {
				t.Fatal("test needs two panel pages")
			}
			firstWritten, secondWritten := pageTestWritten(t, first.Body.String(), a), pageTestWritten(t, second.Body.String(), b)
			if !reflect.DeepEqual(pageTestTagSequence(firstWritten), pageTestTagSequence(secondWritten)) || strings.Count(firstWritten, ">") != strings.Count(secondWritten, ">") {
				t.Fatalf("%s contributed markup: %q", field, attack)
			}
			pageTestAttributeInvariants(t, pageTestWritten(t, second.Body.String(), b))
		}
	}
}

func pageTestChrome(t *testing.T, raw string, r *http.Request) {
	t.Helper()
	pageTestWrittenBanner(t, raw, pageTestBannerData(r))
}

// R-00JJ-1JIG R-02ZB-T2ZU R-05F4-KMH8 R-07UX-C5YM
func TestPageDocumentHeadAndServiceSpelling(t *testing.T) {
	for _, r := range pageTestDocuments() {
		body := pageTestWritten(t, pageTestResponse(coreHandler(t, panelTestStore(), pageTestBanner, io.Discard), r).Body.String(), r)
		stripped := pageTestStrip(body)
		bodyStart := pageTestTags(stripped, "body", false)
		if len(bodyStart) != 1 {
			t.Fatal("missing body")
		}
		titleStarts := pageTestTags(stripped, "title", false)
		if len(titleStarts) != 1 || titleStarts[0][1] > bodyStart[0][0] {
			t.Fatal("title count or position")
		}

		titles := pageTestTags(stripped, "title", false)
		titleEnds := pageTestTags(stripped, "title", true)
		if len(titleEnds) != 1 || titleEnds[0][0] < titles[0][1] || titleEnds[0][1] > bodyStart[0][0] || pageTestNormalize(stripped[titles[0][1]:titleEnds[0][0]]) != panel.ServiceName {
			t.Fatal("title shape")
		}
		var stylesheets [][2]int
		for _, start := range pageTestTags(stripped, "link", false) {
			if rel, ok := pageTestAttribute(stripped[start[0]:start[1]], "rel"); ok && rel == "stylesheet" {
				stylesheets = append(stylesheets, start)
			}
		}
		if len(stylesheets) != 1 || stylesheets[0][1] > bodyStart[0][0] {
			t.Fatal("stylesheet count or position")
		}
		link := stylesheets[0]
		if href, _ := pageTestAttribute(stripped[link[0]:link[1]], "href"); href != "/_appkit/theme.css" {
			t.Fatal("stylesheet URL")
		}

		var viewports []string
		for _, meta := range pageTestTags(stripped, "meta", false) {
			tag := stripped[meta[0]:meta[1]]
			if name, _ := pageTestAttribute(tag, "name"); name == "viewport" {
				if meta[1] > bodyStart[0][0] {
					t.Fatal("viewport follows body")
				}
				content, _ := pageTestAttribute(tag, "content")
				viewports = append(viewports, content)
			}
		}
		if len(viewports) != 1 || viewports[0] != "width=device-width, initial-scale=1" {
			t.Fatal("viewport content")
		}
		if regexp.MustCompile(`(?i)dummy`).FindStringIndex(body) == nil {
			t.Fatal("service name absent")
		}
		for _, match := range regexp.MustCompile(`(?i)dummy`).FindAllString(body, -1) {
			if match != "dummy" {
				t.Fatalf("mixed-case service name %q", match)
			}
		}
	}
}

// R-25D7-5387 R-27SZ-WMPL R-2A8S-O66Z R-2COL-FPOD
func TestPagePanelLayout(t *testing.T) {
	for _, r := range []*http.Request{pageTestRequest("GET", "/widgets"), pageTestFormRequest(widget.Submission{Name: "", Count: "bad", Status: "archived"})} {
		body := pageTestStrip(pageTestWritten(t, pageTestResponse(coreHandler(t, panelTestStore(), pageTestBanner, io.Discard), r).Body.String(), r))
		headings, headingEnds := pageTestTags(body, "h1", false), pageTestTags(body, "h1", true)
		if len(headings) != 1 || len(headingEnds) != 1 || headings[0][1] > headingEnds[0][0] || pageTestNormalize(body[headings[0][1]:headingEnds[0][0]]) != "Widgets" {
			t.Fatal("heading shape and order")
		}
		var panels [][2]int
		for _, span := range pageTestTags(body, "div", false) {
			if class, _ := pageTestAttribute(body[span[0]:span[1]], "class"); class == "panel" {
				panels = append(panels, span)
			}
		}
		if len(panels) != 1 || headingEnds[0][1] > panels[0][0] {
			t.Fatal("panel count or position")
		}
		divStarts := pageTestTags(body[panels[0][0]:], "div", false)
		divEnds := pageTestTags(body[panels[0][0]:], "div", true)
		if len(divStarts) == 0 || len(divEnds) == 0 {
			t.Fatal("unclosed panel")
		}
		depth, panelEnd := 0, -1
		for i := panels[0][0]; i < len(body); i++ {
			if strings.HasPrefix(body[i:], "<div") {
				depth++
			} else if strings.HasPrefix(body[i:], "</div>") {
				depth--
				if depth == 0 {
					panelEnd = i
					break
				}
			}
		}
		if panelEnd < 0 {
			t.Fatal("panel not balanced")
		}
		inside := body[panels[0][1]:panelEnd]
		tables, tableEnds := pageTestTags(inside, "table", false), pageTestTags(inside, "table", true)
		sections, sectionEnds := pageTestTags(inside, "section", false), pageTestTags(inside, "section", true)
		if len(tables) != 1 || len(tableEnds) != 1 || len(sections) != 1 || len(sectionEnds) != 1 || tables[0][0] >= tableEnds[0][0] || tableEnds[0][1] >= sections[0][0] || sections[0][0] >= sectionEnds[0][0] {
			t.Fatalf("table and card shape: %q", inside)
		}
		if class, _ := pageTestAttribute(inside[sections[0][0]:sections[0][1]], "class"); class != "card" {
			t.Fatal("form card class")
		}
		if strings.TrimSpace(inside[:tables[0][0]]) != "" || strings.TrimSpace(inside[tableEnds[0][1]:sections[0][0]]) != "" || strings.TrimSpace(inside[sectionEnds[0][1]:]) != "" {
			t.Fatal("extra panel content")
		}
		bodyStarts, bodyEnds := pageTestTags(body, "body", false), pageTestTags(body, "body", true)
		if len(bodyStarts) != 1 || len(bodyEnds) != 1 || bodyEnds[0][0] <= panelEnd {
			t.Fatal("body frame")
		}

	}
}

// R-0AAQ-3PG0 R-0CQI-V8XE R-0F6B-MSES R-0HM4-EBW6 R-0K1X-5VDK R-0MHP-XEUY
func TestPageDocumentMarkupSafety(t *testing.T) {
	requests := pageTestDocuments()
	attack := `"><svg onload="evil()"><script src="https://elsewhere.test/x">`
	malicious := pageTestFormRequest(widget.Submission{Name: attack, Count: attack, Status: attack})
	malicious.Header.Set("X-User-Email", attack)
	malicious.Host = "dummy." + attack
	requests = append(requests, malicious)
	start := regexp.MustCompile(`<[A-Za-z]`)
	wellFormed := regexp.MustCompile(`^<[A-Za-z][A-Za-z0-9-]*(?:[\t\n\v\f\r ]+[^\t\n\v\f\r "'<>/=]+(?:="[^"<>]*")?)*[\t\n\v\f\r ]*/?>`)
	attribute := regexp.MustCompile(`[\t\n\v\f\r ]+([^\t\n\v\f\r "'<>/=]+)(?:="([^"<>]*)")?`)
	for _, r := range requests {
		body := pageTestWritten(t, pageTestResponse(coreHandler(t, panelTestStore(), pageTestBanner, io.Discard), r).Body.String(), r)
		for _, at := range start.FindAllStringIndex(body, -1) {
			matched := wellFormed.FindString(body[at[0]:])
			if matched == "" {
				t.Fatalf("malformed start tag at %d in %q", at[0], body)
			}
			nameEnd := 1
			for nameEnd < len(matched) && (matched[nameEnd] == '-' || matched[nameEnd] >= 'a' && matched[nameEnd] <= 'z' || matched[nameEnd] >= 'A' && matched[nameEnd] <= 'Z' || matched[nameEnd] >= '0' && matched[nameEnd] <= '9') {
				nameEnd++
			}
			name := strings.ToLower(matched[1:nameEnd])
			if name == "math" {
				t.Fatalf("foreign-content tag: %q", matched)
			}
			if name == "svg" {
				if hidden, ok := pageTestAttribute(matched, "aria-hidden"); !ok || hidden != "true" {
					t.Fatal("SVG not hidden")
				}
				var ends [][2]int
				// at covers < plus first letter; locate content from actual tag end.
				contentStart := at[0] + len(matched)
				ends = pageTestTags(body[contentStart:], "svg", true)
				if len(ends) == 0 {
					t.Fatal("SVG not closed")
				}
				svgContent := body[contentStart : contentStart+ends[0][0]]
				allowed := map[string]bool{"path": true, "g": true, "circle": true, "ellipse": true, "line": true, "polyline": true, "polygon": true, "rect": true}
				for _, inner := range regexp.MustCompile(`<[^>]*>`).FindAllString(svgContent, -1) {
					names := pageTestTagSequence(inner)
					if len(names) != 1 || !allowed[strings.ToLower(names[0])] {
						t.Fatalf("SVG child: %s", inner)
					}
					for _, attr := range coreAttributes(inner) {
						if strings.Contains(strings.ToLower(attr.value), "url(") {
							t.Fatal("SVG resource")
						}
					}
				}
				for _, attr := range coreAttributes(matched) {
					if strings.Contains(strings.ToLower(attr.value), "url(") {
						t.Fatal("SVG resource")
					}
				}
			}
			for _, entry := range attribute.FindAllStringSubmatch(matched[nameEnd:len(matched)-1], -1) {
				key := strings.ToLower(entry[1])
				value := html.UnescapeString(entry[2])
				if key == "style" || key == "ping" || key == "srcdoc" || key == "http-equiv" || strings.HasPrefix(key, "on") && len(key) > 2 && regexp.MustCompile(`^[a-z]+$`).MatchString(key[2:]) {
					t.Fatalf("forbidden attribute %q in %q", key, matched)
				}
				if name == "script" && (key == "src" || key == "href" || key == "xlink:href") {
					t.Fatalf("external script: %q", matched)
				}
				if name != "a" {
					check := func(v string) bool {
						v = strings.Map(func(c rune) rune {
							if c == '\t' || c == '\n' || c == '\r' {
								return -1
							}
							return c
						}, v)
						return v == "/" || len(v) >= 2 && v[0] == '/' && v[1] != '/' && v[1] != '\\'
					}
					switch key {
					case "href", "xlink:href", "src", "poster", "data", "background", "manifest":
						if !check(value) {
							t.Fatalf("nonlocal resource %s=%q", key, value)
						}
					case "srcset", "imagesrcset":
						for _, candidate := range strings.Split(value, ",") {
							if !check(strings.TrimLeft(candidate, " \t\n\v\f\r")) {
								t.Fatalf("nonlocal resource candidate %q", candidate)
							}
						}
					}
				}
			}
		}
	}
}

// R-3FPD-ODEL
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
		first := pageTestResponse(coreHandler(t, panelTestStore(), pageTestBanner, io.Discard), request.Clone(request.Context()))
		t.Chdir(empty)
		secondRequest := secondRequests[i]
		if request.GetBody != nil {
			secondRequest.Body, err = request.GetBody()
			if err != nil {
				t.Fatal(err)
			}
		}
		second := pageTestResponse(coreHandler(t, panelTestStore(), pageTestBanner, io.Discard), secondRequest)
		if first.Code != second.Code || !reflect.DeepEqual(first.Header(), second.Header()) || first.Body.String() != second.Body.String() {
			t.Fatalf("directory-dependent response for %s %s", request.Method, request.URL.Path)
		}
	}
}

// R-1DYY-P2E0
func pageTestContentValue(raw string) (string, bool) {
	body := pageTestStrip(raw)
	starts := pageTestTags(body, "main", false)
	if len(starts) == 0 {
		return "", false
	}
	ends := pageTestTags(body[starts[0][1]:], "main", true)
	if len(ends) == 0 {
		return "", false
	}
	return body[starts[0][1] : starts[0][1]+ends[0][0]], true
}

func pageTestContent(t *testing.T, raw string) string {
	t.Helper()
	content, ok := pageTestContentValue(raw)
	if !ok {
		t.Fatal("missing page content")
	}
	return content
}

// R-1DYY-P2E0
func TestPageContentProcedure(t *testing.T) {
	for _, tc := range []struct {
		raw, want string
		ok        bool
	}{
		{`<main><p>first</p><script>hidden</script></main><main>second</main>`, `<p>first</p>`, true},
		{`<script><main>hidden</main></script><MAIN class="content">first</MAIN>`, `first`, true},
		{`<main></main>`, "", true},
		{`</main><main>unfinished`, "", false},
		{`<mainly>other</mainly>`, "", false},
		{`<p>absent</p>`, "", false},
	} {
		if got, ok := pageTestContentValue(tc.raw); got != tc.want || ok != tc.ok {
			t.Fatalf("content of %q = (%q,%v)", tc.raw, got, ok)
		}
	}
}

// R-ZVNX-IGJO R-ZY3Q-A012
func TestPageMainAndFailureContent(t *testing.T) {
	for _, r := range pageTestDocuments() {
		w := pageTestResponse(coreHandler(t, panelTestStore(), pageTestBanner, io.Discard), r)
		body := pageTestStrip(pageTestWritten(t, w.Body.String(), r))
		starts, ends := pageTestTags(body, "main", false), pageTestTags(body, "main", true)
		bodies, bodyEnds := pageTestTags(body, "body", false), pageTestTags(body, "body", true)
		if len(starts) != 1 || len(ends) != 1 || len(bodies) != 1 || len(bodyEnds) != 1 || starts[0][1] > ends[0][0] {
			t.Fatal("main/body shape")
		}
		inside := body[bodies[0][1]:bodyEnds[0][0]]
		after := strings.TrimLeft(inside, " \t\n\v\f\r")
		main := body[starts[0][0]:ends[0][1]]
		if !strings.HasPrefix(after, main) || !formASCIIWhitespace(after[len(main):]) {
			t.Fatal("content outside main")
		}
		content := pageTestContent(t, body)
		if w.Code == 404 || w.Code == 405 || w.Code == 415 {
			message := map[int]string{404: panel.NotFoundMessage, 405: panel.MethodNotAllowedMessage, 415: panel.UnsupportedMediaTypeMessage}[w.Code]
			if !strings.Contains(pageTestNormalize(content), message) {
				t.Fatal("failure message outside main")
			}
			back := false
			for _, span := range pageTestTags(content, "a", false) {
				value, ok := pageTestAttribute(content[span[0]:span[1]], "href")
				back = back || ok && value == "/widgets"
			}
			if !back {
				t.Fatal("failure return link outside main")
			}
		}
	}
}

// R-2F4E-795R
func pageTestHeading(n int) string {
	noun := "widgets"
	if n == 1 {
		noun = "widget"
	}
	return fmt.Sprintf(`<div class="section-head"><div><h1>Widgets</h1><p id="panel-subtitle">%d %s · refreshes every 5 seconds</p></div></div>`, n, noun)
}

// R-2F4E-795R R-2HK6-YSN5
func TestPageHeadingTracksTableRows(t *testing.T) {
	for _, added := range []int{0, 1, 7} {
		store := panelTestStore()
		for i := 0; i < added; i++ {
			if _, errs := store.Create(widget.Draft{Name: fmt.Sprintf("added-%d", i), Count: 1, Status: widget.StatusActive}); errs.Any() {
				t.Fatal(errs)
			}
		}
		for _, r := range []*http.Request{pageTestRequest("GET", "/widgets"), pageTestFormRequest(widget.Submission{Count: "bad"})} {
			body := pageTestStrip(pageTestWritten(t, pageTestResponse(coreHandler(t, store, pageTestBanner, io.Discard), r).Body.String(), r))
			content := pageTestContent(t, body)
			table := formTable(t, body)
			n := 0
			for _, row := range pageTestTags(table, "tr", false) {
				ends := pageTestTags(table[row[1]:], "tr", true)
				if len(ends) > 0 && len(pageTestTags(table[row[1]:row[1]+ends[0][0]], "td", false)) > 0 {
					n++
				}
			}
			hooks := regexp.MustCompile(`<([A-Za-z][A-Za-z0-9-]*)[^>]* id="panel-subtitle"[^>]*>`).FindAllStringSubmatchIndex(content, -1)
			if len(hooks) != 1 {
				t.Fatalf("subtitle hooks: %v", hooks)
			}
			hook := hooks[0]
			name := content[hook[2]:hook[3]]
			ends := pageTestTags(content[hook[1]:], name, true)
			if len(ends) == 0 {
				t.Fatal("subtitle end missing")
			}
			noun := "widgets"
			if n == 1 {
				noun = "widget"
			}
			want := fmt.Sprintf("%d %s · refreshes every 5 seconds", n, noun)
			if pageTestNormalize(content[hook[1]:hook[1]+ends[0][0]]) != want {
				t.Fatal("subtitle count")
			}
			headings := pageTestTags(content, "h1", false)
			panels := strings.Index(content, `class="panel"`)
			if len(headings) != 1 || headings[0][0] >= hook[0] || panels < hook[1] {
				t.Fatal("subtitle position")
			}
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
							w := pageTestResponse(coreHandler(t, panelTestStore(), pageTestBanner, io.Discard), r)
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

// R-ZDDF-RWF9
func TestPageBareAttributeProcedure(t *testing.T) {
	for _, tc := range []struct {
		tag  string
		want bool
	}{{`<nav popover>`, true}, {`<nav POPOVER />`, true}, {`<nav data-popover>`, false}, {`<nav popover="auto">`, false}, {`<nav popoverx>`, false}} {
		if got := pageTestBareAttribute(tc.tag, "popover"); got != tc.want {
			t.Fatalf("%q: %v", tc.tag, got)
		}
	}
}

func pageTestBannerData(r *http.Request) page.Banner {
	return pageTestBanner(page.User{Email: r.Header.Get("X-User-Email"), ProfileURL: panel.ProfileURL(r.Host, r.Header.Get("X-Forwarded-Proto")), LogoutURL: panel.LogoutURL(r.Host, r.Header.Get("X-Forwarded-Proto"))})
}

// R-YXIQ-SVS8 R-YZYJ-KF9M
func pageTestWrittenBanner(t *testing.T, raw string, data page.Banner) {
	t.Helper()
	bodies := pageTestTags(raw, "body", false)
	if len(bodies) == 0 {
		return
	}
	var rendered bytes.Buffer
	if err := page.Templates().ExecuteTemplate(&rendered, "banner", data); err != nil {
		t.Fatal(err)
	}
	at := bodies[0][1]
	for at < len(raw) && strings.ContainsRune(" \t\n\v\f\r", rune(raw[at])) {
		at++
	}
	if !strings.HasPrefix(raw[at:], rendered.String()) {
		t.Fatalf("body does not start with exact appkit banner: %q", raw[at:])
	}
}

func pageTestWritten(t *testing.T, raw string, r *http.Request) string {
	t.Helper()
	return frameTestWritten(t, raw, pageTestBannerData(r))
}

// R-YV2Y-1CAU R-Z79X-V1PS R-YXIQ-SVS8 R-YZYJ-KF9M
func TestPageBannerSourcePerDocument(t *testing.T) {
	var users []page.User
	var drawn page.Banner
	source := func(u page.User) page.Banner {
		users = append(users, u)
		drawn = page.Banner{Service: "supplied", Email: fmt.Sprintf("fresh-%d", len(users)), ProfileURL: "/profile", LogoutURL: "/logout"}
		return drawn
	}
	h := coreHandler(t, panelTestStore(), source, io.Discard)
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

// R-ZDDF-RWF9
func pageTestBareAttribute(tag, name string) bool {
	for _, attr := range coreAttributes(tag) {
		if !attr.valued && strings.EqualFold(attr.name, name) {
			return true
		}
	}
	return false
}

func coreHandler(t *testing.T, s *widget.Store, banner func(page.User) page.Banner, stderr io.Writer) http.Handler {
	t.Helper()
	t.Setenv(services.Variable, "")
	writer, _, _ := panelTestTelemetry(t, stderr)
	return panel.Handler(s, banner, mcp.NewServer(mcp.ServerConfig{Name: panel.ServiceName, Telemetry: writer}), writer)
}

func coreDraft(sub widget.Submission) widget.Draft { d, _ := widget.ParseSubmission(sub); return d }

func coreSubmissionErrors(s *widget.Store, sub widget.Submission) widget.FieldErrors {
	d, parsed := widget.ParseSubmission(sub)
	checked := s.Check(d)
	if parsed.Name == "" {
		parsed.Name = checked.Name
	}
	if parsed.Count == "" {
		parsed.Count = checked.Count
	}
	if parsed.Status == "" {
		parsed.Status = checked.Status
	}
	return parsed
}

// R-Y5H2-05Q9 R-Y7WU-RP7N R-YACN-J8P1 R-YE0C-OJX4
func TestPageTemplateContract(t *testing.T) {
	set, err := page.Templates().ParseFS(dummy.Assets(), "*.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"page", "table", "form", "script"} {
		if set.Lookup(name) == nil {
			t.Fatalf("missing %s", name)
		}
	}
	store := panelTestStore()
	request := pageTestRequest("GET", "/widgets")
	data := panelTemplateData{Banner: pageTestBannerData(request), Panel: true, Count: len(store.All()), Table: store.All(), Form: panel.FormView{Statuses: widget.Statuses()}}
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
	store := panelTestStore()
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
