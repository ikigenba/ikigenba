package store

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/ikigenba/ikigenba/appkit/events"
)

// Status describes delivery eligibility.
type Status string

// Subscriber status values.
const (
	StatusOK     Status = "ok"
	StatusPaused Status = "paused"
	StatusGone   Status = "gone"
)

// Reason identifies an event that stopped delivery.
type Reason struct {
	Event string
	Name  string
	Seq   int64
	Error string
}

// Subscriber is a persistent service position and its current lag.
type Subscriber struct {
	Service string
	Status  Status
	Cursor  int64
	Lag     int64
	Since   time.Time
	Reason  *Reason
}

const subscriberColumns = "service,status,cursor,since,event,name,seq,error"

func readSubscriber(row scanner) (Subscriber, error) {
	var sub Subscriber
	var since int64
	var reason Reason
	err := row.Scan(&sub.Service, &sub.Status, &sub.Cursor, &since, &reason.Event, &reason.Name, &reason.Seq, &reason.Error)
	sub.Since = time.UnixMicro(since).UTC()
	if sub.Status == StatusPaused {
		sub.Reason = &reason
	}
	return sub, err
}
func subscriber(tx *sql.Tx, service string) (Subscriber, bool, error) {
	sub, err := readSubscriber(tx.QueryRow("SELECT "+subscriberColumns+" FROM subscribers WHERE service=?", service))
	if errors.Is(err, sql.ErrNoRows) {
		return Subscriber{}, false, nil
	}
	return sub, err == nil, err
}
func lag(sub *Subscriber, h int64) { sub.Lag = max(int64(0), h-sub.Cursor) }

// Subscribers returns persistent positions in service byte order.
func (s *Store) Subscribers(ctx context.Context) ([]Subscriber, error) {
	result := make([]Subscriber, 0)
	err := s.d.Read(ctx, func(tx *sql.Tx) error {
		h, err := head(tx)
		if err != nil {
			return err
		}
		rows, err := tx.Query("SELECT " + subscriberColumns + " FROM subscribers ORDER BY service")
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			sub, err := readSubscriber(rows)
			if err != nil {
				return err
			}
			lag(&sub, h)
			result = append(result, sub)
		}
		return rows.Err()
	})
	return result, err
}

// Next scans eligible work, leaving the selected event unfinished.
func (s *Store) Next(ctx context.Context, service string) (events.Event, bool, error) {
	var event events.Event
	var found, active bool
	var target int64
	err := s.d.Read(ctx, func(tx *sql.Tx) error {
		sub, ok, err := subscriber(tx, service)
		if err != nil {
			return err
		}
		if !ok {
			return ErrNoSubscriber
		}
		if sub.Status != StatusOK {
			return nil
		}
		active = true
		d, _, err := declaration(tx, service)
		if err != nil {
			return err
		}
		h, err := head(tx)
		if err != nil {
			return err
		}
		target = h
		rows, err := tx.Query("SELECT "+eventColumns+" FROM events WHERE seq>? ORDER BY seq", sub.Cursor)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			e, err := readEvent(rows)
			if err != nil {
				return err
			}
			if accepts(d, e.Name) {
				event = e
				found = true
				target = max(sub.Cursor, e.Seq-1)
				break
			}
		}
		return rows.Err()
	})
	if err != nil {
		return events.Event{}, false, err
	}
	if !active {
		return events.Event{}, false, nil
	}
	if s.cfg.Scanned != nil {
		s.cfg.Scanned(service)
	}
	err = s.d.Write(ctx, func(tx *sql.Tx) error {
		// The snapshot target never incorporates events appended by Scanned.
		_, err := tx.Exec("UPDATE subscribers SET cursor=? WHERE service=? AND status=? AND cursor<?", target, service, StatusOK, target)
		return err
	})
	if err != nil {
		return events.Event{}, false, err
	}
	return event, found, nil
}

