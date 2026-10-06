package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/dummy"
)

func testNow() time.Time { return time.Unix(123, 0).UTC() }

func futureDatabase(t *testing.T, dir string) {
	t.Helper()
	baseline, err := fs.ReadFile(dummy.Migrations(), "0001_widgets.sql")
	if err != nil {
		t.Fatal(err)
	}
	migrations := fstest.MapFS{"0001_widgets.sql": {Data: baseline}, "0002_future.sql": {Data: []byte("CREATE TABLE future(value TEXT);")}}
	d, err := db.Open(context.Background(), db.Config{Path: filepath.Join(dir, "state", "dummy.db"), Migrations: migrations, Now: testNow})
	if err != nil {
		t.Fatal(err)
	}
	if err = d.Close(); err != nil {
		t.Fatal(err)
	}
}

// R-363G-AWJ7 R-37BC-OO9W R-38J9-2G0L R-39R5-G7RA R-3C6Y-7R8O R-UFZK-A3DN
func TestDatabaseStatusDelegates(t *testing.T) {
	for _, kind := range []string{"absent", "current", "future", "invalid"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "state", "dummy.db")
			switch kind {
			case "current":
				d, err := db.Open(context.Background(), db.Config{Path: path, Migrations: dummy.Migrations(), Now: testNow})
				if err != nil {
					t.Fatal(err)
				}
				if err = d.Close(); err != nil {
					t.Fatal(err)
				}
			case "future":
				futureDatabase(t, dir)
			case "invalid":
				if err := os.WriteFile(filepath.Join(dir, "state"), []byte("file"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			var expected bytes.Buffer
			statusErr := db.Status(context.Background(), db.Config{Path: path, Migrations: dummy.Migrations()}, &expected)
			var out, diagnostics recordingWriter
			calls := 0
			p := Process{Args: []string{"db", "status"}, Dir: dir, Now: testNow, Stdout: &out, Stderr: &diagnostics, LookupEnv: func(string) (string, bool) { calls++; return "", false }, Unsetenv: func(string) error { calls++; return nil }, Inherit: func(uintptr) (net.Listener, error) { calls++; return nil, nil }}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			code := Run(ctx, p)
			wantCode, wantErr := ExitSuccess, ""
			if statusErr != nil {
				wantCode = ExitServerFailed
				wantErr = "dummy: " + strings.ReplaceAll(statusErr.Error(), "\n", " ") + "\n"
			}
			if code != wantCode || out.String() != expected.String() || diagnostics.String() != wantErr || calls != 0 {
				t.Fatalf("code=%d out=%q err=%q calls=%d expected=%q/%q", code, out.String(), diagnostics.String(), calls, expected.String(), wantErr)
			}
			if statusErr != nil && diagnostics.calls != 1 {
				t.Error("diagnostic split")
			}
			if kind == "absent" {
				entries, err := os.ReadDir(dir)
				if err != nil || len(entries) != 0 {
					t.Fatalf("status changed empty directory: %v %v", entries, err)
				}
			}
		})
	}
}

// R-3DEU-LIZD R-3AZ1-TZHZ R-33NN-JD1T R-34VJ-X4SI
func TestRefusedStartsAndCommandsLeaveDirectoryEmpty(t *testing.T) {
	for _, args := range [][]string{{"--version"}, {"manifest"}, {"--help"}, {"db"}, {"db", "other"}, {"db", "status", "extra"}, {"db", "status", "-x"}, nil} {
		dir := t.TempDir()
		var out, diagnostics recordingWriter
		code := Run(context.Background(), Process{Args: args, Dir: dir, Now: testNow, Stdout: &out, Stderr: &diagnostics})
		if len(args) > 0 && args[0] == "db" {
			arg := args[len(args)-1]
			kind := "command"
			if strings.HasPrefix(arg, "-") {
				kind = "option"
			}
			want := "dummy: unknown " + kind + " '" + arg + "'\n\nsee 'dummy --help' for usage\n"
			if code != ExitUsage || diagnostics.String() != want {
				t.Errorf("args=%v code=%d err=%q", args, code, diagnostics.String())
			}
		}
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) != 0 {
			t.Fatalf("refusal changed directory: %v %v", entries, err)
		}
	}
	for _, kind := range []string{"drain", "multiple", "inherit"} {
		dir := t.TempDir()
		env := map[string]string{"LISTEN_PID": "42", "LISTEN_FDS": "1"}
		if kind == "drain" {
			env["DRAIN_SECONDS"] = "bad"
		}
		if kind == "multiple" {
			env["LISTEN_FDS"] = "2"
		}
		Run(context.Background(), Process{Dir: dir, Now: testNow, Pid: 42, LookupEnv: mapLookup(env), Stdout: io.Discard, Stderr: io.Discard, Inherit: func(uintptr) (net.Listener, error) { return nil, errors.New("bad descriptor") }})
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) != 0 {
			t.Fatalf("refusal changed directory: %v %v", entries, err)
		}
	}
}

