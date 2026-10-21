package pages

import (
	"bytes"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/candidates"
	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/routes"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// R-YG23-6DHP R-YH9Z-K58E
func TestDriveViewTypes(_ *testing.T) {
	_ = struct {
		KnownSince, KnownSinceText, NewestFiling, NewestFilingText                                                                         string
		Categories                                                                                                                         []CategoryRow
		ClassifiedBy                                                                                                                       string
		RecordsDiffer                                                                                                                      bool
		RecordCategories                                                                                                                   []CategoryRow
		GovernmentMatch, FleetVerdict, PowerUnits, Drivers, Minutes, MinutesSource, Miles, RouteAddress, RoutePlaceURL, RouteDirectionsURL string
		Written                                                                                                                            []WrittenRow
	}(FactsView{})
	_ = struct{ Kind, At, AtText string }(WrittenRow{})
	_ = struct{ Minutes, Miles, Address, Precision, Status, Why, Error, PlaceURL, DirectionsURL string }(RouteView{})
}

// R-JY3R-JM3Q R-JZBN-XDUF R-YIHV-XWZ3
func TestDriveFactsAndMemberViews(t *testing.T) {
	for _, tc := range []struct {
		f                               store.Facts
		minutes, source, miles, address string
	}{
		{store.Facts{Minutes: new(int64(47)), MinutesSource: store.MinutesByRoute, Miles: new(38.2), RouteAddress: "chosen-address"}, "47", store.MinutesByRoute, "38.2", "chosen-address"},
		{store.Facts{MinutesSource: store.MinutesByRoute, Miles: new(float64(12))}, "", store.MinutesByRoute, "12.0", ""},
		{store.Facts{MinutesSource: store.MinutesByRoute}, "", store.MinutesByRoute, "", ""},
		{store.Facts{Minutes: new(int64(84)), MinutesSource: store.MinutesByZip, Miles: new(38.2), RouteAddress: "ignored-address"}, "84", store.MinutesByZip, "", ""},
		{store.Facts{}, "", "", "", ""},
	} {
		f := factsView(tc.f, "chosen-origin")
		place, directions := "", ""
		if tc.f.MinutesSource == store.MinutesByRoute && tc.f.RouteAddress != "" {
			place = routes.PlaceURL(tc.f.RouteAddress)
			directions = routes.DirectionsURL("chosen-origin", tc.f.RouteAddress)
		}
		if f.RoutePlaceURL != place || f.RouteDirectionsURL != directions {
			t.Fatal(f)
		}
		noOrigin := factsView(tc.f, "")
		if noOrigin.RoutePlaceURL != place || noOrigin.RouteDirectionsURL != "" {
			t.Fatal(noOrigin)
		}
		if f.Minutes != tc.minutes || f.MinutesSource != tc.source || f.Miles != tc.miles || f.RouteAddress != tc.address {
			t.Fatal(f, tc)
		}
		m := memberRow(store.Ranked{Facts: tc.f}, 0)
		if m.Minutes != f.Minutes || m.MinutesSource != f.MinutesSource {
			t.Fatal(m, f)
		}
	}
}

// R-JS09-MRE9 R-JQSD-8ZNK R-JT86-0J4Y
func TestRouteResultProjection(t *testing.T) {
	for _, tc := range []struct {
		found  enrichers.RouteFound
		want   RouteView
		failed bool
	}{
		{enrichers.RouteFound{Minutes: new(int64(84)), Miles: new(61.3), Address: "chosen-address", Precision: routes.PrecisionBuilding, Status: "chosen-status", CostNanos: 5000000}, RouteView{"84", "61.3", "chosen-address", routes.PrecisionBuilding, "chosen-status", "", "", routes.PlaceURL("chosen-address"), ""}, false},
		{enrichers.RouteFound{Miles: new(float64(12))}, RouteView{Miles: "12.0"}, false},
		{enrichers.RouteFound{Precision: routes.PrecisionTown, Why: routes.WhyTooCoarse}, RouteView{Precision: routes.PrecisionTown, Why: routes.WhyTooCoarse}, false},
		{enrichers.RouteFound{Error: "chosen-error"}, RouteView{Error: "chosen-error"}, true},
	} {
		x := store.Enrichment{Kind: enrichers.KindDriveRoute, Source: enrichers.SourceGoogleRoutes, Found: encodedJSON(tc.found), Cost: .005, At: fixtureTime()}
		got := enrichmentRow(x, enrichers.Subject{})
		want := EnrichmentRow{Kind: x.Kind, Source: x.Source, Found: []FactRow{}, Route: &tc.want, Failed: tc.failed, Cost: "$0.005000", At: "2026-10-05T09:00:07Z", AtText: "2026-10-05 09:00"}
		if !reflect.DeepEqual(got, want) {
			t.Fatal(got, want)
		}
	}
}

// R-K0JK-B5L4
func TestDriveRouteTemplateStates(t *testing.T) {
	t.Chdir(t.TempDir())
	set, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	render := func(name string, data any) string {
		t.Helper()
		var b bytes.Buffer
		if err := set.templates.ExecuteTemplate(&b, name, data); err != nil {
			t.Fatal(name, err)
		}
		return b.String()
	}
	for _, source := range []string{store.MinutesByRoute, store.MinutesByZip, ""} {
		for _, miles := range []string{"", "facts-miles-chosen"} {
			for _, address := range []string{"", "facts-address-chosen"} {
				f := &FactsView{MinutesSource: source, Written: []WrittenRow{{Kind: enrichers.KindDriveRoute}}}
				if source != "" {
					f.Minutes = "47"
				}
				if source == store.MinutesByRoute {
					f.Miles, f.RouteAddress = miles, address
				}
				body := render("candidate", CandidateData{Candidate: CandidateView{Status: store.StatusActive}, Enrichments: &EnrichmentsView{Facts: f}})
				for _, value := range []string{f.Miles, f.RouteAddress} {
					if value != "" && !strings.Contains(body, value) {
						t.Fatal(value)
					}
				}
				render("prospect_list", ProspectListData{Members: []MemberRow{{Minutes: f.Minutes, MinutesSource: source}}})
			}
		}
	}
	for _, cost := range []string{"", "$0.005000"} {
		for _, precision := range []string{"", routes.PrecisionBuilding, routes.PrecisionStreet, routes.PrecisionTown} {
			for _, filled := range []bool{false, true} {
				for _, outcome := range []string{"routed", "none", "failed"} {
					r := &RouteView{Precision: precision}
					if filled {
						r.Address = "route-address-chosen"
						r.Status = "route-status-chosen"
					}
					switch outcome {
					case "routed":
						r.Minutes = "route-minutes-chosen"
						r.Miles = "route-miles-chosen"
					case "none":
						r.Why = "route-why-chosen"
					case "failed":
						r.Error = "route-error-chosen"
					}
					body := render("candidate", CandidateData{Candidate: CandidateView{Status: store.StatusActive}, EnrichmentResults: []EnrichmentRow{{Kind: enrichers.KindDriveRoute, Found: []FactRow{}, Route: r, Cost: cost, Failed: outcome == "failed"}, {Kind: enrichers.KindDriveMinutes, Found: []FactRow{}}}})
					for _, value := range []string{r.Minutes, r.Miles, r.Address, r.Precision, r.Status, r.Why, r.Error} {
						if value != "" && !strings.Contains(body, value) {
							t.Fatal(value)
						}
					}
				}
			}
		}
	}
	render("pass", PassData{Pass: PassView{Kind: store.KindEnrichTarget, EnrichKind: enrichers.KindDriveRoute, Runs: 2, RunsFailed: 1, Cost: "$0.010000"}})
}

// R-JUG2-EAVN R-JY3R-JM3Q R-JZBN-XDUF R-YIHV-XWZ3
func TestCandidateAndListDriveDecision(t *testing.T) {
	for i, result := range []enrichers.RouteFound{{Minutes: new(int64(47)), Miles: new(38.2), Address: "chosen-address"}, {Why: routes.WhyTooCoarse}, {Error: "chosen-error"}, {Minutes: new(int64(12)), Address: "old-address"}} {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			cfg, db := pageFixture(t)
			cfg.Store = store.New(db, store.Config{Now: fixtureTime, Rand: &sequence{}, Origin: "chosen-origin"})
			_, ids, err := cfg.Store.Attach(t.Context(), "chosen-run", false, []store.Observation{{Scraper: "fixture", SourceID: "chosen-row", Digest: "chosen-digest", Reading: sources.Reading{Street: "chosen-street", City: "chosen-city", State: "MN", Zip: "55072"}}})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = cfg.Store.JudgeAll(t.Context()); err != nil {
				t.Fatal(err)
			}
			list, err := cfg.Store.CreateProspectList(t.Context(), store.ProspectList{Name: "chosen-list", Size: 10, CreatedUserID: "chosen-owner"})
			if err != nil {
				t.Fatal(err)
			}

			routeInputs := (enrichers.DriveRoute{}).Inputs(enrichers.Subject{Street: "chosen-street", City: "chosen-city", State: "MN", Zip: "55072", Origin: "chosen-origin"})
			if i == 3 {
				routeInputs = "stale-inputs"
			}
			_, err = cfg.Store.AddEnrichments(t.Context(), []store.Enrichment{{Candidate: ids[0], Kind: enrichers.KindDriveMinutes, Source: enrichers.SourceZipDrive, Inputs: "chosen-zip-input", Found: encodedJSON(enrichers.MinutesFound{Minutes: new(int64(84))})}, {Candidate: ids[0], Kind: enrichers.KindDriveRoute, Source: enrichers.SourceGoogleRoutes, Inputs: routeInputs, Found: encodedJSON(result), Cost: .005}})
			if err != nil {
				t.Fatal(err)
			}
			candidate := capturedCandidate(t, cfg, ids[0])
			f := candidate.Enrichments.Facts
			minutes, source := "84", store.MinutesByZip
			if i == 0 {
				minutes, source = "47", store.MinutesByRoute
			}
			if f.Minutes != minutes || f.MinutesSource != source {
				t.Fatal(f)
			}
			if i == 0 {
				if f.Miles != "38.2" || f.RouteAddress != "chosen-address" || f.RoutePlaceURL != routes.PlaceURL("chosen-address") || f.RouteDirectionsURL != routes.DirectionsURL("chosen-origin", "chosen-address") {
					t.Fatal(f)
				}
			} else if f.Miles != "" || f.RouteAddress != "" || f.RoutePlaceURL != "" || f.RouteDirectionsURL != "" {
				t.Fatal(f)
			}
			found := false
			for _, row := range candidate.EnrichmentResults {
				if row.Kind == enrichers.KindDriveRoute {
					found = true
					if row.Route == nil || row.Run != nil || row.Answer != nil || row.Confirmation != nil || len(row.Found) != 0 || row.Failed != (row.Route.Error != "") || row.Cost != "$0.005000" {
						t.Fatal(row)
					}
				}
			}
			if !found {
				t.Fatal(candidate.EnrichmentResults)
			}
			written := false
			for _, w := range f.Written {
				if w.Kind == enrichers.KindDriveRoute {
					written = true
				}
			}
			if !written {
				t.Fatal(f.Written)
			}
			members := pageProspectList(t, cfg, list.ID).Members
			if len(members) != 1 || members[0].Minutes != f.Minutes || members[0].MinutesSource != f.MinutesSource {
				t.Fatal(members, f)
			}
		})
	}
}

