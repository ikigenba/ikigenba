package store

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"math"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/appkit/events"
)

// Producer lists one service's declared attribute names.
type Producer struct {
	Service string
	Attrs   []string
}

// CatalogEntry combines declared capabilities and retained observations.
type CatalogEntry struct {
	Event    string
	Emits    []Producer
	Accepts  []string
	Count    int64
	LastSeen time.Time
}

// Filter restricts retained events by envelope fields and scalar attributes.
type Filter struct {
	Since, Until           *time.Time
	Services, Events       []string
	User, RequestID, Cause *string
	Attrs                  events.Attrs
}

// Cursor is a checked sequence position, independent of filters.
type Cursor string

// Page holds descending retained events and the next position.
type Page struct {
	Records []events.Event
	Next    Cursor
}

// Catalog returns declared and observed event names.
func (s *Store) Catalog(ctx context.Context, service, event string) ([]CatalogEntry, error) {
	result := make([]CatalogEntry, 0)
	err := s.d.Read(ctx, func(tx *sql.Tx) error {
		ds, err := declarations(tx)
		if err != nil {
			return err
		}
		entries := map[string]*CatalogEntry{}
		entry := func(name string) *CatalogEntry {
			if entries[name] == nil {
				entries[name] = &CatalogEntry{Event: name, Emits: []Producer{}, Accepts: []string{}}
			}
			return entries[name]
		}
		services := make([]string, 0, len(ds))
		for svc := range ds {
			services = append(services, svc)
		}
		sort.Strings(services)
		for _, svc := range services {
			seen := map[string]bool{}
			for _, em := range ds[svc].Emits {
				if !seen[em.Event] {
					entry(em.Event).Emits = append(entry(em.Event).Emits, Producer{Service: svc, Attrs: append([]string{}, em.Attrs...)})
					seen[em.Event] = true
				}
			}
		}
		rows, err := tx.Query("SELECT event,COUNT(*),MAX(received) FROM events GROUP BY event")
		if err != nil {
			return err
		}
		for rows.Next() {
			var name string
			var count, last int64
			if err = rows.Scan(&name, &count, &last); err != nil {
				_ = rows.Close()
				return err
			}
			e := entry(name)
			e.Count = count
			e.LastSeen = time.UnixMicro(last).UTC()
		}
		err = rows.Err()
		closeErr := rows.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
		names := make([]string, 0, len(entries))
		for name := range entries {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			e := entries[name]
			for _, svc := range services {
				if accepts(ds[svc], name) {
					e.Accepts = append(e.Accepts, svc)
				}
			}
			if event != "" && event != name {
				continue
			}
			if service != "" {
				d, ok := ds[service]
				keep := ok && accepts(d, name)
				for _, em := range d.Emits {
					keep = keep || em.Event == name
				}
				if !keep {
					continue
				}
			}
			result = append(result, *e)
		}
		return nil
	})
	return result, err
}
func encodeCursor(seq int64) Cursor {
	var b [12]byte
	_, _ = binary.Encode(b[:8], binary.BigEndian, seq)
	binary.BigEndian.PutUint32(b[8:], crc32.ChecksumIEEE(b[:8]))
	return Cursor(base64.RawURLEncoding.Strict().EncodeToString(b[:]))
}
func decodeCursor(c Cursor) (int64, error) {
	b, err := base64.RawURLEncoding.Strict().DecodeString(string(c))
	if err != nil || len(b) != 12 || base64.RawURLEncoding.Strict().EncodeToString(b) != string(c) {
		return 0, ErrCursor
	}
	if crc32.ChecksumIEEE(b[:8]) != binary.BigEndian.Uint32(b[8:]) {
		return 0, ErrCursor
	}
	var seq int64
	if _, err := binary.Decode(b[:8], binary.BigEndian, &seq); err != nil {
		return 0, ErrCursor
	}
	return seq, nil
}
func microsCeil(t time.Time) int64 {
	// Filter and sweep bounds may exceed the range of stored microseconds.
	if t.After(time.UnixMicro(math.MaxInt64)) {
		return math.MaxInt64
	}
	if t.Before(time.UnixMicro(math.MinInt64)) {
		return math.MinInt64
	}
	n := t.UnixMicro()
	if t.After(time.UnixMicro(n)) {
		n++
	}
	return n
}
func scalar(v any) bool {
	if v == nil {
		return false
	}
	value := reflect.ValueOf(v)
	switch value.Kind() {
	case reflect.String, reflect.Bool, reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return true
	case reflect.Float32, reflect.Float64:
		f := value.Float()
		return !math.IsNaN(f) && !math.IsInf(f, 0)
	}
	return false
}

// Search reads a checked page from the retained log.
func (s *Store) Search(ctx context.Context, f Filter, limit int, after Cursor) (Page, error) {
	if limit < 1 {
		return Page{}, fmt.Errorf("limit must be positive")
	}
	var position int64
	var err error
	if after != "" {
		position, err = decodeCursor(after)
		if err != nil {
			return Page{}, err
		}
	}
	where := []string{"1=1"}
	args := []any{}
	add := func(condition string, value any) { where = append(where, condition); args = append(args, value) }
	if after != "" {
		add("seq<?", position)
	}
	if f.Since != nil {
		add("time>=?", microsCeil(*f.Since))
	}
	if f.Until != nil {
		add("time<?", microsCeil(*f.Until))
	}
	for _, filter := range []struct {
		column string
		values []string
	}{{"service", f.Services}, {"event", f.Events}} {
		if len(filter.values) > 0 {
			marks := make([]string, len(filter.values))
			for i, v := range filter.values {
				marks[i] = "?"
				args = append(args, v)
			}
			where = append(where, filter.column+" IN ("+strings.Join(marks, ",")+")")
		}
	}
	for _, filter := range []struct {
		column string
		value  *string
	}{{"user", f.User}, {"request_id", f.RequestID}, {"cause", f.Cause}} {
		if filter.value != nil {
			add(filter.column+"=?", *filter.value)
		}
	}
	keys := make([]string, 0, len(f.Attrs))
	for k := range f.Attrs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := f.Attrs[k]
		if !scalar(v) {
			where = append(where, "0=1")
			continue
		}
		raw, err := json.Marshal(v)
		if err != nil {
			where = append(where, "0=1")
			continue
		}
		where = append(where, "EXISTS (SELECT 1 FROM attrs WHERE attrs.seq=events.seq AND key=? AND value=?)")
		args = append(args, k, string(raw))
	}
	// Fetch one extra row without overflowing a caller's int limit.
	page := Page{Records: make([]events.Event, 0)}
	err = s.d.Read(ctx, func(tx *sql.Tx) error {
		var query strings.Builder
		query.WriteString("SELECT " + eventColumns + " FROM events WHERE ")
		query.WriteString(strings.Join(where, " AND "))
		query.WriteString(" ORDER BY seq DESC")
		rows, err := tx.Query(query.String(), args...)
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			e, err := readEvent(rows)
			if err != nil {
				return err
			}
			if len(page.Records) == limit {
				page.Next = encodeCursor(page.Records[len(page.Records)-1].Seq)
				break
			}
			page.Records = append(page.Records, e)
		}
		return rows.Err()
	})
	if err != nil {
		return Page{}, err
	}
	return page, nil
}
