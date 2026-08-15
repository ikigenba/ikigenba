package outbox

import (
	"context"
	"database/sql"
	"encoding/json"
	"eventplane/correlation"
	"net/http"
	"reflect"
	"testing"
)

type tableColumn struct {
	name       string
	typeName   string
	notNull    int
	defaultSQL string
}

func tableColumns(t *testing.T, db *sql.DB) []tableColumn {
	t.Helper()
	rows, err := db.Query(`SELECT name, type, "notnull", COALESCE(dflt_value, '<NULL>') FROM pragma_table_info('outbox') ORDER BY cid`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []tableColumn
	for rows.Next() {
		var col tableColumn
		if err := rows.Scan(&col.name, &col.typeName, &col.notNull, &col.defaultSQL); err != nil {
			t.Fatal(err)
		}
		got = append(got, col)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return got
}

func openSQLite(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+t.TempDir()+"/outbox.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestSchemaCorrelationColumnAndAutoincrement(t *testing.T) {
	db := openSQLite(t)
	if _, err := db.Exec(SchemaSQL); err != nil {
		t.Fatal(err)
	}
	cols := tableColumns(t, db)
	wantNames := []string{"seq", "event_id", "kind", "subject", "payload", "created_at", "correlation_id"}
	var gotNames []string
	for _, col := range cols {
		gotNames = append(gotNames, col.name)
	}
	// R-UJ7Y-E4QY
	if !reflect.DeepEqual(gotNames, wantNames) || cols[6].notNull != 1 || cols[6].defaultSQL != "''" {
		t.Fatalf("columns = %+v; names want %v and correlation_id NOT NULL DEFAULT ''", cols, wantNames)
	}
	if _, err := db.Exec(`INSERT INTO outbox (event_id, kind, payload, created_at) VALUES ('e1', 'created', '{}', 'now')`); err != nil {
		t.Fatal(err)
	}
	var seq int64
	if err := db.QueryRow(`SELECT seq FROM sqlite_sequence WHERE name = 'outbox'`).Scan(&seq); err != nil || seq != 1 {
		t.Fatalf("sqlite_sequence = %d, %v; want outbox high-water mark 1", seq, err)
	}
}

const preCorrelationSchema = `CREATE TABLE outbox (
  seq            INTEGER PRIMARY KEY AUTOINCREMENT,
  event_id       TEXT    NOT NULL,
  kind           TEXT    NOT NULL,
  subject        TEXT    NOT NULL DEFAULT '',
  payload        TEXT    NOT NULL,
  created_at     TEXT    NOT NULL
);
CREATE INDEX idx_outbox_created_at ON outbox(created_at);`

func TestAddCorrelationIDSQLMatchesFreshSchema(t *testing.T) {
	fresh := openSQLite(t)
	if _, err := fresh.Exec(SchemaSQL); err != nil {
		t.Fatal(err)
	}
	upgraded := openSQLite(t)
	if _, err := upgraded.Exec(preCorrelationSchema); err != nil {
		t.Fatal(err)
	}
	if _, err := upgraded.Exec(`INSERT INTO outbox (event_id, kind, payload, created_at) VALUES ('old', 'created', '{}', 'then')`); err != nil {
		t.Fatal(err)
	}
	if _, err := upgraded.Exec(AddCorrelationIDSQL); err != nil {
		t.Fatal(err)
	}
	var id string
	// R-UKFU-RWHN
	if got, want := tableColumns(t, upgraded), tableColumns(t, fresh); !reflect.DeepEqual(got, want) {
		t.Fatalf("upgraded columns = %+v; fresh = %+v", got, want)
	}
	if err := upgraded.QueryRow(`SELECT correlation_id FROM outbox WHERE event_id = 'old'`).Scan(&id); err != nil || id != "" {
		t.Fatalf("legacy correlation_id = %q, %v; want empty non-NULL value", id, err)
	}
}

func TestAddCorrelationIDSQLPreservesCursorAndAutoincrement(t *testing.T) {
	for _, deleteRows := range []bool{false, true} {
		t.Run(map[bool]string{false: "rows-retained", true: "rows-deleted"}[deleteRows], func(t *testing.T) {
			db := openSQLite(t)
			if _, err := db.Exec(preCorrelationSchema); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 3; i++ {
				if _, err := db.Exec(`INSERT INTO outbox (event_id, kind, payload, created_at) VALUES (?, 'created', '{}', 'then')`, i); err != nil {
					t.Fatal(err)
				}
			}
			var before []int64
			rows, _ := db.Query(`SELECT seq FROM outbox ORDER BY seq`)
			for rows.Next() {
				var seq int64
				_ = rows.Scan(&seq)
				before = append(before, seq)
			}
			rows.Close()
			if _, err := db.Exec(AddCorrelationIDSQL); err != nil {
				t.Fatal(err)
			}
			var maxBefore int64 = 3
			if deleteRows {
				if _, err := db.Exec(`DELETE FROM outbox`); err != nil {
					t.Fatal(err)
				}
			} else {
				var after []int64
				rows, _ := db.Query(`SELECT seq FROM outbox ORDER BY seq`)
				for rows.Next() {
					var seq int64
					_ = rows.Scan(&seq)
					after = append(after, seq)
				}
				rows.Close()
				if !reflect.DeepEqual(after, before) {
					t.Fatalf("seq changed: before %v after %v", before, after)
				}
				var maxAfter int64
				if err := db.QueryRow(`SELECT MAX(seq) FROM outbox`).Scan(&maxAfter); err != nil || maxAfter != maxBefore {
					t.Fatalf("MAX(seq) = %d, %v; want %d", maxAfter, err, maxBefore)
				}
			}
			o, err := New(db, Options{Source: "crm"})
			if err != nil {
				t.Fatal(err)
			}
			tx, _ := db.BeginTx(context.Background(), nil)
			if err := o.Append(context.Background(), tx, Event{Kind: "created", Payload: json.RawMessage(`{}`)}); err != nil {
				t.Fatal(err)
			}
			if err := tx.Commit(); err != nil {
				t.Fatal(err)
			}
			var seq int64
			if err := db.QueryRow(`SELECT MAX(seq) FROM outbox`).Scan(&seq); err != nil {
				t.Fatal(err)
			}
			// R-ULNR-5O8C
			if seq != maxBefore+1 {
				t.Fatalf("post-upgrade seq = %d; want %d", seq, maxBefore+1)
			}
		})
	}
}

func TestAppendPersistsContextCorrelationIDVerbatim(t *testing.T) {
	o, db := newMemOutbox(t)
	id := correlation.New()
	for _, tc := range []struct {
		ctx  context.Context
		want string
	}{{correlation.WithContext(context.Background(), id), id}, {context.Background(), ""}} {
		tx, _ := db.BeginTx(context.Background(), nil)
		if err := o.Append(tc.ctx, tx, Event{Kind: "created", Payload: json.RawMessage(`{}`)}); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		var got string
		if err := db.QueryRow(`SELECT correlation_id FROM outbox ORDER BY seq DESC LIMIT 1`).Scan(&got); err != nil {
			t.Fatal(err)
		}
		// R-UMVN-JFZ1
		if got != tc.want {
			t.Fatalf("stored correlation_id = %q; want %q", got, tc.want)
		}
	}
}

func TestAppendHonorsCancelledContext(t *testing.T) {
	o, db := newMemOutbox(t)
	tx, _ := db.BeginTx(context.Background(), nil)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := o.Append(ctx, tx, Event{Kind: "created", Payload: json.RawMessage(`{}`)})
	var count int
	if queryErr := tx.QueryRow(`SELECT COUNT(*) FROM outbox`).Scan(&count); queryErr != nil {
		t.Fatal(queryErr)
	}
	_ = tx.Rollback()
	// R-UPBG-AZGF
	if err == nil || count != 0 {
		t.Fatalf("Append error = %v, row count = %d; want error and no insert", err, count)
	}
}

func TestFeedCarriesCorrelationIDIncludingEmpty(t *testing.T) {
	o, db := newMemOutbox(t)
	id := correlation.New()
	appendAddressContext(t, correlation.WithContext(context.Background(), id), o, db, "created", "/one")
	appendAddressContext(t, context.Background(), o, db, "created", "/two")
	c := dialFeed(t, feedServer(t, o), http.Header{})
	_ = c.next(t)
	for _, want := range []string{id, ""} {
		var env map[string]json.RawMessage
		if err := json.Unmarshal([]byte(dataOf(c.next(t))), &env); err != nil {
			t.Fatal(err)
		}
		var got string
		if raw, ok := env["correlation_id"]; !ok || json.Unmarshal(raw, &got) != nil {
			t.Fatalf("correlation_id missing or invalid: %v", env)
		}
		// R-UO3J-X7PQ
		if got != want {
			t.Fatalf("wire correlation_id = %q; want %q", got, want)
		}
	}
}
