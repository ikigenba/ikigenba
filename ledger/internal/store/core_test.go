package store_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"math"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	ledger "github.com/ikigenba/ikigenba/ledger"
	"github.com/ikigenba/ikigenba/ledger/internal/store"
)

type coreRandom struct {
	n         byte
	reads     int
	bytesRead int
	fail      bool
	constant  bool
}

func (r *coreRandom) Read(p []byte) (int, error) {
	r.reads++
	r.bytesRead += len(p)
	if r.fail {
		return 0, io.ErrUnexpectedEOF
	}
	for i := range p {
		p[i] = r.n
	}
	if !r.constant {
		r.n++
	}
	return len(p), nil
}

type coreFixture struct {
	s     *store.Store
	d     *db.DB
	path  string
	now   time.Time
	calls int
	rnd   *coreRandom
}

func coreNew(t *testing.T, migrations fs.FS) *coreFixture {
	t.Helper()
	f := &coreFixture{now: time.Date(2026, 3, 1, 23, 30, 12, 987654321, time.FixedZone("west", -5*3600)), rnd: &coreRandom{n: 0x5a}}
	f.path = filepath.Join(t.TempDir(), "ledger.db")
	var e error
	f.d, e = db.Open(context.Background(), db.Config{Path: f.path, Migrations: migrations, Now: func() time.Time { return f.now }})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = f.d.Close() })
	f.s = store.New(f.d, store.Config{Now: func() time.Time { f.calls++; return f.now }, Rand: f.rnd})
	return f
}
func corePtr[T any](v T) *T { return &v }
func coreOpen(t *testing.T, f *coreFixture, name string) {
	t.Helper()
	if _, _, e := f.s.OpenAccount(context.Background(), store.AccountInput{Name: name, Date: "2026-01-01", UserID: "owner"}); e != nil {
		t.Fatal(e)
	}
}
func corePost(t *testing.T, f *coreFixture) store.Transaction {
	t.Helper()
	r, _, e := f.s.Post(context.Background(), store.PostInput{Date: "2026-03-01", UserID: "owner", Postings: []store.PostingInput{{Account: "Assets:Bank:Checking", Amount: corePtr("100.00")}, {Account: "Income:Sales"}}})
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func coreErr(t *testing.T, e, want error) {
	t.Helper()
	if !errors.Is(e, want) {
		t.Fatalf("error %v, want %v", e, want)
	}
}

// R-MI9C-XYVP R-MJH9-BQME R-NZWX-RVI9 R-O14U-5N8Y
func TestCoreAmounts(t *testing.T) {
	for _, tc := range []struct {
		s  string
		p  int
		u  int64
		ok bool
	}{{"42.17", 2, 4217, true}, {"-1200", 2, -120000, true}, {"0.5", 2, 50, true}, {"42.", 2, 4200, true}, {"-0.00", 2, 0, true}, {"007", 2, 700, true}, {"9999999999999.99", 2, 999999999999999, true}, {"1.234", 2, 0, false}, {"+1", 2, 0, false}, {".5", 2, 0, false}, {"1,000", 2, 0, false}, {"1e3", 2, 0, false}, {" 1", 2, 0, false}, {"$1", 2, 0, false}, {"-", 2, 0, false}, {"", 2, 0, false}, {"10000000000000.00", 2, 0, false}, {"5.", 0, 5, true}, {"5.0", 0, 0, false}, {"0", 100, 0, true}, {"1", 100, 0, false}, {"0000000000000000000000001", 0, 1, true}} {
		u, ok := store.ParseAmount(tc.s, tc.p)
		if u != tc.u || ok != tc.ok {
			t.Errorf("ParseAmount(%q,%d)=%d,%v", tc.s, tc.p, u, ok)
		}
	}
	for _, tc := range []struct {
		u int64
		p int
		s string
	}{{4217, 2, "42.17"}, {50, 2, "0.50"}, {0, 2, "0.00"}, {-120000, 2, "-1200.00"}, {-5, 2, "-0.05"}, {7, 0, "7"}, {-7, -1, "-7"}, {math.MinInt64, 2, "-92233720368547758.08"}, {math.MinInt64, 0, "-9223372036854775808"}, {1, 20, "0.00000000000000000001"}} {
		if got := (store.Amount{Units: tc.u, Commodity: "USD", Precision: tc.p}).String(); got != tc.s {
			t.Errorf("format %d/%d=%q", tc.u, tc.p, got)
		}
	}
}

// R-MLX2-3A3S R-O2CQ-JEZN R-O3KM-X6QC R-O4SJ-AYH1 R-O60F-OQ7Q R-O78C-2HYF R-O8G8-G9P4
func TestCoreValidators(t *testing.T) {
	for _, tc := range []struct {
		fn      func(string) bool
		yes, no []string
	}{{store.ValidDate, []string{"2026-02-28", "2028-02-29", "0000-01-01", "9999-12-31"}, []string{"2026-02-29", "2026-02-30", "2026-2-01", "2026-02-01T00:00:00Z", ""}}, {store.ValidMonthDay, []string{"01-01", "04-30", "12-31"}, []string{"02-29", "04-31", "13-01", "00-10", "1-01", "01-01-"}}, {store.ValidAccountName, []string{"Assets:Bank:Checking", "Equity:Opening-Balances", "Expenses:2026", "Liabilities:Card", "Income:Sales"}, []string{"Assets", "Assets:", "Assets::Bank", "Assets:bank", "Asset:Bank", "assets:Bank", "Assets:Café"}}, {store.ValidFlag, []string{store.FlagPending, store.FlagCleared}, []string{"", "Pending", "unknown"}}, {store.ValidToken, []string{"trip-2026", "inv/2026.01_a", strings.Repeat("a", store.MaxToken)}, []string{"a b", "", strings.Repeat("a", store.MaxToken+1), "a:b", "é"}}, {store.ValidMetadataKey, []string{"invoice", "due-date", "aB_1", strings.Repeat("a", 1000)}, []string{"Invoice", "1a", "a.b", "", "a/b"}}, {store.ValidKey, []string{"inv:2026/0001", strings.Repeat("a", 100)}, []string{"", strings.Repeat("a", 101), "a b", "é"}}} {
		for _, s := range tc.yes {
			if !tc.fn(s) {
				t.Errorf("refused %q", s)
			}
		}
		for _, s := range tc.no {
			if tc.fn(s) {
				t.Errorf("accepted %q", s)
			}
		}
	}
	for _, tc := range []struct {
		fn     func(string) bool
		prefix string
	}{{store.ValidTransactionID, store.TransactionIDPrefix}, {store.ValidAssertionID, store.AssertionIDPrefix}, {store.ValidNoteID, store.NoteIDPrefix}, {store.ValidDocumentID, store.DocumentIDPrefix}} {
		if !tc.fn(tc.prefix + strings.Repeat("5a", 8)) {
			t.Fatal("valid id refused")
		}
		for _, s := range []string{tc.prefix + strings.Repeat("5A", 8), tc.prefix + "5a5a", tc.prefix + strings.Repeat("5a", 9), "bad_" + strings.Repeat("5a", 8)} {
			if tc.fn(s) {
				t.Errorf("accepted %q", s)
			}
		}
	}
}

// R-MDDR-EVWX R-MELN-SNNM R-MFTK-6FEB R-MN4Y-H1UH R-MOCU-UTL6 R-MPKR-8LBV R-MQSN-MD2K R-MS0K-04T9 R-MT8G-DWJY R-NLA5-6MLX R-NMI1-KECM R-OEJQ-D4EL R-OJFB-W7DD R-OKN8-9Z42 R-OLV4-NQUR R-ON31-1ILG R-OOAX-FAC5 R-OQQQ-6TTJ R-ORYM-KLK8 R-OT6I-YDAX R-OWU8-3OJ0
func TestCoreBookAndAccounts(t *testing.T) {
	ctx := context.Background()
	f := coreNew(t, ledger.Migrations())
	s, e := f.s.Status(ctx)
	if e != nil {
		t.Fatal(e)
	}
	want := store.Status{Book: store.Book{Currency: "USD", Precision: 2, FiscalYearStart: store.DefaultFiscalYearStart}}
	if !reflect.DeepEqual(s, want) {
		t.Fatalf("initial=%+v", s)
	}
	as, e := f.s.Accounts(ctx)
	if e != nil || as == nil || len(as) != 0 {
		t.Fatalf("accounts=%v %v", as, e)
	}
	for i, err := range []error{store.ErrNotFound, store.ErrInvalid, store.ErrLocked, store.ErrUnbalanced, store.ErrUnreachable} {
		if err == nil {
			t.Fatal("nil sentinel")
		}
		for j, other := range []error{store.ErrNotFound, store.ErrInvalid, store.ErrLocked, store.ErrUnbalanced, store.ErrUnreachable} {
			if i != j && errors.Is(err, other) {
				t.Fatal("sentinel wraps another")
			}
		}
	}
	if store.Unreachable == "" || strings.Contains(store.Unreachable, "\n") {
		t.Fatal("bad unavailable copy")
	}
	for _, edit := range []store.BookEdit{{}, {LockDate: corePtr("bad"), UserID: "owner"}, {FiscalYearStart: corePtr("02-29"), UserID: "owner"}} {
		_, _, e = f.s.SetBook(ctx, edit)
		if edit.UserID == "" {
			coreErr(t, e, store.ErrUnreachable)
		} else {
			coreErr(t, e, store.ErrInvalid)
		}
	}
	for _, lock := range []string{"2026-03-31", "2026-02-01", "2099-01-01", ""} {
		s, changed, e := f.s.SetBook(ctx, store.BookEdit{LockDate: &lock, FiscalYearStart: corePtr("04-01"), UserID: "editor"})
		if e != nil || !changed || s.Book.LockDate != lock || s.Book.FiscalYearStart != "04-01" || s.Book.UpdatedUserID != "editor" || !s.Book.Updated.Equal(f.now.UTC().Truncate(time.Second)) {
			t.Fatalf("book %v %v %v", s, changed, e)
		}
		_, changed, e = f.s.SetBook(ctx, store.BookEdit{LockDate: &lock, UserID: "other"})
		if e != nil || changed {
			t.Fatal("book retry changed")
		}
	}
	_, changed, e := f.s.SetBook(ctx, store.BookEdit{UserID: "owner"})
	if e != nil || changed {
		t.Fatal("empty edit changed")
	}
	for _, root := range []string{store.RootAssets, store.RootLiabilities, store.RootEquity, store.RootIncome, store.RootExpenses} {
		coreOpen(t, f, root+":Test")
	}
	coreOpen(t, f, "Assets:Bank:Checking")
	coreOpen(t, f, "Assets:Bank")
	a, changed, e := f.s.OpenAccount(ctx, store.AccountInput{Name: "Assets:Bank", Date: "bad", UserID: "another"})
	if e != nil || changed || a.OpenedUserID != "owner" {
		t.Fatal("open retry")
	}
	for _, in := range []store.AccountInput{{Name: "Assets", Date: "bad", UserID: "owner"}, {Name: "Assets:bank", UserID: "owner"}, {Name: "Assets:Other", Date: "bad", UserID: "owner"}, {Name: "Assets:Other", Date: "2026-01-01"}} {
		_, _, e = f.s.OpenAccount(ctx, in)
		if in.UserID == "" {
			coreErr(t, e, store.ErrUnreachable)
		} else {
			coreErr(t, e, store.ErrInvalid)
		}
	}
	for _, in := range []store.AccountInput{{Name: "Assets:Absent", Date: "bad", UserID: "owner"}, {Name: "Assets", Date: "2026-01-01", UserID: "owner"}, {Name: "Assets:Bank", Date: "2025-12-31", UserID: "owner"}, {Name: "Assets:Absent", Date: "2026-01-01", UserID: "owner"}, {Name: "Assets:Bank", Date: "2026-01-01"}} {
		_, _, e = f.s.CloseAccount(ctx, in)
		want := store.ErrInvalid
		if in.UserID == "" {
			want = store.ErrUnreachable
		} else if in.Name == "Assets:Absent" && in.Date != "bad" {
			want = store.ErrNotFound
		}
		coreErr(t, e, want)
	}
	a, changed, e = f.s.CloseAccount(ctx, store.AccountInput{Name: "Assets:Bank", Date: "2026-01-01", UserID: "closer"})
	if e != nil || !changed || a.Close != "2026-01-01" || a.ClosedUserID != "closer" {
		t.Fatal("close")
	}
	again, changed, e := f.s.CloseAccount(ctx, store.AccountInput{Name: a.Name, Date: "bad", UserID: "other"})
	if e != nil || changed || !reflect.DeepEqual(a, again) {
		t.Fatal("close retry")
	}
	again, changed, e = f.s.OpenAccount(ctx, store.AccountInput{Name: a.Name, Date: "bad", UserID: "other"})
	if e != nil || changed || !reflect.DeepEqual(a, again) {
		t.Fatal("closed open retry")
	}
	as, e = f.s.Accounts(ctx)
	if e != nil || len(as) != 7 {
		t.Fatalf("chart %v %v", as, e)
	}
	for i := 1; i < len(as); i++ {
		if as[i-1].Name >= as[i].Name {
			t.Fatal("unordered chart")
		}
	}
	for _, q := range []struct {
		name, date string
		want       error
	}{{"Assets", "", store.ErrInvalid}, {"Assets:Absent", "bad", store.ErrInvalid}, {"Assets:Absent", "", store.ErrNotFound}} {
		_, e = f.s.GetAccount(ctx, q.name, q.date)
		coreErr(t, e, q.want)
	}
	r, e := f.s.GetAccount(ctx, a.Name, "")
	if e != nil || r.Date != "2026-03-02" || r.Balance != (store.Amount{Units: 0, Commodity: "USD", Precision: 2}) || r.Assertions == nil || r.Notes == nil || r.Documents == nil {
		t.Fatalf("read %v %v", r, e)
	}
}

type coreAnswer struct {
	value   any
	changed bool
	err     error
}

func coreOne[T any](v T, e error) coreAnswer { return coreAnswer{value: v, err: e} }
func coreChanged[T any](v T, c bool, e error) coreAnswer {
	return coreAnswer{value: v, changed: c, err: e}
}

type coreCall struct {
	name string
	fn   func(context.Context) coreAnswer
}

func coreCalls(f *coreFixture, txn string, a store.Assertion, n store.Note, d store.Document) []coreCall {
	return []coreCall{
		{"status", func(c context.Context) coreAnswer { return coreOne(f.s.Status(c)) }},
		{"set", func(c context.Context) coreAnswer {
			return coreChanged(f.s.SetBook(c, store.BookEdit{UserID: "owner"}))
		}},
		{"open", func(c context.Context) coreAnswer {
			return coreChanged(f.s.OpenAccount(c, store.AccountInput{Name: "Assets:Bank:Checking", Date: "bad", UserID: "owner"}))
		}},
		{"close", func(c context.Context) coreAnswer {
			return coreChanged(f.s.CloseAccount(c, store.AccountInput{Name: "Assets:Missing", Date: "bad"}))
		}},
		{"accounts", func(c context.Context) coreAnswer { return coreOne(f.s.Accounts(c)) }},
		{"account", func(c context.Context) coreAnswer { return coreOne(f.s.GetAccount(c, "Assets:Bank:Checking", "")) }},
		{"post", func(c context.Context) coreAnswer {
			return coreChanged(f.s.Post(c, store.PostInput{Key: "global", UserID: "owner"}))
		}},
		{"transaction", func(c context.Context) coreAnswer { return coreOne(f.s.GetTransaction(c, txn)) }},
		{"annotate", func(c context.Context) coreAnswer {
			return coreChanged(f.s.Annotate(c, store.AnnotationEdit{Transaction: txn, UserID: "owner"}))
		}},
		{"reverse", func(c context.Context) coreAnswer {
			return coreChanged(f.s.Reverse(c, store.ReversalInput{Transaction: txn, UserID: "owner"}))
		}},
		{"versions", func(c context.Context) coreAnswer { return coreOne(f.s.ListVersions(c, txn, 0, 10)) }},
		{"journal", func(c context.Context) coreAnswer {
			return coreOne(f.s.ListTransactions(c, store.JournalFilter{}, "", 0, 10))
		}},
		{"register", func(c context.Context) coreAnswer {
			return coreOne(f.s.ListRegister(c, "Assets", "", "", "", 0, 0, 10))
		}},
		{"assert", func(c context.Context) coreAnswer { return coreOne(f.s.AssertBalance(c, store.AssertionInput{})) }},
		{"retract assertion", func(c context.Context) coreAnswer {
			return coreChanged(f.s.RetractAssertion(c, store.Retraction{ID: a.ID, UserID: "owner"}))
		}},
		{"failing", func(c context.Context) coreAnswer { return coreOne(f.s.ListFailing(c, "", "", "", 10)) }},
		{"note", func(c context.Context) coreAnswer { return coreOne(f.s.AddNote(c, store.NoteInput{})) }},
		{"retract note", func(c context.Context) coreAnswer {
			return coreChanged(f.s.RetractNote(c, store.Retraction{ID: n.ID, UserID: "owner"}))
		}},
		{"check document", func(c context.Context) coreAnswer {
			return coreOne(struct{}{}, f.s.CheckDocument(c, store.DocumentInput{}))
		}},
		{"document", func(c context.Context) coreAnswer { return coreOne(f.s.AddDocument(c, store.DocumentInput{})) }},
		{"retract document", func(c context.Context) coreAnswer {
			return coreChanged(f.s.RetractDocument(c, store.Retraction{ID: d.ID, UserID: "owner"}))
		}},
		{"balances", func(c context.Context) coreAnswer { return coreOne(f.s.Balances(c, "", "")) }},
		{"sheet", func(c context.Context) coreAnswer { return coreOne(f.s.BalanceSheetAt(c, "")) }},
		{"income", func(c context.Context) coreAnswer {
			return coreOne(f.s.IncomeStatementFor(c, "2026-01-01", "2026-12-31"))
		}},
	}
}
func coreSeed(t *testing.T, f *coreFixture) (store.Transaction, store.AssertionRow, store.Note, store.Document) {
	t.Helper()
	ctx := context.Background()
	coreOpen(t, f, "Assets:Bank:Checking")
	coreOpen(t, f, "Income:Sales")
	p := corePost(t, f)
	a, e := f.s.AssertBalance(ctx, store.AssertionInput{Account: "Assets:Bank:Checking", Date: "2026-03-01", Amount: "0", UserID: "owner"})
	if e != nil {
		t.Fatal(e)
	}
	n, e := f.s.AddNote(ctx, store.NoteInput{Account: "Assets:Bank:Checking", Date: "2026-03-01", Text: " memo ", UserID: "owner"})
	if e != nil {
		t.Fatal(e)
	}
	d, e := f.s.AddDocument(ctx, store.DocumentInput{Account: "Assets:Bank:Checking", Transaction: p.ID, Date: "2026-03-01", Path: "receipts/test", Artifact: "file-one", UserID: "owner"})
	if e != nil {
		t.Fatal(e)
	}
	return p, a, n, d
}

// R-WPWA-N5C3 R-ODBT-ZCNW R-NNPX-Y63B R-NOXU-BXU0 R-NRDN-3HBE
func TestCoreEveryMethodPreflight(t *testing.T) {
	f := coreNew(t, ledger.Migrations())
	p, a, n, d := coreSeed(t, f)
	calls := coreCalls(f, p.ID, a.Assertion, n, d)
	ctx := context.Background()
	for _, mode := range []string{"canceled", "deadline", "failing", "closed"} {
		t.Run(mode, func(t *testing.T) {
			c := ctx
			if mode == "canceled" {
				var cancel context.CancelFunc
				c, cancel = context.WithCancel(ctx)
				cancel()
			}
			if mode == "deadline" {
				var cancel context.CancelFunc
				c, cancel = context.WithDeadline(ctx, time.Unix(0, 0))
				defer cancel()
			}
			if mode == "failing" {
				f.d.SetFailing(true)
				defer f.d.SetFailing(false)
			}
			if mode == "closed" {
				if e := f.d.Close(); e != nil {
					t.Fatal(e)
				}
			}
			for _, call := range calls {
				before := f.rnd.reads
				r := call.fn(c)
				coreErr(t, r.err, store.ErrUnreachable)
				if c.Err() != nil {
					coreErr(t, r.err, c.Err())
				}
				for _, other := range []error{store.ErrNotFound, store.ErrInvalid, store.ErrLocked, store.ErrUnbalanced} {
					if errors.Is(r.err, other) {
						t.Fatalf("%s leaked %v", call.name, other)
					}
				}
				if r.changed || !reflect.ValueOf(r.value).IsZero() {
					t.Fatalf("%s nonzero failure: %+v", call.name, r)
				}
				if f.rnd.reads != before {
					t.Fatalf("%s read randomness", call.name)
				}
			}
		})
	}
	if e := f.d.Close(); e != nil {
		t.Fatal(e)
	}
	reopen, e := db.Open(ctx, db.Config{Path: f.path, Migrations: ledger.Migrations(), Now: func() time.Time { return f.now }})
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = reopen.Close() }()
	s := store.New(reopen, store.Config{Now: func() time.Time { return f.now }, Rand: f.rnd})
	status, e := s.Status(ctx)
	if e != nil || status.Transactions != 1 || status.Book.UpdatedUserID != "" {
		t.Fatalf("failure changed records %v %v", status, e)
	}
	r, e := s.GetAccount(ctx, "Assets:Bank:Checking", "")
	if e != nil || len(r.Assertions) != 1 || !r.Assertions[0].Assertion.Retracted.IsZero() || !r.Notes[0].Retracted.IsZero() || !r.Documents[0].Retracted.IsZero() {
		t.Fatal("failure changed records")
	}
}

