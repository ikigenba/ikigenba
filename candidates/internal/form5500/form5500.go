// Package form5500 interprets employer benefit plan filings.
package form5500

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/appkit/scrapes"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
)

// Name is the scraper this source interprets.
const Name = "form5500"

// Source interprets one filing without external state.
type Source struct{}

// New constructs a Form 5500 source.
func New() *Source { return &Source{} }

// Name returns the scraper name.
func (*Source) Name() string { return Name }

func fact(r scrapes.Row, key string) string {
	var s string
	if json.Unmarshal(r.Facts[key], &s) != nil {
		return ""
	}
	return s
}
func date(s string) time.Time {
	if len(s) == 10 {
		if t, err := time.Parse(time.DateOnly, s); err == nil {
			return t
		}
	}
	return time.Time{}
}
func count(s string) int64 {
	if s == "" {
		return 0
	}
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return 0
		}
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n > 36893488147419 {
		return 0
	}
	return n
}
func dba(s string) []string {
	for i := range len(s) {
		for _, p := range []string{" DBA ", " D/B/A ", " D.B.A. "} {
			if i+len(p) <= len(s) && strings.EqualFold(s[i:i+len(p)], p) {
				return []string{strings.TrimSpace(s[:i]), strings.TrimSpace(s[i+len(p):])}
			}
		}
	}
	return nil
}
func alnum(b byte) bool { return b >= 'A' && b <= 'Z' || b >= 'a' && b <= 'z' || b >= '0' && b <= '9' }
func planName(s string) string {
	words := []string{"401(K)", "401K", "403(B)", "403B", "PROFIT SHARING", "PROFIT-SHARING", "RETIREMENT", "PENSION", "SAVINGS", "THRIFT", "INCENTIVE", "EMPLOYEE STOCK", "STOCK OWNERSHIP", "ESOP", "CASH BALANCE", "DEFINED BENEFIT", "DEFINED CONTRIBUTION", "MONEY PURCHASE", "HEALTH", "WELFARE", "BENEFIT", "BENEFITS", "CAFETERIA", "FLEXIBLE", "SECTION 125", "SEC 125", "SEC. 125", "DENTAL", "VISION", "LIFE", "DISABILITY", "MEDICAL", "INSURANCE", "EMPLOYEES'", "EMPLOYEES", "EMPLOYEE", "PLAN", "TRUST"}
	end := len(s)
outer:
	for i := range len(s) {
		if i > 0 && alnum(s[i-1]) {
			continue
		}
		for _, w := range words {
			n := i + len(w)
			if n <= len(s) && strings.EqualFold(s[i:n], w) && (n == len(s) || !alnum(s[n])) {
				end = i
				break outer
			}
		}
	}
	t := strings.TrimSpace(s[:end])
	for len(t) > 0 {
		n := len(t)
		if strings.ContainsRune(",-&", rune(t[n-1])) {
			t = strings.TrimSpace(t[:n-1])
			continue
		}
		if n >= 3 && strings.EqualFold(t[n-3:], "AND") && (n == 3 || !alnum(t[n-4])) {
			t = strings.TrimSpace(t[:n-3])
			continue
		}
		break
	}
	return t
}

// Read returns an independent interpretation of the filing.
func (*Source) Read(r scrapes.Row) sources.Reading {
	out := sources.Reading{NameKey: sources.NameKey(r.Name), Name: r.Name, Names: append([]string{}, r.Names...), Street: r.Address.Street, City: r.Address.City, State: r.Address.State, Zip: r.Address.Zip, Phone: fact(r, "phone"), People: []sources.Person{}, Cargo: []string{}, FleetVerdict: sources.FleetUnknown, Filed: date(fact(r, "received")), KnownSince: date(fact(r, "plan_effective")), Category: sources.NAICSCategory(fact(r, "business_code")), EINKey: sources.EINKey(fact(r, "ein")), Evidence: []sources.Evidence{{Name: sources.EvidenceForm5500Filing, Source: Name, Weight: .2, Description: sources.DescribeForm5500Filing}}}
	seen := map[string]bool{}
	for _, n := range out.Names {
		seen[sources.NameKey(n)] = true
	}
	derived := append(dba(fact(r, "sponsor_name")), planName(fact(r, "plan_name")))
	for _, n := range derived {
		k := sources.NameKey(n)
		if k != "" && !seen[k] {
			out.Names = append(out.Names, n)
			seen[k] = true
		}
	}
	out.NameKeys = sources.NameKeys(out.Names)
	out.PhoneKey = sources.PhoneKey(out.Phone)
	out.GovernmentMatch = sources.GovernmentMatch(out.NameKey)
	for _, p := range r.People {
		out.People = append(out.People, sources.Person{Name: p.Name, Role: p.Role})
	}
	single := fact(r, "form") == "5500" && fact(r, "entity") == "2" || fact(r, "form") == "5500-SF" && fact(r, "entity") == "1"
	if single {
		n, asOf := count(fact(r, "active_participants_eoy")), date(fact(r, "plan_year_end"))
		if n == 0 {
			n, asOf = count(fact(r, "participants_boy")), date(fact(r, "plan_year_begin"))
		}
		if n > 0 {
			out.Headcount = &sources.Headcount{Scraper: Name, Kind: sources.HeadcountParticipants, Count: n, AsOf: asOf}
		}
	}
	return out
}
