package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/parks"
	"github.com/ikigenba/ikigenba/candidates/internal/places"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
)

// Place records a place as first found in a walk.
type Place struct {
	Walk, Park, PlaceID, Name, Address, PrimaryType, Website, Phone string
	Lat, Lon, Radius                                                float64
	Candidate                                                       string
	New                                                             bool
	At                                                              time.Time
}

// ParkWalk records one park's sweep.
type ParkWalk struct {
	Pass, Park, Name                              string
	Walked                                        time.Time
	Circles, Requests, Found, NewCandidates, Cost int64
	CeilingReached                                bool
}

// FoundCandidate is a candidate encountered in a park.
type FoundCandidate struct {
	ID, Name, Status string
	New              bool
}

// Sweep is the result to record for a park.
type Sweep struct {
	Pass, Park              string
	Found                   []places.Found
	Circles, Requests, Cost int64
	CeilingReached          bool
}

var placeColumns = columns("walk:Walk park:Park place_id:PlaceID name:Name address:Address primary_type:PrimaryType website:Website phone:Phone lat:Lat lon:Lon radius:Radius candidate:Candidate founded:New at:At")
var parkWalkColumns = columns("pass:Pass park:Park walked:Walked circles:Circles requests:Requests found:Found new_candidates:NewCandidates cost:Cost ceiling_reached:CeilingReached")

// WalkCeiling reads the saved request ceiling.
func (s *Store) WalkCeiling(ctx context.Context) (int64, error) {
	var out int64
	err := unreachable(s.readTx(ctx, func(tx *sql.Tx) error {
		return scanSQL(ctx, tx, "SELECT walk_ceiling FROM park_rules WHERE id=1", nil, &out)
	}))
	return out, err
}

// FinishWalk ends a running walk and saves its ceiling outcome.
func (s *Store) FinishWalk(ctx context.Context, pass string, c Counts, ceiling bool) (Pass, error) {
	return s.endWalk(ctx, pass, "", c, ceiling, false)
}

// FailWalk ends a walk while retaining its spent work.
func (s *Store) FailWalk(ctx context.Context, pass, reason string, c Counts) (Pass, error) {
	return s.endWalk(ctx, pass, reason, c, false, true)
}
func (s *Store) endWalk(ctx context.Context, pass, reason string, c Counts, ceiling, failed bool) (Pass, error) {
	var out Pass
	err := s.passWrite(ctx, " WHERE id=?", []any{pass}, func(_ *sql.Tx, r *records) error {
		if !nonnegative(c) || failed && !validReason(reason) {
			return ErrUnreachable
		}
		p, ok := r.passes[pass]
		if !ok {
			return ErrNotFound
		}
		if p.Kind != KindWalk || p.Status != StatusRunning {
			return ErrEnded
		}
		p.Finished = s.now()
		if !failed {
			p.Status = StatusDone
			setCounts(&p, c)
			p.CeilingReached = ceiling
		} else {
			p.Status = StatusFailed
			p.Reason = reason
			setCounts(&p, Counts{ParksWalked: c.ParksWalked, Circles: c.Circles, Requests: c.Requests, Found: c.Found, NewCandidates: c.NewCandidates, Cost: c.Cost})
			p.CeilingReached = false
		}
		r.passes[pass] = p
		out = p
		return nil
	})
	if err != nil {
		return Pass{}, err
	}
	return out, nil
}
func validReason(reason string) bool {
	return reason == ReasonScrapes || reason == ReasonInterrupted || reason == ReasonStore || reason == ReasonPrompts || reason == ReasonPlaces
}

func parkName(ctx context.Context, tx *sql.Tx, id string) (string, error) {
	var town string
	var number, n int64
	if err := scanSQL(ctx, tx, "SELECT town,number,(SELECT count(*) FROM parks q WHERE q.county=p.county AND q.town=p.town) FROM parks p WHERE id=?", []any{id}, &town, &number, &n); err != nil {
		return "", err
	}
	return parks.Name(town, number, n), nil
}

// PassWalks reads a pass's parks in recorded order.
func (s *Store) PassWalks(ctx context.Context, pass string) ([]ParkWalk, error) {
	return s.walks(ctx, " WHERE pass=? ORDER BY seq", pass)
}