// R-NTTF-V0SS R-NV1C-8SJH R-NW98-MKA6 R-NYP1-E3RK R-OFRM-QW5A R-OGZJ-4NVZ R-OI7F-IFMO R-OPIT-T22U R-OVMB-PWSB
func TestCorePersistenceAndAccountRollup(t *testing.T) {
	f := coreNew(t, ledger.Migrations())
	p, a, n, d := coreSeed(t, f)
	ctx := context.Background()
	coreOpen(t, f, "Assets:Bank")
	coreOpen(t, f, "Assets:Banking")
	coreOpen(t, f, "Assets:Bank:Savings")
	r, e := f.s.GetAccount(ctx, "Assets:Bank", "")
	if e != nil || r.Balance.Units != 10000 || r.Postings != 0 || r.Date != "2026-03-02" {
		t.Fatalf("rollup %+v %v", r, e)
	}
	r, e = f.s.GetAccount(ctx, "Assets:Bank:Checking", "2026-02-01")
	if e != nil || r.Balance.Units != 0 || r.Postings != 1 || len(r.Assertions) != 1 || r.Assertions[0].Status != store.StatusFails || r.Assertions[0].Actual.Units != 10000 || r.Assertions[0].Difference.Units != 10000 {
		t.Fatalf("account %+v %v", r, e)
	}
	for _, in := range []store.AccountInput{{Name: "Assets:Bank:Checking", Date: "2026-02-28", UserID: "owner"}} {
		_, _, e = f.s.CloseAccount(ctx, in)
		coreErr(t, e, store.ErrInvalid)
	}
	_, _, e = f.s.SetBook(ctx, store.BookEdit{LockDate: corePtr("2026-03-01"), UserID: "owner"})
	if e != nil {
		t.Fatal(e)
	}
	_, e = f.s.AssertBalance(ctx, store.AssertionInput{Account: "Assets", Date: "2026-03-01", Amount: "0", UserID: "owner"})
	coreErr(t, e, store.ErrLocked)
	if _, e = f.s.AssertBalance(ctx, store.AssertionInput{Account: "Assets", Date: "2026-03-02", Amount: "100", UserID: "owner"}); e != nil {
		t.Fatal(e)
	}
	before := f.calls
	closed, changed, e := f.s.CloseAccount(ctx, store.AccountInput{Name: "Assets:Bank:Checking", Date: p.Date, UserID: "closer"})
	if e != nil || !changed || f.calls-before != 1 || !closed.Closed.Equal(p.Posted) {
		t.Fatalf("close %+v %v %v", closed, changed, e)
	}
	_, _, e = f.s.CloseAccount(ctx, store.AccountInput{Name: "Assets:Bank", Date: "2026-01-01", UserID: "owner"})
	if e != nil {
		t.Fatal(e)
	}
	f.now = f.now.Add(time.Hour)
	for _, call := range []func() error{func() error {
		_, _, e := f.s.RetractAssertion(ctx, store.Retraction{ID: a.Assertion.ID, UserID: "retractor"})
		return e
	}, func() error {
		_, _, e := f.s.RetractNote(ctx, store.Retraction{ID: n.ID, UserID: "retractor"})
		return e
	}, func() error {
		_, _, e := f.s.RetractDocument(ctx, store.Retraction{ID: d.ID, UserID: "retractor"})
		return e
	}} {
		if e := call(); e != nil {
			t.Fatal(e)
		}
	}
	wanted, e := f.s.GetAccount(ctx, "Assets:Bank:Checking", "")
	if e != nil {
		t.Fatal(e)
	}
	if wanted.Assertions[0].Assertion.RetractedUserID != "retractor" || wanted.Notes[0].RetractedUserID != "retractor" || wanted.Documents[0].RetractedUserID != "retractor" {
		t.Fatal("retractions missing")
	}
	status, e := f.s.Status(ctx)
	if e != nil || status.OpenAccounts != 3 || status.Transactions != 1 || status.LatestDate != p.Date || status.FailingAssertions != 0 {
		t.Fatalf("status %+v %v", status, e)
	}
	if e = f.d.Close(); e != nil {
		t.Fatal(e)
	}
	next, e := db.Open(ctx, db.Config{Path: f.path, Migrations: ledger.Migrations(), Now: func() time.Time { return f.now }})
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = next.Close() }()
	reopened := store.New(next, store.Config{Now: func() time.Time { return f.now }, Rand: f.rnd})
	got, e := reopened.GetAccount(ctx, "Assets:Bank:Checking", "")
	if e != nil || !reflect.DeepEqual(got, wanted) {
		t.Fatalf("reopened %+v want %+v %v", got, wanted, e)
	}
	sp, e := reopened.Status(ctx)
	if e != nil || !reflect.DeepEqual(sp, status) {
		t.Fatalf("reopened status %+v %v", sp, e)
	}
	tp, e := reopened.GetTransaction(ctx, p.ID)
	if e != nil || !reflect.DeepEqual(tp, p) {
		t.Fatalf("reopened txn %+v %v", tp, e)
	}
}

