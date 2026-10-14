package pages

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/paging"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	app "github.com/ikigenba/ikigenba/scrapes"
	"github.com/ikigenba/ikigenba/scrapes/internal/scrapers"
	"github.com/ikigenba/ikigenba/scrapes/internal/source"
	"github.com/ikigenba/ikigenba/scrapes/internal/store"
	"github.com/ikigenba/ikigenba/scrapes/internal/tools"
)

func templates(t *testing.T) *template.Template {
	t.Helper()
	s, err := page.Templates().ParseFS(app.Assets(), "*.html")
	if err != nil {
		t.Fatal(err)
	}
	return s
}
func rendered(t *testing.T, name string, d any) string {
	t.Helper()
	var b bytes.Buffer
	if err := templates(t).ExecuteTemplate(&b, name, d); err != nil {
		t.Fatal(err)
	}
	return b.String()
}
func banner(u page.User) page.Banner {
	return page.Banner{Service: ServiceName, Release: "test-release", Commit: "test-commit", Email: u.Email, ProfileURL: u.ProfileURL, LogoutURL: u.LogoutURL, Home: "https://home.example.test", Tools: true, Trail: []page.Level{{Name: "prior", URL: "/prior"}}}
}
func request(h http.Handler, method, path, user string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, path, nil)
	r.Host = "scrapes.sbx.ikigenba.dev:443"
	r.Header.Set("X-Forwarded-Proto", "https")
	r.Header.Set("X-User-Id", user)
	r.Header.Set("X-User-Email", "reader@example.test")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func baseBanner() page.Banner {
	return banner(page.User{Email: "reader@example.test", ProfileURL: "https://auth.sbx.ikigenba.dev/", LogoutURL: "https://auth.sbx.ikigenba.dev/logout"})
}
func assertBody(t *testing.T, w *httptest.ResponseRecorder, status int, name string, d any) {
	t.Helper()
	if w.Code != status || w.Body.String() != rendered(t, name, d) {
		t.Fatalf("%s response mismatch: status %d want %d; body length %d", name, w.Code, status, w.Body.Len())
	}
	if got := w.Header().Values("Content-Type"); !reflect.DeepEqual(got, []string{"text/html; charset=utf-8"}) {
		t.Fatal(got)
	}
}

type fake struct {
	name  string
	calls *int
}

func (f fake) Name() string                                           { return f.name }
func (f fake) Scrape(context.Context, func([]source.Row) error) error { *f.calls++; return nil }

type sequence struct{ n byte }

func (s *sequence) Read(p []byte) (int, error) {
	s.n++
	for i := range p {
		p[i] = s.n + byte(i)
	}
	return len(p), nil
}

type fixture struct {
	st     *store.Store
	handle *db.DB
	cfg    Config
	h      http.Handler
	now    *time.Time
	calls  *int
}

