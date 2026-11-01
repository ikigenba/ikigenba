package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/ikigenba/ikigenba/sessions/internal/figures"
)

func rulesIn(ctx context.Context, tx *sql.Tx) ([]figures.Rule, error) {
	rows, err := tx.QueryContext(ctx, "SELECT host,directory,command,kind FROM rules ORDER BY host,directory,command")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]figures.Rule, 0)
	for rows.Next() {
		var r figures.Rule
		if err = rows.Scan(&r.Host, &r.Directory, &r.Command, &r.Kind); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
func pricesIn(ctx context.Context, tx *sql.Tx) ([]figures.Price, error) {
	rows, err := tx.QueryContext(ctx, "SELECT model,effective,input,cached,output,reasoning FROM prices ORDER BY model,effective")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]figures.Price, 0)
	for rows.Next() {
		var p figures.Price
		var date, a, b, c, d string
		if err = rows.Scan(&p.Model, &date, &a, &b, &c, &d); err != nil {
			return nil, err
		}
		p.Effective, _ = figures.ParseDate(date)
		p.Input, _ = figures.ParseRate(a)
		p.Cached, _ = figures.ParseRate(b)
		p.Output, _ = figures.ParseRate(c)
		p.Reasoning, _ = figures.ParseRate(d)
		out = append(out, p)
	}
	return out, rows.Err()
}
func awayIn(ctx context.Context, tx *sql.Tx) (time.Duration, error) {
	var m int
	err := tx.QueryRowContext(ctx, "SELECT away_minutes FROM settings WHERE id=1").Scan(&m)
	return time.Duration(m) * time.Minute, err
}

// Rules returns every rule in key order.
func (s *Store) Rules(ctx context.Context) ([]figures.Rule, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx); err != nil {
		return nil, err
	}
	var out []figures.Rule
	err := s.d.Read(ctx, func(tx *sql.Tx) error { var e error; out, e = rulesIn(ctx, tx); return e })
	if err != nil {
		return nil, inaccessible(err)
	}
	return out, nil
}

// Prices returns every price in model and effective-date order.
func (s *Store) Prices(ctx context.Context) ([]figures.Price, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx); err != nil {
		return nil, err
	}
	var out []figures.Price
	err := s.d.Read(ctx, func(tx *sql.Tx) error { var e error; out, e = pricesIn(ctx, tx); return e })
	if err != nil {
		return nil, inaccessible(err)
	}
	return out, nil
}

// AwayLimit returns the held limit in whole minutes.
func (s *Store) AwayLimit(ctx context.Context) (time.Duration, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx); err != nil {
		return 0, err
	}
	var out time.Duration
	err := s.d.Read(ctx, func(tx *sql.Tx) error { var e error; out, e = awayIn(ctx, tx); return e })
	if err != nil {
		return 0, inaccessible(err)
	}
	return out, nil
}
func validDirectory(d string) bool {
	return len(d) > 0 && d[0] == '/' && (len(d) == 1 || d[len(d)-1] != '/')
}
func (s *Store) change(ctx context.Context, fn func(*sql.Tx) error) (int, error) {
	err := s.d.Write(ctx, func(tx *sql.Tx) error {
		if err := fn(tx); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "UPDATE settings SET refresh_after_tool='',refresh_after_id='' WHERE id=1")
		return err
	})
	if err != nil {
		return 0, inaccessible(err)
	}
	return s.refresh(ctx)
}

// AddRule keeps a new rule and refreshes every root summary.
func (s *Store) AddRule(ctx context.Context, host, directory, command, kind string) (figures.Rule, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx); err != nil {
		return figures.Rule{}, 0, err
	}
	if host == "" {
		return figures.Rule{}, 0, refused(MissingHost)
	}
	if !validDirectory(directory) {
		return figures.Rule{}, 0, refused(BadDirectory)
	}
	if kind == string(figures.KindUntagged) {
		return figures.Rule{}, 0, refused(UntaggedRule)
	}
	k, ok := figures.ParseKind(kind)
	if !ok {
		return figures.Rule{}, 0, refused(UnknownKind)
	}
	r := figures.Rule{Host: host, Directory: directory, Command: command, Kind: k}
	n, err := s.change(ctx, func(tx *sql.Tx) error {
		var held int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM rules WHERE host=? AND directory=? AND command=?", host, directory, command).Scan(&held); err != nil {
			return err
		}
		if held != 0 {
			return refused(RuleHeld)
		}
		_, err := tx.ExecContext(ctx, "INSERT INTO rules(host,directory,command,kind) VALUES(?,?,?,?)", host, directory, command, kind)
		return err
	})
	if err != nil {
		return figures.Rule{}, 0, err
	}
	return r, n, nil
}

