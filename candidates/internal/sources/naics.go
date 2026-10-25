// Package sources defines normalized source facts.
package sources

import "strings"

// NAICSCategory selects the category of a valid NAICS code.
func NAICSCategory(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || strings.IndexFunc(s, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
		return ""
	}
	// Try three-digit prefixes before two-digit prefixes so the first match
	// always selects the most specific category.
	for _, group := range []struct {
		prefixes []string
		name     string
	}{
		{[]string{"484", "488", "492", "493"}, CategoryTrucking},
		{[]string{"562"}, CategoryWaste},
		{[]string{"481", "482", "483", "485", "486", "487", "491"}, CategoryOtherTransportation},
		{[]string{"561"}, CategoryAdministrative},
		{[]string{"811", "812"}, CategoryRepair},
		{[]string{"813"}, CategoryReligious},
		{[]string{"23"}, CategoryConstruction},
		{[]string{"11"}, CategoryAgriculture},
		{[]string{"31", "32", "33"}, CategoryManufacturing},
		{[]string{"42"}, CategoryWholesale},
		{[]string{"21"}, CategoryMining},
		{[]string{"22"}, CategoryUtilities},
		{[]string{"44", "45"}, CategoryRetail},
		{[]string{"51"}, CategoryInformation},
		{[]string{"52"}, CategoryFinance},
		{[]string{"53"}, CategoryRealEstate},
		{[]string{"54", "55"}, CategoryProfessional},
		{[]string{"61"}, CategoryEducation},
		{[]string{"62"}, CategoryHealthCare},
		{[]string{"71"}, CategoryArts},
		{[]string{"72"}, CategoryFood},
		{[]string{"92"}, CategoryGovernment},
	} {
		for _, p := range group.prefixes {
			if strings.HasPrefix(s, p) {
				return group.name
			}
		}
	}
	return CategoryOther
}
