package store_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

func campaignUses(t *testing.T, f *contractFixture, id string) []store.CampaignUse {
	t.Helper()
	us, err := f.s.CampaignUses(context.Background(), id)
	contractOK(t, err)
	if us == nil {
		t.Fatal("nil campaign history")
	}
	return us
}

// R-RQLV-9IAZ R-2UKW-Q9IR R-UOP4-JJTE R-SSER-4EAI R-S0D2-BO8J R-SZDI-2HTS R-T0LE-G9KH
func TestCampaignUseOrdersDuplicatesAndPersistence(t *testing.T) {
	for _, order := range [][]int{{0, 1, 2}, {0, 2, 1}, {1, 0, 2}, {1, 2, 0}, {2, 0, 1}, {2, 1, 0}} {
		t.Run(fmt.Sprint(order), func(t *testing.T) {
			f := contractDB(t)
			ctx := context.Background()
			id := contractSeed(t, f)
			a := time.Date(2026, 9, 15, 23, 59, 59, 987654321, time.FixedZone("input", -5*3600))
			b := a.Add(24 * time.Hour)
			c := b.Add(24 * time.Hour)
			calls := []func() error{func() error { return f.s.AddCampaignUse(ctx, id, "main", "prospectList-first", a) }, func() error { return f.s.RejectCampaignUse(ctx, id, "main", "first reason", b) }, func() error { return f.s.DeliverCampaignUse(ctx, id, "main", c) }}
			want := store.CampaignUse{Candidate: id, Campaign: "main"}
			for _, i := range order {
				contractOK(t, calls[i]())
				switch i {
				case 0:
					want.ProspectList = "prospectList-first"
					want.Added = a.UTC().Truncate(time.Second)
				case 1:
					want.Reason = "first reason"
					want.Rejected = b.UTC().Truncate(time.Second)
				case 2:
					want.Delivered = c.UTC().Truncate(time.Second)
				}
				contractEqual(t, campaignUses(t, f, id), []store.CampaignUse{want})
			}
			for _, i := range order {
				contractOK(t, calls[i]())
			}
			contractOK(t, f.s.AddCampaignUse(ctx, id, "main", "later prospectList", b))
			contractOK(t, f.s.AddCampaignUse(ctx, id, "main", "tie prospectList", a.Truncate(time.Second)))
			contractOK(t, f.s.RejectCampaignUse(ctx, id, "main", "later reason", c))
			contractOK(t, f.s.RejectCampaignUse(ctx, id, "main", "tie reason", b.Truncate(time.Second)))
			contractOK(t, f.s.DeliverCampaignUse(ctx, id, "main", b))
			contractEqual(t, campaignUses(t, f, id), []store.CampaignUse{want})
			earlier := a.Add(-24 * time.Hour)
			contractOK(t, f.s.AddCampaignUse(ctx, id, "main", "earliest prospectList", earlier))
			contractOK(t, f.s.RejectCampaignUse(ctx, id, "main", "earliest reason", earlier))
			contractOK(t, f.s.DeliverCampaignUse(ctx, id, "main", c.Add(time.Hour)))
			want.Added, want.ProspectList = earlier.UTC().Truncate(time.Second), "earliest prospectList"
			want.Rejected, want.Reason = want.Added, "earliest reason"
			want.Delivered = c.Add(time.Hour).UTC().Truncate(time.Second)
			contractOK(t, f.s.AddCampaignUse(ctx, id, "a", "", earlier))
			contractOK(t, f.s.DeliverCampaignUse(ctx, id, "z", a))
			contractOK(t, f.s.RejectCampaignUse(ctx, id, "y", "rejected only", b))
			contractOK(t, f.s.AddCampaignUse(ctx, id, "newest", "", c))
			expected := []store.CampaignUse{{Candidate: id, Campaign: "newest", Added: c.UTC().Truncate(time.Second)}, {Candidate: id, Campaign: "a", Added: want.Added}, want, {Candidate: id, Campaign: "y", Rejected: b.UTC().Truncate(time.Second), Reason: "rejected only"}, {Candidate: id, Campaign: "z", Delivered: a.UTC().Truncate(time.Second)}}
			contractEqual(t, campaignUses(t, f, id), expected)
			f.reopen()
			contractEqual(t, campaignUses(t, f, id), expected)
		})
	}
}

