package cli_test

import (
	"bytes"
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	root "github.com/ikigenba/ikigenba/telemetry"
	"github.com/ikigenba/ikigenba/telemetry/internal/cli"
	"github.com/ikigenba/ikigenba/telemetry/internal/store"
)

// R-QYKV-ANA7 R-QZSR-OF0W R-R10O-26RL R-R28K-FYIA R-R8C2-CT7R R-S41G-AUHT
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

// R-RI39-EZ5B R-S41G-AUHT
func TestRunRefusesInvalidDatabase(t *testing.T) {
	for _, fixture := range []string{"invalid", "unknown"} {
		t.Run(fixture, func(t *testing.T) {
			r := runtimeFor(t, nil)
			path := databasePath(r.p.Dir)
			var fixtureHandle *db.DB
			var before store.Page
			if fixture == "invalid" {
				if err := os.Mkdir(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("invalid database"), 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				baseline, err := fs.ReadFile(root.Migrations(), "0001_trail.sql")
				if err != nil {
					t.Fatal(err)
				}
				migrations := fstest.MapFS{"0001_trail.sql": {Data: baseline}, "0002_extra.sql": {Data: []byte("CREATE TABLE extra(id INTEGER);")}}
				h, err := db.Open(context.Background(), db.Config{Path: path, Migrations: migrations, Now: r.p.Now})
				if err != nil {
					t.Fatal(err)
				}
				fixtureHandle = h
				defer func() { _ = h.Close() }()
				fixtureStore := store.New(h)
				for _, when := range []time.Time{r.p.Now().Add(-30 * 24 * time.Hour), r.p.Now()} {
					if err := fixtureStore.Deliver(context.Background(), telemetry.Event{Time: when, Service: "sibling", Name: "fixture.event", Attrs: telemetry.Attrs{}}); err != nil {
						t.Fatal(err)
					}
				}
				before, err = fixtureStore.Search(context.Background(), store.Filter{}, 500, "")
				if err != nil {
					t.Fatal(err)
				}
			}
			_, openErr := db.Open(context.Background(), db.Config{Path: path, Migrations: root.Migrations(), Now: r.p.Now})
			if openErr == nil {
				t.Fatal("fixture accepted")
			}
			r.p.MCP = nil
			r.p.Banner = nil
			r.start()
			r.finish(t, cli.ExitServerFailed)
			lines := r.output.lines()
			want := "telemetry: cannot open database state/telemetry.db: " + strings.ReplaceAll(openErr.Error(), "\n", " ") + "\n"
			if len(lines) != 1 || lines[0] != want {
				t.Fatalf("diagnostic %q want %q", lines, want)
			}
			if fixtureHandle != nil {
				after, err := store.New(fixtureHandle).Search(context.Background(), store.Filter{}, 500, "")
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(before, after) {
					t.Fatalf("refused start changed records: before %+v after %+v", before, after)
				}
			}

		})
	}
}

// R-QQ1K-M93C R-RARV-4CP5
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
