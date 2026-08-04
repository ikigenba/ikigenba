package db

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	appkitdb "appkit/db"
	"telemetry/internal/record"
	telemetrytime "telemetry/internal/telemetry"
)

func TestMigrationsCreateExactRecordSchemaAndIndexes(t *testing.T) {
	database := openTestDatabase(t)

	columns := map[string]int{}
	rows, err := database.Query(`PRAGMA table_info(records)`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var cid, notNull, primaryKey int
		var name, dataType string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &dataType, &notNull, &defaultValue, &primaryKey); err != nil {
			t.Fatal(err)
		}
		columns[name] = notNull
	}
	if err := rows.Close(); err != nil {
		t.Fatal(err)
	}
	wantColumns := []string{
		"id", "time", "correlation_id", "service", "kind", "owner_email", "client_id",
		"op", "status", "error", "duration_ms", "bytes", "sha256", "params", "detail", "received_at",
	}
	gotColumns := make([]string, 0, len(columns))
	for name := range columns {
		gotColumns = append(gotColumns, name)
	}
	sort.Strings(gotColumns)
	sort.Strings(wantColumns)
	// R-V938-15FM
	if !reflect.DeepEqual(gotColumns, wantColumns) {
		t.Fatalf("records columns = %v, want %v", gotColumns, wantColumns)
	}
	for _, name := range []string{"time", "correlation_id", "service", "kind", "owner_email", "client_id", "op", "status", "error", "sha256", "received_at"} {
		if columns[name] != 1 {
			t.Errorf("records.%s NOT NULL = %d, want 1", name, columns[name])
		}
	}

	var dropsTable string
	if err := database.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'table' AND name = 'ingest_drops'`).Scan(&dropsTable); err != nil {
		t.Fatalf("ingest_drops table missing: %v", err)
	}
	indexes := queryIndexNames(t, database)
	wantIndexes := []string{
		"idx_records_chain", "idx_records_client", "idx_records_kind", "idx_records_owner",
		"idx_records_service", "idx_records_sha", "idx_records_time",
	}
	if !reflect.DeepEqual(indexes, wantIndexes) {
		t.Fatalf("records indexes = %v, want exactly %v", indexes, wantIndexes)
	}
}

func TestInsertRecordsIsIdempotentAndFirstWriteWins(t *testing.T) {
	store := NewStore(openTestDatabase(t))
	ctx := context.Background()
	first := testRecord("01H00000000000000000000001", "2026-08-03T04:05:06Z")
	first.Op = "first-op"
	first.Service = "first-service"
	if stored, err := store.InsertRecords(ctx, []record.Record{first}, fixedTime()); err != nil || stored != 1 {
		t.Fatalf("first insert = (%d, %v), want (1, nil)", stored, err)
	}
	repeat := first
	repeat.Op = "replacement-op"
	repeat.Service = "replacement-service"
	stored, err := store.InsertRecords(ctx, []record.Record{repeat}, fixedTime().Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	got, found, err := store.Get(ctx, first.ID)
	if err != nil || !found {
		t.Fatalf("Get = (%v, %t, %v)", got, found, err)
	}
	// R-VAB4-EX6B
	if stored != 0 || got.Op != "first-op" || got.Service != "first-service" {
		t.Fatalf("repeat stored=%d, op=%q, service=%q; want first write unchanged", stored, got.Op, got.Service)
	}
}

func TestChainUsesNormalizedChronologicalTimestampOrder(t *testing.T) {
	store := NewStore(openTestDatabase(t))
	ctx := context.Background()
	later := testRecord("01H00000000000000000000002", "2026-08-03T04:05:06.5Z")
	earlier := testRecord("01H00000000000000000000001", "2026-08-03T04:05:06.45Z")
	if _, err := store.InsertRecords(ctx, []record.Record{later, earlier}, fixedTime()); err != nil {
		t.Fatal(err)
	}
	chain, err := store.Chain(ctx, later.CorrelationID)
	if err != nil {
		t.Fatal(err)
	}
	// R-VBJ0-SOX0
	if got := ids(chain); !reflect.DeepEqual(got, []string{earlier.ID, later.ID}) {
		t.Fatalf("normalized chain order = %v, want earlier then later", got)
	}
	for _, item := range chain {
		if item.Time != mustNormalize(t, item.Time) || len(item.Time) != len("2026-08-03T04:05:06.000000000Z") {
			t.Errorf("stored time %q is not fixed-width normalized UTC", item.Time)
		}
	}
}

func TestChainIncludesOnlyCorrelationAcrossServicesInAscendingKeyOrder(t *testing.T) {
	store := NewStore(openTestDatabase(t))
	ctx := context.Background()
	items := []record.Record{
		testRecord("01H00000000000000000000003", "2026-08-03T04:05:07Z"),
		testRecord("01H00000000000000000000002", "2026-08-03T04:05:06Z"),
		testRecord("01H00000000000000000000001", "2026-08-03T04:05:06Z"),
	}
	items[0].Service = "gamma"
	items[1].Service = "beta"
	items[2].Service = "alpha"
	other := testRecord("01H00000000000000000000004", "2026-08-03T04:05:05Z")
	other.CorrelationID = "other-chain"
	if _, err := store.InsertRecords(ctx, []record.Record{items[0], other}, fixedTime()); err != nil {
		t.Fatal(err)
	}
	if _, err := store.InsertRecords(ctx, []record.Record{items[1], items[2]}, fixedTime()); err != nil {
		t.Fatal(err)
	}
	chain, err := store.Chain(ctx, items[0].CorrelationID)
	if err != nil {
		t.Fatal(err)
	}
	// R-VCQX-6GNP
	if got, want := ids(chain), []string{items[2].ID, items[1].ID, items[0].ID}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Chain ids = %v, want %v", got, want)
	}
}

func TestSearchKeysetPagesPartitionStableResultSet(t *testing.T) {
	store := NewStore(openTestDatabase(t))
	ctx := context.Background()
	seed := []record.Record{
		testRecord("01H00000000000000000000001", "2026-08-03T04:05:08Z"),
		testRecord("01H00000000000000000000002", "2026-08-03T04:05:07Z"),
		testRecord("01H00000000000000000000003", "2026-08-03T04:05:07Z"),
		testRecord("01H00000000000000000000004", "2026-08-03T04:05:07Z"),
		testRecord("01H00000000000000000000005", "2026-08-03T04:05:06Z"),
		testRecord("01H00000000000000000000006", "2026-08-03T04:05:05Z"),
	}
	if _, err := store.InsertRecords(ctx, seed, fixedTime()); err != nil {
		t.Fatal(err)
	}
	page1, cursor, err := store.Search(ctx, Query{Service: "service-a", Limit: 3})
	if err != nil {
		t.Fatal(err)
	}
	newer := testRecord("01H00000000000000000000007", "2026-08-03T04:05:09Z")
	if _, err := store.InsertRecords(ctx, []record.Record{newer}, fixedTime()); err != nil {
		t.Fatal(err)
	}
	page2, _, err := store.Search(ctx, Query{Service: "service-a", Limit: 3, Cursor: cursor})
	if err != nil {
		t.Fatal(err)
	}
	got := append(ids(page1), ids(page2)...)
	want := []string{seed[0].ID, seed[3].ID, seed[2].ID, seed[1].ID, seed[4].ID, seed[5].ID}
	// R-VDYT-K8EE
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("two keyset pages = %v, want exact stable partition %v", got, want)
	}
	seen := map[string]bool{}
	for _, id := range got {
		if seen[id] {
			t.Fatalf("duplicate id %q across pages", id)
		}
		seen[id] = true
	}
}

func TestSearchAxesUseCorrespondingIndexes(t *testing.T) {
	database := openTestDatabase(t)
	cases := []struct {
		name, index string
		query       Query
	}{
		{"service", "idx_records_service", Query{Service: "service-a"}},
		{"kind", "idx_records_kind", Query{Kind: "request"}},
		{"owner_email", "idx_records_owner", Query{OwnerEmail: "owner@example.test"}},
		{"client_id", "idx_records_client", Query{ClientID: "client-a"}},
		{"sha256", "idx_records_sha", Query{SHA256: "abc"}},
		{"correlation_id", "idx_records_chain", Query{CorrelationID: "chain-a"}},
		{"time", "idx_records_time", Query{Since: "2026-08-03T00:00:00Z"}},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			statement, args, err := searchStatement(test.query)
			if err != nil {
				t.Fatal(err)
			}
			assertQueryPlanUses(t, database, statement, args, test.index)
		})
	}
	// R-VF6P-Y053
	// Every subtest above asserts SEARCH plus the corresponding idx_records_* name.
}

func TestPruneBeforeUsesStrictBoundaryForRecordsAndDrops(t *testing.T) {
	store := NewStore(openTestDatabase(t))
	ctx := context.Background()
	clock := fakeClock{at: time.Date(2026, 8, 3, 4, 5, 6, 500, time.UTC)}
	var _ telemetrytime.Clock = clock
	cutoff := clock.Now()
	items := []record.Record{
		testRecord("01H00000000000000000000001", cutoff.Add(-time.Nanosecond).Format(time.RFC3339Nano)),
		testRecord("01H00000000000000000000002", cutoff.Format(time.RFC3339Nano)),
		testRecord("01H00000000000000000000003", cutoff.Add(time.Nanosecond).Format(time.RFC3339Nano)),
	}
	if _, err := store.InsertRecords(ctx, items, cutoff); err != nil {
		t.Fatal(err)
	}
	for _, at := range []time.Time{cutoff.Add(-time.Nanosecond), cutoff, cutoff.Add(time.Nanosecond)} {
		if err := store.NoteDropped(ctx, "service-a", 2, at); err != nil {
			t.Fatal(err)
		}
	}
	removed, err := store.PruneBefore(ctx, cutoff)
	if err != nil {
		t.Fatal(err)
	}
	remaining, _, err := store.Search(ctx, Query{})
	if err != nil {
		t.Fatal(err)
	}
	var dropRows, droppedTotal int64
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*), SUM(dropped) FROM ingest_drops`).Scan(&dropRows, &droppedTotal); err != nil {
		t.Fatal(err)
	}
	// R-VGEM-BRVS
	if removed != 1 || !reflect.DeepEqual(ids(remaining), []string{items[2].ID, items[1].ID}) || dropRows != 2 || droppedTotal != 4 {
		t.Fatalf("prune removed=%d remaining=%v drop rows=%d total=%d; strict boundary violated", removed, ids(remaining), dropRows, droppedTotal)
	}
}

