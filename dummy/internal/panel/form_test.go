package panel

import (
	"errors"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/dummy/internal/widget"
)

func formRequest(h http.Handler, method, target, mediaType, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	r.Header.Set("X-User-Id", "form-user")
	r.Header.Set("X-User-Email", "form-user@example.test")
	r.Header.Set("Content-Type", mediaType)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func formBody(sub widget.Submission) string {
	return url.Values{"name": {sub.Name}, "count": {sub.Count}, "status": {sub.Status}}.Encode()
}

func formSpan(t *testing.T, body string) string {
	t.Helper()
	body = pageTestStrip(body)
	starts, ends := pageTestTags(body, "form", false), pageTestTags(body, "form", true)
	if len(starts) != 1 || len(ends) != 1 || starts[0][1] > ends[0][0] {
		t.Fatalf("expected one ordered form pair: %q", body)
	}
	return body[starts[0][0]:ends[0][1]]
}

func formTags(body, name string) []string {
	var result []string
	for _, span := range pageTestTags(body, name, false) {
		result = append(result, body[span[0]:span[1]])
	}
	return result
}

func formControls(t *testing.T, body string) map[string]string {
	t.Helper()
	controls := make(map[string]string)
	for _, element := range []string{"input", "select", "textarea"} {
		for _, tag := range formTags(body, element) {
			name, ok := pageTestAttribute(tag, "name")
			if !ok {
				continue
			}
			if _, seen := controls[name]; seen || (name != "name" && name != "count" && name != "status") {
				t.Fatalf("duplicate or additional named control: %s", tag)
			}
			if element != "input" && name != "status" || element != "select" && name == "status" {
				t.Fatalf("wrong control element: %s", tag)
			}
			controls[name] = tag
		}
	}
	if len(controls) != 3 {
		t.Fatalf("named controls = %#v", controls)
	}
	return controls
}

// R-K67P-4EUD R-K7FL-I6L2 R-K8NH-VYBR R-UC44-S8EO R-M49S-JQ63
func assertFormMarkup(t *testing.T, body string) map[string]string {
	t.Helper()
	form := formSpan(t, body)
	start := formTags(form, "form")[0]
	method, _ := pageTestAttribute(start, "method")
	action, _ := pageTestAttribute(start, "action")
	encoding, hasEncoding := pageTestAttribute(start, "enctype")
	if !strings.EqualFold(method, "post") || action != "/widgets" || hasEncoding && !strings.EqualFold(encoding, "application/x-www-form-urlencoded") {
		t.Fatalf("invalid form submission attributes: %s", start)
	}
	controls := formControls(t, form)
	selectStart := pageTestTags(form, "select", false)[0]
	selectEnds := pageTestTags(form[selectStart[1]:], "select", true)
	if len(selectEnds) != 1 {
		t.Fatalf("status select has %d ends", len(selectEnds))
	}
	selectBody := form[selectStart[1] : selectStart[1]+selectEnds[0][0]]
	options := formTags(selectBody, "option")
	statuses := widget.Statuses()
	if len(options) != len(statuses) {
		t.Fatalf("status options = %v", options)
	}
	for i, tag := range options {
		value, ok := pageTestAttribute(tag, "value")
		if !ok || value != string(statuses[i]) {
			t.Fatalf("option %d value = %q, want %q", i, value, statuses[i])
		}
	}
	submits := 0
	for _, element := range []string{"input", "button"} {
		for _, tag := range formTags(form, element) {
			typ, _ := pageTestAttribute(tag, "type")
			if element == "input" && strings.EqualFold(typ, "image") {
				t.Fatalf("image submission control: %s", tag)
			}
			if element == "input" && !strings.EqualFold(typ, "submit") || element == "button" && (strings.EqualFold(typ, "reset") || strings.EqualFold(typ, "button")) {
				continue
			}
			submits++
			for _, forbidden := range []string{"name", "formaction", "formmethod", "formenctype"} {
				if _, ok := pageTestAttribute(tag, forbidden); ok {
					t.Errorf("submit control overrides %s: %s", forbidden, tag)
				}
			}
		}
	}
	if submits == 0 {
		t.Fatal("no submit control")
	}
	return controls
}

// R-K9VE-9Q2G
func TestFormStatusOptionsFollowStatuses(t *testing.T) {
	for _, sub := range []widget.Submission{{}, {Name: "", Count: "bad", Status: "paused"}} {
		method := http.MethodGet
		body := ""
		if sub.Count != "" {
			method = http.MethodPost
			body = formBody(sub)
		}
		response := formRequest(Handler(widget.NewStore()), method, "/widgets", "application/x-www-form-urlencoded", body)
		form := formSpan(t, response.Body.String())
		var statusSelect string
		for _, span := range pageTestTags(form, "select", false) {
			start := form[span[0]:span[1]]
			name, _ := pageTestAttribute(start, "name")
			if name != "status" {
				continue
			}
			if statusSelect != "" {
				t.Fatal("multiple status selects")
			}
			end := pageTestTags(form[span[1]:], "select", true)
			if len(end) == 0 {
				t.Fatal("status select has no end tag")
			}
			statusSelect = form[span[0] : span[1]+end[0][1]]
		}
		if statusSelect == "" {
			t.Fatal("status select missing")
		}
		options, statuses := formTags(statusSelect, "option"), widget.Statuses()
		if len(options) != 3 || len(statuses) != 3 {
			t.Fatalf("status options = %d, statuses = %d", len(options), len(statuses))
		}
		for i, option := range options {
			value, ok := pageTestAttribute(option, "value")
			if !ok || value != string(statuses[i]) {
				t.Errorf("option %d value = %q, want %q", i, value, statuses[i])
			}
		}
	}
}

// R-4NOW-M9YH
func TestFormRejectedStatusSelection(t *testing.T) {
	for _, status := range []string{"active", " paused ", "retired", "archived", "", "PAUSED"} {
		sub := widget.Submission{Name: "", Count: "1", Status: status}
		response := formRequest(Handler(widget.NewStore()), http.MethodPost, "/widgets", "application/x-www-form-urlencoded", formBody(sub))
		if response.Code != http.StatusUnprocessableEntity {
			t.Fatalf("status %q: response = %d", status, response.Code)
		}
		selectedCount := 0
		for _, option := range formTags(formSpan(t, response.Body.String()), "option") {
			value, _ := pageTestAttribute(option, "value")
			_, selected := pageTestAttribute(option, "selected")
			wantSelected := value == strings.TrimSpace(status)
			if selected != wantSelected {
				t.Errorf("status %q: option %q selected=%v, want %v", status, value, selected, wantSelected)
			}
			if selected {
				selectedCount++
			}
		}
		wantCount := 0
		for _, allowed := range widget.Statuses() {
			if string(allowed) == strings.TrimSpace(status) {
				wantCount = 1
			}
		}
		if selectedCount != wantCount {
			t.Errorf("status %q: selected options = %d, want %d", status, selectedCount, wantCount)
		}
	}
}

// R-XE11-R0UN R-W2WT-9FY8 R-KEQZ-ST18 R-KFYW-6KRX
func assertFormErrors(t *testing.T, body string, controls map[string]string, errs widget.FieldErrors) {
	t.Helper()
	stripped := pageTestStrip(body)
	starts := regexp.MustCompile(`(?i)<[a-z][a-z0-9]*[^>]*>`).FindAllStringIndex(stripped, -1)
	for field, want := range map[string]string{"name": errs.Name, "count": errs.Count, "status": errs.Status} {
		count := 0
		for _, span := range starts {
			id, _ := pageTestAttribute(stripped[span[0]:span[1]], "id")
			if id != field+"-error" {
				continue
			}
			count++
			rest := stripped[span[1]:]
			end := strings.IndexByte(rest, '<')
			if end < 0 || !strings.HasPrefix(rest[end:], "</") {
				t.Fatalf("%s error contains child element or lacks end tag", field)
			}
			if got := pageTestNormalize(rest[:end]); got != want {
				t.Errorf("%s error = %q, want %q", field, got, want)
			}
		}
		aria, hasAria := pageTestAttribute(controls[field], "aria-describedby")
		if want == "" {
			if count != 0 || hasAria {
				t.Errorf("accepted %s has error tags=%d aria=%q", field, count, aria)
			}
		} else if count != 1 || !hasAria || aria != field+"-error" {
			t.Errorf("rejected %s has error tags=%d aria=%q", field, count, aria)
		}
	}
}

// R-OPQ1-FGDT R-W44P-N7OX
func TestFormFreshPagesAndFailures(t *testing.T) {
	for _, tc := range []struct{ method, path, contentType string }{
		{http.MethodGet, "/widgets", ""},
		{http.MethodGet, "/absent", ""},
		{http.MethodDelete, "/widgets", ""},
		{http.MethodPost, "/widgets", "application/json"},
	} {
		t.Run(tc.method+tc.path+tc.contentType, func(t *testing.T) {
			w := formRequest(Handler(widget.NewStore()), tc.method, tc.path, tc.contentType, "")
			body := w.Body.String()
			controls := make(map[string]string)
			if w.Code == http.StatusOK {
				controls = assertFormMarkup(t, body)
				for _, field := range []string{"name", "count"} {
					if value, _ := pageTestAttribute(controls[field], "value"); value != "" {
						t.Errorf("fresh %s value = %q", field, value)
					}
				}
				for _, option := range formTags(formSpan(t, body), "option") {
					if _, ok := pageTestAttribute(option, "selected"); ok {
						t.Errorf("fresh page selects %s", option)
					}
				}
			}
			assertFormErrors(t, body, controls, widget.FieldErrors{})
			for _, element := range []string{"input", "select"} {
				for _, tag := range formTags(pageTestStrip(body), element) {
					if _, ok := pageTestAttribute(tag, "aria-describedby"); ok {
						t.Errorf("non-422 carries aria-describedby: %s", tag)
					}
				}
			}
		})
	}
}

// R-XBL8-ZHD9 R-GUU9-JB3W R-KIEO-Y49B R-NV2J-W5BV
func TestFormRejections(t *testing.T) {
	cases := []widget.Submission{
		{Name: "", Count: "2", Status: "active"},
		{Name: " ", Count: "three", Status: "archived"},
		{Name: strings.Repeat("界", widget.MaxNameRunes+1), Count: "0", Status: "paused"},
		{Name: " alpha ", Count: "1", Status: "retired"},
		{Name: "new", Count: " -1 ", Status: "active"},
		{Name: "new", Count: "2.5", Status: " paused "},
		{Name: "new", Count: strings.Repeat("9", 80), Status: "retired"},
		{Name: "new", Count: "1", Status: "archived"},
		{Name: "", Count: "bad", Status: "active"},
		{Name: "", Count: "1", Status: "archived"},
		{Name: "new", Count: "bad", Status: "archived"},
		{Name: " \t<&\" value=\"more >\r\n", Count: " \tthree<&\" name=\"stuff\r\n", Status: "\u2003active\u2003"},
		{Name: "a\x00b", Count: "three\x00", Status: "<option selected=\"selected\">"},
	}
	for i, sub := range cases {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			store, oracle := widget.NewStore(), widget.NewStore()
			before := store.All()
			_, errs := oracle.Create(sub)
			if !errs.Any() {
				t.Fatal("rejection fixture unexpectedly valid")
			}
			h := Handler(store)
			w := formRequest(h, http.MethodPost, "/widgets", "application/x-www-form-urlencoded", formBody(sub))
			if w.Code != http.StatusUnprocessableEntity || w.Header().Get("Content-Type") != "text/html; charset=utf-8" {
				t.Fatalf("rejection = %d %v", w.Code, w.Header())
			}
			if !slices.Equal(before, store.All()) {
				t.Errorf("rejection mutated store: %v", store.All())
			}
			body := w.Body.String()
			controls := assertFormMarkup(t, body)
			assertFormErrors(t, body, controls, errs)
			for field, raw := range map[string]string{"name": sub.Name, "count": sub.Count} {
				if got, _ := pageTestAttribute(controls[field], "value"); got != raw {
					t.Errorf("%s echo = %q, want raw %q", field, got, raw)
				}
			}
			for _, option := range formTags(formSpan(t, body), "option") {
				value, _ := pageTestAttribute(option, "value")
				_, selected := pageTestAttribute(option, "selected")
				if selected != (value == strings.TrimSpace(sub.Status)) {
					t.Errorf("option %q selected=%v for %q", value, selected, sub.Status)
				}
			}
			assertFormPanel(t, body)
			get := formRequest(h, http.MethodGet, "/widgets", "", "")
			if formTable(t, body) != formTable(t, get.Body.String()) {
				t.Error("rejection table differs from unchanged store's GET table")
			}
		})
	}
}

