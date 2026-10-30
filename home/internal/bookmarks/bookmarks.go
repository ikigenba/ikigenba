// Package bookmarks stores each user's ordered links.
package bookmarks

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"io"
	"math"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/ikigenba/ikigenba/appkit/db"
)

// Limits and messages define bookmark validation and display answers.
const (
	IDPrefix                   = "bmk_"
	MaxNameRunes               = 40
	MaxURLBytes                = 2048
	NameRequiredMessage string = "Enter a name."
	NameTooLongMessage  string = "Use a name of at most 40 characters."
	NameTakenMessage    string = "That name is already in use."
	URLInvalidMessage   string = "Enter an absolute http or https URL."
	URLTooLongMessage   string = "Use a URL of at most 2048 bytes."
	Unreachable         string = "The bookmarks cannot be reached."
)

// Sentinel errors distinguish refused and missing bookmarks.
var (
	ErrNotFound  = errors.New("bookmark not found")
	ErrNameTaken = errors.New("bookmark name taken")
	ErrInvalid   = errors.New("invalid bookmark")
)

// Config supplies the created-time clock and id entropy.
type Config struct {
	Now  func() time.Time
	Rand io.Reader
}

// Bookmark is a stored link, without its icon bytes.
type Bookmark struct {
	ID, Name, URL, CreatedBy string
	HasIcon                  bool
	Created                  time.Time
}

// Icon holds an image and its media type.
type Icon struct {
	Type string
	Data []byte
}

// Draft holds a bookmark to add.
type Draft struct {
	Name, URL, CreatedBy string
	Icon                 Icon
}

// Submission holds the raw form fields.
type Submission struct{ Name, URL string }

// FieldErrors holds one validation message per field.
type FieldErrors struct{ Name, URL string }

// Any reports whether either field was refused.
func (e FieldErrors) Any() bool { return e.Name != "" || e.URL != "" }

// Validate judges each trimmed field independently.
func Validate(s Submission) FieldErrors {
	var e FieldErrors
	name, rawURL := strings.TrimSpace(s.Name), strings.TrimSpace(s.URL)
	if name == "" {
		e.Name = NameRequiredMessage
	} else if utf8.RuneCountInString(name) > MaxNameRunes {
		e.Name = NameTooLongMessage
	}
	if len(rawURL) > MaxURLBytes {
		e.URL = URLTooLongMessage
	} else {
		u, err := url.Parse(rawURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			e.URL = URLInvalidMessage
		}
	}
	return e
}

// Store borrows a database handle; its owner closes that handle.
type Store struct {
	d    *db.DB
	now  func() time.Time
	rand io.Reader
	mu   sync.Mutex
}

// New builds a store over an already opened handle.
func New(d *db.DB, cfg Config) *Store {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Rand == nil {
		cfg.Rand = rand.Reader
	}
	return &Store{d: d, now: cfg.Now, rand: cfg.Rand}
}

