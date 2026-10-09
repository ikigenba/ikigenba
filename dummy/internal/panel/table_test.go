package panel_test

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	appidentity "github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/dummy"
	"github.com/ikigenba/ikigenba/dummy/internal/panel"
	"github.com/ikigenba/ikigenba/dummy/internal/widget"
)

func tableTestRequest(h http.Handler, method, path string, headers http.Header, body string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header = headers.Clone()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func tableTestIdentity() http.Header {
	return http.Header{"X-User-Id": {"reader"}, "X-User-Email": {"reader@example.test"}}
}

func tableTestCreate(t *testing.T, store *widget.Store, name string) {
	t.Helper()
	if _, errs := panelStoreCreate(t, store, widget.Draft{Name: name, Count: 45, Status: widget.StatusPaused}); errs.Any() {
		t.Fatalf("create %q: %+v", name, errs)
	}
}

func tableTestFragment(t *testing.T, raw string, widgets []widget.Widget) {
	t.Helper()
	if raw != renderPanelTemplate(t, "table", widgets) {
		t.Fatal("table differs from template")
	}
}

func TestTableMarkupAndPageIdentity(t *testing.T) {
	// R-HIIF-VFOP
	for _, store := range []*widget.Store{panelEmptyStore(t), panelTestStore(t)} {
		response := tableTestRequest(coreHandler(t, store, pageTestBanner, io.Discard), "GET", "/widgets/table", tableTestIdentity(), "")
		if response.Code != http.StatusOK || response.Header().Get("Content-Type") != "text/html; charset=utf-8" {
			t.Fatal("fragment status or content type")
		}
	}
}

func TestTableValidatorsTrackRenderedContent(t *testing.T) {
	// R-HZFA-S3P3 R-I1V3-JN6H R-I4AW-B6NV
	store := panelTestStore(t)
	h := coreHandler(t, store, pageTestBanner, io.Discard)
	first := tableTestRequest(h, "GET", "/widgets/table", tableTestIdentity(), "")
	second := tableTestRequest(h, "GET", "/widgets/table", tableTestIdentity(), "")
	otherStore := tableTestRequest(coreHandler(t, panelTestStore(t), pageTestBanner, io.Discard), "GET", "/widgets/table", tableTestIdentity(), "")
	for _, response := range []*httptest.ResponseRecorder{first, second, otherStore} {
		if response.Code != http.StatusOK || !regexp.MustCompile(`^"[^",\x09-\x0d\x20]+"$`).MatchString(response.Header().Get("ETag")) {
			t.Fatalf("invalid success validator: %d %v", response.Code, response.Header())
		}
		if response.Body.String() != first.Body.String() || response.Header().Get("ETag") != first.Header().Get("ETag") {
			t.Error("identical content did not produce identical validators")
		}
	}
	headers := tableTestIdentity()
	headers.Set("Content-Type", "application/x-www-form-urlencoded")
	created := tableTestRequest(h, "POST", "/widgets", headers, url.Values{"name": {"created"}, "count": {"7"}, "status": {"active"}}.Encode())
	if created.Code != http.StatusSeeOther {
		t.Fatalf("creation status = %d", created.Code)
	}
	after := tableTestRequest(h, "GET", "/widgets/table", tableTestIdentity(), "")
	if after.Code != http.StatusOK || after.Header().Get("ETag") == first.Header().Get("ETag") || after.Body.String() == first.Body.String() {
		t.Error("successful creation did not change fragment and validator")
	}
}

func TestTableConditionalRequests(t *testing.T) {
	// R-HJQC-97FE R-ICU6-ZKUQ R-HG2N-3W7B
	for _, empty := range []bool{false, true} {
		store := panelTestStore(t)
		if empty {
			store = panelEmptyStore(t)
		}
		h := coreHandler(t, store, pageTestBanner, io.Discard)
		baseline := tableTestRequest(h, "GET", "/widgets/table", tableTestIdentity(), "")
		etag := baseline.Header().Get("ETag")
		cases := []struct {
			name   string
			fields []string
			match  bool
		}{
			{"absent", nil, false},
			{"empty", []string{""}, false},
			{"exact", []string{etag}, true},
			{"list", []string{` "stale" , ` + etag + " \t, \"later\""}, true},
			{"wildcard", []string{"*"}, true},
			{"wildcard-list", []string{`"stale",  *  , "later"`}, true},
			{"second-field", []string{`"stale"`, etag}, true},
			{"weak", []string{"W/" + etag}, true},
			{"weak-in-list", []string{`"stale", W/` + etag + `, "later"`}, true},
			{"weak-second-field", []string{`"stale"`, " W/" + etag + " "}, true},
			{"stale", []string{`"stale", "later"`}, false},
			{"weak-stale", []string{`W/"stale"`}, false},
			{"unquoted", []string{strings.Trim(etag, `"`)}, false},
			{"quoted-star", []string{`"*"`}, false},
		}
		for _, tc := range cases {
			t.Run(fmt.Sprintf("empty=%v/%s", empty, tc.name), func(t *testing.T) {
				headers := tableTestIdentity()
				headers["If-None-Match"] = tc.fields
				get := tableTestRequest(h, "GET", "/widgets/table", headers, "")
				head := tableTestRequest(h, "HEAD", "/widgets/table", headers, "")
				wantStatus, wantBody := http.StatusOK, baseline.Body.String()
				if tc.match {
					wantStatus, wantBody = http.StatusNotModified, ""
				}
				if get.Code != wantStatus || get.Body.String() != wantBody || get.Header().Get("ETag") != etag {
					t.Errorf("GET = %d %v %q", get.Code, get.Header(), get.Body.String())
				}
				if !tc.match && !reflect.DeepEqual(get.Header(), baseline.Header()) {
					t.Errorf("nonmatch changed headers: %v versus %v", get.Header(), baseline.Header())
				}
				if head.Code != get.Code || !reflect.DeepEqual(head.Header(), get.Header()) || head.Body.Len() != 0 {
					t.Errorf("HEAD does not mirror GET: %d %v %q", head.Code, head.Header(), head.Body.String())
				}
			})
		}
	}
}

func TestTableFailuresAndReadOnlyRequests(t *testing.T) {
	// R-IF9Z-R4C4 R-IIXO-WFK7 R-0142-4LXJ R-HHAJ-HNY0 R-HG2N-3W7B
	for _, method := range []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS", "TRACE", "CUSTOM"} {
		for _, identity := range []string{"absent", "empty", "present"} {
			for _, conditional := range []string{"", "*", `"stale"`} {
				t.Run(method+"/"+identity+"/"+conditional, func(t *testing.T) {
					store := panelTestStore(t)
					tableTestCreate(t, store, "read-only fixture")
					headers := tableTestIdentity()
					switch identity {
					case "absent":
						headers.Del("X-User-Id")
					case "empty":
						headers.Set("X-User-Id", "")
					}
					headers.Set("If-None-Match", conditional)
					before := panelStoreAll(t, store)
					response := tableTestRequest(coreHandler(t, store, pageTestBanner, io.Discard), method, "/widgets/table", headers, "name=unwanted&count=3&status=active")
					if !slices.Equal(before, panelStoreAll(t, store)) {
						t.Error("table request mutated the store")
					}
					wantStatus, wantBody := http.StatusOK, ""
					switch {
					case identity != "present":
						wantStatus, wantBody = http.StatusInternalServerError, appidentity.MissingBody
					case method != "GET" && method != "HEAD":
						wantStatus, wantBody = http.StatusMethodNotAllowed, panel.MethodNotAllowedBody
					case conditional == "*":
						wantStatus = http.StatusNotModified
					}
					if response.Code != wantStatus {
						t.Fatalf("status = %d, want %d", response.Code, wantStatus)
					}
					if wantStatus != http.StatusOK && wantStatus != http.StatusNotModified {
						if method == "HEAD" {
							wantBody = ""
						}
						if response.Body.String() != wantBody || response.Header().Get("Content-Type") != "text/plain; charset=utf-8" {
							t.Errorf("failure shape = %v %q", response.Header(), response.Body.String())
						}
						if identity == "present" {
							for key := range response.Header() {
								if strings.EqualFold(key, "ETag") {
									t.Error("authenticated failure carries ETag")
								}
							}
						}
						if _, exists := response.Header()["Location"]; wantStatus == http.StatusMethodNotAllowed && exists {
							t.Error("failure carries Location")
						}
					}
					if wantStatus == http.StatusMethodNotAllowed && response.Header().Get("Allow") != "GET, HEAD" {
						t.Errorf("Allow = %q", response.Header().Get("Allow"))
					}
					if method == "HEAD" {
						get := tableTestRequest(coreHandler(t, store, pageTestBanner, io.Discard), "GET", "/widgets/table", headers, "name=unwanted&count=3&status=active")
						if response.Code != get.Code || !reflect.DeepEqual(response.Header(), get.Header()) || response.Body.Len() != 0 {
							t.Error("HEAD failure/success does not mirror GET")
						}
					}
				})
			}
		}
	}
}

