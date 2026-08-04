package retention

import (
	"bytes"
	"context"
	"database/sql"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	appkitdb "appkit/db"
	"telemetry/internal/db"
	"telemetry/internal/record"
)

func TestDefaultWindowKeeps89DaysAndRemoves91Days(t *testing.T) {
	store, _ := openTestStore(t)
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	clock := &mutableClock{at: now}
	days := Days(func(string) string { return "" }, discardLogger())
	insert(t, store,
		testRecord("01H00000000000000000000001", now.Add(-89*24*time.Hour)),
		testRecord("01H00000000000000000000002", now.Add(-91*24*time.Hour)),
	)
	removed, err := New(store, clock, days, discardLogger()).PruneOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	// R-VRDP-RPK1
	if days != 90 || removed != 1 || !exists(t, store, "01H00000000000000000000001") || exists(t, store, "01H00000000000000000000002") {
		t.Fatalf("days=%d removed=%d; 89-day kept=%t 91-day kept=%t", days, removed, exists(t, store, "01H00000000000000000000001"), exists(t, store, "01H00000000000000000000002"))
	}
}

func TestInvalidWindowsUseDefaultAndZeroCannotPruneRecentRecord(t *testing.T) {
	for _, value := range []string{"", "0", "-5", "forever", "90.5"} {
		t.Run(value, func(t *testing.T) {
			var logs bytes.Buffer
			got := Days(func(string) string { return value }, slog.New(slog.NewTextHandler(&logs, nil)))
			// R-VSLM-5HAQ
			if got != 90 || !strings.Contains(logs.String(), "level=WARN") || !strings.Contains(logs.String(), "value="+value) {
				t.Fatalf("Days(%q)=%d logs=%q, want 90 and warning naming value", value, got, logs.String())
			}
		})
	}

	store, _ := openTestStore(t)
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	days := Days(func(string) string { return "0" }, discardLogger())
	insert(t, store,
		testRecord("01H00000000000000000000001", now.Add(-24*time.Hour)),
		testRecord("01H00000000000000000000002", now.Add(-91*24*time.Hour)),
	)
	if _, err := New(store, &mutableClock{at: now}, days, discardLogger()).PruneOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	// R-VSLM-5HAQ
	if !exists(t, store, "01H00000000000000000000001") || exists(t, store, "01H00000000000000000000002") {
		t.Fatal("zero config did not use the active 90-day default window")
	}
}

func TestRunPrunesAtStartupAndAgainAfterClockAdvances(t *testing.T) {
	store, _ := openTestStore(t)
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	clock := &mutableClock{at: now}
	insert(t, store,
		testRecord("01H00000000000000000000001", now.Add(-91*24*time.Hour)),
		testRecord("01H00000000000000000000002", now.Add(-89*24*time.Hour)),
	)
	ctx := context.Background()
	ticks := make(chan time.Time)
	done := make(chan struct{})
	go func() {
		New(store, clock, 90, discardLogger()).Run(ctx, ticks)
		close(done)
	}()
	ticks <- now
	if exists(t, store, "01H00000000000000000000001") {
		t.Fatal("record outside window survived the initial pre-tick prune")
	}
	clock.set(now.Add(2 * 24 * time.Hour))
	ticks <- now
	close(ticks)
	<-done
	// R-VTTI-J91F
	if exists(t, store, "01H00000000000000000000002") {
		t.Fatal("record was not pruned on tick after the injected clock advanced")
	}
}

func TestPrunePreservesMigrationsAndRunRetriesAfterError(t *testing.T) {
	store, database := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)
	old := testRecord("01H00000000000000000000001", now.Add(-91*24*time.Hour))
	insert(t, store, old)
	if err := store.NoteDropped(ctx, "service-a", 3, now.Add(-91*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	var before int
	if err := database.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&before); err != nil {
		t.Fatal(err)
	}
	if _, err := New(store, &mutableClock{at: now}, 90, discardLogger()).PruneOnce(ctx); err != nil {
		t.Fatal(err)
	}
	var after, drops int
	if err := database.QueryRow(`SELECT COUNT(*) FROM schema_migrations`).Scan(&after); err != nil {
		t.Fatal(err)
	}
	if err := database.QueryRow(`SELECT COUNT(*) FROM ingest_drops`).Scan(&drops); err != nil {
		t.Fatal(err)
	}
	migrations, err := appkitdb.LoadMigrations(db.FS, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	if err := appkitdb.Migrate(ctx, database, migrations); err != nil {
		t.Fatalf("subsequent migration run: %v", err)
	}

	retryRecord := testRecord("01H00000000000000000000002", now.Add(-91*24*time.Hour))
	insert(t, store, retryRecord)
	if _, err := database.Exec(`CREATE TRIGGER fail_first_prune BEFORE DELETE ON records BEGIN SELECT RAISE(ABORT, 'forced prune failure'); END`); err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	pruner := New(store, &mutableClock{at: now}, 90, slog.New(slog.NewTextHandler(&logs, nil)))
	ticks := make(chan time.Time)
	done := make(chan struct{})
	go func() { pruner.Run(ctx, ticks); close(done) }()
	ticks <- now // accepted only after the failing initial prune has returned
	if _, err := database.Exec(`DROP TRIGGER fail_first_prune`); err != nil {
		t.Fatal(err)
	}
	ticks <- now
	close(ticks)
	<-done
	// R-VV1E-X0S4
	if before == 0 || after != before || drops != 0 || exists(t, store, old.ID) || exists(t, store, retryRecord.ID) || !strings.Contains(logs.String(), "forced prune failure") {
		t.Fatalf("migrations before/after=%d/%d drops=%d old=%t retry=%t logs=%q", before, after, drops, exists(t, store, old.ID), exists(t, store, retryRecord.ID), logs.String())
	}
}

type mutableClock struct {
	mu sync.RWMutex
	at time.Time
}

func (clock *mutableClock) Now() time.Time {
	clock.mu.RLock()
	defer clock.mu.RUnlock()
	return clock.at
}

func (clock *mutableClock) set(at time.Time) {
	clock.mu.Lock()
	defer clock.mu.Unlock()
	clock.at = at
}

func openTestStore(t *testing.T) (*db.Store, *sql.DB) {
	t.Helper()
	database, err := appkitdb.Open(filepath.Join(t.TempDir(), "telemetry.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	migrations, err := appkitdb.LoadMigrations(db.FS, "migrations")
	if err != nil {
		t.Fatal(err)
	}
	if err := appkitdb.Migrate(context.Background(), database, migrations); err != nil {
		t.Fatal(err)
	}
	return db.NewStore(database), database
}

func insert(t *testing.T, store *db.Store, records ...record.Record) {
	t.Helper()
	if _, err := store.InsertRecords(context.Background(), records, time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC)); err != nil {
		t.Fatal(err)
	}
}

func exists(t *testing.T, store *db.Store, id string) bool {
	t.Helper()
	_, found, err := store.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return found
}

func testRecord(id string, at time.Time) record.Record {
	return record.Record{ID: id, Time: at.Format(time.RFC3339Nano), CorrelationID: "chain-a", Service: "service-a", Kind: record.KindRequest, Op: "widgets.read"}
}

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }
