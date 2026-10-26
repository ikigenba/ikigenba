package tools_test

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/paging"
	"github.com/ikigenba/ikigenba/ledger/internal/tools"
)

type expectedField struct {
	name, kind, child, description string
	required                       bool
}

var inputFields = map[string][]expectedField{
	// R-HFZH-9H8E
	"BookArgs": {},
	// R-HH7D-N8Z3
	"SetBookArgs": {
		{"lock_date", "string", "", tools.ArgSetBookLockDate, false},
		{"fiscal_year_start", "string", "", tools.ArgSetBookFiscalYearStart, false},
	},
	// R-HIFA-10PS
	"AccountArgs": {
		{"name", "string", "", tools.ArgAccountName, true},
		{"date", "string", "", tools.ArgAccountDate, false},
	},
	// R-HJN6-ESGH
	"AccountDateArgs": {
		{"name", "string", "", tools.ArgAccountDateName, true},
		{"date", "string", "", tools.ArgAccountDateDate, true},
	},
	// R-HKV2-SK76
	"PostingArgs": {
		{"account", "string", "", tools.ArgPostingAccount, true},
		{"amount", "string", "", tools.ArgPostingAmount, false},
	},
	// R-HM2Z-6BXV
	"PostArgs": {
		{"date", "string", "", tools.ArgPostDate, true},
		{"postings", "array", "PostingArgs", tools.ArgPostPostings, true},
		{"flag", "string", "", tools.ArgPostFlag, false},
		{"payee", "string", "", tools.ArgPostPayee, false},
		{"narration", "string", "", tools.ArgPostNarration, false},
		{"tags", "array", "string", tools.ArgPostTags, false},
		{"links", "array", "string", tools.ArgPostLinks, false},
		{"metadata", "object", "", tools.ArgPostMetadata, false},
		{"key", "string", "", tools.ArgPostKey, false},
	},
	// R-HNAV-K3OK
	"TransactionArgs": {
		{"transaction", "string", "", tools.ArgTransactionTransaction, true},
	},
	// R-HOIR-XVF9
	"AnnotateArgs": {
		{"transaction", "string", "", tools.ArgAnnotateTransaction, true},
		{"flag", "string", "", tools.ArgAnnotateFlag, false},
		{"payee", "string", "", tools.ArgAnnotatePayee, false},
		{"narration", "string", "", tools.ArgAnnotateNarration, false},
		{"tags", "array", "string", tools.ArgAnnotateTags, false},
		{"links", "array", "string", tools.ArgAnnotateLinks, false},
		{"metadata", "object", "", tools.ArgAnnotateMetadata, false},
	},
	// R-HQYK-PEWN
	"ReverseArgs": {
		{"transaction", "string", "", tools.ArgReverseTransaction, true},
		{"date", "string", "", tools.ArgReverseDate, false},
		{"payee", "string", "", tools.ArgReversePayee, false},
		{"narration", "string", "", tools.ArgReverseNarration, false},
		{"tags", "array", "string", tools.ArgReverseTags, false},
		{"links", "array", "string", tools.ArgReverseLinks, false},
		{"metadata", "object", "", tools.ArgReverseMetadata, false},
	},
	// R-HS6H-36NC
	"HistoryArgs": {
		{"transaction", "string", "", tools.ArgHistoryTransaction, true},
		{"page", "string", "", paging.PageDescription, false},
		{"limit", "integer", "", paging.LimitDescription, false},
	},
	// R-HTED-GYE1
	"AssertBalanceArgs": {
		{"account", "string", "", tools.ArgAssertBalanceAccount, true},
		{"date", "string", "", tools.ArgAssertBalanceDate, true},
		{"amount", "string", "", tools.ArgAssertBalanceAmount, true},
	},
	// R-HUM9-UQ4Q
	"AssertionArgs": {
		{"assertion", "string", "", tools.ArgAssertionAssertion, true},
	},
	// R-HVU6-8HVF
	"CheckArgs": {
		{"page", "string", "", paging.PageDescription, false},
		{"limit", "integer", "", paging.LimitDescription, false},
	},
	// R-HX22-M9M4
	"AddNoteArgs": {
		{"account", "string", "", tools.ArgAddNoteAccount, true},
		{"date", "string", "", tools.ArgAddNoteDate, true},
		{"text", "string", "", tools.ArgAddNoteText, true},
	},
	// R-HY9Z-01CT
	"NoteArgs": {
		{"note", "string", "", tools.ArgNoteNote, true},
	},
	// R-HZHV-DT3I
	"AddDocumentArgs": {
		{"account", "string", "", tools.ArgAddDocumentAccount, true},
		{"date", "string", "", tools.ArgAddDocumentDate, true},
		{"path", "string", "", tools.ArgAddDocumentPath, true},
		{"transaction", "string", "", tools.ArgAddDocumentTransaction, false},
	},
	// R-I0PR-RKU7
	"DocumentArgs": {
		{"document", "string", "", tools.ArgDocumentDocument, true},
	},
	// R-I1XO-5CKW
	"JournalArgs": {
		{"from", "string", "", tools.ArgJournalFrom, false},
		{"to", "string", "", tools.ArgJournalTo, false},
		{"account", "string", "", tools.ArgJournalAccount, false},
		{"tag", "string", "", tools.ArgJournalTag, false},
		{"link", "string", "", tools.ArgJournalLink, false},
		{"payee", "string", "", tools.ArgJournalPayee, false},
		{"flag", "string", "", tools.ArgJournalFlag, false},
		{"page", "string", "", paging.PageDescription, false},
		{"limit", "integer", "", paging.LimitDescription, false},
	},
	// R-I35K-J4BL
	"RegisterArgs": {
		{"account", "string", "", tools.ArgRegisterAccount, true},
		{"from", "string", "", tools.ArgRegisterFrom, false},
		{"to", "string", "", tools.ArgRegisterTo, false},
		{"page", "string", "", paging.PageDescription, false},
		{"limit", "integer", "", paging.LimitDescription, false},
	},
	// R-I4DG-WW2A
	"BalancesArgs": {
		{"date", "string", "", tools.ArgBalancesDate, false},
		{"account", "string", "", tools.ArgBalancesAccount, false},
	},
	// R-I5LD-ANSZ
	"BalanceSheetArgs": {
		{"date", "string", "", tools.ArgBalanceSheetDate, false},
	},
	// R-I6T9-OFJO
	"IncomeStatementArgs": {
		{"from", "string", "", tools.ArgIncomeStatementFrom, true},
		{"to", "string", "", tools.ArgIncomeStatementTo, true},
	},
}
var outputFields = map[string][]expectedField{
	// R-IAGY-TQRR
	"Book": {
		{"currency", "string", "", "", true},
		{"precision", "integer", "", "", true},
		{"lock_date", "string", "", "", true},
		{"fiscal_year_start", "string", "", "", true},
		{"updated", "string", "", "", true},
		{"updated_by", "string", "", "", true},
		{"open_accounts", "integer", "", "", true},
		{"transactions", "integer", "", "", true},
		{"failing_assertions", "integer", "", "", true},
	},
	// R-IBOV-7IIG
	"BookChange": {
		{"book", "object", "Book", "", true},
		{"changed", "boolean", "", "", true},
	},
	// R-ICWR-LA95
	"Account": {
		{"name", "string", "", "", true},
		{"open_date", "string", "", "", true},
		{"close_date", "string", "", "", true},
		{"opened", "string", "", "", true},
		{"opened_by", "string", "", "", true},
		{"closed", "string", "", "", true},
		{"closed_by", "string", "", "", true},
	},
	// R-IE4N-Z1ZU
	"AccountList": {
		{"accounts", "array", "Account", "", true},
	},
	// R-IFCK-CTQJ
	"AccountRead": {
		{"account", "object", "Account", "", true},
		{"date", "string", "", "", true},
		{"balance", "string", "", "", true},
		{"commodity", "string", "", "", true},
		{"postings", "integer", "", "", true},
		{"assertions", "array", "Assertion", "", true},
		{"notes", "array", "Note", "", true},
		{"documents", "array", "Document", "", true},
	},
	// R-IGKG-QLH8
	"AccountChange": {
		{"account", "object", "Account", "", true},
		{"changed", "boolean", "", "", true},
	},
	// R-IHSD-4D7X
	"Posting": {
		{"account", "string", "", "", true},
		{"amount", "string", "", "", true},
		{"commodity", "string", "", "", true},
	},
	// R-IJ09-I4YM
	"Transaction": {
		{"id", "string", "", "", true},
		{"seq", "integer", "", "", true},
		{"date", "string", "", "", true},
		{"key", "string", "", "", true},
		{"flag", "string", "", "", true},
		{"payee", "string", "", "", true},
		{"narration", "string", "", "", true},
		{"tags", "array", "string", "", true},
		{"links", "array", "string", "", true},
		{"metadata", "object", "", "", true},
		{"version", "integer", "", "", true},
		{"reverses", "string", "", "", true},
		{"reversed_by", "string", "", "", true},
		{"posted", "string", "", "", true},
		{"posted_by", "string", "", "", true},
		{"annotated", "string", "", "", true},
		{"annotated_by", "string", "", "", true},
		{"postings", "array", "Posting", "", true},
	},
	// R-IK85-VWPB
	"TransactionChange": {
		{"transaction", "object", "Transaction", "", true},
		{"changed", "boolean", "", "", true},
	},
	// R-ILG2-9OG0
	"Version": {
		{"version", "integer", "", "", true},
		{"flag", "string", "", "", true},
		{"payee", "string", "", "", true},
		{"narration", "string", "", "", true},
		{"tags", "array", "string", "", true},
		{"links", "array", "string", "", true},
		{"metadata", "object", "", "", true},
		{"written", "string", "", "", true},
		{"written_by", "string", "", "", true},
	},
	// R-IMNY-NG6P
	"History": {
		{"transaction", "string", "", "", true},
		{"versions", "array", "Version", "", true},
		{"next", "string", "", paging.NextDescription, false},
	},
	// R-INVV-17XE
	"Assertion": {
		{"id", "string", "", "", true},
		{"account", "string", "", "", true},
		{"date", "string", "", "", true},
		{"amount", "string", "", "", true},
		{"commodity", "string", "", "", true},
		{"status", "string", "", "", true},
		{"actual", "string", "", "", true},
		{"difference", "string", "", "", true},
		{"added", "string", "", "", true},
		{"added_by", "string", "", "", true},
		{"retracted", "string", "", "", true},
		{"retracted_by", "string", "", "", true},
	},
	// R-IP3R-EZO3
	"AssertionChange": {
		{"assertion", "object", "Assertion", "", true},
		{"changed", "boolean", "", "", true},
	},
	// R-IQBN-SRES
	"CheckList": {
		{"assertions", "array", "Assertion", "", true},
		{"next", "string", "", paging.NextDescription, false},
	},
	// R-IRJK-6J5H
	"Note": {
		{"id", "string", "", "", true},
		{"account", "string", "", "", true},
		{"date", "string", "", "", true},
		{"text", "string", "", "", true},
		{"added", "string", "", "", true},
		{"added_by", "string", "", "", true},
		{"retracted", "string", "", "", true},
		{"retracted_by", "string", "", "", true},
	},
	// R-ITZC-Y2MV
	"NoteChange": {
		{"note", "object", "Note", "", true},
		{"changed", "boolean", "", "", true},
	},
	// R-IV79-BUDK
	"Document": {
		{"id", "string", "", "", true},
		{"account", "string", "", "", true},
		{"transaction", "string", "", "", true},
		{"date", "string", "", "", true},
		{"path", "string", "", "", true},
		{"artifact", "string", "", "", true},
		{"added", "string", "", "", true},
		{"added_by", "string", "", "", true},
		{"retracted", "string", "", "", true},
		{"retracted_by", "string", "", "", true},
	},
	// R-IWF5-PM49
	"DocumentChange": {
		{"document", "object", "Document", "", true},
		{"changed", "boolean", "", "", true},
	},
	// R-IXN2-3DUY
	"Journal": {
		{"transactions", "array", "Transaction", "", true},
		{"next", "string", "", paging.NextDescription, false},
	},
	// R-IYUY-H5LN
	"RegisterRow": {
		{"transaction", "string", "", "", true},
		{"date", "string", "", "", true},
		{"position", "integer", "", "", true},
		{"account", "string", "", "", true},
		{"amount", "string", "", "", true},
		{"commodity", "string", "", "", true},
		{"balance", "string", "", "", true},
		{"payee", "string", "", "", true},
		{"narration", "string", "", "", true},
	},
	// R-J02U-UXCC
	"RegisterAnswer": {
		{"account", "string", "", "", true},
		{"rows", "array", "RegisterRow", "", true},
		{"next", "string", "", paging.NextDescription, false},
	},
	// R-J1AR-8P31
	"Line": {
		{"account", "string", "", "", true},
		{"amount", "string", "", "", true},
		{"commodity", "string", "", "", true},
	},
	// R-J2IN-MGTQ
	"Balances": {
		{"date", "string", "", "", true},
		{"lines", "array", "Line", "", true},
		{"total", "string", "", "", true},
		{"commodity", "string", "", "", true},
	},
	// R-J3QK-08KF
	"BalanceSheetAnswer": {
		{"date", "string", "", "", true},
		{"lines", "array", "Line", "", true},
	},
	// R-J4YG-E0B4
	"IncomeStatementAnswer": {
		{"from", "string", "", "", true},
		{"to", "string", "", "", true},
		{"lines", "array", "Line", "", true},
		{"net", "string", "", "", true},
		{"commodity", "string", "", "", true},
	},
}

