// Package store persists transfer records and their transitions.
package store

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"io"
	"math/big"
	"strings"
	"sync"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
)

// Transfer vocabulary and the unavailable copy used by consumers.
const (
	IDPrefix          = "xfr_"
	DirectionPull     = "pull"
	DirectionPush     = "push"
	StatusQueued      = "queued"
	StatusRunning     = "running"
	StatusDone        = "done"
	StatusFailed      = "failed"
	ReasonNotFound    = "not_found"
	ReasonForbidden   = "forbidden"
	ReasonTooLarge    = "too_large"
	ReasonReconnect   = "reconnect"
	ReasonProvider    = "provider"
	ReasonArtifacts   = "artifacts"
	ReasonInterrupted = "interrupted"
	Unreachable       = "The transfer database is unavailable."
)

// Store errors distinguish absent records, completed transitions, and failures.
var (
	ErrNotFound    = errors.New("transfer not found")
	ErrEnded       = errors.New("transfer is no longer in the required state")
	ErrUnreachable = errors.New("transfer database unreachable")
)

// Config supplies the clock and transfer ID source.
type Config struct {
	Now  func() time.Time
	Rand io.Reader
}

// Transfer is the durable record of one file move.
type Transfer struct {
	ID, Direction, Status, Reason, File, Name, Kind, Path, ContentType, Format string
	Bytes                                                                      int64
	Started, Finished                                                          time.Time
	URL, Link, ArtifactID, UserID, UserEmail, RequestID                        string
}

// Ending describes the fields recorded when a transfer ends.
type Ending struct {
	Status, Reason                    string
	Bytes                             int64
	File, Name, URL, Link, ArtifactID string
}

// Store owns transfer operations over one database handle.
type Store struct {
	d   *db.DB
	cfg Config
	mu  sync.Mutex
}

// New builds a store without opening or creating a database.
func New(d *db.DB, cfg Config) *Store {
	return &Store{d: d, cfg: cfg}
}

// ValidID recognizes the transfer ID encoding.
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
func reason(s string) bool {
	switch s {
	case ReasonNotFound, ReasonForbidden, ReasonTooLarge, ReasonReconnect, ReasonProvider, ReasonArtifacts, ReasonInterrupted:
		return true
	}
	return false
}
func draft(t Transfer) bool {
	return (t.Direction == DirectionPull || t.Direction == DirectionPush) && (t.Status == StatusQueued || t.Status == StatusRunning) && t.UserID != "" && t.Path != ""
}
func ending(e Ending) bool {
	return e.Bytes >= 0 && (e.Status == StatusDone && e.Reason == "" || e.Status == StatusFailed && reason(e.Reason) && e.File == "" && e.URL == "" && e.Link == "" && e.ArtifactID == "")
}
func resultError(err error) error {
	if err == nil || errors.Is(err, ErrNotFound) || errors.Is(err, ErrEnded) {
		return err
	}
	return errors.Join(ErrUnreachable, err)
}
func (s *Store) transaction(ctx context.Context, write bool, fn func(*sql.Tx) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return resultError(err)
	}
	if write {
		return resultError(s.d.Write(ctx, fn))
	}
	return resultError(s.d.Read(ctx, fn))
}

const columns = "id,direction,status,reason,file,name,kind,path,content_type,format,bytes,started,finished,url,link,artifact_id,user_id,user_email,request_id"

type scanner interface{ Scan(...any) error }

func scan(row scanner) (Transfer, error) {
	var t Transfer
	var started, finished string
	err := row.Scan(&t.ID, &t.Direction, &t.Status, &t.Reason, &t.File, &t.Name, &t.Kind, &t.Path, &t.ContentType, &t.Format, &t.Bytes, &started, &finished, &t.URL, &t.Link, &t.ArtifactID, &t.UserID, &t.UserEmail, &t.RequestID)
	if errors.Is(err, sql.ErrNoRows) {
		return Transfer{}, ErrNotFound
	}
	if err != nil {
		return Transfer{}, err
	}
	t.Started, err = time.Parse(time.RFC3339, started)
	if err != nil {
		return Transfer{}, err
	}
	if finished != "" {
		t.Finished, err = time.Parse(time.RFC3339, finished)
	}
	return t, err
}
func get(ctx context.Context, tx *sql.Tx, id string) (Transfer, error) {
	return scan(tx.QueryRowContext(ctx, "SELECT "+columns+" FROM transfers WHERE id=?", id))
}

