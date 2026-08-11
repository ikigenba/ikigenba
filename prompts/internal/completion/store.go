// Package completion owns the durable completion work queue.
package completion

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"prompts/internal/ids"
)

const (
	StatusQueued  = "queued"
	StatusRunning = "running"
	StatusDone    = "done"
	StatusFailed  = "failed"

	RetentionTTL = 7 * 24 * time.Hour
)

var ErrNotFound = errors.New("completion: not found")

type Item struct {
	ID            string
	Consumer      string
	Origin        string
	Key           string
	Context       string
	Name          string
	GroupID       string
	CorrelationID string
	Attempt       int
	Request       string
	Status        string
	Result        string
	Error         string
	UsageJSON     string
	CostUSD       float64
	CreatedAt     time.Time
	StartedAt     time.Time
	FinishedAt    time.Time
}

type Store struct {
	db  *sql.DB
	now func() time.Time
}

func NewStore(db *sql.DB, now func() time.Time) *Store {
	if now == nil {
		now = time.Now
	}
	return &Store{db: db, now: now}
}

// Ensure inserts an item or returns the existing item for its consumer/key.
func (s *Store) Ensure(ctx context.Context, item Item) (Item, bool, error) {
	if item.ID == "" {
		item.ID = ids.NewULID()
	}
	if item.Attempt == 0 {
		item.Attempt = 1
	}
	item.Status = StatusQueued
	item.CreatedAt = s.now().UTC()
	result, err := s.db.ExecContext(ctx, `INSERT INTO completions
		(id,consumer,origin,key,context,name,group_id,correlation_id,attempt,request,status,created_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(consumer,key) DO NOTHING`,
		item.ID, item.Consumer, item.Origin, item.Key, item.Context, item.Name, item.GroupID,
		item.CorrelationID, item.Attempt, item.Request, item.Status, formatTime(item.CreatedAt))
	if err != nil {
		return Item{}, false, fmt.Errorf("completion: ensure: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return Item{}, false, fmt.Errorf("completion: ensure count: %w", err)
	}
	if n == 1 {
		return item, true, nil
	}
	existing, err := s.getWhere(ctx, "consumer = ? AND key = ?", item.Consumer, item.Key)
	return existing, false, err
}

func (s *Store) Get(ctx context.Context, id string) (Item, error) {
	return s.getWhere(ctx, "id = ?", id)
}

func (s *Store) getWhere(ctx context.Context, where string, args ...any) (Item, error) {
	item, err := scanItem(s.db.QueryRowContext(ctx, selectItem+" WHERE "+where, args...))
	if errors.Is(err, sql.ErrNoRows) {
		return Item{}, ErrNotFound
	}
	if err != nil {
		return Item{}, fmt.Errorf("completion: get: %w", err)
	}
	return item, nil
}

func (s *Store) Inbox(ctx context.Context, consumer string) ([]Item, error) {
	rows, err := s.db.QueryContext(ctx, selectItem+` WHERE consumer = ? AND status IN ('done','failed')
		ORDER BY created_at ASC, id ASC LIMIT 100`, consumer)
	if err != nil {
		return nil, fmt.Errorf("completion: inbox: %w", err)
	}
	defer rows.Close()
	var items []Item
	for rows.Next() {
		item, err := scanItem(rows)
		if err != nil {
			return nil, fmt.Errorf("completion: inbox scan: %w", err)
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// Claim marks and returns the oldest queued item atomically.
func (s *Store) Claim(ctx context.Context) (Item, error) {
	now := formatTime(s.now().UTC())
	item, err := scanItem(s.db.QueryRowContext(ctx, `UPDATE completions
		SET status = 'running', started_at = ?
		WHERE id = (SELECT id FROM completions WHERE status = 'queued' ORDER BY created_at, id LIMIT 1)
		RETURNING `+itemColumns, now))
	if errors.Is(err, sql.ErrNoRows) {
		return Item{}, ErrNotFound
	}
	if err != nil {
		return Item{}, fmt.Errorf("completion: claim: %w", err)
	}
	return item, nil
}

func (s *Store) Complete(ctx context.Context, id, result, usageJSON string, costUSD float64) error {
	return s.finish(ctx, id, StatusDone, result, "", usageJSON, costUSD)
}

func (s *Store) Fail(ctx context.Context, id, reason, usageJSON string, costUSD float64) error {
	return s.finish(ctx, id, StatusFailed, "", reason, usageJSON, costUSD)
}

func (s *Store) finish(ctx context.Context, id, status, result, reason, usageJSON string, costUSD float64) error {
	r, err := s.db.ExecContext(ctx, `UPDATE completions SET status=?,result=?,error=?,usage_json=?,cost_usd=?,finished_at=?
		WHERE id=? AND status='running'`, status, result, reason, usageJSON, costUSD, formatTime(s.now().UTC()), id)
	if err != nil {
		return fmt.Errorf("completion: finish: %w", err)
	}
	n, _ := r.RowsAffected()
	if n != 1 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) Ack(ctx context.Context, id string) error {
	r, err := s.db.ExecContext(ctx, "DELETE FROM completions WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("completion: ack: %w", err)
	}
	n, _ := r.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) RequeueRunning(ctx context.Context) (int64, error) {
	r, err := s.db.ExecContext(ctx, `UPDATE completions SET status='queued',started_at='' WHERE status='running'`)
	if err != nil {
		return 0, fmt.Errorf("completion: requeue: %w", err)
	}
	return r.RowsAffected()
}

func (s *Store) Sweep(ctx context.Context) (int64, error) {
	cutoff := formatTime(s.now().UTC().Add(-RetentionTTL))
	r, err := s.db.ExecContext(ctx, `DELETE FROM completions
		WHERE status IN ('done','failed') AND finished_at <> '' AND finished_at < ?`, cutoff)
	if err != nil {
		return 0, fmt.Errorf("completion: sweep: %w", err)
	}
	return r.RowsAffected()
}

const itemColumns = `id,consumer,origin,key,context,name,group_id,correlation_id,attempt,request,status,
	result,error,usage_json,cost_usd,created_at,started_at,finished_at`
const selectItem = `SELECT ` + itemColumns + ` FROM completions`

type scanner interface{ Scan(...any) error }

func scanItem(row scanner) (Item, error) {
	var item Item
	var created, started, finished string
	err := row.Scan(&item.ID, &item.Consumer, &item.Origin, &item.Key, &item.Context, &item.Name,
		&item.GroupID, &item.CorrelationID, &item.Attempt, &item.Request, &item.Status,
		&item.Result, &item.Error, &item.UsageJSON, &item.CostUSD, &created, &started, &finished)
	if err != nil {
		return Item{}, err
	}
	item.CreatedAt = parseTime(created)
	item.StartedAt = parseTime(started)
	item.FinishedAt = parseTime(finished)
	return item, nil
}

func formatTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }
func parseTime(v string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, v)
	return t
}
