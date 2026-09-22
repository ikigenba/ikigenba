package panel

import (
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
	if _, errs := store.Create(widget.Submission{Name: name, Count: "45", Status: "paused"}); errs.Any() {
		t.Fatalf("create %q: %+v", name, errs)
	}
}

func tableTestFragment(t *testing.T, raw string, widgets []widget.Widget) {
	t.Helper()
	fragment := strings.TrimSpace(raw)
	starts := pageTestTags(fragment, "table", false)
	ends := pageTestTags(fragment, "table", true)
	if len(starts) != 1 || len(ends) != 1 || starts[0][0] != 0 || ends[0][1] != len(fragment) {
		t.Fatalf("not exactly a table fragment: %q", fragment)
	}
	for _, tag := range []string{"html", "body", "script"} {
		if len(pageTestTags(raw, tag, false)) != 0 {
			t.Errorf("fragment carries forbidden %s tag", tag)
		}
	}
	if strings.Contains(strings.ToLower(raw), "<!doctype") {
		t.Error("fragment carries doctype")
	}
	if id, ok := pageTestAttribute(fragment[starts[0][0]:starts[0][1]], "id"); !ok || id != "widgets-table" {
		t.Errorf("table id = %q, present = %v", id, ok)
	}
	stripped := pageTestStrip(fragment)
	rows := pageTestTags(stripped, "tr", false)
	headerCount, dataCount := 0, 0
	for _, rowStart := range rows {
		endTags := pageTestTags(stripped[rowStart[1]:], "tr", true)
		if len(endTags) == 0 {
			t.Fatal("unclosed row")
		}
		row := stripped[rowStart[1] : rowStart[1]+endTags[0][0]]
		cells := pageTestTags(row, "td", false)
		headingCells := pageTestTags(row, "th", false)
		if len(headingCells) > 0 && len(cells) == 0 {
			headerCount++
			if dataCount != 0 {
				t.Error("header follows a data row")
			}
		}
		if len(cells) == 0 {
			continue
		}
		if dataCount >= len(widgets) {
			t.Fatal("extra data row")
		}
		if len(cells) < 3 {
			t.Fatal("data row has fewer than three cells")
		}
		w := widgets[dataCount]
		want := []string{strings.Join(strings.Fields(w.Name), " "), strconv.Itoa(w.Count), string(w.Status)}
		for i, cell := range cells[:3] {
			endCells := pageTestTags(row[cell[1]:], "td", true)
			if len(endCells) == 0 {
				t.Fatal("unclosed cell")
			}
			got := pageTestNormalize(row[cell[1] : cell[1]+endCells[0][0]])
			if got != want[i] {
				t.Errorf("row %d cell %d = %q, want %q", dataCount, i, got, want[i])
			}
		}
		dataCount++
	}
	if headerCount != 1 || dataCount != len(widgets) {
		t.Errorf("header/data rows = %d/%d, want 1/%d", headerCount, dataCount, len(widgets))
	}
}

func tableTestPageSpan(t *testing.T, raw string) string {
	t.Helper()
	body := pageTestStrip(raw)
	starts, ends := pageTestTags(body, "table", false), pageTestTags(body, "table", true)
	if len(starts) != 1 || len(ends) != 1 || starts[0][0] >= ends[0][0] {
		t.Fatalf("page has no unique ordered table span: %q", body)
	}
	return body[starts[0][0]:ends[0][1]]
}

func TestTableMarkupAndPageIdentity(t *testing.T) {
	// R-KVPQ-RDIA R-KWXN-558Z R-M3J7-QWZS R-M4R4-4OQH R-Q4HP-7G07
	// R-3ABH-NPH7 R-3BJE-1H7W R-62UX-379I
	for _, empty := range []bool{false, true} {
		t.Run(fmt.Sprintf("empty=%v", empty), func(t *testing.T) {
			store := widget.NewStore()
			if empty {
				store = new(widget.Store)
			} else {
				tableTestCreate(t, store, "  my  \twidget\n<&>\"  ")
				tableTestCreate(t, store, "last widget")
			}
			h := Handler(store)
			headers := tableTestIdentity()
			fragment := tableTestRequest(h, "GET", "/widgets/table", headers, "")
			if fragment.Code != http.StatusOK || fragment.Header().Get("Content-Type") != "text/html; charset=utf-8" {
				t.Fatalf("fragment response = %d %v", fragment.Code, fragment.Header())
			}
			tableTestFragment(t, fragment.Body.String(), store.All())
			page := tableTestRequest(h, "GET", "/widgets", headers, "")
			if page.Code != http.StatusOK {
				t.Fatalf("page status = %d", page.Code)
			}
			headers.Set("Content-Type", "application/x-www-form-urlencoded")
			rejected := tableTestRequest(h, "POST", "/widgets", headers, "name=&count=bad&status=unknown")
			if rejected.Code != http.StatusUnprocessableEntity {
				t.Fatalf("rejected status = %d", rejected.Code)
			}
			for _, response := range []*httptest.ResponseRecorder{page, rejected} {
				span := tableTestPageSpan(t, response.Body.String())
				tableTestFragment(t, span, store.All())
				if strings.TrimSpace(span) != strings.TrimSpace(fragment.Body.String()) {
					t.Errorf("page %d table differs from fragment", response.Code)
				}
			}
		})
	}
}