type schema struct {
	Type        string          `json:"type"`
	Description string          `json:"description"`
	Properties  json.RawMessage `json:"properties"`
	Required    []string        `json:"required"`
	Items       json.RawMessage `json:"items"`
}

func keys(t *testing.T, raw json.RawMessage) []string {
	t.Helper()
	if len(raw) == 0 {
		return nil
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	if _, err := d.Token(); err != nil {
		t.Fatal(err)
	}
	var result []string
	for d.More() {
		token, err := d.Token()
		if err != nil {
			t.Fatal(err)
		}
		result = append(result, token.(string))
		var value json.RawMessage
		if err = d.Decode(&value); err != nil {
			t.Fatal(err)
		}
	}
	return result
}
func checkSchema(t *testing.T, raw json.RawMessage, fields []expectedField, all map[string][]expectedField) {
	t.Helper()
	var s schema
	if err := json.Unmarshal(raw, &s); err != nil {
		t.Fatal(err)
	}
	if s.Type != "object" {
		t.Fatalf("schema type %s", s.Type)
	}
	var want []string
	var required []string
	var props map[string]json.RawMessage
	if len(s.Properties) > 0 {
		if err := json.Unmarshal(s.Properties, &props); err != nil {
			t.Fatal(err)
		}
	}
	for _, f := range fields {
		want = append(want, f.name)
		if f.required {
			required = append(required, f.name)
		}
		var got schema
		if err := json.Unmarshal(props[f.name], &got); err != nil {
			t.Fatalf("%s: %v", f.name, err)
		}
		if got.Type != f.kind || f.description != "" && got.Description != f.description {
			t.Errorf("%s schema %+v, want %+v", f.name, got, f)
		}
		if f.child != "" {
			child := props[f.name]
			if f.kind == "array" {
				child = got.Items
			}
			if f.child == "string" {
				var item schema
				if err := json.Unmarshal(child, &item); err != nil {
					t.Fatal(err)
				}
				if item.Type != "string" {
					t.Errorf("%s item type %s", f.name, item.Type)
				}
			} else {
				checkSchema(t, child, all[f.child], all)
			}
		}
	}
	if !reflect.DeepEqual(keys(t, s.Properties), want) {
		t.Errorf("properties %v, want %v", keys(t, s.Properties), want)
	}
	if !reflect.DeepEqual(s.Required, required) {
		t.Errorf("required %v, want %v", s.Required, required)
	}
}

// R-HERK-VPHP R-I816-27AD R-3IT1-23YA R-J66C-RS1T R-HB3V-QE9M
func TestToolSchemas(t *testing.T) {
	h := newHarness(t)
	for _, fields := range inputFields {
		for _, f := range fields {
			if f.description == "" || !utf8.ValidString(f.description) {
				t.Fatalf("invalid argument description %q", f.description)
			}
		}
	}
	list, err := h.client.ListTools(t.Context(), h.caller)
	if err != nil {
		t.Fatal(err)
	}
	catalog := tools.Catalog()
	registered := h.server.Tools()
	if len(list) != 23 || len(registered) != 23 || len(catalog) != 23 {
		t.Fatalf("counts %d %d %d", len(list), len(registered), len(catalog))
	}
	cases := []struct {
		name, in, out string
		effect        mcp.Effect
	}{
		{"book", "BookArgs", "Book", mcp.Read},
		{"set_book", "SetBookArgs", "BookChange", mcp.Additive},
		{"accounts", "AccountsArgs", "AccountList", mcp.Read},
		{"account", "AccountArgs", "AccountRead", mcp.Read},
		{"open_account", "AccountDateArgs", "AccountChange", mcp.Additive},
		{"close_account", "AccountDateArgs", "AccountChange", mcp.Additive},
		{"post", "PostArgs", "TransactionChange", mcp.Additive},
		{"transaction", "TransactionArgs", "Transaction", mcp.Read},
		{"annotate", "AnnotateArgs", "TransactionChange", mcp.Additive},
		{"reverse", "ReverseArgs", "TransactionChange", mcp.Additive},
		{"history", "HistoryArgs", "History", mcp.Read},
		{"assert_balance", "AssertBalanceArgs", "Assertion", mcp.Additive},
		{"retract_assertion", "AssertionArgs", "AssertionChange", mcp.Additive},
		{"check", "CheckArgs", "CheckList", mcp.Read},
		{"add_note", "AddNoteArgs", "Note", mcp.Additive},
		{"retract_note", "NoteArgs", "NoteChange", mcp.Additive},
		{"add_document", "AddDocumentArgs", "Document", mcp.Additive},
		{"retract_document", "DocumentArgs", "DocumentChange", mcp.Additive},
		{"journal", "JournalArgs", "Journal", mcp.Read},
		{"register", "RegisterArgs", "RegisterAnswer", mcp.Read},
		{"balances", "BalancesArgs", "Balances", mcp.Read},
		{"balance_sheet", "BalanceSheetArgs", "BalanceSheetAnswer", mcp.Read},
		{"income_statement", "IncomeStatementArgs", "IncomeStatementAnswer", mcp.Read},
	}
	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := list[i]
			if got.Name != c.name || got.Description != catalog[i].Description || registered[i].Name != c.name || registered[i].Description != got.Description || got.Effect() != c.effect {
				t.Fatalf("tool %d: %+v", i, got)
			}
			checkSchema(t, got.InputSchema, inputFields[c.in], inputFields)
			checkSchema(t, got.OutputSchema, outputFields[c.out], outputFields)
		})
	}
}

