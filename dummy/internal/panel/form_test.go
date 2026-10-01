package panel

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/dummy"
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
	body = pageTestContent(t, body)
	starts, ends := pageTestTags(body, "form", false), pageTestTags(body, "form", true)
	if len(starts) != 1 || len(ends) != 1 || starts[0][1] > ends[0][0] {
		t.Fatalf("expected one ordered form pair: %q", body)
	}
	return body[starts[0][0]:ends[0][1]]
}

// R-7R82-LXSK
func TestWidgetFormPageContent(t *testing.T) {
	for _, request := range []*http.Request{
		pageTestRequest(http.MethodGet, "/widgets"),
		pageTestFormRequest(widget.Submission{Count: "bad"}),
	} {
		body := pageTestResponse(coreHandler(t, widget.NewStore(), pageTestBanner, io.Discard), request).Body.String()
		_ = formSpan(t, body)
	}
}

func formTags(body, name string) []string {
	var result []string
	for _, span := range pageTestTags(body, name, false) {
		result = append(result, body[span[0]:span[1]])
	}
	return result
}

func formASCIIWhitespace(s string) bool {
	return strings.Trim(s, " \t\n\v\f\r") == ""
}

// R-JDF6-GP3K
func TestFormCard(t *testing.T) {
	for _, sub := range []widget.Submission{{}, {Name: "", Count: "bad", Status: "archived"}} {
		method, encoded := http.MethodGet, ""
		if sub.Count != "" {
			method, encoded = http.MethodPost, formBody(sub)
		}
		body := pageTestContent(t, formRequest(coreHandler(t, widget.NewStore(), pageTestBanner, io.Discard), method, "/widgets", "application/x-www-form-urlencoded", encoded).Body.String())
		forms, formEnds := pageTestTags(body, "form", false), pageTestTags(body, "form", true)
		if len(forms) != 1 || len(formEnds) != 1 {
			t.Fatalf("form pairs: starts=%v ends=%v", forms, formEnds)
		}
		var section [2]int
		found := false
		for _, span := range pageTestTags(body[:forms[0][0]], "section", false) {
			section, found = span, true
		}
		if !found {
			t.Fatal("form card section missing")
		}
		if class, ok := pageTestAttribute(body[section[0]:section[1]], "class"); !ok || class != "card" {
			t.Errorf("form card class = %q, present=%v", class, ok)
		}
		opening := body[section[1]:forms[0][0]]
		var parts [4][2]int
		for i, tag := range []struct {
			name string
			end  bool
		}{{"header", false}, {"h2", false}, {"h2", true}, {"header", true}} {
			spans := pageTestTags(opening, tag.name, tag.end)
			if len(spans) != 1 {
				t.Fatalf("form card %s end=%v spans=%v", tag.name, tag.end, spans)
			}
			parts[i] = spans[0]
		}
		if parts[0][0] > parts[0][1] || parts[0][1] > parts[1][0] || parts[1][1] > parts[2][0] || parts[2][1] > parts[3][0] || parts[3][1] > len(opening) {
			t.Fatalf("form card tags are out of order: %v", parts)
		}
		if !formASCIIWhitespace(opening[:parts[0][0]]) || !formASCIIWhitespace(opening[parts[0][1]:parts[1][0]]) ||
			!formASCIIWhitespace(opening[parts[2][1]:parts[3][0]]) || !formASCIIWhitespace(opening[parts[3][1]:]) {
			t.Error("form card opening has content between required tags")
		}
		if class, ok := pageTestAttribute(opening[parts[1][0]:parts[1][1]], "class"); !ok || class != "text-md" {
			t.Error("form heading class")
		}
		heading := opening[parts[1][1]:parts[2][0]]
		if strings.Contains(heading, "<") || pageTestNormalize(heading) != "Add widget" {
			t.Errorf("form card heading = %q", heading)
		}
		closing := body[formEnds[0][1]:]
		sections := pageTestTags(closing, "section", true)
		if len(sections) == 0 || !formASCIIWhitespace(closing[:sections[0][0]]) {
			t.Error("form end is not immediately followed by card section end")
		}
	}
}

