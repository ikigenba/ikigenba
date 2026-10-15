package store

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/sources"
)

func (s *Store) mintKey(ctx context.Context, tx *sql.Tx, prefix string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	table := "candidates"
	if prefix == PassIDPrefix {
		table = "passes"
	}
	for range 8 {
		var b [8]byte
		if s.cfg.Rand == nil {
			return "", ErrUnreachable
		}
		if _, err := io.ReadFull(s.cfg.Rand, b[:]); err != nil {
			return "", ErrUnreachable
		}
		id := prefix + hex.EncodeToString(b[:])
		var exists int
		err := scanSQL(ctx, tx, "SELECT 1 FROM "+table+" WHERE id=?", []any{id}, &exists)
		if errors.Is(err, sql.ErrNoRows) {
			return id, nil
		}
		if err != nil {
			return "", err
		}
	}
	return "", ErrUnreachable
}
func emptyRecords() *records {
	return &records{candidates: map[string]Candidate{}, links: map[string]Link{}, estimates: map[string][]sources.Estimate{}, assessments: map[string]Assessment{}, rulings: []Ruling{}, passes: map[string]Pass{}, zips: map[string]int64{}}
}
func candidateKey(ctx context.Context, tx *sql.Tx, id string) (Candidate, error) {
	cs, err := readRecords[Candidate](ctx, tx, "candidates", candidateColumns, " WHERE id=?", id)
	if err != nil {
		return Candidate{}, err
	}
	if len(cs) == 0 {
		return Candidate{}, ErrNotFound
	}
	return cs[0], nil
}
func putCandidate(ctx context.Context, tx *sql.Tx, c Candidate) error {
	if err := insertRecord(ctx, tx, "candidates", candidateColumns, c, map[string]any{"names_lower": strings.ToLower(strings.Join(c.Names, "\n"))}); err != nil {
		return err
	}
	if err := execSQL(ctx, tx, "DELETE FROM candidate_cargo WHERE candidate=?", c.ID); err != nil {
		return err
	}
	for _, cargo := range c.Cargo {
		if err := execSQL(ctx, tx, "INSERT OR IGNORE INTO candidate_cargo(candidate,cargo) VALUES (?,?)", c.ID, cargo); err != nil {
			return err
		}
	}
	return nil
}
func putLink(ctx context.Context, tx *sql.Tx, l Link) error {
	return insertRecord(ctx, tx, "links", linkColumns, l, map[string]any{"name_key": l.Reading.NameKey, "state": l.Reading.State, "zip": l.Reading.Zip, "phone_key": l.Reading.PhoneKey, "domain_key": l.Reading.DomainKey})
}
func candidateRecords(ctx context.Context, tx *sql.Tx, c Candidate) (*records, error) {
	r := emptyRecords()
	r.candidates[c.ID] = c
	ls, err := readRecords[Link](ctx, tx, "links", linkColumns, " WHERE candidate=?", c.ID)
	if err != nil {
		return nil, err
	}
	for _, l := range ls {
		r.links[sources.SourceKey(l.Scraper, l.SourceID)] = l
	}
	return r, nil
}
func refreshKey(ctx context.Context, tx *sql.Tx, id string, t time.Time) error {
	c, err := candidateKey(ctx, tx, id)
	if err != nil {
		return err
	}
	if c.Status != StatusActive {
		return nil
	}
	r, err := candidateRecords(ctx, tx, c)
	if err != nil {
		return err
	}
	refresh(r, t)
	return putCandidate(ctx, tx, r.candidates[id])
}
func judgeKeys(ctx context.Context, tx *sql.Tx, ids []string, t time.Time) (Counts, error) {
	out := Counts{}
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		c, err := candidateKey(ctx, tx, id)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return Counts{}, err
		}
		if c.Status != StatusActive {
			continue
		}
		r, err := candidateRecords(ctx, tx, c)
		if err != nil {
			return Counts{}, err
		}
		r.rulings, err = readRecords[Ruling](ctx, tx, "decisions", rulingColumns, " WHERE candidate=? ORDER BY seq", id)
		if err != nil {
			return Counts{}, err
		}
		var minutes int64
		err = scanSQL(ctx, tx, "SELECT minutes FROM zip_drive WHERE zip=?", []any{c.Zip}, &minutes)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return Counts{}, err
		}
		if err == nil {
			r.zips[c.Zip] = minutes
		}
		es, a := assessment(r, c, t)
		if err = deleteRecord(ctx, tx, "estimates", "candidate=?", id); err != nil {
			return Counts{}, err
		}
		for _, e := range es {
			if err = insertRecord(ctx, tx, "estimates", estimateColumns, estimateRecord{id, e}, nil); err != nil {
				return Counts{}, err
			}
		}
		if err = insertRecord(ctx, tx, "assessments", assessmentColumns, a, nil); err != nil {
			return Counts{}, err
		}
		out.Judged++
		if a.RevenueLow == nil {
			out.Unranged++
		}
	}
	return out, nil
}

