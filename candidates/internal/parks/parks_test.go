package parks_test

import (
	"encoding/json"
	"math"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/scrapes"
	"github.com/ikigenba/ikigenba/candidates/internal/parks"
)

func equal(t *testing.T, a, b any) {
	t.Helper()
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("got %#v, want %#v", a, b)
	}
}
func fp(f float64) *float64 { return &f }
func ip(i int64) *int64     { return &i }

// R-SRY1-CTKE R-ST5X-QLB3 R-SUDU-4D1S
func TestDeclarations(t *testing.T) {
	type stringConsumer string
	type floatConsumer float32
	var words = []stringConsumer{parks.Scraper, parks.RuleLabel, parks.RuleValue, parks.RuleCandidate, parks.StateActive, parks.StateGone, parks.WalkedAll, parks.WalkedUnwalked, parks.WalkedWalked, parks.GoneActive, parks.GoneGone, parks.GoneAll, parks.SortMinutes, parks.SortValue, parks.SortParcels, parks.OwnerRole, parks.MapSearch}
	equal(t, len(words), 17)
	var sphere floatConsumer = parks.EarthRadius
	equal(t, float32(sphere), float32(6371008.8))

	equal(t, []string{parks.Scraper, parks.RuleLabel, parks.RuleValue, parks.RuleCandidate, parks.StateActive, parks.StateGone, parks.WalkedAll, parks.WalkedUnwalked, parks.WalkedWalked, parks.GoneActive, parks.GoneGone, parks.GoneAll, parks.SortMinutes, parks.SortValue, parks.SortParcels, parks.OwnerRole, parks.MapSearch}, []string{"mn_parcels", "label", "value", "candidate", "active", "gone", "all", "unwalked", "walked", "active", "gone", "all", "minutes", "value", "parcels", "owner", "https://www.google.com/maps/search/?api=1&query="})

	// Assignment to both floating and integer consumers exercises untyped constants.
	equal(t, []int64{parks.SeedDistance, parks.SeedMinParcels, parks.SeedValueThreshold, parks.DistanceMin, parks.DistanceMax, parks.MinParcelsMin, parks.MinParcelsMax, parks.ValueThresholdMin, parks.ValueThresholdMax, parks.MaxCountyBytes, parks.MaxLabelBytes, parks.MaxLabels}, []int64{600, 3, 1000000, 1, 5000, 2, 100, 0, 1000000000, 100, 200, 100})
	var floats = []float64{parks.SeedDistance, parks.SeedMinParcels, parks.SeedValueThreshold, parks.DistanceMin, parks.DistanceMax, parks.MinParcelsMin, parks.MinParcelsMax, parks.ValueThresholdMin, parks.ValueThresholdMax, parks.MaxCountyBytes, parks.MaxLabelBytes, parks.MaxLabels}
	equal(t, len(floats), 12)
	const earth = parks.EarthRadius
	radius := earth
	equal(t, radius, 6371008.8)

	// Conversion checks the complete field sets and order, including fields with zero values.
	p := parks.Parcel{"pin", "street", "city", "zip", "county", fp(1), fp(2), []string{"owner"}, []string{"label"}, "No", ip(3), parks.RuleLabel}
	equal(t, parks.Parcel(struct {
		PIN, Street, City, Zip, County string
		Lat, Lon                       *float64
		Owners, UseClass               []string
		Homestead                      string
		BuildingValue                  *int64
		Rule                           string
	}(p)), p)
	r := parks.Rules{600, 3, 1000000, map[string][]string{}}
	equal(t, parks.Rules(struct {
		Distance, MinParcels, ValueThreshold int64
		Labels                               map[string][]string
	}(r)), r)
	c := parks.Cluster{"county", "town", "zip", 1, 2, 3, 4, []parks.Parcel{p}}
	equal(t, parks.Cluster(struct {
		County, Town, Zip string
		Lat, Lon, Radius  float64
		BuildingValue     int64
		Parcels           []parks.Parcel
	}(c)), c)
	q := parks.Prior{"id", "county", "town", parks.StateGone, 1, 2, 3, time.Time{}, []string{"pin"}}
	equal(t, parks.Prior(struct {
		ID, County, Town, State  string
		Number, Count, GrownFrom int64
		Created                  time.Time
		PINs                     []string
	}(q)), q)
	u := parks.Update{"id", c, 2}
	equal(t, parks.Update(struct {
		ID        string
		Cluster   parks.Cluster
		GrownFrom int64
	}(u)), u)
	w := parks.New{c, 1}
	equal(t, parks.New(struct {
		Cluster parks.Cluster
		Number  int64
	}(w)), w)
	plan := parks.Plan{[]parks.Update{u}, []parks.New{w}, []string{"id"}, 1}
	equal(t, parks.Plan(struct {
		Updates []parks.Update
		News    []parks.New
		Gone    []string
		Grown   int64
	}(plan)), plan)
	signature1 := struct {
		call func(scrapes.Row) parks.Parcel
	}{parks.ParcelOf}
	if signature1.call == nil {
		t.Fatal("missing function")
	}
	signature2 := struct{ call func(string, string) string }{parks.AddressKey}
	if signature2.call == nil {
		t.Fatal("missing function")
	}
	signature3 := struct {
		call func(parks.Parcel, parks.Rules, map[string]bool) string
	}{parks.Classify}
	if signature3.call == nil {
		t.Fatal("missing function")
	}
	signature4 := struct {
		call func(float64, float64, float64, float64) float64
	}{parks.Distance}
	if signature4.call == nil {
		t.Fatal("missing function")
	}
	signature5 := struct {
		call func([]parks.Parcel, parks.Rules) []parks.Cluster
	}{parks.Clusters}
	if signature5.call == nil {
		t.Fatal("missing function")
	}
	signature6 := struct {
		call func([]parks.Cluster, []parks.Prior) parks.Plan
	}{parks.Match}
	if signature6.call == nil {
		t.Fatal("missing function")
	}
	signature7 := struct {
		call func(string, int64, int64) string
	}{parks.Name}
	if signature7.call == nil {
		t.Fatal("missing function")
	}
	signature8 := struct{ call func(float64, float64) string }{parks.MapURL}
	if signature8.call == nil {
		t.Fatal("missing function")
	}
	for _, f := range []func(int64) bool{parks.ValidDistance, parks.ValidMinParcels, parks.ValidValueThreshold} {
		if f == nil {
			t.Fatal("missing validator")
		}
	}
	for _, f := range []func(string) bool{parks.ValidCounty, parks.ValidLabel, parks.ValidWalked, parks.ValidGone, parks.ValidSort} {
		if f == nil {
			t.Fatal("missing validator")
		}
	}
	signature9 := struct{ call func([]string) bool }{parks.ValidLabels}
	if signature9.call == nil {
		t.Fatal("missing function")
	}
}