// R-O9O4-U1FT R-OC3X-LKX7
func TestCoreMinting(t *testing.T) {
	ctx := context.Background()
	for _, kind := range []string{"transaction", "assertion", "note", "document", "reverse"} {
		t.Run(kind, func(t *testing.T) {
			f := coreNew(t, ledger.Migrations())
			p, a, n, d := coreSeed(t, f)
			f.rnd.n = 0x5a
			f.rnd.constant = true
			prefix := store.TransactionIDPrefix
			existing := p.ID
			var call func() coreAnswer
			switch kind {
			case "transaction":
				call = func() coreAnswer {
					return coreChanged(f.s.Post(ctx, store.PostInput{Date: "2026-03-02", UserID: "owner", Postings: []store.PostingInput{{Account: "Assets:Bank:Checking", Amount: corePtr("0")}, {Account: "Income:Sales"}}}))
				}
			case "assertion":
				prefix = store.AssertionIDPrefix
				existing = a.Assertion.ID
				call = func() coreAnswer {
					return coreOne(f.s.AssertBalance(ctx, store.AssertionInput{Account: "Assets", Date: "2026-03-02", Amount: "0", UserID: "owner"}))
				}
			case "note":
				prefix = store.NoteIDPrefix
				existing = n.ID
				call = func() coreAnswer {
					return coreOne(f.s.AddNote(ctx, store.NoteInput{Account: "Assets:Bank:Checking", Date: "2026-03-02", Text: "again", UserID: "owner"}))
				}
			case "document":
				prefix = store.DocumentIDPrefix
				existing = d.ID
				call = func() coreAnswer {
					return coreOne(f.s.AddDocument(ctx, store.DocumentInput{Account: "Assets:Bank:Checking", Date: "2026-03-02", Path: "receipt", Artifact: "file", UserID: "owner"}))
				}
			case "reverse":
				call = func() coreAnswer {
					return coreChanged(f.s.Reverse(ctx, store.ReversalInput{Transaction: p.ID, UserID: "owner"}))
				}
			}
			// Seed each kind's previously minted bytes, including different kinds sharing bytes.
			if !strings.HasPrefix(existing, prefix) {
				t.Fatal("wrong id prefix")
			}
			raw := strings.TrimPrefix(existing, prefix)
			var b byte
			switch raw[:2] {
			case "5a":
				b = 0x5a
			case "5b":
				b = 0x5b
			case "5c":
				b = 0x5c
			case "5d":
				b = 0x5d
			default:
				t.Fatal(raw)
			}
			f.rnd.n = b
			before := f.rnd.reads
			bytesBefore := f.rnd.bytesRead
			snapshot := coreSnapshot(t, f)
			r := call()
			if !reflect.DeepEqual(snapshot, coreSnapshot(t, f)) {
				t.Fatal("collision changed records")
			}
			coreErr(t, r.err, store.ErrUnreachable)
			if f.rnd.bytesRead-bytesBefore != 64 || f.rnd.reads-before != 8 || r.changed || !reflect.ValueOf(r.value).IsZero() {
				t.Fatalf("collision result %+v reads=%d", r, f.rnd.reads-before)
			}
			f.rnd.fail = true
			before = f.rnd.reads
			snapshot = coreSnapshot(t, f)
			r = call()
			if !reflect.DeepEqual(snapshot, coreSnapshot(t, f)) {
				t.Fatal("random failure changed records")
			}
			coreErr(t, r.err, store.ErrUnreachable)
			if f.rnd.reads-before != 1 || r.changed || !reflect.ValueOf(r.value).IsZero() {
				t.Fatal("random failure result")
			}
			status, e := f.s.Status(ctx)
			if e != nil || status.Transactions != 1 {
				t.Fatal("failed mint wrote")
			}
			f.rnd.fail = false
			f.rnd.constant = false
			f.rnd.n = b
			before = f.rnd.reads
			r = call()
			if r.err != nil || f.rnd.reads-before != 2 {
				t.Fatalf("collision retry %+v reads %d", r, f.rnd.reads-before)
			}
			var id string
			switch v := r.value.(type) {
			case store.Transaction:
				id = v.ID
			case store.AssertionRow:
				id = v.Assertion.ID
			case store.Note:
				id = v.ID
			case store.Document:
				id = v.ID
			}
			want := prefix + strings.Repeat(string("0123456789abcdef"[(b+1)>>4])+string("0123456789abcdef"[(b+1)&15]), 8)
			if id != want {
				t.Fatalf("id=%s want=%s", id, want)
			}
		})
	}
	// Independent kinds may use identical bytes without colliding.
	f := coreNew(t, ledger.Migrations())
	f.rnd.constant = true
	_, a, n, d := coreSeed(t, f)
	for _, id := range []string{a.Assertion.ID, n.ID, d.ID} {
		if !strings.HasSuffix(id, strings.Repeat("5a", 8)) {
			t.Fatal(id)
		}
	}
}

