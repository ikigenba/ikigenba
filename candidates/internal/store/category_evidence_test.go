package store_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/fmcsa"
	"github.com/ikigenba/ikigenba/candidates/internal/ppp"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// R-8BI4-YMZ3 R-8CQ1-CEPS R-8DXX-Q6GH
func TestCategoryEvidenceHelpers(t *testing.T) {
	brief := []sources.Category{{Name: sources.CategoryAgriculture, Source: enrichers.KindBrief}, {Name: sources.CategoryRetail, Source: enrichers.KindBrief}}
	records := []sources.Category{{Name: sources.CategoryConstruction, Source: ppp.Name}, {Name: sources.CategoryTrucking, Source: fmcsa.Name}}
	mixed := []sources.Category{brief[1], records[1]}
	for _, tt := range []struct {
		categories []sources.Category
		want       string
	}{
		{nil, ""}, {[]sources.Category{}, ""}, {brief, enrichers.KindBrief}, {records, enrichers.SourceLinks}, {mixed, enrichers.SourceLinks},
	} {
		contractEqual(t, store.ClassifiedBy(tt.categories), tt.want)
	}
	for _, tt := range []struct {
		categories, records []sources.Category
		want                bool
	}{
		{brief, records, true}, {brief, []sources.Category{{Name: sources.CategoryRetail, Source: ppp.Name}}, true}, {brief, nil, true},
		{brief, []sources.Category{{Name: sources.CategoryRetail, Source: ppp.Name}, {Name: sources.CategoryAgriculture, Source: ppp.Name}}, false},
		{[]sources.Category{brief[1]}, []sources.Category{{Name: sources.CategoryRetail, Source: ppp.Name}}, false},
		{records, records, false}, {records, nil, false}, {records, []sources.Category{brief[1]}, false}, {nil, nil, false}, {mixed, nil, false},
		{[]sources.Category{brief[1], brief[1]}, []sources.Category{{Name: sources.CategoryRetail, Source: ppp.Name}}, false},
	} {
		contractEqual(t, store.RecordsDiffer(tt.categories, tt.records), tt.want)
	}
}

func categoryLinks(t *testing.T, id string, categories ...sources.Category) store.Enrichment {
	t.Helper()
	v := enrichers.LinkFound{FleetVerdict: sources.FleetUnknown, Categories: []enrichers.Category{}}
	for _, c := range categories {
		v.Categories = append(v.Categories, enrichers.Category{Name: c.Name, Source: c.Source})
	}
	raw, err := json.Marshal(v)
	contractOK(t, err)
	return store.Enrichment{Candidate: id, Kind: enrichers.KindLinkFacts, Inputs: "record-inputs", Source: enrichers.SourceLinks, Found: raw}
}

// R-7UFJ-LULD R-7VNF-ZMC2 R-80J1-IPAU
func TestBriefCategoriesReplaceRecordsAndEmptyGoodBriefRestoresThem(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	id := contractSeed(t, f)
	records := []sources.Category{{Name: sources.CategoryConstruction, Source: ppp.Name}, {Name: sources.CategoryTrucking, Source: fmcsa.Name}}
	_, err := f.s.AddEnrichments(ctx, []store.Enrichment{categoryLinks(t, id, records[1], records[0], records[1])})
	contractOK(t, err)
	check := func(want []sources.Category, summary string) store.Facts {
		t.Helper()
		got, e := f.s.GetFacts(ctx, id)
		contractOK(t, e)
		contractEqual(t, got.Categories, want)
		contractEqual(t, got.Records, records)
		contractEqual(t, got.Brief, summary)
		return got
	}
	check(records, "")
	brief := []sources.Category{{Name: sources.CategoryAgriculture, Source: enrichers.KindBrief}, {Name: sources.CategoryRetail, Source: enrichers.KindBrief}}
	for _, step := range []struct {
		summary string
		failed  bool
		names   []string
		want    []sources.Category
	}{
		{"first", false, []string{sources.CategoryRetail, sources.CategoryAgriculture, sources.CategoryRetail}, brief},
		{"first", true, nil, brief},
		{"no categories", false, nil, records},
		{"no categories", true, nil, records},
	} {
		f.now = f.now.Add(time.Hour)
		_, err = f.s.AddEnrichments(ctx, []store.Enrichment{briefResult(t, id, step.summary, step.failed, step.names...)})
		contractOK(t, err)
		got := check(step.want, step.summary)
		contractEqual(t, got.Written[0], store.Written{Kind: enrichers.KindBrief, At: f.now.UTC().Truncate(time.Second)})
		f.reopen()
		contractEqual(t, check(step.want, step.summary), got)
	}
	// A later link result replaces the records without restoring an older brief.
	_, err = f.s.AddEnrichments(ctx, []store.Enrichment{categoryLinks(t, id)})
	contractOK(t, err)
	got, err := f.s.GetFacts(ctx, id)
	contractOK(t, err)
	contractEqual(t, got.Categories, []sources.Category{})
	contractEqual(t, got.Records, []sources.Category{})
}

// R-8F5U-3Y76
func TestRecordsReadNewestLinkResultBesidePreviousWriterCategories(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	id := contractSeed(t, f)
	trucking := sources.Category{Name: sources.CategoryTrucking, Source: fmcsa.Name}
	_, err := f.s.AddEnrichments(ctx, []store.Enrichment{categoryLinks(t, id, trucking)})
	contractOK(t, err)
	mixed := []sources.Category{{Name: sources.CategoryRetail, Source: enrichers.KindBrief}, {Name: sources.CategoryConstruction, Source: ppp.Name}, trucking}
	raw, err := json.Marshal(mixed)
	contractOK(t, err)
	contractOK(t, f.d.Write(ctx, func(tx *sql.Tx) error {
		_, e := tx.ExecContext(ctx, "UPDATE facts SET categories=? WHERE candidate=?", string(raw), id)
		return e
	}))
	got, err := f.s.GetFacts(ctx, id)
	contractOK(t, err)
	contractEqual(t, got.Categories, mixed)
	contractEqual(t, got.Records, []sources.Category{trucking})
	empty := categoryLinks(t, id)
	// A previous writer can append results without rewriting its facts row.
	contractOK(t, f.d.Write(ctx, func(tx *sql.Tx) error {
		_, e := tx.ExecContext(ctx, `INSERT INTO enrichments(candidate,kind,inputs,found,source,at,cost) VALUES (?,?,?,?,?,?,?)`, id, empty.Kind, empty.Inputs, string(empty.Found), empty.Source, f.now.UTC().Format(time.RFC3339), 0)
		return e
	}))
	got, err = f.s.GetFacts(ctx, id)
	contractOK(t, err)
	contractEqual(t, got.Categories, mixed)
	contractEqual(t, got.Records, []sources.Category{})
	f.reopen()
	got, err = f.s.GetFacts(ctx, id)
	contractOK(t, err)
	contractEqual(t, got.Records, []sources.Category{})
}

// R-BIB4-4RM6 R-O9NE-3VU4
func TestCohortClassifiesOnlyItsSelectedMembersAndReadsNewCategories(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	first := enrichmentCandidate(t, f, "for-hire", "office@first.test")
	private := enrichmentCandidate(t, f, "private", "office@private.test")
	classified := enrichmentCandidate(t, f, "classified", "office@classified.test")
	_, err := f.s.AddEnrichments(ctx, []store.Enrichment{categoryLinks(t, first, sources.Category{Name: sources.CategoryTrucking, Source: fmcsa.Name}), categoryLinks(t, private), categoryLinks(t, classified), briefResult(t, classified, "carrier", false, sources.CategoryTrucking)})
	contractOK(t, err)
	_, err = f.s.Judge(ctx, []string{first, private, classified})
	contractOK(t, err)
	k := cohortCreate(t, f, "category evidence")
	filters := store.CohortFilters{Categories: []string{sources.CategoryTrucking}}
	size := int64(100)
	k, err = f.s.EditCohort(ctx, store.CohortEdit{ID: k.ID, Size: &size, Filters: &filters, UserID: "owner"})
	contractOK(t, err)
	read := cohortRead(t, f, k)
	contractEqual(t, read.Matching, int64(2))
	contractEqual(t, read.Classified, int64(1))
	contractEqual(t, cohortIDs(read.Members), []string{first, classified})
	filters.CategoryUnknown = true
	k, err = f.s.EditCohort(ctx, store.CohortEdit{ID: k.ID, Filters: &filters, UserID: "owner"})
	contractOK(t, err)
	read = cohortRead(t, f, k)
	contractEqual(t, read.Matching, int64(3))
	contractEqual(t, read.Classified, int64(1))
	_, err = f.s.AddEnrichments(ctx, []store.Enrichment{briefResult(t, private, "builder", false, sources.CategoryConstruction)})
	contractOK(t, err)
	read = cohortRead(t, f, k)
	contractEqual(t, read.Matching, int64(2))
	contractEqual(t, read.Classified, int64(1))
	filters.CategoryUnknown = false
	size = 1
	k, err = f.s.EditCohort(ctx, store.CohortEdit{ID: k.ID, Filters: &filters, Size: &size, UserID: "owner"})
	contractOK(t, err)
	read = cohortRead(t, f, k)
	contractEqual(t, read.Matching, int64(2))
	contractEqual(t, read.Classified, int64(0))
	contractEqual(t, cohortIDs(read.Members), []string{first})
	// A newest good brief without categories leaves records in charge.
	_, err = f.s.AddEnrichments(ctx, []store.Enrichment{briefResult(t, first, "unclassified", false)})
	contractOK(t, err)
	read = cohortRead(t, f, k)
	contractEqual(t, read.Classified, int64(0))
	contractEqual(t, read.Matching, int64(2))
	// A nonempty good brief replaces a matching record category immediately.
	_, err = f.s.AddEnrichments(ctx, []store.Enrichment{briefResult(t, first, "retailer", false, sources.CategoryRetail)})
	contractOK(t, err)
	read = cohortRead(t, f, k)
	contractEqual(t, read.Matching, int64(1))
	contractEqual(t, read.Classified, int64(1))
	contractEqual(t, cohortIDs(read.Members), []string{classified})
	f.reopen()
	contractEqual(t, cohortRead(t, f, k), read)
}

