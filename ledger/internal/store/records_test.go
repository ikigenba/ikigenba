package store_test

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	ledger "github.com/ikigenba/ikigenba/ledger"
	"github.com/ikigenba/ikigenba/ledger/internal/store"
)

type recordRandom struct {
	n     byte
	reads int
}

func (r *recordRandom) Read(p []byte) (int, error) {
	r.reads += len(p)
	for i := range p {
		r.n++
		p[i] = r.n
	}
	return len(p), nil
}

type recordFixture struct {
	s      *store.Store
	d      *db.DB
	path   string
	now    time.Time
	random *recordRandom
}

func newRecords(t *testing.T) *recordFixture {
	t.Helper()
	f := &recordFixture{path: filepath.Join(t.TempDir(), "ledger.db"), now: time.Date(2026, 4, 2, 12, 3, 4, 567, time.FixedZone("test", -5*3600)), random: &recordRandom{}}
	f.open(t)
	t.Cleanup(func() {
		if err := f.d.Close(); err != nil {
			t.Error(err)
		}
	})
	return f
}
func (f *recordFixture) open(t *testing.T) {
	t.Helper()
	d, e := db.Open(context.Background(), db.Config{Path: f.path, Migrations: ledger.Migrations(), Now: func() time.Time { return f.now }, Service: "ledger", Stderr: io.Discard})
	if e != nil {
		t.Fatal(e)
	}
	f.d = d
	f.s = store.New(d, store.Config{Now: func() time.Time { return f.now }, Rand: f.random})
}
func (f *recordFixture) reopen(t *testing.T) {
	t.Helper()
	if err := f.d.Close(); err != nil {
		t.Fatal(err)
	}
	f.open(t)
}
func (f *recordFixture) account(t *testing.T, name string) {
	t.Helper()
	if _, _, e := f.s.OpenAccount(context.Background(), store.AccountInput{Name: name, Date: "2026-01-01", UserID: "owner"}); e != nil {
		t.Fatal(e)
	}
}
func (f *recordFixture) post(t *testing.T, date, a, b, amount string) store.Transaction {
	t.Helper()
	tr, _, e := f.s.Post(context.Background(), store.PostInput{Date: date, Postings: []store.PostingInput{{Account: a, Amount: &amount}, {Account: b}}, UserID: "poster"})
	if e != nil {
		t.Fatal(e)
	}
	return tr
}
func recordErr(t *testing.T, got, want error) {
	t.Helper()
	if !errors.Is(got, want) {
		t.Fatalf("error %v, want %v", got, want)
	}
}
func recordEqual(t *testing.T, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v; want %#v", got, want)
	}
}
func recordLines(lines []store.Line) map[string]int64 {
	m := map[string]int64{}
	for _, line := range lines {
		m[line.Account] = line.Amount.Units
	}
	return m
}

