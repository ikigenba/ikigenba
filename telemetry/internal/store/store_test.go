package store_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"hash/crc32"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/telemetry/internal/store"
)

func open(t *testing.T, source string) *store.Store {
	t.Helper()
	s, err := store.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Error(err)
		}
	})
	return s
}
func event(n string, at time.Time, attrs telemetry.Attrs) telemetry.Event {
	return telemetry.Event{Time: at, Service: "alpha", Name: n + ".record", RequestID: "req", User: "user", Attrs: attrs}
}
func put(t *testing.T, s *store.Store, e telemetry.Event) {
	t.Helper()
	if err := s.Deliver(context.Background(), e); err != nil {
		t.Fatal(err)
	}
}
func page(t *testing.T, s *store.Store, f store.Filter, limit int, c store.Cursor) store.Page {
	t.Helper()
	p, err := s.Search(context.Background(), f, limit, c)
	if err != nil {
		t.Fatal(err)
	}
	return p
}
func number(t *testing.T, s *store.Store, f store.Filter) int64 {
	t.Helper()
	n, err := s.Count(context.Background(), f)
	if err != nil {
		t.Fatal(err)
	}
	return n
}
func wantRecord(t *testing.T, r store.Record, e telemetry.Event) {
	t.Helper()
	b, err := e.MarshalJSON()
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Attrs json.RawMessage `json:"attrs"`
	}
	if err := json.Unmarshal(b, &envelope); err != nil {
		t.Fatal(err)
	}
	want := store.Record{e.Time.UTC().Truncate(time.Microsecond), e.Service, e.Name, e.RequestID, e.User, envelope.Attrs}
	if !reflect.DeepEqual(r, want) {
		t.Fatalf("record = %#v, want %#v", r, want)
	}
}

func TestContractAndMemory(t *testing.T) {
	// R-ULT7-DDU7 R-UN13-R5KW R-UO90-4XBL R-J0MB-S6BP R-J1U8-5Y2E R-J324-JPT3 R-J4A0-XHJS R-J5HX-B9AH R-J6PT-P116 R-J7XQ-2SRV R-J95M-GKIK
	// Unkeyed literals prove exact field order and count by compilation; method assignments prove signatures.
	var sink telemetry.Sink = open(t, ":memory:")
	s := sink.(*store.Store)
	_ = store.Filter{nil, nil, nil, nil, nil, nil, nil}
	_ = store.Page{nil, store.Cursor("")}
	_ = store.CatalogEntry{"", []store.EventEntry{{"", 0, time.Time{}, nil}}}
	_ = store.Group{"", 0}
	if store.ErrCursor == nil || store.ErrGroupBy == nil || errors.Is(store.ErrCursor, store.ErrGroupBy) {
		t.Fatal("sentinels must be nonnil and distinct")
	}
	for by, want := range map[store.GroupBy]string{store.ByService: "service", store.ByEvent: "event", store.ByUser: "user", store.ByRequestID: "request_id", store.ByMinute: "minute", store.ByHour: "hour", store.ByDay: "day"} {
		if string(by) != want {
			t.Fatal(by)
		}
	}
	if store.AttrPrefix != "attrs." {
		t.Fatal(store.AttrPrefix)
	}
	// R-JF94-DF81 R-UQOS-WGSZ R-URWP-A8JO R-JMKI-O1O7 R-3GH0-XHAO
	at := time.Date(1960, 1, 1, 5, 0, 0, 123456789, time.FixedZone("offset", 3600))
	a := event("first", at, telemetry.Attrs{"n": int64(2), "flag": true, "quoted": "x\"y"})
	a.Service = "telemetry"
	a.RequestID = ""
	a.User = ""
	put(t, s, a)
	b := a
	b.Name = "second.record"
	put(t, s, b)
	got := page(t, s, store.Filter{}, 10, "")
	if len(got.Records) != 2 {
		t.Fatal(got)
	}
	wantRecord(t, got.Records[0], b)
	wantRecord(t, got.Records[1], a)
	trace, err := s.Trace(context.Background(), "")
	if err != nil || len(trace) != 2 {
		t.Fatal(trace, err)
	}
	wantRecord(t, trace[0], a)
	wantRecord(t, trace[1], b)
	if err := s.Sweep(context.Background(), at.UTC().Truncate(time.Microsecond)); err != nil {
		t.Fatal(err)
	}
	if number(t, s, store.Filter{}) != 2 {
		t.Fatal("boundary swept")
	}
	if err := s.Sweep(context.Background(), at.UTC().Truncate(time.Microsecond).Add(time.Nanosecond)); err != nil {
		t.Fatal(err)
	}
	if number(t, s, store.Filter{}) != 0 {
		t.Fatal("retained old records")
	}
}

