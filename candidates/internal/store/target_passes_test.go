package store_test

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/parks"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// R-CFB2-WDRM R-CGIZ-A5IB
func TestTargetPassRecordAndMethodShapes(t *testing.T) {
	k := store.KindEntry{"website_search", 1, 2, 3, 4, 5}
	shaped := struct {
		Kind                                            string
		NewResults, NotDue, NotApplicable, Failed, Cost int64
	}(k)
	contractEqual(t, store.KindEntry(shaped), k)
	f := contractDB(t)
	methods := struct {
		add     func(context.Context, store.Pass, []string) (store.Pass, []string, error)
		start   func(context.Context, string, string) (store.Pass, error)
		end     func(context.Context, string, store.KindEntry) (store.Pass, error)
		running func(context.Context, string) (string, error)
	}{f.s.AddTargetPass, f.s.StartPassKind, f.s.EndPassKind, f.s.RunningTargetPass}
	p, ids, err := methods.add(t.Context(), targetedPass(store.CandidateIDPrefix+"0123456789abcdef", ""), nil)
	contractOK(t, err)
	contractEqual(t, ids, []string{})
	_, err = methods.start(t.Context(), p.ID, k.Kind)
	contractOK(t, err)
	p, err = methods.end(t.Context(), p.ID, k)
	contractOK(t, err)
	contractEqual(t, p.KindEntries, []store.KindEntry{k})
	id, err := methods.running(t.Context(), "missing")
	contractOK(t, err)
	contractEqual(t, id, "")
}

func targetPassIDs(ids ...string) store.Pass {
	p := targetedPass("", "")
	p.TargetIDs = ids
	return p
}

func assertPassReads(t *testing.T, f *contractFixture, want store.Pass) {
	t.Helper()
	got, err := f.s.GetPass(t.Context(), want.ID)
	contractOK(t, err)
	contractEqual(t, got, want)
	for _, get := range []func(context.Context, string, string, int) ([]store.Pass, error){f.s.ListPasses, f.s.ListPassesReversed} {
		ps, e := get(t.Context(), "", "", 100)
		contractOK(t, e)
		i := slices.IndexFunc(ps, func(p store.Pass) bool { return p.ID == want.ID })
		if i < 0 {
			t.Fatalf("pass %s absent", want.ID)
		}
		contractEqual(t, ps[i], want)
	}
	ps, err := f.s.RunningPasses(t.Context())
	contractOK(t, err)
	i := slices.IndexFunc(ps, func(p store.Pass) bool { return p.ID == want.ID })
	if want.Status == store.StatusRunning {
		if i < 0 {
			t.Fatal("running pass absent")
		}
		contractEqual(t, ps[i], want)
	} else if i >= 0 {
		t.Fatal("ended pass in running passes")
	}
}

