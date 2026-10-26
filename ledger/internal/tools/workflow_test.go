package tools_test

import (
	"encoding/json"
	"reflect"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/ledger/internal/store"
	"github.com/ikigenba/ikigenba/ledger/internal/tools"
)

func stamp(v time.Time) string {
	if v.IsZero() {
		return ""
	}
	return v.UTC().Format(time.RFC3339)
}
func expectedAccount(a store.Account) tools.Account {
	return tools.Account{Name: a.Name, OpenDate: a.Open, CloseDate: a.Close, Opened: stamp(a.Opened), OpenedBy: a.OpenedUserID, Closed: stamp(a.Closed), ClosedBy: a.ClosedUserID}
}
func expectedBook(s store.Status) tools.Book {
	return tools.Book{Currency: s.Book.Currency, Precision: s.Book.Precision, LockDate: s.Book.LockDate, FiscalYearStart: s.Book.FiscalYearStart, Updated: stamp(s.Book.Updated), UpdatedBy: s.Book.UpdatedUserID, OpenAccounts: s.OpenAccounts, Transactions: s.Transactions, FailingAssertions: s.FailingAssertions}
}
func expectedTransaction(v store.Transaction) tools.Transaction {
	m := v.Annotation.Metadata
	if m == nil {
		m = map[string]string{}
	}
	raw, _ := json.Marshal(m)
	out := tools.Transaction{ID: v.ID, Seq: v.Seq, Date: v.Date, Key: v.Key, Flag: v.Annotation.Flag, Payee: v.Annotation.Payee, Narration: v.Annotation.Narration, Tags: append([]string{}, v.Annotation.Tags...), Links: append([]string{}, v.Annotation.Links...), Metadata: raw, Version: v.Annotation.Version, Reverses: v.Reverses, ReversedBy: v.ReversedBy, Posted: stamp(v.Posted), PostedBy: v.PostedUserID, Annotated: stamp(v.Annotation.Written), AnnotatedBy: v.Annotation.WrittenUserID, Postings: []tools.Posting{}}
	for _, p := range v.Postings {
		out.Postings = append(out.Postings, tools.Posting{Account: p.Account, Amount: p.Amount.String(), Commodity: p.Amount.Commodity})
	}
	return out
}
func equal[T any](t *testing.T, got, want T) {
	t.Helper()
	a, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	b, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var aa, bb any
	if err = json.Unmarshal(a, &aa); err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(b, &bb); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(aa, bb) {
		t.Fatalf("got %s\nwant %s", a, b)
	}
}

