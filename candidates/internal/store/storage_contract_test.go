package store_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/candidates"
	"github.com/ikigenba/ikigenba/candidates/internal/judging"
	"github.com/ikigenba/ikigenba/candidates/internal/parks"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

type contractRandom struct {
	next         byte
	calls, total int
	fail         bool
}

func (r *contractRandom) Read(p []byte) (int, error) {
	r.calls++
	if r.fail {
		return 0, io.ErrUnexpectedEOF
	}
	r.total += len(p)
	for i := range p {
		p[i] = r.next
	}
	r.next++
	return len(p), nil
}

type contractMeter struct {
	reader   io.Reader
	requests []int
	total    int
}

func (r *contractMeter) Read(p []byte) (int, error) {
	r.requests = append(r.requests, len(p))
	n, err := r.reader.Read(p)
	r.total += n
	return n, err
}

type contractFixture struct {
	parkPassID string
	t          *testing.T
	path       string
	now        time.Time
	rnd        io.Reader
	d          *db.DB
	s          *store.Store
}

// Construct the published store using its injected dependencies.
func contractDB(t *testing.T) *contractFixture {
	t.Helper()
	f := &contractFixture{t: t, path: filepath.Join(t.TempDir(), "state", "candidates.db"), now: time.Date(2030, 4, 5, 6, 7, 8, 999, time.FixedZone("fixture", 3600)), rnd: &contractRandom{next: 1}}
	f.open()
	t.Cleanup(func() {
		if err := f.d.Close(); err != nil {
			t.Error(err)
		}
	})
	return f
}
func (f *contractFixture) open() {
	f.t.Helper()
	d, err := db.Open(context.Background(), db.Config{Path: f.path, Migrations: candidates.Migrations(), Now: func() time.Time { return f.now }})
	if err != nil {
		f.t.Fatal(err)
	}
	f.d = d
	f.s = store.New(d, store.Config{Now: func() time.Time { return f.now }, Rand: f.rnd})
}
func (f *contractFixture) reopen() {
	f.t.Helper()
	if err := f.d.Close(); err != nil {
		f.t.Fatal(err)
	}
	f.open()
}
func contractOK(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func contractEqual[T any](t *testing.T, got, want T) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v; want %#v", got, want)
	}
}
func contractError(t *testing.T, err, want error) {
	t.Helper()
	if !errors.Is(err, want) {
		t.Fatalf("got %v; want %v", err, want)
	}
}
func contractNewPass(t *testing.T, f *contractFixture, trigger string) store.Pass {
	t.Helper()
	p := store.Pass{Kind: store.KindIngest, Origin: store.OriginIngest, Trigger: trigger, UserID: "owner", UserEmail: "owner@example.test", RequestID: "request"}
	if trigger != store.TriggerIngest {
		p.Kind, p.Origin = store.KindIngest, store.OriginRun
		p.Delivery = "delivery-" + trigger
	}
	out, err := f.s.AddPass(context.Background(), p)
	contractOK(t, err)
	return out
}
func contractObservation(id, name string, filed time.Time) store.Observation {
	high := int64(20000000)
	return store.Observation{Scraper: "source-z", SourceID: id, Digest: "digest-" + id, Reading: sources.Reading{NameKey: sources.NameKey(name), PhoneKey: "3205550101", DomainKey: "business.test", Name: name, Names: []string{name, "Trading alias"}, Street: "12 Test Road", City: "Test city", State: "MN", Zip: "55072", County: "MN-115", Phone: "320-555-0101", Email: "office@business.test", People: []sources.Person{{Name: "Pat Test", Role: "owner"}}, Class: sources.ClassForHire, Cargo: []string{"grain"}, Filed: filed, Fleet: true, Estimate: &sources.Estimate{Source: "source-z", Low: 10000000, High: &high, Confidence: 0.8, Facts: map[string]json.RawMessage{"units": json.RawMessage(`3`)}, Filed: filed, Formula: sources.Rules}}}
}
func contractAttach(t *testing.T, f *contractFixture, run string, obs ...store.Observation) []string {
	t.Helper()
	_, ids, err := f.s.Attach(context.Background(), run, false, obs)
	contractOK(t, err)
	return ids
}

// R-EDOD-N1K0 R-4HN1-A6QB R-NUUF-5V6M
func TestContractVocabulary(t *testing.T) {
	got := []string{store.CandidateIDPrefix, store.PassIDPrefix, store.StatusActive, store.StatusMerged, store.StatusRunning, store.StatusDone, store.StatusFailed, store.ReasonScrapes, store.ReasonInterrupted, store.ReasonStore, store.TriggerIngest, store.LinkSourceKey, store.LinkMatchKey, store.LinkDecision, store.LinkEIN, store.KindMerge, store.KindSplit, store.KindNotSame, store.KindExclude, store.KindInclude, store.TriggerJudge, store.KindIngest, store.KindJudge, store.KindIngest, store.OriginRun, store.OriginCron, store.OriginIngest, store.OriginJudge, store.TriggerJudgeTool}
	contractEqual(t, got, []string{"cand_", "pass_", "active", "merged", "running", "done", "failed", "scrapes", "interrupted", "store", "ingest", "source_key", "match_key", "decision", "ein", "merge", "split", "not_same", "exclude", "include", "judge_candidates", "ingest", "judge", "ingest", "run", "cron", "ingest", "judge", "judge"})
	for _, s := range []string{"", "0123456789ABCDEF", "0123456789abcde", "0123456789abcdef0", "0123456789abcdeg", "0123456789abcdeé"} {
		if store.ValidCandidateID(store.CandidateIDPrefix+s) || store.ValidPassID(store.PassIDPrefix+s) {
			t.Fatalf("accepted %q", s)
		}
	}
	if !store.ValidCandidateID("cand_0123456789abcdef") || !store.ValidPassID("pass_0123456789abcdef") || store.ValidCandidateID("pass_0123456789abcdef") || store.ValidPassID("cand_0123456789abcdef") || store.ValidCandidateID("") || store.ValidPassID("") {
		t.Fatal("id validation")
	}
	errs := []error{store.ErrNotFound, store.ErrBusy, store.ErrDelivered, store.ErrEnded, store.ErrInvalid, store.ErrPageCap, store.ErrUnreachable}
	for i, a := range errs {
		if a == nil {
			t.Fatal("nil error")
		}
		for j, b := range errs {
			if i != j && errors.Is(a, b) {
				t.Fatal("error identities overlap")
			}
		}
	}
	if store.Unreachable == "" || strings.ContainsAny(store.Unreachable, "\r\n") {
		t.Fatal("copy constant must be one nonempty line")
	}
}