// Add records a draft with a new ID and start timestamp.
func (s *Store) Add(ctx context.Context, t Transfer) (Transfer, error) {
	var out Transfer
	err := s.transaction(ctx, true, func(tx *sql.Tx) error {
		if !draft(t) {
			return ErrUnreachable
		}
		id := ""
		for range 8 {
			var b [8]byte
			if _, err := io.ReadFull(s.cfg.Rand, b[:]); err != nil {
				return err
			}
			candidate := IDPrefix + hex.EncodeToString(b[:])
			_, err := get(ctx, tx, candidate)
			if errors.Is(err, ErrNotFound) {
				id = candidate
				break
			}
			if err != nil {
				return err
			}
		}
		if id == "" {
			return ErrUnreachable
		}
		out = Transfer{ID: id, Direction: t.Direction, Status: t.Status, File: t.File, Name: t.Name, Kind: t.Kind, Path: t.Path, ContentType: t.ContentType, Format: t.Format, Started: s.cfg.Now().UTC().Truncate(time.Second), UserID: t.UserID, UserEmail: t.UserEmail, RequestID: t.RequestID}
		_, err := tx.ExecContext(ctx, "INSERT INTO transfers ("+columns+") VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)", out.ID, out.Direction, out.Status, "", out.File, out.Name, out.Kind, out.Path, out.ContentType, out.Format, 0, out.Started.Format(time.RFC3339), "", "", "", "", out.UserID, out.UserEmail, out.RequestID)
		return err
	})
	if err != nil {
		return Transfer{}, err
	}
	return out, nil
}
func (s *Store) change(ctx context.Context, id string, validate func() bool, fn func(*Transfer) error) (Transfer, error) {
	var out Transfer
	err := s.transaction(ctx, true, func(tx *sql.Tx) error {
		if !validate() {
			return ErrUnreachable
		}
		t, err := get(ctx, tx, id)
		if err != nil {
			return err
		}
		if err = fn(&t); err != nil {
			return err
		}
		finished := ""
		if !t.Finished.IsZero() {
			finished = t.Finished.Format(time.RFC3339)
		}
		_, err = tx.ExecContext(ctx, "UPDATE transfers SET status=?,reason=?,bytes=?,finished=?,file=?,name=?,url=?,link=?,artifact_id=? WHERE id=?", t.Status, t.Reason, t.Bytes, finished, t.File, t.Name, t.URL, t.Link, t.ArtifactID, id)
		out = t
		return err
	})
	if err != nil {
		return Transfer{}, err
	}
	return out, nil
}

// Start moves a queued transfer to running.
func (s *Store) Start(ctx context.Context, id string) (Transfer, error) {
	return s.change(ctx, id, func() bool { return true }, func(t *Transfer) error {
		if t.Status != StatusQueued {
			return ErrEnded
		}
		t.Status = StatusRunning
		return nil
	})
}

// SetBytes replaces progress for a running transfer.
func (s *Store) SetBytes(ctx context.Context, id string, n int64) (Transfer, error) {
	return s.change(ctx, id, func() bool { return n >= 0 }, func(t *Transfer) error {
		if t.Status != StatusRunning {
			return ErrEnded
		}
		t.Bytes = n
		return nil
	})
}

// Finish ends a queued or running transfer once.
func (s *Store) Finish(ctx context.Context, id string, e Ending) (Transfer, error) {
	return s.change(ctx, id, func() bool { return ending(e) }, func(t *Transfer) error {
		if t.Status == StatusDone || t.Status == StatusFailed {
			return ErrEnded
		}
		if t.Status == StatusQueued && e.Status == StatusDone {
			return ErrUnreachable
		}
		t.Status = e.Status
		t.Reason = e.Reason
		t.Bytes = e.Bytes
		t.URL = e.URL
		t.Link = e.Link
		t.ArtifactID = e.ArtifactID
		if e.File != "" {
			t.File = e.File
		}
		if e.Name != "" {
			t.Name = e.Name
		}
		t.Finished = s.cfg.Now().UTC().Truncate(time.Second)
		return nil
	})
}

