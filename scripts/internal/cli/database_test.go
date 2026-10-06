package cli_test

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/scripts"
	"github.com/ikigenba/ikigenba/scripts/internal/cli"
	"github.com/ikigenba/ikigenba/scripts/internal/store"
)

// R-23F2-ZH0V R-24MZ-D8RK R-25UV-R0I9 R-272S-4S8Y R-A2R0-PZYN R-A3YX-3RPC
func TestDatabaseStatusUsesEmbeddedMigrations(t *testing.T) {
	for _, kind := range []string{"absent", "applied", "legacy", "unknown", "invalid"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "state", "scripts.db")
			cfg := db.Config{Path: path, Migrations: scripts.Migrations(), Now: func() time.Time { return time.Date(2025, 2, 3, 4, 5, 6, 0, time.UTC) }}
			if kind == "invalid" {
				mustCLI(t, os.MkdirAll(filepath.Dir(path), 0700))
				mustCLI(t, os.WriteFile(path, []byte("not a database"), 0600))
			} else if kind != "absent" {
				handle, err := db.Open(context.Background(), cfg)
				mustCLI(t, err)
				if kind == "legacy" || kind == "unknown" {
					mustCLI(t, handle.Write(context.Background(), func(tx *sql.Tx) error {
						statement := "DROP TABLE schema_migrations"
						if kind == "unknown" {
							statement = "INSERT INTO schema_migrations (version, applied_at) VALUES (9999, '2025-02-03T04:05:06Z')"
						}
						_, err := tx.Exec(statement)
						return err
					}))
				}
				mustCLI(t, handle.Close())
			}
			var before, stdout, after bytes.Buffer
			// The oracle is appkit's public operation; no report format is restated.
			oracleErr := db.Status(context.Background(), db.Config{Path: path, Migrations: scripts.Migrations()}, &before)
			var stderr writeCounter
			code := cli.Run(context.Background(), cli.Process{Args: []string{"db", "status"}, Dir: dir, Stdout: &stdout, Stderr: &stderr})
			wantCode, wantErr, wantWrites := cli.ExitSuccess, "", 0
			if oracleErr != nil {
				wantCode = cli.ExitServerFailed
				wantErr = "scripts: " + strings.ReplaceAll(oracleErr.Error(), "\n", " ") + "\n"
				wantWrites = 1
			}
			if code != wantCode || stdout.String() != before.String() || stderr.String() != wantErr || stderr.writes != wantWrites {
				t.Fatalf("status %d %q %q (%d writes); want %d %q %q", code, stdout.String(), stderr.String(), stderr.writes, wantCode, before.String(), wantErr)
			}
			afterErr := db.Status(context.Background(), db.Config{Path: path, Migrations: scripts.Migrations()}, &after)
			if after.String() != before.String() || (afterErr == nil) != (oracleErr == nil) {
				t.Fatalf("status changed catalog: %q -> %q; %v -> %v", before.String(), after.String(), oracleErr, afterErr)
			}
			if kind == "absent" {
				entries, err := os.ReadDir(dir)
				mustCLI(t, err)
				if len(entries) != 0 {
					t.Fatalf("status created %v", entries)
				}
			}
		})
	}
}

// R-24MZ-D8RK
func TestDatabaseStatusResolvesEmptyDirAgainstWorkingDirectory(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	var want, out, errOut bytes.Buffer
	mustCLI(t, db.Status(context.Background(), db.Config{Path: filepath.Join(dir, "state", "scripts.db"), Migrations: scripts.Migrations()}, &want))
	if code := cli.Run(context.Background(), cli.Process{Args: []string{"db", "status"}, Stdout: &out, Stderr: &errOut}); code != cli.ExitSuccess || out.String() != want.String() || errOut.Len() != 0 {
		t.Fatalf("status %d %q %q", code, out.String(), errOut.String())
	}
	entries, err := os.ReadDir(dir)
	mustCLI(t, err)
	if len(entries) != 0 {
		t.Fatalf("status created %v", entries)
	}
}

