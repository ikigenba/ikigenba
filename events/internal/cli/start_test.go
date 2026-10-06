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
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/events"
	"github.com/ikigenba/ikigenba/events/internal/cli"
)

// R-1KXC-7WRL R-1M58-LOIA R-8Z7E-U2KG R-90FB-7UB5 R-91N7-LM1U R-92V3-ZDSJ
func TestStartRefusals(t *testing.T) {
	t.Setenv(services.Variable, "")
	for _, tc := range []struct {
		env     map[string]string
		want    string
		inherit bool
		code    int
	}{
		{map[string]string{"DRAIN_SECONDS": "abc"}, "DRAIN_SECONDS is 'abc', not a positive whole number of seconds", false, 2},
		{map[string]string{"EVENTS_RETENTION_DAYS": "2d", "EVENTS_DECLARATIONS_SECONDS": "0"}, "EVENTS_RETENTION_DAYS is '2d', not a positive whole number of days", false, 2},
		{map[string]string{}, "no socket was passed in\n\nrun it under systemd, with a listening socket passed in", false, 2},
		{map[string]string{"LISTEN_PID": "12", "LISTEN_FDS": "2"}, "2 sockets were passed in, expected 1\n\nrun it under systemd, with a listening socket passed in", false, 2},
		{map[string]string{"LISTEN_PID": "12", "LISTEN_FDS": "1"}, "bad descriptor", true, 1},
	} {
		t.Run(tc.want, func(t *testing.T) {
			var out bytes.Buffer
			var errw writes
			dir := t.TempDir()
			called := false
			p := guardedProcess(t)
			p.Pid, p.Dir, p.Stdout, p.Stderr = 12, dir, &out, &errw
			p.LookupEnv = func(k string) (string, bool) { v, ok := tc.env[k]; return v, ok }
			p.Inherit = func(fd uintptr) (net.Listener, error) {
				called = true
				if fd != 3 {
					t.Fatal(fd)
				}
				return nil, errors.New("bad descriptor")
			}
			if tc.inherit {
				p.Unsetenv = func(string) error { return nil }
			}

			code := cli.Run(context.Background(), p)
			if code != tc.code || out.Len() != 0 || errw.String() != "events: "+tc.want+"\n" || errw.Calls != 1 || called != tc.inherit {
				t.Fatal(code, out.String(), errw.String(), called)
			}
			entries, _ := os.ReadDir(dir)
			if len(entries) != 0 {
				t.Fatal(entries)
			}
		})
	}
	for _, pid := range []string{"", "11", "-12", "+12", "12x", " 12"} {
		for _, fds := range []string{"", "0", "-1", "+1", "1x", " 1"} {
			var out, errw bytes.Buffer
			p := guardedProcess(t)
			p.Pid, p.Dir, p.Stdout, p.Stderr = 12, t.TempDir(), &out, &errw
			p.LookupEnv = func(key string) (string, bool) {
				if key == "LISTEN_PID" {
					return pid, true
				}
				if key == "LISTEN_FDS" {
					return fds, true
				}
				return "", false
			}
			code := cli.Run(context.Background(), p)

			if code != cli.ExitUsage || !strings.Contains(errw.String(), "no socket was passed in") {
				t.Fatal(pid, fds, code, errw.String())
			}
		}
	}
}

type unopenedListener struct {
	net.Listener
	calls *atomic.Int32
}

func (l unopenedListener) Accept() (net.Conn, error) { l.calls.Add(1); return l.Listener.Accept() }

// R-9430-D5J8
func TestDatabaseOpenRefusal(t *testing.T) {
	t.Setenv(services.Variable, "")
	fixed := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, kind := range []string{"state_file", "bad_database", "newer_database"} {
		t.Run(kind, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "state", "events.db")
			switch kind {
			case "state_file":
				if err := os.WriteFile(filepath.Join(dir, "state"), []byte("keep"), 0600); err != nil {
					t.Fatal(err)
				}
			case "bad_database":
				if err := os.Mkdir(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte("broken"), 0600); err != nil {
					t.Fatal(err)
				}
			default:
				d, err := db.Open(context.Background(), db.Config{Path: path, Migrations: events.Migrations(), Now: func() time.Time { return fixed }})
				if err != nil {
					t.Fatal(err)
				}
				err = d.Write(context.Background(), func(tx *sql.Tx) error {
					_, e := tx.ExecContext(context.Background(), "INSERT INTO schema_migrations(version, applied_at) VALUES (2, 'future')")
					return e
				})
				if err != nil {
					t.Fatal(err)
				}
				if err := d.Close(); err != nil {
					t.Fatal(err)
				}
			}
			_, reference := db.Open(context.Background(), db.Config{Path: path, Migrations: events.Migrations(), Now: func() time.Time { return fixed }})
			if reference == nil {
				t.Fatal("reference should fail")
			}
			root, err := os.OpenRoot(dir)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = root.Close() }()
			fixture := "state/events.db"
			if kind == "state_file" {
				fixture = "state"
			}
			before, err := root.ReadFile(fixture)
			if err != nil {
				t.Fatal(err)
			}
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			var accepts atomic.Int32
			sockets := shortDir(t)
			notify, err := net.ListenUnixgram("unixgram", &net.UnixAddr{Name: filepath.Join(sockets, "ready"), Net: "unixgram"})
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = notify.Close() }()
			var out bytes.Buffer
			var errw writes
			capture := &telemetry.Capture{}
			p := guardedProcess(t)
			p.Dir, p.Pid, p.Stdout, p.Stderr, p.Sink = dir, 12, &out, &errw, capture
			p.LookupEnv = func(k string) (string, bool) {
				switch k {
				case "LISTEN_PID":
					return "12", true
				case "LISTEN_FDS":
					return "1", true
				case "NOTIFY_SOCKET":
					return notify.LocalAddr().String(), true
				}
				return "", false
			}
			p.Unsetenv = func(string) error { return nil }
			p.Now = func() time.Time { return fixed }
			p.Inherit = func(fd uintptr) (net.Listener, error) {
				if fd != 3 {
					t.Fatal(fd)
				}
				return unopenedListener{listener, &accepts}, nil
			}
			code := cli.Run(context.Background(), p)
			want := "events: cannot open database state/events.db: " + strings.ReplaceAll(reference.Error(), "\n", " ") + "\n"
			if code != cli.ExitFailure || out.Len() != 0 || errw.Calls != 1 || errw.String() != want || accepts.Load() != 0 || len(capture.Events()) != 0 {
				t.Fatal(code, errw.String(), want, accepts.Load(), capture.Events())
			}
			if kind == "newer_database" && !strings.Contains(errw.String(), "0002") {
				t.Fatal(errw.String())
			}
			after, err := root.ReadFile(fixture)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("fixture changed", err)
			}
			if err := notify.SetReadDeadline(time.Now().Add(time.Millisecond)); err != nil {
				t.Fatal(err)
			}
			var data [32]byte
			if _, _, err := notify.ReadFromUnix(data[:]); err == nil {
				t.Fatal("unexpected READY notification")
			}
		})
	}
}
