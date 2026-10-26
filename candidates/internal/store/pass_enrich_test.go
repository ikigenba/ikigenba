package store_test

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// R-VRPW-S7JM R-IXLY-M7WD R-ZYXG-7RHQ
func TestEnrichPassShapeAndVocabulary(t *testing.T) {
	contractEqual(t, []string{store.KindEnrich, store.OriginEnrich, store.OriginPass, store.TriggerEnrich}, []string{"enrich", "enrich", "pass", "enrich"})
	p := store.Pass{ID: "pass", Kind: store.KindEnrich, Origin: store.OriginPass, Trigger: "parent", Status: store.StatusDone, Reason: "reason", Started: time.Time{}, Finished: time.Time{}, Rows: 1, New: 2, Changed: 3, Unchanged: 4, Gone: 5, Created: 6, Linked: 7, Judged: 8, Unranged: 9, Due: 10, Fetched: 11, HTTPS: 12, HTTP: 13, None: 14, UserID: "user", UserEmail: "email", RequestID: "request", Delivery: "delivery"}
	shape := struct {
		ID, Kind, Origin, Trigger, Target, EnrichKind, Scraper, Status, Reason                                                                          string
		Started, Finished                                                                                                                               time.Time
		Rows, New, Changed, Unchanged, Gone, Created, Linked, Judged, Unranged, Derived, Due, Fetched, HTTPS, HTTP, None, Runs, RunsFailed, Pages, Cost int64
		UserID, UserEmail, RequestID, Delivery                                                                                                          string
	}(p)
	contractEqual(t, store.Pass(shape), p)
	contractEqual(t, store.KindEnrichTarget, "enrich_target")
	contractEqual(t, store.ReasonPrompts, "prompts")
	var capValue = store.PageCap
	contractEqual(t, capValue, 20)
}

func enrichPass() store.Pass {
	return store.Pass{Kind: store.KindEnrich, Origin: store.OriginEnrich, Trigger: store.TriggerEnrich, UserID: "owner", UserEmail: "owner@example.test", RequestID: "request"}
}

// R-2760-QOWB R-2ATP-W04E R-2EHF-1BCH
func TestPassLanesAndOrigins(t *testing.T) {
	ctx := context.Background()
	parent := store.PassIDPrefix + "0123456789abcdef"
	cases := []store.Pass{
		{Kind: store.KindIngest, Origin: store.OriginIngest, Trigger: store.TriggerIngest, UserID: "owner"},
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

// R-2760-QOWB R-4SM4-Q4EK
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
	for _, ordinary := range []store.Pass{enrichPass(), {Kind: store.KindIngest, Origin: store.OriginIngest, Trigger: store.TriggerIngest, UserID: "owner"}, {Kind: store.KindIngest, Origin: store.OriginRun, Trigger: "run", Delivery: "event"}, {Kind: store.KindJudge, Origin: store.OriginCron, Trigger: store.TriggerJudge, Delivery: "event"}, {Kind: store.KindEnrich, Origin: store.OriginPass, Trigger: parent}, {Kind: store.KindJudge, Origin: store.OriginPass, Trigger: parent}} {
		p := ordinary
		p.Target = store.CandidateIDPrefix + "0123456789abcdef"
		bad = append(bad, p)
		p = ordinary
		p.EnrichKind = enrichers.Kinds()[0]
		bad = append(bad, p)
	}
	base := targetedPass(store.ProspectListIDPrefix+"0123456789abcdef", enrichers.Kinds()[0])
	for _, mutate := range []func(*store.Pass){func(p *store.Pass) { p.Target = "" }, func(p *store.Pass) { p.Target = "bad" }, func(p *store.Pass) { p.EnrichKind = "" }, func(p *store.Pass) { p.EnrichKind = "bad" }, func(p *store.Pass) { p.Origin = store.OriginPass }, func(p *store.Pass) { p.Trigger = "" }, func(p *store.Pass) { p.Trigger = "other" }, func(p *store.Pass) { p.Delivery = "delivery" }, func(p *store.Pass) { p.UserID = "" }} {
		p := base
		mutate(&p)
		bad = append(bad, p)
	}
	rnd := f.rnd.(*contractRandom)
	before := rnd.total
	for _, p := range bad {
		out, err := f.s.AddPass(ctx, p)
		contractError(t, err, store.ErrUnreachable)
		contractEqual(t, out, store.Pass{})
	}
	contractEqual(t, rnd.total, before)
	ps, err := f.s.ListPasses(ctx, "", "", 10)
	contractOK(t, err)
	contractEqual(t, len(ps), 0)
}

// R-HM7C-HURP
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
				count = 3
			}
			for i := range count {
				wg.Go(func() {
					<-start
					p := enrichPass()
					if !sameLane && i == 1 {
						p = store.Pass{Kind: store.KindIngest, Origin: store.OriginIngest, Trigger: store.TriggerIngest, UserID: "owner"}
					}
					if !sameLane && i == 2 {
						p = targetedPass(store.ProspectListIDPrefix+"0123456789abcdef", enrichers.Kinds()[0])
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
				want = 3
			}
			contractEqual(t, success, want)
		})
	}
}

