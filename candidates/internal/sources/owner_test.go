package sources_test

import (
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/sources"
)

// R-TV9D-PTV3 R-TWHA-3LLS R-TXP6-HDCH
func TestOwnerFactWords(t *testing.T) {
	type word string
	got := []word{sources.FactName, sources.FactHeadcount, sources.FactRevenue, sources.FactCategory, sources.FactWebsite, sources.FactAddress, sources.FactPhone, sources.FactEmail, sources.FactPeople, sources.FactKnownSince, sources.FactEIN, sources.FactFleet, sources.ValueEIN, sources.ValuePowerUnits}
	want := []word{"name", "headcount", "revenue", "category", "website", "address", "phone", "email", "people", "known_since", "ein", "fleet", "ein", "power_units"}
	if !slices.Equal(got, want) {
		t.Fatal(got)
	}
	facts := sources.SettableFacts()
	if !slices.Equal(facts, []string{sources.FactName, sources.FactHeadcount, sources.FactRevenue, sources.FactCategory, sources.FactWebsite, sources.FactAddress, sources.FactPhone, sources.FactEmail, sources.FactPeople, sources.FactKnownSince, sources.FactEIN, sources.FactFleet}) {
		t.Fatal(facts)
	}
	facts[0] = "changed"
	if sources.SettableFacts()[0] != sources.FactName {
		t.Fatal("shared facts")
	}
}

// R-0SZK-R9PA R-0U7H-51FZ
func TestOwnerEvidenceDescription(t *testing.T) {
	const description string = sources.DescribeOwnerRecord
	if description == "" || strings.Contains(description, "\n") {
		t.Fatal(description)
	}
	for _, other := range []string{sources.DescribeFMCSAActive, sources.DescribeFMCSAActivity, sources.DescribePPPLoan, sources.DescribeForm5500Filing, sources.DescribeOSHAITASummary, sources.DescribeOwnDomain, sources.DescribeWebsiteLive} {
		if description == other {
			t.Fatal(description)
		}
	}
}

// R-55D5-TMN2 R-56L2-7EDR R-U3SO-E81Y
func TestOwnerPickedValuesAndAlternatives(t *testing.T) {
	units := int64(12)
	original := sources.Record{Source: "fmcsa", SourceID: "f", Reading: sources.Reading{Name: "Business", Street: "Street", Phone: "phone", Email: "email", People: []sources.Person{{Name: "Officer"}}, KnownSince: on("1995-01-01"), Filed: on("2025-01-01"), PowerUnits: &units}}
	ein := sources.Record{Source: "form5500", SourceID: "x", Reading: sources.Reading{EINKey: "411234567", Filed: on("2025-06-12")}}
	ownerUnits := int64(9)
	owner := sources.Record{Source: sources.Owner, SourceID: "a", Reading: sources.Reading{Name: "Owned name", Street: "Owned street", Phone: "owned phone", Email: "owned email", People: []sources.Person{{Name: "Owner", Role: "president"}}, KnownSince: on("2001-01-01"), EINKey: "419876543", PowerUnits: &ownerUnits, Filed: on("2026-03-01")}}
	older := owner
	older.SourceID = "b"
	older.Reading.KnownSince = on("1990-01-01")
	older.Reading.Filed = on("2026-02-01")
	orders := sources.Orders()
	links := []sources.Record{original, ein}
	given := []sources.Record{older, owner}
	got := sources.Pick(orders, links, given)
	if got.Name.Value != owner.Reading.Name || got.Name.Source != sources.Owner || got.Name.Key != "owner:a" || len(got.OtherNames) != 1 || got.OtherNames[0].Value != original.Reading.Name {
		t.Fatal(got)
	}
	if got.Address.Value.Street != owner.Reading.Street || got.Phone.Value != owner.Reading.Phone || got.Email.Value != owner.Reading.Email || got.People[0].Value != owner.Reading.People[0] {
		t.Fatal(got)
	}
	if !got.KnownSince.Value.Equal(owner.Reading.KnownSince) || !reflect.DeepEqual(got.OtherKnownSince, []sources.Sourced[time.Time]{{older.Reading.KnownSince, sources.Owner, "owner:b"}, {original.Reading.KnownSince, "fmcsa", "fmcsa:f"}}) {
		t.Fatal(got)
	}
	if got.EIN != (sources.Sourced[string]{"419876543", sources.Owner, "owner:a"}) || got.PowerUnits != (sources.Sourced[int64]{9, sources.Owner, "owner:a"}) || !reflect.DeepEqual(got.OtherPowerUnits, []sources.Sourced[int64]{{12, "fmcsa", "fmcsa:f"}}) || got.NewestFiling.Source != "form5500" {
		t.Fatal(got)
	}
	// Equal timestamps use the lesser owner id, including for known-since.
	given[0].Reading.Filed = owner.Reading.Filed
	if p := sources.Pick(orders, links, given); p.KnownSince.Key != "owner:a" {
		t.Fatal(p)
	}
	got.OtherKnownSince[0].Value = time.Time{}
	got.OtherPowerUnits[0].Value = 0
	if p := sources.Pick(orders, links, given); p.OtherKnownSince[0].Value.IsZero() || p.OtherPowerUnits[0].Value != 12 {
		t.Fatal("shared results")
	}
}

// R-55D5-TMN2 R-56L2-7EDR
func TestEINAndFleetPickNewestNonempty(t *testing.T) {
	a, b := int64(12), int64(14)
	links := []sources.Record{
		{Source: "x", SourceID: "1", Reading: sources.Reading{EINKey: "411234567", PowerUnits: &a, Filed: on("2025-01-01")}},
		{Source: "x", SourceID: "2", Reading: sources.Reading{EINKey: "410000001", PowerUnits: &b, Filed: on("2024-01-01")}},
		{Source: "x", SourceID: "3", Reading: sources.Reading{PowerUnits: &a, Filed: on("2023-01-01")}},
		{Source: "x", SourceID: "4", Reading: sources.Reading{Filed: on("2026-01-01")}},
	}
	orders := []sources.Order{{sources.FactEIN, []sources.Slot{{[]string{"x"}, sources.ValueEIN}}}, {sources.FactFleet, []sources.Slot{{[]string{"x"}, sources.ValuePowerUnits}}}}
	p := sources.Pick(orders, links, nil)
	if p.EIN.Key != "x:1" || p.PowerUnits.Value != 12 || !reflect.DeepEqual(p.OtherPowerUnits, []sources.Sourced[int64]{{14, "x", "x:2"}}) {
		t.Fatal(p)
	}
	links[0].Reading.Phone = "wrong value kind"
	orders[0].Slots[0].Value = sources.ValuePhone
	orders[1].Slots[0].Value = sources.ValueFiled
	p = sources.Pick(orders, links, nil)
	if p.EIN != (sources.Sourced[string]{}) || p.PowerUnits != (sources.Sourced[int64]{}) || p.OtherPowerUnits == nil {
		t.Fatal(p)
	}
}

// R-55D5-TMN2 R-56L2-7EDR
func TestStringFactsRejectEINValueKind(t *testing.T) {
	for _, fact := range []string{sources.FactName, sources.FactPhone, sources.FactEmail} {
		p := sources.Pick([]sources.Order{{fact, []sources.Slot{{[]string{"x"}, sources.ValueEIN}}}}, []sources.Record{{Source: "x", Reading: sources.Reading{EINKey: "411234567"}}}, nil)
		if p.Name.Source != "" || p.Phone.Source != "" || p.Email.Source != "" {
			t.Fatal(p)
		}
	}
}
