// Package pages renders candidates' embedded templates.
package pages

import (
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// ServiceName supplies service name page data.
const ServiceName = "candidates"

// Description supplies description page data.
const Description = "Candidate businesses resolved from the records scrapes collects, judged and read through the owner's cohorts"

// MinuteLayout supplies minute layout page data.
const MinuteLayout = "2006-01-02 15:04"

// SecondLayout supplies second layout page data.
const SecondLayout = "2006-01-02 15:04:05 UTC"

// DateLayout supplies date layout page data.
const DateLayout = "2006-01-02"

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
}

// LandingData supplies landing data page data.
type LandingData struct {
	Banner      page.Banner
	Cohorts     []CohortRow
	CohortsPage string
	CohortsPrev string
	CohortsNext string
	Passes      []PassRow
	PassesPage  string
	PassesPrev  string
	PassesNext  string
}

// CohortRow supplies a landing cohort row.
type CohortRow struct {
	ID, Name                                     string
	Size                                         int64
	CreatedBy, CreatedUser, Changed, ChangedText string
}

// CohortData supplies a cohort page.
type CohortData struct {
	Banner   page.Banner
	Cohort   CohortView
	Matching int64
	Members  []MemberRow
	Rejects  []RejectRow
}

// CohortView supplies the cohort definition.
type CohortView struct {
	ID, Name                                                                                   string
	Size                                                                                       int64
	Filters                                                                                    FiltersView
	Order                                                                                      []OrderRow
	CreatedBy, CreatedUser, Created, CreatedText, ChangedBy, ChangedUser, Changed, ChangedText string
}

// OrderRow supplies one ordering term.
type OrderRow struct{ Field, Direction string }

// FiltersView supplies formatted filter values.
type FiltersView struct {
	RevenueMin, RevenueMax string
	RevenueUnknown         bool
	MaxMinutes             string
	MinutesUnknown         bool
	Categories             []string
	CategoryUnknown        bool
	Fleet                  string
	FleetUnknown           bool
	Website                []string
	WebsiteUnknown         bool
	Government             string
	MaxFilingAgeDays       string
	FilingUnknown          bool
	Class                  string
	ClassUnknown           bool
	ShowExcluded           bool
}

// MemberRow supplies one cohort member.
type MemberRow struct {
	Rank                                                                                                                                                                     int64
	ID, Name, City, State, OperatingConfidence, RevenueLow, RevenueHigh, RevenueConfidence                                                                                   string
	Categories                                                                                                                                                               []string
	Class, FleetVerdict, Website, WebsiteURL, SiteText, SiteTextText, Brief, BriefText, Minutes, GovernmentMatch, KnownSince, KnownSinceText, NewestFiling, NewestFilingText string
	Excluded                                                                                                                                                                 bool
}

// RejectRow supplies a standing reject.
type RejectRow struct{ Candidate, By, User, At, AtText, Why string }

// RejectRecordRow supplies a candidate's reject history.
type RejectRecordRow struct{ Kind, Cohort, CohortName, By, User, At, AtText, Why string }

// PassRow supplies pass row page data.
type PassRow struct {
	ID, Kind, Trigger, Status, Reason, Started, StartedText, Finished, FinishedText string
	Rows, Created, Judged                                                           int64
}

// CandidateData supplies candidate data page data.
type CandidateData struct {
	Banner      page.Banner
	Candidate   CandidateView
	Facts       *FactsView
	Brief       *BriefView
	Assessment  *AssessmentView
	Estimates   []EstimateView
	Links       []LinkView
	Rulings     []RulingView
	Pages       []PageRow
	Enrichments []EnrichmentRow
	Rejects     []RejectRecordRow
}

// CandidateView supplies candidate view page data.
type CandidateView struct {
	ID, Status, MergedInto, Name                                                                                          string
	Names                                                                                                                 []string
	Street, City, State, Zip, County, Phone, Email, Website, WebsiteURL, WebsiteTitle, WebsiteChecked, WebsiteCheckedText string
	People                                                                                                                []PersonRow
	Class                                                                                                                 string
	Cargo                                                                                                                 []string
	Created, CreatedText, Updated, UpdatedText                                                                            string
}

// PersonRow supplies person row page data.
type PersonRow struct{ Name, Role string }

// AssessmentView supplies assessment view page data.
type AssessmentView struct {
	OperatingConfidence                        string
	OperatingEvidence                          []EvidenceRow
	RevenueLow, RevenueHigh, RevenueConfidence string
	Excluded                                   bool
	Rules                                      int64
	Calculated, CalculatedText                 string
}

// FactsView supplies the newest recorded facts.
type FactsView struct {
	KnownSince, KnownSinceText, NewestFiling, NewestFilingText  string
	Categories                                                  []CategoryRow
	GovernmentMatch, FleetVerdict, PowerUnits, Drivers, Minutes string
	Written                                                     []WrittenRow
}

// WrittenRow supplies a fact kind's recording time.
type WrittenRow struct{ Kind, At, AtText string }

// EvidenceRow supplies operating evidence page data.
type EvidenceRow struct{ Name, Source, Weight, Description string }

// CategoryRow supplies category page data.
type CategoryRow struct{ Name, Source string }

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
	ID, Kind, Origin, Trigger, RunPage, Status, Reason, StartedBy, Started, StartedText, Finished, FinishedText      string
	Rows, New, Changed, Unchanged, Gone, Created, Linked, Judged, Unranged, Derived, Due, Fetched, HTTPS, HTTP, None int64
	Target, TargetPage, EnrichKind                                                                                   string
	Runs, RunsFailed, Pages                                                                                          int64
	Cost                                                                                                             string
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

// EnrichmentRow supplies one recorded enrichment result.
type EnrichmentRow struct {
	Kind, Source                  string
	Found                         []FactRow
	State, URL, Title, StatusCode string
	Size                          int64
	Error                         string
	Failed                        bool
	Cost, At, AtText              string
}

// PageRow supplies a candidate's newest copy of one URL.
type PageRow struct {
	Number, Path, URL, Title       string
	Size                           int64
	Fetched, FetchedText, By, User string
}

// PageData supplies one kept page's text.
type PageData struct {
	Banner page.Banner
	Page   PageView
}

// PageView supplies a kept page's stored contents.
type PageView struct {
	Candidate, Name, Number, URL, Title  string
	Size                                 int64
	Fetched, FetchedText, By, User, Text string
}

// BriefView supplies the newest successful recorded business brief.
type BriefView struct {
	Summary    string
	Categories []BriefCategoryRow
	Labels     []string
	At, AtText string
}

// BriefCategoryRow supplies a brief category and its explanation.
type BriefCategoryRow struct{ Name, Why string }
