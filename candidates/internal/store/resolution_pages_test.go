package store_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// R-R9XQ-WPK7 R-RDLG-20SA R-OECT-A71Q
func TestMergeMovesAllPageCopiesAndSplitPreservesThem(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	a := contractObservation("page-a", "Page survivor", f.now)
	b := contractObservation("page-b", "Page other", f.now)
	survivor := contractAttach(t, f, "run", a)[0]
	other := contractAttach(t, f, "run", b)[0]
	saved := []store.Page{}
	for _, id := range []string{survivor, other} {
		for i := 0; i < store.PageCap; i++ {
			p, err := f.s.AddPage(ctx, store.Page{Candidate: id, URL: fmt.Sprintf("https://%s.test/%d", id, i), Title: fmt.Sprint(i), Text: "preserved text " + id, Size: 123, UserID: "owner", UserEmail: "owner@example.test"})
			contractOK(t, err)
			saved = append(saved, p)
		}
	}
	// A repeated URL retains both copies, with the newer shown in the list.
	shared := saved[0].URL
	newer, err := f.s.AddPage(ctx, store.Page{Candidate: other, URL: saved[store.PageCap].URL, Text: "newer same URL", UserID: "owner"})
	contractOK(t, err)
	saved = append(saved, newer)
	_, _, err = f.s.Decide(ctx, store.Ruling{Kind: store.KindMerge, Candidate: survivor, Other: other, Why: "same", UserID: "owner"})
	contractOK(t, err)
	assertPages := func() {
		t.Helper()
		for _, p := range saved {
			p.Candidate = survivor
			got, e := f.s.GetPage(ctx, survivor, p.Number)
			contractOK(t, e)
			contractEqual(t, got, p)
			_, e = f.s.GetPage(ctx, other, p.Number)
			contractError(t, e, store.ErrNotFound)
		}
		empty, e := f.s.Pages(ctx, other)
		contractOK(t, e)
		contractEqual(t, empty, []store.Page{})
		count, e := f.s.CountPages(ctx, other)
		contractOK(t, e)
		contractEqual(t, count, store.PageCount{})
		count, e = f.s.CountPages(ctx, survivor)
		contractOK(t, e)
		contractEqual(t, count, store.PageCount{URLs: 2 * store.PageCap, Copies: int64(len(saved))})
	}
	assertPages()
	f.reopen()
	assertPages()
	_, err = f.s.AddPage(ctx, store.Page{Candidate: survivor, URL: "https://new.test/", UserID: "owner"})
	contractError(t, err, store.ErrPageCap)
	copyPage, err := f.s.AddPage(ctx, store.Page{Candidate: survivor, URL: shared, Text: "latest shared", UserID: "owner"})
	contractOK(t, err)
	before, err := f.s.Pages(ctx, survivor)
	contractOK(t, err)
	contractEqual(t, before[0].Number, copyPage.Number)
	split, _, err := f.s.Decide(ctx, store.Ruling{Kind: store.KindSplit, Candidate: survivor, Keys: []string{sources.SourceKey(b.Scraper, b.SourceID)}, Why: "separate", UserID: "owner"})
	contractOK(t, err)
	got, err := f.s.Pages(ctx, survivor)
	contractOK(t, err)
	contractEqual(t, got, before)
	empty, err := f.s.Pages(ctx, split.Other)
	contractOK(t, err)
	contractEqual(t, empty, []store.Page{})
	count, err := f.s.CountPages(ctx, split.Other)
	contractOK(t, err)
	contractEqual(t, count, store.PageCount{})
	for _, p := range append(saved, copyPage) {
		_, err = f.s.GetPage(ctx, split.Other, p.Number)
		contractError(t, err, store.ErrNotFound)
	}
	f.reopen()
	got, err = f.s.Pages(ctx, survivor)
	contractOK(t, err)
	contractEqual(t, got, before)
}

// R-R9XQ-WPK7
func TestMergeChoosesNewestSharedURLWithoutDiscardingCopies(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	a := contractSeed(t, f)
	b := enrichmentCandidate(t, f, "shared-other", "owner@other.test")
	old, err := f.s.AddPage(ctx, store.Page{Candidate: a, URL: "https://shared.test/", Text: "older", UserID: "owner"})
	contractOK(t, err)
	newer, err := f.s.AddPage(ctx, store.Page{Candidate: b, URL: old.URL, Text: "newer", UserID: "owner"})
	contractOK(t, err)
	_, _, err = f.s.Decide(ctx, store.Ruling{Kind: store.KindMerge, Candidate: a, Other: b, Why: "same", UserID: "owner"})
	contractOK(t, err)
	list, err := f.s.Pages(ctx, a)
	contractOK(t, err)
	newer.Candidate = a
	listing := newer
	listing.Text = ""
	contractEqual(t, list, []store.Page{listing})
	for _, want := range []store.Page{old, newer} {
		got, e := f.s.GetPage(ctx, a, want.Number)
		contractOK(t, e)
		contractEqual(t, got, want)
	}
	counts, err := f.s.CountPages(ctx, a)
	contractOK(t, err)
	contractEqual(t, counts, store.PageCount{URLs: 1, Copies: 2})
}
