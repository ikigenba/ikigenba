package broker

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/connections/internal/providers"
	"github.com/ikigenba/ikigenba/connections/internal/store"
	"github.com/ikigenba/ikigenba/connections/internal/trail"
)

// Token provides the declared connection contract.
type Token struct {
	AccessToken string   `json:"access_token"`
	Expires     string   `json:"expires"`
	Account     string   `json:"account"`
	Scopes      []string `json:"scopes"`
}

// MarshalJSON encodes scopes as an array, including an empty token.
func (t Token) MarshalJSON() ([]byte, error) {
	type wireToken Token
	value := wireToken(t)
	if value.Scopes == nil {
		value.Scopes = []string{}
	}
	return marshalTokenValue(value)
}
func marshalTokenValue(value any) ([]byte, error) { return json.Marshal(value) }

// Refusal provides the declared connection contract.
type Refusal struct {
	Error string `json:"error"`
	URL   string `json:"url,omitempty"`
}

// Refusal words identify machine-readable failures.
const (
	RefusalUnknownProvider = "unknown_provider"
	RefusalNotConnected    = "not_connected"
	RefusalReconnect       = "reconnect"
	RefusalProvider        = "provider"
	RefusalUnavailable     = "unavailable"
	RefusalMethod          = "method_not_allowed"
)

// tokenAnswer provides the declared connection contract.
func tokenAnswer(c store.Connection) Token {
	return Token{AccessToken: c.AccessToken, Expires: c.AccessExpires.UTC().Format(time.RFC3339), Account: c.AccountEmail, Scopes: append([]string{}, c.Scopes...)}
}

// Token provides the declared connection contract.
func (b *Broker) Token(ctx context.Context, p string) (Token, error) {
	cl, ok := b.client(p)
	if !ok {
		return Token{}, ErrUnknownProvider
	}
	caller, _ := identity.FromContext(ctx)
	c, err := b.cfg.Store.Get(ctx, caller.UserID, p)
	if err != nil {
		return Token{}, err
	}
	if c.Status == store.StatusBroken {
		return Token{}, ErrReconnect
	}
	if !c.AccessExpires.Before(b.cfg.Now().Truncate(time.Second).Add(RefreshMargin)) {
		return tokenAnswer(c), nil
	}
	g, err := cl.Refresh(ctx, c.RefreshToken)
	if err != nil {
		if !errors.Is(err, providers.ErrRefused) {
			return Token{}, ErrProvider
		}
		c, err = b.cfg.Store.MarkBroken(ctx, caller.UserID, p)
		if err != nil {
			return Token{}, err
		}
		trail.Record(ctx, b.cfg.Telemetry, b.cfg.Events, trail.Broken, c)
		return Token{}, ErrReconnect
	}
	c, err = b.cfg.Store.SetAccess(ctx, caller.UserID, p, store.Access{Token: g.AccessToken, Expires: g.Expires, Refresh: g.RefreshToken, Scopes: g.Scopes})
	if err != nil {
		return Token{}, err
	}
	return tokenAnswer(c), nil
}

// Disconnect provides the declared connection contract.
func (b *Broker) Disconnect(ctx context.Context, p string) (store.Connection, error) {
	cl, ok := b.client(p)
	if !ok {
		return store.Connection{}, ErrUnknownProvider
	}
	caller, _ := identity.FromContext(ctx)
	c, err := b.cfg.Store.Get(ctx, caller.UserID, p)
	if err != nil {
		return store.Connection{}, err
	}
	if err = cl.Revoke(ctx, c.RefreshToken); err != nil {
		trail.RevokeFailure(ctx, b.cfg.Telemetry, c, providers.HTTPStatus(err))
	}
	c, err = b.cfg.Store.Delete(ctx, caller.UserID, p)
	if err != nil {
		return store.Connection{}, err
	}
	trail.Record(ctx, b.cfg.Telemetry, b.cfg.Events, trail.Deleted, c)
	return c, nil
}
