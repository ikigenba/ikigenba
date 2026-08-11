package db

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base32"
	"fmt"
	"strings"
	"time"
)

const timestampLayout = "2006-01-02T15:04:05.000000000Z"

type Store struct {
	db       *sql.DB
	newToken func() string
}

func NewStore(conn *sql.DB, newToken func() string) *Store {
	if newToken == nil {
		newToken = randomToken
	}
	return &Store{db: conn, newToken: newToken}
}

func randomToken() string {
	var entropy [19]byte
	if _, err := rand.Read(entropy[:]); err != nil {
		panic(fmt.Sprintf("generate token: %v", err))
	}
	return strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(entropy[:]))
}

type Upload struct {
	Token       string
	OwnerID     string
	OwnerEmail  string
	Filename    string
	Description string
	Visibility  string
	CreatedAt   time.Time
	ExpiresAt   time.Time
	ConsumedAt  *time.Time
	ArtifactID  *string
}

type CreateUploadParams struct {
	OwnerID     string
	OwnerEmail  string
	Filename    string
	Description string
	Visibility  string
	CreatedAt   time.Time
	ExpiresAt   time.Time
}

func (s *Store) CreateUpload(ctx context.Context, p CreateUploadParams) (Upload, error) {
	var lastErr error
	for range 2 {
		u := Upload{Token: s.newToken(), OwnerID: p.OwnerID, OwnerEmail: p.OwnerEmail, Filename: p.Filename, Description: p.Description, Visibility: p.Visibility, CreatedAt: p.CreatedAt.UTC(), ExpiresAt: p.ExpiresAt.UTC()}
		_, err := s.db.ExecContext(ctx, `INSERT INTO uploads
			(token, owner_id, owner_email, filename, description, visibility, created_at, expires_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, u.Token, u.OwnerID, u.OwnerEmail, u.Filename, u.Description, u.Visibility, formatTime(u.CreatedAt), formatTime(u.ExpiresAt))
		if err == nil {
			return u, nil
		}
		lastErr = err
	}
	return Upload{}, fmt.Errorf("create upload after token retry: %w", lastErr)
}

func (s *Store) GetUpload(ctx context.Context, token string) (Upload, error) {
	row := s.db.QueryRowContext(ctx, `SELECT token, owner_id, owner_email, filename, description, visibility,
		created_at, expires_at, consumed_at, artifact_id FROM uploads WHERE token = ?`, token)
	return scanUpload(row)
}

func (s *Store) ConsumeUpload(ctx context.Context, token, artifactID string, now time.Time) (bool, error) {
	result, err := s.db.ExecContext(ctx, `UPDATE uploads SET consumed_at = ?, artifact_id = ?
		WHERE token = ? AND consumed_at IS NULL`, formatTime(now), artifactID, token)
	if err != nil {
		return false, fmt.Errorf("consume upload: %w", err)
	}
	count, err := result.RowsAffected()
	return count == 1, err
}

func (s *Store) PurgeExpiredUploads(ctx context.Context, now time.Time) (int64, error) {
	result, err := s.db.ExecContext(ctx, `DELETE FROM uploads WHERE consumed_at IS NULL AND expires_at <= ?`, formatTime(now))
	if err != nil {
		return 0, fmt.Errorf("purge expired uploads: %w", err)
	}
	return result.RowsAffected()
}

type Artifact struct {
	ID            string
	OwnerID       string
	OwnerEmail    string
	Filename      string
	Description   string
	Visibility    string
	Size          int64
	ContentHash   string
	DownloadCount int64
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type CreateArtifactParams struct {
	OwnerID     string
	OwnerEmail  string
	Filename    string
	Description string
	Visibility  string
	Size        int64
	ContentHash string
	CreatedAt   time.Time
}

func (s *Store) CreateArtifact(ctx context.Context, p CreateArtifactParams) (Artifact, error) {
	var lastErr error
	for range 2 {
		a := Artifact{ID: s.newToken(), OwnerID: p.OwnerID, OwnerEmail: p.OwnerEmail, Filename: p.Filename, Description: p.Description, Visibility: p.Visibility, Size: p.Size, ContentHash: p.ContentHash, CreatedAt: p.CreatedAt.UTC(), UpdatedAt: p.CreatedAt.UTC()}
		_, err := s.db.ExecContext(ctx, `INSERT INTO artifacts
			(id, owner_id, owner_email, filename, description, visibility, size, content_hash, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, a.ID, a.OwnerID, a.OwnerEmail, a.Filename, a.Description, a.Visibility, a.Size, a.ContentHash, formatTime(a.CreatedAt), formatTime(a.UpdatedAt))
		if err == nil {
			return a, nil
		}
		lastErr = err
	}
	return Artifact{}, fmt.Errorf("create artifact after token retry: %w", lastErr)
}

func (s *Store) GetArtifact(ctx context.Context, id string) (Artifact, error) {
	row := s.db.QueryRowContext(ctx, artifactSelect+` WHERE id = ?`, id)
	return scanArtifact(row)
}

// CommitUpload atomically creates an artifact and consumes its pending upload.
// A false result means another request consumed (or time expired) first.
func (s *Store) CommitUpload(ctx context.Context, token, artifactID string, p CreateArtifactParams, now time.Time) (Artifact, bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Artifact{}, false, fmt.Errorf("begin upload commit: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	result, err := tx.ExecContext(ctx, `UPDATE uploads SET consumed_at = ?, artifact_id = ?
		WHERE token = ? AND consumed_at IS NULL AND expires_at > ?`, formatTime(now), artifactID, token, formatTime(now))
	if err != nil {
		return Artifact{}, false, fmt.Errorf("claim upload: %w", err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return Artifact{}, false, fmt.Errorf("count claimed uploads: %w", err)
	}
	if count != 1 {
		return Artifact{}, false, nil
	}

	created := p.CreatedAt.UTC()
	artifact := Artifact{
		ID: artifactID, OwnerID: p.OwnerID, OwnerEmail: p.OwnerEmail,
		Filename: p.Filename, Description: p.Description, Visibility: p.Visibility,
		Size: p.Size, ContentHash: p.ContentHash, CreatedAt: created, UpdatedAt: created,
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO artifacts
		(id, owner_id, owner_email, filename, description, visibility, size, content_hash, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, artifact.ID, artifact.OwnerID, artifact.OwnerEmail,
		artifact.Filename, artifact.Description, artifact.Visibility, artifact.Size, artifact.ContentHash,
		formatTime(artifact.CreatedAt), formatTime(artifact.UpdatedAt))
	if err != nil {
		return Artifact{}, false, fmt.Errorf("create uploaded artifact: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Artifact{}, false, fmt.Errorf("commit upload: %w", err)
	}
	return artifact, true, nil
}

func (s *Store) ListArtifacts(ctx context.Context) ([]Artifact, error) {
	rows, err := s.db.QueryContext(ctx, artifactSelect+` ORDER BY created_at DESC, id ASC`)
	if err != nil {
		return nil, fmt.Errorf("list artifacts: %w", err)
	}
	defer rows.Close()
	var result []Artifact
	for rows.Next() {
		a, err := scanArtifact(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, a)
	}
	return result, rows.Err()
}

type UpdateArtifactParams struct {
	Filename    string
	Description string
	Visibility  string
	UpdatedAt   time.Time
}

func (s *Store) UpdateArtifact(ctx context.Context, id string, p UpdateArtifactParams) (bool, error) {
	result, err := s.db.ExecContext(ctx, `UPDATE artifacts SET filename = ?, description = ?, visibility = ?, updated_at = ? WHERE id = ?`, p.Filename, p.Description, p.Visibility, formatTime(p.UpdatedAt), id)
	if err != nil {
		return false, fmt.Errorf("update artifact: %w", err)
	}
	count, err := result.RowsAffected()
	return count == 1, err
}

func (s *Store) DeleteArtifact(ctx context.Context, id string) (bool, error) {
	result, err := s.db.ExecContext(ctx, `DELETE FROM artifacts WHERE id = ?`, id)
	if err != nil {
		return false, fmt.Errorf("delete artifact: %w", err)
	}
	count, err := result.RowsAffected()
	return count == 1, err
}

func (s *Store) IncrementDownloadCount(ctx context.Context, id string) (bool, error) {
	result, err := s.db.ExecContext(ctx, `UPDATE artifacts SET download_count = download_count + 1 WHERE id = ?`, id)
	if err != nil {
		return false, fmt.Errorf("increment download count: %w", err)
	}
	count, err := result.RowsAffected()
	return count == 1, err
}

const artifactSelect = `SELECT id, owner_id, owner_email, filename, description, visibility, size,
	content_hash, download_count, created_at, updated_at FROM artifacts`

type scanner interface {
	Scan(...any) error
}

func scanUpload(row scanner) (Upload, error) {
	var u Upload
	var created, expires string
	var consumed, artifactID sql.NullString
	if err := row.Scan(&u.Token, &u.OwnerID, &u.OwnerEmail, &u.Filename, &u.Description, &u.Visibility, &created, &expires, &consumed, &artifactID); err != nil {
		return Upload{}, err
	}
	var err error
	if u.CreatedAt, err = parseTime(created); err != nil {
		return Upload{}, err
	}
	if u.ExpiresAt, err = parseTime(expires); err != nil {
		return Upload{}, err
	}
	if consumed.Valid {
		value, err := parseTime(consumed.String)
		if err != nil {
			return Upload{}, err
		}
		u.ConsumedAt = &value
	}
	if artifactID.Valid {
		u.ArtifactID = &artifactID.String
	}
	return u, nil
}

func scanArtifact(row scanner) (Artifact, error) {
	var a Artifact
	var created, updated string
	if err := row.Scan(&a.ID, &a.OwnerID, &a.OwnerEmail, &a.Filename, &a.Description, &a.Visibility, &a.Size, &a.ContentHash, &a.DownloadCount, &created, &updated); err != nil {
		return Artifact{}, err
	}
	var err error
	if a.CreatedAt, err = parseTime(created); err != nil {
		return Artifact{}, err
	}
	if a.UpdatedAt, err = parseTime(updated); err != nil {
		return Artifact{}, err
	}
	return a, nil
}

func formatTime(value time.Time) string { return value.UTC().Format(timestampLayout) }

func parseTime(value string) (time.Time, error) {
	parsed, err := time.Parse(timestampLayout, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse stored timestamp %q: %w", value, err)
	}
	return parsed, nil
}
