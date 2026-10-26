package tools

import (
	"strings"

	"github.com/ikigenba/ikigenba/appkit/artifacts"
	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/ledger/internal/store"
)

// Config supplies dependencies for every tool.
type Config struct {
	Store     *store.Store
	Telemetry *telemetry.Writer
	Events    *events.Emitter
	Artifacts *artifacts.Client
}

// Entry describes one tool.
type Entry struct{ Name, Description string }

// Refusal categories are stable machine words.
const (
	RefusalNotFound    = "not_found"
	RefusalInvalid     = "invalid"
	RefusalUnavailable = "unavailable"
)

// Refusal sentences contain no caller-supplied values.
const (
	RefuseNotFound   string = "The record or account was not found."
	RefuseNoArtifact string = "The artifact was not found."
	RefuseInvalid    string = "A supplied value is invalid."
	RefuseUnbalanced string = "The postings must balance to zero."
	RefuseLocked     string = "The date must follow the book lock date."
	RefuseArtifacts  string = "Artifacts is unavailable."
)

// Tool descriptions are centralized copy.
const (
	DescribeBook             string = "Reads book settings and counts."
	DescribeSetBook          string = "Changes the lock date or fiscal year start."
	DescribeAccounts         string = "Lists the whole chart of accounts."
	DescribeAccount          string = "Reads an account and its balance, assertions, notes and documents."
	DescribeOpenAccount      string = "Opens an account."
	DescribeCloseAccount     string = "Closes an account."
	DescribePost             string = "Posts a balanced transaction."
	DescribeTransaction      string = "Reads a transaction with its current words and reversal pointers."
	DescribeAnnotate         string = "Changes the words of a transaction."
	DescribeReverse          string = "Posts a reversal of a transaction."
	DescribeHistory          string = "Lists versions of a transaction’s words, newest first."
	DescribeAssertBalance    string = "Adds a balance assertion."
	DescribeRetractAssertion string = "Retracts a balance assertion."
	DescribeCheck            string = "Lists failing balance assertions through today."
	DescribeAddNote          string = "Adds a dated note to an account."
	DescribeRetractNote      string = "Retracts a note."
	DescribeAddDocument      string = "Attaches an artifact document to an account."
	DescribeRetractDocument  string = "Retracts a document reference."
	DescribeJournal          string = "Lists transactions, newest first."
	DescribeRegister         string = "Lists account postings with running balances, oldest first."
	DescribeBalances         string = "Reads the whole trial balance."
	DescribeBalanceSheet     string = "Reads the balance sheet at a date."
	DescribeIncomeStatement  string = "Reads income and expenses over a date range."
)

// Catalog returns an independent slice in registration order.
func Catalog() []Entry {
	return []Entry{
		{"book", DescribeBook},
		{"set_book", DescribeSetBook},
		{"accounts", DescribeAccounts},
		{"account", DescribeAccount},
		{"open_account", DescribeOpenAccount},
		{"close_account", DescribeCloseAccount},
		{"post", DescribePost},
		{"transaction", DescribeTransaction},
		{"annotate", DescribeAnnotate},
		{"reverse", DescribeReverse},
		{"history", DescribeHistory},
		{"assert_balance", DescribeAssertBalance},
		{"retract_assertion", DescribeRetractAssertion},
		{"check", DescribeCheck},
		{"add_note", DescribeAddNote},
		{"retract_note", DescribeRetractNote},
		{"add_document", DescribeAddDocument},
		{"retract_document", DescribeRetractDocument},
		{"journal", DescribeJournal},
		{"register", DescribeRegister},
		{"balances", DescribeBalances},
		{"balance_sheet", DescribeBalanceSheet},
		{"income_statement", DescribeIncomeStatement},
	}
}

// FirstLine returns a description summary.
func FirstLine(description string) string { line, _, _ := strings.Cut(description, "\n"); return line }