// Advance marks work completed for an active subscriber.
func (s *Store) Advance(ctx context.Context, service string, seq int64) error {
	changed := false
	err := s.d.Write(ctx, func(tx *sql.Tx) error {
		sub, ok, err := subscriber(tx, service)
		if err != nil {
			return err
		}
		if !ok {
			return ErrNoSubscriber
		}
		if sub.Status != StatusOK || sub.Cursor >= seq {
			return nil
		}
		changed = true
		_, err = tx.Exec("UPDATE subscribers SET cursor=? WHERE service=?", seq, service)
		return err
	})
	if err == nil && changed {
		s.notify()
	}
	return err
}

// Pause records a delivery failure for an active subscriber.
func (s *Store) Pause(ctx context.Context, service string, seq int64, reason string) error {
	changed := false
	err := s.d.Write(ctx, func(tx *sql.Tx) error {
		sub, ok, err := subscriber(tx, service)
		if err != nil {
			return err
		}
		if !ok {
			return ErrNoSubscriber
		}
		if sub.Status != StatusOK {
			return nil
		}
		var id, name string
		if err = tx.QueryRow("SELECT id,event FROM events WHERE seq=?", seq).Scan(&id, &name); err != nil {
			return err
		}
		_, err = tx.Exec("UPDATE subscribers SET status=?,since=?,event=?,name=?,seq=?,error=? WHERE service=?", StatusPaused, s.now().UnixMicro(), id, name, seq, reason, service)
		changed = err == nil
		return err
	})
	if err == nil && changed {
		s.notify()
	}
	return err
}
func (s *Store) unpause(ctx context.Context, service string, skip bool) (Subscriber, Reason, error) {
	var result Subscriber
	var reason Reason
	err := s.d.Write(ctx, func(tx *sql.Tx) error {
		sub, ok, err := subscriber(tx, service)
		if err != nil {
			return err
		}
		if !ok {
			return ErrNoSubscriber
		}
		if sub.Status != StatusPaused {
			return ErrNotPaused
		}
		reason = *sub.Reason
		c := sub.Cursor
		if skip {
			c = reason.Seq
		}
		if _, err = tx.Exec("UPDATE subscribers SET status=?,cursor=?,since=?,event='',name='',seq=0,error='' WHERE service=?", StatusOK, c, s.now().UnixMicro(), service); err != nil {
			return err
		}
		result, _, err = subscriber(tx, service)
		if err != nil {
			return err
		}
		h, err := head(tx)
		lag(&result, h)
		return err
	})
	if err != nil {
		return Subscriber{}, Reason{}, err
	}
	s.notify()
	return result, reason, nil
}

// Skip clears a pause and advances past its event.
func (s *Store) Skip(ctx context.Context, service string) (Subscriber, Reason, error) {
	return s.unpause(ctx, service, true)
}

// Resume clears a pause and retries its event.
func (s *Store) Resume(ctx context.Context, service string) (Subscriber, error) {
	sub, _, err := s.unpause(ctx, service, false)
	return sub, err
}

// Sweep removes aged records no active subscriber still needs.
func (s *Store) Sweep(ctx context.Context, before time.Time) error {
	for {
		removed := 0
		err := s.d.Write(ctx, func(tx *sql.Tx) error {
			result, err := tx.Exec("DELETE FROM events WHERE seq IN (SELECT seq FROM events WHERE received<? AND seq<=COALESCE((SELECT MIN(cursor) FROM subscribers WHERE status IN ('ok','paused')),9223372036854775807) ORDER BY seq LIMIT ?)", microsCeil(before), SweepBatch)
			if err != nil {
				return err
			}
			n, err := result.RowsAffected()
			removed = int(n)
			return err
		})
		if err != nil {
			return err
		}
		if s.cfg.Swept != nil {
			s.cfg.Swept(removed)
		}
		if removed < SweepBatch {
			return ctx.Err()
		}
	}
}