// R-4ZG4-R3HF R-50O1-4V84 R-533T-WEPI R-54BQ-A6G7 R-4ZXJ-0QUQ R-52DB-SAC4
func TestContractPassLifecyclePersistence(t *testing.T) {
	t.Parallel()
	f := contractDB(t)
	ctx := context.Background()
	stamp := f.now.UTC().Truncate(time.Second)
	input := store.Pass{ID: "ignored", Kind: store.KindIngest, Origin: store.OriginIngest, Trigger: store.TriggerIngest, Status: "ignored", Reason: "ignored", Started: f.now.AddDate(-1, 0, 0), Finished: f.now, Rows: 99, New: 99, Changed: 99, Unchanged: 99, Gone: 99, Created: 99, Linked: 99, Judged: 99, Unranged: 99, Derived: 99, Due: 99, Fetched: 99, HTTPS: 99, HTTP: 99, None: 99, Runs: 99, RunsFailed: 99, Pages: 99, Cost: 99, ParcelRun: "ignored", ParcelsRead: 99, ByLabel: 99, ByValue: 99, ByCandidate: 99, ParksCreated: 99, ParksUpdated: 99, ParksGone: 99, ParksGrown: 99, UserID: "owner", UserEmail: "owner@example.test", RequestID: "request"}
	p, err := f.s.AddPass(ctx, input)
	contractOK(t, err)
	want := store.Pass{Kind: store.KindIngest, Origin: store.OriginIngest, ID: store.PassIDPrefix + "0101010101010101", Trigger: input.Trigger, Status: store.StatusRunning, Started: stamp, UserID: input.UserID, UserEmail: input.UserEmail, RequestID: input.RequestID}
	contractEqual(t, p, want)
	f.reopen()
	running, err := f.s.RunningPasses(ctx)
	contractOK(t, err)
	contractEqual(t, running, []store.Pass{p})
	read, err := f.s.GetPass(ctx, p.ID)
	contractOK(t, err)
	contractEqual(t, read, p)
	f.now = f.now.Add(time.Hour)
	c := store.Counts{Rows: 1, New: 2, Changed: 3, Unchanged: 4, Gone: 5, Created: 6, Linked: 7, Judged: 8, Unranged: 9, Derived: 15, Due: 10, Fetched: 11, HTTPS: 12, HTTP: 13, None: 14, Runs: 16, RunsFailed: 17, Pages: 18, Cost: 19, ParcelsRead: 20, ByLabel: 21, ByValue: 22, ByCandidate: 23, ParksCreated: 24, ParksUpdated: 25, ParksGone: 26, ParksGrown: 27}
	ended, err := f.s.FinishPass(ctx, p.ID, c)
	contractOK(t, err)
	want.Status = store.StatusDone
	want.Finished = f.now.UTC().Truncate(time.Second)
	want.Rows = c.Rows
	want.New = c.New
	want.Changed = c.Changed
	want.Unchanged = c.Unchanged
	want.Gone = c.Gone
	want.Created = c.Created
	want.Linked = c.Linked
	want.Judged = c.Judged
	want.Unranged = c.Unranged
	want.Derived = c.Derived
	want.Due, want.Fetched, want.HTTPS, want.HTTP, want.None = c.Due, c.Fetched, c.HTTPS, c.HTTP, c.None
	want.Runs, want.RunsFailed, want.Pages, want.Cost = c.Runs, c.RunsFailed, c.Pages, c.Cost
	want.ParcelsRead = c.ParcelsRead
	want.ByLabel = c.ByLabel
	want.ByValue = c.ByValue
	want.ByCandidate = c.ByCandidate
	want.ParksCreated = c.ParksCreated
	want.ParksUpdated = c.ParksUpdated
	want.ParksGone = c.ParksGone
	want.ParksGrown = c.ParksGrown
	contractEqual(t, ended, want)
	f.reopen()
	read, err = f.s.GetPass(ctx, p.ID)
	contractOK(t, err)
	contractEqual(t, read, want)
	running, err = f.s.RunningPasses(ctx)
	contractOK(t, err)
	if running == nil || len(running) != 0 {
		t.Fatal(running)
	}
	for _, reason := range []string{store.ReasonScrapes, store.ReasonInterrupted, store.ReasonStore, store.ReasonPrompts} {
		p = contractNewPass(t, f, "run-"+reason)
		f.now = f.now.Add(time.Minute)
		want = p
		want.Status = store.StatusFailed
		want.Reason = reason
		want.Finished = f.now.UTC().Truncate(time.Second)
		ended, err = f.s.FailPass(ctx, p.ID, reason)
		contractOK(t, err)
		contractEqual(t, ended, want)
		f.reopen()
		read, err = f.s.GetPass(ctx, p.ID)
		contractOK(t, err)
		contractEqual(t, read, want)
	}
	read, err = f.s.GetPass(ctx, "missing")
	contractError(t, err, store.ErrNotFound)
	contractEqual(t, read, store.Pass{})
}

// R-4SM4-Q4EK R-O4LM-8146
func TestContractPassRefusals(t *testing.T) {
	t.Parallel()
	f := contractDB(t)
	ctx := context.Background()
	p := contractNewPass(t, f, "run-one")
	rnd := f.rnd.(*contractRandom)
	for _, input := range []store.Pass{{}, {Trigger: store.TriggerIngest}, {Trigger: store.TriggerIngest, UserID: "owner", Delivery: "bad"}, {Trigger: "run"}} {
		before, beforeCalls := rnd.total, rnd.calls
		out, err := f.s.AddPass(ctx, input)
		contractError(t, err, store.ErrUnreachable)
		contractEqual(t, out, store.Pass{})
		contractEqual(t, rnd.total, before)
		contractEqual(t, rnd.calls, beforeCalls)
	}
	for _, input := range []store.Pass{{Kind: store.KindIngest, Origin: store.OriginRun, Trigger: p.Trigger, Delivery: "different"}, {Kind: store.KindIngest, Origin: store.OriginRun, Trigger: "different", Delivery: p.Delivery}} {
		out, err := f.s.AddPass(ctx, input)
		contractError(t, err, store.ErrDelivered)
		contractEqual(t, out, store.Pass{})
	}
	out, err := f.s.AddPass(ctx, store.Pass{Kind: store.KindIngest, Origin: store.OriginIngest, Trigger: store.TriggerIngest, UserID: "owner"})
	contractOK(t, err)
	if !store.ValidPassID(out.ID) {
		t.Fatal(out)
	}
	out, err = f.s.AddPass(ctx, store.Pass{Kind: store.KindIngest, Origin: store.OriginIngest, Trigger: store.TriggerIngest, UserID: "owner"})
	contractError(t, err, store.ErrBusy)
	contractEqual(t, out, store.Pass{})
	negatives := []store.Counts{{Rows: -1}, {New: -1}, {Changed: -1}, {Unchanged: -1}, {Gone: -1}, {Created: -1}, {Linked: -1}, {Judged: -1}, {Unranged: -1}, {Derived: -1}, {Due: -1}, {Fetched: -1}, {HTTPS: -1}, {HTTP: -1}, {None: -1}, {Runs: -1}, {RunsFailed: -1}, {Pages: -1}, {Cost: -1}, {ParcelsRead: -1}, {ByLabel: -1}, {ByValue: -1}, {ByCandidate: -1}, {ParksCreated: -1}, {ParksUpdated: -1}, {ParksGone: -1}, {ParksGrown: -1}}
	for _, c := range negatives {
		for _, id := range []string{p.ID, "missing"} {
			out, err = f.s.FinishPass(ctx, id, c)
			contractError(t, err, store.ErrUnreachable)
			contractEqual(t, out, store.Pass{})
		}
	}
	for _, id := range []string{p.ID, "missing"} {
		out, err = f.s.FailPass(ctx, id, "bad")
		contractError(t, err, store.ErrUnreachable)
		contractEqual(t, out, store.Pass{})
	}
	_, _, err = f.s.Decide(ctx, store.Ruling{Kind: "malformed", Candidate: "missing"})
	contractError(t, err, store.ErrUnreachable)
	_, _, err = f.s.Decide(ctx, store.Ruling{Kind: "malformed", Candidate: "missing", UserID: "owner"})
	contractError(t, err, store.ErrInvalid)
	read, err := f.s.GetPass(ctx, p.ID)
	contractOK(t, err)
	contractEqual(t, read, p)
	out, err = f.s.FinishPass(ctx, "missing", store.Counts{})
	contractError(t, err, store.ErrNotFound)
	contractEqual(t, out, store.Pass{})
	out, err = f.s.FailPass(ctx, "missing", store.ReasonStore)
	contractError(t, err, store.ErrNotFound)
	contractEqual(t, out, store.Pass{})
	done, err := f.s.FinishPass(ctx, p.ID, store.Counts{})
	contractOK(t, err)
	out, err = f.s.FinishPass(ctx, p.ID, store.Counts{})
	contractError(t, err, store.ErrEnded)
	contractEqual(t, out, store.Pass{})
	out, err = f.s.FailPass(ctx, p.ID, store.ReasonStore)
	contractError(t, err, store.ErrEnded)
	contractEqual(t, out, store.Pass{})
	out, err = f.s.AddPass(ctx, store.Pass{Kind: store.KindIngest, Origin: store.OriginRun, Trigger: p.Trigger, Delivery: "new-delivery"})
	contractError(t, err, store.ErrDelivered)
	contractEqual(t, out, store.Pass{})
	read, err = f.s.GetPass(ctx, p.ID)
	contractOK(t, err)
	contractEqual(t, read, done)
}

