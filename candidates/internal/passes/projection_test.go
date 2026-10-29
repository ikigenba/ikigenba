package passes_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"reflect"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/scrapes"
	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/fmcsa"
	"github.com/ikigenba/ikigenba/candidates/internal/passes"
	"github.com/ikigenba/ikigenba/candidates/internal/ppp"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// R-HRBN-OM85
func TestFullIngestUnchangedRowsPreserveCandidate(t *testing.T) {
	t.Parallel()
	for _, reverse := range []bool{false, true} {
		name := "same order"
		if reverse {
			name = "reverse order"
		}
		t.Run(name, func(t *testing.T) {
			ctx := t.Context()
			var second atomic.Bool
			f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/scrapers/fmcsa/current":
					record(w, "stable-run", 3, store.StatusDone)
				case "/runs/stable-run/rows":
					ids := []string{"c", "a", "b"}
					if second.Load() && reverse {
						slices.Reverse(ids)
					}
					for _, id := range ids {
						_ = json.NewEncoder(w).Encode(map[string]any{
							"scraper": fmcsa.Name, "run": "stable-run", "source_id": id, "digest": id,
							"name": "Carrier " + id, "names": []string{"Shared Carrier", "Carrier " + id},
							"scraped": "2000-01-01T00:00:00Z", "people": []any{}, "raw": map[string]any{},
							"address": map[string]any{"street": "12 Main St", "city": "Duluth", "state": "MN", "zip": "55802"},
							"facts":   map[string]string{"mcs150_date": "20250318", "added": "20100301", "status": "A", "class": "AUTHORIZED FOR HIRE", "power_units": "3", "total_drivers": map[string]string{"a": "3", "b": "4", "c": "5"}[id]},
						})
					}
				default:
					t.Errorf("unexpected request %s", r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
				}
			}, true)
			firstTime := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
			f.st = store.New(f.d, store.Config{Now: func() time.Time {
				if second.Load() {
					return firstTime.Add(time.Second)
				}
				return firstTime
			}, Rand: &random{}})
			f.eng = passes.New(passes.Config{Store: f.st, Sources: sources.New(fmcsa.New()), Scrapes: f.sc, Telemetry: f.w, Events: f.em})
			ingest := func() store.Pass {
				t.Helper()
				p, err := f.eng.Ingest(ctx, identity.Caller{UserID: "owner"}, "")
				if err != nil {
					t.Fatal(err)
				}
				p = f.ended(p.ID)
				if p.Status != store.StatusDone || p.Rows != 3 {
					t.Fatal(p)
				}
				return p
			}
			ingest()
			cs, err := f.st.ListCandidates(ctx, store.CandidateFilter{}, "", "", 10)
			if err != nil || len(cs) != 1 {
				t.Fatal(cs, err)
			}
			before, err := f.st.GetCandidate(ctx, cs[0].ID)
			if err != nil {
				t.Fatal(err)
			}
			links, err := f.st.Links(ctx, before.ID)
			if err != nil || len(links) != 3 {
				t.Fatal(links, err)
			}
			second.Store(true)
			p := ingest()
			if p.Unchanged != 3 || p.Changed != 0 || !p.Started.Equal(firstTime.Add(time.Second)) {
				t.Fatal(p)
			}
			after, err := f.st.GetCandidate(ctx, before.ID)
			if err != nil || !reflect.DeepEqual(after, before) {
				t.Fatalf("candidate changed after unchanged ingest: before=%+v after=%+v error=%v", before, after, err)
			}
			links, err = f.st.Links(ctx, before.ID)
			if err != nil {
				t.Fatal(err)
			}
			for _, link := range links {
				if !link.LastSeen.Equal(firstTime.Add(time.Second)) {
					t.Fatal("link not refreshed", link)
				}
			}
		})
	}
}

