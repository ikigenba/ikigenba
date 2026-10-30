// Package store keeps the provenance of accounts-created groups and users.
package store

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
)

// Config supplies the record clock.
type Config struct{ Now func() time.Time }

// Group records the creation of a group.
type Group struct {
	Slug          string
	Created       time.Time
	CreatedUserID string
}

// User records the creation of a user.
type User struct {
	Name          string
	Created       time.Time
	CreatedUserID string
}

// ErrUnreachable identifies every inaccessible record operation.
var ErrUnreachable = errors.New("accounts record is unreachable")

// Unreachable is the user-facing database failure line.
const Unreachable string = "accounts record cannot be reached"

// Store owns the record over an already-open database.
type Store struct {
	d   *db.DB
	now func() time.Time
}

// New constructs a store without opening a database.
func New(d *db.DB, cfg Config) *Store { return &Store{d: d, now: cfg.Now} }

func unreachable(err error) error {
	if err == nil {
		return nil
	}
	return errors.Join(ErrUnreachable, err)
}

type queries struct{ insert, one, remove, window string }

var groupQueries = queries{
	insert: "INSERT INTO groups (slug, created, created_user_id) VALUES (?, ?, ?) ON CONFLICT (slug) DO NOTHING",
	one:    "SELECT created, created_user_id FROM groups WHERE slug = ?",
	remove: "DELETE FROM groups WHERE slug = ?",
	window: "SELECT slug, created, created_user_id FROM groups WHERE slug IN (",
}
var userQueries = queries{
	insert: "INSERT INTO users (name, created, created_user_id) VALUES (?, ?, ?) ON CONFLICT (name) DO NOTHING",
	one:    "SELECT created, created_user_id FROM users WHERE name = ?",
	remove: "DELETE FROM users WHERE name = ?",
	window: "SELECT name, created, created_user_id FROM users WHERE name IN (",
}

func (s *Store) add(ctx context.Context, q queries, name, userID string) (created time.Time, creator string, added bool, err error) {
	if ctx.Err() != nil {
		return time.Time{}, "", false, unreachable(ctx.Err())
	}
	err = s.d.Write(ctx, func(tx *sql.Tx) error {
		if name == "" || userID == "" {
			return ErrUnreachable
		}
		stamp := s.now().UTC().Truncate(time.Second)
		r, e := tx.ExecContext(ctx, q.insert, name, stamp.Format(time.RFC3339), userID)
		if e != nil {
			return e
		}
		n, e := r.RowsAffected()
		if e != nil {
			return e
		}
		added = n != 0
		var text string
		if e = tx.QueryRowContext(ctx, q.one, name).Scan(&text, &creator); e != nil {
			return e
		}
		created, e = time.Parse(time.RFC3339, text)
		created = created.UTC()
		return e
	})
	if err != nil {
		return time.Time{}, "", false, unreachable(err)
	}
	return created, creator, added, nil
}

func (s *Store) remove(ctx context.Context, query, name string) (bool, error) {
	if ctx.Err() != nil {
		return false, unreachable(ctx.Err())
	}
	var removed bool
	err := s.d.Write(ctx, func(tx *sql.Tx) error {
		if name == "" {
			return ErrUnreachable
		}
		r, err := tx.ExecContext(ctx, query, name)
		if err != nil {
			return err
		}
		n, err := r.RowsAffected()
		removed = n != 0
		return err
	})
	if err != nil {
		return false, unreachable(err)
	}
	return removed, nil
}

type record struct {
	name    string
	created time.Time
	creator string
}

func (s *Store) lookup(ctx context.Context, prefix string, names []string) ([]record, error) {
	if ctx.Err() != nil {
		return nil, unreachable(ctx.Err())
	}
	var found []record
	err := s.d.Read(ctx, func(tx *sql.Tx) error {
		// Chunk the requested names to remain below SQLite's parameter bound.
		seen := make(map[string]bool, len(names))
		unique := make([]string, 0, len(names))
		for _, n := range names {
			if !seen[n] {
				seen[n] = true
				unique = append(unique, n)
			}
		}
		for len(unique) > 0 {
			n := min(len(unique), 500)
			args := make([]any, n)
			var query strings.Builder
			query.WriteString(prefix)
			for i, name := range unique[:n] {
				if i > 0 {
					query.WriteByte(',')
				}
				query.WriteByte('?')
				args[i] = name
			}
			query.WriteByte(')')
			rows, err := tx.QueryContext(ctx, query.String(), args...)
			if err != nil {
				return err
			}
			for rows.Next() {
				var r record
				var text string
				if err = rows.Scan(&r.name, &text, &r.creator); err != nil {
					_ = rows.Close()
					return err
				}
				r.created, err = time.Parse(time.RFC3339, text)
				if err != nil {
					_ = rows.Close()
					return err
				}
				r.created = r.created.UTC()
				found = append(found, r)
			}
			err = errors.Join(rows.Err(), rows.Close())
			if err != nil {
				return err
			}
			unique = unique[n:]
		}
		return nil
	})
	if err != nil {
		return nil, unreachable(err)
	}
	return found, nil
}

// AddGroup records a group, preserving an earlier record of the same slug.
func (s *Store) AddGroup(ctx context.Context, slug, userID string) (Group, bool, error) {
	t, u, added, err := s.add(ctx, groupQueries, slug, userID)
	if err != nil {
		return Group{}, false, err
	}
	return Group{slug, t, u}, added, nil
}

// RemoveGroup removes only a group's provenance record.
func (s *Store) RemoveGroup(ctx context.Context, slug string) (bool, error) {
	return s.remove(ctx, groupQueries.remove, slug)
}

// Groups returns the recorded groups among the requested names.
func (s *Store) Groups(ctx context.Context, slugs []string) (map[string]Group, error) {
	rs, err := s.lookup(ctx, groupQueries.window, slugs)
	if err != nil {
		return nil, err
	}
	out := make(map[string]Group, len(rs))
	for _, r := range rs {
		out[r.name] = Group{r.name, r.created, r.creator}
	}
	return out, nil
}

// AddUser records a user, preserving an earlier record of the same name.
func (s *Store) AddUser(ctx context.Context, name, userID string) (User, bool, error) {
	t, u, added, err := s.add(ctx, userQueries, name, userID)
	if err != nil {
		return User{}, false, err
	}
	return User{name, t, u}, added, nil
}

// DeleteUser removes only a user's provenance record.
func (s *Store) DeleteUser(ctx context.Context, name string) (bool, error) {
	return s.remove(ctx, userQueries.remove, name)
}

// Users returns the recorded users among the requested names.
func (s *Store) Users(ctx context.Context, names []string) (map[string]User, error) {
	rs, err := s.lookup(ctx, userQueries.window, names)
	if err != nil {
		return nil, err
	}
	out := make(map[string]User, len(rs))
	for _, r := range rs {
		out[r.name] = User{r.name, r.created, r.creator}
	}
	return out, nil
}
