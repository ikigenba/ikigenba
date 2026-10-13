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

// R-3MMB-1HNE R-ZV2S-V0UF R-UI3X-YAEA
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
				mustCLI(t, handle.Write(context.Background(), func(tx *sql.Tx) error {
					for _, statement := range []string{"DROP TABLE schema_migrations", "DROP TABLE subscriptions", "DROP TABLE event_runs", "DROP TABLE scripts", "DROP TABLE runs"} {
						if _, err := tx.Exec(statement); err != nil {
							return err
						}
					}
					return nil
				}))
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

type observingSink func(context.Context, telemetry.Event) error

func (s observingSink) Deliver(ctx context.Context, event telemetry.Event) error {
	return s(ctx, event)
}

// R-I8BB-ZJ4O R-I9J8-DAVD
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

// R-HDTU-F9LB R-H5AJ-QVEG R-BOZD-TYQ6
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

// R-BNRH-G6ZH
func TestTableSyncFailureRefusesStartup(t *testing.T) {
	for _, cancelStart := range []bool{false, true} {
		t.Run(fmt.Sprint(cancelStart), func(t *testing.T) {
			h := newHarness(t)
			path := filepath.Join(h.p.Dir, "state", "scripts.db")
			handle, err := db.Open(context.Background(), db.Config{Path: path, Migrations: scripts.Migrations(), Now: h.p.Now})
			mustCLI(t, err)
			mustCLI(t, handle.Write(context.Background(), func(tx *sql.Tx) error {
				_, err := tx.Exec("DROP TABLE scripts")
				return err
			}))
			mustCLI(t, handle.Close())
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			h.p.Banner = func(page.User) page.Banner { t.Fatal("banner after failed sync"); return page.Banner{} }
			h.p.MCP = func(*telemetry.Writer) *mcp.Server { t.Fatal("MCP after failed sync"); return nil }
			h.listener = &startupListener{Listener: h.listener, t: t}
			lookup := h.p.LookupEnv
			h.p.LookupEnv = func(key string) (string, bool) {
				if key == "IKIGENBA_SERVICES" && cancelStart {
					cancel()
				}
				if key == "NOTIFY_SOCKET" {
					t.Fatal("readiness after failed sync")
				}
				return lookup(key)
			}
			if code := cli.Run(ctx, h.p); code != cli.ExitServerFailed || h.stdout.String() != "" || h.stderr.String() != "scripts: "+store.Unreachable+"\n" || len(h.stderr.snapshot()) != 1 || len(h.sink.capture.Events()) != 0 {
				t.Fatalf("sync refusal %d %q %v", code, h.stderr.String(), h.sink.capture.Events())
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

// R-BQ7A-7QGV R-UI3X-YAEA
func TestStartupCorrectsStaleTablesBeforeReadyAndAfterCancellation(t *testing.T) {
	for _, mode := range []string{"ready", "cancel", "recover-prune"} {
		t.Run(mode, func(t *testing.T) {
			h := newHarness(t)
			script, records := seedCatalog(t, h)
			if mode == "recover-prune" {
				h.set("RUN_KEEP_DAYS", "1")
				h.set("RUN_KEEP_COUNT", "1")
			} else {
				handle, err := db.Open(context.Background(), db.Config{Path: filepath.Join(h.p.Dir, "state", "scripts.db"), Migrations: scripts.Migrations(), Now: h.p.Now})
				mustCLI(t, err)
				catalog := store.New(handle, store.Config{Now: h.p.Now, Rand: &countingRandom{}})
				_, err = catalog.FinishRun(context.Background(), records[1].ID, store.Ending{Status: store.StatusExited, ExitCode: 7, Finished: h.now, StdoutBytes: 31, StderrBytes: 17, Truncated: true})
				mustCLI(t, err)
				mustCLI(t, handle.Close())
			}
			path := filepath.Join(h.p.Dir, "state", "scripts.db")
			handle, err := db.Open(context.Background(), db.Config{Path: path, Migrations: scripts.Migrations(), Now: h.p.Now})
			mustCLI(t, err)
			mustCLI(t, handle.Write(context.Background(), func(tx *sql.Tx) error {
				for _, query := range []string{"DELETE FROM runs", "UPDATE scripts SET id='scr_ffffffffffffffff', ref='stale'"} {
					if _, err := tx.Exec(query); err != nil {
						return err
					}
				}
				return nil
			}))
			mustCLI(t, handle.Close())
			before := outsideSnapshot(t, filepath.Join(h.p.Dir, "state", "runs"))
			outside := outsideSnapshot(t, h.root, h.p.Dir, h.p.Cgroup)
			trace := filepath.Join(h.root, "startup-git-trace")
			h.set("GIT_TRACE2_EVENT", trace)
			if mode == "cancel" {
				mustCLI(t, os.RemoveAll(filepath.Join(h.p.Dir, "state", "runs")))
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				h.p.Now = func() time.Time {
					if _, err := os.Stat(filepath.Join(h.p.Dir, "state", "runs")); err == nil {
						cancel()
					}
					return h.now
				}
				if code := cli.Run(ctx, h.p); code != cli.ExitSuccess || len(h.sink.capture.Events()) != 0 {
					t.Fatalf("canceled sync %d %v", code, h.sink.capture.Events())
				}
				if ctx.Err() == nil {
					t.Fatal("cancellation hook was not called")
				}
			} else {
				h.start()
				assertCatalogTables(t, path, h.now)
				if mode == "ready" && !reflect.DeepEqual(before, outsideSnapshot(t, filepath.Join(h.p.Dir, "state", "runs"))) {
					t.Fatal("startup changed finished run folders")
				}
				h.stop()
			}
			assertCatalogTables(t, path, h.now)
			if !reflect.DeepEqual(outside, outsideSnapshot(t, h.root, h.p.Dir, h.p.Cgroup)) {
				t.Fatal("startup changed files outside process directory")
			}
			if _, err := os.Stat(trace); !os.IsNotExist(err) {
				t.Fatalf("startup ran git: %v", err)
			}
			if h.stdout.String() != "" || h.stderr.String() != "" {
				t.Fatalf("sync streams %q %q", h.stdout.String(), h.stderr.String())
			}
			if mode == "recover-prune" {
				handle, err := db.Open(context.Background(), db.Config{Path: path, Migrations: scripts.Migrations(), Now: func() time.Time { return h.now }})
				mustCLI(t, err)
				catalog := store.New(handle, store.Config{Now: func() time.Time { return h.now }})
				kept, err := catalog.Runs(context.Background(), script.ID)
				mustCLI(t, err)
				mustCLI(t, handle.Close())
				if len(kept) != 1 || kept[0].Status != store.StatusKilled {
					t.Fatalf("settling after sync %v", kept)
				}
			}
		})
	}
}

func assertCatalogTables(t *testing.T, path string, now time.Time) {
	t.Helper()
	ctx := context.Background()
	handle, err := db.Open(ctx, db.Config{Path: path, Migrations: scripts.Migrations(), Now: func() time.Time { return now }})
	mustCLI(t, err)
	defer func() { _ = handle.Close() }()
	catalog := store.New(handle, store.Config{Now: func() time.Time { return now }})
	scripts, err := catalog.List(ctx, "owner")
	mustCLI(t, err)
	wantScripts := make(map[string][]any)
	wantRuns := make(map[string][]any)
	for _, script := range scripts {
		wantScripts[script.ID] = mirrorCells(script.ID, script.Name, script.Owner, script.Repo, script.Ref, script.Created.UTC().Format(time.RFC3339))
		records, err := catalog.Runs(ctx, script.ID)
		mustCLI(t, err)
		for _, record := range records {
			var finished any
			if !record.Finished.IsZero() {
				finished = record.Finished.UTC().Format(time.RFC3339)
			}
			var truncated int64
			if record.Truncated {
				truncated = 1
			}
			wantRuns[record.ID] = mirrorCells(record.ID, record.Script, record.SHA, record.Ref, record.User, record.RequestID, record.Trigger, record.Event, record.Status, int64(record.ExitCode), record.Started.UTC().Format(time.RFC3339), finished, record.StdoutBytes, record.StderrBytes, truncated, record.Reason)
		}
	}
	mustCLI(t, handle.Read(ctx, func(tx *sql.Tx) error {
		for _, table := range []struct {
			name  string
			query string
			want  map[string][]any
		}{
			{"scripts", "SELECT id, typeof(id), name, typeof(name), owner, typeof(owner), repo, typeof(repo), ref, typeof(ref), created, typeof(created) FROM scripts", wantScripts},
			{"runs", "SELECT id, typeof(id), script, typeof(script), sha, typeof(sha), ref, typeof(ref), user, typeof(user), request_id, typeof(request_id), trigger, typeof(trigger), event, typeof(event), status, typeof(status), exit_code, typeof(exit_code), started, typeof(started), finished, typeof(finished), stdout_bytes, typeof(stdout_bytes), stderr_bytes, typeof(stderr_bytes), truncated, typeof(truncated), reason, typeof(reason) FROM runs", wantRuns},
		} {
			rows, err := tx.Query(table.query)
			if err != nil {
				return err
			}
			columns, err := rows.Columns()
			if err != nil {
				_ = rows.Close()
				return err
			}
			actual := make(map[string][]any)
			for rows.Next() {
				cells := make([]any, len(columns))
				pointers := make([]any, len(cells))
				for i := range cells {
					pointers[i] = &cells[i]
				}
				if err := rows.Scan(pointers...); err != nil {
					_ = rows.Close()
					return err
				}
				id, ok := cells[0].(string)
				if !ok {
					_ = rows.Close()
					return fmt.Errorf("%s row id is %T", table.name, cells[0])
				}
				actual[id] = cells
			}
			err = rows.Err()
			_ = rows.Close()
			if err != nil {
				return err
			}
			if !reflect.DeepEqual(actual, table.want) {
				t.Fatalf("%s cells %v; expected %v", table.name, actual, table.want)
			}
		}
		return nil
	}))
}

func mirrorCells(values ...any) []any {
	cells := make([]any, 0, len(values)*2)
	for _, value := range values {
		storage := "null"
		switch value.(type) {
		case string:
			storage = "text"
		case int64:
			storage = "integer"
		}
		cells = append(cells, value, storage)
	}
	return cells
}
