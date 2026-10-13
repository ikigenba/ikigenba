package panel_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"testing"

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

// R-QEKW-QVH4 R-QFST-4N7T R-QH0P-IEYI R-R92E-B50H R-R7UH-XD9S R-R5EP-5TSE
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
