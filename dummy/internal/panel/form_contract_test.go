package panel_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/dummy/internal/panel"
	"github.com/ikigenba/ikigenba/dummy/internal/widget"
)

// R-APBB-VZAZ
func TestFormCardAdjacency(t *testing.T) {
	for _, sub := range []widget.Submission{{}, {Name: "", Count: "bad", Status: "archived"}} {
		method, encoded := http.MethodGet, ""
		if sub.Count != "" {
			method, encoded = http.MethodPost, formBody(sub)
		}
		body := pageTestContent(t, formRequest(coreHandler(t, panelTestStore(t), pageTestBanner, io.Discard), method, "/widgets", "application/x-www-form-urlencoded", encoded).Body.String())
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
		if strings.Contains(heading, "<") {
			t.Errorf("form card heading = %q", heading)
		}
		closing := body[formEnds[0][1]:]
		sections := pageTestTags(closing, "section", true)
		if len(sections) == 0 || !formASCIIWhitespace(closing[:sections[0][0]]) {
			t.Error("form end is not immediately followed by card section end")
		}
	}
}

// R-6LRH-LQKY
func TestFormRawRejectedEcho(t *testing.T) {
	for i, sub := range []widget.Submission{
		{Name: "", Count: "", Status: "archived"},
		{Name: " \t<&\" value=\"more >\r\n", Count: " \tthree<&\" name=\"stuff\r\n", Status: "active"},
		{Name: strings.Repeat("界", widget.MaxNameRunes+1), Count: " -1 ", Status: "paused"},
		{Name: "a\x00b", Count: "three\x00", Status: "retired"},
	} {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			w := formRequest(coreHandler(t, panelTestStore(t), pageTestBanner, io.Discard), http.MethodPost, "/widgets", "application/x-www-form-urlencoded", formBody(sub))
			if w.Code != http.StatusUnprocessableEntity {
				t.Fatalf("fixture did not produce 422: %d", w.Code)
			}
			values := map[string]string{"name": "", "count": ""}
			for _, tag := range formTags(formSpan(t, w.Body.String()), "input") {
				name, _ := pageTestAttribute(tag, "name")
				if _, ok := values[name]; ok {
					values[name], _ = pageTestAttribute(tag, "value")
				}
			}
			for name, want := range map[string]string{"name": sub.Name, "count": sub.Count} {
				if got := values[name]; got != strings.ReplaceAll(want, "\x00", "\ufffd") {
					t.Errorf("%s echo=%q, want raw %q", name, got, want)
				}
			}
		})
	}
}

func formContractDocuments(t *testing.T, check func(*testing.T, *http.Request, *httptest.ResponseRecorder)) {
	t.Helper()
	for _, tc := range []struct{ method, path, media, body string }{
		{http.MethodGet, "/widgets", "", ""},
		{http.MethodGet, "/absent", "", ""},
		{http.MethodPost, "/", "", ""},
		{http.MethodDelete, "/widgets", "", ""},
		{http.MethodPost, "/widgets", "application/json", "{}"},
		{http.MethodPost, "/widgets", "application/x-www-form-urlencoded", "name=&count=bad&status=archived"},
		{http.MethodPost, "/widgets", "application/x-www-form-urlencoded", "name=valid&count=bad&status=active"},
	} {
		t.Run(tc.method+tc.path+tc.media+tc.body, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			r.Header.Set("X-User-Id", "form-user")
			r.Header.Set("X-User-Email", "form-user@example.test")
			r.Header.Set("Content-Type", tc.media)
			w := pageTestResponse(coreHandler(t, panelTestStore(t), pageTestBanner, io.Discard), r)
			if w.Header().Get("Content-Type") != "text/html; charset=utf-8" {
				t.Fatal("fixture did not produce HTML document")
			}
			check(t, r, w)
		})
	}
}

// R-BURW-W6IL
func TestFormDocumentErrorElements(t *testing.T) {
	formContractDocuments(t, func(t *testing.T, r *http.Request, w *httptest.ResponseRecorder) {
		written := pageTestWritten(t, w.Body.String(), r)
		for _, field := range []string{"name", "count", "status"} {
			count := 0
			for _, span := range regexp.MustCompile(`(?i)<[a-z][a-z0-9]*[^>]*>`).FindAllStringIndex(written, -1) {
				id, _ := pageTestAttribute(written[span[0]:span[1]], "id")
				if id != field+"-error" {
					continue
				}
				count++
				next := strings.IndexByte(written[span[1]:], '<')
				if next < 0 || !strings.HasPrefix(written[span[1]+next:], "</") {
					t.Errorf("%s error contains child element or lacks closing tag", field)
				}
			}
			if count > 1 {
				t.Errorf("%s error occurs %d times", field, count)
			}
		}
	})
}

// R-DMHD-83PR
func TestFormNonRejectionDocumentHasNoErrorHooks(t *testing.T) {
	formContractDocuments(t, func(t *testing.T, r *http.Request, w *httptest.ResponseRecorder) {
		if w.Code == http.StatusUnprocessableEntity {
			return
		}
		written := pageTestWritten(t, w.Body.String(), r)
		formContractNoErrorIDs(t, written)
		for _, element := range []string{"input", "select"} {
			for _, tag := range formTags(written, element) {
				if _, ok := pageTestAttribute(tag, "aria-describedby"); ok {
					t.Errorf("non-422 carries aria-describedby: %s", tag)
				}
			}
		}
	})
}

