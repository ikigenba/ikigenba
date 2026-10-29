// Package tools exposes the candidate funnel through MCP.
package tools

import (
	"encoding/json"

	"github.com/ikigenba/ikigenba/candidates/internal/passes"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// Person is a tool answer value.
type Person struct {
	Name   string `json:"name"`
	Role   string `json:"role"`
	Source string `json:"source,omitempty"`
}

// Sourced identifies the source of one alternative value.
type Sourced struct {
	Value  string `json:"value"`
	Source string `json:"source"`
}

// SourcedAddress identifies the source of one alternative postal address.
type SourcedAddress struct {
	Street string `json:"street"`
	City   string `json:"city"`
	State  string `json:"state"`
	Zip    string `json:"zip"`
	County string `json:"county"`
	Source string `json:"source"`
}

// Sources records the provenance and alternatives of a candidate's display facts.
type Sources struct {
	Name           string           `json:"name"`
	Address        string           `json:"address"`
	Phone          string           `json:"phone"`
	Email          string           `json:"email"`
	OtherNames     []Sourced        `json:"other_names"`
	OtherAddresses []SourcedAddress `json:"other_addresses"`
	OtherPhones    []Sourced        `json:"other_phones"`
	OtherEmails    []Sourced        `json:"other_emails"`
}

// MarshalJSON encodes missing alternatives as empty arrays.
func (v Sources) MarshalJSON() ([]byte, error) {
	type plain Sources
	if v.OtherNames == nil {
		v.OtherNames = []Sourced{}
	}
	if v.OtherAddresses == nil {
		v.OtherAddresses = []SourcedAddress{}
	}
	if v.OtherPhones == nil {
		v.OtherPhones = []Sourced{}
	}
	if v.OtherEmails == nil {
		v.OtherEmails = []Sourced{}
	}
	return json.Marshal(plain(v))
}

// Candidate is a tool answer value.
type Candidate struct {
	ID         string   `json:"id"`
	Status     string   `json:"status"`
	MergedInto string   `json:"merged_into,omitempty"`
	Name       string   `json:"name"`
	Names      []string `json:"names"`
	Street     string   `json:"street"`
	City       string   `json:"city"`
	State      string   `json:"state"`
	Zip        string   `json:"zip"`
	County     string   `json:"county"`
	Phone      string   `json:"phone"`
	Email      string   `json:"email"`
	People     []Person `json:"people"`
	Class      string   `json:"class"`
	Cargo      []string `json:"cargo"`
	Created    string   `json:"created"`
	Updated    string   `json:"updated"`
}

// MarshalJSON encodes empty collections as arrays or objects.
func (v Candidate) MarshalJSON() ([]byte, error) {
	type plain Candidate
	if v.Names == nil {
		v.Names = []string{}
	}
	if v.People == nil {
		v.People = []Person{}
	}
	if v.Cargo == nil {
		v.Cargo = []string{}
	}
	return json.Marshal(plain(v))
}

// Member is a prospectList member's stored judgment.
type Member struct {
	Candidate           Candidate `json:"candidate"`
	Basis               string    `json:"basis"`
	OperatingConfidence *float64  `json:"operating_confidence,omitempty"`
	RevenueLow          *int64    `json:"revenue_low,omitempty"`
	RevenueHigh         *int64    `json:"revenue_high,omitempty"`
	RevenueConfidence   *float64  `json:"revenue_confidence,omitempty"`
	HeadcountLow        *int64    `json:"headcount_low,omitempty"`
	Trend               string    `json:"trend"`
	TrendChange         *int64    `json:"trend_change,omitempty"`
	Categories          []string  `json:"categories"`
	ClassifiedBy        string    `json:"classified_by"`
	FleetVerdict        string    `json:"fleet_verdict"`
	Website             string    `json:"website"`
	WebsiteURL          string    `json:"website_url,omitempty"`
	WebsiteSource       string    `json:"website_source"`
	SiteText            *string   `json:"site_text"`
	Brief               *string   `json:"brief"`
	Minutes             *int64    `json:"minutes,omitempty"`
	MinutesSource       string    `json:"minutes_source"`
	GovernmentMatch     string    `json:"government_match,omitempty"`
	KnownSince          string    `json:"known_since,omitempty"`
	NewestFiling        string    `json:"newest_filing,omitempty"`
	Excluded            bool      `json:"excluded"`
}

// MarshalJSON encodes empty categories as an array.
func (v Member) MarshalJSON() ([]byte, error) {
	type plain Member
	if v.Categories == nil {
		v.Categories = []string{}
	}
	return json.Marshal(plain(v))
}

// ProspectList is a saved search definition.
type ProspectList struct {
	ID           string                    `json:"id"`
	Name         string                    `json:"name"`
	Size         int64                     `json:"size"`
	Filters      store.ProspectListFilters `json:"filters"`
	Order        []store.ProspectListOrder `json:"order"`
	CreatedBy    string                    `json:"created_by"`
	CreatedEmail string                    `json:"created_email"`
	Created      string                    `json:"created"`
	ChangedBy    string                    `json:"changed_by"`
	ChangedEmail string                    `json:"changed_email"`
	Changed      string                    `json:"changed"`
}

// MarshalJSON encodes empty order as an array.
func (v ProspectList) MarshalJSON() ([]byte, error) {
	type plain ProspectList
	if v.Order == nil {
		v.Order = []store.ProspectListOrder{}
	}
	return json.Marshal(plain(v))
}

// ProspectListList is a page of definitions.
type ProspectListList struct {
	ProspectLists []ProspectList `json:"prospect_lists"`
	Next          *string        `json:"next,omitempty" description:"Present when more entries follow; pass it back as page to get them."`
}

// MarshalJSON encodes empty prospectLists as an array.
func (v ProspectListList) MarshalJSON() ([]byte, error) {
	type plain ProspectListList
	if v.ProspectLists == nil {
		v.ProspectLists = []ProspectList{}
	}
	return json.Marshal(plain(v))
}

// Reject is an owner's prospectList ruling.
type Reject struct {
	Kind             string `json:"kind"`
	ProspectList     string `json:"prospect_list"`
	ProspectListName string `json:"prospect_list_name,omitempty"`
	Candidate        string `json:"candidate"`
	Why              string `json:"why,omitempty"`
	User             string `json:"user,omitempty"`
	Email            string `json:"email,omitempty"`
	At               string `json:"at,omitempty"`
}

// ProspectListRead answers a prospectList and its current members.
type ProspectListRead struct {
	ProspectList ProspectList `json:"prospect_list"`
	Matching     int64        `json:"matching"`
	Classified   int64        `json:"classified"`
	ByBasis      BasisCounts  `json:"by_basis"`
	Changes      Changes      `json:"changes"`
	Members      []Member     `json:"members"`
	Rejects      []Reject     `json:"rejects"`
}

// MarshalJSON encodes empty members and rejects as arrays.
func (v ProspectListRead) MarshalJSON() ([]byte, error) {
	type plain ProspectListRead
	if v.Members == nil {
		v.Members = []Member{}
	}
	if v.Rejects == nil {
		v.Rejects = []Reject{}
	}
	return json.Marshal(plain(v))
}

// CandidateList is a tool answer value.
type CandidateList struct {
	Candidates []Candidate `json:"candidates"`
	Next       *string     `json:"next,omitempty" description:"Present when more entries follow; pass it back as page to get them."`
}

// MarshalJSON encodes empty collections as arrays or objects.
func (v CandidateList) MarshalJSON() ([]byte, error) {
	type plain CandidateList
	if v.Candidates == nil {
		v.Candidates = []Candidate{}
	}
	return json.Marshal(plain(v))
}

// PassList is a tool answer value.
type PassList struct {
	Passes []passes.Record `json:"passes"`
	Next   *string         `json:"next,omitempty" description:"Present when more entries follow; pass it back as page to get them."`
}

// MarshalJSON encodes empty collections as arrays or objects.
func (v PassList) MarshalJSON() ([]byte, error) {
	type plain PassList
	if v.Passes == nil {
		v.Passes = []passes.Record{}
	}
	return json.Marshal(plain(v))
}

// Link is a tool answer value.
type Link struct {
	Scraper       string   `json:"scraper"`
	SourceID      string   `json:"source_id"`
	Run           string   `json:"run"`
	Digest        string   `json:"digest"`
	Name          string   `json:"name"`
	Names         []string `json:"names"`
	Street        string   `json:"street"`
	City          string   `json:"city"`
	State         string   `json:"state"`
	Zip           string   `json:"zip"`
	County        string   `json:"county"`
	Phone         string   `json:"phone"`
	Email         string   `json:"email"`
	People        []Person `json:"people"`
	Class         string   `json:"class"`
	Cargo         []string `json:"cargo"`
	Filed         string   `json:"filed,omitempty"`
	FleetVerdict  string   `json:"fleet_verdict"`
	NameKey       string   `json:"name_key"`
	PhoneKey      string   `json:"phone_key"`
	DomainKey     string   `json:"domain_key"`
	EIN           string   `json:"ein"`
	Establishment string   `json:"establishment"`
	Year          *int64   `json:"year,omitempty"`
	Employees     *int64   `json:"employees,omitempty"`
	Hours         *int64   `json:"hours,omitempty"`
	Reason        string   `json:"reason"`
	Confidence    float64  `json:"confidence"`
	FirstSeen     string   `json:"first_seen"`
	LastSeen      string   `json:"last_seen"`
	Gone          bool     `json:"gone"`
	Walk          string   `json:"walk"`
	Park          string   `json:"park"`
}

// MarshalJSON encodes empty collections as arrays or objects.
func (v Link) MarshalJSON() ([]byte, error) {
	type plain Link
	if v.Names == nil {
		v.Names = []string{}
	}
	if v.People == nil {
		v.People = []Person{}
	}
	if v.Cargo == nil {
		v.Cargo = []string{}
	}
	return json.Marshal(plain(v))
}

// Estimate is a tool answer value.
type Estimate struct {
	Source     string                     `json:"source"`
	Low        int64                      `json:"low"`
	High       *int64                     `json:"high,omitempty"`
	Confidence float64                    `json:"confidence"`
	Facts      map[string]json.RawMessage `json:"facts"`
	Filed      string                     `json:"filed,omitempty"`
	Formula    int64                      `json:"formula"`
}

// MarshalJSON encodes empty collections as arrays or objects.
func (v Estimate) MarshalJSON() ([]byte, error) {
	type plain Estimate
	if v.Facts == nil {
		v.Facts = map[string]json.RawMessage{}
	}
	return json.Marshal(plain(v))
}

// Evidence is an operating judgment input.
type Evidence struct {
	Name        string  `json:"name"`
	Source      string  `json:"source"`
	Weight      float64 `json:"weight"`
	Description string  `json:"description"`
}

// Category names the source of a business category.
type Category struct {
	Name   string `json:"name"`
	Source string `json:"source"`
}

// Judgments contains the judgments from the last judge.
type Judgments struct {
	OperatingConfidence *float64   `json:"operating_confidence,omitempty"`
	OperatingEvidence   []Evidence `json:"operating_evidence"`
	RevenueLow          *int64     `json:"revenue_low,omitempty"`
	RevenueHigh         *int64     `json:"revenue_high,omitempty"`
	RevenueConfidence   *float64   `json:"revenue_confidence,omitempty"`
	HeadcountLow        *int64     `json:"headcount_low,omitempty"`
	HeadcountHigh       *int64     `json:"headcount_high,omitempty"`
	HeadcountBasis      string     `json:"headcount_basis,omitempty"`
	HeadcountSeries     []Series   `json:"headcount_series"`
	Trend               string     `json:"trend"`
	TrendKind           string     `json:"trend_kind,omitempty"`
	TrendFromYear       *int64     `json:"trend_from_year,omitempty"`
	TrendToYear         *int64     `json:"trend_to_year,omitempty"`
	TrendFrom           *int64     `json:"trend_from,omitempty"`
	TrendTo             *int64     `json:"trend_to,omitempty"`
	TrendChange         *int64     `json:"trend_change,omitempty"`
	Excluded            bool       `json:"excluded"`
	Rules               int64      `json:"rules"`
	Calculated          string     `json:"calculated"`
}

// MarshalJSON encodes empty slices as arrays.
func (v Judgments) MarshalJSON() ([]byte, error) {
	type plain Judgments
	if v.HeadcountSeries == nil {
		v.HeadcountSeries = []Series{}
	}
	if v.OperatingEvidence == nil {
		v.OperatingEvidence = []Evidence{}
	}
	return json.Marshal(plain(v))
}

// Series holds the figures for one headcount kind.
type Series struct {
	Kind    string   `json:"kind"`
	Scraper string   `json:"scraper"`
	Figures []Figure `json:"figures"`
}

// MarshalJSON encodes missing figures as an empty array.
func (v Series) MarshalJSON() ([]byte, error) {
	type plain Series
	if v.Figures == nil {
		v.Figures = []Figure{}
	}
	return json.Marshal(plain(v))
}

// Figure is a headcount for one calendar year.
type Figure struct {
	Year  int64 `json:"year"`
	Count int64 `json:"count"`
}

// Facts contains the newest recorded facts and their provenance.
type Facts struct {
	KnownSince         string      `json:"known_since,omitempty"`
	KnownSinceSource   string      `json:"known_since_source,omitempty"`
	NewestFiling       string      `json:"newest_filing,omitempty"`
	NewestFilingSource string      `json:"newest_filing_source,omitempty"`
	Categories         []Category  `json:"categories"`
	ClassifiedBy       string      `json:"classified_by"`
	RecordsDiffer      bool        `json:"records_differ"`
	RecordCategories   []Category  `json:"record_categories"`
	GovernmentMatch    string      `json:"government_match,omitempty"`
	FleetVerdict       string      `json:"fleet_verdict"`
	PowerUnits         *int64      `json:"power_units,omitempty"`
	Drivers            *int64      `json:"drivers,omitempty"`
	Website            string      `json:"website"`
	WebsiteURL         string      `json:"website_url,omitempty"`
	WebsiteSource      string      `json:"website_source"`
	WebsiteCheckedURL  string      `json:"website_checked_url,omitempty"`
	Minutes            *int64      `json:"minutes,omitempty"`
	MinutesSource      string      `json:"minutes_source"`
	Miles              *float64    `json:"miles,omitempty"`
	RouteAddress       string      `json:"route_address,omitempty"`
	PlaceURL           string      `json:"place_url,omitempty"`
	DirectionsURL      string      `json:"directions_url,omitempty"`
	Written            []Written   `json:"written"`
	Headcounts         []Headcount `json:"headcounts"`
	BriefLabels        []string    `json:"brief_labels"`
}

// MarshalJSON encodes empty slices as arrays.
func (v Facts) MarshalJSON() ([]byte, error) {
	type plain Facts
	if v.Headcounts == nil {
		v.Headcounts = []Headcount{}
	}
	if v.BriefLabels == nil {
		v.BriefLabels = []string{}
	}
	if v.Categories == nil {
		v.Categories = []Category{}
	}
	if v.RecordCategories == nil {
		v.RecordCategories = []Category{}
	}
	if v.Written == nil {
		v.Written = []Written{}
	}
	return json.Marshal(plain(v))
}

// Headcount is one source headcount.
type Headcount struct {
	Scraper string `json:"scraper"`
	Kind    string `json:"kind"`
	Count   int64  `json:"count"`
	Date    string `json:"date,omitempty"`
	Hours   *int64 `json:"hours,omitempty"`
}

// Written records when a kind last wrote the facts.
type Written struct {
	Kind string `json:"kind"`
	At   string `json:"at"`
}

// Brief contains the newest successful business brief.
type Brief struct {
	Summary    string          `json:"summary"`
	Categories []BriefCategory `json:"categories"`
	Labels     []string        `json:"labels"`
	At         string          `json:"at"`
}

// BriefCategory contains a category and the reason it was assigned.
type BriefCategory struct {
	Name string `json:"name"`
	Why  string `json:"why"`
}

// MarshalJSON encodes empty slices as arrays.
func (v Brief) MarshalJSON() ([]byte, error) {
	type plain Brief
	if v.Categories == nil {
		v.Categories = []BriefCategory{}
	}
	if v.Labels == nil {
		v.Labels = []string{}
	}
	return json.Marshal(plain(v))
}

// BasisCounts counts the returned members at each enrichment basis.
type BasisCounts struct {
	Records  int64 `json:"records"`
	Partial  int64 `json:"partial"`
	Enriched int64 `json:"enriched"`
}

// Changes describes membership changes since the previous open.
type Changes struct {
	Since             *string  `json:"since"`
	DefinitionChanged []string `json:"definition_changed"`
	Joined            []Joined `json:"joined"`
	Left              []Left   `json:"left"`
}

// Joined describes a newly returned member.
type Joined struct {
	Candidate string `json:"candidate"`
	Name      string `json:"name"`
	Basis     string `json:"basis"`
	Why       string `json:"why"`
}

// Left describes a previously returned member.
type Left struct {
	Candidate string `json:"candidate"`
	Name      string `json:"name"`
	BasisThen string `json:"basis_then"`
	BasisNow  string `json:"basis_now,omitempty"`
	Why       string `json:"why"`
	Filter    string `json:"filter,omitempty"`
}

// MarshalJSON encodes empty change collections as arrays.
func (v Changes) MarshalJSON() ([]byte, error) {
	type plain Changes
	if v.DefinitionChanged == nil {
		v.DefinitionChanged = []string{}
	}
	if v.Joined == nil {
		v.Joined = []Joined{}
	}
	if v.Left == nil {
		v.Left = []Left{}
	}
	return json.Marshal(plain(v))
}

// Kinds identifies present and missing targeted enrichment kinds.
type Kinds struct {
	Present []string `json:"present"`
	Missing []string `json:"missing"`
}

// MarshalJSON encodes empty kind collections as arrays.
func (v Kinds) MarshalJSON() ([]byte, error) {
	type plain Kinds
	if v.Present == nil {
		v.Present = []string{}
	}
	if v.Missing == nil {
		v.Missing = []string{}
	}
	return json.Marshal(plain(v))
}

// Enrichments holds the facts and brief apart from judgments.
type Enrichments struct {
	Basis string `json:"basis"`
	Kinds Kinds  `json:"kinds"`
	Facts *Facts `json:"facts,omitempty"`
	Brief *Brief `json:"brief,omitempty"`
}

// Ruling is a tool answer value.
type Ruling struct {
	Kind      string   `json:"kind"`
	Candidate string   `json:"candidate"`
	Other     string   `json:"other,omitempty"`
	Keys      []string `json:"keys"`
	Why       string   `json:"why"`
	User      string   `json:"user"`
	Email     string   `json:"email"`
	At        string   `json:"at"`
}

// MarshalJSON encodes empty collections as arrays or objects.
func (v Ruling) MarshalJSON() ([]byte, error) {
	type plain Ruling
	if v.Keys == nil {
		v.Keys = []string{}
	}
	return json.Marshal(plain(v))
}

// Detail is a tool answer value.
type Detail struct {
	Candidate         Candidate          `json:"candidate"`
	Sources           *Sources           `json:"sources,omitempty"`
	Park              *ParkRef           `json:"park,omitempty"`
	Enrichments       *Enrichments       `json:"enrichments,omitempty"`
	Judgments         *Judgments         `json:"judgments,omitempty"`
	Estimates         []Estimate         `json:"estimates"`
	Links             []Link             `json:"links"`
	Rulings           []Ruling           `json:"rulings"`
	Pages             []Page             `json:"pages"`
	EnrichmentResults []EnrichmentResult `json:"enrichment_results"`
	Rejects           []Reject           `json:"rejects"`
	Campaigns         []CampaignUse      `json:"campaigns"`
}

// MarshalJSON encodes empty collections as arrays or objects.
func (v Detail) MarshalJSON() ([]byte, error) {
	type plain Detail
	if v.Campaigns == nil {
		v.Campaigns = []CampaignUse{}
	}
	if v.Estimates == nil {
		v.Estimates = []Estimate{}
	}
	if v.Links == nil {
		v.Links = []Link{}
	}
	if v.Rulings == nil {
		v.Rulings = []Ruling{}
	}
	if v.Rejects == nil {
		v.Rejects = []Reject{}
	}
	if v.Pages == nil {
		v.Pages = []Page{}
	}
	if v.EnrichmentResults == nil {
		v.EnrichmentResults = []EnrichmentResult{}
	}
	return json.Marshal(plain(v))
}

// Decision is a tool answer value.
type Decision struct {
	Ruling     Ruling      `json:"ruling"`
	Candidates []Candidate `json:"candidates"`
}

// MarshalJSON encodes empty collections as arrays or objects.
func (v Decision) MarshalJSON() ([]byte, error) {
	type plain Decision
	if v.Candidates == nil {
		v.Candidates = []Candidate{}
	}
	return json.Marshal(plain(v))
}

// NoArgs holds a tool call's arguments.
type NoArgs struct {
}

// ProspectListsArgs holds list paging arguments.
type ProspectListsArgs struct {
	Page  *string `json:"page" description:"Continues a previous answer: pass the next value it returned, with the same filters. Leave out for the first page."`
	Limit *int    `json:"limit" description:"Most entries to return, 1 to 500; 25 when left out."`
}

// ProspectListArgs identifies a prospectList.
type ProspectListArgs struct {
	ProspectList string `json:"prospect_list" mcp:"required"`
}

// ReadProspectListArgs selects a list and whether to record an open.
type ReadProspectListArgs struct {
	ProspectList string `json:"prospect_list" mcp:"required"`
	Peek         bool   `json:"peek"`
}

// FilterArgs holds a prospectList's filters.
type FilterArgs struct {
	RevenueMin         *uint32   `json:"revenue_min"`
	RevenueMax         *uint32   `json:"revenue_max"`
	RevenueUnknown     bool      `json:"revenue_unknown"`
	MaxMinutes         *uint32   `json:"max_minutes"`
	MinutesUnknown     bool      `json:"minutes_unknown"`
	Categories         []string  `json:"categories"`
	CategoryUnknown    bool      `json:"category_unknown"`
	Fleet              string    `json:"fleet"`
	FleetUnknown       bool      `json:"fleet_unknown"`
	Website            *[]string `json:"website"`
	WebsiteUnknown     bool      `json:"website_unknown"`
	Trend              *[]string `json:"trend"`
	TrendUnknown       bool      `json:"trend_unknown"`
	Government         *bool     `json:"government"`
	MaxFilingAgeDays   *uint32   `json:"max_filing_age_days"`
	FilingUnknown      bool      `json:"filing_unknown"`
	MinCampaignAgeDays *uint32   `json:"min_campaign_age_days"`
	Class              string    `json:"class"`
	ClassUnknown       bool      `json:"class_unknown"`
	Basis              *[]string `json:"basis"`
	ShowExcluded       bool      `json:"show_excluded"`
}

// OrderArgs selects one member ordering.
type OrderArgs struct {
	Field     string `json:"field" mcp:"required"`
	Direction string `json:"direction" mcp:"required"`
}

// CreateProspectListArgs defines a saved search.
type CreateProspectListArgs struct {
	Name    string       `json:"name" mcp:"required"`
	Filters *FilterArgs  `json:"filters"`
	Order   *[]OrderArgs `json:"order"`
	Size    *int         `json:"size"`
}

// EditProspectListArgs replaces supplied parts of a saved search.
type EditProspectListArgs struct {
	ProspectList string       `json:"prospect_list" mcp:"required"`
	Name         *string      `json:"name"`
	Filters      *FilterArgs  `json:"filters"`
	Order        *[]OrderArgs `json:"order"`
	Size         *int         `json:"size"`
}

// RejectArgs identifies a prospectList ruling.
type RejectArgs struct {
	ProspectList string `json:"prospect_list" mcp:"required"`
	Candidate    string `json:"candidate" mcp:"required"`
	Why          string `json:"why" mcp:"required"`
}

// RulingArgs identifies an exclusion or inclusion.
type RulingArgs struct {
	Candidate string `json:"candidate" mcp:"required"`
	Why       string `json:"why" mcp:"required"`
}

// CandidatesArgs holds a tool call's arguments.
type CandidatesArgs struct {
	Name  string  `json:"name"`
	Zip   string  `json:"zip"`
	Phone string  `json:"phone"`
	Page  *string `json:"page" description:"Continues a previous answer: pass the next value it returned, with the same filters. Leave out for the first page."`
	Limit *int    `json:"limit" description:"Most entries to return, 1 to 500; 25 when left out."`
}

// CandidateArgs holds a tool call's arguments.
type CandidateArgs struct {
	Candidate string `json:"candidate" mcp:"required"`
}

// PassesArgs holds a tool call's arguments.
type PassesArgs struct {
	Page  *string `json:"page" description:"Continues a previous answer: pass the next value it returned, with the same filters. Leave out for the first page."`
	Limit *int    `json:"limit" description:"Most entries to return, 1 to 500; 25 when left out."`
}

// PassArgs holds a tool call's arguments.
type PassArgs struct {
	Pass string `json:"pass" mcp:"required"`
}

// DecideArgs holds a tool call's arguments.
type DecideArgs struct {
	Kind      string   `json:"kind" mcp:"required"`
	Why       string   `json:"why" mcp:"required"`
	Candidate string   `json:"candidate" mcp:"required"`
	Other     string   `json:"other"`
	Links     []string `json:"links"`
}

// EnrichArgs selects a website enrichment pass.
type EnrichArgs struct {
	Kind         string `json:"kind"`
	ProspectList string `json:"prospect_list"`
	Candidate    string `json:"candidate"`
	Force        bool   `json:"force"`
}

// EnrichmentResult is one recorded enrichment result.
type EnrichmentResult struct {
	Kind          string          `json:"kind"`
	Source        string          `json:"source"`
	Inputs        string          `json:"inputs"`
	Found         json.RawMessage `json:"found,omitempty"`
	State         string          `json:"state,omitempty"`
	URL           string          `json:"url,omitempty"`
	Title         string          `json:"title,omitempty"`
	StatusCode    int             `json:"status_code,omitempty"`
	Size          *int64          `json:"size,omitempty"`
	Error         string          `json:"error,omitempty"`
	Domain        string          `json:"domain,omitempty"`
	PlaceURL      string          `json:"place_url,omitempty"`
	DirectionsURL string          `json:"directions_url,omitempty"`
	Cost          float64         `json:"cost"`
	At            string          `json:"at"`
}

// AddPageArgs selects a candidate website page to keep.
type AddPageArgs struct {
	Candidate string `json:"candidate" mcp:"required"`
	URL       string `json:"url" mcp:"required"`
}

// Page describes a kept copy without its text.
type Page struct {
	Number  int64  `json:"number"`
	URL     string `json:"url"`
	Title   string `json:"title,omitempty"`
	Size    int64  `json:"size"`
	Reason  string `json:"reason,omitempty"`
	Fetched string `json:"fetched"`
	By      string `json:"by"`
	Email   string `json:"email,omitempty"`
	Path    string `json:"path"`
}

// CampaignUse is a candidate's campaign history answer.
type CampaignUse struct {
	Campaign     string `json:"campaign"`
	ProspectList string `json:"prospect_list"`
	Added        string `json:"added,omitempty"`
	Rejected     string `json:"rejected,omitempty"`
	Reason       string `json:"reason,omitempty"`
	Delivered    string `json:"delivered,omitempty"`
}

// IngestArgs selects one scraper or every source.
type IngestArgs struct {
	Scraper string `json:"scraper"`
}
