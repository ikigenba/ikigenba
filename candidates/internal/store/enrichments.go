package store

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/enrichers"
)

func enrichmentRecords(ctx context.Context, tx *sql.Tx, where string, args ...any) ([]Enrichment, error) {
	rows, err := querySQL(ctx, tx, "SELECT candidate,kind,inputs,source,found,cost,at FROM enrichments"+where, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []Enrichment{}
	for rows.Next() {
		var e Enrichment
		var at string
		var found string
		if err = rows.Scan(&e.Candidate, &e.Kind, &e.Inputs, &e.Source, &found, &e.Cost, &at); err != nil {
			return nil, err
		}
		e.Found = json.RawMessage(found)
		e.At, err = time.Parse(time.RFC3339, at)
		if err != nil {
			return nil, err
		}
		e.At = e.At.UTC()
		out = append(out, e)
	}
	return out, rows.Err()
}
func putEnrichment(ctx context.Context, tx *sql.Tx, e Enrichment) error {
	return execSQL(ctx, tx, "INSERT INTO enrichments(candidate,kind,inputs,source,found,cost,at) VALUES (?,?,?,?,?,?,?)", e.Candidate, e.Kind, e.Inputs, e.Source, string(e.Found), e.Cost, e.At.Format(time.RFC3339))
}

// AddEnrichments appends valid results and updates their facts.
func (s *Store) AddEnrichments(ctx context.Context, es []Enrichment) ([]Enrichment, error) {
	var out []Enrichment
	err := unreachable(s.writeTx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		reg := enrichers.New(nil)
		for _, e := range es {
			if !reg.Valid(e.Kind, e.Found) || e.Inputs == "" || e.Cost < 0 || math.IsNaN(e.Cost) || math.IsInf(e.Cost, 0) {
				return ErrUnreachable
			}
		}
		t := s.now()
		out = []Enrichment{}
		touched := map[string]bool{}
		for _, e := range es {
			c, err := candidateKey(ctx, tx, e.Candidate)
			if errors.Is(err, ErrNotFound) {
				continue
			}
			if err != nil {
				return err
			}
			if c.Status != StatusActive {
				continue
			}
			var b bytes.Buffer
			if err = json.Compact(&b, e.Found); err != nil {
				return err
			}
			e.Found = append(json.RawMessage(nil), b.Bytes()...)
			e.At = t
			if err = putEnrichment(ctx, tx, e); err != nil {
				return err
			}
			out = append(out, e)
			touched[c.ID] = true
		}
		for id := range touched {
			if err := projectFacts(ctx, tx, id); err != nil {
				return err
			}
		}
		return nil
	}))
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Enrichments reads results newest first.
func (s *Store) Enrichments(ctx context.Context, candidate string) ([]Enrichment, error) {
	var out []Enrichment
	err := unreachable(s.readTx(ctx, func(tx *sql.Tx) error {
		if _, err := candidateKey(ctx, tx, candidate); err != nil {
			return err
		}
		var err error
		out, err = enrichmentRecords(ctx, tx, " WHERE candidate=? ORDER BY seq DESC", candidate)
		return err
	}))
	return out, err
}

// EnrichmentMark captures the result sequence boundary.
func (s *Store) EnrichmentMark(ctx context.Context) (int64, error) {
	var mark int64
	err := unreachable(s.readTx(ctx, func(tx *sql.Tx) error {
		return scanSQL(ctx, tx, "SELECT COALESCE(MAX(seq),0) FROM enrichments", nil, &mark)
	}))
	return mark, err
}

