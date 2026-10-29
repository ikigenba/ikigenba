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
	FactHeadcount    = "headcount"
	FactRevenue      = "revenue"
	FactCategory     = "category"
	FactWebsite      = "website"
	FactEIN          = "ein"
	FactFleet        = "fleet"
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
	ValueEIN         = "ein"
	ValuePowerUnits  = "power_units"
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
	Name            Sourced[string]
	OtherNames      []Sourced[string]
	Address         Sourced[Address]
	OtherAddresses  []Sourced[Address]
	Phone           Sourced[string]
	OtherPhones     []Sourced[string]
	Email           Sourced[string]
	OtherEmails     []Sourced[string]
	People          []Sourced[Person]
	KnownSince      Sourced[time.Time]
	OtherKnownSince []Sourced[time.Time]
	NewestFiling    Sourced[time.Time]
	EIN             Sourced[string]
	PowerUnits      Sourced[int64]
	OtherPowerUnits []Sourced[int64]
}

// SettableFacts returns independent storage for the facts an owner can change.
func SettableFacts() []string {
	return []string{FactName, FactHeadcount, FactRevenue, FactCategory, FactWebsite, FactAddress, FactPhone, FactEmail, FactPeople, FactKnownSince, FactEIN, FactFleet}
}

// Orders returns independent storage for the nine fact orders.
func Orders() []Order {
	return []Order{
		{FactName, []Slot{{[]string{Owner}, ValueName}, {[]string{"places"}, ValueName}, {[]string{"fmcsa"}, ValueTradeNames}, {[]string{"form5500"}, ValueTradeNames}, {[]string{"fmcsa", "ppp", "form5500", "osha_ita"}, ValueNames}}},
		{FactAddress, []Slot{{[]string{Owner}, ValueLocation}, {[]string{Route}, ValueLocation}, {[]string{"places"}, ValueLocation}, {[]string{"osha_ita"}, ValueLocation}, {[]string{"form5500"}, ValueLocation}, {[]string{"fmcsa"}, ValueLocation}, {[]string{"ppp"}, ValueLocation}, {[]string{"form5500"}, ValueMailing}}},
		{FactPhone, []Slot{{[]string{Owner}, ValuePhone}, {[]string{"places"}, ValuePhone}, {[]string{"form5500"}, ValuePhone}, {[]string{"fmcsa"}, ValuePhone}}},
		{FactEmail, []Slot{{[]string{Owner}, ValueEmail}, {[]string{"fmcsa"}, ValueEmail}}},
		{FactPeople, []Slot{{[]string{Owner}, ValuePeople}, {[]string{Brief}, ValuePeople}, {[]string{"form5500"}, ValuePeople}, {[]string{"fmcsa"}, ValuePeople}}},
		{FactKnownSince, []Slot{{[]string{Owner}, ValueKnownSince}, {[]string{Brief}, ValueKnownSince}, {[]string{"fmcsa", "ppp", "form5500", "osha_ita", "places"}, ValueKnownSince}}},
		{FactNewestFiling, []Slot{{[]string{"fmcsa", "ppp", "form5500", "osha_ita", "places"}, ValueFiled}}},
		{FactEIN, []Slot{{[]string{Owner}, ValueEIN}, {[]string{"form5500", "osha_ita"}, ValueEIN}}},
		{FactFleet, []Slot{{[]string{Owner}, ValuePowerUnits}, {[]string{"fmcsa"}, ValuePowerUnits}}},
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
	ein := pickValues[string](orders, records, FactEIN)
	units := pickValues[int64](orders, records, FactFleet)
	p := Picked{OtherNames: []Sourced[string]{}, OtherAddresses: []Sourced[Address]{}, OtherPhones: []Sourced[string]{}, OtherEmails: []Sourced[string]{}, People: people}
	p.Name, p.OtherNames = firstAndRest(names)
	p.Address, p.OtherAddresses = firstAndRest(addresses)
	p.Phone, p.OtherPhones = firstAndRest(phones)
	p.Email, p.OtherEmails = firstAndRest(emails)
	p.KnownSince, p.OtherKnownSince = firstAndRest(known)
	p.EIN, _ = firstAndRest(ein)
	p.PowerUnits, p.OtherPowerUnits = firstAndRest(units)
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
	case int64:
		return x == any(b).(int64)
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
			if !takes(fact, s.Value) {
				continue
			}
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
			if fact == FactKnownSince && !slices.Contains(s.Sources, Owner) {
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
func takes(fact, value string) bool {
	switch fact {
	case FactName, FactPhone, FactEmail:
		return value == ValueName || value == ValueNames || value == ValueTradeNames || value == ValuePhone || value == ValueEmail
	case FactAddress:
		return value == ValueLocation || value == ValueMailing
	case FactPeople:
		return value == ValuePeople
	case FactKnownSince, FactNewestFiling:
		return value == ValueKnownSince || value == ValueFiled
	case FactEIN:
		return value == ValueEIN
	case FactFleet:
		return value == ValuePowerUnits
	}
	return false
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
	case ValueEIN:
		addStrings([]string{r.EINKey})
	case ValuePowerUnits:
		if r.PowerUnits != nil {
			out = append(out, *r.PowerUnits)
		}
	case ValueFiled:
		if !r.Filed.IsZero() {
			out = append(out, r.Filed)
		}
	}
	return out
}