// R-SWTM-VWJ6
func TestParcelOf(t *testing.T) {
	r := scrapes.Row{SourceID: "pin", Lat: fp(45), Lon: fp(-93), Facts: map[string]json.RawMessage{}}
	// Build address and people using the published row fields, without assuming unrelated fields.
	r.Address.Street = "street"
	r.Address.City = "city"
	r.Address.Zip = "55056"
	r.Address.County = "county"
	for _, tc := range []struct {
		raw  string
		want []string
	}{{`["Industrial, Other"]`, []string{"Industrial, Other"}}, {`"Industrial"`, []string{"Industrial"}}, {`null`, []string{}}, {`5`, []string{}}, {`[3]`, []string{}}, {`{`, []string{}}, {`[]`, []string{}}} {
		r.Facts["use_class"] = json.RawMessage(tc.raw)
		p := parks.ParcelOf(r)
		equal(t, p.UseClass, tc.want)
		equal(t, p.PIN, r.SourceID)
		equal(t, p.Street, r.Address.Street)
		equal(t, p.City, r.Address.City)
		equal(t, p.Zip, r.Address.Zip)
		equal(t, p.County, r.Address.County)
		equal(t, p.Lat, r.Lat)
		equal(t, p.Lon, r.Lon)
		equal(t, p.Owners, []string{})
		equal(t, p.Rule, "")
	}
	for _, raw := range []string{`null`, `"1200000"`, `1200000.5`, `{`, ``} {
		r.Facts["emv_bldg"] = json.RawMessage(raw)
		equal(t, parks.ParcelOf(r).BuildingValue, (*int64)(nil))
	}
	r.Facts["emv_bldg"] = json.RawMessage(`1200000`)
	equal(t, parks.ParcelOf(r).BuildingValue, ip(1200000))
	for _, raw := range []string{`null`, `3`, `{`, ``} {
		r.Facts["homestead"] = json.RawMessage(raw)
		equal(t, parks.ParcelOf(r).Homestead, "")
	}
	r.Facts["homestead"] = json.RawMessage(`"Yes"`)
	equal(t, parks.ParcelOf(r).Homestead, "Yes")
	r.People = []scrapes.Person{{Name: "taxpayer", Role: "taxpayer"}, {Name: "owner-one", Role: parks.OwnerRole}, {Name: "other", Role: "other"}, {Name: "owner-two", Role: parks.OwnerRole}}
	equal(t, parks.ParcelOf(r).Owners, []string{"owner-one", "owner-two"})
	r.Lon = nil
	p := parks.ParcelOf(r)
	equal(t, p.Lat, (*float64)(nil))
	equal(t, p.Lon, (*float64)(nil))
}

