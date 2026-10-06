package db_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
)

// R-QB4H-2NM9 R-P68C-RQSP R-BBYT-8YTC R-QJNR-R1T4 R-QNBG-WD17
func TestStatusReportsUnionWithoutApplyingOrReadingClock(t *testing.T) {
	cfg := migrationConfig(t, "CREATE TABLE untouched(value TEXT)", "INSERT INTO untouched VALUES('must not run')", "SELECT 3")
	conn := rawDatabase(t, cfg.Path)
	if _, err := conn.Exec("CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY,applied_at TEXT); INSERT INTO schema_migrations VALUES(1,'first time'),(4,'future time'); CREATE TABLE untouched(value TEXT); INSERT INTO untouched VALUES('original')"); err != nil {
		t.Fatal(err)
	}
	cfg.Now = func() time.Time { t.Fatal("Status called clock"); return time.Time{} }
	before, err := os.ReadFile(cfg.Path)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	status := db.Status
	err = status(context.Background(), cfg, &output)
	if !errors.Is(err, db.ErrUnknownVersion) {
		t.Fatalf("error %v", err)
	}
	if want := "0001 applied first time\n0002 pending\n0003 pending\n0004 unknown future time\n"; output.String() != want {
		t.Fatalf("output %q", output.String())
	}
	after, err := os.ReadFile(cfg.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("Status changed database bytes")
	}
	var value string
	if err := conn.QueryRow("SELECT value FROM untouched").Scan(&value); err != nil || value != "original" {
		t.Fatalf("rows %q %v", value, err)
	}
	var count int
	if err := conn.QueryRow("SELECT count(*) FROM schema_migrations").Scan(&count); err != nil || count != 2 {
		t.Fatalf("migration rows %d %v", count, err)
	}
}

// R-TN4J-9EWI R-BBYT-8YTC R-P68C-RQSP
func TestStatusErrorNamesLowestUnknownVersion(t *testing.T) {
	for _, tc := range []struct {
		name     string
		versions []int
		message  string
		output   string
	}{
		{"single", []int{12}, "unknown migration version: 0012", "0001 pending\n0012 unknown past\n"},
		{"multiple", []int{40, 12, 3}, "unknown migration version: 0003", "0001 pending\n0003 unknown past\n0012 unknown past\n0040 unknown past\n"},
		{"zero", []int{12, 0, 4}, "unknown migration version: 0000", "0000 unknown past\n0001 pending\n0004 unknown past\n0012 unknown past\n"},
		{"negative", []int{12, -3, 0}, "unknown migration version: -003", "-003 unknown past\n0000 unknown past\n0001 pending\n0012 unknown past\n"},
		{"five digits", []int{12000, 10000}, "unknown migration version: 10000", "0001 pending\n10000 unknown past\n12000 unknown past\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := migrationConfig(t, "SELECT 1")
			conn := rawDatabase(t, cfg.Path)
			if _, err := conn.Exec("CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY,applied_at TEXT)"); err != nil {
				t.Fatal(err)
			}
			for _, version := range tc.versions {
				if _, err := conn.Exec("INSERT INTO schema_migrations VALUES(?,'past')", version); err != nil {
					t.Fatal(err)
				}
			}
			var output bytes.Buffer
			err := db.Status(context.Background(), cfg, &output)
			if !errors.Is(err, db.ErrUnknownVersion) || err.Error() != tc.message {
				t.Fatalf("Status error = %v; want %q wrapping ErrUnknownVersion", err, tc.message)
			}
			if output.String() != tc.output {
				t.Fatalf("Status output = %q; want %q", output.String(), tc.output)
			}
		})
	}
}

// R-QM3K-ILAI
func TestStatusPreservesRollbackJournalMode(t *testing.T) {
	cfg := migrationConfig(t, "SELECT 1")
	conn := rawDatabase(t, cfg.Path)
	var mode string
	if err := conn.QueryRow("PRAGMA journal_mode=DELETE").Scan(&mode); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Exec("CREATE TABLE ordinary(value TEXT)"); err != nil {
		t.Fatal(err)
	}
	if err := conn.Close(); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := db.Status(context.Background(), cfg, &output); err != nil {
		t.Fatal(err)
	}
	reopened := rawDatabase(t, cfg.Path)
	if err := reopened.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatal(err)
	}
	if mode != "delete" {
		t.Fatalf("journal mode %q", mode)
	}
}

// R-P7G9-5IJE R-QQZ6-1O9A
func TestStatusAbsentDatabaseCreatesNothing(t *testing.T) {
	cfg := migrationConfig(t, "SELECT 1", "SELECT 2")
	root := filepath.Dir(cfg.Path)
	cfg.Path = filepath.Join(root, "absent", "state.db")
	var output bytes.Buffer
	if err := db.Status(context.Background(), cfg, &output); err != nil {
		t.Fatal(err)
	}
	if output.String() != "0001 pending\n0002 pending\n" {
		t.Fatal(output.String())
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("created entries %v %v", entries, err)
	}
}

