// Package store keeps artifact metadata and immutable blob files.
package store

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
)

// Identifier grammar, body defaults, and shared unavailable copy.
const (
	IDPrefix                  = "art_"
	DefaultContentType        = "application/octet-stream"
	MaxSegment                = 128
	MaxPath                   = 1024
	Unreachable        string = "cannot reach artifact storage; try again later"
)

// Errors distinguish call refusals from inaccessible storage.
var (
	ErrNotFound    = errors.New("artifact not found")
	ErrInvalid     = errors.New("invalid artifact")
	ErrPastExpiry  = errors.New("artifact expiry has passed")
	ErrForbidden   = errors.New("artifact belongs to another user")
	ErrTooLarge    = errors.New("artifact exceeds byte limit")
	ErrBody        = errors.New("cannot read artifact body")
	ErrUnreachable = errors.New(Unreachable)
)

// Config supplies time and artifact id randomness.
type Config struct {
	Now  func() time.Time
	Rand io.Reader
}

// Artifact is the persisted public record.
type Artifact struct {
	ID, Path                                 string
	Bytes                                    int64
	ContentType, SHA256, OwnerID, OwnerEmail string
	Created, Modified, Expires               time.Time
}

// Draft supplies a put's caller-controlled metadata and cap.
type Draft struct {
	Path, ContentType, OwnerID, OwnerEmail string
	Expires                                time.Time
	Max                                    int64
}

// Store coordinates the database and immutable blobs.
type Store struct {
	d     *db.DB
	blobs string
	cfg   Config
	mu    sync.Mutex
}

// New opens and creates nothing.
func New(d *db.DB, blobs string, cfg Config) *Store {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Rand == nil {
		cfg.Rand = rand.Reader
	}
	return &Store{d: d, blobs: blobs, cfg: cfg}
}

// ValidID checks the artifact identifier grammar.
func ValidID(s string) bool {
	if len(s) != 20 || !strings.HasPrefix(s, IDPrefix) {
		return false
	}
	for _, b := range []byte(s[4:]) {
		if (b < '0' || b > '9') && (b < 'a' || b > 'f') {
			return false
		}
	}
	return true
}

// ValidPath checks the artifact path grammar.
func ValidPath(s string) bool {
	if len(s) == 0 || len(s) > MaxPath {
		return false
	}
	for _, p := range strings.Split(s, "/") {
		if len(p) == 0 || len(p) > MaxSegment || p == "." || p == ".." {
			return false
		}
		for _, b := range []byte(p) {
			if (b < 'a' || b > 'z') && (b < 'A' || b > 'Z') && (b < '0' || b > '9') && b != '.' && b != '-' && b != '_' {
				return false
			}
		}
	}
	return true
}

const columns = "id,path,blob,bytes,content_type,sha256,owner_id,owner_email,created,modified,expires"

type record struct {
	Artifact
	blob string
}
type scanner interface{ Scan(...any) error }