func targetedPass(target, kind string) store.Pass {
	return store.Pass{Kind: store.KindEnrichTarget, Origin: store.OriginEnrich, Trigger: store.TriggerEnrich, Target: target, EnrichKind: kind, UserID: "owner", UserEmail: "owner@example.test", RequestID: "request"}
}

// R-2760-QOWB R-2ATP-W04E R-2EHF-1BCH
func TestPassKindScopeMatrix(t *testing.T) {
	ctx := context.Background()
	parent := store.PassIDPrefix + "0123456789abcdef"
	prospectList := store.ProspectListIDPrefix + "0123456789abcdef"
	cases := []store.Pass{
		{Kind: store.KindIngest, Origin: store.OriginIngest, Trigger: store.TriggerIngest, UserID: "owner"},
		{Kind: store.KindIngest, Origin: store.OriginRun, Trigger: "run-one", Delivery: "event-one"},
		{Kind: store.KindIngest, Origin: store.OriginRun, Trigger: "run-two", Delivery: "event-two"},
		{Kind: store.KindJudge, Origin: store.OriginCron, Trigger: store.TriggerJudge, Delivery: "judge-event"},
		{Kind: store.KindJudge, Origin: store.OriginPass, Trigger: parent},
		enrichPass(),
		{Kind: store.KindEnrich, Origin: store.OriginPass, Trigger: parent},
		targetedPass(prospectList, enrichers.Kinds()[0]),
		targetedPass(prospectList, enrichers.Kinds()[1]),
		targetedPass(store.CandidateIDPrefix+"0123456789abcdef", enrichers.Kinds()[0]),
	}
	for i, a := range cases {
		for j, b := range cases {
			t.Run(fmt.Sprintf("%d/%d", i, j), func(t *testing.T) {
				f := contractDB(t)
				one, err := f.s.AddPass(ctx, a)
				contractOK(t, err)
				before, e := f.s.ListPasses(ctx, "", "", 100)
				contractOK(t, e)
				two, err := f.s.AddPass(ctx, b)
				duplicate := b.Delivery != "" && b.Delivery == a.Delivery || b.Origin == store.OriginRun && a.Origin == store.OriginRun && a.Trigger == b.Trigger
				scopeA, scopeB := "", ""
				if a.Kind == store.KindIngest {
					scopeA = a.Trigger
				}
				if b.Kind == store.KindIngest {
					scopeB = b.Trigger
				}
				if a.Kind == store.KindEnrichTarget {
					scopeA = a.Target
				}
				if b.Kind == store.KindEnrichTarget {
					scopeB = b.Target
				}
				if duplicate || a.Kind == b.Kind && scopeA == scopeB {
					want := store.ErrBusy
					if duplicate {
						want = store.ErrDelivered
					}
					contractError(t, err, want)
					contractEqual(t, two, store.Pass{})
					after, e := f.s.ListPasses(ctx, "", "", 100)
					contractOK(t, e)
					contractEqual(t, after, before)
				} else {
					contractOK(t, err)
					if !store.ValidPassID(two.ID) || two.ID == one.ID {
						t.Fatal(two)
					}
					want := b
					want.ID = two.ID
					want.Status = store.StatusRunning
					want.Started = f.now.UTC().Truncate(time.Second)
					contractEqual(t, two, want)
					f.reopen()
					got, e := f.s.GetPass(ctx, two.ID)
					contractOK(t, e)
					contractEqual(t, got, two)
				}
			})
		}
	}
}

