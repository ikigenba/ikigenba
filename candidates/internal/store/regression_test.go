package store_test

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"reflect"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	candidates "github.com/ikigenba/ikigenba/candidates"
	"github.com/ikigenba/ikigenba/candidates/internal/scoring"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

var regCtx = context.Background()

type regRandom struct{ next byte }

func (r *regRandom) Read(p []byte) (int, error) {
	for i := range p {
		r.next++
		p[i] = r.next
	}
	return len(p), nil
}

type regFixture struct {
	st     *store.Store
	d      *db.DB
	path   string
	now    time.Time
	random io.Reader
	calls  int
}

func regNew(t *testing.T) *regFixture {
	t.Helper()
	f := &regFixture{path: filepath.Join(t.TempDir(), "own.db"), now: time.Date(2026, 10, 12, 12, 34, 56, 123, time.FixedZone("test", -5*3600)), random: &regRandom{}}
	f.open(t)
	return f
}
func (f *regFixture) open(t *testing.T) {
	t.Helper()
	d, err := db.Open(regCtx, db.Config{Path: f.path, Migrations: candidates.Migrations(), Now: func() time.Time { return f.now }})
	if err != nil {
		t.Fatal(err)
	}
	f.d = d
	t.Cleanup(func() {
		if err := d.Close(); err != nil {
			t.Error(err)
		}
	})
	f.st = store.New(d, store.Config{Now: func() time.Time { f.calls++; return f.now }, Rand: f.random})
}
func (f *regFixture) reopen(t *testing.T) {
	t.Helper()
	if err := f.d.Close(); err != nil {
		t.Fatal(err)
	}
	f.open(t)
}
func regEqual[T any](t *testing.T, got, want T) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}
func regNoErr(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func regReading(name string) sources.Reading {
	return sources.Reading{NameKey: sources.NameKey(name), Name: name, Names: []string{}, State: "MN", Zip: "55072", People: []sources.Person{}, Class: sources.ClassForHire, Cargo: []string{"genfreight"}, Exclusions: []sources.Exclusion{}, Filed: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), Fleet: true}
}
func regObs(id, name string) store.Observation {
	return store.Observation{Scraper: "fmcsa", SourceID: id, Digest: "digest-first", Reading: regReading(name)}
}
func regAttach(t *testing.T, f *regFixture, run string, obs ...store.Observation) (store.Counts, []string) {
	t.Helper()
	n, ids, err := f.st.Attach(regCtx, run, false, obs)
	regNoErr(t, err)
	return n, ids
}
func regCandidate(t *testing.T, f *regFixture, id string) store.Candidate {
	t.Helper()
	c, err := f.st.GetCandidate(regCtx, id)
	regNoErr(t, err)
	return c
}
func regLinks(t *testing.T, f *regFixture, id string) []store.Link {
	t.Helper()
	ls, err := f.st.Links(regCtx, id)
	regNoErr(t, err)
	return ls
}
func regTime(f *regFixture) time.Time { return f.now.UTC().Truncate(time.Second) }

// R-G7UR-R6WW R-GGE2-FL3R R-GF66-1TD2 R-GHLY-TCUG
func TestResolutionIdentityAndBatch(t *testing.T) {
	f := regNew(t)
	a := regObs("first", "Acme Trucking Inc")
	b := regObs("second", "Acme Trucking LLC")
	f.calls = 0
	n, ids := regAttach(t, f, "run-one", a, b)
	regEqual(t, f.calls, 1)
	regEqual(t, n, store.Counts{Rows: 2, New: 2, Created: 1, Linked: 1})
	if len(ids) != 1 {
		t.Fatal(ids)
	}
	c := regCandidate(t, f, ids[0])
	if !store.ValidCandidateID(c.ID) {
		t.Fatal(c.ID)
	}
	regEqual(t, c.Status, store.StatusActive)
	regEqual(t, c.MergedInto, "")
	regEqual(t, c.Created, regTime(f))
	regEqual(t, c.Updated, regTime(f))
	ls := regLinks(t, f, c.ID)
	regEqual(t, len(ls), 2)
	regEqual(t, ls[0], store.Link{Candidate: c.ID, Scraper: a.Scraper, SourceID: a.SourceID, Run: "run-one", Digest: a.Digest, Reading: a.Reading, Reason: store.LinkSourceKey, Confidence: 1, FirstSeen: regTime(f), LastSeen: regTime(f)})
	regEqual(t, ls[1], store.Link{Candidate: c.ID, Scraper: b.Scraper, SourceID: b.SourceID, Run: "run-one", Digest: b.Digest, Reading: b.Reading, Reason: store.LinkMatchKey, Confidence: 0.8, FirstSeen: regTime(f), LastSeen: regTime(f)})
	for _, tc := range []struct{ name, state, zip string }{{"", "MN", "55072"}, {"Same", "", "55072"}, {"Same", "MN", "5507"}, {"Same", "MN", "5507x"}, {"Same", "MN", "550720"}} {
		x := regObs("invalid-a"+tc.name+tc.state+tc.zip, tc.name)
		x.Reading.State = tc.state
		x.Reading.Zip = tc.zip
		y := x
		y.SourceID += "second"
		n, ids := regAttach(t, f, "run-two", x, y)
		regEqual(t, n, store.Counts{Rows: 2, New: 2, Created: 2})
		regEqual(t, len(ids), 2)
	}
	n, ids = regAttach(t, f, "run-empty")
	regEqual(t, n, store.Counts{})
	if ids == nil || len(ids) != 0 {
		t.Fatal(ids)
	}
	f.reopen(t)
	regEqual(t, regLinks(t, f, c.ID), ls)
}

