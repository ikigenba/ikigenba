package passes_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"reflect"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/scrapes"
	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/fmcsa"
	"github.com/ikigenba/ikigenba/candidates/internal/passes"
	"github.com/ikigenba/ikigenba/candidates/internal/ppp"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// R-37CV-BB9N
func TestRebuildReprojectsUnchangedLinkFactsBeforeHistoricalJudgment(t *testing.T) {
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
			_ = json.NewEncoder(w).Encode(map[string]any{"scraper": row.Scraper, "run": scraper + "-current", "source_id": row.SourceID, "digest": row.Digest, "name": row.Name, "collected": "2000-01-01T00:00:00Z", "names": []string{}, "people": []any{}, "facts": row.Facts, "raw": map[string]any{}, "address": map[string]any{}})
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
	p, err := f.eng.Rebuild(ctx, identity.Caller{UserID: "owner"})
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
		a, err := f.st.GetAssessment(ctx, ids[key])
		if err != nil || !reflect.DeepEqual(a.Categories, want) {
			t.Fatal(key, a, err)
		}
	}
	after, err := f.st.Enrichments(ctx, ids["brief"])
	if err != nil {
		t.Fatal(err)
	}
	if after[0].Kind != enrichers.KindLinkFacts || !bytes.Equal(after[0].Found, original) {
		t.Fatal("link facts changed", after)
	}
	if _, _, err := f.st.Decide(ctx, store.Ruling{Kind: store.KindMerge, Candidate: ids["hire"], Other: ids["brief"], UserID: "owner", Why: "projection history fixture"}); err != nil {
		t.Fatal(err)
	}
	a, err := f.st.GetAssessment(ctx, ids["brief"])
	if err != nil || !reflect.DeepEqual(a.Categories, []sources.Category{{Name: sources.CategoryRetail, Source: enrichers.KindBrief}}) {
		t.Fatal(a, err)
	}
}
