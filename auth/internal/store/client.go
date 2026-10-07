package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/ikigenba/ikigenba/auth/internal/idcodec"
)

// RegisterClient registers a client and prunes expired unused registrations.
func (s *Store) RegisterClient(name string, redirectURIs []string, now time.Time) (Client, error) {
	var client Client
	err := s.db.Write(context.Background(), func(tx *sql.Tx) error {
		id, err := idcodec.NewID(s.rand)
		if err != nil {
			return fmt.Errorf("create client id: %w", err)
		}
		data, err := json.Marshal(redirectURIs)
		if err != nil {
			return err
		}
		client = Client{ID: idcodec.ClientIDPrefix + id, Name: name, RedirectURIs: append([]string(nil), redirectURIs...), CreatedAt: now}
		if _, err = tx.ExecContext(context.Background(), "DELETE FROM clients WHERE received_token = 0 AND created_at < ?", now.Add(-UnusedClientTTL).UnixNano()); err != nil {
			return err
		}
		_, err = tx.ExecContext(context.Background(), "INSERT INTO clients (id,name,redirect_uris,created_at,received_token) VALUES (?,?,?,?,0)", client.ID, name, string(data), now.UnixNano())
		return err
	})
	if err != nil {
		return Client{}, err
	}
	return client, nil
}

func readClient(tx *sql.Tx, id string) (Client, bool, error) {
	var client Client
	var redirects string
	var created int64
	var received bool
	err := tx.QueryRowContext(context.Background(), "SELECT id,name,redirect_uris,created_at,received_token FROM clients WHERE id = ?", id).Scan(&client.ID, &client.Name, &redirects, &created, &received)
	if errors.Is(err, sql.ErrNoRows) {
		return Client{}, false, ErrNotFound
	}
	if err != nil {
		return Client{}, false, err
	}
	if err = json.Unmarshal([]byte(redirects), &client.RedirectURIs); err != nil {
		return Client{}, false, err
	}
	client.CreatedAt = time.Unix(0, created).UTC()
	return client, received, nil
}

// LookupClient removes a stale unused registration or returns the client.
func (s *Store) LookupClient(clientID string, now time.Time) (Client, error) {
	var client Client
	missing := false
	err := s.db.Write(context.Background(), func(tx *sql.Tx) error {
		var received bool
		var err error
		client, received, err = readClient(tx, clientID)
		if errors.Is(err, ErrNotFound) {
			missing = true
			return nil
		}
		if err != nil {
			return err
		}
		if !received && now.Sub(client.CreatedAt) > UnusedClientTTL {
			_, err = tx.ExecContext(context.Background(), "DELETE FROM clients WHERE id = ?", clientID)
			missing = true
		}
		return err
	})
	if err != nil {
		return Client{}, err
	}
	if missing {
		return Client{}, ErrNotFound
	}
	return client, nil
}

// CreateAuthCode issues a single-use authorization code and prunes expired codes.
func (s *Store) CreateAuthCode(clientID, userID, redirectURI, challenge, resource string, now time.Time) (AuthCode, error) {
	var code AuthCode
	err := s.db.Write(context.Background(), func(tx *sql.Tx) error {
		id, err := idcodec.NewID(s.rand)
		if err != nil {
			return fmt.Errorf("create authorization code: %w", err)
		}
		code = AuthCode{Code: id, ClientID: clientID, UserID: userID, RedirectURI: redirectURI, Challenge: challenge, Resource: resource, IssuedAt: now}
		if err = pruneCodes(tx, now); err != nil {
			return err
		}
		_, err = tx.ExecContext(context.Background(), "INSERT INTO auth_codes (code,client_id,user_id,redirect_uri,challenge,resource,issued_at) VALUES (?,?,?,?,?,?,?)", id, clientID, userID, redirectURI, challenge, resource, now.UnixNano())
		return err
	})
	if err != nil {
		return AuthCode{}, err
	}
	return code, nil
}
func pruneCodes(tx *sql.Tx, now time.Time) error {
	_, err := tx.ExecContext(context.Background(), "DELETE FROM auth_codes WHERE issued_at < ?", now.Add(-AuthCodeTTL).UnixNano())
	return err
}

// ConsumeAuthCode removes a code even when expired and prunes other expired codes.
func (s *Store) ConsumeAuthCode(value string, now time.Time) (AuthCode, error) {
	var code AuthCode
	missing := false
	err := s.db.Write(context.Background(), func(tx *sql.Tx) error {
		if err := pruneCodes(tx, now); err != nil {
			return err
		}
		var issued int64
		err := tx.QueryRowContext(context.Background(), "DELETE FROM auth_codes WHERE code = ? RETURNING code,client_id,user_id,redirect_uri,challenge,resource,issued_at", value).Scan(&code.Code, &code.ClientID, &code.UserID, &code.RedirectURI, &code.Challenge, &code.Resource, &issued)
		if errors.Is(err, sql.ErrNoRows) {
			missing = true
			return nil
		}
		if err != nil {
			return err
		}
		code.IssuedAt = time.Unix(0, issued).UTC()
		return nil
	})
	if err != nil {
		return AuthCode{}, err
	}
	if missing {
		return AuthCode{}, ErrNotFound
	}
	return code, nil
}

// CreateClientToken issues a host-bound token and remembers that the client was used.
func (s *Store) CreateClientToken(code AuthCode, host string, now time.Time) (Token, string, error) {
	var token Token
	var secret string
	err := s.db.Write(context.Background(), func(tx *sql.Tx) error {
		client, _, err := readClient(tx, code.ClientID)
		if err != nil {
			return err
		}
		id, err := idcodec.NewID(s.rand)
		if err != nil {
			return fmt.Errorf("create client token id: %w", err)
		}
		secret, err = idcodec.NewSecret(s.rand)
		if err != nil {
			return fmt.Errorf("create client token secret: %w", err)
		}
		expiry := code.IssuedAt.Add(ClientTokenTTL)
		token = Token{ID: idcodec.TokenIDPrefix + id, UserID: code.UserID, Name: client.Name, Hash: idcodec.HashSecret(secret), Kind: TokenClient, Host: host, Enabled: true, CreatedAt: now, ExpiresAt: &expiry}
		if _, err = tx.ExecContext(context.Background(), "INSERT INTO tokens (id,user_id,name,hash,enabled,created_at,expires_at,last_used_at,kind,host) VALUES (?,?,?,?,1,?,?,NULL,?,?)", token.ID, token.UserID, token.Name, token.Hash, now.UnixNano(), expiry.UnixNano(), token.Kind, host); err != nil {
			return err
		}
		_, err = tx.ExecContext(context.Background(), "UPDATE clients SET received_token = 1 WHERE id = ?", client.ID)
		return err
	})
	if err != nil {
		return Token{}, "", err
	}
	return token, secret, nil
}

// RevokeToken removes only a client token owned by the given user.
func (s *Store) RevokeToken(userID, tokenID string) error {
	return s.db.Write(context.Background(), func(tx *sql.Tx) error {
		result, err := tx.ExecContext(context.Background(), "DELETE FROM tokens WHERE id = ? AND user_id = ? AND kind = 'client'", tokenID, userID)
		if err != nil {
			return err
		}
		return requireChangedRow(result, "revoke token")
	})
}