// R-YKXO-PGGH
func TestDriveRouteLinkTemplateStates(t *testing.T) {
	t.Chdir(t.TempDir())
	set, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, linkedFacts := range []bool{false, true} {
		facts := &FactsView{MinutesSource: store.MinutesByRoute, Minutes: "47", RouteAddress: "chosen-facts-address"}
		if linkedFacts {
			facts.RoutePlaceURL = "https://chosen.example.test/facts-place"
			facts.RouteDirectionsURL = "https://chosen.example.test/facts-directions"
		}
		rows := []EnrichmentRow{}
		values := []string{facts.RoutePlaceURL, facts.RouteDirectionsURL}
		for i, outcome := range []string{"current", "historical", "unlinked", "none", "failed"} {
			route := &RouteView{}
			switch outcome {
			case "current", "historical", "unlinked":
				route.Minutes, route.Miles, route.Address = "47", "38.2", "chosen-result-address"
				if outcome != "unlinked" {
					route.PlaceURL = "https://chosen.example.test/place-" + strconv.Itoa(i)
				}
				if outcome == "current" {
					route.DirectionsURL = "https://chosen.example.test/directions-" + strconv.Itoa(i)
				}
			case "none":
				route.Why = "chosen-unroutable"
			case "failed":
				route.Error = "chosen-failure"
			}
			rows = append(rows, EnrichmentRow{Kind: enrichers.KindDriveRoute, Route: route, Failed: outcome == "failed"})
			values = append(values, route.PlaceURL, route.DirectionsURL)
		}
		var body bytes.Buffer
		if err := set.templates.ExecuteTemplate(&body, "candidate", CandidateData{Candidate: CandidateView{Status: store.StatusActive}, Enrichments: &EnrichmentsView{Facts: facts}, EnrichmentResults: rows}); err != nil {
			t.Fatal(err)
		}
		for _, value := range values {
			if value != "" && !strings.Contains(body.String(), value) {
				t.Fatal(value)
			}
		}
	}
}