func TestTableConcurrentSnapshots(t *testing.T) {
	store := panelTestStore(t)
	h := coreHandler(t, store, pageTestBanner, io.Discard)
	var group sync.WaitGroup
	for i := range 12 {
		group.Go(func() {
			name := fmt.Sprintf("concurrent-%d", i)
			if _, errs := panelStoreCreate(t, store, widget.Draft{Name: name, Count: 2, Status: widget.StatusActive}); errs.Any() {
				t.Errorf("create: %+v", errs)
			}
		})
		group.Go(func() {
			response := tableTestRequest(h, "GET", "/widgets/table", tableTestIdentity(), "")
			if response.Code != http.StatusOK || response.Body.Len() == 0 || response.Header().Get("ETag") == "" {
				t.Error("concurrent table request failed")
			}
		})
	}
	group.Wait()
	response := tableTestRequest(h, "GET", "/widgets/table", tableTestIdentity(), "")
	tableTestFragment(t, response.Body.String(), panelStoreAll(t, store))
}

func TestTableTransportHeadParity(t *testing.T) {
	// R-HG2N-3W7B: exercise net/http's real response framing, including lengths
	// it otherwise adds to GET responses but omits from unwritten HEAD bodies.
	store := panelTestStore(t)
	h := coreHandler(t, store, pageTestBanner, io.Discard)
	etag := tableTestRequest(h, "GET", "/widgets/table", tableTestIdentity(), "").Header().Get("ETag")
	server := httptest.NewServer(h)
	defer server.Close()
	for _, tc := range []struct {
		name, identity, condition string
		status                    int
	}{
		{"unconditional", "reader", "", http.StatusOK},
		{"stale", "reader", `"stale"`, http.StatusOK},
		{"matching", "reader", etag, http.StatusNotModified},
		{"wildcard", "reader", "*", http.StatusNotModified},
		{"missing-identity", "", "", http.StatusInternalServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			type answer struct {
				status  int
				headers http.Header
				body    []byte
			}
			var answers []answer
			for _, method := range []string{http.MethodGet, http.MethodHead} {
				request, err := http.NewRequest(method, server.URL+"/widgets/table", nil)
				if err != nil {
					t.Fatal(err)
				}
				if tc.identity != "" {
					request.Header.Set("X-User-Id", tc.identity)
				}
				if tc.condition != "" {
					request.Header.Set("If-None-Match", tc.condition)
				}
				response, err := server.Client().Do(request)
				if err != nil {
					t.Fatal(err)
				}
				body, readErr := io.ReadAll(response.Body)
				closeErr := response.Body.Close()
				if readErr != nil || closeErr != nil {
					t.Fatalf("read/close response: %v / %v", readErr, closeErr)
				}
				// Date is produced by net/http's wall clock, outside Handler.
				response.Header.Del("Date")
				answers = append(answers, answer{response.StatusCode, response.Header, body})
			}
			get, head := answers[0], answers[1]
			if get.status != tc.status || head.status != get.status || !reflect.DeepEqual(head.headers, get.headers) || len(head.body) != 0 {
				t.Fatalf("GET: %d %v; HEAD: %d %v body %q", get.status, get.headers, head.status, head.headers, head.body)
			}
			if tc.status != http.StatusNotModified && get.headers.Get("Content-Length") != strconv.Itoa(len(get.body)) {
				t.Errorf("Content-Length = %q, want %d", get.headers.Get("Content-Length"), len(get.body))
			}
		})
	}
}

// R-HDMU-CCPX
func TestTableUsesAssetData(t *testing.T) {
	set, err := page.Templates().ParseFS(dummy.Assets(), "*.html")
	if err != nil {
		t.Fatal(err)
	}
	for _, empty := range []bool{false, true} {
		store := panelTestStore(t)
		if empty {
			store = panelEmptyStore(t)
		} else {
			tableTestCreate(t, store, "a\x00b <&>")
		}
		var expected bytes.Buffer
		if err := set.ExecuteTemplate(&expected, "table", panelStoreAll(t, store)); err != nil {
			t.Fatal(err)
		}
		response := tableTestRequest(coreHandler(t, store, pageTestBanner, io.Discard), "GET", "/widgets/table", tableTestIdentity(), "")
		if response.Code != http.StatusOK || response.Body.String() != expected.String() {
			t.Fatalf("fragment differs from table template output: %d %q", response.Code, response.Body.String())
		}
	}
}