// R-GUQD-MYEF R-ZV2S-V0UF R-ZYQI-0C2I
func TestStartupAppliesEmbeddedBaselineAtInjectedTime(t *testing.T) {
	for _, kind := range []string{"absent", "empty-state", "legacy"} {
		t.Run(kind, func(t *testing.T) {
			h := newHarness(t)
			path := filepath.Join(h.p.Dir, "state", "scripts.db")
			var kept store.Script
			if kind == "empty-state" {
				mustCLI(t, os.MkdirAll(filepath.Dir(path), 0700))
			}
			if kind == "legacy" {
				handle, err := db.Open(context.Background(), db.Config{Path: path, Migrations: scripts.Migrations(), Now: h.p.Now})
				mustCLI(t, err)
				catalog := store.New(handle, store.Config{Now: h.p.Now, Rand: &countingRandom{}})
				kept, err = catalog.Create(context.Background(), store.Draft{Owner: "owner", Name: "kept", Repo: "rep_0102030405060708", Ref: "main"})
				mustCLI(t, err)
				mustCLI(t, handle.Write(context.Background(), func(tx *sql.Tx) error { _, err := tx.Exec("DROP TABLE schema_migrations"); return err }))
				mustCLI(t, handle.Close())
			}
			oracle := filepath.Join(t.TempDir(), "oracle.db")
			handle, err := db.Open(context.Background(), db.Config{Path: oracle, Migrations: scripts.Migrations(), Now: h.p.Now})
			mustCLI(t, err)
			mustCLI(t, handle.Close())
			var expected bytes.Buffer
			mustCLI(t, db.Status(context.Background(), db.Config{Path: oracle, Migrations: scripts.Migrations()}, &expected))
			h.start()
			var actual bytes.Buffer
			mustCLI(t, db.Status(context.Background(), db.Config{Path: path, Migrations: scripts.Migrations()}, &actual))
			if actual.String() != expected.String() {
				t.Fatalf("startup migrations %q; expected %q", actual.String(), expected.String())
			}
			entries, err := os.ReadDir(filepath.Join(h.p.Dir, "state"))
			mustCLI(t, err)
			for _, entry := range entries {
				switch entry.Name() {
				case "scripts.db", "scripts.db-journal", "scripts.db-wal", "scripts.db-shm", "runs":
				default:
					t.Fatalf("unexpected startup file %s", entry.Name())
				}
			}
			runs, err := os.ReadDir(filepath.Join(h.p.Dir, "state", "runs"))
			mustCLI(t, err)
			if len(runs) != 0 {
				t.Fatalf("startup runs %v", runs)
			}
			database, err := os.Stat(path)
			mustCLI(t, err)
			if !database.Mode().IsRegular() {
				t.Fatal("catalog is not a regular file")
			}
			list := h.call("list", nil)["scripts"].([]any)
			if kind == "legacy" {
				if len(list) != 1 || list[0].(map[string]any)["id"] != kept.ID || list[0].(map[string]any)["name"] != kept.Name {
					t.Fatalf("legacy catalog lost: %v", list)
				}
			} else if len(list) != 0 {
				t.Fatalf("new catalog %v", list)
			}
			h.stop()
		})
	}
}

// R-ZRF3-PPMC
func TestStartupRefusesUnknownCatalogMigration(t *testing.T) {
	h := newHarness(t)
	cfg := db.Config{Path: filepath.Join(h.p.Dir, "state", "scripts.db"), Migrations: scripts.Migrations(), Now: h.p.Now}
	handle, err := db.Open(context.Background(), cfg)
	mustCLI(t, err)
	mustCLI(t, handle.Write(context.Background(), func(tx *sql.Tx) error {
		_, err := tx.Exec("INSERT INTO schema_migrations (version, applied_at) VALUES (9999, '2025-02-03T04:05:06Z')")
		return err
	}))
	mustCLI(t, handle.Close())
	_, oracleErr := db.Open(context.Background(), cfg)
	if oracleErr == nil {
		t.Fatal("fixture is not a newer catalog")
	}
	h.p.Sink = forbiddenSink{t}
	code := cli.Run(context.Background(), h.p)
	want := "scripts: cannot open database state/scripts.db: " + strings.ReplaceAll(oracleErr.Error(), "\n", " ") + "\n"
	if code != cli.ExitServerFailed || h.stderr.String() != want || h.stdout.String() != "" || len(h.stderr.snapshot()) != 1 {
		t.Fatalf("newer catalog %d %q; want %q", code, h.stderr.String(), want)
	}
	if _, err := os.Stat(filepath.Join(h.p.Dir, "state", "runs")); !os.IsNotExist(err) {
		t.Fatalf("runs created: %v", err)
	}
}

