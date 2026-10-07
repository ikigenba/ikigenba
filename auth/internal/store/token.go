package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ikigenba/ikigenba/auth/internal/idcodec"
)

// CreateToken stores an enabled token and returns it with its plaintext secret.
func (s *Store) CreateToken(userID, name string, expiry Expiry, now time.Time) (Token, string, error) {
	var token Token
	var secret string
	err := s.db.Write(context.Background(), func(tx *sql.Tx) error {
		expiresAt, err := tokenExpiry(expiry, now)
		if err != nil {
			return err
		}

		id, err := idcodec.NewID(s.rand)
		if err != nil {
			return fmt.Errorf("create token id: %w", err)
		}
		secret, err = idcodec.NewSecret(s.rand)
		if err != nil {
			return fmt.Errorf("create token secret: %w", err)
		}

		token = Token{
			ID:        idcodec.TokenIDPrefix + id,
			UserID:    userID,
			Name:      name,
			Hash:      idcodec.HashSecret(secret),
			Kind:      TokenPersonal,
			Enabled:   true,
			CreatedAt: now,
			ExpiresAt: expiresAt,
		}
		if _, err := tx.ExecContext(
			context.Background(),
			`INSERT INTO tokens (id, user_id, name, hash, enabled, created_at, expires_at, last_used_at, kind, host)
		 VALUES (?, ?, ?, ?, ?, ?, ?, NULL, 'personal', '')`,
			token.ID,
			token.UserID,
			token.Name,
			token.Hash,
			token.Enabled,
			token.CreatedAt.UnixNano(),
			nullableUnixNano(token.ExpiresAt),
		); err != nil {
			return fmt.Errorf("insert token: %w", err)
		}

		return nil
	})
	if err != nil {
		return Token{}, "", err
	}
	return token, secret, err
}

// ListTokens returns the user's tokens, newest created_at first, then id.
func (s *Store) ListTokens(userID string) ([]Token, error) {
	var tokens []Token
	err := s.db.Read(context.Background(), func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(
			context.Background(),
			`SELECT id, user_id, name, hash, enabled, created_at, expires_at, last_used_at, kind, host
		 FROM tokens
		 WHERE user_id = ?
		 ORDER BY created_at DESC, id ASC`,
			userID,
		)
		if err != nil {
			return fmt.Errorf("list tokens: %w", err)
		}
		defer func() { _ = rows.Close() }()

		tokens = make([]Token, 0)
		for rows.Next() {
			token, err := scanToken(rows)
			if err != nil {
				return fmt.Errorf("scan listed token: %w", err)
			}
			tokens = append(tokens, token)
		}
		if err := rows.Err(); err != nil {
			return fmt.Errorf("list tokens: %w", err)
		}

		return nil
	})
	if err != nil {
		return nil, err
	}
	return tokens, err
}

// SetTokenEnabled sets enabled on the user's token, or returns ErrNotFound.
func (s *Store) SetTokenEnabled(userID, tokenID string, enabled bool) error {
	err := s.db.Write(context.Background(), func(tx *sql.Tx) error {
		result, err := tx.ExecContext(
			context.Background(),
			`UPDATE tokens SET enabled = ? WHERE id = ? AND user_id = ? AND kind = 'personal'`,
			enabled,
			tokenID,
			userID,
		)
		if err != nil {
			return fmt.Errorf("set token enabled: %w", err)
		}
		return requireChangedRow(result, "set token enabled")
	})
	return err
}

// DeleteToken deletes the user's token, or returns ErrNotFound.
func (s *Store) DeleteToken(userID, tokenID string) error {
	err := s.db.Write(context.Background(), func(tx *sql.Tx) error {
		result, err := tx.ExecContext(context.Background(), `DELETE FROM tokens WHERE id = ? AND user_id = ? AND kind = 'personal'`, tokenID, userID)
		if err != nil {
			return fmt.Errorf("delete token: %w", err)
		}
		return requireChangedRow(result, "delete token")
	})
	return err
}

// LookupTokenIdentity returns the token's user when the secret is usable at now.
func (s *Store) LookupTokenIdentity(secret, host string, now time.Time) (Identity, error) {
	var identity Identity
	err := s.db.Read(context.Background(), func(tx *sql.Tx) error {
		err := tx.QueryRowContext(
			context.Background(),
			`SELECT users.id, users.email, tokens.id
		 FROM tokens
		 JOIN users ON users.id = tokens.user_id
		 WHERE tokens.hash = ?
		   AND tokens.enabled = 1
 AND (tokens.kind = 'personal' OR (tokens.kind = 'client' AND ? <> '' AND tokens.host = ? COLLATE NOCASE))
		   AND (tokens.expires_at IS NULL OR tokens.expires_at > ?)
		   AND users.last_google_login >= ?`,
			idcodec.HashSecret(secret),
			host, host,
			now.UnixNano(),
			now.Add(-TokenLoginWindow).UnixNano(),
		).Scan(&identity.UserID, &identity.Email, &identity.TokenID)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("lookup token identity: %w", err)
		}

		return nil
	})
	if err != nil {
		return Identity{}, err
	}
	return identity, err
}

// TouchTokenIdentity records last use and returns the token's user when the secret is usable at now.
func (s *Store) TouchTokenIdentity(secret, host string, now time.Time) (Identity, error) {
	var identity Identity
	err := s.db.Write(context.Background(), func(tx *sql.Tx) error {
		var userID string
		err := tx.QueryRowContext(
			context.Background(),
			`UPDATE tokens
		 SET last_used_at = ?
		 WHERE hash = ?
		   AND enabled = 1
 AND (kind = 'personal' OR (kind = 'client' AND ? <> '' AND host = ? COLLATE NOCASE))
		   AND (expires_at IS NULL OR expires_at > ?)
		   AND EXISTS (
		       SELECT 1 FROM users
		       WHERE users.id = tokens.user_id
		         AND users.last_google_login >= ?
		   )
		 RETURNING user_id, id`,
			now.UnixNano(),
			idcodec.HashSecret(secret),
			host, host,
			now.UnixNano(),
			now.Add(-TokenLoginWindow).UnixNano(),
		).Scan(&userID, &identity.TokenID)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("touch token: %w", err)
		}

		if err := tx.QueryRowContext(context.Background(), `SELECT id, email FROM users WHERE id = ?`, userID).Scan(
			&identity.UserID,
			&identity.Email,
		); err != nil {
			return fmt.Errorf("lookup touched token user: %w", err)
		}

		return nil
	})
	if err != nil {
		return Identity{}, err
	}
	return identity, err
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
		&token.Kind,
		&token.Host,
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
