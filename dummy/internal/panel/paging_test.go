package panel_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/paging"
	"github.com/ikigenba/ikigenba/dummy/internal/panel"
	"github.com/ikigenba/ikigenba/dummy/internal/widget"
)

// R-DRIC-98YE
func TestWidgetsAcrossWindows(t *testing.T) {
	store, handle := panelDatabaseStore(t)
	var want []widget.Widget
	for i := range 17 {
		w, errs := panelStoreCreate(t, store, widget.Draft{Name: fmt.Sprintf("window-%d", i), Count: i, Status: widget.StatusActive})
		if errs.Any() {
			t.Fatal(errs)
		}
		want = append(want, w)
	}
	for _, size := range []int{1, 4, 17, 30} {
		var got []widget.Widget
		var after *int64
		for {
			batch, err := store.List(context.Background(), after, size)
			if err != nil {
				t.Fatal(err)
			}
			got = append(got, batch...)
			if len(batch) < size {
				break
			}
			q := batch[len(batch)-1].Seq
			after = &q
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("window %d: got %#v want %#v", size, got, want)
		}
	}
	if _, err := store.ListBefore(context.Background(), 99, 4); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Check(context.Background(), widget.Draft{}); err != nil {
		t.Fatalf("reachable Check: %v", err)
	}
	if _, errs, err := store.Create(context.Background(), widget.Draft{}); err != nil || !errs.Any() {
		t.Fatalf("reachable invalid Create: errors=%v err=%v", errs, err)
	}
	handle.SetFailing(true)
	if _, err := store.ListBefore(context.Background(), 99, 4); err == nil {
		t.Fatal("unreachable ListBefore succeeded")
	}
	if _, err := store.List(context.Background(), nil, 4); err == nil {
		t.Fatal("unreachable List succeeded")
	}
	if _, err := store.Check(context.Background(), widget.Draft{}); err == nil {
		t.Fatal("unreachable Check succeeded")
	}
	if _, _, err := store.Create(context.Background(), widget.Draft{}); err == nil {
		t.Fatal("unreachable Create succeeded")
	}
	handle.SetFailing(false)
	if !reflect.DeepEqual(panelStoreAll(t, store), want) {
		t.Fatal("failure changed widgets")
	}
}

// R-DLEU-CE8X R-DMMQ-Q5ZM R-EEOF-IW1L R-EDGJ-54AW R-DHR5-730U R-E4X8-GQ41
func TestPanelFirstPageAndRejectedForm(t *testing.T) {
	for _, count := range []int{0, paging.DefaultLimit - 1, paging.DefaultLimit, paging.DefaultLimit + 1, paging.DefaultLimit*2 + 1} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			store := panelEmptyStore(t)
			var all []widget.Widget
			for i := range count {
				w, errs := panelStoreCreate(t, store, widget.Draft{Name: fmt.Sprintf("paged-%d", i), Count: i, Status: widget.StatusActive})
				if errs.Any() {
					t.Fatal(errs)
				}
				all = append(all, w)
			}
			first := all
			next := ""
			if len(first) > paging.DefaultLimit {
				first = first[:paging.DefaultLimit]
				next = (paging.Token{Tool: "list_widgets"}).Encode(first[len(first)-1].Seq)
			}
			h := coreHandler(t, store, pageTestBanner, io.Discard)
			request := pageTestRequest(http.MethodGet, "/widgets")
			got := pageTestResponse(h, request)
			data := panelTemplateData{Banner: pageTestBannerData(request), Panel: true, Count: len(first), Table: first, Next: next, Form: panel.FormView{Statuses: widget.Statuses()}}
			if got.Code != http.StatusOK || got.Header().Get("Content-Type") != "text/html; charset=utf-8" || got.Body.String() != renderPanelTemplate(t, "page", data) {
				t.Fatal("GET page differs from first page data")
			}
			table := tableTestRequest(h, http.MethodGet, "/widgets/table", tableTestIdentity(), "")
			if table.Code != http.StatusOK || table.Body.String() != renderPanelTemplate(t, "table", first) {
				t.Fatal("table differs from first page")
			}
			sub := widget.Submission{Name: "", Count: "bad", Status: " paused "}
			draft, parse := widget.ParseSubmission(sub)
			rules := panelStoreCheck(t, store, draft)
			if parse.Name == "" {
				parse.Name = rules.Name
			}
			if parse.Count == "" {
				parse.Count = rules.Count
			}
			if parse.Status == "" {
				parse.Status = rules.Status
			}
			request = pageTestFormRequest(sub)
			got = pageTestResponse(h, request)
			data.Banner = pageTestBannerData(request)
			data.Form = panel.FormView{Submission: sub, Errors: parse, Statuses: widget.Statuses(), Selected: draft.Status}
			if got.Code != http.StatusUnprocessableEntity || got.Body.String() != renderPanelTemplate(t, "page", data) {
				t.Fatal("422 differs from first page with rejected form view")
			}
		})
	}
}

