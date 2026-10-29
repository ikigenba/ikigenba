// Package oshaita interprets annual OSHA establishment summaries.
package oshaita

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/appkit/scrapes"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
)

// Name is the scraper this source interprets.
const Name = "osha_ita"

// Source interprets one summary without external state.
type Source struct{}

// New constructs an OSHA ITA source.
func New() *Source { return &Source{} }

// Name returns the scraper name.
func (*Source) Name() string { return Name }

func fact(r scrapes.Row, k string) string {
	var s string
	if json.Unmarshal(r.Facts[k], &s) != nil {
		return ""
	}
	return s
}

func digits(s string) bool {
	if s == "" {
		return false
	}
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func integral(s string) string {
	a, b, ok := strings.Cut(s, ".")
	if !ok {
		return s
	}
	if !digits(a) || b == "" || strings.Trim(b, "0") != "" {
		return ""
	}
	return a
}

func figure(s string) *int64 {
	s = integral(s)
	if !digits(s) {
		return nil
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n > 36893488147419 {
		return nil
	}
	return &n
}

func submitted(s string) time.Time {
	for _, layout := range []string{"1/2/2006 15:04:05", "02Jan06:15:04:05", "1/2/2006"} {
		if t, err := time.Parse(layout, s); err == nil {
			return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
		}
	}
	return time.Time{}
}

// Read returns an independent interpretation, retaining figures even when implausible.
func (*Source) Read(r scrapes.Row) sources.Reading {
	out := sources.Reading{TradeNames: []string{}, NameKey: sources.NameKey(r.Name), Name: r.Name, Names: []string{}, Street: r.Address.Street, City: r.Address.City, State: r.Address.State, Zip: r.Address.Zip, People: []sources.Person{}, Cargo: []string{}, FleetVerdict: sources.FleetUnknown, Establishment: fact(r, "establishment_name"), Year: figure(fact(r, "year_filing_for")), Employees: figure(fact(r, "annual_average_employees")), Hours: figure(fact(r, "total_hours_worked")), Filed: submitted(fact(r, "date_submitted")), Category: sources.NAICSCategory(integral(strings.TrimSpace(fact(r, "naics_code")))), Evidence: []sources.Evidence{{Name: sources.EvidenceOSHAITASummary, Source: Name, Weight: .2, Description: sources.DescribeOSHAITASummary}}}
	if r.Name != "" {
		out.Names = append(out.Names, r.Name)
	}
	out.NameKeys = sources.NameKeys(out.Names)
	out.GovernmentMatch = sources.GovernmentMatch(out.NameKey)
	ein := fact(r, "ein")
	if len(ein) == 8 && digits(ein) {
		ein = "0" + ein
	}
	out.EINKey = sources.EINKey(ein)
	if out.Year != nil && (*out.Year < 1 || *out.Year > 9999) {
		out.Year = nil
	}
	var asOf time.Time
	if out.Year != nil {
		out.KnownSince = time.Date(int(*out.Year), time.January, 1, 0, 0, 0, 0, time.UTC)
		asOf = time.Date(int(*out.Year), time.December, 31, 0, 0, 0, 0, time.UTC)
	}
	absent := strings.TrimSpace(fact(r, "total_hours_worked")) == ""
	if out.Employees != nil && *out.Employees > 0 && (absent || out.Hours != nil && *out.Hours > 0 && 500**out.Employees <= *out.Hours && *out.Hours <= 4000**out.Employees) {
		out.Headcount = &sources.Headcount{Scraper: Name, Kind: sources.HeadcountEmployees, Count: *out.Employees, AsOf: asOf}
		if !absent {
			n := *out.Hours
			out.Headcount.Hours = &n
		}
	}
	return out
}
