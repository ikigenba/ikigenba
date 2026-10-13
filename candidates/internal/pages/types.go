// Package pages renders candidates' embedded templates.
package pages

import (
	"time"

	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// ServiceName supplies service name page data.
const ServiceName = "candidates"

// Description supplies description page data.
const Description = "Candidate businesses resolved from the records scrapes collects, ranked against the fit test"

// MinuteLayout supplies minute layout page data.
const MinuteLayout = "2006-01-02 15:04"

// SecondLayout supplies second layout page data.
const SecondLayout = "2006-01-02 15:04:05 UTC"

// DateLayout supplies date layout page data.
const DateLayout = "2006-01-02"

// ShortlistShown supplies shortlist shown page data.
const ShortlistShown = 30

// ToolsLevel supplies tools level page data.
var ToolsLevel = page.Level{Name: "tools", URL: "/tools"}

// AboutLevel supplies about level page data.
var AboutLevel = page.Level{Name: "about", URL: "/about"}

// Config supplies the handler dependencies.
type Config struct {
	Banner       func(u page.User) page.Banner
	Pages        *Set
	ServicesPath string
	Store        *store.Store
	Now          func() time.Time
}

// LandingData supplies landing data page data.
type LandingData struct {
	Banner     page.Banner
	MaxMinutes int64
	MinRevenue string
	Shortlist  []ShortlistRow
	Passes     []PassRow
}

// ShortlistRow supplies shortlist row page data.
type ShortlistRow struct {
	Rank                                int64
	ID, Name, City, State               string
	Minutes                             int64
	Class, Low, High, Confidence, Score string
}

// PassRow supplies pass row page data.
type PassRow struct {
	ID, Trigger, Status, Reason, Started, StartedText, Finished, FinishedText string
	Rows, Created, Rescored                                                   int64
}

// CandidateData supplies candidate data page data.
type CandidateData struct {
	Banner     page.Banner
	Candidate  CandidateView
	Assessment *AssessmentView
	Estimates  []EstimateView
	Links      []LinkView
	Rulings    []RulingView
}

// CandidateView supplies candidate view page data.
type CandidateView struct {
	ID, Status, MergedInto, Name                   string
	Names                                          []string
	Street, City, State, Zip, County, Phone, Email string
	People                                         []PersonRow
	Class                                          string
	Cargo                                          []string
	Created, CreatedText, Updated, UpdatedText     string
}

// PersonRow supplies person row page data.
type PersonRow struct{ Name, Role string }

// AssessmentView supplies assessment view page data.
type AssessmentView struct {
	Fleet                                                                  bool
	Minutes, Low, High, Confidence, Filed, FiledText, Freshness, Effective string
	Stale                                                                  bool
	Point, Base, Score                                                     string
	Excluded                                                               bool
	Exclusions                                                             []ExclusionRow
	Rules                                                                  int64
	Scored, ScoredText                                                     string
}

// ExclusionRow supplies exclusion row page data.
type ExclusionRow struct{ Reason, Detail string }

// EstimateView supplies estimate view page data.
type EstimateView struct {
	Source, Low, High, Confidence, Filed, FiledText string
	Formula                                         int64
	Facts                                           []FactRow
}

// FactRow supplies fact row page data.
type FactRow struct{ Key, Value string }

// LinkView supplies link view page data.
type LinkView struct {
	Scraper, SourceID, Run, RowPage, Name, City, Zip, Reason, Confidence, Filed, FiledText, FirstSeen, FirstSeenText, LastSeen, LastSeenText string
	Gone                                                                                                                                     bool
}

// RulingView supplies ruling view page data.
type RulingView struct {
	Kind, Other               string
	Keys                      []string
	By, User, At, AtText, Why string
}

// PassData supplies pass data page data.
type PassData struct {
	Banner page.Banner
	Pass   PassView
}

// PassView supplies pass view page data.
type PassView struct {
	ID, Trigger, RunPage, Status, Reason, StartedBy, Started, StartedText, Finished, FinishedText string
	Rows, New, Changed, Unchanged, Gone, Created, Linked, Rescored, Unranged                      int64
}

// ToolsData supplies tools data page data.
type ToolsData struct {
	Banner page.Banner
	Tools  []ToolRow
}

// ToolRow supplies tool row page data.
type ToolRow struct{ Name, Description string }

// AboutData supplies about data page data.
type AboutData struct {
	Banner      page.Banner
	Description string
}

// NotFoundData supplies not found data page data.
type NotFoundData struct{ Banner page.Banner }
