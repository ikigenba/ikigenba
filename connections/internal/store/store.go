// Package store keeps connections and single-use pending flows.
package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
)

// Identifier grammar, grant statuses, and unavailable copy.
const (
	IDPrefix            = "con_"
	StatusOK            = "ok"
	StatusBroken        = "broken"
	Unreachable  string = "cannot reach connection storage; try again later"
)

// Errors distinguish provider or storage refusal kinds.
var (
	ErrNotFound    = errors.New("connection not found")
	ErrExpired     = errors.New("pending state expired")
	ErrForeign     = errors.New("pending state belongs to another user")
	ErrUnreachable = errors.New(Unreachable)
)

// Config supplies the store clock and random source.
type Config struct {
	Now  func() time.Time
	Rand io.Reader
}

// Connection is a user’s persisted provider grant.
type Connection struct {
	ID, UserID, UserEmail, Provider, AccountEmail, AccountSubject, RefreshToken, AccessToken string
	AccessExpires                                                                            time.Time
	Scopes                                                                                   []string
	Status                                                                                   string
	Created, Modified                                                                        time.Time
}

// Pending is a single-use connect flow.
type Pending struct {
	State, Verifier, UserID, UserEmail, Provider string
	Expires                                      time.Time
}

// Access supplies a refreshed access token.
type Access struct {
	Token   string
	Expires time.Time
	Refresh string
	Scopes  []string
}

// Store coordinates persisted grants and pending states.
type Store struct {
	d   *db.DB
	cfg Config
	mu  sync.Mutex
}

// New builds a store without opening storage.
func New(d *db.DB, cfg Config) *Store {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Rand == nil {
		cfg.Rand = rand.Reader
	}
	return &Store{d: d, cfg: cfg}
}
func stamp(t time.Time) time.Time { return t.UTC().Truncate(time.Second) }
func text(t time.Time) string     { return stamp(t).Format(time.RFC3339) }
func classify(err error) error {
	if err == nil {
		return nil
	}
	for _, e := range []error{ErrNotFound, ErrExpired, ErrForeign, ErrUnreachable} {
		if errors.Is(err, e) {
			return err
		}
	}
	return errors.Join(ErrUnreachable, err)
}
func (s *Store) tx(ctx context.Context, write bool, fn func(*sql.Tx) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if ctx.Err() != nil {
		return classify(ctx.Err())
	}
	if write {
		return classify(s.d.Write(ctx, fn))
	}
	return classify(s.d.Read(ctx, fn))
}

const columns = "id,user_id,user_email,provider,account_email,account_subject,refresh_token,access_token,access_expires,scopes,status,created,modified"

type scanner interface{ Scan(...any) error }

func scan(r scanner) (Connection, error) {
	var c Connection
	var ex, sc, cr, mo string
	err := r.Scan(&c.ID, &c.UserID, &c.UserEmail, &c.Provider, &c.AccountEmail, &c.AccountSubject, &c.RefreshToken, &c.AccessToken, &ex, &sc, &c.Status, &cr, &mo)
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrNotFound
	}
	if err != nil {
		return c, err
	}
	c.AccessExpires, err = time.Parse(time.RFC3339, ex)
	if err != nil {
		return c, err
	}
	c.Created, err = time.Parse(time.RFC3339, cr)
	if err != nil {
		return c, err
	}
	c.Modified, err = time.Parse(time.RFC3339, mo)
	c.AccessExpires = c.AccessExpires.UTC()
	c.Created = c.Created.UTC()
	c.Modified = c.Modified.UTC()
	c.Scopes = strings.Fields(sc)
	if c.Scopes == nil {
		c.Scopes = []string{}
	}
	return c, err
}
func lookup(ctx context.Context, tx *sql.Tx, user, provider string) (Connection, error) {
	return scan(tx.QueryRowContext(ctx, "SELECT "+columns+" FROM connections WHERE user_id=? AND provider=?", user, provider))
}

// Get reads the user’s connection for a provider.
func (s *Store) Get(ctx context.Context, user, provider string) (Connection, error) {
	var c Connection
	err := s.tx(ctx, false, func(tx *sql.Tx) error { var e error; c, e = lookup(ctx, tx, user, provider); return e })
	if err != nil {
		return Connection{}, err
	}
	return c, nil
}

