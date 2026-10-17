package store_test

import (
	"context"
	"database/sql"
	"runtime"
	"testing"

	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/website"
)

// R-OXV7-EIWU
func TestContractReadsDuringQueuedWrites(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	id := contractSeed(t, f)
	pass := contractNewPass(t, f, "read-while-write")
	cohort, err := f.s.CreateCohort(ctx, store.Cohort{Name: "read", Size: 100, CreatedUserID: "owner"})
	contractOK(t, err)
	reads := []struct {
		name string
		call func() (any, error)
	}{
		{"GetPass", func() (any, error) { return f.s.GetPass(ctx, pass.ID) }},
		{"FindDelivered", func() (any, error) { return f.s.FindDelivered(ctx, pass.Delivery, pass.Trigger) }},
		{"ListPasses", func() (any, error) { return f.s.ListPasses(ctx, "", "", 100) }},
		{"RunningPasses", func() (any, error) { return f.s.RunningPasses(ctx) }},
		{"GetCandidate", func() (any, error) { return f.s.GetCandidate(ctx, id) }},
		{"Links", func() (any, error) { return f.s.Links(ctx, id) }},
		{"Estimates", func() (any, error) { return f.s.Estimates(ctx, id) }},
		{"GetFacts", func() (any, error) { return f.s.GetFacts(ctx, id) }},
		{"GetAssessment", func() (any, error) { return f.s.GetAssessment(ctx, id) }},
		{"Rulings", func() (any, error) { return f.s.Rulings(ctx, id) }},
		{"ListCandidates", func() (any, error) { return f.s.ListCandidates(ctx, store.CandidateFilter{}, "", "", 100) }},
		{"ReadCohort", func() (any, error) { return f.s.ReadCohort(ctx, cohort.ID) }},
		{"GetCohort", func() (any, error) { return f.s.GetCohort(ctx, cohort.ID) }},
		{"ListCohorts", func() (any, error) { return f.s.ListCohorts(ctx, "", "", 100) }},
		{"ListCohortsReversed", func() (any, error) { return f.s.ListCohortsReversed(ctx, "", "", 100) }},
		{"Rejects", func() (any, error) { return f.s.Rejects(ctx, id) }},
		{"ZipMinutes", func() (any, error) { return f.s.ZipMinutes(ctx, "55072") }},
	}
	_, err = f.s.AddEnrichments(ctx, []store.Enrichment{{Candidate: id, Kind: sources.WebsiteCheck, Source: "read.test", Inputs: "read.test", Found: websiteFound(website.Result{State: sources.WebsiteHTTPS})}})
	contractOK(t, err)
	reads = append(reads, struct {
		name string
		call func() (any, error)
	}{"Enrichments", func() (any, error) { return f.s.Enrichments(ctx, id) }},
		struct {
			name string
			call func() (any, error)
		}{"DueEnrichments", func() (any, error) { return f.s.DueEnrichments(ctx, sources.WebsiteCheck, "", 100) }},
		struct {
			name string
			call func() (any, error)
		}{"EnrichmentMark", func() (any, error) { return f.s.EnrichmentMark(ctx) }},
		struct {
			name string
			call func() (any, error)
		}{"EnrichmentAfter", func() (any, error) { return f.s.EnrichmentAfter(ctx, sources.WebsiteCheck, "read.test", 0) }})

	p, err := f.s.AddPage(ctx, store.Page{Candidate: id, URL: "https://read.test/", Text: "held text", UserID: "owner"})
	contractOK(t, err)
	reads = append(reads, struct {
		name string
		call func() (any, error)
	}{"Pages", func() (any, error) { return f.s.Pages(ctx, id) }},
		struct {
			name string
			call func() (any, error)
		}{"GetPage", func() (any, error) { return f.s.GetPage(ctx, id, p.Number) }},
		struct {
			name string
			call func() (any, error)
		}{"CountPages", func() (any, error) { return f.s.CountPages(ctx, id) }},
		struct {
			name string
			call func() (any, error)
		}{"Subject", func() (any, error) { return f.s.Subject(ctx, id) }},
		struct {
			name string
			call func() (any, error)
		}{"DueAmong", func() (any, error) { return f.s.DueAmong(ctx, sources.WebsiteCheck, []string{id}, true) }})
	before := make([]any, len(reads))
	for i, read := range reads {
		value, err := read.call()
		contractOK(t, err)
		before[i] = value
	}
	opened, release := make(chan struct{}), make(chan struct{})
	held := make(chan error, 1)
	ended := make(chan struct{})
	go func() {
		defer close(ended)
		held <- f.d.Write(ctx, func(tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, "UPDATE candidates SET name=? WHERE id=?", "Uncommitted display", id); err != nil {
				return err
			}
			close(opened)
			<-release
			return nil
		})
	}()
	<-opened
	var releaseOnce bool
	unblock := func() {
		if !releaseOnce {
			close(release)
			releaseOnce = true
		}
	}
	defer unblock()
	writes := []func() error{
		func() error {
			obs := contractObservation("evidence", "Changed queued display", f.now)
			obs.Digest = "queued-digest"
			_, _, err := f.s.Attach(ctx, "queued-run", false, []store.Observation{obs})
			return err
		},
		func() error {
			_, _, err := f.s.Decide(ctx, store.Ruling{Kind: store.KindExclude, Candidate: id, Why: "queued ruling", UserID: "owner"})
			return err
		},
		func() error { _, err := f.s.Judge(ctx, []string{id}); return err },
		func() error { return f.s.FillFacts(ctx) },
		func() error { _, _, err := f.s.Derive(ctx, "link_facts", "", false, 10); return err },
		func() error {
			_, err := f.s.AddEnrichments(ctx, []store.Enrichment{{Candidate: id, Kind: sources.WebsiteCheck, Inputs: "fixture.test", Source: "fixture.test", Found: websiteFound(website.Result{State: sources.WebsiteNone})}})
			return err
		},
	}
	results := make(chan error, len(writes))
	begun := make(chan struct{}, len(writes))
	for _, write := range writes {
		go func() { begun <- struct{}{}; results <- write() }()
	}
	defer func() {
		unblock()
		contractOK(t, <-held)
		for range writes {
			contractOK(t, <-results)
		}
	}()
	for range writes {
		<-begun
	}
	// Yielding makes it likely that the waiting writers have entered their calls;
	// the contract deliberately provides no hook for that point.
	for range 32 {
		runtime.Gosched()
		for i, read := range reads {
			value, err := read.call()
			contractOK(t, err)
			contractEqual(t, value, before[i])
			select {
			case <-ended:
				t.Fatalf("transaction ended before %s", read.name)
			default:
			}
			runtime.Gosched()
		}
	}
}
