// Package ppp interprets Paycheck Protection Program loan rows.
package ppp

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
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
func amount(s string) (float64, bool) {
	parts := strings.Split(s, ".")
	if len(parts) > 2 || !digits(parts[0]) || len(parts) == 2 && !digits(parts[1]) {
		return 0, false
	}
	a, err := strconv.ParseFloat(s, 64)
	return a, err == nil && a > 0 && a <= 1000000000
}
func category(s string) string {
	s = strings.TrimSpace(s)
	if !digits(s) {
		return ""
	}
	for _, group := range []struct {
		prefixes []string
		name     string
	}{
		{[]string{"23"}, sources.CategoryConstruction},
		{[]string{"484", "488", "492", "493"}, sources.CategoryTrucking},
		{[]string{"111", "112", "115"}, sources.CategoryAgriculture},
		{[]string{"31", "32", "33"}, sources.CategoryManufacturing},
		{[]string{"42"}, sources.CategoryWholesale},
		{[]string{"212", "213"}, sources.CategoryMining},
		{[]string{"562"}, sources.CategoryWaste},
	} {
		for _, p := range group.prefixes {
			if strings.HasPrefix(s, p) {
				return group.name
			}
		}
	}
	return sources.CategoryOther
}

// Read returns a self-contained interpretation of the loan.
func (*Source) Read(r scrapes.Row) sources.Reading {
	out := sources.Reading{NameKey: sources.NameKey(r.Name), Name: r.Name, Names: append([]string{}, r.Names...), Street: r.Address.Street, City: r.Address.City, State: r.Address.State, Zip: r.Address.Zip, County: sources.County(r.Address.State, r.Address.County), People: []sources.Person{}, Cargo: []string{}, Evidence: []sources.Evidence{{Name: sources.EvidencePPPLoan, Source: Name, Weight: 0.2, Description: sources.DescribePPPLoan}}, Category: category(fact(r.Facts, "naics"))}
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
	if a, ok := amount(fact(r.Facts, "amount")); ok {
		high := int64(math.Round(a / 2.5 * 12 / 0.25))
		est := &sources.Estimate{Source: Name, Low: int64(math.Round(a / 2.5 * 12 / 0.4)), High: &high, Confidence: 0.4, Filed: out.Filed, Formula: sources.Rules, Facts: map[string]json.RawMessage{}}
		for _, k := range []string{"amount", "naics", "date_approved", "business_age", "jobs_reported"} {
			if v, exists := r.Facts[k]; exists {
				est.Facts[k] = append(json.RawMessage(nil), v...)
			}
		}
		out.Estimate = est
	}
	return out
}
