package cli_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/mcp"
	"github.com/ikigenba/ikigenba/appkit/page"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/sites"
	"github.com/ikigenba/ikigenba/sites/internal/cli"
)

func statusConfig(dir string) db.Config {
	return db.Config{Path: filepath.Join(dir, "state", "sites.db"), Migrations: sites.Migrations(), Now: func() time.Time { return time.Date(2021, 2, 3, 4, 5, 6, 0, time.UTC) }}
}

func statusFixture(t *testing.T, cfg db.Config, change string) {
	t.Helper()
	handle, err := db.Open(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := handle.Close(); err != nil {
			t.Error(err)
		}
	}()
	if change != "" {
		if err := handle.Write(context.Background(), func(tx *sql.Tx) error { _, err := tx.Exec(change); return err }); err != nil {
			t.Fatal(err)
		}
	}
}

// R-WGVI-LDWF R-Y1F0-HW6E R-Y2MW-VNX3 R-WFNM-7M5Q R-9VHM-MG7A R-WIJJ-A7T5 R-WX6B-VGPH R-XE8X-8937
func TestDatabaseStatusDelegation(t *testing.T) {
	for _, fixture := range []string{"absent", "applied", "pending", "unknown", "invalid", "newline-path"} {
		t.Run(fixture, func(t *testing.T) {
			dir := t.TempDir()
			if fixture == "newline-path" {
				dir = filepath.Join(dir, "line\nbreak")
				if err := os.Mkdir(dir, 0700); err != nil {
					t.Fatal(err)
				}
			}
			cfg := statusConfig(dir)
			switch fixture {
			case "applied":
				statusFixture(t, cfg, "")
			case "pending":
				statusFixture(t, cfg, "DELETE FROM schema_migrations")
			case "unknown":
				statusFixture(t, cfg, "INSERT INTO schema_migrations(version, applied_at) VALUES(9999, 'future')")
			case "invalid", "newline-path":
				if err := os.Mkdir(filepath.Dir(cfg.Path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(cfg.Path, []byte("not a database"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			var expected bytes.Buffer
			expectedErr := db.Status(context.Background(), cfg, &expected)
			if fixture == "unknown" && (expectedErr != nil || !strings.Contains(expected.String(), "9999")) {
				t.Fatal("unknown version was not reported successfully", expectedErr, expected.String())
			}
			var out, stderr observedWriter
			forbidden := func() { t.Fatal("status used process seam") }
			p := cli.Process{Args: []string{"db", "status"}, Dir: dir, Stdout: &out, Stderr: &stderr,
				LookupEnv: func(string) (string, bool) { forbidden(); return "", false }, Environ: func() []string { forbidden(); return nil }, Unsetenv: func(string) error { forbidden(); return nil },
				Inherit: func(uintptr) (net.Listener, error) { forbidden(); return nil, nil }, Now: func() time.Time { forbidden(); return time.Time{} }, Sleep: func(context.Context, time.Duration) { forbidden() }, After: func(time.Duration) <-chan time.Time { forbidden(); return nil },
				Rand: forbiddenReader{t}, Sink: forbiddenSink{t}, Banner: func(page.User) page.Banner { forbidden(); return page.Banner{} }, MCP: func(*telemetry.Writer) *mcp.Server { forbidden(); return nil },
			}
			// The command's result is independent of the caller's canceled context.
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			code := cli.Run(ctx, p)
			wantCode, wantErr := cli.ExitSuccess, ""
			if expectedErr != nil {
				wantCode = cli.ExitServerFailed
				wantErr = "sites: " + strings.ReplaceAll(expectedErr.Error(), "\n", " ") + "\n"
			}
			if code != wantCode || out.String() != expected.String() || stderr.String() != wantErr {
				t.Fatalf("code=%d stdout=%q stderr=%q want=%d,%q,%q", code, out.String(), stderr.String(), wantCode, expected.String(), wantErr)
			}
			if wantErr != "" && stderr.calls != 1 {
				t.Fatal("diagnostic was not one write")
			}
			var after bytes.Buffer
			afterErr := db.Status(context.Background(), cfg, &after)
			if after.String() != expected.String() || (afterErr == nil) != (expectedErr == nil) {
				t.Fatal("status changed catalog report")
			}
			if fixture == "absent" {
				entries, err := os.ReadDir(dir)
				if err != nil || len(entries) != 0 {
					t.Fatal("status created state", entries, err)
				}
			}
			out.Reset()
			stderr.Reset()
			if code := cli.Run(context.Background(), cli.Process{Args: p.Args, Dir: dir, Stdout: &out, Stderr: &stderr}); code != wantCode || out.String() != expected.String() || stderr.String() != wantErr {
				t.Fatal("minimal process differs")
			}
		})
	}
}

type statusErrorWriter struct{ bytes.Buffer }

func (w *statusErrorWriter) Write(p []byte) (int, error) {
	n, _ := w.Buffer.Write(p)
	return n, errors.New("output\nrefused")
}

// R-WFNM-7M5Q R-WX6B-VGPH
func TestDatabaseStatusOutputErrorFlattened(t *testing.T) {
	dir := t.TempDir()
	cfg := statusConfig(dir)
	var expected statusErrorWriter
	err := db.Status(context.Background(), cfg, &expected)
	if err == nil {
		t.Fatal("fixture did not fail")
	}
	var out statusErrorWriter
	var stderr observedWriter
	if code := cli.Run(context.Background(), cli.Process{Args: []string{"db", "status"}, Dir: dir, Stdout: &out, Stderr: &stderr}); code != cli.ExitServerFailed || out.String() != expected.String() || stderr.String() != "sites: "+strings.ReplaceAll(err.Error(), "\n", " ")+"\n" || stderr.calls != 1 {
		t.Fatalf("stdout=%q stderr=%q", out.String(), stderr.String())
	}
}

// R-Y1F0-HW6E
func TestDatabaseStatusUsesWorkingDirectory(t *testing.T) {
	dir := t.TempDir()
	statusFixture(t, statusConfig(dir), "")
	t.Chdir(dir)
	var expected bytes.Buffer
	if err := db.Status(context.Background(), statusConfig(""), &expected); err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	if cli.Run(context.Background(), cli.Process{Args: []string{"db", "status"}, Stdout: &out, Stderr: &stderr}) != cli.ExitSuccess || out.String() != expected.String() || stderr.Len() != 0 {
		t.Fatal(out.String(), stderr.String())
	}
}

func (w *statusErrorWriter) WriteString(s string) (int, error) { return w.Write([]byte(s)) }
