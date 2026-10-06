package cli_test

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/repos"
	"github.com/ikigenba/ikigenba/repos/internal/cli"
)

func statusConfig(dir string) db.Config {
	return db.Config{Path: filepath.Join(dir, "state", "repos.db"), Migrations: repos.Migrations(), Now: func() time.Time { return time.Date(2001, 2, 3, 4, 5, 6, 0, time.UTC) }}
}
func statusFixture(t *testing.T, cfg db.Config, change string) {
	t.Helper()
	d, err := db.Open(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := d.Close(); err != nil {
			t.Error(err)
		}
	}()
	if change != "" {
		if err := d.Write(t.Context(), func(tx *sql.Tx) error { _, err := tx.Exec(change); return err }); err != nil {
			t.Fatal(err)
		}
	}
}

// R-Y50Z-7V4Y R-Y68V-LMVN R-Y7GR-ZEMC R-Y8OO-D6D1 R-Y9WK-QY3Q R-Y1DA-2JWV R-Y2L6-GBNK
func TestDatabaseStatusDelegation(t *testing.T) {
	for _, fixture := range []string{"absent", "applied", "pending", "unknown", "invalid"} {
		t.Run(fixture, func(t *testing.T) {
			dir := t.TempDir()
			cfg := statusConfig(dir)
			switch fixture {
			case "applied":
				statusFixture(t, cfg, "")
			case "pending":
				statusFixture(t, cfg, "DELETE FROM schema_migrations")
			case "unknown":
				statusFixture(t, cfg, "INSERT INTO schema_migrations(version,applied_at) VALUES(9999,'future')")
			case "invalid":
				if err := os.Mkdir(filepath.Dir(cfg.Path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(cfg.Path, []byte("not a database"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			var expected bytes.Buffer
			expectedErr := db.Status(context.Background(), cfg, &expected)
			var out bytes.Buffer
			var diagnostic recordedWrites
			p := untouchedProcess([]string{"db", "status"}, &out, &diagnostic, dir)
			ctx, cancel := context.WithCancel(t.Context())
			cancel()
			code := cli.Run(ctx, p)
			wantCode, wantErr := cli.ExitSuccess, ""
			if expectedErr != nil {
				wantCode = cli.ExitServerFailed
				wantErr = "repos: " + strings.ReplaceAll(expectedErr.Error(), "\n", " ") + "\n"
			}
			wantWrites := 0
			if wantErr != "" {
				wantWrites = 1
			}
			if code != wantCode || out.String() != expected.String() || diagnostic.String() != wantErr || diagnostic.calls != wantWrites {
				t.Fatalf("exit=%d out=%q diagnostic=%q calls=%d; want %d %q %q %d", code, out.String(), diagnostic.String(), diagnostic.calls, wantCode, expected.String(), wantErr, wantWrites)
			}
			var after bytes.Buffer
			afterErr := db.Status(t.Context(), cfg, &after)
			if after.String() != expected.String() || (afterErr == nil) != (expectedErr == nil) {
				t.Fatal("status changed catalog report")
			}
			if fixture == "absent" {
				assertEmptyDirectory(t, dir)
			}
			out.Reset()
			diagnostic = recordedWrites{}
			if code := cli.Run(t.Context(), cli.Process{Args: p.Args, Dir: dir, Stdout: &out, Stderr: &diagnostic}); code != wantCode || out.String() != expected.String() || diagnostic.String() != wantErr {
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
func (w *statusErrorWriter) WriteString(s string) (int, error) { return w.Write([]byte(s)) }

// R-Y8OO-D6D1 R-Y2L6-GBNK
func TestDatabaseStatusFlattensOutputError(t *testing.T) {
	dir := t.TempDir()
	var expected statusErrorWriter
	err := db.Status(t.Context(), statusConfig(dir), &expected)
	if err == nil {
		t.Fatal("fixture did not fail")
	}
	var out statusErrorWriter
	var diagnostic recordedWrites
	if code := cli.Run(t.Context(), cli.Process{Args: []string{"db", "status"}, Dir: dir, Stdout: &out, Stderr: &diagnostic}); code != cli.ExitServerFailed || out.String() != expected.String() || diagnostic.String() != "repos: "+strings.ReplaceAll(err.Error(), "\n", " ")+"\n" || diagnostic.calls != 1 {
		t.Fatalf("out=%q diagnostic=%q", out.String(), diagnostic.String())
	}
}

// R-Y68V-LMVN
func TestDatabaseStatusUsesWorkingDirectory(t *testing.T) {
	dir := t.TempDir()
	statusFixture(t, statusConfig(dir), "")
	t.Chdir(dir)
	var expected, out, diagnostic bytes.Buffer
	if err := db.Status(t.Context(), statusConfig(""), &expected); err != nil {
		t.Fatal(err)
	}
	if code := cli.Run(t.Context(), cli.Process{Args: []string{"db", "status"}, Stdout: &out, Stderr: &diagnostic}); code != cli.ExitSuccess || out.String() != expected.String() || diagnostic.Len() != 0 {
		t.Fatalf("exit=%d out=%q diagnostic=%q", code, out.String(), diagnostic.String())
	}
}
