package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ikigenba/ikigenba/auth/internal/idcodec"
)

func (s *Store) CreateToken(userID, name string, expiry Expiry, now time.Time) (Token, string, error) {
	expiresAt, err := tokenExpiry(expiry, now)
	if err != nil {
		return Token{}, "", err
	}

	id, err := idcodec.NewID(s.rand)
	if err != nil {
		return Token{}, "", fmt.Errorf("create token id: %w", err)
	}
	secret, err := idcodec.NewSecret(s.rand)
	if err != nil {
		return Token{}, "", fmt.Errorf("create token secret: %w", err)
	}

	token := Token{
		ID:        id,
		UserID:    userID,
		Name:      name,
		Hash:      idcodec.HashSecret(secret),
		Enabled:   true,
		CreatedAt: now,
		ExpiresAt: expiresAt,
	}
	if _, err := s.db.Exec(
		`INSERT INTO tokens (id, user_id, name, hash, enabled, created_at, expires_at, last_used_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, NULL)`,
		token.ID,
		token.UserID,
		token.Name,
		token.Hash,
		token.Enabled,
		token.CreatedAt.UnixNano(),
		nullableUnixNano(token.ExpiresAt),
	); err != nil {
		return Token{}, "", fmt.Errorf("insert token: %w", err)
	}

	return token, secret, nil
}

func (s *Store) ListTokens(userID string) ([]Token, error) {
	rows, err := s.db.Query(
		`SELECT id, user_id, name, hash, enabled, created_at, expires_at, last_used_at
		 FROM tokens
		 WHERE user_id = ?
		 ORDER BY created_at DESC, id ASC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("list tokens: %w", err)
	}
	defer func() { _ = rows.Close() }()

	tokens := make([]Token, 0)
	for rows.Next() {
		token, err := scanToken(rows)
		if err != nil {
			return nil, fmt.Errorf("scan listed token: %w", err)
		}
		tokens = append(tokens, token)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list tokens: %w", err)
	}

	return tokens, nil
}

func (s *Store) SetTokenEnabled(userID, tokenID string, enabled bool) error {
	result, err := s.db.Exec(
		`UPDATE tokens SET enabled = ? WHERE id = ? AND user_id = ?`,
		enabled,
		tokenID,
		userID,
	)
	if err != nil {
		return fmt.Errorf("set token enabled: %w", err)
	}
	return requireChangedRow(result, "set token enabled")
}

func (s *Store) DeleteToken(userID, tokenID string) error {
	result, err := s.db.Exec(`DELETE FROM tokens WHERE id = ? AND user_id = ?`, tokenID, userID)
	if err != nil {
		return fmt.Errorf("delete token: %w", err)
	}
	return requireChangedRow(result, "delete token")
}

func (s *Store) LookupTokenIdentity(secret string, now time.Time) (Identity, error) {
	var identity Identity
	err := s.db.QueryRow(
		`SELECT users.id, users.email
		 FROM tokens
		 JOIN users ON users.id = tokens.user_id
		 WHERE tokens.hash = ?
		   AND tokens.enabled = 1
		   AND (tokens.expires_at IS NULL OR tokens.expires_at > ?)
		   AND users.last_google_login >= ?`,
		idcodec.HashSecret(secret),
		now.UnixNano(),
		now.Add(-TokenLoginWindow).UnixNano(),
	).Scan(&identity.UserID, &identity.Email)
	if errors.Is(err, sql.ErrNoRows) {
		return Identity{}, ErrNotFound
	}
	if err != nil {
		return Identity{}, fmt.Errorf("lookup token identity: %w", err)
	}

	return identity, nil
}

func (s *Store) TouchTokenIdentity(secret string, now time.Time) (Identity, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return Identity{}, fmt.Errorf("begin token touch: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var userID string
	err = tx.QueryRow(
		`UPDATE tokens
		 SET last_used_at = ?
		 WHERE hash = ?
		   AND enabled = 1
		   AND (expires_at IS NULL OR expires_at > ?)
		   AND EXISTS (
		       SELECT 1 FROM users
		       WHERE users.id = tokens.user_id
		         AND users.last_google_login >= ?
		   )
		 RETURNING user_id`,
		now.UnixNano(),
		idcodec.HashSecret(secret),
		now.UnixNano(),
		now.Add(-TokenLoginWindow).UnixNano(),
	).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		return Identity{}, ErrNotFound
	}
	if err != nil {
		return Identity{}, fmt.Errorf("touch token: %w", err)
	}

	var identity Identity
	if err := tx.QueryRow(`SELECT id, email FROM users WHERE id = ?`, userID).Scan(
		&identity.UserID,
		&identity.Email,
	); err != nil {
		return Identity{}, fmt.Errorf("lookup touched token user: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Identity{}, fmt.Errorf("commit token touch: %w", err)
	}

	return identity, nil
}

func tokenExpiry(expiry Expiry, now time.Time) (*time.Time, error) {
	var duration time.Duration
	switch expiry {
	case ExpiryNever:
		return nil, nil
	case Expiry30d:
		duration = 30 * 24 * time.Hour
	case Expiry90d:
		duration = 90 * 24 * time.Hour
	case Expiry365d:
		duration = 365 * 24 * time.Hour
	default:
		return nil, fmt.Errorf("create token: invalid expiry %q", expiry)
	}

	expiresAt := now.Add(duration)
	return &expiresAt, nil
}

func nullableUnixNano(value *time.Time) any {
	if value == nil {
		return nil
	}
	return value.UnixNano()
}

type tokenScanner interface {
	Scan(dest ...any) error
}

func scanToken(scanner tokenScanner) (Token, error) {
	var token Token
	var createdAt int64
	var expiresAt, lastUsedAt sql.NullInt64
	if err := scanner.Scan(
		&token.ID,
		&token.UserID,
		&token.Name,
		&token.Hash,
		&token.Enabled,
		&createdAt,
		&expiresAt,
		&lastUsedAt,
	); err != nil {
		return Token{}, err
	}
	token.CreatedAt = time.Unix(0, createdAt).UTC()
	if expiresAt.Valid {
		value := time.Unix(0, expiresAt.Int64).UTC()
		token.ExpiresAt = &value
	}
	if lastUsedAt.Valid {
		value := time.Unix(0, lastUsedAt.Int64).UTC()
		token.LastUsedAt = &value
	}

	return token, nil
}

func requireChangedRow(result sql.Result, operation string) error {
	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("%s result: %w", operation, err)
	}
	if rows == 0 {
		return ErrNotFound
	}
	return nil
}
