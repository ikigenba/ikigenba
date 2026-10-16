package store_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// R-GB90-C1SV R-IXLY-M7WD
func TestEnrichPassShapeAndVocabulary(t *testing.T) {
	contractEqual(t, []string{store.KindEnrich, store.OriginEnrich, store.OriginPass, store.TriggerEnrich}, []string{"enrich", "enrich", "pass", "enrich"})
	p := store.Pass{ID: "pass", Kind: store.KindEnrich, Origin: store.OriginPass, Trigger: "parent", Status: store.StatusDone, Reason: "reason", Started: time.Time{}, Finished: time.Time{}, Rows: 1, New: 2, Changed: 3, Unchanged: 4, Gone: 5, Created: 6, Linked: 7, Judged: 8, Unranged: 9, Due: 10, Fetched: 11, HTTPS: 12, HTTP: 13, None: 14, UserID: "user", UserEmail: "email", RequestID: "request", Delivery: "delivery"}
	shape := struct {
		ID, Kind, Origin, Trigger, Status, Reason                                                                        string
		Started, Finished                                                                                                time.Time
		Rows, New, Changed, Unchanged, Gone, Created, Linked, Judged, Unranged, Derived, Due, Fetched, HTTPS, HTTP, None int64
		UserID, UserEmail, RequestID, Delivery                                                                           string
	}(p)
	contractEqual(t, store.Pass(shape), p)
}

func enrichPass() store.Pass {
	return store.Pass{Kind: store.KindEnrich, Origin: store.OriginEnrich, Trigger: store.TriggerEnrich, UserID: "owner", UserEmail: "owner@example.test", RequestID: "request"}
}

// R-ILEY-SIHF R-GIKE-MO91 R-IP2N-XTPI
func TestPassLanesAndOrigins(t *testing.T) {
	ctx := context.Background()
	parent := store.PassIDPrefix + "0123456789abcdef"
	cases := []store.Pass{
		{Kind: store.KindRebuild, Origin: store.OriginRebuild, Trigger: store.TriggerRebuild, UserID: "owner"},
		{Kind: store.KindIngest, Origin: store.OriginRun, Trigger: "run", Delivery: "ingest-delivery"},
		{Kind: store.KindJudge, Origin: store.OriginCron, Trigger: store.TriggerJudge, Delivery: "judge-delivery"},
		{Kind: store.KindJudge, Origin: store.OriginPass, Trigger: parent},
	}
	for _, ordinary := range cases {
		for _, firstEnrich := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/%s/enrich_first_%t", ordinary.Kind, ordinary.Origin, firstEnrich), func(t *testing.T) {
				f := contractDB(t)
				a, b := ordinary, enrichPass()
				if firstEnrich {
					a, b = b, a
				}
				one, err := f.s.AddPass(ctx, a)
				contractOK(t, err)
				two, err := f.s.AddPass(ctx, b)
				contractOK(t, err)
				if one.ID == two.ID {
					t.Fatal("duplicate pass id")
				}
				running, err := f.s.RunningPasses(ctx)
				contractOK(t, err)
				contractEqual(t, len(running), 2)
				for _, q := range []store.Pass{enrichPass(), {Kind: store.KindEnrich, Origin: store.OriginPass, Trigger: parent}} {
					out, e := f.s.AddPass(ctx, q)
					contractError(t, e, store.ErrBusy)
					contractEqual(t, out, store.Pass{})
				}
				f.reopen()
				for _, p := range []store.Pass{one, two} {
					got, e := f.s.GetPass(ctx, p.ID)
					contractOK(t, e)
					contractEqual(t, got, p)
				}
			})
		}
	}
	f := contractDB(t)
	for _, kind := range []string{store.KindEnrich, store.KindJudge} {
		p, err := f.s.AddPass(ctx, store.Pass{Kind: kind, Origin: store.OriginPass, Trigger: parent})
		contractOK(t, err)
		_, err = f.s.FinishPass(ctx, p.ID, store.Counts{})
		contractOK(t, err)
	}
	// A completed cron judgment does not deduplicate a later cron delivery.
	p, err := f.s.AddPass(ctx, cases[2])
	contractOK(t, err)
	_, err = f.s.FinishPass(ctx, p.ID, store.Counts{})
	contractOK(t, err)
	q := cases[2]
	q.Delivery = "later-delivery"
	_, err = f.s.AddPass(ctx, q)
	contractOK(t, err)
}

// R-ILEY-SIHF
func TestInvalidEnrichAndFollowOnPasses(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	parent := store.PassIDPrefix + "0123456789abcdef"
	bad := []store.Pass{
		{Kind: store.KindEnrich, Origin: store.OriginEnrich, Trigger: store.TriggerEnrich},
		{Kind: store.KindEnrich, Origin: store.OriginEnrich, Trigger: "other", UserID: "owner"},
		{Kind: store.KindEnrich, Origin: store.OriginEnrich, Trigger: store.TriggerEnrich, UserID: "owner", Delivery: "delivery"},
		{Kind: store.KindEnrich, Origin: store.OriginPass, Trigger: "bad"},
		{Kind: store.KindEnrich, Origin: store.OriginPass, Trigger: parent, Delivery: "delivery"},
		{Kind: store.KindJudge, Origin: store.OriginPass, Trigger: "bad"},
		{Kind: store.KindJudge, Origin: store.OriginPass, Trigger: parent, Delivery: "delivery"},
		{Kind: store.KindIngest, Origin: store.OriginPass, Trigger: parent},
	}
	for _, p := range bad {
		out, err := f.s.AddPass(ctx, p)
		contractError(t, err, store.ErrUnreachable)
		contractEqual(t, out, store.Pass{})
	}
	ps, err := f.s.ListPasses(ctx, "", "", 10)
	contractOK(t, err)
	contractEqual(t, len(ps), 0)
}

// R-IQAK-BLG7
func TestConcurrentPassLanes(t *testing.T) {
	ctx := context.Background()
	for _, sameLane := range []bool{true, false} {
		t.Run(fmt.Sprint(sameLane), func(t *testing.T) {
			f := contractDB(t)
			start := make(chan struct{})
			results := make(chan error, 8)
			var wg sync.WaitGroup
			count := 8
			if !sameLane {
				count = 2
			}
			for i := range count {
				wg.Go(func() {
					<-start
					p := enrichPass()
					if !sameLane && i == 1 {
						p = store.Pass{Kind: store.KindRebuild, Origin: store.OriginRebuild, Trigger: store.TriggerRebuild, UserID: "owner"}
					}
					_, err := f.s.AddPass(ctx, p)
					results <- err
				})
			}
			close(start)
			wg.Wait()
			close(results)
			success := 0
			for err := range results {
				if err == nil {
					success++
				} else {
					contractError(t, err, store.ErrBusy)
				}
			}
			want := 1
			if !sameLane {
				want = 2
			}
			contractEqual(t, success, want)
		})
	}
}
