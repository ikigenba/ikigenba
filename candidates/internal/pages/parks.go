package pages

import (
	"context"
	"errors"
	"math"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/paging"
	"github.com/ikigenba/ikigenba/candidates/internal/parks"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
	"github.com/ikigenba/ikigenba/candidates/internal/urls"
)

func valueText(v int64) string {
	if v >= 0 {
		return dollars(v)
	}
	s := strconv.FormatInt(v, 10)[1:]
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return "-$" + s
}
func parkRow(p store.Park) ParkRow {
	v := ParkRow{ID: p.ID, Name: p.Name, County: p.County, State: p.State, Parcels: p.Parcels, BuildingValue: valueText(p.BuildingValue), GrownFrom: p.GrownFrom}
	if p.Minutes != nil {
		v.Minutes = strconv.FormatInt(*p.Minutes, 10)
	}
	v.Walked, v.WalkedText = stamp(p.Walked, DateLayout)
	return v
}
func parkView(p store.Park, sp string) ParkView {
	v := ParkView{ID: p.ID, Name: p.Name, County: p.County, Town: p.Town, State: p.State, Lat: strconv.FormatFloat(p.Lat, 'f', 4, 64), Lon: strconv.FormatFloat(p.Lon, 'f', 4, 64), MapURL: parks.MapURL(p.Lat, p.Lon), Radius: strconv.FormatInt(int64(math.Round(p.Radius)), 10), ParcelCount: p.Parcels, BuildingValue: valueText(p.BuildingValue), GrownFrom: p.GrownFrom, Walk: p.WalkRun, Found: p.Found, NewCandidates: p.NewCandidates, CeilingReached: p.CeilingReached, Run: p.Run, RunPage: urls.RunPage(urls.Scrapes(sp), p.Run)}
	if p.Minutes != nil {
		v.Minutes = strconv.FormatInt(*p.Minutes, 10)
	}
	v.Walked, v.WalkedText = stamp(p.Walked, DateLayout)
	v.Created, v.CreatedText = stamp(p.Created, SecondLayout)
	v.Updated, v.UpdatedText = stamp(p.Updated, SecondLayout)
	return v
}
func parcelRow(q store.ParkParcel) ParcelRow {
	v := ParcelRow{PIN: q.PIN, Owners: append([]string{}, q.Owners...), Street: q.Street, City: q.City, Zip: q.Zip, Rule: q.Rule}
	if q.BuildingValue != nil {
		v.BuildingValue = valueText(*q.BuildingValue)
	}
	return v
}
func effectiveWalked(s string) string {
	if s == "" {
		return parks.WalkedAll
	}
	return s
}
func effectiveGone(s string) string {
	if s == "" {
		return parks.GoneActive
	}
	return s
}
func effectiveSort(s string) string {
	if s == "" {
		return parks.SortMinutes
	}
	return s
}
func parkSortKey(p store.Park, s string) string {
	switch effectiveSort(s) {
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
func validParkSortKey(s, k string) bool {
	if k == "" && effectiveSort(s) == parks.SortMinutes {
		return true
	}
	_, err := strconv.ParseInt(k, 10, 64)
	return err == nil
}
func viewHref(c, w, g, s, p string) string {
	parts := []string{}
	for _, x := range []struct{ k, v string }{{"county", c}, {"walked", effectiveWalked(w)}, {"gone", effectiveGone(g)}, {"sort", effectiveSort(s)}, {"page", p}} {
		if x.v == "" || x.k == "walked" && x.v == parks.WalkedAll || x.k == "gone" && x.v == parks.GoneActive || x.k == "sort" && x.v == parks.SortMinutes {
			continue
		}
		parts = append(parts, x.k+"="+url.QueryEscape(x.v))
	}
	if len(parts) == 0 {
		return "/parks"
	}
	return "/parks?" + strings.Join(parts, "&")
}

type parksPosition struct {
	key, id        string
	reversed, last bool
}

func decodeParksPage(p, s string) (parksPosition, bool) {
	pos := parksPosition{}
	if p == "" {
		return pos, true
	}
	for _, tool := range []string{"parks", "parks-before"} {
		var sort, key, id string
		if (paging.Token{Tool: tool}).Decode(p, &sort, &key, &id) == nil && sort == effectiveSort(s) && store.ValidParkID(id) && validParkSortKey(sort, key) {
			return parksPosition{key: key, id: id, reversed: tool == "parks-before"}, true
		}
	}
	if p == "last" {
		return parksPosition{reversed: true, last: true}, true
	}
	return pos, false
}
func drawPark(w http.ResponseWriter, r *http.Request, cfg Config, b page.Banner, id string, notfound, fail func()) {
	p, err := cfg.Store.GetPark(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		notfound()
		return
	}
	if err != nil {
		fail()
		return
	}
	qs, err := cfg.Store.ParkParcels(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		notfound()
		return
	}
	if err != nil {
		fail()
		return
	}
	b.Trail = []page.Level{ParksLevel, {Name: id, URL: "/parks/" + id}}
	d := ParkData{Banner: b, Park: parkView(p, cfg.ServicesPath), Walks: []WalkRow{}, Places: []PlaceRow{}, FoundCandidates: []FoundCandidateRow{}, Parcels: []ParcelRow{}}
	walks, err := cfg.Store.ParkWalks(r.Context(), id)
	if err != nil {
		fail()
		return
	}
	for _, x := range walks {
		d.Walks = append(d.Walks, walkRow(x))
	}
	places, err := cfg.Store.ParkPlaces(r.Context(), id)
	if err != nil {
		fail()
		return
	}
	for _, x := range places {
		d.Places = append(d.Places, placeRow(x))
	}
	found, err := cfg.Store.ParkCandidates(r.Context(), id)
	if err != nil {
		fail()
		return
	}
	for _, x := range found {
		d.FoundCandidates = append(d.FoundCandidates, FoundCandidateRow{x.ID, x.Name, x.Status, x.New})
	}
	for _, q := range qs {
		d.Parcels = append(d.Parcels, parcelRow(q))
	}
	cfg.Pages.Write(w, r, 200, "park", d)
}
func drawParks(w http.ResponseWriter, r *http.Request, cfg Config, b page.Banner, notfound, fail func()) {
	q := r.URL.Query()
	c, walked, g, s, p := q.Get("county"), q.Get("walked"), q.Get("gone"), q.Get("sort"), q.Get("page")
	pos, ok := decodeParksPage(p, s)
	if !ok || !parks.ValidWalked(walked) || !parks.ValidGone(g) || !parks.ValidSort(s) {
		notfound()
		return
	}
	counties, err := cfg.Store.ParkCountiesHeld(r.Context())
	if err != nil {
		fail()
		return
	}
	if c != "" && !slices.Contains(counties, c) {
		notfound()
		return
	}
	f := store.ParkFilter{County: c, Walked: walked, Gone: g}
	var ps []store.Park
	if pos.reversed {
		ps, err = cfg.Store.ListParksReversed(r.Context(), f, effectiveSort(s), pos.key, pos.id, paging.DefaultLimit+1)
	} else {
		ps, err = cfg.Store.ListParks(r.Context(), f, effectiveSort(s), pos.key, pos.id, paging.DefaultLimit+1)
	}
	if err != nil {
		fail()
		return
	}
	more := len(ps) > paging.DefaultLimit
	if pos.reversed && !more {
		w.Header().Set("Location", viewHref(c, walked, g, s, ""))
		w.WriteHeader(http.StatusSeeOther)
		return
	}
	if more {
		ps = ps[:paging.DefaultLimit]
	}
	if pos.reversed {
		slices.Reverse(ps)
	}
	b.Trail = []page.Level{ParksLevel}
	d := ParksData{Banner: b, Parks: []ParkRow{}, Page: p, View: ParksView{Counties: []CountyChoice{}, Walked: []ViewChoice{}, Gone: []ViewChoice{}, Sorts: []ViewChoice{}}}
	d.Summary.Count, d.Summary.Gone, err = cfg.Store.CountParks(r.Context())
	if err != nil {
		fail()
		return
	}
	d.Summary.Run, err = cfg.Store.ParksRun(r.Context())
	if err != nil {
		fail()
		return
	}
	d.Summary.RunPage = urls.RunPage(urls.Scrapes(cfg.ServicesPath), d.Summary.Run)
	rules, _, err := cfg.Store.ParkRules(r.Context())
	if err != nil {
		fail()
		return
	}
	read, err := cfg.Store.ParkCountiesRead(r.Context())
	if err != nil {
		fail()
		return
	}
	d.Rules = RulesView{Distance: strconv.FormatInt(rules.Distance, 10), MinParcels: rules.MinParcels, ValueThreshold: valueText(rules.ValueThreshold), Counties: []CountyLabels{}}
	d.Rules.WalkCeiling, err = cfg.Store.WalkCeiling(r.Context())
	if err != nil {
		fail()
		return
	}
	for _, x := range read {
		d.Rules.Counties = append(d.Rules.Counties, CountyLabels{x, append([]string{}, rules.Labels[x]...)})
	}
	for _, x := range append([]string{""}, counties...) {
		d.View.Counties = append(d.View.Counties, CountyChoice{x, viewHref(x, walked, g, s, ""), x == c})
	}
	for _, x := range []string{parks.WalkedAll, parks.WalkedUnwalked, parks.WalkedWalked} {
		d.View.Walked = append(d.View.Walked, ViewChoice{x, viewHref(c, x, g, s, ""), x == effectiveWalked(walked)})
	}
	for _, x := range []string{parks.GoneActive, parks.GoneGone, parks.GoneAll} {
		d.View.Gone = append(d.View.Gone, ViewChoice{x, viewHref(c, walked, x, s, ""), x == effectiveGone(g)})
	}
	for _, x := range []string{parks.SortMinutes, parks.SortValue, parks.SortParcels} {
		d.View.Sorts = append(d.View.Sorts, ViewChoice{x, viewHref(c, walked, g, x, ""), x == effectiveSort(s)})
	}
	for _, x := range ps {
		d.Parks = append(d.Parks, parkRow(x))
	}
	if p != "" {
		d.FirstHref = viewHref(c, walked, g, s, "")
	}
	if len(ps) > 0 {
		first, last := ps[0], ps[len(ps)-1]
		if p != "" {
			t := (paging.Token{Tool: "parks-before"}).Encode(effectiveSort(s), parkSortKey(first, s), first.ID)
			d.PrevHref = viewHref(c, walked, g, s, t)
		}
		if !pos.last && (more || pos.reversed) {
			t := (paging.Token{Tool: "parks"}).Encode(effectiveSort(s), parkSortKey(last, s), last.ID)
			d.NextHref = viewHref(c, walked, g, s, t)
			d.LastHref = viewHref(c, walked, g, s, "last")
		}
	}
	cfg.Pages.Write(w, r, 200, "parks", d)
}

func walkRow(w store.ParkWalk) WalkRow {
	v := WalkRow{Pass: w.Pass, Found: w.Found, NewCandidates: w.NewCandidates, Requests: w.Requests, Cost: costText(w.Cost), CeilingReached: w.CeilingReached}
	v.Walked, v.WalkedText = stamp(w.Walked, DateLayout)
	return v
}
func walkParkRow(w store.ParkWalk) WalkParkRow {
	return WalkParkRow{w.Park, w.Name, w.Found, w.NewCandidates, w.Requests, costText(w.Cost), w.CeilingReached}
}
func placeRow(x store.Place) PlaceRow {
	return PlaceRow{x.PlaceID, x.Name, x.Address, x.PrimaryType, x.Website, x.Phone, x.Walk, x.Candidate, x.New}
}
func parkLink(ctx context.Context, st *store.Store, id string) *ParkLink {
	if id == "" {
		return nil
	}
	p, err := st.GetPark(ctx, id)
	if err != nil {
		return nil
	}
	return &ParkLink{id, p.Name}
}
