package sources

import (
	"slices"
	"strings"
	"time"
)

// Source identifiers and fact and value words identify the independent orders.
const (
	Owner            = "owner"
	Brief            = "brief"
	Route            = "route"
	FactName         = "name"
	FactAddress      = "address"
	FactPhone        = "phone"
	FactEmail        = "email"
	FactPeople       = "people"
	FactKnownSince   = "known_since"
	FactNewestFiling = "newest_filing"
	ValueName        = "name"
	ValueNames       = "names"
	ValueTradeNames  = "trade_names"
	ValueLocation    = "location"
	ValueMailing     = "mailing"
	ValuePhone       = "phone"
	ValueEmail       = "email"
	ValuePeople      = "people"
	ValueKnownSince  = "known_since"
	ValueFiled       = "filed"
)

// Address is one postal address.
type Address struct{ Street, City, State, Zip, County string }

// Slot selects one kind of value from a group of sources.
type Slot struct {
	Sources []string
	Value   string
}

// Order ranks a fact's slots, best first.
type Order struct {
	Fact  string
	Slots []Slot
}

// Record holds a reading and the identity and age of its evidence.
type Record struct {
	Source, SourceID string
	Gone             bool
	Seen             time.Time
	Reading          Reading
}

// Sourced retains the provenance of a chosen value.
type Sourced[T any] struct {
	Value       T
	Source, Key string
}

// Picked holds the chosen facts and all distinct alternatives.
type Picked struct {
	Name           Sourced[string]
	OtherNames     []Sourced[string]
	Address        Sourced[Address]
	OtherAddresses []Sourced[Address]
	Phone          Sourced[string]
	OtherPhones    []Sourced[string]
	Email          Sourced[string]
	OtherEmails    []Sourced[string]
	People         []Sourced[Person]
	KnownSince     Sourced[time.Time]
	NewestFiling   Sourced[time.Time]
}

// Orders returns independent storage for the seven fact orders.
func Orders() []Order {
	return []Order{
		{FactName, []Slot{{[]string{"places"}, ValueName}, {[]string{"fmcsa"}, ValueTradeNames}, {[]string{"form5500"}, ValueTradeNames}, {[]string{"fmcsa", "ppp", "form5500", "osha_ita"}, ValueNames}}},
		{FactAddress, []Slot{{[]string{Route}, ValueLocation}, {[]string{"places"}, ValueLocation}, {[]string{"osha_ita"}, ValueLocation}, {[]string{"form5500"}, ValueLocation}, {[]string{"fmcsa"}, ValueLocation}, {[]string{"ppp"}, ValueLocation}, {[]string{"form5500"}, ValueMailing}}},
		{FactPhone, []Slot{{[]string{"places"}, ValuePhone}, {[]string{"form5500"}, ValuePhone}, {[]string{"fmcsa"}, ValuePhone}}},
		{FactEmail, []Slot{{[]string{"fmcsa"}, ValueEmail}}},
		{FactPeople, []Slot{{[]string{Brief}, ValuePeople}, {[]string{"form5500"}, ValuePeople}, {[]string{"fmcsa"}, ValuePeople}}},
		{FactKnownSince, []Slot{{[]string{Owner}, ValueKnownSince}, {[]string{Brief}, ValueKnownSince}, {[]string{"fmcsa", "ppp", "form5500", "osha_ita", "places"}, ValueKnownSince}}},
		{FactNewestFiling, []Slot{{[]string{"fmcsa", "ppp", "form5500", "osha_ita", "places"}, ValueFiled}}},
	}
}

