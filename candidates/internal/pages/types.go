// Package pages renders candidates' embedded templates.
package pages

import (
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// ServiceName supplies service name page data.
const ServiceName = "candidates"

// Description supplies description page data.
const Description string = "Candidate businesses ingested from public records, enriched and judged, and read through the owner's prospect lists"

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

// ParksLevel supplies the parks breadcrumb.
var ParksLevel = page.Level{Name: "parks", URL: "/parks"}

// Config supplies the handler dependencies.
type Config struct {
	Banner       func(u page.User) page.Banner
	Pages        *Set
	ServicesPath string
	Store        *store.Store
}

// LandingData supplies landing data page data.
type LandingData struct {
	Banner            page.Banner
	ProspectLists     []ProspectListRow
	ProspectListsPage string
	ProspectListsPrev string
	ProspectListsNext string
	Passes            []PassRow
	PassesPage        string
	PassesPrev        string
	PassesNext        string
	Parks             LandingParks
}

// ProspectListRow supplies a landing prospectList row.
type ProspectListRow struct {
	ID, Name                                     string
	Size                                         int64
	CreatedBy, CreatedUser, Changed, ChangedText string
}

// ProspectListData supplies a prospectList page.
type ProspectListData struct {
	Banner               page.Banner
	ProspectList         ProspectListView
	Matching, Classified int64
	ByBasis              BasisCountsView
	Changes              ChangesView
	Members              []MemberRow
	Rejects              []RejectRow
}

// ProspectListView supplies the prospectList definition.
type ProspectListView struct {
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
	Trend                  []string
	TrendUnknown           bool
	Government             string
	MaxFilingAgeDays       string
	FilingUnknown          bool
	MinCampaignAgeDays     string
	Class                  string
	ClassUnknown           bool
	Basis                  []string
	ShowExcluded           bool
}

// MemberRow supplies one prospectList member.
type MemberRow struct {
	Rank                                                                                                                                                                                                                 int64
	ID, Name, City, State, Basis, OperatingConfidence, RevenueLow, RevenueHigh, RevenueConfidence, HeadcountLow, Trend, TrendChange                                                                                      string
	Categories                                                                                                                                                                                                           []string
	ClassifiedBy, Class, FleetVerdict, Website, WebsiteURL, WebsiteSource, SiteText, SiteTextText, Brief, BriefText, Minutes, MinutesSource, GovernmentMatch, KnownSince, KnownSinceText, NewestFiling, NewestFilingText string
	Excluded                                                                                                                                                                                                             bool
}

// RejectRow supplies a standing reject.
type RejectRow struct{ Candidate, By, User, At, AtText, Why string }

// RejectRecordRow supplies a candidate's reject history.
type RejectRecordRow struct{ Kind, ProspectList, ProspectListName, By, User, At, AtText, Why string }

// PassRow supplies pass row page data.
type PassRow struct {
	ID, Kind, Trigger, Status, Reason, Started, StartedText, Finished, FinishedText string
	Rows, Created, Judged                                                           int64
}

// CandidateData supplies candidate data page data.
type CandidateData struct {
	Banner            page.Banner
	Candidate         CandidateView
	Enrichments       *EnrichmentsView
	Judgments         *JudgmentsView
	Estimates         []EstimateView
	Links             []LinkView
	Rulings           []RulingView
	Pages             []PageRow
	EnrichmentResults []EnrichmentRow
	Rejects           []RejectRecordRow
	Campaigns         []CampaignRow
}

// CandidateView supplies candidate view page data.
type CandidateView struct {
	ID, Status, MergedInto, Name                                                                     string
	Names                                                                                            []string
	Street, City, State, Zip, County, Phone, Email                                                   string
	Park                                                                                             *ParkLink
	WebsiteSource, Website, WebsiteURL, WebsiteTitle, WebsiteWhy, WebsiteChecked, WebsiteCheckedText string
	People                                                                                           []PersonRow
	Class                                                                                            string
	Cargo                                                                                            []string
	Created, CreatedText, Updated, UpdatedText                                                       string
}

// PersonRow supplies person row page data.
type PersonRow struct{ Name, Role string }

// JudgmentsView supplies judgments view page data.
type JudgmentsView struct {
	OperatingConfidence                                                                     string
	OperatingEvidence                                                                       []EvidenceRow
	RevenueLow, RevenueHigh, RevenueConfidence, HeadcountLow, HeadcountHigh, HeadcountBasis string
	HeadcountYears                                                                          []string
	HeadcountSeries                                                                         []SeriesRow
	Trend, TrendKind, TrendFromYear, TrendToYear, TrendFrom, TrendTo, TrendChange           string
	Excluded                                                                                bool
	Rules                                                                                   int64
	Calculated, CalculatedText                                                              string
}

// FactsView supplies the newest recorded facts.
type FactsView struct {
	KnownSince, KnownSinceText, NewestFiling, NewestFilingText                     string
	Categories                                                                     []CategoryRow
	ClassifiedBy                                                                   string
	RecordsDiffer                                                                  bool
	RecordCategories                                                               []CategoryRow
	GovernmentMatch, FleetVerdict, PowerUnits, Drivers                             string
	Headcounts                                                                     []HeadcountRow
	BriefLabels                                                                    []string
	Minutes, MinutesSource, Miles, RouteAddress, RoutePlaceURL, RouteDirectionsURL string
	Written                                                                        []WrittenRow
}

// HeadcountRow supplies one dated source headcount.
type HeadcountRow struct{ Scraper, Kind, Count, Hours, AsOf, AsOfText string }

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
	Scraper, SourceID, Run, RowPage, EIN, Name                                                                                               string
	Names                                                                                                                                    []string
	Establishment, City, Zip, Year, Employees, Hours, Reason, Confidence, Filed, FiledText, FirstSeen, FirstSeenText, LastSeen, LastSeenText string
	Gone                                                                                                                                     bool
	Walk                                                                                                                                     string
	Park                                                                                                                                     *ParkLink
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
	ParcelsRead, ByLabel, ByValue, ByCandidate, ParksCreated, ParksUpdated, ParksGone, ParksGrown                    int64
	Circles, Requests, Found, NewCandidates                                                                          int64
	CeilingReached                                                                                                   bool
	Parks                                                                                                            []WalkParkRow
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
	Run                           *RunView
	Answer                        *AnswerView
	Confirmation                  *ConfirmationView
	Reason, Website, WebsiteFrom  string
	SiteText                      *SearchSiteTextView
	Case                          string
	Route                         *RouteView
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

// RunView supplies the website search run.
type RunView struct{ ID, Status, ExitCode string }

// AnswerView supplies the agent's website answer.
type AnswerView struct {
	URL, Why string
	Rejected []RejectedRow
}

// RejectedRow supplies a site set aside by the search.
type RejectedRow struct{ URL, Why string }

// ConfirmationView supplies the fetch that checked a search answer.
type ConfirmationView struct {
	State, URL, Title, StatusCode string
	Size                          int64
	Error                         string
}

// CampaignRow supplies the candidate's recorded campaign history.
type CampaignRow struct {
	Campaign, CampaignPage, ProspectList, ProspectListName, ProspectListPage, Added, AddedText string
	Rejected                                                                                   bool
	RejectedAt, RejectedAtText, Reason, Delivered, DeliveredText                               string
}

// EnrichmentsView holds the facts and brief.
type EnrichmentsView struct {
	Basis string
	Kinds KindsView
	Facts *FactsView
	Brief *BriefView
}

// SearchSiteTextView supplies the site text recorded when a search ran.
type SearchSiteTextView struct {
	Status string
	Pages  int64
}

// RouteView supplies the routing service result.
type RouteView struct{ Minutes, Miles, Address, Precision, Status, Why, Error, PlaceURL, DirectionsURL string }

// KindsView separates recorded targeted kinds from absent kinds.
type KindsView struct{ Present, Missing []string }

// BasisCountsView counts members at each enrichment basis.
type BasisCountsView struct{ Records, Partial, Enriched int64 }

// ChangesView presents differences from the previous recorded open.
type ChangesView struct {
	Since, SinceText  string
	DefinitionChanged []string
	Joined            []JoinedRow
	Left              []LeftRow
}

// JoinedRow presents a member that joined the list.
type JoinedRow struct{ Candidate, Name, Basis, Why string }

// LeftRow presents a member that left the list.
type LeftRow struct{ Candidate, Name, BasisThen, BasisNow, Why, Filter string }

// SeriesRow supplies a headcount series aligned to the displayed years.
type SeriesRow struct {
	Kind, Source string
	Figures      []FigureCell
}

// FigureCell supplies a count for one year, empty when absent.
type FigureCell struct{ Year, Count string }

// LandingParks supplies the active park count.
type LandingParks struct{ Count int64 }

// ParksData supplies one window of parks and its view.
type ParksData struct {
	Banner                                        page.Banner
	Summary                                       ParksSummary
	View                                          ParksView
	Parks                                         []ParkRow
	Page, FirstHref, PrevHref, NextHref, LastHref string
	Rules                                         RulesView
}

// ParksSummary supplies totals and the last derivation run.
type ParksSummary struct {
	Count, Gone  int64
	Run, RunPage string
}

// ParkRow supplies a park in the listing.
type ParkRow struct {
	ID, Name, County, State                    string
	Parcels                                    int64
	BuildingValue, Minutes, Walked, WalkedText string
	GrownFrom                                  int64
}

// ParksView supplies the filter and ordering choices.
type ParksView struct {
	Counties            []CountyChoice
	Walked, Gone, Sorts []ViewChoice
}

// CountyChoice supplies one county link.
type CountyChoice struct {
	County, Href string
	Current      bool
}

// ViewChoice supplies one filter or sort link.
type ViewChoice struct {
	Value, Href string
	Current     bool
}

// RulesView supplies formatted derivation rules.
type RulesView struct {
	Distance       string
	MinParcels     int64
	ValueThreshold string
	WalkCeiling    int64
	Counties       []CountyLabels
}

// CountyLabels supplies a county's industrial labels.
type CountyLabels struct {
	County string
	Labels []string
}

// ParkData supplies one park and its parcels.
type ParkData struct {
	Banner          page.Banner
	Park            ParkView
	Walks           []WalkRow
	Places          []PlaceRow
	FoundCandidates []FoundCandidateRow
	Parcels         []ParcelRow
}

// ParkView supplies formatted park detail.
type ParkView struct {
	ID, Name, County, Town, State, Lat, Lon, MapURL, Radius  string
	ParcelCount                                              int64
	BuildingValue, Minutes                                   string
	GrownFrom                                                int64
	Walked, WalkedText, Walk                                 string
	Found, NewCandidates                                     int64
	CeilingReached                                           bool
	Run, RunPage, Created, CreatedText, Updated, UpdatedText string
}

// ParcelRow supplies one parcel of a park.
type ParcelRow struct {
	PIN                                    string
	Owners                                 []string
	Street, City, Zip, BuildingValue, Rule string
}

// WalkRow supplies one recorded walk of a park.
type WalkRow struct {
	Pass, Walked, WalkedText       string
	Found, NewCandidates, Requests int64
	Cost                           string
	CeilingReached                 bool
}

// WalkParkRow supplies one park in a walk pass.
type WalkParkRow struct {
	ID, Name                       string
	Found, NewCandidates, Requests int64
	Cost                           string
	CeilingReached                 bool
}

// PlaceRow supplies one place returned by a walk.
type PlaceRow struct {
	PlaceID, Name, Address, PrimaryType, Website, Phone, Walk, Candidate string
	New                                                                  bool
}

// FoundCandidateRow supplies a candidate found in a park.
type FoundCandidateRow struct {
	ID, Name, Status string
	New              bool
}

// ParkLink links a candidate or places link to its park.
type ParkLink struct{ ID, Name string }
