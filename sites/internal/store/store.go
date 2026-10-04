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
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	// Register the approved database/sql driver for catalog connections.
	_ "modernc.org/sqlite"
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
	ErrDatabase  = errors.New("database failure")
	ErrNotFound  = errors.New("site not found")
	ErrNameTaken = errors.New("name taken")
	ErrNotPublic = errors.New("site is not public")
)

// Config supplies catalog storage, time, and randomness.
type Config struct {
	Source string
	Now    func() time.Time
	Rand   io.Reader
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

// Store provides serialized access to one SQLite catalog.
type Store struct {
	mu     sync.Mutex
	db     *sql.DB
	now    func() time.Time
	rand   io.Reader
	closed bool
}

type databaseError struct{ cause error }

func (e databaseError) Error() string        { return strings.ReplaceAll(e.cause.Error(), "\n", " ") }
func (e databaseError) Unwrap() error        { return e.cause }
func (e databaseError) Is(target error) bool { return target == ErrDatabase }

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
	if len(s) < 1 || len(s) > 64 || s[0] == '-' || s == "about" || s == "mcp" {
		return false
	}
	for i := range len(s) {
		if (s[i] < 'a' || s[i] > 'z') && (s[i] < '0' || s[i] > '9') && s[i] != '-' {
			return false
		}
	}
	return true
}