// R-GBIG-WI4Z R-GHLY-TCUG
func TestResolutionRefusesWholeInvalidBatch(t *testing.T) {
	f := regNew(t)
	for _, tc := range []struct {
		run string
		obs []store.Observation
	}{{"", []store.Observation{regObs("one", "Name")}}, {"run", []store.Observation{regObs("one", "Name"), {Scraper: "fmcsa"}}}, {"run", []store.Observation{{SourceID: "one"}}}} {
		n, ids, err := f.st.Attach(regCtx, tc.run, false, tc.obs)
		regEqual(t, n, store.Counts{})
		if ids != nil || !errors.Is(err, store.ErrUnreachable) {
			t.Fatal(ids, err)
		}
		cs, e := f.st.ListCandidates(regCtx, store.CandidateFilter{}, 0, 100)
		regNoErr(t, e)
		regEqual(t, len(cs), 0)
	}
	f.random = io.LimitReader(&regRandom{}, 8)
	f.st = store.New(f.d, store.Config{Now: func() time.Time { f.calls++; return f.now }, Rand: f.random})
	n, ids, err := f.st.Attach(regCtx, "run", false, []store.Observation{regObs("one", "A"), regObs("two", "B")})
	regEqual(t, n, store.Counts{})
	if ids != nil || !errors.Is(err, store.ErrUnreachable) {
		t.Fatal(ids, err)
	}
	cs, e := f.st.ListCandidates(regCtx, store.CandidateFilter{}, 0, 100)
	regNoErr(t, e)
	regEqual(t, len(cs), 0)
}

// R-GCQD-A9VO R-GDY9-O1MD R-GAAK-IQEA
func TestResolutionRefreshAndRebuild(t *testing.T) {
	f := regNew(t)
	a := regObs("one", "First")
	_, ids := regAttach(t, f, "before", a)
	old := regLinks(t, f, ids[0])[0]
	oldCandidate := regCandidate(t, f, ids[0])
	f.now = f.now.Add(time.Hour)
	a.Reading.Name = "Ignored"
	a.Reading.NameKey = "OTHER"
	n, seen := regAttach(t, f, "next", a)
	regEqual(t, n, store.Counts{Rows: 1, Unchanged: 1})
	regEqual(t, seen, ids)
	want := old
	want.Run = "next"
	want.LastSeen = regTime(f)
	regEqual(t, regLinks(t, f, ids[0])[0], want)
	regEqual(t, regCandidate(t, f, ids[0]), oldCandidate)
	a.Digest = "different"
	n, seen = regAttach(t, f, "changed", a)
	regEqual(t, n, store.Counts{Rows: 1, Changed: 1, Linked: 1})
	regEqual(t, seen, ids)
	want.Digest = a.Digest
	want.Reading = a.Reading
	want.Run = "changed"
	regEqual(t, regLinks(t, f, ids[0])[0], want)
	regEqual(t, regCandidate(t, f, ids[0]).Name, a.Reading.Name)
	regEqual(t, regCandidate(t, f, ids[0]).Updated, regTime(f))
	a.Reading.Name = "Rebuilt"
	n, seen, e := f.st.Attach(regCtx, "rebuild", true, []store.Observation{a})
	regNoErr(t, e)
	regEqual(t, n, store.Counts{Rows: 1, Changed: 1, Linked: 1})
	regEqual(t, seen, ids)
	want.Reading = a.Reading
	want.Run = "rebuild"
	regEqual(t, regLinks(t, f, ids[0])[0], want)
}

// R-WAZL-PLV0 R-GAAK-IQEA R-GITV-74L5
func TestResolutionDisplayAndGone(t *testing.T) {
	f := regNew(t)
	a := regObs("older", "Acme INC")
	a.Reading.Names = []string{"Alias", "", a.Reading.Name}
	a.Reading.Cargo = []string{"z", "a"}
	a.Reading.Phone = "old"
	b := regObs("newer", "Acme LLC")
	b.Reading.Filed = a.Reading.Filed.AddDate(0, 1, 0)
	b.Reading.Names = []string{"Alias", "Other"}
	b.Reading.Cargo = []string{"b", "a"}
	b.Reading.People = []sources.Person{{Name: "Owner", Role: "chief"}}
	b.Reading.Street = "Newest street"
	b.Reading.City = "City"
	b.Reading.County = "MN-115"
	b.Reading.Phone = "new"
	b.Reading.Email = "new@company.example"
	b.Reading.Class = sources.ClassPrivate
	_, ids := regAttach(t, f, "old-run", a)
	f.now = f.now.Add(time.Hour)
	regAttach(t, f, "new-run", b)
	id := ids[0]
	c := regCandidate(t, f, id)
	regEqual(t, c, store.Candidate{ID: id, Status: store.StatusActive, Name: b.Reading.Name, Names: []string{b.Reading.Name, "Alias", "Other", a.Reading.Name}, Street: b.Reading.Street, City: b.Reading.City, State: b.Reading.State, Zip: b.Reading.Zip, County: b.Reading.County, Phone: b.Reading.Phone, Email: b.Reading.Email, People: b.Reading.People, Class: b.Reading.Class, Cargo: []string{"a", "b", "z"}, Created: c.Created, Updated: regTime(f)})
	before := regLinks(t, f, id)
	f.now = f.now.Add(time.Hour)
	n, gone, e := f.st.MarkGone(regCtx, "fmcsa", "old-run")
	regNoErr(t, e)
	regEqual(t, n, store.Counts{Gone: 1})
	regEqual(t, gone, ids)
	after := regLinks(t, f, id)
	regEqual(t, after[0], before[1])
	want := before[0]
	want.Gone = true
	regEqual(t, after[1], want)
	regEqual(t, regCandidate(t, f, id).Name, a.Reading.Name)
	regEqual(t, regCandidate(t, f, id).Updated, regTime(f))
	n, gone, e = f.st.MarkGone(regCtx, "fmcsa", "no-row-run")
	regNoErr(t, e)
	regEqual(t, n, store.Counts{Gone: 1})
	regEqual(t, gone, ids)
	regEqual(t, regCandidate(t, f, id).Name, b.Reading.Name)
	n, gone, e = f.st.MarkGone(regCtx, "fmcsa", "no-row-run")
	regNoErr(t, e)
	regEqual(t, n, store.Counts{})
	if gone == nil || len(gone) != 0 {
		t.Fatal(gone)
	}
	for _, x := range []struct{ s, r string }{{"", "run"}, {"fmcsa", ""}} {
		n, ids, err := f.st.MarkGone(regCtx, x.s, x.r)
		regEqual(t, n, store.Counts{})
		if ids != nil || !errors.Is(err, store.ErrUnreachable) {
			t.Fatal(ids, err)
		}
	}
}