// List reads the user’s connections in provider order.
func (s *Store) List(ctx context.Context, user string) ([]Connection, error) {
	out := []Connection{}
	err := s.tx(ctx, false, func(tx *sql.Tx) error {
		rows, e := tx.QueryContext(ctx, "SELECT "+columns+" FROM connections WHERE user_id=? ORDER BY provider COLLATE BINARY", user)
		if e != nil {
			return e
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			c, e := scan(rows)
			if e != nil {
				return e
			}
			out = append(out, c)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}
func (s *Store) draw(ctx context.Context, tx *sql.Tx) (string, error) {
	for range 8 {
		var b [8]byte
		if _, e := io.ReadFull(s.cfg.Rand, b[:]); e != nil {
			return "", e
		}
		id := IDPrefix + hex.EncodeToString(b[:])
		var exists int
		e := tx.QueryRowContext(ctx, "SELECT 1 FROM connections WHERE id=?", id).Scan(&exists)
		if errors.Is(e, sql.ErrNoRows) {
			return id, nil
		}
		if e != nil {
			return "", e
		}
	}
	return "", ErrUnreachable
}
func save(ctx context.Context, tx *sql.Tx, c Connection) error {
	_, e := tx.ExecContext(ctx, "INSERT INTO connections ("+columns+") VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(user_id,provider) DO UPDATE SET user_email=excluded.user_email,account_email=excluded.account_email,account_subject=excluded.account_subject,refresh_token=excluded.refresh_token,access_token=excluded.access_token,access_expires=excluded.access_expires,scopes=excluded.scopes,status=excluded.status,modified=excluded.modified", c.ID, c.UserID, c.UserEmail, c.Provider, c.AccountEmail, c.AccountSubject, c.RefreshToken, c.AccessToken, text(c.AccessExpires), strings.Join(c.Scopes, " "), c.Status, text(c.Created), text(c.Modified))
	return e
}

// Upsert creates or replaces a user’s grant.
func (s *Store) Upsert(ctx context.Context, c Connection) (Connection, bool, error) {
	var out Connection
	var created bool
	err := s.tx(ctx, true, func(tx *sql.Tx) error {
		old, e := lookup(ctx, tx, c.UserID, c.Provider)
		if e != nil && !errors.Is(e, ErrNotFound) {
			return e
		}
		created = errors.Is(e, ErrNotFound)
		out = c
		out.ID = old.ID
		out.Created = old.Created
		if created {
			out.ID, e = s.draw(ctx, tx)
			if e != nil {
				return e
			}
			out.Created = stamp(s.cfg.Now())
		}
		out.Modified = stamp(s.cfg.Now())
		out.AccessExpires = stamp(c.AccessExpires)
		out.Status = StatusOK
		out.Scopes = append([]string{}, c.Scopes...)
		return save(ctx, tx, out)
	})
	if err != nil {
		return Connection{}, false, err
	}
	return out, created, nil
}
func (s *Store) change(ctx context.Context, user, provider string, fn func(*Connection)) (Connection, error) {
	var out Connection
	err := s.tx(ctx, true, func(tx *sql.Tx) error {
		var e error
		out, e = lookup(ctx, tx, user, provider)
		if e != nil {
			return e
		}
		fn(&out)
		out.Modified = stamp(s.cfg.Now())
		return save(ctx, tx, out)
	})
	if err != nil {
		return Connection{}, err
	}
	return out, nil
}

// SetAccess updates the access token and optional refresh fields.
func (s *Store) SetAccess(ctx context.Context, user, provider string, a Access) (Connection, error) {
	return s.change(ctx, user, provider, func(c *Connection) {
		c.AccessToken = a.Token
		c.AccessExpires = stamp(a.Expires)
		if a.Refresh != "" {
			c.RefreshToken = a.Refresh
		}
		if a.Scopes != nil {
			c.Scopes = append([]string{}, a.Scopes...)
		}
	})
}

// MarkBroken marks a refused grant as broken.
func (s *Store) MarkBroken(ctx context.Context, user, provider string) (Connection, error) {
	return s.change(ctx, user, provider, func(c *Connection) { c.Status = StatusBroken })
}

// Delete removes and returns the user’s connection.
func (s *Store) Delete(ctx context.Context, user, provider string) (Connection, error) {
	var out Connection
	err := s.tx(ctx, true, func(tx *sql.Tx) error {
		var e error
		out, e = lookup(ctx, tx, user, provider)
		if e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, "DELETE FROM connections WHERE user_id=? AND provider=?", user, provider)
		return e
	})
	if err != nil {
		return Connection{}, err
	}
	return out, nil
}
func purge(ctx context.Context, tx *sql.Tx, now time.Time) error {
	_, e := tx.ExecContext(ctx, "DELETE FROM pending WHERE expires<=?", text(now))
	return e
}

// PutPending stores a flow and removes expired flows.
func (s *Store) PutPending(ctx context.Context, p Pending) error {
	return s.tx(ctx, true, func(tx *sql.Tx) error {
		_, e := tx.ExecContext(ctx, "INSERT INTO pending(state,verifier,user_id,user_email,provider,expires) VALUES(?,?,?,?,?,?) ON CONFLICT(state) DO UPDATE SET verifier=excluded.verifier,user_id=excluded.user_id,user_email=excluded.user_email,provider=excluded.provider,expires=excluded.expires", p.State, p.Verifier, p.UserID, p.UserEmail, p.Provider, text(p.Expires))
		if e != nil {
			return e
		}
		return purge(ctx, tx, s.cfg.Now())
	})
}

// TakePending consumes an unexpired flow owned by the caller.
func (s *Store) TakePending(ctx context.Context, state, user, provider string) (Pending, error) {
	var out Pending
	var refusal error
	err := s.tx(ctx, true, func(tx *sql.Tx) error {
		var ex string
		e := tx.QueryRowContext(ctx, "SELECT state,verifier,user_id,user_email,provider,expires FROM pending WHERE state=? AND provider=?", state, provider).Scan(&out.State, &out.Verifier, &out.UserID, &out.UserEmail, &out.Provider, &ex)
		if errors.Is(e, sql.ErrNoRows) {
			return ErrNotFound
		}
		if e != nil {
			return e
		}
		out.Expires, e = time.Parse(time.RFC3339, ex)
		if e != nil {
			return e
		}
		out.Expires = out.Expires.UTC()
		if !out.Expires.After(stamp(s.cfg.Now())) {
			refusal = ErrExpired
		} else if out.UserID != user {
			return ErrForeign
		}
		_, e = tx.ExecContext(ctx, "DELETE FROM pending WHERE state=?", state)
		return e
	})
	if err != nil {
		return Pending{}, err
	}
	if refusal != nil {
		return Pending{}, refusal
	}
	return out, nil
}

// PurgeExpiredPending removes flows whose expiry has come.
func (s *Store) PurgeExpiredPending(ctx context.Context) error {
	return s.tx(ctx, true, func(tx *sql.Tx) error { return purge(ctx, tx, s.cfg.Now()) })
}
