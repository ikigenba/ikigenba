// Package calls owns the durable, suite-wide record of inference calls.
package calls

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"prompts/internal/ids"
)

type Class string

const (
	ClassSession    Class = "session"
	ClassCompletion Class = "completion"
	ClassEmbedding  Class = "embedding"
)

var (
	originPattern = regexp.MustCompile(`^(user|trigger|service):[a-z0-9._@-]+$`)
	namePattern   = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*\.[a-z0-9][a-z0-9._-]*$`)
)

// ValidOrigin reports whether origin follows the calls origin grammar. Origin
// matching is case-insensitive so email local parts retain their spelling.
func ValidOrigin(origin string) bool {
	return originPattern.MatchString(strings.ToLower(origin))
}

// ValidateOrigin returns an error when origin is outside the calls grammar.
func ValidateOrigin(origin string) error {
	if !ValidOrigin(origin) {
		return fmt.Errorf("calls: invalid origin %q", origin)
	}
	return nil
}

// ValidName reports whether name is a service.stage workload label.
func ValidName(name string) bool { return namePattern.MatchString(name) }

// ValidateName returns an error when name is not a service.stage workload label.
func ValidateName(name string) error {
	if !ValidName(name) {
		return fmt.Errorf("calls: invalid name %q", name)
	}
	return nil
}

type Row struct {
	ID           string
	Class        Class
	Origin       string
	Name         string
	GroupID      string
	Attempt      int
	OwnerEmail   string
	Provider     string
	Model        string
	InputTokens  int64
	OutputTokens int64
	TotalTokens  int64
	UsageJSON    string
	CostUSD      float64
	Error        string
	RequestBody  *string
	ResponseBody *string
	StartedAt    time.Time
	EndedAt      time.Time
}

type Filter struct {
	Class      Class
	Origin     string
	Name       string
	GroupID    string
	ErrorsOnly bool
	Since      time.Time
	Until      time.Time
	Limit      int
	Offset     int
}

type GroupBy string

const (
	GroupByName   GroupBy = "name"
	GroupByOrigin GroupBy = "origin"
	GroupByModel  GroupBy = "model"
	GroupByDay    GroupBy = "day"
)

type Bucket struct {
	Key          string
	Calls        int64
	InputTokens  int64
	OutputTokens int64
	TotalTokens  int64
	CostUSD      float64
	Errors       int64
}

type Store struct {
	db  *sql.DB
	now func() time.Time
}

func NewStore(db *sql.DB) *Store { return &Store{db: db, now: time.Now} }

func (s *Store) Insert(ctx context.Context, row Row) error {
	return s.insert(ctx, s.db, row)
}

func (s *Store) InsertTx(ctx context.Context, tx *sql.Tx, row Row) error {
	if tx == nil {
		return errors.New("calls: nil transaction")
	}
	return s.insert(ctx, tx, row)
}

type execer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func (s *Store) insert(ctx context.Context, dst execer, row Row) error {
	if dst == nil {
		return errors.New("calls: nil database")
	}
	if row.Class != ClassSession && row.Class != ClassCompletion && row.Class != ClassEmbedding {
		return fmt.Errorf("calls: invalid class %q", row.Class)
	}
	if err := ValidateOrigin(row.Origin); err != nil {
		return err
	}
	if row.Class != ClassSession {
		if err := ValidateName(row.Name); err != nil {
			return err
		}
	} else if row.Name == "" {
		return errors.New("calls: session name is empty")
	}
	if row.ID == "" {
		row.ID = ids.NewULID()
	}
	if row.Attempt == 0 {
		row.Attempt = 1
	}
	now := s.now().UTC()
	if row.StartedAt.IsZero() {
		row.StartedAt = now
	}
	if row.EndedAt.IsZero() {
		row.EndedAt = now
	}
	_, err := dst.ExecContext(ctx, `
		INSERT INTO calls (
			id, class, origin, name, group_id, attempt, owner_email, provider, model,
			input_tokens, output_tokens, total_tokens, usage_json, cost_usd, error,
			request_body, response_body, started_at, ended_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		row.ID, row.Class, row.Origin, row.Name, row.GroupID, row.Attempt,
		row.OwnerEmail, row.Provider, row.Model, row.InputTokens, row.OutputTokens,
		row.TotalTokens, row.UsageJSON, row.CostUSD, row.Error, row.RequestBody,
		row.ResponseBody, formatTime(row.StartedAt), formatTime(row.EndedAt))
	if err != nil {
		return fmt.Errorf("calls: insert: %w", err)
	}
	return nil
}

func (s *Store) PruneBodies(ctx context.Context, olderThan time.Time) (int64, error) {
	result, err := s.db.ExecContext(ctx, `
		UPDATE calls SET request_body = NULL, response_body = NULL
		WHERE ended_at < ? AND (request_body IS NOT NULL OR response_body IS NOT NULL)`,
		formatTime(olderThan))
	if err != nil {
		return 0, fmt.Errorf("calls: prune bodies: %w", err)
	}
	n, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("calls: prune bodies count: %w", err)
	}
	return n, nil
}

func (s *Store) Get(ctx context.Context, id string) (Row, error) {
	row, err := scanRow(s.db.QueryRowContext(ctx, selectColumns+` WHERE id = ?`, id))
	if err != nil {
		return Row{}, fmt.Errorf("calls: get %q: %w", id, err)
	}
	return row, nil
}

