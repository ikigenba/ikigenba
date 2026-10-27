package store

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"strconv"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/parks"
)

// Constants define the parks vocabulary and limits.
const (
	KindParks        = "parks"
	OriginRules      = "rules"
	OriginStart      = "start"
	TriggerParkRules = "edit_park_rules"
	ParkIDPrefix     = "park_"
)

// ValidParkID reports whether a park ID has the expected format.
func ValidParkID(s string) bool { return validID(s, ParkIDPrefix) }

// Park holds a stored park and its current display values.
type Park struct {
	ID, Name, County, Town, Zip, State string
	Number                             int64
	Lat, Lon, Radius                   float64
	Parcels, BuildingValue             int64
	Minutes                            *int64
	GrownFrom                          int64
	Walked                             time.Time
	WalkRun                            string
	Found                              int64
	Run                                string
	Created, Updated                   time.Time
}

// ParkParcel holds the stored details of one park parcel.
type ParkParcel struct {
	PIN               string
	Owners            []string
	Street, City, Zip string
	BuildingValue     *int64
	Rule              string
	Lat, Lon          float64
}

// ParkFilter holds the filters for listing parks.
type ParkFilter struct{ County, Walked, Gone string }

// ParkRulesEdit holds an optional set of park rule changes.
type ParkRulesEdit struct {
	Distance, MinParcels, ValueThreshold *int64
	County                               *string
	Labels                               []string
}

var parkColumns = columns("id:ID county:County town:Town number:Number state:State zip:Zip lat:Lat lon:Lon radius:Radius parcels:Parcels building_value:BuildingValue grown_from:GrownFrom walked:Walked walk_run:WalkRun found:Found run:Run created:Created updated:Updated")
var parkParcelColumns = columns("pin:PIN owners:Owners street:Street city:City zip:Zip building_value:BuildingValue rule:Rule lat:Lat lon:Lon")

