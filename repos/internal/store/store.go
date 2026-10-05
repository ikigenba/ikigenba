// Package store keeps the repository catalog and its bare git directories.
package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/repos/internal/git"
	_ "modernc.org/sqlite" // SQLite is the catalog's database driver.
)

// Repository identifiers and the initial branch are shared with consumers.
const (
	IDPrefix      = "rep_"
	DefaultBranch = "main"
)

// Errors distinguish rule refusals from failures to reach storage.
var (
	ErrNotFound  = errors.New("repository not found")
	ErrNameTaken = errors.New("repository name taken")
	ErrDatabase  = errors.New("database unavailable")
	ErrRoot      = errors.New("repository root unavailable")
)

// Config supplies storage paths and the clock, randomness and host git.
type Config struct {
	Source string
	Root   string
	Git    *git.Git
	Now    func() time.Time
	Rand   io.Reader
}

// Repo is the persistent identity and last verified availability of a repository.
type Repo struct {
	ID, Name, Owner string
	Created         time.Time
	Available       bool
}

// Store owns one SQLite connection and serializes catalog/directory mutations.
type Store struct {
	mu       sync.Mutex
	gate     chan struct{}
	cfg      Config
	db       *sql.DB
	closed   bool
	excluded []string
}

type openError struct{ cause, kind error }

func (e openError) Error() string        { return e.cause.Error() }
func (e openError) Unwrap() error        { return e.cause }
func (e openError) Is(target error) bool { return target == e.kind }