// R-CHQV-NX90 R-CMMH-707S R-CTXV-HMNY R-D01D-EHDF
func TestTargetPassClaimsAndCanonicalRecord(t *testing.T) {
	t.Parallel()
	f := contractDB(t)
	ctx := t.Context()
	ids := contractAttach(t, f, "claims", contractObservation("a", "Alpha", f.now), contractObservation("b", "Bravo", f.now), contractObservation("c", "Charlie", f.now), contractObservation("d", "Delta", f.now), contractObservation("m", "Merged", f.now))
	a, b, c, d, m := ids[0], ids[1], ids[2], ids[3], ids[4]
	_, _, err := f.s.Decide(ctx, store.Ruling{Kind: store.KindMerge, Candidate: m, Other: b, Why: "merge evidence", UserID: "owner"})
	contractOK(t, err)
	// Determine which side the merge kept, without assuming the ruling's direction.
	merged, err := f.s.GetCandidate(ctx, m)
	contractOK(t, err)
	if merged.Status == store.StatusActive {
		m, b = b, m
	}
	owner, claimed, err := f.s.AddTargetPass(ctx, targetedPass(c, sources.WebsiteCheck), []string{c})
	contractOK(t, err)
	contractEqual(t, claimed, []string{c})
	for _, id := range []string{a, b, d, "unknown"} {
		got, e := f.s.RunningTargetPass(ctx, id)
		contractOK(t, e)
		contractEqual(t, got, "")
	}
	given := []string{a, a, b, m, "unknown", c}
	p := targetPassIDs(given...)
	p.ID = "ignored"
	p.Status = "ignored"
	p.Reason = "ignored"
	p.ParcelRun = "ignored"
	p.Started = f.now.Add(-time.Hour)
	p.Finished = f.now
	p.Rows, p.New, p.Changed, p.Unchanged, p.Gone, p.Created, p.Linked, p.Judged, p.Unranged = 99, 99, 99, 99, 99, 99, 99, 99, 99
	p.Derived, p.Due, p.Fetched, p.HTTPS, p.HTTP, p.None, p.Runs, p.RunsFailed, p.Pages, p.Cost = 99, 99, 99, 99, 99, 99, 99, 99, 99, 99
	p.ParcelsRead, p.ByLabel, p.ByValue, p.ByCandidate, p.ParksCreated, p.ParksUpdated, p.ParksGone, p.ParksGrown, p.ParksWalked, p.Circles, p.Requests, p.Found, p.NewCandidates, p.SearchDifferent, p.SearchStands, p.SearchNone = 99, 99, 99, 99, 99, 99, 99, 99, 99, 99, 99, 99, 99, 99, 99, 99
	p.CeilingReached = true
	p.RunningKind = "ignored"
	p.Busy = 99
	p.Unmatched = 99
	p.KindEntries = []store.KindEntry{{Kind: "ignored", Cost: 99}}
	got, claimed, err := f.s.AddTargetPass(ctx, p, given)
	contractOK(t, err)
	contractEqual(t, claimed, []string{a, b})
	want := targetPassIDs(given...)
	want.ID = got.ID
	want.Status = store.StatusRunning
	want.Started = f.now.UTC().Truncate(time.Second)
	want.Busy = 1
	want.Unmatched = 2
	if !store.ValidPassID(got.ID) || got.ID == owner.ID {
		t.Fatal(got.ID)
	}
	contractEqual(t, got, want)
	// The caller's slice cannot change the stored target after the call.
	p.TargetIDs[0] = "changed"
	want.TargetIDs = slices.Clone(given)
	want.TargetIDs[0] = a
	assertPassReads(t, f, want)
	f.reopen()
	assertPassReads(t, f, want)
	for _, id := range []string{a, b} {
		claim, e := f.s.RunningTargetPass(ctx, id)
		contractOK(t, e)
		contractEqual(t, claim, want.ID)
	}
	next, claimed, err := f.s.AddTargetPass(ctx, targetedPass(a, ""), []string{a, d, a, d})
	contractOK(t, err)
	contractEqual(t, claimed, []string{d})
	contractEqual(t, next.Busy, int64(1))
	contractEqual(t, next.Unmatched, int64(0))
	_, err = f.s.FinishPass(ctx, want.ID, store.Counts{})
	contractOK(t, err)
	for _, id := range []string{a, b} {
		claim, e := f.s.RunningTargetPass(ctx, id)
		contractOK(t, e)
		contractEqual(t, claim, "")
	}
	again, claimed, err := f.s.AddTargetPass(ctx, targetedPass(a, ""), []string{a})
	contractOK(t, err)
	contractEqual(t, claimed, []string{a})
	_, err = f.s.FailPass(ctx, again.ID, store.ReasonInterrupted)
	contractOK(t, err)
	claim, err := f.s.RunningTargetPass(ctx, a)
	contractOK(t, err)
	contractEqual(t, claim, "")
}

