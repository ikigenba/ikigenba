package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ikigenba/ikigenba/auth/internal/idcodec"
)

// UpsertUserOnLogin returns the refreshed user and whether it was created.
func (s *Store) UpsertUserOnLogin(issuer, subject, email string, now time.Time) (User, bool, error) {
	var user User
	var created bool
	err := s.db.Write(context.Background(), func(tx *sql.Tx) error {
		var id string
		err := tx.QueryRowContext(
			context.Background(),
			`SELECT id FROM users WHERE issuer = ? AND subject = ?`,
			issuer,
			subject,
		).Scan(&id)
		switch {
		case err == nil:
			if _, err := tx.ExecContext(
				context.Background(),
				`UPDATE users SET email = ?, last_google_login = ? WHERE id = ?`,
				email,
				now.UnixNano(),
				id,
			); err != nil {
				return fmt.Errorf("update user on login: %w", err)
			}
		case errors.Is(err, sql.ErrNoRows):
			created = true
			id, err = idcodec.NewID(s.rand)
			if err != nil {
				return fmt.Errorf("create user id: %w", err)
			}
			if _, err := tx.ExecContext(
				context.Background(),
				`INSERT INTO users (id, issuer, subject, email, last_google_login) VALUES (?, ?, ?, ?, ?)`,
				id,
				issuer,
				subject,
				email,
				now.UnixNano(),
			); err != nil {
				return fmt.Errorf("insert user on login: %w", err)
			}
		default:
			return fmt.Errorf("find user on login: %w", err)
		}

		user = User{
			ID:              id,
			Issuer:          issuer,
			Subject:         subject,
			Email:           email,
			LastGoogleLogin: now,
		}
		return nil
	})
	if err != nil {
		return User{}, false, err
	}
	return user, created, err
}

// CreateSession stores a session for the user and returns it.
func (s *Store) CreateSession(userID string, now time.Time) (Session, error) {
	var session Session
	err := s.db.Write(context.Background(), func(tx *sql.Tx) error {
		id, err := idcodec.NewID(s.rand)
		if err != nil {
			return fmt.Errorf("create session id: %w", err)
		}

		session = Session{
			ID:         id,
			UserID:     userID,
			LoginAt:    now,
			LastUsedAt: now,
		}
		if _, err := tx.ExecContext(
			context.Background(),
			`INSERT INTO sessions (id, user_id, login_at, last_used_at) VALUES (?, ?, ?, ?)`,
			session.ID,
			session.UserID,
			session.LoginAt.UnixNano(),
			session.LastUsedAt.UnixNano(),
		); err != nil {
			return fmt.Errorf("insert session: %w", err)
		}

		return nil
	})
	if err != nil {
		return Session{}, err
	}
	return session, err
}

// LookupSessionIdentity returns the session's user when it is still within idle and max age.
func (s *Store) LookupSessionIdentity(sessionID string, now time.Time) (Identity, error) {
	var identity Identity
	err := s.db.Read(context.Background(), func(tx *sql.Tx) error {
		err := tx.QueryRowContext(
			context.Background(),
			`SELECT users.id, users.email
		 FROM sessions
		 JOIN users ON users.id = sessions.user_id
		 WHERE sessions.id = ?
		   AND sessions.last_used_at >= ?
		   AND sessions.login_at >= ?`,
			sessionID,
			now.Add(-SessionIdle).UnixNano(),
			now.Add(-SessionMax).UnixNano(),
		).Scan(&identity.UserID, &identity.Email)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("lookup session identity: %w", err)
		}

		return nil
	})
	if err != nil {
		return Identity{}, err
	}
	return identity, err
}

// TouchSession records last use and returns the session's user when it is still within idle and max age.
func (s *Store) TouchSession(sessionID string, now time.Time) (Identity, error) {
	var identity Identity
	err := s.db.Write(context.Background(), func(tx *sql.Tx) error {
		var userID string
		err := tx.QueryRowContext(
			context.Background(),
			`UPDATE sessions
		 SET last_used_at = ?
		 WHERE id = ?
		   AND last_used_at >= ?
		   AND login_at >= ?
		 RETURNING user_id`,
			now.UnixNano(),
			sessionID,
			now.Add(-SessionIdle).UnixNano(),
			now.Add(-SessionMax).UnixNano(),
		).Scan(&userID)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("touch session: %w", err)
		}

		if err := tx.QueryRowContext(
			context.Background(),
			`SELECT id, email FROM users WHERE id = ?`,
			userID,
		).Scan(&identity.UserID, &identity.Email); err != nil {
			return fmt.Errorf("lookup touched session user: %w", err)
		}

		return nil
	})
	if err != nil {
		return Identity{}, err
	}
	return identity, err
}

// DeleteSession deletes the session. A missing session is not an error.
func (s *Store) DeleteSession(sessionID string) error {
	err := s.db.Write(context.Background(), func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(context.Background(), `DELETE FROM sessions WHERE id = ?`, sessionID); err != nil {
			return fmt.Errorf("delete session: %w", err)
		}

		return nil
	})
	return err
}