func formContractNoErrorIDs(t *testing.T, body string) {
	t.Helper()
	for _, span := range regexp.MustCompile(`(?i)<[a-z][a-z0-9]*[^>]*>`).FindAllStringIndex(body, -1) {
		id, _ := pageTestAttribute(body[span[0]:span[1]], "id")
		if slices.Contains([]string{"name-error", "count-error", "status-error"}, id) {
			t.Errorf("unexpected error id %q", id)
		}
	}
}

// R-K5GV-9F5J R-HTHJ-BDCY
func TestFormMediaClassificationAndValidationDecision(t *testing.T) {
	for _, media := range []string{"application/x-www-form-urlencoded", " APPLICATION/X-WWW-FORM-URLENCODED \t; charset=UTF-8", "\u2003application/x-www-form-urlencoded\u2003;ignored;more", "application/x-www-form-urlencoded; invalid parameter", "", "application/json", "application/x-www-form-urlencoded-extra", ";application/x-www-form-urlencoded", "application/x-www-form-urlencoded, text/plain"} {
		for _, sub := range []widget.Submission{{Name: "fresh", Count: "1", Status: "active"}, {Name: "", Count: "bad", Status: "archived"}, {Name: "fresh", Count: "bad", Status: "active"}} {
			t.Run(media+formBody(sub), func(t *testing.T) {
				mediaPrefix, _, _ := strings.Cut(media, ";")
				want := http.StatusUnsupportedMediaType
				if strings.EqualFold(strings.TrimSpace(mediaPrefix), "application/x-www-form-urlencoded") {
					_, errs := formTestCreate(t, panelTestStore(t), sub)
					want = http.StatusSeeOther
					if errs.Any() {
						want = http.StatusUnprocessableEntity
					}
				}
				w := formRequest(coreHandler(t, panelTestStore(t), pageTestBanner, io.Discard), http.MethodPost, "/widgets", media, formBody(sub))
				if w.Code != want {
					t.Errorf("status=%d, want %d", w.Code, want)
				}
			})
		}
	}
}

// R-DOX5-ZN75
func TestFormRejectedBodyIsPanelPage(t *testing.T) {
	for _, sub := range []widget.Submission{{}, {Name: "valid", Count: "bad", Status: "active"}, {Name: "", Count: "bad", Status: "archived"}} {
		w := formRequest(coreHandler(t, panelTestStore(t), pageTestBanner, io.Discard), http.MethodPost, "/widgets", "application/x-www-form-urlencoded", formBody(sub))
		if w.Code != http.StatusUnprocessableEntity {
			t.Fatalf("fixture did not produce 422: %d", w.Code)
		}
		assertFormPanel(t, w.Body.String())
	}
}

// R-HOLX-SAE6
func TestFormRefusalsPreserveWholeStoreSequence(t *testing.T) {
	for _, media := range []string{"application/json", "application/x-www-form-urlencoded"} {
		for _, seeded := range []bool{false, true} {
			t.Run(fmt.Sprint(media, seeded), func(t *testing.T) {
				store := panelTestStore(t)
				if seeded {
					for _, sub := range []widget.Submission{{Name: "z-last", Count: "6", Status: "retired"}, {Name: "a-first", Count: "0", Status: "paused"}} {
						if _, errs := formTestCreate(t, store, sub); errs.Any() {
							t.Fatal(errs)
						}
					}
				}
				before := panelStoreAll(t, store)
				w := formRequest(coreHandler(t, store, pageTestBanner, io.Discard), http.MethodPost, "/widgets", media, formBody(widget.Submission{Name: "z-last", Count: "bad", Status: "archived"}))
				if w.Code != http.StatusUnsupportedMediaType && w.Code != http.StatusUnprocessableEntity {
					t.Fatalf("fixture did not produce refusal: %d", w.Code)
				}
				if got := panelStoreAll(t, store); !slices.Equal(got, before) {
					t.Errorf("store=%v, want unchanged %v", got, before)
				}
			})
		}
	}
}

// R-KNRC-ZZ9Y R-CFI7-EA4E
func TestFormUnsupportedMediaFailureContract(t *testing.T) {
	for _, media := range []string{"", "application/json", "multipart/form-data; boundary=a", "text/plain", "application/x-www-form-urlencoded-extra", ";application/x-www-form-urlencoded"} {
		t.Run(media, func(t *testing.T) {
			body := &formObservedBody{reader: strings.NewReader("name=valid&count=1&status=active")}
			r := httptest.NewRequest(http.MethodPost, "/widgets", body)
			r.Header.Set("X-User-Id", "form-user")
			r.Header.Set("X-User-Email", "form-user@example.test")
			if media != "" {
				r.Header.Set("Content-Type", media)
			}
			w := pageTestResponse(coreHandler(t, panelTestStore(t), pageTestBanner, io.Discard), r)
			if w.Code != http.StatusUnsupportedMediaType || w.Header().Get("Content-Type") != "text/html; charset=utf-8" {
				t.Fatalf("unsupported response=%d %v", w.Code, w.Header())
			}
			if body.reads != 0 {
				t.Errorf("unsupported body read %d times", body.reads)
			}
			if !strings.Contains(pageTestVisible(w.Body.String()), panel.UnsupportedMediaTypeMessage) {
				t.Error("unsupported message missing")
			}
			back := false
			written := pageTestWritten(t, w.Body.String(), r)
			for _, tag := range formTags(written, "a") {
				href, _ := pageTestAttribute(tag, "href")
				back = back || href == "/widgets"
			}
			if !back {
				t.Error("return link missing")
			}
			if len(formTags(pageTestContent(t, written), "form")) != 0 {
				t.Error("unsupported page content includes form")
			}
			formContractNoErrorIDs(t, written)
		})
	}
}