// Pick selects each fact independently without changing any input storage.
func Pick(orders []Order, links, given []Record) Picked {
	live := slices.ContainsFunc(links, func(r Record) bool { return !r.Gone })
	records := make([]Record, 0, len(links)+len(given))
	for _, r := range links {
		if !live || !r.Gone {
			records = append(records, r)
		}
	}
	records = append(records, given...)
	slices.SortStableFunc(records, func(a, b Record) int {
		if !a.Reading.Filed.Equal(b.Reading.Filed) {
			if a.Reading.Filed.After(b.Reading.Filed) {
				return -1
			}
			return 1
		}
		if n := strings.Compare(a.Source, b.Source); n != 0 {
			return n
		}
		return strings.Compare(a.SourceID, b.SourceID)
	})
	names := pickValues[string](orders, records, FactName)
	addresses := pickValues[Address](orders, records, FactAddress)
	phones := pickValues[string](orders, records, FactPhone)
	emails := pickValues[string](orders, records, FactEmail)
	people := pickValues[Person](orders, records, FactPeople)
	known := pickValues[time.Time](orders, records, FactKnownSince)
	filed := pickValues[time.Time](orders, records, FactNewestFiling)
	p := Picked{OtherNames: []Sourced[string]{}, OtherAddresses: []Sourced[Address]{}, OtherPhones: []Sourced[string]{}, OtherEmails: []Sourced[string]{}, People: people}
	p.Name, p.OtherNames = firstAndRest(names)
	p.Address, p.OtherAddresses = firstAndRest(addresses)
	p.Phone, p.OtherPhones = firstAndRest(phones)
	p.Email, p.OtherEmails = firstAndRest(emails)
	if len(known) > 0 {
		p.KnownSince = known[0]
	}
	if len(filed) > 0 {
		p.NewestFiling = filed[0]
	}
	return p
}

func firstAndRest[T any](values []Sourced[T]) (Sourced[T], []Sourced[T]) {
	if len(values) == 0 {
		return Sourced[T]{}, []Sourced[T]{}
	}
	return values[0], values[1:]
}
func sameValue[T any](a, b T) bool {
	switch x := any(a).(type) {
	case string:
		return x == any(b).(string)
	case Address:
		return x == any(b).(Address)
	case Person:
		return x == any(b).(Person)
	case time.Time:
		return x.Equal(any(b).(time.Time))
	}
	return false
}
func pickValues[T any](orders []Order, records []Record, fact string) []Sourced[T] {
	out := []Sourced[T]{}
	for _, o := range orders {
		if o.Fact != fact {
			continue
		}
		for _, s := range o.Slots {
			slot := []Sourced[T]{}
			for _, r := range records {
				if !slices.Contains(s.Sources, r.Source) {
					continue
				}
				for _, v := range recordValues(r.Reading, s.Value) {
					value, ok := v.(T)
					if !ok {
						continue
					}
					slot = append(slot, Sourced[T]{value, r.Source, SourceKey(r.Source, r.SourceID)})
				}
			}
			if fact == FactKnownSince {
				slices.SortStableFunc(slot, func(a, b Sourced[T]) int { return any(a.Value).(time.Time).Compare(any(b.Value).(time.Time)) })
			}
			for _, v := range slot {
				if !slices.ContainsFunc(out, func(x Sourced[T]) bool { return sameValue(x.Value, v.Value) }) {
					out = append(out, v)
				}
			}
		}
		break
	}
	return out
}
func blank(a Address) bool { return a.Street == "" && a.City == "" && a.State == "" && a.Zip == "" }
func recordValues(r Reading, value string) []any {
	out := []any{}
	addStrings := func(ss []string) {
		seen := map[string]bool{}
		for _, s := range ss {
			if s != "" && !seen[s] {
				out = append(out, s)
				seen[s] = true
			}
		}
	}
	own := Address{r.Street, r.City, r.State, r.Zip, r.County}
	switch value {
	case ValueName:
		addStrings([]string{r.Name})
	case ValueNames:
		addStrings(append([]string{r.Name}, r.Names...))
	case ValueTradeNames:
		addStrings(r.TradeNames)
	case ValueLocation:
		if !r.Mailing && !blank(own) {
			out = append(out, own)
		}
	case ValueMailing:
		if r.Mailing && !blank(own) {
			out = append(out, own)
		}
		if r.MailingAddress != nil && !blank(*r.MailingAddress) && (len(out) == 0 || own != *r.MailingAddress) {
			out = append(out, *r.MailingAddress)
		}
	case ValuePhone:
		addStrings([]string{r.Phone})
	case ValueEmail:
		addStrings([]string{r.Email})
	case ValuePeople:
		for _, p := range r.People {
			if p.Name != "" {
				out = append(out, p)
			}
		}
	case ValueKnownSince:
		if !r.KnownSince.IsZero() {
			out = append(out, r.KnownSince)
		}
	case ValueFiled:
		if !r.Filed.IsZero() {
			out = append(out, r.Filed)
		}
	}
	return out
}
