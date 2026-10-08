package cli_test

import (
	"bytes"
	"context"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	root "github.com/ikigenba/ikigenba/telemetry"
	"github.com/ikigenba/ikigenba/telemetry/internal/cli"
	"github.com/ikigenba/ikigenba/telemetry/internal/web"
)

// R-QYKV-ANA7 R-QZSR-OF0W R-TILA-DUFZ R-TJT6-RM6O R-R28K-FYIA R-R8C2-CT7R R-TX82-Z3CB
func TestDatabaseStatus(t *testing.T) {
	for _, fixture := range []string{"absent", "applied", "unknown", "invalid"} {
		t.Run(fixture, func(t *testing.T) {
			p, out, errout := refusedProcess(t)
			p.Args = []string{"db", "status"}
			p.LookupEnv = func(string) (string, bool) { t.Error("environment consulted"); return "", false }
			path := databasePath(p.Dir)
			switch fixture {
			case "applied":
				h := openDatabase(t, path)
				if err := h.Close(); err != nil {
					t.Fatal(err)
				}
			case "unknown":
				content, err := fs.ReadFile(root.Migrations(), "0001_trail.sql")
				if err != nil {
					t.Fatal(err)
				}
				migrations := fstest.MapFS{"0001_trail.sql": {Data: content}, "0002_extra.sql": {Data: []byte("CREATE TABLE extra (id INTEGER);")}}
				h, err := db.Open(context.Background(), db.Config{Path: path, Migrations: migrations, Now: func() time.Time { return time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC) }})
				if err != nil {
					t.Fatal(err)
				}
				if err := h.Close(); err != nil {
					t.Fatal(err)
				}
			case "invalid":
				if err := os.Mkdir(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("invalid database"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			var want bytes.Buffer
			statusErr := db.Status(context.Background(), db.Config{Path: path, Migrations: root.Migrations()}, &want)
			code := cli.Run(context.Background(), p)
			wantCode, diagnostic := cli.ExitSuccess, ""
			if statusErr != nil {
				wantCode = cli.ExitServerFailed
				diagnostic = "telemetry: " + strings.ReplaceAll(statusErr.Error(), "\n", " ") + "\n"
			}
			if code != wantCode || out.String() != want.String() || errout.text() != diagnostic {
				t.Fatalf("status: %d %q %q; want %d %q %q", code, out, errout.text(), wantCode, want.String(), diagnostic)
			}
			if statusErr != nil && len(errout.calls) != 1 {
				t.Fatal("fragmented diagnostic")
			}
			if fixture == "absent" {
				assertEmpty(t, p.Dir)
			}
		})
	}
}

// R-RJB5-SQW0 R-RARV-4CP5
func TestRunCreatesDeclaredDatabase(t *testing.T) {
	for _, emptyState := range []bool{false, true} {
		t.Run(map[bool]string{false: "absent", true: "empty"}[emptyState], func(t *testing.T) {
			r := runtimeFor(t, nil)
			if emptyState {
				if err := os.Mkdir(filepath.Join(r.p.Dir, "state"), 0700); err != nil {
					t.Fatal(err)
				}
			}
			// Match the schema report to an independently created database stamped with the same clock.
			refDir := t.TempDir()
			h, err := db.Open(context.Background(), db.Config{Path: databasePath(refDir), Migrations: root.Migrations(), Now: r.p.Now})
			if err != nil {
				t.Fatal(err)
			}
			if err := h.Close(); err != nil {
				t.Fatal(err)
			}
			var want bytes.Buffer
			if err := db.Status(context.Background(), db.Config{Path: databasePath(refDir), Migrations: root.Migrations()}, &want); err != nil {
				t.Fatal(err)
			}
			r.start()
			r.ready(t)
			info, err := os.Stat(databasePath(r.p.Dir))
			if err != nil || !info.Mode().IsRegular() {
				t.Fatalf("database file: %v %v", info, err)
			}
			var got bytes.Buffer
			if err := db.Status(context.Background(), db.Config{Path: databasePath(r.p.Dir), Migrations: root.Migrations()}, &got); err != nil {
				t.Fatal(err)
			}
			if got.String() != want.String() {
				t.Fatalf("migration report %q want %q", got.String(), want.String())
			}
			r.cancel(context.Canceled)
			r.finish(t, cli.ExitSuccess)
		})
	}
}

// R-TM8Z-J5O2 R-TX82-Z3CB
func TestRunRefusesInvalidDatabase(t *testing.T) {
	r := runtimeFor(t, nil)
	path := databasePath(r.p.Dir)
	if err := os.Mkdir(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("invalid database"), 0600); err != nil {
		t.Fatal(err)
	}
	_, openErr := db.Open(context.Background(), db.Config{Path: path, Migrations: root.Migrations(), Now: r.p.Now})
	if openErr == nil {
		t.Fatal("fixture accepted")
	}
	r.p.MCP = nil
	r.p.Banner = nil
	r.start()
	r.finish(t, cli.ExitServerFailed)
	want := "telemetry: cannot open database state/telemetry.db: " + strings.ReplaceAll(openErr.Error(), "\n", " ") + "\n"
	if lines := r.output.lines(); len(lines) != 1 || lines[0] != want {
		t.Fatalf("diagnostic %q want %q", lines, want)
	}
}

// R-TNGV-WXER R-TOOS-AP5G
func TestRunWarnsAndServesUpgradedDatabase(t *testing.T) {
	r := runtimeFor(t, nil)
	warningChecked := make(chan struct{}, 1)
	r.p.Stderr = notificationCheckedOutput{output: r.output, socket: r.notify, t: t, checked: warningChecked}
	path := databasePath(r.p.Dir)
	baseline, err := fs.ReadFile(root.Migrations(), "0001_trail.sql")
	if err != nil {
		t.Fatal(err)
	}
	migrations := fstest.MapFS{"0001_trail.sql": {Data: baseline}, "0002_extra.sql": {Data: []byte("CREATE TABLE extra(id INTEGER);")}}
	h, err := db.Open(context.Background(), db.Config{Path: path, Migrations: migrations, Now: r.p.Now})
	if err != nil {
		t.Fatal(err)
	}
	if err = h.Close(); err != nil {
		t.Fatal(err)
	}
	cfg := db.Config{Path: path, Migrations: root.Migrations(), Now: r.p.Now, Service: web.ServiceName}
	var before bytes.Buffer
	if err = db.Status(context.Background(), cfg, &before); err != nil {
		t.Fatal(err)
	}
	var warning writes
	cfg.Stderr = &warning
	h, err = db.Open(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err = h.Close(); err != nil {
		t.Fatal(err)
	}
	if len(warning.calls) != 1 || !strings.HasPrefix(warning.text(), web.ServiceName+": ") {
		t.Fatalf("reference warning %q", warning.calls)
	}
	r.start()
	select {
	case <-warningChecked:
	case <-time.After(3 * time.Second):
		t.Fatal("startup observation did not arrive")
	}
	r.ready(t)
	if lines := r.output.lines(); len(lines) != 1 || lines[0] != warning.text() {
		t.Fatalf("warning before readiness: %q want %q", lines, warning.text())
	}
	w := <-r.writer
	flush(t, w)
	trail := records(t, path)
	if len(trail) != 1 || trail[0].Event != "service.started" {
		t.Fatalf("startup trail %+v", trail)
	}
	status, _ := r.request(t, "/", "ahead-request")
	if status != http.StatusOK {
		t.Fatalf("ahead server status %d", status)
	}
	flush(t, w)
	r.cancel(context.Canceled)
	r.finish(t, cli.ExitSuccess)
	if lines := r.output.lines(); len(lines) != 1 || lines[0] != warning.text() {
		t.Fatalf("final warnings %q", lines)
	}
	var after bytes.Buffer
	if err = db.Status(context.Background(), cfg, &after); err != nil {
		t.Fatal(err)
	}
	if after.String() != before.String() {
		t.Fatalf("schema changed: %q want %q", after.String(), before.String())
	}
}

// R-TG5H-MAYL R-RARV-4CP5
func TestRunDirectorySelectsTrail(t *testing.T) {
	shared := t.TempDir()
	for i, dir := range []string{shared, shared, t.TempDir()} {
		r := runtimeFor(t, nil)
		r.p.Dir = dir
		r.start()
		r.ready(t)
		r.cancel(context.Canceled)
		r.finish(t, cli.ExitSuccess)
		trail := records(t, databasePath(dir))
		want := 2
		if i == 1 {
			want = 4
		}
		if len(trail) != want {
			t.Fatalf("run %d holds %d records want %d", i, len(trail), want)
		}
	}
}
