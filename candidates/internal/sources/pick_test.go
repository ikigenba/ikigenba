package sources_test

import (
	"encoding/json"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/fmcsa"
	"github.com/ikigenba/ikigenba/candidates/internal/form5500"
	"github.com/ikigenba/ikigenba/candidates/internal/oshaita"
	"github.com/ikigenba/ikigenba/candidates/internal/places"
	"github.com/ikigenba/ikigenba/candidates/internal/ppp"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
)

// R-TV9D-PTV3 R-5459-FUWD R-29KI-F542 R-U04Z-8WTV
func TestFactOrders(t *testing.T) {
	type word string
	words := []word{sources.Owner, sources.Brief, sources.Route, sources.FactName, sources.FactAddress, sources.FactPhone, sources.FactEmail, sources.FactPeople, sources.FactKnownSince, sources.FactNewestFiling, sources.ValueName, sources.ValueNames, sources.ValueTradeNames, sources.ValueLocation, sources.ValueMailing, sources.ValuePhone, sources.ValueEmail, sources.ValuePeople, sources.ValueKnownSince, sources.ValueFiled}
	wantWords := []word{"owner", "brief", "route", "name", "address", "phone", "email", "people", "known_since", "newest_filing", "name", "names", "trade_names", "location", "mailing", "phone", "email", "people", "known_since", "filed"}
	if !slices.Equal(words, wantWords) {
		t.Fatal(words)
	}
	orders := sources.Orders
	pick := sources.Pick
	p := pick(nil, nil, nil)
	// Positional construction proves the complete declared fields, including order.
	zero := sources.Picked{sources.Sourced[string]{"", "", ""}, []sources.Sourced[string]{}, sources.Sourced[sources.Address]{sources.Address{"", "", "", "", ""}, "", ""}, []sources.Sourced[sources.Address]{}, sources.Sourced[string]{"", "", ""}, []sources.Sourced[string]{}, sources.Sourced[string]{"", "", ""}, []sources.Sourced[string]{}, []sources.Sourced[sources.Person]{}, sources.Sourced[time.Time]{time.Time{}, "", ""}, []sources.Sourced[time.Time]{}, sources.Sourced[time.Time]{time.Time{}, "", ""}, sources.Sourced[string]{}, sources.Sourced[int64]{}, []sources.Sourced[int64]{}}
	if !reflect.DeepEqual(p, zero) {
		t.Fatal(p)
	}
	r := sources.Record{"a", "b", true, time.Unix(1, 0), sources.Reading{Name: "name"}}
	if r.Source != "a" || r.SourceID != "b" || !r.Gone || !r.Seen.Equal(time.Unix(1, 0)) || r.Reading.Name != "name" {
		t.Fatal(r)
	}
	want := []sources.Order{
		{sources.FactName, []sources.Slot{{[]string{sources.Owner}, sources.ValueName}, {[]string{places.Name}, sources.ValueName}, {[]string{fmcsa.Name}, sources.ValueTradeNames}, {[]string{form5500.Name}, sources.ValueTradeNames}, {[]string{fmcsa.Name, ppp.Name, form5500.Name, oshaita.Name}, sources.ValueNames}}},
		{sources.FactAddress, []sources.Slot{{[]string{sources.Owner}, sources.ValueLocation}, {[]string{sources.Route}, sources.ValueLocation}, {[]string{places.Name}, sources.ValueLocation}, {[]string{oshaita.Name}, sources.ValueLocation}, {[]string{form5500.Name}, sources.ValueLocation}, {[]string{fmcsa.Name}, sources.ValueLocation}, {[]string{ppp.Name}, sources.ValueLocation}, {[]string{form5500.Name}, sources.ValueMailing}}},
		{sources.FactPhone, []sources.Slot{{[]string{sources.Owner}, sources.ValuePhone}, {[]string{places.Name}, sources.ValuePhone}, {[]string{form5500.Name}, sources.ValuePhone}, {[]string{fmcsa.Name}, sources.ValuePhone}}},
		{sources.FactEmail, []sources.Slot{{[]string{sources.Owner}, sources.ValueEmail}, {[]string{fmcsa.Name}, sources.ValueEmail}}},
		{sources.FactPeople, []sources.Slot{{[]string{sources.Owner}, sources.ValuePeople}, {[]string{sources.Brief}, sources.ValuePeople}, {[]string{form5500.Name}, sources.ValuePeople}, {[]string{fmcsa.Name}, sources.ValuePeople}}},
		{sources.FactKnownSince, []sources.Slot{{[]string{sources.Owner}, sources.ValueKnownSince}, {[]string{sources.Brief}, sources.ValueKnownSince}, {[]string{fmcsa.Name, ppp.Name, form5500.Name, oshaita.Name, places.Name}, sources.ValueKnownSince}}},
		{sources.FactNewestFiling, []sources.Slot{{[]string{fmcsa.Name, ppp.Name, form5500.Name, oshaita.Name, places.Name}, sources.ValueFiled}}},
		{sources.FactEIN, []sources.Slot{{[]string{sources.Owner}, sources.ValueEIN}, {[]string{form5500.Name, oshaita.Name}, sources.ValueEIN}}},
		{sources.FactFleet, []sources.Slot{{[]string{sources.Owner}, sources.ValuePowerUnits}, {[]string{fmcsa.Name}, sources.ValuePowerUnits}}},
	}
	if !reflect.DeepEqual(orders(), want) {
		t.Fatal(orders())
	}
	for _, change := range []func([]sources.Order){func(o []sources.Order) { o[0].Fact = "changed" }, func(o []sources.Order) { o[0].Slots[0].Value = "changed" }, func(o []sources.Order) { o[0].Slots[0].Sources[0] = "changed" }} {
		o := orders()
		change(o)
		if !reflect.DeepEqual(orders(), want) {
			t.Fatal("orders share storage")
		}
	}
}
func on(s string) time.Time { t, _ := time.Parse(time.DateOnly, s); return t }

// R-U3SO-E81Y R-55D5-TMN2 R-56L2-7EDR
func TestPickedFactsFromIndependentOrders(t *testing.T) {
	p := sources.Record{Source: places.Name, SourceID: "ChIJ1", Reading: sources.Reading{Name: "Northern Lights Steel", Street: "38945 Forest Blvd", City: "North Branch", State: "MN", Zip: "55056", Phone: "6516744440"}}
	f := sources.Record{Source: fmcsa.Name, SourceID: "1023916", Reading: sources.Reading{Name: "BROWN HAWK LLC", Names: []string{"BROWN HAWK LLC", "NORTHERN LIGHTS STEEL FAB"}, TradeNames: []string{"NORTHERN LIGHTS STEEL FAB"}, Street: "5 Main St", City: "Rush City", State: "MN", Zip: "55069", Phone: "3205550100", Email: "ops@nlsteelfab.com", People: []sources.Person{{Name: "JANE DOE", Role: "company officer"}}, KnownSince: on("2004-01-15"), Filed: on("2025-03-18")}}
	x := sources.Record{Source: form5500.Name, SourceID: "20250612001", Reading: sources.Reading{Name: "BROWN HAWK LLC", Names: []string{"BROWN HAWK LLC"}, Street: "PO BOX 7", City: "Rush City", State: "MN", Zip: "55069", Mailing: true, Phone: "3205550111", People: []sources.Person{{Name: "JOHN ROE", Role: "sponsor signer"}}, KnownSince: on("1995-01-01"), Filed: on("2025-06-12")}}
	want := sources.Picked{
		Name: sources.Sourced[string]{"Northern Lights Steel", places.Name, "places:ChIJ1"}, OtherNames: []sources.Sourced[string]{{"NORTHERN LIGHTS STEEL FAB", fmcsa.Name, "fmcsa:1023916"}, {"BROWN HAWK LLC", form5500.Name, "form5500:20250612001"}},
		Address: sources.Sourced[sources.Address]{sources.Address{Street: "38945 Forest Blvd", City: "North Branch", State: "MN", Zip: "55056"}, places.Name, "places:ChIJ1"}, OtherAddresses: []sources.Sourced[sources.Address]{{sources.Address{Street: "5 Main St", City: "Rush City", State: "MN", Zip: "55069"}, fmcsa.Name, "fmcsa:1023916"}, {sources.Address{Street: "PO BOX 7", City: "Rush City", State: "MN", Zip: "55069"}, form5500.Name, "form5500:20250612001"}},
		Phone: sources.Sourced[string]{"6516744440", places.Name, "places:ChIJ1"}, OtherPhones: []sources.Sourced[string]{{"3205550111", form5500.Name, "form5500:20250612001"}, {"3205550100", fmcsa.Name, "fmcsa:1023916"}}, Email: sources.Sourced[string]{"ops@nlsteelfab.com", fmcsa.Name, "fmcsa:1023916"}, OtherEmails: []sources.Sourced[string]{},
		People: []sources.Sourced[sources.Person]{{sources.Person{"JOHN ROE", "sponsor signer"}, form5500.Name, "form5500:20250612001"}, {sources.Person{"JANE DOE", "company officer"}, fmcsa.Name, "fmcsa:1023916"}}, KnownSince: sources.Sourced[time.Time]{on("1995-01-01"), form5500.Name, "form5500:20250612001"}, OtherKnownSince: []sources.Sourced[time.Time]{{on("2004-01-15"), fmcsa.Name, "fmcsa:1023916"}}, NewestFiling: sources.Sourced[time.Time]{on("2025-06-12"), form5500.Name, "form5500:20250612001"}, OtherPowerUnits: []sources.Sourced[int64]{},
	}
	links := []sources.Record{p, f, x}
	orders := sources.Orders()
	before, _ := json.Marshal([]any{orders, links})
	got := sources.Pick(orders, links, nil)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v want %+v", got, want)
	}
	after, _ := json.Marshal([]any{orders, links})
	if string(before) != string(after) {
		t.Fatal("input changed")
	}
	got.OtherNames[0].Value = "changed"
	got.People[0].Value.Name = "changed"
	if !reflect.DeepEqual(sources.Pick(orders, links, nil), want) {
		t.Fatal("shared result storage")
	}
	got = sources.Pick(orders, []sources.Record{f, x}, nil)
	if got.Name.Value != "NORTHERN LIGHTS STEEL FAB" || got.Name.Source != fmcsa.Name || got.Address.Source != fmcsa.Name || got.Phone.Source != form5500.Name {
		t.Fatal(got)
	}
	got = sources.Pick(orders, []sources.Record{x}, nil)
	if got.Name.Value != "BROWN HAWK LLC" || got.Address.Value.Street != "PO BOX 7" || got.Address.Source != form5500.Name {
		t.Fatal(got)
	}
	route := sources.Record{Source: sources.Route, SourceID: "res1", Reading: sources.Reading{Street: "38955 Forest Blvd", City: "North Branch", State: "MN", Zip: "55056"}}
	given := []sources.Record{route}
	before, _ = json.Marshal(given)
	got = sources.Pick(orders, links, given)
	if got.Address != (sources.Sourced[sources.Address]{routeAddress(route), sources.Route, "route:res1"}) || got.OtherAddresses[0] != want.Address {
		t.Fatal(got)
	}
	after, _ = json.Marshal(given)
	if string(before) != string(after) {
		t.Fatal("given changed")
	}
}
func routeAddress(r sources.Record) sources.Address {
	return sources.Address{Street: r.Reading.Street, City: r.Reading.City, State: r.Reading.State, Zip: r.Reading.Zip, County: r.Reading.County}
}