func (s *Store) List(ctx context.Context, f Filter) ([]Row, error) {
	where, args := filterSQL(f)
	limit := f.Limit
	if limit <= 0 {
		limit = 50
	} else if limit > 500 {
		limit = 500
	}
	offset := f.Offset
	if offset < 0 {
		offset = 0
	}
	args = append(args, limit, offset)
	rows, err := s.db.QueryContext(ctx, selectColumns+where+` ORDER BY started_at DESC, id DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, fmt.Errorf("calls: list: %w", err)
	}
	defer rows.Close()
	result := make([]Row, 0)
	for rows.Next() {
		row, err := scanRow(rows)
		if err != nil {
			return nil, fmt.Errorf("calls: list scan: %w", err)
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("calls: list rows: %w", err)
	}
	return result, nil
}

// ListByGroup returns a run's calls in replay order, including stored bodies.
func (s *Store) ListByGroup(ctx context.Context, groupID string) ([]Row, error) {
	rows, err := s.db.QueryContext(ctx, selectColumns+` WHERE group_id = ? ORDER BY started_at ASC, id ASC`, groupID)
	if err != nil {
		return nil, fmt.Errorf("calls: list by group: %w", err)
	}
	defer rows.Close()
	result := make([]Row, 0)
	for rows.Next() {
		row, err := scanRow(rows)
		if err != nil {
			return nil, fmt.Errorf("calls: list by group scan: %w", err)
		}
		result = append(result, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("calls: list by group rows: %w", err)
	}
	return result, nil
}

func (s *Store) Aggregate(ctx context.Context, group GroupBy, f Filter) ([]Bucket, error) {
	var expression string
	switch group {
	case GroupByName:
		expression = "name"
	case GroupByOrigin:
		expression = "origin"
	case GroupByModel:
		expression = "model"
	case GroupByDay:
		expression = "substr(started_at, 1, 10)"
	default:
		return nil, fmt.Errorf("calls: invalid group %q", group)
	}
	where, args := filterSQL(f)
	query := `SELECT ` + expression + `, COUNT(*), COALESCE(SUM(input_tokens), 0),
		COALESCE(SUM(output_tokens), 0), COALESCE(SUM(total_tokens), 0),
		COALESCE(SUM(cost_usd), 0), SUM(CASE WHEN error <> '' THEN 1 ELSE 0 END)
		FROM calls` + where + ` GROUP BY ` + expression + ` ORDER BY ` + expression
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("calls: aggregate: %w", err)
	}
	defer rows.Close()
	var result []Bucket
	for rows.Next() {
		var bucket Bucket
		if err := rows.Scan(&bucket.Key, &bucket.Calls, &bucket.InputTokens,
			&bucket.OutputTokens, &bucket.TotalTokens, &bucket.CostUSD, &bucket.Errors); err != nil {
			return nil, fmt.Errorf("calls: aggregate scan: %w", err)
		}
		result = append(result, bucket)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("calls: aggregate rows: %w", err)
	}
	return result, nil
}

const selectColumns = `SELECT id, class, origin, name, group_id, attempt, owner_email,
	provider, model, input_tokens, output_tokens, total_tokens, usage_json, cost_usd,
	error, request_body, response_body, started_at, ended_at FROM calls`

type scanner interface{ Scan(...any) error }

func scanRow(src scanner) (Row, error) {
	var row Row
	var request, response sql.NullString
	var started, ended string
	err := src.Scan(&row.ID, &row.Class, &row.Origin, &row.Name, &row.GroupID,
		&row.Attempt, &row.OwnerEmail, &row.Provider, &row.Model, &row.InputTokens,
		&row.OutputTokens, &row.TotalTokens, &row.UsageJSON, &row.CostUSD, &row.Error,
		&request, &response, &started, &ended)
	if err != nil {
		return Row{}, err
	}
	if request.Valid {
		row.RequestBody = &request.String
	}
	if response.Valid {
		row.ResponseBody = &response.String
	}
	row.StartedAt, err = time.Parse(time.RFC3339Nano, started)
	if err != nil {
		return Row{}, fmt.Errorf("parse started_at: %w", err)
	}
	row.EndedAt, err = time.Parse(time.RFC3339Nano, ended)
	if err != nil {
		return Row{}, fmt.Errorf("parse ended_at: %w", err)
	}
	return row, nil
}

func filterSQL(f Filter) (string, []any) {
	var clauses []string
	var args []any
	add := func(clause string, value any) {
		clauses = append(clauses, clause)
		args = append(args, value)
	}
	if f.Class != "" {
		add("class = ?", f.Class)
	}
	if f.Origin != "" {
		add("origin = ?", f.Origin)
	}
	if f.Name != "" {
		add("name = ?", f.Name)
	}
	if f.GroupID != "" {
		add("group_id = ?", f.GroupID)
	}
	if f.ErrorsOnly {
		clauses = append(clauses, "error <> ''")
	}
	if !f.Since.IsZero() {
		add("started_at >= ?", formatTime(f.Since))
	}
	if !f.Until.IsZero() {
		add("started_at <= ?", formatTime(f.Until))
	}
	if len(clauses) == 0 {
		return "", args
	}
	return " WHERE " + strings.Join(clauses, " AND "), args
}

// A fixed-width fractional part keeps SQLite TEXT ordering chronological while
// remaining a valid RFC3339 UTC timestamp.
func formatTime(value time.Time) string {
	return value.UTC().Format("2006-01-02T15:04:05.000000000Z")
}
