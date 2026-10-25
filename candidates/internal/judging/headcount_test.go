package judging_test

import (
	"encoding/json"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/judging"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
)

// R-4FUZ-II7O R-4IAS-A1P2
func TestHeadcountExports(t *testing.T) {
	const wordsRequired = judging.HeadcountSource + judging.BandConstructionTrades + judging.BandConstructionHeavySite + judging.BandConstruction + judging.BandTruckingPrivateMixed + judging.BandManufacturingJobShop + judging.BandManufacturingProductsEquipment + judging.BandManufacturingBuildingProducts + judging.BandManufacturingFoodBeverage + judging.BandManufacturingOther + judging.BandWholesaleDistribution + judging.BandMiningAggregates + judging.BandAgricultureOther + judging.BandWasteEnvironmental + judging.BandOther + judging.ChosenByLabel + judging.ChosenByCategories + judging.ChosenByNone
	if wordsRequired == "" {
		t.Fatal(wordsRequired)
	}
	type band struct {
		Name      string
		Low, High int64
	}
	type choice struct {
		Band               judging.Band
		By, Label, Keyword string
		Categories         []string
	}
	type staff struct {
		Low, High  int64
		Confidence float64
		Basis      string
		Used       []sources.Headcount
		AsOf       time.Time
	}
	if band(judging.Band{}) != (band{}) || !reflect.DeepEqual(choice(judging.Choice{}), choice{}) || !reflect.DeepEqual(staff(judging.Staff{}), staff{}) {
		t.Fatal("shapes")
	}
	words := []string{judging.HeadcountSource, judging.BandConstructionTrades, judging.BandConstructionHeavySite, judging.BandConstruction, judging.BandTruckingPrivateMixed, judging.BandManufacturingJobShop, judging.BandManufacturingProductsEquipment, judging.BandManufacturingBuildingProducts, judging.BandManufacturingFoodBeverage, judging.BandManufacturingOther, judging.BandWholesaleDistribution, judging.BandMiningAggregates, judging.BandAgricultureOther, judging.BandWasteEnvironmental, judging.BandOther, judging.ChosenByLabel, judging.ChosenByCategories, judging.ChosenByNone}
	want := []string{"headcount", "construction_trades", "construction_heavy_site", "construction", "trucking_private_mixed", "manufacturing_job_shop", "manufacturing_products_equipment", "manufacturing_building_products", "manufacturing_food_beverage", "manufacturing_other", "wholesale_distribution", "mining_aggregates", "agriculture_other", "waste_environmental", "other", "label", "categories", "none"}
	if !reflect.DeepEqual(words, want) {
		t.Fatal(words)
	}
}