// R-JKT5-D0Y5 R-JM11-QSOU R-JC9U-OMRA R-JPOQ-W3WX R-JN8Y-4KFJ
func TestBookAndChart(t *testing.T) {
	h := newHarness(t)
	s, err := h.store.Status(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	equal(t, success[tools.Book](h, "book", tools.BookArgs{}), expectedBook(s))
	empty := success[tools.AccountList](h, "accounts", tools.AccountsArgs{})
	if empty.Accounts == nil || len(empty.Accounts) != 0 {
		t.Fatal(empty)
	}
	for _, name := range []string{"Income:Sales", "Assets:Bank", "Expenses:Office"} {
		got := h.open(name)
		if !got.Changed || got.Account.OpenedBy != h.caller.UserID || got.Account.Opened != stamp(h.now) || got.Account.CloseDate != "" || got.Account.Closed != "" || got.Account.ClosedBy != "" {
			t.Fatal(got)
		}
		again := h.open(name)
		if again.Changed {
			t.Fatal(again)
		}
		equal(t, again.Account, got.Account)
	}
	as, err := h.store.Accounts(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	got := success[tools.AccountList](h, "accounts", tools.AccountsArgs{})
	want := tools.AccountList{Accounts: []tools.Account{}}
	for _, a := range as {
		want.Accounts = append(want.Accounts, expectedAccount(a))
	}
	equal(t, got, want)
	for _, args := range []tools.SetBookArgs{{LockDate: ptr("2026-01-02")}, {FiscalYearStart: ptr("04-01")}, {LockDate: ptr("")}} {
		change := success[tools.BookChange](h, "set_book", args)
		if !change.Changed || change.Book.UpdatedBy != h.caller.UserID {
			t.Fatal(change)
		}
		s, err = h.store.Status(t.Context())
		if err != nil {
			t.Fatal(err)
		}
		equal(t, change.Book, expectedBook(s))
		repeat := success[tools.BookChange](h, "set_book", args)
		if repeat.Changed {
			t.Fatal(repeat)
		}
	}
	if success[tools.BookChange](h, "set_book", tools.SetBookArgs{}).Changed {
		t.Fatal("empty edit changed")
	}
	closeArgs := tools.AccountDateArgs{Name: "Expenses:Office", Date: "2026-04-07"}
	closed := success[tools.AccountChange](h, "close_account", closeArgs)
	if !closed.Changed || closed.Account.ClosedBy != h.caller.UserID || closed.Account.Closed != stamp(h.now) {
		t.Fatal(closed)
	}
	again := success[tools.AccountChange](h, "close_account", closeArgs)
	if again.Changed {
		t.Fatal(again)
	}
	equal(t, again.Account, closed.Account)
}

// R-RY8T-9SBP R-JS4J-NNEB R-RZGP-NK2E R-S0OM-1BT3 R-JDHR-2EHZ
func TestTransactionLifecycle(t *testing.T) {
	h := newHarness(t)
	h.open("Assets:Bank")
	h.open("Income:Sales")
	first := h.post("invoice-one")
	if !first.Changed || first.Transaction.Flag != "pending" || first.Transaction.Postings[1].Amount != "-42.00" || first.Transaction.PostedBy != h.caller.UserID || first.Transaction.Tags == nil || first.Transaction.Links == nil || string(first.Transaction.Metadata) != "{}" {
		t.Fatal(first)
	}
	v, err := h.store.GetTransaction(t.Context(), first.Transaction.ID)
	if err != nil {
		t.Fatal(err)
	}
	equal(t, first.Transaction, expectedTransaction(v))
	held := success[tools.TransactionChange](h, "post", tools.PostArgs{Date: "bad", Key: ptr("invoice-one"), Postings: []tools.PostingArgs{{Account: "unknown", Amount: ptr("bad")}}})
	if held.Changed {
		t.Fatal(held)
	}
	equal(t, held.Transaction, first.Transaction)
	h.caller = identity.Caller{UserID: "second", Email: "other@example.test", RequestID: "request-two"}
	edit := tools.AnnotateArgs{Transaction: first.Transaction.ID, Payee: ptr("Customer private"), Narration: ptr("Narration private"), Flag: ptr("cleared"), Tags: ptr([]string{"invoice", "paid"}), Links: ptr([]string{"receipt"}), Metadata: json.RawMessage(`{"invoice":"private-value"}`)}
	changed := success[tools.TransactionChange](h, "annotate", edit)
	if !changed.Changed || changed.Transaction.Version != 2 || changed.Transaction.AnnotatedBy != "second" || changed.Transaction.Payee != *edit.Payee || changed.Transaction.Narration != *edit.Narration {
		t.Fatal(changed)
	}
	v, err = h.store.GetTransaction(t.Context(), first.Transaction.ID)
	if err != nil {
		t.Fatal(err)
	}
	equal(t, changed.Transaction, expectedTransaction(v))
	equal(t, success[tools.Transaction](h, "transaction", tools.TransactionArgs{Transaction: v.ID}), changed.Transaction)
	for _, args := range []any{tools.AnnotateArgs{Transaction: v.ID}, json.RawMessage(`{"transaction":"` + v.ID + `","flag":null,"payee":null,"narration":null,"tags":null,"links":null}`)} {
		if success[tools.TransactionChange](h, "annotate", args).Changed {
			t.Fatal("absent/null edit changed")
		}
	}
	cleared := success[tools.TransactionChange](h, "annotate", tools.AnnotateArgs{Transaction: v.ID, Tags: ptr([]string{}), Links: ptr([]string{}), Metadata: json.RawMessage(`{}`)})
	if !cleared.Changed || len(cleared.Transaction.Tags) != 0 || len(cleared.Transaction.Links) != 0 || string(cleared.Transaction.Metadata) != "{}" {
		t.Fatal(cleared)
	}
	rev := success[tools.TransactionChange](h, "reverse", tools.ReverseArgs{Transaction: v.ID})
	if !rev.Changed || rev.Transaction.Date != "2026-04-08" || rev.Transaction.Reverses != v.ID || rev.Transaction.Flag != "cleared" || rev.Transaction.Payee != cleared.Transaction.Payee || rev.Transaction.Postings[0].Amount != "-42.00" || rev.Transaction.Postings[1].Amount != "42.00" || rev.Transaction.PostedBy != "second" {
		t.Fatal(rev)
	}
	v, err = h.store.GetTransaction(t.Context(), rev.Transaction.ID)
	if err != nil {
		t.Fatal(err)
	}
	equal(t, rev.Transaction, expectedTransaction(v))
	again := success[tools.TransactionChange](h, "reverse", tools.ReverseArgs{Transaction: first.Transaction.ID, Date: ptr("2026-04-06")})
	if again.Changed {
		t.Fatal(again)
	}
	equal(t, again.Transaction, rev.Transaction)
	original := success[tools.Transaction](h, "transaction", tools.TransactionArgs{Transaction: first.Transaction.ID})
	if original.ReversedBy != rev.Transaction.ID {
		t.Fatal(original)
	}
}

// R-JX05-6QD3 R-JY81-KI3S R-K1VQ-PTBV R-JFXJ-TXZD R-JOGU-IC68
func TestAssertionsNotesAndAccount(t *testing.T) {
	h := newHarness(t)
	h.open("Assets:Bank")
	h.open("Income:Sales")
	h.post("entry")
	args := tools.AssertBalanceArgs{Account: "Assets:Bank", Date: "2026-04-01", Amount: "42.00"}
	emptyCheck := success[tools.CheckList](h, "check", tools.CheckArgs{})
	if emptyCheck.Assertions == nil || len(emptyCheck.Assertions) != 0 || emptyCheck.Next != nil {
		t.Fatal(emptyCheck)
	}
	a := success[tools.Assertion](h, "assert_balance", args)
	b := success[tools.Assertion](h, "assert_balance", args)
	if a.ID == b.ID || a.Amount != "42.00" || a.Actual != "42.00" || a.Difference != "0.00" || a.AddedBy != h.caller.UserID || a.Retracted != "" || a.RetractedBy != "" {
		t.Fatal(a, b)
	}
	rows, err := h.store.ListFailing(t.Context(), "", "", "", 100)
	if err != nil || len(rows) != 0 {
		t.Fatal(rows, err)
	}
	n := success[tools.Note](h, "add_note", tools.AddNoteArgs{Account: "Assets:Bank", Date: "2026-04-02", Text: "A private note"})
	if n.Text != "A private note" || n.AddedBy != h.caller.UserID || n.Added != stamp(h.now) || n.Retracted != "" || n.RetractedBy != "" {
		t.Fatal(n)
	}
	h.caller = identity.Caller{UserID: "reviewer", RequestID: "review-request"}
	ra := success[tools.AssertionChange](h, "retract_assertion", tools.AssertionArgs{Assertion: a.ID})
	if !ra.Changed || ra.Assertion.RetractedBy != "reviewer" || ra.Assertion.Retracted != stamp(h.now) {
		t.Fatal(ra)
	}
	again := success[tools.AssertionChange](h, "retract_assertion", tools.AssertionArgs{Assertion: a.ID})
	if again.Changed {
		t.Fatal(again)
	}
	equal(t, again.Assertion, ra.Assertion)
	rn := success[tools.NoteChange](h, "retract_note", tools.NoteArgs{Note: n.ID})
	if !rn.Changed || rn.Note.RetractedBy != "reviewer" || rn.Note.Retracted != stamp(h.now) {
		t.Fatal(rn)
	}
	nr := success[tools.NoteChange](h, "retract_note", tools.NoteArgs{Note: n.ID})
	if nr.Changed {
		t.Fatal(nr)
	}
	equal(t, nr.Note, rn.Note)
	read := success[tools.AccountRead](h, "account", tools.AccountArgs{Name: "Assets:Bank"})
	s, err := h.store.GetAccount(t.Context(), "Assets:Bank", "")
	if err != nil {
		t.Fatal(err)
	}
	equal(t, read.Account, expectedAccount(s.Account))
	if read.Date != s.Date || read.Balance != s.Balance.String() || read.Commodity != s.Balance.Commodity || read.Postings != s.Postings || len(read.Assertions) != len(s.Assertions) || len(read.Notes) != len(s.Notes) || read.Documents == nil {
		t.Fatal(read, s)
	}
	if len(read.Notes) != 1 {
		t.Fatal(read)
	}
	equal(t, read.Notes[0], rn.Note)
	found := false
	for _, x := range read.Assertions {
		if x.ID == a.ID {
			equal(t, x, ra.Assertion)
			found = true
		}
	}
	if !found {
		t.Fatal(read)
	}
}

// R-RY8T-9SBP R-RZGP-NK2E R-S0OM-1BT3: explicit words must reach storage, including empty edits.
func TestExplicitPostingAndReversalMembers(t *testing.T) {
	h := newHarness(t)
	h.open("Assets:Bank")
	h.open("Income:Sales")
	in := tools.PostArgs{Date: "2026-03-20", Flag: ptr("cleared"), Payee: ptr("Chosen payee"), Narration: ptr("Chosen narration"), Tags: []string{"invoice", "paid"}, Links: []string{"receipt"}, Metadata: json.RawMessage(`{"invoice":"chosen-value"}`), Key: ptr("explicit-key"), Postings: []tools.PostingArgs{{Account: "Assets:Bank", Amount: ptr("19.25")}, {Account: "Income:Sales", Amount: ptr("-19.25")}}}
	tx := success[tools.TransactionChange](h, "post", in)
	v, err := h.store.GetTransaction(t.Context(), tx.Transaction.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !tx.Changed || v.Date != in.Date || v.Key != *in.Key || v.PostedUserID != h.caller.UserID || v.Annotation.Flag != *in.Flag || v.Annotation.Payee != *in.Payee || v.Annotation.Narration != *in.Narration {
		t.Fatal(tx, v)
	}
	equal(t, v.Annotation.Tags, in.Tags)
	equal(t, v.Annotation.Links, in.Links)
	equal(t, v.Annotation.Metadata, map[string]string{"invoice": "chosen-value"})
	equal(t, tx.Transaction, expectedTransaction(v))
	edit := tools.AnnotateArgs{Transaction: v.ID, Flag: ptr("pending"), Payee: ptr("Edited payee"), Narration: ptr("Edited narration"), Tags: ptr([]string{"edited"}), Links: ptr([]string{"edited-link"}), Metadata: json.RawMessage(`{"edit":"edited-value"}`)}
	annotated := success[tools.TransactionChange](h, "annotate", edit)
	v, err = h.store.GetTransaction(t.Context(), v.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !annotated.Changed || v.Annotation.Flag != *edit.Flag || v.Annotation.Payee != *edit.Payee || v.Annotation.Narration != *edit.Narration || v.Annotation.WrittenUserID != h.caller.UserID {
		t.Fatal(annotated, v)
	}
	equal(t, v.Annotation.Tags, *edit.Tags)
	equal(t, v.Annotation.Links, *edit.Links)
	equal(t, v.Annotation.Metadata, map[string]string{"edit": "edited-value"})
	equal(t, annotated.Transaction, expectedTransaction(v))
	h.caller = identity.Caller{UserID: "corrector", RequestID: "correction-request"}
	reverse := tools.ReverseArgs{Transaction: v.ID, Date: ptr("2026-04-03"), Payee: ptr("Reversal payee"), Narration: ptr("Reversal narration"), Tags: ptr([]string{"reversal"}), Links: ptr([]string{"correction"}), Metadata: json.RawMessage(`{"reason":"chosen-reason"}`)}
	reversed := success[tools.TransactionChange](h, "reverse", reverse)
	rv, err := h.store.GetTransaction(t.Context(), reversed.Transaction.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !reversed.Changed || rv.Date != *reverse.Date || rv.Reverses != v.ID || rv.PostedUserID != h.caller.UserID || rv.Annotation.Payee != *reverse.Payee || rv.Annotation.Narration != *reverse.Narration {
		t.Fatal(reversed, rv)
	}
	equal(t, rv.Annotation.Tags, *reverse.Tags)
	equal(t, rv.Annotation.Links, *reverse.Links)
	equal(t, rv.Annotation.Metadata, map[string]string{"reason": "chosen-reason"})
	equal(t, reversed.Transaction, expectedTransaction(rv))
}