// R-DGJ8-TBA5 R-DHR5-730U R-DLEU-CE8X R-DMMQ-Q5ZM R-E4X8-GQ41 R-E654-UHUQ R-E8KX-M1C4 R-GXS5-DC2W
func TestPanelNavigatesWindows(t *testing.T) {
	store := panelEmptyStore(t)
	var all []widget.Widget
	for i := range paging.DefaultLimit*2 + 3 {
		w, errs := panelStoreCreate(t, store, widget.Draft{Name: fmt.Sprintf("navigation-%d", i), Count: i, Status: widget.StatusActive})
		if errs.Any() {
			t.Fatal(errs)
		}
		all = append(all, w)
	}
	h := coreHandler(t, store, pageTestBanner, io.Discard)
	server := httptest.NewServer(h)
	t.Cleanup(server.Close)
	client := mcp.NewClient(mcp.ClientConfig{Endpoint: server.URL + "/mcp", HTTPClient: server.Client()})
	codec := paging.Token{Tool: "list_widgets"}
	for _, offset := range []int{0, 1, paging.DefaultLimit, paging.DefaultLimit * 2, len(all)} {
		p := ""
		if offset > 0 {
			p = codec.Encode(all[offset-1].Seq)
		}
		window := all[offset:]
		next := ""
		if len(window) > paging.DefaultLimit {
			window = window[:paging.DefaultLimit]
			next = codec.Encode(window[len(window)-1].Seq)
		}
		args, err := json.Marshal(map[string]string{"page": p})
		if err != nil {
			t.Fatal(err)
		}
		result, err := client.CallTool(context.Background(), identity.Caller{UserID: "reader"}, "list_widgets", args)
		if err != nil || result.IsError() {
			t.Fatalf("tool window: %v", err)
		}
		raw, err := result.MarshalJSON()
		if err != nil {
			t.Fatal(err)
		}
		var answer struct {
			StructuredContent struct {
				Widgets []widget.Widget `json:"widgets"`
				Next    string          `json:"next"`
			} `json:"structuredContent"`
		}
		if err := json.Unmarshal(raw, &answer); err != nil {
			t.Fatal(err)
		}
		if len(answer.StructuredContent.Widgets) != len(window) || answer.StructuredContent.Next != next {
			t.Fatal("tool and panel window length or next differs")
		}
		for i, got := range answer.StructuredContent.Widgets {
			want := window[i]
			if got.ID != want.ID || got.Name != want.Name || got.Count != want.Count || got.Status != want.Status {
				t.Fatal("tool and panel widget object differs")
			}
		}
		query := "?page=" + url.QueryEscape(p) + "&page=ignored&limit=1&noise=yes"
		request := pageTestRequest(http.MethodGet, "/widgets"+query)
		got := pageTestResponse(h, request)
		prev := ""
		if p != "" && len(window) > 0 {
			prev = (paging.Token{Tool: "list_widgets-before"}).Encode(window[0].Seq)
		}
		data := panelTemplateData{Banner: pageTestBannerData(request), Panel: true, Count: len(window), Table: window, Page: p, Prev: prev, Next: next, Form: panel.FormView{Statuses: widget.Statuses()}}
		if got.Code != http.StatusOK || got.Header().Get("Content-Type") != "text/html; charset=utf-8" || got.Body.String() != renderPanelTemplate(t, "page", data) {
			t.Fatalf("page offset %d differs", offset)
		}
		head := pageTestResponse(h, pageTestRequest(http.MethodHead, "/widgets"+query))
		if head.Code != got.Code || !reflect.DeepEqual(head.Header(), got.Header()) || head.Body.Len() != 0 {
			t.Fatal("page HEAD differs")
		}
		fragment := tableTestRequest(h, http.MethodGet, "/widgets/table"+query, tableTestIdentity(), "")
		if fragment.Code != http.StatusOK || fragment.Header().Get("Content-Type") != "text/html; charset=utf-8" || fragment.Body.String() != renderPanelTemplate(t, "table", window) {
			t.Fatalf("fragment offset %d differs", offset)
		}
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			for _, condition := range []string{"*", fragment.Header().Get("ETag"), "W/" + fragment.Header().Get("ETag")} {
				headers := tableTestIdentity()
				headers.Set("If-None-Match", condition)
				conditional := tableTestRequest(h, method, "/widgets/table"+query, headers, "")
				if conditional.Code != http.StatusNotModified || conditional.Header().Get("ETag") != fragment.Header().Get("ETag") || conditional.Body.Len() != 0 {
					t.Fatal("window conditional response differs")
				}
			}
		}
	}
}

