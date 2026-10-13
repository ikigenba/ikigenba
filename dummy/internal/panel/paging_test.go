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

// R-LHTJ-BJ2Z
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
	if _, err := store.Check(context.Background(), widget.Draft{}); err != nil {
		t.Fatalf("reachable Check: %v", err)
	}
	if _, errs, err := store.Create(context.Background(), widget.Draft{}); err != nil || !errs.Any() {
		t.Fatalf("reachable invalid Create: errors=%v err=%v", errs, err)
	}
	handle.SetFailing(true)
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

// R-3MB7-L47W R-3NJ3-YVYL R-43DS-XWLM R-425W-K4UX R-3L3B-7CH7 R-3TML-VQO2
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

// R-3JVE-TKQI R-3L3B-7CH7 R-3MB7-L47W R-3NJ3-YVYL R-3TML-VQO2 R-3UUI-9IER R-3W2E-NA5G R-GXS5-DC2W
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
		data := panelTemplateData{Banner: pageTestBannerData(request), Panel: true, Count: len(window), Table: window, Page: p, Next: next, Form: panel.FormView{Statuses: widget.Statuses()}}
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

// R-3JVE-TKQI R-3OR0-CNPA R-40Y0-6D48 R-3SEP-HYXD R-3PYW-QFFZ R-3XAB-11W5
func TestPageDecodePrecedesStoreAccess(t *testing.T) {
	store, handle := panelDatabaseStore(t)
	h := coreHandler(t, store, pageTestBanner, io.Discard)
	bad := []string{"bad", (paging.Token{Tool: "other_tool"}).Encode(int64(1)), (paging.Token{Tool: "list_widgets"}).Encode("wrong-type")}
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
				for _, p := range []string{"", (paging.Token{Tool: "list_widgets"}).Encode(int64(999))} {
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

// R-3R6T-476O R-43DS-XWLM
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
