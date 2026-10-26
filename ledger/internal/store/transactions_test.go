package store_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/ledger"
	"github.com/ikigenba/ikigenba/ledger/internal/store"
)

func transactionFixture(t *testing.T) (*store.Store, *db.DB, string, func() time.Time) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "book.db")
	now := func() time.Time { return time.Date(2026, 3, 10, 9, 8, 7, 123, time.UTC) }
	d, err := db.Open(context.Background(), db.Config{Path: path, Migrations: ledger.Migrations(), Now: now, Service: "test", Stderr: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	rnd := make([]byte, 8192)
	for i := range rnd {
		rnd[i] = byte(i / 8)
	}
	s := store.New(d, store.Config{Now: now, Rand: bytes.NewReader(rnd)})
	for _, name := range []string{"Assets:Bank:Checking", "Expenses:Office", "Assets:Banking"} {
		if _, _, err = s.OpenAccount(context.Background(), store.AccountInput{Name: name, Date: "2026-01-01", UserID: "owner"}); err != nil {
			t.Fatal(err)
		}
	}
	return s, d, path, now
}
func textPointer(s string) *string { return &s }
func postFixture(t *testing.T, s *store.Store, date, key string) store.Transaction {
	t.Helper()
	v, changed, err := s.Post(context.Background(), store.PostInput{Date: date, Key: key, Postings: []store.PostingInput{{Account: "Assets:Bank:Checking", Amount: textPointer("-50.00")}, {Account: "Expenses:Office"}}, Payee: "  Vendor  ", Tags: []string{"b", "a", "b"}, Links: []string{"invoice"}, Metadata: map[string]string{"code": " value "}, UserID: "owner"})
	if err != nil || !changed {
		t.Fatalf("post: %#v %v %v", v, changed, err)
	}
	return v
}

// R-OY24-HG9P R-OZA0-V80E R-P1PT-MRHS R-P2XQ-0J8H R-P5DI-S2PV R-P6LF-5UGK R-P917-XDXY
func TestTransactionLifecycle(t *testing.T) {
	s, _, path, now := transactionFixture(t)
	ctx := context.Background()
	first := postFixture(t, s, "2026-02-01", "invoice")
	if first.Seq <= 0 || !store.ValidTransactionID(first.ID) || first.Date != "2026-02-01" || first.Key != "invoice" || first.Reverses != "" || first.ReversedBy != "" || first.Posted != now().Truncate(time.Second) || first.PostedUserID != "owner" || first.Annotation.Version != 1 || first.Annotation.Flag != store.FlagPending || first.Annotation.Payee != "Vendor" || !reflect.DeepEqual(first.Annotation.Tags, []string{"a", "b"}) || first.Annotation.Metadata["code"] != " value " || first.Postings[0].Position != 1 || first.Postings[1].Position != 2 || first.Postings[1].Amount.Units != 5000 {
		t.Fatalf("unexpected transaction: %#v", first)
	}
	retry, made, err := s.Post(ctx, store.PostInput{Key: "invoice", Date: "invalid", UserID: "other"})
	if err != nil || made || !reflect.DeepEqual(retry, first) {
		t.Fatalf("retry: %#v %v %v", retry, made, err)
	}
	tags := []string{"new"}
	changed, yes, err := s.Annotate(ctx, store.AnnotationEdit{Transaction: first.ID, Tags: &tags, UserID: "editor"})
	if err != nil || !yes || changed.Annotation.Version != 2 || changed.Annotation.Payee != first.Annotation.Payee || !reflect.DeepEqual(changed.Annotation.Tags, tags) || changed.Annotation.WrittenUserID != "editor" {
		t.Fatalf("annotate: %#v %v %v", changed, yes, err)
	}
	unchanged, yes, err := s.Annotate(ctx, store.AnnotationEdit{Transaction: first.ID, Tags: &tags, UserID: "other"})
	if err != nil || yes || !reflect.DeepEqual(unchanged, changed) {
		t.Fatalf("no-op: %v %v", yes, err)
	}
	tags = []string{}
	changed, yes, err = s.Annotate(ctx, store.AnnotationEdit{Transaction: first.ID, Tags: &tags, UserID: "editor2"})
	if err != nil || !yes || changed.Annotation.Tags == nil || len(changed.Annotation.Tags) != 0 {
		t.Fatal(changed, yes, err)
	}
	versions, err := s.ListVersions(ctx, first.ID, 0, 2)
	if err != nil || len(versions) != 2 || versions[0].Version != 3 || versions[1].Version != 2 {
		t.Fatal(versions, err)
	}
	tail, err := s.ListVersions(ctx, first.ID, 2, 10)
	if err != nil || len(tail) != 1 || !reflect.DeepEqual(tail[0], first.Annotation) {
		t.Fatal(tail, err)
	}
	reversal, made, err := s.Reverse(ctx, store.ReversalInput{Transaction: first.ID, UserID: "reverser"})
	if err != nil || !made || reversal.Seq <= first.Seq || reversal.Date != "2026-03-10" || reversal.Key != "" || reversal.Reverses != first.ID || reversal.Annotation.Flag != store.FlagCleared || reversal.Annotation.Payee != "Vendor" || reversal.PostedUserID != "reverser" || reversal.Postings[0].Amount.Units != 5000 || reversal.Postings[1].Amount.Units != -5000 {
		t.Fatal(reversal, made, err)
	}
	got, err := s.GetTransaction(ctx, first.ID)
	if err != nil || got.ReversedBy != reversal.ID || !reflect.DeepEqual(got.Annotation, changed.Annotation) {
		t.Fatal(got, err)
	}
	again, made, err := s.Reverse(ctx, store.ReversalInput{Transaction: first.ID, Date: "invalid", Payee: textPointer(strings.Repeat("x", 201)), UserID: "retry"})
	if err != nil || made || !reflect.DeepEqual(again, reversal) {
		t.Fatal(again, made, err)
	}
	if _, yes, err = s.Reverse(ctx, store.ReversalInput{Transaction: reversal.ID, UserID: "owner"}); err != nil || !yes {
		t.Fatal(yes, err)
	}
	reopened, err := db.Open(ctx, db.Config{Path: path, Migrations: ledger.Migrations(), Now: now, Service: "test", Stderr: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = reopened.Close() }()
	persisted, err := store.New(reopened, store.Config{Now: now, Rand: bytes.NewReader(nil)}).GetTransaction(ctx, first.ID)
	if err != nil || !reflect.DeepEqual(got, persisted) {
		t.Fatal(persisted, err)
	}
}

// R-P0HX-8ZR3 R-P45M-EAZ6 R-P7TB-JM79 R-PA94-B5ON R-PBH0-OXFC R-PDWT-GGWQ R-PHKI-LS4T
func TestTransactionRefusals(t *testing.T) {
	s, _, _, _ := transactionFixture(t)
	ctx := context.Background()
	first := postFixture(t, s, "2026-02-01", "")
	base := store.PostInput{Date: "2026-02-01", UserID: "owner", Postings: []store.PostingInput{{Account: "Assets:Bank:Checking", Amount: textPointer("-1")}, {Account: "Expenses:Office"}}}
	cases := []struct {
		name string
		edit func(*store.PostInput)
		want error
	}{{"user", func(v *store.PostInput) { v.UserID = ""; v.Date = "bad" }, store.ErrUnreachable}, {"date", func(v *store.PostInput) { v.Date = "bad" }, store.ErrInvalid}, {"precision", func(v *store.PostInput) { v.Postings[0].Amount = textPointer("1.234") }, store.ErrInvalid}, {"sum", func(v *store.PostInput) { v.Postings[1].Amount = textPointer("2") }, store.ErrUnbalanced}, {"missing", func(v *store.PostInput) { v.Postings[0].Amount = nil }, store.ErrUnbalanced}, {"undeclared", func(v *store.PostInput) { v.Postings[0].Account = "Assets:Absent" }, store.ErrNotFound}, {"before-open", func(v *store.PostInput) { v.Date = "2025-12-31" }, store.ErrInvalid}, {"remainder", func(v *store.PostInput) {
		v.Postings = []store.PostingInput{{Account: "Assets:Bank:Checking", Amount: textPointer("9999999999999.99")}, {Account: "Expenses:Office", Amount: textPointer("9999999999999.99")}, {Account: "Expenses:Office"}}
	}, store.ErrInvalid}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := base
			v.Postings = append([]store.PostingInput(nil), base.Postings...)
			tc.edit(&v)
			got, yes, err := s.Post(ctx, v)
			if !errors.Is(err, tc.want) || yes || !reflect.DeepEqual(got, store.Transaction{}) {
				t.Fatalf("%#v %v %v", got, yes, err)
			}
		})
	}
	for _, e := range []store.AnnotationEdit{{Transaction: first.ID, Flag: textPointer(""), UserID: "owner"}, {Transaction: "missing", Payee: textPointer(strings.Repeat("x", 201)), UserID: "owner"}} {
		if _, _, err := s.Annotate(ctx, e); !errors.Is(err, store.ErrInvalid) {
			t.Fatal(err)
		}
	}
	if _, _, err := s.Annotate(ctx, store.AnnotationEdit{Transaction: "missing", UserID: "owner"}); !errors.Is(err, store.ErrNotFound) {
		t.Fatal(err)
	}
	for _, in := range []store.ReversalInput{{Transaction: first.ID, Date: "2026-01-31", UserID: "owner"}, {Transaction: "missing", Date: "bad", UserID: "owner"}} {
		if _, _, err := s.Reverse(ctx, in); !errors.Is(err, store.ErrInvalid) {
			t.Fatal(err)
		}
	}
	if _, _, err := s.Reverse(ctx, store.ReversalInput{Transaction: "missing", UserID: "owner"}); !errors.Is(err, store.ErrNotFound) {
		t.Fatal(err)
	}
	lock := "2026-02-01"
	if _, _, err := s.SetBook(ctx, store.BookEdit{LockDate: &lock, UserID: "owner"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Post(ctx, base); !errors.Is(err, store.ErrLocked) {
		t.Fatal(err)
	}
	if got, err := s.GetTransaction(ctx, "bad"); !errors.Is(err, store.ErrNotFound) || !reflect.DeepEqual(got, store.Transaction{}) {
		t.Fatal(got, err)
	}
	if got, err := s.ListVersions(ctx, "bad", 0, 1); !errors.Is(err, store.ErrNotFound) || got != nil {
		t.Fatal(got, err)
	}
	for _, run := range []func() error{func() error { _, e := s.ListVersions(ctx, "bad", 0, 0); return e }, func() error { _, e := s.ListTransactions(ctx, store.JournalFilter{}, "", 0, 0); return e }, func() error { _, e := s.ListRegister(ctx, "bad", "", "", "", 0, 0, 0); return e }, func() error { _, e := s.ListFailing(ctx, "", "", "", 0); return e }} {
		if err := run(); !errors.Is(err, store.ErrUnreachable) {
			t.Fatal(err)
		}
	}
	for _, f := range []store.JournalFilter{{From: "bad"}, {From: "2026-02-01", To: "2026-01-01"}, {Account: "bad"}, {Tag: "bad token"}, {Link: "bad token"}, {Flag: "bad"}} {
		if rows, err := s.ListTransactions(ctx, f, "", 0, 1); rows != nil || !errors.Is(err, store.ErrInvalid) {
			t.Fatal(rows, err)
		}
	}
	if rows, err := s.ListRegister(ctx, "Assets", "bad", "", "", 0, 0, 1); rows != nil || !errors.Is(err, store.ErrInvalid) {
		t.Fatal(rows, err)
	}
}

// R-PCOX-2P61 R-PGCM-80E4
func TestTransactionWindows(t *testing.T) {
	s, _, _, _ := transactionFixture(t)
	ctx := context.Background()
	a := postFixture(t, s, "2026-01-15", "")
	b := postFixture(t, s, "2026-02-01", "")
	c := postFixture(t, s, "2026-02-01", "")
	got, err := s.ListTransactions(ctx, store.JournalFilter{Account: "Assets:Bank", Tag: "a", Link: "invoice", Payee: "Vendor", Flag: store.FlagPending}, "", 0, 2)
	if err != nil || len(got) != 2 || got[0].ID != c.ID || got[1].ID != b.ID {
		t.Fatal(got, err)
	}
	tail, err := s.ListTransactions(ctx, store.JournalFilter{}, b.Date, b.Seq, 2)
	if err != nil || len(tail) != 1 || tail[0].ID != a.ID {
		t.Fatal(tail, err)
	}
	for _, f := range []store.JournalFilter{{Account: "Assets:Banking"}, {Tag: "absent"}, {Link: "absent"}, {Payee: " Vendor "}, {Flag: store.FlagCleared}, {From: "2026-02-02"}, {To: "2026-01-14"}} {
		got, err = s.ListTransactions(ctx, f, "", 0, 9)
		if err != nil || got == nil || len(got) != 0 {
			t.Fatal(got, err)
		}
	}
	tags := []string{"replacement"}
	if _, _, err = s.Annotate(ctx, store.AnnotationEdit{Transaction: c.ID, Tags: &tags, UserID: "editor"}); err != nil {
		t.Fatal(err)
	}
	got, err = s.ListTransactions(ctx, store.JournalFilter{From: "2026-02-01", To: "2026-02-01", Tag: "a"}, "", 0, 9)
	if err != nil || len(got) != 1 || got[0].ID != b.ID {
		t.Fatal(got, err)
	}
	rows, err := s.ListRegister(ctx, "Assets:Bank", "2026-02-01", "2026-02-01", "", 0, 0, 1)
	if err != nil || len(rows) != 1 || rows[0].Transaction != b.ID || rows[0].Amount.Units != -5000 || rows[0].Balance.Units != -10000 || rows[0].Payee != "Vendor" {
		t.Fatal(rows, err)
	}
	rows, err = s.ListRegister(ctx, "Assets:Bank", "", "", rows[0].Date, rows[0].Seq, rows[0].Position, 10)
	if err != nil || len(rows) != 1 || rows[0].Transaction != c.ID || rows[0].Balance.Units != -15000 {
		t.Fatal(rows, err)
	}
	rows, err = s.ListRegister(ctx, "Assets:Absent", "", "", "", 0, 0, 10)
	if err != nil || rows == nil || len(rows) != 0 {
		t.Fatal(rows, err)
	}
}

type sqlWindowCase struct {
	name, statement string
	args            []any
	keys            [][]any
	suffix          string
}

func queryWindow(t *testing.T, d *db.DB, statement string, args []any) [][]any {
	t.Helper()
	out := make([][]any, 0)
	err := d.Read(context.Background(), func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(context.Background(), statement, args...)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		cols, err := rows.Columns()
		if err != nil {
			return err
		}
		for rows.Next() {
			values := make([]any, len(cols))
			targets := make([]any, len(cols))
			for i := range values {
				targets[i] = &values[i]
			}
			if err = rows.Scan(targets...); err != nil {
				return err
			}
			out = append(out, values)
		}
		return rows.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// R-NSLJ-H923 R-Q2AT-3VQM R-Q3IP-HNHB
func TestExportedWindowStatements(t *testing.T) {
	s, d, _, _ := transactionFixture(t)
	ctx := context.Background()
	a := postFixture(t, s, "2026-02-01", "")
	_ = postFixture(t, s, "2026-02-02", "")
	payee := "Changed"
	if _, _, err := s.Annotate(ctx, store.AnnotationEdit{Transaction: a.ID, Payee: &payee, UserID: "owner"}); err != nil {
		t.Fatal(err)
	}
	for _, in := range []store.AssertionInput{{Account: "Assets", Date: "2026-02-01", Amount: "0", UserID: "owner"}, {Account: "Assets:Bank", Date: "2026-02-02", Amount: "0", UserID: "owner"}, {Account: "Expenses", Date: "2026-02-02", Amount: "100", UserID: "owner"}, {Account: "Assets", Date: "2026-04-01", Amount: "0", UserID: "owner"}} {
		if _, err := s.AssertBalance(ctx, in); err != nil {
			t.Fatal(err)
		}
	}
	f := store.JournalFilter{From: "2026-01-01", To: "2026-03-01", Account: "Assets", Tag: "a", Link: "invoice", Flag: store.FlagPending}
	journalKeys := func(date string, seq int64) [][]any {
		ts, err := s.ListTransactions(ctx, f, date, seq, 9)
		if err != nil {
			t.Fatal(err)
		}
		keys := make([][]any, 0, len(ts))
		for _, v := range ts {
			keys = append(keys, []any{v.Seq})
		}
		return keys
	}
	registerKeys := func(date string, seq, pos int64) [][]any {
		rs, err := s.ListRegister(ctx, "Assets", "2026-01-01", "2026-03-01", date, seq, pos, 9)
		if err != nil {
			t.Fatal(err)
		}
		keys := make([][]any, 0, len(rs))
		for _, v := range rs {
			keys = append(keys, []any{v.Seq, v.Position})
		}
		return keys
	}
	versionKeys := func(version int64) [][]any {
		vs, err := s.ListVersions(ctx, a.ID, version, 9)
		if err != nil {
			t.Fatal(err)
		}
		keys := make([][]any, 0, len(vs))
		for _, v := range vs {
			keys = append(keys, []any{v.Version})
		}
		return keys
	}
	failingKeys := func(date, account, id string) [][]any {
		rs, err := s.ListFailing(ctx, date, account, id, 9)
		if err != nil {
			t.Fatal(err)
		}
		keys := make([][]any, 0, len(rs))
		for _, v := range rs {
			keys = append(keys, []any{v.Assertion.ID})
		}
		return keys
	}
	failures, err := s.ListFailing(ctx, "", "", "", 9)
	if err != nil || len(failures) != 2 {
		t.Fatal(failures, err)
	}
	last := failures[0].Assertion
	args := []any{f.From, f.To, f.Account, f.Tag, f.Link, f.Payee, f.Flag}
	firstArgs := append(slicesCopy(args), 9)
	afterArgs := append(slicesCopy(args), "2026-02-02", int64(2), 9)
	cases := []sqlWindowCase{{"transactions-first", store.TransactionsFirst, firstArgs, journalKeys("", 0), " INDEX transactions_journal (date>? AND date<?)"}, {"transactions-after", store.TransactionsAfter, afterArgs, journalKeys("2026-02-02", 2), " INDEX transactions_journal (date>? AND (date,seq)<(?,?))"}, {"register-first", store.RegisterFirst, []any{"Assets", "2026-01-01", "2026-03-01", 9}, registerKeys("", 0, 0), " INDEX postings_register (date>? AND date<?)"}, {"register-after", store.RegisterAfter, []any{"Assets", "2026-01-01", "2026-03-01", a.Date, a.Seq, int64(1), 9}, registerKeys(a.Date, a.Seq, 1), " INDEX postings_register ((date,txn,position)>(?,?,?) AND date<?)"}, {"versions-first", store.VersionsFirst, []any{a.Seq, 9}, versionKeys(0), " PRIMARY KEY (txn=?)"}, {"versions-after", store.VersionsAfter, []any{a.Seq, int64(2), 9}, versionKeys(2), " PRIMARY KEY (txn=? AND version<?)"}, {"failing-first", store.FailingFirst, []any{"2026-03-10", 9}, failingKeys("", "", ""), " INDEX assertions_check (retracted=? AND date<?)"}, {"failing-after", store.FailingAfter, []any{"2026-03-10", last.Date, last.Account, last.ID, 9}, failingKeys(last.Date, last.Account, last.ID), " INDEX assertions_check (retracted=? AND (date,account,id)>(?,?,?) AND date<?)"}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := queryWindow(t, d, tc.statement, tc.args)
			if !reflect.DeepEqual(got, tc.keys) {
				t.Fatalf("got %#v want %#v", got, tc.keys)
			}
			for i, row := range queryWindow(t, d, "EXPLAIN QUERY PLAN "+tc.statement, tc.args) {
				detail := fmt.Sprint(row[3])
				if strings.HasPrefix(detail, "SCAN ") || strings.Contains(detail, "USE TEMP B-TREE") {
					t.Fatal(detail)
				}
				if i == 0 && (!strings.HasPrefix(detail, "SEARCH ") || !strings.HasSuffix(detail, tc.suffix)) {
					t.Fatalf("unexpected first plan row: %s", detail)
				}
			}
		})
	}
}
func slicesCopy(v []any) []any { return append([]any(nil), v...) }

// R-OY24-HG9P R-P0HX-8ZR3 R-P2XQ-0J8H R-P45M-EAZ6 R-P5DI-S2PV R-P7TB-JM79
func TestTransactionBoundaries(t *testing.T) {
	s, _, _, _ := transactionFixture(t)
	ctx := context.Background()
	zero := store.PostInput{Date: "2026-01-01", Postings: []store.PostingInput{{Account: "Expenses:Office", Amount: textPointer("0")}, {Account: "Expenses:Office"}}, UserID: "owner"}
	a, yes, err := s.Post(ctx, zero)
	if err != nil || !yes || a.Postings[1].Amount.Units != 0 {
		t.Fatal(a, yes, err)
	}
	b, yes, err := s.Post(ctx, zero)
	if err != nil || !yes || a.ID == b.ID || b.Seq <= a.Seq {
		t.Fatal(b, yes, err)
	}
	invalid := []store.PostInput{{Date: "2026-01-01", Key: "bad key", UserID: "owner"}, {Date: "2026-01-01", Flag: "bad", UserID: "owner"}, {Date: "2026-01-01", Payee: strings.Repeat("a", 201), UserID: "owner"}, {Date: "2026-01-01", Postings: make([]store.PostingInput, 101), UserID: "owner"}, {Date: "2026-01-01", Postings: []store.PostingInput{{Account: "bad", Amount: textPointer("0")}}, UserID: "owner"}}
	for _, in := range invalid {
		if out, made, e := s.Post(ctx, in); !errors.Is(e, store.ErrInvalid) || made || !reflect.DeepEqual(out, store.Transaction{}) {
			t.Fatal(out, made, e)
		}
	}
	for _, ps := range [][]store.PostingInput{nil, {{Account: "Expenses:Office", Amount: textPointer("0")}}} {
		in := zero
		in.Postings = ps
		if _, _, e := s.Post(ctx, in); !errors.Is(e, store.ErrUnbalanced) {
			t.Fatal(e)
		}
	}
	if _, _, err = s.CloseAccount(ctx, store.AccountInput{Name: "Expenses:Office", Date: "2026-02-01", UserID: "owner"}); err != nil {
		t.Fatal(err)
	}
	in := zero
	in.Date = "2026-02-02"
	if _, _, err = s.Post(ctx, in); !errors.Is(err, store.ErrInvalid) {
		t.Fatal(err)
	}
	if _, _, err = s.Reverse(ctx, store.ReversalInput{Transaction: a.ID, Date: "2026-02-02", UserID: "owner"}); !errors.Is(err, store.ErrInvalid) {
		t.Fatal(err)
	}
	lock := "2026-01-01"
	if _, _, err = s.SetBook(ctx, store.BookEdit{LockDate: &lock, UserID: "owner"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.Reverse(ctx, store.ReversalInput{Transaction: a.ID, Date: lock, UserID: "owner"}); !errors.Is(err, store.ErrLocked) {
		t.Fatal(err)
	}
	cleared := store.FlagCleared
	empty := ""
	tags := []string{}
	md := map[string]string{}
	edited, yes, err := s.Annotate(ctx, store.AnnotationEdit{Transaction: a.ID, Flag: &cleared, Payee: &empty, Narration: &empty, Tags: &tags, Links: &tags, Metadata: &md, UserID: "editor"})
	if err != nil || !yes || edited.Annotation.Flag != cleared {
		t.Fatal(edited, yes, err)
	}
	noChange, yes, err := s.Annotate(ctx, store.AnnotationEdit{Transaction: a.ID, UserID: "another"})
	if err != nil || yes || !reflect.DeepEqual(noChange, edited) {
		t.Fatal(noChange, yes, err)
	}
	reversal, yes, err := s.Reverse(ctx, store.ReversalInput{Transaction: a.ID, Date: "2026-02-01", Payee: textPointer(" alternate "), Narration: textPointer(" reason "), Tags: &[]string{"z", "a"}, Links: &[]string{}, Metadata: &map[string]string{"reason": "x"}, UserID: "owner"})
	if err != nil || !yes || reversal.Annotation.Payee != "alternate" || reversal.Annotation.Narration != "reason" || !reflect.DeepEqual(reversal.Annotation.Tags, []string{"a", "z"}) || len(reversal.Annotation.Links) != 0 || reversal.Annotation.Metadata["reason"] != "x" {
		t.Fatal(reversal, yes, err)
	}
	if _, yes, err = s.Annotate(ctx, store.AnnotationEdit{Transaction: a.ID, Payee: textPointer("reversed"), UserID: "owner"}); err != nil || !yes {
		t.Fatal(yes, err)
	}
	if _, yes, err = s.Annotate(ctx, store.AnnotationEdit{Transaction: reversal.ID, Payee: textPointer("reversal"), UserID: "owner"}); err != nil || !yes {
		t.Fatal(yes, err)
	}
}

// R-PCOX-2P61 R-PGCM-80E4
func TestRegisterPostingPositionsAndCurrentWords(t *testing.T) {
	s, _, _, _ := transactionFixture(t)
	ctx := context.Background()
	in := store.PostInput{Date: "2026-02-01", Postings: []store.PostingInput{{Account: "Assets:Bank:Checking", Amount: textPointer("10")}, {Account: "Assets:Bank:Checking", Amount: textPointer("20")}, {Account: "Expenses:Office", Amount: textPointer("-30")}}, UserID: "owner"}
	original, made, err := s.Post(ctx, in)
	if err != nil || !made {
		t.Fatal(original, made, err)
	}
	payee, narration := "Current payee", "Current narration"
	current, _, err := s.Annotate(ctx, store.AnnotationEdit{Transaction: original.ID, Payee: &payee, Narration: &narration, UserID: "editor"})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := s.ListRegister(ctx, "Assets:Bank", "", "", "", 0, 0, 1)
	want := store.RegisterRow{Transaction: original.ID, Date: original.Date, Seq: original.Seq, Position: 1, Account: "Assets:Bank:Checking", Amount: original.Postings[0].Amount, Balance: original.Postings[0].Amount, Payee: payee, Narration: narration}
	if err != nil || len(rows) != 1 || !reflect.DeepEqual(rows[0], want) {
		t.Fatal(rows, want, err)
	}
	rows, err = s.ListRegister(ctx, "Assets:Bank", "", "", original.Date, original.Seq, 1, 1)
	want.Position = 2
	want.Amount = original.Postings[1].Amount
	want.Balance.Units = 3000
	if err != nil || len(rows) != 1 || !reflect.DeepEqual(rows[0], want) {
		t.Fatal(rows, want, err)
	}
	rows, err = s.ListRegister(ctx, "Assets:Bank", "", "", original.Date, original.Seq, 0, 10)
	if err != nil || len(rows) != 2 || rows[0].Position != 1 || rows[1].Position != 2 {
		t.Fatal(rows, err)
	}
	reversal, _, err := s.Reverse(ctx, store.ReversalInput{Transaction: original.ID, Date: original.Date, UserID: "owner"})
	if err != nil {
		t.Fatal(err)
	}
	current.ReversedBy = reversal.ID
	ts, err := s.ListTransactions(ctx, store.JournalFilter{}, "", 0, 10)
	if err != nil || !reflect.DeepEqual(ts, []store.Transaction{reversal, current}) {
		t.Fatal(ts, err)
	}
}

type transactionCountReader struct{ reads int }

func (r *transactionCountReader) Read([]byte) (int, error) { r.reads++; return 0, io.ErrUnexpectedEOF }

// R-P0HX-8ZR3 R-P45M-EAZ6 R-P7TB-JM79
func TestRefusalsPreserveRecordsAndRandomness(t *testing.T) {
	s, d, _, now := transactionFixture(t)
	ctx := context.Background()
	original := postFixture(t, s, "2026-02-01", "")
	lock := "2026-02-01"
	if _, _, err := s.SetBook(ctx, store.BookEdit{LockDate: &lock, UserID: "owner"}); err != nil {
		t.Fatal(err)
	}
	counter := &transactionCountReader{}
	st := store.New(d, store.Config{Now: now, Rand: counter})
	before, err := st.Status(ctx)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		run  func() error
		want error
	}{{"undeclared-before-lock", func() error {
		_, _, e := st.Post(ctx, store.PostInput{Date: lock, Postings: []store.PostingInput{{Account: "Assets:Absent", Amount: textPointer("0")}, {Account: "Expenses:Office", Amount: textPointer("0")}}, UserID: "owner"})
		return e
	}, store.ErrNotFound}, {"invalid-before-lock", func() error {
		_, _, e := st.Post(ctx, store.PostInput{Date: "2025-12-31", Postings: []store.PostingInput{{Account: "Assets:Bank:Checking", Amount: textPointer("0")}, {Account: "Expenses:Office", Amount: textPointer("0")}}, UserID: "owner"})
		return e
	}, store.ErrInvalid}, {"reverse-date-before-lock", func() error {
		_, _, e := st.Reverse(ctx, store.ReversalInput{Transaction: original.ID, Date: "2026-01-31", UserID: "owner"})
		return e
	}, store.ErrInvalid}, {"reverse-invalid-before-missing", func() error {
		_, _, e := st.Reverse(ctx, store.ReversalInput{Transaction: "missing", Date: "bad", UserID: "owner"})
		return e
	}, store.ErrInvalid}, {"annotate-invalid-before-missing", func() error {
		_, _, e := st.Annotate(ctx, store.AnnotationEdit{Transaction: "missing", Flag: textPointer(""), UserID: "owner"})
		return e
	}, store.ErrInvalid}, {"post-user-before-values", func() error { _, _, e := st.Post(ctx, store.PostInput{Date: "bad"}); return e }, store.ErrUnreachable}, {"reverse-user-before-values", func() error {
		_, _, e := st.Reverse(ctx, store.ReversalInput{Transaction: original.ID, Date: "bad"})
		return e
	}, store.ErrUnreachable}, {"annotate-user-before-values", func() error {
		_, _, e := st.Annotate(ctx, store.AnnotationEdit{Transaction: original.ID, Flag: textPointer("")})
		return e
	}, store.ErrUnreachable}}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if e := tc.run(); !errors.Is(e, tc.want) {
				t.Fatal(e)
			}
			after, e := st.Status(ctx)
			if e != nil || !reflect.DeepEqual(after, before) {
				t.Fatal(after, e)
			}
			got, e := st.GetTransaction(ctx, original.ID)
			if e != nil || !reflect.DeepEqual(got, original) {
				t.Fatal(got, e)
			}
			versions, e := st.ListVersions(ctx, original.ID, 0, 20)
			if e != nil || !reflect.DeepEqual(versions, []store.Annotation{original.Annotation}) || counter.reads != 0 {
				t.Fatal(versions, counter.reads, e)
			}
		})
	}
}