// R-5RO9-1PCY R-5SW5-FH3N R-5YCC-JPO4
func TestLabelBands(t *testing.T) {
	for _, tc := range []struct {
		name      string
		low, high int64
		words     []string
	}{
		{judging.BandAgricultureOther, 200000, 500000, []string{"dairy farm", "feedlot", "hog farm", "cattle", "livestock", "swine", "poultry farm"}},
		{judging.BandManufacturingFoodBeverage, 200000, 450000, []string{"food", "pizza", "bakery", "dairy", "cheese", "bottling", "beverage", "meat", "brew", "distill", "coffee", "pet food", "snack", "candy"}},
		{judging.BandManufacturingBuildingProducts, 200000, 350000, []string{"truss", "precast", "concrete products", "ready-mix", "lumber", "millwork", "window", "door manufacturer", "block", "cabinet manufacturer", "flooring"}},
		{judging.BandManufacturingJobShop, 120000, 220000, []string{"machine shop", "machining", "fabricat", "weld", "stamping", "plating", "metal finishing", "powder coat", "cnc", "tool and die", "laser cutting", "sheet metal"}},
		{judging.BandManufacturingProductsEquipment, 180000, 350000, []string{"trailer", "fire apparatus", "vehicle", "machinery", "controls", "control panel", "sign", "equipment manufacturer", "plastics", "furniture", "boat", "electronics"}},
		{judging.BandConstructionHeavySite, 200000, 350000, []string{"excavat", "paving", "asphalt", "underground", "utility contractor", "grading", "boring", "site work", "demolition", "sewer", "water main", "earthwork", "crane"}},
		{judging.BandConstructionTrades, 150000, 250000, []string{"electric", "plumb", "hvac", "mechanical contractor", "concrete contractor", "masonry", "roofing", "siding", "insulation", "drywall", "painting", "landscap", "hardscape", "builder", "remodel"}},
		{judging.BandMiningAggregates, 200000, 400000, []string{"gravel", "sand", "quarry", "aggregate", "mining"}},
		{judging.BandWasteEnvironmental, 150000, 250000, []string{"waste", "recycling", "septic", "environmental", "disposal"}},
		{judging.BandWholesaleDistribution, 400000, 1000000, []string{"distributor", "wholesale", "supply", "supplier"}},
		{judging.BandTruckingPrivateMixed, 120000, 200000, []string{"freight", "carrier", "hauling", "trucking company", "logistics"}},
	} {
		for _, word := range tc.words {
			got := judging.ChooseBand([]string{"general services", word}, []sources.Category{{Name: sources.CategoryOther}})
			// A longer keyword containing an earlier keyword uses that earlier word.
			keyword := word
			if word == "pet food" {
				keyword = "food"
			}
			want := judging.Choice{Band: judging.Band{Name: tc.name, Low: tc.low, High: tc.high}, By: judging.ChosenByLabel, Label: word, Keyword: keyword}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("%s: %+v != %+v", word, got, want)
			}
		}
	}
	for _, tc := range []struct {
		labels        []string
		name, keyword string
	}{
		{[]string{"electrical contractor", "cheese maker"}, judging.BandConstructionTrades, "electric"},
		{[]string{"plumbing supply distributor"}, judging.BandConstructionTrades, "plumb"},
		{[]string{"Dairy Farm and creamery"}, judging.BandAgricultureOther, "dairy farm"},
		{[]string{"concrete block and ready-mix"}, judging.BandManufacturingBuildingProducts, "ready-mix"},
		{[]string{"precision CNC machining"}, judging.BandManufacturingJobShop, "machining"},
		{[]string{"Underground Utility Contractor"}, judging.BandConstructionHeavySite, "underground"},
	} {
		before := append([]string{}, tc.labels...)
		got := judging.ChooseBand(tc.labels, nil)
		if got.Band.Name != tc.name || got.Keyword != tc.keyword || got.Label != tc.labels[0] || !reflect.DeepEqual(before, tc.labels) {
			t.Fatal(got)
		}
	}
}

// R-5RO9-1PCY R-5ZK8-XHET
func TestCategoryBands(t *testing.T) {
	for _, tc := range []struct {
		categories []string
		band       judging.Band
		names      []string
		by         string
	}{
		{nil, judging.Band{judging.BandOther, 100000, 250000}, nil, judging.ChosenByNone},
		{[]string{sources.CategoryConstruction}, judging.Band{judging.BandConstruction, 150000, 350000}, []string{sources.CategoryConstruction}, judging.ChosenByCategories},
		{[]string{sources.CategoryManufacturing}, judging.Band{judging.BandManufacturingOther, 120000, 350000}, []string{sources.CategoryManufacturing}, judging.ChosenByCategories},
		{[]string{sources.CategoryConstruction, sources.CategoryTrucking, sources.CategoryConstruction}, judging.Band{judging.BandConstruction + "+" + judging.BandTruckingPrivateMixed, 120000, 350000}, []string{sources.CategoryConstruction, sources.CategoryTrucking}, judging.ChosenByCategories},
		{[]string{sources.CategoryMining, sources.CategoryWholesale}, judging.Band{judging.BandWholesaleDistribution + "+" + judging.BandMiningAggregates, 200000, 1000000}, []string{sources.CategoryMining, sources.CategoryWholesale}, judging.ChosenByCategories},
		{[]string{sources.CategoryAgriculture}, judging.Band{judging.BandAgricultureOther, 200000, 500000}, []string{sources.CategoryAgriculture}, judging.ChosenByCategories},
		{[]string{sources.CategoryWaste, sources.CategoryRetail}, judging.Band{judging.BandWasteEnvironmental + "+" + judging.BandOther, 100000, 250000}, []string{sources.CategoryRetail, sources.CategoryWaste}, judging.ChosenByCategories},
		{[]string{sources.CategoryRetail, sources.CategoryUtilities}, judging.Band{judging.BandOther, 100000, 250000}, []string{sources.CategoryRetail, sources.CategoryUtilities}, judging.ChosenByCategories},
		{[]string{""}, judging.Band{judging.BandOther, 100000, 250000}, []string{""}, judging.ChosenByCategories},
	} {
		cs := []sources.Category{}
		for _, c := range tc.categories {
			cs = append(cs, sources.Category{Name: c, Source: "source"})
		}
		before := append([]sources.Category{}, cs...)
		got := judging.ChooseBand([]string{"general services"}, cs)
		if !reflect.DeepEqual(got, judging.Choice{Band: tc.band, By: tc.by, Categories: tc.names}) || !reflect.DeepEqual(cs, before) {
			t.Fatal(got)
		}
	}
}