// R-CHQV-NX90 R-CNUD-KRYH R-CLEK-T8H3
func TestTargetPassValidationAndOrdinaryRefusal(t *testing.T) {
	t.Parallel()
	f := contractDB(t)
	ctx := t.Context()
	rnd := f.rnd.(*contractRandom)
	base := targetedPass(store.CandidateIDPrefix+"0123456789abcdef", "")
	valid := []store.Pass{base, targetedPass(store.ProspectListIDPrefix+"0123456789abcdef", ""), targetPassIDs("arbitrary", "", "arbitrary")}
	for _, kind := range enrichers.Kinds() {
		p := base
		p.EnrichKind = kind
		valid = append(valid, p)
	}
	for _, p := range valid {
		out, err := f.s.AddPass(ctx, p)
		contractError(t, err, store.ErrUnreachable)
		contractEqual(t, out, store.Pass{})
		got, claimed, err := f.s.AddTargetPass(ctx, p, nil)
		contractOK(t, err)
		contractEqual(t, claimed, []string{})
		contractEqual(t, got.Target, p.Target)
		contractEqual(t, got.TargetIDs, p.TargetIDs)
		contractEqual(t, got.EnrichKind, p.EnrichKind)
	}
	invalid := []store.Pass{{}, enrichPass(), targetedPass("", ""), targetedPass("invalid", "")}
	for _, mutate := range []func(*store.Pass){func(p *store.Pass) { p.TargetIDs = []string{"both"} }, func(p *store.Pass) { p.Kind = store.KindEnrich }, func(p *store.Pass) { p.Origin = store.OriginPass }, func(p *store.Pass) { p.Trigger = "other" }, func(p *store.Pass) { p.Trigger = "" }, func(p *store.Pass) { p.Scraper = "fmcsa" }, func(p *store.Pass) { p.Delivery = "delivery" }, func(p *store.Pass) { p.UserID = "" }, func(p *store.Pass) { p.EnrichKind = "unknown" }} {
		p := base
		mutate(&p)
		invalid = append(invalid, p)
	}
	before, err := f.s.ListPasses(ctx, "", "", 100)
	contractOK(t, err)
	reads := rnd.total
	for _, p := range invalid {
		got, claimed, e := f.s.AddTargetPass(ctx, p, []string{"unknown"})
		contractError(t, e, store.ErrUnreachable)
		contractEqual(t, got, store.Pass{})
		if claimed != nil {
			t.Fatal(claimed)
		}
	}
	contractEqual(t, rnd.total, reads)
	after, err := f.s.ListPasses(ctx, "", "", 100)
	contractOK(t, err)
	contractEqual(t, after, before)
}

// R-CQA6-CBFV R-CRI2-Q36K R-CV5R-VEEN R-D01D-EHDF
func TestTargetPassKindsPersistAndFailureKeepsProgress(t *testing.T) {
	t.Parallel()
	f := contractDB(t)
	ctx := t.Context()
	id := contractSeed(t, f)
	p, _, err := f.s.AddTargetPass(ctx, targetPassIDs(id, "unknown"), []string{id, "unknown"})
	contractOK(t, err)
	other, err := f.s.AddPass(ctx, enrichPass())
	contractOK(t, err)
	for i, kind := range []string{enrichers.KindWebsiteSearch, enrichers.KindSiteText} {
		want := p
		want.RunningKind = kind
		p, err = f.s.StartPassKind(ctx, p.ID, kind)
		contractOK(t, err)
		contractEqual(t, p, want)
		f.reopen()
		assertPassReads(t, f, p)
		entry := store.KindEntry{Kind: kind, NewResults: int64(i + 1), NotDue: 2, NotApplicable: 3, Failed: 4, Cost: 123456789}
		want.KindEntries = append(slices.Clone(want.KindEntries), entry)
		want.RunningKind = ""
		p, err = f.s.EndPassKind(ctx, p.ID, entry)
		contractOK(t, err)
		contractEqual(t, p, want)
		f.reopen()
		assertPassReads(t, f, p)
	}
	p, err = f.s.StartPassKind(ctx, p.ID, enrichers.KindBrief)
	contractOK(t, err)
	before := contractState(t, f)
	f.now = f.now.Add(time.Hour)
	want := p
	want.Status = store.StatusFailed
	want.Reason = store.ReasonPrompts
	want.Finished = f.now.UTC().Truncate(time.Second)
	want.RunningKind = ""
	p, err = f.s.FailPass(ctx, p.ID, store.ReasonPrompts)
	contractOK(t, err)
	contractEqual(t, p, want)
	after := contractState(t, f)
	before.passes = slices.DeleteFunc(before.passes, func(q store.Pass) bool { return q.ID == p.ID })
	after.passes = slices.DeleteFunc(after.passes, func(q store.Pass) bool { return q.ID == p.ID })
	contractEqual(t, after, before)
	assertPassReads(t, f, other)
	f.reopen()
	assertPassReads(t, f, p)
	claim, err := f.s.RunningTargetPass(ctx, id)
	contractOK(t, err)
	contractEqual(t, claim, "")
}