// R-SY1J-9O9V
func TestAddressKey(t *testing.T) {
	for _, tc := range []struct{ s, z, w string }{{"123 Main St.", "55056", "123 MAIN ST|55056"}, {" 123  main st ", " 55056-1234 ", "123 MAIN ST|55056"}, {"12-B Elm", "55056", "12B ELM|55056"}, {"Å rue\t2!", "55056-", "Å RUE 2|55056"}, {"x", "", ""}, {"x", "5505", ""}, {"x", "55O56", ""}, {"x", "550567", ""}, {"...", "55056", ""}} {
		equal(t, parks.AddressKey(tc.s, tc.z), tc.w)
	}
}

// R-SZ9F-NG0K R-T1P8-EZHY
func TestClassification(t *testing.T) {
	r := parks.Rules{ValueThreshold: 1000000, Labels: map[string][]string{"labels": {"Industrial"}}}
	p := parks.Parcel{County: "labels", Street: "1 Main", Zip: "55056", BuildingValue: ip(5000000)}
	keys := map[string]bool{parks.AddressKey(p.Street, p.Zip): true}
	for _, tc := range []struct {
		u []string
		w string
	}{{[]string{"Residential, Industrial"}, parks.RuleLabel}, {[]string{"industrial"}, ""}, {[]string{"Indus  trial"}, ""}, {nil, ""}} {
		p.UseClass = tc.u
		equal(t, parks.Classify(p, r, keys), tc.w)
	}
	p.County = "fallback"
	for _, h := range []string{"No", "", "Unknown"} {
		p.Homestead = h
		equal(t, parks.Classify(p, r, keys), parks.RuleValue)
	}
	for _, h := range []string{"Yes", "Fractional"} {
		p.Homestead = h
		equal(t, parks.Classify(p, r, keys), parks.RuleCandidate)
		equal(t, parks.Classify(p, r, nil), "")
	}
	p.Homestead = "No"
	for _, b := range []*int64{nil, ip(1000000)} {
		p.BuildingValue = b
		equal(t, parks.Classify(p, r, nil), "")
		equal(t, parks.Classify(p, r, keys), parks.RuleCandidate)
	}
	p.Zip = ""
	equal(t, parks.Classify(p, r, keys), "")
}

// R-T2X4-SR8N
func TestDistance(t *testing.T) {
	equal(t, parks.Distance(45, -93, 45, -93), float64(0))
	for _, tc := range [][4]float64{{45.5, -93, 45.501, -93}, {0, 0, 0, 90}, {-30, -170, 50, 170}, {90, 0, -90, 0}} {
		a, b, c, d := tc[0]*math.Pi/180, tc[1]*math.Pi/180, tc[2]*math.Pi/180, tc[3]*math.Pi/180
		h := math.Pow(math.Sin((c-a)/2), 2) + math.Cos(a)*math.Cos(c)*math.Pow(math.Sin((d-b)/2), 2)
		want := 2 * 6371008.8 * math.Asin(math.Sqrt(h))
		got := parks.Distance(tc[0], tc[1], tc[2], tc[3])
		if math.Abs(got-want) > math.Max(1e-6, want*1e-9) {
			t.Fatal(got, want)
		}
		equal(t, got, parks.Distance(tc[2], tc[3], tc[0], tc[1]))
	}
}
func parcel(pin, county string, meters float64) parks.Parcel {
	return parks.Parcel{PIN: pin, County: county, Rule: parks.RuleLabel, Lat: fp(45 + meters/parks.EarthRadius*180/math.Pi), Lon: fp(-93)}
}

