package pages

import (
	"bytes"
	"context"
	"html/template"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/candidates/internal/places"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/store"
)

// R-R0YQ-42YM
func TestWalkViewShapes(_ *testing.T) {
	_ = struct {
		Pass, Walked, WalkedText       string
		Found, NewCandidates, Requests int64
		Cost                           string
		CeilingReached                 bool
	}(WalkRow{})
	_ = struct {
		ID, Name                       string
		Found, NewCandidates, Requests int64
		Cost                           string
		CeilingReached                 bool
	}(WalkParkRow{})
	_ = struct {
		PlaceID, Name, Address, PrimaryType, Website, Phone, Walk, Candidate string
		New                                                                  bool
	}(PlaceRow{})
	_ = struct {
		ID, Name, Status string
		New              bool
	}(FoundCandidateRow{})
	_ = struct{ ID, Name string }(ParkLink{})
}

// R-1LSW-R2N9 R-QF0J-87M4 R-QG8F-LZCT R-QL41-52BL
func TestWalkViewValues(t *testing.T) {
	w := store.ParkWalk{Pass: "chosen-pass", Park: "chosen-park", Name: "chosen-name", Walked: fixtureTime(), Circles: 139, Requests: 140, Found: 31, NewCandidates: 7, Cost: 4900000000, CeilingReached: true}
	if got := walkRow(w); got != (WalkRow{"chosen-pass", "2026-10-05T09:00:07Z", "2026-10-05", 31, 7, 140, "$4.900000", true}) {
		t.Fatal(got)
	}
	if got := walkParkRow(w); got != (WalkParkRow{"chosen-park", "chosen-name", 31, 7, 140, "$4.900000", true}) {
		t.Fatal(got)
	}
	if got := walkRow(store.ParkWalk{}); got != (WalkRow{Cost: "$0.000000"}) {
		t.Fatal(got)
	}
	x := store.Place{PlaceID: "chosen-place", Name: "chosen-place-name", Address: "chosen-address", PrimaryType: "chosen-type", Website: "https://chosen-website.test/", Phone: "chosen-phone", Walk: "chosen-walk", Candidate: "chosen-candidate", New: true}
	if got := placeRow(x); got != (PlaceRow{"chosen-place", "chosen-place-name", "chosen-address", "chosen-type", "https://chosen-website.test/", "chosen-phone", "chosen-walk", "chosen-candidate", true}) {
		t.Fatal(got)
	}
	for _, email := range []string{"", "walker@example.test"} {
		p := store.Pass{Kind: store.KindWalk, Origin: store.OriginWalk, Trigger: store.TriggerWalk, UserID: "walker", UserEmail: email, Circles: 9, Requests: 9, Found: 31, NewCandidates: 7, Cost: 315000000, CeilingReached: true}
		v := passView(p, "")
		by := email
		if by == "" {
			by = p.UserID
		}
		if v.Circles != 9 || v.Requests != 9 || v.Found != 31 || v.NewCandidates != 7 || v.Cost != "$0.315000" || !v.CeilingReached || v.StartedBy != by || v.RunPage != "" || v.Parks == nil || len(v.Parks) != 0 {
			t.Fatal(v)
		}
	}
	p := store.Park{WalkRun: "chosen-newest", Walked: fixtureTime(), Found: 11, NewCandidates: 3, CeilingReached: true}
	v := parkView(p, "")
	if v.Walk != p.WalkRun || v.Walked != "2026-10-05T09:00:07Z" || v.WalkedText != "2026-10-05" || v.Found != 11 || v.NewCandidates != 3 || !v.CeilingReached {
		t.Fatal(v)
	}
	v = parkView(store.Park{}, "")
	if v.Walk != "" || v.Walked != "" || v.WalkedText != "" || v.Found != 0 || v.NewCandidates != 0 || v.CeilingReached {
		t.Fatal(v)
	}
}

func captureWalkData[T any](t *testing.T, cfg Config, path, name string) T {
	t.Helper()
	set, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	var got T
	calls := 0
	x, err := set.templates.Funcs(template.FuncMap{"captureWalkData": func(d T) string { got = d; calls++; return "" }}).Parse(`{{define "` + name + `"}}{{captureWalkData .}}{{end}}`)
	if err != nil {
		t.Fatal(err)
	}
	cfgCapture := cfg
	cfgCapture.Pages = &Set{templates: x}
	w := requestPage(cfgCapture, "GET", path, "reader")
	if w.Code != 200 || calls != 1 {
		t.Fatal(path, w.Code, calls)
	}
	comparePage(t, cfg, path, name, 200, got)
	return got
}