// R-55D5-TMN2 R-56L2-7EDR
func TestCustomOrdersAndGoneFallback(t *testing.T) {
	orders := []sources.Order{{sources.FactPhone, []sources.Slot{{[]string{"x"}, sources.ValuePhone}, {[]string{"y"}, sources.ValuePhone}}}}
	links := []sources.Record{{Source: "y", SourceID: "1", Reading: sources.Reading{Phone: "6515550100", Filed: on("2025-01-01")}}, {Source: "x", SourceID: "1", Reading: sources.Reading{Phone: "6515550199", Filed: on("2020-01-01")}}, {Source: "x", SourceID: "2", Reading: sources.Reading{Phone: "6515550100", Filed: on("2024-01-01")}}}
	want := sources.Picked{Phone: sources.Sourced[string]{"6515550100", "x", "x:2"}, OtherPhones: []sources.Sourced[string]{{"6515550199", "x", "x:1"}}, OtherNames: []sources.Sourced[string]{}, OtherAddresses: []sources.Sourced[sources.Address]{}, OtherEmails: []sources.Sourced[string]{}, People: []sources.Sourced[sources.Person]{}, OtherKnownSince: []sources.Sourced[time.Time]{}, OtherPowerUnits: []sources.Sourced[int64]{}}
	if got := sources.Pick(orders, links, nil); !reflect.DeepEqual(got, want) {
		t.Fatal(got)
	}
	links[2].Gone = true
	got := sources.Pick(orders, links, nil)
	if got.Phone.Key != "x:1" || len(got.OtherPhones) != 1 || got.OtherPhones[0].Key != "y:1" {
		t.Fatal(got)
	}
	for i := range links {
		links[i].Gone = true
	}
	if got := sources.Pick(orders, links, nil); !reflect.DeepEqual(got, want) {
		t.Fatal(got)
	}
	// Given values are considered even when gone; first order only, unknown facts ignored.
	given := []sources.Record{{Source: "x", SourceID: "0", Gone: true, Reading: sources.Reading{Phone: "given", Filed: on("2030-01-01")}}}
	got = sources.Pick(append(orders, sources.Order{Fact: sources.FactPhone}), links, given)
	if got.Phone.Value != "given" {
		t.Fatal(got)
	}
	orders[0].Slots[0].Value = sources.ValueMailing
	orders[0].Slots[1].Value = "unknown"
	if got := sources.Pick(orders, links, nil); got.Phone.Value != "" || len(got.OtherPhones) != 0 {
		t.Fatal(got)
	}
}

