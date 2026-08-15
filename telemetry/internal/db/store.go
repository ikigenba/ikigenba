package db

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"telemetry/internal/record"
	"time"
)

// Store persists and queries telemetry records.
type Store struct {
	db *sql.DB
}

// NewStore constructs a Store over an initialized telemetry database.
func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// Query describes indexed and outcome filters for Search.
type Query struct {
	Since, Until  string
	CorrelationID string
	Service, Kind string
	OwnerEmail    string
	ClientID      string
	SHA256        string
	Status, Error string
	OpContains    string
	Limit         int
	Cursor        Cursor
}

// Cursor is the exclusive keyset position of the previous page's last row.
type Cursor struct {
	Time string `json:"time"`
	ID   string `json:"id"`
}

// String returns an opaque wire representation of the cursor.
func (c Cursor) String() string {
	if c.Time == "" && c.ID == "" {
		return ""
	}
	body, err := json.Marshal(c)
	if err != nil {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString(body)
}

// ParseCursor parses a cursor previously returned by Cursor.String.
func ParseCursor(encoded string) (Cursor, error) {
	if encoded == "" {
		return Cursor{}, nil
	}
	body, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return Cursor{}, fmt.Errorf("decode cursor: %w", err)
	}
	var cursor Cursor
	if err := json.Unmarshal(body, &cursor); err != nil {
		return Cursor{}, fmt.Errorf("parse cursor: %w", err)
	}
	if cursor.Time == "" || cursor.ID == "" {
		return Cursor{}, errors.New("cursor requires time and id")
	}
	normalized, err := record.NormalizeTime(cursor.Time)
	if err != nil || normalized != cursor.Time {
		return Cursor{}, errors.New("cursor time is not normalized")
	}
	return cursor, nil
}

// InsertRecords inserts a batch atomically and ignores records whose id exists.
func (s *Store) InsertRecords(ctx context.Context, records []record.Record, receivedAt time.Time) (stored int, err error) {
	received := receivedAt.UTC().Format(record.TimeLayout)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()

	const statement = `INSERT OR IGNORE INTO records (
		id, time, correlation_id, service, kind, owner_email, client_id, op,
		status, error, duration_ms, bytes, sha256, params, detail, received_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
	for _, item := range records {
		if err = item.Validate(); err != nil {
			return 0, fmt.Errorf("validate record %q: %w", item.ID, err)
		}
		item.Time, err = record.NormalizeTime(item.Time)
		if err != nil {
			return 0, fmt.Errorf("normalize record %q: %w", item.ID, err)
		}
		result, execErr := tx.ExecContext(ctx, statement,
			item.ID, item.Time, item.CorrelationID, item.Service, item.Kind,
			item.Actor.OwnerEmail, item.Actor.ClientID, item.Op, item.Outcome.Status,
			item.Outcome.Error, item.Outcome.DurationMS, item.Outcome.Bytes,
			item.Outcome.SHA256, nullableJSON(item.Params), nullableJSON(item.Detail), received,
		)
		if execErr != nil {
			err = execErr
			return 0, err
		}
		rows, rowsErr := result.RowsAffected()
		if rowsErr != nil {
			err = rowsErr
			return 0, err
		}
		stored += int(rows)
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return stored, nil
}

// NoteDropped records a positive reporter drop count.
func (s *Store) NoteDropped(ctx context.Context, service string, n int64, at time.Time) error {
	if n <= 0 {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO ingest_drops(time, service, dropped) VALUES (?, ?, ?)`, at.UTC().Format(record.TimeLayout), service, n)
	return err
}

// DroppedTotal returns the sum of all reported dropped records.
func (s *Store) DroppedTotal(ctx context.Context) (int64, error) {
	var total int64
	err := s.db.QueryRowContext(ctx, `SELECT COALESCE(SUM(dropped), 0) FROM ingest_drops`).Scan(&total)
	return total, err
}

