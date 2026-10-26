package tools_test

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/paging"
	"github.com/ikigenba/ikigenba/ledger/internal/store"
	"github.com/ikigenba/ikigenba/ledger/internal/tools"
)

func expectedVersion(a store.Annotation) tools.Version {
	m := a.Metadata
	if m == nil {
		m = map[string]string{}
	}
	raw, _ := json.Marshal(m)
	return tools.Version{Version: a.Version, Flag: a.Flag, Payee: a.Payee, Narration: a.Narration, Tags: append([]string{}, a.Tags...), Links: append([]string{}, a.Links...), Metadata: raw, Written: stamp(a.Written), WrittenBy: a.WrittenUserID}
}
func expectedAssertion(r store.AssertionRow) tools.Assertion {
	a := r.Assertion
	return tools.Assertion{ID: a.ID, Account: a.Account, Date: a.Date, Amount: a.Expected.String(), Commodity: a.Expected.Commodity, Status: r.Status, Actual: r.Actual.String(), Difference: r.Difference.String(), Added: stamp(a.Added), AddedBy: a.AddedUserID, Retracted: stamp(a.Retracted), RetractedBy: a.RetractedUserID}
}
func expectedRow(r store.RegisterRow) tools.RegisterRow {
	return tools.RegisterRow{Transaction: r.Transaction, Date: r.Date, Position: r.Position, Account: r.Account, Amount: r.Amount.String(), Commodity: r.Amount.Commodity, Balance: r.Balance.String(), Payee: r.Payee, Narration: r.Narration}
}
func expectedLines(ls []store.Line) []tools.Line {
	out := []tools.Line{}
	for _, l := range ls {
		out = append(out, tools.Line{Account: l.Account, Amount: l.Amount.String(), Commodity: l.Amount.Commodity})
	}
	return out
}