// R-016A-RVJW R-03M3-JF1A
func TestEarlyStartupFailuresLeaveRunRecordsUnchanged(t *testing.T) {
	for _, kind := range []string{"python", "runs-file"} {
		t.Run(kind, func(t *testing.T) {
			h := newHarness(t)
			h.set("RUN_KEEP_DAYS", "1")
			h.set("RUN_KEEP_COUNT", "1")
			_, records := seedCatalog(t, h)
			if kind == "python" {
				bin := filepath.Join(h.root, "only-git")
				mustCLI(t, os.Mkdir(bin, 0700))
				mustCLI(t, os.Symlink(h.git, filepath.Join(bin, "git")))
				h.set("PATH", bin)
			} else {
				runs := filepath.Join(h.p.Dir, "state", "runs")
				mustCLI(t, os.RemoveAll(runs))
				mustCLI(t, os.WriteFile(runs, []byte("unchanged"), 0600))
			}
			if code := cli.Run(context.Background(), h.p); code != cli.ExitServerFailed {
				t.Fatalf("startup %d", code)
			}
			if len(h.sink.capture.Events()) != 0 || h.stdout.String() != "" || len(h.stderr.snapshot()) != 1 {
				t.Fatalf("startup side effects: %q", h.stderr.String())
			}
			handle, err := db.Open(context.Background(), db.Config{Path: filepath.Join(h.p.Dir, "state", "scripts.db"), Migrations: scripts.Migrations(), Now: h.p.Now})
			mustCLI(t, err)
			defer func() { _ = handle.Close() }()
			catalog := store.New(handle, store.Config{Now: h.p.Now, Rand: &countingRandom{}})
			for _, before := range records {
				after, err := catalog.RunByID(context.Background(), before.ID)
				mustCLI(t, err)
				if after != before {
					t.Fatalf("early refusal changed record: %v -> %v", before, after)
				}
			}
			if kind == "runs-file" {
				runs := filepath.Join(h.p.Dir, "state", "runs")
				err := os.MkdirAll(runs, 0700)
				if err == nil || h.stderr.String() != "scripts: cannot create directory state/runs: "+err.Error()+"\n" {
					t.Fatalf("runs refusal %q; mkdir error %v", h.stderr.String(), err)
				}
				b, err := os.ReadFile(filepath.Clean(runs))
				mustCLI(t, err)
				if string(b) != "unchanged" {
					t.Fatalf("runs file changed %q", b)
				}
			}
		})
	}
}

// R-079S-OQ9D R-09PL-G9QR R-1K1R-ZJX5
func TestCancellationAfterCatalogOpenSettlesAndFlushesRecovery(t *testing.T) {
	h := newHarness(t)
	path := filepath.Join(h.p.Dir, "state", "scripts.db")
	handle, err := db.Open(context.Background(), db.Config{Path: path, Migrations: scripts.Migrations(), Now: h.p.Now})
	mustCLI(t, err)
	catalog := store.New(handle, store.Config{Now: h.p.Now, Rand: &countingRandom{}})
	script, err := catalog.Create(context.Background(), store.Draft{Owner: "owner", Name: "recover", Repo: "rep_0102030405060708", Ref: "main"})
	mustCLI(t, err)
	ids := []string{"run_0102030405060708", "run_1112131415161718"}
	for _, id := range ids {
		_, err = catalog.AddRun(context.Background(), store.Run{ID: id, Script: script.ID, SHA: strings.Repeat("a", 40), Ref: "main", User: "owner", RequestID: "earlier", Trigger: store.TriggerManual, Status: store.StatusRunning, Started: h.now.Add(-time.Hour)})
		mustCLI(t, err)
	}
	mustCLI(t, handle.Close())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h.p.Now = func() time.Time {
		if _, err := os.Stat(filepath.Join(h.p.Dir, "state", "runs")); err == nil {
			cancel()
		}
		return h.now
	}
	if code := cli.Run(ctx, h.p); code != cli.ExitSuccess || h.stdout.String() != "" || h.stderr.String() != "" {
		t.Fatalf("canceled recovery %d %q", code, h.stderr.String())
	}
	events := h.sink.capture.Events()
	if len(events) != len(ids) {
		t.Fatalf("unflushed recovery %v", events)
	}
	remaining := map[string]bool{ids[0]: true, ids[1]: true}
	for _, event := range events {
		id, ok := event.Attrs["run"].(string)
		if !ok || !remaining[id] || event.Name != "run.finished" || event.Attrs["status"] != "killed" {
			t.Fatalf("recovery event %v", event)
		}
		delete(remaining, id)
	}
	handle, err = db.Open(context.Background(), db.Config{Path: path, Migrations: scripts.Migrations(), Now: func() time.Time { return h.now }})
	mustCLI(t, err)
	defer func() { _ = handle.Close() }()
	catalog = store.New(handle, store.Config{Now: func() time.Time { return h.now }, Rand: &countingRandom{}})
	for _, id := range ids {
		record, err := catalog.RunByID(context.Background(), id)
		mustCLI(t, err)
		if record.Status != store.StatusKilled {
			t.Fatalf("unsettled recovery %v", record)
		}
	}
}
