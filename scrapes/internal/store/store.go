// Package store persists runs and their source rows.
package store

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/scrapes/internal/source"
)

// Config supplies the clock and random source.
type Config struct {
	Now        func() time.Time
	Rand       io.Reader
	Runs       string
	ChunkBytes int64
	LoadRows   int
}

// Store serializes operations over its database handle.
type Store struct {
	d   *db.DB
	cfg Config
	mu  sync.Mutex
}

// New constructs a store without opening a database.
func New(d *db.DB, cfg Config) *Store { return &Store{d: d, cfg: cfg} }

// Run holds a recorded walk and its outcome.
type Run struct {
	ID, Scraper, Status, Reason                     string
	Started, Finished                               time.Time
	Rows, New, Changed, Unchanged, Gone             int64
	UserID, UserEmail, Trigger, RequestID, Delivery string
}

// Record vocabulary.
const (
	RunIDPrefix       = "run_"
	StatusRunning     = "running"
	StatusDone        = "done"
	StatusFailed      = "failed"
	ReasonSource      = "source"
	ReasonInterrupted = "interrupted"
	ReasonStore       = "store"
	Missing           = "The run rows are missing."
	DefaultChunkBytes = 16777216
	DefaultLoadRows   = 5000
	Unreachable       = "The scrapes database is unavailable."
)

// Record-state and database errors.
var (
	ErrMissing     = errors.New("run file missing")
	ErrNotFound    = errors.New("record not found")
	ErrBusy        = errors.New("scraper already running")
	ErrDelivered   = errors.New("delivery already recorded")
	ErrEnded       = errors.New("run already ended")
	ErrUnreachable = errors.New("database unreachable")
	ErrLocked      = errors.New("database locked")
)