func TestSourcesAndPersistence(t *testing.T) {
	// R-JADI-UC99 R-JBLF-83ZY R-JCTB-LVQN
	for _, source := range []string{"", ":memory:", "file::memory:?cache=shared", "file:isolated?mode=memory&cache=shared"} {
		t.Run(source, func(t *testing.T) {
			s := open(t, source)
			e := event("one", time.Unix(1, 0), telemetry.Attrs{})
			put(t, s, e)
			wantRecord(t, page(t, s, store.Filter{}, 1, "").Records[0], e)
		})
	}
	root := t.TempDir()
	path := filepath.Join(root, "nested", "deep", "trail.db")
	s, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	a := event("a", time.Unix(5, 0), telemetry.Attrs{"x": "a"})
	b := a
	b.Name = "b.record"
	put(t, s, a)
	put(t, s, b)
	for _, p := range []string{filepath.Join(root, "nested"), filepath.Dir(path)} {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0700 {
			t.Fatal(info.Mode())
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s = open(t, path)
	got := page(t, s, store.Filter{}, 5, "")
	wantRecord(t, got.Records[0], b)
	wantRecord(t, got.Records[1], a)
	dir := filepath.Join(root, "existing")
	if err := os.Mkdir(dir, 0750); err != nil {
		t.Fatal(err)
	}
	_ = open(t, filepath.Join(dir, "db"))
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0750 {
		t.Fatal(info.Mode())
	}
}

func TestOpenFailures(t *testing.T) {
	// R-YPG9-AHOS
	root := t.TempDir()
	file := filepath.Join(root, "not-directory")
	data := []byte("original")
	if err := os.WriteFile(file, data, 0600); err != nil {
		t.Fatal(err)
	}
	for _, source := range []string{filepath.Join(file, "database"), file} {
		s, err := store.Open(source)
		if s != nil || err == nil {
			t.Fatalf("Open(%q) = %v,%v", source, s, err)
		}
	}
	got, err := os.ReadFile(filepath.Clean(file))
	if err != nil || !bytes.Equal(got, data) {
		t.Fatal(got, err)
	}
	dir := filepath.Join(root, "locked")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	dbfile := filepath.Join(dir, "db")
	s, err := store.Open(dbfile)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dbfile, 0400); err != nil {
		t.Fatal(err)
	}
	s, err = store.Open(dbfile)
	if s != nil || err == nil || !strings.Contains(strings.ToLower(err.Error()), "readonly") {
		t.Fatal(s, err)
	}
	if err := os.Chmod(dbfile, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, directoryPermission(false)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(dir, directoryPermission(true)); err != nil {
			t.Error(err)
		}
	})
	for _, source := range []string{dbfile, filepath.Join(dir, "new", "db")} {
		s, err = store.Open(source)
		if s != nil || err == nil {
			t.Fatal(s, err)
		}
	}
}