// R-56RJ-1PXL
func TestContractIDMinting(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"pass", "candidate"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			f := contractDB(t)
			ctx := context.Background()
			seed := bytes.Repeat([]byte{0x5a}, 8)
			f.rnd = bytes.NewReader(seed)
			f.s = store.New(f.d, store.Config{Now: func() time.Time { return f.now }, Rand: f.rnd})
			var first string
			mint := func(label string) (string, error) {
				if kind == "pass" {
					p, err := f.s.AddPass(ctx, store.Pass{Kind: store.KindIngest, Origin: store.OriginRun, Trigger: label, Delivery: "delivery-" + label})
					return p.ID, err
				}
				n, ids, err := f.s.Attach(ctx, label, false, []store.Observation{contractObservation(label, label, f.now)})
				if err != nil {
					contractEqual(t, n, store.Counts{})
					if ids != nil {
						t.Fatal(ids)
					}
					return "", err
				}
				return ids[0], nil
			}
			first, err := mint("one")
			contractOK(t, err)
			prefix := store.CandidateIDPrefix
			if kind == "pass" {
				prefix = store.PassIDPrefix
				_, err = f.s.FinishPass(ctx, first, store.Counts{})
				contractOK(t, err)
			}
			contractEqual(t, first, prefix+"5a5a5a5a5a5a5a5a")
			stream := bytes.NewReader(append(bytes.Repeat([]byte{0x5a}, 8), bytes.Repeat([]byte{0x6b}, 8)...))
			f.s = store.New(f.d, store.Config{Now: func() time.Time { return f.now }, Rand: stream})
			second, err := mint("two")
			contractOK(t, err)
			contractEqual(t, second, prefix+"6b6b6b6b6b6b6b6b")
			contractEqual(t, stream.Len(), 0)
			if kind == "pass" {
				_, err = f.s.FinishPass(ctx, second, store.Counts{})
				contractOK(t, err)
			}
			originalPasses, err := f.s.ListPasses(ctx, "", "", 20)
			contractOK(t, err)
			originalCandidates, err := f.s.ListCandidates(ctx, store.CandidateFilter{}, "", "", 20)
			contractOK(t, err)
			originalLinks := map[string][]store.Link{}
			for _, c := range originalCandidates {
				originalLinks[c.ID], err = f.s.Links(ctx, c.ID)
				contractOK(t, err)
			}
			for _, data := range [][]byte{bytes.Repeat([]byte{0x5a}, 64), {}, bytes.Repeat([]byte{0x5a}, 4)} {
				r := &contractMeter{reader: bytes.NewReader(data)}
				f.s = store.New(f.d, store.Config{Now: func() time.Time { return f.now }, Rand: r})
				id, err := mint("three")
				contractError(t, err, store.ErrUnreachable)
				contractEqual(t, id, "")
				contractEqual(t, r.total, len(data))
				wantRequests := []int{8}
				if len(data) == 64 {
					wantRequests = []int{8, 8, 8, 8, 8, 8, 8, 8}
				} else if len(data) == 4 {
					wantRequests = []int{8, 4}
				}
				contractEqual(t, r.requests, wantRequests)
				afterPasses, e := f.s.ListPasses(ctx, "", "", 20)
				contractOK(t, e)
				contractEqual(t, afterPasses, originalPasses)
				afterCandidates, e := f.s.ListCandidates(ctx, store.CandidateFilter{}, "", "", 20)
				contractOK(t, e)
				contractEqual(t, afterCandidates, originalCandidates)
				for _, c := range originalCandidates {
					ls, e := f.s.Links(ctx, c.ID)
					contractOK(t, e)
					contractEqual(t, ls, originalLinks[c.ID])
				}

			}
			if kind == "pass" {
				ps, err := f.s.ListPasses(ctx, "", "", 20)
				contractOK(t, err)
				contractEqual(t, len(ps), 2)
			} else {
				cs, err := f.s.ListCandidates(ctx, store.CandidateFilter{}, "", "", 20)
				contractOK(t, err)
				contractEqual(t, len(cs), 2)
			}
		})
	}
}

// R-9EOK-BXVN
func TestContractPassWindowsAndPrune(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	var ps []store.Pass
	for i := range 6 {
		p := contractNewPass(t, f, fmt.Sprintf("run-%d", i))
		var err error
		if i%2 == 0 {
			p, err = f.s.FinishPass(ctx, p.ID, store.Counts{Rows: int64(i)})
		} else {
			p, err = f.s.FailPass(ctx, p.ID, store.ReasonScrapes)
		}
		contractOK(t, err)
		ps = append(ps, p)
		if i == 2 {
			f.now = f.now.Add(time.Hour)
		}
	}
	running := contractNewPass(t, f, "running")
	all := append(slices.Clone(ps), running)
	slices.SortFunc(all, func(a, b store.Pass) int {
		if a.Started.After(b.Started) {
			return -1
		}
		if a.Started.Before(b.Started) {
			return 1
		}
		return strings.Compare(b.ID, a.ID)
	})
	got, err := f.s.ListPasses(ctx, "", "", 20)
	contractOK(t, err)
	contractEqual(t, got, all)
	got, err = f.s.ListPasses(ctx, all[0].Started.Format(time.RFC3339), all[0].ID, 2)
	contractOK(t, err)
	contractEqual(t, got, all[1:3])
	got, err = f.s.ListPasses(ctx, "", "end", 2)
	contractOK(t, err)
	if got == nil || len(got) != 0 {
		t.Fatal(got)
	}
	for _, n := range []int{0, -1} {
		got, err = f.s.ListPasses(ctx, "", "", n)
		contractError(t, err, store.ErrUnreachable)
		if got != nil {
			t.Fatal(got)
		}
	}
	for _, k := range []int64{0, -1} {
		count, e := f.s.PrunePasses(ctx, k, 500)
		err = e
		contractError(t, err, store.ErrUnreachable)
		if count != 0 {
			t.Fatal(count)
		}
	}
	gone, err := f.s.PrunePasses(ctx, 2, 500)
	contractOK(t, err)
	contractEqual(t, gone, int64(2))
	removed := []store.Pass{ps[1], ps[0]}
	f.reopen()
	for _, p := range removed {
		_, err = f.s.GetPass(ctx, p.ID)
		contractError(t, err, store.ErrNotFound)
	}
	for _, p := range ps {
		if !slices.ContainsFunc(removed, func(q store.Pass) bool { return p.ID == q.ID }) {
			retained, e := f.s.GetPass(ctx, p.ID)
			contractOK(t, e)
			contractEqual(t, retained, p)
		}
	}
	left, err := f.s.RunningPasses(ctx)
	contractOK(t, err)
	contractEqual(t, left, []store.Pass{running})
	_, err = f.s.FailPass(ctx, running.ID, store.ReasonInterrupted)
	contractOK(t, err)
	for _, p := range removed {
		q, err := f.s.AddPass(ctx, store.Pass{Kind: store.KindIngest, Origin: store.OriginRun, Trigger: p.Trigger, Delivery: p.Delivery})
		contractOK(t, err)
		_, err = f.s.FinishPass(ctx, q.ID, store.Counts{})
		contractOK(t, err)
	}
	gone, err = f.s.PrunePasses(ctx, 100, 500)
	contractOK(t, err)
	if gone != 0 {
		t.Fatal(gone)
	}
}