// R-H1CO-O8C2 R-H2KL-202R R-H3SH-FRTG R-HCBS-460B R-HDJO-HXR0 R-H9VZ-CMIX
func TestCatalogAndFirstLine(t *testing.T) {
	want := []tools.Entry{
		{Name: "book", Description: tools.DescribeBook},
		{Name: "set_book", Description: tools.DescribeSetBook},
		{Name: "accounts", Description: tools.DescribeAccounts},
		{Name: "account", Description: tools.DescribeAccount},
		{Name: "open_account", Description: tools.DescribeOpenAccount},
		{Name: "close_account", Description: tools.DescribeCloseAccount},
		{Name: "post", Description: tools.DescribePost},
		{Name: "transaction", Description: tools.DescribeTransaction},
		{Name: "annotate", Description: tools.DescribeAnnotate},
		{Name: "reverse", Description: tools.DescribeReverse},
		{Name: "history", Description: tools.DescribeHistory},
		{Name: "assert_balance", Description: tools.DescribeAssertBalance},
		{Name: "retract_assertion", Description: tools.DescribeRetractAssertion},
		{Name: "check", Description: tools.DescribeCheck},
		{Name: "add_note", Description: tools.DescribeAddNote},
		{Name: "retract_note", Description: tools.DescribeRetractNote},
		{Name: "add_document", Description: tools.DescribeAddDocument},
		{Name: "retract_document", Description: tools.DescribeRetractDocument},
		{Name: "journal", Description: tools.DescribeJournal},
		{Name: "register", Description: tools.DescribeRegister},
		{Name: "balances", Description: tools.DescribeBalances},
		{Name: "balance_sheet", Description: tools.DescribeBalanceSheet},
		{Name: "income_statement", Description: tools.DescribeIncomeStatement},
	}
	a, b := tools.Catalog(), tools.Catalog()
	if !reflect.DeepEqual(a, want) {
		t.Fatalf("catalog %+v", a)
	}
	a[0] = tools.Entry{}
	if !reflect.DeepEqual(b, want) || !reflect.DeepEqual(tools.Catalog(), want) {
		t.Fatal("catalog shares storage")
	}
	seen := map[string]bool{}
	for _, e := range want {
		s := tools.FirstLine(e.Description)
		if !utf8.ValidString(e.Description) || len([]rune(s)) < 1 || len([]rune(s)) > 120 || !strings.HasSuffix(s, ".") || strings.Contains(s, ". ") || strings.TrimSpace(s) != s || seen[s] {
			t.Errorf("description %q", e.Description)
		}
		seen[s] = true
	}
	for in, want := range map[string]string{"": "", "single": "single", "first\n\nrest": "first", "\nrest": ""} {
		if got := tools.FirstLine(in); got != want {
			t.Errorf("FirstLine(%q)=%q", in, got)
		}
	}
}
