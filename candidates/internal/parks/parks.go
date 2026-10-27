// Package parks derives industrial parks from parcel records.
package parks

import (
	"encoding/json"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/ikigenba/ikigenba/appkit/scrapes"
)

// Constants define the parks vocabulary and limits.
const (
	Scraper            = "mn_parcels"
	RuleLabel          = "label"
	RuleValue          = "value"
	RuleCandidate      = "candidate"
	StateActive        = "active"
	StateGone          = "gone"
	WalkedAll          = "all"
	WalkedUnwalked     = "unwalked"
	WalkedWalked       = "walked"
	GoneActive         = "active"
	GoneGone           = "gone"
	GoneAll            = "all"
	SortMinutes        = "minutes"
	SortValue          = "value"
	SortParcels        = "parcels"
	OwnerRole          = "owner"
	MapSearch          = "https://www.google.com/maps/search/?api=1&query="
	SeedDistance       = 600
	SeedMinParcels     = 3
	SeedValueThreshold = 1000000
	DistanceMin        = 1
	DistanceMax        = 5000
	MinParcelsMin      = 2
	MinParcelsMax      = 100
	ValueThresholdMin  = 0
	ValueThresholdMax  = 1000000000
	MaxCountyBytes     = 100
	MaxLabelBytes      = 200
	MaxLabels          = 100
	EarthRadius        = 6371008.8
)

// Parcel holds one parcel record.
type Parcel struct {
	PIN, Street, City, Zip, County string
	Lat, Lon                       *float64
	Owners, UseClass               []string
	Homestead                      string
	BuildingValue                  *int64
	Rule                           string
}

// Rules holds the rules used to classify and cluster parcels.
type Rules struct {
	Distance, MinParcels, ValueThreshold int64
	Labels                               map[string][]string
}

// Cluster holds a connected group of qualifying parcels.
type Cluster struct {
	County, Town, Zip string
	Lat, Lon, Radius  float64
	BuildingValue     int64
	Parcels           []Parcel
}

// Prior holds a previously derived park for identity matching.
type Prior struct {
	ID, County, Town, State  string
	Number, Count, GrownFrom int64
	Created                  time.Time
	PINs                     []string
}

// Update holds a matched park and its replacement cluster.
type Update struct {
	ID        string
	Cluster   Cluster
	GrownFrom int64
}

// New holds an unmatched cluster and its assigned number.
type New struct {
	Cluster Cluster
	Number  int64
}

// Plan holds the changes produced by matching clusters.
type Plan struct {
	Updates []Update
	News    []New
	Gone    []string
	Grown   int64
}

// ParcelOf extracts a parcel from a scrapes row.
func ParcelOf(r scrapes.Row) Parcel {
	p := Parcel{PIN: r.SourceID, Street: r.Address.Street, City: r.Address.City, Zip: r.Address.Zip, County: r.Address.County, Owners: []string{}, UseClass: []string{}}
	if r.Lat != nil && r.Lon != nil {
		lat, lon := *r.Lat, *r.Lon
		p.Lat = &lat
		p.Lon = &lon
	}
	for _, x := range r.People {
		if x.Role == OwnerRole {
			p.Owners = append(p.Owners, x.Name)
		}
	}
	var us []string
	if json.Unmarshal(r.Facts["use_class"], &us) == nil && us != nil {
		p.UseClass = us
	} else {
		var u *string
		if json.Unmarshal(r.Facts["use_class"], &u) == nil && u != nil {
			p.UseClass = []string{*u}
		}
	}
	_ = json.Unmarshal(r.Facts["homestead"], &p.Homestead)
	var b *int64
	if json.Unmarshal(r.Facts["emv_bldg"], &b) == nil {
		p.BuildingValue = b
	}
	return p
}
func zipPrefix(s string) string {
	s = strings.TrimSpace(s)
	if len(s) < 5 || len(s) > 5 && s[5] != '-' {
		return ""
	}
	for _, b := range []byte(s[:5]) {
		if b < '0' || b > '9' {
			return ""
		}
	}
	return s[:5]
}

// AddressKey normalizes a street and ZIP into a match key.
func AddressKey(street, zip string) string {
	z := zipPrefix(zip)
	s := strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsSpace(r) {
			return r
		}
		return -1
	}, strings.ToUpper(street))
	s = strings.Join(strings.Fields(s), " ")
	if s == "" || z == "" {
		return ""
	}
	return s + "|" + z
}

