package store

import (
	"context"
	"database/sql"
	"errors"
	"math/big"
	"time"
)

const runColumns = "id,script,sha,ref,user,request_id,trigger,event,status,exit_code,started,finished,stdout_bytes,stderr_bytes,truncated,reason"

type scanner interface{ Scan(...any) error }

func scanRun(row scanner) (Run, error) {
	var r Run
	var started string
	var finished sql.NullString
	err := row.Scan(&r.ID, &r.Script, &r.SHA, &r.Ref, &r.User, &r.RequestID, &r.Trigger, &r.Event, &r.Status, &r.ExitCode, &started, &finished, &r.StdoutBytes, &r.StderrBytes, &r.Truncated, &r.Reason)
	if err != nil {
		return Run{}, err
	}
	r.Started, err = time.Parse(time.RFC3339, started)
	if err != nil {
		return Run{}, err
	}
	if finished.Valid {
		r.Finished, err = time.Parse(time.RFC3339, finished.String)
	}
	return r, err
}
func (c *catalog) readRun(ctx context.Context, query string, args ...any) (Run, error) {
	r, err := scanRun(c.tx.QueryRowContext(ctx, query, args...))
	if errors.Is(err, sql.ErrNoRows) {
		err = ErrNotFound
	}
	return r, err
}
func (c *catalog) readRunsSQL(ctx context.Context, query string, args ...any) ([]Run, error) {
	out := []Run{}
	rows, err := c.tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
func (c *catalog) windowRuns(ctx context.Context, script, started, id string, n int) ([]Run, error) {
	if id == "" {
		return c.readRunsSQL(ctx, "SELECT "+runColumns+" FROM runs WHERE script=? ORDER BY started DESC,id DESC LIMIT ?", script, max(n, 0))
	}
	return c.readRunsSQL(ctx, "SELECT "+runColumns+" FROM runs WHERE script=? AND (started,id)<(?,?) ORDER BY started DESC,id DESC LIMIT ?", script, started, id, max(n, 0))
}
func (c *catalog) readScripts(ctx context.Context, query string, args ...any) ([]Script, error) {
	out := []Script{}
	rows, err := c.tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var sc Script
		var created string
		if err = rows.Scan(&sc.ID, &sc.Name, &sc.Owner, &sc.Repo, &sc.Ref, &created); err != nil {
			break
		}
		sc.Created, err = time.Parse(time.RFC3339, created)
		if err != nil {
			break
		}
		out = append(out, sc)
	}
	if err == nil {
		err = rows.Err()
	}
	_ = rows.Close()
	if err != nil {
		return nil, err
	}
	for i := range out {
		if err = c.decorate(ctx, &out[i]); err != nil {
			return nil, err
		}
	}
	return out, nil
}
func (c *catalog) readScript(ctx context.Context, query string, args ...any) (Script, error) {
	ss, err := c.readScripts(ctx, query, args...)
	if err != nil {
		return Script{}, err
	}
	if len(ss) == 0 {
		return Script{}, ErrNotFound
	}
	return ss[0], nil
}
func (c *catalog) decorate(ctx context.Context, sc *Script) error {
	sc.Subscriptions = []Subscription{}
	rows, err := c.tx.QueryContext(ctx, "SELECT event,created FROM subscriptions WHERE script=? ORDER BY event", sc.ID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var sub Subscription
		var created int64
		if err = rows.Scan(&sub.Event, &created); err != nil {
			break
		}
		sub.Created = time.Unix(created, 0).UTC()
		sc.Subscriptions = append(sc.Subscriptions, sub)
	}
	if err == nil {
		err = rows.Err()
	}
	_ = rows.Close()
	if err != nil {
		return err
	}
	r, err := c.readRun(ctx, "SELECT "+runColumns+" FROM runs WHERE id=(SELECT id FROM runs WHERE script=? ORDER BY started DESC,id DESC LIMIT 1)", sc.ID)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	sc.Last = &r
	return nil
}
func (c *catalog) load(ctx context.Context) error {
	ss, err := c.readScripts(ctx, "SELECT id,name,owner,repo,ref,created FROM scripts")
	if err != nil {
		return err
	}
	c.subscriptions = map[string][]Subscription{}
	for _, sc := range ss {
		c.subscriptions[sc.ID] = sc.Subscriptions
		sc.Last = nil
		sc.Subscriptions = nil
		c.data.Scripts[sc.ID] = sc
	}
	rr, err := c.readRunsSQL(ctx, "SELECT "+runColumns+" FROM runs")
	if err != nil {
		return err
	}
	for _, r := range rr {
		c.data.Runs[r.ID] = r
	}
	return nil
}
func (c *catalog) readPastKeeping(ctx context.Context, now time.Time, days, count int64) ([]Run, error) {
	if days < 1 || count < 1 {
		return nil, errors.New("invalid retention")
	}
	// An integer second start is old enough iff strictly below this boundary.
	seconds := new(big.Int).Mul(big.NewInt(days), big.NewInt(86400))
	boundary := new(big.Int).Sub(big.NewInt(now.Unix()), seconds)
	if !boundary.IsInt64() {
		return []Run{}, nil
	}
	cutoff := time.Unix(boundary.Int64(), 0).UTC()
	// Outside the RFC3339 domain no persisted run can precede the cutoff.
	if cutoff.Year() < 0 {
		return []Run{}, nil
	}
	if now.Nanosecond() > 0 {
		cutoff = cutoff.Add(time.Second)
	}
	if cutoff.Year() > 9999 {
		return c.readRunsSQL(ctx, "SELECT "+runColumns+" FROM (SELECT "+runColumns+",row_number() OVER (PARTITION BY script ORDER BY started DESC,id DESC)-1 AS rank FROM runs) WHERE rank>=? AND status<>? AND status<>? ORDER BY script,started DESC,id DESC", count, StatusRunning, StatusQueued)
	}
	return c.readRunsSQL(ctx, "SELECT "+runColumns+" FROM (SELECT "+runColumns+",row_number() OVER (PARTITION BY script ORDER BY started DESC,id DESC)-1 AS rank FROM runs) WHERE rank>=? AND status<>? AND status<>? AND started<? ORDER BY script,started DESC,id DESC", count, StatusRunning, StatusQueued, cutoff.Format(time.RFC3339))
}

// RunCount counts all statuses for a script without reading its run records.
func (s *Store) RunCount(ctx context.Context, script string) (int, error) {
	var n int
	err := s.transaction(ctx, false, func(c *catalog) error {
		return c.tx.QueryRowContext(ctx, "SELECT count(*) FROM runs WHERE script=?", script).Scan(&n)
	})
	return n, err
}
