package passes_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/fmcsa"
	"github.com/ikigenba/ikigenba/candidates/internal/passes"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/website"
)

// R-SVWB-0INK R-01D8-ZAZ4 R-ZQE5-JDAV R-1QFF-85G4 R-3WDS-L5IA R-UCBZ-GNJF
func TestDerivedKindsAndIdempotentAll(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"", enrichers.KindDriveMinutes, enrichers.KindLinkFacts} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t, nil, false)
			ctx := context.Background()
			_, ids, err := f.st.Attach(ctx, "seed", false, []store.Observation{
				{Scraper: "fmcsa", SourceID: "first", Digest: "one", Reading: sources.Reading{Name: "first", Zip: "55401"}},
				{Scraper: "fmcsa", SourceID: "second", Digest: "two", Reading: sources.Reading{Name: "second", Zip: "55402"}},
				{Scraper: "fmcsa", SourceID: "third", Digest: "three", Reading: sources.Reading{Name: "third"}},
			})
			if err != nil {
				t.Fatal(err)
			}
			// Prepare two candidates with current results; the first alone remains due.
			var dueID string
			for _, id := range ids {
				c, err := f.st.GetCandidate(ctx, id)
				if err != nil {
					t.Fatal(err)
				}
				if c.Name == "first" {
					dueID = id
					continue
				}
				for _, k := range []string{enrichers.KindDriveMinutes, enrichers.KindLinkFacts} {
					// A one-candidate keyset window records precisely this candidate.
					after := ""
					cs, err := f.st.ListCandidates(ctx, store.CandidateFilter{}, "", "", 100)
					if err != nil {
						t.Fatal(err)
					}
					for _, c := range cs {
						if c.ID < id && c.ID > after {
							after = c.ID
						}
					}
					_, _, err = f.st.Derive(ctx, k, after, false, 1)
					if err != nil {
						t.Fatal(err)
					}
				}
			}
			if err := f.st.ReplaceZipDrive(ctx, map[string]int64{"55401": 84}); err != nil {
				t.Fatal(err)
			}
			before := map[string]int{}
			for _, id := range ids {
				es, err := f.st.Enrichments(ctx, id)
				if err != nil {
					t.Fatal(err)
				}
				before[id] = len(es)
			}
			p := f.enrich(kind)
			want := int64(1)
			if kind == "" {
				want = 2
			}
			if p.Status != store.StatusDone || p.Derived != want || p.Due+p.Fetched+p.HTTPS+p.HTTP+p.None != 0 {
				t.Fatal(p)
			}
			for _, id := range ids {
				es, err := f.st.Enrichments(ctx, id)
				if err != nil {
					t.Fatal(err)
				}
				n := 0
				for _, en := range es[:len(es)-before[id]] {
					n++
					c, err := f.st.GetCandidate(ctx, id)
					if err != nil {
						t.Fatal(err)
					}
					subject := enrichers.Subject{ID: c.ID, Email: c.Email, Zip: c.Zip}
					if m, err := f.st.ZipMinutes(ctx, c.Zip); err == nil {
						subject.Minutes = &m
					}
					links, err := f.st.Links(ctx, id)
					if err != nil {
						t.Fatal(err)
					}
					for _, l := range links {
						subject.Links = append(subject.Links, enrichers.Link{Scraper: l.Scraper, SourceID: l.SourceID, Gone: l.Gone, Reading: l.Reading})
					}
					regEn, _ := enrichers.New(enrichers.Config{}).Get(en.Kind)
					deriver := regEn.(enrichers.Deriver)
					expected := deriver.Derive(subject)
					if en.Inputs != deriver.Inputs(subject) || en.Source != expected.Source || en.Cost != expected.Cost || !bytes.Equal(en.Found, expected.Found) {
						t.Fatal(en, expected)
					}
					if kind != "" && en.Kind != kind {
						t.Fatal(en)
					}
					if en.Kind == sources.WebsiteCheck || en.At != p.Started {
						t.Fatal(en)
					}
					if en.Kind == enrichers.KindDriveMinutes {
						var found enrichers.MinutesFound
						if err := json.Unmarshal(en.Found, &found); err != nil || en.Inputs != "55401" || en.Source != enrichers.SourceZipDrive || en.Cost != 0 {
							t.Fatal(en, err)
						}
					}
					if en.Kind == enrichers.KindLinkFacts {
						if en.Inputs == "" || en.Source != enrichers.SourceLinks || en.Cost != 0 {
							t.Fatal(en)
						}
					}
				}
				if id == dueID && n != int(want) || id != dueID && n != 0 {
					t.Fatal(id, n)
				}
			}
			j := waitFollow(t, f, p.ID)
			if j.Kind != store.KindJudge || j.Judged != 3 {
				t.Fatal(j)
			}
			p = f.enrich(kind)
			if p.Derived+p.Due+p.Fetched+p.HTTPS+p.HTTP+p.None != 0 {
				t.Fatal(p)
			}
			for _, v := range f.list() {
				if v.Trigger == p.ID {
					t.Fatal("empty enrich started follow-on", v)
				}
			}
			if len(f.requests()) != 0 {
				t.Fatal(f.requests())
			}
		})
	}
}