// R-5610-XLK7
func TestContractZipReplacement(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	table := candidates.ZipDrive()
	contractOK(t, f.s.ReplaceZipDrive(ctx, table))
	f.reopen()
	for zip, minutes := range table {
		got, err := f.s.ZipMinutes(ctx, zip)
		contractOK(t, err)
		contractEqual(t, got, minutes)
	}
	table = map[string]int64{"00000": 0, "12345": 37}
	contractOK(t, f.s.ReplaceZipDrive(ctx, table))
	f.reopen()
	for zip, minutes := range table {
		got, err := f.s.ZipMinutes(ctx, zip)
		contractOK(t, err)
		contractEqual(t, got, minutes)
	}
	got, err := f.s.ZipMinutes(ctx, "55072")
	contractError(t, err, store.ErrNotFound)
	contractEqual(t, got, int64(0))
	for _, bad := range []map[string]int64{{"1234": 1}, {"123456": 1}, {"abcde": 1}, {"12345": -1}} {
		contractError(t, f.s.ReplaceZipDrive(ctx, bad), store.ErrUnreachable)
		got, err = f.s.ZipMinutes(ctx, "12345")
		contractOK(t, err)
		contractEqual(t, got, int64(37))
	}
	for _, empty := range []map[string]int64{{}, nil} {
		contractOK(t, f.s.ReplaceZipDrive(ctx, empty))
		f.reopen()
		got, err = f.s.ZipMinutes(ctx, "12345")
		contractError(t, err, store.ErrNotFound)
		contractEqual(t, got, int64(0))
	}
}

// R-55JM-NY6W R-47VU-80SR R-493Q-LSJG R-578X-BDAW R-T3DU-KBYR R-5AWM-GOIZ R-5C4I-UG9O
func TestContractBusinessRecordsPersistence(t *testing.T) {
	t.Parallel()
	f := contractDB(t)
	ctx := context.Background()
	o := contractObservation("one", "ACME TRUCKING INC", f.now.UTC().Truncate(time.Second))
	ids := contractAttach(t, f, "run-one", o)
	id := ids[0]
	stamp := f.now.UTC().Truncate(time.Second)
	want := store.Candidate{ID: id, Status: store.StatusActive, Name: o.Reading.Name, Names: o.Reading.Names, Street: o.Reading.Street, City: o.Reading.City, State: o.Reading.State, Zip: o.Reading.Zip, County: o.Reading.County, Phone: o.Reading.Phone, Email: o.Reading.Email, People: o.Reading.People, Class: o.Reading.Class, Cargo: o.Reading.Cargo, Created: stamp, Updated: stamp}
	c, err := f.s.GetCandidate(ctx, id)
	contractOK(t, err)
	contractEqual(t, c, want)
	if !store.ValidCandidateID(c.ID) {
		t.Fatal(c.ID)
	}
	links, err := f.s.Links(ctx, id)
	contractOK(t, err)
	contractEqual(t, links, []store.Link{{Candidate: id, Scraper: o.Scraper, SourceID: o.SourceID, Run: "run-one", Digest: o.Digest, Reading: o.Reading, Reason: store.LinkSourceKey, Confidence: 1, FirstSeen: stamp, LastSeen: stamp}})
	es, err := f.s.Estimates(ctx, id)
	contractOK(t, err)
	if es == nil || len(es) != 0 {
		t.Fatal(es)
	}
	rs, err := f.s.Rulings(ctx, id)
	contractOK(t, err)
	if rs == nil || len(rs) != 0 {
		t.Fatal(rs)
	}
	a, err := f.s.GetJudgments(ctx, id)
	contractError(t, err, store.ErrNotFound)
	contractEqual(t, a, store.Judgments{})
	contractOK(t, f.s.ReplaceZipDrive(ctx, map[string]int64{o.Reading.Zip: 41}))
	deriveAll(t, f.s)
	_, err = f.s.Judge(ctx, ids)
	contractOK(t, err)
	wantA := store.Judgments{HeadcountSeries: []judging.Series{}, Trend: judging.TrendUnknown, Candidate: id, OperatingKey: -1, Evidence: []sources.Evidence{}, RevenueLow: &o.Reading.Estimate.Low, RevenueHigh: o.Reading.Estimate.High, RevenueConfidence: &o.Reading.Estimate.Confidence, Rules: sources.Rules, Calculated: stamp}
	a, err = f.s.GetJudgments(ctx, id)
	contractOK(t, err)
	contractEqual(t, a, wantA)
	es, err = f.s.Estimates(ctx, id)
	contractOK(t, err)
	contractEqual(t, es, []sources.Estimate{*o.Reading.Estimate})
	f.now = f.now.Add(time.Minute)
	r, _, err := f.s.Decide(ctx, store.Ruling{Kind: store.KindInclude, Candidate: id, Why: "owner inspected", UserID: "owner", UserEmail: "owner@example.test"})
	contractOK(t, err)
	wantR := store.Ruling{Kind: store.KindInclude, Candidate: id, Keys: []string{}, Why: "owner inspected", UserID: "owner", UserEmail: "owner@example.test", At: f.now.UTC().Truncate(time.Second)}
	contractEqual(t, r, wantR)
	f.now = f.now.Add(time.Minute)
	r2, _, err := f.s.Decide(ctx, store.Ruling{Kind: store.KindExclude, Candidate: id, Why: "owner declined", UserID: "second-owner"})
	contractOK(t, err)
	rs, err = f.s.Rulings(ctx, id)
	contractOK(t, err)
	contractEqual(t, rs, []store.Ruling{r2, r})
	// A second source becomes a distinct estimate for the same candidate.
	o2 := o
	o2.Scraper = "source-a"
	o2.SourceID = "two"
	e := *o.Reading.Estimate
	e.Source = o2.Scraper
	o2.Reading.Estimate = &e
	contractAttach(t, f, "run-two", o2)
	_, _, err = f.s.JudgeAll(ctx, "", 500, f.now)
	contractOK(t, err)
	es, err = f.s.Estimates(ctx, id)
	contractOK(t, err)
	contractEqual(t, es, []sources.Estimate{e, *o.Reading.Estimate})
	c, err = f.s.GetCandidate(ctx, id)
	contractOK(t, err)
	links, err = f.s.Links(ctx, id)
	contractOK(t, err)
	a, err = f.s.GetJudgments(ctx, id)
	contractOK(t, err)
	rs, err = f.s.Rulings(ctx, id)
	contractOK(t, err)
	f.reopen()
	c2, err := f.s.GetCandidate(ctx, id)
	contractOK(t, err)
	contractEqual(t, c2, c)
	l2, err := f.s.Links(ctx, id)
	contractOK(t, err)
	contractEqual(t, l2, links)
	e2, err := f.s.Estimates(ctx, id)
	contractOK(t, err)
	contractEqual(t, e2, es)
	a2, err := f.s.GetJudgments(ctx, id)
	contractOK(t, err)
	contractEqual(t, a2, a)
	r3, err := f.s.Rulings(ctx, id)
	contractOK(t, err)
	contractEqual(t, r3, rs)
	other := contractAttach(t, f, "run-other", contractObservation("other", "Other business", o.Reading.Filed))[0]
	merge, _, err := f.s.Decide(ctx, store.Ruling{Kind: store.KindMerge, Candidate: id, Other: other, Why: "one owner", UserID: "owner"})
	contractOK(t, err)
	merged, err := f.s.GetCandidate(ctx, other)
	contractOK(t, err)
	contractEqual(t, merged.Status, store.StatusMerged)
	contractEqual(t, merged.MergedInto, id)
	l2, err = f.s.Links(ctx, other)
	contractOK(t, err)
	if l2 == nil || len(l2) != 0 {
		t.Fatal(l2)
	}
	r3, err = f.s.Rulings(ctx, other)
	contractOK(t, err)
	contractEqual(t, r3, []store.Ruling{merge})
	f.reopen()
	c2, err = f.s.GetCandidate(ctx, other)
	contractOK(t, err)
	contractEqual(t, c2, merged)
}