func formTable(t *testing.T, body string) string {
	t.Helper()
	body = pageTestStrip(body)
	starts, ends := pageTestTags(body, "table", false), pageTestTags(body, "table", true)
	if len(starts) != 1 || len(ends) != 1 || starts[0][1] > ends[0][0] {
		t.Fatalf("expected one table: %q", body)
	}
	return body[starts[0][0]:ends[0][1]]
}

func assertFormPanel(t *testing.T, body string) {
	t.Helper()
	if !strings.HasPrefix(strings.ToLower(strings.TrimSpace(body)), "<!doctype html>") {
		t.Error("422 lacks HTML doctype")
	}
	stripped := pageTestStrip(body)
	if len(pageTestTags(stripped, "body", false)) != 1 || len(pageTestTags(stripped, "body", true)) != 1 {
		t.Error("422 lacks single body pair")
	}
	visible := pageTestVisible(body)
	for _, want := range []string{ServiceName, "form-user@example.test", SignOutText} {
		if !strings.Contains(visible, want) {
			t.Errorf("422 chrome lacks %q", want)
		}
	}
	linkFound := false
	for _, span := range pageTestTags(stripped, "a", false) {
		href, _ := pageTestAttribute(stripped[span[0]:span[1]], "href")
		end := pageTestTags(stripped[span[1]:], "a", true)
		if href == SignOutURL("example.com", "") && len(end) > 0 && pageTestNormalize(stripped[span[1]:span[1]+end[0][0]]) == SignOutText {
			linkFound = true
		}
	}
	if !linkFound {
		t.Error("422 lacks sign-out link")
	}
	_ = formTable(t, body)
	if pageTestTags(stripped, "form", false)[0][0] < pageTestTags(stripped, "table", true)[0][1] {
		t.Error("422 form does not follow table")
	}
	if len(pageTestTags(body, "script", false)) != 1 || len(pageTestTags(body, "script", true)) != 1 {
		t.Error("422 lacks panel script")
	}
}