// R-SZK0-5TVN R-01D8-ZAZ4 R-ZQE5-JDAV R-1QFF-85G4
func TestDerivedFactsExistBeforeFirstFetch(t *testing.T) {
	t.Parallel()
	f := newFixture(t, nil, false)
	ids := seedEmails(t, f, 1, func(int) string { return "owner@derived.test" })
	held, release := make(chan struct{}), make(chan struct{})
	f.checker(func(r *http.Request) (*http.Response, error) {
		close(held)
		select {
		case <-release:
		case <-r.Context().Done():
			return nil, r.Context().Err()
		}
		return pageResponse(r), nil
	})
	p, err := f.eng.Enrich(context.Background(), identity.Caller{UserID: "owner"}, "")
	if err != nil {
		t.Fatal(err)
	}
	waitSignal(t, held)
	es, err := f.st.Enrichments(context.Background(), ids[0])
	if err != nil || len(es) != 1 || es[0].Kind != enrichers.KindLinkFacts {
		t.Fatal(es, err)
	}
	facts, err := f.st.GetFacts(context.Background(), ids[0])
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, k := range facts.Written {
		if k.Kind == enrichers.KindLinkFacts {
			found = true
		}
	}
	if !found {
		t.Fatal(facts)
	}
	running, err := f.st.GetPass(context.Background(), p.ID)
	if err != nil || running.Status != store.StatusRunning {
		t.Fatal(running, err)
	}
	close(release)
	p = f.ended(p.ID)
	if p.Derived != 1 || p.HTTPS != 1 {
		t.Fatal(p)
	}
	waitFollow(t, f, p.ID)
	second := f.enrich("")
	if second.Derived+second.Due+second.Fetched+second.HTTPS+second.HTTP+second.None != 0 {
		t.Fatal(second)
	}
	for _, v := range f.list() {
		if v.Trigger == second.ID {
			t.Fatal(v)
		}
	}
}

// R-JWI9-6EKI
func TestDerivedKindsAvailableWithoutChecker(t *testing.T) {
	for _, kind := range []string{"", enrichers.KindDriveMinutes, enrichers.KindLinkFacts} {
		f := newFixture(t, nil, false)
		blocker, err := f.st.AddPass(context.Background(), store.Pass{Kind: store.KindIngest, Origin: store.OriginIngest, Trigger: store.TriggerIngest, UserID: "owner"})
		if err != nil {
			t.Fatal(err)
		}
		p, err := f.eng.Enrich(context.Background(), identity.Caller{UserID: "owner"}, kind)
		if err != nil {
			t.Fatal(kind, err)
		}
		p = f.ended(p.ID)
		if p.Status != store.StatusDone {
			t.Fatal(p)
		}
		running, err := f.st.GetPass(context.Background(), blocker.ID)
		if err != nil || running.Status != store.StatusRunning {
			t.Fatal(running, err)
		}
		f.eng.Drain(context.Background())
	}
	var _ interface {
		Enrich(context.Context, identity.Caller, string) (store.Pass, error)
	} = (*passes.Engine)(nil)
}

