// Package tools exposes ledger through MCP.
package tools

import "encoding/json"

// BookArgs supplies tool input.
type BookArgs struct{}

// AccountsArgs supplies tool input.
type AccountsArgs struct{}

// SetBookArgs supplies tool input.
type SetBookArgs struct {
	LockDate        *string `json:"lock_date" description:"Last locked date as YYYY-MM-DD; empty clears the lock."`
	FiscalYearStart *string `json:"fiscal_year_start" description:"Fiscal year start as MM-DD."`
}

// AccountArgs supplies tool input.
type AccountArgs struct {
	Name string  `json:"name" mcp:"required" description:"Full account name."`
	Date *string `json:"date" description:"Balance date as YYYY-MM-DD; today when omitted."`
}

// AccountDateArgs supplies tool input.
type AccountDateArgs struct {
	Name string `json:"name" mcp:"required" description:"Full account name."`
	Date string `json:"date" mcp:"required" description:"Effective date as YYYY-MM-DD."`
}

// PostingArgs supplies tool input.
type PostingArgs struct {
	Account string  `json:"account" mcp:"required" description:"Full account name."`
	Amount  *string `json:"amount" description:"Decimal amount; omit one posting amount to fill the remainder."`
}

// PostArgs supplies tool input.
type PostArgs struct {
	Date      string          `json:"date" mcp:"required" description:"Effective date as YYYY-MM-DD."`
	Postings  []PostingArgs   `json:"postings" mcp:"required" description:"Ordered postings; at most one amount may be omitted."`
	Flag      *string         `json:"flag" description:"Transaction flag: pending or cleared."`
	Payee     *string         `json:"payee" description:"Transaction payee."`
	Narration *string         `json:"narration" description:"Transaction narration."`
	Tags      []string        `json:"tags" description:"Transaction tags."`
	Links     []string        `json:"links" description:"Transaction links."`
	Metadata  json.RawMessage `json:"metadata" description:"Object whose unique keys each have a string value."`
	Key       *string         `json:"key" description:"Idempotency key; a held key returns its original transaction."`
}

// TransactionArgs supplies tool input.
type TransactionArgs struct {
	Transaction string `json:"transaction" mcp:"required" description:"Transaction id."`
}

// AnnotateArgs supplies tool input.
type AnnotateArgs struct {
	Transaction string          `json:"transaction" mcp:"required" description:"Transaction id."`
	Flag        *string         `json:"flag" description:"Transaction flag: pending or cleared."`
	Payee       *string         `json:"payee" description:"Transaction payee."`
	Narration   *string         `json:"narration" description:"Transaction narration."`
	Tags        *[]string       `json:"tags" description:"Transaction tags."`
	Links       *[]string       `json:"links" description:"Transaction links."`
	Metadata    json.RawMessage `json:"metadata" description:"Object whose unique keys each have a string value."`
}

// ReverseArgs supplies tool input.
type ReverseArgs struct {
	Transaction string          `json:"transaction" mcp:"required" description:"Transaction id."`
	Date        *string         `json:"date" description:"Reversal date as YYYY-MM-DD; today when omitted."`
	Payee       *string         `json:"payee" description:"Transaction payee."`
	Narration   *string         `json:"narration" description:"Transaction narration."`
	Tags        *[]string       `json:"tags" description:"Transaction tags."`
	Links       *[]string       `json:"links" description:"Transaction links."`
	Metadata    json.RawMessage `json:"metadata" description:"Object whose unique keys each have a string value."`
}

// HistoryArgs supplies tool input.
type HistoryArgs struct {
	Transaction string  `json:"transaction" mcp:"required" description:"Transaction id."`
	Page        *string `json:"page" description:"Continues a previous answer: pass the next value it returned, with the same filters. Leave out for the first page."`
	Limit       *int    `json:"limit" description:"Most entries to return, 1 to 500; 25 when left out."`
}

// AssertBalanceArgs supplies tool input.
type AssertBalanceArgs struct {
	Account string `json:"account" mcp:"required" description:"Full account name."`
	Date    string `json:"date" mcp:"required" description:"Effective date as YYYY-MM-DD."`
	Amount  string `json:"amount" mcp:"required" description:"Decimal amount in the book currency."`
}

