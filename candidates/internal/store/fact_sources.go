package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
	"github.com/ikigenba/ikigenba/candidates/internal/places"
	"github.com/ikigenba/ikigenba/candidates/internal/routes"
	"github.com/ikigenba/ikigenba/candidates/internal/sources"
	"github.com/ikigenba/ikigenba/candidates/internal/website"
)

func sourceRecords(ls []Link) []sources.Record {
	out := make([]sources.Record, 0, len(ls))
	for _, l := range ls {
		out = append(out, sources.Record{Source: l.Scraper, SourceID: l.SourceID, Gone: l.Gone, Seen: l.LastSeen, Reading: l.Reading})
	}
	return out
}
func recordAddress(r *records, id string) sources.Sourced[sources.Address] {
	return sources.Pick(sources.Orders(), sourceRecords(linksFor(r, id)), ownerGiven(r.owners)).Address
}
func (s *Store) routeInputs(ctx context.Context, tx *sql.Tx, id string) (string, error) {
	c, err := candidateKey(ctx, tx, id)
	if err != nil {
		return "", err
	}
	r, err := candidateRecords(ctx, tx, c)
	if err != nil {
		return "", err
	}
	return (enrichers.DriveRoute{}).Inputs(enrichers.Subject{RecordAddress: recordAddress(r, id), Origin: s.cfg.Origin}), nil
}
func (s *Store) standingRoute(ctx context.Context, tx *sql.Tx, id string, a sources.Sourced[sources.Address]) (Enrichment, *enrichers.RouteFound, error) {
	in := (enrichers.DriveRoute{}).Inputs(enrichers.Subject{RecordAddress: a, Origin: s.cfg.Origin})
	if in == "" {
		return Enrichment{}, nil, nil
	}
	rows, err := querySQL(ctx, tx, "SELECT found,at FROM enrichments WHERE candidate=? AND kind=? AND inputs=? ORDER BY seq DESC", id, enrichers.KindDriveRoute, in)
	if err != nil {
		return Enrichment{}, nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var raw, at string
		if err = rows.Scan(&raw, &at); err != nil {
			return Enrichment{}, nil, err
		}
		var g enrichers.RouteFound
		if json.Unmarshal([]byte(raw), &g) != nil || g.Failed() || g.Minutes == nil {
			continue
		}
		stamp, err := time.Parse(time.RFC3339, at)
		if err != nil {
			return Enrichment{}, nil, err
		}
		return Enrichment{Candidate: id, Kind: enrichers.KindDriveRoute, Inputs: in, At: stamp.UTC()}, &g, nil
	}
	return Enrichment{}, nil, rows.Err()
}
func (s *Store) refreshRecords(ctx context.Context, tx *sql.Tx, r *records, id string, t time.Time) error {
	c := r.candidates[id]
	ls := linksFor(r, id)
	if c.Status != StatusActive || len(ls) == 0 && len(r.owners) == 0 {
		return nil
	}
	rs := sourceRecords(ls)
	given := ownerGiven(r.owners)
	p := sources.Pick(sources.Orders(), rs, given)
	e, g, err := s.standingRoute(ctx, tx, id, p.Address)
	if err != nil {
		return err
	}
	if g != nil && g.Precision == routes.PrecisionBuilding {
		x := strings.TrimSpace(g.Address)
		street, city, state, zip := places.Address(x)
		if strings.HasSuffix(x, ", USA") && street != "" && city != "" && state != "" && zip != "" {
			county := ""
			if state == p.Address.Value.State && zip == p.Address.Value.Zip {
				county = p.Address.Value.County
			}
			p = sources.Pick(sources.Orders(), rs, append(given, sources.Record{Source: sources.Route, SourceID: e.Inputs, Seen: e.At, Reading: sources.Reading{Street: street, City: city, State: state, Zip: zip, County: county}}))
		}
	}
	old := c
	c.Name, c.NameSource, c.NameRecord = p.Name.Value, p.Name.Source, p.Name.Key
	c.Street, c.City, c.State, c.Zip, c.County = p.Address.Value.Street, p.Address.Value.City, p.Address.Value.State, p.Address.Value.Zip, p.Address.Value.County
	c.AddressSource, c.AddressRecord = p.Address.Source, p.Address.Key
	c.Phone, c.PhoneSource, c.PhoneRecord = p.Phone.Value, p.Phone.Source, p.Phone.Key
	c.Email, c.EmailSource, c.EmailRecord = p.Email.Value, p.Email.Source, p.Email.Key
	c.People = append([]sources.Sourced[sources.Person]{}, p.People...)
	c.OtherNames = append([]sources.Sourced[string]{}, p.OtherNames...)
	c.OtherAddresses = append([]sources.Sourced[sources.Address]{}, p.OtherAddresses...)
	c.OtherPhones = append([]sources.Sourced[string]{}, p.OtherPhones...)
	c.OtherEmails = append([]sources.Sourced[string]{}, p.OtherEmails...)
	c.Class = ""
	c.Names = []string{}
	seen := map[string]bool{}
	add := func(n string) {
		if n != "" && !seen[n] {
			seen[n] = true
			c.Names = append(c.Names, n)
		}
	}
	add(c.Name)
	cargo := map[string]bool{}
	tags := displayLinks(ls)
	giving := tags
	for _, l := range tags {
		if !l.Gone {
			giving = nil
			for _, v := range tags {
				if !v.Gone {
					giving = append(giving, v)
				}
			}
			break
		}
	}
	for _, l := range giving {
		if l.Reading.Class != "" {
			c.Class = l.Reading.Class
			break
		}
	}
	for _, l := range tags {
		add(l.Reading.Name)
		for _, n := range l.Reading.Names {
			add(n)
		}
		for _, v := range l.Reading.Cargo {
			cargo[v] = true
		}
	}
	c.Cargo = sortedIDs(cargo)
	if reflect.DeepEqual(c, old) {
		return nil
	}
	c.Updated = t
	r.candidates[id] = c
	return putCandidate(ctx, tx, c)
}
func placesURL(ctx context.Context, tx *sql.Tx, id string) (string, error) {
	ls, err := readRecords[Link](ctx, tx, "links", linkColumns, " WHERE candidate=? AND scraper=?", id, places.Name)
	if err != nil {
		return "", err
	}
	r := emptyRecords()
	for _, l := range ls {
		r.links[sources.SourceKey(l.Scraper, l.SourceID)] = l
	}
	for _, l := range linksFor(r, id) {
		var w string
		err = scanSQL(ctx, tx, "SELECT website FROM places WHERE walk=? AND place_id=?", []any{l.Walk, l.SourceID}, &w)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return "", err
		}
		w = strings.TrimSpace(w)
		if website.Inputs(w, "") != "" {
			return w, nil
		}
	}
	return "", nil
}