// R-55D5-TMN2 R-56L2-7EDR
func TestRecordOrderAndValueKinds(t *testing.T) {
	// Filing date, source, source id, then original order decide the provenance; Seen decides nothing.
	base := sources.Record{Source: "x", SourceID: "1", Seen: on("2025-01-01"), Reading: sources.Reading{Name: "base", Filed: on("2025-01-01")}}
	order := []sources.Order{{sources.FactName, []sources.Slot{{[]string{"x", "y"}, sources.ValueName}}}}
	for _, tc := range []struct {
		r    sources.Record
		want string
	}{
		{sources.Record{Source: "x", SourceID: "2", Seen: on("2026-01-01"), Reading: sources.Reading{Name: "newer", Filed: on("2026-01-01")}}, "newer"},
		{sources.Record{Source: "x", SourceID: "2", Seen: on("2026-01-01"), Reading: sources.Reading{Name: "seen", Filed: base.Reading.Filed}}, "base"},
		{sources.Record{Source: "y", SourceID: "0", Seen: on("2030-01-01"), Reading: sources.Reading{Name: "y", Filed: base.Reading.Filed}}, "base"},
		{sources.Record{Source: "x", SourceID: "0", Seen: on("2020-01-01"), Reading: sources.Reading{Name: "id", Filed: base.Reading.Filed}}, "id"},
		{sources.Record{Source: "x", SourceID: "1", Seen: on("2030-01-01"), Reading: sources.Reading{Name: "tie", Filed: base.Reading.Filed}}, "base"},
	} {
		for _, reverse := range []bool{false, true} {
			rs := []sources.Record{base, tc.r}
			want := tc.want
			if reverse {
				slices.Reverse(rs)
				if tc.r.SourceID == base.SourceID && tc.r.Source == base.Source {
					want = "tie"
				}
			}
			if got := sources.Pick(order, rs, nil).Name.Value; got != want {
				t.Fatal(got, want)
			}
		}
	}
	r := sources.Record{Source: "x", SourceID: "1", Reading: sources.Reading{Name: "name", Names: []string{"", "name", "alias", "alias"}, TradeNames: []string{"", "trade", "trade"}, Street: "5 Main St", County: "A", MailingAddress: &sources.Address{Street: "PO BOX 7"}, People: []sources.Person{{"JANE DOE", "company officer"}, {"JANE DOE", "company officer"}, {"JANE DOE", "sponsor signer"}, {"", "empty"}}}}
	orders := []sources.Order{{sources.FactName, []sources.Slot{{[]string{"x"}, sources.ValueTradeNames}, {[]string{"x"}, sources.ValueNames}}}, {sources.FactAddress, []sources.Slot{{[]string{"x"}, sources.ValueLocation}, {[]string{"x"}, sources.ValueMailing}}}, {sources.FactPeople, []sources.Slot{{[]string{"x"}, sources.ValuePeople}}}}
	got := sources.Pick(orders, []sources.Record{r}, nil)
	if got.Name.Value != "trade" || len(got.OtherNames) != 2 || got.OtherNames[0].Value != "name" || got.OtherNames[1].Value != "alias" || got.Address.Value.Street != "5 Main St" || len(got.OtherAddresses) != 1 || got.OtherAddresses[0].Value.Street != "PO BOX 7" || len(got.People) != 2 || got.People[1].Value.Role != "sponsor signer" {
		t.Fatal(got)
	}
	r.Reading.Mailing = true
	r.Reading.MailingAddress = &sources.Address{Street: r.Reading.Street, County: r.Reading.County}
	got = sources.Pick(orders, []sources.Record{r}, nil)
	if got.Address.Value.Street != "5 Main St" || len(got.OtherAddresses) != 0 {
		t.Fatal(got)
	}
	r.Reading.Street = ""
	r.Reading.MailingAddress = &sources.Address{County: "only county"}
	if got = sources.Pick(orders, []sources.Record{r}, nil); got.Address.Value != (sources.Address{}) {
		t.Fatal(got)
	}
}