// R-T451-6IZC R-T5CX-KAQ1 R-T6KT-Y2GQ R-T7SQ-BU7F
func TestClusters(t *testing.T) {
	r := parks.Rules{Distance: 600, MinParcels: 3}
	ps := []parks.Parcel{parcel("c", "county", 1000), parcel("a", "county", 0), parcel("b", "county", 500)}
	ps[0].BuildingValue = ip(2000000)
	ps[1].BuildingValue = ip(1500000)
	ps[0].City = "Stacy"
	ps[1].City = "North Branch"
	ps[2].City = "North Branch"
	ps[0].Zip = "55092"
	ps[1].Zip = "55056"
	ps[2].Zip = "55056-1234"
	cs := parks.Clusters(ps, r)
	equal(t, len(cs), 1)
	c := cs[0]
	equal(t, []string{c.Parcels[0].PIN, c.Parcels[1].PIN, c.Parcels[2].PIN}, []string{"a", "b", "c"})
	equal(t, c.Parcels[0], ps[1])
	equal(t, c.County, "county")
	equal(t, c.Town, "North Branch")
	equal(t, c.Zip, "55056")
	equal(t, c.BuildingValue, int64(3500000))
	if math.Abs(c.Lat-*ps[2].Lat) > 1e-9 || math.Abs(c.Lon+93) > 1e-9 || math.Abs(c.Radius-500) > 1e-6 {
		t.Fatal(c)
	}
	equal(t, parks.Clusters(append(append([]parks.Parcel{}, ps...), ps[0], parcel("far", "county", 1701)), r), cs)
	equal(t, parks.Clusters(ps[:2], r), []parks.Cluster{})
	equal(t, parks.Clusters(nil, r), []parks.Cluster{})
	q := append([]parks.Parcel{}, ps...)
	q[1].County = "other"
	equal(t, parks.Clusters(q, r), []parks.Cluster{})
	q = append([]parks.Parcel{}, ps...)
	q[2].Rule = ""
	equal(t, parks.Clusters(q, r), []parks.Cluster{})
	q[2] = ps[2]
	q[2].Lat = nil
	equal(t, parks.Clusters(q, r), []parks.Cluster{})
	for i := range ps {
		ps[i].City = ""
		ps[i].Zip = ""
	}
	c = parks.Clusters(ps, r)[0]
	equal(t, c.Town, c.County)
	equal(t, c.Zip, "")
	r.MinParcels = 2
	ps[0].City = "Z"
	ps[1].City = "A"
	ps[0].Zip = "55092"
	ps[1].Zip = "55056"
	c = parks.Clusters(ps[:2], parks.Rules{Distance: 1200, MinParcels: 2})[0]
	equal(t, c.Town, "A")
	equal(t, c.Zip, "55056")
}