// R-XYZD-NAQR
func TestWebsiteInputsChangeRecordsAnotherResult(t *testing.T) {
	f := newFixture(t, nil, false)
	ids := seedEmails(t, f, 1, func(int) string { return "owner@a.test" })
	f.checker(func(r *http.Request) (*http.Response, error) { return pageResponse(r), nil })
	first := f.enrich(sources.WebsiteCheck)
	waitFollow(t, f, first.ID)
	_, _, err := f.st.Attach(context.Background(), "changed", false, []store.Observation{{Scraper: "fmcsa", SourceID: "0", Digest: "changed", Reading: sources.Reading{Name: "Business 0", Email: "owner@b.test"}}})
	if err != nil {
		t.Fatal(err)
	}
	second := f.enrich(sources.WebsiteCheck)
	waitFollow(t, f, second.ID)
	es, err := f.st.Enrichments(context.Background(), ids[0])
	if err != nil || len(es) != 2 || es[0].Inputs != "b.test" || es[0].Source != "b.test" || es[1].Inputs != "a.test" || es[1].Source != "a.test" {
		t.Fatal(es, err)
	}
}

// R-TXP6-VEN3 R-TQDS-KS6X R-TSTL-CBOB R-3QEZ-TBSF R-UCBZ-GNJF
func TestFullIngestForcesDerivedResultsBeforeJudging(t *testing.T) {
	t.Parallel()
	f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/scrapers/fmcsa/current" {
			record(w, "forced-run", 2, store.StatusDone)
			return
		}
		for i := 0; i < 2; i++ {
			zip := ""
			if i == 0 {
				zip = "55401"
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"scraper": "fmcsa", "run": "forced-run", "source_id": string(rune('a' + i)), "digest": "same", "name": string(rune('a' + i)), "scraped": "2000-01-01T00:00:00Z", "names": []string{}, "people": []any{}, "facts": map[string]any{}, "raw": map[string]any{}, "address": map[string]any{"state": "MN", "zip": zip}})
		}
	}, true)
	pc, fake := fakePrompts(t, f, promptAnswer)
	f.eng = passes.New(passes.Config{Store: f.st, Sources: sources.New(fmcsa.New()), Scrapes: f.sc, Prompts: pc, Telemetry: f.w, Events: f.em, Website: &website.Checker{Client: &http.Client{Transport: enrichTransport(func(r *http.Request) (*http.Response, error) {
		t.Error("fullIngest fetched")
		return pageResponse(r), nil
	})}}})
	if err := f.st.ReplaceZipDrive(context.Background(), map[string]int64{"55401": 84}); err != nil {
		t.Fatal(err)
	}
	for attempt := 1; attempt <= 2; attempt++ {
		p, err := f.eng.Ingest(context.Background(), identity.Caller{UserID: "owner"}, "")
		if err != nil {
			t.Fatal(err)
		}
		p = f.ended(p.ID)
		if p.Status != store.StatusDone || p.Derived != 3 || p.Judged != 2 || p.Due+p.Fetched+p.HTTPS+p.HTTP+p.None+p.Runs+p.RunsFailed+p.Pages+p.Cost != 0 || len(fake.snapshot()) != 0 {
			t.Fatal(p, fake.snapshot())
		}
		cs, err := f.st.ListCandidates(context.Background(), store.CandidateFilter{}, "", "", 10)
		if err != nil {
			t.Fatal(err)
		}
		for _, c := range cs {
			es, err := f.st.Enrichments(context.Background(), c.ID)
			if err != nil {
				t.Fatal(err)
			}
			want := attempt
			if c.Zip != "" {
				want *= 2
			}
			if len(es) != want {
				t.Fatal(c, es)
			}
			for _, en := range es {
				if en.Kind == sources.WebsiteCheck || en.Kind == enrichers.KindSiteText || en.Kind == enrichers.KindBrief {
					t.Fatal(en)
				}
			}
			facts, err := f.st.GetFacts(context.Background(), c.ID)
			if err != nil || facts.Candidate != c.ID {
				t.Fatal(facts, err)
			}
			a, err := f.st.GetJudgments(context.Background(), c.ID)
			if err != nil || a.Calculated.Before(p.Started) || a.Calculated.After(p.Finished) {
				t.Fatal(a, err)
			}

		}
		for _, v := range f.list() {
			if v.Trigger == p.ID {
				t.Fatal("fullIngest started follow-on", v)
			}
		}
	}
}
