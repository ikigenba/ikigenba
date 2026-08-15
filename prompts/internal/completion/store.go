// Package completion owns the durable completion work queue.
package completion

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"prompts/internal/ids"
	"time"
)

const (
	StatusQueued  = "queued"
	StatusRunning = "running"
	StatusDone    = "done"
	StatusFailed  = "failed"

	RetentionTTL = 7 * 24 * time.Hour
	LeaseTTL     = 5 * time.Minute
)

var ErrNotFound = errors.New("completion: not found")

type Item struct {
	ID             string
	Consumer       string
	Origin         string
	Key            string
	Context        string
	Name           string
	GroupID        string
	CorrelationID  string
	Attempt        int
	Request        string
	Status         string
	Owner          string
	LeaseExpiresAt time.Time
	Reclaims       int
	Result         string
	Error          string
	ErrorCode      string
	UsageJSON      string
	CostUSD        float64
	CreatedAt      time.Time
	StartedAt      time.Time
	FinishedAt     time.Time
}

type Store struct {
	db     *sql.DB
	owner  string
	now    func() time.Time
	logger *slog.Logger
}

type Snapshot struct {
	Queued                 int64
	Running                int64
	Terminal               int64
	OldestQueuedAgeSeconds int64
	ReclaimedItems         int64
}