// R-YJPS-BOPS
func TestCandidateRouteLinksUseCurrentStoreInputs(t *testing.T) {
	cfg, _ := pageFixture(t)
	path := filepath.Join(t.TempDir(), "routes.db")
	open := func(origin string) *db.DB {
		t.Helper()
		d, err := db.Open(t.Context(), db.Config{Path: path, Migrations: candidates.Migrations(), Now: fixtureTime})
		if err != nil {
			t.Fatal(err)
		}
		cfg.Store = store.New(d, store.Config{Now: fixtureTime, Rand: &sequence{}, Origin: origin})
		return d
	}
	d := open("chosen-origin")
	t.Cleanup(func() {
		if d != nil {
			if err := d.Close(); err != nil {
				t.Error(err)
			}
		}
	})
	_, ids, err := cfg.Store.Attach(t.Context(), "chosen-run", false, []store.Observation{{Scraper: "fixture", SourceID: "chosen-row", Digest: "chosen-digest", Reading: sources.Reading{Street: "chosen-street", City: "chosen-city", State: "MN", Zip: "55072"}}})
	if err != nil {
		t.Fatal(err)
	}
	subject, err := cfg.Store.Subject(t.Context(), ids[0])
	if err != nil {
		t.Fatal(err)
	}
	inputs := (enrichers.DriveRoute{}).Inputs(subject)
	results := []struct {
		inputs string
		found  enrichers.RouteFound
	}{
		{"old-inputs", enrichers.RouteFound{Minutes: new(int64(51)), Miles: new(40.1), Address: "historical-address"}},
		{inputs, enrichers.RouteFound{Minutes: new(int64(47)), Miles: new(38.2), Address: "current-address"}},
		{inputs, enrichers.RouteFound{Why: routes.WhyTooCoarse, Address: "unroutable-address"}},
		{inputs, enrichers.RouteFound{Minutes: new(int64(13)), Error: "chosen-error", Address: "failed-address"}},
		{inputs, enrichers.RouteFound{Minutes: new(int64(9))}},
	}
	for _, result := range results {
		if _, err := cfg.Store.AddEnrichments(t.Context(), []store.Enrichment{{Candidate: ids[0], Kind: enrichers.KindDriveRoute, Source: enrichers.SourceGoogleRoutes, Inputs: result.inputs, Found: encodedJSON(result.found)}}); err != nil {
			t.Fatal(err)
		}
	}
	assertLinks := func(origin string) {
		t.Helper()
		candidate := capturedCandidate(t, cfg, ids[0])
		if len(candidate.EnrichmentResults) != len(results) {
			t.Fatal(candidate.EnrichmentResults)
		}
		for _, row := range candidate.EnrichmentResults {
			route := row.Route
			if route == nil {
				t.Fatal(row)
			}
			place, directions := "", ""
			switch route.Address {
			case "current-address":
				place = routes.PlaceURL(route.Address)
				if origin == "chosen-origin" {
					directions = routes.DirectionsURL(origin, route.Address)
				}
			case "historical-address":
				place = routes.PlaceURL(route.Address)
			}
			if route.PlaceURL != place || route.DirectionsURL != directions {
				t.Fatalf("origin %q: %+v", origin, route)
			}
		}
	}
	assertLinks("chosen-origin")
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	d = nil
	d = open("changed-origin")
	assertLinks("changed-origin")
}