// R-T90M-PLY4 R-TA8J-3DOT R-TBGF-H5FI R-TCOB-UX67
func TestMatch(t *testing.T) {
	a := parks.Cluster{County: "county", Town: "town", Parcels: []parks.Parcel{{PIN: "a"}, {PIN: "b"}, {PIN: "c"}}}
	b := parks.Cluster{County: "county", Town: "town", Parcels: []parks.Parcel{{PIN: "d"}, {PIN: "e"}}}
	prior := []parks.Prior{{ID: "older", County: "county", Town: "town", State: parks.StateActive, Number: 1, Count: 2, PINs: []string{"a", "b", "d"}}, {ID: "newer", County: "county", Town: "town", State: parks.StateGone, Number: 2, Count: 1, Created: time.Unix(1, 0), PINs: []string{"b", "d", "e"}}, {ID: "lost", State: parks.StateActive}}
	p := parks.Match([]parks.Cluster{a, b}, prior)
	equal(t, p.Updates, []parks.Update{{ID: "older", Cluster: a, GrownFrom: 2}, {ID: "newer", Cluster: b, GrownFrom: 1}})
	equal(t, p.Grown, int64(2))
	equal(t, p.Gone, []string{"lost"})
	equal(t, p.News, []parks.New{})
	prior[0].GrownFrom = 1
	prior[0].Count = 4
	p = parks.Match([]parks.Cluster{a}, prior)
	equal(t, p.Updates[0].GrownFrom, int64(1))
	equal(t, p.Grown, int64(0))
	// More shared PINs wins before age; equal overlaps prefer age, then id.
	prior[1].PINs = []string{"a", "b", "c"}
	p = parks.Match([]parks.Cluster{a}, prior)
	equal(t, p.Updates[0].ID, "newer")
	prior[1].PINs = prior[0].PINs
	p = parks.Match([]parks.Cluster{a}, prior)
	equal(t, p.Updates[0].ID, "older")
	prior[1].Created = prior[0].Created
	prior[1].ID = "aaa"
	p = parks.Match([]parks.Cluster{a}, prior)
	equal(t, p.Updates[0].ID, "aaa")
	fresh := parks.Cluster{County: "county", Town: "town"}
	other := parks.Cluster{County: "other", Town: "town"}
	p = parks.Match([]parks.Cluster{fresh, fresh, other}, prior)
	equal(t, p.News, []parks.New{{Cluster: fresh, Number: 3}, {Cluster: fresh, Number: 4}, {Cluster: other, Number: 1}})
	p = parks.Match(nil, prior)
	equal(t, p.Updates, []parks.Update{})
	equal(t, p.News, []parks.New{})
	equal(t, p.Gone, []string{"older", "lost"})
	p = parks.Match(nil, nil)
	equal(t, p, parks.Plan{Updates: []parks.Update{}, News: []parks.New{}, Gone: []string{}})
}

// R-TDW8-8OWW R-TF44-MGNL
func TestPresentationValues(t *testing.T) {
	equal(t, parks.Name("Town", 1, 1), "Town")
	equal(t, parks.Name("Town", 2, 2), "Town 2")
	equal(t, parks.Name("Town", 1, 3), "Town 1")
	equal(t, parks.MapURL(45.4741, -92.9783), parks.MapSearch+"45.474100,-92.978300")
}

// R-TGC1-08EA R-THJX-E04Z R-TIRT-RRVO R-TL7M-JBD2
func TestValidators(t *testing.T) {
	for _, tc := range []struct {
		f      func(int64) bool
		lo, hi int64
	}{{parks.ValidDistance, 1, 5000}, {parks.ValidMinParcels, 2, 100}, {parks.ValidValueThreshold, 0, 1000000000}} {
		for _, n := range []int64{tc.lo, tc.hi, (tc.lo + tc.hi) / 2} {
			equal(t, tc.f(n), true)
		}
		for _, n := range []int64{tc.lo - 1, tc.hi + 1} {
			equal(t, tc.f(n), false)
		}
	}
	for _, s := range []string{"St. Louis", "Mille Lacs", strings.Repeat("x", 100)} {
		equal(t, parks.ValidCounty(s), true)
	}
	for _, s := range []string{"", " Anoka", "Anoka ", "bad\nname", string([]byte{255}), strings.Repeat("x", 101)} {
		equal(t, parks.ValidCounty(s), false)
	}
	for _, s := range []string{"3a INDUSTIAL PREFERENTIAL", "INDUSTRIAL (with Buildings)", "Industrial - Bare Land", strings.Repeat("x", 200)} {
		equal(t, parks.ValidLabel(s), true)
	}
	for _, s := range []string{"", " Industrial", "Industrial, Preferred", "bad\nname", strings.Repeat("x", 201), string([]byte{255})} {
		equal(t, parks.ValidLabel(s), false)
	}
	equal(t, parks.ValidLabels(nil), true)
	equal(t, parks.ValidLabels([]string{}), true)
	equal(t, parks.ValidLabels([]string{"a", "b"}), true)
	equal(t, parks.ValidLabels([]string{"a", "a"}), false)
	equal(t, parks.ValidLabels([]string{""}), false)
	ls := []string{}
	for i := range 101 {
		ls = append(ls, strings.Repeat("x", i+1))
	}
	equal(t, parks.ValidLabels(ls), false)
	for _, tc := range []struct {
		f     func(string) bool
		words []string
	}{{parks.ValidWalked, []string{"", "all", "unwalked", "walked"}}, {parks.ValidGone, []string{"", "active", "gone", "all"}}, {parks.ValidSort, []string{"", "minutes", "value", "parcels"}}} {
		for _, s := range tc.words {
			equal(t, tc.f(s), true)
		}
		for _, s := range []string{"Walked", "none", " all"} {
			equal(t, tc.f(s), false)
		}
	}
}

