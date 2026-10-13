package passes_test

import (
	"context"
	"math"
	"net/http"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/candidates/internal/fmcsa"
	"github.com/ikigenba/ikigenba/candidates/internal/passes"
	"github.com/ikigenba/ikigenba/candidates/internal/scoring"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// R-CDS3-7YWU R-9VR5-OQ9D
func TestEveryPassEndingRefreshesStoredScores(t *testing.T) {
	for _, mode := range []string{"nil_client", "record_failure", "rows_failure", "interrupted", "successful_delivery"} {
		t.Run(mode, func(t *testing.T) {
			entered := make(chan struct{})
			f := newFixture(t, func(w http.ResponseWriter, r *http.Request) {
				if mode == "record_failure" {
					w.WriteHeader(500)
					return
				}
				if r.URL.Path == "/scrapers/fmcsa/current" || r.URL.Path == "/runs/run_a/record" {
					record(w, "run_a", 0, store.StatusDone)
					return
				}
				if mode == "interrupted" {
					close(entered)
					<-r.Context().Done()
					return
				}
				if mode != "successful_delivery" {
					w.WriteHeader(500)
				}
			}, mode != "nil_client")
			f.eng.Drain(context.Background())
			now := time.Date(2021, 6, 1, 0, 0, 0, 0, time.UTC)
			f.st = store.New(f.d, store.Config{Now: func() time.Time { return now }, Rand: &random{}})
			f.eng = passes.New(passes.Config{Store: f.st, Sources: sources.New(fmcsa.New()), Scrapes: f.sc, Telemetry: f.w, Events: f.em})
			filed := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
			_, ids, err := f.st.Attach(context.Background(), "seed", false, []store.Observation{{Scraper: "unrelated-source", SourceID: "seed-row", Digest: "seed", Reading: sources.Reading{Name: "Aging fleet", NameKey: "AGINGFLEET", Zip: "55072", Fleet: true, Filed: filed, Estimate: &sources.Estimate{Source: "fmcsa", Low: 20000000, Confidence: 0.8, Filed: filed}}}})
			if err != nil {
				t.Fatal(err)
			}
			if err = f.st.ReplaceZipDrive(context.Background(), map[string]int64{"55072": 1}); err != nil {
				t.Fatal(err)
			}
			if _, err = f.st.Rescore(context.Background(), ids); err != nil {
				t.Fatal(err)
			}
			initial, err := f.st.GetAssessment(context.Background(), ids[0])
			if err != nil || math.Abs(initial.Effective-0.8) > 1e-9 {
				t.Fatal(initial, err)
			}
			now = time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
			var p store.Pass
			if mode == "nil_client" || mode == "record_failure" || mode == "successful_delivery" {
				if out := f.delivery("refresh-event", "run_a"); out != events.OK() {
					t.Fatal(out)
				}
				p = f.list()[0]
			} else {
				p, err = f.eng.Rebuild(context.Background(), identity.Caller{UserID: "owner"})
				if err != nil {
					t.Fatal(err)
				}
				if mode == "interrupted" {
					waitSignal(t, entered)
					f.eng.Drain(context.Background())
				}
			}
			ended := f.ended(p.ID)
			reason := store.ReasonScrapes
			if mode == "interrupted" {
				reason = store.ReasonInterrupted
			}
			status := store.StatusFailed
			if mode == "successful_delivery" {
				status, reason = store.StatusDone, ""
			}
			if ended.Status != status || ended.Reason != reason {
				t.Fatal(ended)
			}
			a, err := f.st.GetAssessment(context.Background(), ids[0])
			if err != nil {
				t.Fatal(err)
			}
			effective := scoring.Effective(a.Confidence, a.Filed, now)
			score := scoring.Score(scoring.Point(a.Low, a.High), effective)
			if a.Effective != effective || a.Score != score || a.ScoreKey != int64(math.Round(score*1e6)) || a.Effective != 0 {
				t.Fatal(a)
			}
			ranked, err := f.st.Shortlist(context.Background(), store.ShortlistFilter{MaxMinutes: 120, MinRevenue: 10000000}, 0, "", 10)
			if err != nil || len(ranked) != 0 {
				t.Fatal(ranked, err)
			}
		})
	}
}