// Classify returns the qualifying rule for a parcel.
func Classify(p Parcel, r Rules, keys map[string]bool) string {
	if ls := r.Labels[p.County]; len(ls) > 0 {
		for _, u := range p.UseClass {
			for _, x := range strings.Split(u, ",") {
				for _, l := range ls {
					if strings.TrimSpace(x) == l {
						return RuleLabel
					}
				}
			}
		}
		return ""
	}
	if p.Homestead != "Yes" && p.Homestead != "Fractional" && p.BuildingValue != nil && *p.BuildingValue > r.ValueThreshold {
		return RuleValue
	}
	if k := AddressKey(p.Street, p.Zip); k != "" && keys[k] {
		return RuleCandidate
	}
	return ""
}

// Distance returns the great-circle distance in metres.
func Distance(lat1, lon1, lat2, lon2 float64) float64 {
	a, b := lat1*math.Pi/180, lat2*math.Pi/180
	d, e := (lat2-lat1)*math.Pi/180, (lon2-lon1)*math.Pi/180
	h := math.Pow(math.Sin(d/2), 2) + math.Cos(a)*math.Cos(b)*math.Pow(math.Sin(e/2), 2)
	return 2 * EarthRadius * math.Asin(math.Sqrt(math.Min(1, h)))
}
func picked(p Parcel) bool {
	return p.Rule == RuleLabel || p.Rule == RuleValue || p.Rule == RuleCandidate
}

// Clusters groups qualifying parcels into connected parks.
func Clusters(ps []Parcel, r Rules) []Cluster {
	kept := []Parcel{}
	seen := map[string]bool{}
	for _, p := range ps {
		if !picked(p) || p.Lat == nil || p.Lon == nil || seen[p.PIN] {
			continue
		}
		seen[p.PIN] = true
		kept = append(kept, p)
	}
	parent := make([]int, len(kept))
	for i := range parent {
		parent[i] = i
	}
	root := func(i int) int {
		for parent[i] != i {
			parent[i] = parent[parent[i]]
			i = parent[i]
		}
		return i
	}
	// A three-dimensional grid on the sphere bounds comparisons while preserving links.
	type cell struct {
		county  string
		x, y, z int64
	}
	grid := map[cell][]int{}
	width := math.Max(1, float64(r.Distance))
	for i, p := range kept {
		a, b := *p.Lat*math.Pi/180, *p.Lon*math.Pi/180
		c := cell{p.County, int64(math.Floor(EarthRadius * math.Cos(a) * math.Cos(b) / width)), int64(math.Floor(EarthRadius * math.Cos(a) * math.Sin(b) / width)), int64(math.Floor(EarthRadius * math.Sin(a) / width))}
		for x := int64(-1); x <= 1; x++ {
			for y := int64(-1); y <= 1; y++ {
				for z := int64(-1); z <= 1; z++ {
					for _, j := range grid[cell{p.County, c.x + x, c.y + y, c.z + z}] {
						q := kept[j]
						if Distance(*p.Lat, *p.Lon, *q.Lat, *q.Lon) <= float64(r.Distance) {
							parent[root(i)] = root(j)
						}
					}
				}
			}
		}
		grid[c] = append(grid[c], i)
	}
	groups := map[int][]Parcel{}
	for i, p := range kept {
		k := root(i)
		groups[k] = append(groups[k], p)
	}
	out := []Cluster{}
	for _, ps := range groups {
		if int64(len(ps)) < r.MinParcels {
			continue
		}
		sort.Slice(ps, func(i, j int) bool { return ps[i].PIN < ps[j].PIN })
		c := Cluster{County: ps[0].County, Parcels: ps}
		towns, zips := map[string]int{}, map[string]int{}
		for _, p := range ps {
			c.Lat += *p.Lat
			c.Lon += *p.Lon
			if p.BuildingValue != nil {
				c.BuildingValue += *p.BuildingValue
			}
			if p.City != "" {
				towns[p.City]++
			}
			if z := zipPrefix(p.Zip); z != "" {
				zips[z]++
			}
		}
		c.Lat /= float64(len(ps))
		c.Lon /= float64(len(ps))
		for _, p := range ps {
			c.Radius = math.Max(c.Radius, Distance(c.Lat, c.Lon, *p.Lat, *p.Lon))
		}
		c.Town = mode(towns)
		if c.Town == "" {
			c.Town = c.County
		}
		c.Zip = mode(zips)
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].County != out[j].County {
			return out[i].County < out[j].County
		}
		return out[i].Parcels[0].PIN < out[j].Parcels[0].PIN
	})
	return out
}
func mode(m map[string]int) string {
	s := ""
	n := 0
	for k, v := range m {
		if v > n || v == n && k < s {
			s, n = k, v
		}
	}
	return s
}

