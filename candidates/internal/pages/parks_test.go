package pages

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"math"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/paging"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/candidates/internal/parks"
	"github.com/ikigenba/ikigenba/candidates/internal/places"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/tools"
)

func parksFixture(t *testing.T, n int) (Config, []store.Park) {
	t.Helper()
	cfg, _ := pageFixture(t)
	p, err := cfg.Store.AddPass(t.Context(), store.Pass{Kind: store.KindParks, Origin: store.OriginStart, Trigger: "parcel-run"})
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.Store.ReplaceZipDrive(t.Context(), map[string]int64{"55001": 42}); err != nil {
		t.Fatal(err)
	}
	lat, lon := 45.47412, -92.97826
	plan := parks.Plan{}
	for i := range n {
		value := int64(i%3)*1000000 - 1850000
		county := "Chisago"
		if i%3 == 1 {
			county = "Anoka"
		}
		qs := []parks.Parcel{}
		for j := range i%3 + 2 {
			qs = append(qs, parks.Parcel{PIN: fmt.Sprintf("pin-%d-%d", i, j), Owners: []string{"owner-one", "owner-two"}, Street: "fixture street", City: "fixture city", Zip: "55001", Lat: &lat, Lon: &lon, BuildingValue: &value, Rule: []string{parks.RuleLabel, parks.RuleValue, parks.RuleCandidate}[j%3]})
		}
		zip := "55001"
		if i%3 == 0 {
			zip = "99999"
		}
		plan.News = append(plan.News, parks.New{Cluster: parks.Cluster{County: county, Town: fmt.Sprintf("Town %02d", i), Zip: zip, Lat: lat, Lon: lon, Radius: 548.5, BuildingValue: value, Parcels: qs}, Number: 1})
	}
	if _, err = cfg.Store.ApplyParks(t.Context(), p.ID, "parcel-run", []string{"Chisago", "Anoka", "Carlton"}, plan, store.Counts{}); err != nil {
		t.Fatal(err)
	}
	ps, err := cfg.Store.ListParks(t.Context(), store.ParkFilter{}, parks.SortMinutes, "", "", n+1)
	if err != nil {
		t.Fatal(err)
	}
	return cfg, ps
}
func captureParks(t *testing.T, cfg Config, path string) ParksData {
	t.Helper()
	set, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	var got ParksData
	calls := 0
	x, err := set.templates.Funcs(template.FuncMap{"parkCapture": func(d ParksData) string { got = d; calls++; return "" }}).Parse(`{{define "parks"}}{{parkCapture .}}{{end}}`)
	if err != nil {
		t.Fatal(err)
	}
	c := cfg
	c.Pages = &Set{templates: x}
	w := requestPage(c, "GET", path, "reader")
	if w.Code != 200 || calls != 1 {
		t.Fatal(path, w.Code, calls)
	}
	comparePage(t, cfg, path, "parks", 200, got)
	return got
}

// R-6IUW-1P29 R-6K2S-FGSY R-6LAO-T8JN R-QHGB-ZR3I R-QIO8-DIU7
func TestParkDataShapes(t *testing.T) {
	if ParksLevel != (page.Level{Name: "parks", URL: "/parks"}) {
		t.Fatal(ParksLevel)
	}
	_ = struct {
		Banner                                        page.Banner
		Summary                                       ParksSummary
		View                                          ParksView
		Parks                                         []ParkRow
		Page, FirstHref, PrevHref, NextHref, LastHref string
		Rules                                         RulesView
	}(ParksData{})
	_ = struct {
		Count, Gone  int64
		Run, RunPage string
	}(ParksSummary{})
	_ = struct {
		ID, Name, County, State                    string
		Parcels                                    int64
		BuildingValue, Minutes, Walked, WalkedText string
		GrownFrom                                  int64
	}(ParkRow{})
	_ = struct {
		Counties            []CountyChoice
		Walked, Gone, Sorts []ViewChoice
	}(ParksView{})
	_ = struct {
		County, Href string
		Current      bool
	}(CountyChoice{})
	_ = struct {
		Value, Href string
		Current     bool
	}(ViewChoice{})
	_ = struct {
		Distance       string
		MinParcels     int64
		ValueThreshold string
		WalkCeiling    int64
		Counties       []CountyLabels
	}(RulesView{})
	_ = struct {
		County string
		Labels []string
	}(CountyLabels{})
	_ = struct {
		Banner          page.Banner
		Park            ParkView
		Walks           []WalkRow
		Places          []PlaceRow
		FoundCandidates []FoundCandidateRow
		Parcels         []ParcelRow
	}(ParkData{})
	_ = struct {
		ID, Name, County, Town, State, Lat, Lon, MapURL, Radius  string
		ParcelCount                                              int64
		BuildingValue, Minutes                                   string
		GrownFrom                                                int64
		Walked, WalkedText, Walk                                 string
		Found, NewCandidates                                     int64
		CeilingReached                                           bool
		Run, RunPage, Created, CreatedText, Updated, UpdatedText string
	}(ParkView{})
	_ = struct {
		PIN                                    string
		Owners                                 []string
		Street, City, Zip, BuildingValue, Rule string
	}(ParcelRow{})
}