func TestSchemaAndRows(t *testing.T) {
	// R-JIWT-IQG4 R-JK4P-WI6T R-JLCM-A9XI
	path := filepath.Join(t.TempDir(), "db")
	s := open(t, path)
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	}()
	for table, want := range map[string][]string{"records": {"id", "ts", "svc", "ev", "req", "user", "attrs"}, "attrs": {"record_id", "key", "value"}} {
		var n int
		if err := db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&n); err != nil || n != 1 {
			t.Fatal(n, err)
		}
		rows, err := db.Query("PRAGMA table_info('" + table + "')")
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for rows.Next() {
			var cid, notnull, pk int
			var name, kind string
			var dflt any
			if err := rows.Scan(&cid, &name, &kind, &notnull, &dflt, &pk); err != nil {
				t.Fatal(err)
			}
			got = append(got, name)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		if err := rows.Close(); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatal(got, want)
		}
	}
	for name, want := range map[string][]string{"records_ts": {"ts"}, "records_svc_ts": {"svc", "ts"}, "records_ev_ts": {"ev", "ts"}, "records_req_ts": {"req", "ts"}, "records_user_ts": {"user", "ts"}, "attrs_key_value": {"key", "value"}} {
		table := "records"
		if strings.HasPrefix(name, "attrs") {
			table = "attrs"
		}
		var n int
		if err := db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='index' AND name=? AND tbl_name=?", name, table).Scan(&n); err != nil || n != 1 {
			t.Fatal(n, err)
		}
		rows, err := db.Query("PRAGMA index_info('" + name + "')")
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for rows.Next() {
			var seq, cid int
			var col string
			if err := rows.Scan(&seq, &cid, &col); err != nil {
				t.Fatal(err)
			}
			got = append(got, col)
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		if err := rows.Close(); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatal(got, want)
		}
	}
	a := event("a", time.Unix(-1, 123456000), telemetry.Attrs{"x": int64(500), "str": "500"})
	b := a
	b.Name = "b.record"
	put(t, s, a)
	put(t, s, b)
	rows, err := db.Query("SELECT id,ts,svc,ev,req,user,attrs FROM records ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	var ids []int64
	for _, e := range []telemetry.Event{a, b} {
		if !rows.Next() {
			t.Fatal("missing row")
		}
		var id, ts int64
		var svc, ev, req, user, attrs string
		if err := rows.Scan(&id, &ts, &svc, &ev, &req, &user, &attrs); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
		wantRecord(t, store.Record{time.UnixMicro(ts).UTC(), svc, ev, req, user, json.RawMessage(attrs)}, e)
	}
	if rows.Next() {
		t.Fatal("extra row")
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	if ids[0] >= ids[1] {
		t.Fatal(ids)
	}
	for _, id := range ids {
		rows, err := db.Query("SELECT key,value FROM attrs WHERE record_id=? ORDER BY key", id)
		if err != nil {
			t.Fatal(err)
		}
		got := map[string]string{}
		for rows.Next() {
			var k, v string
			if err := rows.Scan(&k, &v); err != nil {
				t.Fatal(err)
			}
			got[k] = v
		}
		if err := rows.Err(); err != nil {
			t.Fatal(err)
		}
		if err := rows.Close(); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, map[string]string{"x": "500", "str": "\"500\""}) {
			t.Fatal(got)
		}
	}
	if err := s.Sweep(context.Background(), a.Time.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := db.QueryRow("SELECT count(*) FROM attrs WHERE record_id NOT IN (SELECT id FROM records)").Scan(&n); err != nil || n != 0 {
		t.Fatal(n, err)
	}
}

func TestDeliveryFailureAndRecovery(t *testing.T) {
	// R-JP0B-FL5L R-3F94-JPJZ R-3IWT-P0S2 R-CE85-4LC9
	dir := t.TempDir()
	s := open(t, filepath.Join(dir, "db"))
	at := time.Unix(5, 0)
	e := event("valid", at, telemetry.Attrs{})
	put(t, s, e)
	bad := e
	bad.Attrs = telemetry.Attrs{"nested": []string{"x"}}
	if err := s.Deliver(context.Background(), bad); !errors.Is(err, telemetry.ErrRejected) {
		t.Fatal(err)
	}
	if number(t, s, store.Filter{}) != 1 {
		t.Fatal("bad stored")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := s.Deliver(ctx, e); !errors.Is(err, context.Canceled) || errors.Is(err, telemetry.ErrRejected) {
		t.Fatal(err)
	}
	if err := s.Sweep(ctx, at.Add(time.Second)); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	protected := e
	protected.Time = at.Add(time.Second)
	protected.Name = "protected.record"
	put(t, s, protected)
	if err := os.Chmod(dir, directoryPermission(false)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(dir, directoryPermission(true)); err != nil {
			t.Error(err)
		}
	})
	if err := s.Deliver(context.Background(), e); err == nil || errors.Is(err, telemetry.ErrRejected) {
		t.Fatal(err)
	}
	if err := s.Sweep(context.Background(), at.Add(time.Second)); err == nil {
		t.Fatal("sweep succeeded")
	}
	if number(t, s, store.Filter{}) != 2 {
		t.Fatal("changed after failure")
	}
	wantRecord(t, page(t, s, store.Filter{}, 10, "").Records[0], protected)
	if err := s.Deliver(ctx, e); !errors.Is(err, context.Canceled) || errors.Is(err, telemetry.ErrRejected) {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, directoryPermission(true)); err != nil {
		t.Fatal(err)
	}
	put(t, s, e)
	if number(t, s, store.Filter{}) != 3 {
		t.Fatal("recovery failed")
	}
}

func TestUnavailableReads(t *testing.T) {
	// R-BLBZ-IW8J
	for _, closed := range []bool{true, false} {
		t.Run(map[bool]string{true: "closed", false: "dropped"}[closed], func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "db")
			s := open(t, path)
			if closed {
				if err := s.Close(); err != nil {
					t.Fatal(err)
				}
			} else {
				db, err := sql.Open("sqlite", path)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := db.Exec("DROP TABLE records"); err != nil {
					t.Fatal(err)
				}
				if err := db.Close(); err != nil {
					t.Fatal(err)
				}
			}
			check := func(err error) {
				t.Helper()
				if err == nil || errors.Is(err, store.ErrCursor) || errors.Is(err, store.ErrGroupBy) {
					t.Fatal(err)
				}
			}
			_, err := s.Catalog(context.Background(), "", "")
			check(err)
			_, err = s.Search(context.Background(), store.Filter{}, 1, "")
			check(err)
			_, err = s.Count(context.Background(), store.Filter{})
			check(err)
			_, _, err = s.CountBy(context.Background(), store.Filter{}, store.ByService)
			check(err)
			_, err = s.Trace(context.Background(), "")
			check(err)
			if err := s.Deliver(context.Background(), event("a", time.Unix(1, 0), telemetry.Attrs{})); err == nil || errors.Is(err, telemetry.ErrRejected) {
				t.Fatal(err)
			}
			check(s.Sweep(context.Background(), time.Unix(2, 0)))
			_, err = s.Search(context.Background(), store.Filter{}, 1, "bad")
			if !errors.Is(err, store.ErrCursor) {
				t.Fatal(err)
			}
			_, _, err = s.CountBy(context.Background(), store.Filter{}, "bad")
			if !errors.Is(err, store.ErrGroupBy) {
				t.Fatal(err)
			}
		})
	}
}