// R-846Q-O0IX
func TestFillFactsReprojectsMissingCategoryRowsAndPreservesExistingRows(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	missing := contractSeed(t, f)
	existing := enrichmentCandidate(t, f, "existing-facts", "owner@existing.test")
	for _, id := range []string{missing, existing} {
		_, err := f.s.AddEnrichments(ctx, []store.Enrichment{categoryLinks(t, id, sources.Category{Name: sources.CategoryTrucking, Source: fmcsa.Name}), briefResult(t, id, "classification", false, sources.CategoryRetail), briefResult(t, id, "", true)})
		contractOK(t, err)
	}
	expected, err := f.s.GetFacts(ctx, missing)
	contractOK(t, err)
	// A prior writer's categories are preserved when a facts row already exists.
	old := []sources.Category{{Name: sources.CategoryConstruction, Source: ppp.Name}}
	raw, err := json.Marshal(old)
	contractOK(t, err)
	contractOK(t, f.d.Write(ctx, func(tx *sql.Tx) error {
		if _, e := tx.ExecContext(ctx, "DELETE FROM facts WHERE candidate=?", missing); e != nil {
			return e
		}
		_, e := tx.ExecContext(ctx, "UPDATE facts SET categories=? WHERE candidate=?", string(raw), existing)
		return e
	}))
	_, err = f.s.GetFacts(ctx, missing)
	contractError(t, err, store.ErrNotFound)
	before, err := f.s.GetFacts(ctx, existing)
	contractOK(t, err)
	contractEqual(t, before.Categories, old)
	histories := map[string][]store.Enrichment{}
	for _, id := range []string{missing, existing} {
		histories[id], err = f.s.Enrichments(ctx, id)
		contractOK(t, err)
	}
	for range 2 {
		contractOK(t, f.s.FillFacts(ctx))
		got, e := f.s.GetFacts(ctx, missing)
		contractOK(t, e)
		contractEqual(t, got, expected)
		got, e = f.s.GetFacts(ctx, existing)
		contractOK(t, e)
		contractEqual(t, got, before)
		for _, id := range []string{missing, existing} {
			es, e := f.s.Enrichments(ctx, id)
			contractOK(t, e)
			contractEqual(t, es, histories[id])
		}
		f.reopen()
	}
	got, err := f.s.GetFacts(ctx, missing)
	contractOK(t, err)
	contractEqual(t, got, expected)
	got, err = f.s.GetFacts(ctx, existing)
	contractOK(t, err)
	contractEqual(t, got, before)
}

// R-BIB4-4RM6
func TestCohortClassificationUsesBriefDecisionOrRecordFallback(t *testing.T) {
	for _, tc := range []struct {
		name, category, source string
		emptyBrief             bool
	}{
		{"empty brief keeps trucking", sources.CategoryTrucking, fmcsa.Name, true},
		{"brief replaces construction", sources.CategoryConstruction, ppp.Name, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := contractDB(t)
			ctx := context.Background()
			id := contractSeed(t, f)
			_, err := f.s.AddEnrichments(ctx, []store.Enrichment{categoryLinks(t, id, sources.Category{Name: tc.category, Source: tc.source})})
			contractOK(t, err)
			_, err = f.s.Judge(ctx, []string{id})
			contractOK(t, err)
			k := cohortCreate(t, f, "record fallback")
			filters := store.CohortFilters{Categories: []string{tc.category}}
			k, err = f.s.EditCohort(ctx, store.CohortEdit{ID: k.ID, Filters: &filters, UserID: "owner"})
			contractOK(t, err)
			read := cohortRead(t, f, k)
			contractEqual(t, cohortIDs(read.Members), []string{id})
			contractEqual(t, read.Matching, int64(1))
			contractEqual(t, read.Classified, int64(0))
			names := []string{sources.CategoryRetail}
			if tc.emptyBrief {
				names = nil
			}
			_, err = f.s.AddEnrichments(ctx, []store.Enrichment{briefResult(t, id, "classification", false, names...)})
			contractOK(t, err)
			read = cohortRead(t, f, k)
			contractEqual(t, read.Classified, int64(0))
			if tc.emptyBrief {
				contractEqual(t, cohortIDs(read.Members), []string{id})
				contractEqual(t, read.Matching, int64(1))
			} else {
				contractEqual(t, len(read.Members), 0)
				contractEqual(t, read.Matching, int64(0))
			}
		})
	}
}