func setup(t *testing.T) fixture {
	t.Helper()
	now := time.Date(2026, 10, 5, 9, 0, 7, 123, time.FixedZone("test", 3600))
	ctx := context.Background()
	handle, err := db.Open(ctx, db.Config{Path: filepath.Join(t.TempDir(), "scrapes.db"), Migrations: app.Migrations(), Now: func() time.Time { return now }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := handle.Close(); err != nil {
			t.Error(err)
		}
	})
	st := store.New(handle, store.Config{Now: func() time.Time { return now }, Rand: &sequence{n: 0x99}})
	set, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	calls := new(int)
	cfg := Config{Banner: banner, Pages: set, Store: st, Scrapers: scrapers.New(fake{"zeta", calls}, fake{"alpha", calls})}
	return fixture{st, handle, cfg, identity.Require(Handler(cfg)), &now, calls}
}
func (f fixture) add(t *testing.T, name, trigger string) store.Run {
	t.Helper()
	*f.now = f.now.Add(time.Second)
	delivery := ""
	if trigger != "" {
		delivery = f.now.Format(time.RFC3339Nano)
	}
	r, err := f.st.AddRun(context.Background(), store.Run{Scraper: name, UserID: "owner", UserEmail: "owner@example.test", Trigger: trigger, Delivery: delivery})
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func (f fixture) finish(t *testing.T, r store.Run, rows []source.Row) store.Run {
	t.Helper()
	if err := f.st.PutRows(context.Background(), r.ID, rows); err != nil {
		t.Fatal(err)
	}
	*f.now = f.now.Add(time.Second)
	r, err := f.st.FinishRun(context.Background(), r.ID)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func expectedRun(r store.Run, layout string) RunView {
	by := r.UserEmail
	if r.Trigger != "" {
		by = ""
	}
	finished, text := "", ""
	if !r.Finished.IsZero() {
		finished = r.Finished.UTC().Format(time.RFC3339)
		text = r.Finished.UTC().Format(layout)
	}
	return RunView{r.ID, r.Scraper, r.Status, r.Reason, by, r.Trigger, r.Started.UTC().Format(time.RFC3339), r.Started.UTC().Format(layout), finished, text, r.Rows, r.New, r.Changed, r.Unchanged, r.Gone}
}
func expectedLanding(r store.Run) RunRow {
	v := expectedRun(r, MinuteLayout)
	return RunRow{v.ID, v.Scraper, v.Status, v.Reason, v.StartedBy, v.Trigger, v.Started, v.StartedText, v.Finished, v.FinishedText, v.Rows}
}
func expectedCurrent(r store.Run) *CurrentRun {
	v := expectedRun(r, MinuteLayout)
	return &CurrentRun{v.ID, v.Status, v.Started, v.StartedText, v.Finished, v.FinishedText, v.Rows, v.New, v.Changed, v.Gone}
}

// R-0MB6-K2N2 R-F477-FYMY R-FVJU-0IFY R-FRDA-PLQ5
// R-FT41-8YYK R-F6N0-7I4C R-F7UW-L9V1 R-F92S-Z1LQ R-FAAP-CTCF
// R-FUBX-MQP9 R-FCQI-4CTT R-FF6A-VWB7 R-FGE7-9O1W R-FHM3-NFSL
// R-FIU0-17JA R-FK1W-EZ9Z R-FL9S-SR0O R-FMHP-6IRD R-FNPL-KAI2 R-FOXH-Y28R
func TestLoadAndWrite(t *testing.T) {
	t.Chdir(t.TempDir())
	s, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if ServiceName != "scrapes" || MinuteLayout != "2006-01-02 15:04" || SecondLayout != "2006-01-02 15:04:05 UTC" || Description == "" || ToolsLevel != (page.Level{Name: "tools", URL: "/tools"}) || AboutLevel != (page.Level{Name: "about", URL: "/about"}) {
		t.Fatal("constants")
	}
	manifest, err := fs.ReadFile(app.Etc(), "manifest.toml")
	if err != nil {
		t.Fatal(err)
	}
	manifestDescription := ""
	for _, line := range strings.Split(string(manifest), "\n") {
		key, value, found := strings.Cut(line, "=")
		if found && strings.TrimSpace(key) == "description" {
			manifestDescription, err = strconv.Unquote(strings.TrimSpace(value))
			if err != nil {
				t.Fatal(err)
			}
			break
		}
	}
	if manifestDescription == "" || Description != manifestDescription {
		t.Fatal("description differs from embedded manifest")
	}
	b := baseBanner()
	cases := []struct {
		name string
		data any
	}{{"landing", LandingData{b, []ScraperRow{}, []RunRow{}, "", "", ""}}, {"tools", ToolsData{b, []ToolRow{{"test-tool", "test-description"}}}}, {"about", AboutData{b, Description}}, {"notfound", NotFoundData{b}}, {"row", RowData{b, RowView{}}}, {"row", RowData{b, RowView{"test-scraper", "test-run", "test-source", "collected", "collected-text", "digest", "test-name", []string{"one", "two"}, "street", "city", "state", "zip", "county", "lat", "lon", []PersonRow{{"person", "role"}}, []FactRow{{"key", "value"}}, "raw"}}}}
	for _, status := range []string{store.StatusRunning, store.StatusDone, store.StatusFailed} {
		for _, reason := range []string{"", store.ReasonSource, store.ReasonInterrupted, store.ReasonStore} {
			for _, full := range []bool{false, true} {
				r := RunView{ID: "fixture", Status: status, Reason: reason}
				rows := []RowLine{}
				next := ""
				landingNext := ""
				current := (*CurrentRun)(nil)
				running := (*RunningRun)(nil)
				if full {
					r.Trigger = "test-trigger"
					r.Finished = "test-finished"
					r.FinishedText = "test-text"
					rows = []RowLine{{"source", "name", "city", "state", "county"}}
					next = "test-page-token"
					landingNext = "test-page-token"
					current = &CurrentRun{"fixture", status, "started", "start-text", "finished", "finish-text", 1, 2, 3, 4}
					running = &RunningRun{"running"}
				}
				cases = append(cases, struct {
					name string
					data any
				}{"run", RunData{b, r, rows, "", "", next}}, struct {
					name string
					data any
				}{"landing", LandingData{b, []ScraperRow{{"scraper", current, running}}, []RunRow{{"fixture", "scraper", status, reason, "caller", r.Trigger, "started", "started-text", r.Finished, r.FinishedText, 7}}, "", "", landingNext}})
			}
		}
	}
	for _, clear := range []func(*RowView){
		func(v *RowView) { v.Street = "" }, func(v *RowView) { v.City = "" }, func(v *RowView) { v.State = "" }, func(v *RowView) { v.Zip = "" }, func(v *RowView) { v.County = "" }, func(v *RowView) { v.Lat = "" }, func(v *RowView) { v.Lon = "" }, func(v *RowView) { v.People = nil }, func(v *RowView) { v.Facts = nil },
	} {
		v := RowView{Street: "street", City: "city", State: "state", Zip: "zip", County: "county", Lat: "lat", Lon: "lon", People: []PersonRow{{"person", "role"}}, Facts: []FactRow{{"key", "value"}}}
		clear(&v)
		cases = append(cases, struct {
			name string
			data any
		}{"row", RowData{b, v}})
	}
	initialCases := append([]struct {
		name string
		data any
	}{}, cases...)
	for _, tc := range initialCases {
		for _, pageValue := range []string{"", "test-current-token"} {
			for _, next := range []string{"", "test-next-token"} {
				for _, prev := range []string{"", "test-prev-token"} {
					switch d := tc.data.(type) {
					case LandingData:
						d.Page, d.Prev, d.Next = pageValue, prev, next
						cases = append(cases, struct {
							name string
							data any
						}{tc.name, d})
					case RunData:
						d.Page, d.Prev, d.Next = pageValue, prev, next
						cases = append(cases, struct {
							name string
							data any
						}{tc.name, d})
					}
				}
			}
		}
	}
	for _, tc := range cases {
		for _, method := range []string{http.MethodGet, http.MethodHead} {
			w := httptest.NewRecorder()
			w.Header().Add("Content-Type", "prior")
			s.Write(w, httptest.NewRequest(method, "/", nil), 201, tc.name, tc.data)
			if w.Code != 201 || !reflect.DeepEqual(w.Header().Values("Content-Type"), []string{"text/html; charset=utf-8"}) {
				t.Fatal(w.Result().Header)
			}
			want := rendered(t, tc.name, tc.data)
			if method == http.MethodHead {
				want = ""
			}
			if w.Body.String() != want {
				t.Fatal(tc.name, method)
			}
		}
	}
}

// R-FSL7-3DGU R-FV0Z-UWY8 R-G2V8-B4W4 R-G8FW-2E3V
func TestLanding(t *testing.T) {
	f := setup(t)
	b := baseBanner()
	b.Trail = nil
	assertBody(t, request(f.h, "GET", "/", "first"), 200, "landing", LandingData{b, []ScraperRow{{Name: "alpha"}, {Name: "zeta"}}, []RunRow{}, "", "", ""})
	done := f.finish(t, f.add(t, "alpha", ""), nil)
	failed := f.add(t, "zeta", "cron-slug")
	var err error
	failed, err = f.st.FailRun(context.Background(), failed.ID, store.ReasonSource)
	if err != nil {
		t.Fatal(err)
	}
	running := f.add(t, "alpha", "")
	d := LandingData{b, []ScraperRow{{"alpha", expectedCurrent(done), &RunningRun{running.ID}}, {Name: "zeta"}}, []RunRow{expectedLanding(running), expectedLanding(failed), expectedLanding(done)}, "", "", ""}
	assertBody(t, request(f.h, "GET", "/", "first"), 200, "landing", d)
}

// R-GDUB-R2KD R-GF28-4UB2 R-FV0Z-UWY8 R-EPO9-X5IF R-FZWL-DZX0
func TestRunPages(t *testing.T) {
	f := setup(t)
	callRows := rowsTool(t, f)
	for _, count := range []int{0, paging.DefaultLimit, paging.DefaultLimit + 1, paging.DefaultLimit + 5} {
		r := f.add(t, "alpha", "")
		rows := []source.Row{}
		lines := []RowLine{}
		for i := count - 1; i >= 0; i-- {
			id := fmt.Sprintf("%03d", i)
			rows = append(rows, source.Row{SourceID: id, Name: "name-" + id, Names: []string{"name-" + id}, Address: source.Address{City: "city-" + id, State: "state", County: "county"}, Facts: []byte(`{}`), Raw: []byte(`{}`)})
		}
		r = f.finish(t, r, rows)
		for i := 0; i < count && i < paging.DefaultLimit; i++ {
			id := fmt.Sprintf("%03d", i)
			lines = append(lines, RowLine{id, "name-" + id, "city-" + id, "state", "county"})
		}
		next := ""
		if count > paging.DefaultLimit {
			next = paging.Token{Tool: "rows"}.Encode(lines[len(lines)-1].SourceID)
		}
		b := baseBanner()
		b.Trail = []page.Level{{Name: r.ID, URL: "/runs/" + r.ID}}
		assertBody(t, request(f.h, "GET", "/runs/"+r.ID, "user"), 200, "run", RunData{b, expectedRun(r, SecondLayout), lines, "", "", next})
		first := callRows(r.ID, "")
		toolLines := []RowLine{}
		for _, row := range first.Rows {
			toolLines = append(toolLines, RowLine{row.SourceID, row.Name, row.Address.City, row.Address.State, row.Address.County})
		}
		toolNext := ""
		if first.Next != nil {
			toolNext = *first.Next
		}
		if !reflect.DeepEqual(toolLines, lines) || toolNext != next {
			t.Fatal("run page differs from rows tool")
		}
		if next != "" {
			last := callRows(r.ID, next)
			if len(last.Rows) != count-paging.DefaultLimit || last.Next != nil {
				t.Fatal("cursor did not return remaining rows")
			}
			d := RunData{b, expectedRun(r, SecondLayout), []RowLine{}, next, "", ""}
			for _, row := range last.Rows {
				d.Rows = append(d.Rows, rowLine(row))
				if len(d.Rows) == 1 {
					d.Prev = paging.Token{Tool: "rows-before"}.Encode(row.SourceID)
				}
			}
			assertBody(t, request(f.h, "GET", "/runs/"+r.ID+"?page="+url.QueryEscape(next), "user"), 200, "run", d)
			for i, row := range last.Rows {
				if row.SourceID != fmt.Sprintf("%03d", paging.DefaultLimit+i) {
					t.Fatal("remaining rows out of order")
				}
			}
		}
	}
	for _, reason := range []string{"", store.ReasonSource, store.ReasonInterrupted, store.ReasonStore} {
		r := f.add(t, "alpha", "test-trigger")
		if reason != "" {
			var err error
			r, err = f.st.FailRun(context.Background(), r.ID, reason)
			if err != nil {
				t.Fatal(err)
			}
		}
		b := baseBanner()
		b.Trail = []page.Level{{Name: r.ID, URL: "/runs/" + r.ID}}
		assertBody(t, request(f.h, "GET", "/runs/"+r.ID, "user"), 200, "run", RunData{b, expectedRun(r, SecondLayout), []RowLine{}, "", "", ""})
		if reason == "" {
			f.finish(t, r, nil)
		}
	}
}

// R-EY7K-LJPA R-EPO9-X5IF R-FZWL-DZX0
func TestRowPage(t *testing.T) {
	f := setup(t)
	r := f.add(t, "alpha", "")
	lat, lon := 46.25, -123.125
	w := source.Row{SourceID: "part/with space%?", Collected: *f.now, Digest: source.Digest([]byte(`{"first":"one","second":"two"}`)), Name: "test-company", Names: []string{"test-company", "test-alias"}, Address: source.Address{Street: "test-street", City: "test-city", State: "test-state", Zip: "test-zip", County: "test-county"}, Lat: &lat, Lon: &lon, People: []source.Person{{Name: "test-person", Role: "test-role"}}, Facts: []byte(`{"z": null, "a": ["one", "two"], "m": "14"}`), Raw: []byte(`{"first":"one","second":"two"}`)}
	r = f.finish(t, r, []source.Row{w})
	got, err := f.st.GetRow(context.Background(), r.ID, w.SourceID)
	if err != nil {
		t.Fatal(err)
	}
	v := RowView{r.Scraper, r.ID, w.SourceID, got.Collected.UTC().Format(time.RFC3339), got.Collected.UTC().Format(SecondLayout), got.Digest, w.Name, w.Names, "test-street", "test-city", "test-state", "test-zip", "test-county", "46.25", "-123.125", []PersonRow{{"test-person", "test-role"}}, []FactRow{{"a", `["one","two"]`}, {"m", `"14"`}, {"z", "null"}}, "{\n  \"first\": \"one\",\n  \"second\": \"two\"\n}"}
	p := "/runs/" + r.ID + "/rows/" + url.PathEscape(w.SourceID)
	b := baseBanner()
	b.Trail = []page.Level{{Name: r.ID, URL: "/runs/" + r.ID}, {Name: w.SourceID, URL: p}}
	assertBody(t, request(f.h, "GET", p, "user"), 200, "row", RowData{b, v})
}

// R-G603-AUMH R-G77Z-OMD6 R-G9NS-G5UK R-GAVO-TXL9 R-GDBH-LH2N
// R-GMDM-FGR8 R-G4S6-X2VS R-G3KA-JB53
func TestRoutesMethodsAndFailures(t *testing.T) {
	f := setup(t)
	r := f.add(t, "alpha", "")
	id := r.ID
	paths := []string{"/runs/" + strings.ToUpper(id[4:]), "/runs/run_" + strings.ToUpper(id[4:]), "/runs/" + id[:len(id)-1], "/runs/" + id + "0", "/runs/" + id + "/", "/runs/%72" + id[1:], "/" + id, "/runs", "/runs/", "/runs/" + id + "/rows/", "/runs/" + id + "/rows/1/2", "/runs/" + id[:len(id)-1] + "/rows/1", "/unknown"}
	b := baseBanner()
	nf := NotFoundData{b}
	// Running rows and absent runs share the complete not-found response.
	baseline := request(f.h, "GET", "/unknown", "user")
	for _, p := range []string{"/runs/run_0000000000000000", "/runs/" + id + "/rows/missing"} {
		got := request(f.h, "GET", p, "user")
		if got.Code != baseline.Code || !reflect.DeepEqual(got.Header(), baseline.Header()) || got.Body.String() != baseline.Body.String() {
			t.Fatal("notfound mismatch", p)
		}
	}
	done := f.finish(t, r, nil)
	newer := f.finish(t, f.add(t, "alpha", ""), nil)
	_ = newer
	if _, err := f.st.PruneRuns(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	assertBody(t, request(f.h, "GET", "/runs/"+done.ID, "user"), 404, "notfound", nf)
	assertBody(t, request(f.h, "GET", "/runs/"+done.ID+"/rows/missing", "user"), 404, "notfound", nf)
	assertBody(t, request(f.h, "GET", "/runs/"+newer.ID+"/rows/missing", "user"), 404, "notfound", nf)
	failed := f.add(t, "zeta", "")
	if _, err := f.st.FailRun(context.Background(), failed.ID, store.ReasonSource); err != nil {
		t.Fatal(err)
	}
	assertBody(t, request(f.h, "GET", "/runs/"+failed.ID+"/rows/missing", "user"), 404, "notfound", nf)
	f.handle.SetFailing(true)
	for _, p := range paths {
		for _, method := range []string{"GET", "POST"} {
			assertBody(t, request(f.h, method, p, "user"), 404, "notfound", nf)
		}
	}
	toolsData := ToolsData{Banner: b, Tools: []ToolRow{}}
	toolsData.Banner.Trail = []page.Level{ToolsLevel}
	for _, e := range tools.Catalog() {
		toolsData.Tools = append(toolsData.Tools, ToolRow{e.Name, e.Description})
	}
	ab := b
	ab.Trail = []page.Level{AboutLevel}
	assertBody(t, request(f.h, "GET", "/tools", "user"), 200, "tools", toolsData)
	assertBody(t, request(f.h, "GET", "/about", "user"), 200, "about", AboutData{ab, Description})
	for _, p := range []string{"/", "/tools", "/about", "/runs/" + id, "/runs/" + id + "/rows/missing"} {
		for _, method := range []string{"POST", "PUT", "DELETE", "OPTIONS"} {
			got := request(f.h, method, p, "user")
			if got.Code != 405 || got.Header().Get("Allow") != "GET, HEAD" || got.Body.Len() != 0 {
				t.Fatal(p, method, got.Code)
			}
		}
	}
	for _, p := range []string{"/", "/runs/" + id, "/runs/" + id + "/rows/missing"} {
		got := request(f.h, "GET", p, "user")
		if got.Code != 503 || got.Body.String() != store.Unreachable+"\n" || got.Header().Get("Content-Type") != "text/plain; charset=utf-8" {
			t.Fatal(p, got.Code, got.Body.String())
		}
	}
	for _, failing := range []bool{true, false} {
		f.handle.SetFailing(failing)
		for _, p := range []string{"/", "/tools", "/about", "/unknown", "/runs/" + id, "/runs/" + id + "/rows/missing"} {
			get, head := request(f.h, "GET", p, "user"), request(f.h, "HEAD", p, "user")
			if head.Code != get.Code || !reflect.DeepEqual(head.Header(), get.Header()) || head.Body.Len() != 0 {
				t.Fatal("HEAD mismatch", p)
			}
		}
	}
}

// R-8LWH-MXGF R-GFRA-D0K1 R-GGZ6-QSAQ
func TestReadOnlyPrivacyAndSharedRecords(t *testing.T) {
	f := setup(t)
	ctx := context.Background()
	r := f.add(t, "alpha", "")
	secrets := []string{"private-business", "private-alias", "private-street", "private-city", "private-state", "private-zip", "private-county", "private-person", "private-phone", "private-email"}
	w := source.Row{SourceID: "one", Name: secrets[0], Names: secrets[:2], Address: source.Address{Street: secrets[2], City: secrets[3], State: secrets[4], Zip: secrets[5], County: secrets[6]}, People: []source.Person{{Name: secrets[7]}}, Facts: []byte(fmt.Sprintf(`{"phone":%q,"email":%q}`, secrets[8], secrets[9])), Raw: []byte(`{}`)}
	r = f.finish(t, r, []source.Row{w})
	running := f.add(t, "alpha", "")
	*f.now = f.now.Add(48 * time.Hour)
	beforeRuns, err := f.st.ListRunsAfter(ctx, "", "", "", 1000)
	if err != nil {
		t.Fatal(err)
	}
	beforeRun, err := f.st.GetRun(ctx, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	beforeRows, err := f.st.ListRowsAfter(ctx, r.ID, "", "", 1000)
	if err != nil {
		t.Fatal(err)
	}
	beforeRow, err := f.st.GetRow(ctx, r.ID, "one")
	if err != nil {
		t.Fatal(err)
	}
	oldBanner := baseBanner()
	oldBanner.Trail = []page.Level{{Name: running.ID, URL: "/runs/" + running.ID}}
	assertBody(t, request(f.h, "GET", "/runs/"+running.ID, "owner"), 200, "run", RunData{oldBanner, expectedRun(running, SecondLayout), []RowLine{}, "", "", ""})
	paths := []string{"/", "/tools", "/about", "/unknown", "/runs/" + r.ID, "/runs/" + r.ID + "/rows/one", "/runs/" + running.ID, "/runs/" + running.ID + "/rows/one"}
	for _, p := range paths {
		first, second := request(f.h, "GET", p, "owner"), request(f.h, "GET", p, "other")
		if first.Code != second.Code || first.Body.String() != second.Body.String() || !reflect.DeepEqual(first.Header(), second.Header()) {
			t.Fatal("user affected output", p)
		}
		if len(first.Header().Values("Set-Cookie")) != 0 {
			t.Fatal("cookie", p)
		}
		if p == "/" || p == "/tools" || p == "/about" || p == "/unknown" {
			for _, s := range secrets {
				if strings.Contains(first.Body.String(), s) {
					t.Fatal("private row data", p, s)
				}
			}
		}
	}
	afterRuns, err := f.st.ListRunsAfter(ctx, "", "", "", 1000)
	if err != nil {
		t.Fatal(err)
	}
	afterRun, err := f.st.GetRun(ctx, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	afterRows, err := f.st.ListRowsAfter(ctx, r.ID, "", "", 1000)
	if err != nil {
		t.Fatal(err)
	}
	afterRow, err := f.st.GetRow(ctx, r.ID, "one")
	if err != nil {
		t.Fatal(err)
	}
	afterRunning, err := f.st.GetRun(ctx, running.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(beforeRuns, afterRuns) || !reflect.DeepEqual(beforeRun, afterRun) || !reflect.DeepEqual(beforeRows, afterRows) || !reflect.DeepEqual(beforeRow, afterRow) || afterRunning.Status != store.StatusRunning || *f.calls != 0 {
		t.Fatal("read mutated state or called scraper")
	}
}

// R-G2V8-B4W4 R-G434-OWMT
func TestLandingCursorPageMatchesRunsTool(t *testing.T) {
	for _, count := range []int{paging.DefaultLimit, paging.DefaultLimit + 1} {
		t.Run(strconv.Itoa(count), func(t *testing.T) {
			f := setup(t)
			recorded := []store.Run{}
			for i := 0; i < count; i++ {
				name := "alpha"
				if i%2 != 0 {
					name = "zeta"
				}
				recorded = append(recorded, f.finish(t, f.add(t, name, ""), nil))
			}
			b := baseBanner()
			b.Trail = nil
			d := LandingData{b, []ScraperRow{}, []RunRow{}, "", "", ""}
			for _, name := range f.cfg.Scrapers.Names() {
				r, err := f.st.CurrentRun(t.Context(), name)
				if err != nil {
					t.Fatal(err)
				}
				d.Scrapers = append(d.Scrapers, ScraperRow{Name: name, Current: expectedCurrent(r)})
			}
			for i := count - 1; i >= count-paging.DefaultLimit; i-- {
				d.Runs = append(d.Runs, expectedLanding(recorded[i]))
			}
			if count > paging.DefaultLimit {
				last := recorded[count-paging.DefaultLimit]
				d.Next = paging.Token{Tool: "runs"}.Encode(last.Started.UTC().Format(time.RFC3339), last.ID)
			}
			assertBody(t, request(f.h, "GET", "/", "user"), 200, "landing", d)
			writer := telemetry.New(telemetry.Config{Service: ServiceName, Version: "test", Sink: &telemetry.Capture{}, Now: func() time.Time { return *f.now }, Rand: bytes.NewReader(make([]byte, 65536)), Sleep: func(context.Context, time.Duration) {}, Stderr: io.Discard})
			defer writer.Shutdown(context.Background(), "test")
			srv := mcp.NewServer(mcp.ServerConfig{Name: ServiceName, Version: "test", Telemetry: writer})
			tools.Register(srv, tools.Config{Store: f.st, Scrapers: f.cfg.Scrapers})
			server := httptest.NewServer(identity.Require(srv))
			defer server.Close()
			client := mcp.NewClient(mcp.ClientConfig{Endpoint: server.URL, HTTPClient: server.Client(), Name: "test", Version: "test"})
			call := func(args json.RawMessage) tools.RunList {
				t.Helper()
				result, err := client.CallTool(t.Context(), identity.Caller{UserID: "test-user"}, "runs", args)
				if err != nil || result.IsError() {
					t.Fatalf("runs: %v %+v", err, result)
				}
				encoded, err := json.Marshal(result)
				if err != nil {
					t.Fatal(err)
				}
				var wire struct {
					Structured tools.RunList `json:"structuredContent"`
				}
				if err := json.Unmarshal(encoded, &wire); err != nil {
					t.Fatal(err)
				}
				return wire.Structured
			}
			first := call(json.RawMessage(`{}`))
			if len(first.Runs) != len(d.Runs) {
				t.Fatal("run page length differs")
			}
			for i, r := range first.Runs {
				if r.ID != d.Runs[i].ID {
					t.Fatal("run page order differs")
				}
			}
			next := ""
			if first.Next != nil {
				next = *first.Next
			}
			if next != d.Next {
				t.Fatal("run page token differs")
			}
			if next != "" {
				args, err := json.Marshal(map[string]string{"page": next})
				if err != nil {
					t.Fatal(err)
				}
				last := call(args)
				d.Page, d.Next = next, ""
				d.Runs = []RunRow{expectedLanding(recorded[0])}
				d.Prev = paging.Token{Tool: "runs-before"}.Encode(recorded[0].Started.UTC().Format(time.RFC3339), recorded[0].ID)
				assertBody(t, request(f.h, "GET", "/?page="+url.QueryEscape(next), "user"), 200, "landing", d)
				if len(last.Runs) != 1 || last.Runs[0].ID != recorded[0].ID || last.Next != nil {
					t.Fatal("cursor did not return remaining run")
				}
			}
		})
	}
}

// R-FV0Z-UWY8 R-EPO9-X5IF
func TestViewConversions(t *testing.T) {
	tm := time.Date(2026, 10, 5, 10, 0, 7, 999, time.FixedZone("test", 3600))
	r := store.Run{ID: "test-id", Scraper: "test-scraper", Status: store.StatusDone, Started: tm, Finished: tm.Add(3 * time.Second), Rows: 15, New: 4, Changed: 3, Unchanged: 8, Gone: 7, UserEmail: "owner@example.test"}
	for _, trigger := range []string{"", "scrape_test"} {
		r.Trigger = trigger
		for _, finished := range []time.Time{{}, tm.Add(3 * time.Second)} {
			r.Finished = finished
			if !reflect.DeepEqual(landingRow(r), expectedLanding(r)) || !reflect.DeepEqual(runView(r), expectedRun(r, SecondLayout)) || !reflect.DeepEqual(currentView(r), *expectedCurrent(r)) {
				t.Fatal("run conversion")
			}
		}
	}
	lat, lon := 46.25, -123.125
	w := source.Row{Scraper: "s", Run: "r", SourceID: "id", Collected: tm, Digest: "digest", Name: "name", Names: []string{"name", "alias"}, Address: source.Address{Street: "street", City: "city", State: "state", Zip: "zip", County: "county"}, Lat: &lat, Lon: &lon, People: []source.Person{{Name: "person", Role: "role"}}, Facts: []byte(`{"z": null,"a": [1, 2]}`), Raw: []byte(`{"second":2,"first":1}`)}
	expected := RowView{"s", "r", "id", "2026-10-05T09:00:07Z", "2026-10-05 09:00:07 UTC", "digest", "name", []string{"name", "alias"}, "street", "city", "state", "zip", "county", "46.25", "-123.125", []PersonRow{{"person", "role"}}, []FactRow{{"a", "[1,2]"}, {"z", "null"}}, "{\n  \"second\": 2,\n  \"first\": 1\n}"}
	if !reflect.DeepEqual(rowView(w), expected) || rowLine(w) != (RowLine{"id", "name", "city", "state", "county"}) {
		t.Fatal("row conversion")
	}
	w.Lat = nil
	w.Lon = nil
	w.People = nil
	w.Facts = nil
	w.Address = source.Address{}
	v := rowView(w)
	if v.Lat != "" || v.Lon != "" || v.Street != "" || v.City != "" || v.State != "" || v.Zip != "" || v.County != "" || len(v.People) != 0 || len(v.Facts) != 0 {
		t.Fatal("optional row conversion")
	}
}

// R-G8FW-2E3V
func TestBannerUserFromServices(t *testing.T) {
	f := setup(t)
	path := filepath.Join(t.TempDir(), "services.json")
	if err := os.WriteFile(path, []byte(`{"services":[{"name":"auth","url":"https://identity.example.test///","description":"test","socket":"","enabled":true,"mcp":false}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	cfg := f.cfg
	cfg.ServicesPath = path
	var users []page.User
	cfg.Banner = func(u page.User) page.Banner { users = append(users, u); return banner(u) }
	h := identity.Require(Handler(cfg))
	request(h, "GET", "/about", "owner")
	want := page.User{Email: "reader@example.test", ProfileURL: "https://identity.example.test/", LogoutURL: "https://identity.example.test/logout"}
	if len(users) != 1 || users[0] != want {
		t.Fatal(users)
	}
}

func rowsTool(t *testing.T, f fixture) func(string, string) tools.RowList {
	t.Helper()
	writer := telemetry.New(telemetry.Config{Service: ServiceName, Version: "test", Sink: &telemetry.Capture{}, Now: func() time.Time { return *f.now }, Rand: bytes.NewReader(make([]byte, 65536)), Sleep: func(context.Context, time.Duration) {}, Stderr: io.Discard})
	t.Cleanup(func() { writer.Shutdown(context.Background(), "test") })
	srv := mcp.NewServer(mcp.ServerConfig{Name: ServiceName, Version: "test", Telemetry: writer})
	tools.Register(srv, tools.Config{Store: f.st, Scrapers: f.cfg.Scrapers})
	server := httptest.NewServer(identity.Require(srv))
	t.Cleanup(server.Close)
	client := mcp.NewClient(mcp.ClientConfig{Endpoint: server.URL, HTTPClient: server.Client(), Name: "test", Version: "test"})
	return func(run, token string) tools.RowList {
		t.Helper()
		args := map[string]string{"run": run}
		if token != "" {
			args["page"] = token
		}
		encodedArgs, err := json.Marshal(args)
		if err != nil {
			t.Fatal(err)
		}
		result, err := client.CallTool(t.Context(), identity.Caller{UserID: "test-user"}, "rows", encodedArgs)
		if err != nil || result.IsError() {
			t.Fatalf("rows: %v %+v", err, result)
		}
		encoded, err := json.Marshal(result)
		if err != nil {
			t.Fatal(err)
		}
		var wire struct {
			Structured struct {
				Rows []struct {
					SourceID string         `json:"source_id"`
					Name     string         `json:"name"`
					Address  source.Address `json:"address"`
				} `json:"rows"`
				Next *string `json:"next"`
			} `json:"structuredContent"`
		}
		if err := json.Unmarshal(encoded, &wire); err != nil {
			t.Fatal(err)
		}
		decoded := tools.RowList{Rows: []source.Row{}, Next: wire.Structured.Next}
		for _, row := range wire.Structured.Rows {
			decoded.Rows = append(decoded.Rows, source.Row{SourceID: row.SourceID, Name: row.Name, Address: row.Address})
		}
		return decoded
	}
}

// R-FWRQ-EA6N R-G5B1-2ODI R-GGA4-IM1R R-GMDM-FGR8
func TestPageTokenRefusals(t *testing.T) {
	f := setup(t)
	pruned := f.finish(t, f.add(t, "alpha", ""), nil)
	done := f.finish(t, f.add(t, "alpha", ""), []source.Row{{SourceID: "one", Facts: []byte(`{}`), Raw: []byte(`{}`)}})
	if _, err := f.st.PruneRuns(t.Context(), 1); err != nil {
		t.Fatal(err)
	}
	failed := f.add(t, "alpha", "")
	if _, err := f.st.FailRun(t.Context(), failed.ID, store.ReasonSource); err != nil {
		t.Fatal(err)
	}
	running := f.add(t, "alpha", "")
	runsToken := (paging.Token{Tool: "runs"}).Encode(done.Started.UTC().Format(time.RFC3339), done.ID)
	rowsToken := (paging.Token{Tool: "rows"}).Encode("one")
	landingTokens := []string{"LAST", "last/", (paging.Token{Tool: "rows-before"}).Encode("one"), (paging.Token{Tool: "runs-before"}).Encode("time"), "bogus", rowsToken, (paging.Token{Tool: "runs"}).Encode("time"), (paging.Token{Tool: "runs"}).Encode("time", "id", "extra")}
	runTokens := []string{"LAST", "last/", (paging.Token{Tool: "runs-before"}).Encode("time", "id"), (paging.Token{Tool: "rows-before"}).Encode("one", "extra"), "bogus", runsToken, (paging.Token{Tool: "rows"}).Encode("one", "extra")}
	for _, failing := range []bool{false, true} {
		f.handle.SetFailing(failing)
		for _, method := range []string{"GET", "HEAD"} {
			baseline := request(f.h, method, "/unknown", "user")
			check := func(path string) {
				t.Helper()
				got := request(f.h, method, path, "user")
				if got.Code != baseline.Code || !reflect.DeepEqual(got.Header(), baseline.Header()) || got.Body.String() != baseline.Body.String() {
					t.Fatalf("invalid page differs from notfound: %s failing=%v", path, failing)
				}
			}
			for _, token := range landingTokens {
				check("/?page=" + url.QueryEscape(token) + "&page=&limit=1&scraper=alpha")
			}
			for _, id := range []string{pruned.ID, done.ID, failed.ID, running.ID, "run_0000000000000000"} {
				for _, token := range runTokens {
					check("/runs/" + id + "?page=" + url.QueryEscape(token) + "&page=&name=one&limit=1")
				}
			}
			for _, path := range []string{"/?page=last", "/?page=" + url.QueryEscape((paging.Token{Tool: "runs-before"}).Encode("time", "id")), "/runs/" + done.ID + "?page=last", "/runs/" + done.ID + "?page=" + url.QueryEscape((paging.Token{Tool: "rows-before"}).Encode("one")), "/?page=", "/?page=" + url.QueryEscape(runsToken), "/runs/" + done.ID + "?page=", "/runs/" + done.ID + "?page=" + url.QueryEscape(rowsToken)} {
				if failing {
					got := request(f.h, method, path, "user")
					body := store.Unreachable + "\n"
					if method == "HEAD" {
						body = ""
					}
					if got.Code != 503 || got.Body.String() != body || got.Header().Get("Content-Type") != "text/plain; charset=utf-8" {
						t.Fatal("decodable page outage", path, got.Code)
					}
				}
			}
		}
		for _, path := range []string{"/?page=bogus", "/runs/" + done.ID + "?page=bogus"} {
			got := request(f.h, "POST", path, "user")
			if got.Code != 405 || got.Header().Get("Allow") != "GET, HEAD" || got.Body.Len() != 0 {
				t.Fatal("method must precede token", path)
			}
		}
	}
	f.handle.SetFailing(false)
	for _, path := range []string{"/runs/" + done.ID + "/rows/one", "/about", "/tools", "/unknown"} {
		for _, method := range []string{"GET", "HEAD"} {
			plain, queried := request(f.h, method, path, "user"), request(f.h, method, path+"?page=bogus&name=missing&limit=0", "user")
			if plain.Code != queried.Code || !reflect.DeepEqual(plain.Header(), queried.Header()) || plain.Body.String() != queried.Body.String() {
				t.Fatal("query affected unpaged path", path)
			}
		}
	}
}

// R-FWRQ-EA6N R-G2V8-B4W4 R-GDUB-R2KD
func TestPageQuerySelection(t *testing.T) {
	f := setup(t)
	done := f.finish(t, f.add(t, "alpha", ""), []source.Row{{SourceID: "one", Name: "chosen", Facts: []byte(`{}`), Raw: []byte(`{}`)}})
	runsToken := (paging.Token{Tool: "runs"}).Encode(done.Started.UTC().Format(time.RFC3339), done.ID)
	rowsToken := (paging.Token{Tool: "rows"}).Encode("one")
	for _, path := range []string{"/", "/runs/" + done.ID} {
		for _, query := range []string{"?page=", "?page=&page=bogus", "?name=missing&scraper=missing&limit=0&offset=999"} {
			plain, queried := request(f.h, "GET", path, "user"), request(f.h, "GET", path+query, "user")
			if plain.Code != queried.Code || !reflect.DeepEqual(plain.Header(), queried.Header()) || plain.Body.String() != queried.Body.String() {
				t.Fatal("first empty page or unrelated query affected response", path, query)
			}
		}
	}
	b := baseBanner()
	b.Trail = nil
	landing := LandingData{b, []ScraperRow{{"alpha", expectedCurrent(done), nil}, {Name: "zeta"}}, []RunRow{}, runsToken, "", ""}
	assertBody(t, request(f.h, "GET", "/?page="+url.QueryEscape(runsToken)+"&page=bogus&scraper=missing&limit=1", "user"), 200, "landing", landing)
	b.Trail = []page.Level{{Name: done.ID, URL: "/runs/" + done.ID}}
	run := RunData{b, expectedRun(done, SecondLayout), []RowLine{}, rowsToken, "", ""}
	assertBody(t, request(f.h, "GET", "/runs/"+done.ID+"?page="+url.QueryEscape(rowsToken)+"&page=bogus&name=missing&limit=1", "user"), 200, "run", run)
	for _, path := range []string{"/?page=" + url.QueryEscape(runsToken), "/runs/" + done.ID + "?page=" + url.QueryEscape(rowsToken)} {
		get, head := request(f.h, "GET", path, "user"), request(f.h, "HEAD", path, "user")
		if get.Code != head.Code || !reflect.DeepEqual(get.Header(), head.Header()) || head.Body.Len() != 0 {
			t.Fatal("paged HEAD mismatch", path)
		}
	}
}

// R-GDUB-R2KD
func TestNonDoneRunPreservesDecodablePage(t *testing.T) {
	f := setup(t)
	for _, token := range []string{(paging.Token{Tool: "rows"}).Encode("position"), (paging.Token{Tool: "rows-before"}).Encode("position"), "last"} {
		for _, reason := range []string{"", store.ReasonSource, store.ReasonInterrupted, store.ReasonStore} {
			r := f.add(t, "alpha", "")
			if reason != "" {
				var err error
				r, err = f.st.FailRun(t.Context(), r.ID, reason)
				if err != nil {
					t.Fatal(err)
				}
			}
			b := baseBanner()
			b.Trail = []page.Level{{Name: r.ID, URL: "/runs/" + r.ID}}
			d := RunData{b, expectedRun(r, SecondLayout), []RowLine{}, token, "", ""}
			path := "/runs/" + r.ID + "?page=" + url.QueryEscape(token)
			assertBody(t, request(f.h, "GET", path, "user"), 200, "run", d)
			// The production template hides navigation for these statuses. A renderer
			// exposing the data proves the carried Page and empty Rows/Next too.
			cfg := f.cfg
			var observed RunData
			cfg.Pages = &Set{template.Must(template.New("run").Funcs(template.FuncMap{
				"observe": func(data RunData) string { observed = data; return "" },
			}).Parse("{{observe .}}"))}
			got := request(identity.Require(Handler(cfg)), "GET", path, "user")
			if got.Code != 200 || !reflect.DeepEqual(observed, d) {
				t.Fatal("non-done page data", got.Code, got.Body.String())
			}
			if reason == "" {
				f.finish(t, r, nil)
			}
		}
	}
}

func assertRedirect(t *testing.T, w *httptest.ResponseRecorder, path string) {
	t.Helper()
	if w.Code != http.StatusSeeOther || !reflect.DeepEqual(w.Header().Values("Location"), []string{path}) || w.Body.Len() != 0 {
		t.Fatalf("redirect: status=%d headers=%v body=%q", w.Code, w.Header(), w.Body.String())
	}
}

// Observe the handler's data, then independently execute the embedded asset.
func observedLanding(t *testing.T, f fixture, value string) (LandingData, *httptest.ResponseRecorder) {
	t.Helper()
	var d LandingData
	cfg := f.cfg
	cfg.Pages = &Set{template.Must(template.New("landing").Funcs(template.FuncMap{"observe": func(v LandingData) string { d = v; return "" }}).Parse("{{observe .}}"))}
	path := "/?page=" + url.QueryEscape(value)
	w := request(identity.Require(Handler(cfg)), "GET", path, "user")
	if w.Code == 200 {
		assertBody(t, request(f.h, "GET", path, "user"), 200, "landing", d)
	}
	return d, w
}
func observedRun(t *testing.T, f fixture, id, value string) (RunData, *httptest.ResponseRecorder) {
	t.Helper()
	var d RunData
	cfg := f.cfg
	cfg.Pages = &Set{template.Must(template.New("run").Funcs(template.FuncMap{"observe": func(v RunData) string { d = v; return "" }}).Parse("{{observe .}}"))}
	path := "/runs/" + id + "?page=" + url.QueryEscape(value)
	w := request(identity.Require(Handler(cfg)), "GET", path, "user")
	if w.Code == 200 {
		assertBody(t, request(f.h, "GET", path, "user"), 200, "run", d)
	}
	return d, w
}
func runBefore(r store.Run) string {
	return paging.Token{Tool: "runs-before"}.Encode(r.Started.UTC().Format(time.RFC3339), r.ID)
}
func runAfter(r store.Run) string {
	return paging.Token{Tool: "runs"}.Encode(r.Started.UTC().Format(time.RFC3339), r.ID)
}

// R-FXZM-S1XC R-FZ7J-5TO1 R-G1NB-XD5F R-G6IX-GG47
// R-G7QT-U7UW R-G8YQ-7ZLL R-G2V8-B4W4
func TestLandingNavigation(t *testing.T) {
	for _, count := range []int{0, paging.DefaultLimit, paging.DefaultLimit + 1, paging.DefaultLimit + 2, 2 * paging.DefaultLimit, 3*paging.DefaultLimit + 8} {
		t.Run(strconv.Itoa(count), func(t *testing.T) {
			f := setup(t)
			ordered := []store.Run{}
			for i := 0; i < count; i++ {
				name := "alpha"
				if i%2 != 0 {
					name = "zeta"
				}
				ordered = append([]store.Run{f.finish(t, f.add(t, name, ""), nil)}, ordered...)
			}
			var windows [][]RunRow
			token := ""
			for offset := 0; ; offset += paging.DefaultLimit {
				d, w := observedLanding(t, f, token)
				if w.Code != 200 {
					t.Fatal(w.Code)
				}
				end := min(offset+paging.DefaultLimit, count)
				want := []RunRow{}
				for _, r := range ordered[offset:end] {
					want = append(want, expectedLanding(r))
				}
				prev, next := "", ""
				if token != "" && len(want) > 0 {
					prev = runBefore(ordered[offset])
				}
				if end < count {
					next = runAfter(ordered[end-1])
				}
				b := baseBanner()
				b.Trail = nil
				scrapersWant := []ScraperRow{}
				for _, name := range f.cfg.Scrapers.Names() {
					x := ScraperRow{Name: name}
					for _, r := range ordered {
						if r.Scraper == name {
							x.Current = expectedCurrent(r)
							break
						}
					}
					scrapersWant = append(scrapersWant, x)
				}
				expected := LandingData{b, scrapersWant, want, token, prev, next}
				if !reflect.DeepEqual(d, expected) {
					t.Fatalf("forward offset %d: got %+v want %+v", offset, d, expected)
				}
				windows = append(windows, want)
				if next == "" {
					break
				}
				token = next
			}
			if count > paging.DefaultLimit {
				for j := len(windows) - 1; j > 0; j-- {
					d, w := observedLanding(t, f, token)
					if w.Code != 200 || !reflect.DeepEqual(d.Runs, windows[j]) {
						t.Fatalf("backward window %d: %+v", j, d)
					}
					wantPrev := (paging.Token{Tool: "runs-before"}).Encode(windows[j][0].Started, windows[j][0].ID)
					wantNext := ""
					if j < len(windows)-1 {
						last := windows[j][len(windows[j])-1]
						wantNext = (paging.Token{Tool: "runs"}).Encode(last.Started, last.ID)
					}
					if d.Page != token || d.Prev != wantPrev || d.Next != wantNext {
						t.Fatal("backward navigation", j, d.Page, d.Prev, d.Next)
					}
					token = d.Prev
				}
				_, w := observedLanding(t, f, token)
				assertRedirect(t, w, "/")
				d, w := observedLanding(t, f, "last")
				want := []RunRow{}
				for _, r := range ordered[count-paging.DefaultLimit:] {
					want = append(want, expectedLanding(r))
				}
				if w.Code != 200 || !reflect.DeepEqual(d.Runs, want) || d.Page != "last" || d.Next != "" || d.Prev != runBefore(ordered[count-paging.DefaultLimit]) {
					t.Fatal("last window", d, w.Code)
				}
			} else {
				_, w := observedLanding(t, f, "last")
				assertRedirect(t, w, "/")
			}
			// Nearest-before with a key absent from the store, including an empty result.
			if count > paging.DefaultLimit+1 {
				anchor := ordered[count-1]
				absent := paging.Token{Tool: "runs-before"}.Encode(anchor.Started.Add(time.Second).UTC().Format(time.RFC3339), anchor.ID)
				d, w := observedLanding(t, f, absent)
				start := count - 1 - paging.DefaultLimit
				want := []RunRow{}
				for _, r := range ordered[start : count-1] {
					want = append(want, expectedLanding(r))
				}
				if w.Code != 200 || !reflect.DeepEqual(d.Runs, want) || d.Prev != runBefore(ordered[start]) || d.Next != runAfter(ordered[count-2]) {
					t.Fatal("absent before anchor", d, w.Code)
				}
			}
			_, w := observedLanding(t, f, paging.Token{Tool: "runs-before"}.Encode("9999-01-01T00:00:00Z", "run_ffffffffffffffff"))
			assertRedirect(t, w, "/")
			path := "/?page=last&scraper=missing&limit=1&page=bogus"
			get, head := request(f.h, "GET", path, "user"), request(f.h, "HEAD", path, "user")
			if get.Code != head.Code || !reflect.DeepEqual(get.Header(), head.Header()) || head.Body.Len() != 0 {
				t.Fatal("navigation HEAD")
			}
		})
	}
}

// R-GA6M-LRCA R-GBEI-ZJ2Z R-GCMF-DATO R-GHI0-WDSG
// R-GJXT-NX9U R-GL5Q-1P0J R-GDUB-R2KD
func TestRunNavigation(t *testing.T) {
	for _, count := range []int{0, paging.DefaultLimit, paging.DefaultLimit + 1, paging.DefaultLimit + 2, 2 * paging.DefaultLimit, 3*paging.DefaultLimit + 8} {
		t.Run(strconv.Itoa(count), func(t *testing.T) {
			f := setup(t)
			r := f.add(t, "alpha", "")
			rows := []source.Row{}
			ordered := []RowLine{}
			for i := 0; i < count; i++ {
				s := fmt.Sprintf("%03d", i)
				rows = append(rows, source.Row{SourceID: s, Name: "name-" + s, Address: source.Address{City: "city-" + s, State: "state", County: "county"}, Facts: []byte(`{}`), Raw: []byte(`{}`)})
				ordered = append(ordered, RowLine{s, "name-" + s, "city-" + s, "state", "county"})
			}
			r = f.finish(t, r, rows)
			var windows [][]RowLine
			token := ""
			for offset := 0; ; offset += paging.DefaultLimit {
				d, w := observedRun(t, f, r.ID, token)
				end := min(offset+paging.DefaultLimit, count)
				want := append([]RowLine{}, ordered[offset:end]...)
				prev, next := "", ""
				if token != "" && len(want) > 0 {
					prev = paging.Token{Tool: "rows-before"}.Encode(want[0].SourceID)
				}
				if end < count {
					next = paging.Token{Tool: "rows"}.Encode(want[len(want)-1].SourceID)
				}
				b := baseBanner()
				b.Trail = []page.Level{{Name: r.ID, URL: "/runs/" + r.ID}}
				expected := RunData{b, expectedRun(r, SecondLayout), want, token, prev, next}
				if w.Code != 200 || !reflect.DeepEqual(d, expected) {
					t.Fatalf("forward offset %d: got %+v want %+v", offset, d, expected)
				}
				windows = append(windows, want)
				if next == "" {
					break
				}
				token = next
			}
			if count > paging.DefaultLimit {
				for j := len(windows) - 1; j > 0; j-- {
					d, w := observedRun(t, f, r.ID, token)
					if w.Code != 200 || !reflect.DeepEqual(d.Rows, windows[j]) {
						t.Fatal("backward", j, d)
					}
					wantPrev := (paging.Token{Tool: "rows-before"}).Encode(windows[j][0].SourceID)
					wantNext := ""
					if j < len(windows)-1 {
						wantNext = (paging.Token{Tool: "rows"}).Encode(windows[j][len(windows[j])-1].SourceID)
					}
					if d.Page != token || d.Prev != wantPrev || d.Next != wantNext {
						t.Fatal("backward navigation", j, d.Page, d.Prev, d.Next)
					}
					token = d.Prev
				}
				_, w := observedRun(t, f, r.ID, token)
				assertRedirect(t, w, "/runs/"+r.ID)
				d, w := observedRun(t, f, r.ID, "last")
				want := ordered[count-paging.DefaultLimit:]
				if w.Code != 200 || !reflect.DeepEqual(d.Rows, want) || d.Page != "last" || d.Next != "" || d.Prev != (paging.Token{Tool: "rows-before"}).Encode(want[0].SourceID) {
					t.Fatal("last", d, w.Code)
				}
			} else {
				_, w := observedRun(t, f, r.ID, "last")
				assertRedirect(t, w, "/runs/"+r.ID)
			}
			if count > paging.DefaultLimit+1 {
				absent := paging.Token{Tool: "rows-before"}.Encode(ordered[count-2].SourceID + "x")
				d, w := observedRun(t, f, r.ID, absent)
				want := ordered[count-1-paging.DefaultLimit : count-1]
				if w.Code != 200 || !reflect.DeepEqual(d.Rows, want) || d.Prev != (paging.Token{Tool: "rows-before"}).Encode(want[0].SourceID) || d.Next != (paging.Token{Tool: "rows"}).Encode(want[len(want)-1].SourceID) {
					t.Fatal("absent before anchor", d, w.Code)
				}
			}
			_, w := observedRun(t, f, r.ID, paging.Token{Tool: "rows-before"}.Encode("!"))
			assertRedirect(t, w, "/runs/"+r.ID)
			path := "/runs/" + r.ID + "?page=last&name=missing&limit=1&page=bogus"
			get, head := request(f.h, "GET", path, "user"), request(f.h, "HEAD", path, "user")
			if get.Code != head.Code || !reflect.DeepEqual(get.Header(), head.Header()) || head.Body.Len() != 0 {
				t.Fatal("navigation HEAD")
			}
		})
	}
}