// R-BA1M-E2WS
func TestFormIconButton(t *testing.T) {
	for _, sub := range []widget.Submission{{}, {Name: "", Count: "bad", Status: "archived"}} {
		method, encoded := http.MethodGet, ""
		if sub.Count != "" {
			method, encoded = http.MethodPost, formBody(sub)
		}
		form := formSpan(t, formRequest(coreHandler(t, widget.NewStore(), pageTestBanner, io.Discard), method, "/widgets", "application/x-www-form-urlencoded", encoded).Body.String())
		buttons := pageTestTags(form, "button", false)
		if len(buttons) != 1 {
			t.Fatalf("button start tags = %d", len(buttons))
		}
		button := form[buttons[0][0]:buttons[0][1]]
		buttonType, _ := pageTestAttribute(button, "type")
		if strings.EqualFold(buttonType, "reset") || strings.EqualFold(buttonType, "button") {
			t.Errorf("button is not a submit control: %s", button)
		}
		for _, input := range formTags(form, "input") {
			typ, _ := pageTestAttribute(input, "type")
			if strings.EqualFold(typ, "submit") || strings.EqualFold(typ, "button") || strings.EqualFold(typ, "reset") {
				t.Errorf("additional submit or forbidden input control: %s", input)
			}
		}
		rest := form[buttons[0][1]:]
		ends := pageTestTags(rest, "button", true)
		if len(ends) != 1 {
			t.Fatalf("button end tags: %d", len(ends))
		}
		content := rest[:ends[0][0]]
		icons, iconEnds := pageTestTags(content, "svg", false), pageTestTags(content, "svg", true)
		if len(icons) != 1 || len(iconEnds) != 1 || !formASCIIWhitespace(content[:icons[0][0]]) || icons[0][1] > iconEnds[0][0] {
			t.Fatalf("button icon structure = %q", content)
		}
		icon := content[icons[0][0]:iconEnds[0][1]]
		if hidden, ok := pageTestAttribute(content[icons[0][0]:icons[0][1]], "aria-hidden"); !ok || hidden != "true" || pageTestNormalize(icon) != "" {
			t.Fatalf("icon is not hidden and textless: %q", icon)
		}
		text := content[iconEnds[0][1]:]
		if strings.Contains(text, "<") || pageTestNormalize(text) != "Add widget" {
			t.Errorf("button has nested element, missing end, or wrong text: %s", rest)
		}
	}
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

// R-CLWD-QZGX R-8D69-HT52 R-9IMU-I0CO R-A4L1-DVP6
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

// R-8XWJ-ZWQV
func TestFormStatusOptionsFollowStatuses(t *testing.T) {
	for _, sub := range []widget.Submission{{}, {Name: "", Count: "bad", Status: "paused"}} {
		method := http.MethodGet
		body := ""
		if sub.Count != "" {
			method = http.MethodPost
			body = formBody(sub)
		}
		response := formRequest(coreHandler(t, widget.NewStore(), pageTestBanner, io.Discard), method, "/widgets", "application/x-www-form-urlencoded", body)
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

// R-JOE9-WMRT
func TestFormRejectedStatusSelection(t *testing.T) {
	for _, status := range []string{"active", " paused ", "retired", "archived", "", "PAUSED"} {
		sub := widget.Submission{Name: "", Count: "1", Status: status}
		response := formRequest(coreHandler(t, widget.NewStore(), pageTestBanner, io.Discard), http.MethodPost, "/widgets", "application/x-www-form-urlencoded", formBody(sub))
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

func formFieldErrorText(body, field string) (string, bool) {
	stripped := pageTestStrip(body)
	for _, span := range regexp.MustCompile(`(?i)<[a-z][a-z0-9]*[^>]*>`).FindAllStringIndex(stripped, -1) {
		id, _ := pageTestAttribute(stripped[span[0]:span[1]], "id")
		if id != field+"-error" {
			continue
		}
		rest := stripped[span[1]:]
		end := strings.IndexByte(rest, '<')
		if end < 0 {
			return pageTestNormalize(rest), true
		}
		return pageTestNormalize(rest[:end]), true
	}
	return "", false
}

// R-D08H-WDQ7
func TestFormFieldErrorTextProcedure(t *testing.T) {
	body := `<script><span id="name-error">wrong</span></script><style>x</style><span id="name-error">  A &amp; B  </span>`
	if got, ok := formFieldErrorText(body, "name"); !ok || got != "A & B" {
		t.Errorf("field error text = %q, present=%v", got, ok)
	}
	response := formRequest(coreHandler(t, widget.NewStore(), pageTestBanner, io.Discard), http.MethodPost, "/widgets", "application/x-www-form-urlencoded", "name=valid&count=bad&status=active")
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d", response.Code)
	}
	if got, ok := formFieldErrorText(response.Body.String(), "count"); !ok || got != widget.CountNotWholeMessage {
		t.Errorf("rendered count error text = %q, present=%v", got, ok)
	}
}

// R-MQGS-V2YL R-MROP-8UPA
func assertFormErrors(t *testing.T, body string, controls map[string]string, errs widget.FieldErrors) {
	t.Helper()
	r := httptest.NewRequest(http.MethodGet, "/widgets", nil)
	r.Header.Set("X-User-Id", "form-user")
	r.Header.Set("X-User-Email", "form-user@example.test")
	stripped := pageTestStrip(pageTestWritten(t, body, r))
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

// R-JPM6-AEII
func TestFormFreshPagesAndFailures(t *testing.T) {
	for _, tc := range []struct{ method, path, contentType string }{
		{http.MethodGet, "/widgets", ""},
		{http.MethodGet, "/absent", ""},
		{http.MethodDelete, "/widgets", ""},
		{http.MethodPost, "/widgets", "application/json"},
	} {
		t.Run(tc.method+tc.path+tc.contentType, func(t *testing.T) {
			w := formRequest(coreHandler(t, widget.NewStore(), pageTestBanner, io.Discard), tc.method, tc.path, tc.contentType, "")
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
			r := httptest.NewRequest(tc.method, tc.path, nil)
			r.Header.Set("X-User-Id", "form-user")
			r.Header.Set("X-User-Email", "form-user@example.test")
			written := pageTestStrip(pageTestWritten(t, body, r))
			for _, element := range []string{"input", "select"} {
				for _, tag := range formTags(written, element) {
					if _, ok := pageTestAttribute(tag, "aria-describedby"); ok {
						t.Errorf("non-422 carries aria-describedby: %s", tag)
					}
				}
			}
		})
	}
}

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
			_, errs := formTestCreate(oracle, sub)
			if !errs.Any() {
				t.Fatal("rejection fixture unexpectedly valid")
			}
			h := coreHandler(t, store, pageTestBanner, io.Discard)
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
				if got, _ := pageTestAttribute(controls[field], "value"); got != strings.ReplaceAll(raw, "\x00", "\ufffd") {
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
	r := httptest.NewRequest("POST", "/widgets", nil)
	r.Header.Set("X-User-Id", "form-user")
	r.Header.Set("X-User-Email", "form-user@example.test")
	pageTestChrome(t, body, r)
	body = pageTestWritten(t, body, r)
	_ = formTable(t, body)
	content := pageTestContent(t, body)
	if pageTestTags(content, "form", false)[0][0] < pageTestTags(content, "table", true)[0][1] {
		t.Error("422 form does not follow table")
	}
	table := formTable(t, body)
	rows := 0
	for _, row := range pageTestTags(table, "tr", false) {
		ends := pageTestTags(table[row[1]:], "tr", true)
		if len(ends) > 0 && len(pageTestTags(table[row[1]:row[1]+ends[0][0]], "td", false)) > 0 {
			rows++
		}
	}
	heading := pageTestHeading(rows)
	trimmed := strings.Trim(content, " \t\n\v\f\r")
	if !strings.HasPrefix(trimmed, heading) {
		t.Fatal("422 heading does not count its table rows")
	}
	rest := strings.TrimLeft(trimmed[len(heading):], " \t\n\v\f\r")
	divs, divEnds := pageTestTags(rest, "div", false), pageTestTags(rest, "div", true)
	if len(divs) == 0 || len(divEnds) == 0 || divs[0][0] != 0 || divEnds[len(divEnds)-1][1] != len(rest) {
		t.Fatal("422 content lacks panel wrapper after heading")
	}
	if class, _ := pageTestAttribute(rest[divs[0][0]:divs[0][1]], "class"); class != "panel" {
		t.Fatal("422 panel wrapper class")
	}
	inside := rest[divs[0][1]:divEnds[len(divEnds)-1][0]]
	forms, formEnds := pageTestTags(inside, "form", false), pageTestTags(inside, "form", true)
	if len(forms) != 1 || len(formEnds) != 1 || forms[0][1] > formEnds[0][0] {
		t.Fatal("422 widget form pair")
	}
	sections := pageTestTags(inside[:forms[0][0]], "section", false)
	sectionEnds := pageTestTags(inside[formEnds[0][1]:], "section", true)
	if len(sections) == 0 || len(sectionEnds) == 0 {
		t.Fatal("422 form card pair")
	}
	card := inside[sections[len(sections)-1][0] : formEnds[0][1]+sectionEnds[0][1]]
	inside = strings.TrimLeft(inside, " \t\n\v\f\r")
	if !strings.HasPrefix(inside, table) {
		t.Fatal("422 panel wrapper does not begin with table")
	}
	inside = strings.TrimLeft(inside[len(table):], " \t\n\v\f\r")
	if !strings.HasPrefix(inside, card) || !formASCIIWhitespace(inside[len(card):]) {
		t.Fatal("422 panel wrapper does not end with form card")
	}
	starts, ends := pageTestTags(body, "script", false), pageTestTags(body, "script", true)
	if len(starts) != 1 || len(ends) != 1 || starts[0][1] > ends[0][0] {
		t.Fatal("422 lacks panel script pair")
	}
	if _, present := pageTestAttribute(body[starts[0][0]:starts[0][1]], "src"); present {
		t.Error("422 panel script loads external source")
	}
	for _, literal := range []string{"/widgets/table", "widgets-table", "5000"} {
		if !strings.Contains(body[starts[0][1]:ends[0][0]], literal) {
			t.Errorf("422 panel script lacks %q", literal)
		}
	}
	tableStart := pageTestTags(body, "table", false)[0]
	tableEnd := pageTestTags(body, "table", true)[0]
	if starts[0][0] > tableStart[0] && starts[0][0] < tableEnd[1] {
		t.Error("422 panel script is inside table")
	}
}

// R-MLL7-BZZT R-KE05-XTCE R-KGFY-PCTS
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
			created, errs := formTestCreate(oracle, sub)
			if errs.Any() {
				t.Fatalf("valid fixture rejected: %+v", errs)
			}
			// Duplicates and URL query fields must not replace the first body values.
			body := formBody(sub) + "&name=wrong&count=-9&status=archived&extra=ignored"
			h := coreHandler(t, store, pageTestBanner, io.Discard)
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

// R-MLL7-BZZT: behavior above proves first body values and one widget added on
// acceptance; this proves raw/missing values and no widget added on rejection.
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
			store := widget.NewStore()
			before := store.All()
			w := formRequest(coreHandler(t, store, pageTestBanner, io.Discard), http.MethodPost, "/widgets?name=query&count=9&status=active", "application/x-www-form-urlencoded", tc.body)
			if w.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d", w.Code)
			}
			if got := store.All(); !slices.Equal(got, before) {
				t.Errorf("rejected submission changed store: %v, want %v", got, before)
			}
			controls := assertFormMarkup(t, w.Body.String())
			_, errs := formTestCreate(widget.NewStore(), tc.sub)
			assertFormErrors(t, w.Body.String(), controls, errs)
			for field, want := range map[string]string{"name": tc.sub.Name, "count": tc.sub.Count} {
				if got, _ := pageTestAttribute(controls[field], "value"); got != want {
					t.Errorf("%s=%q, want %q", field, got, want)
				}
			}
			for _, option := range formTags(formSpan(t, w.Body.String()), "option") {
				value, _ := pageTestAttribute(option, "value")
				if _, selected := pageTestAttribute(option, "selected"); selected != (value == strings.TrimSpace(tc.sub.Status)) {
					t.Errorf("option %q selected=%v for status %q", value, selected, tc.sub.Status)
				}
			}
		})
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
			coreHandler(t, store, pageTestBanner, io.Discard).ServeHTTP(w, r)
			if w.Code != http.StatusUnsupportedMediaType || w.Header().Get("Content-Type") != "text/html; charset=utf-8" {
				t.Fatalf("unsupported answer = %d %v", w.Code, w.Header())
			}
			if body.reads != 0 || !slices.Equal(before, store.All()) {
				t.Errorf("unsupported request read body %d times or changed store", body.reads)
			}
			markup := pageTestStrip(w.Body.String())
			if len(formTags(pageTestContent(t, markup), "form")) != 0 {
				t.Error("unsupported answer includes form")
			}
			assertFormErrors(t, markup, map[string]string{}, widget.FieldErrors{})
			visible := pageTestVisible(w.Body.String())
			for _, want := range []string{"Sign out", UnsupportedMediaTypeMessage} {
				if !strings.Contains(visible, want) {
					t.Errorf("unsupported chrome lacks %q", want)
				}
			}
			pageTestChrome(t, w.Body.String(), r)
			back := false
			for _, span := range pageTestTags(markup, "a", false) {
				href, _ := pageTestAttribute(markup[span[0]:span[1]], "href")
				back = back || href == "/widgets"
			}
			if !back {
				t.Error("unsupported answer missing return link")
			}

		})
	}
}