// R-CSPZ-3UX9
func TestTargetPassKindRefusalsPreserveRecords(t *testing.T) {
	t.Parallel()
	f := contractDB(t)
	ctx := t.Context()
	p, _, err := f.s.AddTargetPass(ctx, targetPassIDs("unknown"), nil)
	contractOK(t, err)
	for _, negative := range []store.KindEntry{{NewResults: -1}, {NotDue: -1}, {NotApplicable: -1}, {Failed: -1}, {Cost: -1}} {
		for _, id := range []string{"missing", p.ID} {
			out, e := f.s.EndPassKind(ctx, id, negative)
			contractError(t, e, store.ErrUnreachable)
			contractEqual(t, out, store.Pass{})
		}
	}
	out, err := f.s.StartPassKind(ctx, "missing", "anything")
	contractError(t, err, store.ErrNotFound)
	contractEqual(t, out, store.Pass{})
	out, err = f.s.EndPassKind(ctx, "missing", store.KindEntry{})
	contractError(t, err, store.ErrNotFound)
	contractEqual(t, out, store.Pass{})
	for range 5 {
		p, err = f.s.EndPassKind(ctx, p.ID, store.KindEntry{})
		contractOK(t, err)
	}
	before := p
	out, err = f.s.EndPassKind(ctx, p.ID, store.KindEntry{})
	contractError(t, err, store.ErrUnreachable)
	contractEqual(t, out, store.Pass{})
	assertPassReads(t, f, before)
	p, err = f.s.FinishPass(ctx, p.ID, store.Counts{})
	contractOK(t, err)
	out, err = f.s.StartPassKind(ctx, p.ID, "anything")
	contractError(t, err, store.ErrEnded)
	contractEqual(t, out, store.Pass{})
	out, err = f.s.EndPassKind(ctx, p.ID, store.KindEntry{})
	contractError(t, err, store.ErrEnded)
	contractEqual(t, out, store.Pass{})
	out, err = f.s.EndPassKind(ctx, p.ID, store.KindEntry{Cost: -1})
	contractError(t, err, store.ErrUnreachable)
	contractEqual(t, out, store.Pass{})
	f.reopen()
	assertPassReads(t, f, p)
}

// R-D01D-EHDF
func TestEveryPassEndClearsRunningKind(t *testing.T) {
	t.Parallel()
	for _, ending := range []string{"finish", "fail", "parks", "finish_walk", "fail_walk"} {
		t.Run(ending, func(t *testing.T) {
			t.Parallel()
			f := contractDB(t)
			ctx := t.Context()
			input := enrichPass()
			if ending == "parks" {
				input = parkPass(store.OriginStart)
			}
			if ending == "finish_walk" || ending == "fail_walk" {
				input = baseWalkPass()
			}
			p, err := f.s.AddPass(ctx, input)
			contractOK(t, err)
			p, err = f.s.StartPassKind(ctx, p.ID, "in progress")
			contractOK(t, err)
			p, err = f.s.EndPassKind(ctx, p.ID, store.KindEntry{Kind: "finished", NewResults: 1})
			contractOK(t, err)
			p, err = f.s.StartPassKind(ctx, p.ID, "still in progress")
			contractOK(t, err)
			switch ending {
			case "finish":
				p, err = f.s.FinishPass(ctx, p.ID, store.Counts{Cost: 4})
			case "fail":
				p, err = f.s.FailPass(ctx, p.ID, store.ReasonStore)
			case "parks":
				p, err = f.s.ApplyParks(ctx, p.ID, "parcel-run", nil, parks.Plan{}, store.Counts{})
			case "finish_walk":
				p, err = f.s.FinishWalk(ctx, p.ID, store.Counts{}, false)
			case "fail_walk":
				p, err = f.s.FailWalk(ctx, p.ID, store.ReasonPlaces, store.Counts{})
			}
			contractOK(t, err)
			contractEqual(t, p.RunningKind, "")
			contractEqual(t, p.KindEntries, []store.KindEntry{{Kind: "finished", NewResults: 1}})
			assertPassReads(t, f, p)
			f.reopen()
			assertPassReads(t, f, p)
		})
	}
}