// R-GL9N-YO2J R-GMHK-CFT8
func TestResolutionRulingRefusalOrder(t *testing.T) {
	f := regNew(t)
	_, ids := regAttach(t, f, "run", regObs("one", "First"), regObs("two", "Second"))
	id := ids[0]
	cases := []struct {
		r    store.Ruling
		want error
	}{{store.Ruling{}, store.ErrUnreachable}, {store.Ruling{UserID: "user"}, store.ErrInvalid}, {store.Ruling{Kind: store.KindExclude, Candidate: "missing", Why: " ", UserID: "user"}, store.ErrInvalid}, {store.Ruling{Kind: store.KindMerge, Candidate: id, Why: "why", UserID: "user"}, store.ErrInvalid}, {store.Ruling{Kind: store.KindSplit, Candidate: id, Why: "why", UserID: "user"}, store.ErrInvalid}, {store.Ruling{Kind: store.KindExclude, Candidate: "missing", Why: "why", UserID: "user"}, store.ErrNotFound}, {store.Ruling{Kind: store.KindMerge, Candidate: id, Other: "missing", Why: "why", UserID: "user"}, store.ErrNotFound}, {store.Ruling{Kind: store.KindMerge, Candidate: id, Other: id, Why: "why", UserID: "user"}, store.ErrInvalid}, {store.Ruling{Kind: store.KindNotSame, Candidate: id, Other: id, Why: "why", UserID: "user"}, store.ErrInvalid}, {store.Ruling{Kind: store.KindSplit, Candidate: id, Keys: []string{"fmcsa:missing"}, Why: "why", UserID: "user"}, store.ErrInvalid}}
	for _, tc := range cases {
		r, cs, e := f.st.Decide(regCtx, tc.r)
		regEqual(t, r, store.Ruling{})
		if cs != nil || !errors.Is(e, tc.want) {
			t.Fatal(cs, e, tc.want)
		}
	}
	ls := regLinks(t, f, id)
	_, _, e := f.st.Decide(regCtx, store.Ruling{Kind: store.KindSplit, Candidate: id, Keys: []string{sources.SourceKey(ls[0].Scraper, ls[0].SourceID)}, Why: "why", UserID: "user"})
	if !errors.Is(e, store.ErrInvalid) {
		t.Fatal(e)
	}
	rs, e := f.st.Rulings(regCtx, id)
	regNoErr(t, e)
	regEqual(t, len(rs), 0)
}

// R-GNPG-Q7JX R-GRD5-VIS0 R-GL9N-YO2J R-GK1R-KWBU
func TestResolutionSplitAndPersistence(t *testing.T) {
	f := regNew(t)
	a := regObs("one", "Acme INC")
	b := regObs("two", "Acme LLC")
	c := regObs("three", "Acme CO")
	_, ids := regAttach(t, f, "run", a, b, c)
	id := ids[0]
	before := regLinks(t, f, id)
	f.now = f.now.Add(time.Hour)
	keys := []string{sources.SourceKey(b.Scraper, b.SourceID), sources.SourceKey(c.Scraper, c.SourceID)}
	r, cs, e := f.st.Decide(regCtx, store.Ruling{Kind: store.KindSplit, Candidate: id, Other: "ignored", Keys: []string{keys[1], keys[0], keys[0]}, Why: "reason", UserID: "owner", UserEmail: "owner@example.test", At: f.now.Add(-time.Hour)})
	regNoErr(t, e)
	regEqual(t, len(cs), 2)
	if !store.ValidCandidateID(r.Other) || r.Other == id {
		t.Fatal(r)
	}
	sort.Strings(keys)
	regEqual(t, r, store.Ruling{Kind: store.KindSplit, Candidate: id, Other: cs[1].ID, Keys: keys, Why: "reason", UserID: "owner", UserEmail: "owner@example.test", At: regTime(f)})
	regEqual(t, cs[0], regCandidate(t, f, id))
	regEqual(t, cs[1], regCandidate(t, f, r.Other))
	regEqual(t, cs[1].Status, store.StatusActive)
	regEqual(t, cs[1].Created, regTime(f))
	regEqual(t, regLinks(t, f, id), []store.Link{before[0]})
	moved := regLinks(t, f, r.Other)
	for i, l := range moved {
		want := before[i+1]
		want.Candidate = r.Other
		want.Reason = store.LinkDecision
		want.Confidence = 1
		regEqual(t, l, want)
	}
	pair := store.Ruling{Kind: store.KindNotSame, Candidate: id, Other: r.Other, Keys: []string{}, Why: r.Why, UserID: r.UserID, UserEmail: r.UserEmail, At: r.At}
	for _, cid := range []string{id, r.Other} {
		rs, e := f.st.Rulings(regCtx, cid)
		regNoErr(t, e)
		regEqual(t, rs, []store.Ruling{pair, r})
	}
	f.reopen(t)
	rs, e := f.st.Rulings(regCtx, r.Other)
	regNoErr(t, e)
	regEqual(t, rs, []store.Ruling{pair, r})
	// Identity evidence remains authoritative even after the split's not_same ruling.
	f.now = f.now.Add(time.Hour)
	_, seen := regAttach(t, f, "later", regObs("four", "Acme LTD"))
	regEqual(t, seen, []string{id})
	_, _, e = f.st.Attach(regCtx, "rebuild", true, []store.Observation{a, b, c})
	regNoErr(t, e)
	_, _, e = f.st.MarkGone(regCtx, "fmcsa", "rebuild")
	regNoErr(t, e)
	_, e = f.st.RescoreAll(regCtx)
	regNoErr(t, e)
	for _, l := range regLinks(t, f, r.Other) {
		if l.Candidate != r.Other || l.Reason != store.LinkDecision || l.Confidence != 1 {
			t.Fatal(l)
		}
	}
	rs, e = f.st.Rulings(regCtx, id)
	regNoErr(t, e)
	regEqual(t, rs, []store.Ruling{pair, r})
}