// R-T5CX-KAQ1 R-T451-6IZC
func TestMultipleClustersKeepOrderAndParcelValues(t *testing.T) {
	ps := []parks.Parcel{parcel("z2", "Z", 1), parcel("b2", "A", 10001), parcel("a2", "A", 1), parcel("z1", "Z", 0), parcel("b1", "A", 10000), parcel("a1", "A", 0)}
	ps[1].Rule = parks.RuleValue
	ps[2].Rule = parks.RuleCandidate
	got := parks.Clusters(ps, parks.Rules{Distance: 100, MinParcels: 2})
	equal(t, len(got), 3)
	equal(t, []string{got[0].County, got[1].County, got[2].County}, []string{"A", "A", "Z"})
	equal(t, got[0].Parcels, []parks.Parcel{ps[5], ps[2]})
	equal(t, got[1].Parcels, []parks.Parcel{ps[4], ps[1]})
	equal(t, got[2].Parcels, []parks.Parcel{ps[3], ps[0]})
}

// R-T90M-PLY4 R-TA8J-3DOT R-TCOB-UX67 R-TBGF-H5FI
func TestSplitClaimsFallbackAndIndexTie(t *testing.T) {
	cluster := func(pins ...string) parks.Cluster {
		c := parks.Cluster{County: "county", Town: "Town", Parcels: []parks.Parcel{}}
		for _, pin := range pins {
			c.Parcels = append(c.Parcels, parks.Parcel{PIN: pin})
		}
		return c
	}
	a, b := cluster("a", "b", "c", "d"), cluster("e", "f")
	old := parks.Prior{ID: "old", County: "county", Town: "Town", State: parks.StateActive, Number: 1, Count: 6, PINs: []string{"a", "b", "c", "d", "e", "f"}}
	fallback := parks.Prior{ID: "fallback", County: "county", Town: "Town", State: parks.StateGone, Number: 2, Count: 1, Created: time.Unix(1, 0), PINs: []string{"e"}}
	p := parks.Match([]parks.Cluster{a, b}, []parks.Prior{old, fallback})
	equal(t, p, parks.Plan{Updates: []parks.Update{{ID: "old", Cluster: a}, {ID: "fallback", Cluster: b, GrownFrom: 1}}, News: []parks.New{}, Gone: []string{}, Grown: 1})
	p = parks.Match([]parks.Cluster{a, b}, []parks.Prior{old})
	equal(t, p, parks.Plan{Updates: []parks.Update{{ID: "old", Cluster: a}}, News: []parks.New{{Cluster: b, Number: 2}}, Gone: []string{}})
	a, b = cluster("a", "b"), cluster("e", "f")
	p = parks.Match([]parks.Cluster{a, b}, []parks.Prior{old})
	equal(t, p, parks.Plan{Updates: []parks.Update{{ID: "old", Cluster: a}}, News: []parks.New{{Cluster: b, Number: 2}}, Gone: []string{}})
	// Distinct-PIN overlap ignores duplicates on both sides.
	a = cluster("a", "a")
	b = cluster("b", "c")
	old.PINs = []string{"a", "a", "b", "c"}
	p = parks.Match([]parks.Cluster{a, b}, []parks.Prior{old})
	equal(t, p.Updates, []parks.Update{{ID: "old", Cluster: b}})
}