// R-HGCK-8OJW
func TestFullIngestReprojectsUnchangedLinkFacts(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	rows := []scrapes.Row{
		{Scraper: fmcsa.Name, SourceID: "hire", Digest: "hire", Name: "Hire Carrier", Facts: map[string]json.RawMessage{"class": json.RawMessage(`"AUTHORIZED FOR HIRE"`)}},
		{Scraper: fmcsa.Name, SourceID: "private", Digest: "private", Name: "Private Carrier", Facts: map[string]json.RawMessage{"class": json.RawMessage(`"PRIVATE PROPERTY"`)}},
		{Scraper: ppp.Name, SourceID: "brief", Digest: "brief", Name: "Brief Business", Facts: map[string]json.RawMessage{"naics": json.RawMessage(`"236220"`)}},
	}
	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		scraper := fmcsa.Name
		if r.URL.Path == "/scrapers/ppp/current" || r.URL.Path == "/runs/ppp-current/rows" {
			scraper = ppp.Name
		}
		var selected []scrapes.Row
		for _, row := range rows {
			if row.Scraper == scraper {
				selected = append(selected, row)
			}
		}
		if r.URL.Path == "/scrapers/"+scraper+"/current" {
			_ = json.NewEncoder(w).Encode(map[string]any{"id": scraper + "-current", "scraper": scraper, "status": store.StatusDone, "started": "2000-01-01T00:00:00Z", "rows": len(selected), "new": 0, "changed": 0, "unchanged": 0, "gone": 0})
			return
		}
		if r.URL.Path != "/runs/"+scraper+"-current/rows" {
			t.Errorf("unexpected request %s", r.URL.Path)
			return
		}
		for _, row := range selected {
			_ = json.NewEncoder(w).Encode(map[string]any{"scraper": row.Scraper, "run": scraper + "-current", "source_id": row.SourceID, "digest": row.Digest, "name": row.Name, "scraped": "2000-01-01T00:00:00Z", "names": []string{}, "people": []any{}, "facts": row.Facts, "raw": map[string]any{}, "address": map[string]any{}})
		}
	}, true)
	ids := map[string]string{}
	for _, row := range rows {
		var reading sources.Reading
		if row.Scraper == fmcsa.Name {
			reading = fmcsa.New().Read(row)
		} else {
			reading = ppp.New().Read(row)
		}
		if row.SourceID == "private" {
			reading.Category = sources.CategoryTrucking
		}
		_, got, err := f.st.Attach(ctx, row.Scraper+"-current", false, []store.Observation{{Scraper: row.Scraper, SourceID: row.SourceID, Digest: row.Digest, Reading: reading}})
		if err != nil || len(got) != 1 {
			t.Fatal(got, err)
		}
		ids[row.SourceID] = got[0]
	}
	if _, _, err := f.st.Derive(ctx, enrichers.KindLinkFacts, "", false, 100); err != nil {
		t.Fatal(err)
	}
	code := 0
	brief, _ := json.Marshal(enrichers.BriefFound{Run: "run-brief", Status: enrichers.RunExited, ExitCode: &code, Summary: "fixture", Categories: []enrichers.BriefCategory{{Name: sources.CategoryRetail, Why: "fixture"}}, Labels: []string{}})
	if _, err := f.st.AddEnrichments(ctx, []store.Enrichment{{Candidate: ids["brief"], Kind: enrichers.KindBrief, Found: brief, Source: enrichers.SourcePrompts, Inputs: "pages"}}); err != nil {
		t.Fatal(err)
	}
	before, err := f.st.Enrichments(ctx, ids["brief"])
	if err != nil {
		t.Fatal(err)
	}
	var original json.RawMessage
	for _, en := range before {
		if en.Kind == enrichers.KindLinkFacts {
			original = en.Found
			break
		}
	}
	stale, _ := json.Marshal([]sources.Category{{Name: sources.CategoryConstruction, Source: ppp.Name}, {Name: sources.CategoryRetail, Source: enrichers.KindBrief}})
	if err := f.d.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "UPDATE facts SET categories=? WHERE candidate=?", string(stale), ids["brief"])
		return err
	}); err != nil {
		t.Fatal(err)
	}
	f.eng = passes.New(passes.Config{Store: f.st, Sources: sources.New(fmcsa.New(), ppp.New()), Scrapes: f.sc, Telemetry: f.w, Events: f.em})
	p, err := f.eng.Ingest(ctx, identity.Caller{UserID: "owner"}, "")
	if err != nil {
		t.Fatal(err)
	}
	p = f.ended(p.ID)
	if p.Status != store.StatusDone || p.Judged != 3 {
		t.Fatal(p)
	}
	for key, want := range map[string][]sources.Category{
		"hire":    {{Name: sources.CategoryTrucking, Source: fmcsa.Name}},
		"private": {},
		"brief":   {{Name: sources.CategoryRetail, Source: enrichers.KindBrief}},
	} {
		facts, err := f.st.GetFacts(ctx, ids[key])
		if err != nil || !reflect.DeepEqual(facts.Categories, want) {
			t.Fatal(key, facts, err)
		}
	}
	after, err := f.st.Enrichments(ctx, ids["brief"])
	if err != nil {
		t.Fatal(err)
	}
	if after[0].Kind != enrichers.KindLinkFacts || !bytes.Equal(after[0].Found, original) {
		t.Fatal("link facts changed", after)
	}
}