func TestFilteringAndPaging(t *testing.T) {
	// R-JV3T-CFV2 R-JWBP-Q7LR R-JXJM-3ZCG R-JYRI-HR35 R-K17B-9AKJ R-K2F7-N2B8 R-K3N4-0U1X R-K62W-SDJB R-KC6E-P88S
	path := filepath.Join(t.TempDir(), "db")
	s, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2020, 2, 1, 3, 4, 5, 1000, time.UTC)
	events := []telemetry.Event{event("earlier", at.Add(-time.Second), telemetry.Attrs{"status": int64(500), "ok": true}), event("first", at, telemetry.Attrs{"status": uint64(500), "ok": true}), event("second", at, telemetry.Attrs{"status": float64(500), "ok": false}), event("string", at.Add(time.Second), telemetry.Attrs{"status": "500"})}
	events[0].Service = "beta"
	events[0].User = ""
	events[0].RequestID = ""
	for _, e := range events {
		put(t, s, e)
	}
	user := "user"
	empty := ""
	before := at.Add(-time.Second)
	after := at.Add(time.Second)
	offset := at.In(time.FixedZone("other", -3600))
	unknown := "unknown"
	cases := []struct {
		f     store.Filter
		names []string
	}{
		{store.Filter{}, []string{"string", "second", "first", "earlier"}},
		{store.Filter{Since: &offset, Until: &after}, []string{"second", "first"}},
		{store.Filter{Since: &after, Until: &before}, []string{}},
		{store.Filter{Services: []string{"beta", "none"}}, []string{"earlier"}},
		{store.Filter{Events: []string{"first.record", "second.record"}}, []string{"second", "first"}},
		{store.Filter{User: &empty, RequestID: &empty}, []string{"earlier"}},
		{store.Filter{User: &user, Attrs: telemetry.Attrs{"status": int(500), "ok": true}}, []string{"first"}},
		{store.Filter{Attrs: telemetry.Attrs{"status": float64(500)}}, []string{"second", "first", "earlier"}},
		{store.Filter{Attrs: telemetry.Attrs{"status": "500"}}, []string{"string"}},
		{store.Filter{RequestID: &unknown}, []string{}},
		{store.Filter{Attrs: telemetry.Attrs{"missing": false}}, []string{}},
		{store.Filter{Attrs: telemetry.Attrs{"status": nil}}, []string{}},
		{store.Filter{Attrs: telemetry.Attrs{"status": map[string]int{"x": 1}}}, []string{}},
	}
	for _, tc := range cases {
		p := page(t, s, tc.f, 10, "")
		names := make([]string, 0)
		for _, r := range p.Records {
			names = append(names, strings.TrimSuffix(r.Event, ".record"))
		}
		if !reflect.DeepEqual(names, tc.names) || number(t, s, tc.f) != int64(len(tc.names)) || p.Next != "" {
			t.Fatal(tc.f, names, p.Next, tc.names)
		}
		if again := page(t, s, tc.f, 10, ""); !reflect.DeepEqual(p, again) {
			t.Fatal("read mutated trail")
		}
	}
	p := page(t, s, store.Filter{}, 2, "")
	if p.Next == "" {
		t.Fatal("missing cursor")
	}
	b, err := base64.RawURLEncoding.Strict().DecodeString(string(p.Next))
	if err != nil || len(b) != 20 {
		t.Fatal(b, err)
	}
	var ts, id int64
	r := bytes.NewReader(b[:16])
	if err := binary.Read(r, binary.BigEndian, &ts); err != nil {
		t.Fatal(err)
	}
	if err := binary.Read(r, binary.BigEndian, &id); err != nil {
		t.Fatal(err)
	}
	if ts != at.UnixMicro() || id != 3 || binary.BigEndian.Uint32(b[16:]) != crc32.ChecksumIEEE(b[:16]) {
		t.Fatal(ts, id, b)
	}
	next := page(t, s, store.Filter{}, 2, p.Next)
	if next.Next != "" || len(next.Records) != 2 || next.Records[0].Event != "first.record" || next.Records[1].Event != "earlier.record" {
		t.Fatal(next)
	}
	changed := page(t, s, store.Filter{Services: []string{"beta"}}, 10, p.Next)
	if len(changed.Records) != 1 || changed.Records[0].Event != "earlier.record" {
		t.Fatal(changed)
	}
	for _, bad := range []store.Cursor{"page2", p.Next + "=", p.Next[:len(p.Next)-1], p.Next + "A", store.Cursor(base64.RawURLEncoding.EncodeToString([]byte("too short")))} {
		got, err := s.Search(context.Background(), store.Filter{}, 1, bad)
		if !errors.Is(err, store.ErrCursor) || !reflect.DeepEqual(got, store.Page{}) {
			t.Fatal(got, err)
		}
	}
	for i := range len(p.Next) {
		chars := []byte(p.Next)
		if chars[i] == 'A' {
			chars[i] = 'B'
		} else {
			chars[i] = 'A'
		}
		got, err := s.Search(context.Background(), store.Filter{}, 1, store.Cursor(chars))
		if !errors.Is(err, store.ErrCursor) || !reflect.DeepEqual(got, store.Page{}) {
			t.Fatalf("corruption %d: %v,%v", i, got, err)
		}
	}
	for _, limit := range []int{0, -1} {
		got, err := s.Search(context.Background(), store.Filter{}, limit, "")
		if err == nil || errors.Is(err, store.ErrCursor) || !reflect.DeepEqual(got, store.Page{}) {
			t.Fatal(got, err)
		}
	}
	// A cursor persists across restart and does not need its anchor record.
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s = open(t, path)
	if got := page(t, s, store.Filter{}, 2, p.Next); !reflect.DeepEqual(next, got) {
		t.Fatal(got, next)
	}
	trace, err := s.Trace(context.Background(), "req")
	if err != nil || len(trace) != 3 || trace[0].Event != "first.record" || trace[1].Event != "second.record" || trace[2].Event != "string.record" {
		t.Fatal(trace, err)
	}
	none, err := s.Trace(context.Background(), "no-request")
	if err != nil || len(none) != 0 {
		t.Fatal(none, err)
	}
	if err := s.Sweep(context.Background(), at.Add(time.Nanosecond)); err != nil {
		t.Fatal(err)
	}
	if got := page(t, s, store.Filter{}, 10, p.Next); len(got.Records) != 0 || got.Next != "" {
		t.Fatal(got)
	}
}