// R-DGJ8-TBA5 R-DNUN-3XQB R-EB0Q-DKTI R-3SEP-HYXD R-DP2J-HPH0 R-E9ST-ZT2T
func TestPageDecodePrecedesStoreAccess(t *testing.T) {
	store, handle := panelDatabaseStore(t)
	h := coreHandler(t, store, pageTestBanner, io.Discard)
	bad := []string{"bad", (paging.Token{Tool: "other_tool"}).Encode(int64(1)), (paging.Token{Tool: "list_widgets"}).Encode("wrong-type"), (paging.Token{Tool: "list_widgets-before"}).Encode("wrong-type")}
	for _, failing := range []bool{false, true} {
		handle.SetFailing(failing)
		for _, p := range bad {
			for _, method := range []string{http.MethodGet, http.MethodHead} {
				for _, condition := range []string{"", "*", `"stale"`} {
					r := pageTestRequest(method, "/widgets?page="+url.QueryEscape(p))
					r.Header.Set("If-None-Match", condition)
					got := pageTestResponse(h, r)
					if got.Code != http.StatusNotFound {
						t.Fatalf("page invalid token: %d", got.Code)
					}
					pageTestFailure(t, got, r, panel.NotFoundMessage)
					r = pageTestRequest(method, "/widgets/table?page="+url.QueryEscape(p))
					r.Header.Set("If-None-Match", condition)
					got = pageTestResponse(h, r)
					body := panel.NotFoundBody
					if method == http.MethodHead {
						body = ""
					}
					if got.Code != http.StatusNotFound || got.Header().Get("Content-Type") != "text/plain; charset=utf-8" || got.Header().Get("ETag") != "" || got.Body.String() != body {
						t.Fatal("fragment invalid-token failure differs")
					}
				}
			}
		}
		if failing {
			for _, path := range []string{"/widgets", "/widgets/table"} {
				for _, p := range []string{"", (paging.Token{Tool: "list_widgets"}).Encode(int64(999)), (paging.Token{Tool: "list_widgets-before"}).Encode(int64(1)), (paging.Token{Tool: "list_widgets-before"}).Encode(int64(999))} {
					for _, method := range []string{http.MethodGet, http.MethodHead} {
						headers := tableTestIdentity()
						headers.Set("If-None-Match", "*")
						got := tableTestRequest(h, method, path+"?page="+url.QueryEscape(p), headers, "")
						body := widget.Unreachable + "\n"
						if method == http.MethodHead {
							body = ""
						}
						if got.Code != http.StatusServiceUnavailable || got.Header().Get("Content-Type") != "text/plain; charset=utf-8" || got.Header().Get("ETag") != "" || got.Body.String() != body {
							t.Fatal("valid page must reach unreachable store")
						}
					}
				}
			}
		}
	}
}

