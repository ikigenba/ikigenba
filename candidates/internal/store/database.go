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
	owners     []OwnerRecord
	candidates map[string]Candidate
	links      map[string]Link
	estimates  map[string][]sources.Estimate
	judgments  map[string]Judgments
	rulings    []Ruling
	passes     map[string]Pass
	zips       map[string]int64
}
type estimateRecord struct {
	Candidate string
	sources.Estimate
}
type column struct{ name, field string }

const candidateColumnNames = "id,status,merged_into,name,names,street,city,state,zip,county,phone,email,people,class,cargo,created,updated,name_source,name_record,address_source,address_record,phone_source,phone_record,email_source,email_record,other_names,other_addresses,other_phones,other_emails"

var candidateColumns = mappedColumns(candidateColumnNames, "ID Status MergedInto Name Names Street City State Zip County Phone Email People Class Cargo Created Updated NameSource NameRecord AddressSource AddressRecord PhoneSource PhoneRecord EmailSource EmailRecord OtherNames OtherAddresses OtherPhones OtherEmails")
var linkColumns = columns("candidate:Candidate scraper:Scraper source_id:SourceID run:Run digest:Digest reading:Reading reason:Reason confidence:Confidence first_seen:FirstSeen last_seen:LastSeen gone:Gone walk:Walk park:Park")
var estimateColumns = columns("candidate:Candidate source:Source low:Low high:High confidence:Confidence facts:Facts filed:Filed formula:Formula")
var judgmentsColumns = columns("candidate:Candidate operating:Operating operating_key:OperatingKey evidence:Evidence revenue_low:RevenueLow revenue_high:RevenueHigh revenue_confidence:RevenueConfidence headcount_low:HeadcountLow headcount_high:HeadcountHigh headcount_basis:HeadcountBasis headcount_series:HeadcountSeries trend:Trend trend_kind:TrendKind trend_from_year:TrendFromYear trend_to_year:TrendToYear trend_from:TrendFrom trend_to:TrendTo trend_change:TrendChange excluded:Excluded rules:Rules calculated:Calculated")
var rulingColumns = columns("kind:Kind candidate:Candidate other:Other keys:Keys why:Why user_id:UserID user_email:UserEmail at:At")
var passColumns = columns("id:ID kind:Kind origin:Origin trigger:Trigger target:Target enrich_kind:EnrichKind scraper:Scraper parcel_run:ParcelRun status:Status reason:Reason started:Started finished:Finished rows:Rows new:New changed:Changed unchanged:Unchanged gone:Gone created:Created linked:Linked judged:Judged unranged:Unranged derived:Derived due:Due fetched:Fetched https:HTTPS http:HTTP none:None runs:Runs runs_failed:RunsFailed pages:Pages cost:Cost parcels_read:ParcelsRead by_label:ByLabel by_value:ByValue by_candidate:ByCandidate parks_created:ParksCreated parks_updated:ParksUpdated parks_gone:ParksGone parks_grown:ParksGrown parks_walked:ParksWalked circles:Circles requests:Requests found:Found new_candidates:NewCandidates search_different:SearchDifferent search_stands:SearchStands search_none:SearchNone ceiling_reached:CeilingReached user_id:UserID user_email:UserEmail request_id:RequestID delivery:Delivery")