// R-WC7I-3DLP R-GK1R-KWBU R-GSL2-9AIP R-GL9N-YO2J R-GMHK-CFT8
func TestResolutionMergeAndNotSame(t *testing.T) {
	f := regNew(t)
	a := regObs("one", "First")
	b := regObs("two", "Second")
	_, ids := regAttach(t, f, "run", a, b)
	ca := regCandidate(t, f, ids[0])
	cb := regCandidate(t, f, ids[1])
	_, e := f.st.RescoreAll(regCtx)
	regNoErr(t, e)
	oldAssessment, e := f.st.GetAssessment(regCtx, cb.ID)
	regNoErr(t, e)
	oldEstimates, e := f.st.Estimates(regCtx, cb.ID)
	regNoErr(t, e)
	oldLinks := regLinks(t, f, cb.ID)
	r, cs, e := f.st.Decide(regCtx, store.Ruling{Kind: store.KindNotSame, Candidate: ca.ID, Other: cb.ID, Keys: []string{"ignored"}, Why: "why", UserID: "user"})
	regNoErr(t, e)
	regEqual(t, r.Keys, []string{})
	regEqual(t, cs, []store.Candidate{ca, cb})
	regEqual(t, regLinks(t, f, cb.ID), oldLinks)
	regEqual(t, regCandidate(t, f, ca.ID), ca)
	f.now = f.now.Add(time.Hour)
	r, cs, e = f.st.Decide(regCtx, store.Ruling{Kind: store.KindMerge, Candidate: ca.ID, Other: cb.ID, Keys: []string{"ignored"}, Why: "merge later", UserID: "user"})
	regNoErr(t, e)
	want := cb
	want.Status = store.StatusMerged
	want.MergedInto = ca.ID
	want.Updated = regTime(f)
	regEqual(t, cs[1], want)
	regEqual(t, cs[0], regCandidate(t, f, ca.ID))
	regEqual(t, regLinks(t, f, cb.ID), []store.Link{})
	for _, l := range regLinks(t, f, ca.ID) {
		if l.SourceID == oldLinks[0].SourceID {
			m := oldLinks[0]
			m.Candidate = ca.ID
			m.Reason = store.LinkDecision
			m.Confidence = 1
			regEqual(t, l, m)
		}
	}
	got, e := f.st.GetAssessment(regCtx, cb.ID)
	regNoErr(t, e)
	regEqual(t, got, oldAssessment)
	es, e := f.st.Estimates(regCtx, cb.ID)
	regNoErr(t, e)
	regEqual(t, es, oldEstimates)
	_, _, e = f.st.Decide(regCtx, store.Ruling{Kind: store.KindInclude, Candidate: cb.ID, Why: "why", UserID: "user"})
	if !errors.Is(e, store.ErrInvalid) {
		t.Fatal(e)
	}
	_, _, e = f.st.Decide(regCtx, store.Ruling{Kind: store.KindNotSame, Candidate: ca.ID, Other: cb.ID, Why: "why", UserID: "user"})
	if !errors.Is(e, store.ErrInvalid) {
		t.Fatal(e)
	}
	b.Digest = "fresh"
	_, _, e = f.st.Attach(regCtx, "later", true, []store.Observation{a, b})
	regNoErr(t, e)
	_, _, e = f.st.MarkGone(regCtx, "fmcsa", "later")
	regNoErr(t, e)
	_, e = f.st.RescoreAll(regCtx)
	regNoErr(t, e)
	regEqual(t, regCandidate(t, f, cb.ID), want)
	regEqual(t, regLinks(t, f, ca.ID)[1].Candidate, ca.ID)
	regEqual(t, regLinks(t, f, ca.ID)[1].Reason, store.LinkDecision)
	got, e = f.st.GetAssessment(regCtx, cb.ID)
	regNoErr(t, e)
	regEqual(t, got, oldAssessment)
}