// R-NLBC-TZEB R-6OLO-3T75 R-GUU9-JB3W R-NQ6Y-D2D3 R-NREU-QU3S
func TestFormAcceptedSubmission(t *testing.T) {
	for _, mediaType := range []string{
		"application/x-www-form-urlencoded",
		" APPLICATION/X-WWW-FORM-URLENCODED \t; charset=UTF-8",
		"application/x-www-form-urlencoded; deliberately not a MIME parameter",
	} {
		t.Run(mediaType, func(t *testing.T) {
			store, oracle := widget.NewStore(), widget.NewStore()
			before := store.All()
			sub := widget.Submission{Name: " \tnew & widget\n", Count: " +004 ", Status: "\u2003paused "}
			created, errs := oracle.Create(sub)
			if errs.Any() {
				t.Fatalf("valid fixture rejected: %+v", errs)
			}
			// Duplicates and URL query fields must not replace the first body values.
			body := formBody(sub) + "&name=wrong&count=-9&status=archived&extra=ignored"
			h := Handler(store)
			w := formRequest(h, http.MethodPost, "/widgets?name=query&count=-1&status=archived", mediaType, body)
			if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/widgets" || w.Body.Len() != 0 {
				t.Fatalf("accepted answer = %d %v %q", w.Code, w.Header(), w.Body.String())
			}
			if got := store.All(); !slices.Equal(got, append(before, created)) {
				t.Errorf("created sequence = %v, want old sequence + %v", got, created)
			}
			page := formRequest(h, http.MethodGet, "/widgets", "", "")
			if !strings.Contains(pageTestNormalize(formTable(t, page.Body.String())), created.Name) {
				t.Error("redirect destination does not show created widget")
			}
		})
	}
}