// R-55D5-TMN2 R-56L2-7EDR
func TestDateSlotsAndEquality(t *testing.T) {
	o := []sources.Order{{sources.FactKnownSince, []sources.Slot{{[]string{"o"}, sources.ValueKnownSince}, {[]string{"x", "y"}, sources.ValueKnownSince}}}, {sources.FactNewestFiling, []sources.Slot{{[]string{"x", "y"}, sources.ValueFiled}}}}
	links := []sources.Record{{Source: "x", SourceID: "1", Reading: sources.Reading{KnownSince: on("2004-01-15"), Filed: on("2025-03-18")}}, {Source: "y", SourceID: "1", Reading: sources.Reading{KnownSince: on("1995-01-01"), Filed: on("2020-01-01")}}}
	p := sources.Pick(o, links, nil)
	if p.KnownSince.Source != "y" || !p.KnownSince.Value.Equal(on("1995-01-01")) || p.NewestFiling.Source != "x" {
		t.Fatal(p)
	}
	given := []sources.Record{{Source: "o", SourceID: "1", Reading: sources.Reading{KnownSince: on("2010-06-01")}}}
	if got := sources.Pick(o, links, given).KnownSince; got.Source != "o" || !got.Value.Equal(on("2010-06-01")) {
		t.Fatal(got)
	}
	// String facts accept each string value kind, time facts each time value kind.
	for _, fact := range []string{sources.FactName, sources.FactPhone, sources.FactEmail} {
		for _, kind := range []string{sources.ValueName, sources.ValueNames, sources.ValueTradeNames, sources.ValuePhone, sources.ValueEmail} {
			r := sources.Record{Source: "x", Reading: sources.Reading{Name: "v", Names: []string{"v"}, TradeNames: []string{"v"}, Phone: "v", Email: "v"}}
			p := sources.Pick([]sources.Order{{fact, []sources.Slot{{[]string{"x"}, kind}}}}, []sources.Record{r}, nil)
			got := map[string]string{sources.FactName: p.Name.Value, sources.FactPhone: p.Phone.Value, sources.FactEmail: p.Email.Value}[fact]
			if got != "v" {
				t.Fatal(fact, kind, p)
			}
		}
	}
	for _, fact := range []string{sources.FactKnownSince, sources.FactNewestFiling} {
		for _, kind := range []string{sources.ValueKnownSince, sources.ValueFiled} {
			p := sources.Pick([]sources.Order{{fact, []sources.Slot{{[]string{"x"}, kind}}}}, links, nil)
			if fact == sources.FactKnownSince && p.KnownSince.Value.IsZero() || fact == sources.FactNewestFiling && p.NewestFiling.Value.IsZero() {
				t.Fatal(fact, kind, p)
			}
		}
	}
}