// R-H14C-XOPK R-H2C9-BGG9 R-H3K5-P86Y R-H4S2-2ZXN
func TestScoringAssessmentAndFreshestSources(t *testing.T) {
	f := regNew(t)
	regNoErr(t, f.st.ReplaceZipDrive(regCtx, map[string]int64{"55072": 42}))
	high := int64(800)
	a := regObs("old", "Acme INC")
	a.Reading.Estimate = &sources.Estimate{Source: "fmcsa", Low: 9999, Confidence: 1, Facts: nil, Filed: a.Reading.Filed, Formula: sources.Rules}
	b := regObs("new", "Acme LLC")
	b.Reading.Filed = a.Reading.Filed.AddDate(0, 1, 0)
	b.Reading.Estimate = &sources.Estimate{Source: "fmcsa", Low: 100, Confidence: 0.25, Facts: nil, Filed: b.Reading.Filed, Formula: sources.Rules}
	b.Reading.Fleet = false
	b.Reading.Exclusions = []sources.Exclusion{{Reason: "signal", Detail: "one"}, {Reason: "signal", Detail: "one"}}
	c := regObs("other", "Acme CO")
	c.Scraper = "other-source"
	c.Reading.Filed = b.Reading.Filed.AddDate(0, 1, 0)
	c.Reading.Estimate = &sources.Estimate{Source: c.Scraper, Low: 400, High: &high, Confidence: 0.75, Filed: c.Reading.Filed, Formula: sources.Rules}
	c.Reading.Exclusions = []sources.Exclusion{{Reason: "signal", Detail: "two"}, {Reason: "signal", Detail: "one"}}
	d := regObs("no-estimate", "Acme LTD")
	d.Scraper = "date-source"
	d.Reading.Filed = c.Reading.Filed.AddDate(0, 1, 0)
	d.Reading.Fleet = false
	_, ids := regAttach(t, f, "run", a, b, c, d)
	id := ids[0]
	untouched := regObs("unranged", "Other business")
	untouched.Reading.Zip = "99999"
	_, other := regAttach(t, f, "run", untouched)
	before := regCandidate(t, f, id)
	links := regLinks(t, f, id)
	f.now = f.now.Add(time.Hour)
	f.calls = 0
	counts, e := f.st.Rescore(regCtx, []string{id, id, "absent"})
	regEqual(t, f.calls, 1)
	regNoErr(t, e)
	regEqual(t, counts, store.Counts{Rescored: 1})
	aWant := store.Assessment{Candidate: id, Fleet: true, Low: 325, High: &high, Confidence: 0.75, Filed: d.Reading.Filed, Minutes: func() *int64 { v := int64(42); return &v }(), Exclusions: []sources.Exclusion{{Reason: "signal", Detail: "two"}, {Reason: "signal", Detail: "one"}}, Excluded: true, Base: scoring.Score(scoring.Point(325, &high), 0.75), Rules: sources.Rules, Scored: regTime(f)}
	got, e := f.st.GetAssessment(regCtx, id)
	regNoErr(t, e)
	regEqual(t, got, aWant)
	es, e := f.st.Estimates(regCtx, id)
	regNoErr(t, e)
	regEqual(t, es, []sources.Estimate{*b.Reading.Estimate, *c.Reading.Estimate})
	regEqual(t, regCandidate(t, f, id), before)
	regEqual(t, regLinks(t, f, id), links)
	_, e = f.st.GetAssessment(regCtx, other[0])
	if !errors.Is(e, store.ErrNotFound) {
		t.Fatal(e)
	}
	f.reopen(t)
	got, e = f.st.GetAssessment(regCtx, id)
	regNoErr(t, e)
	regEqual(t, got, aWant)
	es, e = f.st.Estimates(regCtx, id)
	regNoErr(t, e)
	regEqual(t, es, []sources.Estimate{*b.Reading.Estimate, *c.Reading.Estimate})
	counts, e = f.st.Rescore(regCtx, nil)
	regNoErr(t, e)
	regEqual(t, counts, store.Counts{})
	got, e = f.st.GetAssessment(regCtx, id)
	regNoErr(t, e)
	regEqual(t, got, aWant)
	counts, e = f.st.RescoreAll(regCtx)
	regNoErr(t, e)
	regEqual(t, counts, store.Counts{Rescored: 2, Unranged: 1})
	// A freshest reading with no estimate removes that source's estimate.
	b.Reading.Estimate = nil
	b.Digest = "no-estimate"
	regAttach(t, f, "new-run", b)
	_, _, e = f.st.MarkGone(regCtx, "other-source", "none")
	regNoErr(t, e)
	_, e = f.st.Rescore(regCtx, []string{id})
	regNoErr(t, e)
	es, e = f.st.Estimates(regCtx, id)
	regNoErr(t, e)
	regEqual(t, es, []sources.Estimate{})
	got, e = f.st.GetAssessment(regCtx, id)
	regNoErr(t, e)
	regEqual(t, got.Fleet, false)
	regEqual(t, got.High, (*int64)(nil))
	regEqual(t, got.Low, int64(0))
	regEqual(t, got.Confidence, float64(0))
	regEqual(t, got.Filed, d.Reading.Filed)
}