// R-59OQ-2WSA
func TestContractLinkReadOrder(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	filed := f.now.UTC().Truncate(time.Second)
	var obs []store.Observation
	// These all match one business. The tie rules are deliberately independent.
	for _, key := range []struct {
		scraper, id string
		filed       time.Time
	}{{"source-b", "b", filed}, {"source-a", "b", filed}, {"source-a", "a", filed}, {"source-a", "newer-seen", filed}, {"source-c", "newer-filed", filed.Add(time.Hour)}, {"source-d", "undated", time.Time{}}, {"source-e", "gone", filed.Add(2 * time.Hour)}} {
		o := contractObservation(key.id, "ACME", key.filed)
		o.Scraper = key.scraper
		obs = append(obs, o)
	}
	id := contractAttach(t, f, "old", obs...)[0]
	f.now = f.now.Add(time.Minute)
	contractAttach(t, f, "new", obs[3])
	cursor, count, err := f.s.MarkGone(ctx, "source-e", "new", "", 500)
	contractOK(t, err)
	contractEqual(t, cursor, "gone")
	contractEqual(t, count, store.Counts{Gone: 1})
	got, err := f.s.Links(ctx, id)
	contractOK(t, err)
	var keys []string
	for _, l := range got {
		keys = append(keys, sources.SourceKey(l.Scraper, l.SourceID))
	}
	contractEqual(t, keys, []string{"source-c:newer-filed", "source-a:newer-seen", "source-a:a", "source-a:b", "source-b:b", "source-d:undated", "source-e:gone"})
	if !got[len(got)-1].Gone {
		t.Fatal("gone link must follow live evidence")
	}
}

// R-4HU1-P3L0 R-419E-48H2
func TestContractCandidateWindowsAndFilters(t *testing.T) {
	f := contractDB(t)
	ctx := context.Background()
	var ids []string
	for _, name := range []string{"ACME TRUCKING INC", "ACME TRUCK", "TRUCK ING LLC", "Later business"} {
		o := contractObservation(name, name, f.now.UTC().Truncate(time.Second))
		if name == "Later business" {
			f.now = f.now.Add(time.Hour)
			o.Reading.Zip = "12345"
		}
		ids = append(ids, contractAttach(t, f, "run", o)[0])
	}
	got, err := f.s.ListCandidates(ctx, store.CandidateFilter{}, "", "", 20)
	contractOK(t, err)
	var gotIDs []string
	for _, c := range got {
		gotIDs = append(gotIDs, c.ID)
	}
	want := []string{ids[3], ids[2], ids[1], ids[0]}
	contractEqual(t, gotIDs, want)
	page, err := f.s.ListCandidates(ctx, store.CandidateFilter{}, got[0].Created.Format(time.RFC3339), got[0].ID, 2)
	contractOK(t, err)
	contractEqual(t, page, got[1:3])
	page, err = f.s.ListCandidates(ctx, store.CandidateFilter{}, "", "end", 2)
	contractOK(t, err)
	if page == nil || len(page) != 0 {
		t.Fatal(page)
	}
	for _, name := range []string{"TRUCKING", "trucking", "Trucking"} {
		page, err = f.s.ListCandidates(ctx, store.CandidateFilter{Name: name}, "", "", 20)
		contractOK(t, err)
		contractEqual(t, len(page), 1)
		contractEqual(t, page[0].ID, ids[0])
	}
	page, err = f.s.ListCandidates(ctx, store.CandidateFilter{Name: "ALIAS", Zip: "12345", Phone: "1 (320) 555-0101"}, "", "", 20)
	contractOK(t, err)
	contractEqual(t, len(page), 1)
	contractEqual(t, page[0].ID, ids[3])
	page, err = f.s.ListCandidates(ctx, store.CandidateFilter{Phone: "letters"}, "", "", 20)
	contractOK(t, err)
	if page == nil || len(page) != 0 {
		t.Fatal(page)
	}
	_, _, err = f.s.MarkGone(ctx, "source-z", "absent", "", 500)
	contractOK(t, err)
	page, err = f.s.ListCandidates(ctx, store.CandidateFilter{Phone: "320.555.0101"}, "", "", 20)
	contractOK(t, err)
	contractEqual(t, len(page), 4)
	_, _, err = f.s.Decide(ctx, store.Ruling{Kind: store.KindMerge, Candidate: ids[0], Other: ids[1], Why: "same", UserID: "owner"})
	contractOK(t, err)
	page, err = f.s.ListCandidates(ctx, store.CandidateFilter{}, "", "", 20)
	contractOK(t, err)
	for _, c := range page {
		if c.ID == ids[1] || c.Status != store.StatusActive {
			t.Fatal(c)
		}
	}
	contractEqual(t, len(page), 3)
	for _, n := range []int{0, -1} {
		page, err = f.s.ListCandidates(ctx, store.CandidateFilter{}, "", "", n)
		contractError(t, err, store.ErrUnreachable)
		if page != nil {
			t.Fatal(page)
		}

	}
}

// R-57ZF-FHOA R-T3DU-KBYR
func TestContractFreshStoreAndUnknownReads(t *testing.T) {
	t.Parallel()
	f := contractDB(t)
	ctx := context.Background()
	ps, err := f.s.ListPasses(ctx, "", "", 5)
	contractOK(t, err)
	if ps == nil || len(ps) != 0 {
		t.Fatal(ps)
	}
	ps, err = f.s.RunningPasses(ctx)
	contractOK(t, err)
	if ps == nil || len(ps) != 0 {
		t.Fatal(ps)
	}
	cs, err := f.s.ListCandidates(ctx, store.CandidateFilter{Name: "anything"}, "", "", 3)
	contractOK(t, err)
	if cs == nil || len(cs) != 0 {
		t.Fatal(cs)
	}
	rs, err := f.s.ListProspectLists(ctx, "", "", 3)
	contractOK(t, err)
	if rs == nil || len(rs) != 0 {
		t.Fatal(rs)
	}
	ks, err := f.s.ListProspectListsReversed(ctx, "", "", 3)
	contractOK(t, err)
	if ks == nil || len(ks) != 0 {
		t.Fatal(ks)
	}
	k, err := f.s.GetProspectList(ctx, "unknown")
	contractError(t, err, store.ErrNotFound)
	contractEqual(t, k, store.ProspectList{})
	cr, err := f.s.ReadProspectList(ctx, "unknown")
	contractError(t, err, store.ErrNotFound)
	contractEqual(t, cr, store.ProspectListRead{})
	rejects, err := f.s.Rejects(ctx, "unknown")
	contractError(t, err, store.ErrNotFound)
	if rejects != nil {
		t.Fatal(rejects)
	}
	p, err := f.s.GetPass(ctx, "unknown")
	contractError(t, err, store.ErrNotFound)
	contractEqual(t, p, store.Pass{})
	c, err := f.s.GetCandidate(ctx, "unknown")
	contractError(t, err, store.ErrNotFound)
	contractEqual(t, c, store.Candidate{})
	l, err := f.s.Links(ctx, "unknown")
	contractError(t, err, store.ErrNotFound)
	if l != nil {
		t.Fatal(l)
	}
	e, err := f.s.Estimates(ctx, "unknown")
	contractError(t, err, store.ErrNotFound)
	if e != nil {
		t.Fatal(e)
	}
	a, err := f.s.GetJudgments(ctx, "unknown")
	contractError(t, err, store.ErrNotFound)
	contractEqual(t, a, store.Judgments{})
	r, err := f.s.Rulings(ctx, "unknown")
	contractError(t, err, store.ErrNotFound)
	if r != nil {
		t.Fatal(r)
	}
	z, err := f.s.ZipMinutes(ctx, "unknown")
	contractError(t, err, store.ErrNotFound)
	contractEqual(t, z, int64(0))
}