// R-JVS8-SYME R-K0NU-C1L6 R-K6RC-8WAN R-K7Z8-MO1C R-JH5G-7PQ2
func TestPagedWindows(t *testing.T) {
	h := newHarness(t)
	h.open("Assets:Bank")
	h.open("Income:Sales")
	var first string
	for i := range 30 {
		tx := h.post(fmt.Sprintf("entry-%02d", i))
		if i == 0 {
			first = tx.Transaction.ID
		}
		success[tools.TransactionChange](h, "annotate", tools.AnnotateArgs{Transaction: first, Payee: ptr(fmt.Sprintf("payee-%02d", i))})
		success[tools.Assertion](h, "assert_balance", tools.AssertBalanceArgs{Account: "Assets:Bank", Date: "2026-04-02", Amount: fmt.Sprintf("%d.00", i+2000)})
	}
	versions, err := h.store.ListVersions(t.Context(), first, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	var hv []tools.Version
	var page *string
	for n := 0; n < 10; n++ {
		got := success[tools.History](h, "history", tools.HistoryArgs{Transaction: first, Page: page})
		if got.Transaction != first || got.Versions == nil {
			t.Fatal(got)
		}
		checkPage(t, len(got.Versions), got.Next, len(versions)-len(hv), defaultLimit())
		hv = append(hv, got.Versions...)
		if got.Next == nil {
			break
		}
		var last int64
		if err = (paging.Token{Tool: "history"}).Decode(*got.Next, &last); err != nil || last != got.Versions[len(got.Versions)-1].Version {
			t.Fatal(last, err)
		}
		page = got.Next
	}
	if len(hv) != 31 {
		t.Fatal(len(hv))
	}
	for i, v := range versions {
		equal(t, hv[i], expectedVersion(v))
	}
	checks, err := h.store.ListFailing(t.Context(), "", "", "", 100)
	if err != nil {
		t.Fatal(err)
	}
	var cs []tools.Assertion
	page = nil
	for n := 0; n < 10; n++ {
		got := success[tools.CheckList](h, "check", tools.CheckArgs{Page: page, Limit: ptr(7)})
		checkPage(t, len(got.Assertions), got.Next, len(checks)-len(cs), 7)
		cs = append(cs, got.Assertions...)
		if got.Next == nil {
			break
		}
		last := got.Assertions[len(got.Assertions)-1]
		var d, a, id string
		if err = (paging.Token{Tool: "check"}).Decode(*got.Next, &d, &a, &id); err != nil || d != last.Date || a != last.Account || id != last.ID {
			t.Fatal(d, a, id, err)
		}
		page = got.Next
	}
	if len(cs) != 30 {
		t.Fatal(len(cs))
	}
	for i, c := range checks {
		equal(t, cs[i], expectedAssertion(c))
	}
	txns, err := h.store.ListTransactions(t.Context(), store.JournalFilter{}, "", 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	var js []tools.Transaction
	page = nil
	for n := 0; n < 10; n++ {
		got := success[tools.Journal](h, "journal", tools.JournalArgs{Page: page})
		checkPage(t, len(got.Transactions), got.Next, len(txns)-len(js), defaultLimit())
		js = append(js, got.Transactions...)
		if got.Next == nil {
			break
		}
		last := got.Transactions[len(got.Transactions)-1]
		var d string
		var seq int64
		if err = (paging.Token{Tool: "journal"}).Decode(*got.Next, &d, &seq); err != nil || d != last.Date || seq != last.Seq {
			t.Fatal(d, seq, err)
		}
		page = got.Next
	}
	if len(js) != 30 {
		t.Fatal(len(js))
	}
	for i, v := range txns {
		equal(t, js[i], expectedTransaction(v))
	}
	rows, err := h.store.ListRegister(t.Context(), "Assets", "", "", "", 0, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	var rs []tools.RegisterRow
	page = nil
	for n := 0; n < 10; n++ {
		got := success[tools.RegisterAnswer](h, "register", tools.RegisterArgs{Account: "Assets", Page: page, Limit: ptr(9)})
		if got.Account != "Assets" {
			t.Fatal(got)
		}
		checkPage(t, len(got.Rows), got.Next, len(rows)-len(rs), 9)
		rs = append(rs, got.Rows...)
		if got.Next == nil {
			break
		}
		last := rows[len(rs)-1]
		var d string
		var seq, pos int64
		if err = (paging.Token{Tool: "register"}).Decode(*got.Next, &d, &seq, &pos); err != nil || d != last.Date || seq != last.Seq || pos != last.Position {
			t.Fatal(d, seq, pos, err)
		}
		page = got.Next
	}
	if len(rs) != 30 {
		t.Fatal(len(rs))
	}
	for i, r := range rows {
		equal(t, rs[i], expectedRow(r))
	}
	last := rs[len(rs)-1]
	account := success[tools.AccountRead](h, "account", tools.AccountArgs{Name: "Assets:Bank", Date: &last.Date})
	if account.Balance != last.Balance {
		t.Fatal(account, last)
	}
	// Tokens carry only a position: filters and target can change.
	firstJournal := success[tools.Journal](h, "journal", tools.JournalArgs{Limit: ptr(1)})
	success[tools.Journal](h, "journal", tools.JournalArgs{Page: firstJournal.Next, Payee: ptr("absent")})
	firstHistory := success[tools.History](h, "history", tools.HistoryArgs{Transaction: first, Limit: ptr(1)})
	success[tools.History](h, "history", tools.HistoryArgs{Transaction: txns[0].ID, Page: firstHistory.Next})
}

// R-K975-0FS1 R-KAF1-E7IQ R-KBMX-RZ9F R-JN8Y-4KFJ
func TestWholeReportsAndFilters(t *testing.T) {
	h := newHarness(t)
	h.open("Income:Sales")
	for i := range 30 {
		h.open(fmt.Sprintf("Assets:Bank%02d", i))
	}
	all := success[tools.AccountList](h, "accounts", tools.AccountsArgs{})
	if len(all.Accounts) != 31 {
		t.Fatal(len(all.Accounts))
	}
	h.open("Assets:Bank")
	h.post("report")
	date := "2026-04-01"
	for _, prefix := range []string{"", "Assets"} {
		got := success[tools.Balances](h, "balances", tools.BalancesArgs{Date: &date, Account: &prefix})
		want, err := h.store.Balances(t.Context(), date, prefix)
		if err != nil {
			t.Fatal(err)
		}
		equal(t, got, tools.Balances{Date: want.Date, Lines: expectedLines(want.Lines), Total: want.Total.String(), Commodity: want.Total.Commodity})
	}
	for _, d := range []*string{nil, &date} {
		got := success[tools.BalanceSheetAnswer](h, "balance_sheet", tools.BalanceSheetArgs{Date: d})
		arg := ""
		if d != nil {
			arg = *d
		}
		want, err := h.store.BalanceSheetAt(t.Context(), arg)
		if err != nil {
			t.Fatal(err)
		}
		equal(t, got, tools.BalanceSheetAnswer{Date: want.Date, Lines: expectedLines(want.Lines)})
	}
	is := success[tools.IncomeStatementAnswer](h, "income_statement", tools.IncomeStatementArgs{From: "2026-01-01", To: "2026-04-08"})
	want, err := h.store.IncomeStatementFor(t.Context(), "2026-01-01", "2026-04-08")
	if err != nil {
		t.Fatal(err)
	}
	equal(t, is, tools.IncomeStatementAnswer{From: want.From, To: want.To, Lines: expectedLines(want.Lines), Net: want.Net.String(), Commodity: want.Net.Commodity})
	for _, filter := range []tools.JournalArgs{{From: ptr("2026-04-02")}, {To: ptr("2026-03-31")}, {Account: ptr("Income:Sales")}, {Tag: ptr("absent")}, {Link: ptr("absent")}, {Payee: ptr("absent")}, {Flag: ptr("cleared")}} {
		got := success[tools.Journal](h, "journal", filter)
		f := store.JournalFilter{}
		if filter.From != nil {
			f.From = *filter.From
		}
		if filter.To != nil {
			f.To = *filter.To
		}
		if filter.Account != nil {
			f.Account = *filter.Account
		}
		if filter.Tag != nil {
			f.Tag = *filter.Tag
		}
		if filter.Link != nil {
			f.Link = *filter.Link
		}
		if filter.Payee != nil {
			f.Payee = *filter.Payee
		}
		if filter.Flag != nil {
			f.Flag = *filter.Flag
		}
		vs, err := h.store.ListTransactions(t.Context(), f, "", 0, 100)
		if err != nil {
			t.Fatal(err)
		}
		if len(got.Transactions) != len(vs) {
			t.Fatal(got, vs)
		}
		for i, v := range vs {
			equal(t, got.Transactions[i], expectedTransaction(v))
		}
	}
}

// R-K7Z8-MO1C: date bounds must reach the store, including when continuing a page.
func TestRegisterDateBounds(t *testing.T) {
	h := newHarness(t)
	h.open("Assets:Bank")
	h.open("Income:Sales")
	for _, d := range []string{"2026-03-31", "2026-04-01", "2026-04-02", "2026-04-03"} {
		success[tools.TransactionChange](h, "post", tools.PostArgs{Date: d, Postings: []tools.PostingArgs{{Account: "Assets:Bank", Amount: ptr("1.00")}, {Account: "Income:Sales"}}})
	}
	from, to := "2026-04-01", "2026-04-02"
	a := success[tools.RegisterAnswer](h, "register", tools.RegisterArgs{Account: "Assets:Bank", From: &from, To: &to, Limit: ptr(1)})
	if len(a.Rows) != 1 || a.Rows[0].Date != from || a.Next == nil {
		t.Fatal(a)
	}
	b := success[tools.RegisterAnswer](h, "register", tools.RegisterArgs{Account: "Assets:Bank", From: &from, To: &to, Page: a.Next, Limit: ptr(1)})
	if len(b.Rows) != 1 || b.Rows[0].Date != to || b.Next != nil {
		t.Fatal(b)
	}
	want, err := h.store.ListRegister(t.Context(), "Assets:Bank", from, to, "", 0, 0, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(want) != 2 {
		t.Fatal(want)
	}
	equal(t, a.Rows[0], expectedRow(want[0]))
	equal(t, b.Rows[0], expectedRow(want[1]))
}

func defaultLimit() int { n, _ := paging.Window(nil); return n }
func checkPage(t *testing.T, length int, next *string, remaining, limit int) {
	t.Helper()
	if length != min(remaining, limit) || (next != nil) != (remaining > limit) {
		t.Fatalf("page entries=%d next=%v; remaining=%d limit=%d", length, next, remaining, limit)
	}
}