// R-H5ZY-GROC R-GSL2-9AIP R-GL9N-YO2J R-GNPG-Q7JX
func TestScoringRulingsTakeEffectImmediately(t *testing.T) {
	f := regNew(t)
	regNoErr(t, f.st.ReplaceZipDrive(regCtx, map[string]int64{"55072": 0}))
	a := regObs("one", "Business")
	a.Reading.Estimate = &sources.Estimate{Source: a.Scraper, Low: 20000000, Confidence: 0.7, Filed: a.Reading.Filed, Formula: sources.Rules}
	_, ids := regAttach(t, f, "run", a)
	id := ids[0]
	_, e := f.st.RescoreAll(regCtx)
	regNoErr(t, e)
	filter := store.ShortlistFilter{MaxMinutes: 120, MinRevenue: 10000000}
	rs, e := f.st.Shortlist(regCtx, filter, 0, 10)
	regNoErr(t, e)
	regEqual(t, len(rs), 1)
	before := regCandidate(t, f, id)
	links := regLinks(t, f, id)
	for _, kind := range []string{store.KindExclude, store.KindInclude} {
		f.now = f.now.Add(time.Hour)
		r, cs, e := f.st.Decide(regCtx, store.Ruling{Kind: kind, Candidate: id, Other: "ignored", Keys: []string{"ignored"}, At: time.Time{}, Why: "owner reason", UserID: "owner", UserEmail: "owner@example.test"})
		regNoErr(t, e)
		regEqual(t, r, store.Ruling{Kind: kind, Candidate: id, Keys: []string{}, Why: "owner reason", UserID: "owner", UserEmail: "owner@example.test", At: regTime(f)})
		regEqual(t, cs, []store.Candidate{before})
		regEqual(t, regLinks(t, f, id), links)
		a, e := f.st.GetAssessment(regCtx, id)
		regNoErr(t, e)
		regEqual(t, a.Excluded, kind == store.KindExclude)
		regEqual(t, a.Scored, regTime(f))
		if kind == store.KindExclude {
			regEqual(t, a.Exclusions, []sources.Exclusion{{Reason: sources.ExcludeRuling}})
		} else {
			regEqual(t, a.Exclusions, []sources.Exclusion{})
		}
		rs, e = f.st.Shortlist(regCtx, filter, 0, 10)
		regNoErr(t, e)
		want := 1
		if kind == store.KindExclude {
			want = 0
		}
		regEqual(t, len(rs), want)
	}
	a.Reading.Exclusions = []sources.Exclusion{{Reason: sources.ExcludeGovernmentName, Detail: "pattern"}}
	a.Digest = "excluded-signal"
	regAttach(t, f, "run-new", a)
	_, e = f.st.RescoreAll(regCtx)
	regNoErr(t, e)
	got, e := f.st.GetAssessment(regCtx, id)
	regNoErr(t, e)
	regEqual(t, got.Excluded, false)
	regEqual(t, got.Exclusions, a.Reading.Exclusions)
	f.reopen(t)
	got, e = f.st.GetAssessment(regCtx, id)
	regNoErr(t, e)
	regEqual(t, got.Excluded, false)
	rulings, e := f.st.Rulings(regCtx, id)
	regNoErr(t, e)
	regEqual(t, len(rulings), 2)
	regEqual(t, rulings[0].Kind, store.KindInclude)
}

// R-H77U-UJF1
func TestScoringShortlistFiltersOrderAndAge(t *testing.T) {
	f := regNew(t)
	f.now = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	regNoErr(t, f.st.ReplaceZipDrive(regCtx, map[string]int64{"55072": 120, "55073": 121, "55074": 0}))
	specs := []struct {
		name, zip, class, cargo string
		low                     int64
		high                    *int64
		filed                   time.Time
		fleet                   bool
		confidence              float64
		ex                      []sources.Exclusion
	}{
		{"floor", "55072", sources.ClassForHire, "genfreight", 12000000, nil, f.now, true, 0.7, nil},
		{"boundary", "55072", sources.ClassForHire, "genfreight", 10000000, func() *int64 { v := int64(10000000); return &v }(), f.now, true, 0.7, nil},
		{"far", "55073", sources.ClassForHire, "genfreight", 20000000, nil, f.now, true, 1, nil},
		{"small", "55072", sources.ClassForHire, "genfreight", 9000000, func() *int64 { v := int64(9999999); return &v }(), f.now, true, 1, nil},
		{"old", "55072", sources.ClassForHire, "genfreight", 20000000, nil, f.now.AddDate(-6, 0, 0), true, 1, nil},
		{"excluded", "55072", sources.ClassForHire, "genfreight", 20000000, nil, f.now, true, 1, []sources.Exclusion{{Reason: "signal"}}},
		{"no-fleet", "55072", sources.ClassForHire, "genfreight", 20000000, nil, f.now, false, 1, nil},
		{"unranged", "99999", sources.ClassForHire, "genfreight", 20000000, nil, f.now, true, 1, nil},
		{"private", "55072", sources.ClassPrivate, "genfreight", 20000000, nil, f.now, true, 1, nil},
		{"cargo", "55072", sources.ClassForHire, "other-cargo", 20000000, nil, f.now, true, 1, nil},
		{"same-score", "55072", sources.ClassForHire, "genfreight", 12000000, nil, f.now, true, 0.7, nil},
		{"zero-minutes", "55074", sources.ClassForHire, "genfreight", 12000000, nil, f.now, true, 0.7, nil},
	}
	byName := map[string]string{}
	for _, x := range specs {
		o := regObs(x.name, x.name)
		o.Reading.Zip = x.zip
		o.Reading.Class = x.class
		o.Reading.Cargo = []string{x.cargo}
		o.Reading.Filed = x.filed
		o.Reading.Fleet = x.fleet
		o.Reading.Exclusions = x.ex
		o.Reading.Estimate = &sources.Estimate{Source: o.Scraper, Low: x.low, High: x.high, Confidence: x.confidence, Filed: x.filed, Formula: sources.Rules}
		_, ids := regAttach(t, f, "run", o)
		byName[x.name] = ids[0]
	}
	_, e := f.st.RescoreAll(regCtx)
	regNoErr(t, e)
	filter := store.ShortlistFilter{MaxMinutes: 120, MinRevenue: 10000000, Class: sources.ClassForHire, Cargo: "genfreight"}
	rs, e := f.st.Shortlist(regCtx, filter, 0, 100)
	regNoErr(t, e)
	want := []string{byName["floor"], byName["same-score"], byName["zero-minutes"], byName["boundary"]}
	regEqual(t, len(rs), len(want))
	for i, r := range rs {
		regEqual(t, r.Candidate.ID, want[i])
		regEqual(t, r.Candidate, regCandidate(t, f, r.Candidate.ID))
		a, e := f.st.GetAssessment(regCtx, r.Candidate.ID)
		regNoErr(t, e)
		regEqual(t, r.Assessment, a)
		regEqual(t, r.Effective, scoring.Effective(a.Confidence, a.Filed, f.now))
		regEqual(t, r.Score, scoring.Score(scoring.Point(a.Low, a.High), r.Effective))
	}
	page, e := f.st.Shortlist(regCtx, filter, 1, 2)
	regNoErr(t, e)
	regEqual(t, page, rs[1:3])
	filter.MaxMinutes = 0
	zero, e := f.st.Shortlist(regCtx, filter, 0, 100)
	regNoErr(t, e)
	regEqual(t, len(zero), 1)
	regEqual(t, zero[0].Candidate.ID, byName["zero-minutes"])
	// With no class/cargo restrictions those independent filters disappear.
	filter = store.ShortlistFilter{MaxMinutes: 121, MinRevenue: 9999999}
	all, e := f.st.Shortlist(regCtx, filter, 0, 100)
	regNoErr(t, e)
	regEqual(t, len(all), 8)
	f.now = f.now.AddDate(6, 0, 0)
	aged, e := f.st.Shortlist(regCtx, filter, 0, 100)
	regNoErr(t, e)
	regEqual(t, len(aged), 0)
}

