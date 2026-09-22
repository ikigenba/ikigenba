package panel

import (
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"

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

// R-L9C7-MT7L R-LAK4-0KYA R-LBS0-ECOZ R-LCZW-S4FO R-LE7T-5W6D
// R-LFFP-JNX2 R-LGNL-XFNR R-M1DW-FJ9K
func TestPagePublicDeclarations(t *testing.T) {
	constructor := Handler
	if constructor(widget.NewStore()) == nil {
		t.Fatal("nil handler")
	}
	derive := SignOutURL
	if derive("localhost", "") != LocalSignOutURL {
		t.Fatal("derivation function")
	}
	const service, missing, method, notFound, notAllowed, unsupported, signOut, local = ServiceName, MissingIdentityBody, MethodNotAllowedBody, NotFoundMessage, MethodNotAllowedMessage, UnsupportedMediaTypeMessage, SignOutText, LocalSignOutURL
	got := []string{service, missing, method, notFound, notAllowed, unsupported, signOut, local}
	want := []string{"Dummy", "identity header missing\n", "method not allowed\n", "That page was not found.", "That method is not allowed here.", "That media type is not supported.", "Sign out", "http://127.0.0.1:3001/"}
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

// R-LXQ7-A81H R-LYY3-NZS6 R-LU2I-4WTE R-IZ05-BFFI
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
				w := pageTestResponse(Handler(s), r)
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
// R-M9X7-3XGF R-MB53-HP74 R-KS39-O19S R-IZ05-BFFI
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
			w := pageTestResponse(Handler(s), r)
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
			h := Handler(widget.NewStore())
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
		h := Handler(widget.NewStore())
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

// R-IWKC-JVY4 R-KKRV-DETM R-KPNG-WHSE R-KQVD-A9J3 R-KTB6-1T0H
func TestPageDocumentChromeAndAttributes(t *testing.T) {
	for _, r := range pageTestDocuments() {
		r.Header.Set("X-User-Email", "reader &lt; <b>\" &\t \n other@example.test")
		r.Host = "dummy.a\" id=\"count-error<>&\t href=\"other:8443"
		r.Header.Set("X-Forwarded-Proto", "HTTPS")
		w := pageTestResponse(Handler(widget.NewStore()), r)
		body := w.Body.String()
		if w.Header().Get("Content-Type") != "text/html; charset=utf-8" || body == "" || r.URL.Path == "/widgets/table" {
			t.Fatalf("expected HTML document: %d %q", w.Code, body)
		}
		stripped := pageTestStrip(body)
		if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(body)), "<!doctype html>") || len(pageTestTags(stripped, "body", false)) != 1 || len(pageTestTags(stripped, "body", true)) != 1 {
			t.Fatal("HTML document frame")
		}
		visible := pageTestVisible(body)
		for _, want := range []string{ServiceName, SignOutText, strings.Join(strings.Fields(r.Header.Get("X-User-Email")), " ")} {
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
		w := pageTestResponse(Handler(widget.NewStore()), r)
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
		response := pageTestResponse(Handler(widget.NewStore()), request)
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
	fragment := pageTestResponse(Handler(widget.NewStore()), pageTestRequest(http.MethodGet, "/widgets/table"))
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
		h := Handler(store)
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

// R-LQES-ZLLB R-RNWZ-DAKG R-MDKW-98OI R-KWYV-748K
func TestPageScriptAndDocumentOrder(t *testing.T) {
	requests := append(pageTestDocuments(), pageTestRequest("GET", "/widgets/table"), pageTestRequest("POST", "/widgets/table"), pageTestRequest("GET", "/"))
	missing := pageTestRequest("GET", "/widgets")
	missing.Header.Del("X-User-Id")
	requests = append(requests, missing)
	for _, r := range requests {
		w := pageTestResponse(Handler(widget.NewStore()), r)
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
			h := Handler(s)
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
		first := pageTestResponse(Handler(widget.NewStore()), request.Clone(request.Context()))
		t.Chdir(empty)
		secondRequest := secondRequests[i]
		if request.GetBody != nil {
			secondRequest.Body, err = request.GetBody()
			if err != nil {
				t.Fatal(err)
			}
		}
		second := pageTestResponse(Handler(widget.NewStore()), secondRequest)
		if first.Code != second.Code || !reflect.DeepEqual(first.Header(), second.Header()) || first.Body.String() != second.Body.String() {
			t.Fatalf("directory-dependent response for %s %s", request.Method, request.URL.Path)
		}
	}
}
