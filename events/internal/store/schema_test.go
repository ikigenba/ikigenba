package store_test

import (
	"database/sql"
	"reflect"
	"strings"
	"testing"

	"github.com/ikigenba/ikigenba/events/internal/store"
)

func TestSchema(t *testing.T) {
	// R-CTIL-LWU2 R-CUQH-ZOKR R-CVYE-DGBG
	_, d, _ := openStore(t, store.Config{})
	must(t, d.Read(ctx, func(tx *sql.Tx) error {
		rows, err := tx.Query("SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name")
		if err != nil {
			return err
		}
		names := []string{}
		for rows.Next() {
			var n string
			if err = rows.Scan(&n); err != nil {
				return err
			}
			names = append(names, n)
		}
		must(t, rows.Err())
		must(t, rows.Close())
		if !reflect.DeepEqual(names, []string{"attrs", "declarations", "events", "schema_migrations", "subscribers"}) {
			t.Fatal(names)
		}
		tables := map[string][]string{"events": {"seq", "id", "time", "service", "event", "request_id", "user", "attrs", "cause", "depth", "received"}, "attrs": {"seq", "key", "value"}, "subscribers": {"service", "status", "cursor", "since", "event", "name", "seq", "error"}, "declarations": {"service", "emits", "accepts", "asked"}}
		for table, want := range tables {
			rows, err = tx.Query("PRAGMA table_info('" + table + "')")
			if err != nil {
				return err
			}
			got := []string{}
			pks := []string{}
			for rows.Next() {
				var cid, notnull, pk int
				var name, typ string
				var def sql.NullString
				if err = rows.Scan(&cid, &name, &typ, &notnull, &def, &pk); err != nil {
					return err
				}
				got = append(got, name)
				if pk != 0 {
					pks = append(pks, name)
				}
				if table == "events" && name == "seq" && typ != "INTEGER" {
					t.Fatal(typ)
				}
			}
			must(t, rows.Err())
			must(t, rows.Close())
			if !reflect.DeepEqual(got, want) {
				t.Fatal(table, got)
			}
			if table == "events" && !reflect.DeepEqual(pks, []string{"seq"}) {
				t.Fatal(pks)
			}
			if (table == "subscribers" || table == "declarations") && !reflect.DeepEqual(pks, []string{"service"}) {
				t.Fatal(pks)
			}
		}
		indexes := map[string][]string{"events_service_seq": {"service", "seq"}, "events_event_seq": {"event", "seq"}, "events_user_seq": {"user", "seq"}, "events_cause_seq": {"cause", "seq"}, "events_request_id_seq": {"request_id", "seq"}, "events_received": {"received"}, "attrs_key_value": {"key", "value"}}
		for name, want := range indexes {
			var table string
			if err = tx.QueryRow("SELECT tbl_name FROM sqlite_master WHERE type='index' AND name=?", name).Scan(&table); err != nil {
				return err
			}
			expected := "events"
			if strings.HasPrefix(name, "attrs_") {
				expected = "attrs"
			}
			if table != expected {
				t.Fatal(table)
			}
			got, err := indexColumns(tx, name)
			if err != nil {
				return err
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatal(name, got)
			}
		}
		rows, err = tx.Query("PRAGMA index_list('events')")
		if err != nil {
			return err
		}
		unique := []string{}
		for rows.Next() {
			var seq, u, partial int
			var name, origin string
			if err = rows.Scan(&seq, &name, &u, &origin, &partial); err != nil {
				return err
			}
			if u == 1 {
				unique = append(unique, name)
			}
		}
		must(t, rows.Err())
		must(t, rows.Close())
		idUnique := false
		for _, name := range unique {
			cols, err := indexColumns(tx, name)
			if err != nil {
				return err
			}
			idUnique = idUnique || reflect.DeepEqual(cols, []string{"id"})
		}
		if !idUnique {
			t.Fatal("id not unique")
		}
		return nil
	}))
}
func indexColumns(tx *sql.Tx, name string) ([]string, error) {
	rows, err := tx.Query("PRAGMA index_info('" + name + "')")
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	result := []string{}
	for rows.Next() {
		var pos, cid int
		var column string
		if err = rows.Scan(&pos, &cid, &column); err != nil {
			return nil, err
		}
		result = append(result, column)
	}
	return result, rows.Err()
}
