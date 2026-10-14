// Package store persists runs and their source rows.
package store

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/scrapes/internal/source"
)

// Config supplies the clock and random source.
type Config struct {
	Now  func() time.Time
	Rand io.Reader
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
	Unreachable       = "The scrapes database is unavailable."
)

// Record-state and database errors.
var (
	ErrNotFound    = errors.New("record not found")
	ErrBusy        = errors.New("scraper already running")
	ErrDelivered   = errors.New("delivery already recorded")
	ErrEnded       = errors.New("run already ended")
	ErrUnreachable = errors.New("database unreachable")
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
	if err == nil || errors.Is(err, ErrNotFound) || errors.Is(err, ErrBusy) || errors.Is(err, ErrDelivered) || errors.Is(err, ErrEnded) {
		return err
	}
	return errors.Join(ErrUnreachable, err)
}
func (s *Store) transaction(ctx context.Context, write bool, fn func(*sql.Tx) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
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

const rowCols = `"run",source_id,scraper,collected,digest,name,names,street,city,state,zip,county,lat,lon,people,facts,raw`

func scanRow(row scanner) (source.Row, error) {
	var r source.Row
	var collected, names, people string
	var facts, raw []byte
	err := row.Scan(&r.Run, &r.SourceID, &r.Scraper, &collected, &r.Digest, &r.Name, &names, &r.Address.Street, &r.Address.City, &r.Address.State, &r.Address.Zip, &r.Address.County, &r.Lat, &r.Lon, &people, &facts, &raw)
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
	r.Collected = decodeTime(collected)
	return r, nil
}

// PutRows atomically stores a source batch, replacing repeated identifiers.
func (s *Store) PutRows(ctx context.Context, id string, batch []source.Row) error {
	return s.transaction(ctx, true, func(tx *sql.Tx) error {
		for _, r := range batch {
			if r.SourceID == "" {
				return ErrUnreachable
			}
		}
		run, err := readRun(ctx, tx, id)
		if err != nil {
			return err
		}
		if run.Status != StatusRunning {
			return ErrEnded
		}
		for _, r := range batch {
			names, e := json.Marshal(r.Names)
			if e != nil {
				return e
			}
			people, e := json.Marshal(r.People)
			if e != nil {
				return e
			}
			_, e = tx.ExecContext(ctx, "INSERT OR REPLACE INTO rows ("+rowCols+",names_lower) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)", id, r.SourceID, run.Scraper, encodeTime(r.Collected), r.Digest, r.Name, string(names), r.Address.Street, r.Address.City, r.Address.State, r.Address.Zip, r.Address.County, r.Lat, r.Lon, string(people), []byte(r.Facts), []byte(r.Raw), strings.ToLower(strings.Join(r.Names, "\n")))
			if e != nil {
				return e
			}
			if _, e = tx.ExecContext(ctx, `DELETE FROM row_names WHERE "run"=? AND source_id=?`, id, r.SourceID); e != nil {
				return e
			}
			for _, name := range r.Names {
				if _, e = tx.ExecContext(ctx, `INSERT INTO row_names ("run",source_id,name,name_lower) VALUES (?,?,?,?)`, id, r.SourceID, name, strings.ToLower(name)); e != nil {
					return e
				}
			}
		}
		return nil
	})
}
func (s *Store) end(ctx context.Context, id, reason string) (Run, error) {
	var out Run
	err := s.transaction(ctx, true, func(tx *sql.Tx) error {
		if reason != "" && reason != ReasonSource && reason != ReasonInterrupted && reason != ReasonStore {
			return ErrUnreachable
		}
		r, e := readRun(ctx, tx, id)
		if e != nil {
			return e
		}
		if r.Status != StatusRunning {
			return ErrEnded
		}
		r.Finished = stamp(s.cfg.Now())
		if reason != "" {
			r.Status = StatusFailed
			r.Reason = reason
			if _, e = tx.ExecContext(ctx, `DELETE FROM row_names WHERE "run"=?`, id); e != nil {
				return e
			}
			_, e = tx.ExecContext(ctx, `DELETE FROM rows WHERE "run"=?`, id)
			if e != nil {
				return e
			}
		} else {
			r.Status = StatusDone
			baseline, e := scanRun(tx.QueryRowContext(ctx, "SELECT "+runCols+" FROM runs WHERE scraper=? AND status=? AND id<>? ORDER BY started DESC,id DESC LIMIT 1", r.Scraper, StatusDone, id))
			if e != nil && !errors.Is(e, ErrNotFound) {
				return e
			}
			b := baseline.ID
			if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM rows WHERE "run"=?`, id).Scan(&r.Rows); e != nil {
				return e
			}
			if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM rows a WHERE a."run"=? AND NOT EXISTS (SELECT 1 FROM rows b WHERE b."run"=? AND b.source_id=a.source_id)`, id, b).Scan(&r.New); e != nil {
				return e
			}
			if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM rows a JOIN rows b ON a.source_id=b.source_id WHERE a."run"=? AND b."run"=? AND a.digest<>b.digest`, id, b).Scan(&r.Changed); e != nil {
				return e
			}
			r.Unchanged = r.Rows - r.New - r.Changed
			if e = tx.QueryRowContext(ctx, `SELECT count(*) FROM rows a WHERE a."run"=? AND NOT EXISTS (SELECT 1 FROM rows b WHERE b."run"=? AND b.source_id=a.source_id)`, b, id).Scan(&r.Gone); e != nil {
				return e
			}
		}
		if e = putRun(ctx, tx, r); e == nil {
			out = r
		}
		return e
	})
	return out, err
}

// FinishRun completes a run and compares its rows with the latest baseline.
func (s *Store) FinishRun(ctx context.Context, id string) (Run, error) { return s.end(ctx, id, "") }

// FailRun fails a run and removes its partial rows.
func (s *Store) FailRun(ctx context.Context, id, reason string) (Run, error) {
	if reason == "" {
		return Run{}, s.transaction(ctx, true, func(*sql.Tx) error { return ErrUnreachable })
	}
	return s.end(ctx, id, reason)
}

// GetRow reads a raw row from a completed run.
func (s *Store) GetRow(ctx context.Context, run, id string) (source.Row, error) {
	var out source.Row
	err := s.transaction(ctx, false, func(tx *sql.Tx) error {
		r, e := readRun(ctx, tx, run)
		if e != nil {
			return e
		}
		if r.Status != StatusDone {
			return ErrNotFound
		}
		out, e = scanRow(tx.QueryRowContext(ctx, "SELECT "+rowCols+` FROM rows WHERE "run"=? AND source_id=?`, run, id))
		return e
	})
	return out, err
}
func each(ctx context.Context, tx *sql.Tx, run string, fn func(source.Row) error) error {
	r, e := readRun(ctx, tx, run)
	if e != nil {
		return e
	}
	if r.Status != StatusDone {
		return nil
	}
	rows, e := tx.QueryContext(ctx, "SELECT "+rowCols+` FROM rows WHERE "run"=? ORDER BY source_id`, run)
	if e != nil {
		return e
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		r, e := scanRow(rows)
		if e != nil {
			return e
		}
		if e = fn(r); e != nil {
			return e
		}
	}
	return rows.Err()
}

// ListRowsAfter reads a bounded filtered window without raw records.
func (s *Store) ListRowsAfter(ctx context.Context, run, name, after string, limit int) ([]source.Row, error) {
	var out []source.Row
	err := s.transaction(ctx, false, func(tx *sql.Tx) error {
		if limit < 1 {
			return ErrUnreachable
		}
		r, e := readRun(ctx, tx, run)
		if e != nil {
			return e
		}
		out = []source.Row{}
		if r.Status != StatusDone {
			return nil
		}
		// Select no raw data: the window's memory is bounded by its envelopes.
		const cols = `r."run",r.source_id,r.scraper,r.collected,r.digest,r.name,r.names,r.street,r.city,r.state,r.zip,r.county,r.lat,r.lon,r.people,r.facts,NULL`
		const named = ` AND EXISTS (SELECT 1 FROM row_names n WHERE n."run"=r."run" AND n.source_id=r.source_id AND instr(n.name_lower,?)>0)`
		const first = "SELECT " + cols + ` FROM "rows" r WHERE r."run"=?`
		const positioned = first + ` AND r.source_id>?`
		const ordered = " ORDER BY r.source_id LIMIT ?"
		const firstRows = first + ordered
		const namedFirstRows = first + named + ordered
		const rowsAfter = positioned + ordered
		const namedRowsAfter = positioned + named + ordered
		var q string
		var args []any
		switch {
		case name != "" && after != "":
			q = namedRowsAfter
			args = []any{run, after, strings.ToLower(name), limit}
		case name != "":
			q = namedFirstRows
			args = []any{run, strings.ToLower(name), limit}
		case after != "":
			q = rowsAfter
			args = []any{run, after, limit}
		default:
			q = firstRows
			args = []any{run, limit}
		}
		rows, e := tx.QueryContext(ctx, q, args...)
		if e != nil {
			return e
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			row, e := scanRow(rows)
			if e != nil {
				return e
			}
			out = append(out, row)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// ListRowsBefore reads a reversed bounded filtered window without raw records.
func (s *Store) ListRowsBefore(ctx context.Context, run, name, before string, limit int) ([]source.Row, error) {
	var out []source.Row
	err := s.transaction(ctx, false, func(tx *sql.Tx) error {
		if limit < 1 {
			return ErrUnreachable
		}
		r, e := readRun(ctx, tx, run)
		if e != nil {
			return e
		}
		out = []source.Row{}
		if r.Status != StatusDone {
			return nil
		}
		// Select no raw data: the window's memory is bounded by its envelopes.
		const firstRows = `SELECT r."run",r.source_id,r.scraper,r.collected,r.digest,r.name,r.names,r.street,r.city,r.state,r.zip,r.county,r.lat,r.lon,r.people,r.facts,NULL FROM "rows" r WHERE r."run"=? ORDER BY r.source_id DESC LIMIT ?`
		const namedFirstRows = `SELECT r."run",r.source_id,r.scraper,r.collected,r.digest,r.name,r.names,r.street,r.city,r.state,r.zip,r.county,r.lat,r.lon,r.people,r.facts,NULL FROM "rows" r WHERE r."run"=? AND EXISTS (SELECT 1 FROM row_names n WHERE n."run"=r."run" AND n.source_id=r.source_id AND instr(n.name_lower,?)>0) ORDER BY r.source_id DESC LIMIT ?`
		const rowsBefore = `SELECT r."run",r.source_id,r.scraper,r.collected,r.digest,r.name,r.names,r.street,r.city,r.state,r.zip,r.county,r.lat,r.lon,r.people,r.facts,NULL FROM "rows" r WHERE r."run"=? AND r.source_id<? ORDER BY r.source_id DESC LIMIT ?`
		const namedRowsBefore = `SELECT r."run",r.source_id,r.scraper,r.collected,r.digest,r.name,r.names,r.street,r.city,r.state,r.zip,r.county,r.lat,r.lon,r.people,r.facts,NULL FROM "rows" r WHERE r."run"=? AND r.source_id<? AND EXISTS (SELECT 1 FROM row_names n WHERE n."run"=r."run" AND n.source_id=r.source_id AND instr(n.name_lower,?)>0) ORDER BY r.source_id DESC LIMIT ?`
		var q string
		var args []any
		switch {
		case name != "" && before != "":
			q = namedRowsBefore
			args = []any{run, before, strings.ToLower(name), limit}
		case name != "":
			q = namedFirstRows
			args = []any{run, strings.ToLower(name), limit}
		case before != "":
			q = rowsBefore
			args = []any{run, before, limit}
		default:
			q = firstRows
			args = []any{run, limit}
		}
		rows, e := tx.QueryContext(ctx, q, args...)
		if e != nil {
			return e
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			row, e := scanRow(rows)
			if e != nil {
				return e
			}
			out = append(out, row)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// EachRow streams completed rows in source identifier order.
func (s *Store) EachRow(ctx context.Context, run string, fn func(source.Row) error) error {
	var callback error
	err := s.transaction(ctx, false, func(tx *sql.Tx) error {
		return each(ctx, tx, run, func(r source.Row) error { callback = fn(r); return callback })
	})
	if callback != nil {
		return callback
	}
	return err
}

// PruneRuns removes older completed and failed runs per scraper and status.
func (s *Store) PruneRuns(ctx context.Context, keep int64) ([]Run, error) {
	var out []Run
	err := s.transaction(ctx, true, func(tx *sql.Tx) error {
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
			if _, e = tx.ExecContext(ctx, `DELETE FROM row_names WHERE "run"=?`, r.ID); e != nil {
				return e
			}
			if _, e = tx.ExecContext(ctx, `DELETE FROM rows WHERE "run"=?`, r.ID); e != nil {
				return e
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
	return out, nil
}