// AssertionArgs supplies tool input.
type AssertionArgs struct {
	Assertion string `json:"assertion" mcp:"required" description:"Assertion id."`
}

// CheckArgs supplies tool input.
type CheckArgs struct {
	Page  *string `json:"page" description:"Continues a previous answer: pass the next value it returned, with the same filters. Leave out for the first page."`
	Limit *int    `json:"limit" description:"Most entries to return, 1 to 500; 25 when left out."`
}

// AddNoteArgs supplies tool input.
type AddNoteArgs struct {
	Account string `json:"account" mcp:"required" description:"Full account name."`
	Date    string `json:"date" mcp:"required" description:"Effective date as YYYY-MM-DD."`
	Text    string `json:"text" mcp:"required" description:"Note text."`
}

// NoteArgs supplies tool input.
type NoteArgs struct {
	Note string `json:"note" mcp:"required" description:"Note id."`
}

// AddDocumentArgs supplies tool input.
type AddDocumentArgs struct {
	Account     string  `json:"account" mcp:"required" description:"Full account name."`
	Date        string  `json:"date" mcp:"required" description:"Effective date as YYYY-MM-DD."`
	Path        string  `json:"path" mcp:"required" description:"Artifact path."`
	Transaction *string `json:"transaction" description:"Transaction id."`
}

// DocumentArgs supplies tool input.
type DocumentArgs struct {
	Document string `json:"document" mcp:"required" description:"Document id."`
}

// JournalArgs supplies tool input.
type JournalArgs struct {
	From    *string `json:"from" description:"First included date as YYYY-MM-DD."`
	To      *string `json:"to" description:"Last included date as YYYY-MM-DD."`
	Account *string `json:"account" description:"Full account name."`
	Tag     *string `json:"tag" description:"Match this transaction tag."`
	Link    *string `json:"link" description:"Match this transaction link."`
	Payee   *string `json:"payee" description:"Transaction payee."`
	Flag    *string `json:"flag" description:"Transaction flag: pending or cleared."`
	Page    *string `json:"page" description:"Continues a previous answer: pass the next value it returned, with the same filters. Leave out for the first page."`
	Limit   *int    `json:"limit" description:"Most entries to return, 1 to 500; 25 when left out."`
}

// RegisterArgs supplies tool input.
type RegisterArgs struct {
	Account string  `json:"account" mcp:"required" description:"Full account name."`
	From    *string `json:"from" description:"First included date as YYYY-MM-DD."`
	To      *string `json:"to" description:"Last included date as YYYY-MM-DD."`
	Page    *string `json:"page" description:"Continues a previous answer: pass the next value it returned, with the same filters. Leave out for the first page."`
	Limit   *int    `json:"limit" description:"Most entries to return, 1 to 500; 25 when left out."`
}

// BalancesArgs supplies tool input.
type BalancesArgs struct {
	Date    *string `json:"date" description:"Balance date as YYYY-MM-DD; today when omitted."`
	Account *string `json:"account" description:"Full account name."`
}

// BalanceSheetArgs supplies tool input.
type BalanceSheetArgs struct {
	Date *string `json:"date" description:"Balance date as YYYY-MM-DD; today when omitted."`
}

// IncomeStatementArgs supplies tool input.
type IncomeStatementArgs struct {
	From string `json:"from" mcp:"required" description:"First included date as YYYY-MM-DD."`
	To   string `json:"to" mcp:"required" description:"Last included date as YYYY-MM-DD."`
}

// Book carries a tool answer.
type Book struct {
	Currency          string `json:"currency" mcp:"required"`
	Precision         int    `json:"precision" mcp:"required"`
	LockDate          string `json:"lock_date" mcp:"required"`
	FiscalYearStart   string `json:"fiscal_year_start" mcp:"required"`
	Updated           string `json:"updated" mcp:"required"`
	UpdatedBy         string `json:"updated_by" mcp:"required"`
	OpenAccounts      int    `json:"open_accounts" mcp:"required"`
	Transactions      int    `json:"transactions" mcp:"required"`
	FailingAssertions int    `json:"failing_assertions" mcp:"required"`
}

// BookChange carries a tool answer.
type BookChange struct {
	Book    Book `json:"book" mcp:"required"`
	Changed bool `json:"changed" mcp:"required"`
}

