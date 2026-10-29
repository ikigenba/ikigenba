package passes_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/fmcsa"
	"github.com/ikigenba/ikigenba/candidates/internal/passes"
	"github.com/ikigenba/ikigenba/candidates/internal/places"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// R-FLA3-7ARP R-SODA-05AS R-EOCS-VHQY R-EPKP-99HN
func TestFullIngestRebuildsEveryPageBeforeDerivingAndStaysStable(t *testing.T) {
	t.Parallel()
	for _, advance := range []bool{false, true} {
		t.Run(fmt.Sprint(advance), func(t *testing.T) {
			f := newFixture(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) }, true)
			ctx := context.Background()
			var seconds atomic.Int64
			seconds.Store(100000)
			f.st = store.New(f.d, store.Config{Now: func() time.Time { return time.Unix(seconds.Load(), 0) }, Rand: &random{}})
			f.eng = passes.New(passes.Config{Store: f.st, Sources: sources.New(fmcsa.New()), Scrapes: f.sc, Telemetry: f.w, Events: f.em})
			ks := seedWalkParks(t, f, 1)
			walk, err := f.st.AddPass(ctx, store.Pass{Kind: store.KindWalk, Origin: store.OriginWalk, Trigger: store.TriggerWalk, UserID: "owner"})
			if err != nil {
				t.Fatal(err)
			}
			place := places.Place{ID: "rebuild-place", Name: "Place business", Address: "12 Fixture St, Fixture, MN 55072", Phone: "3205550123", PrimaryType: "store", Website: "https://place.test"}
			if _, err = f.st.RecordSweep(ctx, store.Sweep{Pass: walk.ID, Park: ks[0].ID, Found: []places.Found{{Place: place}}, Circles: 1, Requests: 1}); err != nil {
				t.Fatal(err)
			}
			if _, err = f.st.FinishWalk(ctx, walk.ID, store.Counts{}, false); err != nil {
				t.Fatal(err)
			}
			observations := make([]store.Observation, passes.BatchSize+1)
			for i := range observations {
				observations[i] = store.Observation{Scraper: fmcsa.Name, SourceID: fmt.Sprint(i), Digest: fmt.Sprint(i), Reading: sources.Reading{Name: fmt.Sprintf("Unvisited business %d", i), Zip: "55072"}}
			}
			if _, _, err = f.st.Attach(ctx, "not-current", false, observations); err != nil {
				t.Fatal(err)
			}
			// Stand in for rows written before the current display/reading rules.
			stale, err := json.Marshal(sources.Reading{Name: "Old place reading", Zip: "99999"})
			if err != nil {
				t.Fatal(err)
			}
			if err = f.d.Write(ctx, func(tx *sql.Tx) error {
				if _, err := tx.ExecContext(ctx, "UPDATE links SET reading=? WHERE scraper=? AND source_id=?", string(stale), places.Name, place.ID); err != nil {
					return err
				}
				_, err := tx.ExecContext(ctx, "UPDATE candidates SET name=?,zip=?", "Stale display", "99999")
				return err
			}); err != nil {
				t.Fatal(err)
			}
			if err = f.st.ReplaceZipDrive(ctx, map[string]int64{"55072": 7, "99999": 999}); err != nil {
				t.Fatal(err)
			}
			run := func() store.Pass {
				p, err := f.eng.Ingest(ctx, identity.Caller{UserID: "owner"}, "")
				if err != nil {
					t.Fatal(err)
				}
				p = f.ended(p.ID)
				if p.Status != store.StatusDone || p.Rows != 0 || p.Judged != int64(len(observations)+1) || p.Derived != 2*p.Judged {
					t.Fatal(p)
				}
				return p
			}
			first := run()
			cs, err := f.st.ListCandidates(ctx, store.CandidateFilter{}, "", "", len(observations)+1)
			if err != nil || len(cs) != len(observations)+1 {
				t.Fatal(cs, err)
			}
			type snapshot struct {
				Candidate store.Candidate
				Links     []store.Link
				Facts     store.Facts
				Judgments store.Judgments
				Estimates []sources.Estimate
			}
			read := func(c store.Candidate) snapshot {
				current, err := f.st.GetCandidate(ctx, c.ID)
				if err != nil {
					t.Fatal(err)
				}
				links, err := f.st.Links(ctx, c.ID)
				if err != nil {
					t.Fatal(err)
				}
				facts, err := f.st.GetFacts(ctx, c.ID)
				if err != nil {
					t.Fatal(err)
				}
				judgments, err := f.st.GetJudgments(ctx, c.ID)
				if err != nil {
					t.Fatal(err)
				}
				estimates, err := f.st.Estimates(ctx, c.ID)
				if err != nil {
					t.Fatal(err)
				}
				return snapshot{current, links, facts, judgments, estimates}
			}
			before := make([]snapshot, len(cs))
			for i, c := range cs {
				before[i] = read(c)
				if len(before[i].Links) != 1 {
					t.Fatal(before[i].Links)
				}
				link := before[i].Links[0]
				wantName := place.Name
				if link.Scraper == fmcsa.Name {
					wantName = ""
					for _, observation := range observations {
						if observation.SourceID == link.SourceID {
							wantName = observation.Reading.Name
						}
					}
				}
				if wantName == "" || c.Name != wantName || c.Zip != "55072" {
					t.Fatal(c)
				}
				for _, link := range before[i].Links {
					if link.Scraper == places.Name && !reflect.DeepEqual(link.Reading, places.Reading(place)) {
						t.Fatal(link)
					}
				}
				es, err := f.st.Enrichments(ctx, c.ID)
				if err != nil || len(es) != 2 {
					t.Fatal(es, err)
				}
				for _, en := range es {
					if en.Kind == enrichers.KindDriveMinutes && en.Inputs != c.Zip {
						t.Fatal("derived before display rebuild", c, en)
					}
				}
			}
			if advance {
				seconds.Add(1)
			}
			second := run()
			for i, c := range cs {
				after := read(c)
				if advance {
					if !reflect.DeepEqual(before[i].Candidate, after.Candidate) {
						t.Fatal("repeat changed candidate", before[i].Candidate, after.Candidate)
					}
				} else if !reflect.DeepEqual(before[i], after) {
					t.Fatal("repeat changed stable state", before[i], after)
				}
			}
			for _, p := range f.list() {
				if p.Trigger == first.ID || p.Trigger == second.ID {
					t.Fatal("full ingest started follow-on", p)
				}
			}
		})
	}
}