// R-GTSY-N29E
func TestResolutionConcurrentMergeAttachAndRescore(t *testing.T) {
	f := regNew(t)
	_, ids := regAttach(t, f, "initial", regObs("one", "Survivor"), regObs("two", "Other"))
	survivor, other := ids[0], ids[1]
	var wg sync.WaitGroup
	errs := make(chan error, 5)
	actions := []func() error{
		func() error {
			_, _, e := f.st.Decide(regCtx, store.Ruling{Kind: store.KindMerge, Candidate: survivor, Other: other, Why: "same business", UserID: "owner"})
			return e
		},
		func() error {
			_, _, e := f.st.Attach(regCtx, "next", false, []store.Observation{regObs("three", "Other")})
			return e
		},
		func() error { _, _, e := f.st.MarkGone(regCtx, "fmcsa", "next"); return e },
		func() error { _, e := f.st.Rescore(regCtx, ids); return e }, func() error { _, e := f.st.RescoreAll(regCtx); return e },
	}
	start := make(chan struct{})
	for _, a := range actions {
		wg.Add(1)
		go func(fn func() error) { defer wg.Done(); <-start; errs <- fn() }(a)
	}
	close(start)
	wg.Wait()
	close(errs)
	for e := range errs {
		regNoErr(t, e)
	}
	cs, e := f.st.ListCandidates(regCtx, store.CandidateFilter{}, 0, 100)
	regNoErr(t, e)
	for _, c := range cs {
		for _, l := range regLinks(t, f, c.ID) {
			owner := regCandidate(t, f, l.Candidate)
			regEqual(t, owner.Status, store.StatusActive)
		}
	}
	regEqual(t, regCandidate(t, f, other).Status, store.StatusMerged)
	regEqual(t, len(regLinks(t, f, other)), 0)
}

// R-GF66-1TD2 R-GHLY-TCUG R-GAAK-IQEA
func TestResolutionOldestTieAndGoneIdentity(t *testing.T) {
	f := regNew(t)
	a, b := regObs("one", "Acme Inc"), regObs("two", "Acme LLC")
	_, ids := regAttach(t, f, "run", a, b)
	r, cs, e := f.st.Decide(regCtx, store.Ruling{Kind: store.KindSplit, Candidate: ids[0], Keys: []string{sources.SourceKey(b.Scraper, b.SourceID)}, Why: "separate entities", UserID: "owner"})
	regNoErr(t, e)
	regEqual(t, cs[0].Created, cs[1].Created)
	_, _, e = f.st.MarkGone(regCtx, "fmcsa", "empty-run")
	regNoErr(t, e)
	// Equal creation times are broken by id even when every matching link is gone.
	n, touched := regAttach(t, f, "new-run", regObs("three", "Acme Co"))
	regEqual(t, n, store.Counts{Rows: 1, New: 1, Linked: 1})
	want := ids[0]
	if r.Other < want {
		want = r.Other
	}
	regEqual(t, touched, []string{want})
	l := regLinks(t, f, want)[0]
	regEqual(t, l.SourceID, "three")
	regEqual(t, l.Reason, store.LinkMatchKey)
	regEqual(t, l.Confidence, 0.8)
	// A batch returns sorted distinct touched ids even when input order differs.
	n, touched = regAttach(t, f, "batch", b, a, b)
	regEqual(t, n, store.Counts{Rows: 3, Unchanged: 3})
	wantIDs := []string{ids[0], r.Other}
	sort.Strings(wantIDs)
	regEqual(t, touched, wantIDs)
}

