package tools_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/appkit/paging"
	"github.com/ikigenba/ikigenba/ledger/internal/store"
	"github.com/ikigenba/ikigenba/ledger/internal/tools"
)

// R-H7G6-L31J R-H8O2-YUS8
func TestRefusalVocabulary(t *testing.T) {
	equal(t, []string{tools.RefusalNotFound, tools.RefusalInvalid, tools.RefusalUnavailable}, []string{"not_found", "invalid", "unavailable"})
	seen := map[string]bool{store.Unreachable: true}
	for _, s := range []string{tools.RefuseNotFound, tools.RefuseNoArtifact, tools.RefuseInvalid, tools.RefuseUnbalanced, tools.RefuseLocked, tools.RefuseArtifacts} {
		if s == "" || strings.Contains(s, "\n") || seen[s] {
			t.Fatal(s)
		}
		seen[s] = true
	}
}

// R-J9U1-X39W R-JB1Y-AV0L R-J8M5-JBJ7
func TestStoreRefusals(t *testing.T) {
	h := newHarness(t)
	h.open("Assets:Bank")
	h.open("Income:Sales")
	for _, tc := range []struct {
		name       string
		args       any
		word, text string
	}{
		{"transaction", tools.TransactionArgs{Transaction: "malformed-private-id"}, tools.RefusalNotFound, tools.RefuseNotFound},
		{"account", tools.AccountArgs{Name: "Assets:Missing"}, tools.RefusalNotFound, tools.RefuseNotFound},
		{"post", tools.PostArgs{Date: "2026-04-01", Postings: []tools.PostingArgs{{Account: "Assets:Missing", Amount: ptr("1.00")}, {Account: "Income:Sales"}}}, tools.RefusalNotFound, tools.RefuseNotFound},
		{"post", tools.PostArgs{Date: "2026-04-01", Postings: []tools.PostingArgs{{Account: "Assets:Bank", Amount: ptr("1.00")}, {Account: "Income:Sales", Amount: ptr("1.00")}}}, tools.RefusalInvalid, tools.RefuseUnbalanced},
		{"post", tools.PostArgs{Date: "2026-04-01", Postings: []tools.PostingArgs{{Account: "Assets:Bank", Amount: ptr("1.001")}, {Account: "Income:Sales"}}}, tools.RefusalInvalid, tools.RefuseInvalid},
	} {
		h.refuse(tc.name, tc.args, tc.word+": "+tc.text)
	}
	success[tools.BookChange](h, "set_book", tools.SetBookArgs{LockDate: ptr("2026-04-01")})
	h.refuse("post", tools.PostArgs{Date: "2026-04-01", Postings: []tools.PostingArgs{{Account: "Assets:Bank", Amount: ptr("1.00")}, {Account: "Income:Sales"}}}, tools.RefusalInvalid+": "+tools.RefuseLocked)
	h.db.SetFailing(true)
	for _, name := range []string{"book", "accounts", "journal", "check", "balances", "balance_sheet"} {
		h.refuse(name, map[string]any{}, tools.RefusalUnavailable+": "+store.Unreachable)
	}
	for _, tc := range []struct {
		name string
		args any
	}{{"account", tools.AccountArgs{Name: "Assets:Bank"}}, {"transaction", tools.TransactionArgs{Transaction: "missing"}}, {"history", tools.HistoryArgs{Transaction: "missing"}}, {"register", tools.RegisterArgs{Account: "Assets:Bank"}}, {"set_book", tools.SetBookArgs{}}, {"open_account", tools.AccountDateArgs{Name: "Assets:New", Date: "2026-04-02"}}, {"close_account", tools.AccountDateArgs{Name: "Assets:Bank", Date: "2026-04-02"}}, {"post", tools.PostArgs{Date: "2026-04-02", Postings: []tools.PostingArgs{{Account: "Assets:Bank", Amount: ptr("1.00")}, {Account: "Income:Sales"}}}}, {"annotate", tools.AnnotateArgs{Transaction: "missing"}}, {"reverse", tools.ReverseArgs{Transaction: "missing"}}, {"assert_balance", tools.AssertBalanceArgs{Account: "Assets:Bank", Date: "2026-04-02", Amount: "1.00"}}, {"retract_assertion", tools.AssertionArgs{Assertion: "missing"}}, {"add_note", tools.AddNoteArgs{Account: "Assets:Bank", Date: "2026-04-02", Text: "text"}}, {"retract_note", tools.NoteArgs{Note: "missing"}}, {"retract_document", tools.DocumentArgs{Document: "missing"}}, {"income_statement", tools.IncomeStatementArgs{From: "2026-01-01", To: "2026-04-02"}}} {
		h.refuse(tc.name, tc.args, tools.RefusalUnavailable+": "+store.Unreachable)
	}
}

// R-JH5G-7PQ2 R-JIDC-LHGR R-U4XW-7MO6 R-I816-27AD R-HFZH-9H8E
func TestPreflightBeforeUnreachableStore(t *testing.T) {
	h := newHarness(t)
	h.open("Assets:Bank")
	h.open("Income:Sales")
	held := h.post("held-key")
	h.db.SetFailing(true)
	for _, name := range []string{"history", "check", "journal", "register"} {
		base := map[string]any{}
		if name == "history" {
			base["transaction"] = "missing"
		}
		if name == "register" {
			base["account"] = "missing"
		}
		for _, limit := range []int{0, -1, paging.MaxLimit + 1} {
			base["page"] = "bogus"
			base["limit"] = limit
			_, err := paging.Window(&limit)
			h.refuse(name, base, err.Error())
		}
		delete(base, "limit")
		h.refuse(name, base, paging.BadPage)
		foreign := (paging.Token{Tool: "foreign"}).Encode("date", int64(2), int64(3))
		base["page"] = foreign
		h.refuse(name, base, paging.BadPage)
		for _, page := range []any{nil, ""} {
			base["page"] = page
			base["limit"] = nil
			h.refuse(name, base, tools.RefusalUnavailable+": "+store.Unreachable)
		}
	}
	for _, name := range []string{"post", "annotate", "reverse"} {
		for _, metadata := range []string{`{"a":1}`, `{"a":null}`, `{"a":{"b":"c"}}`, `{"a":"x","a":"y"}`} {
			var args json.RawMessage
			if name == "post" {
				args = json.RawMessage(`{"date":"bad","postings":[],"key":"held-key","metadata":` + metadata + `}`)
			} else {
				args = json.RawMessage(`{"transaction":"` + held.Transaction.ID + `","metadata":` + metadata + `}`)
			}
			h.refuse(name, args, tools.RefusalInvalid+": "+tools.RefuseInvalid)
		}
	}
	for _, tc := range []struct {
		name string
		args json.RawMessage
	}{{"book", json.RawMessage(`{"page":"anything"}`)}, {"accounts", json.RawMessage(`{"limit":1}`)}, {"post", json.RawMessage(`{"date":"2026-04-01","postings":[],"metadata":[]}`)}, {"post", json.RawMessage(`{"date":3,"postings":[],"key":"held-key"}`)}, {"account", json.RawMessage(`{}`)}, {"post", json.RawMessage(`{"date":"2026-04-01","postings":[],"key":"held-key","extra":true}`)}} {
		r := h.call(tc.name, tc.args)
		if !r.IsError() {
			t.Fatal(tc.name, r)
		}
		if unpack(t, r).Content[0].Text == tools.RefusalUnavailable+": "+store.Unreachable {
			t.Fatal("argument checking reached store")
		}
	}
}
