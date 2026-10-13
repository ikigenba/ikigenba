// Package store owns the persistent event trail.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
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

// Position is a record's database sort key.
type Position struct{ TS, ID int64 }

// Page is one search result.
type Page struct {
	Records []Record
	Next    *Position
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

// ErrGroupBy reports an unknown grouping.
var ErrGroupBy = errors.New("invalid grouping")

// Store holds the trail over an appkit database handle.
type Store struct{ db *db.DB }

// New builds a store over the caller-owned database handle.
func New(d *db.DB) *Store { return &Store{db: d} }

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
	return s.db.Write(ctx, func(tx *sql.Tx) error {
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
		return nil
	})
}

// Sweep removes records older than before in bounded transactions.
func (s *Store) Sweep(ctx context.Context, before time.Time) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		var n int64
		err := s.db.Write(ctx, func(tx *sql.Tx) error {
			cutoff := before.UnixMicro()
			if before.Nanosecond()%1000 != 0 {
				cutoff++
			}
			if _, err := tx.ExecContext(ctx, "DELETE FROM attrs WHERE record_id IN (SELECT id FROM records WHERE ts < ? ORDER BY id LIMIT 500)", cutoff); err != nil {
				return err
			}
			res, err := tx.ExecContext(ctx, "DELETE FROM records WHERE id IN (SELECT id FROM records WHERE ts < ? ORDER BY id LIMIT 500)", cutoff)
			if err != nil {
				return err
			}
			n, err = res.RowsAffected()
			return err
		})
		if err != nil {
			return err
		}
		if n < 500 {
			return nil
		}
	}
}

// filterSQL expresses every selector in the database query.
func filterSQL(f Filter) (string, []any) {
	clauses := []string{"1=1"}
	args := make([]any, 0)
	add := func(clause string, value any) { clauses = append(clauses, clause); args = append(args, value) }
	ceilMicro := func(t time.Time) int64 {
		n := t.UnixMicro()
		if t.Nanosecond()%1000 != 0 {
			n++
		}
		return n
	}
	if f.Since != nil {
		add("r.ts >= ?", ceilMicro(*f.Since))
	}
	if f.Until != nil {
		add("r.ts < ?", ceilMicro(*f.Until))
	}
	for _, set := range []struct {
		column string
		values []string
	}{{"svc", f.Services}, {"ev", f.Events}} {
		if len(set.values) != 0 {
			clauses = append(clauses, "r."+set.column+" IN ("+strings.TrimSuffix(strings.Repeat("?,", len(set.values)), ",")+")")
			for _, value := range set.values {
				args = append(args, value)
			}
		}
	}
	if f.User != nil {
		add("r.user = ?", *f.User)
	}
	if f.RequestID != nil {
		add("r.req = ?", *f.RequestID)
	}
	keys := make([]string, 0, len(f.Attrs))
	for key := range f.Attrs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		// Event normalizes named basic types and rejects invalid attribute values.
		e := telemetry.Event{Time: time.Unix(0, 0).UTC(), Service: "filter", Name: "filter.checked", Attrs: telemetry.Attrs{key: f.Attrs[key]}}
		body, err := e.MarshalJSON()
		if err != nil {
			clauses = append(clauses, "0")
			continue
		}
		var envelope struct {
			Attrs map[string]json.RawMessage `json:"attrs"`
		}
		if err := json.Unmarshal(body, &envelope); err != nil {
			clauses = append(clauses, "0")
			continue
		}
		clauses = append(clauses, "EXISTS (SELECT 1 FROM attrs a WHERE a.record_id = r.id AND a.key = ? AND a.value = ?)")
		args = append(args, key, string(envelope.Attrs[key]))
	}
	return strings.Join(clauses, " AND "), args
}

func scanRecord(rows *sql.Rows) (Record, Position, error) {
	var r Record
	var p Position
	var attrs string
	err := rows.Scan(&p.ID, &p.TS, &r.Service, &r.Event, &r.RequestID, &r.User, &attrs)
	r.Time = time.UnixMicro(p.TS).UTC()
	r.Attrs = json.RawMessage(attrs)
	return r, p, err
}

const recordColumns = "r.id,r.ts,r.svc,r.ev,r.req,r.user,r.attrs"

// Search returns one SQL window in reverse chronological arrival order.
func (s *Store) Search(ctx context.Context, f Filter, limit int, after *Position) (Page, error) {
	if limit < 1 {
		return Page{}, errors.New("limit must be positive")
	}
	where, args := filterSQL(f)
	if after != nil {
		where += " AND (r.ts,r.id) < (?,?)"
		args = append(args, after.TS, after.ID)
	}
	// Use an int64 limit to avoid overflowing an int at its maximum.
	window := int64(limit)
	if window < int64(^uint64(0)>>1) {
		window++
	}
	args = append(args, window)
	p := Page{Records: make([]Record, 0)}
	err := s.db.Read(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, strings.Join([]string{"SELECT ", recordColumns, " FROM records r WHERE ", where, " ORDER BY r.ts DESC,r.id DESC LIMIT ?"}, ""), args...)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		var last Position
		for rows.Next() {
			r, pos, err := scanRecord(rows)
			if err != nil {
				return err
			}
			if len(p.Records) == limit {
				p.Next = &last
				break
			}
			p.Records = append(p.Records, r)
			last = pos
		}
		return rows.Err()
	})
	if err != nil {
		return Page{}, err
	}
	return p, nil
}

