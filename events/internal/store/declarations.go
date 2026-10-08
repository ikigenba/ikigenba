package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/ikigenba/ikigenba/appkit/events"
)

// Declaration holds the most recent service capabilities.
type Declaration struct {
	Emits   []events.Emission
	Accepts []string
}

type emissionJSON struct {
	Event string   `json:"event"`
	Attrs []string `json:"attrs"`
}

func encodeDeclaration(d Declaration) (string, string, error) {
	emits := make([]emissionJSON, len(d.Emits))
	for i, e := range d.Emits {
		a := append([]string{}, e.Attrs...)
		emits[i] = emissionJSON{e.Event, a}
	}
	a, err := json.Marshal(emits)
	if err != nil {
		return "", "", err
	}
	b, err := json.Marshal(append([]string{}, d.Accepts...))
	return string(a), string(b), err
}
func declaration(tx *sql.Tx, service string) (Declaration, bool, error) {
	var emits, accepts string
	err := tx.QueryRow("SELECT emits,accepts FROM declarations WHERE service=?", service).Scan(&emits, &accepts)
	if errors.Is(err, sql.ErrNoRows) {
		return Declaration{}, false, nil
	}
	if err != nil {
		return Declaration{}, false, err
	}
	d, err := decodeDeclaration(emits, accepts)
	return d, true, err
}
func decodeDeclaration(emits, accepts string) (Declaration, error) {
	var es []emissionJSON
	var d Declaration
	if err := json.Unmarshal([]byte(emits), &es); err != nil {
		return d, err
	}
	d.Emits = make([]events.Emission, len(es))
	for i, e := range es {
		d.Emits[i] = events.Emission{Event: e.Event, Attrs: e.Attrs}
	}
	err := json.Unmarshal([]byte(accepts), &d.Accepts)
	return d, err
}
func declarations(tx *sql.Tx) (map[string]Declaration, error) {
	result := make(map[string]Declaration)
	rows, err := tx.Query("SELECT service,emits,accepts FROM declarations")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var service, emits, accepts string
		if err = rows.Scan(&service, &emits, &accepts); err != nil {
			return nil, err
		}
		d, err := decodeDeclaration(emits, accepts)
		if err != nil {
			return nil, err
		}
		result[service] = d
	}
	return result, rows.Err()
}

// Declarations returns all held declarations.
func (s *Store) Declarations(ctx context.Context) (map[string]Declaration, error) {
	var result map[string]Declaration
	err := s.d.Read(ctx, func(tx *sql.Tx) error { var err error; result, err = declarations(tx); return err })
	return result, err
}

// Declare replaces capabilities and updates subscriber membership.
func (s *Store) Declare(ctx context.Context, service string, d Declaration) error {
	emits, accepts, err := encodeDeclaration(d)
	if err != nil {
		return err
	}
	changed := false
	err = s.d.Write(ctx, func(tx *sql.Tx) error {
		now := s.now().UnixMicro()
		if _, err := tx.Exec("INSERT INTO declarations(service,emits,accepts,asked) VALUES(?,?,?,?) ON CONFLICT(service) DO UPDATE SET emits=excluded.emits,accepts=excluded.accepts,asked=excluded.asked", service, emits, accepts, now); err != nil {
			return err
		}
		sub, ok, err := subscriber(tx, service)
		if err != nil {
			return err
		}
		if !subscribes(d) {
			if ok && sub.Status != StatusGone {
				changed = true
				return setGone(tx, service, now)
			}
			return nil
		}
		h, err := head(tx)
		if err != nil {
			return err
		}
		if !ok {
			changed = true
			_, err = tx.Exec("INSERT INTO subscribers(service,status,cursor,since) VALUES(?,?,?,?)", service, StatusOK, h, now)
			return err
		}
		if sub.Status == StatusGone {
			var count int64
			if err = tx.QueryRow("SELECT COUNT(*) FROM events WHERE seq>? AND seq<=?", sub.Cursor, h).Scan(&count); err != nil {
				return err
			}
			c := sub.Cursor
			if h > c && count != h-c {
				c = h
			}
			changed = true
			_, err = tx.Exec("UPDATE subscribers SET status=?,cursor=?,since=? WHERE service=?", StatusOK, c, now, service)
			return err
		}
		return nil
	})
	if err == nil && changed {
		s.notify()
	}
	return err
}
func setGone(tx *sql.Tx, service string, now int64) error {
	_, err := tx.Exec("UPDATE subscribers SET status=?,since=?,event='',name='',seq=0,error='' WHERE service=?", StatusGone, now, service)
	return err
}

// Forget removes capabilities while retaining the subscriber history.
func (s *Store) Forget(ctx context.Context, service string) error {
	changed := false
	err := s.d.Write(ctx, func(tx *sql.Tx) error {
		if _, err := tx.Exec("DELETE FROM declarations WHERE service=?", service); err != nil {
			return err
		}
		sub, ok, err := subscriber(tx, service)
		if err != nil {
			return err
		}
		if ok && sub.Status != StatusGone {
			changed = true
			return setGone(tx, service, s.now().UnixMicro())
		}
		return nil
	})
	if err == nil && changed {
		s.notify()
	}
	return err
}
func accepts(d Declaration, name string) bool {
	for _, a := range d.Accepts {
		if a == name || a == "*" {
			return true
		}
	}
	return false
}

func subscribes(d Declaration) bool {
	for _, a := range d.Accepts {
		if a == "*" || events.Match(a, a) {
			return true
		}
	}
	return false
}