// R-SY5L-OQ33 R-QEFR-MGTW R-OD8H-2EDG
func TestCampaignUseErrorsAreAtomicAndPrioritized(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	id := contractSeed(t, f)
	contractOK(t, f.s.AddCampaignUse(ctx, id, "kept", "prospect_list", f.now))
	before := campaignUses(t, f, id)
	calls := []func(context.Context, string, string, time.Time) error{func(c context.Context, id, k string, at time.Time) error { return f.s.AddCampaignUse(c, id, k, "", at) }, func(c context.Context, id, k string, at time.Time) error {
		return f.s.RejectCampaignUse(c, id, k, "", at)
	}, f.s.DeliverCampaignUse}
	for _, call := range calls {
		for _, tc := range []struct {
			id, k string
			at    time.Time
			err   error
		}{{"", "", time.Time{}, store.ErrUnreachable}, {"missing", "", time.Time{}, store.ErrUnreachable}, {"", "x", f.now, store.ErrInvalid}, {"missing", "", f.now, store.ErrInvalid}, {"missing", "x", f.now, store.ErrNotFound}} {
			contractError(t, call(ctx, tc.id, tc.k, tc.at), tc.err)
			contractEqual(t, campaignUses(t, f, id), before)
		}
		f.d.SetFailing(true)
		contractError(t, call(ctx, "", "", time.Time{}), store.ErrUnreachable)
		f.d.SetFailing(false)
		canceled, cancel := context.WithCancel(ctx)
		cancel()
		err := call(canceled, id, "new", f.now)
		contractError(t, err, store.ErrUnreachable)
		contractError(t, err, context.Canceled)
	}
	missing, err := f.s.CampaignUses(ctx, "missing")
	contractError(t, err, store.ErrNotFound)
	contractEqual(t, missing, []store.CampaignUse(nil))
	f.d.SetFailing(true)
	missing, err = f.s.CampaignUses(ctx, id)
	contractError(t, err, store.ErrUnreachable)
	contractEqual(t, missing, []store.CampaignUse(nil))
	f.d.SetFailing(false)
	contractEqual(t, campaignUses(t, f, id), before)
}

// R-SS23-RVDM R-SDRY-J5E6 R-SOW1-ZWQM
func TestCampaignUsesMergeUnionTiesChainAndSplit(t *testing.T) {
	for _, ties := range []bool{false, true} {
		t.Run(fmt.Sprint(ties), func(t *testing.T) {
			f := contractDB(t)
			ctx := context.Background()
			a := contractObservation("merge-a", "survivor", f.now)
			b := contractObservation("merge-b", "other", f.now)
			s := contractAttach(t, f, "run", a)[0]
			o := contractAttach(t, f, "run", b)[0]
			t0 := f.now.UTC().Truncate(time.Second)
			t1 := t0.Add(time.Hour)
			t2 := t1.Add(time.Hour)
			t3 := t2.Add(time.Hour)
			otherAdd, otherReject := t0, t1
			if ties {
				otherAdd = t1
				otherReject = t2
			}
			contractOK(t, f.s.AddCampaignUse(ctx, s, "common", "survivor prospectList", t1))
			contractOK(t, f.s.RejectCampaignUse(ctx, s, "common", "survivor reason", t2))
			contractOK(t, f.s.DeliverCampaignUse(ctx, s, "common", t3))
			contractOK(t, f.s.AddCampaignUse(ctx, o, "common", "other prospectList", otherAdd))
			contractOK(t, f.s.RejectCampaignUse(ctx, o, "common", "other reason", otherReject))
			contractOK(t, f.s.DeliverCampaignUse(ctx, o, "common", t2))
			contractOK(t, f.s.DeliverCampaignUse(ctx, o, "other-only", t3))
			contractOK(t, f.s.RejectCampaignUse(ctx, s, "survivor-only", "s", t0))
			_, _, err := f.s.Decide(ctx, store.Ruling{Kind: store.KindMerge, Candidate: s, Other: o, Why: "same business", UserID: "owner"})
			contractOK(t, err)
			common := store.CampaignUse{Candidate: s, Campaign: "common", ProspectList: "other prospectList", Added: t0, Rejected: t1, Reason: "other reason", Delivered: t3}
			if ties {
				common.ProspectList = "survivor prospectList"
				common.Added = t1
				common.Rejected = t2
				common.Reason = "survivor reason"
			}
			want := []store.CampaignUse{common, {Candidate: s, Campaign: "other-only", Delivered: t3}, {Candidate: s, Campaign: "survivor-only", Rejected: t0, Reason: "s"}}
			contractEqual(t, campaignUses(t, f, s), want)
			contractEqual(t, campaignUses(t, f, o), []store.CampaignUse{})
			f.reopen()
			contractEqual(t, campaignUses(t, f, s), want)
			for _, kind := range []string{store.KindExclude, store.KindInclude} {
				_, _, err = f.s.Decide(ctx, store.Ruling{Kind: kind, Candidate: s, Why: "owner ruling", UserID: "owner"})
				contractOK(t, err)
				contractEqual(t, campaignUses(t, f, s), want)
			}
			r, _, err := f.s.Decide(ctx, store.Ruling{Kind: store.KindSplit, Candidate: s, Keys: []string{sources.SourceKey(b.Scraper, b.SourceID)}, Why: "distinct business", UserID: "owner"})
			contractOK(t, err)
			contractEqual(t, campaignUses(t, f, s), want)
			contractEqual(t, campaignUses(t, f, r.Other), []store.CampaignUse{})
			_, _, err = f.s.Decide(ctx, store.Ruling{Kind: store.KindNotSame, Candidate: s, Other: r.Other, Why: "distinct", UserID: "owner"})
			contractOK(t, err)
			contractEqual(t, campaignUses(t, f, s), want)
			next := contractAttach(t, f, "run", contractObservation("merge-c", "next survivor", f.now))[0]
			_, _, err = f.s.Decide(ctx, store.Ruling{Kind: store.KindMerge, Candidate: next, Other: s, Why: "same again", UserID: "owner"})
			contractOK(t, err)
			contractOK(t, f.s.AddCampaignUse(ctx, o, "chain", "chain prospectList", t0))
			contractOK(t, f.s.RejectCampaignUse(ctx, o, "chain", "chain reason", t1))
			contractOK(t, f.s.DeliverCampaignUse(ctx, o, "chain", t2))
			found := false
			for _, u := range campaignUses(t, f, next) {
				if u.Campaign == "chain" {
					found = true
					contractEqual(t, u, store.CampaignUse{Candidate: next, Campaign: "chain", ProspectList: "chain prospectList", Added: t0, Rejected: t1, Reason: "chain reason", Delivered: t2})
				}
			}
			contractEqual(t, found, true)
			contractEqual(t, campaignUses(t, f, o), []store.CampaignUse{})
			contractEqual(t, campaignUses(t, f, s), []store.CampaignUse{})
		})
	}
}