// R-MUGC-ROAN R-MVO9-5G1C R-MWW5-J7S1 R-MY41-WZIQ R-MZBY-AR9F R-N0JU-OJ04 R-N1RR-2AQT R-N2ZN-G2HI R-N5FG-7LYW R-N6NC-LDPL R-N7V8-Z5GA R-N935-CX6Z R-NAB1-QOXO R-NBIY-4GOD R-NCQU-I8F2 R-NDYQ-W05R R-NF6N-9RWG R-NGEJ-NJN5 R-NHMG-1BDU R-NIUC-F34J R-NK28-SUV8 R-GQ94-FEAD R-MH1G-K750
func TestCoreWordsAndRecordSurface(t *testing.T) {
	ctx := context.Background()
	f := coreNew(t, ledger.Migrations())
	coreOpen(t, f, "Assets:Bank:Checking")
	coreOpen(t, f, "Income:Sales")
	p, _, e := f.s.Post(ctx, store.PostInput{Date: "2026-03-01", UserID: "writer", Postings: []store.PostingInput{{Account: "Assets:Bank:Checking", Amount: corePtr("1")}, {Account: "Income:Sales"}}, Payee: "  " + strings.Repeat("界", store.MaxText) + "  ", Narration: " sale ", Tags: []string{"b", "a", "b"}, Links: []string{"b", "a"}, Metadata: map[string]string{"a": " untrimmed "}})
	if e != nil {
		t.Fatal(e)
	}
	if p.Seq != 1 || p.PostedUserID != "writer" || p.Key != "" || p.Reverses != "" || p.ReversedBy != "" || p.Postings[0].Position != 1 || p.Postings[1].Amount.String() != "-1.00" || p.Annotation.Version != 1 || p.Annotation.Flag != store.FlagPending || p.Annotation.Payee != strings.Repeat("界", store.MaxText) || p.Annotation.Narration != "sale" || !reflect.DeepEqual(p.Annotation.Tags, []string{"a", "b"}) || !reflect.DeepEqual(p.Annotation.Links, []string{"a", "b"}) || p.Annotation.Metadata["a"] != " untrimmed " || p.Annotation.WrittenUserID != "writer" || !p.Annotation.Written.Equal(p.Posted) {
		t.Fatalf("transaction %+v", p)
	}
	tags := []string{}
	for i := range store.MaxTokens {
		tags = append(tags, string(rune('a'+i)))
	}
	tags = append(tags, "a")
	_, _, e = f.s.Annotate(ctx, store.AnnotationEdit{Transaction: p.ID, Tags: &tags, UserID: "editor"})
	if e != nil {
		t.Fatal(e)
	}
	tags = append(tags, "z")
	_, _, e = f.s.Annotate(ctx, store.AnnotationEdit{Transaction: p.ID, Tags: &tags, UserID: "editor"})
	coreErr(t, e, store.ErrInvalid)
	for _, edit := range []store.AnnotationEdit{{Payee: corePtr(strings.Repeat("界", store.MaxText+1))}, {Narration: corePtr(strings.Repeat("界", store.MaxText+1))}, {Links: corePtr([]string{strings.Repeat("a", store.MaxToken+1)})}, {Metadata: corePtr(map[string]string{"a": " " + strings.Repeat("a", store.MaxMetadataValue) + " "})}} {
		edit.Transaction = p.ID
		edit.UserID = "editor"
		_, _, e = f.s.Annotate(ctx, edit)
		coreErr(t, e, store.ErrInvalid)
	}
	md := map[string]string{}
	for i := range store.MaxMetadata {
		md[string(rune('a'+i))] = strings.Repeat("界", store.MaxMetadataValue)
	}
	_, _, e = f.s.Annotate(ctx, store.AnnotationEdit{Transaction: p.ID, Metadata: &md, UserID: "editor"})
	if e != nil {
		t.Fatal(e)
	}
	md["z"] = "extra"
	_, _, e = f.s.Annotate(ctx, store.AnnotationEdit{Transaction: p.ID, Metadata: &md, UserID: "editor"})
	coreErr(t, e, store.ErrInvalid)
	reversal, _, e := f.s.Reverse(ctx, store.ReversalInput{Transaction: p.ID, Payee: corePtr(" refund "), Narration: corePtr(" cancelled "), Tags: corePtr([]string{}), Links: corePtr([]string{}), Metadata: corePtr(map[string]string{}), UserID: "reverser"})
	if e != nil || reversal.Annotation.Payee != "refund" || reversal.Annotation.Narration != "cancelled" || reversal.Annotation.Flag != store.FlagCleared || reversal.Annotation.Tags == nil || reversal.Annotation.Links == nil || reversal.Annotation.Metadata == nil {
		t.Fatalf("reversal %+v %v", reversal, e)
	}
	rows, e := f.s.ListRegister(ctx, "Assets", "", "", "", 0, 0, 10)
	if e != nil || len(rows) != 2 || rows[0].Transaction != p.ID || rows[0].Seq != p.Seq || rows[0].Position != 1 || rows[0].Account != "Assets:Bank:Checking" || rows[0].Amount.Units != 100 || rows[0].Balance.Units != 100 || rows[0].Narration != "sale" || rows[1].Balance.Units != 0 {
		t.Fatalf("register %+v %v", rows, e)
	}
	js, e := f.s.ListTransactions(ctx, store.JournalFilter{From: "2026-03-01", To: "2026-03-01", Account: "Assets", Tag: "a", Link: "a", Payee: p.Annotation.Payee, Flag: store.FlagPending}, "", 0, 10)
	if e != nil || len(js) != 1 || js[0].ID != p.ID {
		t.Fatal("journal filter")
	}
	a, e := f.s.AssertBalance(ctx, store.AssertionInput{Account: "Assets", Date: "2026-03-02", Amount: "0", UserID: "checker"})
	if e != nil || a.Status != store.StatusHolds || a.Assertion.Expected.String() != "0.00" || a.Assertion.AddedUserID != "checker" || a.Actual.Units != 0 || a.Difference.Units != 0 {
		t.Fatal("assertion")
	}
	note, e := f.s.AddNote(ctx, store.NoteInput{Account: "Assets:Bank:Checking", Date: "2026-03-02", Text: " " + strings.Repeat("界", store.MaxNote) + " ", UserID: "noter"})
	if e != nil || note.Text != strings.Repeat("界", store.MaxNote) || note.AddedUserID != "noter" {
		t.Fatal("note")
	}
	doc, e := f.s.AddDocument(ctx, store.DocumentInput{Account: note.Account, Transaction: p.ID, Date: note.Date, Path: "path", Artifact: "artifact", UserID: "documenter"})
	if e != nil || doc.Transaction != p.ID || doc.Path != "path" || doc.Artifact != "artifact" || doc.AddedUserID != "documenter" {
		t.Fatal("document")
	}
	trial, e := f.s.Balances(ctx, "", "Assets")
	if e != nil || trial.Date != "2026-03-02" || trial.Lines == nil || trial.Total.Units != 0 {
		t.Fatal("trial")
	}
	sheet, e := f.s.BalanceSheetAt(ctx, "")
	if e != nil || sheet.Date != "2026-03-02" || sheet.Lines == nil {
		t.Fatal("sheet")
	}
	income, e := f.s.IncomeStatementFor(ctx, "2026-01-01", "2026-12-31")
	if e != nil || income.From != "2026-01-01" || income.To != "2026-12-31" || income.Lines == nil || income.Net.Units != 0 {
		t.Fatal("income")
	}
}