// R-OJPZ-AHHX R-OM5S-20ZB
func TestHeadcountSelection(t *testing.T) {
	kinds := []string{sources.HeadcountEmployees, sources.HeadcountParticipants, sources.HeadcountJobs, sources.HeadcountDrivers, sources.HeadcountSnippet}
	for i, k := range kinds {
		s := judging.PickHeadcount([]sources.Headcount{{Scraper: "source", Kind: k, Count: 10}})
		want := []float64{.5, .5, .4, .3, .3}[i]
		if s == nil || s.Low != 10 || s.High != 10 || s.Confidence != want || s.Basis != k+":source" {
			t.Fatal(s)
		}
	}
	for _, hs := range [][]sources.Headcount{nil, {}, {{Kind: sources.HeadcountJobs, Count: 0}}, {{Kind: sources.HeadcountJobs, Count: -3}}, {{Kind: "payroll", Count: 5}}} {
		if s := judging.PickHeadcount(hs); s != nil {
			t.Fatal(s)
		}
	}
	d1 := time.Date(2020, 4, 28, 0, 0, 0, 0, time.UTC)
	d2 := d1.AddDate(1, 0, 0)
	hs := []sources.Headcount{{Scraper: "a", Kind: sources.HeadcountJobs, Count: 40, AsOf: time.Time{}}, {Scraper: "b", Kind: sources.HeadcountDrivers, Count: 20, AsOf: d2}, {Scraper: "a", Kind: sources.HeadcountJobs, Count: 26, AsOf: d1}, {Scraper: "c", Kind: sources.HeadcountJobs, Count: 27, AsOf: d1}, {Scraper: "a", Kind: sources.HeadcountJobs, Count: 0, AsOf: d2}}
	before := append([]sources.Headcount{}, hs...)
	s := judging.PickHeadcount(hs)
	if s == nil || !reflect.DeepEqual(s.Used, []sources.Headcount{hs[3], hs[1]}) || s.AsOf != d1 || s.Basis != "jobs:c drivers:b" || !reflect.DeepEqual(hs, before) {
		t.Fatal(s)
	}
	hs[4].Count = 50
	s = judging.PickHeadcount(hs)
	if s.Used[0] != hs[4] {
		t.Fatal(s)
	}
	for _, tc := range []struct {
		jobs, drivers, low, high int64
		confidence               float64
	}{
		{100, 40, 40, 100, .4}, {30, 28, 30, 30, .7}, {30, 40, 30, 30, .7}, {30, 41, 30, 41, .4}, {30, 20, 30, 30, .7}, {30, 19, 19, 30, .4}, {math.MaxInt64, 1, 1, math.MaxInt64, .4}, {1, math.MaxInt64, 1, math.MaxInt64, .4}, {math.MaxInt64, math.MaxInt64 - 1, math.MaxInt64, math.MaxInt64, .7},
	} {
		got := judging.PickHeadcount([]sources.Headcount{{Kind: sources.HeadcountDrivers, Count: tc.drivers}, {Kind: sources.HeadcountJobs, Count: tc.jobs}})
		if got.Low != tc.low || got.High != tc.high || got.Confidence != tc.confidence {
			t.Fatal(tc, got)
		}
	}
	s = judging.PickHeadcount([]sources.Headcount{{Kind: sources.HeadcountSnippet, Count: 9}, {Kind: sources.HeadcountParticipants, Count: 11}, {Kind: sources.HeadcountEmployees, Count: 10}})
	if s.Used[0].Kind != sources.HeadcountEmployees || s.Used[1].Kind != sources.HeadcountParticipants || s.Used[2].Kind != sources.HeadcountSnippet || s.Confidence != .7 {
		t.Fatal(s)
	}
}

