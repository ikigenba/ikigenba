// Package store owns the sites catalog and its atomic record operations.
package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
)

// Catalog identifiers and visibility values.
const (
	IDPrefix    = "sit_"
	Public      = "public"
	Private     = "private"
	Unreachable = "cannot reach the catalog; try again later"
)

// Catalog error sentinels distinguish refusals from storage failures.
var (
	ErrNotFound  = errors.New("site not found")
	ErrNameTaken = errors.New("name taken")
	ErrNotPublic = errors.New("site is not public")
)

// Config supplies catalog time and randomness.
type Config struct {
	Now  func() time.Time
	Rand io.Reader
}

// Site is the catalog record exposed to consumers.
type Site struct {
	ID, Name, Slug, Owner, Repo, Ref, Visibility string
	Listed                                       bool
	Commit                                       string
	Created, Published                           time.Time
}

// Draft supplies the initial fields of a site.
type Draft struct {
	Owner, Name, Repo, Ref, Visibility string
	Listed                             bool
}

// Change contains optional fields to update atomically.
type Change struct {
	Visibility *string
	Listed     *bool
	Ref        *string
}

// Store provides record operations over one catalog handle.
type Store struct {
	db   *db.DB
	now  func() time.Time
	rand io.Reader
}

// ValidID reports whether s is a catalog site identifier.
func ValidID(s string) bool { return strings.HasPrefix(s, IDPrefix) && validHex(s[len(IDPrefix):], 16) }
func validHex(s string, n int) bool {
	if len(s) != n {
		return false
	}
	for i := range len(s) {
		if (s[i] < '0' || s[i] > '9') && (s[i] < 'a' || s[i] > 'f') {
			return false
		}
	}
	return true
}

// ValidName reports whether s is an allowed site name.
func ValidName(s string) bool {
	if len(s) < 1 || len(s) > 64 || s[0] == '-' || s == "about" || s == "mcp" || s == "api" || s == "tools" {
		return false
	}
	for i := range len(s) {
		if (s[i] < 'a' || s[i] > 'z') && (s[i] < '0' || s[i] > '9') && s[i] != '-' {
			return false
		}
	}
	return true
}

// New builds a catalog over a caller-owned database handle.
func New(d *db.DB, cfg Config) *Store {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Rand == nil {
		cfg.Rand = rand.Reader
	}
	return &Store{db: d, now: cfg.Now, rand: cfg.Rand}
}

func transact[T any](ctx context.Context, operation func(context.Context, func(*sql.Tx) error) error, fn func(*sql.Tx) (T, error)) (T, error) {
	var out T
	if err := ctx.Err(); err != nil {
		return out, err
	}
	err := operation(ctx, func(tx *sql.Tx) error {
		var err error
		out, err = fn(tx)
		return err
	})
	if err != nil {
		var zero T
		return zero, err
	}
	return out, nil
}

type updateResult struct {
	site   Site
	fields []string
}
type apexResult struct {
	site Site
	set  bool
}