// Open opens or initializes a catalog using cfg.
func Open(ctx context.Context, cfg Config) (*Store, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	source := cfg.Source
	file := source != "" && source != ":memory:"
	existed := false
	if file {
		parent := filepath.Dir(source)
		if err := os.MkdirAll(parent, 0700); err != nil {
			return nil, databaseError{err}
		}
		info, err := os.Stat(source)
		if err != nil && !os.IsNotExist(err) {
			return nil, databaseError{err}
		}
		existed = err == nil
		if existed && info.Size() > 0 {
			f, e := os.Open(filepath.Clean(source))
			if e != nil {
				return nil, databaseError{e}
			}
			head := make([]byte, 16)
			_, e = io.ReadFull(f, head)
			ce := f.Close()
			if e != nil || string(head) != "SQLite format 3\x00" {
				return nil, databaseError{errors.New("file is not a database")}
			}
			if ce != nil {
				return nil, databaseError{ce}
			}
		}
		probe, err := os.CreateTemp(parent, ".sites-write-")
		if err != nil {
			return nil, databaseError{err}
		}
		name := probe.Name()
		err = probe.Close()
		removeErr := os.Remove(name)
		if err != nil {
			return nil, databaseError{err}
		}
		if removeErr != nil {
			return nil, databaseError{removeErr}
		}
		f, err := os.OpenFile(filepath.Clean(source), os.O_RDWR|os.O_CREATE, 0600)
		if err != nil {
			return nil, databaseError{err}
		}
		if err = f.Close(); err != nil {
			return nil, databaseError{err}
		}
		absolute, err := filepath.Abs(source)
		if err != nil {
			return nil, databaseError{err}
		}
		source = (&url.URL{Scheme: "file", Path: absolute}).String()
	} else {
		source = ":memory:"
	}
	db, err := sql.Open("sqlite", source)
	if err != nil {
		return nil, databaseError{err}
	}
	db.SetMaxOpenConns(1)
	fail := func(e error) (*Store, error) {
		_ = db.Close()
		if file && !existed {
			_ = os.Remove(cfg.Source)
		}
		return nil, databaseError{e}
	}
	if err = db.PingContext(ctx); err != nil {
		return fail(err)
	}
	if file {
		if _, err = db.ExecContext(ctx, "PRAGMA journal_mode=WAL"); err != nil {
			return fail(err)
		}
	}
	if _, err = db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS sites (id TEXT PRIMARY KEY, name TEXT NOT NULL, slug TEXT NOT NULL, owner TEXT NOT NULL, payload TEXT NOT NULL); CREATE TABLE IF NOT EXISTS settings (key TEXT PRIMARY KEY, value TEXT NOT NULL)`); err != nil {
		return fail(err)
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Rand == nil {
		cfg.Rand = rand.Reader
	}
	return &Store{db: db, now: cfg.Now, rand: cfg.Rand}, nil
}

// Close closes the catalog and makes subsequent operations fail.
func (s *Store) Close() error { s.mu.Lock(); defer s.mu.Unlock(); s.closed = true; return s.db.Close() }
func (s *Store) ready(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.closed {
		return ErrDatabase
	}
	return nil
}
func (s *Store) all(ctx context.Context) ([]Site, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT payload FROM sites ORDER BY name COLLATE BINARY")
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
		out = append(out, x)
	}
	return out, rows.Err()
}
func (s *Store) byID(ctx context.Context, id string) (Site, error) {
	var payload string
	err := s.db.QueryRowContext(ctx, "SELECT payload FROM sites WHERE id=?", id).Scan(&payload)
	if errors.Is(err, sql.ErrNoRows) {
		return Site{}, ErrNotFound
	}
	if err != nil {
		return Site{}, err
	}
	var x Site
	err = json.Unmarshal([]byte(payload), &x)
	return x, err
}
func (s *Store) save(ctx context.Context, x Site) error {
	b, err := json.Marshal(x)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, "INSERT INTO sites(id,name,slug,owner,payload) VALUES(?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET payload=excluded.payload", x.ID, x.Name, x.Slug, x.Owner, string(b))
	return err
}
func (s *Store) apexID(ctx context.Context) (string, error) {
	var id string
	err := s.db.QueryRowContext(ctx, "SELECT value FROM settings WHERE key='apex'").Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return id, err
}
func (s *Store) selectOne(ctx context.Context, match func(Site) bool) (Site, error) {
	if err := s.ready(ctx); err != nil {
		return Site{}, err
	}
	xs, err := s.all(ctx)
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
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.selectOne(ctx, func(x Site) bool { return x.Owner == owner && x.Name == name })
}

// BySlug finds a site by its exact serving slug.
func (s *Store) BySlug(ctx context.Context, slug string) (Site, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.selectOne(ctx, func(x Site) bool { return x.Slug == slug })
}
func (s *Store) list(ctx context.Context, match func(Site) bool) ([]Site, error) {
	if err := s.ready(ctx); err != nil {
		return nil, err
	}
	xs, err := s.all(ctx)
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
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.list(ctx, func(x Site) bool { return x.Owner == owner })
}

// Visible lists all listed sites and the user's own unlisted sites.
func (s *Store) Visible(ctx context.Context, user string) ([]Site, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.list(ctx, func(x Site) bool { return x.Listed || x.Owner == user })
}
func (s *Store) taken(ctx context.Context, name string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, "SELECT count(*) FROM sites WHERE name=? OR slug=?", name, name).Scan(&n)
	return n > 0, err
}

// Taken reports whether a name or slug is already reserved.
func (s *Store) Taken(ctx context.Context, name string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx); err != nil {
		return false, err
	}
	return s.taken(ctx, name)
}

// Create creates a site with a unique ID and serving slug.
func (s *Store) Create(ctx context.Context, d Draft) (Site, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx); err != nil {
		return Site{}, err
	}
	if !ValidName(d.Name) || d.Owner == "" || d.Ref == "" || d.Visibility != Public && d.Visibility != Private {
		return Site{}, errors.New("invalid draft")
	}
	taken, err := s.taken(ctx, d.Name)
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
		_, err = s.byID(ctx, id)
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
			taken, err = s.taken(ctx, slug)
			if err != nil {
				return Site{}, err
			}
			if !taken {
				break
			}
		}
	}
	x := Site{ID: id, Name: d.Name, Slug: slug, Owner: d.Owner, Repo: d.Repo, Ref: d.Ref, Visibility: d.Visibility, Listed: d.Listed, Created: s.now().UTC().Truncate(time.Second)}
	if err = s.save(ctx, x); err != nil {
		return Site{}, err
	}
	return x, nil
}

// Publish atomically replaces the published commit and timestamp.
func (s *Store) Publish(ctx context.Context, id, commit string) (Site, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx); err != nil {
		return Site{}, err
	}
	if !validHex(commit, 40) {
		return Site{}, errors.New("invalid commit")
	}
	x, err := s.byID(ctx, id)
	if err != nil {
		return Site{}, err
	}
	x.Commit = commit
	x.Published = s.now().UTC().Truncate(time.Second)
	if err = s.save(ctx, x); err != nil {
		return Site{}, err
	}
	return x, nil
}

// Update atomically changes visibility, listing, and tracking ref.
func (s *Store) Update(ctx context.Context, id string, c Change) (Site, []string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx); err != nil {
		return Site{}, nil, err
	}
	if c.Visibility != nil && *c.Visibility != Public && *c.Visibility != Private || c.Ref != nil && *c.Ref == "" {
		return Site{}, nil, errors.New("invalid change")
	}
	x, err := s.byID(ctx, id)
	if err != nil {
		return Site{}, nil, err
	}
	apex, err := s.apexID(ctx)
	if err != nil {
		return Site{}, nil, err
	}
	if apex == id && c.Visibility != nil && *c.Visibility == Private {
		return Site{}, nil, ErrNotPublic
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
		if err = s.save(ctx, x); err != nil {
			return Site{}, nil, err
		}
	}
	return x, changed, nil
}

// Delete deletes a site and clears its apex setting if present.
func (s *Store) Delete(ctx context.Context, id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx); err != nil {
		return false, err
	}
	if _, err := s.byID(ctx, id); err != nil {
		return false, err
	}
	apex, err := s.apexID(ctx)
	if err != nil {
		return false, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, "DELETE FROM sites WHERE id=?", id); err != nil {
		return false, err
	}
	if _, err = tx.ExecContext(ctx, "DELETE FROM settings WHERE key='apex' AND value=?", id); err != nil {
		return false, err
	}
	if err = tx.Commit(); err != nil {
		return false, err
	}
	return apex == id, nil
}

// Apex returns the currently configured apex site, if any.
func (s *Store) Apex(ctx context.Context) (Site, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx); err != nil {
		return Site{}, false, err
	}
	id, err := s.apexID(ctx)
	if err != nil {
		return Site{}, false, err
	}
	if id == "" {
		return Site{}, false, nil
	}
	x, err := s.byID(ctx, id)
	return x, err == nil, err
}

// SetApex sets the apex to a public site.
func (s *Store) SetApex(ctx context.Context, id string) (Site, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx); err != nil {
		return Site{}, err
	}
	x, err := s.byID(ctx, id)
	if err != nil {
		return Site{}, err
	}
	if x.Visibility != Public {
		return Site{}, ErrNotPublic
	}
	apex, err := s.apexID(ctx)
	if err != nil {
		return Site{}, err
	}
	if apex != id {
		if _, err = s.db.ExecContext(ctx, "INSERT INTO settings(key,value) VALUES('apex',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value", id); err != nil {
			return Site{}, err
		}
	}
	return x, nil
}

// ClearApex clears the apex without changing any site.
func (s *Store) ClearApex(ctx context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx); err != nil {
		return err
	}
	id, err := s.apexID(ctx)
	if err != nil {
		return err
	}
	if id == "" {
		return nil
	}
	_, err = s.db.ExecContext(ctx, "DELETE FROM settings WHERE key='apex'")
	return err
}