// R-ONDO-FSQ0 R-OOLK-TKGP
func TestHeadcountRevenueAndFacts(t *testing.T) {
	date := time.Date(2020, 4, 28, 23, 0, 0, 0, time.FixedZone("west", -5*3600))
	for _, tc := range []struct {
		n                 int64
		label             string
		categories        []sources.Category
		low, high         int64
		band              string
		bandLow, bandHigh int64
		by                string
	}{
		{26, "", []sources.Category{{Name: sources.CategoryConstruction}}, 3900000, 9100000, judging.BandConstruction, 150000, 350000, judging.ChosenByCategories},
		{26, "excavating contractor", nil, 5200000, 9100000, judging.BandConstructionHeavySite, 200000, 350000, judging.ChosenByLabel},
		{10, "", nil, 1000000, 2500000, judging.BandOther, 100000, 250000, judging.ChosenByNone},
		{20000000000000, "", []sources.Category{{Name: sources.CategoryWholesale}}, 8000000000000000000, math.MaxInt64, judging.BandWholesaleDistribution, 400000, 1000000, judging.ChosenByCategories},
		{36893488147419, "", []sources.Category{{Name: sources.CategoryWholesale}}, math.MaxInt64, math.MaxInt64, judging.BandWholesaleDistribution, 400000, 1000000, judging.ChosenByCategories},
	} {
		hs := []sources.Headcount{{Scraper: "a", Kind: sources.HeadcountJobs, Count: tc.n, AsOf: date}}
		labels := []string{tc.label}
		before, _ := json.Marshal([]any{hs, labels, tc.categories})
		e := judging.HeadcountEstimate(hs, labels, tc.categories)
		if e == nil || e.Source != judging.HeadcountSource || e.Low != tc.low || e.High == nil || *e.High != tc.high || e.Confidence != .4 || e.Filed != date || e.Formula != sources.Rules {
			t.Fatal(tc, e)
		}
		expected := map[string]json.RawMessage{"headcounts": json.RawMessage(`[{"scraper":"a","kind":"jobs","count":` + jsonNumber(tc.n) + `,"date":"2020-04-29"}]`)}
		put := func(k string, v any) { b, _ := json.Marshal(v); expected[k] = b }
		put("band", tc.band)
		put("band_low", tc.bandLow)
		put("band_high", tc.bandHigh)
		put("chosen_by", tc.by)
		if tc.by == judging.ChosenByLabel {
			put("label", tc.label)
			put("keyword", "excavat")
		}
		if tc.by == judging.ChosenByCategories {
			put("categories", []string{tc.categories[0].Name})
		}
		if !reflect.DeepEqual(e.Facts, expected) {
			t.Fatalf("%s != %s", e.Facts, expected)
		}
		after, _ := json.Marshal([]any{hs, labels, tc.categories})
		if string(before) != string(after) {
			t.Fatal("inputs changed")
		}
	}
	if e := judging.HeadcountEstimate(nil, nil, nil); e != nil {
		t.Fatal(e)
	}
	e := judging.HeadcountEstimate([]sources.Headcount{{Scraper: "a", Kind: sources.HeadcountJobs, Count: 10, AsOf: time.Time{}}}, nil, nil)
	if !e.Filed.IsZero() || string(e.Facts["headcounts"]) != `[{"scraper":"a","kind":"jobs","count":10}]` {
		t.Fatal(e)
	}
}
func jsonNumber(n int64) string { b, _ := json.Marshal(n); return string(b) }

// R-ONDO-FSQ0 R-OOLK-TKGP
func TestMultipleHeadcountsRevenue(t *testing.T) {
	d1 := time.Date(2020, 4, 28, 0, 0, 0, 0, time.UTC)
	d2 := time.Date(2021, 2, 1, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		jobs, drivers, low, high int64
		confidence               float64
		counts                   string
	}{
		{100, 40, 6000000, 35000000, .4, `[{"scraper":"a","kind":"jobs","count":100,"date":"2020-04-28"},{"scraper":"b","kind":"drivers","count":40,"date":"2021-02-01"}]`},
		{30, 28, 4500000, 10500000, .7, `[{"scraper":"a","kind":"jobs","count":30,"date":"2020-04-28"},{"scraper":"b","kind":"drivers","count":28,"date":"2021-02-01"}]`},
	} {
		hs := []sources.Headcount{{Scraper: "b", Kind: sources.HeadcountDrivers, Count: tc.drivers, AsOf: d2}, {Scraper: "a", Kind: sources.HeadcountJobs, Count: tc.jobs, AsOf: d1}}
		before := append([]sources.Headcount{}, hs...)
		e := judging.HeadcountEstimate(hs, nil, []sources.Category{{Name: sources.CategoryConstruction}})
		if e == nil || e.Low != tc.low || e.High == nil || *e.High != tc.high || e.Confidence != tc.confidence || e.Filed != d1 || string(e.Facts["headcounts"]) != tc.counts || !reflect.DeepEqual(hs, before) {
			t.Fatal(tc, e)
		}
	}
}