// ValidID recognizes an id without accepting uppercase hexadecimal.
func ValidID(s string) bool {
	if len(s) != len(IDPrefix)+16 || !strings.HasPrefix(s, IDPrefix) {
		return false
	}
	for _, c := range []byte(s[len(IDPrefix):]) {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// ValidName recognizes the public repository name alphabet and length.
func ValidName(s string) bool {
	if len(s) < 1 || len(s) > 64 || s[0] == '-' {
		return false
	}
	for _, c := range []byte(s) {
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
			return false
		}
	}
	return true
}

// Open checks the database first, then the root, rebuilding only a new catalog.
func Open(ctx context.Context, cfg Config) (*Store, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Rand == nil {
		cfg.Rand = rand.Reader
	}
	memory := cfg.Source == "" || cfg.Source == ":memory:"
	fresh := memory
	source := ":memory:"
	if !memory {
		if err := os.MkdirAll(filepath.Dir(cfg.Source), 0700); err != nil {
			return nil, openError{err, ErrDatabase}
		}
		// Even an empty catalog needs a writable journal directory on this connection.
		if err := syscall.Access(filepath.Dir(cfg.Source), 2); err != nil {
			return nil, openError{err, ErrDatabase}
		}
		info, err := os.Stat(cfg.Source)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, openError{err, ErrDatabase}
		}
		fresh = errors.Is(err, os.ErrNotExist) || info != nil && info.Size() == 0
		source, err = filepath.Abs(cfg.Source)
		if err != nil {
			return nil, openError{err, ErrDatabase}
		}
		source = (&url.URL{Scheme: "file", Path: source}).String()
	}
	db, err := sql.Open("sqlite", source)
	if err != nil {
		return nil, openError{err, ErrDatabase}
	}
	db.SetMaxOpenConns(1)
	s := &Store{cfg: cfg, db: db, gate: make(chan struct{}, 1)}
	s.gate <- struct{}{}
	fail := func(err, kind error) (*Store, error) {
		_ = db.Close()
		if fresh && !memory {
			_ = os.Remove(cfg.Source)
		}
		return nil, openError{err, kind}
	}
	// Keep the database's journal mode, including WAL used by replication.
	if _, err = db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS repos (id TEXT PRIMARY KEY,name TEXT NOT NULL,owner TEXT NOT NULL,created TEXT NOT NULL,available INTEGER NOT NULL,UNIQUE(owner,name)); BEGIN IMMEDIATE; UPDATE repos SET available=available; COMMIT;`); err != nil {
		return fail(err, ErrDatabase)
	}
	if err = os.MkdirAll(cfg.Root, 0700); err != nil {
		return fail(err, ErrRoot)
	}
	if _, err = os.ReadDir(cfg.Root); err != nil {
		return fail(err, ErrRoot)
	}
	if fresh {
		if err = s.rebuild(ctx); err != nil {
			return fail(err, ErrDatabase)
		}
	}
	if err = ctx.Err(); err != nil {
		return fail(err, ErrDatabase)
	}
	return s, nil
}

// Close releases the catalog. Other methods refuse subsequent calls.
func (s *Store) Close() error {
	release, err := s.mutationLock(context.Background())
	if err != nil {
		return err
	}
	defer release()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return s.db.Close()
}

func (s *Store) ready(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.closed {
		return errors.New("store closed")
	}
	return nil
}

// Dir returns the stable filesystem path, without validating id.
func (s *Store) Dir(id string) string { return filepath.Join(s.cfg.Root, id+".git") }

const columns = "id,name,owner,created,available"

type scanner interface{ Scan(...any) error }

func scanRepo(row scanner) (Repo, error) {
	var r Repo
	var created string
	if err := row.Scan(&r.ID, &r.Name, &r.Owner, &created, &r.Available); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Repo{}, ErrNotFound
		}
		return Repo{}, err
	}
	var err error
	r.Created, err = time.Parse(time.RFC3339, created)
	return r, err
}
func (s *Store) byID(ctx context.Context, id string) (Repo, error) {
	return scanRepo(s.db.QueryRowContext(ctx, "SELECT "+columns+" FROM repos WHERE id=?", id))
}

// Find resolves only an owner's id or name, with the rep_ prefix choosing id.
func (s *Store) Find(ctx context.Context, owner, ref string) (Repo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx); err != nil {
		return Repo{}, err
	}
	key := "name"
	if strings.HasPrefix(ref, IDPrefix) {
		key = "id"
	}
	return scanRepo(s.db.QueryRowContext(ctx, "SELECT "+columns+" FROM repos WHERE owner=? AND "+key+"=?", owner, ref))
}

func (s *Store) rows(ctx context.Context, query string, args ...any) ([]Repo, error) {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	result := make([]Repo, 0)
	for rows.Next() {
		r, err := scanRepo(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	return result, rows.Err()
}

// List returns this owner's repositories in name order.
func (s *Store) List(ctx context.Context, owner string) ([]Repo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx); err != nil {
		return nil, err
	}
	return s.rows(ctx, "SELECT "+columns+" FROM repos WHERE owner=? ORDER BY name", owner)
}

// All returns every repository in id order.
func (s *Store) All(ctx context.Context) ([]Repo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx); err != nil {
		return nil, err
	}
	return s.rows(ctx, "SELECT "+columns+" FROM repos ORDER BY id")
}

// Verify records soundness without mutating repository files, reporting damage as events.
func (s *Store) Verify(ctx context.Context, w *telemetry.Writer) error {
	release, err := s.mutationLock(ctx)
	if err != nil {
		return err
	}
	defer release()
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx); err != nil {
		return err
	}
	repos, err := s.rows(ctx, "SELECT "+columns+" FROM repos ORDER BY id")
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	unavailable := append([]string(nil), s.excluded...)
	for i, r := range repos {
		sound := s.sound(ctx, r.ID)
		if err = ctx.Err(); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, "UPDATE repos SET available=? WHERE id=?", sound, r.ID); err != nil {
			return err
		}
		if !sound {
			unavailable = append(unavailable, r.ID)
		}
		repos[i].Available = sound
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	if c := s.scope(ctx); c != nil {
		if c.verified == nil {
			c.verified = make(map[string]bool)
		}
		for _, r := range repos {
			c.verified[r.ID] = r.Available
		}
	}
	sort.Strings(unavailable)
	for _, id := range unavailable {
		w.Emit(context.Background(), "repo.unavailable", telemetry.Attrs{"repo": id})
	}
	s.excluded = nil
	return nil
}

func (s *Store) sound(ctx context.Context, id string) bool {
	dir, err := filepath.Abs(s.Dir(id))
	if err != nil {
		return false
	}
	output, err := s.cfg.Git.Output(ctx, "/", "--git-dir="+dir, "rev-parse", "--is-bare-repository")
	if err != nil || string(output) != "true\n" {
		return false
	}
	for _, name := range []string{"objects", "refs"} {
		if _, err = os.ReadDir(filepath.Join(dir, name)); err != nil {
			return false
		}
	}
	return true
}

// Head reads exactly the default branch and treats newly damaged directories as empty.
func (s *Store) Head(ctx context.Context, id string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx); err != nil {
		return "", err
	}
	r, err := s.byID(ctx, id)
	if err != nil {
		return "", err
	}
	if !r.Available || !s.sound(ctx, id) {
		return "", ctx.Err()
	}
	dir, err := filepath.Abs(s.Dir(id))
	if err != nil {
		return "", err
	}
	output, err := s.cfg.Git.Output(ctx, "/", "--git-dir="+dir, "rev-parse", "--verify", "-q", "refs/heads/"+DefaultBranch)
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		var exit interface{ ExitCode() int }
		if errors.As(err, &exit) && exit.ExitCode() == 1 {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSuffix(string(output), "\n"), nil
}

// Size sums apparent lengths of readable regular files, without following links.
func (s *Store) Size(ctx context.Context, id string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ready(ctx); err != nil {
		return 0, err
	}
	if _, err := s.byID(ctx, id); err != nil {
		return 0, err
	}
	root, err := os.OpenRoot(s.cfg.Root)
	if err != nil {
		return 0, ctx.Err()
	}
	defer func() { _ = root.Close() }()
	var size int64
	err = filepath.WalkDir(s.Dir(id), func(path string, d os.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			return nil
		}
		if d.Type().IsRegular() {
			relative, err := filepath.Rel(s.cfg.Root, path)
			if err != nil {
				return nil
			}
			info, err := root.Lstat(relative)
			if err != nil {
				return nil
			}
			f, err := root.Open(relative)
			if err != nil {
				return nil
			}
			if err = f.Close(); err != nil {
				return nil
			}
			size += info.Size()
		}
		return nil
	})
	return size, err
}
