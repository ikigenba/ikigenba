package store_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/website"
)

func fillFactsWalk(ctx context.Context, s *store.Store) error {
	after := ""
	for {
		next, err := s.FillFacts(ctx, after, 100)
		if err != nil {
			return err
		}
		if next == "" {
			return nil
		}
		after = next
	}
}

// R-EI9A-YN1H R-D31I-W9WB
func TestFactsRowExistsOnlyAfterProjection(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	id := contractSeed(t, f)
	for _, key := range []string{id, "absent"} {
		got, err := f.s.GetFacts(ctx, key)
		contractError(t, err, store.ErrNotFound)
		contractEqual(t, got, store.Facts{})
	}
	_, err := f.s.FillFacts(ctx, "", 1)
	contractOK(t, err)
	_, err = f.s.GetFacts(ctx, id)
	contractError(t, err, store.ErrNotFound)
	_, err = f.s.AddEnrichments(ctx, []store.Enrichment{headcountLinkResult(t, id)})
	contractOK(t, err)
	got, err := f.s.GetFacts(ctx, id)
	contractOK(t, err)
	contractEqual(t, got, store.Facts{Candidate: id, Categories: []sources.Category{}, Records: []sources.Category{}, FleetVerdict: sources.FleetUnknown, Headcounts: []sources.Headcount{}, Website: sources.WebsiteUnknown, BriefLabels: []string{}, Written: []store.Written{{Kind: enrichers.KindLinkFacts, At: f.now.UTC().Truncate(time.Second)}}})
}

// R-Y053-JMWZ
func TestFactsProjectionAllocationBound(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	id := contractSeed(t, f)
	f.s = routeStore(f, "base")
	contractOK(t, f.d.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "UPDATE candidates SET street='12 Main',city='Town',state='MN',zip='55072' WHERE id=?", id)
		return err
	}))
	su, err := f.s.Subject(ctx, id)
	contractOK(t, err)
	inputs := (enrichers.DriveRoute{}).Inputs(su)
	pad := strings.Repeat("x", 4096)
	minutes := int64(42)
	link := headcountLinkResult(t, id)
	var lf enrichers.LinkFound
	contractOK(t, json.Unmarshal(link.Found, &lf))
	lf.GovernmentMatch = pad
	link.Found, err = json.Marshal(lf)
	contractOK(t, err)
	check := websiteResult(id, sources.WebsiteHTTPS)
	check.Found, err = json.Marshal(website.Result{State: sources.WebsiteHTTPS, URL: "https://business.test", Title: pad})
	contractOK(t, err)
	routeBytes, err := json.Marshal(enrichers.RouteFound{Minutes: &minutes, Address: pad})
	contractOK(t, err)
	good := briefResult(t, id, pad, false)
	bad := briefResult(t, id, pad, true)
	var failed enrichers.BriefFound
	contractOK(t, json.Unmarshal(bad.Found, &failed))
	failed.Summary = pad
	bad.Found, err = json.Marshal(failed)
	contractOK(t, err)
	searchBytes, err := json.Marshal(enrichers.WebsiteSearchFound{Run: enrichers.WebsiteSearchRun{ID: "search-run", Status: enrichers.RunExited, ExitCode: new(int)}, Answer: &enrichers.WebsiteSearchAnswer{URL: "https://other.test/", Why: pad, Rejected: []enrichers.WebsiteSearchSite{}}, Reason: enrichers.SearchReasonAgain, Website: "https://business.test", WebsiteFrom: store.WebsiteByCheck, Case: enrichers.SearchCaseStands, Confirmation: &enrichers.WebsiteSearchConfirmation{State: sources.WebsiteHTTPS, URL: "https://www.business.test/contact"}})
	contractOK(t, err)
	groups := []struct {
		n int
		e store.Enrichment
	}{{4000, link}, {1000, check}, {1000, store.Enrichment{Candidate: id, Kind: enrichers.KindWebsiteSearch, Inputs: "search", Source: enrichers.SourcePrompts, Found: searchBytes}}, {1000, store.Enrichment{Candidate: id, Kind: enrichers.KindDriveRoute, Inputs: inputs, Source: enrichers.SourceGoogleRoutes, Found: routeBytes}}, {999, good}, {1, bad}, {40000, store.Enrichment{Candidate: id, Kind: enrichers.KindDriveMinutes, Inputs: "zip", Source: "zip_drive", Found: json.RawMessage(`{"minutes":42}`)}}}
	for _, g := range groups {
		if g.n != 40000 && len(g.e.Found) < 4096 {
			t.Fatalf("fixture %s only %d bytes", g.e.Kind, len(g.e.Found))
		}
		for left := g.n; left > 0; {
			n := min(left, 100)
			batch := make([]store.Enrichment, n)
			for i := range batch {
				batch[i] = g.e
			}
			_, err = f.s.AddEnrichments(ctx, batch)
			contractOK(t, err)
			left -= n
		}
	}
	small := headcountLinkResult(t, id)
	calls := []struct {
		name string
		call func() error
	}{{"add", func() error { _, e := f.s.AddEnrichments(ctx, []store.Enrichment{small}); return e }}, {"derive", func() error { _, _, e := f.s.Derive(ctx, enrichers.KindLinkFacts, "", true, 1); return e }}, {"fill", func() error { _, e := f.s.FillFacts(ctx, "", 1); return e }}}
	for _, c := range calls {
		runtime.GC()
		var before, after runtime.MemStats
		runtime.ReadMemStats(&before)
		err = c.call()
		runtime.ReadMemStats(&after)
		contractOK(t, err)
		allocated := after.TotalAlloc - before.TotalAlloc
		t.Logf("%s allocated %d bytes", c.name, allocated)
		got := factsAt(ctx, t, f.s, id)
		contractEqual(t, got.Brief, pad)
		contractEqual(t, got.MinutesSource, store.MinutesByRoute)
		contractEqual(t, *got.Minutes, minutes)
		contractEqual(t, got.RouteAddress, pad)
		contractEqual(t, got.Website, sources.WebsiteHTTPS)
		if allocated >= 4194304 {
			t.Errorf("%s allocated %d bytes; bound 4194304", c.name, allocated)
		}
	}
}