// RemoveRule removes and returns a held rule, then refreshes every root.
func (s *Store) RemoveRule(ctx context.Context, host, directory, command string) (figures.Rule, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx); err != nil {
		return figures.Rule{}, 0, err
	}
	var r figures.Rule
	n, err := s.change(ctx, func(tx *sql.Tx) error {
		err := tx.QueryRowContext(ctx, "SELECT host,directory,command,kind FROM rules WHERE host=? AND directory=? AND command=?", host, directory, command).Scan(&r.Host, &r.Directory, &r.Command, &r.Kind)
		if errors.Is(err, sql.ErrNoRows) {
			return refused(RuleAbsent)
		}
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "DELETE FROM rules WHERE host=? AND directory=? AND command=?", host, directory, command)
		return err
	})
	if err != nil {
		return figures.Rule{}, 0, err
	}
	return r, n, nil
}

// SetPrice keeps or replaces a price and refreshes every root summary.
func (s *Store) SetPrice(ctx context.Context, p PriceInput) (figures.Price, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx); err != nil {
		return figures.Price{}, 0, err
	}
	if p.Model == "" {
		return figures.Price{}, 0, refused(MissingModel)
	}
	date, ok := figures.ParseDate(p.Effective)
	if !ok {
		return figures.Price{}, 0, refused(BadDate)
	}
	rates := make([]figures.Rate, 0, 4)
	for _, v := range []string{p.Input, p.Cached, p.Output, p.Reasoning} {
		r, ok := figures.ParseRate(v)
		if !ok {
			return figures.Price{}, 0, refused(BadRate)
		}
		rates = append(rates, r)
	}
	out := figures.Price{Model: p.Model, Effective: date, Input: rates[0], Cached: rates[1], Output: rates[2], Reasoning: rates[3]}
	n, err := s.change(ctx, func(tx *sql.Tx) error {
		_, e := tx.ExecContext(ctx, "INSERT INTO prices(model,effective,input,cached,output,reasoning) VALUES(?,?,?,?,?,?) ON CONFLICT(model,effective) DO UPDATE SET input=excluded.input,cached=excluded.cached,output=excluded.output,reasoning=excluded.reasoning", p.Model, p.Effective, p.Input, p.Cached, p.Output, p.Reasoning)
		return e
	})
	if err != nil {
		return figures.Price{}, 0, err
	}
	return out, n, nil
}

// RemovePrice removes a held price and refreshes every root summary.
func (s *Store) RemovePrice(ctx context.Context, model, effective string) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx); err != nil {
		return 0, err
	}
	if _, ok := figures.ParseDate(effective); !ok {
		return 0, refused(BadDate)
	}
	return s.change(ctx, func(tx *sql.Tx) error {
		r, err := tx.ExecContext(ctx, "DELETE FROM prices WHERE model=? AND effective=?", model, effective)
		if err != nil {
			return err
		}
		n, err := r.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			return refused(PriceAbsent)
		}
		return nil
	})
}

// SetAwayLimit keeps the limit and refreshes every root summary.
func (s *Store) SetAwayLimit(ctx context.Context, minutes int) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx); err != nil {
		return 0, err
	}
	if minutes < MinAwayMinutes || minutes > MaxAwayMinutes {
		return 0, refused(AwayOutOfRange)
	}
	return s.change(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "UPDATE settings SET away_minutes=? WHERE id=1", minutes)
		return err
	})
}

// FinishRefresh completes a persisted pending refresh before serving reads.
func (s *Store) FinishRefresh(ctx context.Context) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx); err != nil {
		return 0, err
	}
	return s.refresh(ctx)
}
