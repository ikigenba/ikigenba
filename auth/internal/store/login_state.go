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
	var loginState LoginState
	err := s.db.Write(context.Background(), func(tx *sql.Tx) error {
		state, err := idcodec.NewID(s.rand)
		if err != nil {
			return fmt.Errorf("create login state id: %w", err)
		}

		loginState = LoginState{
			State:     state,
			Verifier:  verifier,
			ReturnURL: returnURL,
		}
		if _, err := tx.ExecContext(
			context.Background(),
			`INSERT INTO login_states (state, verifier, return_url) VALUES (?, ?, ?)`,
			loginState.State,
			loginState.Verifier,
			loginState.ReturnURL,
		); err != nil {
			return fmt.Errorf("insert login state: %w", err)
		}

		return nil
	})
	if err != nil {
		return LoginState{}, err
	}
	return loginState, err
}

// ConsumeLoginState deletes the login state and returns it, or ErrNotFound.
func (s *Store) ConsumeLoginState(state string) (LoginState, error) {
	var loginState LoginState
	err := s.db.Write(context.Background(), func(tx *sql.Tx) error {
		err := tx.QueryRowContext(
			context.Background(),
			`DELETE FROM login_states WHERE state = ? RETURNING state, verifier, return_url`,
			state,
		).Scan(&loginState.State, &loginState.Verifier, &loginState.ReturnURL)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return fmt.Errorf("consume login state: %w", err)
		}

		return nil
	})
	if err != nil {
		return LoginState{}, err
	}
	return loginState, err
}