// R-55D5-TMN2 R-56L2-7EDR
func TestDuplicateIdentityKeepsLinkThenGivenOrder(t *testing.T) {
	order := []sources.Order{{sources.FactName, []sources.Slot{{[]string{"x"}, sources.ValueName}}}}
	record := func(name, seen string) sources.Record {
		return sources.Record{Source: "x", SourceID: "1", Seen: on(seen), Reading: sources.Reading{Name: name, Filed: on("2024-01-01")}}
	}
	a := record("first", "2020-01-01")
	b := record("second", "2030-01-01")
	c := record("third", "2040-01-01")
	for _, tc := range []struct {
		name         string
		links, given []sources.Record
		want         []string
	}{
		{"links before given", []sources.Record{a, b}, []sources.Record{c}, []string{"first", "second", "third"}},
		{"reversed links", []sources.Record{b, a}, []sources.Record{c}, []string{"second", "first", "third"}},
		{"given order", nil, []sources.Record{a, b, c}, []string{"first", "second", "third"}},
		{"reversed given", nil, []sources.Record{c, b, a}, []string{"third", "second", "first"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := sources.Pick(order, tc.links, tc.given)
			values := []string{got.Name.Value}
			for _, other := range got.OtherNames {
				values = append(values, other.Value)
			}
			if !slices.Equal(values, tc.want) {
				t.Fatalf("got %v, want %v", values, tc.want)
			}
		})
	}
}

// R-W5FA-5MY8
func TestSameFilingChoosesSourceIDRegardlessOfSeen(t *testing.T) {
	orders := []sources.Order{{sources.FactName, []sources.Slot{{[]string{"x"}, sources.ValueName}}}}
	record := func(id, name, seen string) sources.Record {
		return sources.Record{Source: "x", SourceID: id, Seen: on(seen), Reading: sources.Reading{Name: name, Filed: on("2024-01-01")}}
	}
	wantName := sources.Sourced[string]{Value: "ALPHA FREIGHT", Source: "x", Key: "x:1"}
	wantOthers := []sources.Sourced[string]{{Value: "BETA FREIGHT", Source: "x", Key: "x:2"}, {Value: "GAMMA FREIGHT", Source: "x", Key: "x:3"}}
	for _, links := range [][]sources.Record{
		{record("3", "GAMMA FREIGHT", "2025-03-01"), record("1", "ALPHA FREIGHT", "2025-01-01"), record("2", "BETA FREIGHT", "2025-02-01")},
		{record("2", "BETA FREIGHT", "2025-02-01"), record("3", "GAMMA FREIGHT", "2025-01-01"), record("1", "ALPHA FREIGHT", "2025-03-01")},
	} {
		got := sources.Pick(orders, links, nil)
		if got.Name != wantName || !reflect.DeepEqual(got.OtherNames, wantOthers) {
			t.Fatalf("got name %+v and alternatives %+v", got.Name, got.OtherNames)
		}
	}
}