// ParkRules reads the current park rules and known counties.
func (s *Store) ParkRules(ctx context.Context) (parks.Rules, []string, error) {
	var out parks.Rules
	var counties []string
	err := unreachable(s.readTx(ctx, func(tx *sql.Tx) error {
		var e error
		out, e = parkRules(ctx, tx)
		if e != nil {
			return e
		}
		counties, e = stringRows(ctx, tx, "SELECT county FROM park_labels UNION SELECT county FROM park_counties ORDER BY county")
		return e
	}))
	if err != nil {
		return parks.Rules{}, nil, err
	}
	return out, counties, nil
}
func parkRules(ctx context.Context, tx *sql.Tx) (parks.Rules, error) {
	r := parks.Rules{Labels: map[string][]string{}}
	if e := scanSQL(ctx, tx, "SELECT distance,min_parcels,value_threshold FROM park_rules WHERE id=1", nil, &r.Distance, &r.MinParcels, &r.ValueThreshold); e != nil {
		return r, e
	}
	rows, e := querySQL(ctx, tx, "SELECT county,label FROM park_labels ORDER BY county,position")
	if e != nil {
		return r, e
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var c, l string
		if e = rows.Scan(&c, &l); e != nil {
			return r, e
		}
		r.Labels[c] = append(r.Labels[c], l)
	}
	return r, rows.Err()
}
func stringRows(ctx context.Context, tx *sql.Tx, q string, args ...any) ([]string, error) {
	out := []string{}
	rows, e := querySQL(ctx, tx, q, args...)
	if e != nil {
		return nil, e
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var s string
		if e = rows.Scan(&s); e != nil {
			return nil, e
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// SetParkRules edits park rules and records their recalculation pass atomically.
func (s *Store) SetParkRules(ctx context.Context, e ParkRulesEdit, p Pass) (Pass, error) {
	var out Pass
	err := unreachable(s.writeTx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if !validPass(p) || p.Kind != KindParks || p.Origin != OriginRules {
			return ErrUnreachable
		}
		if e.Distance != nil && !parks.ValidDistance(*e.Distance) || e.MinParcels != nil && !parks.ValidMinParcels(*e.MinParcels) || e.ValueThreshold != nil && !parks.ValidValueThreshold(*e.ValueThreshold) || e.County != nil && !parks.ValidCounty(*e.County) || e.County == nil && len(e.Labels) > 0 || !parks.ValidLabels(e.Labels) {
			return ErrInvalid
		}
		var err error
		out, err = s.addPassTx(ctx, tx, p)
		if err != nil {
			return err
		}
		r, err := parkRules(ctx, tx)
		if err != nil {
			return err
		}
		if e.Distance != nil {
			r.Distance = *e.Distance
		}
		if e.MinParcels != nil {
			r.MinParcels = *e.MinParcels
		}
		if e.ValueThreshold != nil {
			r.ValueThreshold = *e.ValueThreshold
		}
		if err = execSQL(ctx, tx, "UPDATE park_rules SET distance=?,min_parcels=?,value_threshold=? WHERE id=1", r.Distance, r.MinParcels, r.ValueThreshold); err != nil {
			return err
		}
		if e.County != nil {
			if err = execSQL(ctx, tx, "DELETE FROM park_labels WHERE county=?", *e.County); err != nil {
				return err
			}
			for i, l := range e.Labels {
				if err = execSQL(ctx, tx, "INSERT INTO park_labels(county,position,label) VALUES(?,?,?)", *e.County, i+1, l); err != nil {
					return err
				}
			}
		}
		return nil
	}))
	if err != nil {
		return Pass{}, err
	}
	return out, nil
}

// ActiveAddressKeys pages distinct address keys of active candidates.
func (s *Store) ActiveAddressKeys(ctx context.Context, after string, n int) ([]string, string, error) {
	var out []string
	var cursor string
	err := unreachable(s.readTx(ctx, func(tx *sql.Tx) error {
		if n < 1 {
			return ErrUnreachable
		}
		rows, e := querySQL(ctx, tx, "SELECT id,street,zip FROM candidates WHERE status=? AND id>? ORDER BY id LIMIT ?", StatusActive, after, n)
		if e != nil {
			return e
		}
		defer func() { _ = rows.Close() }()
		out = []string{}
		for rows.Next() {
			var street, zip string
			if e = rows.Scan(&cursor, &street, &zip); e != nil {
				return e
			}
			if k := parks.AddressKey(street, zip); k != "" {
				out = append(out, k)
			}
		}
		return rows.Err()
	}))
	if err != nil {
		return nil, "", err
	}
	return out, cursor, nil
}

// PriorParks reads all prior park identities and their parcel PINs.
func (s *Store) PriorParks(ctx context.Context) ([]parks.Prior, error) {
	var out []parks.Prior
	err := unreachable(s.readTx(ctx, func(tx *sql.Tx) error {
		ps, e := readRecords[Park](ctx, tx, "parks", parkColumns, " ORDER BY id")
		if e != nil {
			return e
		}
		out = []parks.Prior{}
		for _, p := range ps {
			pins, e := stringRows(ctx, tx, "SELECT pin FROM park_parcels WHERE park=? ORDER BY pin", p.ID)
			if e != nil {
				return e
			}
			out = append(out, parks.Prior{ID: p.ID, County: p.County, Town: p.Town, State: p.State, Number: p.Number, Count: p.Parcels, GrownFrom: p.GrownFrom, Created: p.Created, PINs: pins})
		}
		return nil
	}))
	return out, err
}
func validCluster(c parks.Cluster) bool {
	seen := map[string]bool{}
	for _, p := range c.Parcels {
		if p.Lat == nil || p.Lon == nil || p.Rule != parks.RuleLabel && p.Rule != parks.RuleValue && p.Rule != parks.RuleCandidate || seen[p.PIN] {
			return false
		}
		seen[p.PIN] = true
	}
	return true
}

// ApplyParks stores a derivation and finishes its pass atomically.
func (s *Store) ApplyParks(ctx context.Context, pass, run string, counties []string, plan parks.Plan, c Counts) (Pass, error) {
	var out Pass
	err := unreachable(s.retryUnit(ctx, func(ctx context.Context, tx *sql.Tx) error {
		if run == "" || !nonnegative(c) {
			return ErrUnreachable
		}
		ids := map[string]bool{}
		for _, u := range plan.Updates {
			if !validCluster(u.Cluster) || ids[u.ID] {
				return ErrUnreachable
			}
			ids[u.ID] = true
		}
		for _, id := range plan.Gone {
			if ids[id] {
				return ErrUnreachable
			}
			ids[id] = true
		}
		for _, w := range plan.News {
			if !validCluster(w.Cluster) || w.Number < 1 {
				return ErrUnreachable
			}
		}
		for id := range ids {
			var n int64
			if e := scanSQL(ctx, tx, "SELECT count(*) FROM parks WHERE id=?", []any{id}, &n); e != nil {
				return e
			}
			if n == 0 {
				return ErrUnreachable
			}
		}
		ps, e := readRecords[Pass](ctx, tx, "passes", passColumns, " WHERE id=? AND kind=?", pass, KindParks)
		if e != nil {
			return e
		}
		if len(ps) == 0 {
			return ErrNotFound
		}
		p := ps[0]
		if p.Status != StatusRunning {
			return ErrEnded
		}
		now := s.now()
		write := func(p Park, cl parks.Cluster) error {
			p.Zip = cl.Zip
			p.Lat = cl.Lat
			p.Lon = cl.Lon
			p.Radius = cl.Radius
			p.BuildingValue = cl.BuildingValue
			p.Parcels = int64(len(cl.Parcels))
			p.State = parks.StateActive
			p.Run = run
			p.Updated = now
			if e := insertRecord(ctx, tx, "parks", parkColumns, p, nil); e != nil {
				return e
			}
			if e := execSQL(ctx, tx, "DELETE FROM park_parcels WHERE park=?", p.ID); e != nil {
				return e
			}
			for _, q := range cl.Parcels {
				owners := q.Owners
				if owners == nil {
					owners = []string{}
				}
				v := ParkParcel{PIN: q.PIN, Owners: owners, Street: q.Street, City: q.City, Zip: q.Zip, BuildingValue: q.BuildingValue, Rule: q.Rule, Lat: *q.Lat, Lon: *q.Lon}
				if e := insertRecord(ctx, tx, "park_parcels", parkParcelColumns, v, map[string]any{"park": p.ID}); e != nil {
					return e
				}
			}
			return nil
		}
		for _, u := range plan.Updates {
			ps, e := readRecords[Park](ctx, tx, "parks", parkColumns, " WHERE id=?", u.ID)
			if e != nil {
				return e
			}
			q := ps[0]
			q.GrownFrom = u.GrownFrom
			if e = write(q, u.Cluster); e != nil {
				return e
			}
		}
		for _, w := range plan.News {
			id, e := s.mintKey(ctx, tx, ParkIDPrefix)
			if e != nil {
				return e
			}
			q := Park{ID: id, County: w.Cluster.County, Town: w.Cluster.Town, Number: w.Number, Created: now}
			if e = write(q, w.Cluster); e != nil {
				return e
			}
		}
		for _, id := range plan.Gone {
			if e = execSQL(ctx, tx, "UPDATE parks SET state=?,updated=? WHERE id=?", parks.StateGone, now.Format(time.RFC3339), id); e != nil {
				return e
			}
		}
		if e = execSQL(ctx, tx, "DELETE FROM park_counties"); e != nil {
			return e
		}
		for _, county := range counties {
			if e = execSQL(ctx, tx, "INSERT OR IGNORE INTO park_counties(county) VALUES(?)", county); e != nil {
				return e
			}
		}
		if e = execSQL(ctx, tx, "UPDATE park_rules SET derived_run=? WHERE id=1", run); e != nil {
			return e
		}
		p.Status = StatusDone
		p.Finished = now
		p.ParcelRun = run
		setCounts(&p, c)
		if e = insertRecord(ctx, tx, "passes", passColumns, p, nil); e != nil {
			return e
		}
		out = p
		return nil
	}))
	if err != nil {
		return Pass{}, err
	}
	return out, nil
}

// ParkCountiesRead reads counties present in the last parcel derivation.
func (s *Store) ParkCountiesRead(ctx context.Context) ([]string, error) {
	var out []string
	err := unreachable(s.readTx(ctx, func(tx *sql.Tx) error {
		var e error
		out, e = stringRows(ctx, tx, "SELECT county FROM park_counties ORDER BY county")
		return e
	}))
	return out, err
}

// ParksRun reads the last successfully derived parcel run.
func (s *Store) ParksRun(ctx context.Context) (string, error) {
	var out string
	err := unreachable(s.readTx(ctx, func(tx *sql.Tx) error {
		return scanSQL(ctx, tx, "SELECT derived_run FROM park_rules WHERE id=1", nil, &out)
	}))
	return out, err
}
func readParks(ctx context.Context, tx *sql.Tx) ([]Park, error) {
	ps, e := readRecords[Park](ctx, tx, "parks", parkColumns, "")
	if e != nil {
		return nil, e
	}
	type town struct{ county, name string }
	counts := map[town]int64{}
	for _, p := range ps {
		counts[town{p.County, p.Town}]++
	}
	for i := range ps {
		p := &ps[i]
		p.Name = parks.Name(p.Town, p.Number, counts[town{p.County, p.Town}])
		if p.Zip != "" {
			var n int64
			e = scanSQL(ctx, tx, "SELECT minutes FROM zip_drive WHERE zip=?", []any{p.Zip}, &n)
			if e == nil {
				p.Minutes = &n
			} else if !errors.Is(e, sql.ErrNoRows) {
				return nil, e
			}
		}
	}
	return ps, nil
}

// GetPark reads one park.
func (s *Store) GetPark(ctx context.Context, id string) (Park, error) {
	var out Park
	err := unreachable(s.readTx(ctx, func(tx *sql.Tx) error {
		ps, e := readParks(ctx, tx)
		if e != nil {
			return e
		}
		for _, p := range ps {
			if p.ID == id {
				out = p
				return nil
			}
		}
		return ErrNotFound
	}))
	return out, err
}

// ParkParcels reads the parcels held by a park in PIN order.
func (s *Store) ParkParcels(ctx context.Context, id string) ([]ParkParcel, error) {
	var out []ParkParcel
	err := unreachable(s.readTx(ctx, func(tx *sql.Tx) error {
		var n int64
		if e := scanSQL(ctx, tx, "SELECT count(*) FROM parks WHERE id=?", []any{id}, &n); e != nil {
			return e
		}
		if n == 0 {
			return ErrNotFound
		}
		var e error
		out, e = readRecords[ParkParcel](ctx, tx, "park_parcels", parkParcelColumns, " WHERE park=? ORDER BY building_value IS NULL,building_value DESC,pin", id)
		return e
	}))
	return out, err
}

// CountParks counts active and gone parks.
func (s *Store) CountParks(ctx context.Context) (active, gone int64, err error) {
	err = unreachable(s.readTx(ctx, func(tx *sql.Tx) error {
		return scanSQL(ctx, tx, "SELECT count(CASE WHEN state=? THEN 1 END),count(CASE WHEN state=? THEN 1 END) FROM parks", []any{parks.StateActive, parks.StateGone}, &active, &gone)
	}))
	return
}

// ParkCountiesHeld reads the distinct counties of held parks.
func (s *Store) ParkCountiesHeld(ctx context.Context) ([]string, error) {
	var out []string
	err := unreachable(s.readTx(ctx, func(tx *sql.Tx) error {
		var e error
		out, e = stringRows(ctx, tx, "SELECT DISTINCT county FROM parks ORDER BY county")
		return e
	}))
	return out, err
}
func parkLess(a, b Park, s string) bool {
	switch s {
	case parks.SortValue:
		if a.BuildingValue != b.BuildingValue {
			return a.BuildingValue > b.BuildingValue
		}
	case parks.SortParcels:
		if a.Parcels != b.Parcels {
			return a.Parcels > b.Parcels
		}
	default:
		if a.Minutes == nil && b.Minutes != nil {
			return false
		}
		if a.Minutes != nil && b.Minutes == nil {
			return true
		}
		if a.Minutes != nil && *a.Minutes != *b.Minutes {
			return *a.Minutes < *b.Minutes
		}
	}
	return a.ID < b.ID
}
func parkPasses(p Park, f ParkFilter) bool {
	return (f.County == "" || p.County == f.County) && (f.Walked == "" || f.Walked == parks.WalkedAll || f.Walked == parks.WalkedUnwalked && p.Walked.IsZero() || f.Walked == parks.WalkedWalked && !p.Walked.IsZero()) && (f.Gone == parks.GoneAll || f.Gone == parks.GoneGone && p.State == parks.StateGone || (f.Gone == "" || f.Gone == parks.GoneActive) && p.State == parks.StateActive)
}
func (s *Store) parkWindow(ctx context.Context, f ParkFilter, order, key, id string, n int, reversed bool) ([]Park, error) {
	var out []Park
	err := unreachable(s.readTx(ctx, func(tx *sql.Tx) error {
		if n < 1 || !parks.ValidSort(order) || !parks.ValidWalked(f.Walked) || !parks.ValidGone(f.Gone) {
			return ErrUnreachable
		}
		edge := Park{ID: id}
		if id != "" {
			if key != "" || order != "" && order != parks.SortMinutes {
				v, e := strconv.ParseInt(key, 10, 64)
				if e != nil {
					return ErrUnreachable
				}
				switch order {
				case parks.SortValue:
					edge.BuildingValue = v
				case parks.SortParcels:
					edge.Parcels = v
				default:
					edge.Minutes = &v
				}
			}
		}
		ps, e := readParks(ctx, tx)
		if e != nil {
			return e
		}
		out = []Park{}
		for _, p := range ps {
			if !parkPasses(p, f) {
				continue
			}
			if id != "" {
				if !reversed && !parkLess(edge, p, order) || reversed && !parkLess(p, edge, order) {
					continue
				}
			}
			out = append(out, p)
		}
		sort.Slice(out, func(i, j int) bool {
			if reversed {
				return parkLess(out[j], out[i], order)
			}
			return parkLess(out[i], out[j], order)
		})
		if len(out) > n {
			out = out[:n]
		}
		return nil
	}))
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ListParks pages parks in the selected order.
func (s *Store) ListParks(ctx context.Context, f ParkFilter, order, key, id string, n int) ([]Park, error) {
	return s.parkWindow(ctx, f, order, key, id, n, false)
}

// ListParksReversed pages parks before a boundary in reverse order.
func (s *Store) ListParksReversed(ctx context.Context, f ParkFilter, order, key, id string, n int) ([]Park, error) {
	return s.parkWindow(ctx, f, order, key, id, n, true)
}