// Register installs the tools in catalog order.
func Register(srv *mcp.Server, cfg Config) {
	h := handlers{cfg}
	mcp.AddTool(srv, mcp.Tool[BookArgs, Book]{Name: "book", Description: DescribeBook, Effect: mcp.Read, Handler: h.book})
	mcp.AddTool(srv, mcp.Tool[SetBookArgs, BookChange]{Name: "set_book", Description: DescribeSetBook, Effect: mcp.Additive, Handler: h.setBook})
	mcp.AddTool(srv, mcp.Tool[AccountsArgs, AccountList]{Name: "accounts", Description: DescribeAccounts, Effect: mcp.Read, Handler: h.accounts})
	mcp.AddTool(srv, mcp.Tool[AccountArgs, AccountRead]{Name: "account", Description: DescribeAccount, Effect: mcp.Read, Handler: h.account})
	mcp.AddTool(srv, mcp.Tool[AccountDateArgs, AccountChange]{Name: "open_account", Description: DescribeOpenAccount, Effect: mcp.Additive, Handler: h.openAccount})
	mcp.AddTool(srv, mcp.Tool[AccountDateArgs, AccountChange]{Name: "close_account", Description: DescribeCloseAccount, Effect: mcp.Additive, Handler: h.closeAccount})
	mcp.AddTool(srv, mcp.Tool[PostArgs, TransactionChange]{Name: "post", Description: DescribePost, Effect: mcp.Additive, Handler: h.post})
	mcp.AddTool(srv, mcp.Tool[TransactionArgs, Transaction]{Name: "transaction", Description: DescribeTransaction, Effect: mcp.Read, Handler: h.transaction})
	mcp.AddTool(srv, mcp.Tool[AnnotateArgs, TransactionChange]{Name: "annotate", Description: DescribeAnnotate, Effect: mcp.Additive, Handler: h.annotate})
	mcp.AddTool(srv, mcp.Tool[ReverseArgs, TransactionChange]{Name: "reverse", Description: DescribeReverse, Effect: mcp.Additive, Handler: h.reverse})
	mcp.AddTool(srv, mcp.Tool[HistoryArgs, History]{Name: "history", Description: DescribeHistory, Effect: mcp.Read, Handler: h.history})
	mcp.AddTool(srv, mcp.Tool[AssertBalanceArgs, Assertion]{Name: "assert_balance", Description: DescribeAssertBalance, Effect: mcp.Additive, Handler: h.assertBalance})
	mcp.AddTool(srv, mcp.Tool[AssertionArgs, AssertionChange]{Name: "retract_assertion", Description: DescribeRetractAssertion, Effect: mcp.Additive, Handler: h.retractAssertion})
	mcp.AddTool(srv, mcp.Tool[CheckArgs, CheckList]{Name: "check", Description: DescribeCheck, Effect: mcp.Read, Handler: h.check})
	mcp.AddTool(srv, mcp.Tool[AddNoteArgs, Note]{Name: "add_note", Description: DescribeAddNote, Effect: mcp.Additive, Handler: h.addNote})
	mcp.AddTool(srv, mcp.Tool[NoteArgs, NoteChange]{Name: "retract_note", Description: DescribeRetractNote, Effect: mcp.Additive, Handler: h.retractNote})
	mcp.AddTool(srv, mcp.Tool[AddDocumentArgs, Document]{Name: "add_document", Description: DescribeAddDocument, Effect: mcp.Additive, Handler: h.addDocument})
	mcp.AddTool(srv, mcp.Tool[DocumentArgs, DocumentChange]{Name: "retract_document", Description: DescribeRetractDocument, Effect: mcp.Additive, Handler: h.retractDocument})
	mcp.AddTool(srv, mcp.Tool[JournalArgs, Journal]{Name: "journal", Description: DescribeJournal, Effect: mcp.Read, Handler: h.journal})
	mcp.AddTool(srv, mcp.Tool[RegisterArgs, RegisterAnswer]{Name: "register", Description: DescribeRegister, Effect: mcp.Read, Handler: h.register})
	mcp.AddTool(srv, mcp.Tool[BalancesArgs, Balances]{Name: "balances", Description: DescribeBalances, Effect: mcp.Read, Handler: h.balances})
	mcp.AddTool(srv, mcp.Tool[BalanceSheetArgs, BalanceSheetAnswer]{Name: "balance_sheet", Description: DescribeBalanceSheet, Effect: mcp.Read, Handler: h.balanceSheet})
	mcp.AddTool(srv, mcp.Tool[IncomeStatementArgs, IncomeStatementAnswer]{Name: "income_statement", Description: DescribeIncomeStatement, Effect: mcp.Read, Handler: h.incomeStatement})
}

type handlers struct{ Config }