func coreWorkflow(t *testing.T, f *coreFixture) []coreAnswer {
	t.Helper()
	ctx := context.Background()
	p, a, n, d := coreSeed(t, f)
	out := []coreAnswer{}
	run := func(fn func() coreAnswer) {
		before := f.calls
		out = append(out, fn())
		if f.calls-before != 1 {
			t.Fatalf("clock read %d times", f.calls-before)
		}
	}
	run(func() coreAnswer {
		return coreChanged(f.s.Annotate(ctx, store.AnnotationEdit{Transaction: p.ID, Payee: corePtr("edit"), UserID: "editor"}))
	})
	run(func() coreAnswer {
		return coreChanged(f.s.Reverse(ctx, store.ReversalInput{Transaction: p.ID, UserID: "owner"}))
	})
	run(func() coreAnswer {
		return coreChanged(f.s.RetractAssertion(ctx, store.Retraction{ID: a.Assertion.ID, UserID: "owner"}))
	})
	run(func() coreAnswer {
		return coreChanged(f.s.RetractNote(ctx, store.Retraction{ID: n.ID, UserID: "owner"}))
	})
	run(func() coreAnswer {
		return coreChanged(f.s.RetractDocument(ctx, store.Retraction{ID: d.ID, UserID: "owner"}))
	})
	run(func() coreAnswer {
		return coreChanged(f.s.SetBook(ctx, store.BookEdit{LockDate: corePtr("2026-03-01"), UserID: "owner"}))
	})
	run(func() coreAnswer {
		return coreChanged(f.s.CloseAccount(ctx, store.AccountInput{Name: "Income:Sales", Date: "2026-03-02", UserID: "owner"}))
	})
	for _, call := range coreCalls(f, p.ID, a.Assertion, n, d) {
		run(func() coreAnswer { return call.fn(ctx) })
	}
	return out
}

// R-Q5YI-96YP R-NTTF-V0SS
func TestCoreExpandedSchemaParity(t *testing.T) {
	data, e := fs.ReadFile(ledger.Migrations(), "0001_ledger.sql")
	if e != nil {
		t.Fatal(e)
	}
	expanded := fstest.MapFS{"0001_ledger.sql": {Data: data}, "0002_posting_values.sql": {Data: []byte("ALTER TABLE postings ADD COLUMN cost INTEGER;\nALTER TABLE postings ADD COLUMN price INTEGER;\n")}}
	base := coreWorkflow(t, coreNew(t, ledger.Migrations()))
	next := coreWorkflow(t, coreNew(t, expanded))
	if len(base) != len(next) {
		t.Fatal("workflow length")
	}
	for i := range base {
		if !reflect.DeepEqual(base[i].value, next[i].value) || base[i].changed != next[i].changed || !errors.Is(base[i].err, next[i].err) {
			t.Fatalf("expanded schema differs at %d: %+v vs %+v", i, base[i], next[i])
		}
	}
}

// R-ODBT-ZCNW
func TestCoreNoRandomOnNonMintingCalls(t *testing.T) {
	f := coreNew(t, ledger.Migrations())
	p, a, n, d := coreSeed(t, f)
	ctx := context.Background()
	if _, _, e := f.s.Reverse(ctx, store.ReversalInput{Transaction: p.ID, UserID: "owner"}); e != nil {
		t.Fatal(e)
	}
	f.rnd.fail = true
	before := f.rnd.reads
	for _, call := range coreCalls(f, p.ID, a.Assertion, n, d) {
		_ = call.fn(ctx)
	}
	if f.rnd.reads != before {
		t.Fatal("nonminting calls read randomness")
	}
	r, _, e := f.s.Post(ctx, store.PostInput{Date: "bad", UserID: "owner"})
	if !reflect.ValueOf(r).IsZero() {
		t.Fatal("invalid post result")
	}
	coreErr(t, e, store.ErrInvalid)
	if f.rnd.reads != before {
		t.Fatal("invalid post read randomness")
	}
}

// R-Q8EB-0QG3
func TestCoreConcurrentMethods(t *testing.T) {
	f := coreNew(t, ledger.Migrations())
	p, a, n, d := coreSeed(t, f)
	ctx := context.Background()
	for _, kind := range []string{"open", "close", "assertion", "note", "document", "post-key", "reverse"} {
		t.Run(kind, func(t *testing.T) {
			const count = 12
			answers := make([]coreAnswer, count)
			var wg sync.WaitGroup
			start := make(chan struct{})
			for i := range count {
				wg.Go(func() {
					<-start
					switch kind {
					case "open":
						answers[i] = coreChanged(f.s.OpenAccount(ctx, store.AccountInput{Name: "Assets:Concurrent", Date: "2026-01-01", UserID: "owner"}))
					case "close":
						answers[i] = coreChanged(f.s.CloseAccount(ctx, store.AccountInput{Name: "Assets:Concurrent", Date: "2026-03-02", UserID: "owner"}))
					case "assertion":
						answers[i] = coreChanged(f.s.RetractAssertion(ctx, store.Retraction{ID: a.Assertion.ID, UserID: "owner"}))
					case "note":
						answers[i] = coreChanged(f.s.RetractNote(ctx, store.Retraction{ID: n.ID, UserID: "owner"}))
					case "document":
						answers[i] = coreChanged(f.s.RetractDocument(ctx, store.Retraction{ID: d.ID, UserID: "owner"}))
					case "post-key":
						answers[i] = coreChanged(f.s.Post(ctx, store.PostInput{Key: "concurrent", Date: "2026-03-01", UserID: "owner", Postings: []store.PostingInput{{Account: "Assets:Bank:Checking", Amount: corePtr("0")}, {Account: "Income:Sales"}}}))
					case "reverse":
						answers[i] = coreChanged(f.s.Reverse(ctx, store.ReversalInput{Transaction: p.ID, UserID: "owner"}))
					}
				})
			}
			close(start)
			wg.Wait()
			changed := 0
			for _, r := range answers {
				if r.err != nil {
					t.Fatal(r.err)
				}
				if r.changed {
					changed++
				}
				if !reflect.DeepEqual(r.value, answers[0].value) {
					t.Fatal("concurrent results differ")
				}
			}
			if changed != 1 {
				t.Fatalf("changed=%d", changed)
			}
		})
	}
	const count = 12
	posted := make([]store.Transaction, count)
	errs := make([]error, count)
	var wg sync.WaitGroup
	for i := range count {
		wg.Go(func() {
			posted[i], _, errs[i] = f.s.Post(ctx, store.PostInput{Date: "2026-03-02", UserID: "owner", Postings: []store.PostingInput{{Account: "Assets:Bank:Checking", Amount: corePtr("0")}, {Account: "Income:Sales"}}})
		})
	}
	wg.Wait()
	ids := map[string]bool{}
	seqs := map[int64]bool{}
	for i, p := range posted {
		if errs[i] != nil {
			t.Fatal(errs[i])
		}
		if ids[p.ID] || seqs[p.Seq] {
			t.Fatal("duplicate transaction identity")
		}
		ids[p.ID] = true
		seqs[p.Seq] = true
	}
	status, e := f.s.Status(ctx)
	if e != nil || status.Transactions != count+3 {
		t.Fatalf("concurrent count %+v %v", status, e)
	}
}

