// Package ppp interprets Paycheck Protection Program loan rows.
package ppp

import (
	"encoding/json"
	"strconv"
	"time"

	"github.com/ikigenba/ikigenba/appkit/scrapes"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
)

// Name is the scraper this source interprets.
const Name = "ppp"

// Source interprets one PPP loan without external state.
type Source struct{}

// New constructs a PPP source.
func New() *Source { return &Source{} }

// Name returns the scraper name.
func (*Source) Name() string { return Name }

func fact(f map[string]json.RawMessage, k string) string {
	var s string
	if json.Unmarshal(f[k], &s) != nil {
		return ""
	}
	return s
}
func digits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// Read returns a self-contained interpretation of the loan.
func (*Source) Read(r scrapes.Row) sources.Reading {
	out := sources.Reading{TradeNames: []string{}, FleetVerdict: sources.FleetUnknown, NameKey: sources.NameKey(r.Name), Name: r.Name, Names: append([]string{}, r.Names...), Street: r.Address.Street, City: r.Address.City, State: r.Address.State, Zip: r.Address.Zip, County: sources.County(r.Address.State, r.Address.County), People: []sources.Person{}, Cargo: []string{}, Evidence: []sources.Evidence{{Name: sources.EvidencePPPLoan, Source: Name, Weight: 0.2, Description: sources.DescribePPPLoan}}, Category: sources.NAICSCategory(fact(r.Facts, "naics"))}
	out.NameKeys = sources.NameKeys(out.Names)
	out.GovernmentMatch = sources.GovernmentMatch(out.NameKey)
	s := fact(r.Facts, "date_approved")
	if len(s) == 10 {
		if t, err := time.Parse("01/02/2006", s); err == nil {
			out.Filed = t
		}
	}
	out.KnownSince = out.Filed
	if !out.KnownSince.IsZero() && fact(r.Facts, "business_age") == "Existing or more than 2 years old" {
		out.KnownSince = out.KnownSince.AddDate(-2, 0, 0)
	}
	ns := fact(r.Facts, "jobs_reported")
	n, err := strconv.ParseInt(ns, 10, 64)
	if digits(ns) && err == nil && n > 0 && n <= 36893488147419 {
		out.Headcount = &sources.Headcount{Scraper: Name, Kind: sources.HeadcountJobs, Count: n, AsOf: out.Filed}
	}
	return out
}