// R-3FUN-D2GR R-0QW9-B16C
func TestDatabaseOpenFailuresPrecedeReadinessAndTelemetry(t *testing.T) {
	for _, kind := range []string{"state-file", "invalid", "future"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			switch kind {
			case "state-file":
				if err := os.WriteFile(filepath.Join(dir, "state"), []byte("bad"), 0600); err != nil {
					t.Fatal(err)
				}
			case "invalid":
				if err := os.Mkdir(filepath.Join(dir, "state"), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "state", "dummy.db"), []byte("bad database"), 0600); err != nil {
					t.Fatal(err)
				}
			case "future":
				futureDatabase(t, dir)
			}
			_, openErr := db.Open(context.Background(), db.Config{Path: filepath.Join(dir, "state", "dummy.db"), Migrations: dummy.Migrations(), Now: testNow})
			if openErr == nil {
				t.Fatal("fixture opens")
			}
			path, notify := readySocket(t)
			ln := &observingListener{err: errors.New("unexpected accept")}
			trail := testMCP(t)
			for _, writer := range []*telemetry.Writer{nil, trail.writer} {
				var out, diagnostics recordingWriter
				code := Run(context.Background(), Process{Dir: dir, Now: testNow, Pid: 42, LookupEnv: mapLookup(map[string]string{"LISTEN_PID": "42", "LISTEN_FDS": "1", "NOTIFY_SOCKET": path}), Inherit: func(uintptr) (net.Listener, error) { return ln, nil }, Banner: func(page.User) page.Banner { t.Error("banner called"); return page.Banner{} }, Telemetry: writer, Stdout: &out, Stderr: &diagnostics})
				want := "dummy: cannot open database state/dummy.db: " + strings.ReplaceAll(openErr.Error(), "\n", " ") + "\n"
				if code != ExitServerFailed || out.Len() != 0 || diagnostics.String() != want || diagnostics.calls != 1 || ln.accepts != 0 {
					t.Fatalf("code=%d out=%q err=%q accepts=%d", code, out.String(), diagnostics.String(), ln.accepts)
				}
				assertNoNotification(t, notify)
			}
			if err := trail.writer.Flush(context.Background()); err != nil {
				t.Fatal(err)
			}
			if len(trail.capture.Events()) != 0 {
				t.Fatal("database failure recorded events")
			}
			trail.writer.Ready()
			if err := trail.writer.Flush(context.Background()); err != nil {
				t.Fatal(err)
			}
			if events := trail.capture.Events(); len(events) != 1 || events[0].Name != "service.started" {
				t.Fatalf("writer touched: %+v", events)
			}
		})
	}
}

// R-3EMQ-ZAQ2
func TestDatabaseIsReadyBeforeNotification(t *testing.T) {
	for _, stateExists := range []bool{false, true} {
		dir := t.TempDir()
		if stateExists {
			if err := os.Mkdir(filepath.Join(dir, "state"), 0700); err != nil {
				t.Fatal(err)
			}
		}
		reference := filepath.Join(t.TempDir(), "reference.db")
		handle, err := db.Open(context.Background(), db.Config{Path: reference, Migrations: dummy.Migrations(), Now: testNow})
		if err != nil {
			t.Fatal(err)
		}
		if err = handle.Close(); err != nil {
			t.Fatal(err)
		}
		var want bytes.Buffer
		if err = db.Status(context.Background(), db.Config{Path: reference, Migrations: dummy.Migrations()}, &want); err != nil {
			t.Fatal(err)
		}
		run := startConfiguredRun(t, testMCP(t), func(p *Process) { p.Dir = dir })
		path := filepath.Join(dir, "state", "dummy.db")
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			t.Fatalf("database absent: %v", err)
		}
		var got bytes.Buffer
		if err = db.Status(context.Background(), db.Config{Path: path, Migrations: dummy.Migrations()}, &got); err != nil {
			t.Fatal(err)
		}
		if got.String() != want.String() {
			t.Errorf("status=%q want=%q", got.String(), want.String())
		}
		run.stop(t)
	}
}