// PlacesURL reads the offered website from the candidate's current place records.
func (s *Store) PlacesURL(ctx context.Context, id string) (string, error) {
	var out string
	err := unreachable(s.readTx(ctx, func(tx *sql.Tx) error {
		if _, err := candidateKey(ctx, tx, id); err != nil {
			return err
		}
		var err error
		out, err = placesURL(ctx, tx, id)
		return err
	}))
	if err != nil {
		return "", err
	}
	return out, nil
}

// Rebuild repairs a page of active candidates from their current records.
func (s *Store) Rebuild(ctx context.Context, after string, n int) (string, error) {
	last := ""
	err := unreachable(s.retryUnit(ctx, func(ctx context.Context, tx *sql.Tx) error {
		last = ""
		if n < 1 {
			return ErrUnreachable
		}
		cs, err := readRecords[Candidate](ctx, tx, "candidates INDEXED BY sqlite_autoindex_candidates_1", candidateColumns, " WHERE status=? AND id>? ORDER BY id LIMIT ?", StatusActive, after, n)
		if err != nil {
			return err
		}
		t := s.now()
		for _, c := range cs {
			last = c.ID
			r, err := candidateRecords(ctx, tx, c)
			if err != nil {
				return err
			}
			for key, l := range r.links {
				if l.Scraper != places.Name {
					continue
				}
				ps, err := readRecords[Place](ctx, tx, "places", placeColumns, " WHERE walk=? AND place_id=?", l.Walk, l.SourceID)
				if err != nil {
					return err
				}
				if len(ps) == 0 {
					continue
				}
				p := ps[0]
				l.Reading = places.Reading(places.Place{ID: p.PlaceID, Name: p.Name, Address: p.Address, PrimaryType: p.PrimaryType, Website: p.Website, Phone: p.Phone})
				if err = putLink(ctx, tx, l); err != nil {
					return err
				}
				r.links[key] = l
			}
			if err = s.refreshRecords(ctx, tx, r, c.ID, t); err != nil {
				return err
			}
		}
		return nil
	}))
	if err != nil {
		return "", err
	}
	return last, nil
}