// R-DQAF-VH7P R-EEOF-IW1L
func TestOnlyReadPageValueAffectsQuery(t *testing.T) {
	store, handle := panelDatabaseStore(t)
	h := coreHandler(t, store, pageTestEchoingBanner(), io.Discard)
	for _, failing := range []bool{false, true} {
		handle.SetFailing(failing)
		for _, tc := range []struct{ method, path, body string }{
			{"GET", "/widgets", ""}, {"HEAD", "/widgets/table", ""},
			{"GET", "/", ""}, {"GET", "/about", ""}, {"HEAD", "/tools", ""}, {"GET", "/missing", ""},
			{"POST", "/widgets", "name=&count=bad&status=unknown"}, {"PUT", "/widgets", ""}, {"POST", "/widgets/table", ""},
		} {
			headers := tableTestIdentity()
			headers.Set("Content-Type", "application/x-www-form-urlencoded")
			queries := []string{"?page=&noise=one", "?noise=two&page=&page=ignored"}
			if (tc.path != "/widgets" && tc.path != "/widgets/table") || (tc.method != http.MethodGet && tc.method != http.MethodHead) {
				queries = append(queries, "?page=bad&noise=three")
			}
			baseline := tableTestRequest(h, tc.method, tc.path, headers, tc.body)
			for _, query := range queries {
				got := tableTestRequest(h, tc.method, tc.path+query, headers, tc.body)
				if got.Code != baseline.Code || !reflect.DeepEqual(got.Header(), baseline.Header()) || got.Body.String() != baseline.Body.String() {
					t.Fatalf("query affected %s %s failing=%v", tc.method, tc.path, failing)
				}
			}
		}
	}
}

// R-DK6X-YMI8 R-DSQ8-N0P3 R-EC8M-RCK7 R-DGJ8-TBA5
// R-DLEU-CE8X R-DMMQ-Q5ZM R-E4X8-GQ41 R-E654-UHUQ R-E8KX-M1C4 R-GXS5-DC2W
func TestBeforeWindowsAndUnderflow(t *testing.T) {
	store := panelEmptyStore(t)
	var all []widget.Widget
	for i := range paging.DefaultLimit*3 + 3 {
		w, fields := panelStoreCreate(t, store, widget.Draft{Name: fmt.Sprintf("before-%d", i), Count: i, Status: widget.StatusActive})
		if fields.Any() {
			t.Fatal(fields)
		}
		all = append(all, w)
	}
	h := coreHandler(t, store, pageTestBanner, io.Discard)
	beforeCodec := paging.Token{Tool: "list_widgets-before"}
	afterCodec := paging.Token{Tool: "list_widgets"}
	// The outer keys have no corresponding row; bounds are strict and signed.
	for _, key := range []int64{-1 << 63, 0, all[0].Seq, all[paging.DefaultLimit-1].Seq, all[paging.DefaultLimit].Seq, all[paging.DefaultLimit+1].Seq, all[paging.DefaultLimit*2].Seq, all[len(all)-1].Seq, 1<<63 - 1} {
		p := beforeCodec.Encode(key)
		var preceding []widget.Widget
		for _, w := range all {
			if w.Seq < key {
				preceding = append(preceding, w)
			}
		}
		underflow := len(preceding) <= paging.DefaultLimit
		window := preceding
		if !underflow {
			window = preceding[len(preceding)-paging.DefaultLimit:]
		}
		for _, path := range []string{"/widgets", "/widgets/table"} {
			query := "?page=" + url.QueryEscape(p) + "&page=ignored&limit=1&noise=yes"
			for _, condition := range []string{"", "*", `"stale"`} {
				request := pageTestRequest(http.MethodGet, path+query)
				request.Header.Set("If-None-Match", condition)
				got := pageTestResponse(h, request)
				switch {
				case underflow:
					if got.Code != http.StatusSeeOther || got.Header().Get("Location") != path || got.Body.Len() != 0 {
						t.Fatalf("underflow %s key %d: %d %v %q", path, key, got.Code, got.Header(), got.Body.String())
					}
					// R-0142-4LXJ: only a successful fragment answer carries a validator.
					if path == "/widgets/table" && got.Header().Get("ETag") != "" {
						t.Fatal("underflow fragment carries ETag")
					}
				case path == "/widgets":
					data := panelTemplateData{Banner: pageTestBannerData(request), Panel: true, Count: len(window), Table: window, Page: p, Prev: beforeCodec.Encode(window[0].Seq), Next: afterCodec.Encode(window[len(window)-1].Seq), Form: panel.FormView{Statuses: widget.Statuses()}}
					if got.Code != http.StatusOK || got.Header().Get("Content-Type") != "text/html; charset=utf-8" || got.Body.String() != renderPanelTemplate(t, "page", data) {
						t.Fatalf("before page key %d differs", key)
					}
				case condition == "*":
					if got.Code != http.StatusNotModified || got.Body.Len() != 0 {
						t.Fatal("before conditional fragment differs")
					}
				default:
					if got.Code != http.StatusOK || got.Header().Get("Content-Type") != "text/html; charset=utf-8" || got.Body.String() != renderPanelTemplate(t, "table", window) {
						t.Fatalf("before fragment key %d differs", key)
					}
				}
				request.Method = http.MethodHead
				head := pageTestResponse(h, request)
				if head.Code != got.Code || !reflect.DeepEqual(head.Header(), got.Header()) || head.Body.Len() != 0 {
					t.Fatal("before HEAD differs")
				}
			}
			if !underflow && path == "/widgets/table" {
				baseline := pageTestResponse(h, pageTestRequest(http.MethodGet, path+query))
				for _, method := range []string{http.MethodGet, http.MethodHead} {
					for _, condition := range []string{baseline.Header().Get("ETag"), "W/" + baseline.Header().Get("ETag")} {
						request := pageTestRequest(method, path+query)
						request.Header.Add("If-None-Match", `"stale"`)
						request.Header.Add("If-None-Match", " "+condition+", \"later\" ")
						got := pageTestResponse(h, request)
						if got.Code != http.StatusNotModified || got.Header().Get("ETag") != baseline.Header().Get("ETag") || got.Body.Len() != 0 {
							t.Fatal("before validator differs")
						}
					}
				}
			}
		}
	}
}