// Get retrieves one record by id.
func (s *Store) Get(ctx context.Context, id string) (record.Record, bool, error) {
	item, err := scanRecord(s.db.QueryRowContext(ctx, selectRecords+` WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return record.Record{}, false, nil
	}
	return item, err == nil, err
}

// Chain retrieves a complete causal chain in chronological order.
func (s *Store) Chain(ctx context.Context, correlationID string) ([]record.Record, error) {
	rows, err := s.db.QueryContext(ctx, selectRecords+` WHERE correlation_id = ? ORDER BY time ASC, id ASC`, correlationID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanRecords(rows)
}

// Search finds matching records in descending keyset order.
func (s *Store) Search(ctx context.Context, query Query) ([]record.Record, Cursor, error) {
	statement, args, err := searchStatement(query)
	if err != nil {
		return nil, Cursor{}, err
	}
	rows, err := s.db.QueryContext(ctx, statement, args...)
	if err != nil {
		return nil, Cursor{}, err
	}
	defer rows.Close()
	items, err := scanRecords(rows)
	if err != nil {
		return nil, Cursor{}, err
	}
	if len(items) == 0 {
		return items, Cursor{}, nil
	}
	last := items[len(items)-1]
	return items, Cursor{Time: last.Time, ID: last.ID}, nil
}

// Oldest returns the earliest stored record timestamp.
func (s *Store) Oldest(ctx context.Context) (timestamp string, found bool, err error) {
	var oldest sql.NullString
	if err := s.db.QueryRowContext(ctx, `SELECT MIN(time) FROM records`).Scan(&oldest); err != nil {
		return "", false, err
	}
	return oldest.String, oldest.Valid, nil
}

// PruneBefore removes records and drop notices strictly older than cutoff.
func (s *Store) PruneBefore(ctx context.Context, cutoff time.Time) (removed int, err error) {
	normalized := cutoff.UTC().Format(record.TimeLayout)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	result, err := tx.ExecContext(ctx, `DELETE FROM records WHERE time < ?`, normalized)
	if err != nil {
		return 0, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return 0, err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM ingest_drops WHERE time < ?`, normalized); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return int(rows), nil
}

const selectRecords = `SELECT id, time, correlation_id, service, kind,
	owner_email, client_id, op, status, error, duration_ms, bytes, sha256, params, detail
	FROM records`

type scanner interface {
	Scan(dest ...any) error
}

func scanRecord(row scanner) (record.Record, error) {
	var item record.Record
	var params, detail []byte
	err := row.Scan(
		&item.ID, &item.Time, &item.CorrelationID, &item.Service, &item.Kind,
		&item.Actor.OwnerEmail, &item.Actor.ClientID, &item.Op, &item.Outcome.Status,
		&item.Outcome.Error, &item.Outcome.DurationMS, &item.Outcome.Bytes,
		&item.Outcome.SHA256, &params, &detail,
	)
	item.Params = params
	item.Detail = detail
	return item, err
}

func scanRecords(rows *sql.Rows) ([]record.Record, error) {
	var records []record.Record
	for rows.Next() {
		item, err := scanRecord(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, item)
	}
	return records, rows.Err()
}

func searchStatement(query Query) (queryText string, arguments []any, err error) {
	conditions := make([]string, 0, 10)
	args := make([]any, 0, 12)
	add := func(condition string, value any) {
		conditions = append(conditions, condition)
		args = append(args, value)
	}
	if query.Since != "" {
		normalized, err := record.NormalizeTime(query.Since)
		if err != nil {
			return "", nil, fmt.Errorf("normalize since: %w", err)
		}
		add("time >= ?", normalized)
	}
	if query.Until != "" {
		normalized, err := record.NormalizeTime(query.Until)
		if err != nil {
			return "", nil, fmt.Errorf("normalize until: %w", err)
		}
		add("time <= ?", normalized)
	}
	for _, filter := range []struct {
		value, predicate string
	}{
		{query.CorrelationID, "correlation_id = ?"},
		{query.Service, "service = ?"},
		{query.Kind, "kind = ?"},
		{query.OwnerEmail, "owner_email = ?"},
		{query.ClientID, "client_id = ?"},
		{query.SHA256, "sha256 = ?"},
		{query.Status, "status = ?"},
		{query.Error, "error = ?"},
	} {
		if filter.value != "" {
			add(filter.predicate, filter.value)
		}
	}
	if query.OpContains != "" {
		add("instr(lower(op), lower(?)) > 0", query.OpContains)
	}
	if query.Cursor.Time != "" || query.Cursor.ID != "" {
		if query.Cursor.Time == "" || query.Cursor.ID == "" {
			return "", nil, errors.New("cursor requires time and id")
		}
		normalized, err := record.NormalizeTime(query.Cursor.Time)
		if err != nil {
			return "", nil, fmt.Errorf("normalize cursor: %w", err)
		}
		conditions = append(conditions, "(time < ? OR (time = ? AND id < ?))")
		args = append(args, normalized, normalized, query.Cursor.ID)
	}
	statement := selectRecords
	if len(conditions) > 0 {
		statement += " WHERE " + strings.Join(conditions, " AND ")
	}
	statement += " ORDER BY time DESC, id DESC"
	if query.Limit > 0 {
		statement += " LIMIT ?"
		args = append(args, query.Limit)
	}
	return statement, args, nil
}

func nullableJSON(raw json.RawMessage) any {
	if len(raw) == 0 {
		return nil
	}
	return string(raw)
}