// Trace returns a request's records in chronological arrival order.
func (s *Store) Trace(ctx context.Context, requestID string) ([]Record, error) {
	result := make([]Record, 0)
	err := s.db.Read(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, strings.Join([]string{"SELECT ", recordColumns, " FROM records r WHERE r.req = ? ORDER BY r.ts,r.id"}, ""), requestID)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			r, _, err := scanRecord(rows)
			if err != nil {
				return err
			}
			result = append(result, r)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// Count returns the matching population.
func (s *Store) Count(ctx context.Context, f Filter) (int64, error) {
	where, args := filterSQL(f)
	var total int64
	err := s.db.Read(ctx, func(tx *sql.Tx) error {
		return tx.QueryRowContext(ctx, strings.Join([]string{"SELECT count(*) FROM records r WHERE ", where}, ""), args...).Scan(&total)
	})
	if err != nil {
		return 0, err
	}
	return total, nil
}

// Catalog summarizes services and event names using aggregate queries.
func (s *Store) Catalog(ctx context.Context, service, event string) ([]CatalogEntry, error) {
	f := Filter{}
	if service != "" {
		f.Services = []string{service}
	}
	if event != "" {
		f.Events = []string{event}
	}
	where, args := filterSQL(f)
	result := make([]CatalogEntry, 0)
	err := s.db.Read(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, strings.Join([]string{"SELECT r.svc,r.ev,count(*),max(r.ts) FROM records r WHERE ", where, " GROUP BY r.svc,r.ev ORDER BY r.svc,r.ev"}, ""), args...)
		if err != nil {
			return err
		}
		for rows.Next() {
			var svc string
			var e EventEntry
			var ts int64
			if err := rows.Scan(&svc, &e.Event, &e.Count, &ts); err != nil {
				_ = rows.Close()
				return err
			}
			e.LastSeen = time.UnixMicro(ts).UTC()
			e.Attrs = make([]string, 0)
			if len(result) == 0 || result[len(result)-1].Service != svc {
				result = append(result, CatalogEntry{Service: svc, Events: make([]EventEntry, 0)})
			}
			c := &result[len(result)-1]
			c.Events = append(c.Events, e)
		}
		err = rows.Err()
		closeErr := rows.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		rows, err = tx.QueryContext(ctx, strings.Join([]string{"SELECT DISTINCT r.svc,r.ev,a.key FROM records r JOIN attrs a ON a.record_id=r.id WHERE ", where, " ORDER BY r.svc,r.ev,a.key"}, ""), args...)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		ci, ei := 0, 0
		for rows.Next() {
			var svc, ev, key string
			if err := rows.Scan(&svc, &ev, &key); err != nil {
				return err
			}
			for result[ci].Service != svc {
				ci++
				ei = 0
			}
			for result[ci].Events[ei].Event != ev {
				ei++
			}
			e := &result[ci].Events[ei]
			e.Attrs = append(e.Attrs, key)
		}
		return rows.Err()
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}

// CountBy returns SQL aggregates grouped by field, bucket, or attribute.
func (s *Store) CountBy(ctx context.Context, f Filter, by GroupBy) (int64, []Group, error) {
	expression := ""
	join := ""
	order := "count(*) DESC,group_key ASC"
	groupArgs := make([]any, 0)
	switch by {
	case ByService:
		expression = "r.svc"
	case ByEvent:
		expression = "r.ev"
	case ByUser:
		expression = "r.user"
	case ByRequestID:
		expression = "r.req"
	case ByMinute, ByHour, ByDay:
		width := int64(time.Minute / time.Microsecond)
		if by == ByHour {
			width = int64(time.Hour / time.Microsecond)
		}
		if by == ByDay {
			width = int64(24 * time.Hour / time.Microsecond)
		}
		expression = fmt.Sprintf("strftime('%%Y-%%m-%%dT%%H:%%M:%%SZ',(r.ts-((r.ts %% %d + %d) %% %d))/1000000,'unixepoch')", width, width, width)
		order = "group_key ASC"
	default:
		if !strings.HasPrefix(string(by), AttrPrefix) || len(string(by)) == len(AttrPrefix) {
			return 0, nil, ErrGroupBy
		}
		expression = "CASE WHEN json_type(a.value)='text' THEN json_extract(a.value,'$') ELSE a.value END"
		join = " JOIN attrs a ON a.record_id=r.id AND a.key=?"
		groupArgs = append(groupArgs, strings.TrimPrefix(string(by), AttrPrefix))
	}
	where, args := filterSQL(f)
	result := make([]Group, 0)
	var total int64
	err := s.db.Read(ctx, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, strings.Join([]string{"SELECT count(*) FROM records r WHERE ", where}, ""), args...).Scan(&total); err != nil {
			return err
		}
		groupArgs = append(groupArgs, args...)
		rows, err := tx.QueryContext(ctx, strings.Join([]string{"SELECT ", expression, " AS group_key,count(*) FROM records r", join, " WHERE ", where, " GROUP BY group_key ORDER BY ", order}, ""), groupArgs...)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var g Group
			if err := rows.Scan(&g.Key, &g.Count); err != nil {
				return err
			}
			result = append(result, g)
		}
		return rows.Err()
	})
	if err != nil {
		return 0, nil, err
	}
	return total, result, nil
}
