package tools

import (
	"context"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/ledger/internal/store"
)

func (h handlers) book(ctx context.Context, _ identity.Caller, _ BookArgs) (out Book, err error) {
	s, e := h.Store.Status(ctx)
	return bookAnswer(s), storeError(e)
}
func (h handlers) accounts(ctx context.Context, _ identity.Caller, _ AccountsArgs) (out AccountList, err error) {
	as, e := h.Store.Accounts(ctx)
	if e != nil {
		return out, storeError(e)
	}
	out.Accounts = make([]Account, 0, len(as))
	for _, a := range as {
		out.Accounts = append(out.Accounts, accountAnswer(a))
	}
	return out, nil
}
func (h handlers) account(ctx context.Context, _ identity.Caller, in AccountArgs) (out AccountRead, err error) {
	r, e := h.Store.GetAccount(ctx, in.Name, value(in.Date))
	if e != nil {
		return out, storeError(e)
	}
	out = AccountRead{Account: accountAnswer(r.Account), Date: r.Date, Balance: r.Balance.String(), Commodity: r.Balance.Commodity, Postings: r.Postings, Assertions: []Assertion{}, Notes: []Note{}, Documents: []Document{}}
	for _, a := range r.Assertions {
		out.Assertions = append(out.Assertions, assertionAnswer(a))
	}
	for _, n := range r.Notes {
		out.Notes = append(out.Notes, noteAnswer(n))
	}
	for _, d := range r.Documents {
		out.Documents = append(out.Documents, documentAnswer(d))
	}
	return out, nil
}
func (h handlers) transaction(ctx context.Context, _ identity.Caller, in TransactionArgs) (out Transaction, err error) {
	t, e := h.Store.GetTransaction(ctx, in.Transaction)
	return transactionAnswer(t), storeError(e)
}
func (h handlers) history(ctx context.Context, _ identity.Caller, in HistoryArgs) (out History, err error) {
	var v int64
	z, e := window("history", in.Page, in.Limit, &v)
	if e != nil {
		return out, e
	}
	w, e := h.Store.ListVersions(ctx, in.Transaction, v, z+1)
	if e != nil {
		return out, storeError(e)
	}
	out.Transaction = in.Transaction
	out.Versions = []Version{}
	for j, a := range w {
		if j == z {
			out.Next = next("history", w[j-1].Version)
			break
		}
		out.Versions = append(out.Versions, versionAnswer(a))
	}
	return out, nil
}
func (h handlers) check(ctx context.Context, _ identity.Caller, in CheckArgs) (out CheckList, err error) {
	var d, a, i string
	z, e := window("check", in.Page, in.Limit, &d, &a, &i)
	if e != nil {
		return out, e
	}
	w, e := h.Store.ListFailing(ctx, d, a, i, z+1)
	if e != nil {
		return out, storeError(e)
	}
	out.Assertions = []Assertion{}
	for j, r := range w {
		if j == z {
			p := w[j-1].Assertion
			out.Next = next("check", p.Date, p.Account, p.ID)
			break
		}
		out.Assertions = append(out.Assertions, assertionAnswer(r))
	}
	return out, nil
}
func (h handlers) journal(ctx context.Context, _ identity.Caller, in JournalArgs) (out Journal, err error) {
	var d string
	var s int64
	z, e := window("journal", in.Page, in.Limit, &d, &s)
	if e != nil {
		return out, e
	}
	w, e := h.Store.ListTransactions(ctx, journalFilter(in), d, s, z+1)
	if e != nil {
		return out, storeError(e)
	}
	out.Transactions = []Transaction{}
	for j, t := range w {
		if j == z {
			p := w[j-1]
			out.Next = next("journal", p.Date, p.Seq)
			break
		}
		out.Transactions = append(out.Transactions, transactionAnswer(t))
	}
	return out, nil
}
func (h handlers) register(ctx context.Context, _ identity.Caller, in RegisterArgs) (out RegisterAnswer, err error) {
	var d string
	var s, p int64
	z, e := window("register", in.Page, in.Limit, &d, &s, &p)
	if e != nil {
		return out, e
	}
	w, e := h.Store.ListRegister(ctx, in.Account, value(in.From), value(in.To), d, s, p, z+1)
	if e != nil {
		return out, storeError(e)
	}
	out.Account = in.Account
	out.Rows = []RegisterRow{}
	for j, r := range w {
		if j == z {
			p := w[j-1]
			out.Next = next("register", p.Date, p.Seq, p.Position)
			break
		}
		out.Rows = append(out.Rows, rowAnswer(r))
	}
	return out, nil
}
func (h handlers) balances(ctx context.Context, _ identity.Caller, in BalancesArgs) (out Balances, err error) {
	t, e := h.Store.Balances(ctx, value(in.Date), value(in.Account))
	if e != nil {
		return out, storeError(e)
	}
	return Balances{t.Date, linesAnswer(t.Lines), t.Total.String(), t.Total.Commodity}, nil
}
func (h handlers) balanceSheet(ctx context.Context, _ identity.Caller, in BalanceSheetArgs) (out BalanceSheetAnswer, err error) {
	b, e := h.Store.BalanceSheetAt(ctx, value(in.Date))
	if e != nil {
		return out, storeError(e)
	}
	return BalanceSheetAnswer{b.Date, linesAnswer(b.Lines)}, nil
}
func (h handlers) incomeStatement(ctx context.Context, _ identity.Caller, in IncomeStatementArgs) (out IncomeStatementAnswer, err error) {
	s, e := h.Store.IncomeStatementFor(ctx, in.From, in.To)
	if e != nil {
		return out, storeError(e)
	}
	return IncomeStatementAnswer{s.From, s.To, linesAnswer(s.Lines), s.Net.String(), s.Net.Commodity}, nil
}

func journalFilter(in JournalArgs) store.JournalFilter {
	return store.JournalFilter{From: value(in.From), To: value(in.To), Account: value(in.Account), Tag: value(in.Tag), Link: value(in.Link), Payee: value(in.Payee), Flag: value(in.Flag)}
}