// Get reads a transfer belonging to the given user.
func (s *Store) Get(ctx context.Context, userID, id string) (Transfer, error) {
	var out Transfer
	err := s.transaction(ctx, false, func(tx *sql.Tx) error {
		t, err := get(ctx, tx, id)
		if err != nil {
			return err
		}
		if t.UserID != userID {
			return ErrNotFound
		}
		out = t
		return nil
	})
	if err != nil {
		return Transfer{}, err
	}
	return out, nil
}
func rows(ctx context.Context, tx *sql.Tx, query string, args ...any) ([]Transfer, error) {
	r, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = r.Close() }()
	out := []Transfer{}
	for r.Next() {
		t, err := scan(r)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, r.Err()
}

// List reads a user’s transfers newest first.
func (s *Store) List(ctx context.Context, userID string) ([]Transfer, error) {
	var out []Transfer
	err := s.transaction(ctx, false, func(tx *sql.Tx) error {
		var err error
		out, err = rows(ctx, tx, "SELECT "+columns+" FROM transfers WHERE user_id=? ORDER BY started DESC,id DESC", userID)
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ListAfter reads a bounded window of a user's transfers after a key.
func (s *Store) ListAfter(ctx context.Context, userID string, started time.Time, id string, n int) ([]Transfer, error) {
	var out []Transfer
	err := s.transaction(ctx, false, func(tx *sql.Tx) error {
		if n < 1 {
			out = []Transfer{}
			return nil
		}
		query := "SELECT " + columns + " FROM transfers WHERE user_id=?"
		args := []any{userID}
		if id != "" {
			query += " AND (started,id)<(?,?)"
			args = append(args, started.UTC().Format(time.RFC3339), id)
		}
		query += " ORDER BY started DESC,id DESC LIMIT ?"
		args = append(args, n)
		var err error
		out, err = rows(ctx, tx, query, args...)
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Unfinished reads queued and running transfers oldest first.
func (s *Store) Unfinished(ctx context.Context) ([]Transfer, error) {
	var out []Transfer
	err := s.transaction(ctx, false, func(tx *sql.Tx) error {
		var err error
		out, err = rows(ctx, tx, "SELECT "+columns+" FROM transfers WHERE status IN (?,?) ORDER BY started ASC,id ASC", StatusQueued, StatusRunning)
		return err
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Prune removes ended transfers outside both retention limits.
func (s *Store) Prune(ctx context.Context, now time.Time, keepDays, keepCount int64) ([]Transfer, error) {
	var out []Transfer
	err := s.transaction(ctx, true, func(tx *sql.Tx) error {
		if keepDays < 1 || keepCount < 1 {
			return ErrUnreachable
		}
		out = []Transfer{}
		all, err := rows(ctx, tx, "SELECT "+columns+" FROM transfers ORDER BY user_id ASC,started DESC,id DESC")
		if err != nil {
			return err
		}
		var user string
		var rank int64
		for _, t := range all {
			if t.UserID != user {
				user = t.UserID
				rank = 0
			}
			if rank >= keepCount && (t.Status == StatusDone || t.Status == StatusFailed) && olderThan(t.Started, now, keepDays) {
				if _, err := tx.ExecContext(ctx, "DELETE FROM transfers WHERE id=?", t.ID); err != nil {
					return err
				}
				out = append(out, t)
			}
			rank++
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// olderThan compares elapsed seconds without overflowing a time.Duration.
func olderThan(started, now time.Time, days int64) bool {
	if !started.Before(now) {
		return false
	}
	elapsed := new(big.Int).Sub(big.NewInt(now.Unix()), big.NewInt(started.Unix()))
	limit := new(big.Int).Mul(big.NewInt(days), big.NewInt(24*60*60))
	cmp := elapsed.Cmp(limit)
	return cmp > 0 || cmp == 0 && now.Nanosecond() > started.Nanosecond()
}
