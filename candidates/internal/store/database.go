package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/ikigenba/ikigenba/candidates/internal/sources"
)

type records struct {
	candidates  map[string]Candidate
	links       map[string]Link
	estimates   map[string][]sources.Estimate
	assessments map[string]Assessment
	rulings     []Ruling
	passes      map[string]Pass
	zips        map[string]int64
}
type estimateRecord struct {
	Candidate string
	sources.Estimate
}
type column struct{ name, field string }

var candidateColumns = columns("id:ID status:Status merged_into:MergedInto name:Name names:Names street:Street city:City state:State zip:Zip county:County phone:Phone email:Email people:People class:Class cargo:Cargo created:Created updated:Updated")
var linkColumns = columns("candidate:Candidate scraper:Scraper source_id:SourceID run:Run digest:Digest reading:Reading reason:Reason confidence:Confidence first_seen:FirstSeen last_seen:LastSeen gone:Gone")
var estimateColumns = columns("candidate:Candidate source:Source low:Low high:High confidence:Confidence facts:Facts filed:Filed formula:Formula")
var assessmentColumns = columns("candidate:Candidate fleet:Fleet low:Low high:High confidence:Confidence filed:Filed minutes:Minutes exclusions:Exclusions excluded:Excluded base:Base rules:Rules scored:Scored")
var rulingColumns = columns("kind:Kind candidate:Candidate other:Other keys:Keys why:Why user_id:UserID user_email:UserEmail at:At")
var passColumns = columns("id:ID trigger:Trigger status:Status reason:Reason started:Started finished:Finished rows:Rows new:New changed:Changed unchanged:Unchanged gone:Gone created:Created linked:Linked rescored:Rescored unranged:Unranged user_id:UserID user_email:UserEmail request_id:RequestID delivery:Delivery")

