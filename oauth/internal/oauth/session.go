// Package oauth implements the OAuth protocol.
package oauth

import (
	"encoding/base64"
	"io"
)

// Session carries the per-login secrets from authorization through exchange.
type Session struct{ State, CodeVerifier string }

// NewSession draws both per-login secrets from entropy.
func NewSession(entropy io.Reader) (Session, error) {
	verifierBytes := make([]byte, 64)
	if _, err := io.ReadFull(entropy, verifierBytes); err != nil {
		return Session{}, err
	}

	stateBytes := make([]byte, 32)
	if _, err := io.ReadFull(entropy, stateBytes); err != nil {
		return Session{}, err
	}

	return Session{
		State:        base64.RawURLEncoding.EncodeToString(stateBytes),
		CodeVerifier: base64.RawURLEncoding.EncodeToString(verifierBytes),
	}, nil
}
