package panel

import (
	"crypto/sha256"
	"fmt"
	"html"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/dummy"
	"github.com/ikigenba/ikigenba/dummy/internal/widget"
)

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

func pageTestAttribute(tag, name string) (string, bool) {
	re := regexp.MustCompile(`(?i)[\t\n\v\f\r ]` + regexp.QuoteMeta(name) + `="([^"]*)"`)
	match := re.FindStringSubmatch(tag)
	if match == nil {
		return "", false
	}
	return html.UnescapeString(match[1]), true
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

// R-KDGH-2SDG R-KFW9-UBUU R-KH46-83LJ R-KIC2-LVC8
// R-KLZR-R6KB R-RMP2-ZITR
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

// R-ML16-47JW R-XH4O-AY7C R-LBS0-ECOZ R-LCZW-S4FO R-LE7T-5W6D
// R-ULUZ-4YJX R-LGNL-XFNR R-M1DW-FJ9K
func TestPagePublicDeclarations(t *testing.T) {
	construct := func(f func(*widget.Store, io.Writer) http.Handler) http.Handler {
		return f(widget.NewStore(), io.Discard)
	}
	if construct(Handler) == nil {
		t.Fatal("nil handler")
	}
	derive := SignOutURL
	if derive("localhost", "") != LocalSignOutURL {
		t.Fatal("derivation function")
	}
	const service, missing, method, notFound, notAllowed, unsupported, signOut, local = ServiceName, MissingIdentityBody, MethodNotAllowedBody, NotFoundMessage, MethodNotAllowedMessage, UnsupportedMediaTypeMessage, SignOutText, LocalSignOutURL
	got := []string{service, missing, method, notFound, notAllowed, unsupported, signOut, local}
	want := []string{"dummy", "identity header missing\n", "method not allowed\n", "That page was not found.", "That method is not allowed here.", "That media type is not supported.", "Sign out", "http://localhost:3001/"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("constants: %#v", got)
	}
	for _, body := range []string{MissingIdentityBody, MethodNotAllowedBody} {
		if strings.Count(body, "\n") != 1 || !strings.HasSuffix(body, "\n") {
			t.Fatalf("plain line: %q", body)
		}
	}
}

// R-KVQY-TCHV
func TestPageSignOutURL(t *testing.T) {
	cases := []struct{ host, proto, want string }{
		{"dummy.space.test", "http", "http://auth.space.test/"},
		{"dummy.space.test:8443", "https", "https://auth.space.test/"},
		{"dummy.dummy.space.test:bad", "http", "http://auth.dummy.space.test/"},
		{"dummy.space:part:last", "http", "http://auth.space:part/"},
		{"dummy.x:", "http", "http://auth.x/"},
		{"dummy.", "http", LocalSignOutURL}, {"dummy.:80", "https", LocalSignOutURL},
		{"127.0.0.1:3000", "https", LocalSignOutURL}, {"Dummy.space", "http", LocalSignOutURL},
		{"[::1]:3000", "http", LocalSignOutURL}, {"", "http", LocalSignOutURL},
	}
	for _, proto := range []string{"", "HTTPS", "https, http", " https", "https ", "javascript:alert(1)"} {
		cases = append(cases, struct{ host, proto, want string }{"dummy.space.test", proto, "https://auth.space.test/"})
	}
	for _, tc := range cases {
		if got := SignOutURL(tc.host, tc.proto); got != tc.want {
			t.Errorf("(%q,%q): %q want %q", tc.host, tc.proto, got, tc.want)
		}
	}
}

// R-LXQ7-A81H R-LYY3-NZS6 R-LU2I-4WTE
func TestPageIdentityBeforeRouting(t *testing.T) {
	for _, path := range []string{"/", "/widgets", "/widgets/table", "/widgets/", "/unknown"} {
		for _, method := range []string{"GET", "HEAD", "POST", "PUT", "OPTIONS"} {
			for _, present := range []bool{false, true} {
				s := widget.NewStore()
				before := s.All()
				r := pageTestRequest(method, path)
				r.Header.Del("X-User-Id")
				if present {
					r.Header["X-User-Id"] = []string{""}
				}
				w := pageTestResponse(Handler(s, io.Discard), r)
				want := MissingIdentityBody
				if method == "HEAD" {
					want = ""
				}
				if w.Code != 500 || w.Header().Get("Content-Type") != "text/plain; charset=utf-8" || w.Header().Get("Allow") != "" || w.Body.String() != want {
					t.Fatalf("missing %s %s: %d %v %q", method, path, w.Code, w.Header(), w.Body.String())
				}
				if !reflect.DeepEqual(before, s.All()) {
					t.Fatal("missing identity changed store")
				}
			}
		}
	}
}

// R-ISWN-EKQ1 R-M3TP-72QY R-M51L-KUHN R-M69H-YM8C R-M8PA-Q5PQ
// R-Y0N2-FA2G R-MB53-HP74 R-KS39-O19S
func TestPageRoutes(t *testing.T) {
	cases := []struct {
		method, path   string
		status         int
		allow, message string
	}{
		{"GET", "/", 303, "", ""}, {"HEAD", "/?q=x", 303, "", ""},
		{"POST", "/", 405, "GET, HEAD", MethodNotAllowedMessage}, {"OPTIONS", "/", 405, "GET, HEAD", MethodNotAllowedMessage},
		{"GET", "/widgets", 200, "", ""}, {"GET", "/widgets?q=/unknown", 200, "", ""},
		{"PUT", "/widgets", 405, "GET, HEAD, POST", MethodNotAllowedMessage},
		{"OPTIONS", "/widgets", 405, "GET, HEAD, POST", MethodNotAllowedMessage},
		{"GET", "/unknown", 404, "", NotFoundMessage}, {"POST", "/unknown", 404, "", NotFoundMessage},
		{"HEAD", "/unknown", 404, "", NotFoundMessage}, {"DELETE", "/unknown", 404, "", NotFoundMessage},
		{"GET", "/widgets/", 404, "", NotFoundMessage}, {"POST", "/widgets/", 404, "", NotFoundMessage},
		{"GET", "/widgets/table/", 404, "", NotFoundMessage}, {"HEAD", "/widgets/table/", 404, "", NotFoundMessage},
		{"GET", "//widgets", 404, "", NotFoundMessage}, {"GET", "/Widgets", 404, "", NotFoundMessage},
		{"GET", "/widgets/table", 200, "", ""}, {"POST", "/widgets/table", 405, "GET, HEAD", ""},
	}
	for _, tc := range cases {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			s := widget.NewStore()
			before := s.All()
			r := pageTestRequest(tc.method, tc.path)
			r.Host = "unrelated.test"
			r.Header.Set("Accept", "application/json")
			r.Header.Set("X-Original-URI", "/different")
			w := pageTestResponse(Handler(s, io.Discard), r)
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
	if !strings.Contains(pageTestVisible(w.Body.String()), message) {
		t.Fatalf("failure message absent: %q", w.Body.String())
	}
	found := false
	for _, span := range pageTestTags(pageTestStrip(w.Body.String()), "a", false) {
		if href, ok := pageTestAttribute(w.Body.String()[span[0]:span[1]], "href"); ok && href == "/widgets" {
			found = true
		}
	}
	if !found {
		t.Fatal("failure missing backlink")
	}
}

// R-IVCG-647F R-JJDV-KL7M
func TestPageHeadAndOptionalEmail(t *testing.T) {
	for _, path := range []string{"/", "/widgets", "/widgets/table", "/unknown", "/widgets/", "/widgets/table/"} {
		for _, identity := range []bool{false, true} {
			h := Handler(widget.NewStore(), io.Discard)
			get := pageTestRequest("GET", path)
			if !identity {
				get.Header.Del("X-User-Id")
			}
			head := get.Clone(get.Context())
			head.Method = "HEAD"
			a, b := pageTestResponse(h, get), pageTestResponse(h, head)
			if a.Code != b.Code || !reflect.DeepEqual(a.Header(), b.Header()) || b.Body.Len() != 0 {
				t.Fatalf("HEAD %s identity=%v: %d/%d %v/%v", path, identity, a.Code, b.Code, a.Header(), b.Header())
			}
		}
		h := Handler(widget.NewStore(), io.Discard)
		absent := pageTestRequest("GET", path)
		absent.Header.Del("X-User-Email")
		empty := absent.Clone(absent.Context())
		empty.Header["X-User-Email"] = []string{""}
		a, b := pageTestResponse(h, absent), pageTestResponse(h, empty)
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

// R-IWKC-JVY4 R-KKRV-DETM R-XKSD-G9FF R-KTB6-1T0H
func TestPageDocumentChromeAndAttributes(t *testing.T) {
	for _, r := range pageTestDocuments() {
		r.Header.Set("X-User-Email", "reader &lt; <b>\" &\t \n other@example.test")
		r.Host = "dummy.a\" id=\"count-error<>&\t href=\"other:8443"
		r.Header.Set("X-Forwarded-Proto", "HTTPS")
		w := pageTestResponse(Handler(widget.NewStore(), io.Discard), r)
		body := w.Body.String()
		if w.Header().Get("Content-Type") != "text/html; charset=utf-8" || body == "" || r.URL.Path == "/widgets/table" {
			t.Fatalf("expected HTML document: %d %q", w.Code, body)
		}
		stripped := pageTestStrip(body)
		if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(body)), "<!doctype html>") || len(pageTestTags(stripped, "body", false)) != 1 || len(pageTestTags(stripped, "body", true)) != 1 {
			t.Fatal("HTML document frame")
		}
		visible := pageTestVisible(body)
		for _, want := range []string{"ikigenba", SignOutText, strings.Join(strings.Fields(r.Header.Get("X-User-Email")), " ")} {
			if !strings.Contains(visible, want) {
				t.Fatalf("chrome missing %q in %q", want, visible)
			}
		}
		found := false
		for _, span := range pageTestTags(stripped, "a", false) {
			href, ok := pageTestAttribute(stripped[span[0]:span[1]], "href")
			ends := pageTestTags(stripped[span[1]:], "a", true)
			if ok && href == SignOutURL(r.Host, r.Header.Get("X-Forwarded-Proto")) && len(ends) > 0 && pageTestNormalize(stripped[span[1]:span[1]+ends[0][0]]) == SignOutText {
				found = true
			}
		}
		if !found {
			t.Fatalf("sign out link mismatch for %q: %s", r.Host, body)
		}
		pageTestAttributeInvariants(t, body)
	}
	// The fragment and bare faults do not belong to the HTML-document class.
	for _, r := range []*http.Request{pageTestRequest("GET", "/widgets/table"), pageTestRequest("GET", "/")} {
		w := pageTestResponse(Handler(widget.NewStore(), io.Discard), r)
		if w.Body.Len() > 0 && w.Header().Get("Content-Type") == "text/html; charset=utf-8" && r.URL.Path != "/widgets/table" {
			t.Fatal("non-document misclassified")
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
	named := map[string]bool{"href": true, "id": true, "method": true, "action": true, "enctype": true, "name": true, "type": true, "value": true, "selected": true, "aria-describedby": true, "formaction": true, "formmethod": true, "formenctype": true, "src": true}
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

// R-KEOD-GK45
func TestPageNamedAttributesHaveOneQuotedOccurrence(t *testing.T) {
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
		response := pageTestResponse(Handler(widget.NewStore(), io.Discard), request)
		if response.Body.Len() == 0 || response.Header().Get("Content-Type") != "text/html; charset=utf-8" {
			t.Fatalf("expected HTML document: %d", response.Code)
		}
		pageTestAttributeInvariants(t, response.Body.String())
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
	fragment := pageTestResponse(Handler(widget.NewStore(), io.Discard), pageTestRequest(http.MethodGet, "/widgets/table"))
	if fragment.Code != http.StatusOK || fragment.Body.Len() == 0 {
		t.Fatalf("expected table fragment: %d", fragment.Code)
	}
	pageTestAttributeInvariants(t, fragment.Body.String())
}

// R-RP4V-R2B5
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
		oracle := widget.NewStore()
		_, firstErrors := oracle.Create(base)
		_, secondErrors := oracle.Create(variant)
		if firstErrors != secondErrors {
			t.Fatalf("%s: unequal validation errors", field)
		}
		store := widget.NewStore()
		before := store.All()
		h := Handler(store, io.Discard)
		a, b := pageTestResponse(h, first), pageTestResponse(h, second)
		if a.Code != b.Code || a.Code != http.StatusUnprocessableEntity || !reflect.DeepEqual(before, store.All()) {
			t.Fatalf("%s: comparison preconditions failed", field)
		}
		if !reflect.DeepEqual(pageTestTagSequence(a.Body.String()), pageTestTagSequence(b.Body.String())) {
			t.Errorf("%s changed tag-name sequence", field)
		}
		if strings.Count(a.Body.String(), ">") != strings.Count(b.Body.String(), ">") {
			t.Errorf("%s changed greater-than count", field)
		}
	}
}

// R-Y32V-6TJU R-Y4AR-KLAJ R-6TWQ-SJY0 R-KWYV-748K
func TestPageScriptAndDocumentOrder(t *testing.T) {
	requests := append(pageTestDocuments(), pageTestRequest("GET", "/widgets/table"), pageTestRequest("POST", "/widgets/table"), pageTestRequest("GET", "/"))
	missing := pageTestRequest("GET", "/widgets")
	missing.Header.Del("X-User-Id")
	requests = append(requests, missing)
	for _, r := range requests {
		w := pageTestResponse(Handler(widget.NewStore(), io.Discard), r)
		body := w.Body.String()
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
		for _, literal := range []string{"/widgets/table", "widgets-table"} {
			if !strings.Contains(script, literal) {
				t.Fatalf("script lacks %s", literal)
			}
		}
		tables := pageTestTags(body, "table", false)
		tableEnds := pageTestTags(body, "table", true)
		if len(tables) != 1 || len(tableEnds) != 1 || (starts[0][0] > tables[0][0] && starts[0][0] < tableEnds[0][1]) {
			t.Fatal("script inside table")
		}
		forms := pageTestTags(stripped, "form", false)
		strippedEnds := pageTestTags(stripped, "table", true)
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
			s := widget.NewStore()
			_, baseErr := s.Create(base)
			_, variantErr := s.Create(variant)
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
			h := Handler(s, io.Discard)
			first := pageTestResponse(h, a)
			second := pageTestResponse(h, b)
			if first.Code != 422 || second.Code != 422 {
				t.Fatal("test needs two panel pages")
			}
			if !reflect.DeepEqual(pageTestTagSequence(first.Body.String()), pageTestTagSequence(second.Body.String())) || strings.Count(first.Body.String(), ">") != strings.Count(second.Body.String(), ">") {
				t.Fatalf("%s contributed markup: %q", field, attack)
			}
			pageTestAttributeInvariants(t, second.Body.String())
			if field == "email" && !strings.Contains(pageTestVisible(second.Body.String()), strings.Join(strings.Fields(attack), " ")) {
				t.Fatal("email bytes lost")
			}
		}
	}
}

// R-XICK-OPY1 R-XJKH-2HOQ R-XKSD-G9FF
func TestPageChromeHeader(t *testing.T) {
	for _, r := range pageTestDocuments() {
		for _, email := range []string{"", " one\t& <two>  three "} {
			r.Header.Set("X-User-Email", email)
			body := pageTestStrip(pageTestResponse(Handler(widget.NewStore(), io.Discard), r).Body.String())
			bodies := pageTestTags(body, "body", false)
			if len(bodies) != 1 {
				t.Fatalf("body start count: %d", len(bodies))
			}
			headers := pageTestTags(body, "header", false)
			if len(headers) < 1 || strings.TrimSpace(body[bodies[0][1]:headers[0][0]]) != "" {
				t.Fatal("chrome header does not immediately follow body")
			}
			ends := pageTestTags(body[headers[0][1]:], "header", true)
			if len(ends) == 0 {
				t.Fatal("chrome header has no end")
			}
			chrome := body[headers[0][0] : headers[0][1]+ends[0][1]]
			if len(pageTestTags(chrome, "header", false)) != 1 {
				t.Fatal("nested header in chrome")
			}
			strong, spans, links := pageTestTags(chrome, "strong", false), pageTestTags(chrome, "span", false), pageTestTags(chrome, "a", false)
			if len(strong) != 1 || len(spans) != 1 || len(links) != 1 || strong[0][0] >= spans[0][0] || spans[0][0] >= links[0][0] {
				t.Fatalf("chrome order: %q", chrome)
			}
			if class, _ := pageTestAttribute(chrome[strong[0][0]:strong[0][1]], "class"); class != "mark" {
				t.Fatal("mark class")
			}
			if service, _ := pageTestAttribute(chrome[strong[0][0]:strong[0][1]], "data-service"); service != ServiceName {
				t.Fatal("mark service")
			}
			for _, item := range []struct {
				open      [2]int
				tag, want string
			}{
				{strong[0], "strong", "ikigenba"},
				{spans[0], "span", strings.Join(strings.Fields(email), " ")},
				{links[0], "a", SignOutText},
			} {
				closingTags := pageTestTags(chrome[item.open[1]:], item.tag, true)
				if len(closingTags) == 0 || pageTestNormalize(chrome[item.open[1]:item.open[1]+closingTags[0][0]]) != item.want {
					t.Fatalf("chrome %s text: %q", item.tag, chrome)
				}
			}
			if href, _ := pageTestAttribute(chrome[links[0][0]:links[0][1]], "href"); href != SignOutURL(r.Host, r.Header.Get("X-Forwarded-Proto")) {
				t.Fatal("sign out URL")
			}
		}
	}
}

// R-XM09-U164 R-XN86-7SWT R-XOG2-LKNI R-U98Z-WRRS
func TestPageDocumentHeadAndServiceSpelling(t *testing.T) {
	for _, r := range pageTestDocuments() {
		body := pageTestResponse(Handler(widget.NewStore(), io.Discard), r).Body.String()
		stripped := pageTestStrip(body)
		bodyStart := pageTestTags(stripped, "body", false)
		if len(bodyStart) != 1 {
			t.Fatal("missing body")
		}
		for _, tc := range []struct{ tag, attr, value string }{
			{"title", "", ServiceName},
			{"link", "rel", "stylesheet"},
		} {
			starts := pageTestTags(stripped, tc.tag, false)
			if len(starts) != 1 || starts[0][1] > bodyStart[0][0] {
				t.Fatalf("%s count or position: %q", tc.tag, body)
			}
			if tc.attr != "" {
				if value, _ := pageTestAttribute(stripped[starts[0][0]:starts[0][1]], tc.attr); value != tc.value {
					t.Fatalf("%s %s=%q", tc.tag, tc.attr, value)
				}
			}
		}
		titles := pageTestTags(stripped, "title", false)
		titleEnds := pageTestTags(stripped, "title", true)
		if len(titleEnds) != 1 || titleEnds[0][0] < titles[0][1] || titleEnds[0][1] > bodyStart[0][0] || pageTestNormalize(stripped[titles[0][1]:titleEnds[0][0]]) != ServiceName {
			t.Fatal("title shape")
		}
		link := pageTestTags(stripped, "link", false)[0]
		if href, _ := pageTestAttribute(stripped[link[0]:link[1]], "href"); href != "/assets/theme.css" {
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

// R-XS3R-QVVL R-XTBO-4NMA R-XVRG-W73O R-XWZD-9YUD R-XY79-NQL2 R-XZF6-1IBR
func TestPagePanelLayout(t *testing.T) {
	for _, r := range []*http.Request{pageTestRequest("GET", "/widgets"), pageTestFormRequest(widget.Submission{Name: "", Count: "bad", Status: "archived"})} {
		body := pageTestStrip(pageTestResponse(Handler(widget.NewStore(), io.Discard), r).Body.String())
		headers, headerEnds := pageTestTags(body, "header", false), pageTestTags(body, "header", true)
		headings, headingEnds := pageTestTags(body, "h1", false), pageTestTags(body, "h1", true)
		if len(headers) < 1 || len(headerEnds) < 1 || len(headings) != 1 || len(headingEnds) != 1 || headerEnds[0][1] > headings[0][0] || headings[0][1] > headingEnds[0][0] || pageTestNormalize(body[headings[0][1]:headingEnds[0][0]]) != "Widgets" {
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
		outside := body[bodyStarts[0][1]:headers[0][0]] + body[headerEnds[0][1]:panels[0][0]] + body[panelEnd+len("</div>"):bodyEnds[0][0]]
		if pageTestNormalize(outside) != "Widgets" {
			t.Fatalf("extra outside text: %q", outside)
		}
	}
}

// R-F30C-CO4I R-F488-QFV7 R-6TWQ-SJY0 R-F7VX-VR3A R-F5G5-47LW R-F6O1-HZCL
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
		body := pageTestResponse(Handler(widget.NewStore(), io.Discard), r).Body.String()
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
			if name == "svg" || name == "math" {
				t.Fatalf("foreign-content tag: %q", matched)
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

// R-Y5IN-YD18 R-Y1UY-T1T5
func TestPageAssetRouteAndMissingIdentityETag(t *testing.T) {
	for _, path := range []string{"/", "/widgets", "/widgets/table", "/assets/theme.css", "/assets/OFL.txt", "/unknown"} {
		for _, method := range []string{"GET", "HEAD", "POST"} {
			r := pageTestRequest(method, path)
			r.Header.Del("X-User-Id")
			w := pageTestResponse(Handler(widget.NewStore(), io.Discard), r)
			if w.Code != 500 || w.Header().Get("ETag") != "" {
				t.Fatalf("missing identity %s %s: %d %v", method, path, w.Code, w.Header())
			}
			if strings.HasPrefix(path, "/assets/") {
				r.Header.Set("X-User-Id", "caller")
				w = pageTestResponse(Handler(widget.NewStore(), io.Discard), r)
				if w.Code == 404 {
					t.Fatalf("asset path treated as unknown: %s %s", method, path)
				}
			}
		}
	}
}

// R-JKLR-YCYB
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
		first := pageTestResponse(Handler(widget.NewStore(), io.Discard), request.Clone(request.Context()))
		t.Chdir(empty)
		secondRequest := secondRequests[i]
		if request.GetBody != nil {
			secondRequest.Body, err = request.GetBody()
			if err != nil {
				t.Fatal(err)
			}
		}
		second := pageTestResponse(Handler(widget.NewStore(), io.Discard), secondRequest)
		if first.Code != second.Code || !reflect.DeepEqual(first.Header(), second.Header()) || first.Body.String() != second.Body.String() {
			t.Fatalf("directory-dependent response for %s %s", request.Method, request.URL.Path)
		}
	}
}

// R-5JK4-98EF R-5KS0-N054 R-5LZX-0RVT R-5N7T-EJMI R-5OFP-SBD7
// R-5PNM-633W R-5QVI-JUUL R-5S3E-XMLA R-5TBB-BEBZ R-5UJ7-P62O
func TestEmbeddedAssetResponses(t *testing.T) {
	entries, err := fs.ReadDir(dummy.Assets, "assets")
	if err != nil {
		t.Fatal(err)
	}
	store := widget.NewStore()
	h := Handler(store, io.Discard)
	present := make(map[string]bool, len(entries))
	for _, entry := range entries {
		present[entry.Name()] = true
	}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			t.Fatal(err)
		}
		if !info.Mode().IsRegular() {
			continue
		}
		name := entry.Name()
		body, err := dummy.Assets.ReadFile("assets/" + name)
		if err != nil {
			t.Fatal(err)
		}
		path := "/assets/" + name
		wantType := "application/octet-stream"
		switch {
		case strings.HasSuffix(name, ".css"):
			wantType = "text/css; charset=utf-8"
		case strings.HasSuffix(name, ".woff2"):
			wantType = "font/woff2"
		case strings.HasSuffix(name, ".txt"):
			wantType = "text/plain; charset=utf-8"
		}
		etag := fmt.Sprintf(`"%x"`, sha256.Sum256(body))
		base := pageTestResponse(h, pageTestRequest(http.MethodGet, path))
		if base.Code != 200 || !slices.Equal(base.Body.Bytes(), body) || base.Header().Get("Content-Type") != wantType || base.Header().Get("ETag") != etag || !reflect.DeepEqual(base.Header().Values("Cache-Control"), []string{"no-cache"}) {
			t.Fatalf("%s: status=%d headers=%v body length=%d", path, base.Code, base.Header(), base.Body.Len())
		}
		for _, tc := range []struct {
			name   string
			fields []string
			match  bool
		}{
			{"absent", nil, false},
			{"empty", []string{""}, false},
			{"exact", []string{etag}, true},
			{"weak", []string{"W/" + etag}, true},
			{"wildcard", []string{"*"}, true},
			{"later-field", []string{`"stale"`, " \tW/" + etag + " "}, true},
			{"middle-entry", []string{`"stale", ` + etag + `, "other"`}, true},
			{"quoted-star", []string{`"*"`}, false},
			{"stale", []string{`"stale", W/"other"`}, false},
			{"unquoted", []string{strings.Trim(etag, `"`)}, false},
		} {
			t.Run(name+"/"+tc.name, func(t *testing.T) {
				var get *httptest.ResponseRecorder
				for _, method := range []string{http.MethodGet, http.MethodHead} {
					r := pageTestRequest(method, path)
					if tc.fields != nil {
						r.Header["If-None-Match"] = tc.fields
					}
					answer := pageTestResponse(h, r)
					if method == http.MethodGet {
						get = answer
					}
					wantStatus := 200
					if tc.match {
						wantStatus = 304
					}
					if answer.Code != wantStatus || answer.Header().Get("ETag") != etag || !reflect.DeepEqual(answer.Header().Values("Cache-Control"), []string{"no-cache"}) {
						t.Errorf("%s response: %d %v", method, answer.Code, answer.Header())
					}
					if method == http.MethodHead || tc.match {
						if answer.Body.Len() != 0 {
							t.Error("body must be empty")
						}
					} else if !slices.Equal(answer.Body.Bytes(), body) || !reflect.DeepEqual(answer.Header(), base.Header()) {
						t.Error("nonmatching condition changed response")
					}
					if method == http.MethodHead && (get.Code != answer.Code || !reflect.DeepEqual(get.Header(), answer.Header())) {
						t.Error("HEAD does not mirror GET")
					}
				}
			})
		}
		for _, method := range []string{"POST", "PUT", "DELETE", "OPTIONS"} {
			r := pageTestRequest(method, path)
			r.Header.Set("If-None-Match", "*")
			answer := pageTestResponse(h, r)
			if answer.Code != 405 || answer.Header().Get("Allow") != "GET, HEAD" || answer.Header().Get("ETag") != "" {
				t.Errorf("%s %s: %d %v", method, path, answer.Code, answer.Header())
			}
			pageTestFailure(t, answer, r, MethodNotAllowedMessage)
		}
	}
	// A percent-encoded spelling is decoded into URL.Path before routing.
	encoded := pageTestResponse(h, pageTestRequest("GET", "/assets/%74heme.css"))
	plain := pageTestResponse(h, pageTestRequest("GET", "/assets/theme.css"))
	if encoded.Code != 200 || !reflect.DeepEqual(encoded.Header(), plain.Header()) || encoded.Body.String() != plain.Body.String() {
		t.Error("decoded name did not select the same asset")
	}
	absent := "missing"
	for present[absent] {
		absent += "x"
	}
	missingPaths := []string{"/assets/", "/assets/" + absent, "/assets/theme.css/extra", "/assets/theme.css%2Fextra", "/assets/.."}
	if !present["THEME.css"] {
		missingPaths = append(missingPaths, "/assets/THEME.css")
	}
	for _, path := range missingPaths {
		for _, method := range []string{"GET", "HEAD", "POST"} {
			r := pageTestRequest(method, path)
			r.Header.Set("If-None-Match", "*")
			answer := pageTestResponse(h, r)
			if answer.Code != 404 || answer.Header().Get("ETag") != "" {
				t.Errorf("%s %s: %d %v", method, path, answer.Code, answer.Header())
			}
			pageTestFailure(t, answer, r, NotFoundMessage)
		}
	}
	missing := pageTestRequest("GET", "/assets/theme.css")
	missing.Header.Del("X-User-Id")
	answer := pageTestResponse(h, missing)
	if answer.Code != 500 || answer.Header().Get("ETag") != "" || answer.Body.String() != MissingIdentityBody {
		t.Errorf("identity failure: %d %v %q", answer.Code, answer.Header(), answer.Body.String())
	}
}

// R-5LZX-0RVT: The embedded inventory need not contain every extension.
func TestAssetContentTypeFallback(t *testing.T) {
	for _, tc := range []struct{ name, want string }{
		{"theme.css", "text/css; charset=utf-8"},
		{"font.woff2", "font/woff2"},
		{"OFL.txt", "text/plain; charset=utf-8"},
		{"file", "application/octet-stream"},
		{"file.bin", "application/octet-stream"},
		{"file.CSS", "application/octet-stream"},
		{"file.css.bin", "application/octet-stream"},
	} {
		if got := assetContentType(tc.name); got != tc.want {
			t.Errorf("%q: %q, want %q", tc.name, got, tc.want)
		}
	}
}

func cssPreprocess(source []byte) []rune {
	runes := []rune(strings.TrimPrefix(string(source), "\ufeff"))
	out := make([]rune, 0, len(runes))
	for i, r := range runes {
		switch r {
		case '\x00':
			r = '\ufffd'
		case '\r':
			if i+1 < len(runes) && runes[i+1] == '\n' {
				continue
			}
			r = '\n'
		case '\f':
			r = '\n'
		}
		out = append(out, r)
	}
	return out
}

func cssHex(r rune) bool   { return r >= '0' && r <= '9' || r >= 'a' && r <= 'f' || r >= 'A' && r <= 'F' }
func cssSpace(r rune) bool { return r == ' ' || r == '\n' || r == '\t' }
func cssNameStart(r rune) bool {
	return r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= 0x80
}
func cssName(r rune) bool                 { return cssNameStart(r) || r == '-' || r >= '0' && r <= '9' }
func cssValidEscape(s []rune, i int) bool { return i+1 < len(s) && s[i] == '\\' && s[i+1] != '\n' }
func cssIdentStart(s []rune, i int) bool {
	if i >= len(s) {
		return false
	}
	if s[i] == '-' {
		return i+1 < len(s) && (cssNameStart(s[i+1]) || s[i+1] == '-' || cssValidEscape(s, i+1))
	}
	return cssNameStart(s[i]) || cssValidEscape(s, i)
}
func cssEscape(s []rune, i int) (rune, int) {
	if i+1 >= len(s) {
		return '\ufffd', i + 1
	}
	i++
	if !cssHex(s[i]) {
		return s[i], i + 1
	}
	start := i
	for i < len(s) && i < start+6 && cssHex(s[i]) {
		i++
	}
	var value rune
	for _, r := range s[start:i] {
		value *= 16
		switch {
		case r >= '0' && r <= '9':
			value += r - '0'
		case r >= 'a' && r <= 'f':
			value += r - 'a' + 10
		default:
			value += r - 'A' + 10
		}
	}
	if i < len(s) && cssSpace(s[i]) {
		i++
	}
	if value == 0 || value > 0x10ffff || value >= 0xd800 && value <= 0xdfff {
		value = '\ufffd'
	}
	return value, i
}
func cssConsumeName(s []rune, i int) (string, int) {
	var value strings.Builder
	for i < len(s) {
		switch {
		case cssName(s[i]):
			value.WriteRune(s[i])
			i++
		case cssValidEscape(s, i):
			r, next := cssEscape(s, i)
			value.WriteRune(r)
			i = next
		default:
			return value.String(), i
		}
	}
	return value.String(), i
}
func cssConsumeString(s []rune, i int) (string, int, bool) {
	quote := s[i]
	var value strings.Builder
	for i++; i < len(s); {
		switch {
		case s[i] == quote:
			return value.String(), i + 1, true
		case s[i] == '\n':
			return "", i, false // bad-string-token
		case s[i] == '\\' && i+1 < len(s) && s[i+1] == '\n':
			i += 2
		case cssValidEscape(s, i):
			r, next := cssEscape(s, i)
			value.WriteRune(r)
			i = next
		default:
			value.WriteRune(s[i])
			i++
		}
	}
	return value.String(), i, true
}
func cssConsumeURL(s []rune, i int) (string, int, bool) {
	var value strings.Builder
	for i < len(s) {
		switch {
		case s[i] == ')':
			return value.String(), i + 1, true
		case cssSpace(s[i]):
			for i < len(s) && cssSpace(s[i]) {
				i++
			}
			if i == len(s) || s[i] == ')' {
				if i < len(s) {
					i++
				}
				return value.String(), i, true
			}
			return "", cssSkipBadURL(s, i), false
		case s[i] == '"' || s[i] == '\'' || s[i] == '(' || s[i] < 0x20 || s[i] == 0x7f || s[i] == '\\' && !cssValidEscape(s, i):
			return "", cssSkipBadURL(s, i), false
		case cssValidEscape(s, i):
			r, next := cssEscape(s, i)
			value.WriteRune(r)
			i = next
		default:
			value.WriteRune(s[i])
			i++
		}
	}
	return value.String(), i, true
}
func cssSkipBadURL(s []rune, i int) int {
	for i < len(s) && s[i] != ')' {
		if cssValidEscape(s, i) {
			_, i = cssEscape(s, i)
		} else {
			i++
		}
	}
	if i < len(s) {
		i++
	}
	return i
}
func cssDigit(r rune) bool { return r >= '0' && r <= '9' }
func cssNumberStart(s []rune, i int) bool {
	if i >= len(s) {
		return false
	}
	if s[i] == '+' || s[i] == '-' {
		i++
	}
	return i < len(s) && (cssDigit(s[i]) || s[i] == '.' && i+1 < len(s) && cssDigit(s[i+1]))
}
func cssConsumeNumber(s []rune, i int) int {
	if s[i] == '+' || s[i] == '-' {
		i++
	}
	for i < len(s) && cssDigit(s[i]) {
		i++
	}
	if i+1 < len(s) && s[i] == '.' && cssDigit(s[i+1]) {
		i++
		for i < len(s) && cssDigit(s[i]) {
			i++
		}
	}
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		exponent := i + 1
		if exponent < len(s) && (s[exponent] == '+' || s[exponent] == '-') {
			exponent++
		}
		if exponent < len(s) && cssDigit(s[exponent]) {
			i = exponent + 1
			for i < len(s) && cssDigit(s[i]) {
				i++
			}
		}
	}
	return i
}
func cssReferenceAllowed(value string) bool {
	value = strings.TrimFunc(value, func(r rune) bool { return r >= 0 && r <= 0x20 })
	value = strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' || r == '\r' {
			return -1
		}
		return r
	}, value)
	if colon := strings.IndexByte(value, ':'); colon >= 0 && strings.EqualFold(value[:colon], "data") {
		return true
	}
	runes := []rune(value)
	if len(runes) >= 2 && (runes[0] == '/' || runes[0] == '\\') && (runes[1] == '/' || runes[1] == '\\') {
		return false
	}
	for _, r := range runes {
		if r == ':' {
			return false
		}
		if r == '/' || r == '\\' || r == '?' || r == '#' {
			return true
		}
	}
	return true
}
func cssReferenceValues(source []byte) []string {
	s := cssPreprocess(source)
	var values []string
	for i := 0; i < len(s); {
		switch {
		case i+1 < len(s) && s[i] == '/' && s[i+1] == '*':
			i += 2
			for i+1 < len(s) && (s[i] != '*' || s[i+1] != '/') {
				i++
			}
			if i+1 < len(s) {
				i += 2
			} else {
				i = len(s)
			}
		case s[i] == '"' || s[i] == '\'':
			value, next, ok := cssConsumeString(s, i)
			if ok {
				values = append(values, value)
			}
			i = next
		case cssNumberStart(s, i):
			i = cssConsumeNumber(s, i)
			if cssIdentStart(s, i) {
				_, i = cssConsumeName(s, i) // dimension-token, not a following url-token
			} else if i < len(s) && s[i] == '%' {
				i++
			}
		case (s[i] == '#' || s[i] == '@') && cssIdentStart(s, i+1):
			_, i = cssConsumeName(s, i+1) // hash-token or at-keyword-token
		case cssIdentStart(s, i):
			name, next := cssConsumeName(s, i)
			i = next
			if !strings.EqualFold(name, "url") || i >= len(s) || s[i] != '(' {
				continue
			}
			i++
			for i < len(s) && cssSpace(s[i]) {
				i++
			}
			if i < len(s) && (s[i] == '"' || s[i] == '\'') {
				continue // function-token; next string-token is scanned normally
			}
			value, next, ok := cssConsumeURL(s, i)
			if ok {
				values = append(values, value)
			}
			i = next
		default:
			i++
		}
	}
	return values
}