// R-CYTH-0PMQ
func TestConcurrentTargetClaimsAndOrdinaryPasses(t *testing.T) {
	t.Parallel()
	f := contractDB(t)
	ctx := t.Context()
	id := contractSeed(t, f)
	type result struct {
		p   store.Pass
		ids []string
		err error
	}
	results := make(chan result, 8)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			<-start
			p, ids, e := f.s.AddTargetPass(ctx, targetPassIDs(id), []string{id})
			results <- result{p, ids, e}
		})
	}
	close(start)
	wg.Wait()
	close(results)
	claimed := 0
	for r := range results {
		contractOK(t, r.err)
		contractEqual(t, r.p.Unmatched, int64(0))
		if len(r.ids) > 0 {
			contractEqual(t, r.ids, []string{id})
			contractEqual(t, r.p.Busy, int64(0))
			claimed++
		} else {
			contractEqual(t, r.ids, []string{})
			contractEqual(t, r.p.Busy, int64(1))
		}
	}
	contractEqual(t, claimed, 1)
	start = make(chan struct{})
	errs := make(chan error, 3)
	for _, p := range []store.Pass{enrichPass(), {Kind: store.KindIngest, Origin: store.OriginIngest, Trigger: store.TriggerIngest, UserID: "owner"}} {
		wg.Go(func() { <-start; _, e := f.s.AddPass(ctx, p); errs <- e })
	}
	wg.Go(func() { <-start; _, _, e := f.s.AddTargetPass(ctx, targetPassIDs(id), []string{id}); errs <- e })
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		contractOK(t, err)
	}
}

// R-CYTH-0PMQ
func TestTargetClaimRaceWithEndAndKindFailure(t *testing.T) {
	t.Parallel()
	for _, ending := range []string{"finish", "fail"} {
		t.Run(ending, func(t *testing.T) {
			t.Parallel()
			f := contractDB(t)
			ctx := t.Context()
			id := contractSeed(t, f)
			p, _, err := f.s.AddTargetPass(ctx, targetPassIDs(id), []string{id})
			contractOK(t, err)
			start := make(chan struct{})
			var wg sync.WaitGroup
			var next store.Pass
			var claimed []string
			var addErr, endErr error
			wg.Go(func() { <-start; next, claimed, addErr = f.s.AddTargetPass(ctx, targetPassIDs(id), []string{id}) })
			wg.Go(func() {
				<-start
				if ending == "finish" {
					_, endErr = f.s.FinishPass(ctx, p.ID, store.Counts{})
				} else {
					_, endErr = f.s.FailPass(ctx, p.ID, store.ReasonStore)
				}
			})
			close(start)
			wg.Wait()
			contractOK(t, addErr)
			contractOK(t, endErr)
			if len(claimed) == 0 {
				contractEqual(t, next.Busy, int64(1))
			} else {
				contractEqual(t, claimed, []string{id})
				contractEqual(t, next.Busy, int64(0))
			}
			owner, e := f.s.RunningTargetPass(ctx, id)
			contractOK(t, e)
			want := ""
			if len(claimed) > 0 {
				want = next.ID
			}
			contractEqual(t, owner, want)
		})
	}
	f := contractDB(t)
	ctx := t.Context()
	p, _, err := f.s.AddTargetPass(ctx, targetPassIDs("unknown"), nil)
	contractOK(t, err)
	start := make(chan struct{})
	var wg sync.WaitGroup
	var endErr, failErr error
	k := store.KindEntry{Kind: "kind", NewResults: 2}
	wg.Go(func() { <-start; _, endErr = f.s.EndPassKind(ctx, p.ID, k) })
	wg.Go(func() { <-start; _, failErr = f.s.FailPass(ctx, p.ID, store.ReasonStore) })
	close(start)
	wg.Wait()
	contractOK(t, failErr)
	got, err := f.s.GetPass(ctx, p.ID)
	contractOK(t, err)
	if endErr == nil {
		contractEqual(t, got.KindEntries, []store.KindEntry{k})
	} else {
		contractError(t, endErr, store.ErrEnded)
		contractEqual(t, got.KindEntries, []store.KindEntry(nil))
	}
}