func TestCatalogAndGroups(t *testing.T) {
	// R-K4V0-ELSM R-K7AT-65A0 R-K8IP-JX0P R-K9QL-XORE R-KAYI-BGI3
	s := open(t, ":memory:")
	at := time.Date(2020, 2, 1, 23, 59, 30, 0, time.UTC)
	a := event("x", at, telemetry.Attrs{"status": int64(200), "flag": true})
	b := event("x", at.Add(30*time.Second), telemetry.Attrs{"status": "200", "other": "z"})
	c := event("y", at.Add(time.Hour), telemetry.Attrs{"status": false})
	c.Service = "beta"
	c.User = ""
	c.RequestID = ""
	d := event("y", at.Add(2*time.Hour), telemetry.Attrs{})
	d.Service = "beta"
	d.User = ""
	d.RequestID = ""
	for _, e := range []telemetry.Event{a, b, c, d} {
		put(t, s, e)
	}
	cat, err := s.Catalog(context.Background(), "", "")
	want := []store.CatalogEntry{{"alpha", []store.EventEntry{{"x.record", 2, b.Time, []string{"flag", "other", "status"}}}}, {"beta", []store.EventEntry{{"y.record", 2, d.Time, []string{"status"}}}}}
	if err != nil || !reflect.DeepEqual(cat, want) {
		t.Fatal(cat, want, err)
	}
	filtered, err := s.Catalog(context.Background(), "alpha", "x.record")
	if err != nil || !reflect.DeepEqual(filtered, want[:1]) {
		t.Fatal(filtered, err)
	}
	for _, f := range [][2]string{{"alpha", "y.record"}, {"none", ""}, {"", "none"}} {
		got, err := s.Catalog(context.Background(), f[0], f[1])
		if err != nil || len(got) != 0 {
			t.Fatal(got, err)
		}
	}
	cases := []struct {
		by   store.GroupBy
		want []store.Group
	}{
		{store.ByService, []store.Group{{"alpha", 2}, {"beta", 2}}}, {store.ByEvent, []store.Group{{"x.record", 2}, {"y.record", 2}}}, {store.ByUser, []store.Group{{"", 2}, {"user", 2}}}, {store.ByRequestID, []store.Group{{"", 2}, {"req", 2}}},
		{store.ByMinute, []store.Group{{"2020-02-01T23:59:00Z", 1}, {"2020-02-02T00:00:00Z", 1}, {"2020-02-02T00:59:00Z", 1}, {"2020-02-02T01:59:00Z", 1}}},
		{store.ByHour, []store.Group{{"2020-02-01T23:00:00Z", 1}, {"2020-02-02T00:00:00Z", 2}, {"2020-02-02T01:00:00Z", 1}}},
		{store.ByDay, []store.Group{{"2020-02-01T00:00:00Z", 1}, {"2020-02-02T00:00:00Z", 3}}},
		{store.GroupBy(store.AttrPrefix + "status"), []store.Group{{"200", 2}, {"false", 1}}}, {store.GroupBy(store.AttrPrefix + "missing"), []store.Group{}},
	}
	for _, tc := range cases {
		n, groups, err := s.CountBy(context.Background(), store.Filter{}, tc.by)
		if err != nil || n != 4 || !reflect.DeepEqual(groups, tc.want) {
			t.Fatal(tc.by, n, groups, tc.want, err)
		}
		n, groups, err = s.CountBy(context.Background(), store.Filter{Services: []string{"none"}}, tc.by)
		if err != nil || n != 0 || len(groups) != 0 {
			t.Fatal(n, groups, err)
		}
	}
	for _, by := range []store.GroupBy{"", "attrs.", "path", "status"} {
		n, groups, err := s.CountBy(context.Background(), store.Filter{}, by)
		if n != 0 || groups != nil || !errors.Is(err, store.ErrGroupBy) {
			t.Fatal(n, groups, err)
		}
	}
	again, err := s.Catalog(context.Background(), "", "")
	if err != nil || !reflect.DeepEqual(again, cat) {
		t.Fatal(again, err)
	}
}