// R-8VF0-T1ME
func TestStylesheetReferencesStayOnOrigin(t *testing.T) {
	fixtures := []struct {
		css     string
		values  []string
		allowed bool
	}{
		{`a{background:url(../font.woff2)}`, []string{"../font.woff2"}, true},
		{`a{background:URL("DATA:image/svg+xml,a:b")}`, []string{"DATA:image/svg+xml,a:b"}, true},
		{`/* url(https://ignored.test/) */ a{background:url(local.svg)}`, []string{"local.svg"}, true},
		{`a{background:url(https://elsewhere.test/x)}`, []string{"https://elsewhere.test/x"}, false},
		{`a{background:url(//elsewhere.test/x)}`, []string{"//elsewhere.test/x"}, false},
		{`a{background:u\72l(\\\\elsewhere.test/x)}`, []string{`\\elsewhere.test/x`}, false},
		{`a{--x:"/\9 /elsewhere.test/x"}`, []string{"/\t/elsewhere.test/x"}, false},
		{`@import "https://elsewhere.test/x";`, []string{"https://elsewhere.test/x"}, false},
		{`a{--x:5url(https://elsewhere.test/x)}`, nil, true},
		{`a{--x:1e2url(https://elsewhere.test/x)}`, nil, true},
		{`a{--x:#url(https://elsewhere.test/x)}`, nil, true},
		{`@url(https://elsewhere.test/x)`, nil, true},
	}
	for _, tc := range fixtures {
		values := cssReferenceValues([]byte(tc.css))
		if !slices.Equal(values, tc.values) {
			t.Errorf("fixture %q: tokens=%q, want %q", tc.css, values, tc.values)
		}
		allowed := true
		for _, value := range values {
			allowed = allowed && cssReferenceAllowed(value)
		}
		if allowed != tc.allowed {
			t.Errorf("fixture %q: tokens=%q allowed=%v", tc.css, values, allowed)
		}
	}
	entries, err := fs.ReadDir(dummy.Assets, "assets")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".css") {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			t.Fatal(err)
		}
		if !info.Mode().IsRegular() {
			continue
		}
		content, err := dummy.Assets.ReadFile("assets/" + entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		for _, value := range cssReferenceValues(content) {
			if !cssReferenceAllowed(value) {
				t.Errorf("%s has external reference %q", entry.Name(), value)
			}
		}
	}
}
