// Package tools exposes the candidate funnel through MCP.
package tools

import (
	"encoding/json"

	"github.com/ikigenba/ikigenba/candidates/internal/passes"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// Person is a tool answer value.
type Person struct {
	Name string `json:"name"`
	Role string `json:"role"`
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

// Member is a cohort member's stored judgment.
type Member struct {
	Candidate           Candidate `json:"candidate"`
	OperatingConfidence *float64  `json:"operating_confidence,omitempty"`
	RevenueLow          *int64    `json:"revenue_low,omitempty"`
	RevenueHigh         *int64    `json:"revenue_high,omitempty"`
	RevenueConfidence   *float64  `json:"revenue_confidence,omitempty"`
	Categories          []string  `json:"categories"`
	ClassifiedBy        string    `json:"classified_by"`
	FleetVerdict        string    `json:"fleet_verdict"`
	Website             string    `json:"website"`
	WebsiteURL          string    `json:"website_url,omitempty"`
	SiteText            *string   `json:"site_text"`
	Brief               *string   `json:"brief"`
	Minutes             *int64    `json:"minutes,omitempty"`
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

// Cohort is a saved search definition.
type Cohort struct {
	ID           string              `json:"id"`
	Name         string              `json:"name"`
	Size         int64               `json:"size"`
	Filters      store.CohortFilters `json:"filters"`
	Order        []store.CohortOrder `json:"order"`
	CreatedBy    string              `json:"created_by"`
	CreatedEmail string              `json:"created_email"`
	Created      string              `json:"created"`
	ChangedBy    string              `json:"changed_by"`
	ChangedEmail string              `json:"changed_email"`
	Changed      string              `json:"changed"`
}

// MarshalJSON encodes empty order as an array.
func (v Cohort) MarshalJSON() ([]byte, error) {
	type plain Cohort
	if v.Order == nil {
		v.Order = []store.CohortOrder{}
	}
	return json.Marshal(plain(v))
}

// CohortList is a page of definitions.
type CohortList struct {
	Cohorts []Cohort `json:"cohorts"`
	Next    *string  `json:"next,omitempty" description:"Present when more entries follow; pass it back as page to get them."`
}

// MarshalJSON encodes empty cohorts as an array.
func (v CohortList) MarshalJSON() ([]byte, error) {
	type plain CohortList
	if v.Cohorts == nil {
		v.Cohorts = []Cohort{}
	}
	return json.Marshal(plain(v))
}

// Reject is an owner's cohort ruling.
type Reject struct {
	Kind       string `json:"kind"`
	Cohort     string `json:"cohort"`
	CohortName string `json:"cohort_name,omitempty"`
	Candidate  string `json:"candidate"`
	Why        string `json:"why,omitempty"`
	User       string `json:"user,omitempty"`
	Email      string `json:"email,omitempty"`
	At         string `json:"at,omitempty"`
}

// CohortRead answers a cohort and its current members.
type CohortRead struct {
	Cohort     Cohort   `json:"cohort"`
	Matching   int64    `json:"matching"`
	Classified int64    `json:"classified"`
	Members    []Member `json:"members"`
	Rejects    []Reject `json:"rejects"`
}

// MarshalJSON encodes empty members and rejects as arrays.
func (v CohortRead) MarshalJSON() ([]byte, error) {
	type plain CohortRead
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
	Scraper      string   `json:"scraper"`
	SourceID     string   `json:"source_id"`
	Run          string   `json:"run"`
	Digest       string   `json:"digest"`
	Name         string   `json:"name"`
	Names        []string `json:"names"`
	Street       string   `json:"street"`
	City         string   `json:"city"`
	State        string   `json:"state"`
	Zip          string   `json:"zip"`
	County       string   `json:"county"`
	Phone        string   `json:"phone"`
	Email        string   `json:"email"`
	People       []Person `json:"people"`
	Class        string   `json:"class"`
	Cargo        []string `json:"cargo"`
	Filed        string   `json:"filed,omitempty"`
	FleetVerdict string   `json:"fleet_verdict"`
	NameKey      string   `json:"name_key"`
	PhoneKey     string   `json:"phone_key"`
	DomainKey    string   `json:"domain_key"`
	Reason       string   `json:"reason"`
	Confidence   float64  `json:"confidence"`
	FirstSeen    string   `json:"first_seen"`
	LastSeen     string   `json:"last_seen"`
	Gone         bool     `json:"gone"`
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

// Assessment is a stored judgment answer.
type Assessment struct {
	OperatingConfidence *float64   `json:"operating_confidence,omitempty"`
	OperatingEvidence   []Evidence `json:"operating_evidence"`
	RevenueLow          *int64     `json:"revenue_low,omitempty"`
	RevenueHigh         *int64     `json:"revenue_high,omitempty"`
	RevenueConfidence   *float64   `json:"revenue_confidence,omitempty"`
	KnownSince          string     `json:"known_since,omitempty"`
	NewestFiling        string     `json:"newest_filing,omitempty"`
	Categories          []Category `json:"categories"`
	ClassifiedBy        string     `json:"classified_by"`
	RecordsDiffer       bool       `json:"records_differ"`
	RecordCategories    []Category `json:"record_categories"`
	GovernmentMatch     string     `json:"government_match,omitempty"`
	FleetVerdict        string     `json:"fleet_verdict"`
	PowerUnits          *int64     `json:"power_units,omitempty"`
	Drivers             *int64     `json:"drivers,omitempty"`
	Website             string     `json:"website"`
	WebsiteURL          string     `json:"website_url,omitempty"`
	Minutes             *int64     `json:"minutes,omitempty"`
	Brief               string     `json:"brief,omitempty"`
	Excluded            bool       `json:"excluded"`
	Rules               int64      `json:"rules"`
	Calculated          string     `json:"calculated"`
}

// MarshalJSON encodes empty collections as arrays.
func (v Assessment) MarshalJSON() ([]byte, error) {
	type plain Assessment
	if v.OperatingEvidence == nil {
		v.OperatingEvidence = []Evidence{}
	}
	if v.Categories == nil {
		v.Categories = []Category{}
	}
	if v.RecordCategories == nil {
		v.RecordCategories = []Category{}
	}
	return json.Marshal(plain(v))
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
	Candidate   Candidate    `json:"candidate"`
	Assessment  *Assessment  `json:"assessment,omitempty"`
	Estimates   []Estimate   `json:"estimates"`
	Links       []Link       `json:"links"`
	Rulings     []Ruling     `json:"rulings"`
	Pages       []Page       `json:"pages"`
	Enrichments []Enrichment `json:"enrichments"`
	Rejects     []Reject     `json:"rejects"`
}

// MarshalJSON encodes empty collections as arrays or objects.
func (v Detail) MarshalJSON() ([]byte, error) {
	type plain Detail
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
	if v.Enrichments == nil {
		v.Enrichments = []Enrichment{}
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

// CohortsArgs holds list paging arguments.
type CohortsArgs struct {
	Page  *string `json:"page" description:"Continues a previous answer: pass the next value it returned, with the same filters. Leave out for the first page."`
	Limit *int    `json:"limit" description:"Most entries to return, 1 to 500; 25 when left out."`
}

// CohortArgs identifies a cohort.
type CohortArgs struct {
	Cohort string `json:"cohort" mcp:"required"`
}

// FilterArgs holds a cohort's filters.
type FilterArgs struct {
	RevenueMin       *uint32   `json:"revenue_min"`
	RevenueMax       *uint32   `json:"revenue_max"`
	RevenueUnknown   bool      `json:"revenue_unknown"`
	MaxMinutes       *uint32   `json:"max_minutes"`
	MinutesUnknown   bool      `json:"minutes_unknown"`
	Categories       []string  `json:"categories"`
	CategoryUnknown  bool      `json:"category_unknown"`
	Fleet            string    `json:"fleet"`
	FleetUnknown     bool      `json:"fleet_unknown"`
	Website          *[]string `json:"website"`
	WebsiteUnknown   bool      `json:"website_unknown"`
	Government       *bool     `json:"government"`
	MaxFilingAgeDays *uint32   `json:"max_filing_age_days"`
	FilingUnknown    bool      `json:"filing_unknown"`
	Class            string    `json:"class"`
	ClassUnknown     bool      `json:"class_unknown"`
	ShowExcluded     bool      `json:"show_excluded"`
}

// OrderArgs selects one member ordering.
type OrderArgs struct {
	Field     string `json:"field" mcp:"required"`
	Direction string `json:"direction" mcp:"required"`
}

// CreateCohortArgs defines a saved search.
type CreateCohortArgs struct {
	Name    string       `json:"name" mcp:"required"`
	Filters *FilterArgs  `json:"filters"`
	Order   *[]OrderArgs `json:"order"`
	Size    *int         `json:"size"`
}

// EditCohortArgs replaces supplied parts of a saved search.
type EditCohortArgs struct {
	Cohort  string       `json:"cohort" mcp:"required"`
	Name    *string      `json:"name"`
	Filters *FilterArgs  `json:"filters"`
	Order   *[]OrderArgs `json:"order"`
	Size    *int         `json:"size"`
}

// RejectArgs identifies a cohort ruling.
type RejectArgs struct {
	Cohort    string `json:"cohort" mcp:"required"`
	Candidate string `json:"candidate" mcp:"required"`
	Why       string `json:"why" mcp:"required"`
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
	Kind      string `json:"kind"`
	Cohort    string `json:"cohort"`
	Candidate string `json:"candidate"`
	Force     bool   `json:"force"`
}

// Enrichment is one recorded enrichment result.
type Enrichment struct {
	Kind       string          `json:"kind"`
	Source     string          `json:"source"`
	Inputs     string          `json:"inputs"`
	Found      json.RawMessage `json:"found,omitempty"`
	State      string          `json:"state,omitempty"`
	URL        string          `json:"url,omitempty"`
	Title      string          `json:"title,omitempty"`
	StatusCode int             `json:"status_code,omitempty"`
	Size       *int64          `json:"size,omitempty"`
	Error      string          `json:"error,omitempty"`
	Domain     string          `json:"domain,omitempty"`
	Cost       float64         `json:"cost"`
	At         string          `json:"at"`
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
	Fetched string `json:"fetched"`
	By      string `json:"by"`
	Email   string `json:"email,omitempty"`
	Path    string `json:"path"`
}
