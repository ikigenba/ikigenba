package store_test

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

func keptPage(id, url string) store.Page {
	return store.Page{Candidate: id, URL: url, Title: "a title", Text: "page content", Size: 123, UserID: "owner", UserEmail: "owner@example.test"}
}

// R-ZLJX-D1G1 R-ZP7M-ICO4 R-OAP4-4VTN R-OBX0-INKC R-OD4W-WFB1
func TestPageCopiesPersistAndListNewest(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	id := contractSeed(t, f)
	pages, err := f.s.Pages(ctx, id)
	contractOK(t, err)
	contractEqual(t, pages, []store.Page{})
	counts, err := f.s.CountPages(ctx, id)
	contractOK(t, err)
	contractEqual(t, counts, store.PageCount{})
	p := keptPage(id, "https://business.test/a")
	p.Number = 999
	p.Fetched = time.Unix(1, 0)
	a, err := f.s.AddPage(ctx, p)
	contractOK(t, err)
	p.Number = a.Number
	p.Fetched = f.now.UTC().Truncate(time.Second)
	contractEqual(t, a, p)
	contractEqual(t, store.Page(struct {
		Candidate                string
		Number                   int64
		URL, Title, Text, Reason string
		Size                     int64
		Fetched                  time.Time
		UserID, UserEmail        string
	}(a)), a)
	b, err := f.s.AddPage(ctx, keptPage(id, "https://business.test/b"))
	contractOK(t, err)
	p.Title = "new title"
	p.Text = "new body"
	newer, err := f.s.AddPage(ctx, p)
	contractOK(t, err)
	if a.Number <= 0 || b.Number <= a.Number || newer.Number <= b.Number {
		t.Fatal("page numbers did not increase")
	}
	other := enrichmentCandidate(t, f, "other-business", "other@other.test")
	global, err := f.s.AddPage(ctx, keptPage(other, "https://other.test"))
	contractOK(t, err)
	if global.Number <= newer.Number {
		t.Fatal("numbers are not global")
	}
	f.reopen()
	for _, want := range []store.Page{a, b, newer, global} {
		got, e := f.s.GetPage(ctx, want.Candidate, want.Number)
		contractOK(t, e)
		contractEqual(t, got, want)
	}
	pages, err = f.s.Pages(ctx, id)
	contractOK(t, err)
	newer.Text = ""
	b.Text = ""
	contractEqual(t, pages, []store.Page{newer, b})
	counts, err = f.s.CountPages(ctx, id)
	contractOK(t, err)
	contractEqual(t, counts, store.PageCount{URLs: 2, Copies: 3})
	contractEqual(t, store.PageCount(struct{ URLs, Copies int64 }(counts)), counts)
	for _, pair := range []struct {
		id string
		n  int64
	}{{other, a.Number}, {id, 0}, {"missing", a.Number}, {id, global.Number}} {
		got, e := f.s.GetPage(ctx, pair.id, pair.n)
		contractError(t, e, store.ErrNotFound)
		contractEqual(t, got, store.Page{})
	}
	pages, err = f.s.Pages(ctx, "missing")
	contractError(t, err, store.ErrNotFound)
	contractEqual(t, pages, []store.Page(nil))
	counts, err = f.s.CountPages(ctx, "missing")
	contractError(t, err, store.ErrNotFound)
	contractEqual(t, counts, store.PageCount{})
}