func columns(s string) []column {
	out := []column{}
	for _, part := range strings.Fields(s) {
		p := strings.Split(part, ":")
		out = append(out, column{p[0], p[1]})
	}
	return out
}
func mappedColumns(names, fields string) []column {
	ns, fs := strings.Split(names, ","), strings.Fields(fields)
	out := make([]column, len(ns))
	for i, name := range ns {
		out[i] = column{name, fs[i]}
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
	return readQueryRecords[T](ctx, tx, cs, strings.Join([]string{"SELECT ", quoted(cs), " FROM ", table, order}, ""), args...)
}

func readQueryRecords[T any](ctx context.Context, tx *sql.Tx, cs []column, query string, args ...any) ([]T, error) {
	rows, err := querySQL(ctx, tx, query, args...)
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
		if c, ok := any(&v).(*Candidate); ok {
			c.Created = c.Created.UTC().Truncate(time.Second)
			c.Updated = c.Updated.UTC().Truncate(time.Second)
			if c.OtherNames == nil {
				c.OtherNames = []sources.Sourced[string]{}
			}
			if c.OtherAddresses == nil {
				c.OtherAddresses = []sources.Sourced[sources.Address]{}
			}
			if c.OtherPhones == nil {
				c.OtherPhones = []sources.Sourced[string]{}
			}
			if c.OtherEmails == nil {
				c.OtherEmails = []sources.Sourced[string]{}
			}
		}
		out = append(out, v)
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if err = rows.Close(); err != nil {
		return nil, err
	}
	if len(out) > 0 {
		if _, ok := any(&out[0]).(*Candidate); ok {
			ctx = ownerReadContext(ctx)
		}
	}
	for i := range out {
		if c, ok := any(&out[i]).(*Candidate); ok {
			if err = withCandidateEIN(ctx, tx, c); err != nil {
				return nil, err
			}
		}
	}
	return out, nil
}

type storedHeadcount struct {
	Scraper string `json:"scraper"`
	Kind    string `json:"kind"`
	Count   int64  `json:"count"`
	Date    string `json:"date,omitempty"`
	Hours   *int64 `json:"hours,omitempty"`
}

type storedPerson struct {
	Name, Role, Source, Key string
}

var peopleType = reflect.TypeFor[[]sources.Sourced[sources.Person]]()

var headcountsType = reflect.TypeFor[[]sources.Headcount]()
var timeType = reflect.TypeFor[time.Time]()

func decodeValue(v reflect.Value, x any) error {
	if v.Type() == peopleType {
		text, ok := x.(string)
		if !ok {
			return fmt.Errorf("invalid people %T", x)
		}
		var rows []storedPerson
		if err := json.Unmarshal([]byte(text), &rows); err != nil {
			return err
		}
		out := make([]sources.Sourced[sources.Person], 0, len(rows))
		for _, row := range rows {
			out = append(out, sources.Sourced[sources.Person]{Value: sources.Person{Name: row.Name, Role: row.Role}, Source: row.Source, Key: row.Key})
		}
		v.Set(reflect.ValueOf(out))
		return nil
	}

	if v.Type() == headcountsType {
		text, ok := x.(string)
		if !ok {
			return fmt.Errorf("invalid headcounts %T", x)
		}
		var rows []storedHeadcount
		if err := json.Unmarshal([]byte(text), &rows); err != nil {
			return err
		}
		hs := make([]sources.Headcount, 0, len(rows))
		for _, row := range rows {
			var at time.Time
			if row.Date != "" {
				var err error
				at, err = time.Parse(time.RFC3339, row.Date)
				if err != nil {
					return err
				}
			}
			hs = append(hs, sources.Headcount{Scraper: row.Scraper, Kind: row.Kind, Count: row.Count, AsOf: at.UTC(), Hours: row.Hours})
		}
		v.Set(reflect.ValueOf(hs))
		return nil
	}
	if x == nil && v.Type() == timeType {
		return nil
	}
	if x == nil && v.Kind() == reflect.String {
		return nil
	}
	if x == nil && v.Kind() == reflect.Slice {
		v.Set(reflect.MakeSlice(v.Type(), 0, 0))
		return nil
	}
	if x == nil && (v.Kind() == reflect.Float64 || v.Kind() == reflect.Int64) {
		return nil
	}
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
	case reflect.Int, reflect.Int64:
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
	if v.Type() == peopleType {
		rows := []storedPerson{}
		for _, person := range v.Interface().([]sources.Sourced[sources.Person]) {
			rows = append(rows, storedPerson{Name: person.Value.Name, Role: person.Value.Role, Source: person.Source, Key: person.Key})
		}
		b, err := json.Marshal(rows)
		return string(b), err
	}

	if v.Type() == headcountsType {
		hs := v.Interface().([]sources.Headcount)
		rows := make([]storedHeadcount, 0, len(hs))
		for _, h := range hs {
			row := storedHeadcount{Scraper: h.Scraper, Kind: h.Kind, Count: h.Count, Hours: h.Hours}
			if !h.AsOf.IsZero() {
				row.Date = h.AsOf.UTC().Truncate(time.Second).Format(time.RFC3339)
			}
			rows = append(rows, row)
		}
		b, err := json.Marshal(rows)
		return string(b), err
	}
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
	case reflect.String, reflect.Int, reflect.Int64, reflect.Float64, reflect.Bool:
		return v.Interface(), nil
	default:
		b, err := json.Marshal(v.Interface())
		return string(b), err
	}
}
func insertRecord(ctx context.Context, tx *sql.Tx, table string, cs []column, v any, extra map[string]any) error {
	if table == "passes" {
		if extra == nil {
			extra = map[string]any{}
		}
		extra["rescored"] = int64(0)
	}
	if table == "rejects" || table == "campaign_uses" {
		if extra == nil {
			extra = map[string]any{}
		}
		extra["cohort"] = ""
	}
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
func deleteRecord(ctx context.Context, tx *sql.Tx, table, where string, args ...any) error {
	err := execSQL(ctx, tx, strings.Join([]string{"DELETE FROM ", table, " WHERE ", where}, ""), args...)
	return err
}