func (s *Store) all(ctx context.Context, tx *sql.Tx) ([]Site, error) {
	rows, err := tx.QueryContext(ctx, "SELECT payload FROM sites ORDER BY name COLLATE BINARY")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]Site, 0)
	for rows.Next() {
		var payload string
		var x Site
		if err = rows.Scan(&payload); err != nil {
			return nil, err
		}
		if err = json.Unmarshal([]byte(payload), &x); err != nil {
			return nil, err
		}
		x.Created = x.Created.UTC()
		x.Published = x.Published.UTC()
		out = append(out, x)
	}
	return out, rows.Err()
}
func (s *Store) byID(ctx context.Context, tx *sql.Tx, id string) (Site, error) {
	var payload string
	err := tx.QueryRowContext(ctx, "SELECT payload FROM sites WHERE id=?", id).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return Site{}, ErrNotFound
	}
	if err != nil {
		return Site{}, err
	}
	var x Site
	err = json.Unmarshal([]byte(payload), &x)
	x.Created = x.Created.UTC()
	x.Published = x.Published.UTC()
	return x, err
}
func (s *Store) save(ctx context.Context, tx *sql.Tx, x Site) error {
	b, err := json.Marshal(x)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO sites(id,name,slug,owner,payload) VALUES(?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET payload=excluded.payload", x.ID, x.Name, x.Slug, x.Owner, string(b))
	return err
}
func (s *Store) apexID(ctx context.Context, tx *sql.Tx) (string, error) {
	var id string
	err := tx.QueryRowContext(ctx, "SELECT value FROM settings WHERE key='apex'").Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return id, err
}
func (s *Store) selectOne(ctx context.Context, tx *sql.Tx, match func(Site) bool) (Site, error) {
	xs, err := s.all(ctx, tx)
	if err != nil {
		return Site{}, err
	}
	for _, x := range xs {
		if match(x) {
			return x, nil
		}
	}
	return Site{}, ErrNotFound
}

// Find finds an owner's site by name.
func (s *Store) Find(ctx context.Context, owner, name string) (Site, error) {
	return transact(ctx, s.db.Read, func(tx *sql.Tx) (Site, error) {
		return s.selectOne(ctx, tx, func(x Site) bool { return x.Owner == owner && x.Name == name })
	})
}

// BySlug finds a site by its exact serving slug.
func (s *Store) BySlug(ctx context.Context, slug string) (Site, error) {
	return transact(ctx, s.db.Read, func(tx *sql.Tx) (Site, error) {
		return s.selectOne(ctx, tx, func(x Site) bool { return x.Slug == slug })
	})
}
func (s *Store) list(ctx context.Context, tx *sql.Tx, match func(Site) bool) ([]Site, error) {
	xs, err := s.all(ctx, tx)
	if err != nil {
		return nil, err
	}
	out := make([]Site, 0)
	for _, x := range xs {
		if match(x) {
			out = append(out, x)
		}
	}
	return out, nil
}

// List lists one owner's sites in name order.
func (s *Store) List(ctx context.Context, owner string) ([]Site, error) {
	return transact(ctx, s.db.Read, func(tx *sql.Tx) ([]Site, error) {
		return s.list(ctx, tx, func(x Site) bool { return x.Owner == owner })
	})
}

// Visible lists all listed sites and the user's own unlisted sites.
func (s *Store) Visible(ctx context.Context, user string) ([]Site, error) {
	return transact(ctx, s.db.Read, func(tx *sql.Tx) ([]Site, error) {
		return s.list(ctx, tx, func(x Site) bool { return x.Listed || x.Owner == user })
	})
}
func (s *Store) taken(ctx context.Context, tx *sql.Tx, name string) (bool, error) {
	var n int
	err := tx.QueryRowContext(ctx, "SELECT count(*) FROM sites WHERE name=? OR slug=?", name, name).Scan(&n)
	return n > 0, err
}

// Taken reports whether a name or slug is already reserved.
func (s *Store) Taken(ctx context.Context, name string) (bool, error) {
	return transact(ctx, s.db.Read, func(tx *sql.Tx) (bool, error) {
		return s.taken(ctx, tx, name)
	})
}

// Create creates a site with a unique ID and serving slug.
func (s *Store) Create(ctx context.Context, d Draft) (Site, error) {
	return transact(ctx, s.db.Write, func(tx *sql.Tx) (Site, error) {
		if !ValidName(d.Name) || d.Owner == "" || d.Ref == "" || d.Visibility != Public && d.Visibility != Private {
			return Site{}, errors.New("invalid draft")
		}
		taken, err := s.taken(ctx, tx, d.Name)
		if err != nil {
			return Site{}, err
		}
		if taken {
			return Site{}, ErrNameTaken
		}
		var id string
		for {
			b := make([]byte, 8)
			if _, err = io.ReadFull(s.rand, b); err != nil {
				return Site{}, errors.New(err.Error())
			}
			id = IDPrefix + hex.EncodeToString(b)
			_, err = s.byID(ctx, tx, id)
			if errors.Is(err, ErrNotFound) {
				break
			}
			if err != nil {
				return Site{}, err
			}
		}
		slug := d.Name
		if !d.Listed {
			for {
				b := make([]byte, 4)
				if _, err = io.ReadFull(s.rand, b); err != nil {
					return Site{}, errors.New(err.Error())
				}
				slug = d.Name + "-" + hex.EncodeToString(b)
				taken, err = s.taken(ctx, tx, slug)
				if err != nil {
					return Site{}, err
				}
				if !taken {
					break
				}
			}
		}
		x := Site{ID: id, Name: d.Name, Slug: slug, Owner: d.Owner, Repo: d.Repo, Ref: d.Ref, Visibility: d.Visibility, Listed: d.Listed, Created: s.now().UTC().Truncate(time.Second)}
		if err = s.save(ctx, tx, x); err != nil {
			return Site{}, err
		}
		return x, nil
	})
}

// Publish atomically replaces the published commit and timestamp.
func (s *Store) Publish(ctx context.Context, id, commit string) (Site, error) {
	return transact(ctx, s.db.Write, func(tx *sql.Tx) (Site, error) {
		if !validHex(commit, 40) {
			return Site{}, errors.New("invalid commit")
		}
		x, err := s.byID(ctx, tx, id)
		if err != nil {
			return Site{}, err
		}
		x.Commit = commit
		x.Published = s.now().UTC().Truncate(time.Second)
		if err = s.save(ctx, tx, x); err != nil {
			return Site{}, err
		}
		return x, nil
	})
}

// Update atomically changes visibility, listing, and tracking ref.
func (s *Store) Update(ctx context.Context, id string, c Change) (Site, []string, error) {
	result, err := transact(ctx, s.db.Write, func(tx *sql.Tx) (updateResult, error) {
		if c.Visibility != nil && *c.Visibility != Public && *c.Visibility != Private || c.Ref != nil && *c.Ref == "" {
			return updateResult{}, errors.New("invalid change")
		}
		x, err := s.byID(ctx, tx, id)
		if err != nil {
			return updateResult{}, err
		}
		apex, err := s.apexID(ctx, tx)
		if err != nil {
			return updateResult{}, err
		}
		if apex == id && c.Visibility != nil && *c.Visibility == Private {
			return updateResult{}, ErrNotPublic
		}
		changed := make([]string, 0)
		if c.Visibility != nil && *c.Visibility != x.Visibility {
			x.Visibility = *c.Visibility
			changed = append(changed, "visibility")
		}
		if c.Listed != nil && *c.Listed != x.Listed {
			x.Listed = *c.Listed
			changed = append(changed, "listed")
		}
		if c.Ref != nil && *c.Ref != x.Ref {
			x.Ref = *c.Ref
			changed = append(changed, "ref")
		}
		if len(changed) > 0 {
			if err = s.save(ctx, tx, x); err != nil {
				return updateResult{}, err
			}
		}
		return updateResult{x, changed}, nil
	})
	return result.site, result.fields, err
}

// Delete deletes a site and clears its apex setting if present.
func (s *Store) Delete(ctx context.Context, id string) (bool, error) {
	return transact(ctx, s.db.Write, func(tx *sql.Tx) (bool, error) {
		if _, err := s.byID(ctx, tx, id); err != nil {
			return false, err
		}
		apex, err := s.apexID(ctx, tx)
		if err != nil {
			return false, err
		}
		if _, err = tx.ExecContext(ctx, "DELETE FROM sites WHERE id=?", id); err != nil {
			return false, err
		}
		if _, err = tx.ExecContext(ctx, "DELETE FROM settings WHERE key='apex' AND value=?", id); err != nil {
			return false, err
		}
		return apex == id, nil
	})
}

// Apex returns the currently configured apex site, if any.
func (s *Store) Apex(ctx context.Context) (Site, bool, error) {
	result, err := transact(ctx, s.db.Read, func(tx *sql.Tx) (apexResult, error) {
		id, err := s.apexID(ctx, tx)
		if err != nil {
			return apexResult{}, err
		}
		if id == "" {
			return apexResult{}, nil
		}
		x, err := s.byID(ctx, tx, id)
		return apexResult{x, err == nil}, err
	})
	return result.site, result.set, err
}

// SetApex sets the apex to a public site.
func (s *Store) SetApex(ctx context.Context, id string) (Site, error) {
	return transact(ctx, s.db.Write, func(tx *sql.Tx) (Site, error) {
		x, err := s.byID(ctx, tx, id)
		if err != nil {
			return Site{}, err
		}
		if x.Visibility != Public {
			return Site{}, ErrNotPublic
		}
		apex, err := s.apexID(ctx, tx)
		if err != nil {
			return Site{}, err
		}
		if apex != id {
			if _, err = tx.ExecContext(ctx, "INSERT INTO settings(key,value) VALUES('apex',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", id); err != nil {
				return Site{}, err
			}
		}
		return x, nil
	})
}

// ClearApex clears the apex without changing any site.
func (s *Store) ClearApex(ctx context.Context) error {
	_, err := transact(ctx, s.db.Write, func(tx *sql.Tx) (struct{}, error) {
		id, err := s.apexID(ctx, tx)
		if err != nil {
			return struct{}{}, err
		}
		if id == "" {
			return struct{}{}, nil
		}
		_, err = tx.ExecContext(ctx, "DELETE FROM settings WHERE key='apex'")
		return struct{}{}, err
	})
	return err
}
