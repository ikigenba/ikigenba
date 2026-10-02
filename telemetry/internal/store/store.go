// Package store owns the persistent event trail.
package store

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/appkit/telemetry"
	_ "modernc.org/sqlite" // Register the pure Go SQLite driver.
)

// Record is the stored envelope.
type Record struct {
	Time                            time.Time
	Service, Event, RequestID, User string
	Attrs                           json.RawMessage
}

// Filter selects matching records.
type Filter struct {
	Since, Until     *time.Time
	Services, Events []string
	User, RequestID  *string
	Attrs            telemetry.Attrs
}

// Cursor encodes a search position.
type Cursor string

// Page is one search result.
type Page struct {
	Records []Record
	Next    Cursor
}

// CatalogEntry describes a service's events.
type CatalogEntry struct {
	Service string
	Events  []EventEntry
}

// EventEntry describes an event's retained records.
type EventEntry struct {
	Event    string
	Count    int64
	LastSeen time.Time
	Attrs    []string
}

// GroupBy selects a count grouping.
type GroupBy string

// Count groupings.
const (
	ByService   GroupBy = "service"
	ByEvent     GroupBy = "event"
	ByUser      GroupBy = "user"
	ByRequestID GroupBy = "request_id"
	ByMinute    GroupBy = "minute"
	ByHour      GroupBy = "hour"
	ByDay       GroupBy = "day"
	AttrPrefix          = "attrs."
)

// Group is one count group.
type Group struct {
	Key   string
	Count int64
}

// ErrCursor reports a malformed cursor.
var ErrCursor = errors.New("invalid cursor")

// ErrGroupBy reports an unknown grouping.
var ErrGroupBy = errors.New("invalid grouping")

// Store holds a single SQLite database.
type Store struct{ db *sql.DB }

const schema = `CREATE TABLE IF NOT EXISTS records (id INTEGER PRIMARY KEY AUTOINCREMENT, ts INTEGER NOT NULL, svc TEXT NOT NULL, ev TEXT NOT NULL, req TEXT NOT NULL, user TEXT NOT NULL, attrs TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS attrs (record_id INTEGER NOT NULL, key TEXT NOT NULL, value TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS records_ts ON records(ts);
CREATE INDEX IF NOT EXISTS records_svc_ts ON records(svc,ts);
CREATE INDEX IF NOT EXISTS records_ev_ts ON records(ev,ts);
CREATE INDEX IF NOT EXISTS records_req_ts ON records(req,ts);
CREATE INDEX IF NOT EXISTS records_user_ts ON records(user,ts);
CREATE INDEX IF NOT EXISTS attrs_key_value ON attrs(key,value);`

// Open initializes or opens the database source.
func Open(source string) (*Store, error) {
	if source != "" && source != ":memory:" && !strings.HasPrefix(source, "file:") {
		if err := os.MkdirAll(filepath.Dir(source), 0700); err != nil {
			return nil, fmt.Errorf("create database directory: %w", err)
		}
	}
	db, err := sql.Open("sqlite", source)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err = db.Exec(schema); err == nil {
		// Force a write even when all schema objects already exist.
		_, err = db.Exec("BEGIN IMMEDIATE; INSERT INTO records(ts,svc,ev,req,user,attrs) VALUES(0,'','','','','{}'); ROLLBACK;")
	}
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("open database: %w", err)
	}
	return &Store{db: db}, nil
}

// Close releases the database.
func (s *Store) Close() error { return s.db.Close() }

