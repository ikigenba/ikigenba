// Package store keeps the repository catalog and its bare git directories.
package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/repos/internal/git"
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
	ErrRoot      = errors.New("repository root unavailable")
)

// Config supplies storage paths and the clock, randomness and host git.
type Config struct {
	Root string
	Git  *git.Git
	Now  func() time.Time
	Rand io.Reader
}

// Repo is the persistent identity and last verified availability of a repository.
type Repo struct {
	ID, Name, Owner string
	Created         time.Time
	Available       bool
}

// Store reaches its catalog through the supplied appkit handle.
type Store struct {
	cfg      Config
	db       *db.DB
	excluded []string
	reported atomic.Bool
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

// Open creates the repository root and settles an unfinished catalog.
func Open(ctx context.Context, d *db.DB, cfg Config) (*Store, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Rand == nil {
		cfg.Rand = rand.Reader
	}
	s := &Store{cfg: cfg, db: d}
	err := d.Write(ctx, func(tx *sql.Tx) error {
		if err := os.MkdirAll(cfg.Root, 0700); err != nil {
			return openError{err, ErrRoot}
		}
		if _, err := os.ReadDir(cfg.Root); err != nil {
			return openError{err, ErrRoot}
		}
		var marked, count int
		if err := tx.QueryRowContext(ctx, "PRAGMA user_version").Scan(&marked); err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM repos").Scan(&count); err != nil {
			return err
		}
		if marked == 0 && count == 0 {
			if err := s.rebuild(ctx, tx); err != nil {
				return err
			}
		}
		_, err := tx.ExecContext(ctx, "PRAGMA user_version=1")
		return err
	})
	if err != nil {
		return nil, err
	}
	return s, nil
}

// transact uses the handle, or the transaction owned by a coordinated mutation.
func transact[T any](ctx context.Context, s *Store, write bool, fn func(*sql.Tx) (T, error)) (T, error) {
	var result T
	var err error
	invoke := func(tx *sql.Tx) error { result, err = fn(tx); return err }
	if c := s.scope(ctx); c != nil {
		// A scoped transaction still honors subsequent handle failure switches.
		err = s.db.Read(ctx, func(*sql.Tx) error { return nil })
		if err == nil {
			err = invoke(c.tx)
		}
	} else if write {
		err = s.db.Write(ctx, invoke)
	} else {
		err = s.db.Read(ctx, invoke)
	}
	if err != nil {
		var zero T
		return zero, err
	}
	return result, nil
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
func (s *Store) byID(ctx context.Context, tx *sql.Tx, id string) (Repo, error) {
	return scanRepo(tx.QueryRowContext(ctx, "SELECT "+columns+" FROM repos WHERE id=?", id))
}

// Find resolves only an owner's id or name, with the rep_ prefix choosing id.
func (s *Store) Find(ctx context.Context, owner, ref string) (Repo, error) {
	return transact(ctx, s, false, func(tx *sql.Tx) (Repo, error) {
		key := "name"
		if strings.HasPrefix(ref, IDPrefix) {
			key = "id"
		}
		return scanRepo(tx.QueryRowContext(ctx, "SELECT "+columns+" FROM repos WHERE owner=? AND "+key+"=?", owner, ref))
	})
}

func (s *Store) rows(ctx context.Context, tx *sql.Tx, query string, args ...any) ([]Repo, error) {
	rows, err := tx.QueryContext(ctx, query, args...)
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
	return transact(ctx, s, false, func(tx *sql.Tx) ([]Repo, error) {
		return s.rows(ctx, tx, "SELECT "+columns+" FROM repos WHERE owner=? ORDER BY name", owner)
	})
}

// All returns every repository in id order.
func (s *Store) All(ctx context.Context) ([]Repo, error) {
	return transact(ctx, s, false, func(tx *sql.Tx) ([]Repo, error) {
		return s.rows(ctx, tx, "SELECT "+columns+" FROM repos ORDER BY id")
	})
}

// Verify records soundness without mutating repository files, reporting damage as events.
func (s *Store) Verify(ctx context.Context, w *telemetry.Writer) error {
	var unavailable []string
	_, err := transact(ctx, s, true, func(tx *sql.Tx) (struct{}, error) {
		repos, err := s.rows(ctx, tx, "SELECT "+columns+" FROM repos ORDER BY id")
		if err != nil {
			return struct{}{}, err
		}
		for _, r := range repos {
			sound := s.sound(ctx, r.ID)
			if err = ctx.Err(); err != nil {
				return struct{}{}, err
			}
			if _, err = tx.ExecContext(ctx, "UPDATE repos SET available=? WHERE id=?", sound, r.ID); err != nil {
				return struct{}{}, err
			}
			if !sound {
				unavailable = append(unavailable, r.ID)
			}
		}
		return struct{}{}, nil
	})
	if err != nil {
		return err
	}
	emit := func() {
		if s.reported.CompareAndSwap(false, true) {
			unavailable = append(unavailable, s.excluded...)
		}
		sort.Strings(unavailable)
		for _, id := range unavailable {
			w.Emit(context.Background(), "repo.unavailable", telemetry.Attrs{"repo": id})
		}
	}
	if c := s.scope(ctx); c != nil {
		c.events = append(c.events, emit)
	} else {
		emit()
	}
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
	return transact(ctx, s, false, func(tx *sql.Tx) (string, error) {
		r, err := s.byID(ctx, tx, id)
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
	})
}

// Size sums apparent lengths of readable regular files, without following links.
func (s *Store) Size(ctx context.Context, id string) (int64, error) {
	return transact(ctx, s, false, func(tx *sql.Tx) (int64, error) {
		if _, err := s.byID(ctx, tx, id); err != nil {
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
	})
}