// ValidRunID recognizes the run identifier format.
func ValidRunID(v string) bool {
	if len(v) != 20 || !strings.HasPrefix(v, RunIDPrefix) {
		return false
	}
	for _, c := range v[4:] {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
func stamp(t time.Time) time.Time { return t.UTC().Truncate(time.Second) }
func encodeTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return stamp(t).Format(time.RFC3339)
}
func decodeTime(v string) time.Time {
	if v == "" {
		return time.Time{}
	}
	t, _ := time.Parse(time.RFC3339, v)
	return t.UTC()
}
func normalize(err error) error {
	if err == nil || errors.Is(err, ErrNotFound) || errors.Is(err, ErrBusy) || errors.Is(err, ErrDelivered) || errors.Is(err, ErrEnded) || errors.Is(err, ErrMissing) {
		return err
	}
	var coded interface{ Code() int }
	if errors.As(err, &coded) && coded.Code()&0xff == 5 {
		return errors.Join(ErrUnreachable, ErrLocked, err)
	}
	return errors.Join(ErrUnreachable, err)
}
func (s *Store) transaction(ctx context.Context, write bool, fn func(*sql.Tx) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.transactionLocked(ctx, write, fn)
}
func (s *Store) transactionLocked(ctx context.Context, write bool, fn func(*sql.Tx) error) error {
	if err := ctx.Err(); err != nil {
		return errors.Join(ErrUnreachable, err)
	}
	if write {
		return normalize(s.d.Write(ctx, fn))
	}
	return normalize(s.d.Read(ctx, fn))
}

const runCols = `id,scraper,status,reason,started,finished,"rows","new",changed,unchanged,gone,user_id,user_email,"trigger",request_id,delivery`

type scanner interface{ Scan(...any) error }

func scanRun(row scanner) (Run, error) {
	var r Run
	var a, b string
	err := row.Scan(&r.ID, &r.Scraper, &r.Status, &r.Reason, &a, &b, &r.Rows, &r.New, &r.Changed, &r.Unchanged, &r.Gone, &r.UserID, &r.UserEmail, &r.Trigger, &r.RequestID, &r.Delivery)
	if errors.Is(err, sql.ErrNoRows) {
		return Run{}, ErrNotFound
	}
	r.Started = decodeTime(a)
	r.Finished = decodeTime(b)
	return r, err
}
func readRun(ctx context.Context, tx *sql.Tx, id string) (Run, error) {
	return scanRun(tx.QueryRowContext(ctx, "SELECT "+runCols+" FROM runs WHERE id=?", id))
}
func putRun(ctx context.Context, tx *sql.Tx, r Run) error {
	_, err := tx.ExecContext(ctx, "INSERT OR REPLACE INTO runs ("+runCols+") VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)", r.ID, r.Scraper, r.Status, r.Reason, encodeTime(r.Started), encodeTime(r.Finished), r.Rows, r.New, r.Changed, r.Unchanged, r.Gone, r.UserID, r.UserEmail, r.Trigger, r.RequestID, r.Delivery)
	return err
}
func queryRuns(ctx context.Context, tx *sql.Tx, q string, args ...any) ([]Run, error) {
	out := []Run{}
	rows, err := tx.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		r, e := scanRun(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// AddRun records a new running walk.
func (s *Store) AddRun(ctx context.Context, r Run) (Run, error) {
	var out Run
	err := s.transaction(ctx, true, func(tx *sql.Tx) error {
		tool := r.Trigger == "" && r.Delivery == "" && r.UserID != ""
		delivery := r.Trigger != "" && r.Delivery != ""
		if r.Scraper == "" || (!tool && !delivery) {
			return ErrUnreachable
		}
		var n int
		if r.Delivery != "" {
			if e := tx.QueryRowContext(ctx, "SELECT count(*) FROM runs WHERE delivery=?", r.Delivery).Scan(&n); e != nil {
				return e
			}
			if n > 0 {
				return ErrDelivered
			}
		}
		if e := tx.QueryRowContext(ctx, "SELECT count(*) FROM runs WHERE scraper=? AND status=?", r.Scraper, StatusRunning).Scan(&n); e != nil {
			return e
		}
		if n > 0 {
			return ErrBusy
		}
		for range 8 {
			b := make([]byte, 8)
			if _, e := io.ReadFull(s.cfg.Rand, b); e != nil {
				return e
			}
			id := RunIDPrefix + hex.EncodeToString(b)
			if e := tx.QueryRowContext(ctx, "SELECT count(*) FROM runs WHERE id=?", id).Scan(&n); e != nil {
				return e
			}
			if n != 0 {
				continue
			}
			r = Run{ID: id, Scraper: r.Scraper, Status: StatusRunning, Started: stamp(s.cfg.Now()), UserID: r.UserID, UserEmail: r.UserEmail, Trigger: r.Trigger, RequestID: r.RequestID, Delivery: r.Delivery}
			if e := putRun(ctx, tx, r); e != nil {
				return e
			}
			out = r
			return nil
		}
		return ErrUnreachable
	})
	return out, err
}

// GetRun reads any run.
func (s *Store) GetRun(ctx context.Context, id string) (Run, error) {
	var out Run
	err := s.transaction(ctx, false, func(tx *sql.Tx) error { var e error; out, e = readRun(ctx, tx, id); return e })
	return out, err
}
func (s *Store) list(ctx context.Context, q string, args ...any) ([]Run, error) {
	var out []Run
	err := s.transaction(ctx, false, func(tx *sql.Tx) error { var e error; out, e = queryRuns(ctx, tx, q, args...); return e })
	return out, err
}

// ListRunsAfter reads runs following a position in newest-first order.
func (s *Store) ListRunsAfter(ctx context.Context, scraper, started, id string, limit int) ([]Run, error) {
	var out []Run
	err := s.transaction(ctx, false, func(tx *sql.Tx) error {
		if limit < 1 {
			return ErrUnreachable
		}
		var q string
		var args []any
		switch {
		case scraper != "" && id != "":
			q = "SELECT " + runCols + " FROM runs WHERE scraper=? AND (started,id)<(?,?) ORDER BY started DESC,id DESC LIMIT ?"
			args = []any{scraper, started, id, limit}
		case scraper != "":
			q = "SELECT " + runCols + " FROM runs WHERE scraper=? ORDER BY started DESC,id DESC LIMIT ?"
			args = []any{scraper, limit}
		case id != "":
			q = "SELECT " + runCols + " FROM runs WHERE (started,id)<(?,?) ORDER BY started DESC,id DESC LIMIT ?"
			args = []any{started, id, limit}
		default:
			q = "SELECT " + runCols + " FROM runs ORDER BY started DESC,id DESC LIMIT ?"
			args = []any{limit}
		}
		var e error
		out, e = queryRuns(ctx, tx, q, args...)
		return e
	})
	return out, err
}

// ListRunsBefore reads runs preceding a position in oldest-first order.
func (s *Store) ListRunsBefore(ctx context.Context, scraper, started, id string, limit int) ([]Run, error) {
	var out []Run
	err := s.transaction(ctx, false, func(tx *sql.Tx) error {
		if limit < 1 {
			return ErrUnreachable
		}
		var q string
		var args []any
		switch {
		case scraper != "" && id != "":
			q = `SELECT id,scraper,status,reason,started,finished,"rows","new",changed,unchanged,gone,user_id,user_email,"trigger",request_id,delivery FROM runs WHERE scraper=? AND (started,id)>(?,?) ORDER BY started,id LIMIT ?`
			args = []any{scraper, started, id, limit}
		case scraper != "":
			q = `SELECT id,scraper,status,reason,started,finished,"rows","new",changed,unchanged,gone,user_id,user_email,"trigger",request_id,delivery FROM runs WHERE scraper=? ORDER BY started,id LIMIT ?`
			args = []any{scraper, limit}
		case id != "":
			q = `SELECT id,scraper,status,reason,started,finished,"rows","new",changed,unchanged,gone,user_id,user_email,"trigger",request_id,delivery FROM runs WHERE (started,id)>(?,?) ORDER BY started,id LIMIT ?`
			args = []any{started, id, limit}
		default:
			q = `SELECT id,scraper,status,reason,started,finished,"rows","new",changed,unchanged,gone,user_id,user_email,"trigger",request_id,delivery FROM runs ORDER BY started,id LIMIT ?`
			args = []any{limit}
		}
		var e error
		out, e = queryRuns(ctx, tx, q, args...)
		return e
	})
	return out, err
}
func (s *Store) byStatus(ctx context.Context, scraper, status string) (Run, error) {
	var out Run
	err := s.transaction(ctx, false, func(tx *sql.Tx) error {
		var e error
		out, e = scanRun(tx.QueryRowContext(ctx, "SELECT "+runCols+" FROM runs WHERE scraper=? AND status=? ORDER BY started DESC,id DESC LIMIT 1", scraper, status))
		return e
	})
	return out, err
}

// CurrentRun reads the newest completed run.
func (s *Store) CurrentRun(ctx context.Context, scraper string) (Run, error) {
	return s.byStatus(ctx, scraper, StatusDone)
}

// RunningRun reads the scraper's active run.
func (s *Store) RunningRun(ctx context.Context, scraper string) (Run, error) {
	return s.byStatus(ctx, scraper, StatusRunning)
}

// RunningRuns reads all active runs in identifier order.
func (s *Store) RunningRuns(ctx context.Context) ([]Run, error) {
	return s.list(ctx, "SELECT "+runCols+" FROM runs WHERE status=? ORDER BY id", StatusRunning)
}

const rowCols = `"run",source_id,scraper,scraped,digest,name,names,street,city,state,zip,county,lat,lon,people,facts,raw`

func scanRow(row scanner) (source.Row, error) {
	var r source.Row
	var scraped, names, people string
	var facts, raw []byte
	err := row.Scan(&r.Run, &r.SourceID, &r.Scraper, &scraped, &r.Digest, &r.Name, &names, &r.Address.Street, &r.Address.City, &r.Address.State, &r.Address.Zip, &r.Address.County, &r.Lat, &r.Lon, &people, &facts, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return source.Row{}, ErrNotFound
	}
	if err != nil {
		return source.Row{}, err
	}
	if err = json.Unmarshal([]byte(names), &r.Names); err != nil {
		return source.Row{}, err
	}
	if err = json.Unmarshal([]byte(people), &r.People); err != nil {
		return source.Row{}, err
	}
	r.Facts, r.Raw = facts, raw
	r.Scraped = decodeTime(scraped)
	return r, nil
}

// Counts describes the comparison produced by a load.
type Counts struct{ Rows, New, Changed, Unchanged, Gone int64 }

func (s *Store) file(id string) string { return filepath.Join(s.cfg.Runs, id+".db") }
func inPlace(path string) bool         { _, err := os.Lstat(path); return err == nil }

func (s *Store) end(ctx context.Context, id, reason string, c Counts) (Run, error) {
	var out Run
	err := s.transaction(ctx, true, func(tx *sql.Tx) error {
		if reason == "" {
			if c.Rows < 0 || c.New < 0 || c.Changed < 0 || c.Unchanged < 0 || c.Gone < 0 || c.Rows != c.New+c.Changed+c.Unchanged {
				return ErrUnreachable
			}
		} else if reason != ReasonSource && reason != ReasonInterrupted && reason != ReasonStore {
			return ErrUnreachable
		}
		r, e := readRun(ctx, tx, id)
		if e != nil {
			return e
		}
		if r.Status != StatusRunning {
			return ErrEnded
		}
		if reason == "" {
			if !inPlace(s.file(id)) {
				return ErrMissing
			}
			r.Status = StatusDone
			r.Rows = c.Rows
			r.New = c.New
			r.Changed = c.Changed
			r.Unchanged = c.Unchanged
			r.Gone = c.Gone
		} else {
			r.Status = StatusFailed
			r.Reason = reason
		}
		r.Finished = stamp(s.cfg.Now())
		if e = putRun(ctx, tx, r); e == nil {
			out = r
		}
		return e
	})
	return out, err
}

// FinishRun records a loaded run's counts.
func (s *Store) FinishRun(ctx context.Context, id string, c Counts) (Run, error) {
	return s.end(ctx, id, "", c)
}

// FailRun records a failure without touching build files.
func (s *Store) FailRun(ctx context.Context, id, reason string) (Run, error) {
	if reason == "" {
		return Run{}, s.transaction(ctx, true, func(*sql.Tx) error { return ErrUnreachable })
	}
	return s.end(ctx, id, reason, Counts{})
}

// PruneRuns removes older completed and failed runs per scraper and status.
func (s *Store) PruneRuns(ctx context.Context, keep int64) ([]Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Run
	err := s.transactionLocked(ctx, true, func(tx *sql.Tx) error {
		if keep < 1 {
			return ErrUnreachable
		}
		all, e := queryRuns(ctx, tx, "SELECT "+runCols+" FROM runs ORDER BY scraper,started DESC,id DESC")
		if e != nil {
			return e
		}
		out = []Run{}
		ranks := map[string]int64{}
		for _, r := range all {
			if r.Status == StatusRunning {
				continue
			}
			key := r.Scraper + "\x00" + r.Status
			rank := ranks[key]
			ranks[key]++
			if rank < keep {
				continue
			}
			if _, e = tx.ExecContext(ctx, "DELETE FROM runs WHERE id=?", r.ID); e != nil {
				return e
			}
			out = append(out, r)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	for _, r := range out {
		_ = os.RemoveAll(s.file(r.ID))
	}
	return out, nil
}

// TidyFiles removes abandoned files while retaining done runs.
func (s *Store) TidyFiles(ctx context.Context) error {
	return s.transaction(ctx, false, func(tx *sql.Tx) error {
		runs, e := queryRuns(ctx, tx, "SELECT "+runCols+" FROM runs WHERE status=?", StatusDone)
		if e != nil {
			return e
		}
		keep := map[string]bool{}
		for _, r := range runs {
			keep[r.ID+".db"] = true
		}
		info, e := os.Lstat(s.cfg.Runs)
		if e != nil || !info.IsDir() {
			return nil
		}
		entries, e := os.ReadDir(s.cfg.Runs)
		if e != nil {
			return nil
		}
		for _, entry := range entries {
			if !keep[entry.Name()] {
				_ = os.RemoveAll(filepath.Join(s.cfg.Runs, entry.Name()))
			}
		}
		return nil
	})
}