// A snapshot uses only store reads, so record preservation is checked through
// the same surface its consumers use, including after reopening the handle.
type contractSnapshot struct {
	candidates  []store.Candidate
	passes      []store.Pass
	links       map[string][]store.Link
	estimates   map[string][]sources.Estimate
	judgments   map[string]store.Judgments
	enrichments map[string][]store.Enrichment
	rulings     map[string][]store.Ruling
	minutes     int64
}

func contractState(t *testing.T, f *contractFixture) contractSnapshot {
	t.Helper()
	ctx := context.Background()
	cs, err := f.s.ListCandidates(ctx, store.CandidateFilter{}, "", "", 100)
	contractOK(t, err)
	ps, err := f.s.ListPasses(ctx, "", "", 100)
	contractOK(t, err)
	snap := contractSnapshot{candidates: cs, passes: ps, links: map[string][]store.Link{}, estimates: map[string][]sources.Estimate{}, judgments: map[string]store.Judgments{}, enrichments: map[string][]store.Enrichment{}, rulings: map[string][]store.Ruling{}}
	for _, c := range cs {
		snap.links[c.ID], err = f.s.Links(ctx, c.ID)
		contractOK(t, err)
		snap.estimates[c.ID], err = f.s.Estimates(ctx, c.ID)
		contractOK(t, err)
		snap.judgments[c.ID], err = f.s.GetJudgments(ctx, c.ID)
		contractOK(t, err)
		snap.enrichments[c.ID], err = f.s.Enrichments(ctx, c.ID)
		contractOK(t, err)
		snap.rulings[c.ID], err = f.s.Rulings(ctx, c.ID)
		contractOK(t, err)
	}
	snap.minutes, err = f.s.ZipMinutes(ctx, "55072")
	contractOK(t, err)
	return snap
}
func contractSeed(t *testing.T, f *contractFixture) string {
	t.Helper()
	ctx := context.Background()
	id := contractAttach(t, f, "run-evidence", contractObservation("evidence", "Seed business", f.now.UTC().Truncate(time.Second)))[0]
	contractOK(t, f.s.ReplaceZipDrive(ctx, map[string]int64{"55072": 42}))
	_, _, err := f.s.JudgeAll(ctx, "", 500, f.now)
	contractOK(t, err)
	_, _, err = f.s.Decide(ctx, store.Ruling{Kind: store.KindInclude, Candidate: id, Why: "seed ruling", UserID: "owner"})
	contractOK(t, err)
	return id
}

// R-533T-WEPI R-54BQ-A6G7 R-5610-XLK7
func TestContractPassAndZipWritesPreserveBusinessRecords(t *testing.T) {
	t.Parallel()
	f := contractDB(t)
	ctx := context.Background()
	id := contractSeed(t, f)
	_, e := f.s.AddEnrichments(ctx, []store.Enrichment{websiteResult(id, sources.WebsiteHTTPS)})
	contractOK(t, e)
	before := contractState(t, f)
	for i := range 4 {
		p := contractNewPass(t, f, fmt.Sprintf("preserve-%d", i))
		if i%2 == 0 {
			_, err := f.s.FinishPass(ctx, p.ID, store.Counts{Rows: 3})
			contractOK(t, err)
		} else {
			_, err := f.s.FailPass(ctx, p.ID, store.ReasonStore)
			contractOK(t, err)
		}
	}
	_, err := f.s.PrunePasses(ctx, 1, 500)
	contractOK(t, err)
	after := contractState(t, f)
	after.passes = before.passes
	contractEqual(t, after, before)
	zipBefore := contractState(t, f)
	contractOK(t, f.s.ReplaceZipDrive(ctx, map[string]int64{"55072": 77}))
	after = contractState(t, f)
	after.minutes = zipBefore.minutes
	contractEqual(t, after, zipBefore)
	f.reopen()
	after = contractState(t, f)
	after.minutes = zipBefore.minutes
	contractEqual(t, after, zipBefore)
}