// R-H5ZY-GROC R-H3K5-P86Y R-GMHK-CFT8
func TestScoringMergeAndSplitAssessOnlyActiveSides(t *testing.T) {
	f := regNew(t)
	regNoErr(t, f.st.ReplaceZipDrive(regCtx, map[string]int64{"55072": 18}))
	a, b := regObs("one", "Acme Inc"), regObs("two", "Acme LLC")
	a.Reading.Estimate = &sources.Estimate{Source: "fmcsa", Low: 100, Confidence: 0.7, Filed: a.Reading.Filed, Formula: sources.Rules}
	b.Reading.Estimate = &sources.Estimate{Source: "fmcsa", Low: 200, Confidence: 0.8, Filed: b.Reading.Filed, Formula: sources.Rules}
	_, ids := regAttach(t, f, "run", a, b)
	_, e := f.st.RescoreAll(regCtx)
	regNoErr(t, e)
	f.now = f.now.Add(time.Hour)
	f.calls = 0
	r, cs, e := f.st.Decide(regCtx, store.Ruling{Kind: store.KindSplit, Candidate: ids[0], Keys: []string{sources.SourceKey(b.Scraper, b.SourceID)}, Why: "split", UserID: "owner"})
	regNoErr(t, e)
	regEqual(t, f.calls, 1)
	for i, c := range cs {
		got, e := f.st.GetAssessment(regCtx, c.ID)
		regNoErr(t, e)
		regEqual(t, got.Scored, regTime(f))
		regEqual(t, got.Fleet, true)
		regEqual(t, *got.Minutes, int64(18))
		want := a.Reading.Estimate
		if i == 1 {
			want = b.Reading.Estimate
		}
		es, e := f.st.Estimates(regCtx, c.ID)
		regNoErr(t, e)
		regEqual(t, es, []sources.Estimate{*want})
		regEqual(t, got.Low, want.Low)
		regEqual(t, got.Confidence, want.Confidence)
	}
	mergedBefore, e := f.st.GetAssessment(regCtx, r.Other)
	regNoErr(t, e)
	f.now = f.now.Add(time.Hour)
	_, _, e = f.st.Decide(regCtx, store.Ruling{Kind: store.KindMerge, Candidate: ids[0], Other: r.Other, Why: "merge", UserID: "owner"})
	regNoErr(t, e)
	got, e := f.st.GetAssessment(regCtx, r.Other)
	regNoErr(t, e)
	regEqual(t, got, mergedBefore)
	survivor, e := f.st.GetAssessment(regCtx, ids[0])
	regNoErr(t, e)
	regEqual(t, survivor.Scored, regTime(f))
	f.now = f.now.Add(time.Hour)
	counts, e := f.st.Rescore(regCtx, []string{r.Other, "absent", ids[0], ids[0]})
	regNoErr(t, e)
	regEqual(t, counts, store.Counts{Rescored: 1})
	got, e = f.st.GetAssessment(regCtx, r.Other)
	regNoErr(t, e)
	regEqual(t, got, mergedBefore)
	// Failure of random input to a valid split leaves every record untouched.
	f2 := regNew(t)
	_, id2 := regAttach(t, f2, "run", a, b)
	original := regLinks(t, f2, id2[0])
	f2.st = store.New(f2.d, store.Config{Now: func() time.Time { return f2.now }, Rand: io.LimitReader(&regRandom{}, 0)})
	out, moved, err := f2.st.Decide(regCtx, store.Ruling{Kind: store.KindSplit, Candidate: id2[0], Keys: []string{sources.SourceKey(b.Scraper, b.SourceID)}, Why: "split", UserID: "owner"})
	regEqual(t, out, store.Ruling{})
	if moved != nil || !errors.Is(err, store.ErrUnreachable) {
		t.Fatal(moved, err)
	}
	regEqual(t, regLinks(t, f2, id2[0]), original)
	rulings, e := f2.st.Rulings(regCtx, id2[0])
	regNoErr(t, e)
	regEqual(t, len(rulings), 0)
}

// R-GHLY-TCUG
func TestResolutionBatchPreservesIntermediateDisplayUpdate(t *testing.T) {
	batch, single := regNew(t), regNew(t)
	original := regObs("one", "Original business")
	_, batchIDs := regAttach(t, batch, "initial-run", original)
	_, singleIDs := regAttach(t, single, "initial-run", original)
	regEqual(t, batchIDs, singleIDs)
	batch.now = batch.now.Add(time.Hour)
	single.now = batch.now
	changed := regObs("one", "Changed business")
	changed.Digest = "changed-digest"
	restored := original
	restored.Digest = "restored-digest"
	batchCounts, batchTouched := regAttach(t, batch, "next-run", changed, restored)
	firstCounts, firstTouched := regAttach(t, single, "next-run", changed)
	secondCounts, secondTouched := regAttach(t, single, "next-run", restored)
	regEqual(t, batchCounts, store.Counts{Rows: firstCounts.Rows + secondCounts.Rows, New: firstCounts.New + secondCounts.New, Changed: firstCounts.Changed + secondCounts.Changed, Unchanged: firstCounts.Unchanged + secondCounts.Unchanged, Gone: firstCounts.Gone + secondCounts.Gone, Created: firstCounts.Created + secondCounts.Created, Linked: firstCounts.Linked + secondCounts.Linked, Rescored: firstCounts.Rescored + secondCounts.Rescored, Unranged: firstCounts.Unranged + secondCounts.Unranged})
	regEqual(t, batchTouched, firstTouched)
	regEqual(t, batchTouched, secondTouched)
	id := batchIDs[0]
	regEqual(t, regCandidate(t, batch, id), regCandidate(t, single, id))
	regEqual(t, regCandidate(t, batch, id).Updated, regTime(batch))
	regEqual(t, regLinks(t, batch, id), regLinks(t, single, id))
	batchCandidates, err := batch.st.ListCandidates(regCtx, store.CandidateFilter{}, 0, 20)
	regNoErr(t, err)
	singleCandidates, err := single.st.ListCandidates(regCtx, store.CandidateFilter{}, 0, 20)
	regNoErr(t, err)
	regEqual(t, batchCandidates, singleCandidates)
	batch.reopen(t)
	single.reopen(t)
	regEqual(t, regCandidate(t, batch, id), regCandidate(t, single, id))
	regEqual(t, regLinks(t, batch, id), regLinks(t, single, id))
}
