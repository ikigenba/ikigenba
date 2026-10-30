package store

import (
	"strings"

	"github.com/ikigenba/ikigenba/candidates/internal/judging"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
)

// ValidProspectListID recognizes prospectList identifiers.
func ValidProspectListID(s string) bool { return validID(s, ProspectListIDPrefix) }

// ValidProspectListName accepts a name containing non-whitespace text.
func ValidProspectListName(s string) bool { return strings.TrimSpace(s) != "" }

// ValidProspectListSize checks the supported member limit.
func ValidProspectListSize(n int64) bool { return n >= ProspectListSizeMin && n <= ProspectListSizeMax }

// ValidProspectListFilters checks the values of all carried filters.
func ValidProspectListFilters(f ProspectListFilters) bool {
	for _, p := range []*int64{f.RevenueMin, f.RevenueMax, f.MaxMinutes, f.MaxFilingAgeDays, f.MinCampaignAgeDays} {
		if p != nil && *p < 0 {
			return false
		}
	}
	if f.RevenueMin != nil && f.RevenueMax != nil && *f.RevenueMin > *f.RevenueMax {
		return false
	}
	for _, c := range f.Categories {
		switch c {
		case sources.CategoryConstruction, sources.CategoryTrucking, sources.CategoryAgriculture, sources.CategoryManufacturing, sources.CategoryWholesale, sources.CategoryMining, sources.CategoryWaste, sources.CategoryOther,
			sources.CategoryUtilities, sources.CategoryRetail, sources.CategoryOtherTransportation,
			sources.CategoryInformation, sources.CategoryFinance, sources.CategoryRealEstate,
			sources.CategoryProfessional, sources.CategoryAdministrative, sources.CategoryEducation,
			sources.CategoryHealthCare, sources.CategoryArts, sources.CategoryFood,
			sources.CategoryRepair, sources.CategoryReligious, sources.CategoryGovernment:
		default:
			return false
		}
	}
	switch f.Fleet {
	case "", sources.FleetPass, sources.FleetFail:
	default:
		return false
	}
	if f.Website != nil && len(f.Website) == 0 {
		return false
	}
	seen := make(map[string]bool, len(f.Website))
	for _, state := range f.Website {
		switch state {
		case sources.WebsiteHTTPS, sources.WebsiteHTTP, sources.WebsiteNone:
		default:
			return false
		}
		if seen[state] {
			return false
		}
		seen[state] = true
	}
	for _, trend := range f.Trend {
		if trend != judging.TrendGrowing && trend != judging.TrendFlat && trend != judging.TrendShrinking {
			return false
		}
	}
	for _, b := range f.Basis {
		if b != BasisRecords && b != BasisPartial && b != BasisEnriched {
			return false
		}
	}
	if f.CampaignAny != "" && f.CampaignAny != "yes" && f.CampaignAny != "no" {
		return false
	}
	if f.Campaign != "" && !validID(f.Campaign, "camp_") || f.CampaignNot && f.Campaign == "" {
		return false
	}
	if f.Opened != "" && f.Opened != "yes" && f.Opened != "no" {
		return false
	}
	switch f.Class {
	case "", sources.ClassForHire, sources.ClassPrivate, sources.ClassMixed:
	default:
		return false
	}
	return true
}

// ValidProspectListOrder checks the ordering fields, directions and uniqueness.
func ValidProspectListOrder(o []ProspectListOrder) bool {
	seen := make(map[string]bool, len(o))
	for _, x := range o {
		switch x.Field {
		case OrderOperatingConfidence, OrderRevenueLow, OrderRevenueConfidence, OrderDriveMinutes, OrderTrend, OrderNewestFiling, OrderKnownSince:
		default:
			return false
		}
		if x.Direction != DirectionAsc && x.Direction != DirectionDesc || seen[x.Field] {
			return false
		}
		seen[x.Field] = true
	}
	return true
}