// R-KSMY-J28Q
func TestFormMissingIdentityNeverReads(t *testing.T) {
	for _, mediaType := range []string{"application/x-www-form-urlencoded", "application/json", ""} {
		for _, identity := range []string{"absent", "empty"} {
			t.Run(mediaType+identity, func(t *testing.T) {
				store := widget.NewStore()
				before := store.All()
				h := coreHandler(t, store, pageTestBanner, io.Discard)
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

// R-KV2R-ALQ4 R-KWAN-ODGT
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
				coreHandler(t, widget.NewStore(), pageTestBanner, io.Discard).ServeHTTP(w, r)
				if w.Code != tc.want {
					t.Errorf("status=%d, want=%d", w.Code, tc.want)
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

// formTestCreate independently models the submission's field error definition.
func formTestCreate(store *widget.Store, sub widget.Submission) (widget.Widget, widget.FieldErrors) {
	d, parse := widget.ParseSubmission(sub)
	if !parse.Any() {
		return store.Create(d)
	}
	rules := store.Check(d)
	errors := widget.FieldErrors{Name: parse.Name, Count: parse.Count, Status: parse.Status}
	if errors.Name == "" {
		errors.Name = rules.Name
	}
	if errors.Count == "" {
		errors.Count = rules.Count
	}
	if errors.Status == "" {
		errors.Status = rules.Status
	}
	return widget.Widget{}, errors
}

// R-IQ93-720D R-MP8W-HB7W R-MMT3-PRQI
func TestFormAssetAndDeclaredView(t *testing.T) {
	set, err := page.Templates().ParseFS(dummy.Assets(), "*.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, sub := range []widget.Submission{
		{}, {Name: "", Count: "bad", Status: "archived"},
		{Name: " alpha ", Count: "-1", Status: " active "},
		{Name: "valid", Count: "bad", Status: " paused "},
		{Name: "new", Count: "1", Status: "ARCHIVED"},
	} {
		store := widget.NewStore()
		view := FormView{Statuses: widget.Statuses()}
		method, encoded := http.MethodGet, ""
		if sub != (widget.Submission{}) {
			method, encoded = http.MethodPost, formBody(sub)
			draft, _ := widget.ParseSubmission(sub)
			_, errs := formTestCreate(widget.NewStore(), sub)
			view = FormView{Submission: sub, Errors: errs, Statuses: widget.Statuses(), Selected: draft.Status}
		}
		response := formRequest(coreHandler(t, store, pageTestBanner, io.Discard), method, "/widgets", "application/x-www-form-urlencoded", encoded)
		if method == http.MethodPost && response.Code != http.StatusUnprocessableEntity {
			t.Fatalf("response = %d", response.Code)
		}
		var expected bytes.Buffer
		if err := set.ExecuteTemplate(&expected, "form", view); err != nil {
			t.Fatal(err)
		}
		content := pageTestContent(t, response.Body.String())
		form := pageTestTags(content, "form", false)[0]
		formEnd := pageTestTags(content, "form", true)[0]
		sections := pageTestTags(content[:form[0]], "section", false)
		sectionEnds := pageTestTags(content[formEnd[1]:], "section", true)
		if len(sections) == 0 || len(sectionEnds) == 0 {
			t.Fatal("card missing")
		}
		card := content[sections[len(sections)-1][0] : formEnd[1]+sectionEnds[0][1]]
		if card != strings.Trim(expected.String(), " \t\n\v\f\r") {
			t.Errorf("form does not execute asset with declared view")
		}
		if method == http.MethodPost {
			assertFormErrors(t, response.Body.String(), formControls(t, formSpan(t, response.Body.String())), view.Errors)
		}
	}
}

// R-MLL7-BZZT R-MO10-3JH7 R-MMT3-PRQI
func TestFormConcurrentCreationOutcomes(t *testing.T) {
	store := widget.NewStore()
	before := store.All()
	handler := coreHandler(t, store, pageTestBanner, io.Discard)
	sub := widget.Submission{Name: " contested ", Count: "4", Status: "paused"}
	start := make(chan struct{})
	answers := make(chan *httptest.ResponseRecorder, 2)
	var group sync.WaitGroup
	for range 2 {
		group.Go(func() {
			<-start
			answers <- formRequest(handler, http.MethodPost, "/widgets", "application/x-www-form-urlencoded", formBody(sub))
		})
	}
	close(start)
	group.Wait()
	close(answers)
	accepted, rejected := 0, 0
	for response := range answers {
		switch response.Code {
		case http.StatusSeeOther:
			accepted++
		case http.StatusUnprocessableEntity:
			rejected++
			controls := formControls(t, formSpan(t, response.Body.String()))
			assertFormErrors(t, response.Body.String(), controls, widget.FieldErrors{Name: widget.NameTakenMessage})
		default:
			t.Fatalf("creation status = %d", response.Code)
		}
	}
	want := slices.Clone(before)
	want = append(want, widget.Widget{Name: "contested", Count: 4, Status: widget.StatusPaused})
	if accepted != 1 || rejected != 1 || !slices.Equal(store.All(), want) {
		t.Fatalf("outcomes=%d/%d store=%v", accepted, rejected, store.All())
	}
}
