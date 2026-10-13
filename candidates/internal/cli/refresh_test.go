package cli_test

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/scoring"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// R-4QDC-DHRV
func TestStartupRefreshesBeforeReady(t *testing.T) {
	t0 := time.Date(2021, 6, 1, 0, 0, 0, 0, time.UTC)
	t1 := time.Date(2023, 7, 3, 0, 0, 0, 987654321, time.UTC)
	dir := t.TempDir()
	d, st := runStore(t, dir, func() time.Time { return t0 })
	filed := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	_, ids, err := st.Attach(context.Background(), "seed", false, []store.Observation{{Scraper: "fmcsa", SourceID: "seed-row", Digest: "seed-digest", Reading: sources.Reading{Name: "Startup business", NameKey: "STARTUPBUSINESS", Fleet: true, Filed: filed, Estimate: &sources.Estimate{Source: "fmcsa", Low: 2000000, Confidence: 0.8, Filed: filed}}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = st.Rescore(context.Background(), ids); err != nil {
		t.Fatal(err)
	}
	initial, err := st.GetAssessment(context.Background(), ids[0])
	if err != nil {
		t.Fatal(err)
	}
	if math.Abs(initial.Effective-0.8) > 1e-9 {
		t.Fatal(initial)
	}
	if err = d.Close(); err != nil {
		t.Fatal(err)
	}
	h := newRunHarness(t, dir, "")
	h.p.Now = func() time.Time { return t1 }
	h.start()
	// No HTTP request has been sent: readiness alone promises persisted freshness.
	_, fresh := runStore(t, dir, h.p.Now)
	got, err := fresh.GetAssessment(context.Background(), ids[0])
	if err != nil {
		t.Fatal(err)
	}
	effective := scoring.Effective(got.Confidence, got.Filed, t1.UTC().Truncate(time.Second))
	score := scoring.Score(scoring.Point(got.Low, got.High), effective)
	if math.Abs(got.Effective-0.4) > 1e-9 || math.Abs(got.Effective-effective) > 1e-9 || math.Abs(got.Score-score) > 1e-9 || got.ScoreKey != int64(math.Round(score*1e6)) {
		t.Fatal(got)
	}
	h.stop()
}