// R-P2XQ-0J8H R-P5DI-S2PV
func TestAnnotationAndReversalPreserveMoneyAndHistory(t *testing.T) {
	s, d, _, _ := transactionFixture(t)
	ctx := context.Background()
	original := postFixture(t, s, "2026-02-01", "original-key")
	advanced := original.Posted.Add(2 * time.Hour)
	s = store.New(d, store.Config{Now: func() time.Time { return advanced }, Rand: bytes.NewReader(bytes.Repeat([]byte{0x7b}, 8))})
	narration := " revised "
	edited, changed, err := s.Annotate(ctx, store.AnnotationEdit{Transaction: original.ID, Narration: &narration, UserID: "editor"})
	if err != nil || !changed || edited.Annotation.Written != advanced {
		t.Fatal(edited, changed, err)
	}
	expected := original
	expected.Annotation = edited.Annotation
	if !reflect.DeepEqual(edited, expected) || edited.Annotation.Narration != "revised" || edited.Annotation.Flag != original.Annotation.Flag || edited.Annotation.Payee != original.Annotation.Payee || !reflect.DeepEqual(edited.Annotation.Tags, original.Annotation.Tags) || !reflect.DeepEqual(edited.Annotation.Links, original.Annotation.Links) || !reflect.DeepEqual(edited.Annotation.Metadata, original.Annotation.Metadata) {
		t.Fatal(edited, expected)
	}
	reversal, changed, err := s.Reverse(ctx, store.ReversalInput{Transaction: original.ID, Date: original.Date, UserID: "reverser"})
	if err != nil || !changed || reversal.Posted != advanced || reversal.Annotation.Written != advanced {
		t.Fatal(reversal, changed, err)
	}
	expected.ReversedBy = reversal.ID
	got, err := s.GetTransaction(ctx, original.ID)
	if err != nil || !reflect.DeepEqual(got, expected) {
		t.Fatal(got, expected, err)
	}
	expectedReversal := expected
	expectedReversal.ID = reversal.ID
	expectedReversal.Seq = reversal.Seq
	expectedReversal.Key = ""
	expectedReversal.Reverses = original.ID
	expectedReversal.ReversedBy = ""
	expectedReversal.Posted = reversal.Posted
	expectedReversal.PostedUserID = "reverser"
	expectedReversal.Postings = append([]store.Posting(nil), original.Postings...)
	for i := range expectedReversal.Postings {
		expectedReversal.Postings[i].Amount.Units = -expectedReversal.Postings[i].Amount.Units
	}
	expectedReversal.Annotation.Version = 1
	expectedReversal.Annotation.Flag = store.FlagCleared
	expectedReversal.Annotation.Written = reversal.Posted
	expectedReversal.Annotation.WrittenUserID = "reverser"
	if !reflect.DeepEqual(reversal, expectedReversal) {
		t.Fatal(reversal, expectedReversal)
	}
	history, err := s.ListVersions(ctx, original.ID, 0, 10)
	if err != nil || !reflect.DeepEqual(history, []store.Annotation{edited.Annotation, original.Annotation}) {
		t.Fatal(history, err)
	}
}

// R-PBH0-OXFC
func TestInvalidWindowSizeReturnsNil(t *testing.T) {
	s, _, _, _ := transactionFixture(t)
	ctx := context.Background()
	for _, n := range []int{0, -1} {
		a, e := s.ListVersions(ctx, "missing", 0, n)
		if a != nil || !errors.Is(e, store.ErrUnreachable) {
			t.Fatal(a, e)
		}
		b, e := s.ListTransactions(ctx, store.JournalFilter{From: "bad"}, "", 0, n)
		if b != nil || !errors.Is(e, store.ErrUnreachable) {
			t.Fatal(b, e)
		}
		c, e := s.ListRegister(ctx, "bad", "bad", "", "", 0, 0, n)
		if c != nil || !errors.Is(e, store.ErrUnreachable) {
			t.Fatal(c, e)
		}
		d, e := s.ListFailing(ctx, "", "", "", n)
		if d != nil || !errors.Is(e, store.ErrUnreachable) {
			t.Fatal(d, e)
		}
	}
}