func TestTableValidatorsTrackRenderedContent(t *testing.T) {
	// R-KY5J-IWZO R-MC2I-FB6N R-MDAE-T2XC
	store := widget.NewStore()
	h := Handler(store)
	first := tableTestRequest(h, "GET", "/widgets/table", tableTestIdentity(), "")
	second := tableTestRequest(h, "GET", "/widgets/table", tableTestIdentity(), "")
	otherStore := tableTestRequest(Handler(widget.NewStore()), "GET", "/widgets/table", tableTestIdentity(), "")
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
	// R-642T-GZ07 R-65AP-UQQW R-WIGA-XCYS
	for _, empty := range []bool{false, true} {
		store := widget.NewStore()
		if empty {
			store = new(widget.Store)
		}
		h := Handler(store)
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
			{"stale", []string{`"stale", "later"`}, false},
			{"weak", []string{"W/" + etag}, false},
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
	// R-66IM-8IHL R-67QI-MA8A R-MLTP-HH47 R-MN1L-V8UW R-WIGA-XCYS
	for _, method := range []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS", "TRACE", "CUSTOM"} {
		for _, identity := range []string{"absent", "empty", "present"} {
			for _, conditional := range []string{"", "*", `"stale"`} {
				t.Run(method+"/"+identity+"/"+conditional, func(t *testing.T) {
					store := widget.NewStore()
					tableTestCreate(t, store, "read-only fixture")
					headers := tableTestIdentity()
					switch identity {
					case "absent":
						headers.Del("X-User-Id")
					case "empty":
						headers.Set("X-User-Id", "")
					}
					headers.Set("If-None-Match", conditional)
					before := store.All()
					response := tableTestRequest(Handler(store), method, "/widgets/table", headers, "name=unwanted&count=3&status=active")
					if !slices.Equal(before, store.All()) {
						t.Error("table request mutated the store")
					}
					wantStatus, wantBody := http.StatusOK, ""
					switch {
					case identity != "present":
						wantStatus, wantBody = http.StatusInternalServerError, MissingIdentityBody
					case method != "GET" && method != "HEAD":
						wantStatus, wantBody = http.StatusMethodNotAllowed, MethodNotAllowedBody
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
						if _, exists := response.Header()["Etag"]; exists {
							t.Error("failure carries ETag")
						}
						if _, exists := response.Header()["Location"]; exists {
							t.Error("failure carries Location")
						}
					}
					if wantStatus == http.StatusMethodNotAllowed && response.Header().Get("Allow") != "GET, HEAD" {
						t.Errorf("Allow = %q", response.Header().Get("Allow"))
					}
					if method == "HEAD" {
						get := tableTestRequest(Handler(store), "GET", "/widgets/table", headers, "name=unwanted&count=3&status=active")
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
	store := widget.NewStore()
	h := Handler(store)
	var group sync.WaitGroup
	for i := range 12 {
		group.Go(func() {
			name := fmt.Sprintf("concurrent-%d", i)
			if _, errs := store.Create(widget.Submission{Name: name, Count: "2", Status: "active"}); errs.Any() {
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
	tableTestFragment(t, response.Body.String(), store.All())
}

func TestTableTransportHeadParity(t *testing.T) {
	// R-WIGA-XCYS: exercise net/http's real response framing, including lengths
	// it otherwise adds to GET responses but omits from unwritten HEAD bodies.
	store := widget.NewStore()
	h := Handler(store)
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
