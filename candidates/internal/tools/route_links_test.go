package tools_test

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	candidates "github.com/ikigenba/ikigenba/candidates"
	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/routes"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/tools"
)

// R-FT25-CWCQ R-FLQR-29WK R-FUA1-QO3F
func TestCandidateRouteLinksFollowResolvedResultsAndCurrentOrigin(t *testing.T) {
	const origin = "Chosen origin & base"
	const address = "42 Resolved St, Town"
	const olderAddress = "17 Earlier Rd"
	f := fixtureWithRoutes(t, nil, nil, nil, nil, origin)
	id := f.seed(1)[0]
	subject, err := f.st.Subject(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	inputs := (enrichers.DriveRoute{}).Inputs(subject)
	if inputs == "" {
		t.Fatal("empty inputs")
	}
	minutes, miles, zip := int64(23), 14.5, int64(64)
	add := func(kind, inputs string, found any) {
		t.Helper()
		f.now = f.now.Add(time.Second)
		source := enrichers.SourceGoogleRoutes
		if kind == enrichers.KindDriveMinutes {
			source = enrichers.SourceZipDrive
		}
		if _, err := f.st.AddEnrichments(t.Context(), []store.Enrichment{{Candidate: id, Kind: kind, Source: source, Inputs: inputs, Found: encodedJSON(found), Cost: .125}}); err != nil {
			t.Fatal(err)
		}
	}
	read := func() tools.Detail {
		t.Helper()
		r := f.call("candidate", tools.CandidateArgs{Candidate: id})
		if r.IsError() {
			t.Fatal(decode(t, r))
		}
		return decodeList[tools.Detail](t, r)
	}
	checkFacts := func(wantPlace, wantDirections string) {
		t.Helper()
		got := read().Enrichments.Facts
		if got == nil || got.PlaceURL != wantPlace || got.DirectionsURL != wantDirections {
			t.Fatalf("facts: %+v", got)
		}
	}
	checkLatest := func(place, directions string) {
		t.Helper()
		got := read().EnrichmentResults[0]
		if got.PlaceURL != place || got.DirectionsURL != directions {
			t.Fatalf("result: %+v", got)
		}
	}
	add(enrichers.KindDriveMinutes, "zip-inputs", enrichers.MinutesFound{Minutes: &zip})
	checkFacts("", "")
	for _, found := range []enrichers.RouteFound{
		{Why: "No routable business", Address: address},
		{Error: "Chosen request failed", Address: address, Minutes: &minutes},
		{Minutes: &minutes},
	} {
		add(enrichers.KindDriveRoute, inputs, found)
		checkFacts("", "")
		checkLatest("", "")
	}
	add(enrichers.KindDriveRoute, "old-inputs", enrichers.RouteFound{Minutes: &minutes, Miles: &miles, Address: olderAddress, Precision: routes.PrecisionStreet})
	checkFacts("", "")
	checkLatest(routes.PlaceURL(olderAddress), "")
	good := enrichers.RouteFound{Minutes: &minutes, Miles: &miles, Address: address, Precision: routes.PrecisionStreet}
	add(enrichers.KindDriveRoute, inputs, good)
	checkFacts(routes.PlaceURL(address), routes.DirectionsURL(origin, address))
	checkLatest(routes.PlaceURL(address), routes.DirectionsURL(origin, address))
	got := read().EnrichmentResults
	if got[1].PlaceURL != routes.PlaceURL(olderAddress) || got[1].DirectionsURL != "" || got[len(got)-1].PlaceURL != "" || got[len(got)-1].DirectionsURL != "" {
		t.Fatal(got)
	}
	add(enrichers.KindDriveRoute, inputs, enrichers.RouteFound{Error: "Later request failed"})
	checkFacts(routes.PlaceURL(address), routes.DirectionsURL(origin, address))
	checkLatest("", "")

	// A new handle and store recompute current inputs from the changed origin.
	later, err := db.Open(t.Context(), db.Config{Path: f.dbPath, Migrations: candidates.Migrations(), Now: func() time.Time { return f.now }, Service: "candidates"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := later.Close(); err != nil {
			t.Error(err)
		}
	})
	f.st = store.New(later, store.Config{Origin: "Another origin", Now: func() time.Time { return f.now }, Rand: &random{}})
	w := telemetry.New(telemetry.Config{Service: "candidates", Sink: &telemetry.Capture{}, Now: func() time.Time { return f.now }, Sleep: func(context.Context, time.Duration) {}, Rand: &random{}})
	srv := mcp.NewServer(mcp.ServerConfig{Name: "candidates", Telemetry: w})
	tools.Register(srv, tools.Config{Store: f.st})
	host := httptest.NewServer(identity.Require(srv))
	t.Cleanup(host.Close)
	f.client = mcp.NewClient(mcp.ClientConfig{Endpoint: host.URL, HTTPClient: host.Client()})
	got = read().EnrichmentResults
	if got[1].PlaceURL != routes.PlaceURL(address) || got[1].DirectionsURL != "" {
		t.Fatal(got)
	}
}