// List returns the user's links in their stored order.
func (s *Store) List(ctx context.Context, user string) ([]Bookmark, error) {
	result := make([]Bookmark, 0)
	err := s.d.Read(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, "SELECT id, name, url, created_by, length(icon) > 0, created_at FROM bookmarks WHERE created_by = ? ORDER BY position, id", user)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var b Bookmark
			var icon sql.NullBool
			var created string
			if err := rows.Scan(&b.ID, &b.Name, &b.URL, &b.CreatedBy, &icon, &created); err != nil {
				return err
			}
			b.HasIcon = icon.Bool
			b.Created, err = time.Parse(time.RFC3339, created)
			if err != nil {
				return err
			}
			b.Created = b.Created.UTC()
			result = append(result, b)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// Create validates and adds one link atomically with the uniqueness check.
func (s *Store) Create(ctx context.Context, d Draft) (Bookmark, error) {
	if Validate(Submission{d.Name, d.URL}).Any() {
		return Bookmark{}, ErrInvalid
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var b Bookmark
	err := s.d.Write(ctx, func(tx *sql.Tx) error {
		name, rawURL := strings.TrimSpace(d.Name), strings.TrimSpace(d.URL)
		var count int
		if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM bookmarks WHERE created_by = ? AND name = ? COLLATE NOCASE", d.CreatedBy, name).Scan(&count); err != nil {
			return err
		}
		if count != 0 {
			return ErrNameTaken
		}
		var id string
		for {
			var data [8]byte
			if _, err := io.ReadFull(s.rand, data[:]); err != nil {
				return err
			}
			id = IDPrefix + hex.EncodeToString(data[:])
			if err := tx.QueryRowContext(ctx, "SELECT count(*) FROM bookmarks WHERE id = ?", id).Scan(&count); err != nil {
				return err
			}
			if count == 0 {
				break
			}
		}
		b = Bookmark{id, name, rawURL, d.CreatedBy, len(d.Icon.Data) > 0, s.now().UTC().Truncate(time.Second)}
		var icon, typ any
		if b.HasIcon {
			icon = d.Icon.Data
			typ = d.Icon.Type
		}
		var position int64
		if err := tx.QueryRowContext(ctx, "SELECT coalesce(max(position),0) FROM bookmarks WHERE created_by = ?", b.CreatedBy).Scan(&position); err != nil {
			return err
		}
		if position == math.MaxInt64 {
			// Reclaim integer space without changing the owner's visible order.
			if _, err := tx.ExecContext(ctx, "WITH ordered AS MATERIALIZED (SELECT id, row_number() OVER (ORDER BY position, id) AS position FROM bookmarks WHERE created_by = ?) UPDATE bookmarks SET position = (SELECT position FROM ordered WHERE ordered.id = bookmarks.id) WHERE created_by = ?", b.CreatedBy, b.CreatedBy); err != nil {
				return err
			}
			if err := tx.QueryRowContext(ctx, "SELECT max(position) FROM bookmarks WHERE created_by = ?", b.CreatedBy).Scan(&position); err != nil {
				return err
			}
		}
		_, err := tx.ExecContext(ctx, "INSERT INTO bookmarks (id,name,url,icon,icon_type,created_by,created_at,position) VALUES (?,?,?,?,?,?,?,?)", b.ID, b.Name, b.URL, icon, typ, b.CreatedBy, b.Created.Format(time.RFC3339), position+1)
		return err
	})
	if err != nil {
		return Bookmark{}, err
	}
	return b, nil
}

// Icon returns a stored image or ErrNotFound.
func (s *Store) Icon(ctx context.Context, user, id string) (Icon, error) {
	var ic Icon
	err := s.d.Read(ctx, func(tx *sql.Tx) error {
		var typ sql.NullString
		err := tx.QueryRowContext(ctx, "SELECT icon,icon_type FROM bookmarks WHERE created_by = ? AND id = ?", user, id).Scan(&ic.Data, &typ)
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if len(ic.Data) == 0 {
			return ErrNotFound
		}
		ic.Type = typ.String
		return nil
	})
	if err != nil {
		return Icon{}, err
	}
	return ic, nil
}

// Delete removes a link and reports whether it existed.
func (s *Store) Delete(ctx context.Context, user, id string) (bool, error) {
	var removed bool
	err := s.d.Write(ctx, func(tx *sql.Tx) error {
		r, err := tx.ExecContext(ctx, "DELETE FROM bookmarks WHERE created_by = ? AND id = ?", user, id)
		if err != nil {
			return err
		}
		n, err := r.RowsAffected()
		removed = n > 0
		return err
	})
	if err != nil {
		return false, err
	}
	return removed, nil
}

// Direction names the neighbour a bookmark moves toward.
type Direction string

// Left and Right move toward the start and end of the owner's list.
const (
	Left  Direction = "left"
	Right Direction = "right"
)

// Moved reports the bookmark's indexes before and after a move.
type Moved struct{ From, To int }

// Move exchanges a bookmark with its neighbour in one database write.
func (s *Store) Move(ctx context.Context, user, id string, dir Direction) (Moved, bool, error) {
	var moved Moved
	changed := false
	err := s.d.Write(ctx, func(tx *sql.Tx) error {
		if dir != Left && dir != Right {
			return nil
		}
		rows, err := tx.QueryContext(ctx, "SELECT id, position FROM bookmarks WHERE created_by = ? ORDER BY position, id", user)
		if err != nil {
			return err
		}
		var ids []string
		var positions []int64
		index := -1
		hasTie := false
		for rows.Next() {
			var key string
			var position int64
			if err := rows.Scan(&key, &position); err != nil {
				_ = rows.Close()
				return err
			}
			if key == id {
				index = len(ids)
			}
			if len(positions) > 0 && positions[len(positions)-1] == position {
				hasTie = true
			}
			ids = append(ids, key)
			positions = append(positions, position)
		}
		rowErr := rows.Err()
		if err := rows.Close(); err != nil {
			return err
		}
		if rowErr != nil {
			return rowErr
		}
		target := index - 1
		if dir == Right {
			target = index + 1
		}
		if index < 0 || target < 0 || target >= len(ids) {
			return nil
		}
		if hasTie {
			// A directly inserted tie needs distinct positions to exchange its order.
			ids[index], ids[target] = ids[target], ids[index]
			for i, key := range ids {
				if _, err := tx.ExecContext(ctx, "UPDATE bookmarks SET position = ? WHERE id = ?", i+1, key); err != nil {
					return err
				}
			}
		} else {
			if _, err := tx.ExecContext(ctx, "UPDATE bookmarks SET position = CASE id WHEN ? THEN ? WHEN ? THEN ? END WHERE id IN (?, ?)", id, positions[target], ids[target], positions[index], id, ids[target]); err != nil {
				return err
			}
		}
		moved, changed = Moved{From: index, To: target}, true
		return nil
	})
	if err != nil {
		return Moved{}, false, err
	}
	return moved, changed, nil
}