// Match assigns clusters to prior park identities.
func Match(cs []Cluster, prior []Prior) Plan {
	type pair struct{ i, j, n int }
	pairs := []pair{}
	for i, c := range cs {
		for j, q := range prior {
			pins := map[string]bool{}
			for _, p := range q.PINs {
				pins[p] = true
			}
			n := 0
			used := map[string]bool{}
			for _, p := range c.Parcels {
				if pins[p.PIN] && !used[p.PIN] {
					n++
					used[p.PIN] = true
				}
			}
			if n > 0 {
				pairs = append(pairs, pair{i, j, n})
			}
		}
	}
	sort.Slice(pairs, func(i, j int) bool {
		a, b := pairs[i], pairs[j]
		if a.n != b.n {
			return a.n > b.n
		}
		x, y := prior[a.j], prior[b.j]
		if !x.Created.Equal(y.Created) {
			return x.Created.Before(y.Created)
		}
		if x.ID != y.ID {
			return x.ID < y.ID
		}
		return a.i < b.i
	})
	claims := map[int]int{}
	used := map[int]bool{}
	for _, p := range pairs {
		if _, ok := claims[p.i]; !ok && !used[p.j] {
			claims[p.i] = p.j
			used[p.j] = true
		}
	}
	out := Plan{Updates: []Update{}, News: []New{}, Gone: []string{}}
	type town struct{ county, name string }
	numbers := map[town]int64{}
	for _, q := range prior {
		k := town{q.County, q.Town}
		numbers[k] = max(numbers[k], q.Number)
	}
	for i, c := range cs {
		if j, ok := claims[i]; ok {
			q := prior[j]
			g := q.GrownFrom
			if int64(len(c.Parcels)) > q.Count {
				out.Grown++
				if g == 0 {
					g = q.Count
				}
			}
			out.Updates = append(out.Updates, Update{q.ID, c, g})
		} else {
			k := town{c.County, c.Town}
			numbers[k]++
			out.News = append(out.News, New{c, numbers[k]})
		}
	}
	for j, q := range prior {
		if !used[j] && q.State == StateActive {
			out.Gone = append(out.Gone, q.ID)
		}
	}
	return out
}

// Name formats a park display name.
func Name(town string, number, inTown int64) string {
	if inTown <= 1 {
		return town
	}
	return town + " " + strconv.FormatInt(number, 10)
}

// MapURL returns the map search URL for a point.
func MapURL(lat, lon float64) string {
	return MapSearch + strconv.FormatFloat(lat, 'f', 6, 64) + "," + strconv.FormatFloat(lon, 'f', 6, 64)
}

// ValidDistance reports whether a distance is within the allowed range.
func ValidDistance(n int64) bool { return n >= DistanceMin && n <= DistanceMax }

// ValidMinParcels reports whether a parcel minimum is within the allowed range.
func ValidMinParcels(n int64) bool { return n >= MinParcelsMin && n <= MinParcelsMax }

// ValidValueThreshold reports whether a value threshold is within the allowed range.
func ValidValueThreshold(n int64) bool { return n >= ValueThresholdMin && n <= ValueThresholdMax }
func validText(s string, n int) bool {
	return len(s) > 0 && len(s) <= n && utf8.ValidString(s) && s == strings.TrimSpace(s) && !strings.ContainsFunc(s, unicode.IsControl)
}

// ValidCounty reports whether a county is valid.
func ValidCounty(s string) bool { return validText(s, MaxCountyBytes) }

// ValidLabel reports whether a label is valid.
func ValidLabel(s string) bool { return validText(s, MaxLabelBytes) && !strings.Contains(s, ",") }

// ValidLabels reports whether labels are valid and distinct.
func ValidLabels(ls []string) bool {
	if len(ls) > MaxLabels {
		return false
	}
	seen := map[string]bool{}
	for _, s := range ls {
		if !ValidLabel(s) || seen[s] {
			return false
		}
		seen[s] = true
	}
	return true
}

// ValidWalked reports whether a walked filter is valid.
func ValidWalked(s string) bool {
	return s == "" || s == WalkedAll || s == WalkedUnwalked || s == WalkedWalked
}

// ValidGone reports whether a state filter is valid.
func ValidGone(s string) bool { return s == "" || s == GoneActive || s == GoneGone || s == GoneAll }

// ValidSort reports whether a park sort is valid.
func ValidSort(s string) bool {
	return s == "" || s == SortMinutes || s == SortValue || s == SortParcels
}
