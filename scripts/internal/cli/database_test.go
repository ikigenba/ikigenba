package cli_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/scripts"
	"github.com/ikigenba/ikigenba/scripts/internal/cli"
	"github.com/ikigenba/ikigenba/scripts/internal/pages"
	"github.com/ikigenba/ikigenba/scripts/internal/store"
)

// R-23F2-ZH0V R-24MZ-D8RK R-25UV-R0I9 R-H0EY-7SFO R-I73F-LRDZ R-A2R0-PZYN R-A3YX-3RPC
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
			code := cli.Run(context.Background(), cli.Process{Version: testVersion, Args: []string{"db", "status"}, Dir: dir, Stdout: &stdout, Stderr: &stderr})
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
	if code := cli.Run(context.Background(), cli.Process{Version: testVersion, Args: []string{"db", "status"}, Stdout: &out, Stderr: &errOut}); code != cli.ExitSuccess || out.String() != want.String() || errOut.Len() != 0 {
		t.Fatalf("status %d %q %q", code, out.String(), errOut.String())
	}
	entries, err := os.ReadDir(dir)
	mustCLI(t, err)
	if len(entries) != 0 {
		t.Fatalf("status created %v", entries)
	}
}

// R-AI7X-YOIR R-ZV2S-V0UF R-X4UD-EYXR
func TestStartupAppliesEmbeddedBaselineAtInjectedTime(t *testing.T) {
	for _, kind := range []string{"absent", "empty-state", "legacy"} {
		t.Run(kind, func(t *testing.T) {
			h := newHarness(t)
			mustCLI(t, os.MkdirAll(h.p.Dir, 0700))
			mustCLI(t, os.WriteFile(filepath.Join(h.p.Dir, "keep"), []byte("unchanged"), 0600))
			path := filepath.Join(h.p.Dir, "state", "scripts.db")
			if kind == "empty-state" {
				mustCLI(t, os.MkdirAll(filepath.Dir(path), 0700))
			}
			beforeCatalog := map[int64]string{}
			if kind == "legacy" {
				handle, err := db.Open(context.Background(), db.Config{Path: path, Migrations: fstest.MapFS{}, Now: h.p.Now})
				mustCLI(t, err)
				beforeCatalog[1] = "earlier stored bytes\x00\xff"
				mustCLI(t, handle.Write(context.Background(), func(tx *sql.Tx) error {
					if _, err := tx.Exec("CREATE TABLE catalog (id INTEGER PRIMARY KEY CHECK(id=1), value BLOB NOT NULL)"); err != nil {
						return err
					}
					if _, err := tx.Exec("INSERT INTO catalog (id, value) VALUES (1, ?)", []byte(beforeCatalog[1])); err != nil {
						return err
					}
					_, err := tx.Exec("DROP TABLE schema_migrations")
					return err
				}))
				mustCLI(t, handle.Close())
			}
			oracle := filepath.Join(t.TempDir(), "oracle.db")
			handle, err := db.Open(context.Background(), db.Config{Path: oracle, Migrations: scripts.Migrations(), Now: h.p.Now})
			mustCLI(t, err)
			mustCLI(t, handle.Close())
			var expected bytes.Buffer
			mustCLI(t, db.Status(context.Background(), db.Config{Path: oracle, Migrations: scripts.Migrations()}, &expected))
			outside := outsideSnapshot(t, h.root, filepath.Join(h.p.Dir, "state"), h.p.Cgroup)
			h.start()
			var actual bytes.Buffer
			mustCLI(t, db.Status(context.Background(), db.Config{Path: path, Migrations: scripts.Migrations()}, &actual))
			if actual.String() != expected.String() {
				t.Fatalf("startup migrations %q; expected %q", actual.String(), expected.String())
			}
			if !reflect.DeepEqual(outside, outsideSnapshot(t, h.root, filepath.Join(h.p.Dir, "state"), h.p.Cgroup)) {
				t.Fatal("startup changed files outside process directory")
			}
			if actual := startupCatalogRows(t, path, h.p.Now); !reflect.DeepEqual(actual, beforeCatalog) {
				t.Fatalf("startup changed stored value: %v; want %v", actual, beforeCatalog)
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
			if len(list) != 0 {
				t.Fatalf("new catalog %v", list)
			}
			h.stop()
		})
	}
}

type observingSink func(context.Context, telemetry.Event) error

func (s observingSink) Deliver(ctx context.Context, event telemetry.Event) error {
	return s(ctx, event)
}

// R-I8BB-ZJ4O R-I4DL-ZCW8
func TestStartupServesNewerCatalogAndWarnsOnce(t *testing.T) {
	h := newHarness(t)
	h.set("RUN_KEEP_DAYS", "1")
	h.set("RUN_KEEP_COUNT", "2")
	script, records := seedCatalog(t, h)
	cfg := db.Config{Path: filepath.Join(h.p.Dir, "state", "scripts.db"), Migrations: scripts.Migrations(), Now: h.p.Now, Service: pages.ServiceName}
	handle, err := db.Open(context.Background(), cfg)
	mustCLI(t, err)
	queuedID := "run_aabbccddeeff0011"
	catalog := store.New(handle, store.Config{Now: h.p.Now, Rand: &countingRandom{}})
	_, err = catalog.AddRun(context.Background(), store.Run{ID: queuedID, Script: script.ID, SHA: strings.Repeat("a", 40), Ref: "main", Trigger: store.TriggerManual, Status: store.StatusQueued, User: "owner", Started: h.now})
	mustCLI(t, err)
	mustCLI(t, handle.Write(context.Background(), func(tx *sql.Tx) error {
		_, err := tx.Exec("INSERT INTO schema_migrations (version, applied_at) VALUES (9999, '2025-02-03T04:05:06Z')")
		return err
	}))
	mustCLI(t, handle.Close())
	var warning, before, after bytes.Buffer
	cfg.Stderr = &warning
	handle, err = db.Open(context.Background(), cfg)
	mustCLI(t, err)
	mustCLI(t, handle.Close())
	if !strings.HasPrefix(warning.String(), pages.ServiceName+": ") {
		t.Fatal("newer catalog did not warn")
	}
	mustCLI(t, db.Status(context.Background(), cfg, &before))
	h.p.Sink = observingSink(func(ctx context.Context, event telemetry.Event) error {
		if h.stderr.String() != warning.String() || len(h.stderr.snapshot()) != 1 {
			t.Error("warning was not written before recovery event delivery")
		}
		return h.sink.Deliver(ctx, event)
	})
	h.p.Now = func() time.Time {
		if _, err := os.Stat(filepath.Join(h.p.Dir, "state", "runs")); err == nil && h.stderr.String() != warning.String() {
			t.Error("warning was not written before recovery measured time")
		}
		return h.now
	}
	originalMCP := h.p.MCP
	h.p.MCP = func(w *telemetry.Writer) *mcp.Server {
		if h.stderr.String() != warning.String() || len(h.stderr.snapshot()) != 1 {
			t.Error("warning was not written before handler construction")
		}
		return originalMCP(w)
	}
	h.start()
	listed := h.call("list", nil)["scripts"].([]any)
	if len(listed) != 1 || listed[0].(map[string]any)["name"] != script.Name {
		t.Fatalf("newer catalog list %v", listed)
	}
	result := h.call("result", map[string]any{"run": records[1].ID})
	if result["status"] != "killed" || result["exit_code"] != nil {
		t.Fatalf("newer recovery %v", result)
	}
	result = h.call("result", map[string]any{"run": queuedID})
	if result["status"] != "failed" || result["reason"] != "queue_abandoned" || result["exit_code"] != nil {
		t.Fatalf("newer queued recovery %v", result)
	}
	if _, err := os.Stat(filepath.Join(h.p.Dir, "state", "runs", script.ID, records[0].ID)); !os.IsNotExist(err) {
		t.Fatalf("old run folder retained: %v", err)
	}
	events := h.sink.capture.Events()
	if len(events) < 4 || events[0].Name != "run.finished" || events[1].Name != "run.finished" || events[2].Name != "service.started" || events[3].Name != "request.started" {
		t.Fatalf("newer startup trail %v", events)
	}
	h.stop()
	cfg.Stderr = nil
	handle, err = db.Open(context.Background(), cfg)
	mustCLI(t, err)
	catalog = store.New(handle, store.Config{Now: h.p.Now, Rand: &countingRandom{}})
	if _, err = catalog.RunByID(context.Background(), records[0].ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("pruned record retained: %v", err)
	}
	mustCLI(t, handle.Close())
	mustCLI(t, db.Status(context.Background(), cfg, &after))
	if before.String() != after.String() || h.stderr.String() != warning.String() || len(h.stderr.snapshot()) != 1 || h.stdout.String() != "" {
		t.Fatalf("newer status/warning changed %q -> %q; %q", before.String(), after.String(), h.stderr.String())
	}
}

// R-016A-RVJW R-H2UQ-ZBX2
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

// R-HDTU-F9LB R-H5AJ-QVEG R-I0PW-U1O5
func TestCancellationAfterCatalogOpenSettlesAndFlushesRecovery(t *testing.T) {
	for _, queued := range []bool{false, true} {
		t.Run(fmt.Sprint(queued), func(t *testing.T) {
			h := newHarness(t)
			path := filepath.Join(h.p.Dir, "state", "scripts.db")
			handle, err := db.Open(context.Background(), db.Config{Path: path, Migrations: scripts.Migrations(), Now: h.p.Now})
			mustCLI(t, err)
			catalog := store.New(handle, store.Config{Now: h.p.Now, Rand: &countingRandom{}})
			script, err := catalog.Create(context.Background(), store.Draft{Owner: "owner", Name: "recover", Repo: "rep_0102030405060708", Ref: "main"})
			mustCLI(t, err)
			ids := []string{"run_0102030405060708", "run_1112131415161718"}
			for i, id := range ids {
				status := store.StatusRunning
				if i == 1 && queued {
					status = store.StatusQueued
				}
				_, err = catalog.AddRun(context.Background(), store.Run{ID: id, Script: script.ID, SHA: strings.Repeat("a", 40), Ref: "main", User: "owner", RequestID: "earlier", Trigger: store.TriggerManual, Status: status, Started: h.now.Add(-time.Hour)})
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
				status := store.StatusKilled
				if id == ids[1] && queued {
					status = store.StatusFailed
				}
				if !ok || !remaining[id] || event.Name != "run.finished" || event.Attrs["status"] != status || id == ids[1] && queued && event.Attrs["reason"] != store.ReasonQueueAbandoned {
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
				status := store.StatusKilled
				if id == ids[1] && queued {
					status = store.StatusFailed
				}
				if record.Status != status || id == ids[1] && queued && record.Reason != store.ReasonQueueAbandoned {
					t.Fatalf("unsettled recovery %v", record)
				}
			}
		})
	}
}

// R-HYA4-2I6R
func TestEmptyCatalogRecoveryFailureRefusesStartup(t *testing.T) {
	for _, cancelStart := range []bool{false, true} {
		t.Run(fmt.Sprint(cancelStart), func(t *testing.T) {
			h := newHarness(t)
			path := filepath.Join(h.p.Dir, "state", "scripts.db")
			handle, err := db.Open(context.Background(), db.Config{Path: path, Migrations: scripts.Migrations(), Now: h.p.Now})
			mustCLI(t, err)
			mustCLI(t, handle.Write(context.Background(), func(tx *sql.Tx) error {
				_, err := tx.Exec("DROP TABLE runs")
				return err
			}))
			mustCLI(t, handle.Close())
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			h.p.Banner = func(page.User) page.Banner { t.Fatal("banner after failed recovery"); return page.Banner{} }
			h.p.MCP = func(*telemetry.Writer) *mcp.Server { t.Fatal("MCP after failed recovery"); return nil }
			h.listener = &startupListener{Listener: h.listener, t: t}
			lookup := h.p.LookupEnv
			h.p.LookupEnv = func(key string) (string, bool) {
				if key == "IKIGENBA_SERVICES" && cancelStart {
					cancel()
				}
				if key == "NOTIFY_SOCKET" {
					t.Fatal("readiness after failed recovery")
				}
				return lookup(key)
			}
			if code := cli.Run(ctx, h.p); code != cli.ExitServerFailed || h.stdout.String() != "" || h.stderr.String() != "scripts: "+store.Unreachable+"\n" || len(h.stderr.snapshot()) != 1 || len(h.sink.capture.Events()) != 0 {
				t.Fatalf("recovery refusal %d %q %v", code, h.stderr.String(), h.sink.capture.Events())
			}
			if cancelStart && ctx.Err() == nil {
				t.Fatal("cancellation hook was not called")
			}
		})
	}
}

type startupListener struct {
	net.Listener
	t *testing.T
}

func (l *startupListener) Accept() (net.Conn, error) {
	l.t.Error("connection accepted during refused startup")
	return nil, errors.New("unexpected accept")
}

// R-X4UD-EYXR
func TestStartupLeavesSettledRecordsAndFoldersUntouched(t *testing.T) {
	h := newHarness(t)
	script, records := seedCatalog(t, h)
	path := filepath.Join(h.p.Dir, "state", "scripts.db")
	handle, err := db.Open(context.Background(), db.Config{Path: path, Migrations: scripts.Migrations(), Now: h.p.Now})
	mustCLI(t, err)
	catalog := store.New(handle, store.Config{Now: h.p.Now})
	_, err = catalog.FinishRun(context.Background(), records[1].ID, store.Ending{Status: store.StatusExited, ExitCode: 7, Finished: h.now, StdoutBytes: 31, StderrBytes: 17, Truncated: true})
	mustCLI(t, err)
	beforeRecords, err := catalog.Runs(context.Background(), script.ID, "", "", 100)
	mustCLI(t, err)
	// Stale stored bytes must neither replace the tables nor be rewritten at start.
	mustCLI(t, handle.Write(context.Background(), func(tx *sql.Tx) error {
		_, err := tx.Exec("INSERT OR REPLACE INTO catalog (id, value) VALUES (1, ?)", []byte("stale stored bytes\x00\xff"))
		return err
	}))
	mustCLI(t, handle.Close())
	beforeFiles := outsideSnapshot(t, filepath.Join(h.p.Dir, "state", "runs"))
	inside := outsideSnapshot(t, h.p.Dir, path, path+"-journal", path+"-wal", path+"-shm")
	outside := outsideSnapshot(t, h.root, h.p.Dir, h.p.Cgroup)
	trace := filepath.Join(h.root, "startup-git-trace")
	h.set("GIT_TRACE2_EVENT", trace)
	h.start()
	if actual := startupCatalogRows(t, path, h.p.Now); !reflect.DeepEqual(actual, map[int64]string{1: "stale stored bytes\x00\xff"}) {
		t.Fatalf("startup changed stored value: %v", actual)
	}
	if !reflect.DeepEqual(beforeFiles, outsideSnapshot(t, filepath.Join(h.p.Dir, "state", "runs"))) || !reflect.DeepEqual(outside, outsideSnapshot(t, h.root, h.p.Dir, h.p.Cgroup)) {
		t.Fatal("startup changed settled files")
	}
	if !reflect.DeepEqual(inside, outsideSnapshot(t, h.p.Dir, path, path+"-journal", path+"-wal", path+"-shm")) {
		t.Fatal("startup changed other files in process directory")
	}
	for _, record := range beforeRecords {
		result := h.call("result", map[string]any{"run": record.ID})
		if result["status"] != record.Status || result["exit_code"] != float64(record.ExitCode) {
			t.Fatalf("startup changed result: %v; before %v", result, record)
		}
	}
	h.stop()
	handle, err = db.Open(context.Background(), db.Config{Path: path, Migrations: scripts.Migrations(), Now: h.p.Now})
	mustCLI(t, err)
	defer func() { _ = handle.Close() }()
	catalog = store.New(handle, store.Config{Now: h.p.Now})
	afterRecords, err := catalog.Runs(context.Background(), script.ID, "", "", 100)
	mustCLI(t, err)
	if !reflect.DeepEqual(beforeRecords, afterRecords) || !reflect.DeepEqual(beforeFiles, outsideSnapshot(t, filepath.Join(h.p.Dir, "state", "runs"))) || !reflect.DeepEqual(outside, outsideSnapshot(t, h.root, h.p.Dir, h.p.Cgroup)) {
		t.Fatal("startup changed settled records or files")
	}
	if _, err := os.Stat(trace); !os.IsNotExist(err) {
		t.Fatalf("startup ran git: %v", err)
	}
	if h.stdout.String() != "" || h.stderr.String() != "" {
		t.Fatalf("startup streams %q %q", h.stdout.String(), h.stderr.String())
	}
}

func startupCatalogRows(t *testing.T, path string, now func() time.Time) map[int64]string {
	t.Helper()
	handle, err := db.Open(context.Background(), db.Config{Path: path, Migrations: scripts.Migrations(), Now: now})
	mustCLI(t, err)
	defer func() { _ = handle.Close() }()
	result := map[int64]string{}
	mustCLI(t, handle.Read(context.Background(), func(tx *sql.Tx) error {
		rows, err := tx.Query("SELECT id, value FROM catalog")
		if err != nil {
			return err
		}
		defer func() { _ = rows.Close() }()
		for rows.Next() {
			var id int64
			var value []byte
			if err := rows.Scan(&id, &value); err != nil {
				return err
			}
			result[id] = string(value)
		}
		return rows.Err()
	}))
	return result
}