// R-6OLO-3T75: behavior above proves first body values, this proves raw/missing
// values on rejection, and source inspection below checks the concrete call count.
func TestFormMissingAndRepeatedFields(t *testing.T) {
	for _, tc := range []struct {
		body string
		sub  widget.Submission
	}{
		{"", widget.Submission{}},
		{"name=&name=later&count=&count=1&status=&status=active", widget.Submission{}},
		{"name=+first+&name=later&count=+bad+&count=1&status=+paused+&status=active", widget.Submission{Name: " first ", Count: " bad ", Status: " paused "}},
	} {
		t.Run(tc.body, func(t *testing.T) {
			w := formRequest(Handler(widget.NewStore()), http.MethodPost, "/widgets?name=query&count=9&status=active", "application/x-www-form-urlencoded", tc.body)
			if w.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d", w.Code)
			}
			controls := assertFormMarkup(t, w.Body.String())
			_, errs := widget.NewStore().Create(tc.sub)
			assertFormErrors(t, w.Body.String(), controls, errs)
			for field, want := range map[string]string{"name": tc.sub.Name, "count": tc.sub.Count} {
				if got, _ := pageTestAttribute(controls[field], "value"); got != want {
					t.Errorf("%s=%q, want %q", field, got, want)
				}
			}
		})
	}
}

