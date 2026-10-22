package store

import (
	"cmp"
	"encoding/json"

	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
)

// BasisOf describes the enrichment evidence held in a result history.
func BasisOf(es []Enrichment) string {
	partial, brief, route := false, false, false
	for _, e := range es {
		switch e.Kind {
		case enrichers.KindWebsiteSearch, enrichers.KindSiteText:
			partial = true
		case enrichers.KindBrief:
			partial = true
			brief = brief || !(enrichers.Brief{}).Failed(e.Found)
		case enrichers.KindDriveRoute:
			partial = true
			var f enrichers.RouteFound
			if json.Unmarshal(e.Found, &f) == nil && f.Minutes != nil {
				route = true
			}
		}
	}
	if brief && route {
		return BasisEnriched
	}
	if partial {
		return BasisPartial
	}
	return BasisRecords
}

// KindsOf partitions targeted kinds in their workflow order.
func KindsOf(es []Enrichment) (present, missing []string) {
	present, missing = []string{}, []string{}
	for _, kind := range []string{enrichers.KindWebsiteSearch, enrichers.KindSiteText, enrichers.KindBrief, enrichers.KindDriveRoute} {
		found := false
		for _, e := range es {
			if e.Kind == kind {
				found = true
				break
			}
		}
		if found {
			present = append(present, kind)
		} else {
			missing = append(missing, kind)
		}
	}
	return
}
func compareBasis(a, b string) int {
	rank := func(v string) int {
		switch v {
		case BasisRecords:
			return 0
		case BasisPartial:
			return 1
		case BasisEnriched:
			return 2
		}
		return 3
	}
	if d := cmp.Compare(rank(a), rank(b)); d != 0 {
		return d
	}
	return cmp.Compare(a, b)
}
