package store_test

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/ikigenba/ikigenba/repos/internal/store"
)

func journalMode(t *testing.T, source string) string {
	t.Helper()
	db, err := sql.Open("sqlite", source)
	must(t, err)
	defer func() { must(t, db.Close()) }()
	var mode string
	must(t, db.QueryRowContext(testContext(t), "PRAGMA journal_mode").Scan(&mode))
	return mode
}

func TestOpenPreservesWALJournalMode(t *testing.T) {
	// R-XT4R-XL2Y
	for _, reader := range []string{"idle", "read-transaction"} {
		for _, root := range []string{"absent", "existing"} {
			t.Run(reader+"/"+root, func(t *testing.T) {
				f := setup(t)
				ctx := testContext(t)
				must(t, os.MkdirAll(filepath.Dir(f.cfg.Source), 0700))
				if root == "existing" {
					must(t, os.MkdirAll(f.cfg.Root, 0700))
				}
				db, err := sql.Open("sqlite", f.cfg.Source)
				must(t, err)
				defer func() { must(t, db.Close()) }()
				db.SetMaxOpenConns(1)
				var mode string
				must(t, db.QueryRowContext(ctx, "PRAGMA journal_mode=WAL").Scan(&mode))
				same(t, mode, "wal")
				_, err = db.ExecContext(ctx, "CREATE TABLE reader_fixture (value TEXT); INSERT INTO reader_fixture VALUES ('held')")
				must(t, err)
				var tx *sql.Tx
				if reader == "read-transaction" {
					tx, err = db.BeginTx(ctx, nil)
					must(t, err)
					defer func() { must(t, tx.Rollback()) }()
					var value string
					must(t, tx.QueryRowContext(ctx, "SELECT value FROM reader_fixture").Scan(&value))
					same(t, value, "held")
				}
				s, err := store.Open(ctx, f.cfg)
				must(t, err)
				if s == nil {
					t.Fatal("Open returned a nil store")
				}
				defer func() {
					if s != nil {
						must(t, s.Close())
					}
				}()
				_, err = s.Create(ctx, "owner", "notes")
				must(t, err)
				same(t, journalMode(t, f.cfg.Source), "wal")
				must(t, s.Close())
				s = nil
				same(t, journalMode(t, f.cfg.Source), "wal")
				if tx != nil {
					var value string
					must(t, tx.QueryRowContext(ctx, "SELECT value FROM reader_fixture").Scan(&value))
					same(t, value, "held")
				}
			})
		}
	}
}

func TestOpenCreatesDefaultJournalMode(t *testing.T) {
	// R-XT4R-XL2Y
	for _, parent := range []string{"absent", "existing"} {
		t.Run(parent, func(t *testing.T) {
			f := setup(t)
			if parent == "existing" {
				must(t, os.MkdirAll(filepath.Dir(f.cfg.Source), 0700))
			}
			s, err := store.Open(testContext(t), f.cfg)
			must(t, err)
			if s == nil {
				t.Fatal("Open returned a nil store")
			}
			must(t, s.Close())
			same(t, journalMode(t, f.cfg.Source), "delete")
		})
	}
}