func TestSweepBoundaryAndBatches(t *testing.T) {
	// R-J95L-9C6F
	s := open(t, ":memory:")
	at := time.Unix(100, 0)
	for i := range 510 {
		e := event("old", at.Add(-time.Second), telemetry.Attrs{"i": i})
		put(t, s, e)
	}
	a := event("boundary", at, telemetry.Attrs{"keep": true})
	b := a
	b.Name = "later.record"
	b.Time = at.Add(time.Second)
	put(t, s, a)
	put(t, s, b)
	if err := s.Sweep(context.Background(), at); err != nil {
		t.Fatal(err)
	}
	p := page(t, s, store.Filter{}, 10, "")
	if len(p.Records) != 2 {
		t.Fatal(p)
	}
	wantRecord(t, p.Records[0], b)
	wantRecord(t, p.Records[1], a)
	if err := s.Sweep(context.Background(), at); err != nil {
		t.Fatal(err)
	}
	if got := page(t, s, store.Filter{}, 10, ""); !reflect.DeepEqual(p, got) {
		t.Fatal(got)
	}
}

func TestConcurrentCalls(t *testing.T) {
	// R-XVL9-VUVQ
	s := open(t, ":memory:")
	at := time.Unix(100, 0)
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			e := event("parallel", at, telemetry.Attrs{"sequence": i})
			if err := s.Deliver(context.Background(), e); err != nil {
				t.Error(err)
			}
			if _, err := s.Catalog(context.Background(), "", ""); err != nil {
				t.Error(err)
			}
			if _, err := s.Search(context.Background(), store.Filter{}, 50, ""); err != nil {
				t.Error(err)
			}
			if _, err := s.Count(context.Background(), store.Filter{}); err != nil {
				t.Error(err)
			}
			if _, _, err := s.CountBy(context.Background(), store.Filter{}, store.ByService); err != nil {
				t.Error(err)
			}
			if _, err := s.Trace(context.Background(), "req"); err != nil {
				t.Error(err)
			}
			if err := s.Sweep(context.Background(), at); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	p := page(t, s, store.Filter{}, 50, "")
	if len(p.Records) != 20 {
		t.Fatal(len(p.Records))
	}
	seen := map[int]bool{}
	for _, r := range p.Records {
		var attrs struct {
			Sequence int `json:"sequence"`
		}
		if err := json.Unmarshal(r.Attrs, &attrs); err != nil {
			t.Fatal(err)
		}
		if seen[attrs.Sequence] {
			t.Fatal("duplicate", attrs)
		}
		seen[attrs.Sequence] = true
	}
}