// ParkWalks reads the park's newest walk first.
func (s *Store) ParkWalks(ctx context.Context, park string) ([]ParkWalk, error) {
	return s.walks(ctx, " WHERE park=? ORDER BY walked DESC,seq DESC", park)
}
func (s *Store) walks(ctx context.Context, where, id string) ([]ParkWalk, error) {
	var out []ParkWalk
	err := unreachable(s.readTx(ctx, func(tx *sql.Tx) error {
		var err error
		out, err = readRecords[ParkWalk](ctx, tx, "park_walks", parkWalkColumns, where, id)
		if err != nil {
			return err
		}
		for i := range out {
			out[i].Name, err = parkName(ctx, tx, out[i].Park)
			if err != nil {
				return err
			}
		}
		return nil
	}))
	return out, err
}

// ParkPlaces reads immutable places found in a park.
func (s *Store) ParkPlaces(ctx context.Context, park string) ([]Place, error) {
	var out []Place
	err := unreachable(s.readTx(ctx, func(tx *sql.Tx) error {
		var err error
		out, err = readRecords[Place](ctx, tx, "places", placeColumns, " WHERE park=? ORDER BY at DESC,walk,name,place_id", park)
		return err
	}))
	return out, err
}

// ParkCandidates reads the distinct candidate identities encountered.
func (s *Store) ParkCandidates(ctx context.Context, park string) ([]FoundCandidate, error) {
	out := []FoundCandidate{}
	err := unreachable(s.readTx(ctx, func(tx *sql.Tx) error {
		rows, err := querySQL(ctx, tx, "SELECT c.id,CASE WHEN c.status=? THEN c.name ELSE '' END,c.status,max(p.founded) FROM places p JOIN candidates c ON c.id=p.candidate WHERE p.park=? AND p.candidate<>'' GROUP BY c.id ORDER BY 2,c.id", StatusActive, park)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var f FoundCandidate
			if err = rows.Scan(&f.ID, &f.Name, &f.Status, &f.New); err != nil {
				return err
			}
			out = append(out, f)
		}
		return rows.Err()
	}))
	if err != nil {
		return nil, err
	}
	return out, nil
}

// CandidatePark reads an active candidate's park from its lowest eligible Place ID.
func (s *Store) CandidatePark(ctx context.Context, candidate string) (string, error) {
	var out string
	err := unreachable(s.readTx(ctx, func(tx *sql.Tx) error {
		c, err := candidateKey(ctx, tx, candidate)
		if err != nil {
			return err
		}
		if c.Status != StatusActive {
			return nil
		}
		err = scanSQL(ctx, tx, "SELECT park FROM links WHERE candidate=? AND scraper=? AND park<>'' ORDER BY source_id LIMIT 1", []any{candidate, places.Name}, &out)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	}))
	return out, err
}