func columns(s string) []column {
	out := []column{}
	for _, part := range strings.Fields(s) {
		p := strings.Split(part, ":")
		out = append(out, column{p[0], p[1]})
	}
	return out
}
func quoted(cs []column) string {
	a := []string{}
	for _, c := range cs {
		a = append(a, `"`+c.name+`"`)
	}
	return strings.Join(a, ",")
}
func readRecords[T any](ctx context.Context, tx *sql.Tx, table string, cs []column, order string, args ...any) ([]T, error) {
	rows, err := querySQL(ctx, tx, strings.Join([]string{"SELECT ", quoted(cs), " FROM ", table, order}, ""), args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []T{}
	for rows.Next() {
		var v T
		rv := reflect.ValueOf(&v).Elem()
		vals := make([]any, len(cs))
		ptrs := make([]any, len(cs))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err = rows.Scan(ptrs...); err != nil {
			return nil, err
		}
		for i, c := range cs {
			if err = decodeValue(rv.FieldByName(c.field), vals[i]); err != nil {
				return nil, err
			}
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

var timeType = reflect.TypeFor[time.Time]()

func decodeValue(v reflect.Value, x any) error {
	if v.Type() == timeType {
		s, ok := x.(string)
		if !ok {
			return fmt.Errorf("invalid time %T", x)
		}
		if s == "" {
			v.Set(reflect.ValueOf(time.Time{}))
			return nil
		}
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			return err
		}
		v.Set(reflect.ValueOf(t.UTC()))
		return nil
	}
	if v.Kind() == reflect.Pointer {
		if x == nil {
			return nil
		}
		v.Set(reflect.New(v.Type().Elem()))
		return decodeValue(v.Elem(), x)
	}
	switch v.Kind() {
	case reflect.String:
		s, ok := x.(string)
		if !ok {
			return fmt.Errorf("invalid string %T", x)
		}
		v.SetString(s)
	case reflect.Int64:
		n, ok := x.(int64)
		if !ok {
			return fmt.Errorf("invalid integer %T", x)
		}
		v.SetInt(n)
	case reflect.Float64:
		switch n := x.(type) {
		case float64:
			v.SetFloat(n)
		case int64:
			v.SetFloat(float64(n))
		default:
			return fmt.Errorf("invalid float %T", x)
		}
	case reflect.Bool:
		n, ok := x.(int64)
		if !ok {
			return fmt.Errorf("invalid bool %T", x)
		}
		v.SetBool(n != 0)
	default:
		s, ok := x.(string)
		if !ok {
			return fmt.Errorf("invalid JSON %T", x)
		}
		return json.Unmarshal([]byte(s), v.Addr().Interface())
	}
	return nil
}
func encodeValue(v reflect.Value) (any, error) {
	if v.Type() == timeType {
		t := v.Interface().(time.Time)
		if t.IsZero() {
			return "", nil
		}
		return t.UTC().Format(time.RFC3339), nil
	}
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return nil, nil
		}
		return encodeValue(v.Elem())
	}
	switch v.Kind() {
	case reflect.String, reflect.Int64, reflect.Float64, reflect.Bool:
		return v.Interface(), nil
	default:
		b, err := json.Marshal(v.Interface())
		return string(b), err
	}
}
func insertRecord(ctx context.Context, tx *sql.Tx, table string, cs []column, v any, extra map[string]any) error {
	rv := reflect.ValueOf(v)
	values := []any{}
	names := []string{}
	for _, c := range cs {
		x, err := encodeValue(rv.FieldByName(c.field))
		if err != nil {
			return err
		}
		values = append(values, x)
		names = append(names, `"`+c.name+`"`)
	}
	extraKeys := make([]string, 0, len(extra))
	for k := range extra {
		extraKeys = append(extraKeys, k)
	}
	sort.Strings(extraKeys)
	for _, k := range extraKeys {
		v := extra[k]
		names = append(names, `"`+k+`"`)
		values = append(values, v)
	}
	marks := strings.TrimSuffix(strings.Repeat("?,", len(values)), ",")
	updates := make([]string, len(names))
	for i, name := range names {
		updates[i] = strings.Join([]string{name, "=excluded.", name}, "")
	}
	err := execSQL(ctx, tx, strings.Join([]string{"INSERT INTO ", table, " (", strings.Join(names, ","), ") VALUES (", marks, ") ON CONFLICT DO UPDATE SET ", strings.Join(updates, ",")}, ""), values...)
	return err
}
func load(ctx context.Context, tx *sql.Tx) (*records, error) {
	r := &records{candidates: map[string]Candidate{}, links: map[string]Link{}, estimates: map[string][]sources.Estimate{}, assessments: map[string]Assessment{}, rulings: []Ruling{}, passes: map[string]Pass{}, zips: map[string]int64{}}
	cs, err := readRecords[Candidate](ctx, tx, "candidates", candidateColumns, "")
	if err != nil {
		return nil, err
	}
	for _, c := range cs {
		r.candidates[c.ID] = c
	}
	ls, err := readRecords[Link](ctx, tx, "links", linkColumns, "")
	if err != nil {
		return nil, err
	}
	for _, l := range ls {
		r.links[sources.SourceKey(l.Scraper, l.SourceID)] = l
	}
	es, err := readRecords[estimateRecord](ctx, tx, "estimates", estimateColumns, " ORDER BY source")
	if err != nil {
		return nil, err
	}
	for _, e := range es {
		r.estimates[e.Candidate] = append(r.estimates[e.Candidate], e.Estimate)
	}
	as, err := readRecords[Assessment](ctx, tx, "assessments", assessmentColumns, "")
	if err != nil {
		return nil, err
	}
	for _, a := range as {
		r.assessments[a.Candidate] = a
	}
	r.rulings, err = readRecords[Ruling](ctx, tx, "decisions", rulingColumns, " ORDER BY seq")
	if err != nil {
		return nil, err
	}
	ps, err := readRecords[Pass](ctx, tx, "passes", passColumns, "")
	if err != nil {
		return nil, err
	}
	for _, p := range ps {
		r.passes[p.ID] = p
	}
	rows, err := querySQL(ctx, tx, "SELECT zip,minutes FROM zip_drive")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var z string
		var n int64
		if err = rows.Scan(&z, &n); err != nil {
			return nil, err
		}
		r.zips[z] = n
	}
	return r, rows.Err()
}
func cloneRecords(r *records) (*records, error) {
	// Keep a separate before-image so the writer touches only changed records.
	copyMap := func(src, dst any) error {
		b, err := json.Marshal(src)
		if err != nil {
			return err
		}
		return json.Unmarshal(b, dst)
	}
	old := &records{}
	for _, pair := range [][2]any{{r.candidates, &old.candidates}, {r.links, &old.links}, {r.estimates, &old.estimates}, {r.assessments, &old.assessments}, {r.rulings, &old.rulings}, {r.passes, &old.passes}, {r.zips, &old.zips}} {
		if err := copyMap(pair[0], pair[1]); err != nil {
			return nil, err
		}
	}
	return old, nil
}
func deleteRecord(ctx context.Context, tx *sql.Tx, table, where string, args ...any) error {
	err := execSQL(ctx, tx, strings.Join([]string{"DELETE FROM ", table, " WHERE ", where}, ""), args...)
	return err
}
func save(ctx context.Context, tx *sql.Tx, old, r *records) error {
	for id, c := range r.candidates {
		if reflect.DeepEqual(old.candidates[id], c) {
			continue
		}
		if err := insertRecord(ctx, tx, "candidates", candidateColumns, c, map[string]any{"names_lower": strings.ToLower(strings.Join(c.Names, "\n"))}); err != nil {
			return err
		}
	}
	for key, l := range r.links {
		if reflect.DeepEqual(old.links[key], l) {
			continue
		}
		if err := insertRecord(ctx, tx, "links", linkColumns, l, map[string]any{"name_key": l.Reading.NameKey, "state": l.Reading.State, "zip": l.Reading.Zip, "phone_key": l.Reading.PhoneKey, "domain_key": l.Reading.DomainKey}); err != nil {
			return err
		}
	}
	for id, es := range r.estimates {
		if reflect.DeepEqual(old.estimates[id], es) {
			continue
		}
		next := map[string]sources.Estimate{}
		for _, e := range es {
			next[e.Source] = e
		}
		for _, e := range old.estimates[id] {
			if _, ok := next[e.Source]; !ok {
				if err := deleteRecord(ctx, tx, "estimates", "candidate=? AND source=?", id, e.Source); err != nil {
					return err
				}
			}
		}
		for _, e := range es {
			if err := insertRecord(ctx, tx, "estimates", estimateColumns, estimateRecord{id, e}, nil); err != nil {
				return err
			}
		}
	}
	for id, a := range r.assessments {
		if reflect.DeepEqual(old.assessments[id], a) {
			continue
		}
		if err := insertRecord(ctx, tx, "assessments", assessmentColumns, a, nil); err != nil {
			return err
		}
	}
	for i := len(old.rulings); i < len(r.rulings); i++ {
		if err := insertRecord(ctx, tx, "decisions", rulingColumns, r.rulings[i], map[string]any{"seq": i + 1}); err != nil {
			return err
		}
	}
	for id := range old.passes {
		if _, ok := r.passes[id]; !ok {
			if err := deleteRecord(ctx, tx, "passes", "id=?", id); err != nil {
				return err
			}
		}
	}
	for id, p := range r.passes {
		if reflect.DeepEqual(old.passes[id], p) {
			continue
		}
		if err := insertRecord(ctx, tx, "passes", passColumns, p, nil); err != nil {
			return err
		}
	}
	for z := range old.zips {
		if _, ok := r.zips[z]; !ok {
			if err := deleteRecord(ctx, tx, "zip_drive", "zip=?", z); err != nil {
				return err
			}
		}
	}
	for z, n := range r.zips {
		if oldN, ok := old.zips[z]; ok && oldN == n {
			continue
		}
		if err := execSQL(ctx, tx, "INSERT INTO zip_drive(zip,minutes) VALUES (?,?) ON CONFLICT(zip) DO UPDATE SET minutes=excluded.minutes", z, n); err != nil {
			return err
		}
	}
	return nil
}
func (s *Store) transact(ctx context.Context, write bool, fn func(*records) error) error {
	f := s.d.Read
	if write {
		f = s.d.Write
	}
	return unreachable(f(ctx, func(tx *sql.Tx) error {
		r, err := load(ctx, tx)
		if err != nil {
			return err
		}
		var old *records
		if write {
			old, err = cloneRecords(r)
			if err != nil {
				return err
			}
		}
		if err = fn(r); err != nil {
			return err
		}
		if write {
			return save(ctx, tx, old, r)
		}
		return nil
	}))
}