// R-OJPZ-AHHX R-OM5S-20ZB R-ONDO-FSQ0 R-OOLK-TKGP
func TestSameKindTies(t *testing.T) {
	d1 := time.Date(2020, 4, 28, 0, 0, 0, 0, time.UTC)
	d2 := time.Date(2021, 2, 1, 0, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		a, b  sources.Headcount
		index int
	}{
		{sources.Headcount{Scraper: "a", Kind: sources.HeadcountJobs, Count: 26, AsOf: d1}, sources.Headcount{Scraper: "b", Kind: sources.HeadcountJobs, Count: 27, AsOf: d1}, 1},
		{sources.Headcount{Scraper: "a", Kind: sources.HeadcountJobs, Count: 27, AsOf: d1}, sources.Headcount{Scraper: "b", Kind: sources.HeadcountJobs, Count: 26, AsOf: d1}, 0},
		{sources.Headcount{Scraper: "a", Kind: sources.HeadcountJobs, Count: 26, AsOf: d1}, sources.Headcount{Scraper: "b", Kind: sources.HeadcountJobs, Count: 26, AsOf: d1}, 0},
		{sources.Headcount{Scraper: "a", Kind: sources.HeadcountJobs, Count: 40, AsOf: d1}, sources.Headcount{Scraper: "b", Kind: sources.HeadcountJobs, Count: 26, AsOf: d2}, 1},
		{sources.Headcount{Scraper: "b", Kind: sources.HeadcountJobs, Count: 26, AsOf: d2}, sources.Headcount{Scraper: "a", Kind: sources.HeadcountJobs, Count: 40, AsOf: d1}, 0},
		{sources.Headcount{Scraper: "a", Kind: sources.HeadcountParticipants, Count: 12}, sources.Headcount{Scraper: "b", Kind: sources.HeadcountParticipants, Count: 21}, 1},
		{sources.Headcount{Scraper: "a", Kind: sources.HeadcountJobs, Count: 26, AsOf: d1}, sources.Headcount{Scraper: "b", Kind: sources.HeadcountJobs, Count: 40}, 0},
	} {
		hs := []sources.Headcount{tc.a, tc.b}
		before := append([]sources.Headcount{}, hs...)
		want := hs[tc.index]
		got := judging.PickHeadcount(hs)
		confidence := .4
		if want.Kind == sources.HeadcountParticipants {
			confidence = .5
		}
		if got == nil || !reflect.DeepEqual(got.Used, []sources.Headcount{want}) || got.Low != want.Count || got.High != want.Count || got.AsOf != want.AsOf || got.Confidence != confidence || got.Basis != want.Kind+":"+want.Scraper || !reflect.DeepEqual(hs, before) {
			t.Fatal(tc, got)
		}
		e := judging.HeadcountEstimate(hs, nil, nil)
		type fact struct {
			Scraper string `json:"scraper"`
			Kind    string `json:"kind"`
			Count   int64  `json:"count"`
			Date    string `json:"date,omitempty"`
		}
		date := ""
		if !want.AsOf.IsZero() {
			date = want.AsOf.UTC().Format(time.DateOnly)
		}
		counts, _ := json.Marshal([]fact{{want.Scraper, want.Kind, want.Count, date}})
		if e.Low != want.Count*100000 || e.High == nil || *e.High != want.Count*250000 || e.Filed != want.AsOf || e.Confidence != confidence || string(e.Facts["headcounts"]) != string(counts) {
			t.Fatal(tc, e)
		}
	}
	for _, counts := range [][3]int64{{10, 14, 12}, {12, 14, 10}, {30, 25, 50}} {
		hs := []sources.Headcount{{Kind: sources.HeadcountEmployees, Count: counts[0]}, {Kind: sources.HeadcountParticipants, Count: counts[1]}, {Kind: sources.HeadcountJobs, Count: counts[2]}}
		got := judging.PickHeadcount(hs)
		low, high, confidence := int64(10), int64(14), .5
		if counts[0] == 12 {
			low, high, confidence = 12, 12, .7
		}
		if counts[0] == 30 {
			low, high = 25, 50
		}
		if got.Low != low || got.High != high || got.Confidence != confidence {
			t.Fatal(counts, got)
		}
	}
}