// R-P8O5-JAA3 R-QQZ6-1O9A R-QJNR-R1T4
func TestStatusDatabaseWithoutMigrationTable(t *testing.T) {
	cfg := migrationConfig(t, "SELECT 1", "SELECT 2")
	conn := rawDatabase(t, cfg.Path)
	if _, err := conn.Exec("CREATE TABLE existing(value TEXT); INSERT INTO existing VALUES('unchanged')"); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := db.Status(context.Background(), cfg, &output); err != nil {
		t.Fatal(err)
	}
	if output.String() != "0001 pending\n0002 pending\n" {
		t.Fatal(output.String())
	}
	var count int
	if err := conn.QueryRow("SELECT count(*) FROM sqlite_schema WHERE name='schema_migrations'").Scan(&count); err != nil || count != 0 {
		t.Fatalf("migration table %d %v", count, err)
	}
}

// R-QS72-FFZZ
func TestStatusRejectsSpecialPathsWithoutOutputOrFiles(t *testing.T) {
	for _, path := range []string{"", ":memory:", "file:state.db", "state.db?mode=ro"} {
		t.Run(path, func(t *testing.T) {
			root := t.TempDir()
			t.Chdir(root)
			cfg := migrationConfig(t)
			cfg.Path = path
			var output bytes.Buffer
			if err := db.Status(context.Background(), cfg, &output); err == nil {
				t.Fatal("accepted invalid path")
			}
			if output.Len() != 0 {
				t.Fatal(output.String())
			}
			entries, err := os.ReadDir(root)
			if err != nil || len(entries) != 0 {
				t.Fatalf("created entries %v %v", entries, err)
			}
		})
	}
}

// R-NIYW-1WBH R-QES6-7YUC
func TestStatusInvalidMigrationsPrecedeUnknownVersion(t *testing.T) {
	cfg := migrationConfig(t)
	conn := rawDatabase(t, cfg.Path)
	if _, err := conn.Exec("CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY,applied_at TEXT); INSERT INTO schema_migrations VALUES(999,'future')"); err != nil {
		t.Fatal(err)
	}
	cfg.Migrations = nil
	var output bytes.Buffer
	err := db.Status(context.Background(), cfg, &output)
	if err == nil || errors.Is(err, db.ErrUnknownVersion) || output.Len() != 0 {
		t.Fatalf("error %v output %q", err, output.String())
	}
}

// R-5660-FNLL R-QUMV-6ZHD R-QVUR-KR82
func TestStatusRejectsUnusableDatabaseWithoutOutput(t *testing.T) {
	for _, kind := range []string{"non-sqlite", "corrupt", "directory", "unreadable"} {
		t.Run(kind, func(t *testing.T) {
			cfg := migrationConfig(t)
			switch kind {
			case "directory":
				if err := os.Mkdir(cfg.Path, 0700); err != nil {
					t.Fatal(err)
				}
			case "non-sqlite":
				if err := os.WriteFile(cfg.Path, []byte("not a SQLite database"), 0600); err != nil {
					t.Fatal(err)
				}
			case "corrupt":
				if err := os.WriteFile(cfg.Path, append([]byte("SQLite format 3\x00"), make([]byte, 100)...), 0600); err != nil {
					t.Fatal(err)
				}
			case "unreadable":
				conn := rawDatabase(t, cfg.Path)
				if _, err := conn.Exec("CREATE TABLE item(value TEXT)"); err != nil {
					t.Fatal(err)
				}
				if err := conn.Close(); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(cfg.Path, 0000); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if err := os.Chmod(cfg.Path, 0600); err != nil {
						t.Error(err)
					}
				})
			}
			var output bytes.Buffer
			if err := db.Status(context.Background(), cfg, &output); err == nil {
				t.Fatal("accepted unusable database")
			}
			if output.Len() != 0 {
				t.Fatal(output.String())
			}
		})
	}
}

// R-BGUE-S1S4
func TestStatusDoneContextProducesNoOutput(t *testing.T) {
	cfg := migrationConfig(t, "SELECT 1")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var output bytes.Buffer
	err := db.Status(ctx, cfg, &output)
	if !errors.Is(err, ctx.Err()) || output.Len() != 0 {
		t.Fatalf("error %v output %q", err, output.String())
	}
}

type migrationErrorWriter struct{ err error }

func (w migrationErrorWriter) Write([]byte) (int, error) { return 0, w.err }

// R-NLEO-TFSV
func TestStatusReturnsWriterError(t *testing.T) {
	cfg := migrationConfig(t, "SELECT 1")
	failure := errors.New("output unavailable")
	if err := db.Status(context.Background(), cfg, migrationErrorWriter{failure}); !errors.Is(err, failure) {
		t.Fatalf("error %v", err)
	}
}

// R-QQZ6-1O9A R-P68C-RQSP R-P7G9-5IJE
func TestStatusAllKnownVersionsReturnsNil(t *testing.T) {
	cfg := migrationConfig(t, "SELECT 1")
	handle := openMigrationDB(t, cfg)
	if err := handle.Close(); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := db.Status(context.Background(), cfg, &output); err != nil {
		t.Fatal(err)
	}
	if output.String() != "0001 applied 2025-01-02T02:04:05.123456Z\n" {
		t.Fatalf("output %q", output.String())
	}
	empty := migrationConfig(t)
	output.Reset()
	if err := db.Status(context.Background(), empty, &output); err != nil || output.Len() != 0 {
		t.Fatalf("empty error %v output %q", err, output.String())
	}
}
