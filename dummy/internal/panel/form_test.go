package panel_test

import (
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/ikigenba/ikigenba/dummy/internal/panel"
	"github.com/ikigenba/ikigenba/dummy/internal/widget"
)

func formTestRequest(method, target, mediaType, body string) *http.Request {
	r := httptest.NewRequest(method, target, strings.NewReader(body))
	r.Header.Set("X-User-Id", "form-user")
	r.Header.Set("X-User-Email", "form-user@example.test")
	r.Header.Set("Content-Type", mediaType)
	return r
}

func formRequest(h http.Handler, method, target, mediaType, body string) *httptest.ResponseRecorder {
	return pageTestResponse(h, formTestRequest(method, target, mediaType, body))
}

func formBody(sub widget.Submission) string {
	return url.Values{"name": {sub.Name}, "count": {sub.Count}, "status": {sub.Status}}.Encode()
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
			store, oracle := panelTestStore(t), panelTestStore(t)
			before := panelStoreAll(t, store)
			_, errs := formTestCreate(t, oracle, sub)
			if !errs.Any() {
				t.Fatal("rejection fixture unexpectedly valid")
			}
			h := coreHandler(t, store, pageTestBanner, io.Discard)
			w := formRequest(h, http.MethodPost, "/widgets", "application/x-www-form-urlencoded", formBody(sub))
			if w.Code != http.StatusUnprocessableEntity || w.Header().Get("Content-Type") != "text/html; charset=utf-8" {
				t.Fatalf("rejection = %d %v", w.Code, w.Header())
			}
			if !slices.Equal(before, panelStoreAll(t, store)) {
				t.Errorf("rejection mutated store: %v", panelStoreAll(t, store))
			}
			assertFormPage(t, w, formTestRequest("POST", "/widgets", "application/x-www-form-urlencoded", formBody(sub)), before, sub, errs)
		})
	}
}

// R-MLL7-BZZT R-KE05-XTCE R-HNE1-EINH
func TestFormAcceptedSubmission(t *testing.T) {
	for _, mediaType := range []string{
		"application/x-www-form-urlencoded",
		" APPLICATION/X-WWW-FORM-URLENCODED \t; charset=UTF-8",
		"application/x-www-form-urlencoded; deliberately not a MIME parameter",
	} {
		t.Run(mediaType, func(t *testing.T) {
			store, oracle := panelTestStore(t), panelTestStore(t)
			before := panelStoreAll(t, store)
			sub := widget.Submission{Name: " \tnew & widget\n", Count: " +004 ", Status: "\u2003paused "}
			created, errs := formTestCreate(t, oracle, sub)
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
			if got := panelStoreAll(t, store); !slices.Equal(got, append(before, created)) {
				t.Errorf("created sequence = %v, want old sequence + %v", got, created)
			}
			page := formRequest(h, http.MethodGet, "/widgets", "", "")
			if !strings.Contains(page.Body.String(), html.EscapeString(created.Name)) {
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
			store := panelTestStore(t)
			before := panelStoreAll(t, store)
			w := formRequest(coreHandler(t, store, pageTestBanner, io.Discard), http.MethodPost, "/widgets?name=query&count=9&status=active", "application/x-www-form-urlencoded", tc.body)
			if w.Code != http.StatusUnprocessableEntity {
				t.Fatalf("status = %d", w.Code)
			}
			if got := panelStoreAll(t, store); !slices.Equal(got, before) {
				t.Errorf("rejected submission changed store: %v, want %v", got, before)
			}
			_, errs := formTestCreate(t, panelTestStore(t), tc.sub)
			assertFormPage(t, w, formTestRequest("POST", "/widgets", "application/x-www-form-urlencoded", tc.body), before, tc.sub, errs)
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
			store := panelTestStore(t)
			before := panelStoreAll(t, store)
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
			if body.reads != 0 || !slices.Equal(before, panelStoreAll(t, store)) {
				t.Errorf("unsupported request read body %d times or changed store", body.reads)
			}
			pageTestFailure(t, w, r, panel.UnsupportedMediaTypeMessage)
		})
	}
}

// R-HPTU-624V
func TestFormMissingIdentityNeverReads(t *testing.T) {
	for _, mediaType := range []string{"application/x-www-form-urlencoded", "application/json", ""} {
		for _, identity := range []string{"absent", "empty"} {
			t.Run(mediaType+identity, func(t *testing.T) {
				store := panelTestStore(t)
				before := panelStoreAll(t, store)
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
					if w.Code != http.StatusInternalServerError || body.reads != 0 || !slices.Equal(before, panelStoreAll(t, store)) {
						t.Errorf("missing identity status=%d reads=%d store=%v", w.Code, body.reads, panelStoreAll(t, store))
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

// R-HR1Q-JTVK R-HS9M-XLM9
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
				coreHandler(t, panelTestStore(t), pageTestBanner, io.Discard).ServeHTTP(w, r)
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
func formTestCreate(t *testing.T, store *widget.Store, sub widget.Submission) (widget.Widget, widget.FieldErrors) {
	d, parse := widget.ParseSubmission(sub)
	if !parse.Any() {
		return panelStoreCreate(t, store, d)
	}
	rules := panelStoreCheck(t, store, d)
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

// R-UAD2-32MV R-U955-PAW6 R-RHG9-RNM6 R-RG8D-DVVH
// R-IQ93-720D  R-MMT3-PRQI
func TestFormAssetAndDeclaredView(t *testing.T) {
	for _, sub := range []widget.Submission{
		{}, {Name: "", Count: "bad", Status: "archived"},
		{Name: " alpha ", Count: "-1", Status: " active "},
		{Name: "valid", Count: "bad", Status: " paused "},
		{Name: "new", Count: "1", Status: "ARCHIVED"},
	} {
		store := panelTestStore(t)
		before := panelStoreAll(t, store)
		request := pageTestRequest("GET", "/widgets")
		errs := widget.FieldErrors{}
		if sub != (widget.Submission{}) {
			request = pageTestFormRequest(sub)
			_, errs = formTestCreate(t, panelTestStore(t), sub)
		}
		response := pageTestResponse(coreHandler(t, store, pageTestBanner, io.Discard), request)
		wantStatus := http.StatusOK
		if request.Method == http.MethodPost {
			wantStatus = http.StatusUnprocessableEntity
		}
		if response.Code != wantStatus {
			t.Fatalf("status = %d", response.Code)
		}
		assertFormPage(t, response, request, before, sub, errs)
	}
}

// R-MLL7-BZZT R-HTHJ-BDCY R-MMT3-PRQI
func TestFormConcurrentCreationOutcomes(t *testing.T) {
	store := panelTestStore(t)
	before := panelStoreAll(t, store)
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
			assertFormPage(t, response, formTestRequest("POST", "/widgets", "application/x-www-form-urlencoded", formBody(sub)), panelStoreAll(t, store), sub, widget.FieldErrors{Name: widget.NameTakenMessage})
		default:
			t.Fatalf("creation status = %d", response.Code)
		}
	}
	want := slices.Clone(before)
	expected, errors := panelStoreCreate(t, panelTestStore(t), coreDraft(sub))
	if errors.Any() {
		t.Fatal(errors)
	}
	want = append(want, expected)
	if accepted != 1 || rejected != 1 || !slices.Equal(panelStoreAll(t, store), want) {
		t.Fatalf("outcomes=%d/%d store=%v", accepted, rejected, panelStoreAll(t, store))
	}
}
