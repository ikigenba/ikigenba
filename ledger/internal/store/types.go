// Package store keeps the business ledger.
package store

import (
	"io"
	"time"
)

// Config describes a ledger value.
type Config struct {
	Now  func() time.Time
	Rand io.Reader
}

// Amount describes a ledger value.
type Amount struct {
	Units     int64
	Commodity string
	Precision int
}

// Book describes a ledger value.
type Book struct {
	Currency                  string
	Precision                 int
	LockDate, FiscalYearStart string
	Updated                   time.Time
	UpdatedUserID             string
}

// Status describes a ledger value.
type Status struct {
	Book                       Book
	OpenAccounts, Transactions int
	LatestDate                 string
	FailingAssertions          int
}

// BookEdit describes a ledger value.
type BookEdit struct {
	LockDate, FiscalYearStart *string
	UserID                    string
}

// Account describes a ledger value.
type Account struct {
	Name, Open, Close string
	Opened            time.Time
	OpenedUserID      string
	Closed            time.Time
	ClosedUserID      string
}

// AccountInput describes a ledger value.
type AccountInput struct{ Name, Date, UserID string }

// AccountRead describes a ledger value.
type AccountRead struct {
	Account    Account
	Date       string
	Balance    Amount
	Postings   int
	Assertions []AssertionRow
	Notes      []Note
	Documents  []Document
}

// Posting describes a ledger value.
type Posting struct {
	Position int64
	Account  string
	Amount   Amount
}

// PostingInput describes a ledger value.
type PostingInput struct {
	Account string
	Amount  *string
}

// Annotation describes a ledger value.
type Annotation struct {
	Version                int64
	Flag, Payee, Narration string
	Tags, Links            []string
	Metadata               map[string]string
	Written                time.Time
	WrittenUserID          string
}

// Transaction describes a ledger value.
type Transaction struct {
	ID                              string
	Seq                             int64
	Date, Key, Reverses, ReversedBy string
	Posted                          time.Time
	PostedUserID                    string
	Postings                        []Posting
	Annotation                      Annotation
}

// PostInput describes a ledger value.
type PostInput struct {
	Date                   string
	Postings               []PostingInput
	Flag, Payee, Narration string
	Tags, Links            []string
	Metadata               map[string]string
	Key, UserID            string
}

// AnnotationEdit describes a ledger value.
type AnnotationEdit struct {
	Transaction            string
	Flag, Payee, Narration *string
	Tags, Links            *[]string
	Metadata               *map[string]string
	UserID                 string
}

// ReversalInput describes a ledger value.
type ReversalInput struct {
	Transaction, Date string
	Payee, Narration  *string
	Tags, Links       *[]string
	Metadata          *map[string]string
	UserID            string
}

// JournalFilter describes a ledger value.
type JournalFilter struct{ From, To, Account, Tag, Link, Payee, Flag string }

// RegisterRow describes a ledger value.
type RegisterRow struct {
	Transaction, Date string
	Seq, Position     int64
	Account           string
	Amount, Balance   Amount
	Payee, Narration  string
}

// Assertion describes a ledger value.
type Assertion struct {
	ID, Account, Date string
	Expected          Amount
	Added             time.Time
	AddedUserID       string
	Retracted         time.Time
	RetractedUserID   string
}

// AssertionRow describes a ledger value.
type AssertionRow struct {
	Assertion          Assertion
	Status             string
	Actual, Difference Amount
}

// AssertionInput describes a ledger value.
type AssertionInput struct{ Account, Date, Amount, UserID string }

// Retraction describes a ledger value.
type Retraction struct{ ID, UserID string }

// Note describes a ledger value.
type Note struct {
	ID, Account, Date, Text string
	Added                   time.Time
	AddedUserID             string
	Retracted               time.Time
	RetractedUserID         string
}

// NoteInput describes a ledger value.
type NoteInput struct{ Account, Date, Text, UserID string }

// Document describes a ledger value.
type Document struct {
	ID, Account, Transaction, Date, Path, Artifact string
	Added                                          time.Time
	AddedUserID                                    string
	Retracted                                      time.Time
	RetractedUserID                                string
}

// DocumentInput describes a ledger value.
type DocumentInput struct{ Account, Transaction, Date, Path, Artifact, UserID string }

// Line describes a ledger value.
type Line struct {
	Account string
	Amount  Amount
}

// Trial describes a ledger value.
type Trial struct {
	Date  string
	Lines []Line
	Total Amount
}

// BalanceSheet describes a ledger value.
type BalanceSheet struct {
	Date  string
	Lines []Line
}

// IncomeStatement describes a ledger value.
type IncomeStatement struct {
	From, To string
	Lines    []Line
	Net      Amount
}