// R-DTY5-0SFS R-DHR5-730U R-DK6X-YMI8 R-DSQ8-N0P3
func TestNextThenPrevVisitsSameWindows(t *testing.T) {
	for _, count := range []int{paging.DefaultLimit + 1, paging.DefaultLimit * 2, paging.DefaultLimit*3 + 3} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			store := panelEmptyStore(t)
			var all []widget.Widget
			for i := range count {
				w, fields := panelStoreCreate(t, store, widget.Draft{Name: fmt.Sprintf("walk-%d", i), Count: i, Status: widget.StatusActive})
				if fields.Any() {
					t.Fatal(fields)
				}
				all = append(all, w)
			}
			h := coreHandler(t, store, pageTestBanner, io.Discard)
			afterCodec := paging.Token{Tool: "list_widgets"}
			beforeCodec := paging.Token{Tool: "list_widgets-before"}
			var windows [][]widget.Widget
			p := ""
			for offset := 0; offset < count; offset += paging.DefaultLimit {
				end := min(offset+paging.DefaultLimit, count)
				window := all[offset:end]
				windows = append(windows, window)
				prev, next := "", ""
				if offset > 0 {
					prev = beforeCodec.Encode(window[0].Seq)
				}
				if end < count {
					next = afterCodec.Encode(window[len(window)-1].Seq)
				}
				assertNavigationPage(t, h, p, prev, next, window)
				if next != "" {
					p = next
				}
			}
			// p is the last forward page. Its Prev is the next backward page.
			for j := len(windows) - 1; j > 0; j-- {
				window := windows[j]
				prev := beforeCodec.Encode(window[0].Seq)
				next := ""
				if j != len(windows)-1 {
					next = afterCodec.Encode(window[len(window)-1].Seq)
				}
				assertNavigationPage(t, h, p, prev, next, window)
				p = prev
			}
			got := pageTestResponse(h, pageTestRequest(http.MethodGet, "/widgets?page="+url.QueryEscape(p)))
			if got.Code != http.StatusSeeOther || got.Header().Get("Location") != "/widgets" || got.Body.Len() != 0 {
				t.Fatal("backward walk did not reach first-page redirect")
			}
			window := windows[0]
			assertNavigationPage(t, h, "", "", afterCodec.Encode(window[len(window)-1].Seq), window)
		})
	}
}

func assertNavigationPage(t *testing.T, h http.Handler, p, prev, next string, window []widget.Widget) {
	t.Helper()
	r := pageTestRequest(http.MethodGet, "/widgets?page="+url.QueryEscape(p))
	got := pageTestResponse(h, r)
	data := panelTemplateData{Banner: pageTestBannerData(r), Panel: true, Count: len(window), Table: window, Form: panel.FormView{Statuses: widget.Statuses()}, Page: p, Prev: prev, Next: next}
	if got.Code != http.StatusOK || got.Body.String() != renderPanelTemplate(t, "page", data) {
		t.Fatalf("walk page %q differs from expected window", p)
	}
}