func (s *Store) passWrite(ctx context.Context, where string, args []any, fn func(*sql.Tx, *records) error) error {
	return unreachable(s.writeTx(ctx, func(ctx context.Context, tx *sql.Tx) error {
		ps, err := readRecords[Pass](ctx, tx, "passes", passColumns, where, args...)
		if err != nil {
			return err
		}
		r := emptyRecords()
		for _, p := range ps {
			r.passes[p.ID] = p
		}
		if err = fn(tx, r); err != nil {
			return err
		}
		for _, p := range r.passes {
			if err = insertRecord(ctx, tx, "passes", passColumns, p, nil); err != nil {
				return err
			}
		}
		return nil
	}))
}

func (s *Store) readScope(ctx context.Context, scope, id string, fn func(*records) error) error {
	return unreachable(s.readTx(ctx, func(tx *sql.Tx) error {
		r := emptyRecords()
		if scope == "pass" || scope == "running" {
			where := ""
			args := []any{}
			if scope == "pass" {
				where = " WHERE id=?"
				args = append(args, id)
			}
			if scope == "running" {
				where = " WHERE status=?"
				args = append(args, StatusRunning)
			}
			ps, err := readRecords[Pass](ctx, tx, "passes", passColumns, where, args...)
			if err != nil {
				return err
			}
			for _, p := range ps {
				r.passes[p.ID] = p
			}
			return fn(r)
		}
		if scope == "zip" {
			var m int64
			err := tx.QueryRowContext(ctx, "SELECT minutes FROM zip_drive WHERE zip=?", id).Scan(&m)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			if err == nil {
				r.zips[id] = m
			}
			return fn(r)
		}
		where := " WHERE id=?"
		args := []any{id}
		cs, err := readRecords[Candidate](ctx, tx, "candidates", candidateColumns, where, args...)
		if err != nil {
			return err
		}
		for _, c := range cs {
			r.candidates[c.ID] = c
		}
		switch scope {
		case "links":
			where = " WHERE candidate=?"
			args = []any{id}
			ls, e := readRecords[Link](ctx, tx, "links", linkColumns, where, args...)
			if e != nil {
				return e
			}
			for _, l := range ls {
				r.links[sources.SourceKey(l.Scraper, l.SourceID)] = l
			}
		case "estimates":
			es, e := readRecords[estimateRecord](ctx, tx, "estimates", estimateColumns, " WHERE candidate=? ORDER BY source", id)
			if e != nil {
				return e
			}
			for _, v := range es {
				r.estimates[id] = append(r.estimates[id], v.Estimate)
			}
		case "assessment":
			where = " WHERE candidate=?"
			args = []any{id}
			as, e := readRecords[Assessment](ctx, tx, "assessments", assessmentColumns, where, args...)
			if e != nil {
				return e
			}
			for _, a := range as {
				r.assessments[a.Candidate] = a
			}
		case "rulings":
			r.rulings, err = readRecords[Ruling](ctx, tx, "decisions", rulingColumns, " WHERE candidate=? OR other=? ORDER BY seq", id, id)
			if err != nil {
				return err
			}
		}
		return fn(r)
	}))
}

func (s *Store) writeTx(ctx context.Context, fn func(context.Context, *sql.Tx) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.d.Write(ctx, func(tx *sql.Tx) error {
		cache := map[string]*sql.Stmt{}
		defer func() {
			for _, stmt := range cache {
				_ = stmt.Close()
			}
		}()
		return fn(context.WithValue(ctx, statementCacheKey{}, cache), tx)
	})
}
func (s *Store) readTx(ctx context.Context, fn func(*sql.Tx) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.d.Read(ctx, fn)
}

type statementCacheKey struct{}

func preparedSQL(ctx context.Context, tx *sql.Tx, query string) (*sql.Stmt, error) {
	cache, ok := ctx.Value(statementCacheKey{}).(map[string]*sql.Stmt)
	if !ok {
		return nil, nil
	}
	if stmt := cache[query]; stmt != nil {
		return stmt, nil
	}
	stmt, err := tx.PrepareContext(ctx, query)
	if err == nil {
		cache[query] = stmt
	}
	return stmt, err
}
func querySQL(ctx context.Context, tx *sql.Tx, query string, args ...any) (*sql.Rows, error) {
	stmt, err := preparedSQL(ctx, tx, query)
	if err != nil {
		return nil, err
	}
	if stmt != nil {
		return stmt.QueryContext(ctx, args...)
	}
	return tx.QueryContext(ctx, query, args...)
}
func execSQL(ctx context.Context, tx *sql.Tx, query string, args ...any) error {
	stmt, err := preparedSQL(ctx, tx, query)
	if err != nil {
		return err
	}
	if stmt != nil {
		_, err = stmt.ExecContext(ctx, args...)
	} else {
		_, err = tx.ExecContext(ctx, query, args...)
	}
	return err
}

func scanSQL(ctx context.Context, tx *sql.Tx, query string, args []any, targets ...any) error {
	stmt, err := preparedSQL(ctx, tx, query)
	if err != nil {
		return err
	}
	if stmt != nil {
		return stmt.QueryRowContext(ctx, args...).Scan(targets...)
	}
	return tx.QueryRowContext(ctx, query, args...).Scan(targets...)
}