func TestFormCreateCallCount(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "form.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	calls, directCalls := 0, 0
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "serveForm" {
			continue
		}
		for _, statement := range fn.Body.List {
			assignment, ok := statement.(*ast.AssignStmt)
			if !ok {
				continue
			}
			for _, rhs := range assignment.Rhs {
				call, ok := rhs.(*ast.CallExpr)
				if !ok {
					continue
				}
				selector, ok := call.Fun.(*ast.SelectorExpr)
				if ok && selector.Sel.Name == "Create" {
					directCalls++
				}
			}
		}
	}
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if ok && selector.Sel.Name == "Create" {
			calls++
		}
		return true
	})
	if calls != 1 || directCalls != 1 {
		t.Errorf("form has %d Create call sites, %d directly in handler body; want one without a loop or conditional", calls, directCalls)
	}
}

type formObservedBody struct {
	reader io.Reader
	reads  int
}

func (b *formObservedBody) Read(p []byte) (int, error) {
	b.reads++
	return b.reader.Read(p)
}

func (*formObservedBody) Close() error { return nil }

// R-NLBC-TZEB R-KJML-BW00 R-KKUH-PNQP R-NV2J-W5BV
func TestFormUnsupportedMediaNeverReads(t *testing.T) {
	for _, mediaType := range []string{"", "application/json", "multipart/form-data; boundary=a", "text/plain", "application/x-www-form-urlencoded-extra", ";application/x-www-form-urlencoded"} {
		t.Run(mediaType, func(t *testing.T) {
			store := widget.NewStore()
			before := store.All()
			body := &formObservedBody{reader: strings.NewReader("name=otherwise-valid&count=1&status=active")}
			r := httptest.NewRequest(http.MethodPost, "/widgets", body)
			r.Header.Set("X-User-Id", "form-user")
			r.Header.Set("X-User-Email", "form-user@example.test")
			if mediaType != "" {
				r.Header.Set("Content-Type", mediaType)
			}
			w := httptest.NewRecorder()
			Handler(store).ServeHTTP(w, r)
			if w.Code != http.StatusUnsupportedMediaType || w.Header().Get("Content-Type") != "text/html; charset=utf-8" {
				t.Fatalf("unsupported answer = %d %v", w.Code, w.Header())
			}
			if body.reads != 0 || !slices.Equal(before, store.All()) {
				t.Errorf("unsupported request read body %d times or changed store", body.reads)
			}
			markup := pageTestStrip(w.Body.String())
			if len(formTags(markup, "form")) != 0 {
				t.Error("unsupported answer includes form")
			}
			assertFormErrors(t, markup, map[string]string{}, widget.FieldErrors{})
			visible := pageTestVisible(w.Body.String())
			for _, want := range []string{ServiceName, "form-user@example.test", SignOutText, UnsupportedMediaTypeMessage} {
				if !strings.Contains(visible, want) {
					t.Errorf("unsupported chrome lacks %q", want)
				}
			}
			back, signOut := false, false
			for _, span := range pageTestTags(markup, "a", false) {
				href, _ := pageTestAttribute(markup[span[0]:span[1]], "href")
				back = back || href == "/widgets"
				ends := pageTestTags(markup[span[1]:], "a", true)
				if href == SignOutURL(r.Host, "") && len(ends) > 0 && pageTestNormalize(markup[span[1]:span[1]+ends[0][0]]) == SignOutText {
					signOut = true
				}
			}
			if !back || !signOut {
				t.Errorf("unsupported answer missing chrome links: back=%v sign-out=%v", back, signOut)
			}
		})
	}
}

