// Package store keeps the broker's retained log and subscriber positions.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
)

// Config supplies the clock, ingest limits, and runtime hooks.
type Config struct {
	Now       func() time.Time
	DepthMax  int64
	Ask       func(context.Context, string)
	Telemetry *telemetry.Writer
	Swept     func(int)
	Scanned   func(string)
}

// SweepBatch bounds the number of records removed by one write.
const SweepBatch = 500

// Store uses one appkit database handle for every transaction.
type Store struct {
	d       *db.DB
	cfg     Config
	mu      sync.Mutex
	changed chan struct{}
}

// New builds a store over an already migrated database.
func New(d *db.DB, cfg Config) *Store {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Store{d: d, cfg: cfg, changed: make(chan struct{})}
}

// Store errors distinguish refusals from unavailable persistence.
var (
	ErrUndeclared   = fmt.Errorf("undeclared event: %w", events.ErrRejected)
	ErrTooDeep      = fmt.Errorf("event too deep: %w", events.ErrRejected)
	ErrNoSubscriber = errors.New("no subscriber")
	ErrNotPaused    = errors.New("subscriber not paused")
	ErrCursor       = errors.New("invalid cursor")
)

func (s *Store) now() time.Time { return s.cfg.Now().UTC().Truncate(time.Microsecond) }
func (s *Store) notify() {
	s.mu.Lock()
	close(s.changed)
	s.changed = make(chan struct{})
	s.mu.Unlock()
}

// Changed returns the next state-change notification.
func (s *Store) Changed() <-chan struct{} { s.mu.Lock(); defer s.mu.Unlock(); return s.changed }

func head(tx *sql.Tx) (int64, error) {
	var h int64
	err := tx.QueryRow("SELECT COALESCE((SELECT seq FROM sqlite_sequence WHERE name='events'),0)").Scan(&h)
	return h, err
}

// Head returns the last allocated sequence, including swept events.
func (s *Store) Head(ctx context.Context) (int64, error) {
	var h int64
	err := s.d.Read(ctx, func(tx *sql.Tx) error { var err error; h, err = head(tx); return err })
	return h, err
}

func known(tx *sql.Tx, e events.Event) (bool, bool, error) {
	var n int
	if err := tx.QueryRow("SELECT COUNT(*) FROM events WHERE id=?", e.ID).Scan(&n); err != nil {
		return false, false, err
	}
	if n != 0 {
		return true, false, nil
	}
	d, ok, err := declaration(tx, e.Service)
	if err != nil {
		return false, false, err
	}
	if ok {
		for _, em := range d.Emits {
			if em.Event == e.Name {
				return false, true, nil
			}
		}
	}
	return false, false, nil
}

// Deliver accepts one event, or acknowledges an already retained event.
func (s *Store) Deliver(ctx context.Context, e events.Event) error {
	raw, err := e.MarshalJSON()
	if err != nil {
		return fmt.Errorf("invalid event: %w", events.ErrRejected)
	}
	var duplicate, declared bool
	err = s.d.Read(ctx, func(tx *sql.Tx) error { var err error; duplicate, declared, err = known(tx, e); return err })
	if err != nil || duplicate {
		return err
	}
	if !declared && s.cfg.Ask != nil {
		s.cfg.Ask(ctx, e.Service)
	}
	stored := false
	err = s.d.Write(ctx, func(tx *sql.Tx) error {
		duplicate, declared, err = known(tx, e)
		if err != nil || duplicate {
			return err
		}
		if !declared {
			return ErrUndeclared
		}
		if int64(e.Depth) > s.cfg.DepthMax {
			return ErrTooDeep
		}
		var envelope map[string]json.RawMessage
		if err = json.Unmarshal(raw, &envelope); err != nil {
			return err
		}
		attrs := envelope["attrs"]
		result, err := tx.Exec("INSERT INTO events(id,time,service,event,request_id,user,attrs,cause,depth,received) VALUES(?,?,?,?,?,?,?,?,?,?)", e.ID, e.Time.UnixMicro(), e.Service, e.Name, e.RequestID, e.User, string(attrs), e.Cause, e.Depth, s.now().UnixMicro())
		if err != nil {
			return err
		}
		seq, err := result.LastInsertId()
		if err != nil {
			return err
		}
		var values map[string]json.RawMessage
		if err = json.Unmarshal(attrs, &values); err != nil {
			return err
		}
		for k, v := range values {
			if _, err = tx.Exec("INSERT INTO attrs(seq,key,value) VALUES(?,?,?)", seq, k, string(v)); err != nil {
				return err
			}
		}
		if s.cfg.Telemetry != nil {
			s.cfg.Telemetry.Emit(ctx, "event.accepted", telemetry.Attrs{"event": e.ID, "cause": e.Cause})
		}
		stored = true
		return nil
	})
	if err == nil && stored {
		s.notify()
	}
	return err
}

const eventColumns = "seq,id,time,service,event,request_id,user,attrs,cause,depth,received"

type scanner interface{ Scan(...any) error }

func readEvent(row scanner) (events.Event, error) {
	var e events.Event
	var tm, received int64
	var attrs string
	err := row.Scan(&e.Seq, &e.ID, &tm, &e.Service, &e.Name, &e.RequestID, &e.User, &attrs, &e.Cause, &e.Depth, &received)
	if err != nil {
		return events.Event{}, err
	}
	e.Time = time.UnixMicro(tm).UTC()
	e.Received = time.UnixMicro(received).UTC()
	// Decode through the event contract to retain integer precision and canonical numbers.
	wire, err := json.Marshal(map[string]any{"id": e.ID, "time": e.Time.Format("2006-01-02T15:04:05.000000Z"), "service": e.Service, "event": e.Name, "request_id": e.RequestID, "user": e.User, "attrs": json.RawMessage(attrs), "cause": e.Cause, "depth": e.Depth, "seq": e.Seq, "received": e.Received.Format("2006-01-02T15:04:05.000000Z")})
	if err != nil {
		return events.Event{}, err
	}
	err = e.UnmarshalJSON(wire)
	return e, err
}
