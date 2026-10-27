package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/paging"
	"github.com/ikigenba/ikigenba/candidates/internal/parks"
	"github.com/ikigenba/ikigenba/candidates/internal/passes"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// Park tool descriptions are public copy.
const (
	DescribeParks         string = "Read industrial parks derived from county parcel records."
	DescribePark          string = "Read an industrial park and its parcels."
	DescribeParkRules     string = "Read the rules used to derive industrial parks."
	DescribeEditParkRules string = "Change park rules and start a new derivation."
)

// ParksArgs selects a positioned window of parks.
type ParksArgs struct {
	County string  `json:"county"`
	Walked string  `json:"walked"`
	Gone   string  `json:"gone"`
	Sort   string  `json:"sort"`
	Page   *string `json:"page" description:"Continues a previous answer: pass the next value it returned, with the same filters. Leave out for the first page."`
	Limit  *int    `json:"limit" description:"Most entries to return, 1 to 500; 25 when left out."`
}

// ParkArgs names one park.
type ParkArgs struct {
	Park string `json:"park" mcp:"required"`
}

// EditParkRulesArgs changes selected rules.
type EditParkRulesArgs struct {
	Distance       *uint32   `json:"distance"`
	MinParcels     *uint32   `json:"min_parcels"`
	ValueThreshold *uint32   `json:"value_threshold"`
	County         *string   `json:"county"`
	Labels         *[]string `json:"labels"`
}

// Park is one industrial park answer.
type Park struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	County        string  `json:"county"`
	Town          string  `json:"town"`
	Zip           string  `json:"zip"`
	State         string  `json:"state"`
	Lat           float64 `json:"lat"`
	Lon           float64 `json:"lon"`
	Radius        float64 `json:"radius"`
	Parcels       int64   `json:"parcels"`
	BuildingValue int64   `json:"building_value"`
	Minutes       *int64  `json:"minutes"`
	GrownFrom     int64   `json:"grown_from"`
	Walked        string  `json:"walked"`
	WalkRun       string  `json:"walk_run"`
	Found         int64   `json:"found"`
	Run           string  `json:"run"`
	MapURL        string  `json:"map_url"`
	Created       string  `json:"created"`
	Updated       string  `json:"updated"`
}

// ParkList is a window of parks.
type ParkList struct {
	Parks []Park  `json:"parks"`
	Next  *string `json:"next,omitempty" description:"Present when more entries follow; pass it back as page to get them."`
}

// MarshalJSON keeps parks an array.
func (v ParkList) MarshalJSON() ([]byte, error) {
	type plain ParkList
	if v.Parks == nil {
		v.Parks = []Park{}
	}
	return json.Marshal(plain(v))
}

// ParkParcel is one parcel answer.
type ParkParcel struct {
	PIN           string   `json:"pin"`
	Owners        []string `json:"owners"`
	Street        string   `json:"street"`
	City          string   `json:"city"`
	Zip           string   `json:"zip"`
	BuildingValue *int64   `json:"building_value"`
	Rule          string   `json:"rule"`
	Lat           float64  `json:"lat"`
	Lon           float64  `json:"lon"`
}

// MarshalJSON keeps owners an array.
func (v ParkParcel) MarshalJSON() ([]byte, error) {
	type plain ParkParcel
	if v.Owners == nil {
		v.Owners = []string{}
	}
	return json.Marshal(plain(v))
}

// ParkDetail includes the park and its parcels.
type ParkDetail struct {
	Park    Park         `json:"park"`
	Parcels []ParkParcel `json:"parcels"`
}

// MarshalJSON keeps parcels an array.
func (v ParkDetail) MarshalJSON() ([]byte, error) {
	type plain ParkDetail
	if v.Parcels == nil {
		v.Parcels = []ParkParcel{}
	}
	return json.Marshal(plain(v))
}

// CountyLabels lists one county's industrial labels.
type CountyLabels struct {
	County string   `json:"county"`
	Labels []string `json:"labels"`
}

// MarshalJSON keeps labels an array.
func (v CountyLabels) MarshalJSON() ([]byte, error) {
	type plain CountyLabels
	if v.Labels == nil {
		v.Labels = []string{}
	}
	return json.Marshal(plain(v))
}

// ParkRules is the current derivation rules answer.
type ParkRules struct {
	Distance       int64          `json:"distance"`
	MinParcels     int64          `json:"min_parcels"`
	ValueThreshold int64          `json:"value_threshold"`
	Counties       []CountyLabels `json:"counties"`
}

// MarshalJSON keeps counties an array.
func (v ParkRules) MarshalJSON() ([]byte, error) {
	type plain ParkRules
	if v.Counties == nil {
		v.Counties = []CountyLabels{}
	}
	return json.Marshal(plain(v))
}

// EditedParkRules holds the new rules and the pass they started.
type EditedParkRules struct {
	Rules ParkRules     `json:"rules"`
	Pass  passes.Record `json:"pass"`
}

