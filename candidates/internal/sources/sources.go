// Package sources defines source readings and their normalized identity keys.
package sources

import (
	"encoding/json"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/ikigenba/ikigenba/appkit/scrapes"
)

// Class, category, evidence and rules constants identify the source rules.
const (
	FleetPass             = "pass"
	FleetFail             = "fail"
	FleetUnknown          = "unknown"
	ClassPrivate          = "private"
	ClassForHire          = "for_hire"
	ClassMixed            = "mixed"
	ClassUnknown          = "unknown"
	CategoryConstruction  = "construction"
	CategoryTrucking      = "trucking and logistics"
	CategoryAgriculture   = "agriculture"
	CategoryManufacturing = "manufacturing"
	CategoryWholesale     = "wholesale and distribution"
	CategoryMining        = "mining and aggregates"
	CategoryWaste         = "waste and environmental"
	CategoryOther         = "other"
	EvidenceFMCSAActive   = "fmcsa_active"
	EvidenceFMCSAActivity = "fmcsa_activity"
	EvidencePPPLoan       = "ppp_loan"
	EvidenceOwnDomain     = "own_domain"
	Rules                 = 4
)

// Person is a party named by a source record.
type Person struct{ Name, Role string }

// Evidence describes one sign of current or past operation.
type Evidence struct {
	Name, Source string
	Weight       float64
	Description  string
}

// Category identifies a business category and its source.
type Category struct{ Name, Source string }

// Evidence descriptions are the copy displayed with each evidence kind.
const (
	DescribeFMCSAActive   string = "Active FMCSA registration"
	DescribeFMCSAActivity string = "Reported drivers and mileage"
	DescribePPPLoan       string = "Recorded PPP loan"
	DescribeOwnDomain     string = "Email on a business domain"
)

// Estimate is one source's revenue estimate and supporting facts.
type Estimate struct {
	Source     string
	Low        int64
	High       *int64
	Confidence float64
	Facts      map[string]json.RawMessage
	Filed      time.Time
	Formula    int64
}

// Reading is the independent interpretation of one scraped row.
type Reading struct {
	NameKey, PhoneKey, DomainKey, Name             string
	Names                                          []string
	Street, City, State, Zip, County, Phone, Email string
	People                                         []Person
	Class                                          string
	Cargo                                          []string
	Filed                                          time.Time
	Fleet                                          bool
	FleetVerdict                                   string
	PowerUnits, Drivers                            *int64
	Estimate                                       *Estimate
	Evidence                                       []Evidence
	KnownSince                                     time.Time
	Category, GovernmentMatch                      string
}

// Source interprets rows from a named scraper.
type Source interface {
	Name() string
	Read(r scrapes.Row) Reading
}

// Registry holds the first source registered for each name.
type Registry struct{ entries map[string]Source }

// New builds a registry from sources in priority order.
func New(list ...Source) *Registry {
	r := &Registry{entries: make(map[string]Source)}
	for _, s := range list {
		n := s.Name()
		if _, ok := r.entries[n]; !ok {
			r.entries[n] = s
		}
	}
	return r
}

// Names returns a new sorted slice of the registered names.
func (r *Registry) Names() []string {
	out := make([]string, 0, len(r.entries))
	for n := range r.entries {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Get looks up a source by its scraper name.
func (r *Registry) Get(name string) (Source, bool) { s, ok := r.entries[name]; return s, ok }

// NameKey removes punctuation and trailing legal suffixes from a name.
func NameKey(name string) string {
	s := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsSpace(r) {
			return r
		}
		return -1
	}, strings.ToUpper(name))
	words := strings.Fields(s)
	for len(words) > 1 {
		switch words[len(words)-1] {
		case "INC", "LLC", "CO", "CORP", "LTD", "COMPANY":
			words = words[:len(words)-1]
		default:
			return strings.Join(words, " ")
		}
	}
	return strings.Join(words, " ")
}

// PhoneKey returns up to the last ten ASCII digits of a phone number.
func PhoneKey(phone string) string {
	b := make([]byte, 0, len(phone))
	for i := 0; i < len(phone); i++ {
		if phone[i] >= '0' && phone[i] <= '9' {
			b = append(b, phone[i])
		}
	}
	if len(b) > 10 {
		b = b[len(b)-10:]
	}
	return string(b)
}

// DomainKey returns a normalized non-free email domain.
func DomainKey(email string) string {
	i := strings.LastIndexByte(email, '@')
	if i < 0 {
		return ""
	}
	d := strings.ToLower(strings.TrimSpace(email[i+1:]))
	switch d {
	case "aol.com", "arvig.net", "att.net", "bellsouth.net", "centurylink.net", "centurytel.net", "charter.net", "comcast.net", "earthlink.net", "embarqmail.com", "frontier.com", "frontiernet.net", "gmail.com", "gmx.com", "googlemail.com", "hotmail.com", "hughes.net", "icloud.com", "live.com", "mac.com", "mail.com", "mchsi.com", "me.com", "midco.net", "msn.com", "outlook.com", "protonmail.com", "q.com", "qwest.net", "rocketmail.com", "sbcglobal.net", "tds.net", "usfamily.net", "verizon.net", "windstream.net", "yahoo.com", "ymail.com":
		return ""
	}
	return d
}

// County combines a state and FIPS code when both are present.
func County(state, fips string) string {
	s, f := strings.TrimSpace(state), strings.TrimSpace(fips)
	if s == "" || f == "" {
		return ""
	}
	return s + "-" + f
}

// SourceKey identifies one record within a scraper.
func SourceKey(scraper, sourceID string) string { return scraper + ":" + sourceID }

// GovernmentMatch returns the first whole-word government name pattern.
func GovernmentMatch(k string) string {
	for _, p := range []string{"COUNTY", "CITY OF", "TOWN OF", "VILLAGE OF", "TOWNSHIP", "SCHOOL DISTRICT", "STATE OF", "DEPT", "DEPARTMENT", "UNIVERSITY", "PUBLIC WORKS"} {
		if strings.Contains(" "+k+" ", " "+p+" ") {
			return p
		}
	}
	return ""
}
