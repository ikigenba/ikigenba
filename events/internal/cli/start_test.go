package cli_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ikigenba/ikigenba/appkit/db"
	appEvents "github.com/ikigenba/ikigenba/appkit/events"
	"github.com/ikigenba/ikigenba/appkit/services"
	"github.com/ikigenba/ikigenba/appkit/telemetry"
	"github.com/ikigenba/ikigenba/events"
	"github.com/ikigenba/ikigenba/events/internal/cli"
	"github.com/ikigenba/ikigenba/events/internal/store"
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

// R-9TUH-IZEB
func TestDatabaseOpenRefusal(t *testing.T) {
	t.Setenv(services.Variable, "")
	fixed := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, kind := range []string{"state_file", "bad_database"} {
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

// R-9V2D-WR50 R-9WAA-AIVP R-A15V-TLUH R-A2DS-7DL6
func TestNewerDatabaseWarnsAndServes(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "state", "events.db")
	fixed := time.Date(2026, 10, 6, 1, 2, 3, 456789123, time.FixedZone("test", 3600))
	clock := func() time.Time { return fixed }
	d, err := db.Open(ctx, db.Config{Path: path, Migrations: events.Migrations(), Now: clock})
	if err != nil {
		t.Fatal(err)
	}
	st := store.New(d, store.Config{Now: clock, DepthMax: 8})
	if err := st.Declare(ctx, "repos", store.Declaration{Emits: []appEvents.Emission{{Event: "repo.pushed"}}}); err != nil {
		t.Fatal(err)
	}
	kept := appEvents.Event{ID: "evt_0000000000000001", Time: fixed, Service: "repos", Name: "repo.pushed", Attrs: appEvents.Attrs{}}
	old := appEvents.Event{ID: "evt_0000000000000002", Time: fixed.Add(-72 * time.Hour), Service: "repos", Name: "repo.pushed", Attrs: appEvents.Attrs{}}
	if err := st.Deliver(ctx, kept); err != nil {
		t.Fatal(err)
	}
	oldStore := store.New(d, store.Config{Now: func() time.Time { return old.Time }, DepthMax: 8})
	if err := oldStore.Deliver(ctx, old); err != nil {
		t.Fatal(err)
	}
	if err := d.Write(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, "INSERT INTO schema_migrations(version, applied_at) SELECT MAX(version) + 1, 'future' FROM schema_migrations")
		return err
	}); err != nil {
		t.Fatal(err)
	}
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	var statusBefore, expected bytes.Buffer
	if err := db.Status(ctx, db.Config{Path: path, Migrations: events.Migrations()}, &statusBefore); err != nil {
		t.Fatal(err)
	}
	reference, err := db.Open(ctx, db.Config{Path: path, Migrations: events.Migrations(), Service: appEvents.ServiceName, Stderr: &expected, Now: clock})
	if err != nil {
		t.Fatal(err)
	}
	if err := reference.Close(); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(expected.String(), "events: ") || strings.Count(expected.String(), "\n") != 1 {
		t.Fatal(expected.String())
	}
	logs := &trailWrites{}
	f := startRun(t, dir, map[string]string{}, func(f *runFixture) { f.p.Stderr = logs })
	// startRun returns only after the notification socket receives READY=1.
	if calls := logs.snapshot(); len(calls) != 1 || string(calls[0]) != expected.String() {
		t.Fatal("warning missing at readiness", calls)
	}
	result := call(t, f, "search", map[string]any{}, "newer-search")
	data, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	var answer struct {
		Structured struct {
			Records []struct {
				ID string `json:"id"`
			} `json:"records"`
		} `json:"structuredContent"`
	}
	if err := json.Unmarshal(data, &answer); err != nil {
		t.Fatal(err)
	}
	if result.IsError() || len(answer.Structured.Records) != 1 || answer.Structured.Records[0].ID != kept.ID {
		t.Fatal(string(data))
	}
	f.stop(t)
	calls := logs.snapshot()
	if len(calls) != 1 || string(calls[0]) != expected.String() || f.out.Len() != 0 {
		t.Fatal(calls, f.out.String())
	}
	started := false
	for _, event := range f.capture.Events() {
		if event.Name == "service.started" {
			started = true
			break
		}
		if event.Name != "sibling.called" {
			t.Fatal("warning recorded in trail", event)
		}
	}
	if !started {
		t.Fatal("no service.started")
	}
	var statusAfter bytes.Buffer
	if err := db.Status(ctx, db.Config{Path: path, Migrations: events.Migrations()}, &statusAfter); err != nil || statusAfter.String() != statusBefore.String() {
		t.Fatal("migrations changed", err, statusAfter.String(), statusBefore.String())
	}
}
