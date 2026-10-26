package judging

import (
	"encoding/json"
	"math"
	"math/big"
	"slices"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/sources"
)

// Band bounds yearly revenue per employee.
type Band struct {
	Name      string
	Low, High int64
}

// Choice records the evidence selecting a band.
type Choice struct {
	Band               Band
	By, Label, Keyword string
	Categories         []string
}

// Staff records the selected counts and their agreement.
type Staff struct {
	Low, High  int64
	Confidence float64
	Basis      string
	Used       []sources.Headcount
	AsOf       time.Time
}

// HeadcountSource names staffing estimates; the remaining words identify bands and selection evidence.
const (
	HeadcountSource                    = "headcount"
	ChosenByLabel                      = "label"
	ChosenByCategories                 = "categories"
	ChosenByNone                       = "none"
	BandConstructionTrades             = "construction_trades"
	BandConstructionHeavySite          = "construction_heavy_site"
	BandConstruction                   = "construction"
	BandTruckingPrivateMixed           = "trucking_private_mixed"
	BandManufacturingJobShop           = "manufacturing_job_shop"
	BandManufacturingProductsEquipment = "manufacturing_products_equipment"
	BandManufacturingBuildingProducts  = "manufacturing_building_products"
	BandManufacturingFoodBeverage      = "manufacturing_food_beverage"
	BandManufacturingOther             = "manufacturing_other"
	BandWholesaleDistribution          = "wholesale_distribution"
	BandMiningAggregates               = "mining_aggregates"
	BandAgricultureOther               = "agriculture_other"
	BandWasteEnvironmental             = "waste_environmental"
	BandOther                          = "other"
)

var bands = []Band{
	{BandConstructionTrades, 150000, 250000},
	{BandConstructionHeavySite, 200000, 350000},
	{BandConstruction, 150000, 350000},
	{BandTruckingPrivateMixed, 120000, 200000},
	{BandManufacturingJobShop, 120000, 220000},
	{BandManufacturingProductsEquipment, 180000, 350000},
	{BandManufacturingBuildingProducts, 200000, 350000},
	{BandManufacturingFoodBeverage, 200000, 450000},
	{BandManufacturingOther, 120000, 350000},
	{BandWholesaleDistribution, 400000, 1000000},
	{BandMiningAggregates, 200000, 400000},
	{BandAgricultureOther, 200000, 500000},
	{BandWasteEnvironmental, 150000, 250000},
	{BandOther, 100000, 250000},
}
var keywordRows = []struct {
	name  string
	words []string
}{
	{BandAgricultureOther, []string{"dairy farm", "feedlot", "hog farm", "cattle", "livestock", "swine", "poultry farm"}},
	{BandManufacturingFoodBeverage, []string{"food", "pizza", "bakery", "dairy", "cheese", "bottling", "beverage", "meat", "brew", "distill", "coffee", "pet food", "snack", "candy"}},
	{BandManufacturingBuildingProducts, []string{"truss", "precast", "concrete products", "ready-mix", "lumber", "millwork", "window", "door manufacturer", "block", "cabinet manufacturer", "flooring"}},
	{BandManufacturingJobShop, []string{"machine shop", "machining", "fabricat", "weld", "stamping", "plating", "metal finishing", "powder coat", "cnc", "tool and die", "laser cutting", "sheet metal"}},
	{BandManufacturingProductsEquipment, []string{"trailer", "fire apparatus", "vehicle", "machinery", "controls", "control panel", "sign", "equipment manufacturer", "plastics", "furniture", "boat", "electronics"}},
	{BandConstructionHeavySite, []string{"excavat", "paving", "asphalt", "underground", "utility contractor", "grading", "boring", "site work", "demolition", "sewer", "water main", "earthwork", "crane"}},
	{BandConstructionTrades, []string{"electric", "plumb", "hvac", "mechanical contractor", "concrete contractor", "masonry", "roofing", "siding", "insulation", "drywall", "painting", "landscap", "hardscape", "builder", "remodel"}},
	{BandMiningAggregates, []string{"gravel", "sand", "quarry", "aggregate", "mining"}},
	{BandWasteEnvironmental, []string{"waste", "recycling", "septic", "environmental", "disposal"}},
	{BandWholesaleDistribution, []string{"distributor", "wholesale", "supply", "supplier"}},
	{BandTruckingPrivateMixed, []string{"freight", "carrier", "hauling", "trucking company", "logistics"}},
}

func namedBand(name string) Band {
	for _, b := range bands {
		if b.Name == name {
			return b
		}
	}
	return Band{}
}

// ChooseBand prefers the first matching label, then combines categories.
func ChooseBand(labels []string, categories []sources.Category) Choice {
	for _, label := range labels {
		for _, row := range keywordRows {
			for _, word := range row.words {
				if strings.Contains(strings.ToLower(label), word) {
					return Choice{Band: namedBand(row.name), By: ChosenByLabel, Label: label, Keyword: word}
				}
			}
		}
	}
	if len(categories) == 0 {
		return Choice{Band: namedBand(BandOther), By: ChosenByNone}
	}
	mapping := map[string]string{sources.CategoryConstruction: BandConstruction, sources.CategoryManufacturing: BandManufacturingOther, sources.CategoryTrucking: BandTruckingPrivateMixed, sources.CategoryAgriculture: BandAgricultureOther, sources.CategoryWholesale: BandWholesaleDistribution, sources.CategoryMining: BandMiningAggregates, sources.CategoryWaste: BandWasteEnvironmental}
	names := map[string]bool{}
	cs := []string{}
	for _, c := range categories {
		name := mapping[c.Name]
		if name == "" {
			name = BandOther
		}
		names[name] = true
		if !slices.Contains(cs, c.Name) {
			cs = append(cs, c.Name)
		}
	}
	slices.Sort(cs)
	choice := Choice{By: ChosenByCategories, Categories: cs}
	ns := []string{}
	for _, b := range bands {
		if names[b.Name] {
			ns = append(ns, b.Name)
			if choice.Band.Low == 0 || b.Low < choice.Band.Low {
				choice.Band.Low = b.Low
			}
			choice.Band.High = max(choice.Band.High, b.High)
		}
	}
	choice.Band.Name = strings.Join(ns, "+")
	return choice
}