// R-2ATP-W04E R-O25T-GHMS R-YID5-EN5N
func TestTargetedPassLifecyclePreservesOtherRecords(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	id := contractSeed(t, f)
	base := targetedPass(id, enrichers.Kinds()[0])
	base.ID = "ignored"
	base.Status = "ignored"
	base.Reason = "ignored"
	base.Started = f.now.Add(-time.Hour)
	base.Finished = f.now
	base.Rows = 99
	base.New = 99
	base.Changed = 99
	base.Unchanged = 99
	base.Gone = 99
	base.Created = 99
	base.Linked = 99
	base.Judged = 99
	base.Unranged = 99
	base.Derived = 99
	base.Due = 99
	base.Fetched = 99
	base.HTTPS = 99
	base.HTTP = 99
	base.None = 99
	base.Runs = 99
	base.RunsFailed = 99
	base.Pages = 99
	base.Cost = 99
	p, err := f.s.AddPass(ctx, base)
	contractOK(t, err)
	want := targetedPass(id, enrichers.Kinds()[0])
	want.ID = p.ID
	want.Status = store.StatusRunning
	want.Started = f.now.UTC().Truncate(time.Second)
	contractEqual(t, p, want)
	f.reopen()
	got, err := f.s.GetPass(ctx, p.ID)
	contractOK(t, err)
	contractEqual(t, got, p)
	other, err := f.s.AddPass(ctx, enrichPass())
	contractOK(t, err)
	f.now = f.now.Add(time.Hour)
	p, err = f.s.FinishPass(ctx, p.ID, store.Counts{Runs: 7, RunsFailed: 2, Pages: 11, Cost: 123456789})
	contractOK(t, err)
	want.Status = store.StatusDone
	want.Finished = f.now.UTC().Truncate(time.Second)
	want.Runs = 7
	want.RunsFailed = 2
	want.Pages = 11
	want.Cost = 123456789
	contractEqual(t, p, want)
	f.reopen()
	got, err = f.s.GetPass(ctx, p.ID)
	contractOK(t, err)
	contractEqual(t, got, p)
	got, err = f.s.GetPass(ctx, other.ID)
	contractOK(t, err)
	contractEqual(t, got, other)
	q, err := f.s.AddPass(ctx, targetedPass(id, enrichers.Kinds()[0]))
	contractOK(t, err)
	before := contractState(t, f)
	q, err = f.s.FailPass(ctx, q.ID, store.ReasonPrompts)
	contractOK(t, err)
	after := contractState(t, f)
	// Remove the single pass record allowed to change before comparing all other state.
	before.passes = slices.DeleteFunc(before.passes, func(p store.Pass) bool { return p.ID == q.ID })
	after.passes = slices.DeleteFunc(after.passes, func(p store.Pass) bool { return p.ID == q.ID })
	contractEqual(t, after, before)
	f.reopen()
	got, err = f.s.GetPass(ctx, q.ID)
	contractOK(t, err)
	contractEqual(t, got, q)
}

// R-2760-QOWB R-2ATP-W04E
func TestTargetedPassAcceptsEveryEnrichmentKind(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	for _, kind := range enrichers.Kinds() {
		p, err := f.s.AddPass(ctx, targetedPass(store.ProspectListIDPrefix+"0123456789abcdef", kind))
		contractOK(t, err)
		contractEqual(t, p.EnrichKind, kind)
		_, err = f.s.FinishPass(ctx, p.ID, store.Counts{})
		contractOK(t, err)
	}
}

// R-PO4D-1YWE
func TestPassStoreMethodSignatures(t *testing.T) {
	f := contractDB(t)
	// R-PO4D-1YWE
	methods := struct {
		add     func(context.Context, store.Pass) (store.Pass, error)
		finish  func(context.Context, string, store.Counts) (store.Pass, error)
		fail    func(context.Context, string, string) (store.Pass, error)
		get     func(context.Context, string) (store.Pass, error)
		list    func(context.Context, string, string, int) ([]store.Pass, error)
		running func(context.Context) ([]store.Pass, error)
		prune   func(context.Context, int64, int) (int64, error)
	}{add: f.s.AddPass, finish: f.s.FinishPass, fail: f.s.FailPass, get: f.s.GetPass, list: f.s.ListPasses, running: f.s.RunningPasses, prune: f.s.PrunePasses}
	if methods.add == nil || methods.finish == nil || methods.fail == nil || methods.get == nil || methods.list == nil || methods.running == nil || methods.prune == nil {
		t.Fatal("missing pass store method")
	}
}