func directoryPermission(writable bool) os.FileMode {
	if writable {
		return os.ModeDir | 0700
	}
	return os.ModeDir | 0500
}

// A named basic type remains its basic value in appkit's attribute contract,
// regardless of methods its producer attached to it.
type customText string

func (customText) MarshalJSON() ([]byte, error) {
	return []byte(`"custom-marshaler-output"`), nil
}

func TestFilterBasicNormalization(t *testing.T) {
	// R-JV3T-CFV2
	s := open(t, ":memory:")
	at := time.Unix(100, 0)
	for _, tc := range []struct {
		name      string
		value     any
		different any
		text      string
	}{
		{"float", float32(0.1), float64(0.1), "0.10000000149011612"},
		{"named", customText("basic-text"), "custom-marshaler-output", `"basic-text"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := event(tc.name, at, telemetry.Attrs{tc.name: tc.value})
			put(t, s, e)
			other := event("different", at, telemetry.Attrs{tc.name: tc.different})
			put(t, s, other)
			f := store.Filter{Attrs: telemetry.Attrs{tc.name: tc.value}}
			p := page(t, s, f, 10, "")
			if len(p.Records) != 1 {
				t.Fatalf("filter matches %d records, want 1", len(p.Records))
			}
			wantRecord(t, p.Records[0], e)
			var attrs map[string]json.RawMessage
			if err := json.Unmarshal(p.Records[0].Attrs, &attrs); err != nil {
				t.Fatal(err)
			}
			if string(attrs[tc.name]) != tc.text {
				t.Fatal(string(attrs[tc.name]), tc.text)
			}
			if n := number(t, s, f); n != 1 {
				t.Fatal(n)
			}
			n, groups, err := s.CountBy(context.Background(), f, store.ByEvent)
			if err != nil || n != 1 || !reflect.DeepEqual(groups, []store.Group{{e.Name, 1}}) {
				t.Fatal(n, groups, err)
			}
		})
	}
}