// R-ON6O-ADSL R-1O8P-IM4N R-QMBX-IU2A R-QPZM-O5AD R-ZXPP-6T6P R-ZYXL-KKXE R-03T7-3NW6
func TestWalkPageData(t *testing.T) {
	cfg, ps := parksFixture(t, 3)
	for _, p := range ps {
		d := captureWalkData[ParkData](t, cfg, "/parks/"+p.ID, "park")
		if d.Walks == nil || d.Places == nil || d.FoundCandidates == nil || len(d.Walks)+len(d.Places)+len(d.FoundCandidates) != 0 {
			t.Fatal(d)
		}
	}
	var pass store.Pass
	for round := range 2 {
		var err error
		pass, err = cfg.Store.AddPass(t.Context(), store.Pass{Kind: store.KindWalk, Origin: store.OriginWalk, Trigger: store.TriggerWalk, UserID: "walker", UserEmail: "walker@example.test"})
		if err != nil {
			t.Fatal(err)
		}
		for i := range 2 {
			id := "first-place"
			name := "First business"
			if i == 1 {
				id, name = "second-place", "Second business"
			}
			_, err = cfg.Store.RecordSweep(t.Context(), store.Sweep{Pass: pass.ID, Park: ps[i].ID, Found: []places.Found{{Place: places.Place{ID: id, Name: name, Address: "12 Test St, Fixture, MN 55001, USA", PrimaryType: "fixture-type", Website: "https://" + id + ".test/", Phone: "555-000" + strconv.Itoa(i)}, Circle: places.Circle{Lat: 45, Lon: -93, Radius: 100}}}, Circles: 1, Requests: int64(i + 1), Cost: int64(i+1) * places.PriceNanos, CeilingReached: round == 1 && i == 1})
			if err != nil {
				t.Fatal(err)
			}
		}
		pass, err = cfg.Store.FinishWalk(t.Context(), pass.ID, store.Counts{ParksWalked: 2, Circles: 2, Requests: 3, Found: 2, Cost: 3 * places.PriceNanos}, round == 1)
		if err != nil {
			t.Fatal(err)
		}
	}
	wp, err := cfg.Store.PassWalks(t.Context(), pass.ID)
	if err != nil {
		t.Fatal(err)
	}
	dp := captureWalkData[PassData](t, cfg, "/passes/"+pass.ID, "pass")
	wantParks := []WalkParkRow{}
	for _, w := range wp {
		wantParks = append(wantParks, walkParkRow(w))
	}
	if !reflect.DeepEqual(dp.Pass.Parks, wantParks) || len(wantParks) != 2 {
		t.Fatal(dp.Pass.Parks, wantParks)
	}
	for _, p := range ps[:2] {
		d := captureWalkData[ParkData](t, cfg, "/parks/"+p.ID, "park")
		walks, err := cfg.Store.ParkWalks(t.Context(), p.ID)
		if err != nil {
			t.Fatal(err)
		}
		found, err := cfg.Store.ParkPlaces(t.Context(), p.ID)
		if err != nil {
			t.Fatal(err)
		}
		candidates, err := cfg.Store.ParkCandidates(t.Context(), p.ID)
		if err != nil {
			t.Fatal(err)
		}
		wantWalks, wantPlaces, wantFound := []WalkRow{}, []PlaceRow{}, []FoundCandidateRow{}
		for _, x := range walks {
			wantWalks = append(wantWalks, walkRow(x))
		}
		for _, x := range found {
			wantPlaces = append(wantPlaces, placeRow(x))
		}
		for _, x := range candidates {
			wantFound = append(wantFound, FoundCandidateRow{x.ID, x.Name, x.Status, x.New})
		}
		if !reflect.DeepEqual(d.Walks, wantWalks) || !reflect.DeepEqual(d.Places, wantPlaces) || !reflect.DeepEqual(d.FoundCandidates, wantFound) || len(d.Walks) != 2 {
			t.Fatal(d)
		}
		for _, c := range candidates {
			cd := captureWalkData[CandidateData](t, cfg, "/candidates/"+c.ID, "candidate")
			if cd.Candidate.Name != c.Name || cd.Candidate.NameSource != places.Name || cd.Candidate.AddressSource != places.Name || cd.Candidate.WebsiteSource != store.WebsiteByPlaces || cd.Candidate.WebsiteURL != "" || cd.Candidate.Website != sources.WebsiteUnknown {
				t.Fatal(cd.Candidate)
			}
			if cd.Candidate.Park == nil || *cd.Candidate.Park != (ParkLink{p.ID, p.Name}) {
				t.Fatal(cd.Candidate.Park)
			}
			links, err := cfg.Store.Links(t.Context(), c.ID)
			if err != nil {
				t.Fatal(err)
			}
			for i, l := range links {
				v := cd.Links[i]
				if v.Walk != l.Walk || v.Park == nil || *v.Park != (ParkLink{p.ID, p.Name}) || v.Run != "" || v.RowPage != "" {
					t.Fatal(v)
				}
			}
		}
	}
	// A different pass kind still receives a non-nil empty park slice.
	all, err := cfg.Store.ListPasses(t.Context(), "", "", 100)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range all {
		if p.Kind != store.KindWalk {
			d := captureWalkData[PassData](t, cfg, "/passes/"+p.ID, "pass")
			if d.Pass.Parks == nil || len(d.Pass.Parks) != 0 {
				t.Fatal(d)
			}
		}
	}
	// Finding the same place in another park updates the card and its link.
	first, err := cfg.Store.ParkCandidates(t.Context(), ps[0].ID)
	if err != nil || len(first) != 1 {
		t.Fatal(first, err)
	}
	second, err := cfg.Store.ParkCandidates(t.Context(), ps[1].ID)
	if err != nil || len(second) != 1 {
		t.Fatal(second, err)
	}
	pass, err = cfg.Store.AddPass(t.Context(), store.Pass{Kind: store.KindWalk, Origin: store.OriginWalk, Trigger: store.TriggerWalk, UserID: "walker"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = cfg.Store.RecordSweep(t.Context(), store.Sweep{Pass: pass.ID, Park: ps[2].ID, Found: []places.Found{{Place: places.Place{ID: "first-place", Name: "First business"}}}, Circles: 1, Requests: 1})
	if err != nil {
		t.Fatal(err)
	}
	cd := captureWalkData[CandidateData](t, cfg, "/candidates/"+first[0].ID, "candidate")
	if cd.Candidate.Park == nil || *cd.Candidate.Park != (ParkLink{ps[2].ID, ps[2].Name}) || cd.Links[0].Walk != pass.ID || cd.Links[0].Park == nil || cd.Links[0].Park.ID != ps[2].ID {
		t.Fatal(cd)
	}
	if _, _, err = cfg.Store.Decide(t.Context(), store.Ruling{Kind: store.KindMerge, Candidate: second[0].ID, Other: first[0].ID, Why: "chosen merge", UserID: "owner"}); err != nil {
		t.Fatal(err)
	}
	cd = captureWalkData[CandidateData](t, cfg, "/candidates/"+first[0].ID, "candidate")
	if cd.Candidate.Status != store.StatusMerged || cd.Candidate.Park != nil || len(cd.Links) != 0 {
		t.Fatal(cd)
	}
}

// R-OOEK-O5JA
func TestParkLink(t *testing.T) {
	cfg, ps := parksFixture(t, 1)
	if got := parkLink(t.Context(), cfg.Store, ps[0].ID); got == nil || *got != (ParkLink{ps[0].ID, ps[0].Name}) {
		t.Fatal(got)
	}
	for _, id := range []string{"", "park_ffffffffffffffff"} {
		if got := parkLink(t.Context(), cfg.Store, id); got != nil {
			t.Fatal(got)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if got := parkLink(ctx, cfg.Store, ps[0].ID); got != nil {
		t.Fatal(got)
	}
}

// R-1QOI-A5M1
func TestWalkCeilingOnParksPage(t *testing.T) {
	cfg, _ := pageFixture(t)
	for _, ceiling := range []int64{places.SeedCeiling, 271} {
		if ceiling != places.SeedCeiling {
			p, err := cfg.Store.SetParkRules(t.Context(), store.ParkRulesEdit{WalkCeiling: &ceiling}, store.Pass{Kind: store.KindParks, Origin: store.OriginRules, Trigger: store.TriggerParkRules, UserID: "owner"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err = cfg.Store.FinishPass(t.Context(), p.ID, store.Counts{}); err != nil {
				t.Fatal(err)
			}
		}
		if d := captureParks(t, cfg, "/parks"); d.Rules.WalkCeiling != ceiling {
			t.Fatal(d.Rules)
		}
	}
}

// R-J3KN-8MJW
func TestWalkTemplateStates(t *testing.T) {
	set, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	render := func(t *testing.T, name string, data any, values ...string) {
		t.Helper()
		var out bytes.Buffer
		if err := set.templates.ExecuteTemplate(&out, name, data); err != nil {
			t.Fatal(name, err)
		}
		for _, value := range values {
			if value != "" && !strings.Contains(out.String(), value) {
				t.Fatal(name, "missing supplied value", value)
			}
		}
	}
	for _, ceiling := range []bool{false, true} {
		for mask := range 8 {
			for optional := range 32 {
				d := ParkData{Park: ParkView{CeilingReached: ceiling}}
				values := []string{}
				if mask&1 != 0 {
					for i, reached := range []bool{false, true} {
						suffix := strconv.Itoa(i)
						row := WalkRow{Pass: "history-pass-token-" + suffix, WalkedText: "history-date-token-" + suffix, Cost: "history-cost-token-" + suffix, CeilingReached: reached}
						d.Walks = append(d.Walks, row)
						values = append(values, row.Pass, row.WalkedText, row.Cost)
					}
				}
				if mask&2 != 0 {
					for i, isNew := range []bool{false, true} {
						suffix := strconv.Itoa(i)
						row := PlaceRow{PlaceID: "place-identifier-token-" + suffix, Name: "place-name-token-" + suffix, Walk: "place-walk-token-" + suffix, New: isNew}
						values = append(values, row.PlaceID, row.Name, row.Walk)
						for field, value := range []*string{&row.Address, &row.PrimaryType, &row.Website, &row.Phone, &row.Candidate} {
							if optional&(1<<field) != 0 {
								*value = "place-option-token-" + suffix + "-" + strconv.Itoa(field)
								values = append(values, *value)
							}
						}
						d.Places = append(d.Places, row)
					}
				}
				if mask&4 != 0 {
					for _, status := range []string{store.StatusActive, store.StatusMerged} {
						for _, isNew := range []bool{false, true} {
							for _, hasName := range []bool{false, true} {
								suffix := strconv.Itoa(len(d.FoundCandidates))
								row := FoundCandidateRow{ID: "found-identifier-token-" + suffix, Status: status, New: isNew}
								if hasName {
									row.Name = "found-name-token-" + suffix
								}
								d.FoundCandidates = append(d.FoundCandidates, row)
								values = append(values, row.ID, row.Name)
							}
						}
					}
				}
				t.Run("park/ceiling="+strconv.FormatBool(ceiling)+"/slices="+strconv.Itoa(mask)+"/optional="+strconv.Itoa(optional), func(t *testing.T) {
					render(t, "park", d, values...)
				})
			}
		}
		for _, status := range []string{store.StatusRunning, store.StatusDone, store.StatusFailed} {
			for _, rows := range [][]WalkParkRow{nil, {
				{ID: "pass-park-identifier-first", Name: "pass-park-name-alpha", Cost: "pass-park-cost-one", CeilingReached: false},
				{ID: "pass-park-identifier-second", Name: "pass-park-name-beta", Cost: "pass-park-cost-two", CeilingReached: true},
			}} {
				p := PassView{Kind: store.KindWalk, Origin: store.OriginWalk, Trigger: store.TriggerWalk, StartedBy: "unique-walker", Status: status, CeilingReached: ceiling, Parks: rows}
				if status == store.StatusFailed {
					p.Reason = store.ReasonPlaces
				}
				values := []string{}
				for _, row := range rows {
					values = append(values, row.ID, row.Name, row.Cost)
				}
				render(t, "pass", PassData{Pass: p}, values...)
			}
		}
	}
	render(t, "pass", PassData{Pass: PassView{Kind: store.KindJudge}})
	for _, candidatePark := range []*ParkLink{nil, {ID: "card-park-identifier", Name: "card-park-name"}} {
		for _, linkPark := range []*ParkLink{nil, {ID: "link-park-identifier", Name: "link-park-name"}} {
			d := CandidateData{Candidate: CandidateView{Status: store.StatusActive, Park: candidatePark}, Links: []LinkView{{Scraper: places.Name, Walk: "unique-link-walk", Park: linkPark}, {}}}
			values := []string{"unique-link-walk"}
			if candidatePark != nil {
				values = append(values, candidatePark.ID, candidatePark.Name)
			}
			if linkPark != nil {
				values = append(values, linkPark.ID, linkPark.Name)
			}
			render(t, "candidate", d, values...)
		}
	}
	render(t, "parks", ParksData{Rules: RulesView{WalkCeiling: 3827}}, "3827")
}
