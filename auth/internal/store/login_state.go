package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/ikigenba/ikigenba/auth/internal/idcodec"
)

// CreateLoginState stores a login state under a new id and returns it.
func (s *Store) CreateLoginState(verifier, returnURL string) (LoginState, error) {
	state, err := idcodec.NewID(s.rand)
	if err != nil {
		return LoginState{}, fmt.Errorf("create login state id: %w", err)
	}

	loginState := LoginState{
		State:     state,
		Verifier:  verifier,
		ReturnURL: returnURL,
	}
	if _, err := s.db.ExecContext(
		context.Background(),
		`INSERT INTO login_states (state, verifier, return_url) VALUES (?, ?, ?)`,
		loginState.State,
		loginState.Verifier,
		loginState.ReturnURL,
	); err != nil {
		return LoginState{}, fmt.Errorf("insert login state: %w", err)
	}

	return loginState, nil
}

// ConsumeLoginState deletes the login state and returns it, or ErrNotFound.
func (s *Store) ConsumeLoginState(state string) (LoginState, error) {
	var loginState LoginState
	err := s.db.QueryRowContext(
		context.Background(),
		`DELETE FROM login_states WHERE state = ? RETURNING state, verifier, return_url`,
		state,
	).Scan(&loginState.State, &loginState.Verifier, &loginState.ReturnURL)
	if errors.Is(err, sql.ErrNoRows) {
		return LoginState{}, ErrNotFound
	}
	if err != nil {
		return LoginState{}, fmt.Errorf("consume login state: %w", err)
	}

	return loginState, nil
}