// Account carries a tool answer.
type Account struct {
	Name      string `json:"name" mcp:"required"`
	OpenDate  string `json:"open_date" mcp:"required"`
	CloseDate string `json:"close_date" mcp:"required"`
	Opened    string `json:"opened" mcp:"required"`
	OpenedBy  string `json:"opened_by" mcp:"required"`
	Closed    string `json:"closed" mcp:"required"`
	ClosedBy  string `json:"closed_by" mcp:"required"`
}

// AccountList carries a tool answer.
type AccountList struct {
	Accounts []Account `json:"accounts" mcp:"required"`
}

// AccountRead carries a tool answer.
type AccountRead struct {
	Account    Account     `json:"account" mcp:"required"`
	Date       string      `json:"date" mcp:"required"`
	Balance    string      `json:"balance" mcp:"required"`
	Commodity  string      `json:"commodity" mcp:"required"`
	Postings   int         `json:"postings" mcp:"required"`
	Assertions []Assertion `json:"assertions" mcp:"required"`
	Notes      []Note      `json:"notes" mcp:"required"`
	Documents  []Document  `json:"documents" mcp:"required"`
}

// AccountChange carries a tool answer.
type AccountChange struct {
	Account Account `json:"account" mcp:"required"`
	Changed bool    `json:"changed" mcp:"required"`
}

// Posting carries a tool answer.
type Posting struct {
	Account   string `json:"account" mcp:"required"`
	Amount    string `json:"amount" mcp:"required"`
	Commodity string `json:"commodity" mcp:"required"`
}

// Transaction carries a tool answer.
type Transaction struct {
	ID          string          `json:"id" mcp:"required"`
	Seq         int64           `json:"seq" mcp:"required"`
	Date        string          `json:"date" mcp:"required"`
	Key         string          `json:"key" mcp:"required"`
	Flag        string          `json:"flag" mcp:"required"`
	Payee       string          `json:"payee" mcp:"required"`
	Narration   string          `json:"narration" mcp:"required"`
	Tags        []string        `json:"tags" mcp:"required"`
	Links       []string        `json:"links" mcp:"required"`
	Metadata    json.RawMessage `json:"metadata" mcp:"required"`
	Version     int64           `json:"version" mcp:"required"`
	Reverses    string          `json:"reverses" mcp:"required"`
	ReversedBy  string          `json:"reversed_by" mcp:"required"`
	Posted      string          `json:"posted" mcp:"required"`
	PostedBy    string          `json:"posted_by" mcp:"required"`
	Annotated   string          `json:"annotated" mcp:"required"`
	AnnotatedBy string          `json:"annotated_by" mcp:"required"`
	Postings    []Posting       `json:"postings" mcp:"required"`
}

// TransactionChange carries a tool answer.
type TransactionChange struct {
	Transaction Transaction `json:"transaction" mcp:"required"`
	Changed     bool        `json:"changed" mcp:"required"`
}

// Version carries a tool answer.
type Version struct {
	Version   int64           `json:"version" mcp:"required"`
	Flag      string          `json:"flag" mcp:"required"`
	Payee     string          `json:"payee" mcp:"required"`
	Narration string          `json:"narration" mcp:"required"`
	Tags      []string        `json:"tags" mcp:"required"`
	Links     []string        `json:"links" mcp:"required"`
	Metadata  json.RawMessage `json:"metadata" mcp:"required"`
	Written   string          `json:"written" mcp:"required"`
	WrittenBy string          `json:"written_by" mcp:"required"`
}

// History carries a tool answer.
type History struct {
	Transaction string    `json:"transaction" mcp:"required"`
	Versions    []Version `json:"versions" mcp:"required"`
	Next        *string   `json:"next" description:"Present when more entries follow; pass it back as page to get them."`
}

// Assertion carries a tool answer.
type Assertion struct {
	ID          string `json:"id" mcp:"required"`
	Account     string `json:"account" mcp:"required"`
	Date        string `json:"date" mcp:"required"`
	Amount      string `json:"amount" mcp:"required"`
	Commodity   string `json:"commodity" mcp:"required"`
	Status      string `json:"status" mcp:"required"`
	Actual      string `json:"actual" mcp:"required"`
	Difference  string `json:"difference" mcp:"required"`
	Added       string `json:"added" mcp:"required"`
	AddedBy     string `json:"added_by" mcp:"required"`
	Retracted   string `json:"retracted" mcp:"required"`
	RetractedBy string `json:"retracted_by" mcp:"required"`
}