// R-ZQFI-W4ET R-O9H7-R42Y
func TestPageCapValidationAndConcurrentAdds(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	id := contractSeed(t, f)
	for _, p := range []store.Page{{Candidate: id, UserID: "owner"}, {Candidate: id, URL: "url"}, {Candidate: id, URL: "url", UserID: "owner", Size: -1}} {
		got, err := f.s.AddPage(ctx, p)
		contractError(t, err, store.ErrUnreachable)
		contractEqual(t, got, store.Page{})
	}
	_, err := f.s.AddPage(ctx, keptPage("missing", "url"))
	contractError(t, err, store.ErrNotFound)
	for i := 0; i < store.PageCap-1; i++ {
		_, err = f.s.AddPage(ctx, keptPage(id, fmt.Sprintf("url-%d", i)))
		contractOK(t, err)
	}
	const racers = 8
	errs := make(chan error, racers)
	var wg sync.WaitGroup
	for i := range racers {
		wg.Go(func() { _, e := f.s.AddPage(ctx, keptPage(id, fmt.Sprintf("new-%d", i))); errs <- e })
	}
	wg.Wait()
	close(errs)
	successes := 0
	for e := range errs {
		if e == nil {
			successes++
		} else {
			contractError(t, e, store.ErrPageCap)
		}
	}
	contractEqual(t, successes, 1)
	counts, err := f.s.CountPages(ctx, id)
	contractOK(t, err)
	contractEqual(t, counts, store.PageCount{URLs: store.PageCap, Copies: store.PageCap})
	_, err = f.s.AddPage(ctx, keptPage(id, "url-0"))
	contractOK(t, err)
	_, err = f.s.AddPage(ctx, keptPage(id, "URL-0"))
	contractError(t, err, store.ErrPageCap)
	counts, err = f.s.CountPages(ctx, id)
	contractOK(t, err)
	contractEqual(t, counts, store.PageCount{URLs: store.PageCap, Copies: store.PageCap + 1})
	merged := enrichmentCandidate(t, f, "merge-me", "merge@other.test")
	_, _, err = f.s.Decide(ctx, store.Ruling{Kind: store.KindMerge, Candidate: id, Other: merged, UserID: "owner", Why: "same business"})
	contractOK(t, err)
	_, err = f.s.AddPage(ctx, keptPage(merged, "url"))
	contractError(t, err, store.ErrInvalid)
	pages, err := f.s.Pages(ctx, merged)
	contractOK(t, err)
	contractEqual(t, pages, []store.Page{})
	counts, err = f.s.CountPages(ctx, merged)
	contractOK(t, err)
	contractEqual(t, counts, store.PageCount{})
}

// R-VWK4-AYB8
func TestPageErrorsAndTimeNormalization(t *testing.T) {
	t.Parallel()
	f := contractDB(t)
	ctx := context.Background()
	id := contractSeed(t, f)
	p, err := f.s.AddPage(ctx, keptPage(id, "first"))
	contractOK(t, err)
	if p.Number < 1 || p.Fetched.Location() != time.UTC || p.Fetched.Nanosecond() != 0 {
		t.Fatal("page number or fetched time not normalized")
	}
	before, err := f.s.CountPages(ctx, id)
	contractOK(t, err)
	done, cancel := context.WithCancel(ctx)
	cancel()
	for _, failing := range []bool{false, true} {
		f.d.SetFailing(failing)
		for _, c := range []context.Context{done, ctx} {
			if !failing && c == ctx {
				continue
			}
			got, e := f.s.AddPage(c, store.Page{Candidate: "missing"})
			contractError(t, e, store.ErrUnreachable)
			contractEqual(t, got, store.Page{})
			if c == done {
				contractError(t, e, context.Canceled)
			}
			ps, e := f.s.Pages(c, "missing")
			contractError(t, e, store.ErrUnreachable)
			contractEqual(t, ps, []store.Page(nil))
			got, e = f.s.GetPage(c, "missing", -1)
			contractError(t, e, store.ErrUnreachable)
			contractEqual(t, got, store.Page{})
			counts, e := f.s.CountPages(c, "missing")
			contractError(t, e, store.ErrUnreachable)
			contractEqual(t, counts, store.PageCount{})
		}
	}
	f.d.SetFailing(false)
	after, err := f.s.CountPages(ctx, id)
	contractOK(t, err)
	contractEqual(t, after, before)
}

// R-WNFA-Q8TO
func TestOrdinaryWritesPreservePageCopies(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	id := contractSeed(t, f)
	a, err := f.s.AddPage(ctx, keptPage(id, "first"))
	contractOK(t, err)
	b, err := f.s.AddPage(ctx, keptPage(id, "first"))
	contractOK(t, err)
	c, err := f.s.AddPage(ctx, keptPage(id, "second"))
	contractOK(t, err)
	verify := func() {
		t.Helper()
		for _, p := range []store.Page{a, b, c} {
			got, e := f.s.GetPage(ctx, id, p.Number)
			contractOK(t, e)
			contractEqual(t, got, p)
		}
		counts, e := f.s.CountPages(ctx, id)
		contractOK(t, e)
		contractEqual(t, counts, store.PageCount{URLs: 2, Copies: 3})
	}
	actions := []func() error{
		func() error { _, e := f.s.AddEnrichments(ctx, []store.Enrichment{websiteResult(id, "none")}); return e },
		func() error { _, _, e := f.s.Derive(ctx, "link_facts", "", true, 100); return e },
		func() error { return func() error { _, e := f.s.FillFacts(ctx, "", 500); return e }() },
		func() error { _, e := f.s.Judge(ctx, []string{id}); return e },
		func() error { _, _, e := f.s.JudgeAll(ctx, "", 500, f.now); return e },
		func() error { return f.s.ReplaceZipDrive(ctx, map[string]int64{"55072": 15}) },
	}
	for _, action := range actions {
		contractOK(t, action())
		verify()
	}
}