// RecordSweep atomically attaches new places and records the park's walk.
func (s *Store) RecordSweep(ctx context.Context, sw Sweep) (ParkWalk, error) {
	var out ParkWalk
	err := unreachable(s.retryUnit(ctx, func(ctx context.Context, tx *sql.Tx) error {
		out = ParkWalk{}
		if sw.Pass == "" || sw.Park == "" || sw.Circles < 0 || sw.Requests < 0 || sw.Cost < 0 {
			return ErrUnreachable
		}
		for _, f := range sw.Found {
			if f.Place.ID == "" {
				return ErrUnreachable
			}
		}
		ps, err := readRecords[Pass](ctx, tx, "passes", passColumns, " WHERE id=?", sw.Pass)
		if err != nil {
			return err
		}
		if len(ps) == 0 {
			return ErrNotFound
		}
		var n int64
		if err = scanSQL(ctx, tx, "SELECT count(*) FROM parks WHERE id=?", []any{sw.Park}, &n); err != nil {
			return err
		}
		if n == 0 {
			return ErrNotFound
		}
		if ps[0].Kind != KindWalk || ps[0].Status != StatusRunning {
			return ErrEnded
		}
		now := s.now()
		name, err := parkName(ctx, tx, sw.Park)
		if err != nil {
			return err
		}
		out = ParkWalk{Pass: sw.Pass, Park: sw.Park, Name: name, Circles: sw.Circles, Requests: sw.Requests, Cost: sw.Cost}
		touched := map[string]bool{}
		oldURLs := map[string]string{}
		for _, f := range sw.Found {
			if err = scanSQL(ctx, tx, "SELECT count(*) FROM places WHERE walk=? AND place_id=?", []any{sw.Pass, f.Place.ID}, &n); err != nil {
				return err
			}
			if n > 0 {
				continue
			}
			reading := places.Reading(f.Place)
			ls, err := readRecords[Link](ctx, tx, "links", linkColumns, " WHERE scraper=? AND source_id=?", places.Name, f.Place.ID)
			if err != nil {
				return err
			}
			var l Link
			founded := false
			if len(ls) > 0 {
				l = ls[0]
				l.Reading = reading
			} else {
				id, reason, confidence, err := matchPlace(ctx, tx, reading)
				if err != nil {
					return err
				}
				if id == "" {
					id, err = s.mintKey(ctx, tx, CandidateIDPrefix)
					if err != nil {
						return err
					}
					c := Candidate{ID: id, Status: StatusActive, Names: []string{}, People: []sources.Sourced[sources.Person]{}, Cargo: []string{}, Created: now, Updated: now}
					if err = putCandidate(ctx, tx, c); err != nil {
						return err
					}
					founded = true
					out.NewCandidates++
				}
				l = Link{Candidate: id, Scraper: places.Name, SourceID: f.Place.ID, Reading: reading, Reason: reason, Confidence: confidence, FirstSeen: now}
			}
			if !touched[l.Candidate] {
				oldURLs[l.Candidate], err = placesURL(ctx, tx, l.Candidate)
				if err != nil {
					return err
				}
			}
			l.LastSeen = now
			l.Gone = false
			l.Walk = sw.Pass
			l.Park = sw.Park
			if err = putLink(ctx, tx, l); err != nil {
				return err
			}
			touched[l.Candidate] = true
			p := Place{Walk: sw.Pass, Park: sw.Park, PlaceID: f.Place.ID, Name: f.Place.Name, Address: f.Place.Address, PrimaryType: f.Place.PrimaryType, Website: f.Place.Website, Phone: f.Place.Phone, Lat: f.Circle.Lat, Lon: f.Circle.Lon, Radius: f.Circle.Radius, Candidate: l.Candidate, New: founded, At: now}
			if err = insertRecord(ctx, tx, "places", placeColumns, p, nil); err != nil {
				return err
			}
			out.Found++
		}
		for id := range touched {
			if err = s.refreshKey(ctx, tx, id, now); err != nil {
				return err
			}
			c, err := candidateKey(ctx, tx, id)
			if err != nil {
				return err
			}
			u, err := placesURL(ctx, tx, id)
			if err != nil {
				return err
			}
			if c.Status == StatusActive && u != oldURLs[id] {
				if err = s.projectFacts(ctx, tx, id); err != nil {
					return err
				}
			}
		}
		if sw.Circles > 0 {
			out.Walked = now
			out.CeilingReached = sw.CeilingReached
			if err = insertRecord(ctx, tx, "park_walks", parkWalkColumns, out, nil); err != nil {
				return err
			}
			latest, e := readRecords[ParkWalk](ctx, tx, "park_walks", parkWalkColumns, " WHERE park=? ORDER BY walked DESC,seq DESC LIMIT 1", sw.Park)
			if e != nil {
				return e
			}
			if err = execSQL(ctx, tx, "UPDATE parks SET walked=?,walk_run=?,found=? WHERE id=?", latest[0].Walked.Format(time.RFC3339), latest[0].Pass, latest[0].Found, sw.Park); err != nil {
				return err
			}
		}
		return nil
	}))
	if err != nil {
		return ParkWalk{}, err
	}
	return out, nil
}

func matchPlace(ctx context.Context, tx *sql.Tx, r sources.Reading) (string, string, float64, error) {
	for _, m := range []struct {
		key, column, reason string
		confidence          float64
	}{{r.PhoneKey, "phone_key", LinkPhone, .85}, {r.DomainKey, "domain_key", LinkDomain, .85}} {
		if m.key == "" {
			continue
		}
		var id string
		err := scanSQL(ctx, tx, "SELECT c.id FROM candidates c JOIN links l ON l.candidate=c.id WHERE c.status=? AND l."+m.column+"=? ORDER BY c.created,c.id LIMIT 1", []any{StatusActive, m.key}, &id)
		if err == nil {
			return id, m.reason, m.confidence, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return "", "", 0, err
		}
	}
	if r.State != "" && validZip(r.Zip) && r.NameKey != "" {
		var id string
		err := scanSQL(ctx, tx, "SELECT c.id FROM candidates c JOIN links l ON l.candidate=c.id WHERE c.status=? AND l.state=? AND l.zip=? AND (l.name_key=? OR EXISTS(SELECT 1 FROM link_name_keys k WHERE k.scraper=l.scraper AND k.source_id=l.source_id AND k.name_key=?)) ORDER BY c.created,c.id LIMIT 1", []any{StatusActive, r.State, r.Zip, r.NameKey, r.NameKey}, &id)
		if err == nil {
			return id, LinkMatchKey, .8, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return "", "", 0, err
		}
	}
	return "", LinkSourceKey, 1, nil
}