// AssertionChange carries a tool answer.
type AssertionChange struct {
	Assertion Assertion `json:"assertion" mcp:"required"`
	Changed   bool      `json:"changed" mcp:"required"`
}

// CheckList carries a tool answer.
type CheckList struct {
	Assertions []Assertion `json:"assertions" mcp:"required"`
	Next       *string     `json:"next" description:"Present when more entries follow; pass it back as page to get them."`
}

// Note carries a tool answer.
type Note struct {
	ID          string `json:"id" mcp:"required"`
	Account     string `json:"account" mcp:"required"`
	Date        string `json:"date" mcp:"required"`
	Text        string `json:"text" mcp:"required"`
	Added       string `json:"added" mcp:"required"`
	AddedBy     string `json:"added_by" mcp:"required"`
	Retracted   string `json:"retracted" mcp:"required"`
	RetractedBy string `json:"retracted_by" mcp:"required"`
}

// NoteChange carries a tool answer.
type NoteChange struct {
	Note    Note `json:"note" mcp:"required"`
	Changed bool `json:"changed" mcp:"required"`
}

// Document carries a tool answer.
type Document struct {
	ID          string `json:"id" mcp:"required"`
	Account     string `json:"account" mcp:"required"`
	Transaction string `json:"transaction" mcp:"required"`
	Date        string `json:"date" mcp:"required"`
	Path        string `json:"path" mcp:"required"`
	Artifact    string `json:"artifact" mcp:"required"`
	Added       string `json:"added" mcp:"required"`
	AddedBy     string `json:"added_by" mcp:"required"`
	Retracted   string `json:"retracted" mcp:"required"`
	RetractedBy string `json:"retracted_by" mcp:"required"`
}

// DocumentChange carries a tool answer.
type DocumentChange struct {
	Document Document `json:"document" mcp:"required"`
	Changed  bool     `json:"changed" mcp:"required"`
}

// Journal carries a tool answer.
type Journal struct {
	Transactions []Transaction `json:"transactions" mcp:"required"`
	Next         *string       `json:"next" description:"Present when more entries follow; pass it back as page to get them."`
}

// RegisterRow carries a tool answer.
type RegisterRow struct {
	Transaction string `json:"transaction" mcp:"required"`
	Date        string `json:"date" mcp:"required"`
	Position    int64  `json:"position" mcp:"required"`
	Account     string `json:"account" mcp:"required"`
	Amount      string `json:"amount" mcp:"required"`
	Commodity   string `json:"commodity" mcp:"required"`
	Balance     string `json:"balance" mcp:"required"`
	Payee       string `json:"payee" mcp:"required"`
	Narration   string `json:"narration" mcp:"required"`
}

// RegisterAnswer carries a tool answer.
type RegisterAnswer struct {
	Account string        `json:"account" mcp:"required"`
	Rows    []RegisterRow `json:"rows" mcp:"required"`
	Next    *string       `json:"next" description:"Present when more entries follow; pass it back as page to get them."`
}

// Line carries a tool answer.
type Line struct {
	Account   string `json:"account" mcp:"required"`
	Amount    string `json:"amount" mcp:"required"`
	Commodity string `json:"commodity" mcp:"required"`
}

// Balances carries a tool answer.
type Balances struct {
	Date      string `json:"date" mcp:"required"`
	Lines     []Line `json:"lines" mcp:"required"`
	Total     string `json:"total" mcp:"required"`
	Commodity string `json:"commodity" mcp:"required"`
}

// BalanceSheetAnswer carries a tool answer.
type BalanceSheetAnswer struct {
	Date  string `json:"date" mcp:"required"`
	Lines []Line `json:"lines" mcp:"required"`
}

// IncomeStatementAnswer carries a tool answer.
type IncomeStatementAnswer struct {
	From      string `json:"from" mcp:"required"`
	To        string `json:"to" mcp:"required"`
	Lines     []Line `json:"lines" mcp:"required"`
	Net       string `json:"net" mcp:"required"`
	Commodity string `json:"commodity" mcp:"required"`
}
