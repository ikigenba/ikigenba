package store_test

import (
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/scrapes/internal/source"
	"github.com/ikigenba/ikigenba/scrapes/internal/store"
)

const previousFileSchema = `CREATE TABLE rows ("run" TEXT,source_id TEXT PRIMARY KEY,scraper TEXT,collected TEXT,digest TEXT,name TEXT,names TEXT,street TEXT,city TEXT,state TEXT,zip TEXT,county TEXT,lat REAL,lon REAL,people TEXT,facts BLOB,raw BLOB) WITHOUT ROWID;
CREATE INDEX rows_source_digest ON rows(source_id,digest);
CREATE TABLE names(source_id TEXT,seq INTEGER,name_lower TEXT,PRIMARY KEY(source_id,seq)) WITHOUT ROWID;`
const previousRowColumns = `"run",source_id,scraper,collected,digest,name,names,street,city,state,zip,county,lat,lon,people,facts,raw`

func compatibilityRows(run store.Run) []source.Row {
	lat, lon := 44.25, -93.5
	return []source.Row{
		{Run: run.ID, Scraper: run.Scraper, SourceID: "1", Scraped: time.Date(2026, 2, 3, 4, 5, 6, 0, time.UTC), Digest: "one", Name: "Acme Trucking", Names: []string{"Acme Trucking", "Acme"}, Address: source.Address{Street: "one street", City: "city", State: "MN", Zip: "12345", County: "county"}, Lat: &lat, Lon: &lon, People: []source.Person{{Name: "person", Role: "owner"}}, Facts: []byte(" { \"one\": 1 } "), Raw: []byte(" { \"raw\": 1 } ")},
		{Run: run.ID, Scraper: run.Scraper, SourceID: "2", Scraped: time.Date(2026, 2, 4, 4, 5, 6, 0, time.UTC), Digest: "two", Name: "Other", Names: []string{}, People: []source.Person{}},
	}
}