type fakeClock struct{ at time.Time }

func (clock fakeClock) Now() time.Time { return clock.at }

func openTestDatabase(t *testing.T) *sql.DB {
	t.Helper()
	database, err := appkitdb.Open(filepath.Join(t.TempDir(), "telemetry.db"))
	if err != nil {
		t.Fatalf("open real sqlite database: %v", err)
	}
	t.Cleanup(func() { _ = database.Close() })
	migrations, err := appkitdb.LoadMigrations(FS, "migrations")
	if err != nil {
		t.Fatalf("load migrations: %v", err)
	}
	if err := appkitdb.Migrate(context.Background(), database, migrations); err != nil {
		t.Fatalf("apply migrations: %v", err)
	}
	return database
}

func testRecord(id, at string) record.Record {
	duration := int64(12)
	bytes := int64(34)
	return record.Record{
		ID: id, Time: at, CorrelationID: "chain-a", Service: "service-a",
		Kind: record.KindRequest, Actor: record.Actor{OwnerEmail: "owner@example.test", ClientID: "client-a"},
		Op: "widgets.read", Params: []byte(`{ "kept" : true }`),
		Outcome: record.Outcome{Status: "ok", DurationMS: &duration, Bytes: &bytes, SHA256: "abc"},
		Detail:  []byte(`{"answer":42}`),
	}
}