func scan(r scanner) (record, error) {
	var a record
	var c, m string
	var e sql.NullString
	err := r.Scan(&a.ID, &a.Path, &a.blob, &a.Bytes, &a.ContentType, &a.SHA256, &a.OwnerID, &a.OwnerEmail, &c, &m, &e)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			err = ErrNotFound
		}
		return a, err
	}
	a.Created, err = time.Parse(time.RFC3339, c)
	if err != nil {
		return a, err
	}
	a.Modified, err = time.Parse(time.RFC3339, m)
	if err != nil {
		return a, err
	}
	if e.Valid {
		a.Expires, err = time.Parse(time.RFC3339, e.String)
	}
	a.Created = a.Created.UTC()
	a.Modified = a.Modified.UTC()
	a.Expires = a.Expires.UTC()
	return a, err
}
func classify(err error) error {
	if err == nil {
		return nil
	}
	for _, e := range []error{ErrNotFound, ErrInvalid, ErrPastExpiry, ErrForbidden, ErrTooLarge, ErrBody, ErrUnreachable} {
		if errors.Is(err, e) {
			return err
		}
	}
	return errors.Join(ErrUnreachable, err)
}
func (s *Store) tx(ctx context.Context, write bool, fn func(*sql.Tx) error) error {
	if err := ctx.Err(); err != nil {
		return classify(err)
	}
	if write {
		return classify(s.d.Write(ctx, fn))
	}
	return classify(s.d.Read(ctx, fn))
}
func lookup(ctx context.Context, tx *sql.Tx, key, value string) (record, error) {
	return scan(tx.QueryRowContext(ctx, "SELECT "+columns+" FROM artifacts WHERE "+key+" = ?", value))
}
func all(ctx context.Context, tx *sql.Tx) ([]record, error) {
	out := make([]record, 0)
	rows, err := tx.QueryContext(ctx, "SELECT "+columns+" FROM artifacts ORDER BY path COLLATE BINARY")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		a, e := scan(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
func (s *Store) get(ctx context.Context, key, value string) (Artifact, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var a record
	err := s.tx(ctx, false, func(tx *sql.Tx) error {
		if key == "path" && !ValidPath(value) {
			return ErrInvalid
		}
		var e error
		a, e = lookup(ctx, tx, key, value)
		return e
	})
	return a.Artifact, err
}

// Get finds an exact path, regardless of owner.
func (s *Store) Get(ctx context.Context, path string) (Artifact, error) {
	return s.get(ctx, "path", path)
}

// GetID finds an exact id.
func (s *Store) GetID(ctx context.Context, id string) (Artifact, error) { return s.get(ctx, "id", id) }

// List returns every record in path order.
func (s *Store) List(ctx context.Context) ([]Artifact, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Artifact, 0)
	err := s.tx(ctx, false, func(tx *sql.Tx) error {
		aa, e := all(ctx, tx)
		for _, a := range aa {
			out = append(out, a.Artifact)
		}
		return e
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Open returns the record and an immutable file positioned at its start.
func (s *Store) Open(ctx context.Context, path string) (Artifact, *os.File, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var a record
	var f *os.File
	err := s.tx(ctx, false, func(tx *sql.Tx) error {
		if !ValidPath(path) {
			return ErrInvalid
		}
		var e error
		a, e = lookup(ctx, tx, "path", path)
		if e != nil {
			return e
		}
		f, e = os.Open(filepath.Join(s.blobs, a.blob))
		if e != nil {
			return e
		}
		info, e := f.Stat()
		if e != nil {
			return e
		}
		if !info.Mode().IsRegular() {
			return ErrUnreachable
		}
		return nil
	})
	if err != nil {
		if f != nil {
			_ = f.Close()
		}
		return Artifact{}, nil, err
	}
	return a.Artifact, f, nil
}
func stamp(t time.Time) time.Time { return t.UTC().Truncate(time.Second) }
func expiry(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.Format(time.RFC3339)
}
func (s *Store) draw(ctx context.Context, tx *sql.Tx) (string, error) {
	for {
		var b [8]byte
		if _, err := io.ReadFull(s.cfg.Rand, b[:]); err != nil {
			return "", err
		}
		id := IDPrefix + hex.EncodeToString(b[:])
		_, err := lookup(ctx, tx, "id", id)
		if errors.Is(err, ErrNotFound) {
			return id, nil
		}
		if err != nil {
			return "", err
		}
	}
}
func copyBody(ctx context.Context, f *os.File, body io.Reader, capBytes int64) (int64, string, error) {
	h := sha256.New()
	var n int64
	buf := make([]byte, 32768)
	for {
		size := len(buf)
		if capBytes != math.MaxInt64 && int64(size) > capBytes-n+1 {
			size = int(capBytes - n + 1)
		}
		k, e := body.Read(buf[:size])
		if k > 0 {
			n += int64(k)
			if n > capBytes {
				return 0, "", ErrTooLarge
			}
			if _, err := f.Write(buf[:k]); err != nil {
				return 0, "", err
			}
			_, _ = h.Write(buf[:k])
		}
		if e != nil {
			if !errors.Is(e, io.EOF) {
				return 0, "", errors.Join(ErrBody, e)
			}
			if err := ctx.Err(); err != nil {
				return 0, "", err
			}
			return n, hex.EncodeToString(h.Sum(nil)), nil
		}
		if err := ctx.Err(); err != nil {
			return 0, "", err
		}
	}
}

// Put streams and publishes a body atomically with its metadata.
func (s *Store) Put(ctx context.Context, d Draft, body io.Reader) (Artifact, bool, error) {
	now := stamp(s.cfg.Now())
	s.mu.Lock()
	err := s.tx(ctx, false, func(tx *sql.Tx) error {
		if !ValidPath(d.Path) || d.OwnerID == "" || d.Max < 0 {
			return ErrInvalid
		}
		if !d.Expires.IsZero() && !stamp(d.Expires).After(now) {
			return ErrPastExpiry
		}
		a, e := lookup(ctx, tx, "path", d.Path)
		if e != nil && !errors.Is(e, ErrNotFound) {
			return e
		}
		if e == nil && a.OwnerID != d.OwnerID {
			return ErrForbidden
		}
		return nil
	})
	s.mu.Unlock()
	if err != nil {
		return Artifact{}, false, err
	}
	f, err := os.CreateTemp(s.blobs, "tmp-")
	if err != nil {
		return Artifact{}, false, classify(err)
	}
	name := f.Name()
	defer func() { _ = f.Close(); _ = os.Remove(name) }()
	n, hash, err := copyBody(ctx, f, body, d.Max)
	if err != nil {
		return Artifact{}, false, classify(err)
	}
	if err = f.Close(); err != nil {
		return Artifact{}, false, classify(err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var out Artifact
	var created bool
	var old string
	err = s.tx(ctx, true, func(tx *sql.Tx) error {
		a, e := lookup(ctx, tx, "path", d.Path)
		if e != nil && !errors.Is(e, ErrNotFound) {
			return e
		}
		created = errors.Is(e, ErrNotFound)
		if !created && a.OwnerID != d.OwnerID {
			return ErrForbidden
		}
		out = a.Artifact
		if created {
			id, e := s.draw(ctx, tx)
			if e != nil {
				return e
			}
			out = Artifact{ID: id, Path: d.Path, OwnerID: d.OwnerID, OwnerEmail: d.OwnerEmail, Created: now}
		}
		out.Bytes = n
		out.SHA256 = hash
		out.ContentType = d.ContentType
		if out.ContentType == "" {
			out.ContentType = DefaultContentType
		}
		out.Modified = now
		out.Expires = stamp(d.Expires)
		var b [8]byte
		if _, e = io.ReadFull(rand.Reader, b[:]); e != nil {
			return e
		}
		final := filepath.Join(s.blobs, hex.EncodeToString(b[:]))
		if e = os.Rename(name, final); e != nil {
			return e
		}
		name = final
		old = a.blob
		_, e = tx.ExecContext(ctx, "INSERT INTO artifacts ("+columns+") VALUES (?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(path) DO UPDATE SET blob=excluded.blob,bytes=excluded.bytes,content_type=excluded.content_type,sha256=excluded.sha256,modified=excluded.modified,expires=excluded.expires", out.ID, out.Path, filepath.Base(final), out.Bytes, out.ContentType, out.SHA256, out.OwnerID, out.OwnerEmail, out.Created.Format(time.RFC3339), out.Modified.Format(time.RFC3339), expiry(out.Expires))
		return e
	})
	if err != nil {
		return Artifact{}, false, err
	}
	name = ""
	if old != "" {
		_ = os.Remove(filepath.Join(s.blobs, old))
	}
	return out, created, nil
}

// Delete removes an artifact only for its owner.
func (s *Store) Delete(ctx context.Context, path, userID string) (Artifact, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var a record
	err := s.tx(ctx, true, func(tx *sql.Tx) error {
		if !ValidPath(path) {
			return ErrInvalid
		}
		var e error
		a, e = lookup(ctx, tx, "path", path)
		if e != nil {
			return e
		}
		if a.OwnerID != userID {
			return ErrForbidden
		}
		if _, e = os.ReadDir(s.blobs); e != nil {
			return e
		}
		if _, e = os.Stat(filepath.Join(s.blobs, a.blob)); e != nil {
			return e
		}
		_, e = tx.ExecContext(ctx, "DELETE FROM artifacts WHERE id=?", a.ID)
		return e
	})
	if err != nil {
		return Artifact{}, err
	}
	_ = os.Remove(filepath.Join(s.blobs, a.blob))
	return a.Artifact, nil
}

// Sweep removes expired records in path order.
func (s *Store) Sweep(ctx context.Context, now time.Time) ([]Artifact, error) {
	return s.prune(ctx, func(a record) bool { return !a.Expires.IsZero() && !a.Expires.After(stamp(now)) }, false)
}

// Reconcile removes crash leftovers and records without regular blob files.
func (s *Store) Reconcile(ctx context.Context) ([]Artifact, error) {
	return s.prune(ctx, func(a record) bool {
		i, e := os.Lstat(filepath.Join(s.blobs, a.blob))
		return e != nil || !i.Mode().IsRegular()
	}, true)
}
func (s *Store) prune(ctx context.Context, drop func(record) bool, reconcile bool) ([]Artifact, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Artifact, 0)
	var removed []record
	var kept = map[string]bool{}
	err := s.tx(ctx, true, func(tx *sql.Tx) error {
		aa, e := all(ctx, tx)
		if e != nil {
			return e
		}
		if _, e = os.ReadDir(s.blobs); e != nil {
			return e
		}
		for _, a := range aa {
			if !drop(a) {
				kept[a.blob] = true
				continue
			}
			if _, e = tx.ExecContext(ctx, "DELETE FROM artifacts WHERE id=?", a.ID); e != nil {
				return e
			}
			removed = append(removed, a)
			out = append(out, a.Artifact)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	for _, a := range removed {
		_ = os.RemoveAll(filepath.Join(s.blobs, a.blob))
	}
	if reconcile {
		entries, e := os.ReadDir(s.blobs)
		if e != nil {
			return nil, classify(e)
		}
		for _, entry := range entries {
			if !kept[entry.Name()] {
				if e = os.RemoveAll(filepath.Join(s.blobs, entry.Name())); e != nil {
					return nil, classify(e)
				}
			}
		}
	}
	return out, nil
}