// R-D199-S944 R-CYTH-0PMQ
func TestPruneRemovesOnlyItsPassKindsAndClaims(t *testing.T) {
	t.Parallel()
	f := contractDB(t)
	ctx := t.Context()
	id := contractSeed(t, f)
	var ps []store.Pass
	for i := range 3 {
		claimIDs := []string{id}
		if i == 2 {
			claimIDs = nil
		}
		p, _, err := f.s.AddTargetPass(ctx, targetPassIDs(id), claimIDs)
		contractOK(t, err)
		p, err = f.s.EndPassKind(ctx, p.ID, store.KindEntry{Kind: fmt.Sprint(i), NewResults: int64(i + 1)})
		contractOK(t, err)
		if i < 2 {
			p, err = f.s.FinishPass(ctx, p.ID, store.Counts{})
			contractOK(t, err)
		}
		ps = append(ps, p)
		f.now = f.now.Add(time.Second)
	}
	read := func() map[string]map[string][]any {
		t.Helper()
		out := map[string]map[string][]any{}
		contractOK(t, f.d.Read(ctx, func(tx *sql.Tx) error {
			for _, spec := range []struct{ table, query string }{{"pass_kinds", "SELECT pass,seq,kind,new_results,not_due,not_applicable,failed,cost FROM pass_kinds"}, {"pass_claims", "SELECT pass,candidate FROM pass_claims"}} {
				table, query := spec.table, spec.query
				out[table] = map[string][]any{}
				rows, e := tx.QueryContext(ctx, query)
				if e != nil {
					return e
				}
				columns, e := rows.Columns()
				if e != nil {
					_ = rows.Close()
					return e
				}
				for rows.Next() {
					values := make([]any, len(columns))
					ptrs := make([]any, len(columns))
					for i := range values {
						ptrs[i] = &values[i]
					}
					if e = rows.Scan(ptrs...); e != nil {
						_ = rows.Close()
						return e
					}
					out[table][values[0].(string)] = values
				}
				e = rows.Err()
				ce := rows.Close()
				if e != nil {
					return e
				}
				if ce != nil {
					return ce
				}
			}
			return nil
		}))
		return out
	}
	before := read()
	start := make(chan struct{})
	var wg sync.WaitGroup
	var n int64
	var pruneErr, addErr error
	var next store.Pass
	var claimed []string
	wg.Go(func() { <-start; n, pruneErr = f.s.PrunePasses(ctx, 1, 100) })
	wg.Go(func() { <-start; next, claimed, addErr = f.s.AddTargetPass(ctx, targetPassIDs(id), []string{id}) })
	close(start)
	wg.Wait()
	contractOK(t, pruneErr)
	contractOK(t, addErr)
	contractEqual(t, n, int64(1))
	contractEqual(t, claimed, []string{id})
	contractEqual(t, next.Busy, int64(0))
	after := read()
	for _, table := range []string{"pass_kinds", "pass_claims"} {
		delete(before[table], ps[0].ID)
		if table == "pass_claims" {
			before[table][next.ID] = []any{next.ID, id}
		}
		contractEqual(t, after[table], before[table])
	}
	owner, err := f.s.RunningTargetPass(ctx, id)
	contractOK(t, err)
	contractEqual(t, owner, next.ID)
}