// R-PQIJ-CBUP R-PRQF-Q3LE
func TestPreviousRunFileReadsAndComparison(t *testing.T) {
	s, _, runs := configured(t, 1, 2)
	r := add(t, s, "a")
	want := compatibilityRows(r)
	path := filepath.Join(t.TempDir(), "previous.db")
	bulk, err := db.OpenBulk(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	err = bulk.Write(ctx, func(tx *sql.Tx) error {
		if _, e := tx.ExecContext(ctx, previousFileSchema); e != nil {
			return e
		}
		for _, row := range want {
			names, e := json.Marshal(row.Names)
			if e != nil {
				return e
			}
			people, e := json.Marshal(row.People)
			if e != nil {
				return e
			}
			_, e = tx.ExecContext(ctx, "INSERT INTO rows ("+previousRowColumns+") VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)", row.Run, row.SourceID, row.Scraper, row.Scraped.Format(time.RFC3339), row.Digest, row.Name, string(names), row.Address.Street, row.Address.City, row.Address.State, row.Address.Zip, row.Address.County, row.Lat, row.Lon, string(people), []byte(row.Facts), []byte(row.Raw))
			if e != nil {
				return e
			}
			for i, name := range row.Names {
				if _, e = tx.ExecContext(ctx, "INSERT INTO names VALUES(?,?,?)", row.SourceID, i, strings.ToLower(name)); e != nil {
					return e
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = bulk.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(runs, 0700); err != nil {
		t.Fatal(err)
	}
	if err = writeRunFile(runs, r.ID, data); err != nil {
		t.Fatal(err)
	}
	if _, err = s.FinishRun(ctx, r.ID, store.Counts{Rows: 2, New: 2}); err != nil {
		t.Fatal(err)
	}
	for _, row := range want {
		got, e := s.GetRow(ctx, r.ID, row.SourceID)
		if e != nil || !reflect.DeepEqual(got, row) {
			t.Fatalf("row %#v, want %#v: %v", got, row, e)
		}
	}
	var streamed []source.Row
	if err = s.EachRow(ctx, r.ID, func(row source.Row) error { streamed = append(streamed, row); return nil }); err != nil || !reflect.DeepEqual(streamed, want) {
		t.Fatal(streamed, err)
	}
	after, err := s.ListRowsAfter(ctx, r.ID, "acme", "", 2)
	listed := want[0]
	listed.Raw = nil
	if err != nil || !reflect.DeepEqual(after, []source.Row{listed}) {
		t.Fatal(after, err)
	}
	before, err := s.ListRowsBefore(ctx, r.ID, "", "", 2)
	if err != nil || !reflect.DeepEqual(before, []source.Row{want[1], listed}) {
		t.Fatal(before, err)
	}
	filtered, err := s.ListRowsBefore(ctx, r.ID, "ACME", "", 2)
	if err != nil || !reflect.DeepEqual(filtered, []source.Row{listed}) {
		t.Fatal(filtered, err)
	}
	next := add(t, s, "a")
	counts := load(t, build(t, s, next, nil), []source.Row{want[1], {SourceID: "3", Digest: "three"}})
	if counts != (store.Counts{Rows: 2, New: 1, Unchanged: 1, Gone: 1}) {
		t.Fatal(counts)
	}
	if got := snapshot(t, runs)[r.ID+".db"]; got != string(data) {
		t.Fatal("comparison changed previous file")
	}
}

// R-PPAM-YK40
func TestLoadedFileCanBeReadByPreviousRelease(t *testing.T) {
	s, _, runs := configured(t, 1, 1)
	r := add(t, s, "a")
	want := compatibilityRows(r)
	r = finish(t, s, r, want...)
	d, err := db.OpenImmutable(ctx, filepath.Join(runs, r.ID+".db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.Close() }()
	err = d.Read(ctx, func(tx *sql.Tx) error {
		rows, e := tx.QueryContext(ctx, "SELECT "+previousRowColumns+",scraped FROM rows ORDER BY source_id")
		if e != nil {
			return e
		}
		defer func() { _ = rows.Close() }()
		i := 0
		for rows.Next() {
			if i >= len(want) {
				t.Fatal("extra row")
			}
			var got source.Row
			var collected, scraped, names, people string
			var facts, raw []byte
			e = rows.Scan(&got.Run, &got.SourceID, &got.Scraper, &collected, &got.Digest, &got.Name, &names, &got.Address.Street, &got.Address.City, &got.Address.State, &got.Address.Zip, &got.Address.County, &got.Lat, &got.Lon, &people, &facts, &raw, &scraped)
			if e != nil {
				return e
			}
			if collected != scraped || scraped != want[i].Scraped.Format(time.RFC3339) {
				t.Fatal(collected, scraped)
			}
			got.Scraped, e = time.Parse(time.RFC3339, collected)
			if e != nil {
				return e
			}
			if e = json.Unmarshal([]byte(names), &got.Names); e != nil {
				return e
			}
			if e = json.Unmarshal([]byte(people), &got.People); e != nil {
				return e
			}
			got.Facts, got.Raw = facts, raw
			read, e := s.GetRow(ctx, r.ID, got.SourceID)
			if e != nil || !reflect.DeepEqual(got, read) || !reflect.DeepEqual(got, want[i]) {
				t.Fatalf("previous read %#v, current %#v, want %#v: %v", got, read, want[i], e)
			}
			i++
		}
		if e = rows.Err(); e != nil {
			return e
		}
		if i != len(want) {
			t.Fatal(i)
		}
		namesRows, e := tx.QueryContext(ctx, "SELECT source_id,seq,name_lower FROM names ORDER BY source_id,seq")
		if e != nil {
			return e
		}
		defer func() { _ = namesRows.Close() }()
		for _, row := range want {
			for i, name := range row.Names {
				if !namesRows.Next() {
					t.Fatal("missing name", namesRows.Err())
				}
				var sourceID, lower string
				var seq int
				if e = namesRows.Scan(&sourceID, &seq, &lower); e != nil {
					return e
				}
				if sourceID != row.SourceID || seq != i || lower != strings.ToLower(name) {
					t.Fatal(sourceID, seq, lower)
				}
			}
		}
		if namesRows.Next() {
			t.Fatal("extra name")
		}
		return namesRows.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
}