// R-WR47-0X2S
func TestCoreMigrationBytes(t *testing.T) {
	got, e := fs.ReadFile(ledger.Migrations(), "0001_ledger.sql")
	if e != nil {
		t.Fatal(e)
	}
	want := `CREATE TABLE book (id INTEGER PRIMARY KEY CHECK (id = 1), currency TEXT NOT NULL, lock_date TEXT NOT NULL DEFAULT '', fiscal_year_start TEXT NOT NULL DEFAULT '01-01', updated TEXT NOT NULL DEFAULT '', updated_user_id TEXT NOT NULL DEFAULT '');
INSERT INTO book (id, currency) VALUES (1, 'USD');
CREATE TABLE commodities (code TEXT PRIMARY KEY, precision INTEGER NOT NULL) WITHOUT ROWID;
INSERT INTO commodities (code, precision) VALUES ('USD', 2);
CREATE TABLE accounts (name TEXT PRIMARY KEY, open_date TEXT NOT NULL, close_date TEXT NOT NULL DEFAULT '', opened TEXT NOT NULL, opened_user_id TEXT NOT NULL, closed TEXT NOT NULL DEFAULT '', closed_user_id TEXT NOT NULL DEFAULT '') WITHOUT ROWID;
CREATE TABLE transactions (seq INTEGER PRIMARY KEY, id TEXT NOT NULL, date TEXT NOT NULL, key TEXT NOT NULL DEFAULT '', reverses TEXT NOT NULL DEFAULT '', reversed_by TEXT NOT NULL DEFAULT '', version INTEGER NOT NULL, posted TEXT NOT NULL, posted_user_id TEXT NOT NULL) WITHOUT ROWID;
CREATE UNIQUE INDEX transactions_id ON transactions(id);
CREATE UNIQUE INDEX transactions_key ON transactions(key) WHERE key <> '';
CREATE UNIQUE INDEX transactions_reverses ON transactions(reverses) WHERE reverses <> '';
CREATE INDEX transactions_journal ON transactions(date,seq);
CREATE TABLE postings (txn INTEGER NOT NULL, position INTEGER NOT NULL, account TEXT NOT NULL, amount INTEGER NOT NULL, commodity TEXT NOT NULL, date TEXT NOT NULL, PRIMARY KEY (txn,position)) WITHOUT ROWID;
CREATE INDEX postings_register ON postings(date,txn,position);
CREATE INDEX postings_account ON postings(account,date);
CREATE TABLE annotations (txn INTEGER NOT NULL, version INTEGER NOT NULL, flag TEXT NOT NULL, payee TEXT NOT NULL, narration TEXT NOT NULL, tags TEXT NOT NULL, links TEXT NOT NULL, metadata TEXT NOT NULL, written TEXT NOT NULL, written_user_id TEXT NOT NULL, PRIMARY KEY (txn,version)) WITHOUT ROWID;
CREATE TABLE tags (tag TEXT NOT NULL, txn INTEGER NOT NULL, PRIMARY KEY (tag,txn)) WITHOUT ROWID;
CREATE TABLE links (link TEXT NOT NULL, txn INTEGER NOT NULL, PRIMARY KEY (link,txn)) WITHOUT ROWID;
CREATE TABLE assertions (id TEXT NOT NULL PRIMARY KEY, account TEXT NOT NULL, date TEXT NOT NULL, amount INTEGER NOT NULL, commodity TEXT NOT NULL, added TEXT NOT NULL, added_user_id TEXT NOT NULL, retracted TEXT NOT NULL DEFAULT '', retracted_user_id TEXT NOT NULL DEFAULT '');
CREATE INDEX assertions_check ON assertions(retracted,date,account,id);
CREATE INDEX assertions_account ON assertions(account,date,id);
CREATE TABLE notes (id TEXT NOT NULL PRIMARY KEY, account TEXT NOT NULL, date TEXT NOT NULL, text TEXT NOT NULL, added TEXT NOT NULL, added_user_id TEXT NOT NULL, retracted TEXT NOT NULL DEFAULT '', retracted_user_id TEXT NOT NULL DEFAULT '');
CREATE INDEX notes_account ON notes(account,date,id);
CREATE TABLE documents (id TEXT NOT NULL PRIMARY KEY, account TEXT NOT NULL, txn TEXT NOT NULL DEFAULT '', date TEXT NOT NULL, path TEXT NOT NULL, artifact TEXT NOT NULL, added TEXT NOT NULL, added_user_id TEXT NOT NULL, retracted TEXT NOT NULL DEFAULT '', retracted_user_id TEXT NOT NULL DEFAULT '');
CREATE INDEX documents_account ON documents(account,date,id);
`
	if !bytes.Equal(got, []byte(want)) {
		t.Fatal("migration bytes differ")
	}
}

// R-MELN-SNNM R-MFTK-6FEB R-MH1G-K750
func TestCoreConstantValues(t *testing.T) {
	stringsGot := []string{store.TransactionIDPrefix, store.AssertionIDPrefix, store.NoteIDPrefix, store.DocumentIDPrefix, store.FlagPending, store.FlagCleared, store.StatusHolds, store.StatusFails, store.DateLayout, store.DefaultFiscalYearStart, store.RootAssets, store.RootLiabilities, store.RootEquity, store.RootIncome, store.RootExpenses, store.EarningsCurrent, store.EarningsPrevious}
	stringsWant := []string{"txn_", "bal_", "note_", "doc_", "pending", "cleared", "holds", "fails", "2006-01-02", "01-01", "Assets", "Liabilities", "Equity", "Income", "Expenses", "Equity:Earnings:Current", "Equity:Earnings:Previous"}
	if !reflect.DeepEqual(stringsGot, stringsWant) {
		t.Fatal("constant values")
	}
	numbers := []int{store.MaxText, store.MaxToken, store.MaxTokens, store.MaxMetadata, store.MaxMetadataValue, store.MaxNote}
	if !reflect.DeepEqual(numbers, []int{200, 50, 20, 20, 500, 2000}) {
		t.Fatal("limit values")
	}
	type namedString string
	var untypedString namedString = store.TransactionIDPrefix
	type namedInt int
	var untypedInt namedInt = store.MaxText
	if untypedString != "txn_" || untypedInt != 200 {
		t.Fatal("untyped constants")
	}
}

func coreCheckValue(t *testing.T, value any) {
	t.Helper()
	checkAmount := func(a store.Amount) {
		if a.Commodity != "USD" || a.Precision != 2 {
			t.Fatalf("amount lacks commodity precision: %+v", a)
		}
	}
	switch v := value.(type) {
	case store.Amount:
		checkAmount(v)
	case store.Annotation:
		if v.Tags == nil || v.Links == nil || v.Metadata == nil {
			t.Fatalf("nil word collections: %+v", v)
		}
	case store.Posting:
		checkAmount(v.Amount)
	case store.Transaction:
		if v.Postings == nil {
			t.Fatal("nil postings")
		}
		for _, p := range v.Postings {
			coreCheckValue(t, p)
		}
		coreCheckValue(t, v.Annotation)
	case store.AssertionRow:
		checkAmount(v.Assertion.Expected)
		checkAmount(v.Actual)
		checkAmount(v.Difference)
	case store.AccountRead:
		checkAmount(v.Balance)
		if v.Assertions == nil || v.Notes == nil || v.Documents == nil {
			t.Fatal("nil account collections")
		}
		for _, a := range v.Assertions {
			coreCheckValue(t, a)
		}
	case store.RegisterRow:
		checkAmount(v.Amount)
		checkAmount(v.Balance)
	case store.Line:
		checkAmount(v.Amount)
	case store.Trial:
		checkAmount(v.Total)
		if v.Lines == nil {
			t.Fatal("nil trial lines")
		}
		for _, l := range v.Lines {
			coreCheckValue(t, l)
		}
	case store.BalanceSheet:
		if v.Lines == nil {
			t.Fatal("nil sheet lines")
		}
		for _, l := range v.Lines {
			coreCheckValue(t, l)
		}
	case store.IncomeStatement:
		checkAmount(v.Net)
		if v.Lines == nil {
			t.Fatal("nil income lines")
		}
		for _, l := range v.Lines {
			coreCheckValue(t, l)
		}
	case []store.Account:
		if v == nil {
			t.Fatal("nil accounts")
		}
	case []store.Transaction:
		if v == nil {
			t.Fatal("nil transactions")
		}
		for _, x := range v {
			coreCheckValue(t, x)
		}
	case []store.Annotation:
		if v == nil {
			t.Fatal("nil versions")
		}
		for _, x := range v {
			coreCheckValue(t, x)
		}
	case []store.RegisterRow:
		if v == nil {
			t.Fatal("nil register")
		}
		for _, x := range v {
			coreCheckValue(t, x)
		}
	case []store.AssertionRow:
		if v == nil {
			t.Fatal("nil assertions")
		}
		for _, x := range v {
			coreCheckValue(t, x)
		}
	}
}

// R-OFRM-QW5A R-OGZJ-4NVZ R-OEJQ-D4EL
func TestCoreCollectionAndAmountInvariants(t *testing.T) {
	ctx := context.Background()
	f := coreNew(t, ledger.Migrations())
	for _, answer := range []coreAnswer{coreOne(f.s.Accounts(ctx)), coreOne(f.s.ListTransactions(ctx, store.JournalFilter{}, "", 0, 10)), coreOne(f.s.ListRegister(ctx, "Assets", "", "", "", 0, 0, 10)), coreOne(f.s.ListFailing(ctx, "", "", "", 10)), coreOne(f.s.Balances(ctx, "", "")), coreOne(f.s.BalanceSheetAt(ctx, "")), coreOne(f.s.IncomeStatementFor(ctx, "2026-01-01", "2026-12-31"))} {
		if answer.err != nil {
			t.Fatal(answer.err)
		}
		coreCheckValue(t, answer.value)
	}
	for _, answer := range []coreAnswer{coreOne(f.s.GetTransaction(ctx, "bad")), coreOne(f.s.ListVersions(ctx, "bad", 0, 1)), coreOne(f.s.GetAccount(ctx, "Assets:Missing", "")), coreChanged(f.s.RetractAssertion(ctx, store.Retraction{ID: "bad", UserID: "owner"})), coreChanged(f.s.RetractNote(ctx, store.Retraction{ID: "bad", UserID: "owner"})), coreChanged(f.s.RetractDocument(ctx, store.Retraction{ID: "bad", UserID: "owner"}))} {
		coreErr(t, answer.err, store.ErrNotFound)
	}
	p, a, n, d := coreSeed(t, f)
	for _, answer := range []coreAnswer{coreOne(p, nil), coreOne(a, nil), coreOne(f.s.GetAccount(ctx, "Assets:Bank:Checking", "")), coreOne(f.s.GetTransaction(ctx, p.ID)), coreOne(f.s.ListVersions(ctx, p.ID, 0, 10)), coreOne(f.s.ListVersions(ctx, p.ID, 1, 10)), coreOne(f.s.ListTransactions(ctx, store.JournalFilter{}, "", 0, 10)), coreOne(f.s.ListRegister(ctx, "Assets", "", "", "", 0, 0, 10)), coreOne(f.s.ListFailing(ctx, "", "", "", 10)), coreOne(f.s.Balances(ctx, "", "")), coreOne(f.s.BalanceSheetAt(ctx, "")), coreOne(f.s.IncomeStatementFor(ctx, "2026-01-01", "2026-12-31")), coreChanged(f.s.RetractAssertion(ctx, store.Retraction{ID: a.Assertion.ID, UserID: "owner"})), coreChanged(f.s.RetractNote(ctx, store.Retraction{ID: n.ID, UserID: "owner"})), coreChanged(f.s.RetractDocument(ctx, store.Retraction{ID: d.ID, UserID: "owner"})), coreChanged(f.s.Annotate(ctx, store.AnnotationEdit{Transaction: p.ID, Payee: corePtr("changed"), UserID: "owner"})), coreChanged(f.s.Reverse(ctx, store.ReversalInput{Transaction: p.ID, UserID: "owner"}))} {
		if answer.err != nil {
			t.Fatal(answer.err)
		}
		coreCheckValue(t, answer.value)
	}
	for _, answer := range []coreAnswer{coreOne(f.s.GetTransaction(ctx, "bad")), coreOne(f.s.ListVersions(ctx, "bad", 0, 1)), coreOne(f.s.GetAccount(ctx, "Assets:Missing", "")), coreChanged(f.s.RetractAssertion(ctx, store.Retraction{ID: "bad", UserID: "owner"})), coreChanged(f.s.RetractNote(ctx, store.Retraction{ID: "bad", UserID: "owner"})), coreChanged(f.s.RetractDocument(ctx, store.Retraction{ID: "bad", UserID: "owner"}))} {
		coreErr(t, answer.err, store.ErrNotFound)
	}
}