// R-6OYD-YJRQ R-6Q6A-CBIF R-QL41-52BL
func TestParkFormatting(t *testing.T) {
	for _, tc := range []struct {
		n    int64
		want string
	}{{21400000, "$21,400,000"}, {1000000, "$1,000,000"}, {0, "$0"}, {-1850000, "-$1,850,000"}, {math.MinInt64, "-$9,223,372,036,854,775,808"}} {
		if got := valueText(tc.n); got != tc.want {
			t.Fatal(got, tc.want)
		}
	}
	minutes := int64(42)
	at := fixtureTime()
	p := store.Park{ID: "chosen-id", Name: "chosen-name", County: "chosen-county", Town: "chosen-town", State: parks.StateGone, Lat: 45.47412, Lon: -92.97826, Radius: 548.5, Parcels: 3, BuildingValue: 21400000, Minutes: &minutes, GrownFrom: 2, Run: "chosen-run", Created: at, Updated: at, Walked: at, WalkRun: "chosen-walk", Found: 4}
	want := ParkRow{"chosen-id", "chosen-name", "chosen-county", parks.StateGone, 3, "$21,400,000", "42", "2026-10-05T09:00:07Z", "2026-10-05", 2}
	if got := parkRow(p); !reflect.DeepEqual(got, want) {
		t.Fatal(got, want)
	}
	v := parkView(p, "")
	wantView := ParkView{ID: "chosen-id", Name: "chosen-name", County: "chosen-county", Town: "chosen-town", State: parks.StateGone, Lat: "45.4741", Lon: "-92.9783", MapURL: parks.MapURL(45.47412, -92.97826), Radius: "549", ParcelCount: 3, BuildingValue: "$21,400,000", Minutes: "42", GrownFrom: 2, Walked: "2026-10-05T09:00:07Z", WalkedText: "2026-10-05", Walk: "chosen-walk", Found: 4, Run: "chosen-run", Created: "2026-10-05T09:00:07Z", CreatedText: "2026-10-05 09:00:07 UTC", Updated: "2026-10-05T09:00:07Z", UpdatedText: "2026-10-05 09:00:07 UTC"}
	if !reflect.DeepEqual(v, wantView) {
		t.Fatal(v, wantView)
	}
	servicesPath := filepath.Join(t.TempDir(), "services.json")
	if err := os.WriteFile(servicesPath, []byte(`{"services":[{"name":"scrapes","url":"https://fixture-scrapes.test","description":"","socket":"","enabled":true,"mcp":false}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	linked := parkView(p, servicesPath)
	wantView.RunPage = "https://fixture-scrapes.test/runs/chosen-run"
	if !reflect.DeepEqual(linked, wantView) {
		t.Fatal(linked, wantView)
	}
	p.Minutes = nil
	p.Walked = time.Time{}
	p.WalkRun = ""
	p.Found = 0
	p.GrownFrom = 0
	v = parkView(p, "")
	r := parkRow(p)
	if v.Minutes != "" || v.Walked != "" || v.WalkedText != "" || r.Minutes != "" || r.Walked != "" || r.GrownFrom != 0 {
		t.Fatal(v, r)
	}
	value := int64(1850000)
	q := store.ParkParcel{PIN: "chosen-pin", Owners: []string{"a", "b"}, Street: "street", City: "city", Zip: "zip", BuildingValue: &value, Rule: parks.RuleValue}
	if got := parcelRow(q); !reflect.DeepEqual(got, ParcelRow{"chosen-pin", []string{"a", "b"}, "street", "city", "zip", "$1,850,000", parks.RuleValue}) {
		t.Fatal(got)
	}
	q.BuildingValue = nil
	q.Owners = nil
	if got := parcelRow(q); got.BuildingValue != "" || got.Owners == nil || len(got.Owners) != 0 {
		t.Fatal(got)
	}
}

// R-6TTZ-HMQI R-6W9S-967W R-6XHO-MXYL R-GCQ8-ELBO R-GF61-64T2 R-6SM3-3UZT R-GGDX-JWJR R-GITQ-BG15 R-GK1M-P7RU R-73L6-JSO2
func TestParksWindows(t *testing.T) {
	for _, tc := range []struct{ c, w, g, s, p, want string }{{"", "", "", "", "", "/parks"}, {"", "all", "active", "minutes", "", "/parks"}, {"St. Louis", "unwalked", "", "value", "last", "/parks?county=St.+Louis&walked=unwalked&sort=value&page=last"}, {"", "", "all", "parcels", "", "/parks?gone=all&sort=parcels"}} {
		if got := viewHref(tc.c, tc.w, tc.g, tc.s, tc.p); got != tc.want {
			t.Fatal(got, tc.want)
		}
	}
	fresh, _ := pageFixture(t)
	empty := captureParks(t, fresh, "/parks")
	if empty.Summary != (ParksSummary{}) || len(empty.Parks) != 0 || empty.Parks == nil || empty.Rules.Counties == nil || len(empty.Rules.Counties) != 0 || empty.Rules.Distance != "600" || empty.Rules.MinParcels != 3 || empty.Rules.ValueThreshold != "$1,000,000" {
		t.Fatal(empty)
	}
	cfg, _ := parksFixture(t, 3*paging.DefaultLimit+2)
	writer := telemetry.New(telemetry.Config{Service: ServiceName, Sink: &telemetry.Capture{}, Now: fixtureTime, Rand: &sequence{}})
	t.Cleanup(func() { writer.Shutdown(t.Context(), "fixture-stop") })
	srv := mcp.NewServer(mcp.ServerConfig{Name: "candidates", Telemetry: writer})
	tools.Register(srv, tools.Config{Store: cfg.Store})
	host := httptest.NewServer(identity.Require(srv))
	t.Cleanup(host.Close)
	client := mcp.NewClient(mcp.ClientConfig{Endpoint: host.URL, HTTPClient: host.Client()})
	for _, sort := range []string{parks.SortMinutes, parks.SortValue, parks.SortParcels} {
		for _, county := range []string{"", "Chisago"} {
			f := store.ParkFilter{County: county}
			want, err := cfg.Store.ListParks(t.Context(), f, sort, "", "", 1000)
			if err != nil {
				t.Fatal(err)
			}
			windows := []ParksData{}
			p := ""
			offset := 0
			for {
				path := viewHref(county, "", "", sort, p)
				d := captureParks(t, cfg, path)
				windows = append(windows, d)
				end := min(offset+paging.DefaultLimit, len(want))
				rows := []ParkRow{}
				for _, x := range want[offset:end] {
					rows = append(rows, parkRow(x))
				}
				if !reflect.DeepEqual(d.Parks, rows) || d.Page != p || d.Summary.Count != 3*paging.DefaultLimit+2 || d.Summary.Gone != 0 || d.Summary.Run != "parcel-run" || d.Summary.RunPage != "" || !reflect.DeepEqual(d.Banner.Trail, []page.Level{ParksLevel}) {
					t.Fatal(path, d)
				}
				if len(d.Rules.Counties) != 3 || d.Rules.Counties[0].County != "Anoka" || d.Rules.Counties[1].County != "Carlton" || d.Rules.Counties[1].Labels == nil {
					t.Fatal(d.Rules)
				}
				if len(d.View.Counties) != 3 || !d.View.Walked[0].Current || !d.View.Gone[0].Current {
					t.Fatal(d.View)
				}
				for _, x := range d.View.Counties {
					if x.Current != (x.County == county) || x.Href != viewHref(x.County, "", "", sort, "") {
						t.Fatal(x)
					}
				}
				for _, x := range d.View.Sorts {
					if x.Current != (x.Value == sort) || strings.Contains(x.Href, "page=") {
						t.Fatal(x)
					}
				}
				a := tools.ParksArgs{County: county, Sort: sort}
				if p != "" {
					a.Page = &p
				}
				raw, err := json.Marshal(a)
				if err != nil {
					t.Fatal(err)
				}
				result, err := client.CallTool(t.Context(), identity.Caller{UserID: "reader"}, "parks", raw)
				if err != nil || result.IsError() {
					t.Fatal(result, err)
				}
				var wire struct {
					Structured tools.ParkList `json:"structuredContent"`
				}
				b, _ := json.Marshal(result)
				if err = json.Unmarshal(b, &wire); err != nil {
					t.Fatal(err)
				}
				ids := []string{}
				for _, x := range wire.Structured.Parks {
					ids = append(ids, x.ID)
				}
				gotIDs := []string{}
				for _, x := range d.Parks {
					gotIDs = append(gotIDs, x.ID)
				}
				if !reflect.DeepEqual(ids, gotIDs) {
					t.Fatal(ids, gotIDs)
				}
				if wire.Structured.Next == nil {
					if d.NextHref != "" || d.LastHref != "" {
						t.Fatal(d)
					}
					break
				}
				if d.NextHref != viewHref(county, "", "", sort, *wire.Structured.Next) || d.LastHref != viewHref(county, "", "", sort, "last") {
					t.Fatal(d)
				}
				p = *wire.Structured.Next
				offset = end
			}
			for i := len(windows) - 1; i > 0; i-- {
				prev := windows[i].PrevHref
				if i == 1 {
					w := requestPage(cfg, "GET", prev, "reader")
					if w.Code != 303 || w.Header().Get("Location") != viewHref(county, "", "", sort, "") || w.Body.Len() != 0 {
						t.Fatal(w)
					}
				} else {
					d := captureParks(t, cfg, prev)
					if !reflect.DeepEqual(d.Parks, windows[i-1].Parks) {
						t.Fatal(d.Parks, windows[i-1].Parks)
					}
				}
			}
			last := captureParks(t, cfg, viewHref(county, "", "", sort, "last"))
			rows := []ParkRow{}
			for _, x := range want[len(want)-paging.DefaultLimit:] {
				rows = append(rows, parkRow(x))
			}
			if !reflect.DeepEqual(last.Parks, rows) || last.Page != "last" || last.NextHref != "" || last.LastHref != "" || last.PrevHref == "" || last.FirstHref != viewHref(county, "", "", sort, "") {
				t.Fatal(last)
			}
		}
	}
	for _, n := range []int{0, paging.DefaultLimit} {
		c, _ := parksFixture(t, n)
		for _, method := range []string{"GET", "HEAD"} {
			w := requestPage(c, method, "/parks?walked=all&page=last", "reader")
			if w.Code != 303 || w.Header().Get("Location") != "/parks" || len(w.Header().Values("Location")) != 1 || w.Body.Len() != 0 {
				t.Fatal(w)
			}
		}
	}
}

// R-78GS-2VMU R-QMBX-IU2A R-715D-S96O R-72DA-60XD R-6BJH-R2M3 R-6CRE-4UCS R-6GF3-A5KV R-6DZA-IM3H R-66NW-7ZNB
func TestParkRoutes(t *testing.T) {
	cfg, ps := parksFixture(t, 2)
	p := ps[0]
	qs, err := cfg.Store.ParkParcels(t.Context(), p.ID)
	if err != nil {
		t.Fatal(err)
	}
	b := bannerFor(cfg)
	b.Trail = []page.Level{ParksLevel, {Name: p.ID, URL: "/parks/" + p.ID}}
	d := ParkData{Banner: b, Park: parkView(p, ""), Parcels: []ParcelRow{}}
	for _, q := range qs {
		d.Parcels = append(d.Parcels, parcelRow(q))
	}
	comparePage(t, cfg, "/parks/"+p.ID+"?ignored=anything", "park", 200, d)
	blank := requestPage(cfg, "GET", "/no-such-route", "reader")
	bad := []string{"/parks/", "/parks/" + strings.ToUpper(p.ID), "/parks/" + p.ID[:len(p.ID)-1], "/parks/" + p.ID + "a", "/parks/" + p.ID + "/", "/parks/%70" + p.ID[1:], "/parks/" + store.CandidateIDPrefix + strings.Repeat("a", 16), "/parks/" + store.ParkIDPrefix + strings.Repeat("0", 16), "/parks?page=bogus", "/parks?page=LAST", "/parks?page=last/", "/parks?walked=Walked", "/parks?gone=none", "/parks?sort=name", "/parks?sort=%20minutes", "/parks?county=Nowhere", "/parks?county=chisago", "/parks?county=Chisago%20", "/parks?county=Nowhere&page=last"}
	for _, token := range []string{(paging.Token{Tool: "parks"}).Encode(parks.SortValue, "5", p.ID), (paging.Token{Tool: "parks-before"}).Encode(parks.SortValue, "5", p.ID), (paging.Token{Tool: "parks"}).Encode(parks.SortMinutes, "12", store.CandidateIDPrefix+strings.Repeat("a", 16)), (paging.Token{Tool: "parks"}).Encode(parks.SortMinutes, "12x", p.ID)} {
		bad = append(bad, "/parks?page="+url.QueryEscape(token))
	}
	for _, path := range bad {
		w := requestPage(cfg, "GET", path, "reader")
		if w.Code != blank.Code || !reflect.DeepEqual(w.Header(), blank.Header()) || w.Body.String() != blank.Body.String() {
			t.Fatal(path, w.Code)
		}
	}
	// The failure handle comes from the same configuration used to make requests.
	failed, handle := pageFixture(t)
	handle.SetFailing(true)
	defer handle.SetFailing(false)
	for _, path := range []string{"/parks", "/parks?county=Nowhere", "/parks/" + p.ID} {
		for _, method := range []string{"GET", "HEAD"} {
			w := requestPage(failed, method, path, "reader")
			if w.Code != 503 || w.Header().Get("Location") != "" {
				t.Fatal(path, w.Code)
			}
		}
	}
	for _, path := range bad {
		if strings.Contains(path, "county=") || strings.HasPrefix(path, "/parks/"+store.ParkIDPrefix) {
			continue
		}
		w := requestPage(failed, "GET", path, "reader")
		if w.Code != 404 {
			t.Fatal(path, w.Code)
		}
	}
	for _, path := range []string{"/parks", "/parks/" + p.ID} {
		for _, method := range []string{"POST", "PUT", "OPTIONS"} {
			w := requestPage(failed, method, path, "reader")
			if w.Code != 405 || w.Header().Get("Allow") != "GET, HEAD" || w.Body.Len() != 0 {
				t.Fatal(path, w)
			}
		}
	}
}

// R-QF0J-87M4 R-QG8F-LZCT R-QPZM-O5AD R-QR7J-1X12 R-5DEB-1HUN
func TestParkLandingAndPass(t *testing.T) {
	cfg, ps := parksFixture(t, 2)
	passes, err := cfg.Store.ListPasses(t.Context(), "", "", 100)
	if err != nil || len(passes) != 1 {
		t.Fatal(passes, err)
	}
	data := LandingData{Banner: bannerFor(cfg), ProspectLists: []ProspectListRow{}, Passes: []PassRow{passRow(passes[0])}, Parks: LandingParks{Count: 2}}
	comparePage(t, cfg, "/", "landing", 200, data)
	for _, origin := range []string{store.OriginRules, store.OriginRun, store.OriginStart} {
		for _, email := range []string{"", "owner@fixture.test"} {
			p := store.Pass{Kind: store.KindParks, Origin: origin, Trigger: "run-fixture", UserID: "fixture-user", UserEmail: email, ParcelsRead: 832000, ByLabel: 193, ByValue: 4, ByCandidate: 2, ParksCreated: 15, ParksUpdated: 3, ParksGone: 1, ParksGrown: 2}
			v := passView(p, "")
			if v.ParcelsRead != 832000 || v.ByLabel != 193 || v.ByValue != 4 || v.ByCandidate != 2 || v.ParksCreated != 15 || v.ParksUpdated != 3 || v.ParksGone != 1 || v.ParksGrown != 2 || v.Cost != "$0.000000" || v.RunPage != "" {
				t.Fatal(v)
			}
			expected := ""
			if origin == store.OriginRules {
				expected = email
				if expected == "" {
					expected = p.UserID
				}
			}
			if v.StartedBy != expected {
				t.Fatal(v)
			}
		}
	}
	p, err := cfg.Store.AddPass(t.Context(), store.Pass{Kind: store.KindParks, Origin: store.OriginRules, Trigger: store.TriggerParkRules, UserID: "fixture-user"})
	if err != nil {
		t.Fatal(err)
	}
	p, err = cfg.Store.FinishPass(t.Context(), p.ID, store.Counts{ParcelsRead: 900, ByLabel: 700, ByValue: 100, ByCandidate: 20, ParksCreated: 4, ParksUpdated: 3, ParksGone: 2, ParksGrown: 1})
	if err != nil {
		t.Fatal(err)
	}
	b := bannerFor(cfg)
	b.Trail = []page.Level{{Name: p.ID, URL: "/passes/" + p.ID}}
	comparePage(t, cfg, "/passes/"+p.ID, "pass", 200, PassData{Banner: b, Pass: passView(p, "")})
	gonePass, err := cfg.Store.AddPass(t.Context(), store.Pass{Kind: store.KindParks, Origin: store.OriginStart, Trigger: "later-run"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = cfg.Store.ApplyParks(t.Context(), gonePass.ID, "later-run", []string{"Chisago"}, parks.Plan{Gone: []string{ps[0].ID}}, store.Counts{}); err != nil {
		t.Fatal(err)
	}
	all, err := cfg.Store.ListPasses(t.Context(), "", "", 100)
	if err != nil {
		t.Fatal(err)
	}
	data.Passes = []PassRow{}
	for _, x := range all {
		data.Passes = append(data.Passes, passRow(x))
	}
	data.Parks.Count = 1
	comparePage(t, cfg, "/", "landing", 200, data)
	gone := captureParks(t, cfg, "/parks?gone=gone")
	if len(gone.Parks) != 1 || gone.Parks[0].ID != ps[0].ID || gone.Parks[0].State != parks.StateGone || gone.Summary.Count != 1 || gone.Summary.Gone != 1 {
		t.Fatal(gone)
	}
}

// R-J2CQ-UUT7 R-INPY-9LWV
func TestParksTemplateStates(t *testing.T) {
	t.Chdir(t.TempDir())
	set, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	render := func(name string, v any) string {
		t.Helper()
		var b bytes.Buffer
		if err := set.templates.ExecuteTemplate(&b, name, v); err != nil {
			t.Fatal(name, err)
		}
		return b.String()
	}
	for _, n := range []int64{0, 17} {
		render("landing", LandingData{Parks: LandingParks{n}})
	}
	for _, status := range []string{store.StatusRunning, store.StatusDone, store.StatusFailed} {
		for _, reason := range []string{"", store.ReasonScrapes, store.ReasonStore, store.ReasonPrompts, store.ReasonInterrupted} {
			for _, text := range []string{"", "unique-value"} {
				for _, origin := range []string{store.OriginRun, store.OriginRules, store.OriginStart} {
					for _, n := range []int64{0, 91} {
						render("pass", PassData{Pass: PassView{Kind: store.KindParks, Origin: origin, Trigger: "run-fixture", Status: status, Reason: reason, StartedBy: text, RunPage: text, Finished: text, FinishedText: text, ParcelsRead: n, ByLabel: n, ByValue: n, ByCandidate: n, ParksCreated: n, ParksUpdated: n, ParksGone: n, ParksGrown: n}})
					}
				}
			}
		}
	}
	for _, state := range []string{parks.StateActive, parks.StateGone} {
		for _, text := range []string{"", "optional-value"} {
			for _, n := range []int64{0, 3} {
				d := ParksData{Summary: ParksSummary{Count: n, Gone: n, Run: "summary-run", RunPage: "https://unique-run.test/"}, View: ParksView{Counties: []CountyChoice{{"", "/unique-all", true}, {"unique-choice-county", "/unique-county", false}}, Walked: []ViewChoice{{parks.WalkedAll, "/unique-walk", true}}, Gone: []ViewChoice{{parks.GoneAll, "/unique-gone", true}}, Sorts: []ViewChoice{{parks.SortMinutes, "/unique-sort", true}}}, Parks: []ParkRow{{"unique-park-id", "unique-park-name", "unique-park-county", state, 3, "unique-building-value", text, text, text, n}}, Page: text, FirstHref: "/unique-first", PrevHref: "/unique-prev", NextHref: "/unique-next", LastHref: "/unique-last", Rules: RulesView{"unique-distance", 3, "unique-threshold", 0, []CountyLabels{{"unique-rules-county", []string{"unique-label-one", "unique-label-two"}}, {"no-labels", nil}}}}
				body := render("parks", d)
				for _, v := range []string{d.Summary.Run, d.Summary.RunPage, d.Parks[0].ID, d.Parks[0].Name, d.Parks[0].County, d.Parks[0].BuildingValue, d.FirstHref, d.PrevHref, d.NextHref, d.LastHref, d.Rules.Distance, d.Rules.ValueThreshold, "unique-rules-county", "unique-label-one", "unique-label-two", "unique-choice-county", "/unique-all", "/unique-county", "/unique-walk", "/unique-gone", "/unique-sort"} {
					if !strings.Contains(body, v) {
						t.Fatal(v)
					}
				}
				d.Parks = nil
				d.Rules.Counties = nil
				d.FirstHref = ""
				d.PrevHref = ""
				d.NextHref = ""
				d.LastHref = ""
				render("parks", d)
				pv := ParkView{ID: "detail-id", Name: "detail-name", County: "detail-county", Town: "detail-town", State: state, Lat: "detail-lat", Lon: "detail-lon", MapURL: "https://detail-map.test/", Radius: "detail-radius", BuildingValue: "detail-value", Minutes: text, GrownFrom: n, Walked: text, WalkedText: text, Walk: text, Found: n, Run: "detail-run", RunPage: text, CreatedText: "detail-created", UpdatedText: "detail-updated"}
				for _, owners := range [][]string{nil, {"unique-owner-one"}, {"unique-owner-one", "unique-owner-two"}} {
					for _, rule := range []string{parks.RuleLabel, parks.RuleValue, parks.RuleCandidate} {
						pd := ParkData{Park: pv, Parcels: []ParcelRow{{"detail-pin", owners, "detail-street", "detail-city", "detail-zip", "detail-parcel-value", rule}}}
						body := render("park", pd)
						for _, v := range []string{pv.ID, pv.Name, pv.County, pv.Town, pv.Lat, pv.Lon, pv.MapURL, pv.Radius, pv.BuildingValue, pv.Run, pv.CreatedText, pv.UpdatedText, "detail-pin", "detail-street", "detail-city", "detail-zip", "detail-parcel-value"} {
							if !strings.Contains(body, v) {
								t.Fatal(v)
							}
						}
						for _, v := range owners {
							if !strings.Contains(body, v) {
								t.Fatal(v)
							}
						}
						pd.Parcels = nil
						render("park", pd)
					}
				}
			}
		}
	}
}

// R-J2CQ-UUT7
func TestParkTemplateDistinctOptionalValues(t *testing.T) {
	set, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	render := func(name string, v any) string {
		t.Helper()
		var b bytes.Buffer
		if err := set.templates.ExecuteTemplate(&b, name, v); err != nil {
			t.Fatal(err)
		}
		return b.String()
	}
	includes := func(body string, values ...string) {
		t.Helper()
		for _, v := range values {
			if v != "" && !strings.Contains(body, v) {
				t.Fatalf("missing supplied value %q", v)
			}
		}
	}
	for _, current := range []int{0, 1, 2} {
		for _, summary := range []ParksSummary{{}, {Run: "summary-only-run"}, {Run: "summary-linked-run", RunPage: "https://summary-run.test/"}} {
			d := ParksData{Summary: summary, View: ParksView{Counties: []CountyChoice{{"", "/county-all", current == 0}, {"County A", "/county-a", current == 1}, {"County B", "/county-b", current == 2}}}, Rules: RulesView{Counties: []CountyLabels{{"County empty", nil}, {"County single", []string{"single-label"}}, {"County double", []string{"double-label-one", "double-label-two"}}}}}
			for i, x := range []string{parks.WalkedAll, parks.WalkedUnwalked, parks.WalkedWalked} {
				d.View.Walked = append(d.View.Walked, ViewChoice{x, fmt.Sprintf("/walked-choice-%d", i), i == current})
			}
			for i, x := range []string{parks.GoneActive, parks.GoneGone, parks.GoneAll} {
				d.View.Gone = append(d.View.Gone, ViewChoice{x, fmt.Sprintf("/gone-choice-%d", i), i == current})
			}
			for i, x := range []string{parks.SortMinutes, parks.SortValue, parks.SortParcels} {
				d.View.Sorts = append(d.View.Sorts, ViewChoice{x, fmt.Sprintf("/sort-choice-%d", i), i == current})
			}
			for _, populated := range []bool{false, true} {
				if populated {
					d.Parks = []ParkRow{{ID: "listing-unique-id", Name: "listing-unique-name", County: "listing-unique-county", State: parks.StateGone, BuildingValue: "listing-unique-value", Minutes: "478", Walked: "2001-01-02T03:04:05Z", WalkedText: "listing-unique-walked", GrownFrom: 3}}
				} else {
					d.Parks = nil
				}
				for _, p := range []string{"", "last", "another-page"} {
					d.Page = p
					body := render("parks", d)
					includes(body, summary.Run, summary.RunPage, "County A", "County B", "County empty", "County single", "County double", "single-label", "double-label-one", "double-label-two")
					for _, x := range d.View.Counties {
						includes(body, x.Href)
					}
					for _, xs := range [][]ViewChoice{d.View.Walked, d.View.Gone, d.View.Sorts} {
						for _, x := range xs {
							includes(body, x.Href)
						}
					}
					if populated {
						includes(body, "listing-unique-id", "listing-unique-name", "listing-unique-county", "listing-unique-value", "478", "listing-unique-walked")
					}
				}
			}
		}
	}
	for _, walked := range []bool{false, true} {
		for _, optional := range []bool{false, true} {
			for _, state := range []string{parks.StateActive, parks.StateGone} {
				v := ParkView{ID: "unique-detail-id", Name: "unique-detail-name", County: "unique-detail-county", Town: "unique-detail-town", State: state, Lat: "unique-lat", Lon: "unique-lon", MapURL: "https://unique-map.test/", Radius: "unique-radius", BuildingValue: "unique-value", Run: "unique-derivation", CreatedText: "unique-created", UpdatedText: "unique-updated"}
				if optional {
					v.Minutes = "367"
					v.RunPage = "https://unique-derivation.test/"
				}
				if walked {
					v.Walked = "2001-01-02T03:04:05Z"
					v.WalkedText = "unique-walked-date"
					v.Walk = "unique-walking-run"
				}
				for _, found := range []int64{0, 5} {
					v.Found = found
					if !walked {
						v.Found = 0
					}
					for _, q := range []ParcelRow{{}, {PIN: "unique-parcel-pin", Owners: []string{"unique-parcel-owner"}, Street: "unique-parcel-street", City: "unique-parcel-city", Zip: "unique-parcel-zip", BuildingValue: "unique-parcel-value", Rule: parks.RuleLabel}} {
						body := render("park", ParkData{Park: v, Parcels: []ParcelRow{q}})
						includes(body, v.ID, v.Name, v.County, v.Town, v.Lat, v.Lon, v.MapURL, v.Radius, v.BuildingValue, v.Run, v.CreatedText, v.UpdatedText, v.Minutes, v.RunPage, v.WalkedText, v.Walk, q.PIN, q.Street, q.City, q.Zip, q.BuildingValue)
						for _, x := range q.Owners {
							includes(body, x)
						}
					}
				}
			}
		}
	}
	for _, state := range []string{parks.StateActive, parks.StateGone} {
		for _, found := range []int64{0, 193847} {
			for _, newCandidates := range []int64{0, 284759} {
				for _, ceiling := range []bool{false, true} {
					v := ParkView{State: state, Walked: "chosen-walked-stamp", WalkedText: "unique-rendered-walk-date", Walk: "unique-rendered-walk-pass", Found: found, NewCandidates: newCandidates, CeilingReached: ceiling}
					body := render("park", ParkData{Park: v})
					includes(body, v.WalkedText, v.Walk)
				}
			}
		}
	}
}

// R-6HMZ-NXBK
func TestParksDoNotRecordProspectListOpens(t *testing.T) {
	cfg, ps := parksFixture(t, 2)
	list, err := cfg.Store.CreateProspectList(t.Context(), store.ProspectList{Name: "preserved-list", Size: 2, CreatedUserID: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	before, err := cfg.Store.ReadProspectList(t.Context(), list.ID)
	if err != nil {
		t.Fatal(err)
	}
	opens, err := cfg.Store.Opens(t.Context(), list.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/parks", "/parks/" + ps[0].ID, "/parks?page=bad", "/parks?county=Nowhere"} {
		for _, method := range []string{"GET", "HEAD", "POST"} {
			requestPage(cfg, method, path, "reader")
			after, err := cfg.Store.ReadProspectList(t.Context(), list.ID)
			if err != nil {
				t.Fatal(err)
			}
			later, err := cfg.Store.Opens(t.Context(), list.ID)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, after) || !reflect.DeepEqual(opens, later) {
				t.Fatal(path, method, "recorded open")
			}
		}
	}
}

// R-715D-S96O R-6GF3-A5KV R-6W9S-967W
func TestInvalidParkViewsNeverReadStore(t *testing.T) {
	cfg, ps := parksFixture(t, 1)
	cfg.Store = nil
	token := (paging.Token{Tool: "parks-before"}).Encode(parks.SortValue, "5", ps[0].ID)
	badKey := (paging.Token{Tool: "parks"}).Encode(parks.SortValue, "abc", ps[0].ID)
	for _, path := range []string{"/parks?sort=value&page=" + url.QueryEscape(badKey), "/parks?page=bogus", "/parks?page=LAST", "/parks?page=last/", "/parks?walked=Walked", "/parks?gone=none", "/parks?sort=name", "/parks?sort=%20minutes", "/parks?page=" + url.QueryEscape(token)} {
		for _, method := range []string{"GET", "HEAD"} {
			w := requestPage(cfg, method, path, "reader")
			if w.Code != 404 {
				t.Fatal(path, method, w.Code)
			}
		}
	}
	for _, path := range []string{"/parks", "/parks/" + ps[0].ID} {
		w := requestPage(cfg, "POST", path, "reader")
		if w.Code != 405 || w.Header().Get("Allow") != "GET, HEAD" {
			t.Fatal(path, w.Code)
		}
	}
}

// R-J2CQ-UUT7
func TestExactParksPassAndNavigationTemplateStates(t *testing.T) {
	set, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	render := func(name string, v any) {
		t.Helper()
		if err := set.templates.ExecuteTemplate(&bytes.Buffer{}, name, v); err != nil {
			t.Fatal(err)
		}
	}
	for _, by := range []string{"owner@fixture.test", "fixture-user"} {
		for _, status := range []string{store.StatusRunning, store.StatusDone, store.StatusFailed} {
			for _, reason := range []string{store.ReasonScrapes, store.ReasonStore, store.ReasonInterrupted, store.ReasonPrompts} {
				for _, finished := range []string{"", "2001-01-02T03:04:05Z"} {
					for _, p := range []PassView{{Kind: store.KindParks, Origin: store.OriginRules, Trigger: store.TriggerParkRules, StartedBy: by}, {Kind: store.KindParks, Origin: store.OriginStart, Trigger: "fixture-start-run"}, {Kind: store.KindParks, Origin: store.OriginRun, Trigger: "fixture-delivery-run"}, {Kind: store.KindParks, Origin: store.OriginRun, Trigger: "fixture-delivery-run", RunPage: "https://fixture-delivery.test/"}} {
						p.Status = status
						p.Reason = reason
						p.Finished = finished
						render("pass", PassData{Pass: p})
					}
				}
			}
		}
	}
	for _, first := range []string{"", "/unique-first"} {
		for _, prev := range []string{"", "/unique-prev"} {
			render("parks", ParksData{View: ParksView{Counties: []CountyChoice{{"", "/parks", true}}}, Page: "later-page", FirstHref: first, PrevHref: prev})
		}
	}
}

// R-6TTZ-HMQI R-GCQ8-ELBO R-GF61-64T2 R-GK1M-P7RU R-QMBX-IU2A
func TestParksExplicitViewAndGoneStates(t *testing.T) {
	cfg, ps := parksFixture(t, paging.DefaultLimit+2)
	pass, err := cfg.Store.AddPass(t.Context(), store.Pass{Kind: store.KindParks, Origin: store.OriginStart, Trigger: "gone-run"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = cfg.Store.ApplyParks(t.Context(), pass.ID, "gone-run", []string{"Anoka", "Chisago"}, parks.Plan{Gone: []string{ps[0].ID}}, store.Counts{}); err != nil {
		t.Fatal(err)
	}
	first := captureParks(t, cfg, "/parks")
	if len(first.Parks) != paging.DefaultLimit || first.Summary.Count != paging.DefaultLimit+1 || first.Summary.Gone != 1 || first.NextHref == "" {
		t.Fatal(first)
	}
	unknown := false
	for _, p := range first.Parks {
		if p.Minutes == "" {
			unknown = true
		} else if unknown {
			t.Fatal(first.Parks)
		}
	}
	gone := captureParks(t, cfg, "/parks?gone=gone")
	if len(gone.Parks) != 1 || gone.Parks[0].ID != ps[0].ID {
		t.Fatal(gone.Parks)
	}
	park, err := cfg.Store.GetPark(t.Context(), ps[0].ID)
	if err != nil || park.State != parks.StateGone {
		t.Fatal(park, err)
	}
	parcels, err := cfg.Store.ParkParcels(t.Context(), park.ID)
	if err != nil || len(parcels) == 0 {
		t.Fatal(parcels, err)
	}
	banner := bannerFor(cfg)
	banner.Trail = []page.Level{ParksLevel, {Name: park.ID, URL: "/parks/" + park.ID}}
	goneDetail := ParkData{Banner: banner, Park: parkView(park, cfg.ServicesPath), Parcels: []ParcelRow{}}
	for _, parcel := range parcels {
		goneDetail.Parcels = append(goneDetail.Parcels, parcelRow(parcel))
	}
	comparePage(t, cfg, "/parks/"+park.ID, "park", 200, goneDetail)
	all := captureParks(t, cfg, "/parks?gone=all&sort=parcels")
	wantAll, err := cfg.Store.ListParks(t.Context(), store.ParkFilter{Gone: parks.GoneAll}, parks.SortParcels, "", "", paging.DefaultLimit)
	if err != nil {
		t.Fatal(err)
	}
	wantRows := []ParkRow{}
	for _, p := range wantAll {
		wantRows = append(wantRows, parkRow(p))
	}
	if !reflect.DeepEqual(all.Parks, wantRows) {
		t.Fatal(all.Parks, wantRows)
	}
	single := captureParks(t, cfg, "/parks?county=Chisago")
	duplicate := captureParks(t, cfg, "/parks?county=Chisago&county=Anoka")
	if !reflect.DeepEqual(single, duplicate) {
		t.Fatal(single, duplicate)
	}
	u, err := url.Parse(first.NextHref)
	if err != nil {
		t.Fatal(err)
	}
	token := u.Query().Get("page")
	d := captureParks(t, cfg, "/parks?walked=unwalked&gone=all&page="+url.QueryEscape(token))
	if d.Page != token || d.FirstHref != "/parks?walked=unwalked&gone=all" {
		t.Fatal(d)
	}
	wantWalked := []ViewChoice{{parks.WalkedAll, "/parks?gone=all", false}, {parks.WalkedUnwalked, "/parks?walked=unwalked&gone=all", true}, {parks.WalkedWalked, "/parks?walked=walked&gone=all", false}}
	wantGone := []ViewChoice{{parks.GoneActive, "/parks?walked=unwalked", false}, {parks.GoneGone, "/parks?walked=unwalked&gone=gone", false}, {parks.GoneAll, "/parks?walked=unwalked&gone=all", true}}
	wantSorts := []ViewChoice{{parks.SortMinutes, "/parks?walked=unwalked&gone=all", true}, {parks.SortValue, "/parks?walked=unwalked&gone=all&sort=value", false}, {parks.SortParcels, "/parks?walked=unwalked&gone=all&sort=parcels", false}}
	wantCounties := []CountyChoice{{"", "/parks?walked=unwalked&gone=all", true}, {"Anoka", "/parks?county=Anoka&walked=unwalked&gone=all", false}, {"Chisago", "/parks?county=Chisago&walked=unwalked&gone=all", false}}
	if !reflect.DeepEqual(d.View, ParksView{Counties: wantCounties, Walked: wantWalked, Gone: wantGone, Sorts: wantSorts}) {
		t.Fatal(d.View)
	}
	for _, sort := range []string{parks.SortMinutes, parks.SortValue, parks.SortParcels} {
		last := captureParks(t, cfg, viewHref("", "", parks.GoneAll, sort, "last"))
		p, err := cfg.Store.GetPark(t.Context(), last.Parks[0].ID)
		if err != nil {
			t.Fatal(err)
		}
		key := ""
		switch sort {
		case parks.SortValue:
			key = fmt.Sprint(p.BuildingValue)
		case parks.SortParcels:
			key = fmt.Sprint(p.Parcels)
		case parks.SortMinutes:
			if p.Minutes != nil {
				key = fmt.Sprint(*p.Minutes)
			}
		}
		before := (paging.Token{Tool: "parks-before"}).Encode(sort, key, p.ID)
		if last.PrevHref != viewHref("", "", parks.GoneAll, sort, before) {
			t.Fatal(last.PrevHref)
		}
	}
}

// R-6SM3-3UZT
func TestParksLinkedSummaryAndExactRules(t *testing.T) {
	cfg, _ := parksFixture(t, 2)
	cfg.ServicesPath = filepath.Join(t.TempDir(), "services.json")
	if err := os.WriteFile(cfg.ServicesPath, []byte(`{"services":[{"name":"scrapes","url":"https://fixture-scrapes.test///","description":"","socket":"","enabled":true,"mcp":false}]}`), 0600); err != nil {
		t.Fatal(err)
	}
	rules, _, err := cfg.Store.ParkRules(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	counties, err := cfg.Store.ParkCountiesRead(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	wantRules := RulesView{WalkCeiling: places.SeedCeiling, Distance: fmt.Sprint(rules.Distance), MinParcels: rules.MinParcels, ValueThreshold: valueText(rules.ValueThreshold), Counties: []CountyLabels{}}
	nonempty := false
	for _, county := range counties {
		labels := append([]string{}, rules.Labels[county]...)
		nonempty = nonempty || len(labels) > 0
		wantRules.Counties = append(wantRules.Counties, CountyLabels{County: county, Labels: labels})
	}
	if !nonempty {
		t.Fatal(wantRules)
	}
	got := captureParks(t, cfg, "/parks")
	wantSummary := ParksSummary{Count: 2, Run: "parcel-run", RunPage: "https://fixture-scrapes.test/runs/parcel-run"}
	if got.Summary != wantSummary || !reflect.DeepEqual(got.Rules, wantRules) {
		t.Fatal(got.Summary, wantSummary, got.Rules, wantRules)
	}
}