// R-W2ZH-E3GU
func TestPickInvariantUnderPermutationsAndSeen(t *testing.T) {
	group := []string{"x", "y", "z"}
	orders := []sources.Order{
		{sources.FactName, []sources.Slot{{group, sources.ValueNames}, {group, sources.ValueTradeNames}}},
		{sources.FactAddress, []sources.Slot{{group, sources.ValueLocation}, {group, sources.ValueMailing}}},
		{sources.FactPhone, []sources.Slot{{group, sources.ValuePhone}}},
		{sources.FactEmail, []sources.Slot{{group, sources.ValueEmail}}},
		{sources.FactPeople, []sources.Slot{{group, sources.ValuePeople}}},
		{sources.FactKnownSince, []sources.Slot{{group, sources.ValueKnownSince}}},
		{sources.FactNewestFiling, []sources.Slot{{group, sources.ValueFiled}}},
	}
	record := func(source, id, value string) sources.Record {
		return sources.Record{Source: source, SourceID: id, Seen: on("2025-01-01"), Reading: sources.Reading{
			Name: value, Names: []string{value, "shared alias"}, TradeNames: []string{"trade " + value},
			Street: value, MailingAddress: &sources.Address{Street: "mail " + value}, Phone: "phone " + value, Email: "mail@" + value,
			People:     []sources.Person{{Name: value, Role: "officer"}, {Name: "shared person", Role: "signer"}},
			KnownSince: on("2000-01-01"), Filed: on("2024-01-01"),
		}}
	}
	links := []sources.Record{record("y", "1", "beta"), record("x", "2", "gamma"), record("x", "1", "alpha")}
	given := []sources.Record{record("z", "1", "delta"), record("y", "2", "alpha")}
	given[0].Gone = true
	given[1].Reading.Filed = on("2023-01-01")
	given[1].Reading.KnownSince = on("1990-01-01")
	for _, gone := range []string{"none", "some", "all"} {
		for i := range links {
			links[i].Gone = gone == "all" || gone == "some" && i == 0
		}
		want := sources.Pick(orders, links, given)
		for li, l := range recordPermutations(links) {
			for gi, g := range recordPermutations(given) {
				for _, seen := range []string{"unchanged", "ascending", "descending", "zero"} {
					l, g := slices.Clone(l), slices.Clone(g)
					for i := range l {
						l[i].Seen = permutedSeen(l[i].Seen, seen, i)
					}
					for i := range g {
						g[i].Seen = permutedSeen(g[i].Seen, seen, i+len(l))
					}
					if got := sources.Pick(orders, l, g); !reflect.DeepEqual(got, want) {
						t.Fatalf("gone=%s links=%d given=%d seen=%s: got %+v, want %+v", gone, li, gi, seen, got, want)
					}
				}
			}
		}
	}
}

func recordPermutations(records []sources.Record) [][]sources.Record {
	permutations := [][]sources.Record{}
	var visit func(int)
	items := slices.Clone(records)
	visit = func(at int) {
		if at == len(items) {
			permutations = append(permutations, slices.Clone(items))
			return
		}
		for i := at; i < len(items); i++ {
			items[at], items[i] = items[i], items[at]
			visit(at + 1)
			items[at], items[i] = items[i], items[at]
		}
	}
	visit(0)
	return permutations
}

func permutedSeen(original time.Time, mode string, at int) time.Time {
	switch mode {
	case "ascending":
		return time.Unix(int64(at+1), 0)
	case "descending":
		return time.Unix(-int64(at+1), 0)
	case "zero":
		return time.Time{}
	default:
		return original
	}
}