// R-T317-7T1V R-NEW2-V92O R-5N3M-ADXX
func TestCampaignConcurrentWritesAndMergeAreWhole(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	s := contractSeed(t, f)
	o := contractAttach(t, f, "run", contractObservation("racing", "racing other", f.now))[0]
	contractOK(t, f.s.AddCampaignUse(ctx, o, "moved", "", f.now))
	start := make(chan struct{})
	results := make(chan error, 61)
	var wg sync.WaitGroup
	run := func(call func() error) { wg.Go(func() { <-start; results <- call() }) }
	for i := range 20 {
		at := f.now.Add(time.Duration(i) * time.Hour)
		run(func() error { return f.s.AddCampaignUse(ctx, o, "racing", "prospect_list", at) })
		run(func() error { return f.s.RejectCampaignUse(ctx, o, "racing", "reason", at) })
		run(func() error { return f.s.DeliverCampaignUse(ctx, o, "racing", at) })
	}
	run(func() error {
		_, _, err := f.s.Decide(ctx, store.Ruling{Kind: store.KindMerge, Candidate: s, Other: o, Why: "same", UserID: "owner"})
		return err
	})
	close(start)
	for range 20 {
		ls, err := f.s.Links(ctx, s)
		contractOK(t, err)
		moved := false
		for _, u := range campaignUses(t, f, s) {
			if u.Campaign == "moved" {
				moved = true
			}
		}
		if len(ls) == 2 && !moved {
			t.Fatal("links moved before campaign history")
		}
		if moved {
			ls, err = f.s.Links(ctx, o)
			contractOK(t, err)
			contractEqual(t, len(ls), 0)
		}
	}
	wg.Wait()
	close(results)
	for err := range results {
		contractOK(t, err)
	}
	us := campaignUses(t, f, s)
	contractEqual(t, len(us), 2)
	contractEqual(t, us[1], store.CampaignUse{Candidate: s, Campaign: "racing", ProspectList: "prospect_list", Added: f.now.UTC().Truncate(time.Second), Rejected: f.now.UTC().Truncate(time.Second), Reason: "reason", Delivered: f.now.Add(19 * time.Hour).UTC().Truncate(time.Second)})
	contractEqual(t, campaignUses(t, f, o), []store.CampaignUse{})
}

