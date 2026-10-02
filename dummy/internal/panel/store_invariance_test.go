package panel_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/dummy/internal/widget"
)

func invariantSeededStore(t *testing.T) *widget.Store {
	t.Helper()
	store := panelTestStore()
	for _, sub := range []widget.Submission{
		{Name: "later first", Count: "8", Status: string(widget.StatusPaused)},
		{Name: "later second", Count: "2", Status: string(widget.StatusActive)},
	} {
		if _, errs := store.Create(coreDraft(sub)); errs.Any() {
			t.Fatalf("seed submission rejected: %+v", errs)
		}
	}
	return store
}

// R-2MFS-HVLX
func invariantSubmission(t *testing.T) string {
	t.Helper()
	sub := widget.Submission{Name: "new entry", Count: "9", Status: string(widget.StatusRetired)}
	draft, parsed := widget.ParseSubmission(sub)
	if parsed.Any() {
		t.Fatal(parsed)
	}
	if errs := invariantSeededStore(t).Check(draft); errs.Any() {
		t.Fatalf("valid submission rejected: %+v", errs)
	}
	return url.Values{"name": {sub.Name}, "count": {sub.Count}, "status": {sub.Status}}.Encode()
}

func assertPanelStoreInvariant(t *testing.T, method, path, identity string, present, form bool, wantStatus int) {
	t.Helper()
	store := invariantSeededStore(t)
	before := store.All()
	var body string
	if form {
		body = invariantSubmission(t)
	}
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if form {
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if present {
		request.Header["X-User-Id"] = []string{identity}
	}
	response := httptest.NewRecorder()
	coreHandler(t, store, pageTestBanner, io.Discard).ServeHTTP(response, request)
	if response.Code != wantStatus {
		t.Errorf("%s %s status = %d, want %d", method, path, response.Code, wantStatus)
	}
	if after := store.All(); !slices.Equal(before, after) {
		t.Errorf("%s %s changed ordered store: before=%+v after=%+v", method, path, before, after)
	}
}

// R-2Q3H-N6U0 R-2SJA-EQBE R-2UZ3-69SS R-2YMS-BL0V
func TestPanelReadRoutesPreserveStore(t *testing.T) {
	for _, tc := range []struct {
		method, path string
		status       int
	}{
		{http.MethodGet, "/", http.StatusSeeOther},
		{http.MethodHead, "/", http.StatusSeeOther},
		{http.MethodGet, "/widgets", http.StatusOK},
		{http.MethodHead, "/widgets", http.StatusOK},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			assertPanelStoreInvariant(t, tc.method, tc.path, "reader", true, false, tc.status)
		})
	}
}

// R-312L-34I9 R-34QA-8FQC R-0GMA-SDQL
func TestPanelRejectedRoutesPreserveStore(t *testing.T) {
	for _, tc := range []struct {
		method, path string
		form         bool
		status       int
	}{
		{http.MethodPost, "/", true, http.StatusMethodNotAllowed},
		{http.MethodPut, "/widgets", true, http.StatusMethodNotAllowed},
		{http.MethodGet, "/unknown", false, http.StatusNotFound},
		{http.MethodHead, "/widgets/table/", false, http.StatusNotFound},
		{http.MethodPost, "/widgets/", true, http.StatusNotFound},
		{http.MethodPut, "/other", true, http.StatusNotFound},
	} {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			assertPanelStoreInvariant(t, tc.method, tc.path, "reader", true, tc.form, tc.status)
		})
	}
}

// R-39LV-RIP4 R-3D9K-WTX7
func TestPanelMissingIdentityPreservesStore(t *testing.T) {
	for _, identity := range []struct {
		name    string
		present bool
	}{
		{"absent", false},
		{"empty", true},
	} {
		for _, tc := range []struct {
			method, path string
			form         bool
		}{
			{http.MethodGet, "/", false},
			{http.MethodHead, "/widgets", false},
			{http.MethodPost, "/widgets", true},
			{http.MethodPost, "/widgets/", true},
			{http.MethodPut, "/other", true},
			{http.MethodPost, "/mcp", false},
			{http.MethodGet, "/widgets/table", false},
		} {
			t.Run(identity.name+"/"+tc.method+tc.path, func(t *testing.T) {
				assertPanelStoreInvariant(t, tc.method, tc.path, "", identity.present, tc.form, http.StatusInternalServerError)
			})
		}
	}
}
