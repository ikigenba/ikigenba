// Package repos owns repository custody and persistence.
package repos

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// Repository is the metadata SQLite owns for a git repository.
type Repository struct {
	Kind          string
	Name          string
	OwnerID       string
	OwnerEmail    string
	DefaultBranch string
	ArchivedAt    *time.Time
	ArchivedPath  *string
	CreatedAt     time.Time
}

// Status is one named check verdict for a commit held by git.
type Status struct {
	Kind      string
	Name      string
	SHA       string
	CheckName string
	State     string
	Detail    *string
	Actor     string
	UpdatedAt time.Time
}

// RunToken is the stored digest and repository scope for a live run token.
type RunToken struct {
	TokenSHA256 string
	Kind        string
	Name        string
	ExpiresAt   time.Time
	CreatedAt   time.Time
}

// Store is the domain's persistence handle over the shared SQLite connection.
type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

func (s *Store) BeginTx(ctx context.Context) (*sql.Tx, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("repos store: database is not configured")
	}
	return s.db.BeginTx(ctx, nil)
}

func (s *Store) InsertRepository(ctx context.Context, tx *sql.Tx, repository Repository) error {
	if err := requireTx(tx); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO repositories
			(kind, name, owner_id, owner_email, default_branch, archived_at, archived_path, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		repository.Kind, repository.Name, repository.OwnerID, repository.OwnerEmail,
		repository.DefaultBranch, nullableTime(repository.ArchivedAt), repository.ArchivedPath,
		formatTime(repository.CreatedAt))
	return err
}

func (s *Store) GetRepository(ctx context.Context, kind, name string) (Repository, error) {
	return scanRepository(s.db.QueryRowContext(ctx, `
		SELECT kind, name, owner_id, owner_email, default_branch, archived_at, archived_path, created_at
		FROM repositories WHERE kind = ? AND name = ?`, kind, name))
}

func (s *Store) ListRepositories(ctx context.Context, ownerID string, kind *string) ([]Repository, error) {
	query := `
		SELECT kind, name, owner_id, owner_email, default_branch, archived_at, archived_path, created_at
		FROM repositories WHERE owner_id = ? AND archived_at IS NULL`
	args := []any{ownerID}
	if kind != nil {
		query += ` AND kind = ?`
		args = append(args, *kind)
	}
	query += ` ORDER BY kind, name`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Repository, 0)
	for rows.Next() {
		repository, err := scanRepository(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, repository)
	}
	return result, rows.Err()
}

func (s *Store) RenameRepository(ctx context.Context, tx *sql.Tx, kind, name, newName string) error {
	if err := requireTx(tx); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `UPDATE repositories SET name = ? WHERE kind = ? AND name = ?`, newName, kind, name)
	return err
}

func (s *Store) ArchiveRepository(ctx context.Context, tx *sql.Tx, kind, name string, archivedAt time.Time, archivedPath string) error {
	if err := requireTx(tx); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `
		UPDATE repositories SET archived_at = ?, archived_path = ? WHERE kind = ? AND name = ?`,
		formatTime(archivedAt), archivedPath, kind, name)
	return err
}

func (s *Store) SetStatus(ctx context.Context, tx *sql.Tx, status Status) error {
	if err := requireTx(tx); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO statuses (kind, name, sha, check_name, state, detail, actor, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (kind, name, sha, check_name) DO UPDATE SET
			state = excluded.state,
			detail = excluded.detail,
			actor = excluded.actor,
			updated_at = excluded.updated_at`,
		status.Kind, status.Name, status.SHA, status.CheckName, status.State,
		status.Detail, status.Actor, formatTime(status.UpdatedAt))
	return err
}

func (s *Store) ListStatuses(ctx context.Context, kind, name, sha string) ([]Status, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT kind, name, sha, check_name, state, detail, actor, updated_at
		FROM statuses WHERE kind = ? AND name = ? AND sha = ? ORDER BY check_name`, kind, name, sha)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Status, 0)
	for rows.Next() {
		status, err := scanStatus(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, status)
	}
	return result, rows.Err()
}

func (s *Store) InsertRunToken(ctx context.Context, tx *sql.Tx, token RunToken) error {
	if err := requireTx(tx); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `
		INSERT INTO run_tokens (token_sha256, kind, name, expires_at, created_at)
		VALUES (?, ?, ?, ?, ?)`, token.TokenSHA256, token.Kind, token.Name,
		formatTime(token.ExpiresAt), formatTime(token.CreatedAt))
	return err
}

func (s *Store) LookupRunToken(ctx context.Context, hash string) (RunToken, error) {
	var token RunToken
	var expiresAt, createdAt string
	err := s.db.QueryRowContext(ctx, `
		SELECT token_sha256, kind, name, expires_at, created_at
		FROM run_tokens WHERE token_sha256 = ?`, hash).Scan(
		&token.TokenSHA256, &token.Kind, &token.Name, &expiresAt, &createdAt)
	if err != nil {
		return RunToken{}, err
	}
	if token.ExpiresAt, err = parseTime(expiresAt); err != nil {
		return RunToken{}, fmt.Errorf("parse run token expires_at: %w", err)
	}
	if token.CreatedAt, err = parseTime(createdAt); err != nil {
		return RunToken{}, fmt.Errorf("parse run token created_at: %w", err)
	}
	return token, nil
}

func (s *Store) SweepExpiredTokens(ctx context.Context, tx *sql.Tx, now time.Time) (int64, error) {
	if err := requireTx(tx); err != nil {
		return 0, err
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM run_tokens WHERE expires_at < ?`, formatTime(now))
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanRepository(row rowScanner) (Repository, error) {
	var repository Repository
	var archivedAt, archivedPath sql.NullString
	var createdAt string
	err := row.Scan(&repository.Kind, &repository.Name, &repository.OwnerID, &repository.OwnerEmail,
		&repository.DefaultBranch, &archivedAt, &archivedPath, &createdAt)
	if err != nil {
		return Repository{}, err
	}
	if archivedAt.Valid {
		value, err := parseTime(archivedAt.String)
		if err != nil {
			return Repository{}, fmt.Errorf("parse repository archived_at: %w", err)
		}
		repository.ArchivedAt = &value
	}
	if archivedPath.Valid {
		repository.ArchivedPath = &archivedPath.String
	}
	repository.CreatedAt, err = parseTime(createdAt)
	if err != nil {
		return Repository{}, fmt.Errorf("parse repository created_at: %w", err)
	}
	return repository, nil
}

func scanStatus(row rowScanner) (Status, error) {
	var status Status
	var detail sql.NullString
	var updatedAt string
	err := row.Scan(&status.Kind, &status.Name, &status.SHA, &status.CheckName,
		&status.State, &detail, &status.Actor, &updatedAt)
	if err != nil {
		return Status{}, err
	}
	if detail.Valid {
		status.Detail = &detail.String
	}
	status.UpdatedAt, err = parseTime(updatedAt)
	if err != nil {
		return Status{}, fmt.Errorf("parse status updated_at: %w", err)
	}
	return status, nil
}

func requireTx(tx *sql.Tx) error {
	if tx == nil {
		return fmt.Errorf("repos store: transaction is required")
	}
	return nil
}

func formatTime(value time.Time) string {
	return value.UTC().Format("2006-01-02T15:04:05.000000000Z")
}

func nullableTime(value *time.Time) any {
	if value == nil {
		return nil
	}
	return formatTime(*value)
}

func parseTime(value string) (time.Time, error) { return time.Parse(time.RFC3339Nano, value) }