// Deliver atomically appends one event.
func (s *Store) Deliver(ctx context.Context, e telemetry.Event) error {
	data, err := e.MarshalJSON()
	if err != nil {
		return fmt.Errorf("%w: %w", telemetry.ErrRejected, err)
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	var envelope struct {
		Attrs json.RawMessage `json:"attrs"`
	}
	if err = json.Unmarshal(data, &envelope); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	res, err := tx.ExecContext(ctx, "INSERT INTO records(ts,svc,ev,req,user,attrs) VALUES(?,?,?,?,?,?)", e.Time.UTC().Truncate(time.Microsecond).UnixMicro(), e.Service, e.Name, e.RequestID, e.User, string(envelope.Attrs))
	if err != nil {
		return err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	var attrs map[string]json.RawMessage
	if err = json.Unmarshal(envelope.Attrs, &attrs); err != nil {
		return err
	}
	for key, value := range attrs {
		if _, err = tx.ExecContext(ctx, "INSERT INTO attrs(record_id,key,value) VALUES(?,?,?)", id, key, string(value)); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Sweep removes records older than before in bounded transactions.
func (s *Store) Sweep(ctx context.Context, before time.Time) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		// Compare the microsecond timestamp to the precise cutoff.
		cutoff := before.UnixMicro()
		if before.Nanosecond()%1000 != 0 {
			cutoff++
		}
		_, err = tx.ExecContext(ctx, "DELETE FROM attrs WHERE record_id IN (SELECT id FROM records WHERE ts < ? ORDER BY id LIMIT 500)", cutoff)
		var res sql.Result
		if err == nil {
			res, err = tx.ExecContext(ctx, "DELETE FROM records WHERE id IN (SELECT id FROM records WHERE ts < ? ORDER BY id LIMIT 500)", cutoff)
		}
		if err != nil {
			_ = tx.Rollback()
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			_ = tx.Rollback()
			return err
		}
		if err = tx.Commit(); err != nil {
			return err
		}
		if n < 500 {
			return nil
		}
	}
}

type stored struct {
	Record
	id int64
}

func (s *Store) records(ctx context.Context) ([]stored, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id,ts,svc,ev,req,user,attrs FROM records ORDER BY ts DESC,id DESC")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	result := make([]stored, 0)
	for rows.Next() {
		var r stored
		var ts int64
		var attrs string
		if err := rows.Scan(&r.id, &ts, &r.Service, &r.Event, &r.RequestID, &r.User, &attrs); err != nil {
			return nil, err
		}
		r.Time = time.UnixMicro(ts).UTC()
		r.Attrs = json.RawMessage(attrs)
		result = append(result, r)
	}
	return result, rows.Err()
}
func match(r Record, f Filter) bool {
	if f.Since != nil && r.Time.Before(*f.Since) || f.Until != nil && !r.Time.Before(*f.Until) || len(f.Services) > 0 && !contains(f.Services, r.Service) || len(f.Events) > 0 && !contains(f.Events, r.Event) || f.User != nil && *f.User != r.User || f.RequestID != nil && *f.RequestID != r.RequestID {
		return false
	}
	var attrs map[string]json.RawMessage
	if err := json.Unmarshal(r.Attrs, &attrs); err != nil {
		return false
	}
	for k, v := range f.Attrs {
		// The shared contract validates and normalizes named basic values.
		e := telemetry.Event{Time: r.Time, Service: "filter", Name: "filter.checked", Attrs: telemetry.Attrs{k: v}}
		data, err := e.MarshalJSON()
		if err != nil {
			return false
		}
		var envelope struct {
			Attrs map[string]json.RawMessage `json:"attrs"`
		}
		if err := json.Unmarshal(data, &envelope); err != nil || !bytes.Equal(envelope.Attrs[k], attrs[k]) {
			return false
		}
	}
	return true
}
func contains(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}
func encode(ts, id int64) Cursor {
	var position bytes.Buffer
	// bytes.Buffer writes cannot fail.
	_ = binary.Write(&position, binary.BigEndian, ts)
	_ = binary.Write(&position, binary.BigEndian, id)
	_ = binary.Write(&position, binary.BigEndian, crc32.ChecksumIEEE(position.Bytes()))
	return Cursor(base64.RawURLEncoding.Strict().EncodeToString(position.Bytes()))
}
func decode(c Cursor) (int64, int64, error) {
	b, err := base64.RawURLEncoding.Strict().DecodeString(string(c))
	if err != nil || len(b) != 20 {
		return 0, 0, ErrCursor
	}
	if binary.BigEndian.Uint32(b[16:]) != crc32.ChecksumIEEE(b[:16]) {
		return 0, 0, ErrCursor
	}
	var ts, id int64
	r := bytes.NewReader(b[:16])
	if err := binary.Read(r, binary.BigEndian, &ts); err != nil {
		return 0, 0, err
	}
	if err := binary.Read(r, binary.BigEndian, &id); err != nil {
		return 0, 0, err
	}
	return ts, id, nil
}

// Search returns records in reverse chronological arrival order.
func (s *Store) Search(ctx context.Context, f Filter, limit int, after Cursor) (Page, error) {
	if limit < 1 {
		return Page{}, errors.New("limit must be positive")
	}
	var ts, id int64
	var err error
	if after != "" {
		ts, id, err = decode(after)
		if err != nil {
			return Page{}, err
		}
	}
	records, err := s.records(ctx)
	if err != nil {
		return Page{}, err
	}
	p := Page{Records: make([]Record, 0)}
	var last stored
	for _, r := range records {
		if after != "" && (r.Time.UnixMicro() > ts || r.Time.UnixMicro() == ts && r.id >= id) || !match(r.Record, f) {
			continue
		}
		if len(p.Records) == limit {
			p.Next = encode(last.Time.UnixMicro(), last.id)
			break
		}
		p.Records = append(p.Records, r.Record)
		last = r
	}
	return p, nil
}

// Trace returns a request's records in chronological arrival order.
func (s *Store) Trace(ctx context.Context, requestID string) ([]Record, error) {
	records, err := s.records(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]Record, 0)
	for i := len(records) - 1; i >= 0; i-- {
		if records[i].RequestID == requestID {
			result = append(result, records[i].Record)
		}
	}
	return result, nil
}

// Count returns the matching population.
func (s *Store) Count(ctx context.Context, f Filter) (int64, error) {
	records, err := s.records(ctx)
	if err != nil {
		return 0, err
	}
	var n int64
	for _, r := range records {
		if match(r.Record, f) {
			n++
		}
	}
	return n, nil
}

// Catalog summarizes services and event names.
func (s *Store) Catalog(ctx context.Context, service, event string) ([]CatalogEntry, error) {
	records, err := s.records(ctx)
	if err != nil {
		return nil, err
	}
	entries := map[string]map[string]*EventEntry{}
	keys := map[*EventEntry]map[string]bool{}
	for _, r := range records {
		if service != "" && r.Service != service || event != "" && r.Event != event {
			continue
		}
		if entries[r.Service] == nil {
			entries[r.Service] = map[string]*EventEntry{}
		}
		e := entries[r.Service][r.Event]
		if e == nil {
			e = &EventEntry{Event: r.Event, LastSeen: r.Time, Attrs: make([]string, 0)}
			entries[r.Service][r.Event] = e
			keys[e] = map[string]bool{}
		}
		e.Count++
		var attrs map[string]json.RawMessage
		if err := json.Unmarshal(r.Attrs, &attrs); err != nil {
			return nil, err
		}
		for k := range attrs {
			keys[e][k] = true
		}
	}
	result := make([]CatalogEntry, 0)
	for svc, events := range entries {
		c := CatalogEntry{Service: svc, Events: make([]EventEntry, 0)}
		for _, e := range events {
			for k := range keys[e] {
				e.Attrs = append(e.Attrs, k)
			}
			sort.Strings(e.Attrs)
			c.Events = append(c.Events, *e)
		}
		sort.Slice(c.Events, func(i, j int) bool { return c.Events[i].Event < c.Events[j].Event })
		result = append(result, c)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Service < result[j].Service })
	return result, nil
}

