package store

import (
	"strings"

	"github.com/ikigenba/ikigenba/candidates/internal/sources"
)

// ValidCohortID recognizes cohort identifiers.
func ValidCohortID(s string) bool { return validID(s, CohortIDPrefix) }

// ValidCohortName accepts a name containing non-whitespace text.
func ValidCohortName(s string) bool { return strings.TrimSpace(s) != "" }

// ValidCohortSize checks the supported member limit.
func ValidCohortSize(n int64) bool { return n >= CohortSizeMin && n <= CohortSizeMax }

// ValidCohortFilters checks the values of all carried filters.
func ValidCohortFilters(f CohortFilters) bool {
	for _, p := range []*int64{f.RevenueMin, f.RevenueMax, f.MaxMinutes, f.MaxFilingAgeDays} {
		if p != nil && *p < 0 {
			return false
		}
	}
	if f.RevenueMin != nil && f.RevenueMax != nil && *f.RevenueMin > *f.RevenueMax {
		return false
	}
	for _, c := range f.Categories {
		switch c {
		case sources.CategoryConstruction, sources.CategoryTrucking, sources.CategoryAgriculture, sources.CategoryManufacturing, sources.CategoryWholesale, sources.CategoryMining, sources.CategoryWaste, sources.CategoryOther:
		default:
			return false
		}
	}
	switch f.Fleet {
	case "", sources.FleetPass, sources.FleetFail:
	default:
		return false
	}
	switch f.Class {
	case "", sources.ClassForHire, sources.ClassPrivate, sources.ClassMixed:
	default:
		return false
	}
	return true
}

// ValidCohortOrder checks the ordering fields, directions and uniqueness.
func ValidCohortOrder(o []CohortOrder) bool {
	seen := make(map[string]bool, len(o))
	for _, x := range o {
		switch x.Field {
		case OrderOperatingConfidence, OrderRevenueLow, OrderRevenueConfidence, OrderDriveMinutes, OrderNewestFiling, OrderKnownSince:
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