func TestAssertionRecordsAndLiveRows(t *testing.T) {
	// R-PISE-ZJVI R-PK0B-DBM7
	f := newRecords(t)
	ctx := context.Background()
	f.account(t, "Assets:Bank:Checking")
	f.account(t, "Equity:Opening")
	f.post(t, "2026-02-01", "Assets:Bank:Checking", "Equity:Opening", "120.00")
	in := store.AssertionInput{Account: "Assets:Bank", Date: "2026-03-01", Amount: "100.00", UserID: "checker"}
	r, e := f.s.AssertBalance(ctx, in)
	if e != nil {
		t.Fatal(e)
	}
	if !store.ValidAssertionID(r.Assertion.ID) || r.Assertion.Account != in.Account || r.Assertion.Date != in.Date || r.Assertion.Expected.Units != 10000 || r.Assertion.AddedUserID != in.UserID || !r.Assertion.Added.Equal(f.now.UTC().Truncate(time.Second)) || !r.Assertion.Retracted.IsZero() || r.Assertion.RetractedUserID != "" || r.Actual.Units != 12000 || r.Difference.Units != 2000 || r.Status != store.StatusFails {
		t.Fatalf("bad assertion: %+v", r)
	}
	for _, a := range []store.Amount{r.Assertion.Expected, r.Actual, r.Difference} {
		if a.Commodity != "USD" || a.Precision != 2 {
			t.Fatal(a)
		}
	}
	r2, e := f.s.AssertBalance(ctx, in)
	if e != nil || r2.Assertion.ID == r.Assertion.ID {
		t.Fatalf("second assertion %+v %v", r2, e)
	}
	root, e := f.s.AssertBalance(ctx, store.AssertionInput{Account: store.RootAssets, Date: "2025-12-31", Amount: "0", UserID: "checker"})
	if e != nil || root.Status != store.StatusHolds {
		t.Fatalf("root %+v %v", root, e)
	}
	pre, e := f.s.AssertBalance(ctx, store.AssertionInput{Account: "Assets:Bank:Checking", Date: "2025-12-31", Amount: "0", UserID: "checker"})
	if e != nil || pre.Status != store.StatusHolds {
		t.Fatalf("before open %+v %v", pre, e)
	}
	if _, _, e = f.s.CloseAccount(ctx, store.AccountInput{Name: "Assets:Bank:Checking", Date: "2026-03-01", UserID: "closer"}); e != nil {
		t.Fatal(e)
	}
	after, e := f.s.AssertBalance(ctx, store.AssertionInput{Account: "Assets:Bank:Checking", Date: "2026-03-02", Amount: "120", UserID: "checker"})
	if e != nil || after.Status != store.StatusHolds {
		t.Fatalf("after close %+v %v", after, e)
	}
	f.reopen(t)
	rows, e := f.s.ListFailing(ctx, "", "", "", 100)
	if e != nil {
		t.Fatal(e)
	}
	recordEqual(t, rows, []store.AssertionRow{r, r2})
	f.post(t, "2026-03-01", "Assets:Bank:Checking", "Equity:Opening", "-20")
	updated, _, e := f.s.RetractAssertion(ctx, store.Retraction{ID: r.Assertion.ID, UserID: "reviewer"})
	if e != nil {
		t.Fatal(e)
	}
	if updated.Status != store.StatusHolds || updated.Actual.Units != 10000 || updated.Difference.Units != 0 {
		t.Fatalf("not live: %+v", updated)
	}
}
func TestNoteAndDocumentRecords(t *testing.T) {
	// R-PQ3T-A6BO R-PTRI-FHJR
	f := newRecords(t)
	ctx := context.Background()
	for _, name := range []string{"Assets:Bank", "Income:Sales", "Expenses:Office"} {
		f.account(t, name)
	}
	tr := f.post(t, "2026-01-01", "Assets:Bank", "Income:Sales", "5")
	note, e := f.s.AddNote(ctx, store.NoteInput{Account: "Expenses:Office", Date: "2026-02-01", Text: " \n請求書\t ", UserID: "scribe"})
	if e != nil {
		t.Fatal(e)
	}
	if !store.ValidNoteID(note.ID) || note.Account != "Expenses:Office" || note.Date != "2026-02-01" || note.Text != "請求書" || note.AddedUserID != "scribe" || !note.Added.Equal(f.now.UTC().Truncate(time.Second)) || !note.Retracted.IsZero() || note.RetractedUserID != "" {
		t.Fatal(note)
	}
	in := store.DocumentInput{Account: "Expenses:Office", Transaction: tr.ID, Date: "2026-03-01", Path: "receipts/march", Artifact: "artifact-a", UserID: "filer"}
	doc, e := f.s.AddDocument(ctx, in)
	if e != nil {
		t.Fatal(e)
	}
	doc2, e := f.s.AddDocument(ctx, in)
	if e != nil || doc.ID == doc2.ID {
		t.Fatalf("duplicate path %+v %v", doc2, e)
	}
	if !store.ValidDocumentID(doc.ID) || doc.Account != in.Account || doc.Transaction != tr.ID || doc.Date != in.Date || doc.Path != in.Path || doc.Artifact != in.Artifact || doc.AddedUserID != in.UserID || !doc.Added.Equal(f.now.UTC().Truncate(time.Second)) || !doc.Retracted.IsZero() || doc.RetractedUserID != "" {
		t.Fatal(doc)
	}
	f.reopen(t)
	read, e := f.s.GetAccount(ctx, in.Account, "")
	if e != nil {
		t.Fatal(e)
	}
	recordEqual(t, read.Notes, []store.Note{note})
	recordEqual(t, read.Documents, []store.Document{doc, doc2})
}
func TestRetractionsKeepFirstActorAndStamp(t *testing.T) {
	// R-WOOE-9DLE
	f := newRecords(t)
	ctx := context.Background()
	f.account(t, "Assets:Bank")
	a, e := f.s.AssertBalance(ctx, store.AssertionInput{Account: "Assets:Bank", Date: "2026-02-01", Amount: "0", UserID: "first"})
	if e != nil {
		t.Fatal(e)
	}
	n, e := f.s.AddNote(ctx, store.NoteInput{Account: "Assets:Bank", Date: "2026-02-01", Text: "kept", UserID: "first"})
	if e != nil {
		t.Fatal(e)
	}
	d, e := f.s.AddDocument(ctx, store.DocumentInput{Account: "Assets:Bank", Date: "2026-02-01", Path: "receipt", Artifact: "artifact", UserID: "first"})
	if e != nil {
		t.Fatal(e)
	}
	lock := "2026-12-31"
	if _, _, e = f.s.SetBook(ctx, store.BookEdit{LockDate: &lock, UserID: "locker"}); e != nil {
		t.Fatal(e)
	}
	f.now = f.now.Add(time.Hour)
	stamp := f.now.UTC().Truncate(time.Second)
	ar, changed, e := f.s.RetractAssertion(ctx, store.Retraction{ID: a.Assertion.ID, UserID: "second"})
	if e != nil || !changed {
		t.Fatal(changed, e)
	}
	a.Assertion.Retracted = stamp
	a.Assertion.RetractedUserID = "second"
	recordEqual(t, ar, a)
	nr, changed, e := f.s.RetractNote(ctx, store.Retraction{ID: n.ID, UserID: "second"})
	if e != nil || !changed {
		t.Fatal(changed, e)
	}
	n.Retracted = stamp
	n.RetractedUserID = "second"
	recordEqual(t, nr, n)
	dr, changed, e := f.s.RetractDocument(ctx, store.Retraction{ID: d.ID, UserID: "second"})
	if e != nil || !changed {
		t.Fatal(changed, e)
	}
	d.Retracted = stamp
	d.RetractedUserID = "second"
	recordEqual(t, dr, d)
	f.reopen(t)
	f.now = f.now.Add(time.Hour)
	ar, changed, e = f.s.RetractAssertion(ctx, store.Retraction{ID: a.Assertion.ID, UserID: "third"})
	if e != nil || changed {
		t.Fatal(changed, e)
	}
	recordEqual(t, ar, a)
	nr, changed, e = f.s.RetractNote(ctx, store.Retraction{ID: n.ID, UserID: "third"})
	if e != nil || changed {
		t.Fatal(changed, e)
	}
	recordEqual(t, nr, n)
	dr, changed, e = f.s.RetractDocument(ctx, store.Retraction{ID: d.ID, UserID: "third"})
	if e != nil || changed {
		t.Fatal(changed, e)
	}
	recordEqual(t, dr, d)
	read, e := f.s.GetAccount(ctx, "Assets:Bank", "")
	if e != nil {
		t.Fatal(e)
	}
	recordEqual(t, read.Assertions, []store.AssertionRow{a})
	recordEqual(t, read.Notes, []store.Note{n})
	recordEqual(t, read.Documents, []store.Document{d})
}
func TestRecordRefusals(t *testing.T) {
	// R-PL87-R3CW R-PRBP-NY2D R-PUZE-T9AG R-PNO0-IMUA
	f := newRecords(t)
	ctx := context.Background()
	f.account(t, "Assets:Bank")
	if _, _, e := f.s.CloseAccount(ctx, store.AccountInput{Name: "Assets:Bank", Date: "2026-03-31", UserID: "owner"}); e != nil {
		t.Fatal(e)
	}
	lock := "2026-02-01"
	if _, _, e := f.s.SetBook(ctx, store.BookEdit{LockDate: &lock, UserID: "owner"}); e != nil {
		t.Fatal(e)
	}
	reads := f.random.reads
	for _, tc := range []struct {
		in  store.AssertionInput
		err error
	}{
		{store.AssertionInput{}, store.ErrUnreachable},
		{store.AssertionInput{Account: "bad", Date: "2026-01-01", Amount: "0", UserID: "u"}, store.ErrInvalid},
		{store.AssertionInput{Account: "Assets", Date: "bad", Amount: "0", UserID: "u"}, store.ErrInvalid},
		{store.AssertionInput{Account: "Assets", Date: "2026-01-01", Amount: "1.001", UserID: "u"}, store.ErrInvalid},
		{store.AssertionInput{Account: "Assets", Date: "2026-02-01", Amount: "0", UserID: "u"}, store.ErrLocked},
	} {
		r, e := f.s.AssertBalance(ctx, tc.in)
		recordErr(t, e, tc.err)
		recordEqual(t, r, store.AssertionRow{})
	}
	for _, tc := range []struct {
		account, date, text, user string
		err                       error
	}{
		{"bad", "bad", "", "", store.ErrUnreachable}, {"bad", "2026-01-01", "x", "u", store.ErrInvalid}, {"Assets:Bank", "bad", "x", "u", store.ErrInvalid}, {"Assets:Bank", "2026-01-01", " \t ", "u", store.ErrInvalid}, {"Assets:Bank", "2026-01-01", strings.Repeat("字", store.MaxNote+1), "u", store.ErrInvalid}, {"Assets:Missing", "2026-01-01", "x", "u", store.ErrNotFound}, {"Assets:Bank", "2025-12-31", "x", "u", store.ErrInvalid}, {"Assets:Bank", "2026-04-01", "x", "u", store.ErrInvalid}, {"Assets:Bank", "2026-02-01", "x", "u", store.ErrLocked},
	} {
		r, e := f.s.AddNote(ctx, store.NoteInput{Account: tc.account, Date: tc.date, Text: tc.text, UserID: tc.user})
		recordErr(t, e, tc.err)
		recordEqual(t, r, store.Note{})
	}
	for _, tc := range []struct {
		in  store.DocumentInput
		err error
	}{
		{store.DocumentInput{}, store.ErrUnreachable},
		{store.DocumentInput{UserID: "u"}, store.ErrUnreachable},
		{store.DocumentInput{UserID: "u", Artifact: "a", Account: "bad", Date: "bad"}, store.ErrInvalid},
		{store.DocumentInput{UserID: "u", Artifact: "a", Account: "Assets:Bank", Date: "2026-01-01"}, store.ErrInvalid},
		{store.DocumentInput{UserID: "u", Artifact: "a", Account: "Assets:Missing", Date: "2026-01-01", Path: "p"}, store.ErrNotFound},
		{store.DocumentInput{UserID: "u", Artifact: "a", Account: "Assets:Bank", Date: "2025-12-31", Path: "p", Transaction: "missing"}, store.ErrNotFound},
		{store.DocumentInput{UserID: "u", Artifact: "a", Account: "Assets:Bank", Date: "2025-12-31", Path: "p"}, store.ErrInvalid},
		{store.DocumentInput{UserID: "u", Artifact: "a", Account: "Assets:Bank", Date: "2026-04-01", Path: "p"}, store.ErrInvalid},
		{store.DocumentInput{UserID: "u", Artifact: "a", Account: "Assets:Bank", Date: "2026-02-01", Path: "p"}, store.ErrLocked},
	} {
		r, e := f.s.AddDocument(ctx, tc.in)
		recordErr(t, e, tc.err)
		recordEqual(t, r, store.Document{})
	}
	for _, user := range []string{"", "u"} {
		want := store.ErrNotFound
		if user == "" {
			want = store.ErrUnreachable
		}
		for _, id := range []string{"bad", "bal_0000000000000000", "note_0000000000000000", "doc_0000000000000000"} {
			r, c, e := f.s.RetractAssertion(ctx, store.Retraction{ID: id, UserID: user})
			recordErr(t, e, want)
			recordEqual(t, r, store.AssertionRow{})
			if c {
				t.Fatal("changed")
			}
			n, c, e := f.s.RetractNote(ctx, store.Retraction{ID: id, UserID: user})
			recordErr(t, e, want)
			recordEqual(t, n, store.Note{})
			if c {
				t.Fatal("changed")
			}
			d, c, e := f.s.RetractDocument(ctx, store.Retraction{ID: id, UserID: user})
			recordErr(t, e, want)
			recordEqual(t, d, store.Document{})
			if c {
				t.Fatal("changed")
			}
		}
	}
	if f.random.reads != reads {
		t.Fatal("refusal consumed random")
	}
	status, e := f.s.Status(ctx)
	if e != nil {
		t.Fatal(e)
	}
	if status.FailingAssertions != 0 {
		t.Fatal("assertion refusal wrote records")
	}
	failing, e := f.s.ListFailing(ctx, "", "", "", 100)
	if e != nil || len(failing) != 0 {
		t.Fatal(failing, e)
	}
	read, e := f.s.GetAccount(ctx, "Assets:Bank", "")
	if e != nil {
		t.Fatal(e)
	}
	if len(read.Assertions)+len(read.Notes)+len(read.Documents) != 0 {
		t.Fatal("refusal changed records")
	}
}
func TestDocumentPreflight(t *testing.T) {
	// R-WNGH-VLUP
	f := newRecords(t)
	ctx := context.Background()
	f.account(t, "Assets:Bank")
	reads := f.random.reads
	base := store.DocumentInput{Account: "Assets:Bank", Date: "2026-02-01", Path: "receipt", UserID: "u"}
	inputs := []store.DocumentInput{base, {Account: "Assets:Missing", Date: base.Date, Path: base.Path, UserID: "u"}, {Account: base.Account, Date: "bad", Path: base.Path, UserID: "u"}, {Account: base.Account, Date: base.Date, Path: base.Path, Transaction: "missing", UserID: "u"}, {Account: base.Account, Date: base.Date, Path: base.Path}}
	for _, in := range inputs {
		before, e := f.s.GetAccount(ctx, base.Account, "")
		if e != nil {
			t.Fatal(e)
		}
		pre := f.s.CheckDocument(ctx, in)
		after, e := f.s.GetAccount(ctx, base.Account, "")
		if e != nil {
			t.Fatal(e)
		}
		recordEqual(t, after, before)
		if f.random.reads != reads {
			t.Fatal("preflight consumed random")
		}
		in.Artifact = "artifact"
		_, add := f.s.AddDocument(ctx, in)
		if pre == nil {
			if add != nil {
				t.Fatal(add)
			}
			reads = f.random.reads
		} else {
			for _, kind := range []error{store.ErrNotFound, store.ErrInvalid, store.ErrLocked, store.ErrUnreachable} {
				if errors.Is(pre, kind) != errors.Is(add, kind) {
					t.Fatalf("preflight %v add %v", pre, add)
				}
			}
		}
	}
	read, e := f.s.GetAccount(ctx, base.Account, "")
	if e != nil {
		t.Fatal(e)
	}
	if len(read.Documents) != 1 {
		t.Fatal(read.Documents)
	}
	lock := "2026-02-01"
	if _, _, e = f.s.SetBook(ctx, store.BookEdit{LockDate: &lock, UserID: "u"}); e != nil {
		t.Fatal(e)
	}
	recordErr(t, f.s.CheckDocument(ctx, base), store.ErrLocked)
}
func TestFailingWindows(t *testing.T) {
	// R-POVW-WEKZ
	f := newRecords(t)
	ctx := context.Background()
	f.account(t, "Assets:Bank")
	f.account(t, "Equity:Opening")
	var expected []store.AssertionRow
	for _, in := range []store.AssertionInput{{Account: "Assets:Bank", Date: "2026-02-01", Amount: "1", UserID: "u"}, {Account: "Assets", Date: "2026-02-02", Amount: "2", UserID: "u"}, {Account: "Assets:Bank", Date: "2026-02-02", Amount: "3", UserID: "u"}, {Account: "Assets:Bank", Date: "2026-02-02", Amount: "4", UserID: "u"}} {
		r, e := f.s.AssertBalance(ctx, in)
		if e != nil {
			t.Fatal(e)
		}
		expected = append(expected, r)
	}
	for _, in := range []store.AssertionInput{{Account: "Assets", Date: "2026-02-01", Amount: "0", UserID: "u"}, {Account: "Assets", Date: "2026-12-01", Amount: "1", UserID: "u"}} {
		if _, e := f.s.AssertBalance(ctx, in); e != nil {
			t.Fatal(e)
		}
	}
	retracted, e := f.s.AssertBalance(ctx, store.AssertionInput{Account: "Assets", Date: "2026-02-01", Amount: "1", UserID: "u"})
	if e != nil {
		t.Fatal(e)
	}
	if _, _, e = f.s.RetractAssertion(ctx, store.Retraction{ID: retracted.Assertion.ID, UserID: "u"}); e != nil {
		t.Fatal(e)
	}
	all, e := f.s.ListFailing(ctx, "ignored", "ignored", "", 100)
	if e != nil {
		t.Fatal(e)
	}
	recordEqual(t, all, expected)
	first, e := f.s.ListFailing(ctx, "", "", "", 2)
	if e != nil {
		t.Fatal(e)
	}
	recordEqual(t, first, expected[:2])
	last := first[1].Assertion
	rest, e := f.s.ListFailing(ctx, last.Date, last.Account, last.ID, 2)
	if e != nil {
		t.Fatal(e)
	}
	recordEqual(t, rest, expected[2:])
	arbitrary, e := f.s.ListFailing(ctx, "2026-02-02", "Assets:AAA", "nonexistent", 100)
	if e != nil {
		t.Fatal(e)
	}
	recordEqual(t, arbitrary, expected[2:])
	empty, e := f.s.ListFailing(ctx, "9999-12-31", "z", "z", 1)
	if e != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("empty %#v %v", empty, e)
	}
	f.post(t, "2026-02-01", "Assets:Bank", "Equity:Opening", "1")
	all, e = f.s.ListFailing(ctx, "", "", "", 100)
	if e != nil {
		t.Fatal(e)
	}
	if len(all) != 4 {
		t.Fatalf("fresh count %d", len(all))
	}
	for _, r := range all {
		if r.Assertion.ID == expected[0].Assertion.ID {
			t.Fatal("holding assertion returned")
		}
	}
}
func TestTrialBalances(t *testing.T) {
	// R-SR26-1FBX
	f := newRecords(t)
	ctx := context.Background()
	for _, name := range []string{"Assets:Bank:Checking", "Assets:Bank:Savings", "Assets:Zero", "Assets:Offset:A", "Assets:Offset:B", "Equity:Opening"} {
		f.account(t, name)
	}
	f.post(t, "2026-02-01", "Assets:Bank:Checking", "Equity:Opening", "100")
	f.post(t, "2026-03-01", "Assets:Bank:Savings", "Equity:Opening", "50")
	f.post(t, "2026-03-01", "Assets:Offset:A", "Assets:Offset:B", "1")
	if _, _, e := f.s.CloseAccount(ctx, store.AccountInput{Name: "Assets:Zero", Date: "2026-03-01", UserID: "u"}); e != nil {
		t.Fatal(e)
	}
	r, e := f.s.Balances(ctx, "", "Assets:Bank")
	if e != nil {
		t.Fatal(e)
	}
	if r.Date != f.now.UTC().Format(store.DateLayout) || r.Total.Units != 15000 {
		t.Fatal(r)
	}
	recordEqual(t, recordLines(r.Lines), map[string]int64{"Assets:Bank": 15000, "Assets:Bank:Checking": 10000, "Assets:Bank:Savings": 5000})
	all, e := f.s.Balances(ctx, "2026-03-01", "")
	if e != nil {
		t.Fatal(e)
	}
	if all.Total.Units != 0 {
		t.Fatal(all)
	}
	lines := recordLines(all.Lines)
	if _, ok := lines["Assets:Offset"]; ok {
		t.Fatal("zero implied parent")
	}
	if _, ok := lines["Assets:Zero"]; !ok {
		t.Fatal("missing declared zero")
	}
	for i := 1; i < len(all.Lines); i++ {
		if all.Lines[i-1].Account >= all.Lines[i].Account {
			t.Fatal("unsorted")
		}
	}
	early, e := f.s.Balances(ctx, "2026-02-28", "Assets:Bank")
	if e != nil || early.Total.Units != 10000 {
		t.Fatal(early, e)
	}
}
func TestFinancialStatements(t *testing.T) {
	// R-PXF7-KSRU R-PYN3-YKIJ
	f := newRecords(t)
	ctx := context.Background()
	for _, name := range []string{"Assets:Bank", "Income:Sales", "Expenses:Office", "Expenses:Zero", store.EarningsCurrent, "Equity:Capital", "Liabilities:Loan", "Assets:Zero"} {
		f.account(t, name)
	}
	f.post(t, "2026-01-02", "Assets:Bank", "Income:Sales", "10")
	f.post(t, "2026-05-01", "Expenses:Office", "Assets:Bank", "20")
	f.post(t, "2026-03-01", "Assets:Bank", "Liabilities:Loan", "30")
	f.post(t, "2026-03-31", "Assets:Bank", "Income:Sales", "1000")
	f.post(t, "2026-04-01", "Expenses:Office", "Assets:Bank", "300")
	f.post(t, "2026-04-02", store.EarningsCurrent, "Equity:Capital", "5")
	fiscal := "04-01"
	if _, _, e := f.s.SetBook(ctx, store.BookEdit{FiscalYearStart: &fiscal, UserID: "u"}); e != nil {
		t.Fatal(e)
	}
	march, e := f.s.BalanceSheetAt(ctx, "2026-03-31")
	if e != nil {
		t.Fatal(e)
	}
	m := recordLines(march.Lines)
	if m[store.EarningsCurrent] != -101000 || m[store.EarningsPrevious] != 0 {
		t.Fatal(march)
	}
	april, e := f.s.BalanceSheetAt(ctx, "")
	if e != nil {
		t.Fatal(e)
	}
	m = recordLines(april.Lines)
	if april.Date != f.now.UTC().Format(store.DateLayout) || m[store.EarningsCurrent] != 30500 || m[store.EarningsPrevious] != -101000 || m["Equity:Earnings"] != -70500 || m[store.RootAssets]+m[store.RootLiabilities]+m[store.RootEquity] != 0 {
		t.Fatal(april)
	}
	recordEqual(t, m, map[string]int64{store.RootAssets: 74000, "Assets:Bank": 74000, "Assets:Zero": 0, store.RootLiabilities: -3000, "Liabilities:Loan": -3000, store.RootEquity: -71000, "Equity:Capital": -500, "Equity:Earnings": -70500, store.EarningsCurrent: 30500, store.EarningsPrevious: -101000})
	for i := 1; i < len(april.Lines); i++ {
		if april.Lines[i-1].Account >= april.Lines[i].Account {
			t.Fatal("unsorted")
		}
	}
	income, e := f.s.IncomeStatementFor(ctx, "2026-03-31", "2026-04-01")
	if e != nil {
		t.Fatal(e)
	}
	if income.From != "2026-03-31" || income.To != "2026-04-01" || income.Net.Units != 70000 {
		t.Fatal(income)
	}
	recordEqual(t, recordLines(income.Lines), map[string]int64{store.RootIncome: -100000, "Income:Sales": -100000, store.RootExpenses: 30000, "Expenses:Office": 30000, "Expenses:Zero": 0})
	for i := 1; i < len(income.Lines); i++ {
		if income.Lines[i-1].Account >= income.Lines[i].Account {
			t.Fatal("unsorted")
		}
	}
}
func TestReportRefusals(t *testing.T) {
	// R-Q12W-Q3ZX
	f := newRecords(t)
	ctx := context.Background()
	for _, tc := range [][2]string{{"bad", ""}, {"", "bad"}, {"2026-02-30", "Assets"}} {
		r, e := f.s.Balances(ctx, tc[0], tc[1])
		recordErr(t, e, store.ErrInvalid)
		recordEqual(t, r, store.Trial{})
	}
	for _, date := range []string{"bad", "2026-02-30"} {
		r, e := f.s.BalanceSheetAt(ctx, date)
		recordErr(t, e, store.ErrInvalid)
		recordEqual(t, r, store.BalanceSheet{})
	}
	for _, tc := range [][2]string{{"", "2026-01-01"}, {"2026-01-01", "bad"}, {"2026-02-01", "2026-01-01"}} {
		r, e := f.s.IncomeStatementFor(ctx, tc[0], tc[1])
		recordErr(t, e, store.ErrInvalid)
		recordEqual(t, r, store.IncomeStatement{})
	}
}