// R-O9O4-U1FT R-OC3X-LKX7
func TestCorePartialRandomAndRetainedCollisions(t *testing.T) {
	ctx := context.Background()
	for _, kind := range []string{"transaction", "reverse", "assertion", "note", "document"} {
		t.Run(kind, func(t *testing.T) {
			f := coreNew(t, ledger.Migrations())
			p, a, n, d := coreSeed(t, f)
			f.s = store.New(f.d, store.Config{Now: func() time.Time { return f.now }, Rand: bytes.NewReader(bytes.Repeat([]byte{0x7f}, 7))})
			var r coreAnswer
			snapshot := coreSnapshot(t, f)
			switch kind {
			case "transaction":
				r = coreChanged(f.s.Post(ctx, store.PostInput{Date: "2026-03-01", UserID: "owner", Postings: []store.PostingInput{{Account: "Assets:Bank:Checking", Amount: corePtr("0")}, {Account: "Income:Sales"}}}))
			case "reverse":
				r = coreChanged(f.s.Reverse(ctx, store.ReversalInput{Transaction: p.ID, UserID: "owner"}))
			case "assertion":
				r = coreOne(f.s.AssertBalance(ctx, store.AssertionInput{Account: "Assets", Date: "2026-03-01", Amount: "0", UserID: "owner"}))
			case "note":
				r = coreOne(f.s.AddNote(ctx, store.NoteInput{Account: n.Account, Date: n.Date, Text: "new", UserID: "owner"}))
			case "document":
				r = coreOne(f.s.AddDocument(ctx, store.DocumentInput{Account: d.Account, Date: d.Date, Path: d.Path, Artifact: d.Artifact, UserID: "owner"}))
			}
			coreErr(t, r.err, store.ErrUnreachable)
			if !reflect.DeepEqual(snapshot, coreSnapshot(t, f)) {
				t.Fatal("partial source changed records")
			}
			if r.changed || !reflect.ValueOf(r.value).IsZero() {
				t.Fatalf("partial source nonzero %+v", r)
			}
			status, e := f.s.Status(ctx)
			if e != nil || status.Transactions != 1 {
				t.Fatal("partial source wrote")
			}
			f.s = store.New(f.d, store.Config{Now: func() time.Time { return f.now }, Rand: f.rnd})
			if _, _, e = f.s.RetractAssertion(ctx, store.Retraction{ID: a.Assertion.ID, UserID: "owner"}); e != nil {
				t.Fatal(e)
			}
			if _, _, e = f.s.RetractNote(ctx, store.Retraction{ID: n.ID, UserID: "owner"}); e != nil {
				t.Fatal(e)
			}
			if _, _, e = f.s.RetractDocument(ctx, store.Retraction{ID: d.ID, UserID: "owner"}); e != nil {
				t.Fatal(e)
			}
			f.rnd.constant = true
			switch kind {
			case "assertion":
				f.rnd.n = 0x5b
				r = coreOne(f.s.AssertBalance(ctx, store.AssertionInput{Account: "Assets", Date: a.Assertion.Date, Amount: "0", UserID: "owner"}))
			case "note":
				f.rnd.n = 0x5c
				r = coreOne(f.s.AddNote(ctx, store.NoteInput{Account: n.Account, Date: n.Date, Text: "new", UserID: "owner"}))
			case "document":
				f.rnd.n = 0x5d
				r = coreOne(f.s.AddDocument(ctx, store.DocumentInput{Account: d.Account, Date: d.Date, Path: d.Path, Artifact: d.Artifact, UserID: "owner"}))
			case "transaction", "reverse":
				f.rnd.n = 0x6b
				rev, _, e := f.s.Reverse(ctx, store.ReversalInput{Transaction: p.ID, UserID: "owner"})
				if e != nil {
					t.Fatal(e)
				}
				if kind == "reverse" {
					r = coreChanged(f.s.Reverse(ctx, store.ReversalInput{Transaction: rev.ID, UserID: "owner"}))
				} else {
					r = coreChanged(f.s.Post(ctx, store.PostInput{Date: "2026-03-01", UserID: "owner", Postings: []store.PostingInput{{Account: "Assets:Bank:Checking", Amount: corePtr("0")}, {Account: "Income:Sales"}}}))
				}
			}
			coreErr(t, r.err, store.ErrUnreachable)
			if r.changed || !reflect.ValueOf(r.value).IsZero() {
				t.Fatal("retained collision result")
			}
		})
	}
}

func coreSnapshot(t *testing.T, f *coreFixture) []coreAnswer {
	t.Helper()
	c := context.Background()
	out := []coreAnswer{coreOne(f.s.Status(c)), coreOne(f.s.Accounts(c)), coreOne(f.s.GetAccount(c, "Assets:Bank:Checking", "")), coreOne(f.s.ListFailing(c, "", "", "", 100))}
	txns, e := f.s.ListTransactions(c, store.JournalFilter{}, "", 0, 100)
	if e != nil {
		t.Fatal(e)
	}
	out = append(out, coreOne(txns, nil))
	for _, tx := range txns {
		out = append(out, coreOne(f.s.ListVersions(c, tx.ID, 0, 100)))
	}
	return out
}

