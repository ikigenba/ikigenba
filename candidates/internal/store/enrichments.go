package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"math"
	"math/big"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/sources"
)

func enrichmentRecords(ctx context.Context, tx *sql.Tx, where string, args ...any) ([]Enrichment, error) {
	rows, err := querySQL(ctx, tx, "SELECT candidate,kind,found,source,at,cost FROM enrichments"+where, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []Enrichment{}
	for rows.Next() {
		var e Enrichment
		var found, at string
		if err = rows.Scan(&e.Candidate, &e.Kind, &found, &e.Source, &at, &e.Cost); err != nil {
			return nil, err
		}
		var body Enrichment
		if err = json.Unmarshal([]byte(found), &body); err != nil {
			return nil, err
		}
		e.State, e.URL, e.Title, e.StatusCode, e.Size, e.Error = body.State, body.URL, body.Title, body.StatusCode, body.Size, body.Error
		e.At, err = time.Parse(time.RFC3339, at)
		if err != nil {
			return nil, err
		}
		e.At = e.At.UTC()
		out = append(out, e)
	}
	return out, rows.Err()
}

// AddEnrichments records valid results for candidates that remain active.
func (s *Store) AddEnrichments(ctx context.Context, es []Enrichment) ([]Enrichment, error) {
	var out []Enrichment
	err := unreachable(s.writeTx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		for _, e := range es {
			if e.Kind != sources.WebsiteCheck || (e.State != sources.WebsiteHTTPS && e.State != sources.WebsiteHTTP && e.State != sources.WebsiteNone) || e.StatusCode < 0 || e.Size < 0 || e.Cost < 0 || math.IsNaN(e.Cost) || math.IsInf(e.Cost, 0) {
				return ErrUnreachable
			}
		}
		t := s.now()
		out = []Enrichment{}
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
			e.At = t
			e.URL = string([]rune(e.URL))
			e.Title = string([]rune(e.Title))
			e.Error = string([]rune(e.Error))
			body, err := json.Marshal(e)
			if err != nil {
				return err
			}
			if err = execSQL(ctx, tx, "INSERT INTO enrichments(candidate,kind,found,source,at,cost) VALUES (?,?,?,?,?,?)", e.Candidate, e.Kind, string(body), e.Source, t.Format(time.RFC3339), e.Cost); err != nil {
				return err
			}
			out = append(out, e)
		}
		return nil
	}))
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Enrichments reads results in reverse recorded order.
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

// EnrichmentMark captures the current recorded-result boundary.
func (s *Store) EnrichmentMark(ctx context.Context) (int64, error) {
	var mark int64
	err := unreachable(s.readTx(ctx, func(tx *sql.Tx) error {
		return scanSQL(ctx, tx, "SELECT COALESCE(MAX(seq),0) FROM enrichments", nil, &mark)
	}))
	return mark, err
}

// EnrichmentAfter reads the latest result following a recorded boundary.
func (s *Store) EnrichmentAfter(ctx context.Context, kind, source string, mark int64) (Enrichment, error) {
	var out Enrichment
	err := unreachable(s.readTx(ctx, func(tx *sql.Tx) error {
		if mark < 0 {
			return ErrUnreachable
		}
		es, err := enrichmentRecords(ctx, tx, " WHERE kind=? AND source=? AND seq>? ORDER BY seq DESC LIMIT 1", kind, source, mark)
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

func enrichmentOld(at, t time.Time, days int64) bool {
	// Compute seconds without overflowing either time.Duration or int64 when
	// the caller supplies an extreme day count.
	var cutoff, offset big.Int
	cutoff.SetInt64(t.Unix())
	offset.SetInt64(days)
	offset.Mul(&offset, big.NewInt(86400))
	cutoff.Sub(&cutoff, &offset)
	minimum := big.NewInt(time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC).Unix())
	return cutoff.Cmp(minimum) >= 0 && cutoff.Cmp(big.NewInt(at.Unix())) >= 0
}

// DueEnrichments reads an ascending candidate window eligible for refresh.
func (s *Store) DueEnrichments(ctx context.Context, kind, after string, live, none int64, n int) ([]Candidate, error) {
	var out []Candidate
	err := unreachable(s.readTx(ctx, func(tx *sql.Tx) error {
		if n < 1 {
			return ErrUnreachable
		}
		t := s.now()
		out = []Candidate{}
		cs, err := readRecords[Candidate](ctx, tx, "candidates", candidateColumns, " WHERE status=? AND email<>'' AND id>? ORDER BY id", StatusActive, after)
		if err != nil {
			return err
		}
		for _, c := range cs {
			es, err := enrichmentRecords(ctx, tx, " WHERE candidate=? AND kind=? ORDER BY seq DESC LIMIT 1", c.ID, kind)
			if err != nil {
				return err
			}
			due := len(es) == 0
			if !due {
				e := es[0]
				switch e.State {
				case sources.WebsiteHTTPS, sources.WebsiteHTTP:
					due = enrichmentOld(e.At, t, live)
				case sources.WebsiteNone:
					due = enrichmentOld(e.At, t, none)
				}
			}
			if due {
				out = append(out, c)
				if len(out) == n {
					break
				}
			}
		}
		return nil
	}))
	return out, err
}