// R-25AU-BG4E R-OD8H-2EDG
func TestContractAllMethodsUnavailable(t *testing.T) {
	t.Parallel()
	f := contractDB(t)
	ctx := context.Background()
	id := contractSeed(t, f)
	p := contractNewPass(t, f, "existing-run")
	before := contractState(t, f)
	calls := []struct {
		name string
		call func(context.Context) error
	}{

		{"ParkRules", func(c context.Context) error { _, _, e := f.s.ParkRules(c); return e }},
		{"SetParkRules", func(c context.Context) error {
			_, e := f.s.SetParkRules(c, store.ParkRulesEdit{Distance: i64(0)}, store.Pass{})
			return e
		}},
		{"ActiveAddressKeys", func(c context.Context) error { _, _, e := f.s.ActiveAddressKeys(c, "", 0); return e }},
		{"PriorParks", func(c context.Context) error { _, e := f.s.PriorParks(c); return e }},
		{"ApplyParks", func(c context.Context) error {
			_, e := f.s.ApplyParks(c, "", "", nil, parks.Plan{}, store.Counts{})
			return e
		}},
		{"ListParks", func(c context.Context) error { _, e := f.s.ListParks(c, store.ParkFilter{}, "", "", "", 0); return e }},
		{"ListParksReversed", func(c context.Context) error {
			_, e := f.s.ListParksReversed(c, store.ParkFilter{}, "", "", "", 0)
			return e
		}},
		{"GetPark", func(c context.Context) error { _, e := f.s.GetPark(c, "missing"); return e }},
		{"ParkParcels", func(c context.Context) error { _, e := f.s.ParkParcels(c, "missing"); return e }},
		{"CountParks", func(c context.Context) error { _, _, e := f.s.CountParks(c); return e }},
		{"ParkCountiesHeld", func(c context.Context) error { _, e := f.s.ParkCountiesHeld(c); return e }},
		{"ParkCountiesRead", func(c context.Context) error { _, e := f.s.ParkCountiesRead(c); return e }},
		{"ParksRun", func(c context.Context) error { _, e := f.s.ParksRun(c); return e }},

		{"AddCampaignUse", func(c context.Context) error { return f.s.AddCampaignUse(c, "", "", "", time.Time{}) }},
		{"RejectCampaignUse", func(c context.Context) error { return f.s.RejectCampaignUse(c, "", "", "", time.Time{}) }},
		{"DeliverCampaignUse", func(c context.Context) error { return f.s.DeliverCampaignUse(c, "", "", time.Time{}) }},
		{"CampaignUses", func(c context.Context) error { _, e := f.s.CampaignUses(c, "missing"); return e }},
		{"AddPage", func(c context.Context) error { _, e := f.s.AddPage(c, store.Page{}); return e }},
		{"Pages", func(c context.Context) error { _, e := f.s.Pages(c, "missing"); return e }},
		{"GetPage", func(c context.Context) error { _, e := f.s.GetPage(c, "missing", -1); return e }},
		{"CountPages", func(c context.Context) error { _, e := f.s.CountPages(c, "missing"); return e }},
		{"Subject", func(c context.Context) error { _, e := f.s.Subject(c, "missing"); return e }},
		{"DueAmong", func(c context.Context) error { _, e := f.s.DueAmong(c, "invalid", []string{id}, true); return e }},
		{"DeriveAmong", func(c context.Context) error { _, e := f.s.DeriveAmong(c, "invalid", []string{id}, true); return e }},
		{"AddEnrichments", func(c context.Context) error {
			_, e := f.s.AddEnrichments(c, []store.Enrichment{{Candidate: "missing", Kind: "invalid"}})
			return e
		}},
		{"AddEnrichmentsValid", func(c context.Context) error {
			_, e := f.s.AddEnrichments(c, []store.Enrichment{websiteResult(id, sources.WebsiteHTTPS)})
			return e
		}},
		{"Enrichments", func(c context.Context) error { _, e := f.s.Enrichments(c, "missing"); return e }},
		{"DueEnrichments", func(c context.Context) error {
			_, _, e := f.s.DueEnrichments(c, sources.WebsiteCheck, "", 0)
			return e
		}},
		{"Derive", func(c context.Context) error { _, _, e := f.s.Derive(c, "invalid", "", false, 0); return e }},
		{"FillFacts", func(c context.Context) error { _, e := f.s.FillFacts(c, "", 500); return e }},
		{"GetFacts", func(c context.Context) error { _, e := f.s.GetFacts(c, id); return e }},
		{"EnrichmentMark", func(c context.Context) error { _, e := f.s.EnrichmentMark(c); return e }},
		{"EnrichmentAfter", func(c context.Context) error {
			_, e := f.s.EnrichmentAfter(c, sources.WebsiteCheck, "missing", -1)
			return e
		}},

		{"AddPass", func(c context.Context) error { _, e := f.s.AddPass(c, store.Pass{}); return e }},
		{"FinishPass", func(c context.Context) error { _, e := f.s.FinishPass(c, p.ID, store.Counts{Rows: -1}); return e }},
		{"FinishPassValid", func(c context.Context) error { _, e := f.s.FinishPass(c, p.ID, store.Counts{Rows: 1}); return e }},
		{"AddPassDuplicate", func(c context.Context) error {
			_, e := f.s.AddPass(c, store.Pass{Kind: store.KindIngest, Origin: store.OriginRun, Trigger: p.Trigger, Delivery: p.Delivery})
			return e
		}},
		{"FailPass", func(c context.Context) error { _, e := f.s.FailPass(c, p.ID, "invalid"); return e }},
		{"FailPassValid", func(c context.Context) error { _, e := f.s.FailPass(c, p.ID, store.ReasonStore); return e }},
		{"GetPass", func(c context.Context) error { _, e := f.s.GetPass(c, "missing"); return e }},
		{"FindDelivered", func(c context.Context) error { _, e := f.s.FindDelivered(c, p.Delivery, p.Trigger); return e }},
		{"ListPasses", func(c context.Context) error { _, e := f.s.ListPasses(c, "", "", 0); return e }},
		{"RunningPasses", func(c context.Context) error { _, e := f.s.RunningPasses(c); return e }},
		{"PrunePasses", func(c context.Context) error { _, e := f.s.PrunePasses(c, 0, 500); return e }},
		{"Attach", func(c context.Context) error { _, _, e := f.s.Attach(c, "", false, nil); return e }},
		{"AttachValid", func(c context.Context) error {
			_, _, e := f.s.Attach(c, "changed", false, []store.Observation{contractObservation("new", "New business", f.now)})
			return e
		}},
		{"MarkGone", func(c context.Context) error { _, _, e := f.s.MarkGone(c, "", "", "", 500); return e }},
		{"MarkGoneValid", func(c context.Context) error { _, _, e := f.s.MarkGone(c, "source-z", "absent", "", 500); return e }},
		{"Decide", func(c context.Context) error { _, _, e := f.s.Decide(c, store.Ruling{}); return e }},
		{"DecideValid", func(c context.Context) error {
			_, _, e := f.s.Decide(c, store.Ruling{Kind: store.KindExclude, Candidate: id, Why: "failure", UserID: "owner"})
			return e
		}},
		{"Judge", func(c context.Context) error { _, e := f.s.Judge(c, []string{id}); return e }},
		{"JudgeAll", func(c context.Context) error { _, _, e := f.s.JudgeAll(c, "", 500, f.now); return e }},
		{"ReplaceZipDrive", func(c context.Context) error { return f.s.ReplaceZipDrive(c, map[string]int64{"bad": -1}) }},
		{"ReplaceZipDriveValid", func(c context.Context) error { return f.s.ReplaceZipDrive(c, nil) }},
		{"ZipMinutes", func(c context.Context) error { _, e := f.s.ZipMinutes(c, "missing"); return e }},
		{"GetCandidate", func(c context.Context) error { _, e := f.s.GetCandidate(c, "missing"); return e }},
		{"Links", func(c context.Context) error { _, e := f.s.Links(c, "missing"); return e }},
		{"Estimates", func(c context.Context) error { _, e := f.s.Estimates(c, "missing"); return e }},
		{"GetJudgments", func(c context.Context) error { _, e := f.s.GetJudgments(c, "missing"); return e }},
		{"Rulings", func(c context.Context) error { _, e := f.s.Rulings(c, "missing"); return e }},
		{"CreateProspectList", func(c context.Context) error { _, e := f.s.CreateProspectList(c, store.ProspectList{}); return e }},
		{"EditProspectList", func(c context.Context) error { _, e := f.s.EditProspectList(c, store.ProspectListEdit{}); return e }},
		{"DeleteProspectList", func(c context.Context) error { _, e := f.s.DeleteProspectList(c, "missing"); return e }},
		{"GetProspectList", func(c context.Context) error { _, e := f.s.GetProspectList(c, "missing"); return e }},
		{"ReadProspectList", func(c context.Context) error { _, e := f.s.ReadProspectList(c, "missing"); return e }},
		{"ListProspectLists", func(c context.Context) error { _, e := f.s.ListProspectLists(c, "", "", 0); return e }},
		{"ListProspectListsReversed", func(c context.Context) error { _, e := f.s.ListProspectListsReversed(c, "", "", 0); return e }},
		{"Reject", func(c context.Context) error { _, e := f.s.Reject(c, store.Reject{}); return e }},
		{"Rejects", func(c context.Context) error { _, e := f.s.Rejects(c, "missing"); return e }},
		{"ListCandidates", func(c context.Context) error {
			_, e := f.s.ListCandidates(c, store.CandidateFilter{}, "", "", 0)
			return e
		}},
	}
	check := func(c context.Context, want error) {
		t.Helper()
		for _, call := range calls {
			e := call.call(c)
			contractError(t, e, store.ErrUnreachable)
			if want != nil {
				contractError(t, e, want)
			}
			for _, recordError := range []error{store.ErrNotFound, store.ErrBusy, store.ErrDelivered, store.ErrEnded, store.ErrInvalid} {
				if errors.Is(e, recordError) {
					t.Fatalf("%s returned record error %v", call.name, e)
				}
			}
		}
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	f.d.SetFailing(true)
	check(canceled, context.Canceled)
	check(ctx, nil)
	f.d.SetFailing(false)
	check(canceled, context.Canceled)
	contractEqual(t, contractState(t, f), before)
	f.reopen()
	contractEqual(t, contractState(t, f), before)
}

// R-242X-XODP R-5N3M-ADXX
func TestContractConcurrentStoreCalls(t *testing.T) {
	t.Parallel()
	f := contractDB(t)
	ctx := context.Background()
	id := contractSeed(t, f)
	var wg sync.WaitGroup
	results := make(chan error, 8)
	start := make(chan struct{})
	for i := range 8 {
		wg.Go(func() {
			<-start
			_, err := f.s.AddPass(ctx, store.Pass{Kind: store.KindIngest, Origin: store.OriginRun, Trigger: "one-run", Delivery: fmt.Sprintf("event-%d", i)})
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
		} else if !errors.Is(err, store.ErrBusy) && !errors.Is(err, store.ErrDelivered) {
			t.Fatal(err)
		}
	}
	contractEqual(t, success, 1)
	running, err := f.s.RunningPasses(ctx)
	contractOK(t, err)
	contractEqual(t, len(running), 1)
	p := running[0]
	results = make(chan error, 2)
	start = make(chan struct{})
	wg.Go(func() { <-start; _, e := f.s.FinishPass(ctx, p.ID, store.Counts{Rows: 5}); results <- e })
	wg.Go(func() { <-start; _, e := f.s.FailPass(ctx, p.ID, store.ReasonStore); results <- e })
	close(start)
	wg.Wait()
	close(results)
	success = 0
	for e := range results {
		if e == nil {
			success++
		} else {
			contractError(t, e, store.ErrEnded)
		}
	}
	contractEqual(t, success, 1)
	ended, err := f.s.GetPass(ctx, p.ID)
	contractOK(t, err)
	if ended.Status != store.StatusDone && ended.Status != store.StatusFailed {
		t.Fatal(ended)
	}
	// Every method shares the same Store; its non-thread-safe random reader is
	// exercised by both candidate and pass minting under the race detector.
	calls := []func() error{
		func() error {
			_, e := f.s.AddPass(ctx, store.Pass{Kind: store.KindIngest, Origin: store.OriginIngest, Trigger: store.TriggerIngest, UserID: "owner"})
			return e
		},
		func() error { _, e := f.s.FinishPass(ctx, p.ID, store.Counts{}); return e },
		func() error { _, e := f.s.FailPass(ctx, p.ID, store.ReasonScrapes); return e },
		func() error { _, e := f.s.GetPass(ctx, p.ID); return e },
		func() error { _, e := f.s.ListPasses(ctx, "", "", 20); return e },
		func() error { _, e := f.s.RunningPasses(ctx); return e },
		func() error { _, e := f.s.PrunePasses(ctx, 20, 500); return e },
		func() error {
			_, _, e := f.s.Attach(ctx, "parallel-run", false, []store.Observation{contractObservation("parallel", "Parallel business", f.now.UTC().Truncate(time.Second))})
			return e
		},
		func() error { _, _, e := f.s.MarkGone(ctx, "source-z", "parallel-run", "", 500); return e },
		func() error {
			_, _, e := f.s.Decide(ctx, store.Ruling{Kind: store.KindInclude, Candidate: id, Why: "parallel", UserID: "owner"})
			return e
		},
		func() error { _, e := f.s.Judge(ctx, []string{id}); return e },
		func() error { _, _, e := f.s.JudgeAll(ctx, "", 500, f.now); return e },
		func() error { return f.s.ReplaceZipDrive(ctx, map[string]int64{"55072": 42}) },
		func() error { _, e := f.s.ZipMinutes(ctx, "55072"); return e },
		func() error { _, e := f.s.GetCandidate(ctx, id); return e },
		func() error { _, e := f.s.Links(ctx, id); return e },
		func() error { _, e := f.s.Estimates(ctx, id); return e },
		func() error { _, e := f.s.GetJudgments(ctx, id); return e },
		func() error { _, e := f.s.Rulings(ctx, id); return e },
		func() error { _, e := f.s.ListCandidates(ctx, store.CandidateFilter{}, "", "", 20); return e },
	}
	start = make(chan struct{})
	for _, call := range calls {
		wg.Go(func() {
			<-start
			e := call()
			if e != nil && !errors.Is(e, store.ErrEnded) && !errors.Is(e, store.ErrBusy) {
				t.Error(e)
			}
		})
	}
	close(start)
	wg.Wait()
}

type contractErrorReader struct {
	err      error
	requests []int
}

func (r *contractErrorReader) Read(p []byte) (int, error) {
	r.requests = append(r.requests, len(p))
	return 0, r.err
}

// R-56RJ-1PXL
func TestContractMintReaderRecordErrorsAreUnreachable(t *testing.T) {
	t.Parallel()
	recordErrors := []struct {
		name string
		err  error
	}{
		{"not_found", store.ErrNotFound},
		{"busy", store.ErrBusy},
		{"delivered", store.ErrDelivered},
		{"ended", store.ErrEnded},
		{"invalid", store.ErrInvalid},
	}
	for _, kind := range []string{"pass", "candidate"} {
		for _, sentinel := range recordErrors {
			for _, wrapped := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/wrapped_%t", kind, sentinel.name, wrapped), func(t *testing.T) {
					t.Parallel()
					f := contractDB(t)
					ctx := context.Background()
					contractSeed(t, f)
					previous := contractNewPass(t, f, "previous-run")
					_, err := f.s.FinishPass(ctx, previous.ID, store.Counts{Rows: 3, Created: 1})
					contractOK(t, err)
					before := contractState(t, f)
					readError := sentinel.err
					if wrapped {
						readError = fmt.Errorf("reader failed: %w", readError)
					}
					rnd := &contractErrorReader{err: readError}
					f.rnd = rnd
					f.s = store.New(f.d, store.Config{Now: func() time.Time { return f.now }, Rand: rnd})
					if kind == "pass" {
						result, e := f.s.AddPass(ctx, store.Pass{Kind: store.KindIngest, Origin: store.OriginRun, Trigger: "new-run", Delivery: "new-delivery", UserID: "owner"})
						err = e
						contractEqual(t, result, store.Pass{})
					} else {
						// Include an update before the mint attempt to prove the failed batch
						// cannot retain earlier writes either.
						changed := contractObservation("evidence", "Changed seed business", f.now.UTC().Truncate(time.Second))
						changed.Digest = "changed-digest"
						newObservation := contractObservation("new-evidence", "New distinct business", f.now.UTC().Truncate(time.Second))
						counts, ids, e := f.s.Attach(ctx, "new-run", false, []store.Observation{changed, newObservation})
						err = e
						contractEqual(t, counts, store.Counts{})
						if ids != nil {
							t.Fatalf("failed mint returned touched ids: %v", ids)
						}
					}
					contractError(t, err, store.ErrUnreachable)
					for _, recordError := range recordErrors {
						if errors.Is(err, recordError.err) {
							t.Fatalf("random failure leaked %s classification: %v", recordError.name, err)
						}
					}
					contractEqual(t, rnd.requests, []int{8})
					contractEqual(t, contractState(t, f), before)
					f.reopen()
					contractEqual(t, contractState(t, f), before)
				})
			}
		}
	}
}

func ptrInt(n int64) *int64 { return &n }
