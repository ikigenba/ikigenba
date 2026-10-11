// Package broker implements the caller's connection operations.
package broker

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
	"net/url"
	"time"

	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/identity"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/connections/internal/providers"
	"github.com/ikigenba/ikigenba/connections/internal/store"
	"github.com/ikigenba/ikigenba/connections/internal/trail"
	"github.com/ikigenba/ikigenba/connections/internal/urls"
)

// StatePrefix provides the declared connection contract.
const StatePrefix = "connections."

// PendingLifetime provides the declared connection contract.
const PendingLifetime = 10 * time.Minute

// RefreshMargin provides the declared connection contract.
const RefreshMargin = 2 * time.Minute

// NoCallback provides the declared connection contract.
const NoCallback = "connections callback address is not configured"

// NoRandom provides the declared connection contract.
const NoRandom = "cannot generate a connection flow"

// Broker operation errors identify fixed failure categories.
var (
	ErrUnknownProvider = errors.New("unknown provider")
	ErrNoCallback      = errors.New("callback address missing")
	ErrRandom          = errors.New("random source failed")
	ErrMissing         = errors.New("missing state")
	ErrUnknown         = errors.New("unknown state")
	ErrExpired         = errors.New("expired state")
	ErrForeign         = errors.New("foreign state")
	ErrDenied          = errors.New("authorization denied")
	ErrExchange        = errors.New("exchange failed")
	ErrAccount         = errors.New("account unavailable")
	ErrReconnect       = errors.New("reconnect required")
	ErrProvider        = errors.New("provider failed")
)

// Config provides the declared connection contract.
type Config struct {
	Clients                   []providers.Client
	Store                     *store.Store
	Telemetry                 *telemetry.Writer
	Events                    *events.Emitter
	CallbackURL, ServicesPath string
	Now                       func() time.Time
	Rand                      io.Reader
}

// Broker provides the declared connection contract.
type Broker struct{ cfg Config }

// New provides the declared connection contract.
func New(cfg Config) *Broker {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Rand == nil {
		cfg.Rand = rand.Reader
	}
	return &Broker{cfg: cfg}
}

// client provides the declared connection contract.
func (b *Broker) client(p string) (providers.Client, bool) {
	for _, c := range b.cfg.Clients {
		if c.Provider.Name == p {
			return c, true
		}
	}
	return providers.Client{}, false
}

// redirect provides the declared connection contract.
func (b *Broker) redirect(p string) string {
	return urls.Callback(b.cfg.CallbackURL, b.cfg.ServicesPath) + "/" + p + "/callback"
}

// Begin provides the declared connection contract.
func (b *Broker) Begin(ctx context.Context, p string) (string, error) {
	cl, ok := b.client(p)
	if !ok {
		return "", ErrUnknownProvider
	}
	if urls.Callback(b.cfg.CallbackURL, b.cfg.ServicesPath) == "" {
		return "", ErrNoCallback
	}
	var state [16]byte
	var verifier [32]byte
	if _, err := io.ReadFull(b.cfg.Rand, state[:]); err != nil {
		return "", ErrRandom
	}
	if _, err := io.ReadFull(b.cfg.Rand, verifier[:]); err != nil {
		return "", ErrRandom
	}
	s := StatePrefix + hex.EncodeToString(state[:])
	v := base64.RawURLEncoding.EncodeToString(verifier[:])
	c, _ := identity.FromContext(ctx)
	err := b.cfg.Store.PutPending(ctx, store.Pending{State: s, Verifier: v, UserID: c.UserID, UserEmail: c.Email, Provider: p, Expires: b.cfg.Now().UTC().Truncate(time.Second).Add(PendingLifetime)})
	if err != nil {
		return "", err
	}
	return cl.AuthorizeURL(s, v, b.redirect(p)), nil
}

// Complete provides the declared connection contract.
func (b *Broker) Complete(ctx context.Context, p string, q url.Values) (store.Connection, error) {
	cl, ok := b.client(p)
	if !ok {
		return store.Connection{}, ErrUnknownProvider
	}
	if q.Get("state") == "" {
		return store.Connection{}, ErrMissing
	}
	c, _ := identity.FromContext(ctx)
	pd, err := b.cfg.Store.TakePending(ctx, q.Get("state"), c.UserID, p)
	if err != nil {
		for _, pair := range []struct{ from, to error }{{store.ErrNotFound, ErrUnknown}, {store.ErrExpired, ErrExpired}, {store.ErrForeign, ErrForeign}} {
			if errors.Is(err, pair.from) {
				return store.Connection{}, pair.to
			}
		}
		return store.Connection{}, err
	}
	if q.Has("error") {
		return store.Connection{}, ErrDenied
	}
	if q.Get("code") == "" {
		return store.Connection{}, ErrExchange
	}
	g, err := cl.Exchange(ctx, q.Get("code"), pd.Verifier, b.redirect(p))
	if err != nil {
		if errors.Is(err, providers.ErrAccount) {
			return store.Connection{}, ErrAccount
		}
		return store.Connection{}, ErrExchange
	}
	con, _, err := b.cfg.Store.Upsert(ctx, store.Connection{UserID: c.UserID, UserEmail: c.Email, Provider: p, AccountEmail: g.Account.Email, AccountSubject: g.Account.Subject, RefreshToken: g.RefreshToken, AccessToken: g.AccessToken, AccessExpires: g.Expires, Scopes: g.Scopes})
	if err != nil {
		return store.Connection{}, err
	}
	trail.Record(ctx, b.cfg.Telemetry, b.cfg.Events, trail.Created, con)
	return con, nil
}
