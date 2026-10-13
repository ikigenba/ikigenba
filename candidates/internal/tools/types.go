// Package tools exposes the candidate funnel through MCP.
package tools

import (
	"encoding/json"

	"github.com/ikigenba/ikigenba/candidates/internal/passes"
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

// ShortlistEntry is a tool answer value.
type ShortlistEntry struct {
	Candidate  Candidate `json:"candidate"`
	Minutes    int64     `json:"minutes"`
	Low        int64     `json:"low"`
	High       *int64    `json:"high,omitempty"`
	Confidence float64   `json:"confidence"`
	Effective  float64   `json:"effective"`
	Point      int64     `json:"point"`
	Score      int64     `json:"score"`
}

// ShortlistPage is a tool answer value.
type ShortlistPage struct {
	Shortlist []ShortlistEntry `json:"shortlist"`
	Next      *int64           `json:"next,omitempty"`
}

// MarshalJSON encodes empty collections as arrays or objects.
func (v ShortlistPage) MarshalJSON() ([]byte, error) {
	type plain ShortlistPage
	if v.Shortlist == nil {
		v.Shortlist = []ShortlistEntry{}
	}
	return json.Marshal(plain(v))
}

// CandidateList is a tool answer value.
type CandidateList struct {
	Candidates []Candidate `json:"candidates"`
	Next       *int64      `json:"next,omitempty"`
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
	Next   *int64          `json:"next,omitempty"`
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
	Scraper    string   `json:"scraper"`
	SourceID   string   `json:"source_id"`
	Run        string   `json:"run"`
	Digest     string   `json:"digest"`
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
	Filed      string   `json:"filed,omitempty"`
	Fleet      bool     `json:"fleet"`
	NameKey    string   `json:"name_key"`
	PhoneKey   string   `json:"phone_key"`
	DomainKey  string   `json:"domain_key"`
	Reason     string   `json:"reason"`
	Confidence float64  `json:"confidence"`
	FirstSeen  string   `json:"first_seen"`
	LastSeen   string   `json:"last_seen"`
	Gone       bool     `json:"gone"`
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

// Exclusion is a tool answer value.
type Exclusion struct {
	Reason string `json:"reason"`
	Detail string `json:"detail,omitempty"`
}

// Assessment is a tool answer value.
type Assessment struct {
	Fleet      bool        `json:"fleet"`
	Minutes    *int64      `json:"minutes,omitempty"`
	Low        int64       `json:"low"`
	High       *int64      `json:"high,omitempty"`
	Confidence float64     `json:"confidence"`
	Filed      string      `json:"filed,omitempty"`
	Freshness  float64     `json:"freshness"`
	Effective  float64     `json:"effective"`
	Point      int64       `json:"point"`
	Score      int64       `json:"score"`
	Base       int64       `json:"base"`
	Excluded   bool        `json:"excluded"`
	Exclusions []Exclusion `json:"exclusions"`
	Rules      int64       `json:"rules"`
	Scored     string      `json:"scored"`
}

// MarshalJSON encodes empty collections as arrays or objects.
func (v Assessment) MarshalJSON() ([]byte, error) {
	type plain Assessment
	if v.Exclusions == nil {
		v.Exclusions = []Exclusion{}
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
	Candidate  Candidate   `json:"candidate"`
	Assessment *Assessment `json:"assessment,omitempty"`
	Estimates  []Estimate  `json:"estimates"`
	Links      []Link      `json:"links"`
	Rulings    []Ruling    `json:"rulings"`
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

// ShortlistArgs holds a tool call's arguments.
type ShortlistArgs struct {
	MaxMinutes *uint32 `json:"max_minutes"`
	MinRevenue *uint32 `json:"min_revenue"`
	Class      string  `json:"class"`
	Cargo      string  `json:"cargo"`
	Page       uint32  `json:"page"`
}

// CandidatesArgs holds a tool call's arguments.
type CandidatesArgs struct {
	Name  string `json:"name"`
	Zip   string `json:"zip"`
	Phone string `json:"phone"`
	Page  uint32 `json:"page"`
}

// CandidateArgs holds a tool call's arguments.
type CandidateArgs struct {
	Candidate string `json:"candidate" mcp:"required"`
}

// PassesArgs holds a tool call's arguments.
type PassesArgs struct {
	Page uint32 `json:"page"`
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