func fixedTime() time.Time {
	return time.Date(2026, 8, 3, 4, 5, 6, 0, time.UTC)
}

func ids(records []record.Record) []string {
	result := make([]string, len(records))
	for i, item := range records {
		result[i] = item.ID
	}
	return result
}

func mustNormalize(t *testing.T, value string) string {
	t.Helper()
	normalized, err := record.NormalizeTime(value)
	if err != nil {
		t.Fatal(err)
	}
	return normalized
}

func queryIndexNames(t *testing.T, database *sql.DB) []string {
	t.Helper()
	rows, err := database.Query(`PRAGMA index_list(records)`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var indexes []string
	for rows.Next() {
		var sequence, unique, partial int
		var name, origin string
		if err := rows.Scan(&sequence, &name, &unique, &origin, &partial); err != nil {
			t.Fatal(err)
		}
		if strings.HasPrefix(name, "idx_records_") {
			indexes = append(indexes, name)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	sort.Strings(indexes)
	return indexes
}

func assertQueryPlanUses(t *testing.T, database *sql.DB, statement string, args []any, index string) {
	t.Helper()
	rows, err := database.Query(`EXPLAIN QUERY PLAN `+statement, args...)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var details []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		details = append(details, detail)
	}
	plan := strings.Join(details, " | ")
	if !strings.Contains(plan, "SEARCH records") || !strings.Contains(plan, index) || strings.Contains(plan, "SCAN records") {
		t.Fatalf("query plan %q does not SEARCH with %s", plan, index)
	}
}

func TestCursorRoundTrip(t *testing.T) {
	cursor := Cursor{Time: "2026-08-03T04:05:06.000000000Z", ID: "01H00000000000000000000001"}
	got, err := ParseCursor(cursor.String())
	if err != nil || got != cursor {
		t.Fatalf("ParseCursor(Cursor.String()) = (%v, %v), want %v", got, err, cursor)
	}
	if _, err := ParseCursor(fmt.Sprintf("%%%d", 1)); err == nil {
		t.Fatal("ParseCursor accepted malformed encoding")
	}
}