// R-W5CM-0ZFM
func TestFormMissingIdentityNeverReads(t *testing.T) {
	for _, mediaType := range []string{"application/x-www-form-urlencoded", "application/json", ""} {
		for _, identity := range []string{"absent", "empty"} {
			t.Run(mediaType+identity, func(t *testing.T) {
				store := widget.NewStore()
				before := store.All()
				h := Handler(store)
				var previous *httptest.ResponseRecorder
				for _, content := range []string{"name=new&count=1&status=active", "name=&count=wrong&status=archived"} {
					body := &formObservedBody{reader: strings.NewReader(content)}
					r := httptest.NewRequest(http.MethodPost, "/widgets", body)
					r.Header.Set("Content-Type", mediaType)
					if identity == "empty" {
						r.Header.Set("X-User-Id", "")
					}
					w := httptest.NewRecorder()
					h.ServeHTTP(w, r)
					if w.Code != http.StatusInternalServerError || body.reads != 0 || !slices.Equal(before, store.All()) {
						t.Errorf("missing identity status=%d reads=%d store=%v", w.Code, body.reads, store.All())
					}
					if previous != nil && (previous.Code != w.Code || !reflect.DeepEqual(previous.Header(), w.Header()) || previous.Body.String() != w.Body.String()) {
						t.Error("missing-identity response depends on body")
					}
					previous = w
				}
			})
		}
	}
}

type formFailingReader struct{}

func (formFailingReader) Read([]byte) (int, error) { return 0, errors.New("broken body") }

// R-NSMR-4LUH R-NZY5-F8AN R-W6KI-ER6B
func TestFormAnswerSetAndAcceptIndependence(t *testing.T) {
	for _, tc := range []struct {
		name, identity, mediaType, body string
		want                            int
		broken                          bool
	}{
		{name: "missing identity", mediaType: "application/json", body: "{}", want: 500},
		{name: "unsupported", identity: "user", mediaType: "application/json", body: "{}", want: 415},
		{name: "accepted", identity: "user", mediaType: "application/x-www-form-urlencoded", body: "name=delta&count=3&status=active", want: 303},
		{name: "rejected", identity: "user", mediaType: "application/x-www-form-urlencoded", body: "name=&count=bad&status=archived", want: 422},
		{name: "malformed", identity: "user", mediaType: "application/x-www-form-urlencoded", body: "name=%zz&count=%&status=;", want: 422},
		{name: "read error", identity: "user", mediaType: "application/x-www-form-urlencoded", want: 422, broken: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var previous *httptest.ResponseRecorder
			for _, accept := range []string{"", "application/json", "text/plain", "text/html", "*/*;q=0"} {
				var reader io.Reader = strings.NewReader(tc.body)
				if tc.broken {
					reader = formFailingReader{}
				}
				r := httptest.NewRequest(http.MethodPost, "/widgets", reader)
				r.Header.Set("X-User-Id", tc.identity)
				r.Header.Set("Content-Type", tc.mediaType)
				if accept != "" {
					r.Header.Set("Accept", accept)
				}
				w := httptest.NewRecorder()
				Handler(widget.NewStore()).ServeHTTP(w, r)
				if w.Code != tc.want {
					t.Errorf("status=%d, want=%d", w.Code, tc.want)
				}
				for key := range w.Header() {
					if strings.EqualFold(key, "Set-Cookie") {
						t.Error("POST answer carries Set-Cookie")
					}
				}
				mediaType, _, _ := strings.Cut(w.Header().Get("Content-Type"), ";")
				if strings.EqualFold(strings.TrimSpace(mediaType), "application/json") {
					t.Error("POST answer is JSON")
				}
				if previous != nil && (previous.Code != w.Code || !reflect.DeepEqual(previous.Header(), w.Header()) || previous.Body.String() != w.Body.String()) {
					t.Errorf("response depends on Accept=%q", accept)
				}
				previous = w
			}
		})
	}
}