func NewStore(db *sql.DB, owner string, now func() time.Time, loggers ...*slog.Logger) *Store {
	if now == nil {
		now = time.Now
	}
	logger := slog.Default()
	if len(loggers) > 0 && loggers[0] != nil {
		logger = loggers[0]
	}
	return &Store{db: db, owner: owner, now: now, logger: logger}
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

func (s *Store) Get(ctx context.Context, id, consumer string) (Item, error) {
	return s.getWhere(ctx, "id = ? AND consumer = ?", id, consumer)
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

// Claim marks and returns the oldest eligible item atomically.
func (s *Store) Claim(ctx context.Context) (Item, error) {
	for {
		nowTime := s.now().UTC()
		now := formatTime(nowTime)
		item, err := scanItem(s.db.QueryRowContext(ctx, `UPDATE completions
			SET status = CASE WHEN status='running' AND reclaims+1 >= 2 THEN 'failed' ELSE 'running' END,
				owner = CASE WHEN status='running' AND reclaims+1 >= 2 THEN '' ELSE ? END,
				lease_expires_at = CASE WHEN status='running' AND reclaims+1 >= 2 THEN '' ELSE ? END,
				reclaims = reclaims + CASE WHEN status='running' THEN 1 ELSE 0 END,
				started_at = CASE WHEN status='queued' THEN ? ELSE started_at END,
				error = CASE WHEN status='running' AND reclaims+1 >= 2 THEN 'completion abandoned after 2 expired leases' ELSE error END,
				error_code = CASE WHEN status='running' AND reclaims+1 >= 2 THEN 'abandoned' ELSE error_code END,
				finished_at = CASE WHEN status='running' AND reclaims+1 >= 2 THEN ? ELSE finished_at END
			WHERE id = (SELECT candidate.id FROM completions AS candidate
				WHERE candidate.status='queued' OR (candidate.status='running' AND candidate.lease_expires_at < ?)
				ORDER BY CASE WHEN NOT EXISTS (
					SELECT 1 FROM completions AS active WHERE active.consumer=candidate.consumer AND active.status='running'
				) THEN 0 ELSE 1 END, candidate.created_at, candidate.id LIMIT 1)
			RETURNING `+itemColumns, s.owner, formatTime(nowTime.Add(LeaseTTL)), now, now, now))
		if errors.Is(err, sql.ErrNoRows) {
			return Item{}, ErrNotFound
		}
		if err != nil {
			return Item{}, fmt.Errorf("completion: claim: %w", err)
		}
		if item.Status == StatusFailed {
			continue
		}
		return item, nil
	}
}

// Renew extends the current owner's running lease. False means ownership was lost.
func (s *Store) Renew(ctx context.Context, id string) (bool, error) {
	now := s.now().UTC()
	r, err := s.db.ExecContext(ctx, `UPDATE completions SET lease_expires_at=?
		WHERE id=? AND owner=? AND status='running' AND lease_expires_at >= ?`,
		formatTime(now.Add(LeaseTTL)), id, s.owner, formatTime(now))
	if err != nil {
		return false, fmt.Errorf("completion: renew: %w", err)
	}
	n, err := r.RowsAffected()
	return n == 1, err
}

// Release freely returns a currently-owned running item to the queue.
func (s *Store) Release(ctx context.Context, id string) (bool, error) {
	r, err := s.db.ExecContext(ctx, `UPDATE completions SET status='queued',owner='',lease_expires_at='' WHERE id=? AND owner=? AND status='running'`, id, s.owner)
	if err != nil {
		return false, fmt.Errorf("completion: release: %w", err)
	}
	n, err := r.RowsAffected()
	return n == 1, err
}

func (s *Store) Complete(ctx context.Context, id, result, usageJSON string, costUSD float64) error {
	return s.finish(ctx, id, StatusDone, result, "", usageJSON, costUSD, false)
}

func (s *Store) Fail(ctx context.Context, id, reason, usageJSON string, costUSD float64) error {
	return s.finish(ctx, id, StatusFailed, "", reason, usageJSON, costUSD, true)
}

func (s *Store) finish(ctx context.Context, id, status, result, reason, usageJSON string, costUSD float64, ownerGuard bool) error {
	guard := "status <> 'done'"
	args := []any{status, result, reason, usageJSON, costUSD, formatTime(s.now().UTC()), id}
	if ownerGuard {
		guard = "status='running' AND owner=?"
		args = append(args, s.owner)
	}
	r, err := s.db.ExecContext(ctx, `UPDATE completions SET status=?,result=?,error=?,usage_json=?,cost_usd=?,finished_at=?
		WHERE id=? AND `+guard, args...)
	if err != nil {
		return fmt.Errorf("completion: finish: %w", err)
	}
	n, _ := r.RowsAffected()
	if n == 0 {
		s.logger.Info("completion terminal write lost", "id", id, "status", status)
	}
	return nil
}

func (s *Store) Ack(ctx context.Context, id, consumer string) error {
	r, err := s.db.ExecContext(ctx, "DELETE FROM completions WHERE id = ? AND consumer = ?", id, consumer)
	if err != nil {
		return fmt.Errorf("completion: ack: %w", err)
	}
	n, _ := r.RowsAffected()
	if n == 0 {
		return ErrNotFound
	}
	return nil
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

// Snapshot derives the queue's current health directly from its durable rows.
func (s *Store) Snapshot(ctx context.Context) (Snapshot, error) {
	var snapshot Snapshot
	var oldestQueued string
	err := s.db.QueryRowContext(ctx, `SELECT
		(SELECT COUNT(*) FROM completions WHERE status='queued'),
		(SELECT COUNT(*) FROM completions WHERE status='running'),
		(SELECT COUNT(*) FROM completions WHERE status IN ('done','failed')),
		COALESCE((SELECT MIN(created_at) FROM completions WHERE status='queued'), ''),
		(SELECT COUNT(*) FROM completions WHERE reclaims <> 0)`).Scan(
		&snapshot.Queued, &snapshot.Running, &snapshot.Terminal, &oldestQueued, &snapshot.ReclaimedItems)
	if err != nil {
		return Snapshot{}, fmt.Errorf("completion: snapshot: %w", err)
	}
	if created := parseTime(oldestQueued); !created.IsZero() {
		snapshot.OldestQueuedAgeSeconds = int64(s.now().UTC().Sub(created) / time.Second)
		if snapshot.OldestQueuedAgeSeconds < 0 {
			snapshot.OldestQueuedAgeSeconds = 0
		}
	}
	return snapshot, nil
}

const itemColumns = `id,consumer,origin,key,context,name,group_id,correlation_id,attempt,request,status,
	owner,lease_expires_at,reclaims,result,error,error_code,usage_json,cost_usd,created_at,started_at,finished_at`
const selectItem = `SELECT ` + itemColumns + ` FROM completions`

type scanner interface{ Scan(...any) error }

func scanItem(row scanner) (Item, error) {
	var item Item
	var created, started, finished, leaseExpires string
	err := row.Scan(&item.ID, &item.Consumer, &item.Origin, &item.Key, &item.Context, &item.Name,
		&item.GroupID, &item.CorrelationID, &item.Attempt, &item.Request, &item.Status, &item.Owner,
		&leaseExpires, &item.Reclaims, &item.Result, &item.Error, &item.ErrorCode, &item.UsageJSON,
		&item.CostUSD, &created, &started, &finished)
	if err != nil {
		return Item{}, err
	}
	item.CreatedAt = parseTime(created)
	item.StartedAt = parseTime(started)
	item.LeaseExpiresAt = parseTime(leaseExpires)
	item.FinishedAt = parseTime(finished)
	return item, nil
}

func formatTime(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }
func parseTime(v string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, v)
	return t
}