func TestCoreExactRecordShapes(t *testing.T) {
	// R-MDDR-EVWX
	{
		value := store.Config{}
		var shape struct {
			Now  func() time.Time
			Rand io.Reader
		} = value
		if !reflect.DeepEqual(value, store.Config(shape)) {
			t.Fatal("Config shape conversion")
		}
	}
	// R-MI9C-XYVP
	{
		value := store.Amount{}
		var shape struct {
			Units     int64
			Commodity string
			Precision int
		} = value
		if !reflect.DeepEqual(value, store.Amount(shape)) {
			t.Fatal("Amount shape conversion")
		}
	}
	// R-MN4Y-H1UH
	{
		value := store.Book{}
		var shape struct {
			Currency                  string
			Precision                 int
			LockDate, FiscalYearStart string
			Updated                   time.Time
			UpdatedUserID             string
		} = value
		if !reflect.DeepEqual(value, store.Book(shape)) {
			t.Fatal("Book shape conversion")
		}
	}
	// R-MOCU-UTL6
	{
		value := store.Status{}
		var shape struct {
			Book                       store.Book
			OpenAccounts, Transactions int
			LatestDate                 string
			FailingAssertions          int
		} = value
		if !reflect.DeepEqual(value, store.Status(shape)) {
			t.Fatal("Status shape conversion")
		}
	}
	// R-MPKR-8LBV
	{
		value := store.BookEdit{}
		var shape struct {
			LockDate, FiscalYearStart *string
			UserID                    string
		} = value
		if !reflect.DeepEqual(value, store.BookEdit(shape)) {
			t.Fatal("BookEdit shape conversion")
		}
	}
	// R-MQSN-MD2K
	{
		value := store.Account{}
		var shape struct {
			Name, Open, Close string
			Opened            time.Time
			OpenedUserID      string
			Closed            time.Time
			ClosedUserID      string
		} = value
		if !reflect.DeepEqual(value, store.Account(shape)) {
			t.Fatal("Account shape conversion")
		}
	}
	// R-MS0K-04T9
	{
		value := store.AccountInput{}
		var shape struct{ Name, Date, UserID string } = value
		if !reflect.DeepEqual(value, store.AccountInput(shape)) {
			t.Fatal("AccountInput shape conversion")
		}
	}
	// R-MT8G-DWJY
	{
		value := store.AccountRead{}
		var shape struct {
			Account    store.Account
			Date       string
			Balance    store.Amount
			Postings   int
			Assertions []store.AssertionRow
			Notes      []store.Note
			Documents  []store.Document
		} = value
		if !reflect.DeepEqual(value, store.AccountRead(shape)) {
			t.Fatal("AccountRead shape conversion")
		}
	}
	// R-MUGC-ROAN
	{
		value := store.Posting{}
		var shape struct {
			Position int64
			Account  string
			Amount   store.Amount
		} = value
		if !reflect.DeepEqual(value, store.Posting(shape)) {
			t.Fatal("Posting shape conversion")
		}
	}
	// R-MVO9-5G1C
	{
		value := store.PostingInput{}
		var shape struct {
			Account string
			Amount  *string
		} = value
		if !reflect.DeepEqual(value, store.PostingInput(shape)) {
			t.Fatal("PostingInput shape conversion")
		}
	}
	// R-MWW5-J7S1
	{
		value := store.Annotation{}
		var shape struct {
			Version                int64
			Flag, Payee, Narration string
			Tags, Links            []string
			Metadata               map[string]string
			Written                time.Time
			WrittenUserID          string
		} = value
		if !reflect.DeepEqual(value, store.Annotation(shape)) {
			t.Fatal("Annotation shape conversion")
		}
	}
	// R-MY41-WZIQ
	{
		value := store.Transaction{}
		var shape struct {
			ID                              string
			Seq                             int64
			Date, Key, Reverses, ReversedBy string
			Posted                          time.Time
			PostedUserID                    string
			Postings                        []store.Posting
			Annotation                      store.Annotation
		} = value
		if !reflect.DeepEqual(value, store.Transaction(shape)) {
			t.Fatal("Transaction shape conversion")
		}
	}
	// R-MZBY-AR9F
	{
		value := store.PostInput{}
		var shape struct {
			Date                   string
			Postings               []store.PostingInput
			Flag, Payee, Narration string
			Tags, Links            []string
			Metadata               map[string]string
			Key, UserID            string
		} = value
		if !reflect.DeepEqual(value, store.PostInput(shape)) {
			t.Fatal("PostInput shape conversion")
		}
	}
	// R-N0JU-OJ04
	{
		value := store.AnnotationEdit{}
		var shape struct {
			Transaction            string
			Flag, Payee, Narration *string
			Tags, Links            *[]string
			Metadata               *map[string]string
			UserID                 string
		} = value
		if !reflect.DeepEqual(value, store.AnnotationEdit(shape)) {
			t.Fatal("AnnotationEdit shape conversion")
		}
	}
	// R-N1RR-2AQT
	{
		value := store.ReversalInput{}
		var shape struct {
			Transaction, Date string
			Payee, Narration  *string
			Tags, Links       *[]string
			Metadata          *map[string]string
			UserID            string
		} = value
		if !reflect.DeepEqual(value, store.ReversalInput(shape)) {
			t.Fatal("ReversalInput shape conversion")
		}
	}
	// R-N2ZN-G2HI
	{
		value := store.JournalFilter{}
		var shape struct{ From, To, Account, Tag, Link, Payee, Flag string } = value
		if !reflect.DeepEqual(value, store.JournalFilter(shape)) {
			t.Fatal("JournalFilter shape conversion")
		}
	}
	// R-N5FG-7LYW
	{
		value := store.RegisterRow{}
		var shape struct {
			Transaction, Date string
			Seq, Position     int64
			Account           string
			Amount, Balance   store.Amount
			Payee, Narration  string
		} = value
		if !reflect.DeepEqual(value, store.RegisterRow(shape)) {
			t.Fatal("RegisterRow shape conversion")
		}
	}
	// R-N6NC-LDPL
	{
		value := store.Assertion{}
		var shape struct {
			ID, Account, Date string
			Expected          store.Amount
			Added             time.Time
			AddedUserID       string
			Retracted         time.Time
			RetractedUserID   string
		} = value
		if !reflect.DeepEqual(value, store.Assertion(shape)) {
			t.Fatal("Assertion shape conversion")
		}
	}
	// R-N7V8-Z5GA
	{
		value := store.AssertionRow{}
		var shape struct {
			Assertion          store.Assertion
			Status             string
			Actual, Difference store.Amount
		} = value
		if !reflect.DeepEqual(value, store.AssertionRow(shape)) {
			t.Fatal("AssertionRow shape conversion")
		}
	}
	// R-N935-CX6Z
	{
		value := store.AssertionInput{}
		var shape struct{ Account, Date, Amount, UserID string } = value
		if !reflect.DeepEqual(value, store.AssertionInput(shape)) {
			t.Fatal("AssertionInput shape conversion")
		}
	}
	// R-NAB1-QOXO
	{
		value := store.Retraction{}
		var shape struct{ ID, UserID string } = value
		if !reflect.DeepEqual(value, store.Retraction(shape)) {
			t.Fatal("Retraction shape conversion")
		}
	}
	// R-NBIY-4GOD
	{
		value := store.Note{}
		var shape struct {
			ID, Account, Date, Text string
			Added                   time.Time
			AddedUserID             string
			Retracted               time.Time
			RetractedUserID         string
		} = value
		if !reflect.DeepEqual(value, store.Note(shape)) {
			t.Fatal("Note shape conversion")
		}
	}
	// R-NCQU-I8F2
	{
		value := store.NoteInput{}
		var shape struct{ Account, Date, Text, UserID string } = value
		if !reflect.DeepEqual(value, store.NoteInput(shape)) {
			t.Fatal("NoteInput shape conversion")
		}
	}
	// R-NDYQ-W05R
	{
		value := store.Document{}
		var shape struct {
			ID, Account, Transaction, Date, Path, Artifact string
			Added                                          time.Time
			AddedUserID                                    string
			Retracted                                      time.Time
			RetractedUserID                                string
		} = value
		if !reflect.DeepEqual(value, store.Document(shape)) {
			t.Fatal("Document shape conversion")
		}
	}
	// R-NF6N-9RWG
	{
		value := store.DocumentInput{}
		var shape struct{ Account, Transaction, Date, Path, Artifact, UserID string } = value
		if !reflect.DeepEqual(value, store.DocumentInput(shape)) {
			t.Fatal("DocumentInput shape conversion")
		}
	}
	// R-NGEJ-NJN5
	{
		value := store.Line{}
		var shape struct {
			Account string
			Amount  store.Amount
		} = value
		if !reflect.DeepEqual(value, store.Line(shape)) {
			t.Fatal("Line shape conversion")
		}
	}
	// R-NHMG-1BDU
	{
		value := store.Trial{}
		var shape struct {
			Date  string
			Lines []store.Line
			Total store.Amount
		} = value
		if !reflect.DeepEqual(value, store.Trial(shape)) {
			t.Fatal("Trial shape conversion")
		}
	}
	// R-NIUC-F34J
	{
		value := store.BalanceSheet{}
		var shape struct {
			Date  string
			Lines []store.Line
		} = value
		if !reflect.DeepEqual(value, store.BalanceSheet(shape)) {
			t.Fatal("BalanceSheet shape conversion")
		}
	}
	// R-NK28-SUV8
	{
		value := store.IncomeStatement{}
		var shape struct {
			From, To string
			Lines    []store.Line
			Net      store.Amount
		} = value
		if !reflect.DeepEqual(value, store.IncomeStatement(shape)) {
			t.Fatal("IncomeStatement shape conversion")
		}
	}
}

type coreSurface interface {
	Status(context.Context) (store.Status, error)
	SetBook(context.Context, store.BookEdit) (store.Status, bool, error)
	OpenAccount(context.Context, store.AccountInput) (store.Account, bool, error)
	CloseAccount(context.Context, store.AccountInput) (store.Account, bool, error)
	Accounts(context.Context) ([]store.Account, error)
	GetAccount(context.Context, string, string) (store.AccountRead, error)
	Post(context.Context, store.PostInput) (store.Transaction, bool, error)
	GetTransaction(context.Context, string) (store.Transaction, error)
	Annotate(context.Context, store.AnnotationEdit) (store.Transaction, bool, error)
	Reverse(context.Context, store.ReversalInput) (store.Transaction, bool, error)
	ListVersions(context.Context, string, int64, int) ([]store.Annotation, error)
	ListTransactions(context.Context, store.JournalFilter, string, int64, int) ([]store.Transaction, error)
	ListRegister(context.Context, string, string, string, string, int64, int64, int) ([]store.RegisterRow, error)
	AssertBalance(context.Context, store.AssertionInput) (store.AssertionRow, error)
	RetractAssertion(context.Context, store.Retraction) (store.AssertionRow, bool, error)
	ListFailing(context.Context, string, string, string, int) ([]store.AssertionRow, error)
	AddNote(context.Context, store.NoteInput) (store.Note, error)
	RetractNote(context.Context, store.Retraction) (store.Note, bool, error)
	CheckDocument(context.Context, store.DocumentInput) error
	AddDocument(context.Context, store.DocumentInput) (store.Document, error)
	RetractDocument(context.Context, store.Retraction) (store.Document, bool, error)
	Balances(context.Context, string, string) (store.Trial, error)
	BalanceSheetAt(context.Context, string) (store.BalanceSheet, error)
	IncomeStatementFor(context.Context, string, string) (store.IncomeStatement, error)
}

// R-NMI1-KECM R-NNPX-Y63B R-NOXU-BXU0 R-NRDN-3HBE
func TestCoreExactMethodSurface(t *testing.T) {
	f := coreNew(t, ledger.Migrations())
	var api coreSurface = f.s
	if s, e := api.Status(context.Background()); e != nil || s.Book.Currency != "USD" {
		t.Fatalf("surface status %+v %v", s, e)
	}
}