// EnrichmentAfter finds a result recorded after a boundary.
func (s *Store) EnrichmentAfter(ctx context.Context, kind, inputs string, mark int64) (Enrichment, error) {
	var out Enrichment
	err := unreachable(s.readTx(ctx, func(tx *sql.Tx) error {
		if mark < 0 {
			return ErrUnreachable
		}
		es, err := enrichmentRecords(ctx, tx, " WHERE kind=? AND inputs=? AND seq>? ORDER BY seq DESC LIMIT 1", kind, inputs, mark)
		if err != nil {
			return err
		}
		if len(es) == 0 {
			return ErrNotFound
		}
		out = es[0]
		return nil
	}))
	return out, err
}
func subject(ctx context.Context, tx *sql.Tx, c Candidate) (enrichers.Subject, error) {
	r, err := candidateRecords(ctx, tx, c)
	if err != nil {
		return enrichers.Subject{}, err
	}
	out := enrichers.Subject{ID: c.ID, Email: c.Email, Zip: c.Zip, Links: []enrichers.Link{}}
	for _, l := range linksFor(r, c.ID) {
		out.Links = append(out.Links, enrichers.Link{Scraper: l.Scraper, SourceID: l.SourceID, Gone: l.Gone, Reading: l.Reading})
	}
	var m int64
	err = scanSQL(ctx, tx, "SELECT minutes FROM zip_drive WHERE zip=?", []any{c.Zip}, &m)
	if err == nil {
		out.Minutes = &m
	} else if !errors.Is(err, sql.ErrNoRows) {
		return out, err
	}
	return out, nil
}
func due(ctx context.Context, tx *sql.Tx, id, kind, inputs string) (bool, error) {
	if inputs == "" {
		return false, nil
	}
	es, err := enrichmentRecords(ctx, tx, " WHERE candidate=? AND kind=? ORDER BY seq DESC LIMIT 1", id, kind)
	return len(es) == 0 || es[0].Inputs != inputs, err
}

// DueEnrichments reads an ascending window of candidates with changed inputs.
func (s *Store) DueEnrichments(ctx context.Context, kind, after string, n int) ([]Due, error) {
	var out []Due
	err := unreachable(s.readTx(ctx, func(tx *sql.Tx) error {
		en, ok := enrichers.New(nil).Get(kind)
		if !ok || n < 1 {
			return ErrUnreachable
		}
		out = []Due{}
		rows, err := querySQL(ctx, tx, "SELECT id,email,zip,COALESCE((SELECT inputs FROM enrichments e WHERE e.candidate=c.id AND e.kind=? ORDER BY seq DESC LIMIT 1),'') FROM candidates c WHERE status=? AND id>? ORDER BY id", kind, StatusActive, after)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var id, email, zip, previous string
			if err = rows.Scan(&id, &email, &zip, &previous); err != nil {
				return err
			}
			su := enrichers.Subject{ID: id, Email: email, Zip: zip}
			if kind == enrichers.KindLinkFacts {
				c, err := candidateKey(ctx, tx, id)
				if err != nil {
					return err
				}
				su, err = subject(ctx, tx, c)
				if err != nil {
					return err
				}
			}
			in := en.Inputs(su)
			if in != "" && in != previous {
				out = append(out, Due{id, in})
				if len(out) == n {
					break
				}
			}
		}
		return rows.Err()
	}))
	return out, err
}
func deriveCandidate(ctx context.Context, tx *sql.Tx, c Candidate, d enrichers.Deriver, force bool, t time.Time) (bool, error) {
	su, err := subject(ctx, tx, c)
	if err != nil {
		return false, err
	}
	in := d.Inputs(su)
	if in == "" {
		return false, nil
	}
	yes, err := due(ctx, tx, c.ID, d.Kind(), in)
	if err != nil {
		return false, err
	}
	if !force && !yes {
		return false, nil
	}
	r := d.Derive(su)
	e := Enrichment{Candidate: c.ID, Kind: d.Kind(), Inputs: in, Source: r.Source, Found: r.Found, Cost: r.Cost, At: t}
	if err = putEnrichment(ctx, tx, e); err != nil {
		return false, err
	}
	return true, nil
}

// Derive records a window of results from local facts.
func (s *Store) Derive(ctx context.Context, kind, after string, force bool, n int) (string, int64, error) {
	var last string
	var count int64
	err := unreachable(s.writeTx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		en, _ := enrichers.New(nil).Get(kind)
		d, ok := en.(enrichers.Deriver)
		if !ok || n < 1 {
			return ErrUnreachable
		}
		cs, err := readRecords[Candidate](ctx, tx, "candidates", candidateColumns, " WHERE status=? AND id>? ORDER BY id LIMIT ?", StatusActive, after, n)
		if err != nil {
			return err
		}
		t := s.now()
		for _, c := range cs {
			last = c.ID
			added, err := deriveCandidate(ctx, tx, c, d, force, t)
			if err != nil {
				return err
			}
			if added {
				count++
				if err = projectFacts(ctx, tx, c.ID); err != nil {
					return err
				}
			}
		}
		return nil
	}))
	if err != nil {
		return "", 0, err
	}
	return last, count, nil
}