// R-Y52V-K5G8 R-UOP4-JJTE R-SSER-4EAI R-S0D2-BO8J R-RQLV-9IAZ
func TestCampaignWritesPreserveOtherRecordsAndOtherWritesPreserveHistory(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	id := contractSeed(t, f)
	type shape struct {
		Candidate, Campaign, ProspectList string
		Added, Rejected                   time.Time
		Reason                            string
		Delivered                         time.Time
	}
	contractEqual(t, shape(store.CampaignUse{}), shape{})
	before := contractState(t, f)
	for _, call := range []func() error{func() error { return f.s.AddCampaignUse(ctx, id, "kept", "origin", f.now) }, func() error { return f.s.RejectCampaignUse(ctx, id, "kept", "reason", f.now) }, func() error { return f.s.DeliverCampaignUse(ctx, id, "kept", f.now) }} {
		contractOK(t, call())
		contractEqual(t, contractState(t, f), before)
	}
	want := campaignUses(t, f, id)
	check := func() { t.Helper(); contractEqual(t, campaignUses(t, f, id), want) }
	_, _, err := f.s.Attach(ctx, "next-run", false, []store.Observation{contractObservation("evidence", "Seed business", f.now)})
	contractOK(t, err)
	check()
	_, _, err = f.s.MarkGone(ctx, "source-z", "absent")
	contractOK(t, err)
	check()
	_, err = f.s.Judge(ctx, []string{id})
	contractOK(t, err)
	check()
	_, err = f.s.JudgeAll(ctx)
	contractOK(t, err)
	check()
	_, err = f.s.AddEnrichments(ctx, []store.Enrichment{websiteResult(id, sources.WebsiteHTTPS)})
	contractOK(t, err)
	check()
	_, _, err = f.s.Derive(ctx, "link_facts", "", true, 100)
	contractOK(t, err)
	check()
	_, err = f.s.DeriveAmong(ctx, "link_facts", []string{id}, true)
	contractOK(t, err)
	check()
	contractOK(t, f.s.FillFacts(ctx))
	check()
	contractOK(t, f.s.ReplaceZipDrive(ctx, map[string]int64{"55072": 99}))
	check()
	_, err = f.s.AddPage(ctx, store.Page{Candidate: id, URL: "https://preserved.test/", Text: "preserved", UserID: "owner"})
	contractOK(t, err)
	check()
	k := prospectListCreate(t, f, "preserving")
	check()
	_, err = f.s.EditProspectList(ctx, store.ProspectListEdit{ID: k.ID, Filters: &store.ProspectListFilters{MinCampaignAgeDays: ptrInt(1)}, UserID: "owner"})
	contractOK(t, err)
	check()
	for _, kind := range []string{store.KindReject, store.KindUnreject} {
		_, err = f.s.Reject(ctx, store.Reject{Kind: kind, ProspectList: k.ID, Candidate: id, Why: "owner", UserID: "owner"})
		contractOK(t, err)
		check()
	}
	_, err = f.s.DeleteProspectList(ctx, k.ID)
	contractOK(t, err)
	check()
	for _, kind := range []string{store.KindExclude, store.KindInclude} {
		_, _, err = f.s.Decide(ctx, store.Ruling{Kind: kind, Candidate: id, Why: "owner", UserID: "owner"})
		contractOK(t, err)
		check()
	}
	for _, finished := range []bool{false, true} {
		p := contractNewPass(t, f, fmt.Sprint("preserving-", finished))
		check()
		if finished {
			_, err = f.s.FinishPass(ctx, p.ID, store.Counts{})
		} else {
			_, err = f.s.FailPass(ctx, p.ID, store.ReasonStore)
		}
		contractOK(t, err)
		check()
	}
	_, err = f.s.PrunePasses(ctx, 1)
	contractOK(t, err)
	check()
	f.reopen()
	check()
}

// R-S0D2-BO8J R-UOP4-JJTE R-SSER-4EAI
func TestCampaignUseValidTimesBeforeZero(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	id := contractSeed(t, f)
	at := time.Date(0, 1, 1, 0, 0, 0, 0, time.UTC)
	contractOK(t, f.s.DeliverCampaignUse(ctx, id, "early", at))
	contractOK(t, f.s.AddCampaignUse(ctx, id, "early", "origin", at))
	contractOK(t, f.s.RejectCampaignUse(ctx, id, "early", "reason", at))
	contractOK(t, f.s.DeliverCampaignUse(ctx, id, "early", at))
	contractEqual(t, campaignUses(t, f, id), []store.CampaignUse{{Candidate: id, Campaign: "early", ProspectList: "origin", Added: at, Rejected: at, Reason: "reason", Delivered: at}})
	contractOK(t, f.s.AddCampaignUse(ctx, id, "existing", "", at))
	contractOK(t, f.s.DeliverCampaignUse(ctx, id, "existing", at))
	contractEqual(t, campaignUses(t, f, id)[1].Delivered, time.Time{})
}