// PickHeadcount ranks each kind's newest positive report and measures agreement.
func PickHeadcount(hs []sources.Headcount) *Staff {
	kinds := []string{sources.HeadcountEmployees, sources.HeadcountParticipants, sources.HeadcountJobs, sources.HeadcountDrivers, sources.HeadcountSnippet}
	confidences := []float64{.5, .5, .4, .3, .3}
	staff := &Staff{Used: []sources.Headcount{}}
	basis := []string{}
	contenders := headcountContenders(hs)
	for i, kind := range kinds {
		var best *sources.Headcount
		for _, h := range contenders {
			if h.Kind == kind && h.Count > 0 && (best == nil || !h.AsOf.IsZero() && (best.AsOf.IsZero() || h.AsOf.After(best.AsOf)) || h.AsOf.Equal(best.AsOf) && h.Count > best.Count) {
				v := h
				best = &v
			}
		}
		if best != nil {
			if len(staff.Used) == 0 {
				staff.AsOf = best.AsOf
				staff.Confidence = confidences[i]
			}
			staff.Used = append(staff.Used, *best)
			basis = append(basis, kind+":"+best.Scraper)
		}
	}
	if len(staff.Used) == 0 {
		return nil
	}
	staff.Basis = strings.Join(basis, " ")
	base := staff.Used[0].Count
	staff.Low = base
	staff.High = base
	disagree := false
	for _, h := range staff.Used {
		d := max(h.Count, base) - min(h.Count, base)
		if d > base/3 {
			disagree = true
		}
		staff.Low = min(staff.Low, h.Count)
		staff.High = max(staff.High, h.Count)
	}
	if !disagree {
		staff.Low = base
		staff.High = base
		if len(staff.Used) > 1 {
			staff.Confidence = .7
		}
	}
	return staff
}

// headcountContenders combines employee sites before choosing among reports.
func headcountContenders(hs []sources.Headcount) []sources.Headcount {
	type pair struct{ count, hours int64 }
	type group struct {
		at           int
		count, hours big.Int
		missingHours bool
		seen         map[pair]bool
	}
	out := []sources.Headcount{}
	groups := []*group{}
	for _, h := range hs {
		if h.Kind != sources.HeadcountEmployees || h.Count <= 0 {
			out = append(out, h)
			continue
		}
		var g *group
		if !h.AsOf.IsZero() {
			for _, candidate := range groups {
				first := out[candidate.at]
				if first.Scraper == h.Scraper && first.AsOf.Equal(h.AsOf) {
					g = candidate
					break
				}
			}
		}
		if g == nil {
			g = &group{at: len(out), seen: map[pair]bool{}}
			groups = append(groups, g)
			out = append(out, h)
		}
		if h.Hours != nil {
			key := pair{h.Count, *h.Hours}
			if g.seen[key] {
				continue
			}
			g.seen[key] = true
			g.hours.Add(&g.hours, big.NewInt(*h.Hours))
		} else {
			g.missingHours = true
		}
		g.count.Add(&g.count, big.NewInt(h.Count))
	}
	for _, g := range groups {
		out[g.at].Count = saturatedInt64(&g.count)
		out[g.at].Hours = nil
		if !g.missingHours {
			hours := saturatedInt64(&g.hours)
			out[g.at].Hours = &hours
		}
	}
	return out
}

func saturatedInt64(n *big.Int) int64 {
	if n.IsInt64() {
		return n.Int64()
	}
	if n.Sign() < 0 {
		return math.MinInt64
	}
	return math.MaxInt64
}

func product(a, b int64) int64 {
	if a > math.MaxInt64/b {
		return math.MaxInt64
	}
	return a * b
}

// HeadcountEstimate converts selected staffing through its evidence's band.
func HeadcountEstimate(hs []sources.Headcount, labels []string, categories []sources.Category) *sources.Estimate {
	s := PickHeadcount(hs)
	if s == nil {
		return nil
	}
	c := ChooseBand(labels, categories)
	high := product(s.High, c.Band.High)
	facts := map[string]json.RawMessage{}
	put := func(k string, v any) { b, _ := json.Marshal(v); facts[k] = b }
	type fact struct {
		Scraper string `json:"scraper"`
		Kind    string `json:"kind"`
		Count   int64  `json:"count"`
		Date    string `json:"date,omitempty"`
		Hours   *int64 `json:"hours,omitempty"`
	}
	counts := []fact{}
	for _, h := range s.Used {
		date := ""
		if !h.AsOf.IsZero() {
			date = h.AsOf.UTC().Format(time.DateOnly)
		}
		counts = append(counts, fact{h.Scraper, h.Kind, h.Count, date, h.Hours})
	}
	put("headcounts", counts)
	put("band", c.Band.Name)
	put("band_low", c.Band.Low)
	put("band_high", c.Band.High)
	put("chosen_by", c.By)
	if c.By == ChosenByLabel {
		put("label", c.Label)
		put("keyword", c.Keyword)
	}
	if c.By == ChosenByCategories {
		put("categories", c.Categories)
	}
	return &sources.Estimate{Source: HeadcountSource, Low: product(s.Low, c.Band.Low), High: &high, Confidence: s.Confidence, Facts: facts, Filed: s.AsOf, Formula: sources.Rules}
}
