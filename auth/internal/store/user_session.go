package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/ikigenba/ikigenba/auth/internal/idcodec"
)

func (s *Store) UpsertUserOnLogin(issuer, subject, email string, now time.Time) (User, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return User{}, fmt.Errorf("begin user upsert: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var id string
	err = tx.QueryRow(
		`SELECT id FROM users WHERE issuer = ? AND subject = ?`,
		issuer,
		subject,
	).Scan(&id)
	switch {
	case err == nil:
		if _, err := tx.Exec(
			`UPDATE users SET email = ?, last_google_login = ? WHERE id = ?`,
			email,
			now.UnixNano(),
			id,
		); err != nil {
			return User{}, fmt.Errorf("update user on login: %w", err)
		}
	case errors.Is(err, sql.ErrNoRows):
		id, err = idcodec.NewID(s.rand)
		if err != nil {
			return User{}, fmt.Errorf("create user id: %w", err)
		}
		if _, err := tx.Exec(
			`INSERT INTO users (id, issuer, subject, email, last_google_login) VALUES (?, ?, ?, ?, ?)`,
			id,
			issuer,
			subject,
			email,
			now.UnixNano(),
		); err != nil {
			return User{}, fmt.Errorf("insert user on login: %w", err)
		}
	default:
		return User{}, fmt.Errorf("find user on login: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return User{}, fmt.Errorf("commit user upsert: %w", err)
	}

	return User{
		ID:              id,
		Issuer:          issuer,
		Subject:         subject,
		Email:           email,
		LastGoogleLogin: now,
	}, nil
}

func (s *Store) CreateSession(userID string, now time.Time) (Session, error) {
	id, err := idcodec.NewID(s.rand)
	if err != nil {
		return Session{}, fmt.Errorf("create session id: %w", err)
	}

	session := Session{
		ID:         id,
		UserID:     userID,
		LoginAt:    now,
		LastUsedAt: now,
	}
	if _, err := s.db.Exec(
		`INSERT INTO sessions (id, user_id, login_at, last_used_at) VALUES (?, ?, ?, ?)`,
		session.ID,
		session.UserID,
		session.LoginAt.UnixNano(),
		session.LastUsedAt.UnixNano(),
	); err != nil {
		return Session{}, fmt.Errorf("insert session: %w", err)
	}

	return session, nil
}

func (s *Store) LookupSessionIdentity(sessionID string, now time.Time) (Identity, error) {
	var identity Identity
	err := s.db.QueryRow(
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
		return Identity{}, ErrNotFound
	}
	if err != nil {
		return Identity{}, fmt.Errorf("lookup session identity: %w", err)
	}

	return identity, nil
}

func (s *Store) TouchSession(sessionID string, now time.Time) (Identity, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return Identity{}, fmt.Errorf("begin session touch: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var userID string
	err = tx.QueryRow(
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
		return Identity{}, ErrNotFound
	}
	if err != nil {
		return Identity{}, fmt.Errorf("touch session: %w", err)
	}

	var identity Identity
	if err := tx.QueryRow(
		`SELECT id, email FROM users WHERE id = ?`,
		userID,
	).Scan(&identity.UserID, &identity.Email); err != nil {
		return Identity{}, fmt.Errorf("lookup touched session user: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return Identity{}, fmt.Errorf("commit session touch: %w", err)
	}

	return identity, nil
}

func (s *Store) DeleteSession(sessionID string) error {
	if _, err := s.db.Exec(`DELETE FROM sessions WHERE id = ?`, sessionID); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}

	return nil
}