func parkOf(p store.Park) Park {
	walked := ""
	if !p.Walked.IsZero() {
		walked = stamp(p.Walked)
	}
	var minutes *int64
	if p.Minutes != nil {
		v := *p.Minutes
		minutes = &v
	}
	return Park{p.ID, p.Name, p.County, p.Town, p.Zip, p.State, p.Lat, p.Lon, p.Radius, p.Parcels, p.BuildingValue, minutes, p.GrownFrom, walked, p.WalkRun, p.Found, p.Run, parks.MapURL(p.Lat, p.Lon), stamp(p.Created), stamp(p.Updated)}
}
func parkParcelOf(q store.ParkParcel) ParkParcel {
	var value *int64
	if q.BuildingValue != nil {
		v := *q.BuildingValue
		value = &v
	}
	return ParkParcel{q.PIN, append([]string{}, q.Owners...), q.Street, q.City, q.Zip, value, q.Rule, q.Lat, q.Lon}
}
func parkRulesOf(r parks.Rules, cs []string) ParkRules {
	v := ParkRules{r.Distance, r.MinParcels, r.ValueThreshold, []CountyLabels{}}
	for _, c := range cs {
		v.Counties = append(v.Counties, CountyLabels{c, append([]string{}, r.Labels[c]...)})
	}
	return v
}
func effectiveParkSort(s string) string {
	if s == "" {
		return parks.SortMinutes
	}
	return s
}
func validParkKey(s, k string) bool {
	if k == "" && effectiveParkSort(s) == parks.SortMinutes {
		return true
	}
	_, err := strconv.ParseInt(k, 10, 64)
	return err == nil
}
func parkKey(p store.Park, s string) string {
	switch effectiveParkSort(s) {
	case parks.SortValue:
		return strconv.FormatInt(p.BuildingValue, 10)
	case parks.SortParcels:
		return strconv.FormatInt(p.Parcels, 10)
	}
	if p.Minutes == nil {
		return ""
	}
	return strconv.FormatInt(*p.Minutes, 10)
}
func registerParks(srv *mcp.Server, cfg Config) {
	add(srv, "parks", DescribeParks, mcp.Read, func(ctx context.Context, _ identity.Caller, a ParksArgs) mcp.Result {
		z, err := paging.Window(a.Limit)
		if err != nil {
			return mcp.ErrorResult(err.Error())
		}
		var s, k, i string
		if a.Page != nil && *a.Page != "" {
			if (paging.Token{Tool: "parks"}).Decode(*a.Page, &s, &k, &i) != nil || s != effectiveParkSort(a.Sort) || !store.ValidParkID(i) || !validParkKey(s, k) {
				return mcp.ErrorResult(paging.BadPage)
			}
		}
		if !parks.ValidWalked(a.Walked) || !parks.ValidGone(a.Gone) || !parks.ValidSort(a.Sort) || a.County != "" && !parks.ValidCounty(a.County) {
			return refuse(passes.RefusalInvalid)
		}
		ps, err := cfg.Store.ListParks(ctx, store.ParkFilter{County: a.County, Walked: a.Walked, Gone: a.Gone}, a.Sort, k, i, z+1)
		if err != nil {
			return failure(err)
		}
		out := ParkList{Parks: []Park{}}
		for j, p := range ps {
			if j == z {
				break
			}
			out.Parks = append(out.Parks, parkOf(p))
		}
		if len(ps) > z {
			p := ps[z-1]
			t := (paging.Token{Tool: "parks"}).Encode(effectiveParkSort(a.Sort), parkKey(p, a.Sort), p.ID)
			out.Next = &t
		}
		return answer(out)
	})
	add(srv, "park", DescribePark, mcp.Read, func(ctx context.Context, _ identity.Caller, a ParkArgs) mcp.Result {
		if !store.ValidParkID(a.Park) {
			return refuse(passes.RefusalNotFound)
		}
		p, err := cfg.Store.GetPark(ctx, a.Park)
		if err != nil {
			return failure(err)
		}
		qs, err := cfg.Store.ParkParcels(ctx, a.Park)
		if err != nil {
			return failure(err)
		}
		out := ParkDetail{Park: parkOf(p), Parcels: []ParkParcel{}}
		for _, q := range qs {
			out.Parcels = append(out.Parcels, parkParcelOf(q))
		}
		return answer(out)
	})
	add(srv, "park_rules", DescribeParkRules, mcp.Read, func(ctx context.Context, _ identity.Caller, _ NoArgs) mcp.Result {
		r, cs, err := cfg.Store.ParkRules(ctx)
		if err != nil {
			return failure(err)
		}
		return answer(parkRulesOf(r, cs))
	})
	add(srv, "edit_park_rules", DescribeEditParkRules, mcp.Additive, func(ctx context.Context, c identity.Caller, a EditParkRulesArgs) mcp.Result {
		if a.Distance != nil && !parks.ValidDistance(int64(*a.Distance)) || a.MinParcels != nil && !parks.ValidMinParcels(int64(*a.MinParcels)) || a.ValueThreshold != nil && !parks.ValidValueThreshold(int64(*a.ValueThreshold)) || a.County != nil && !parks.ValidCounty(*a.County) || a.County == nil && a.Labels != nil || a.County != nil && a.Labels == nil || a.Labels != nil && !parks.ValidLabels(*a.Labels) {
			return refuse(passes.RefusalInvalid)
		}
		e := store.ParkRulesEdit{Distance: int64Of(a.Distance), MinParcels: int64Of(a.MinParcels), ValueThreshold: int64Of(a.ValueThreshold), County: a.County}
		if a.Labels != nil {
			e.Labels = append([]string{}, (*a.Labels)...)
		}
		p, err := cfg.Passes.EditParkRules(ctx, c, e)
		if err != nil {
			var r *passes.Refusal
			if errors.As(err, &r) {
				return refuse(r.Word)
			}
			return refuse(passes.RefusalUnavailable)
		}
		r, cs, err := cfg.Store.ParkRules(ctx)
		if err != nil {
			return refuse(passes.RefusalUnavailable)
		}
		return answer(EditedParkRules{parkRulesOf(r, cs), passes.RecordOf(p)})
	})
}