// CountBy returns matching populations grouped by field, bucket, or attribute.
func (s *Store) CountBy(ctx context.Context, f Filter, by GroupBy) (int64, []Group, error) {
	isTime := by == ByMinute || by == ByHour || by == ByDay
	isAttr := strings.HasPrefix(string(by), AttrPrefix) && len(string(by)) > len(AttrPrefix)
	if !isTime && !isAttr && by != ByService && by != ByEvent && by != ByUser && by != ByRequestID {
		return 0, nil, ErrGroupBy
	}
	records, err := s.records(ctx)
	if err != nil {
		return 0, nil, err
	}
	var total int64
	groups := map[string]int64{}
	for _, r := range records {
		if !match(r.Record, f) {
			continue
		}
		total++
		var key string
		switch by {
		case ByService:
			key = r.Service
		case ByEvent:
			key = r.Event
		case ByUser:
			key = r.User
		case ByRequestID:
			key = r.RequestID
		case ByMinute:
			key = r.Time.Truncate(time.Minute).Format("2006-01-02T15:04:05Z")
		case ByHour:
			key = r.Time.Truncate(time.Hour).Format("2006-01-02T15:04:05Z")
		case ByDay:
			key = time.Date(r.Time.Year(), r.Time.Month(), r.Time.Day(), 0, 0, 0, 0, time.UTC).Format("2006-01-02T15:04:05Z")
		default:
			var attrs map[string]json.RawMessage
			if err := json.Unmarshal(r.Attrs, &attrs); err != nil {
				return 0, nil, err
			}
			value, ok := attrs[strings.TrimPrefix(string(by), AttrPrefix)]
			if !ok {
				continue
			}
			key = string(value)
			if len(value) > 0 && value[0] == '"' {
				if err := json.Unmarshal(value, &key); err != nil {
					return 0, nil, err
				}
			}
		}
		groups[key]++
	}
	result := make([]Group, 0, len(groups))
	for k, n := range groups {
		result = append(result, Group{k, n})
	}
	sort.Slice(result, func(i, j int) bool {
		if isTime || result[i].Count == result[j].Count {
			return result[i].Key < result[j].Key
		}
		return result[i].Count > result[j].Count
	})
	return total, result, nil
}
